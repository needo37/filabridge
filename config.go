package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	SpoolmanURL  string
	PollInterval time.Duration
	DBFile       string
	WebPort      string
	Printers     map[string]PrinterConfig // Key is printer ID, value is printer config
}

// LoadConfig loads configuration from database
func LoadConfig(bridge *FilamentBridge) (*Config, error) {
	// Get configuration from database
	configValues, err := bridge.GetAllConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load config from database: %w", err)
	}

	// Parse poll interval
	pollInterval := 30
	if pollStr, exists := configValues["poll_interval"]; exists {
		if parsed, err := strconv.Atoi(pollStr); err == nil {
			pollInterval = parsed
		}
	}

	config := &Config{
		SpoolmanURL:  configValues["spoolman_url"],
		PollInterval: time.Duration(pollInterval) * time.Second,
		DBFile:       getDBFilePath(),
		WebPort:      configValues["web_port"],
		Printers:     make(map[string]PrinterConfig),
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
		printerName := printerConfig.Name
		printerModel := printerConfig.Model
		toolheads := printerConfig.Toolheads

		// Attempt to get actual printer info from PrusaLink if we have an API key
		if printerConfig.APIKey != "" && printerConfig.IPAddress != "" {
			client := NewPrusaLinkClient(printerConfig.IPAddress, printerConfig.APIKey)
			printerInfo, err := client.GetPrinterInfo()
			if err != nil {
				fmt.Printf("Error getting printer info for %s (%s): %v\n", printerConfig.IPAddress, printerName, err)
				// Keep the stored config as fallback
			} else {
				// Don't override the user-configured name with hostname
				// printerName = printerInfo.Hostname  // REMOVED: This was overriding user's configured name

				// Only update model if it wasn't explicitly configured by user
				if printerConfig.Model == "" || printerConfig.Model == "Unknown" {
					// Determine model based on hostname or other indicators
					if strings.Contains(strings.ToLower(printerInfo.Hostname), "core") {
						printerModel = "CORE One"
					} else if strings.Contains(strings.ToLower(printerInfo.Hostname), "xl") {
						printerModel = "XL"
					} else {
						printerModel = "Unknown"
					}
				}
				// Keep the user-configured toolheads count
				// toolheads = 1  // REMOVED: This was overriding user's configured toolheads
			}
		}

		config.Printers[printerID] = PrinterConfig{
			Name:      printerName,
			Model:     printerModel,
			IPAddress: printerConfig.IPAddress,
			APIKey:    printerConfig.APIKey,
			Toolheads: toolheads,
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
		return filepath.Join(dbPath, "filabridge.db")
	}
	return "filabridge.db"
}
