#pragma once
#include <windows.h>
#include <map>
#include <mutex>
#include <optional>
#define PID_UTILS_H
class ProcessStorage {
public:
    // Add a process to storage
    static void AddProcess(DWORD pid, PROCESS_INFORMATION pi);
    
    // Get process information (returns std::nullopt if not found)
    static std::optional<PROCESS_INFORMATION> GetProcess(DWORD pid);
    

private:
    static std::map<DWORD, PROCESS_INFORMATION> processes_;
    static std::mutex mutex_;
};


bool IsPidRunning(DWORD pid);
