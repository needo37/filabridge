package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// PrinterConfig represents configuration for a single printer
type PrinterConfig struct {
	Name      string `json:"name"`
	Model     string `json:"model"`
	IPAddress string `json:"ip_address"`
	APIKey    string `json:"api_key,omitempty"`
	Toolheads int    `json:"toolheads"`
}

// FilamentSpool represents a filament spool from Spoolman
type FilamentSpool struct {
	ID              int     `json:"id"`
	Name            string  `json:"name"`
	Brand           string  `json:"brand"`
	Material        string  `json:"material"`
	Color           string  `json:"color"`
	RemainingLength float64 `json:"remaining_length"`
	TotalLength     float64 `json:"total_length"`
	ToolheadMapping *int    `json:"toolhead_mapping,omitempty"`
}

// Config holds all configuration for the application
type Config struct {
	SpoolmanURL                  string
	PollInterval                 time.Duration
	DBFile                       string
	WebPort                      string
	PrusaLinkTimeout             int
	PrusaLinkFileDownloadTimeout int
	SpoolmanTimeout              int
	Printers                     map[string]PrinterConfig // Key is printer ID, value is printer config
}

// LoadConfig loads configuration from database
func LoadConfig(bridge *FilamentBridge) (*Config, error) {
	// Get configuration from database
	configValues, err := bridge.GetAllConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load config from database: %w", err)
	}

	// Parse poll interval
	pollInterval := DefaultPollInterval
	if pollStr, exists := configValues[ConfigKeyPollInterval]; exists {
		if parsed, err := strconv.Atoi(pollStr); err == nil {
			pollInterval = parsed
		}
	}

	// Parse timeout values
	prusaLinkTimeout := PrusaLinkTimeout
	if timeoutStr, exists := configValues[ConfigKeyPrusaLinkTimeout]; exists {
		if parsed, err := strconv.Atoi(timeoutStr); err == nil {
			prusaLinkTimeout = parsed
		}
	}

	prusaLinkFileDownloadTimeout := PrusaLinkFileDownloadTimeout
	if timeoutStr, exists := configValues[ConfigKeyPrusaLinkFileDownloadTimeout]; exists {
		if parsed, err := strconv.Atoi(timeoutStr); err == nil {
			prusaLinkFileDownloadTimeout = parsed
		}
	}

	spoolmanTimeout := SpoolmanTimeout
	if timeoutStr, exists := configValues[ConfigKeySpoolmanTimeout]; exists {
		if parsed, err := strconv.Atoi(timeoutStr); err == nil {
			spoolmanTimeout = parsed
		}
	}

	config := &Config{
		SpoolmanURL:                  configValues[ConfigKeySpoolmanURL],
		PollInterval:                 time.Duration(pollInterval) * time.Second,
		DBFile:                       getDBFilePath(),
		WebPort:                      configValues[ConfigKeyWebPort],
		PrusaLinkTimeout:             prusaLinkTimeout,
		PrusaLinkFileDownloadTimeout: prusaLinkFileDownloadTimeout,
		SpoolmanTimeout:              spoolmanTimeout,
		Printers:                     make(map[string]PrinterConfig),
	}

	// Load individual printer configurations from database
	printerConfigs, err := bridge.GetAllPrinterConfigs()
	if err != nil {
		fmt.Printf("Error loading printer configs: %v\n", err)
		// Fallback to empty config
		config.Printers["no_printers"] = PrinterConfig{
			Name:      "No Printers Configured",
			Model:     "Unknown",
			IPAddress: "",
			APIKey:    "",
			Toolheads: 0,
		}
		return config, nil
	}

	// Process each printer configuration
	for printerID, printerConfig := range printerConfigs {
		// Load printer configs directly from database without making API calls
		// This prevents race conditions and timeouts during config loading
		// Live printer status will be handled by the monitoring cycle
		config.Printers[printerID] = PrinterConfig{
			Name:      printerConfig.Name,
			Model:     printerConfig.Model,
			IPAddress: printerConfig.IPAddress,
			APIKey:    printerConfig.APIKey,
			Toolheads: printerConfig.Toolheads,
		}
	}

	// If no printers configured, add placeholder
	if len(config.Printers) == 0 {
		config.Printers["no_printers"] = PrinterConfig{
			Name:      "No Printers Configured",
			Model:     "Unknown",
			IPAddress: "",
			APIKey:    "",
			Toolheads: 0,
		}
	}

	return config, nil
}

// getDBFilePath returns the database file path, checking environment variable first
func getDBFilePath() string {
	if dbPath := os.Getenv("FILABRIDGE_DB_PATH"); dbPath != "" {
		return filepath.Join(dbPath, DefaultDBFileName)
	}
	return DefaultDBFileName
}
