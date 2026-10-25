package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"corvusminer/panel/handlers"
)

// TestRegisterRoutes guards the route table: an unregistered path falls through
// to "/" and renders the dashboard instead of the intended page.
func TestRegisterRoutes(t *testing.T) {
	h := handlers.NewHandler(nil, t.TempDir())
	mux := http.NewServeMux()
	registerRoutes(mux, h)

	cases := map[string]string{
		"/":                          "/",
		"/login":                     "/login",
		"/register":                  "/register",
		"/miners":                    "/miners",
		"/config":                    "/config",
		"/updates":                   "/updates",
		"/donations":                 "/donations",
		"/builder":                   "/builder",
		"/builder/":                  "/builder/",
		"/proxy":                     "/proxy",
		"/proxy/":                    "/proxy/",
		"/api/proxy/status":          "/api/proxy/status",
		"/api/proxy/start":           "/api/proxy/start",
		"/api/proxy/stop":            "/api/proxy/stop",
		"/api/build":                 "/api/build",
		"/api/build/embedded-config": "/api/build/embedded-config",
		"/api/builds":                "/api/builds",
		"/api/miners":                "/api/miners",
		"/api/miners/submit":         "/api/miners/submit",
		"/resources/xmrig":           "/resources/xmrig",
		"/resources/wildrig":         "/resources/wildrig",
		"/not-a-page":                "/",
	}

	for path, want := range cases {
		_, pattern := mux.Handler(httptest.NewRequest(http.MethodGet, path, nil))
		if pattern != want {
			t.Errorf("route %s matched %q, want %q", path, pattern, want)
		}
	}
}
