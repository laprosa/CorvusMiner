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

void ProcessStorage::RemoveProcess(DWORD pid) {
    // Only drops the entry: the handles were already closed by the owner
    // (main.cpp / transacted_hollowing). Without this the map grew forever and
    // kept stale HANDLE values around that could be reused by a later PID.
    std::lock_guard<std::mutex> lock(mutex_);
    processes_.erase(pid);
}

void ProcessStorage::TerminateAll() {
    std::lock_guard<std::mutex> lock(mutex_);
    for (auto& entry : processes_) {
        if (entry.second.hProcess) {
            TerminateProcess(entry.second.hProcess, 0);
        }
    }
}
