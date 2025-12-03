package main

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// FilamentBridge manages the connection between PrusaLink and Spoolman
type FilamentBridge struct {
	config           *Config
	spoolman         *SpoolmanClient
	db               *sql.DB
	wasPrinting      map[string]bool
	currentJobFile   map[string]string     // Store current job filename per printer
	processingPrints map[string]bool       // Track prints being processed
	printErrors      map[string]PrintError // Store print processing errors
	errorMutex       sync.RWMutex
	mutex            sync.RWMutex
}

// ToolheadMapping represents a mapping between a printer toolhead and a spool
type ToolheadMapping struct {
	PrinterName string    `json:"printer_name"`
	ToolheadID  int       `json:"toolhead_id"`
	SpoolID     int       `json:"spool_id"`
	MappedAt    time.Time `json:"mapped_at"`
	DisplayName string    `json:"display_name,omitempty"` // Custom toolhead name or empty for default
}

// PrintHistory represents a record of filament usage
type PrintHistory struct {
	ID            int       `json:"id"`
	PrinterName   string    `json:"printer_name"`
	ToolheadID    int       `json:"toolhead_id"`
	SpoolID       int       `json:"spool_id"`
	FilamentUsed  float64   `json:"filament_used"`
	PrintStarted  time.Time `json:"print_started"`
	PrintFinished time.Time `json:"print_finished"`
	JobName       string    `json:"job_name"`
}

// PrintError represents a failed print processing attempt
type PrintError struct {
	ID           string    `json:"id"`
	PrinterName  string    `json:"printer_name"`
	Filename     string    `json:"filename"`
	Error        string    `json:"error"`
	Timestamp    time.Time `json:"timestamp"`
	Acknowledged bool      `json:"acknowledged"`
}

// FilaBridgeLocation represents a location managed by FilaBridge
type FilaBridgeLocation struct {
	Name        string    `json:"name"`
	Type        string    `json:"type"` // "printer", "storage", "other"
	PrinterName string    `json:"printer_name,omitempty"`
	ToolheadID  int       `json:"toolhead_id,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// PrinterStatus represents the current status of all printers
type PrinterStatus struct {
	Printers         map[string]PrinterData             `json:"printers"`
	ToolheadMappings map[string]map[int]ToolheadMapping `json:"toolhead_mappings"`
	Timestamp        time.Time                          `json:"timestamp"`
}

// PrinterData represents data for a single printer
type PrinterData struct {
	Name  string `json:"name"`
	State string `json:"state"`
}

// NewFilamentBridge creates a new FilamentBridge instance
func NewFilamentBridge(config *Config) (*FilamentBridge, error) {
	bridge := &FilamentBridge{
		config:           config,
		spoolman:         NewSpoolmanClient(DefaultSpoolmanURL, SpoolmanTimeout, "", ""), // Default URL and timeout, will be updated
		wasPrinting:      make(map[string]bool),
		currentJobFile:   make(map[string]string),
		processingPrints: make(map[string]bool),
		printErrors:      make(map[string]PrintError),
	}

	// Initialize database
	if err := bridge.initDatabase(); err != nil {
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}

	// Update Spoolman URL and timeout if config is provided
	if config != nil && config.SpoolmanURL != "" {
		bridge.spoolman = NewSpoolmanClient(config.SpoolmanURL, config.SpoolmanTimeout, config.SpoolmanUsername, config.SpoolmanPassword)
	}

	return bridge, nil
}

// initDatabase initializes the SQLite database
func (b *FilamentBridge) initDatabase() error {
	dbFile := DefaultDBFileName
	if b.config != nil && b.config.DBFile != "" {
		dbFile = b.config.DBFile
	}
	// Check for environment variable (path only, append filename)
	if envDBPath := os.Getenv("FILABRIDGE_DB_PATH"); envDBPath != "" {
		dbFile = filepath.Join(envDBPath, DefaultDBFileName)
	}

	db, err := sql.Open("sqlite3", dbFile)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	b.db = db

	// Create tables
	createTables := []string{
		`CREATE TABLE IF NOT EXISTS configuration (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			description TEXT,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS printer_configs (
			printer_id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			model TEXT,
			ip_address TEXT NOT NULL,
			api_key TEXT,
			toolheads INTEGER DEFAULT 1,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS toolhead_mappings (
			printer_name TEXT,
			toolhead_id INTEGER,
			spool_id INTEGER,
			mapped_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (printer_name, toolhead_id)
		)`,
		`CREATE TABLE IF NOT EXISTS print_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			printer_name TEXT,
			toolhead_id INTEGER,
			spool_id INTEGER,
			filament_used REAL,
			print_started TIMESTAMP,
			print_finished TIMESTAMP,
			job_name TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS nfc_sessions (
			session_id TEXT PRIMARY KEY,
			spool_id INTEGER,
			printer_name TEXT,
			toolhead_id INTEGER,
			location_name TEXT,
			is_printer_location BOOLEAN,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			expires_at TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS fb_locations (
			name TEXT PRIMARY KEY,
			type TEXT NOT NULL DEFAULT 'storage',
			printer_name TEXT,
			toolhead_id INTEGER,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS toolhead_names (
			printer_id TEXT,
			toolhead_id INTEGER,
			display_name TEXT NOT NULL,
			PRIMARY KEY (printer_id, toolhead_id)
		)`,
	}

	for _, query := range createTables {
		if _, err := b.db.Exec(query); err != nil {
			return fmt.Errorf("failed to create table: %w", err)
		}
	}

	// Initialize default configuration
	if err := b.initializeDefaultConfig(); err != nil {
		return fmt.Errorf("failed to initialize default configuration: %w", err)
	}

	return nil
}

// initializeDefaultConfig sets up default configuration values
func (b *FilamentBridge) initializeDefaultConfig() error {
	defaultConfigs := map[string]string{
		ConfigKeyPrinterIPs:                   "", // Comma-separated list of printer IP addresses
		ConfigKeyAPIKey:                       "", // PrusaLink API key for authentication
		ConfigKeySpoolmanURL:                  DefaultSpoolmanURL,
		ConfigKeySpoolmanUsername:             "", // Spoolman basic auth username (optional)
		ConfigKeySpoolmanPassword:             "", // Spoolman basic auth password (optional)
		ConfigKeyPollInterval:                 fmt.Sprintf("%d", DefaultPollInterval),
		ConfigKeyWebPort:                      DefaultWebPort,
		ConfigKeyPrusaLinkTimeout:             fmt.Sprintf("%d", PrusaLinkTimeout),
		ConfigKeyPrusaLinkFileDownloadTimeout: fmt.Sprintf("%d", PrusaLinkFileDownloadTimeout),
		ConfigKeySpoolmanTimeout:              fmt.Sprintf("%d", SpoolmanTimeout),
		ConfigKeyAutoAssignPreviousSpoolEnabled: "false", // Enable auto-assignment of previous spool to default location
		ConfigKeyAutoAssignPreviousSpoolLocation: "",      // Default location name for auto-assigned previous spools
	}

	// Check if this is a fresh installation by checking if any config exists
	var totalCount int
	err := b.db.QueryRow("SELECT COUNT(*) FROM configuration").Scan(&totalCount)
	if err != nil {
		return fmt.Errorf("failed to check config existence: %w", err)
	}

	// Only insert defaults if this is a fresh installation
	if totalCount == 0 {
		for key, value := range defaultConfigs {
			_, err := b.db.Exec(
				"INSERT INTO configuration (key, value, description) VALUES (?, ?, ?)",
				key, value, getConfigDescription(key),
			)
			if err != nil {
				return fmt.Errorf("failed to insert default config %s: %w", key, err)
			}
		}
	}

	return nil
}

// getConfigDescription returns a description for a configuration key
func getConfigDescription(key string) string {
	descriptions := map[string]string{
		ConfigKeyPrinterIPs:                   "Comma-separated list of printer IP addresses for PrusaLink",
		ConfigKeyAPIKey:                       "PrusaLink API key for authentication",
		ConfigKeySpoolmanURL:                  "URL of Spoolman instance",
		ConfigKeySpoolmanUsername:             "Spoolman basic auth username (optional, leave empty if not using basic auth)",
		ConfigKeySpoolmanPassword:             "Spoolman basic auth password (optional, leave empty if not using basic auth)",
		ConfigKeyPollInterval:                 "Polling interval in seconds",
		ConfigKeyWebPort:                      "Port for web interface",
		ConfigKeyPrusaLinkTimeout:             "PrusaLink API timeout in seconds",
		ConfigKeyPrusaLinkFileDownloadTimeout: "PrusaLink file download timeout in seconds",
		ConfigKeySpoolmanTimeout:              "Spoolman API timeout in seconds",
		ConfigKeyAutoAssignPreviousSpoolEnabled: "Enable automatic assignment of previous spool to default location when assigning new spool to toolhead",
		ConfigKeyAutoAssignPreviousSpoolLocation: "Default location name where previous spools will be automatically assigned (must exist as a location)",
	}
	if desc, exists := descriptions[key]; exists {
		return desc
	}
	return "Configuration value"
}

// GetConfigValue gets a configuration value from the database
func (b *FilamentBridge) GetConfigValue(key string) (string, error) {
	var value string
	err := b.db.QueryRow("SELECT value FROM configuration WHERE key = ?", key).Scan(&value)
	if err != nil {
		return "", fmt.Errorf("failed to get config value for %s: %w", key, err)
	}
	return value, nil
}

// SetConfigValue sets a configuration value in the database
func (b *FilamentBridge) SetConfigValue(key, value string) error {
	_, err := b.db.Exec(
		"INSERT OR REPLACE INTO configuration (key, value, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP)",
		key, value,
	)
	if err != nil {
		return fmt.Errorf("failed to set config value for %s: %w", key, err)
	}
	return nil
}

// GetAllConfig gets all configuration values
func (b *FilamentBridge) GetAllConfig() (map[string]string, error) {
	rows, err := b.db.Query("SELECT key, value FROM configuration")
	if err != nil {
		return nil, fmt.Errorf("failed to get all config: %w", err)
	}
	defer rows.Close()

	config := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("failed to scan config row: %w", err)
		}
		config[key] = value
	}

	return config, nil
}

// GetAutoAssignPreviousSpoolEnabled gets whether auto-assignment of previous spool is enabled
func (b *FilamentBridge) GetAutoAssignPreviousSpoolEnabled() (bool, error) {
	value, err := b.GetConfigValue(ConfigKeyAutoAssignPreviousSpoolEnabled)
	if err != nil {
		// If key doesn't exist, return false (default)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return value == "true", nil
}

// SetAutoAssignPreviousSpoolEnabled sets whether auto-assignment of previous spool is enabled
func (b *FilamentBridge) SetAutoAssignPreviousSpoolEnabled(enabled bool) error {
	value := "false"
	if enabled {
		value = "true"
	}
	return b.SetConfigValue(ConfigKeyAutoAssignPreviousSpoolEnabled, value)
}

// GetAutoAssignPreviousSpoolLocation gets the default location name for auto-assigned previous spools
func (b *FilamentBridge) GetAutoAssignPreviousSpoolLocation() (string, error) {
	value, err := b.GetConfigValue(ConfigKeyAutoAssignPreviousSpoolLocation)
	if err != nil {
		// If key doesn't exist, return empty string (default)
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return value, nil
}

// SetAutoAssignPreviousSpoolLocation sets the default location name for auto-assigned previous spools
func (b *FilamentBridge) SetAutoAssignPreviousSpoolLocation(location string) error {
	return b.SetConfigValue(ConfigKeyAutoAssignPreviousSpoolLocation, location)
}

// GetAllPrinterConfigs gets all printer configurations
func (b *FilamentBridge) GetAllPrinterConfigs() (map[string]PrinterConfig, error) {
	rows, err := b.db.Query("SELECT printer_id, name, model, ip_address, api_key, toolheads FROM printer_configs")
	if err != nil {
		return nil, fmt.Errorf("failed to get printer configs: %w", err)
	}
	defer rows.Close()

	configs := make(map[string]PrinterConfig)
	for rows.Next() {
		var printerID, name, model, ipAddress, apiKey string
		var toolheads int
		if err := rows.Scan(&printerID, &name, &model, &ipAddress, &apiKey, &toolheads); err != nil {
			return nil, fmt.Errorf("failed to scan printer config row: %w", err)
		}
		configs[printerID] = PrinterConfig{
			Name:      name,
			Model:     model,
			IPAddress: ipAddress,
			APIKey:    apiKey,
			Toolheads: toolheads,
		}
	}

	return configs, nil
}

// SavePrinterConfig saves a printer configuration
func (b *FilamentBridge) SavePrinterConfig(printerID string, config PrinterConfig) error {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	_, err := b.db.Exec(`
		INSERT OR REPLACE INTO printer_configs (printer_id, name, model, ip_address, api_key, toolheads)
		VALUES (?, ?, ?, ?, ?, ?)
	`, printerID, config.Name, config.Model, config.IPAddress, config.APIKey, config.Toolheads)
	if err != nil {
		return fmt.Errorf("failed to save printer config: %w", err)
	}
	return nil
}

// DeletePrinterConfig deletes a printer configuration
func (b *FilamentBridge) DeletePrinterConfig(printerID string) error {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	_, err := b.db.Exec("DELETE FROM printer_configs WHERE printer_id = ?", printerID)
	if err != nil {
		return fmt.Errorf("failed to delete printer config: %w", err)
	}
	return nil
}

// GetToolheadName gets the display name for a toolhead, or returns default "Toolhead {ID}"
func (b *FilamentBridge) GetToolheadName(printerID string, toolheadID int) (string, error) {
	b.mutex.RLock()
	defer b.mutex.RUnlock()

	var displayName string
	err := b.db.QueryRow(
		"SELECT display_name FROM toolhead_names WHERE printer_id = ? AND toolhead_id = ?",
		printerID, toolheadID,
	).Scan(&displayName)

	if err == sql.ErrNoRows {
		// Return default name if not found
		return fmt.Sprintf("Toolhead %d", toolheadID), nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to get toolhead name: %w", err)
	}

	return displayName, nil
}

// SetToolheadName sets the display name for a toolhead
func (b *FilamentBridge) SetToolheadName(printerID string, toolheadID int, name string) error {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	// Validate name is not empty
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("toolhead name cannot be empty")
	}

	_, err := b.db.Exec(
		"INSERT OR REPLACE INTO toolhead_names (printer_id, toolhead_id, display_name) VALUES (?, ?, ?)",
		printerID, toolheadID, name,
	)
	if err != nil {
		return fmt.Errorf("failed to set toolhead name: %w", err)
	}

	log.Printf("Set toolhead name for printer %s, toolhead %d: %s", printerID, toolheadID, name)
	return nil
}

// GetAllToolheadNames gets all toolhead display names for a printer
func (b *FilamentBridge) GetAllToolheadNames(printerID string) (map[int]string, error) {
	b.mutex.RLock()
	defer b.mutex.RUnlock()

	rows, err := b.db.Query(
		"SELECT toolhead_id, display_name FROM toolhead_names WHERE printer_id = ? ORDER BY toolhead_id",
		printerID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get toolhead names: %w", err)
	}
	defer rows.Close()

	names := make(map[int]string)
	for rows.Next() {
		var toolheadID int
		var displayName string
		if err := rows.Scan(&toolheadID, &displayName); err != nil {
			return nil, fmt.Errorf("failed to scan toolhead name row: %w", err)
		}
		names[toolheadID] = displayName
	}

	return names, nil
}

// GetConfigSnapshot returns a snapshot of the current config for safe iteration
func (b *FilamentBridge) GetConfigSnapshot() *Config {
	b.mutex.RLock()
	defer b.mutex.RUnlock()

	// Return a copy of the config to prevent iteration issues during updates
	if b.config == nil {
		return nil
	}

	// Create a shallow copy of the config
	configCopy := &Config{
		SpoolmanURL:                  b.config.SpoolmanURL,
		PollInterval:                 b.config.PollInterval,
		DBFile:                       b.config.DBFile,
		WebPort:                      b.config.WebPort,
		PrusaLinkTimeout:             b.config.PrusaLinkTimeout,
		PrusaLinkFileDownloadTimeout: b.config.PrusaLinkFileDownloadTimeout,
		SpoolmanTimeout:              b.config.SpoolmanTimeout,
		Printers:                     make(map[string]PrinterConfig),
	}

	// Copy printer configs
	for id, printer := range b.config.Printers {
		configCopy.Printers[id] = printer
	}

	return configCopy
}

// ReloadConfig reloads the configuration from the database
func (b *FilamentBridge) ReloadConfig() error {
	// Load config outside the lock to minimize lock time
	config, err := LoadConfig(b)
	if err != nil {
		return fmt.Errorf("failed to reload config: %w", err)
	}

	// Only lock briefly to swap the config pointer and recreate SpoolmanClient
	b.mutex.Lock()
	b.config = config
	if config.SpoolmanURL != "" {
		b.spoolman = NewSpoolmanClient(config.SpoolmanURL, config.SpoolmanTimeout, config.SpoolmanUsername, config.SpoolmanPassword)
	}
	b.mutex.Unlock()

	return nil
}

// IsFirstRun checks if this is the first time the application is running
func (b *FilamentBridge) IsFirstRun() (bool, error) {
	var count int
	err := b.db.QueryRow("SELECT COUNT(*) FROM printer_configs").Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check first run status: %w", err)
	}

	// If no printers are configured, this is a first run
	return count == 0, nil
}

// UpdateConfig updates the bridge configuration
func (b *FilamentBridge) UpdateConfig(config *Config) error {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	b.config = config
	b.spoolman = NewSpoolmanClient(config.SpoolmanURL, config.SpoolmanTimeout, config.SpoolmanUsername, config.SpoolmanPassword)

	return nil
}

// GetToolheadMapping gets spool ID mapped to a specific toolhead
func (b *FilamentBridge) GetToolheadMapping(printerName string, toolheadID int) (int, error) {
	b.mutex.RLock()
	defer b.mutex.RUnlock()

	var spoolID int
	err := b.db.QueryRow(
		"SELECT spool_id FROM toolhead_mappings WHERE printer_name = ? AND toolhead_id = ?",
		printerName, toolheadID,
	).Scan(&spoolID)

	if err == sql.ErrNoRows {
		return 0, nil // No mapping found
	}
	if err != nil {
		return 0, fmt.Errorf("failed to get toolhead mapping: %w", err)
	}

	return spoolID, nil
}

// SetToolheadMapping maps a spool to a specific toolhead
func (b *FilamentBridge) SetToolheadMapping(printerName string, toolheadID int, spoolID int) error {
	b.mutex.Lock()

	// Get the previous spool ID before replacing it (for auto-assignment feature)
	var previousSpoolID int
	err := b.db.QueryRow(
		"SELECT spool_id FROM toolhead_mappings WHERE printer_name = ? AND toolhead_id = ?",
		printerName, toolheadID,
	).Scan(&previousSpoolID)
	if err != nil && err != sql.ErrNoRows {
		b.mutex.Unlock()
		return fmt.Errorf("failed to get previous spool mapping: %w", err)
	}
	// If no previous mapping exists, previousSpoolID will be 0

	// Check if this spool is already assigned to a different toolhead
	rows, err := b.db.Query(
		"SELECT printer_name, toolhead_id FROM toolhead_mappings WHERE spool_id = ? AND NOT (printer_name = ? AND toolhead_id = ?)",
		spoolID, printerName, toolheadID,
	)
	if err != nil {
		b.mutex.Unlock()
		return fmt.Errorf("failed to check existing spool assignments: %w", err)
	}
	defer rows.Close()

	// If we find any rows, this spool is already assigned elsewhere
	if rows.Next() {
		var existingPrinterName string
		var existingToolheadID int
		if err := rows.Scan(&existingPrinterName, &existingToolheadID); err != nil {
			b.mutex.Unlock()
			return fmt.Errorf("failed to scan existing assignment: %w", err)
		}
		b.mutex.Unlock()
		return fmt.Errorf("spool %d is already assigned to %s toolhead %d", spoolID, existingPrinterName, existingToolheadID)
	}

	_, err = b.db.Exec(
		"INSERT OR REPLACE INTO toolhead_mappings (printer_name, toolhead_id, spool_id, mapped_at) VALUES (?, ?, ?, ?)",
		printerName, toolheadID, spoolID, time.Now(),
	)
	if err != nil {
		b.mutex.Unlock()
		return fmt.Errorf("failed to set toolhead mapping: %w", err)
	}

	log.Printf("Mapped %s toolhead %d to spool %d", printerName, toolheadID, spoolID)

	// Check if auto-assign feature is enabled and we have a previous spool to assign
	enabled, err := b.GetAutoAssignPreviousSpoolEnabled()
	if err != nil {
		log.Printf("Warning: Failed to check auto-assign previous spool setting: %v", err)
		b.mutex.Unlock()
		return nil // Don't fail the assignment if we can't check the setting
	}

	// Unlock before potentially calling AssignSpoolToLocation (which may need locks)
	b.mutex.Unlock()

	if enabled && previousSpoolID > 0 && previousSpoolID != spoolID {
		// Get the configured default location
		locationName, err := b.GetAutoAssignPreviousSpoolLocation()
		if err != nil {
			log.Printf("Warning: Failed to get auto-assign previous spool location setting: %v", err)
			return nil // Don't fail the assignment
		}

		if locationName != "" {
			// Verify the location exists
			location, err := b.FindLocationByName(locationName)
			if err != nil || location == nil {
				log.Printf("Warning: Auto-assign previous spool location '%s' does not exist, skipping auto-assignment of spool %d", locationName, previousSpoolID)
				return nil // Don't fail the assignment
			}

			// Assign the previous spool to the default location
			// Use isPrinterLocation = false since this is a storage location
			if err := b.AssignSpoolToLocation(previousSpoolID, "", 0, locationName, false); err != nil {
				log.Printf("Warning: Failed to auto-assign previous spool %d to location '%s': %v", previousSpoolID, locationName, err)
				// Don't fail the original assignment if auto-assignment fails
			} else {
				log.Printf("Auto-assigned previous spool %d to location '%s'", previousSpoolID, locationName)
			}
		}
	}

	return nil
}

// GetToolheadMappings gets all toolhead mappings for a printer
func (b *FilamentBridge) GetToolheadMappings(printerName string) (map[int]ToolheadMapping, error) {
	rows, err := b.db.Query(
		"SELECT toolhead_id, spool_id, mapped_at FROM toolhead_mappings WHERE printer_name = ?",
		printerName,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	mappings := make(map[int]ToolheadMapping)
	for rows.Next() {
		var toolheadID, spoolID int
		var mappedAt time.Time
		if err := rows.Scan(&toolheadID, &spoolID, &mappedAt); err != nil {
			return nil, err
		}
		mappings[toolheadID] = ToolheadMapping{
			PrinterName: printerName,
			ToolheadID:  toolheadID,
			SpoolID:     spoolID,
			MappedAt:    mappedAt,
		}
	}

	return mappings, nil
}

// GetAllToolheadMappings gets all toolhead mappings across all printers
func (b *FilamentBridge) GetAllToolheadMappings() (map[string]map[int]ToolheadMapping, error) {
	rows, err := b.db.Query(
		"SELECT printer_name, toolhead_id, spool_id, mapped_at FROM toolhead_mappings ORDER BY printer_name, toolhead_id",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	mappings := make(map[string]map[int]ToolheadMapping)
	for rows.Next() {
		var printerName string
		var toolheadID, spoolID int
		var mappedAt time.Time
		if err := rows.Scan(&printerName, &toolheadID, &spoolID, &mappedAt); err != nil {
			return nil, err
		}

		if mappings[printerName] == nil {
			mappings[printerName] = make(map[int]ToolheadMapping)
		}

		mappings[printerName][toolheadID] = ToolheadMapping{
			PrinterName: printerName,
			ToolheadID:  toolheadID,
			SpoolID:     spoolID,
			MappedAt:    mappedAt,
		}
	}

	return mappings, nil
}

// UnmapToolhead removes a spool mapping from a toolhead
func (b *FilamentBridge) UnmapToolhead(printerName string, toolheadID int) error {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	_, err := b.db.Exec(
		"DELETE FROM toolhead_mappings WHERE printer_name = ? AND toolhead_id = ?",
		printerName, toolheadID,
	)
	if err != nil {
		return fmt.Errorf("failed to unmap toolhead: %w", err)
	}

	log.Printf("Unmapped %s toolhead %d", printerName, toolheadID)
	return nil
}

// LogPrintUsage logs filament usage for a print job
func (b *FilamentBridge) LogPrintUsage(printerName string, toolheadID int, spoolID int, filamentUsed float64, jobName string) error {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	// Get print start time from current job file tracking
	printStarted := time.Now() // Default to now if we can't determine start time
	if storedJobFile, exists := b.currentJobFile[printerName]; exists && storedJobFile != "" {
		// If we have a stored job file, the print likely started when we first stored it
		// This is a rough approximation - ideally we'd track this more precisely
		printStarted = time.Now().Add(-time.Hour) // Assume 1 hour ago as rough estimate
	}

	_, err := b.db.Exec(
		"INSERT INTO print_history (printer_name, toolhead_id, spool_id, filament_used, print_started, print_finished, job_name) VALUES (?, ?, ?, ?, ?, ?, ?)",
		printerName, toolheadID, spoolID, filamentUsed, printStarted, time.Now(), jobName,
	)
	if err != nil {
		return fmt.Errorf("failed to log print usage: %w", err)
	}

	return nil
}

// MonitorPrinters monitors all printers for print status changes
func (b *FilamentBridge) MonitorPrinters() {
	log.Printf("Monitoring printers at %s", time.Now().Format(time.RFC3339))

	// Get a safe snapshot of the config to prevent iteration issues
	configSnapshot := b.GetConfigSnapshot()
	if configSnapshot == nil || len(configSnapshot.Printers) == 0 {
		log.Printf("No printers configured - skipping monitoring")
		return
	}

	// Monitor each printer using PrusaLink
	for printerID, printerConfig := range configSnapshot.Printers {
		if printerID == "no_printers" {
			continue // Skip placeholder
		}
		go func(printerID string, config PrinterConfig) {
			if err := b.monitorPrusaLink(printerID, config); err != nil {
				log.Printf("Error monitoring printer %s (%s): %v", config.IPAddress, printerID, err)
			}
		}(printerID, printerConfig)
	}
}

// monitorPrusaLink monitors a single printer using PrusaLink API
func (b *FilamentBridge) monitorPrusaLink(printerID string, config PrinterConfig) error {
	log.Printf("Starting monitoring for printer %s (%s) at %s", printerID, config.IPAddress, config.Name)
	client := NewPrusaLinkClient(config.IPAddress, config.APIKey, b.config.PrusaLinkTimeout, b.config.PrusaLinkFileDownloadTimeout)

	status, err := client.GetStatus()
	if err != nil {
		log.Printf("Warning: Failed to get printer status from %s (%s): %v", config.IPAddress, printerID, err)
		return nil // Don't fail the entire monitoring cycle for one printer
	}

	jobInfo, err := client.GetJobInfo()
	if err != nil {
		log.Printf("Warning: Failed to get job info from %s (%s): %v", config.IPAddress, printerID, err)
		// Continue with status-only monitoring if job info fails
		jobInfo = &PrusaLinkJob{}
	}

	currentState := status.Printer.State
	jobName := "No active job"
	currentJobFilename := ""
	if jobInfo.File.Name != "" {
		jobName = jobInfo.File.DisplayName // Use display name for better readability
		// Use the download path directly from refs - it's already in the correct format
		if jobInfo.File.Refs.Download != "" {
			currentJobFilename = strings.TrimPrefix(jobInfo.File.Refs.Download, "/")
		} else {
			// Fallback: construct the path manually
			storage := strings.TrimPrefix(jobInfo.File.Path, "/")
			currentJobFilename = storage + "/" + jobInfo.File.Name
		}
	}

	// Check if print just finished - minimize lock scope
	b.mutex.RLock()
	wasPrinting := b.wasPrinting[printerID]
	storedJobFile := b.currentJobFile[printerID]
	b.mutex.RUnlock()

	// Debug logging for all printers
	log.Printf("Printer %s (%s): state=%s, wasPrinting=%v, job=%s, stored_file=%s",
		config.IPAddress, printerID, currentState, wasPrinting, jobName, storedJobFile)

	// Check if print just finished
	if (currentState == StateIdle || currentState == StateFinished) && wasPrinting {
		// Use stored filename (should be available since we stored it when printing started)
		filenameToUse := storedJobFile
		if filenameToUse == "" {
			log.Printf("Warning: No stored filename for %s (%s), using current job filename: %s",
				config.IPAddress, printerID, currentJobFilename)
			filenameToUse = currentJobFilename
		}

		log.Printf("🎉 Print finished detected for %s (%s): %s (state: %s, file: %s)",
			config.IPAddress, printerID, jobName, currentState, filenameToUse)

		// Mark as processing to prevent filename from being cleared
		b.mutex.Lock()
		b.wasPrinting[printerID] = false
		b.processingPrints[printerID] = true
		b.mutex.Unlock()

		// Now process the print (this takes a long time)
		err := b.handlePrusaLinkPrintFinished(config, filenameToUse)

		// Clear processing flag and filename after completion
		b.mutex.Lock()
		b.processingPrints[printerID] = false
		if err == nil {
			b.currentJobFile[printerID] = ""
		}
		b.mutex.Unlock()

		if err != nil {
			log.Printf("Error handling PrusaLink print finished: %v", err)
		}
	} else {
		// Update state tracking - minimize lock scope
		b.mutex.Lock()
		defer b.mutex.Unlock()

		// Store the current job filename when printing starts (only if not already stored)
		if currentState == StatePrinting && currentJobFilename != "" && storedJobFile == "" {
			b.currentJobFile[printerID] = currentJobFilename
			log.Printf("📁 Stored job filename for %s (%s): %s", config.IPAddress, printerID, currentJobFilename)
		}

		// Update wasPrinting flag for NEXT cycle
		b.wasPrinting[printerID] = currentState == StatePrinting

		// Clear stored filename when print finishes (but only if not currently processing)
		if (currentState == StateIdle || currentState == StateFinished) && !b.processingPrints[printerID] {
			b.currentJobFile[printerID] = ""
		}
	}

	return nil
}

// handlePrusaLinkPrintFinished handles when a print job finishes via PrusaLink
func (b *FilamentBridge) handlePrusaLinkPrintFinished(config PrinterConfig, filename string) error {
	log.Printf("Print finished via PrusaLink (%s): %s", config.IPAddress, filename)

	printerName := resolvePrinterName(config)

	// Create PrusaLink client for this printer
	prusaClient := NewPrusaLinkClient(config.IPAddress, config.APIKey, b.config.PrusaLinkTimeout, b.config.PrusaLinkFileDownloadTimeout)

	// Use the filename parameter (stored when print started)
	if filename == "" {
		errorMsg := "no filename available for print processing"
		b.addPrintError(printerName, "unknown", errorMsg)
		return fmt.Errorf("%s", errorMsg)
	}

	// Download and parse the G-code file (.gcode or .bgcode) for filament usage
	log.Printf("Analyzing G-code file for filament usage: %s", filename)

	// Download with retry logic
	gcodeContent, err := prusaClient.GetGcodeFileWithRetry(filename, b.config.PrusaLinkFileDownloadTimeout)
	if err != nil {
		errorMsg := fmt.Sprintf("failed to download G-code file after retries: %v", err)
		b.addPrintError(printerName, filename, errorMsg)
		return fmt.Errorf("%s", errorMsg)
	}

	// Parse the downloaded file
	filamentUsage, err := prusaClient.ParseGcodeFilamentUsage(gcodeContent)
	if err != nil {
		errorMsg := fmt.Sprintf("failed to parse G-code for filament usage: %v", err)
		b.addPrintError(printerName, filename, errorMsg)
		return fmt.Errorf("%s", errorMsg)
	}

	// Check if we got any filament usage data
	if len(filamentUsage) == 0 {
		errorMsg := "no filament usage data found in G-code file"
		b.addPrintError(printerName, filename, errorMsg)
		return fmt.Errorf("%s", errorMsg)
	}

	log.Printf("Successfully parsed G-code file for filament usage: %+v", filamentUsage)

	// Process filament usage using helper function
	if err := b.processFilamentUsage(printerName, filamentUsage, filename); err != nil {
		log.Printf("Error processing filament usage: %v", err)
		return err
	}

	return nil
}

// GetPrintErrors returns all unacknowledged print errors
func (b *FilamentBridge) GetPrintErrors() []PrintError {
	b.errorMutex.RLock()
	defer b.errorMutex.RUnlock()

	var errors []PrintError
	for _, err := range b.printErrors {
		if !err.Acknowledged {
			errors = append(errors, err)
		}
	}
	return errors
}

// AcknowledgePrintError marks a print error as acknowledged
func (b *FilamentBridge) AcknowledgePrintError(errorID string) error {
	b.errorMutex.Lock()
	defer b.errorMutex.Unlock()

	if err, exists := b.printErrors[errorID]; exists {
		err.Acknowledged = true
		b.printErrors[errorID] = err
		return nil
	}
	return fmt.Errorf("print error not found: %s", errorID)
}

// sanitizeErrorID replaces problematic characters in error IDs to make them URL-safe
func sanitizeErrorID(s string) string {
	// Replace forward slashes with underscores
	s = strings.ReplaceAll(s, "/", "_")
	// Replace spaces with underscores
	s = strings.ReplaceAll(s, " ", "_")
	// Replace backslashes with underscores
	s = strings.ReplaceAll(s, "\\", "_")
	return s
}

// addPrintError adds a new print error
func (b *FilamentBridge) addPrintError(printerName, filename, errorMsg string) {
	b.errorMutex.Lock()
	defer b.errorMutex.Unlock()

	// Sanitize printer name and filename to ensure URL-safe error IDs
	sanitizedPrinterName := sanitizeErrorID(printerName)
	sanitizedFilename := sanitizeErrorID(filename)
	errorID := fmt.Sprintf("%s_%s_%d", sanitizedPrinterName, sanitizedFilename, time.Now().Unix())
	b.printErrors[errorID] = PrintError{
		ID:           errorID,
		PrinterName:  printerName,
		Filename:     filename,
		Error:        errorMsg,
		Timestamp:    time.Now(),
		Acknowledged: false,
	}

	log.Printf("⚠️  Print processing failed for %s (%s): %s - Manual Spoolman update required",
		printerName, filename, errorMsg)
}

// GetStatus gets current status of all printers and mappings
func (b *FilamentBridge) GetStatus() (*PrinterStatus, error) {
	status := &PrinterStatus{
		Printers:         make(map[string]PrinterData),
		ToolheadMappings: make(map[string]map[int]ToolheadMapping),
		Timestamp:        time.Now(),
	}

	// Get a safe snapshot of the config to prevent iteration issues
	configSnapshot := b.GetConfigSnapshot()
	if configSnapshot == nil {
		// No printers configured
		status.Printers["no_printers"] = PrinterData{
			Name:  "No Printers Configured",
			State: StateNotConfigured,
		}
		return status, nil
	}

	// Get printer statuses from PrusaLink
	if len(configSnapshot.Printers) > 0 {
		for printerID, printerConfig := range configSnapshot.Printers {
			if printerID == "no_printers" {
				continue // Skip placeholder
			}

			client := NewPrusaLinkClient(printerConfig.IPAddress, printerConfig.APIKey, b.config.PrusaLinkTimeout, b.config.PrusaLinkFileDownloadTimeout)

			// Use the configured printer name, not the hostname from PrusaLink
			printerName := printerConfig.Name

			// Get current status
			printerStatus, err := client.GetStatus()
			if err != nil {
				status.Printers[printerID] = PrinterData{
					Name:  printerName,
					State: StateOffline,
				}
				continue
			}

			status.Printers[printerID] = PrinterData{
				Name:  printerName,
				State: printerStatus.Printer.State,
			}
		}
	} else {
		// No printers configured
		status.Printers["no_printers"] = PrinterData{
			Name:  "No Printers Configured",
			State: StateNotConfigured,
		}
	}

	// Get toolhead mappings for all printers
	for printerID, printerConfig := range configSnapshot.Printers {
		if printerID == "no_printers" {
			continue // Skip placeholder
		}

		printerName := printerConfig.Name
		mappings, err := b.GetToolheadMappings(printerName)
		if err != nil {
			log.Printf("Error getting toolhead mappings for %s: %v", printerName, err)
			mappings = make(map[int]ToolheadMapping)
		}

		// Get toolhead names for this printer
		toolheadNames, err := b.GetAllToolheadNames(printerID)
		if err != nil {
			log.Printf("Warning: Failed to get toolhead names for printer %s: %v", printerID, err)
			toolheadNames = make(map[int]string)
		}

		// Create enhanced mappings for ALL toolheads (including unmapped ones)
		enhancedMappings := make(map[int]ToolheadMapping)
		for toolheadID := 0; toolheadID < printerConfig.Toolheads; toolheadID++ {
			// Get display name (custom or default)
			var displayName string
			if name, exists := toolheadNames[toolheadID]; exists {
				displayName = name
			} else {
				displayName = fmt.Sprintf("Toolhead %d", toolheadID)
			}

			// If this toolhead has a mapping, use it and add display name
			if mapping, exists := mappings[toolheadID]; exists {
				mapping.DisplayName = displayName
				enhancedMappings[toolheadID] = mapping
			} else {
				// Create empty mapping with just display name for unmapped toolheads
				enhancedMappings[toolheadID] = ToolheadMapping{
					PrinterName: printerName,
					ToolheadID:  toolheadID,
					SpoolID:     0, // No spool mapped
					DisplayName: displayName,
				}
			}
		}
		status.ToolheadMappings[printerID] = enhancedMappings
	}

	return status, nil
}

// processFilamentUsage processes filament usage updates for all toolheads
func (b *FilamentBridge) processFilamentUsage(printerName string, filamentUsage map[int]float64, jobName string) error {
	// Update Spoolman with filament usage for each toolhead
	for toolheadID, usedWeight := range filamentUsage {
		if usedWeight <= 0 {
			continue
		}

		// Get the mapped spool for this toolhead
		spoolID, err := b.GetToolheadMapping(printerName, toolheadID)
		if err != nil {
			log.Printf("Error getting toolhead mapping for %s toolhead %d: %v",
				printerName, toolheadID, err)
			continue
		}

		if spoolID == 0 {
			log.Printf("No spool mapped to %s toolhead %d, skipping filament usage update",
				printerName, toolheadID)
			continue
		}

		// Update Spoolman
		if err := b.spoolman.UpdateSpoolUsage(spoolID, usedWeight); err != nil {
			log.Printf("Error updating spool %d usage: %v", spoolID, err)
			continue
		}

		// Log the usage in our database
		if err := b.LogPrintUsage(printerName, toolheadID, spoolID, usedWeight, jobName); err != nil {
			log.Printf("Error logging print usage: %v", err)
		}

		log.Printf("Updated spool %d: used %.2fg filament on %s toolhead %d",
			spoolID, usedWeight, printerName, toolheadID)
	}

	// Summary log
	if len(filamentUsage) > 0 {
		log.Printf("✅ Print completion processing finished for %s: processed %d toolheads", printerName, len(filamentUsage))
	} else {
		log.Printf("⚠️  No filament usage data processed for %s", printerName)
	}

	return nil
}

// Location Management Methods

// CreateLocation creates a new FilaBridge location
func (b *FilamentBridge) CreateLocation(name, locationType string, printerName string, toolheadID int) (*FilaBridgeLocation, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	// Validate location type (allow free-text types; only special-case printer)
	locationType = strings.TrimSpace(locationType)
	if locationType == "" {
		locationType = "storage"
	}

	// For printer locations, validate printer exists
	if locationType == "printer" {
		if printerName == "" || toolheadID < 0 {
			return nil, fmt.Errorf("printer_name and toolhead_id required for printer locations")
		}
		// Check if printer exists in config
		found := false
		for _, printer := range b.config.Printers {
			if printer.Name == printerName {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("printer %s not found in configuration", printerName)
		}
	}

	now := time.Now()
	location := &FilaBridgeLocation{
		Name:        name,
		Type:        locationType,
		PrinterName: printerName,
		ToolheadID:  toolheadID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// Insert into database
	_, err := b.db.Exec(
		"INSERT INTO fb_locations (name, type, printer_name, toolhead_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		location.Name, location.Type, location.PrinterName, location.ToolheadID, location.CreatedAt, location.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create location: %w", err)
	}

	log.Printf("Created FilaBridge location '%s' (type: %s)", location.Name, location.Type)
	return location, nil
}

// CreateLocationFromSpoolman creates a new FilaBridge location that references an existing Spoolman location
func (b *FilamentBridge) CreateLocationFromSpoolman(name, locationType string) (*FilaBridgeLocation, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	// Validate location name
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("location name cannot be empty")
	}

	// Validate location type (allow free-text types; only special-case printer)
	locationType = strings.TrimSpace(locationType)
	if locationType == "" {
		locationType = "storage"
	}

	now := time.Now()
	location := &FilaBridgeLocation{
		Name:      name,
		Type:      locationType,
		CreatedAt: now,
		UpdatedAt: now,
	}

	// Insert into database
	_, err := b.db.Exec(
		"INSERT INTO fb_locations (name, type, created_at, updated_at) VALUES (?, ?, ?, ?)",
		location.Name, location.Type, location.CreatedAt, location.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create location from Spoolman: %w", err)
	}

	log.Printf("Created FilaBridge location '%s' for Spoolman location", name)
	return location, nil
}

// GetAllFilaBridgeLocations returns all FilaBridge locations
func (b *FilamentBridge) GetAllFilaBridgeLocations() ([]FilaBridgeLocation, error) {
	b.mutex.RLock()
	defer b.mutex.RUnlock()

	rows, err := b.db.Query(
		"SELECT name, type, printer_name, toolhead_id, created_at, updated_at FROM fb_locations ORDER BY name",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query locations: %w", err)
	}
	defer rows.Close()

	var locations []FilaBridgeLocation
	for rows.Next() {
		var loc FilaBridgeLocation
		var printerName sql.NullString
		var toolheadID sql.NullInt64
		err := rows.Scan(&loc.Name, &loc.Type, &printerName, &toolheadID, &loc.CreatedAt, &loc.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan location: %w", err)
		}
		if printerName.Valid {
			loc.PrinterName = printerName.String
		} else {
			loc.PrinterName = ""
		}
		if toolheadID.Valid {
			loc.ToolheadID = int(toolheadID.Int64)
		} else {
			loc.ToolheadID = 0
		}
		locations = append(locations, loc)
	}

	return locations, nil
}

// FindLocationByName finds a location by name
func (b *FilamentBridge) FindLocationByName(name string) (*FilaBridgeLocation, error) {
	b.mutex.RLock()
	defer b.mutex.RUnlock()

	var loc FilaBridgeLocation
	var printerName sql.NullString
	var toolheadID sql.NullInt64
	err := b.db.QueryRow(
		"SELECT name, type, printer_name, toolhead_id, created_at, updated_at FROM fb_locations WHERE name = ?",
		name,
	).Scan(&loc.Name, &loc.Type, &printerName, &toolheadID, &loc.CreatedAt, &loc.UpdatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // Location not found
		}
		return nil, fmt.Errorf("failed to query location: %w", err)
	}

	if printerName.Valid {
		loc.PrinterName = printerName.String
	} else {
		loc.PrinterName = ""
	}
	if toolheadID.Valid {
		loc.ToolheadID = int(toolheadID.Int64)
	} else {
		loc.ToolheadID = 0
	}

	return &loc, nil
}

// UpdateLocation updates a location's name
func (b *FilamentBridge) UpdateLocation(oldName, newName string) error {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	// Check if location exists (inline query, no nested lock)
	var exists bool
	err := b.db.QueryRow(
		"SELECT 1 FROM fb_locations WHERE name = ?",
		oldName,
	).Scan(&exists)

	if err == sql.ErrNoRows {
		return fmt.Errorf("location '%s' not found", oldName)
	}
	if err != nil {
		return fmt.Errorf("failed to query location: %w", err)
	}

	// Update in database
	_, err = b.db.Exec(
		"UPDATE fb_locations SET name = ?, updated_at = ? WHERE name = ?",
		newName, time.Now(), oldName,
	)
	if err != nil {
		return fmt.Errorf("failed to update location: %w", err)
	}

	// Try to update the location in Spoolman if it exists there
	// This will update any spools that reference the old location name
	if err := b.spoolman.UpdateSpoolmanLocationReferences(oldName, newName); err != nil {
		log.Printf("Warning: Failed to update Spoolman location references from '%s' to '%s': %v", oldName, newName, err)
		// Don't fail the entire operation if Spoolman update fails
	} else {
		log.Printf("Successfully updated Spoolman location references from '%s' to '%s'", oldName, newName)
	}

	log.Printf("Updated FilaBridge location from '%s' to '%s'", oldName, newName)
	return nil
}

// DeleteLocation deletes a location
func (b *FilamentBridge) DeleteLocation(name string) error {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	// Check if location exists (inline query, no nested lock)
	var exists bool
	err := b.db.QueryRow(
		"SELECT 1 FROM fb_locations WHERE name = ?",
		name,
	).Scan(&exists)

	if err == sql.ErrNoRows {
		return fmt.Errorf("location '%s' not found", name)
	}
	if err != nil {
		return fmt.Errorf("failed to query location: %w", err)
	}

	// Delete from database
	_, err = b.db.Exec("DELETE FROM fb_locations WHERE name = ?", name)
	if err != nil {
		return fmt.Errorf("failed to delete location: %w", err)
	}

	log.Printf("Deleted location '%s'", name)
	return nil
}

// isVirtualPrinterToolheadLocation checks if a location name matches the pattern
// of a virtual printer toolhead location (e.g., "PrinterName - Toolhead 0" or "PrinterName - Black")
func (b *FilamentBridge) isVirtualPrinterToolheadLocation(name string) bool {
	// Get all printer configurations
	printerConfigs, err := b.GetAllPrinterConfigs()
	if err != nil {
		// If we can't get printer configs, assume it's not a virtual location
		log.Printf("Warning: Could not get printer configurations to check virtual location: %v", err)
		return false
	}

	// Check if the name matches any printer's toolhead location pattern
	for printerID, printerConfig := range printerConfigs {
		// Get toolhead names for this printer
		toolheadNames, err := b.GetAllToolheadNames(printerID)
		if err != nil {
			log.Printf("Warning: Could not get toolhead names for printer %s: %v", printerID, err)
			toolheadNames = make(map[int]string)
		}

		for toolheadID := 0; toolheadID < printerConfig.Toolheads; toolheadID++ {
			// Check default pattern
			expectedNameDefault := fmt.Sprintf("%s - Toolhead %d", printerConfig.Name, toolheadID)
			if name == expectedNameDefault {
				return true
			}

			// Check custom name pattern
			if displayName, exists := toolheadNames[toolheadID]; exists {
				expectedNameCustom := fmt.Sprintf("%s - %s", printerConfig.Name, displayName)
				if name == expectedNameCustom {
					return true
				}
			}
		}
	}

	return false
}

// GetLocationStatus returns detailed status information for a location
func (b *FilamentBridge) GetLocationStatus(name string) (*LocationStatus, error) {
	// Get FilaBridge location
	fbLocation, err := b.FindLocationByName(name)
	if err != nil {
		return nil, fmt.Errorf("failed to find FilaBridge location: %w", err)
	}
	if fbLocation == nil {
		return nil, fmt.Errorf("location '%s' not found in FilaBridge", name)
	}

	// Check if location exists in Spoolman
	existsInSpoolman, err := b.spoolman.LocationExistsInSpoolman(name)
	if err != nil {
		// If we can't check Spoolman, assume it doesn't exist there
		log.Printf("Warning: Could not check if location '%s' exists in Spoolman: %v", name, err)
		existsInSpoolman = false
	}

	status := &LocationStatus{
		Name:               name,
		Type:               fbLocation.Type,
		PrinterName:        fbLocation.PrinterName,
		ToolheadID:         fbLocation.ToolheadID,
		CreatedAt:          fbLocation.CreatedAt,
		UpdatedAt:          fbLocation.UpdatedAt,
		ExistsInFilaBridge: true,
		ExistsInSpoolman:   existsInSpoolman,
		IsLocalOnly:        !existsInSpoolman,
	}

	return status, nil
}

// LocationStatus represents the status of a location across both systems
type LocationStatus struct {
	Name               string    `json:"name"`
	Type               string    `json:"type"`
	PrinterName        string    `json:"printer_name,omitempty"`
	ToolheadID         int       `json:"toolhead_id,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	ExistsInFilaBridge bool      `json:"exists_in_filabridge"`
	ExistsInSpoolman   bool      `json:"exists_in_spoolman"`
	IsLocalOnly        bool      `json:"is_local_only"`
}

// AutoSyncSpoolmanLocations automatically syncs locations from Spoolman to FilaBridge
// This runs on startup and periodically to keep locations in sync
func (b *FilamentBridge) AutoSyncSpoolmanLocations() error {
	log.Printf("AutoSyncSpoolmanLocations: Starting automatic sync...")

	// Get all locations from Spoolman
	spoolmanLocations, err := b.spoolman.GetLocations()
	if err != nil {
		log.Printf("AutoSyncSpoolmanLocations: Failed to get Spoolman locations: %v", err)
		return fmt.Errorf("failed to get Spoolman locations: %w", err)
	}

	// Get all current FilaBridge locations
	fbLocations, err := b.GetAllFilaBridgeLocations()
	if err != nil {
		log.Printf("AutoSyncSpoolmanLocations: Failed to get FilaBridge locations: %v", err)
		return fmt.Errorf("failed to get FilaBridge locations: %w", err)
	}

	// Create a map of existing FilaBridge locations for quick lookup
	fbLocationMap := make(map[string]bool)
	for _, loc := range fbLocations {
		fbLocationMap[loc.Name] = true
	}

	importedCount := 0
	for _, smLocation := range spoolmanLocations {
		// Skip archived locations
		if smLocation.Archived {
			continue
		}

		// Skip locations with empty or invalid names
		if strings.TrimSpace(smLocation.Name) == "" {
			log.Printf("AutoSyncSpoolmanLocations: Skipping location with empty name from Spoolman")
			continue
		}

		// Skip if location already exists in FilaBridge
		if fbLocationMap[smLocation.Name] {
			continue
		}

		// Skip if location matches a virtual printer toolhead location
		if b.isVirtualPrinterToolheadLocation(smLocation.Name) {
			log.Printf("AutoSyncSpoolmanLocations: Skipping location '%s' from Spoolman - it matches a virtual printer toolhead location", smLocation.Name)
			continue
		}

		// Create location in FilaBridge
		_, err = b.CreateLocationFromSpoolman(smLocation.Name, "storage")
		if err != nil {
			log.Printf("AutoSyncSpoolmanLocations: Failed to import location '%s': %v", smLocation.Name, err)
			continue
		}

		importedCount++
		log.Printf("AutoSyncSpoolmanLocations: Imported location '%s' from Spoolman", smLocation.Name)
	}

	if importedCount > 0 {
		log.Printf("AutoSyncSpoolmanLocations: Imported %d new locations from Spoolman", importedCount)
	} else {
		log.Printf("AutoSyncSpoolmanLocations: No new locations to import")
	}

	return nil
}

// ImportSpoolmanLocations imports all locations from Spoolman into FilaBridge cache
// This is a one-time migration function to populate the local cache
func (b *FilamentBridge) ImportSpoolmanLocations() error {
	// Get all locations from Spoolman
	spoolmanLocations, err := b.spoolman.GetLocations()
	if err != nil {
		return fmt.Errorf("failed to get Spoolman locations: %w", err)
	}

	importedCount := 0
	for _, smLocation := range spoolmanLocations {
		// Skip archived locations
		if smLocation.Archived {
			continue
		}

		// Check if location already exists in FilaBridge
		existing, err := b.FindLocationByName(smLocation.Name)
		if err != nil {
			log.Printf("Error checking for existing location '%s': %v", smLocation.Name, err)
			continue
		}

		if existing != nil {
			// Location already exists, skip
			continue
		}

		// Create location in FilaBridge
		_, err = b.CreateLocationFromSpoolman(smLocation.Name, "storage")
		if err != nil {
			log.Printf("Failed to import location '%s': %v", smLocation.Name, err)
			continue
		}

		importedCount++
		log.Printf("Imported location '%s' from Spoolman", smLocation.Name)
	}

	log.Printf("Import complete: %d locations imported from Spoolman", importedCount)
	return nil
}

// StartLocationSync starts the background location sync process
func (b *FilamentBridge) StartLocationSync() {
	// Get sync interval from config
	syncInterval := b.config.LocationSyncInterval
	if syncInterval == 0 {
		syncInterval = 5 * time.Minute // Default fallback
	}

	// Run initial sync on startup
	go func() {
		// Wait a bit for the system to fully initialize
		time.Sleep(5 * time.Second)
		if err := b.AutoSyncSpoolmanLocations(); err != nil {
			log.Printf("Startup location sync failed: %v", err)
		}
	}()

	// Start periodic sync
	go func() {
		ticker := time.NewTicker(syncInterval)
		defer ticker.Stop()

		for range ticker.C {
			if err := b.AutoSyncSpoolmanLocations(); err != nil {
				log.Printf("Periodic location sync failed: %v", err)
			}
		}
	}()

	log.Printf("Location sync started - will run every %v", syncInterval)
}

// Close closes the database connection
func (b *FilamentBridge) Close() error {
	if b.db != nil {
		return b.db.Close()
	}
	return nil
}
