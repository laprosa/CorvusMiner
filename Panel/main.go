package main

import (
	"bufio"
	"corvusminer/panel/database"
	"corvusminer/panel/handlers"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"
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
	log.Printf("Running from: %s", baseDir)

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

	// Start goroutine to mark stale miners as offline (10 minute timeout)
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			if err := db.MarkStaleMinerAsOffline(10); err != nil {
				log.Printf("Error marking stale miners as offline: %v", err)
			}
		}
	}()

	// Initialize handlers with database and base directory
	h := handlers.NewHandler(db, baseDir)

	// Auth routes (no middleware)
	http.HandleFunc("/login", h.Login)
	http.HandleFunc("/register", h.Register)

	// API endpoint for miner submissions (no auth required)
	http.HandleFunc("/api/miners/submit", h.MinerSubmit)

	// API endpoint for version check (no auth required - clients need this)
	http.HandleFunc("/api/updates/current", h.GetCurrentVersion)

	// Resource endpoints (no auth required - miners need to download these)
	http.HandleFunc("/resources/xmrig", h.ServeXMRig)
	http.HandleFunc("/resources/wildrig", h.ServeWildrig)

	// Protected routes (with auth middleware)
	http.HandleFunc("/logout", h.AuthMiddleware(h.Logout))
	http.HandleFunc("/", h.AuthMiddleware(h.Dashboard))
	http.HandleFunc("/api/miners", h.AuthMiddleware(h.GetMiners))
	http.HandleFunc("/api/miners/delete", h.AuthMiddleware(h.DeleteMiner))
	http.HandleFunc("/api/miners/delete-stale", h.AuthMiddleware(h.DeleteStaleMiners))
	http.HandleFunc("/miners", h.AuthMiddleware(h.MinerList))
	http.HandleFunc("/config", h.AuthMiddleware(h.ConfigPage))
	http.HandleFunc("/updates", h.AuthMiddleware(h.UpdatesPage))
	http.HandleFunc("/api/updates/upload", h.AuthMiddleware(h.UploadUpdate))
	http.HandleFunc("/api/updates/list", h.AuthMiddleware(h.ListUpdates))
	http.HandleFunc("/api/updates/set-current", h.AuthMiddleware(h.SetCurrentUpdate))
	http.HandleFunc("/api/updates/delete", h.AuthMiddleware(h.DeleteUpdate))
	http.HandleFunc("/donations", h.AuthMiddleware(h.Donations))
	http.HandleFunc("/api/config/get", h.AuthMiddleware(h.GetConfig))
	http.HandleFunc("/api/config/update", h.AuthMiddleware(h.UpdateConfig))

	// Serve static files (public - no auth required so login/register pages can load assets)
	staticDir := filepath.Join(baseDir, "static")
	http.HandleFunc("/static/", func(w http.ResponseWriter, r *http.Request) {
		http.StripPrefix("/static/", http.FileServer(http.Dir(staticDir))).ServeHTTP(w, r)
	})

	// Serve updates directory (public - no auth required for clients to download)
	updatesDir := filepath.Join(baseDir, "updates")
	http.HandleFunc("/updates/", func(w http.ResponseWriter, r *http.Request) {
		http.StripPrefix("/updates/", http.FileServer(http.Dir(updatesDir))).ServeHTTP(w, r)
	})

	log.Println("Server running on port 8080")
	log.Fatal(http.ListenAndServe("0.0.0.0:8080", nil))
}
