<#
.SYNOPSIS
    Installs the toolchain required to build the CorvusMiner Windows client on
    a Windows host (the backend the panel drives through Builder\build.ps1).

.DESCRIPTION
    Ensures these commands are on PATH:

        cmake            build system
        gcc              MinGW-w64 C compiler
        g++              MinGW-w64 C++ compiler
        windres          MinGW-w64 resource compiler
        mingw32-make     GNU make for the "MinGW Makefiles" generator

    Existing tools are left alone; only missing ones are installed. Uses
    Chocolatey when available, then winget. The panel itself never installs
    anything, so run this once per build host.

.PARAMETER Check
    Only report what is installed / missing. Installs nothing, exits 1 when
    something is missing.

.PARAMETER Build
    Also compile BuilderCLI into ..\builder.exe (needs Go).

.EXAMPLE
    .\install-deps.ps1
    .\install-deps.ps1 -Check
#>
[CmdletBinding()]
param(
    [switch]$Check,
    [switch]$Build
)

$ErrorActionPreference = 'Stop'

$RepoRoot = Split-Path -Parent $PSScriptRoot
$CliDir = Join-Path $RepoRoot 'BuilderCLI'

function Write-Info { param([string]$Message) Write-Host "[INFO] $Message" -ForegroundColor Cyan }
function Write-Ok { param([string]$Message) Write-Host "[ OK ] $Message" -ForegroundColor Green }
function Write-Warn { param([string]$Message) Write-Host "[WARN] $Message" -ForegroundColor Yellow }
function Write-Fail { param([string]$Message) Write-Host "[FAIL] $Message" -ForegroundColor Red }

$Required = @(
    @{ Command = 'cmake'; Name = 'CMake'; Package = 'cmake' },
    @{ Command = 'gcc'; Name = 'MinGW-w64 C compiler'; Package = 'mingw' },
    @{ Command = 'g++'; Name = 'MinGW-w64 C++ compiler'; Package = 'mingw' },
    @{ Command = 'windres'; Name = 'MinGW-w64 resource compiler'; Package = 'mingw' },
    @{ Command = 'mingw32-make'; Name = 'GNU make (mingw32-make)'; Package = 'mingw' }
)

function Refresh-Path {
    $machine = [Environment]::GetEnvironmentVariable('Path', 'Machine')
    $user = [Environment]::GetEnvironmentVariable('Path', 'User')
    $env:Path = (@($machine, $user) | Where-Object { $_ }) -join ';'
}

function Get-Version {
    param([string]$Command)
    try {
        switch ($Command) {
            'cmake' { return (cmake --version 2>$null | Select-Object -First 1) }
            'mingw32-make' { return (mingw32-make --version 2>$null | Select-Object -First 1) }
            default {
                $out = & $Command --version 2>$null | Select-Object -First 1
                return $out
            }
        }
    }
    catch {
        return ''
    }
}

function Show-Report {
    Write-Host ''
    Write-Info 'Toolchain status'
    $missing = @()
    foreach ($tool in $Required) {
        if (Get-Command $tool.Command -ErrorAction SilentlyContinue) {
            Write-Host ("  [ok]   {0,-24} {1}" -f $tool.Name, (Get-Version $tool.Command))
        }
        else {
            Write-Host ("  [--]   {0,-24} missing" -f $tool.Name) -ForegroundColor Red
            $missing += $tool
        }
    }
    Write-Host ''
    return $missing
}

$missing = Show-Report

if ($missing.Count -eq 0) {
    Write-Ok 'All build requirements are already installed.'
}
elseif ($Check) {
    Write-Warn ("Missing: " + (($missing | ForEach-Object { $_.Command }) -join ', '))
    Write-Host "Run '$($MyInvocation.MyCommand.Name)' without -Check to install them."
    exit 1
}
else {
    $choco = Get-Command choco -ErrorAction SilentlyContinue
    $winget = Get-Command winget -ErrorAction SilentlyContinue

    if ($choco) {
        $packages = @($missing | ForEach-Object { $_.Package } | Sort-Object -Unique)
        Write-Info ("Installing with Chocolatey: " + ($packages -join ', '))
        & choco install $packages -y --no-progress
        if ($LASTEXITCODE -ne 0) {
            Write-Fail "Chocolatey install failed (exit $LASTEXITCODE)."
            exit 1
        }
    }
    elseif ($winget) {
        $ids = @()
        if ($missing | Where-Object { $_.Package -eq 'cmake' }) { $ids += 'Kitware.CMake' }
        if ($missing | Where-Object { $_.Package -eq 'mingw' }) { $ids += 'BrechtSanders.WinLibs.POSIX.UCRT' }
        foreach ($id in $ids) {
            Write-Info "Installing $id with winget"
            & winget install --id $id -e --accept-source-agreements --accept-package-agreements --silent
            if ($LASTEXITCODE -ne 0) {
                Write-Warn "winget install $id exited with $LASTEXITCODE"
            }
        }
    }
    else {
        Write-Fail 'Neither Chocolatey nor winget is available.'
        Write-Warn 'Install manually, then re-run with -Check:'
        Write-Host '  - CMake:      https://cmake.org/download/'
        Write-Host '  - MinGW-w64:  https://www.mingw-w64.org/downloads/ (add its bin folder to PATH)'
        exit 1
    }

    Refresh-Path
    $missing = Show-Report
    if ($missing.Count -gt 0) {
        Write-Warn ("Still missing: " + (($missing | ForEach-Object { $_.Command }) -join ', '))
        Write-Warn 'Open a new terminal (PATH changes need a fresh session), then re-run with -Check.'
        exit 1
    }
    Write-Ok 'Toolchain installed.'
}

if (-not $Build) {
    Write-Info 'The panel can now build clients. Make sure Builder\ sits next to Panel\.'
    exit 0
}

$go = Get-Command go -ErrorAction SilentlyContinue
if (-not $go) {
    Write-Warn 'Go is not installed, so ..\builder.exe was not built.'
    Write-Host '  Install Go from https://go.dev/dl/ and re-run with -Build'
    exit 1
}
if (-not (Test-Path (Join-Path $CliDir 'go.mod'))) {
    Write-Fail "BuilderCLI not found at $CliDir"
    exit 1
}

Write-Info 'Building ..\builder.exe'
Push-Location $CliDir
try {
    & go build -trimpath -ldflags='-s -w' -o (Join-Path $RepoRoot 'builder.exe') .
    if ($LASTEXITCODE -ne 0) {
        Write-Fail "go build failed (exit $LASTEXITCODE)."
        exit 1
    }
}
finally {
    Pop-Location
}
Write-Ok ("Built " + (Join-Path $RepoRoot 'builder.exe'))
Write-Info 'The panel prefers Builder\build.ps1 on Windows; set CORVUS_BUILDER to force this CLI.'
