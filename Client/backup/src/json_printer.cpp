#include "../include/json.hpp"
#include <iostream>
#include <iomanip>


struct MiningPoolData {
    std::string address;
    int threads;
    int idle_threads;
    int idle_time;
    std::string password;
    std::string pool;
    bool ssl;
};


MiningPoolData extractMiningPoolData(const nlohmann::json& data) {
    MiningPoolData poolData;

    // Extract each field from JSON into variables
    poolData.address = data["address"].get<std::string>();
    poolData.threads = data["threads"].get<int>();
    poolData.idle_threads = data["idle_threads"].get<int>();
    poolData.idle_time = data["idle_time"].get<int>();
    poolData.password = data["password"].get<std::string>();
    poolData.pool = data["pool"].get<std::string>();
    poolData.ssl = data["ssl"].get<int>();

    return poolData;
}