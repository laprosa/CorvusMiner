package main

import (
	"bufio"
	"context"
	"corvusminer/panel/database"
	"corvusminer/panel/handlers"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"
)

const (
	// The client reports every 60s (CHECK_IN_INTERVAL in Client/src/main.cpp).
	// Ten missed reports are tolerated before a device is shown as offline, so a
	// slow pool, a suspended laptop or one dropped packet can never flap a rig.
	clientCheckInInterval = time.Minute
	minerOfflineAfter     = 10 * clientCheckInInterval

	// HTTP server bounds. WriteTimeout has to outlive a client build
	// (handlers.buildTimeout = 25m) because /api/build answers synchronously.
	readHeaderTimeout = 15 * time.Second
	readTimeout       = 10 * time.Minute
	writeTimeout      = 35 * time.Minute
	idleTimeout       = 2 * time.Minute
	shutdownTimeout   = 20 * time.Second
)

func prompt(reader *bufio.Reader, label, defaultValue string) (string, error) {
	if defaultValue == "" {
		fmt.Printf("%s: ", label)
	} else {
		fmt.Printf("%s [%s]: ", label, defaultValue)
	}

	value, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultValue, nil
	}
	return value, nil
}

func promptPassword(reader *bufio.Reader) (string, error) {
	fmt.Print("Password: ")
	if term.IsTerminal(int(os.Stdin.Fd())) {
		password, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		return string(password), err
	}

	password, err := reader.ReadString('\n')
	return strings.TrimSpace(password), err
}

func promptDatabaseURL() (string, error) {
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("PostgreSQL connection settings")

	username, err := prompt(reader, "User", "postgres")
	if err != nil {
		return "", err
	}
	password, err := promptPassword(reader)
	if err != nil {
		return "", err
	}
	host, err := prompt(reader, "Host/IP", "localhost")
	if err != nil {
		return "", err
	}
	port, err := prompt(reader, "Port", "5432")
	if err != nil {
		return "", err
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return "", fmt.Errorf("invalid PostgreSQL port %q", port)
	}
	databaseName, err := prompt(reader, "Database", "corvus")
	if err != nil {
		return "", err
	}
	sslMode, err := prompt(reader, "SSL mode", "disable")
	if err != nil {
		return "", err
	}

	connectionURL := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(username, password),
		Host:   net.JoinHostPort(host, strconv.Itoa(portNumber)),
		Path:   "/" + databaseName,
	}
	query := connectionURL.Query()
	query.Set("sslmode", sslMode)
	connectionURL.RawQuery = query.Encode()
	return connectionURL.String(), nil
}

func main() {
	// Get the directory of the executable
	exePath, err := os.Executable()
	if err != nil {
		log.Fatalf("Failed to get executable path: %v", err)
	}
	baseDir := filepath.Dir(exePath)
	log.Printf("CorvusMiner panel %s", Version)
	log.Printf("Running from: %s", baseDir)

	// Log which build is running: a stale binary silently lacks new pages.
	if info, ok := debug.ReadBuildInfo(); ok {
		revision, dirty := "", ""
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				revision = setting.Value
			case "vcs.modified":
				if setting.Value == "true" {
					dirty = "-dirty"
				}
			}
		}
		if revision != "" {
			log.Printf("Panel build: %s%s", revision, dirty)
		}
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL, err = promptDatabaseURL()
		if err != nil {
			log.Fatalf("Failed to read database settings: %v", err)
		}
	}

	db, err := database.InitDB(databaseURL)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	// Mark devices offline only after minerOfflineAfter without a report.
	stopOfflineSweeper := make(chan struct{})
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-stopOfflineSweeper:
				return
			case <-ticker.C:
				minutes := int(minerOfflineAfter / time.Minute)
				if err := db.MarkStaleMinerAsOffline(minutes); err != nil {
					log.Printf("Error marking stale miners as offline: %v", err)
				}
			}
		}
	}()

	// Initialize handlers with database and base directory
	h := handlers.NewHandler(db, baseDir)

	mux := http.NewServeMux()
	registerRoutes(mux, h)

	// Serve static files (public - no auth required so login/register pages can load assets)
	staticDir := filepath.Join(baseDir, "static")
	mux.HandleFunc("/static/", func(w http.ResponseWriter, r *http.Request) {
		http.StripPrefix("/static/", http.FileServer(http.Dir(staticDir))).ServeHTTP(w, r)
	})

	// Serve updates directory (public - no auth required for clients to download)
	updatesDir := filepath.Join(baseDir, "updates")
	mux.HandleFunc("/updates/", func(w http.ResponseWriter, r *http.Request) {
		http.StripPrefix("/updates/", http.FileServer(http.Dir(updatesDir))).ServeHTTP(w, r)
	})

	srv := &http.Server{
		Addr:              "0.0.0.0:8080",
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}

	serveErr := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
		close(serveErr)
	}()

	log.Printf("Server running on port 8080")
	log.Printf("Devices report every %s; marked offline after %d missed check-ins (%s)",
		clientCheckInInterval, int(minerOfflineAfter/clientCheckInInterval), minerOfflineAfter)

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serveErr:
		if err != nil {
			close(stopOfflineSweeper)
			log.Fatalf("Server error: %v", err)
		}
	case sig := <-signals:
		log.Printf("Received %s, shutting down...", sig)
	}

	// Stop accepting work, then let in-flight requests finish.
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("Graceful shutdown timed out: %v", err)
		_ = srv.Close()
	}

	close(stopOfflineSweeper)
	h.Shutdown()
	log.Printf("Shutdown complete")
}

// registerRoutes wires every endpoint onto mux. It is separate from main so the
// route table can be tested: a missing route silently falls through to "/" and
// renders the dashboard instead of the intended page.
func registerRoutes(mux *http.ServeMux, h *handlers.Handler) {
	// Auth routes (no middleware)
	mux.HandleFunc("/login", h.Login)
	mux.HandleFunc("/register", h.Register)

	// API endpoint for miner submissions (no auth required)
	mux.HandleFunc("/api/miners/submit", h.MinerSubmit)

	// API endpoint for version check (no auth required - clients need this)
	mux.HandleFunc("/api/updates/current", h.GetCurrentVersion)

	// Resource endpoints (no auth required - miners need to download these)
	mux.HandleFunc("/resources/xmrig", h.ServeXMRig)
	mux.HandleFunc("/resources/wildrig", h.ServeWildrig)

	// Protected routes (with auth middleware)
	mux.HandleFunc("/logout", h.AuthMiddleware(h.Logout))
	mux.HandleFunc("/", h.AuthMiddleware(h.Dashboard))
	mux.HandleFunc("/api/miners", h.AuthMiddleware(h.GetMiners))
	mux.HandleFunc("/api/miners/delete", h.AuthMiddleware(h.DeleteMiner))
	mux.HandleFunc("/api/miners/delete-stale", h.AuthMiddleware(h.DeleteStaleMiners))
	mux.HandleFunc("/miners", h.AuthMiddleware(h.MinerList))
	mux.HandleFunc("/config", h.AuthMiddleware(h.ConfigPage))
	mux.HandleFunc("/updates", h.AuthMiddleware(h.UpdatesPage))
	mux.HandleFunc("/api/updates/upload", h.AuthMiddleware(h.UploadUpdate))
	mux.HandleFunc("/api/updates/list", h.AuthMiddleware(h.ListUpdates))
	mux.HandleFunc("/api/updates/set-current", h.AuthMiddleware(h.SetCurrentUpdate))
	mux.HandleFunc("/api/updates/delete", h.AuthMiddleware(h.DeleteUpdate))
	mux.HandleFunc("/builder", h.AuthMiddleware(h.BuilderPage))
	mux.HandleFunc("/builder/", h.AuthMiddleware(h.BuilderPage))
	mux.HandleFunc("/api/build", h.AuthMiddleware(h.BuildMiner))
	mux.HandleFunc("/api/builds", h.AuthMiddleware(h.ListBuilds))
	mux.HandleFunc("/api/builds/delete", h.AuthMiddleware(h.DeleteBuild))
	mux.HandleFunc("/api/build/embedded-config", h.AuthMiddleware(h.EmbeddedConfigGetOrSave))
	mux.HandleFunc("/proxy", h.AuthMiddleware(h.ProxyPage))
	mux.HandleFunc("/proxy/", h.AuthMiddleware(h.ProxyPage))
	mux.HandleFunc("/api/proxy/status", h.AuthMiddleware(h.ProxyStatus))
	mux.HandleFunc("/api/proxy/start", h.AuthMiddleware(h.ProxyStart))
	mux.HandleFunc("/api/proxy/stop", h.AuthMiddleware(h.ProxyStop))
	mux.HandleFunc("/donations", h.AuthMiddleware(h.Donations))
	mux.HandleFunc("/api/config/get", h.AuthMiddleware(h.GetConfig))
	mux.HandleFunc("/api/config/update", h.AuthMiddleware(h.UpdateConfig))
}
