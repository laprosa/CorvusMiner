package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var (
	errProxyRunning    = errors.New("proxy is already running")
	errProxyNotRunning = errors.New("proxy is not running")
)

const (
	defaultProxyBind = "0.0.0.0:3333"
	proxyLogBytes    = 64 << 10
	proxyStopTimeout = 5 * time.Second
	proxyKillTimeout = 2 * time.Second

	// Unexpected exits are retried with a growing delay; after
	// proxyMaxRestarts in a row the panel stops trying and reports why.
	proxyMaxRestarts   = 8
	proxyRestartBase   = 5 * time.Second
	proxyRestartMax    = 2 * time.Minute
	proxyStateFileName = "proxy-state.json"
)

// proxyPool is one upstream pool. TLS selects the pool URL scheme:
// stratum+ssl:// when true, stratum+tcp:// otherwise.
type proxyPool struct {
	URL string `json:"url"`
	TLS bool   `json:"tls"`
}

// proxySettings is the xmrig-proxy configuration managed from the panel.
type proxySettings struct {
	Pools []proxyPool `json:"pools"`
	// PoolURLs is the legacy comma-separated form; it is migrated into Pools.
	PoolURLs  string `json:"pool_urls,omitempty"`
	Wallet    string `json:"wallet"`
	Password  string `json:"password"`
	Bind      string `json:"bind"`
	Algo      string `json:"algo"`
	ExtraArgs string `json:"extra_args"`
}

var poolSchemes = []struct {
	prefix string
	tls    bool
}{
	{"stratum+tcp://", false},
	{"stratum+ssl://", true},
	{"stratum+tls://", true},
}

// splitPoolScheme strips a known pool URL scheme. ok reports whether one was
// present; an explicit scheme wins over the TLS checkbox.
func splitPoolScheme(raw string) (hostport string, tls bool, ok bool) {
	for _, scheme := range poolSchemes {
		if len(raw) >= len(scheme.prefix) && strings.EqualFold(raw[:len(scheme.prefix)], scheme.prefix) {
			return raw[len(scheme.prefix):], scheme.tls, true
		}
	}
	return raw, false, false
}

// poolURLForProxy renders the URL handed to xmrig-proxy for a pool.
func poolURLForProxy(pool proxyPool) string {
	if pool.TLS {
		return "stratum+ssl://" + pool.URL
	}
	return "stratum+tcp://" + pool.URL
}

func validatePoolURL(hostport string) error {
	if hostport == "" {
		return errors.New("pool URL is empty")
	}
	if strings.ContainsAny(hostport, " \"'\\/") || hasControlChars(hostport) {
		return fmt.Errorf("pool URL %q contains invalid characters", hostport)
	}
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		return fmt.Errorf("pool URL %q must be host:port (%v)", hostport, err)
	}
	if host == "" {
		return fmt.Errorf("pool URL %q has no host", hostport)
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("pool URL %q has an invalid port", hostport)
	}
	return nil
}

func (s proxySettings) normalized() proxySettings {
	out := s

	// Legacy: a comma-separated pool_urls string becomes the pool list.
	if len(out.Pools) == 0 && strings.TrimSpace(out.PoolURLs) != "" {
		for _, raw := range splitPoolURLs(out.PoolURLs) {
			hostport, tls, hasScheme := splitPoolScheme(strings.TrimSpace(raw))
			out.Pools = append(out.Pools, proxyPool{URL: strings.TrimSpace(hostport), TLS: hasScheme && tls})
		}
	}

	pools := make([]proxyPool, 0, len(out.Pools))
	for _, pool := range out.Pools {
		hostport, schemeTLS, hasScheme := splitPoolScheme(strings.TrimSpace(pool.URL))
		hostport = strings.TrimSpace(hostport)
		if hostport == "" {
			continue
		}
		if hasScheme {
			pool.TLS = schemeTLS
		}
		pool.URL = hostport
		pools = append(pools, pool)
	}
	out.Pools = pools
	out.PoolURLs = ""

	out.Wallet = strings.TrimSpace(out.Wallet)
	out.Bind = strings.TrimSpace(out.Bind)
	out.Algo = strings.TrimSpace(out.Algo)
	out.ExtraArgs = strings.TrimSpace(out.ExtraArgs)
	if out.Bind == "" {
		out.Bind = defaultProxyBind
	}
	return out
}

func hasControlChars(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// splitPoolURLs accepts comma, semicolon or newline separated pools.
func splitPoolURLs(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t'
	})
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (s proxySettings) validate() error {
	if len(s.Pools) == 0 {
		return errors.New("at least one pool is required")
	}
	for _, pool := range s.Pools {
		if err := validatePoolURL(pool.URL); err != nil {
			return err
		}
	}

	if s.Wallet == "" {
		return errors.New("wallet/user is required")
	}
	if strings.ContainsAny(s.Wallet, " \t\r\n\"'\\") || hasControlChars(s.Wallet) {
		return errors.New("wallet/user contains invalid characters")
	}

	_, port, err := net.SplitHostPort(s.Bind)
	if err != nil {
		return fmt.Errorf("bind address must be host:port (%v)", err)
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("bind port must be 1-65535, got %q", port)
	}

	if s.Algo != "" {
		for _, r := range s.Algo {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			case r == '.', r == '-', r == '_', r == '/':
			default:
				return fmt.Errorf("algo %q contains invalid characters", s.Algo)
			}
		}
	}

	if hasControlChars(s.Password) {
		return errors.New("password contains control characters")
	}
	if _, err := tokenizeArgs(s.ExtraArgs); err != nil {
		return err
	}
	return nil
}

// tokenizeArgs splits the extra-arguments field shell-style (quotes and
// backslash escapes). The result is passed as separate argv entries, so nothing
// is ever evaluated by a shell.
func tokenizeArgs(input string) ([]string, error) {
	var (
		args    []string
		current strings.Builder
		started bool
		single  bool
		double  bool
		escaped bool
	)
	flush := func() {
		if started {
			args = append(args, current.String())
			current.Reset()
			started = false
		}
	}

	for _, r := range input {
		if escaped {
			current.WriteRune(r)
			started = true
			escaped = false
			continue
		}
		switch {
		case r == '\\' && !single:
			escaped = true
			started = true
		case r == '\'' && !double:
			single = !single
			started = true
		case r == '"' && !single:
			double = !double
			started = true
		case (r == ' ' || r == '\t' || r == '\n' || r == '\r') && !single && !double:
			flush()
		case r < 0x20 || r == 0x7f:
			return nil, errors.New("extra arguments contain a control character")
		default:
			current.WriteRune(r)
			started = true
		}
	}

	if single || double {
		return nil, errors.New("extra arguments contain an unclosed quote")
	}
	if escaped {
		return nil, errors.New("extra arguments end with a dangling backslash")
	}
	flush()
	return args, nil
}

func buildProxyArgs(s proxySettings) []string {
	extra, _ := tokenizeArgs(s.ExtraArgs) // validated before use

	args := []string{"--no-color"}
	for _, pool := range s.Pools {
		args = append(args, "-o", poolURLForProxy(pool))
	}
	args = append(args, "-u", s.Wallet, "-b", s.Bind)
	if s.Password != "" {
		args = append(args, "-p", s.Password)
	}
	if s.Algo != "" {
		args = append(args, "--algo", s.Algo)
	}
	return append(args, extra...)
}

// ─── Ring buffer for proxy output ────────────────────────────────────────────

type ringLog struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func newRingLog(max int) *ringLog {
	return &ringLog{max: max}
}

func (r *ringLog) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = append(r.buf, p...)
	if len(r.buf) > r.max {
		r.buf = r.buf[len(r.buf)-r.max:]
	}
	return len(p), nil
}

func (r *ringLog) Reset() {
	r.mu.Lock()
	r.buf = nil
	r.mu.Unlock()
}

func (r *ringLog) Tail(maxLines int) []string {
	r.mu.Lock()
	data := string(r.buf)
	r.mu.Unlock()

	data = strings.TrimRight(data, "\r\n")
	if data == "" {
		return []string{}
	}
	lines := strings.Split(strings.ReplaceAll(data, "\r\n", "\n"), "\n")
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return lines
}

// ─── Process management ──────────────────────────────────────────────────────

type proxyProcess struct {
	cmd       *exec.Cmd
	startedAt time.Time
	done      chan struct{}
}

// proxyState is written next to the settings so a panel restart can tell that
// it left a proxy running (the process would otherwise be uncontrollable).
type proxyState struct {
	PID       int   `json:"pid"`
	StartedAt int64 `json:"started_at"`
}

type proxyManager struct {
	baseDir      string
	settingsPath string
	statePath    string

	mu           sync.Mutex
	proc         *proxyProcess
	lastExit     string
	settings     proxySettings
	binPath      string
	stopping     bool
	restartCount int
	restartAt    time.Time
	orphanPID    int
	orphanSince  int64
	log          *ringLog

	// now is overridable for tests.
	now func() time.Time
}

func newProxyManager(baseDir string) *proxyManager {
	m := &proxyManager{
		baseDir:      baseDir,
		settingsPath: filepath.Join(baseDir, "proxy-settings.json"),
		statePath:    filepath.Join(baseDir, proxyStateFileName),
		log:          newRingLog(proxyLogBytes),
		now:          time.Now,
	}
	m.loadSettings()
	m.detectOrphan()
	return m
}

// detectOrphan finds a proxy left behind by a previous panel process.
func (m *proxyManager) detectOrphan() {
	data, err := os.ReadFile(m.statePath)
	if err != nil {
		return
	}
	var state proxyState
	if err := json.Unmarshal(data, &state); err != nil || state.PID <= 0 {
		os.Remove(m.statePath)
		return
	}
	if !processAlive(state.PID) {
		os.Remove(m.statePath)
		return
	}
	m.orphanPID = state.PID
	m.orphanSince = state.StartedAt
	log.Printf("proxy: found a running proxy from a previous panel instance (pid %d); stop it from the proxy page", state.PID)
}

func (m *proxyManager) writeState(pid int) {
	if pid <= 0 {
		os.Remove(m.statePath)
		return
	}
	data, err := json.Marshal(proxyState{PID: pid, StartedAt: m.now().Unix()})
	if err != nil {
		return
	}
	if err := os.WriteFile(m.statePath, data, 0o600); err != nil {
		log.Printf("proxy: could not persist state: %v", err)
	}
}

// binaryCandidates lists the supported drop-in locations for the proxy binary.
func (m *proxyManager) binaryCandidates() []string {
	name := "xmrig-proxy"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	var candidates []string
	if env := strings.TrimSpace(os.Getenv("CORVUS_PROXY_BIN")); env != "" {
		candidates = append(candidates, env)
	}
	candidates = append(candidates,
		filepath.Join(m.baseDir, "proxy", name),
		filepath.Join(filepath.Dir(m.baseDir), name),
		filepath.Join(m.baseDir, name),
	)
	return candidates
}

func (m *proxyManager) resolveBinary() (string, error) {
	candidates := m.binaryCandidates()
	for _, candidate := range candidates {
		abs, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		info, err := os.Stat(abs)
		if err != nil || info.IsDir() {
			continue
		}
		if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
			return "", fmt.Errorf("%s is not executable; run: chmod +x %s", abs, abs)
		}
		return abs, nil
	}
	return "", fmt.Errorf("xmrig-proxy binary not found (looked for %s); drop it in Panel/proxy/ or set CORVUS_PROXY_BIN",
		strings.Join(candidates, ", "))
}

func (m *proxyManager) loadSettings() {
	data, err := os.ReadFile(m.settingsPath)
	if err != nil {
		return
	}
	var s proxySettings
	if err := json.Unmarshal(data, &s); err != nil {
		log.Printf("proxy: ignoring invalid settings file %s: %v", m.settingsPath, err)
		return
	}
	m.settings = s
}

func (m *proxyManager) saveSettings(s proxySettings) {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(m.settingsPath, data, 0o600); err != nil {
		log.Printf("proxy: could not save settings: %v", err)
	}
}

func (m *proxyManager) Start(settings proxySettings) error {
	return m.start(settings, true)
}

// start launches the proxy. manual resets the auto-restart budget; internal
// restarts keep counting so a crash loop eventually gives up.
func (m *proxyManager) start(settings proxySettings, manual bool) error {
	// Surface the orphan first: it is the actionable blocker, and reporting a
	// missing binary instead would be misleading.
	m.mu.Lock()
	orphanPID := m.orphanPID
	m.mu.Unlock()
	if orphanPID != 0 {
		return fmt.Errorf("a proxy started by a previous panel instance is still running (pid %d); stop it first", orphanPID)
	}

	settings = settings.normalized()
	if err := settings.validate(); err != nil {
		return err
	}

	binary, err := m.resolveBinary()
	if err != nil {
		return err
	}
	args := buildProxyArgs(settings)

	m.mu.Lock()
	if m.proc != nil {
		m.mu.Unlock()
		return errProxyRunning
	}

	cmd := exec.Command(binary, args...)
	cmd.Dir = filepath.Dir(binary)
	cmd.Stdout = m.log
	cmd.Stderr = m.log

	m.log.Reset()
	fmt.Fprintf(m.log, "[panel] starting %s %s\n", binary, strings.Join(args, " "))

	if err := cmd.Start(); err != nil {
		m.mu.Unlock()
		return fmt.Errorf("start proxy: %w", err)
	}

	proc := &proxyProcess{cmd: cmd, startedAt: m.now(), done: make(chan struct{})}
	m.proc = proc
	m.settings = settings
	m.binPath = binary
	m.stopping = false
	m.restartAt = time.Time{}
	if manual {
		m.restartCount = 0
		m.lastExit = ""
	}
	m.mu.Unlock()

	m.writeState(proc.cmd.Process.Pid)
	if manual {
		m.saveSettings(settings)
	}
	go m.monitor(proc)
	return nil
}

func (m *proxyManager) monitor(proc *proxyProcess) {
	err := proc.cmd.Wait()

	m.mu.Lock()
	if m.proc != proc {
		m.mu.Unlock()
		close(proc.done)
		return
	}
	m.proc = nil
	m.writeState(0)

	if err != nil {
		m.lastExit = err.Error()
	} else {
		m.lastExit = "exited"
	}

	userStopped := m.stopping
	m.stopping = false
	settings := m.settings

	restart := false
	var delay time.Duration
	if !userStopped {
		if m.restartCount >= proxyMaxRestarts {
			m.lastExit += fmt.Sprintf(" (giving up after %d automatic restarts)", proxyMaxRestarts)
		} else {
			m.restartCount++
			delay = proxyRestartDelay(m.restartCount)
			m.restartAt = m.now().Add(delay)
			restart = true
		}
	}
	m.mu.Unlock()
	close(proc.done)

	if !restart {
		return
	}

	poolSummary := "configured pools"
	if len(settings.Pools) > 0 {
		poolSummary = settings.Pools[0].URL
	}
	log.Printf("proxy: exited unexpectedly (%s); restarting in %s (attempt %d/%d)",
		poolSummary, delay, m.currentRestartCount(), proxyMaxRestarts)

	time.Sleep(delay)

	m.mu.Lock()
	cancelled := m.stopping || m.proc != nil
	m.restartAt = time.Time{}
	m.mu.Unlock()
	if cancelled {
		return
	}

	if err := m.start(settings, false); err != nil {
		log.Printf("proxy: automatic restart failed: %v", err)
		m.mu.Lock()
		m.lastExit = err.Error()
		m.mu.Unlock()
	}
}

func proxyRestartDelay(attempt int) time.Duration {
	delay := proxyRestartBase
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay >= proxyRestartMax {
			return proxyRestartMax
		}
	}
	return delay
}

func (m *proxyManager) currentRestartCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.restartCount
}

func (m *proxyManager) Stop() error {
	m.mu.Lock()
	proc := m.proc
	orphan := m.orphanPID
	pendingRestart := !m.restartAt.IsZero()
	// A user-initiated stop must also cancel a pending automatic restart.
	m.stopping = true
	m.restartCount = 0
	m.restartAt = time.Time{}
	m.mu.Unlock()

	if orphan != 0 {
		if err := killProcess(orphan); err != nil {
			return fmt.Errorf("stop orphaned proxy (pid %d): %w", orphan, err)
		}
		m.mu.Lock()
		m.orphanPID = 0
		m.orphanSince = 0
		m.mu.Unlock()
		m.writeState(0)
		log.Printf("proxy: stopped orphaned instance (pid %d)", orphan)
		return nil
	}

	if proc == nil {
		if pendingRestart {
			log.Printf("proxy: cancelled pending automatic restart")
			return nil
		}
		m.mu.Lock()
		m.stopping = false
		m.mu.Unlock()
		return errProxyNotRunning
	}

	if runtime.GOOS == "windows" {
		_ = proc.cmd.Process.Kill()
	} else if err := proc.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		_ = proc.cmd.Process.Kill()
	}

	select {
	case <-proc.done:
		return nil
	case <-time.After(proxyStopTimeout):
	}

	_ = proc.cmd.Process.Kill()
	select {
	case <-proc.done:
		return nil
	case <-time.After(proxyKillTimeout):
		return errors.New("proxy did not exit after being killed")
	}
}

type proxyStatus struct {
	Success        bool          `json:"success"`
	Running        bool          `json:"running"`
	PID            int           `json:"pid"`
	StartedAt      int64         `json:"started_at"`
	UptimeSec      int64         `json:"uptime_seconds"`
	Binary         string        `json:"binary"`
	BinaryOK       bool          `json:"binary_ok"`
	Expected       []string      `json:"expected_paths"`
	Settings       proxySettings `json:"settings"`
	LastExit       string        `json:"last_exit,omitempty"`
	Error          string        `json:"error,omitempty"`
	Log            []string      `json:"log"`
	RestartCount   int           `json:"restart_count"`
	RestartPending bool          `json:"restart_pending"`
	NextRestartSec int64         `json:"next_restart_seconds"`
	OrphanPID      int           `json:"orphan_pid"`
	OrphanUptime   int64         `json:"orphan_uptime_seconds"`
}

func (m *proxyManager) Status() proxyStatus {
	binary, binErr := m.resolveBinary()

	m.mu.Lock()
	status := proxyStatus{
		Success:  true,
		BinaryOK: binErr == nil,
		Binary:   binary,
		Expected: m.binaryCandidates(),
		Settings: m.settings,
		LastExit: m.lastExit,
	}
	status.RestartCount = m.restartCount
	if !m.restartAt.IsZero() {
		status.RestartPending = true
		status.NextRestartSec = int64(m.restartAt.Sub(m.now()).Seconds())
		if status.NextRestartSec < 0 {
			status.NextRestartSec = 0
		}
	}
	status.OrphanPID = m.orphanPID
	if m.orphanPID != 0 && m.orphanSince > 0 {
		status.OrphanUptime = m.now().Unix() - m.orphanSince
	}

	if m.proc != nil {
		status.Running = true
		status.PID = m.proc.cmd.Process.Pid
		status.StartedAt = m.proc.startedAt.Unix()
		status.UptimeSec = int64(m.now().Sub(m.proc.startedAt).Seconds())
	} else if m.binPath != "" && binary == "" {
		status.Binary = m.binPath
	}
	m.mu.Unlock()

	if binErr != nil {
		status.Error = binErr.Error()
	}
	status.Log = m.log.Tail(200)
	return status
}

// ─── HTTP handlers ───────────────────────────────────────────────────────────

// ProxyPage renders the XMRig Proxy control page.
func (h *Handler) ProxyPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	data := map[string]any{
		"quote":  getRandomQuote(),
		"status": h.proxy.Status(),
	}

	layoutPath := filepath.Join(h.baseDir, "templates", "layout.html")
	proxyTpl := filepath.Join(h.baseDir, "templates", "proxy.html")
	tmpl, err := template.New("layout").ParseFiles(layoutPath, proxyTpl)
	if err != nil {
		log.Printf("Error parsing templates: %v", err)
		http.Error(w, "Error loading template", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	tmpl.ExecuteTemplate(w, "layout", data)
}

// ProxyStatus returns the current process state and recent output.
func (h *Handler) ProxyStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeProxyJSON(w, http.StatusOK, h.proxy.Status())
}

// ProxyStart launches xmrig-proxy with the posted settings.
func (h *Handler) ProxyStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var settings proxySettings
	if err := json.NewDecoder(io.LimitReader(r.Body, 32<<10)).Decode(&settings); err != nil {
		writeProxyError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	settings = settings.normalized()
	if err := settings.validate(); err != nil {
		writeProxyError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.proxy.Start(settings); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errProxyRunning) {
			status = http.StatusConflict
		}
		writeProxyError(w, status, err.Error())
		return
	}

	log.Printf("xmrig-proxy started (pid %d)", h.proxy.Status().PID)
	writeProxyJSON(w, http.StatusOK, h.proxy.Status())
}

// ProxyStop terminates the running proxy.
func (h *Handler) ProxyStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := h.proxy.Stop(); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errProxyNotRunning) {
			status = http.StatusConflict
		}
		writeProxyError(w, status, err.Error())
		return
	}

	log.Printf("xmrig-proxy stopped")
	writeProxyJSON(w, http.StatusOK, h.proxy.Status())
}

func writeProxyError(w http.ResponseWriter, status int, message string) {
	writeProxyJSON(w, status, map[string]any{
		"success": false,
		"error":   message,
	})
}

func writeProxyJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(payload)
}
