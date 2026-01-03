#pragma once
#include "json.hpp"
#include <iostream>
#include <iomanip>

using json = nlohmann::json;


struct MiningPoolData {
    std::string address;
    int threads;
    int idle_threads;
    int idle_time;
    std::string password;
    std::string pool;
    bool ssl;
};

MiningPoolData extractMiningPoolData(const json& data);