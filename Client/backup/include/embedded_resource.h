// embedded_resource.h
#pragma once
#include <windows.h>
#include <cstdint>
#include <utility>
#include "resources.h"  // Include the resource IDs
#include <stdexcept>

void LoadEmbeddedExe(BYTE*& payloadBuf, size_t& payloadSize) {
    HMODULE hModule = GetModuleHandle(nullptr);
    if (!hModule) throw std::runtime_error("Failed to get module handle.");

    HRSRC hRes = FindResource(hModule, MAKEINTRESOURCE(EMBEDDED_EXE), RT_RCDATA);
    if (!hRes) throw std::runtime_error("Failed to find resource.");

    HGLOBAL hResData = LoadResource(hModule, hRes);
    if (!hResData) throw std::runtime_error("Failed to load resource.");

    LPVOID pData = LockResource(hResData);
    if (!pData) throw std::runtime_error("Failed to lock resource.");

    DWORD size = SizeofResource(hModule, hRes);
    if (size == 0) throw std::runtime_error("Resource size is zero.");

    payloadBuf = new BYTE[size];
    memcpy(payloadBuf, pData, size);
    payloadSize = static_cast<size_t>(size); // safe cast
}