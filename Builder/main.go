package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
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
)

func init() {
	reader = bufio.NewReader(os.Stdin)
	clientDir = "./Client"
	buildDir = "./Client/build"
}

func main() {
	flag.Parse()

	// If flags are provided, use non-interactive mode
	if *panelURLFlag != "" {
		if err := buildClientNonInteractive(); err != nil {
			logError("Build failed: %v", err)
			os.Exit(1)
		}
		return
	}

	// Otherwise, use interactive mode
	linuxMain()
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
