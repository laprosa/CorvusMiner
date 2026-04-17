/*
CreateFileMappingA
CreateProcessA
CreateRemoteThread
CreateRemoteThreadEx
GetModuleHandleA
GetProcAddress
GetThreadContext
HeapCreate
LoadLibraryA
LoadLibraryExA
LocalAlloc
MapViewOfFile
MapViewOfFile2
MapViewOfFile3
MapViewOfFileEx
OpenThread
Process32First
Process32Next
QueueUserAPC
ReadProcessMemory
ResumeThread
SetProcessDEPPolicy
SetThreadContext
SuspendThread
Thread32First
Thread32Next
Toolhelp32ReadProcessMemory
VirtualAlloc
VirtualAllocEx
VirtualProtect
VirtualProtectEx
WriteProcessMemory
VirtualAllocExNuma
VirtualAlloc2
VirtualAlloc2FromApp
VirtualAllocFromApp
VirtualProtectFromApp
CreateThread
WaitForSingleObject
OpenProcess
OpenFileMappingA
GetProcessHeap
GetProcessHeaps
HeapAlloc
HeapReAlloc
GlobalAlloc
AdjustTokenPrivileges
CreateProcessAsUserA
OpenProcessToken
CreateProcessWithTokenW
NtAdjustPrivilegesToken
NtAllocateVirtualMemory
NtContinue
NtCreateProcess
NtCreateProcessEx
NtCreateSection
NtCreateThread
NtCreateThreadEx
NtCreateUserProcess
NtDuplicateObject
NtMapViewOfSection
NtOpenProcess
NtOpenThread
NtProtectVirtualMemory
NtQueueApcThread
NtQueueApcThreadEx
NtQueueApcThreadEx2
NtReadVirtualMemory
NtResumeThread
NtUnmapViewOfSection
NtWaitForMultipleObjects
NtWaitForSingleObject
NtWriteVirtualMemory
RtlCreateHeap
LdrLoadDll
RtlMoveMemory
RtlCopyMemory
SetPropA
WaitForSingleObjectEx
WaitForMultipleObjects
WaitForMultipleObjectsEx
KeInsertQueueApc
Wow64SetThreadContext
NtSuspendProcess
NtResumeProcess
DuplicateToken
NtReadVirtualMemoryEx
CreateProcessInternal
EnumSystemLocalesA
UuidFromStringA
DebugActiveProcessStop
*/


#pragma once

#include <windows.h>
#include <tlhelp32.h>
#include <cstdio>
#include <string>
#include <windows.h>

#include "../../include/ntddk.h"
#include "../Instrumentation/materialization/state/Obfusk8Core.hpp"

namespace K8_ProcessManipulationAPIs
{
    using OpenProcess_t                 =   HANDLE(WINAPI*)(DWORD, BOOL, DWORD);
    using TerminateProcess_t            =       BOOL(WINAPI*)(HANDLE, UINT);
    using CreateRemoteThread_t          =       HANDLE(WINAPI*)(HANDLE, LPSECURITY_ATTRIBUTES, SIZE_T, LPTHREAD_START_ROUTINE, LPVOID, DWORD, LPDWORD);
    using VirtualAllocEx_t              =       LPVOID(WINAPI*)(HANDLE, LPVOID, SIZE_T, DWORD, DWORD);
    using WriteProcessMemory_t          =       BOOL(WINAPI*)(HANDLE, LPVOID, LPCVOID, SIZE_T, SIZE_T*);
    using ReadProcessMemory_t           =       BOOL(WINAPI*)(HANDLE, LPCVOID, LPVOID, SIZE_T, SIZE_T*);
    using GetProcAddress_t              =       FARPROC(WINAPI*)(HMODULE, LPCSTR);
    using GetModuleHandleA_t            =       HMODULE(WINAPI*)(LPCSTR);
    using NtQueryInformationProcess_t   =       NTSTATUS(WINAPI*)(HANDLE, PROCESSINFOCLASS, PVOID, ULONG, PULONG);
    using SuspendThread_t               =       DWORD(WINAPI*)(HANDLE);
    using GetCurrentProcessId_t         =       DWORD(WINAPI*)();
    using GetThreadContext_t            =       BOOL(WINAPI*)(HANDLE, LPCONTEXT);
    using SetThreadContext_t            =       BOOL(WINAPI*)(HANDLE, const CONTEXT*);
    using Wow64GetThreadContext_t       =       BOOL(WINAPI*)(HANDLE, PWOW64_CONTEXT);
    using Wow64SetThreadContext_t       =       BOOL(WINAPI*)(HANDLE, const WOW64_CONTEXT*);

    class ProcessAPI
    {
    public:
        OpenProcess_t pOpenProcess;
        TerminateProcess_t pTerminateProcess;
        CreateRemoteThread_t pCreateRemoteThread;
        VirtualAllocEx_t pVirtualAllocEx;
        WriteProcessMemory_t pWriteProcessMemory;
        ReadProcessMemory_t pReadProcessMemory;
        GetProcAddress_t pGetProcAddress;
        GetModuleHandleA_t pGetModuleHandleA;
        NtQueryInformationProcess_t pNtQueryInformationProcess;
        SuspendThread_t pSuspendThread;
        GetCurrentProcessId_t pGetCurrentProcessId;
        GetThreadContext_t pGetThreadContext;
        SetThreadContext_t pSetThreadContext;
        Wow64GetThreadContext_t pWow64GetThreadContext;
        Wow64SetThreadContext_t pWow64SetThreadContext;

        bool m_initialized;

        ProcessAPI() :
            pOpenProcess(nullptr),
            pTerminateProcess(nullptr),
            pCreateRemoteThread(nullptr),
            pVirtualAllocEx(nullptr),
            pWriteProcessMemory(nullptr),
            pReadProcessMemory(nullptr),
            pGetProcAddress(nullptr),
            pGetModuleHandleA(nullptr),
            pNtQueryInformationProcess(nullptr),
            pSuspendThread(nullptr),
            pGetCurrentProcessId(nullptr),
            pGetThreadContext(nullptr),
            pSetThreadContext(nullptr),
            pWow64GetThreadContext(nullptr),
            pWow64SetThreadContext(nullptr),
            m_initialized(false)
        {
            resolveAPIs();
        }

        bool IsInitialized() const {
            return m_initialized;
        }

    private:
        void resolveAPIs()
        {
            pOpenProcess                    =       reinterpret_cast<OpenProcess_t>(STEALTH_API_OBFSTR("kernel32.dll", "OpenProcess"));
            pTerminateProcess               =       reinterpret_cast<TerminateProcess_t>(STEALTH_API_OBFSTR("kernel32.dll", "TerminateProcess"));
            pCreateRemoteThread             =       reinterpret_cast<CreateRemoteThread_t>(STEALTH_API_OBFSTR("kernel32.dll", "CreateRemoteThread"));
            pVirtualAllocEx                 =       reinterpret_cast<VirtualAllocEx_t>(STEALTH_API_OBFSTR("kernel32.dll", "VirtualAllocEx"));
            pWriteProcessMemory             =       reinterpret_cast<WriteProcessMemory_t>(STEALTH_API_OBFSTR("kernel32.dll", "WriteProcessMemory"));
            pReadProcessMemory              =       reinterpret_cast<ReadProcessMemory_t>(STEALTH_API_OBFSTR("kernel32.dll", "ReadProcessMemory"));
            pGetProcAddress                 =       reinterpret_cast<GetProcAddress_t>(STEALTH_API_OBFSTR("kernel32.dll", "GetProcAddress"));
            pGetModuleHandleA               =       reinterpret_cast<GetModuleHandleA_t>(STEALTH_API_OBFSTR("kernel32.dll", "GetModuleHandleA"));
            pNtQueryInformationProcess      =       reinterpret_cast<NtQueryInformationProcess_t>(STEALTH_API_OBFSTR("ntdll.dll", "NtQueryInformationProcess"));
            pSuspendThread                  =       reinterpret_cast<SuspendThread_t>(STEALTH_API_OBFSTR("kernel32.dll", "SuspendThread"));
            pGetCurrentProcessId            =       reinterpret_cast<GetCurrentProcessId_t>(STEALTH_API_OBFSTR("kernel32.dll", "GetCurrentProcessId"));
            pGetThreadContext               =       reinterpret_cast<GetThreadContext_t>(STEALTH_API_OBFSTR("kernel32.dll", "GetThreadContext"));
            pSetThreadContext               =       reinterpret_cast<SetThreadContext_t>(STEALTH_API_OBFSTR("kernel32.dll", "SetThreadContext"));
            pWow64GetThreadContext          =       reinterpret_cast<Wow64GetThreadContext_t>(STEALTH_API_OBFSTR("kernel32.dll", "Wow64GetThreadContext"));
            pWow64SetThreadContext          =       reinterpret_cast<Wow64SetThreadContext_t>(STEALTH_API_OBFSTR("kernel32.dll", "Wow64SetThreadContext"));

            if (pOpenProcess && pTerminateProcess && pCreateRemoteThread && pVirtualAllocEx &&
                pWriteProcessMemory && pReadProcessMemory && pGetProcAddress &&
                pGetModuleHandleA && pNtQueryInformationProcess && pSuspendThread && pGetCurrentProcessId &&
                pGetThreadContext && pSetThreadContext && pWow64GetThreadContext && pWow64SetThreadContext)
            {
                m_initialized = true;
            }
        }
    };
}
