#pragma once

#include <string>

namespace Persistence {
    // Add current executable to Windows Run registry key for persistence
    // Returns true on success, false on failure
    bool AddToStartup();
    
    // Remove current executable from Windows Run registry key
    // Returns true on success, false on failure
    bool RemoveFromStartup();
    
    // Helper functions
    std::string GetExecutablePath();
    std::string GetExecutableName();
    
} // namespace Persistence
