package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestValidateProxySettings(t *testing.T) {
	valid := func() proxySettings {
		return proxySettings{
			Pools:    []proxyPool{{URL: "pool.example.com:3333"}},
			Wallet:   "WALLET",
			Password: "x",
			Bind:     "0.0.0.0:3333",
		}.normalized()
	}

	tests := []struct {
		name    string
		mutate  func(*proxySettings)
		wantErr string
	}{
		{"valid", func(*proxySettings) {}, ""},
		{"two pools", func(s *proxySettings) {
			s.Pools = []proxyPool{{URL: "a.example:3333"}, {URL: "b.example:4444", TLS: true}}
		}, ""},
		{"no pool", func(s *proxySettings) { s.Pools = nil }, "at least one pool"},
		{"pool without port", func(s *proxySettings) { s.Pools = []proxyPool{{URL: "pool.example.com"}} }, "host:port"},
		{"pool with space", func(s *proxySettings) { s.Pools = []proxyPool{{URL: "pool example:3333"}} }, "invalid characters"},
		{"pool with path", func(s *proxySettings) { s.Pools = []proxyPool{{URL: "pool.example.com:3333/x"}} }, "invalid characters"},
		{"no wallet", func(s *proxySettings) { s.Wallet = "" }, "wallet"},
		{"wallet with newline", func(s *proxySettings) { s.Wallet = "a\nb" }, "invalid characters"},
		{"bad bind", func(s *proxySettings) { s.Bind = "3333" }, "host:port"},
		{"bad bind port", func(s *proxySettings) { s.Bind = "0.0.0.0:99999" }, "1-65535"},
		{"bad algo", func(s *proxySettings) { s.Algo = "rx/0;rm -rf" }, "algo"},
		{"unclosed quote", func(s *proxySettings) { s.ExtraArgs = `--http-host "127.0.0.1` }, "unclosed quote"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := valid()
			tc.mutate(&s)
			err := s.validate()
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

func TestNormalizeProxySettingsDefaultBind(t *testing.T) {
	s := proxySettings{Pools: []proxyPool{{URL: "pool:3333"}}, Wallet: "w"}.normalized()
	if s.Bind != defaultProxyBind {
		t.Errorf("bind = %q, want %q", s.Bind, defaultProxyBind)
	}
}

func TestNormalizeMigratesLegacyPools(t *testing.T) {
	s := proxySettings{
		PoolURLs: "a.example:3333, stratum+ssl://b.example:4444",
		Wallet:   "w",
	}.normalized()

	if len(s.Pools) != 2 {
		t.Fatalf("pools = %+v", s.Pools)
	}
	if s.Pools[0].URL != "a.example:3333" || s.Pools[0].TLS {
		t.Errorf("first pool = %+v", s.Pools[0])
	}
	if s.Pools[1].URL != "b.example:4444" || !s.Pools[1].TLS {
		t.Errorf("second pool = %+v", s.Pools[1])
	}
	if s.PoolURLs != "" {
		t.Errorf("legacy field should be cleared, got %q", s.PoolURLs)
	}
}

func TestNormalizeExplicitPoolSchemeWins(t *testing.T) {
	s := proxySettings{
		Pools: []proxyPool{
			{URL: "stratum+ssl://a.example:3333", TLS: false},
			{URL: "stratum+tcp://b.example:4444", TLS: true},
			{URL: "stratum+tls://c.example:5555", TLS: false},
		},
		Wallet: "w",
	}.normalized()

	if !s.Pools[0].TLS || s.Pools[0].URL != "a.example:3333" {
		t.Errorf("ssl pool = %+v", s.Pools[0])
	}
	if s.Pools[1].TLS || s.Pools[1].URL != "b.example:4444" {
		t.Errorf("tcp pool = %+v", s.Pools[1])
	}
	if !s.Pools[2].TLS {
		t.Errorf("tls pool = %+v", s.Pools[2])
	}
}

func TestPoolURLForProxy(t *testing.T) {
	if got := poolURLForProxy(proxyPool{URL: "pool.example.com:3333", TLS: true}); got != "stratum+ssl://pool.example.com:3333" {
		t.Errorf("tls url = %q", got)
	}
	if got := poolURLForProxy(proxyPool{URL: "pool.example.com:3333", TLS: false}); got != "stratum+tcp://pool.example.com:3333" {
		t.Errorf("plain url = %q", got)
	}
}

func TestTokenizeArgs(t *testing.T) {
	tests := []struct {
		in      string
		want    []string
		wantErr string
	}{
		{"", nil, ""},
		{"--http-host 127.0.0.1 --http-port 8081", []string{"--http-host", "127.0.0.1", "--http-port", "8081"}, ""},
		{`--pass "two words"`, []string{"--pass", "two words"}, ""},
		{`--pass 'two words'`, []string{"--pass", "two words"}, ""},
		{`a\ b`, []string{"a b"}, ""},
		{`--x "`, nil, "unclosed quote"},
		{`trailing\`, nil, "dangling backslash"},
		{"bad\x01arg", nil, "control character"},
	}
	for _, tc := range tests {
		got, err := tokenizeArgs(tc.in)
		if tc.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("tokenizeArgs(%q): want %q, got %v", tc.in, tc.wantErr, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("tokenizeArgs(%q): %v", tc.in, err)
		}
		if strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Errorf("tokenizeArgs(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestBuildProxyArgs(t *testing.T) {
	args := buildProxyArgs(proxySettings{
		Pools: []proxyPool{
			{URL: "a.example:3333"},
			{URL: "b.example:4444", TLS: true},
		},
		Wallet:    "WALLET",
		Password:  "pass",
		Bind:      "0.0.0.0:3333",
		Algo:      "rx/0",
		ExtraArgs: "--http-port 8081",
	})
	want := []string{
		"--no-color",
		"-o", "stratum+tcp://a.example:3333",
		"-o", "stratum+ssl://b.example:4444",
		"-u", "WALLET",
		"-b", "0.0.0.0:3333",
		"-p", "pass",
		"--algo", "rx/0",
		"--http-port", "8081",
	}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Fatalf("args = %v, want %v", args, want)
	}
}

// writeFakeProxy creates a stand-in proxy binary that runs until it is killed.
func writeFakeProxy(t *testing.T, dir string) string {
	t.Helper()
	name := "xmrig-proxy"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(dir, name)
	script := "#!/bin/sh\n" +
		"echo \"fake proxy started\"\n" +
		"trap 'echo stopping; exit 0' TERM INT\n" +
		"while true; do sleep 1; done\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CORVUS_PROXY_BIN", path)
	return path
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestProxyManagerStartStop(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake proxy requires a POSIX shell")
	}
	writeFakeProxy(t, t.TempDir())

	m := newProxyManager(t.TempDir())
	if err := m.Start(proxySettings{Pools: []proxyPool{{URL: "pool.example.com:3333"}}, Wallet: "W"}); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer m.Stop()

	status := m.Status()
	if !status.Running || status.PID == 0 {
		t.Fatalf("status after start = %+v", status)
	}
	waitFor(t, 2*time.Second, func() bool {
		return strings.Contains(strings.Join(m.Status().Log, "\n"), "fake proxy started")
	}, "proxy output")

	if err := m.Start(proxySettings{Pools: []proxyPool{{URL: "pool.example.com:3333"}}, Wallet: "W"}); err == nil {
		t.Error("second start must fail while running")
	} else if !strings.Contains(err.Error(), "already running") {
		t.Errorf("unexpected second-start error: %v", err)
	}

	if err := m.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	status = m.Status()
	if status.Running {
		t.Errorf("still running after stop: %+v", status)
	}
	if status.LastExit == "" {
		t.Error("expected a recorded exit reason")
	}
	if err := m.Stop(); err == nil {
		t.Error("second stop must report not running")
	}
}

func TestProxyManagerPersistsSettings(t *testing.T) {
	baseDir := t.TempDir()
	writeFakeProxy(t, t.TempDir())

	m := newProxyManager(baseDir)
	settings := proxySettings{Pools: []proxyPool{{URL: "pool.example.com:3333"}}, Wallet: "WALLET", Bind: "0.0.0.0:4444"}
	if err := m.Start(settings); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer m.Stop()

	reloaded := newProxyManager(baseDir)
	if got := reloaded.Status().Settings.Bind; got != "0.0.0.0:4444" {
		t.Errorf("persisted bind = %q", got)
	}
}

func newProxyTestHandler(t *testing.T) *Handler {
	t.Helper()
	return NewHandler(nil, t.TempDir())
}

func TestProxyStatusHandler(t *testing.T) {
	h := newProxyTestHandler(t)

	rec := httptest.NewRecorder()
	h.ProxyStatus(rec, httptest.NewRequest(http.MethodGet, "/api/proxy/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var status proxyStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.Running {
		t.Error("proxy should not be running")
	}
	if status.BinaryOK {
		t.Error("no binary should be found in an empty directory")
	}
	if len(status.Expected) == 0 {
		t.Error("expected paths must be reported")
	}
}

func TestProxyStartValidationHandler(t *testing.T) {
	h := newProxyTestHandler(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/proxy/start", strings.NewReader(`{"pools":[],"wallet":""}`))
	h.ProxyStart(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (%s)", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ProxyStop(rec, httptest.NewRequest(http.MethodPost, "/api/proxy/stop", nil))
	if rec.Code != http.StatusConflict {
		t.Errorf("stop status = %d, want 409", rec.Code)
	}
}

func TestProxyStartMissingBinary(t *testing.T) {
	h := newProxyTestHandler(t)
	t.Setenv("CORVUS_PROXY_BIN", filepath.Join(t.TempDir(), "does-not-exist"))

	body := `{"pools":[{"url":"pool.example.com:3333","tls":true}],"wallet":"W"}`
	rec := httptest.NewRecorder()
	h.ProxyStart(rec, httptest.NewRequest(http.MethodPost, "/api/proxy/start", strings.NewReader(body)))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "not found") {
		t.Errorf("error should mention the missing binary: %s", rec.Body.String())
	}
}

func TestProxyHandlersStartStop(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake proxy requires a POSIX shell")
	}
	writeFakeProxy(t, t.TempDir())
	h := newProxyTestHandler(t)

	body := `{"pools":[{"url":"pool.example.com:3333","tls":false}],"wallet":"W","bind":"127.0.0.1:13333"}`
	rec := httptest.NewRecorder()
	h.ProxyStart(rec, httptest.NewRequest(http.MethodPost, "/api/proxy/start", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("start status = %d (%s)", rec.Code, rec.Body.String())
	}
	var status proxyStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if !status.Running || status.PID == 0 {
		t.Fatalf("start response = %+v", status)
	}

	rec = httptest.NewRecorder()
	h.ProxyStart(rec, httptest.NewRequest(http.MethodPost, "/api/proxy/start", strings.NewReader(body)))
	if rec.Code != http.StatusConflict {
		t.Errorf("second start status = %d, want 409", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ProxyStop(rec, httptest.NewRequest(http.MethodPost, "/api/proxy/stop", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("stop status = %d (%s)", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.Running {
		t.Error("proxy still running after stop")
	}
}

func TestProxyPageRenders(t *testing.T) {
	baseDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(baseDir, "templates"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"layout.html", "proxy.html"} {
		src, err := os.ReadFile(filepath.Join("..", "templates", name))
		if err != nil {
			t.Skipf("%s not reachable from the test working directory", name)
		}
		if err := os.WriteFile(filepath.Join(baseDir, "templates", name), src, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	h := NewHandler(nil, baseDir)
	rec := httptest.NewRecorder()
	h.ProxyPage(rec, httptest.NewRequest(http.MethodGet, "/proxy", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"XMRig Proxy", "Start proxy", "Stop proxy", "xmrig-proxy", "Add pool", "TLS"} {
		if !strings.Contains(body, want) {
			t.Errorf("page is missing %q", want)
		}
	}
}

func TestProxyRestartDelay(t *testing.T) {
	if d := proxyRestartDelay(1); d != proxyRestartBase {
		t.Errorf("delay(1) = %v, want %v", d, proxyRestartBase)
	}
	if d := proxyRestartDelay(2); d != 2*proxyRestartBase {
		t.Errorf("delay(2) = %v", d)
	}
	if d := proxyRestartDelay(50); d != proxyRestartMax {
		t.Errorf("delay(50) = %v, want cap %v", d, proxyRestartMax)
	}
}

func TestProxyAutoRestartOnUnexpectedExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake proxy requires a POSIX shell")
	}
	dir := t.TempDir()
	// Proxy exits immediately on its own (crash loop).
	path := filepath.Join(dir, "xmrig-proxy")
	script := "#!/bin/sh\necho starting\nexit 3\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CORVUS_PROXY_BIN", path)

	m := newProxyManager(t.TempDir())
	// Shrink the backoff so the test is fast.
	if err := m.Start(proxySettings{Pools: []proxyPool{{URL: "pool.example.com:3333"}}, Wallet: "W"}); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = m.Stop() })

	waitFor(t, 3*time.Second, func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.restartCount > 0
	}, "an automatic restart attempt")

	status := m.Status()
	if status.RestartCount == 0 {
		t.Fatalf("restart_count = 0, want > 0 (%+v)", status)
	}
	if status.LastExit == "" {
		t.Error("last_exit should describe the crash")
	}
	// Stop must cancel the pending restart loop.
	if err := m.Stop(); err != nil && !strings.Contains(err.Error(), "not running") {
		t.Fatalf("stop: %v", err)
	}
}

func TestProxyStopCancelsPendingRestart(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake proxy requires a POSIX shell")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "xmrig-proxy")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CORVUS_PROXY_BIN", path)

	base := t.TempDir()
	m := newProxyManager(base)
	if err := m.Start(proxySettings{Pools: []proxyPool{{URL: "pool.example.com:3333"}}, Wallet: "W"}); err != nil {
		t.Fatal(err)
	}

	waitFor(t, 3*time.Second, func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		return !m.restartAt.IsZero()
	}, "a scheduled restart")

	if err := m.Stop(); err != nil {
		t.Fatalf("stop during pending restart: %v", err)
	}
	if status := m.Status(); status.RestartPending {
		t.Errorf("restart still pending after stop: %+v", status)
	}

	// Give the sleeping restart goroutine time to wake up: it must not relaunch.
	time.Sleep(300 * time.Millisecond)
	if status := m.Status(); status.Running {
		t.Error("proxy restarted after an explicit stop")
	}
}

func TestProxyDetectsOrphan(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX sleeper")
	}
	base := t.TempDir()

	// Simulate a proxy left behind by a previous panel instance.
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start sleeper: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })

	state := fmt.Sprintf(`{"pid":%d,"started_at":%d}`, cmd.Process.Pid, time.Now().Unix())
	if err := os.WriteFile(filepath.Join(base, proxyStateFileName), []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}

	m := newProxyManager(base)
	status := m.Status()
	if status.OrphanPID != cmd.Process.Pid {
		t.Fatalf("orphan_pid = %d, want %d", status.OrphanPID, cmd.Process.Pid)
	}
	if status.Running {
		t.Error("an orphan is not a managed process")
	}

	// Starting a new proxy must refuse while the orphan is alive.
	err := m.Start(proxySettings{Pools: []proxyPool{{URL: "pool.example.com:3333"}}, Wallet: "W"})
	if err == nil || !strings.Contains(err.Error(), "previous panel instance") {
		t.Fatalf("start error = %v, want orphan refusal", err)
	}

	// Stop terminates the orphan and clears the state file.
	if err := m.Stop(); err != nil {
		t.Fatalf("stop orphan: %v", err)
	}
	if status := m.Status(); status.OrphanPID != 0 {
		t.Errorf("orphan still reported: %+v", status)
	}
	if _, err := os.Stat(filepath.Join(base, proxyStateFileName)); !os.IsNotExist(err) {
		t.Error("state file should be removed")
	}
}

func TestHandlerShutdownStopsProxy(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake proxy requires a POSIX shell")
	}
	writeFakeProxy(t, t.TempDir())
	h := newProxyTestHandler(t)

	if err := h.proxy.Start(proxySettings{Pools: []proxyPool{{URL: "pool.example.com:3333"}}, Wallet: "W"}); err != nil {
		t.Fatalf("start: %v", err)
	}
	if !h.proxy.Status().Running {
		t.Fatal("proxy should be running before shutdown")
	}

	h.Shutdown()

	if status := h.proxy.Status(); status.Running {
		t.Errorf("proxy still running after Shutdown: %+v", status)
	}
	// A second shutdown must be a no-op (it runs on the SIGINT and error paths).
	h.Shutdown()
}
