#include "../include/process_info.h"
#include <iostream>

std::map<DWORD, PROCESS_INFORMATION> ProcessStorage::processes_;
std::mutex ProcessStorage::mutex_;

void ProcessStorage::AddProcess(DWORD pid, PROCESS_INFORMATION pi) {
    std::lock_guard<std::mutex> lock(mutex_);
    processes_[pid] = pi;
}

std::optional<PROCESS_INFORMATION> ProcessStorage::GetProcess(DWORD pid) {
    std::lock_guard<std::mutex> lock(mutex_);
    auto it = processes_.find(pid);
    if (it != processes_.end()) {
        return it->second;
    }
    return std::nullopt;
}
