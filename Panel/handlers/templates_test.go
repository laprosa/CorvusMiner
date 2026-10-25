package handlers

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"corvusminer/panel/models"
)

// templatesDir points the tests at the real template directory.
func templatesDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "templates"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "layout.html")); err != nil {
		t.Skipf("templates not reachable from the test working directory: %v", err)
	}
	return dir
}

// TestAllTemplatesParse guards every page template against syntax errors.
func TestAllTemplatesParse(t *testing.T) {
	dir := templatesDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".html") || name == "layout.html" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			// Superset of the helpers pages use; each handler registers only
			// the ones it needs.
			fm := template.FuncMap{
				"formatUptime": formatUptimeFunc,
				"formatSize":   formatSizeFunc,
				"formatTime":   formatTimeFunc,
			}
			if _, err := template.New("layout").Funcs(fm).
				ParseFiles(filepath.Join(dir, "layout.html"), filepath.Join(dir, name)); err != nil {
				t.Fatalf("parse %s: %v", name, err)
			}
		})
	}
}

func TestDonationsPage(t *testing.T) {
	dir := templatesDir(t)
	h := NewHandler(nil, filepath.Dir(dir))

	rec := httptest.NewRecorder()
	h.Donations(rec, httptest.NewRequest(http.MethodGet, "/donations", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	for _, want := range []string{
		"Pearl (PRL)",
		"prl1plcfqd3w6h6l426lucxlfj6y8srr2344cy59rt9gdfh0mqc2k78vqq9usd0",
		"Monero (XMR)",
		"878CwSB3HgCd5yMF2JmMEMFwTNJ2QV5EvUepvxFjTwqY7cJvPBLx6hXWR4drPjjnQzj3ZeqbCu1xzTUReu3eyJk4F9hTUzN",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("donations page is missing %q", want)
		}
	}

	for _, unwanted := range []string{"Bitcoin", "Litecoin", "bc1qvrpaku2uf2d0nafucz9pxkujpaht0zwy6zt29u", "ltc1q0t9qd2sj284yhpnpqwghua900zlxdclpykv3q9", "85DxSo8AHVnXBnK74b3YerAF9TUayPUxRXSU9NNTdtyDJqKAaKReQ4UMqqjKeWrNwvDS16bLSW5yhRvDmyPRhMMZPc91T5Q"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("donations page must not contain %q", unwanted)
		}
	}

	// Each address needs exactly one copy button and one QR code.
	for _, addr := range []string{
		"prl1plcfqd3w6h6l426lucxlfj6y8srr2344cy59rt9gdfh0mqc2k78vqq9usd0",
		"878CwSB3HgCd5yMF2JmMEMFwTNJ2QV5EvUepvxFjTwqY7cJvPBLx6hXWR4drPjjnQzj3ZeqbCu1xzTUReu3eyJk4F9hTUzN",
	} {
		if got := strings.Count(body, "copyToClipboard('"+addr+"')"); got != 1 {
			t.Errorf("copy button for %s appears %d times", addr, got)
		}
		if got := strings.Count(body, "data="+addr); got != 1 {
			t.Errorf("QR code for %s appears %d times", addr, got)
		}
	}
}

func sampleMiners() []models.Miner {
	return []models.Miner{
		{ID: 1, PCUsername: "rig-active", DeviceHash: "aaa", CPUName: "Ryzen", GPUName: "RTX",
			MiningStatus: "active", CPUMiningStatus: "active", GPUMiningStatus: "active", Status: "online"},
		{ID: 2, PCUsername: "rig-paused", DeviceHash: "bbb", CPUName: "Core", GPUName: "none",
			MiningStatus: "paused", CPUMiningStatus: "paused", GPUMiningStatus: "disabled", Status: "online"},
		{ID: 3, PCUsername: "rig-broken", DeviceHash: "ccc", CPUName: "Xeon", GPUName: "none",
			MiningStatus: "error", CPUMiningStatus: "error", GPUMiningStatus: "error", Status: "online"},
	}
}

func renderPage(t *testing.T, dir, page string, data map[string]any) string {
	t.Helper()
	tmpl, err := template.New("layout").Funcs(template.FuncMap{
		"formatUptime": formatUptimeFunc,
		"formatSize":   formatSizeFunc,
		"formatTime":   formatTimeFunc,
	}).ParseFiles(filepath.Join(dir, "layout.html"), filepath.Join(dir, page))
	if err != nil {
		t.Fatalf("parse %s: %v", page, err)
	}
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "layout", data); err != nil {
		t.Fatalf("render %s: %v", page, err)
	}
	return buf.String()
}

func TestDashboardShowsMiningState(t *testing.T) {
	dir := templatesDir(t)

	body := renderPage(t, dir, "dashboard.html", map[string]any{
		"miners":         sampleMiners(),
		"onlineDevices":  3,
		"offlineDevices": 0,
		"miningActive":   1,
		"miningPaused":   1,
		"miningError":    1,
		"new24h":         3,
		"new7d":          3,
		"new30d":         3,
		"quote":          "hi",
	})

	for _, want := range []string{
		"MINING", "PAUSED", "MINING ERR", "NEW 24H", "NEW 7D", "NEW 30D",
		`class="status active"`, `class="status paused"`, `class="status error"`,
		"CPU: active · GPU: active",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard is missing %q", want)
		}
	}
}

func TestMinersPageShowsMiningState(t *testing.T) {
	dir := templatesDir(t)

	body := renderPage(t, dir, "miners.html", map[string]any{
		"miners": sampleMiners(),
		"quote":  "hi",
	})

	for _, want := range []string{
		">Mining<",
		`class="mining-status status active"`,
		`class="mining-status status paused"`,
		`class="mining-status status error"`,
		"miner.mining_status",
		"cells[8]",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("miners page is missing %q", want)
		}
	}
}

func TestAuthPagesUseTheme(t *testing.T) {
	dir := templatesDir(t)

	for _, page := range []string{"login.html", "register.html"} {
		t.Run(page, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join(dir, page))
			if err != nil {
				t.Fatal(err)
			}
			body := string(src)

			for _, want := range []string{
				`href="/static/css/style.css"`, // theme stylesheet (was a broken /static/style.css)
				"Space+Grotesk",                // theme fonts
				"navbar-logo",                  // brand mark from the sidebar
				`class="btn btn-primary`,       // themed button
				"lucide",                       // theme icons
				`action="/login"`,
			} {
				if page == "register.html" && want == `action="/login"` {
					want = `action="/register"`
				}
				if !strings.Contains(body, want) {
					t.Errorf("%s is missing %q", page, want)
				}
			}

			// Render it the way the handler does (standalone template + data).
			parsed, err := template.ParseFiles(filepath.Join(dir, page))
			if err != nil {
				t.Fatalf("parse %s: %v", page, err)
			}
			var out strings.Builder
			if err := parsed.Execute(&out, map[string]interface{}{"quote": "Hashrate go brrrrr."}); err != nil {
				t.Fatalf("render %s: %v", page, err)
			}
			rendered := out.String()
			for _, want := range []string{"auth-card", "Corvus", "Hashrate go brrrrr.", "autocomplete"} {
				if !strings.Contains(rendered, want) {
					t.Errorf("%s render is missing %q", page, want)
				}
			}

			for _, unwanted := range []string{`href="/static/style.css"`, "#4CAF50", "#4caf50", "🔐", "📝"} {
				if strings.Contains(body, unwanted) {
					t.Errorf("%s must not contain %q", page, unwanted)
				}
			}
		})
	}
}

// TestDashboardRendersStatValues guards a subtle failure mode: html/template
// renders a *missing* key as an empty element, so a card can look present but
// blank when the handler stops supplying its value.
func TestDashboardRendersStatValues(t *testing.T) {
	dir := templatesDir(t)

	body := renderPage(t, dir, "dashboard.html", map[string]any{
		"miners":         sampleMiners(),
		"onlineDevices":  11,
		"offlineDevices": 22,
		"miningActive":   33,
		"miningPaused":   44,
		"miningError":    55,
		"new24h":         66,
		"new7d":          77,
		"new30d":         88,
		"quote":          "hi",
	})

	for _, value := range []string{">11<", ">22<", ">33<", ">44<", ">55<", ">66<", ">77<", ">88<"} {
		if !strings.Contains(body, value) {
			t.Errorf("dashboard is missing a rendered stat value %q", value)
		}
	}

	// Ordering: 24h, 7d, 30d must read left-to-right.
	i24 := strings.Index(body, "NEW 24H")
	i7 := strings.Index(body, "NEW 7D")
	i30 := strings.Index(body, "NEW 30D")
	if !(i24 < i7 && i7 < i30) {
		t.Errorf("NEW cards out of order: 24h=%d 7d=%d 30d=%d", i24, i7, i30)
	}
}
