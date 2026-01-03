#pragma once

#include <string>
#include <vector>
#include <utility>

namespace AntiVM {
    // VM MAC address prefixes
    extern const std::vector<std::string> vmMacList;

    // Main VM detection function
    // Returns: pair<isVM, detectionReason>
    std::pair<bool, std::string> DetectVM();

    // Individual VM detection functions
    bool DetectVMwareWindows();
    bool DetectVirtualBoxWindows();
    bool DetectQEMU();
    bool DetectXenWindows();
    bool DetectParallels();
    bool DetectKVMWindows();
    bool DetectHostingProvider();

    // Username and MAC address checks
    bool CheckUsernameBlacklist(const std::string& username);
    std::pair<std::string, bool> CheckMacAddress();

    // IP-related functions
    std::string GetPublicIP();

    // Helper functions
    std::string GetUsername();
    bool FileExists(const std::string& path);
    bool DirectoryExists(const std::string& path);
    bool CheckRegistryKey(const std::string& keyPath);
    bool CheckServiceExists(const std::string& serviceName);
    bool CheckProcessRunning(const std::string& processName);
    std::string FetchIPFromService(const std::string& url);
    std::string ToLower(const std::string& str);

} // namespace AntiVM
