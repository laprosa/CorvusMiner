package models

import (
	"encoding/json"
	"testing"
)

// TestMinerReportWireFormat pins the JSON keys the client sends, so a rename on
// either side breaks a test instead of silently blanking the dashboard.
func TestMinerReportWireFormat(t *testing.T) {
	payload := `{
		"pc_username": "alice",
		"device_hash": "abc123",
		"cpu_name": "Ryzen 9",
		"gpu_name": "RTX 4090",
		"mining_status": "paused",
		"cpu_mining_status": "paused",
		"gpu_mining_status": "active",
		"antivirus_name": "Defender",
		"device_uptime_min": 42,
		"client_version": "3.1.0",
		"timestamp": 1700000000
	}`

	var report MinerReport
	if err := json.Unmarshal([]byte(payload), &report); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if report.MiningStatus != "paused" {
		t.Errorf("mining_status = %q", report.MiningStatus)
	}
	if report.CPUMiningStatus != "paused" {
		t.Errorf("cpu_mining_status = %q", report.CPUMiningStatus)
	}
	if report.GPUMiningStatus != "active" {
		t.Errorf("gpu_mining_status = %q", report.GPUMiningStatus)
	}
	if report.PCUsername != "alice" || report.DeviceHash != "abc123" || report.ClientVersion != "3.1.0" {
		t.Errorf("unexpected report: %+v", report)
	}
}

// TestMinerJSONExposesMiningState keeps the /api/miners contract stable for the
// page's live refresh.
func TestMinerJSONExposesMiningState(t *testing.T) {
	data, err := json.Marshal(Miner{
		MiningStatus:    "error",
		CPUMiningStatus: "error",
		GPUMiningStatus: "disabled",
	})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"mining_status":     "error",
		"cpu_mining_status": "error",
		"gpu_mining_status": "disabled",
	} {
		if decoded[key] != want {
			t.Errorf("%s = %v, want %q", key, decoded[key], want)
		}
	}
}
