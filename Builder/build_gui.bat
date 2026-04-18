@echo off
REM CorvusMiner GUI Builder Launcher
REM This script launches the GUI builder with automatic dependency installation

setlocal enabledelayedexpansion

REM Check if Python is installed
python --version >nul 2>&1
if errorlevel 1 (
    echo Error: Python 3 is not installed or not in PATH
    echo Please install Python from https://www.python.org/downloads/
    echo Make sure to check "Add Python to PATH" during installation
    pause
    exit /b 1
)

REM Check if PySimpleGUI is installed, if not, install it
python -c "import PySimpleGUI" >nul 2>&1
if errorlevel 1 (
    echo Installing PySimpleGUI...
    python -m pip install --upgrade pip -q
    python -m pip install PySimpleGUI>=4.60.0 -q
    if errorlevel 1 (
        echo Error: Failed to install PySimpleGUI
        pause
        exit /b 1
    )
    echo PySimpleGUI installed successfully!
)

REM Launch the GUI
echo Launching CorvusMiner GUI Builder...
python "%~dp0build_gui.py"
if errorlevel 1 (
    echo Error running build_gui.py
    pause
    exit /b 1
)

endlocal
