#pragma once
#include <string>
#include <windows.h>
#include <winhttp.h>

std::string fetchJsonFromUrl(const std::wstring& url, int useSSL = 0);
std::string postJsonToUrl(const std::wstring& url, const std::string& jsonData, int useSSL = 0);
double GetMinerHashrate();