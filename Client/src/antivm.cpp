#include "antivm.h"
#include <windows.h>
#include <winhttp.h>
#include <iphlpapi.h>
#include <tlhelp32.h>
#include <wininet.h>
#include <sstream>
#include <iomanip>
#include <algorithm>
#include <cctype>

#pragma comment(lib, "winhttp.lib")
#pragma comment(lib, "iphlpapi.lib")
#pragma comment(lib, "advapi32.lib")
#pragma comment(lib, "wininet.lib")

namespace AntiVM {

// VM MAC address list
const std::vector<std::string> vmMacList = {
    "00:0c:29", "00:50:56", "08:00:27", "52:54:00", "00:21:F6",
    "00:14:4F", "00:0F:4B", "00:10:E0", "00:00:7D", "00:21:28",
    "00:01:5D", "00:A0:A4", "00:07:82", "00:03:BA", "08:00:20",
    "2C:C2:60", "00:10:4F", "00:13:97", "00:20:F2"
};

// Blacklisted usernames
const std::vector<std::string> usernameBlacklist = {
    "billy", "george", "abby", "darrel jones", "john",
    "john zalinsk", "john doe", "shctaga3rm", "uv0u6479bogy",
    "8wjxnbz", "walker", "oxyt3lzggzmk", "t3wobowwaw",
    "uh6pn", "smdvvcp", "06aay3", "mlfanllp", "jpqlavkfb0lt0",
    "7hv8but5biscz", "afgxgd9fq4iv8", "frank", "anna",
    "wdagutilityaccount", "hal9th", "virus", "malware",
    "sandbox", "sample", "currentuser", "emily", "hapubws",
    "hong lee", "jaakw.q", "it-admin", "johnson", "miller",
    "milozs", "microsoft", "sand box", "maltest"
};

// Helper function to convert string to lowercase
std::string ToLower(const std::string& str) {
    std::string lower = str;
    std::transform(lower.begin(), lower.end(), lower.begin(),
        [](unsigned char c) { return std::tolower(c); });
    return lower;
}

// Get current username
std::string GetUsername() {
    char username[256];
    DWORD size = sizeof(username);
    if (GetUserNameA(username, &size)) {
        return std::string(username);
    }
    return "";
}

// Check if file exists
bool FileExists(const std::string& path) {
    DWORD attrs = GetFileAttributesA(path.c_str());
    return (attrs != INVALID_FILE_ATTRIBUTES && !(attrs & FILE_ATTRIBUTE_DIRECTORY));
}

// Check if directory exists
bool DirectoryExists(const std::string& path) {
    DWORD attrs = GetFileAttributesA(path.c_str());
    return (attrs != INVALID_FILE_ATTRIBUTES && (attrs & FILE_ATTRIBUTE_DIRECTORY));
}

// Check if registry key exists
bool CheckRegistryKey(const std::string& keyPath) {
    HKEY hKey;
    size_t pos = keyPath.find('\\');
    if (pos == std::string::npos) return false;

    std::string rootKey = keyPath.substr(0, pos);
    std::string subKey = keyPath.substr(pos + 1);

    HKEY root = HKEY_LOCAL_MACHINE;
    if (rootKey == "HKEY_LOCAL_MACHINE" || rootKey == "HKLM") {
        root = HKEY_LOCAL_MACHINE;
    } else if (rootKey == "HKEY_CURRENT_USER" || rootKey == "HKCU") {
        root = HKEY_CURRENT_USER;
    }

    LONG result = RegOpenKeyExA(root, subKey.c_str(), 0, KEY_READ, &hKey);
    if (result == ERROR_SUCCESS) {
        RegCloseKey(hKey);
        return true;
    }
    return false;
}

// Check if a Windows service exists
bool CheckServiceExists(const std::string& serviceName) {
    SC_HANDLE scManager = OpenSCManagerA(NULL, NULL, SC_MANAGER_CONNECT);
    if (!scManager) return false;

    SC_HANDLE service = OpenServiceA(scManager, serviceName.c_str(), SERVICE_QUERY_STATUS);
    bool exists = (service != NULL);

    if (service) CloseServiceHandle(service);
    CloseServiceHandle(scManager);

    return exists;
}

// Check if a process is running
bool CheckProcessRunning(const std::string& processName) {
    HANDLE snapshot = CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0);
    if (snapshot == INVALID_HANDLE_VALUE) return false;

    PROCESSENTRY32 processEntry;
    processEntry.dwSize = sizeof(PROCESSENTRY32);

    std::string lowerProcessName = ToLower(processName);

    if (Process32First(snapshot, &processEntry)) {
        do {
            std::wstring wideExeFile(processEntry.szExeFile);
            std::string exeFile = ToLower(std::string(wideExeFile.begin(), wideExeFile.end()));
            if (exeFile.find(lowerProcessName) != std::string::npos) {
                CloseHandle(snapshot);
                return true;
            }
        } while (Process32Next(snapshot, &processEntry));
    }

    CloseHandle(snapshot);
    return false;
}

// DetectVM - Main detection function
std::pair<bool, std::string> DetectVM() {
    // Check username blacklist
    std::string username = GetUsername();
    if (CheckUsernameBlacklist(username)) {
        return std::make_pair(true, "Blacklisted username: " + username);
    }

    // Check MAC addresses
    auto macResult = CheckMacAddress();
    if (macResult.second) {
        return std::make_pair(true, "Suspicious MAC address: " + macResult.first);
    }

    // Check for VMware
    if (DetectVMwareWindows()) {
        return std::make_pair(true, "VMware detected");
    }

    // Check for VirtualBox
    if (DetectVirtualBoxWindows()) {
        return std::make_pair(true, "VirtualBox detected");
    }

    // Check for QEMU
    if (DetectQEMU()) {
        return std::make_pair(true, "QEMU detected");
    }

    // Check for Xen
    if (DetectXenWindows()) {
        return std::make_pair(true, "Xen detected");
    }

    // Check for Parallels
    if (DetectParallels()) {
        return std::make_pair(true, "Parallels detected");
    }

    // Check for KVM
    if (DetectKVMWindows()) {
        return std::make_pair(true, "KVM detected");
    }

    // Check for datacenter/hosting provider
    if (DetectHostingProvider()) {
        return std::make_pair(true, "Hosting provider detected");
    }

    return std::make_pair(false, "No VM detected");
}

// DetectVMwareWindows
bool DetectVMwareWindows() {
    // Check for VMware driver
    if (FileExists("C:\\Windows\\System32\\drivers\\vmci.sys")) {
        return true;
    }

    // Check for VMware Tools service
    if (CheckServiceExists("VMTools")) {
        return true;
    }

    // Check for VMware registry entry
    if (CheckRegistryKey("HKEY_LOCAL_MACHINE\\Software\\VMware, Inc.")) {
        return true;
    }

    // Check for VMware specific files
    std::vector<std::string> vmwareFiles = {
        "C:\\Program Files\\VMware\\VMware Tools\\vmtoolsd.exe",
        "C:\\Program Files (x86)\\VMware\\VMware Tools\\vmtoolsd.exe"
    };

    for (const auto& file : vmwareFiles) {
        if (FileExists(file)) {
            return true;
        }
    }

    return false;
}

// DetectVirtualBoxWindows
bool DetectVirtualBoxWindows() {
    // Check for VirtualBox driver
    if (FileExists("C:\\Windows\\System32\\drivers\\VBoxMouse.sys")) {
        return true;
    }

    // Check for VirtualBox registry entry
    if (CheckRegistryKey("HKEY_LOCAL_MACHINE\\Software\\Oracle\\VirtualBox")) {
        return true;
    }

    // Check for VirtualBox specific directories
    std::vector<std::string> vboxDirs = {
        "C:\\Program Files\\Oracle\\VirtualBox",
        "C:\\Program Files (x86)\\Oracle\\VirtualBox"
    };

    for (const auto& dir : vboxDirs) {
        if (DirectoryExists(dir)) {
            return true;
        }
    }

    // Check for VirtualBox process
    if (CheckProcessRunning("VBoxTray.exe")) {
        return true;
    }

    return false;
}

// DetectQEMU
bool DetectQEMU() {
    // Check for QEMU driver
    if (FileExists("C:\\Windows\\System32\\drivers\\qemu-ga.sys")) {
        return true;
    }

    // Check for QEMU process
    if (CheckProcessRunning("qemu-system")) {
        return true;
    }

    return false;
}

// DetectXenWindows
bool DetectXenWindows() {
    // Check for Xen driver
    if (FileExists("C:\\Windows\\System32\\drivers\\xenevtchn.sys")) {
        return true;
    }

    // Check for Xen registry entry
    if (CheckRegistryKey("HKEY_LOCAL_MACHINE\\Software\\Xen")) {
        return true;
    }

    return false;
}

// DetectParallels
bool DetectParallels() {
    // Check for Parallels driver
    if (FileExists("C:\\Windows\\System32\\drivers\\prlfs.sys")) {
        return true;
    }

    // Check for Parallels registry entry
    if (CheckRegistryKey("HKEY_LOCAL_MACHINE\\Software\\Parallels")) {
        return true;
    }

    // Check for Parallels specific directories
    std::vector<std::string> parallelDirs = {
        "C:\\Program Files\\Parallels\\Parallels Tools",
        "C:\\Program Files (x86)\\Parallels\\Parallels Tools"
    };

    for (const auto& dir : parallelDirs) {
        if (DirectoryExists(dir)) {
            return true;
        }
    }

    return false;
}

// DetectKVMWindows
bool DetectKVMWindows() {
    // Check for KVM driver
    if (FileExists("C:\\Windows\\System32\\drivers\\balloon.sys")) {
        return true;
    }

    // Check for KVM process
    if (CheckProcessRunning("kvm.exe")) {
        return true;
    }

    return false;
}

// CheckUsernameBlacklist
bool CheckUsernameBlacklist(const std::string& username) {
    std::string usernameLower = ToLower(username);

    for (const auto& blacklistedName : usernameBlacklist) {
        if (usernameLower.find(blacklistedName) != std::string::npos) {
            return true;
        }
    }

    return false;
}

// CheckMacAddress
std::pair<std::string, bool> CheckMacAddress() {
    ULONG bufferSize = 15000;
    PIP_ADAPTER_INFO adapterInfo = (IP_ADAPTER_INFO*)malloc(bufferSize);
    
    if (adapterInfo == NULL) {
        return std::make_pair("", false);
    }

    if (GetAdaptersInfo(adapterInfo, &bufferSize) == ERROR_BUFFER_OVERFLOW) {
        free(adapterInfo);
        adapterInfo = (IP_ADAPTER_INFO*)malloc(bufferSize);
        if (adapterInfo == NULL) {
            return std::make_pair("", false);
        }
    }

    if (GetAdaptersInfo(adapterInfo, &bufferSize) == NO_ERROR) {
        PIP_ADAPTER_INFO adapter = adapterInfo;
        
        while (adapter) {
            // Skip loopback
            if (adapter->Type == MIB_IF_TYPE_LOOPBACK) {
                adapter = adapter->Next;
                continue;
            }

            // Format MAC address
            std::ostringstream macStream;
            for (UINT i = 0; i < adapter->AddressLength; i++) {
                if (i > 0) macStream << ":";
                macStream << std::hex << std::setfill('0') << std::setw(2) 
                         << (int)adapter->Address[i];
            }

            std::string mac = macStream.str();
            if (mac.length() >= 8) {
                std::string macPrefix = ToLower(mac.substr(0, 8));
                
                for (const auto& vmMac : vmMacList) {
                    std::string vmMacLower = ToLower(vmMac);
                    if (macPrefix.find(vmMacLower) == 0) {
                        free(adapterInfo);
                        return std::make_pair(mac, true);
                    }
                }
            }

            adapter = adapter->Next;
        }
    }

    free(adapterInfo);
    return std::make_pair("", false);
}

// FetchIPFromService
std::string FetchIPFromService(const std::string& url) {
    std::string result;
    
    // Parse URL
    URL_COMPONENTSA urlComp;
    ZeroMemory(&urlComp, sizeof(urlComp));
    urlComp.dwStructSize = sizeof(urlComp);
    
    char hostName[256] = {0};
    char urlPath[1024] = {0};
    
    urlComp.lpszHostName = hostName;
    urlComp.dwHostNameLength = sizeof(hostName);
    urlComp.lpszUrlPath = urlPath;
    urlComp.dwUrlPathLength = sizeof(urlPath);

    if (!InternetCrackUrlA(url.c_str(), url.length(), 0, &urlComp)) {
        return "";
    }

    HINTERNET hSession = WinHttpOpen(L"AntiVM/1.0", 
                                      WINHTTP_ACCESS_TYPE_DEFAULT_PROXY,
                                      WINHTTP_NO_PROXY_NAME, 
                                      WINHTTP_NO_PROXY_BYPASS, 0);
    if (!hSession) return "";

    std::wstring wHostName(hostName, hostName + strlen(hostName));
    HINTERNET hConnect = WinHttpConnect(hSession, wHostName.c_str(), 
                                        urlComp.nPort, 0);
    if (!hConnect) {
        WinHttpCloseHandle(hSession);
        return "";
    }

    std::wstring wUrlPath(urlPath, urlPath + strlen(urlPath));
    HINTERNET hRequest = WinHttpOpenRequest(hConnect, L"GET", wUrlPath.c_str(),
                                            NULL, WINHTTP_NO_REFERER,
                                            WINHTTP_DEFAULT_ACCEPT_TYPES,
                                            (urlComp.nScheme == INTERNET_SCHEME_HTTPS) ? 
                                            WINHTTP_FLAG_SECURE : 0);
    if (!hRequest) {
        WinHttpCloseHandle(hConnect);
        WinHttpCloseHandle(hSession);
        return "";
    }

    if (WinHttpSendRequest(hRequest, WINHTTP_NO_ADDITIONAL_HEADERS, 0,
                           WINHTTP_NO_REQUEST_DATA, 0, 0, 0) &&
        WinHttpReceiveResponse(hRequest, NULL)) {
        
        DWORD bytesAvailable = 0;
        char buffer[1024];
        
        while (WinHttpQueryDataAvailable(hRequest, &bytesAvailable) && bytesAvailable > 0) {
            DWORD bytesRead = 0;
            DWORD toRead = min(bytesAvailable, sizeof(buffer) - 1);
            
            if (WinHttpReadData(hRequest, buffer, toRead, &bytesRead)) {
                buffer[bytesRead] = '\0';
                result += buffer;
            }
        }
    }

    WinHttpCloseHandle(hRequest);
    WinHttpCloseHandle(hConnect);
    WinHttpCloseHandle(hSession);

    // Trim whitespace
    result.erase(0, result.find_first_not_of(" \t\n\r"));
    result.erase(result.find_last_not_of(" \t\n\r") + 1);

    return result;
}

// GetPublicIP
std::string GetPublicIP() {
    std::vector<std::string> services = {
        "https://api.ipify.org?format=text",
        "https://icanhazip.com",
        "https://ifconfig.me",
        "https://checkip.amazonaws.com"
    };

    for (const auto& service : services) {
        std::string ip = FetchIPFromService(service);
        if (!ip.empty() && ip != "unknown") {
            return ip;
        }
    }

    return "unknown";
}

// DetectHostingProvider
bool DetectHostingProvider() {
    std::string response = FetchIPFromService("https://api.ipapi.is/");
    
    if (response.empty()) {
        return false;
    }

    // Simple JSON parsing for "is_datacenter" field
    size_t pos = response.find("\"is_datacenter\"");
    if (pos != std::string::npos) {
        size_t truePos = response.find("true", pos);
        size_t falsePos = response.find("false", pos);
        
        if (truePos != std::string::npos && 
            (falsePos == std::string::npos || truePos < falsePos)) {
            return true;
        }
    }

    return false;
}

} // namespace AntiVM
