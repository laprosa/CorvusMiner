package main

import (
	"corvusminer/panel/database"
	"corvusminer/panel/handlers"
	"log"
	"net/http"
	"time"
)

func main() {
	// Initialize database
	db, err := database.InitDB("./corvusminer.db")
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

	// Initialize handlers with database
	h := handlers.NewHandler(db)

	// Auth routes (no middleware)
	http.HandleFunc("/login", h.Login)
	http.HandleFunc("/register", h.Register)

	// API endpoint for miner submissions (no auth required)
	http.HandleFunc("/api/miners/submit", h.MinerSubmit)

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

	// Serve static files (protected - requires authentication)
	http.Handle("/static/", h.AuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
		http.StripPrefix("/static/", http.FileServer(http.Dir("./static"))).ServeHTTP(w, r)
	}))

	log.Println("Server running on port 8080")
	log.Fatal(http.ListenAndServe("0.0.0.0:8080", nil))
}
