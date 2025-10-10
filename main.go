package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	// Command line flags
	var (
		webOnly    = flag.Bool("web-only", false, "Run only the web interface")
		bridgeOnly = flag.Bool("bridge-only", false, "Run only the bridge service")
		port       = flag.String("port", DefaultWebPort, "Web interface port")
		host       = flag.String("host", "0.0.0.0", "Web interface host")
	)
	flag.Parse()

	// Create bridge instance first (with default config)
	bridge, err := NewFilamentBridge(nil)
	if err != nil {
		log.Fatalf("Failed to create bridge: %v", err)
	}
	defer bridge.Close()

	// Load configuration from database
	config, err := LoadConfig(bridge)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Update bridge with loaded config
	if err := bridge.UpdateConfig(config); err != nil {
		log.Fatalf("Failed to update bridge config: %v", err)
	}

	// Override port from config if not specified
	if *port == DefaultWebPort && config.WebPort != DefaultWebPort {
		*port = config.WebPort
	}

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	if *webOnly {
		// Run only web interface
		fmt.Println("Starting web interface only...")
		webServer := NewWebServer(bridge)
		go func() {
			if err := webServer.Start(*port); err != nil {
				log.Fatalf("Web server error: %v", err)
			}
		}()

		// Wait for shutdown signal
		<-sigChan
		fmt.Println("Shutting down web server...")

	} else if *bridgeOnly {
		// Run only bridge service
		fmt.Println("Starting bridge service only...")
		fmt.Printf("Monitoring printers: %v\n", getPrinterNames(config))
		fmt.Printf("Spoolman URL: %s\n", config.SpoolmanURL)
		fmt.Printf("Poll interval: %v\n", config.PollInterval)

		// Start monitoring in a goroutine
		go func() {
			ticker := time.NewTicker(config.PollInterval)
			defer ticker.Stop()

			// Run initial check
			bridge.MonitorPrinters()

			// Continue monitoring
			for {
				select {
				case <-ticker.C:
					bridge.MonitorPrinters()
				case <-sigChan:
					return
				}
			}
		}()

		// Wait for shutdown signal
		<-sigChan
		fmt.Println("Shutting down bridge service...")

	} else {
		// Run both bridge service and web interface
		fmt.Println("Starting both bridge service and web interface...")
		fmt.Printf("Monitoring printers: %v\n", getPrinterNames(config))
		fmt.Printf("Spoolman URL: %s\n", config.SpoolmanURL)
		fmt.Printf("Poll interval: %v\n", config.PollInterval)
		fmt.Printf("Web interface: http://%s:%s\n", *host, *port)

		// Create web server first so we can pass it to monitoring
		webServer := NewWebServer(bridge)

		// Start bridge monitoring in a goroutine
		go func() {
			ticker := time.NewTicker(config.PollInterval)
			defer ticker.Stop()

			// Run initial check
			bridge.MonitorPrinters()
			// Broadcast initial status
			webServer.BroadcastStatus()

			// Continue monitoring
			for {
				select {
				case <-ticker.C:
					bridge.MonitorPrinters()
					// Broadcast status after each monitoring cycle
					webServer.BroadcastStatus()
				case <-sigChan:
					return
				}
			}
		}()

		// Start web server in a goroutine
		go func() {
			if err := webServer.Start(*port); err != nil {
				log.Fatalf("Web server error: %v", err)
			}
		}()

		// Wait for shutdown signal
		<-sigChan
		fmt.Println("Shutting down services...")
	}
}

// getPrinterNames returns a slice of printer names from config
func getPrinterNames(config *Config) []string {
	names := make([]string, 0, len(config.Printers))
	for name := range config.Printers {
		names = append(names, name)
	}
	return names
}
