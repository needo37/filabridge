package main

// Printer states
const (
	StateIdle          = "IDLE"
	StatePrinting      = "PRINTING"
	StateFinished      = "FINISHED"
	StateOffline       = "offline"
	StateNotConfigured = "not_configured"
)

// Default configuration values
const (
	DefaultSpoolmanURL  = "http://localhost:7912"
	DefaultWebPort      = "5000"
	DefaultPollInterval = 30
	DefaultDBFileName   = "filabridge.db"
)

// Database configuration keys
const (
	ConfigKeyPrinterIPs                   = "printer_ips"
	ConfigKeyAPIKey                       = "prusalink_api_key"
	ConfigKeySpoolmanURL                  = "spoolman_url"
	ConfigKeyPollInterval                 = "poll_interval"
	ConfigKeyWebPort                      = "web_port"
	ConfigKeyPrusaLinkTimeout             = "prusalink_timeout"
	ConfigKeyPrusaLinkFileDownloadTimeout = "prusalink_file_download_timeout"
	ConfigKeySpoolmanTimeout              = "spoolman_timeout"
)

// HTTP timeouts
const (
	PrusaLinkTimeout             = 10  // seconds
	PrusaLinkFileDownloadTimeout = 300 // seconds for file downloads (USB storage can be slow)
	SpoolmanTimeout              = 10  // seconds
)

// Printer model detection patterns
const (
	ModelCorePattern = "core"
	ModelXLPattern   = "xl"
	ModelMK4Pattern  = "mk4"
	ModelMK3Pattern  = "mk3"
	ModelMiniPattern = "mini"
)

// Printer model names
const (
	ModelCoreOne  = "CORE One"
	ModelXL       = "XL"
	ModelMK4      = "MK4"
	ModelMK35     = "MK3.5"
	ModelMiniPlus = "MINI+"
	ModelUnknown  = "Unknown"
)
