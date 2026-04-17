package main

import (
	"corvusminer/panel/database"
	"corvusminer/panel/handlers"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func main() {
	// Get the directory of the executable
	exePath, err := os.Executable()
	if err != nil {
		log.Fatalf("Failed to get executable path: %v", err)
	}
	baseDir := filepath.Dir(exePath)
	log.Printf("Running from: %s", baseDir)

	// Initialize database with absolute path
	dbPath := filepath.Join(baseDir, "corvusminer.db")
	db, err := database.InitDB(dbPath)
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

	// Resource endpoints (no auth required - miners need to download these)
	http.HandleFunc("/resources/xmrig", h.ServeXMRig)
	http.HandleFunc("/resources/gminer", h.ServeGMiner)

	// Protected routes (with auth middleware)
	http.HandleFunc("/logout", h.AuthMiddleware(h.Logout))
	http.HandleFunc("/", h.AuthMiddleware(h.Dashboard))
	http.HandleFunc("/api/miners", h.AuthMiddleware(h.GetMiners))
	http.HandleFunc("/api/miners/delete", h.AuthMiddleware(h.DeleteMiner))
	http.HandleFunc("/api/miners/delete-stale", h.AuthMiddleware(h.DeleteStaleMiners))
	http.HandleFunc("/miners", h.AuthMiddleware(h.MinerList))
	http.HandleFunc("/config", h.AuthMiddleware(h.ConfigPage))
	http.HandleFunc("/donations", h.AuthMiddleware(h.Donations))
	http.HandleFunc("/api/config/get", h.AuthMiddleware(h.GetConfig))
	http.HandleFunc("/api/config/update", h.AuthMiddleware(h.UpdateConfig))

	// Serve static files (public - no auth required so login/register pages can load assets)
	staticDir := filepath.Join(baseDir, "static")
	http.HandleFunc("/static/", func(w http.ResponseWriter, r *http.Request) {
		http.StripPrefix("/static/", http.FileServer(http.Dir(staticDir))).ServeHTTP(w, r)
	})

	log.Println("Server running on port 8080")
	log.Fatal(http.ListenAndServe("0.0.0.0:8080", nil))
}
