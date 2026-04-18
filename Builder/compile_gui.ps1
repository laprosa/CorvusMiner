# Compile GUI to executable with PyInstaller
# Usage: .\compile_gui.ps1
# Output: ./dist/build_gui.exe

param(
    [switch]$Onedir  # Use --onedir instead of --onefile for faster builds
)

Write-Host "Building CorvusMiner GUI Builder Executable..." -ForegroundColor Cyan

# Determine output type
$outputType = if ($Onedir) { "--onedir" } else { "--onefile" }

# Find Python - try multiple approaches
$pythonExe = $null

# First, try the activated venv (if any)
$pythonExe = (Get-Command python.exe -ErrorAction SilentlyContinue).Source
if ($pythonExe) {
    Write-Host "[*] Using activated Python: $pythonExe" -ForegroundColor Yellow
} else {
    # Try to find in .venv subdirectories
    $paths = @(
        "..\..\venv\Scripts\python.exe",
        "..\.venv\Scripts\python.exe",
        ".venv\Scripts\python.exe",
        "..\venv\Scripts\python.exe"
    )
    
    foreach ($path in $paths) {
        $fullPath = Join-Path $PSScriptRoot $path
        if (Test-Path $fullPath) {
            $pythonExe = $fullPath
            Write-Host "[*] Found Python at: $pythonExe" -ForegroundColor Yellow
            break
        }
    }
}

if (-not $pythonExe) {
    Write-Host "ERROR: Python environment not found!" -ForegroundColor Red
    Write-Host "Make sure either:" -ForegroundColor Yellow
    Write-Host "  1. The .venv virtual environment is activated (you have '(.venv)' in your prompt)" -ForegroundColor Yellow
    Write-Host "  2. Or the .venv folder exists at C:\Users\admin\Desktop\CorvusMiner\.venv" -ForegroundColor Yellow
    exit 1
}

Write-Host "`n[*] Running PyInstaller..." -ForegroundColor Green
& $pythonExe -m PyInstaller `
    --name "build_gui" `
    --windowed `
    $outputType `
    --specpath build_spec `
    --distpath dist `
    --workpath build `
    --clean `
    --collect-data PySimpleGUI `
    build_gui.py

if ($LASTEXITCODE -eq 0) {
    Write-Host "`n[+] Build complete!" -ForegroundColor Green
    
    $exePath = if ($Onedir) { ".\dist\build_gui\build_gui.exe" } else { ".\dist\build_gui.exe" }
    
    if (Test-Path $exePath) {
        Write-Host "[+] Executable: $exePath" -ForegroundColor Green
        Write-Host "`nYou can now distribute this .exe file to customers!" -ForegroundColor Cyan
        Write-Host "No Python installation required on their machine." -ForegroundColor Cyan
        
        # Offer to open the output folder
        $response = Read-Host -Prompt "`nOpen dist folder? (Y/n)"
        if ($response -ne 'n' -and $response -ne 'N') {
            explorer.exe .\dist
        }
    }
} else {
    Write-Host "`n[ERROR] Build failed!" -ForegroundColor Red
    exit 1
}
