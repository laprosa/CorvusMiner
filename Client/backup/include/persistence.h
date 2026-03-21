#pragma once

#include <string>

namespace Persistence {
    // Returns true if the current process has administrator privileges
    bool IsRunningAsAdmin();

    // If admin: creates a scheduled task that runs at any user logon with highest privileges.
    // If not admin: adds the executable to HKCU Run registry key.
    // Returns true on success, false on failure
    bool AddToStartup();
    
    // Removes both the scheduled task and the Run registry key (whichever exists).
    // Returns true if at least one removal succeeded.
    bool RemoveFromStartup();
    
    // Helper functions
    std::string GetExecutablePath();
    std::string GetExecutableName();
    
} // namespace Persistence
