package main

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// FilamentBridge manages the connection between PrusaLink and Spoolman
type FilamentBridge struct {
	config         *Config
	spoolman       *SpoolmanClient
	db             *sql.DB
	wasPrinting    map[string]bool
	currentJobFile map[string]string // Store current job filename per printer
	mutex          sync.RWMutex
}

// ToolheadMapping represents a mapping between a printer toolhead and a spool
type ToolheadMapping struct {
	PrinterName string    `json:"printer_name"`
	ToolheadID  int       `json:"toolhead_id"`
	SpoolID     int       `json:"spool_id"`
	MappedAt    time.Time `json:"mapped_at"`
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
		config:         config,
		spoolman:       NewSpoolmanClient("http://localhost:8000"), // Default URL, will be updated
		wasPrinting:    make(map[string]bool),
		currentJobFile: make(map[string]string),
	}

	// Initialize database
	if err := bridge.initDatabase(); err != nil {
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}

	// Update Spoolman URL if config is provided
	if config != nil && config.SpoolmanURL != "" {
		bridge.spoolman = NewSpoolmanClient(config.SpoolmanURL)
	}

	return bridge, nil
}

// initDatabase initializes the SQLite database
func (b *FilamentBridge) initDatabase() error {
	dbFile := "filabridge.db"
	if b.config != nil {
		dbFile = b.config.DBFile
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
		"printer_ips":       "", // Comma-separated list of printer IP addresses
		"prusalink_api_key": "", // PrusaLink API key for authentication
		"spoolman_url":      "http://localhost:8000",
		"poll_interval":     "30",
		"web_port":          "5000",
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
		"printer_ips":       "Comma-separated list of printer IP addresses for PrusaLink",
		"prusalink_api_key": "PrusaLink API key for authentication",
		"spoolman_url":      "URL of Spoolman instance",
		"poll_interval":     "Polling interval in seconds",
		"web_port":          "Port for web interface",
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
	_, err := b.db.Exec("DELETE FROM printer_configs WHERE printer_id = ?", printerID)
	if err != nil {
		return fmt.Errorf("failed to delete printer config: %w", err)
	}
	return nil
}

// ReloadConfig reloads the configuration from the database
func (b *FilamentBridge) ReloadConfig() error {
	config, err := LoadConfig(b)
	if err != nil {
		return fmt.Errorf("failed to reload config: %w", err)
	}
	b.config = config
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
	b.spoolman = NewSpoolmanClient(config.SpoolmanURL)

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
	defer b.mutex.Unlock()

	_, err := b.db.Exec(
		"INSERT OR REPLACE INTO toolhead_mappings (printer_name, toolhead_id, spool_id, mapped_at) VALUES (?, ?, ?, ?)",
		printerName, toolheadID, spoolID, time.Now(),
	)
	if err != nil {
		return fmt.Errorf("failed to set toolhead mapping: %w", err)
	}

	log.Printf("Mapped %s toolhead %d to spool %d", printerName, toolheadID, spoolID)
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

	_, err := b.db.Exec(
		"INSERT INTO print_history (printer_name, toolhead_id, spool_id, filament_used, print_finished, job_name) VALUES (?, ?, ?, ?, ?, ?)",
		printerName, toolheadID, spoolID, filamentUsed, time.Now(), jobName,
	)
	if err != nil {
		return fmt.Errorf("failed to log print usage: %w", err)
	}

	return nil
}

// MonitorPrinters monitors all printers for print status changes
func (b *FilamentBridge) MonitorPrinters() {
	log.Printf("Monitoring printers at %s", time.Now().Format(time.RFC3339))

	if len(b.config.Printers) == 0 {
		log.Printf("No printers configured - skipping monitoring")
		return
	}

	// Monitor each printer using PrusaLink
	for printerID, printerConfig := range b.config.Printers {
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
	client := NewPrusaLinkClient(config.IPAddress, config.APIKey)

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

	// Check if print just finished
	b.mutex.Lock()
	wasPrinting := b.wasPrinting[printerID]
	storedJobFile := b.currentJobFile[printerID]
	b.mutex.Unlock()

	// Debug logging for all printers
	log.Printf("Printer %s (%s): state=%s, wasPrinting=%v, job=%s, stored_file=%s",
		config.IPAddress, printerID, currentState, wasPrinting, jobName, storedJobFile)

	// SIMPLE LOGIC: Check if we just finished printing
	// If we were printing in the previous cycle AND now we're finished, process it
	if (currentState == "IDLE" || currentState == "FINISHED") && wasPrinting {
		// Use stored filename (should be available since we stored it when printing started)
		filenameToUse := storedJobFile
		if filenameToUse == "" {
			log.Printf("Warning: No stored filename for %s (%s), using current job filename: %s",
				config.IPAddress, printerID, currentJobFilename)
			filenameToUse = currentJobFilename
		}

		log.Printf("🎉 Print finished detected for %s (%s): %s (state: %s, file: %s)",
			config.IPAddress, printerID, jobName, currentState, filenameToUse)

		if err := b.handlePrusaLinkPrintFinished(config, filenameToUse); err != nil {
			log.Printf("Error handling PrusaLink print finished: %v", err)
		}
	}

	// Update state tracking
	b.mutex.Lock()

	// Store the current job filename when printing starts (only if not already stored)
	if currentState == "PRINTING" && currentJobFilename != "" && storedJobFile == "" {
		b.currentJobFile[printerID] = currentJobFilename
		log.Printf("📁 Stored job filename for %s (%s): %s", config.IPAddress, printerID, currentJobFilename)
	}

	// Update wasPrinting flag for NEXT cycle
	b.wasPrinting[printerID] = currentState == "PRINTING"

	// Clear stored filename when print finishes
	if currentState == "IDLE" || currentState == "FINISHED" {
		b.currentJobFile[printerID] = ""
	}

	b.mutex.Unlock()

	return nil
}

// handlePrusaLinkPrintFinished handles when a print job finishes via PrusaLink
func (b *FilamentBridge) handlePrusaLinkPrintFinished(config PrinterConfig, filename string) error {
	log.Printf("Print finished via PrusaLink (%s): %s", config.IPAddress, filename)

	printerName := config.Name
	if printerName == "" {
		printerName = fmt.Sprintf("Printer_%s", config.IPAddress)
	}

	// Create PrusaLink client for this printer
	prusaClient := NewPrusaLinkClient(config.IPAddress, config.APIKey)

	// Always use G-code analysis since PrusaLink doesn't provide filament usage data
	var filamentUsage map[int]float64

	// Download and parse the .bgcode file for filament usage
	log.Printf("Analyzing .bgcode file for filament usage")

	// Use the filename parameter (stored when print started)
	if filename != "" {
		log.Printf("Downloading .bgcode file: %s", filename)
		gcodeContent, err := prusaClient.GetGcodeFile(filename)
		if err != nil {
			log.Printf("Error downloading G-code file %s: %v", filename, err)
			// Use rough estimation as last resort
			filamentUsage = b.estimateFilamentUsage(config.Toolheads)
		} else {
			log.Printf("Successfully downloaded .bgcode file, parsing for filament usage...")
			filamentUsage, err = prusaClient.ParseGcodeFilamentUsage(gcodeContent)
			if err != nil {
				log.Printf("Error parsing G-code for filament usage: %v", err)
				filamentUsage = b.estimateFilamentUsage(config.Toolheads)
			} else {
				log.Printf("Successfully parsed .bgcode file for filament usage")
			}
		}
	} else {
		log.Printf("No filename available, using rough estimation")
		filamentUsage = b.estimateFilamentUsage(config.Toolheads)
	}

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
		if err := b.LogPrintUsage(printerName, toolheadID, spoolID, usedWeight, filename); err != nil {
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

// estimateFilamentUsage provides a rough estimation when no accurate data is available
func (b *FilamentBridge) estimateFilamentUsage(toolheadCount int) map[int]float64 {
	// Very rough estimation: 5g per toolhead
	// This is a fallback when no accurate data is available
	usage := make(map[int]float64)
	for i := 0; i < toolheadCount; i++ {
		usage[i] = 5.0 // 5g per toolhead as rough estimate
	}
	log.Printf("Using rough filament estimation: 5g per toolhead")
	return usage
}

// GetStatus gets current status of all printers and mappings
func (b *FilamentBridge) GetStatus() (*PrinterStatus, error) {
	status := &PrinterStatus{
		Printers:         make(map[string]PrinterData),
		ToolheadMappings: make(map[string]map[int]ToolheadMapping),
		Timestamp:        time.Now(),
	}

	// Get printer statuses from PrusaLink
	if len(b.config.Printers) > 0 {
		for printerID, printerConfig := range b.config.Printers {
			if printerID == "no_printers" {
				continue // Skip placeholder
			}

			client := NewPrusaLinkClient(printerConfig.IPAddress, printerConfig.APIKey)

			// Use the configured printer name, not the hostname from PrusaLink
			printerName := printerConfig.Name

			// Get current status
			printerStatus, err := client.GetStatus()
			if err != nil {
				status.Printers[printerID] = PrinterData{
					Name:  printerName,
					State: "offline",
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
			State: "not_configured",
		}
	}

	// Get toolhead mappings for all printers
	for printerID, printerConfig := range b.config.Printers {
		if printerID == "no_printers" {
			continue // Skip placeholder
		}

		printerName := printerConfig.Name
		mappings, err := b.GetToolheadMappings(printerName)
		if err != nil {
			log.Printf("Error getting toolhead mappings for %s: %v", printerName, err)
			status.ToolheadMappings[printerID] = make(map[int]ToolheadMapping)
		} else {
			status.ToolheadMappings[printerID] = mappings
		}
	}

	return status, nil
}

// Close closes the database connection
func (b *FilamentBridge) Close() error {
	if b.db != nil {
		return b.db.Close()
	}
	return nil
}
