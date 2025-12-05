package main

import (
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/skip2/go-qrcode"
)

//go:embed templates/*
var templatesFS embed.FS

//go:embed static/**
var staticFS embed.FS

// WebServer handles HTTP requests using Gin
type WebServer struct {
	bridge         *FilamentBridge
	router         *gin.Engine
	operationMutex sync.Mutex // Protects add/update/delete printer operations
	wsHub          *WebSocketHub
}

// WebSocketHub manages WebSocket connections and broadcasts
type WebSocketHub struct {
	clients    map[*WebSocketClient]bool
	register   chan *WebSocketClient
	unregister chan *WebSocketClient
	broadcast  chan []byte
	mutex      sync.RWMutex
}

// WebSocketClient represents a WebSocket connection
type WebSocketClient struct {
	hub  *WebSocketHub
	conn *websocket.Conn
	send chan []byte
}

// WebSocketMessage represents the structure of messages sent to clients
type WebSocketMessage struct {
	Type             string                             `json:"type"`
	Timestamp        time.Time                          `json:"timestamp"`
	Printers         map[string]PrinterData             `json:"printers"`
	Spools           []SpoolmanSpool                    `json:"spools"`
	ToolheadMappings map[string]map[int]ToolheadMapping `json:"toolhead_mappings"`
	PrintErrors      []PrintError                       `json:"print_errors,omitempty"`
}

// NewWebServer creates a new web server with Gin
func NewWebServer(bridge *FilamentBridge) *WebServer {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()

	// Add middleware
	router.Use(gin.Logger())
	router.Use(gin.Recovery())

	// Add custom recovery middleware for API routes to ensure JSON responses
	router.Use(func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				// Check if this is an API route
				if strings.HasPrefix(c.Request.URL.Path, "/api/") {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
					c.Abort()
				} else {
					// For non-API routes, use default recovery behavior
					c.AbortWithStatus(http.StatusInternalServerError)
				}
			}
		}()
		c.Next()
	})

	// Create WebSocket hub
	wsHub := &WebSocketHub{
		clients:    make(map[*WebSocketClient]bool),
		register:   make(chan *WebSocketClient),
		unregister: make(chan *WebSocketClient),
		broadcast:  make(chan []byte),
	}

	ws := &WebServer{
		bridge: bridge,
		router: router,
		wsHub:  wsHub,
	}

	// Start WebSocket hub
	go wsHub.run()

	ws.setupRoutes()
	return ws
}

// generateToolheadIDs generates a slice of toolhead IDs from 0 to count-1
func generateToolheadIDs(count int) []int {
	ids := make([]int, count)
	for i := 0; i < count; i++ {
		ids[i] = i
	}
	return ids
}

// setupRoutes configures all the routes
func (ws *WebServer) setupRoutes() {
	// Load HTML templates with custom functions from embedded filesystem
	tmpl := template.Must(template.New("").Funcs(template.FuncMap{
		"generateToolheadIDs": generateToolheadIDs,
	}).ParseFS(templatesFS, "templates/*"))
	ws.router.SetHTMLTemplate(tmpl)

	// Static files (embedded in binary)
	// Use fs.Sub to strip the "static/" prefix from embedded paths
	staticSubFS, err := fs.Sub(staticFS, "static")
	if err != nil {
		log.Fatalf("Failed to create static filesystem: %v", err)
	}
	ws.router.StaticFS("/static", http.FS(staticSubFS))

	// Main dashboard
	ws.router.GET("/", ws.dashboardHandler)

	// API routes
	api := ws.router.Group("/api")
	{
		api.GET("/status", ws.statusHandler)
		api.GET("/spools", ws.spoolsHandler)
		api.GET("/filaments", ws.filamentsHandler)
		api.POST("/map_toolhead", ws.mapToolheadHandler)
		api.GET("/available_spools", ws.availableSpoolsHandler)
		api.GET("/spoolman/test", ws.testSpoolmanConnectionHandler)
		api.GET("/spoolman/debug", ws.debugSpoolmanHandler)
		api.POST("/test/print_complete", ws.testPrintCompleteHandler)
		api.GET("/config", ws.getConfigHandler)
		api.POST("/config", ws.updateConfigHandler)
		api.GET("/config/auto-assign-previous-spool", ws.getAutoAssignPreviousSpoolHandler)
		api.PUT("/config/auto-assign-previous-spool", ws.updateAutoAssignPreviousSpoolHandler)
		api.GET("/printers", ws.getPrintersHandler)
		api.POST("/printers", ws.addPrinterHandler)
		api.PUT("/printers/:id", ws.updatePrinterHandler)
		api.DELETE("/printers/:id", ws.deletePrinterHandler)
		api.GET("/printers/:id/toolheads", ws.getToolheadNamesHandler)
		api.PUT("/printers/:id/toolheads/:toolhead_id", ws.updateToolheadNameHandler)
		api.POST("/detect_printer", ws.detectPrinterHandler)
		api.GET("/print-errors", ws.getPrintErrorsHandler)
		api.POST("/print-errors/:id/acknowledge", ws.acknowledgePrintErrorHandler)
		api.GET("/nfc/assign", ws.nfcAssignHandler)
		api.GET("/nfc/urls", ws.nfcUrlsHandler)
		api.GET("/nfc/session/status", ws.nfcSessionStatusHandler)
		api.GET("/locations", ws.getLocationsHandler)
		api.GET("/locations/:name/status", ws.getLocationStatusHandler)
		api.POST("/locations", ws.createLocationHandler)
		api.PUT("/locations/:name", ws.updateLocationHandler)
		api.DELETE("/locations/:name", ws.deleteLocationHandler)
	}

	// WebSocket endpoint
	ws.router.GET("/ws/status", ws.websocketHandler)
}

// WebSocket hub methods

// run starts the WebSocket hub
func (h *WebSocketHub) run() {
	for {
		select {
		case client := <-h.register:
			h.mutex.Lock()
			h.clients[client] = true
			h.mutex.Unlock()
			log.Printf("WebSocket client connected. Total clients: %d", len(h.clients))

		case client := <-h.unregister:
			h.mutex.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mutex.Unlock()
			log.Printf("WebSocket client disconnected. Total clients: %d", len(h.clients))

		case message := <-h.broadcast:
			h.mutex.RLock()
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
			h.mutex.RUnlock()
		}
	}
}

// BroadcastStatus sends status updates to all connected clients
func (ws *WebServer) BroadcastStatus() {
	// Get current status
	status, err := ws.bridge.GetStatus()
	if err != nil {
		log.Printf("Error getting status for broadcast: %v", err)
		return
	}

	// Get current spools
	spools, err := ws.bridge.spoolman.GetAllSpools()
	if err != nil {
		log.Printf("Error getting spools for broadcast: %v", err)
		spools = []SpoolmanSpool{}
	}

	// Get print errors
	printErrors := ws.bridge.GetPrintErrors()

	// Create message
	message := WebSocketMessage{
		Type:             "status_update",
		Timestamp:        time.Now(),
		Printers:         status.Printers,
		Spools:           spools,
		ToolheadMappings: status.ToolheadMappings,
		PrintErrors:      printErrors,
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(message)
	if err != nil {
		log.Printf("Error marshaling WebSocket message: %v", err)
		return
	}

	// Broadcast to all clients
	select {
	case ws.wsHub.broadcast <- jsonData:
		log.Printf("Broadcasted status update to %d clients", len(ws.wsHub.clients))
	default:
		log.Printf("No clients connected to receive broadcast")
	}
}

// websocketHandler handles WebSocket connections
func (ws *WebServer) websocketHandler(c *gin.Context) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true // Allow connections from any origin
		},
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	client := &WebSocketClient{
		hub:  ws.wsHub,
		conn: conn,
		send: make(chan []byte, 256),
	}

	client.hub.register <- client

	// Start goroutines for reading and writing
	go client.writePump()
	go client.readPump()
}

// WebSocket client methods

// readPump pumps messages from the WebSocket connection to the hub
func (c *WebSocketClient) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(512)
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}
	}
}

// writePump pumps messages from the hub to the WebSocket connection
func (c *WebSocketClient) writePump() {
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Add queued chat messages to the current websocket message
			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write([]byte{'\n'})
				w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// dashboardHandler serves the main dashboard
func (ws *WebServer) dashboardHandler(c *gin.Context) {
	status, err := ws.bridge.GetStatus()
	if err != nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{
			"Error": "Failed to get printer status",
		})
		return
	}

	// Test Spoolman connection
	spoolmanConnected := true
	spoolmanError := ""
	spools, err := ws.bridge.spoolman.GetAllSpools()
	if err != nil {
		spoolmanConnected = false
		spoolmanError = err.Error()
		spools = []SpoolmanSpool{}
	}

	// Check if this is a first run
	isFirstRun, err := ws.bridge.IsFirstRun()
	if err != nil {
		isFirstRun = false
	}

	hasErrors := !spoolmanConnected || hasConnectionErrors(status)

	// Get print errors
	printErrors := ws.bridge.GetPrintErrors()
	hasPrintErrors := len(printErrors) > 0

	c.HTML(http.StatusOK, "index.html", gin.H{
		"Status":            status,
		"Spools":            spools,
		"HasErrors":         hasErrors,
		"HasPrintErrors":    hasPrintErrors,
		"PrintErrors":       printErrors,
		"IsFirstRun":        isFirstRun,
		"Printers":          ws.bridge.config.Printers,
		"SpoolmanConnected": spoolmanConnected,
		"SpoolmanError":     spoolmanError,
		"SpoolmanBaseURL":   ws.bridge.config.SpoolmanURL,
	})
}

// hasConnectionErrors checks if there are connection errors
func hasConnectionErrors(status *PrinterStatus) bool {
	for _, printer := range status.Printers {
		if printer.State == StateOffline {
			return true
		}
	}
	return false
}

// statusHandler returns current status as JSON
func (ws *WebServer) statusHandler(c *gin.Context) {
	status, err := ws.bridge.GetStatus()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, status)
}

// spoolsHandler returns all spools as JSON
func (ws *WebServer) spoolsHandler(c *gin.Context) {
	spools, err := ws.bridge.spoolman.GetAllSpools()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, spools)
}

// filamentsHandler returns all filament types as JSON
func (ws *WebServer) filamentsHandler(c *gin.Context) {
	filaments, err := ws.bridge.spoolman.GetAllFilaments()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, filaments)
}

// validatePrinterConfig validates printer configuration input
func validatePrinterConfig(config PrinterConfig) error {
	if config.Name == "" {
		return fmt.Errorf("printer name is required")
	}
	if config.IPAddress == "" {
		return fmt.Errorf("address is required")
	}
	if config.Toolheads < 1 {
		return fmt.Errorf("toolheads must be at least 1")
	}
	if config.Toolheads > 10 {
		return fmt.Errorf("toolheads cannot exceed 10")
	}
	return nil
}

// validateAddress validates hostname or IP address format
func validateAddress(address string) error {
	if address == "" {
		return fmt.Errorf("address cannot be empty")
	}
	// Basic validation - check for reasonable length (hostnames can be longer than IPs)
	// Minimum: 1 character (e.g., "a"), Maximum: 253 characters (RFC 1035)
	if len(address) < 1 || len(address) > 253 {
		return fmt.Errorf("invalid address format")
	}
	// Basic character validation - allow common characters used in hostnames and IP addresses
	// This includes: letters, numbers, dots, hyphens, underscores, colons (for IPv6), and brackets (for IPv6)
	// The HTTP client will perform more thorough validation when connecting
	for _, char := range address {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || 
			(char >= '0' && char <= '9') || char == '.' || char == '-' || char == '_' || 
			char == ':' || char == '[' || char == ']') {
			return fmt.Errorf("invalid address format: contains invalid characters")
		}
	}
	return nil
}

// mapToolheadHandler maps a spool to a toolhead
func (ws *WebServer) mapToolheadHandler(c *gin.Context) {
	var req struct {
		PrinterName string `json:"printer_name" binding:"required"`
		ToolheadID  int    `json:"toolhead_id"`
		SpoolID     int    `json:"spool_id"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}

	if req.PrinterName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing required parameters"})
		return
	}

	if req.ToolheadID < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Toolhead ID must be non-negative"})
		return
	}

	// Handle unmapping (SpoolID = 0) or mapping (SpoolID > 0)
	if req.SpoolID == 0 {
		// Unmap the toolhead
		if err := ws.bridge.UnmapToolhead(req.PrinterName, req.ToolheadID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "Toolhead unmapped successfully"})
	} else {
		// Map the spool to the toolhead
		if err := ws.bridge.SetToolheadMapping(req.PrinterName, req.ToolheadID, req.SpoolID); err != nil {
			// Check if this is a spool conflict error
			if strings.Contains(err.Error(), "is already assigned to") {
				c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			}
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "Toolhead mapped successfully"})
	}
}

// availableSpoolsHandler returns spools available for assignment to a specific toolhead
func (ws *WebServer) availableSpoolsHandler(c *gin.Context) {
	printerName := c.Query("printer_name")
	toolheadIDStr := c.Query("toolhead_id")

	if printerName == "" || toolheadIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "printer_name and toolhead_id parameters are required"})
		return
	}

	toolheadID, err := strconv.Atoi(toolheadIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid toolhead_id"})
		return
	}

	// Get all spools from Spoolman
	allSpools, err := ws.bridge.spoolman.GetAllSpools()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Get all current toolhead mappings
	allMappings, err := ws.bridge.GetAllToolheadMappings()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Create a set of assigned spool IDs (excluding the current toolhead)
	assignedSpoolIDs := make(map[int]bool)
	for _, printerMappings := range allMappings {
		for tid, mapping := range printerMappings {
			// Skip the current toolhead (allow re-assignment to the same toolhead)
			if mapping.PrinterName == printerName && tid == toolheadID {
				continue
			}
			// Mark this spool as assigned (prevents same spool being used on multiple printers)
			assignedSpoolIDs[mapping.SpoolID] = true
		}
	}

	// Filter out assigned spools
	var availableSpools []SpoolmanSpool
	for _, spool := range allSpools {
		if !assignedSpoolIDs[spool.ID] {
			availableSpools = append(availableSpools, spool)
		}
	}

	c.JSON(http.StatusOK, gin.H{"spools": availableSpools})
}

// getConfigHandler returns current configuration
func (ws *WebServer) getConfigHandler(c *gin.Context) {
	config, err := ws.bridge.GetAllConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, config)
}

// updateConfigHandler updates configuration
func (ws *WebServer) updateConfigHandler(c *gin.Context) {
	var config map[string]string
	if err := c.ShouldBindJSON(&config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}

	// Update each config value
	for key, value := range config {
		if err := ws.bridge.SetConfigValue(key, value); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	// Reload configuration
	newConfig, err := LoadConfig(ws.bridge)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if err := ws.bridge.UpdateConfig(newConfig); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Configuration updated successfully"})
}

// getAutoAssignPreviousSpoolHandler returns current auto-assign previous spool settings
func (ws *WebServer) getAutoAssignPreviousSpoolHandler(c *gin.Context) {
	enabled, err := ws.bridge.GetAutoAssignPreviousSpoolEnabled()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	location, err := ws.bridge.GetAutoAssignPreviousSpoolLocation()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"enabled":  enabled,
		"location": location,
	})
}

// updateAutoAssignPreviousSpoolHandler updates auto-assign previous spool settings
func (ws *WebServer) updateAutoAssignPreviousSpoolHandler(c *gin.Context) {
	var req struct {
		Enabled  bool   `json:"enabled" binding:"required"`
		Location string `json:"location"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON or missing 'enabled' field"})
		return
	}

	// Update enabled setting
	if err := ws.bridge.SetAutoAssignPreviousSpoolEnabled(req.Enabled); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Update location setting
	if err := ws.bridge.SetAutoAssignPreviousSpoolLocation(req.Location); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Auto-assign previous spool settings updated successfully"})
}

// getPrintersHandler returns all configured printers
func (ws *WebServer) getPrintersHandler(c *gin.Context) {
	printerConfigs, err := ws.bridge.GetAllPrinterConfigs()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Enhance printer configs with toolhead names
	result := make(map[string]interface{})
	for printerID, printerConfig := range printerConfigs {
		printerData := map[string]interface{}{
			"name":       printerConfig.Name,
			"model":      printerConfig.Model,
			"ip_address": printerConfig.IPAddress,
			"api_key":    printerConfig.APIKey,
			"toolheads":  printerConfig.Toolheads,
		}

		// Get toolhead names for this printer
		toolheadNames, err := ws.bridge.GetAllToolheadNames(printerID)
		if err == nil {
			// Build toolhead names map with defaults
			toolheadNamesMap := make(map[int]string)
			for toolheadID := 0; toolheadID < printerConfig.Toolheads; toolheadID++ {
				if name, exists := toolheadNames[toolheadID]; exists {
					toolheadNamesMap[toolheadID] = name
				} else {
					toolheadNamesMap[toolheadID] = fmt.Sprintf("Toolhead %d", toolheadID)
				}
			}
			printerData["toolhead_names"] = toolheadNamesMap
		}

		result[printerID] = printerData
	}

	c.JSON(http.StatusOK, gin.H{"printers": result})
}

// addPrinterHandler adds a new printer configuration
func (ws *WebServer) addPrinterHandler(c *gin.Context) {
	// Serialize printer operations to prevent race conditions
	ws.operationMutex.Lock()
	defer ws.operationMutex.Unlock()

	var printerConfig PrinterConfig
	if err := c.ShouldBindJSON(&printerConfig); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate printer configuration
	if err := validatePrinterConfig(printerConfig); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate address
	if err := validateAddress(printerConfig.IPAddress); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Generate a unique printer ID using nanosecond timestamp + random component
	printerID := fmt.Sprintf("printer_%d_%d", time.Now().UnixNano(), time.Now().Nanosecond()%1000)

	// Save the printer configuration
	if err := ws.bridge.SavePrinterConfig(printerID, printerConfig); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Reload configuration to include the new printer
	if err := ws.reloadBridgeConfig(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to reload configuration"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Printer added successfully", "printer_id": printerID})
}

// updatePrinterHandler updates an existing printer configuration
func (ws *WebServer) updatePrinterHandler(c *gin.Context) {
	// Serialize printer operations to prevent race conditions
	ws.operationMutex.Lock()
	defer ws.operationMutex.Unlock()

	printerID := c.Param("id")

	var printerConfig PrinterConfig
	if err := c.ShouldBindJSON(&printerConfig); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate printer configuration
	if err := validatePrinterConfig(printerConfig); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate address
	if err := validateAddress(printerConfig.IPAddress); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Auto-detect model if address or API key changed, or if model is currently "Unknown"
	if printerConfig.Model == "" || printerConfig.Model == ModelUnknown {
		log.Printf("🔍 [Auto-Detection] Detecting model for printer %s (IP: %s)", printerID, printerConfig.IPAddress)

		// Create PrusaLink client for detection
		client := NewPrusaLinkClient(printerConfig.IPAddress, printerConfig.APIKey, 10, 60) // Use default timeouts for detection

		// Try to get printer info
		printerInfo, err := client.GetPrinterInfo()
		if err != nil {
			log.Printf("⚠️ [Auto-Detection] Failed to detect model for %s: %v (keeping current model: %s)",
				printerConfig.IPAddress, err, printerConfig.Model)
		} else {
			// Use shared model detection function
			detectedModel := detectPrinterModel(printerInfo.Hostname)

			if detectedModel != ModelUnknown {
				log.Printf("✅ [Auto-Detection] Detected model for %s: '%s' -> %s",
					printerConfig.IPAddress, printerInfo.Hostname, detectedModel)
				printerConfig.Model = detectedModel
			} else {
				log.Printf("❌ [Auto-Detection] No pattern matched for hostname '%s' from %s",
					printerInfo.Hostname, printerConfig.IPAddress)
			}
		}
	}

	// Save the updated printer configuration
	if err := ws.bridge.SavePrinterConfig(printerID, printerConfig); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Reload configuration to include the updated printer
	if err := ws.reloadBridgeConfig(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to reload configuration"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Printer updated successfully"})
}

// deletePrinterHandler deletes a printer configuration
func (ws *WebServer) deletePrinterHandler(c *gin.Context) {
	// Serialize printer operations to prevent race conditions
	ws.operationMutex.Lock()
	defer ws.operationMutex.Unlock()

	printerID := c.Param("id")

	// Delete the printer configuration
	if err := ws.bridge.DeletePrinterConfig(printerID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Reload configuration to remove the deleted printer
	if err := ws.reloadBridgeConfig(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to reload configuration"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Printer deleted successfully"})
}

// getToolheadNamesHandler returns all toolhead names for a printer
func (ws *WebServer) getToolheadNamesHandler(c *gin.Context) {
	printerID := c.Param("id")

	// Verify printer exists
	printerConfigs, err := ws.bridge.GetAllPrinterConfigs()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	printerConfig, exists := printerConfigs[printerID]
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Printer not found"})
		return
	}

	// Get all toolhead names
	toolheadNames, err := ws.bridge.GetAllToolheadNames(printerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Build response with all toolheads (including defaults for unnamed ones)
	result := make(map[int]string)
	for toolheadID := 0; toolheadID < printerConfig.Toolheads; toolheadID++ {
		if name, exists := toolheadNames[toolheadID]; exists {
			result[toolheadID] = name
		} else {
			result[toolheadID] = fmt.Sprintf("Toolhead %d", toolheadID)
		}
	}

	c.JSON(http.StatusOK, gin.H{"toolhead_names": result})
}

// updateToolheadNameHandler updates a toolhead's display name
func (ws *WebServer) updateToolheadNameHandler(c *gin.Context) {
	printerID := c.Param("id")
	toolheadIDStr := c.Param("toolhead_id")

	// Parse toolhead ID
	toolheadID, err := strconv.Atoi(toolheadIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid toolhead ID"})
		return
	}

	// Verify printer exists
	printerConfigs, err := ws.bridge.GetAllPrinterConfigs()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	printerConfig, exists := printerConfigs[printerID]
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Printer not found"})
		return
	}

	// Validate toolhead ID is within range
	if toolheadID < 0 || toolheadID >= printerConfig.Toolheads {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Toolhead ID must be between 0 and %d", printerConfig.Toolheads-1)})
		return
	}

	// Parse request body
	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON or missing 'name' field"})
		return
	}

	// Update toolhead name
	if err := ws.bridge.SetToolheadName(printerID, toolheadID, req.Name); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Toolhead name updated successfully"})
}

// detectPrinterModel detects printer model from hostname
func detectPrinterModel(hostname string) string {
	model := ModelUnknown
	hostnameLower := strings.ToLower(hostname)
	hostnameLower = strings.TrimSpace(hostnameLower) // Clean up any whitespace

	log.Printf("🔍 [Detection] Checking hostname '%s' against patterns:", hostnameLower)

	if strings.Contains(hostnameLower, ModelCorePattern) {
		model = ModelCoreOne
		log.Printf("✅ [Detection] Matched pattern '%s' -> %s", ModelCorePattern, model)
	} else if strings.Contains(hostnameLower, ModelXLPattern) {
		model = ModelXL
		log.Printf("✅ [Detection] Matched pattern '%s' -> %s", ModelXLPattern, model)
	} else if strings.Contains(hostnameLower, ModelMK4Pattern) {
		model = ModelMK4
		log.Printf("✅ [Detection] Matched pattern '%s' -> %s", ModelMK4Pattern, model)
	} else if strings.Contains(hostnameLower, ModelMK3Pattern) {
		model = ModelMK35
		log.Printf("✅ [Detection] Matched pattern '%s' -> %s", ModelMK3Pattern, model)
	} else if strings.Contains(hostnameLower, ModelMiniPattern) {
		model = ModelMiniPlus
		log.Printf("✅ [Detection] Matched pattern '%s' -> %s", ModelMiniPattern, model)
	} else {
		log.Printf("❌ [Detection] No pattern matched for hostname '%s'. Available patterns: %s, %s, %s, %s, %s",
			hostnameLower, ModelCorePattern, ModelXLPattern, ModelMK4Pattern, ModelMK3Pattern, ModelMiniPattern)
	}

	log.Printf("🎯 [Detection] Final result: hostname='%s' -> model='%s'", hostname, model)
	return model
}

// detectPrinterHandler detects printer model from PrusaLink API
func (ws *WebServer) detectPrinterHandler(c *gin.Context) {
	var req struct {
		IPAddress string `json:"ip_address" binding:"required"`
		APIKey    string `json:"api_key" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}

	// Validate address
	if err := validateAddress(req.IPAddress); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	log.Printf("🔍 [Detection] Starting printer model detection for IP: %s", req.IPAddress)

	// Create PrusaLink client
	client := NewPrusaLinkClient(req.IPAddress, req.APIKey, 10, 60) // Use default timeouts for detection

	// Try to get printer info, but don't fail if it times out
	printerInfo, err := client.GetPrinterInfo()
	if err != nil {
		log.Printf("❌ [Detection] Failed to get printer info from %s: %v", req.IPAddress, err)
		// If API call fails, return default values instead of error
		// This allows users to add printers even if they're offline
		c.JSON(http.StatusOK, gin.H{
			"model":    ModelUnknown,
			"hostname": "Unknown",
			"detected": false,
			"warning":  "Could not connect to printer. You can still add it manually.",
		})
		return
	}

	log.Printf("📥 [Detection] Received printer info: hostname='%s'", printerInfo.Hostname)

	// Use shared model detection function
	model := detectPrinterModel(printerInfo.Hostname)

	// Return detected information (toolheads will be provided by user)
	c.JSON(http.StatusOK, gin.H{
		"model":    model,
		"hostname": printerInfo.Hostname,
		"detected": true,
	})
}

// testSpoolmanConnectionHandler tests the connection to Spoolman
func (ws *WebServer) testSpoolmanConnectionHandler(c *gin.Context) {
	if err := ws.bridge.spoolman.TestConnection(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "connected": false})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Connection successful", "connected": true})
}

// debugSpoolmanHandler provides detailed debug information about Spoolman data
func (ws *WebServer) debugSpoolmanHandler(c *gin.Context) {
	spools, err := ws.bridge.spoolman.GetAllSpools()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	debugInfo := gin.H{
		"spool_count": len(spools),
		"spools":      spools,
		"raw_data":    make([]gin.H, len(spools)),
	}

	// Add raw field analysis
	for i, spool := range spools {
		debugInfo["raw_data"].([]gin.H)[i] = gin.H{
			"id":               spool.ID,
			"name":             spool.Name,
			"brand":            spool.Brand,
			"material":         spool.Material,
			"color":            spool.Filament.ColorHex,
			"remaining_length": spool.RemainingLength,
			"name_empty":       spool.Name == "",
			"brand_empty":      spool.Brand == "",
			"material_empty":   spool.Material == "",
			"color_empty":      spool.Filament.ColorHex == "",
		}
	}

	c.JSON(http.StatusOK, debugInfo)
}

// testPrintCompleteHandler simulates a print completion for testing
func (ws *WebServer) testPrintCompleteHandler(c *gin.Context) {
	var request struct {
		PrinterName   string          `json:"printer_name" binding:"required"`
		JobName       string          `json:"job_name"`
		FilamentUsage map[int]float64 `json:"filament_usage"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if request.JobName == "" {
		request.JobName = "Test Print Job"
	}

	// If no filament usage provided, use default test values
	if len(request.FilamentUsage) == 0 {
		request.FilamentUsage = map[int]float64{
			0: 10.0, // 10g for toolhead 0
		}
	}

	// Get printer config - first try by name, then by ID
	var config PrinterConfig
	var found bool

	// Try to find by name first
	for _, printerConfig := range ws.bridge.config.Printers {
		if printerConfig.Name == request.PrinterName {
			config = printerConfig
			found = true
			break
		}
	}

	// If not found by name, try by ID
	if !found {
		config, found = ws.bridge.config.Printers[request.PrinterName]
	}

	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "Printer not found"})
		return
	}

	// Simulate the print completion with provided filament usage
	printerName := resolvePrinterName(config)

	// Process filament usage using helper function
	if err := ws.bridge.processFilamentUsage(printerName, request.FilamentUsage, request.JobName); err != nil {
		log.Printf("Error processing filament usage: %v", err)
	}

	c.JSON(http.StatusOK, gin.H{
		"message":        "Print completion simulated successfully",
		"printer":        request.PrinterName,
		"job":            request.JobName,
		"filament_usage": request.FilamentUsage,
	})
}

// getPrintErrorsHandler returns all unacknowledged print errors
func (ws *WebServer) getPrintErrorsHandler(c *gin.Context) {
	errors := ws.bridge.GetPrintErrors()
	c.JSON(http.StatusOK, gin.H{
		"errors": errors,
	})
}

// acknowledgePrintErrorHandler acknowledges a print error
func (ws *WebServer) acknowledgePrintErrorHandler(c *gin.Context) {
	// Ensure we always return JSON
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Panic in acknowledgePrintErrorHandler: %v", r)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
		}
	}()

	errorID := c.Param("id")
	if errorID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Error ID is required"})
		return
	}

	if err := ws.bridge.AcknowledgePrintError(errorID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Error acknowledged"})
}

// reloadBridgeConfig reloads the bridge configuration after changes
func (ws *WebServer) reloadBridgeConfig() error {
	// Reload configuration to include changes
	if err := ws.bridge.ReloadConfig(); err != nil {
		return fmt.Errorf("failed to reload configuration: %w", err)
	}
	return nil
}

// Start starts the web server
func (ws *WebServer) Start(port string) error {
	return ws.router.Run(":" + port)
}

// nfcAssignHandler handles NFC tag scans
func (ws *WebServer) nfcAssignHandler(c *gin.Context) {
	spoolIDStr := c.Query("spool")
	locationStr := c.Query("location")
	clientIP := getClientIP(c.ClientIP())

	// Generate session ID based on client IP
	sessionID := generateSessionID(clientIP)

	var spoolID int
	var printerName string
	var toolheadID int
	var err error

	// Parse parameters
	if spoolIDStr != "" {
		spoolID, err = strconv.Atoi(spoolIDStr)
		if err != nil {
			c.HTML(http.StatusBadRequest, "nfc_error.html", gin.H{
				"Error": "Invalid spool ID",
			})
			return
		}
	}

	var locationName string
	var isPrinterLocation bool

	if locationStr != "" {
		printerName, toolheadID, locationName, isPrinterLocation, err = ws.bridge.parseLocationParam(locationStr)
		if err != nil {
			c.HTML(http.StatusBadRequest, "nfc_error.html", gin.H{
				"Error": err.Error(),
			})
			return
		}
	}

	// Create or update session
	session, err := ws.bridge.createOrUpdateSession(sessionID, spoolID, printerName, toolheadID, locationName, isPrinterLocation)
	if err != nil {
		c.HTML(http.StatusInternalServerError, "nfc_error.html", gin.H{
			"Error": "Failed to create session: " + err.Error(),
		})
		return
	}

	// Check if session is complete
	if session.isSessionComplete() {
		// Complete the assignment
		err = ws.bridge.AssignSpoolToLocation(session.SpoolID, session.PrinterName, session.ToolheadID, session.LocationName, session.IsPrinterLocation)
		if err != nil {
			c.HTML(http.StatusInternalServerError, "nfc_error.html", gin.H{
				"Error": "Assignment failed: " + err.Error(),
			})
			return
		}

		// Broadcast update to all connected clients
		ws.BroadcastStatus()

		// Clean up session
		ws.bridge.deleteSession(sessionID)

		// Show success page
		c.HTML(http.StatusOK, "nfc_success.html", gin.H{
			"SpoolID":           session.SpoolID,
			"PrinterName":       session.PrinterName,
			"ToolheadID":        session.ToolheadID,
			"IsPrinterLocation": session.IsPrinterLocation,
			"LocationName":      session.LocationName,
		})
		return
	}

	// Session not complete, show progress
	var message string
	if session.HasSpool && !session.HasLocation {
		message = fmt.Sprintf("Spool %d selected. Now scan a location tag.", session.SpoolID)
	} else if session.HasLocation && !session.HasSpool {
		if session.IsPrinterLocation {
			message = fmt.Sprintf("Location %s - Toolhead %d selected. Now scan a spool tag.", session.PrinterName, session.ToolheadID)
		} else {
			message = fmt.Sprintf("Location '%s' selected. Now scan a spool tag.", session.LocationName)
		}
	} else {
		message = "Session started. Scan a spool or location tag."
	}

	c.HTML(http.StatusOK, "nfc_progress.html", gin.H{
		"Message":     message,
		"SessionID":   sessionID,
		"HasSpool":    session.HasSpool,
		"HasLocation": session.HasLocation,
	})
}

// nfcUrlsHandler returns all available NFC URLs with QR codes
func (ws *WebServer) nfcUrlsHandler(c *gin.Context) {
	var urls []gin.H

	// Get all spools
	spools, err := ws.bridge.spoolman.GetAllSpools()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Generate spool URLs
	for _, spool := range spools {
		url := fmt.Sprintf("http://%s/api/nfc/assign?spool=%d", c.Request.Host, spool.ID)

		// Safely get color hex
		colorHex := ""
		if spool.Filament != nil && spool.Filament.ColorHex != "" {
			colorHex = spool.Filament.ColorHex
			// Ensure it starts with #
			if !strings.HasPrefix(colorHex, "#") {
				colorHex = "#" + colorHex
			}
		}

		// Generate QR code
		qrCode, err := qrcode.Encode(url, qrcode.Medium, 256)
		if err != nil {
			log.Printf("Error generating QR code for spool %d: %v", spool.ID, err)
			// Continue without QR code if generation fails
			urls = append(urls, gin.H{
				"type":             "spool",
				"spool_id":         spool.ID,
				"spool_name":       spool.Name,
				"material":         spool.Material,
				"brand":            spool.Brand,
				"color_hex":        colorHex,
				"remaining_weight": spool.RemainingWeight,
				"url":              url,
				"qr_code_base64":   "",
			})
			continue
		}

		qrCodeBase64 := base64.StdEncoding.EncodeToString(qrCode)
		urls = append(urls, gin.H{
			"type":             "spool",
			"spool_id":         spool.ID,
			"spool_name":       spool.Name,
			"material":         spool.Material,
			"brand":            spool.Brand,
			"color_hex":        colorHex,
			"remaining_weight": spool.RemainingWeight,
			"url":              url,
			"qr_code_base64":   qrCodeBase64,
		})
	}

	// Get all filaments
	filaments, err := ws.bridge.spoolman.GetAllFilaments()
	if err != nil {
		log.Printf("Warning: Failed to get filaments for NFC URLs: %v", err)
		filaments = []SpoolmanFilament{}
	}

	// Generate filament URLs
	for _, filament := range filaments {
		url := fmt.Sprintf("%s/filament/show/%d", ws.bridge.config.SpoolmanURL, filament.ID)

		// Safely get color hex
		colorHex := ""
		if filament.ColorHex != "" {
			colorHex = filament.ColorHex
			// Ensure it starts with #
			if !strings.HasPrefix(colorHex, "#") {
				colorHex = "#" + colorHex
			}
		}

		// Get brand name
		brand := "Unknown Brand"
		if filament.Vendor != nil {
			brand = filament.Vendor.Name
		}

		// Generate QR code
		qrCode, err := qrcode.Encode(url, qrcode.Medium, 256)
		if err != nil {
			log.Printf("Error generating QR code for filament %d: %v", filament.ID, err)
			// Continue without QR code if generation fails
			urls = append(urls, gin.H{
				"type":           "filament",
				"filament_id":    filament.ID,
				"filament_name":  filament.Name,
				"material":       filament.Material,
				"brand":          brand,
				"color_hex":      colorHex,
				"extruder_temp":  filament.SettingsExtruderTemp,
				"bed_temp":       filament.SettingsBedTemp,
				"diameter":       filament.Diameter,
				"density":        filament.Density,
				"url":            url,
				"qr_code_base64": "",
			})
			continue
		}

		qrCodeBase64 := base64.StdEncoding.EncodeToString(qrCode)
		urls = append(urls, gin.H{
			"type":           "filament",
			"filament_id":    filament.ID,
			"filament_name":  filament.Name,
			"material":       filament.Material,
			"brand":          brand,
			"color_hex":      colorHex,
			"extruder_temp":  filament.SettingsExtruderTemp,
			"bed_temp":       filament.SettingsBedTemp,
			"diameter":       filament.Diameter,
			"density":        filament.Density,
			"url":            url,
			"qr_code_base64": qrCodeBase64,
		})
	}

	// Get Spoolman locations
	spoolmanLocations, err := ws.bridge.spoolman.GetLocations()
	if err != nil {
		log.Printf("Warning: Failed to get Spoolman locations: %v", err)
		spoolmanLocations = []SpoolmanLocation{}
	}

	// Get printer configurations to build a map of printer toolhead location names
	printerConfigs, err := ws.bridge.GetAllPrinterConfigs()
	if err != nil {
		log.Printf("Warning: Failed to get printer configurations: %v", err)
		printerConfigs = make(map[string]PrinterConfig)
	}

	printerLocationNames := make(map[string]bool)
	for printerID, printerConfig := range printerConfigs {
		toolheadNames, err := ws.bridge.GetAllToolheadNames(printerID)
		if err != nil {
			toolheadNames = make(map[int]string)
		}
		for toolheadID := 0; toolheadID < printerConfig.Toolheads; toolheadID++ {
			var displayName string
			if name, exists := toolheadNames[toolheadID]; exists {
				displayName = name
			} else {
				displayName = fmt.Sprintf("Toolhead %d", toolheadID)
			}
			locationName := fmt.Sprintf("%s - %s", printerConfig.Name, displayName)
			printerLocationNames[locationName] = true
		}
	}

	// Generate location URLs for Spoolman locations (excluding printer toolhead locations)
	for _, location := range spoolmanLocations {
		// Skip archived locations
		if location.Archived {
			continue
		}

		// Skip printer toolhead locations (they're added separately below)
		if printerLocationNames[location.Name] {
			continue
		}

		locationParam := location.Name
		url := fmt.Sprintf("http://%s/api/nfc/assign?location=%s", c.Request.Host, locationParam)

		// Generate QR code
		qrCode, err := qrcode.Encode(url, qrcode.Medium, 256)
		if err != nil {
			log.Printf("Error generating QR code for Spoolman location %s: %v", locationParam, err)
			// Continue without QR code if generation fails
			urls = append(urls, gin.H{
				"type":           "location",
				"location_type":  "storage",
				"location_name":  location.Name,
				"display_name":   location.Name,
				"url":            url,
				"qr_code_base64": "",
			})
			continue
		}

		qrCodeBase64 := base64.StdEncoding.EncodeToString(qrCode)
		urls = append(urls, gin.H{
			"type":           "location",
			"location_type":  "storage",
			"location_name":  location.Name,
			"display_name":   location.Name,
			"url":            url,
			"qr_code_base64": qrCodeBase64,
		})
	}

	// Generate virtual printer toolhead locations (these will be created in Spoolman on-demand)
	for printerID, printerConfig := range printerConfigs {
		// Get toolhead names for this printer
		toolheadNames, err := ws.bridge.GetAllToolheadNames(printerID)
		if err != nil {
			log.Printf("Warning: Failed to get toolhead names for printer %s: %v", printerID, err)
			toolheadNames = make(map[int]string)
		}

		for toolheadID := 0; toolheadID < printerConfig.Toolheads; toolheadID++ {
			// Get display name (custom or default)
			var displayName string
			if name, exists := toolheadNames[toolheadID]; exists {
				displayName = name
			} else {
				displayName = fmt.Sprintf("Toolhead %d", toolheadID)
			}

			locationParam := fmt.Sprintf("%s - %s", printerConfig.Name, displayName)
			url := fmt.Sprintf("http://%s/api/nfc/assign?location=%s", c.Request.Host, locationParam)

			// Generate QR code
			qrCode, err := qrcode.Encode(url, qrcode.Medium, 256)
			if err != nil {
				log.Printf("Error generating QR code for printer location %s: %v", locationParam, err)
				// Continue without QR code if generation fails
				urls = append(urls, gin.H{
					"type":           "location",
					"location_type":  "printer",
					"location_name":  locationParam,
					"display_name":   locationParam,
					"printer_name":   printerConfig.Name,
					"toolhead_id":    toolheadID,
					"url":            url,
					"qr_code_base64": "",
				})
				continue
			}

			qrCodeBase64 := base64.StdEncoding.EncodeToString(qrCode)
			urls = append(urls, gin.H{
				"type":           "location",
				"location_type":  "printer",
				"location_name":  locationParam,
				"display_name":   locationParam,
				"printer_name":   printerConfig.Name,
				"toolhead_id":    toolheadID,
				"url":            url,
				"qr_code_base64": qrCodeBase64,
			})
		}
	}

	// Sort URLs: filaments first, then spools, then locations alphabetically by display name
	sort.Slice(urls, func(i, j int) bool {
		typeI := urls[i]["type"].(string)
		typeJ := urls[j]["type"].(string)

		// Filaments come first, then spools, then locations
		if typeI != typeJ {
			if typeI == "filament" {
				return true
			}
			if typeJ == "filament" {
				return false
			}
			if typeI == "spool" {
				return true
			}
			if typeJ == "spool" {
				return false
			}
			// Both are locations
			return true
		}

		// Both are the same type - apply appropriate sorting
		if typeI == "location" {
			// Locations: sort by display name (case-insensitive)
			displayNameI := urls[i]["display_name"].(string)
			displayNameJ := urls[j]["display_name"].(string)
			return strings.ToLower(displayNameI) < strings.ToLower(displayNameJ)
		}

		if typeI == "filament" {
			// Filaments: sort by ID (same as GetAllFilaments)
			idI := urls[i]["filament_id"].(int)
			idJ := urls[j]["filament_id"].(int)
			return idI < idJ
		}

		if typeI == "spool" {
			// Spools: sort by display name (Material - Brand - Name), then by remaining weight
			// This matches the sorting logic in GetAllSpools()
			materialI := urls[i]["material"].(string)
			materialJ := urls[j]["material"].(string)
			brandI := urls[i]["brand"].(string)
			brandJ := urls[j]["brand"].(string)
			nameI := urls[i]["spool_name"].(string)
			nameJ := urls[j]["spool_name"].(string)

			// Create display names for comparison (same as getSpoolDisplayName())
			displayNameI := fmt.Sprintf("%s - %s - %s", materialI, brandI, nameI)
			displayNameJ := fmt.Sprintf("%s - %s - %s", materialJ, brandJ, nameJ)

			if displayNameI != displayNameJ {
				return displayNameI < displayNameJ
			}

			// If display names are the same, sort by remaining weight (ascending - use less filament first)
			weightI := urls[i]["remaining_weight"].(float64)
			weightJ := urls[j]["remaining_weight"].(float64)
			return weightI < weightJ
		}

		return false
	})

	c.JSON(http.StatusOK, gin.H{"urls": urls})
}

// nfcSessionStatusHandler returns the current session status
func (ws *WebServer) nfcSessionStatusHandler(c *gin.Context) {
	clientIP := getClientIP(c.ClientIP())
	sessionID := generateSessionID(clientIP)

	session, err := ws.bridge.getSession(sessionID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"active": false,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"active":              true,
		"session_id":          session.SessionID,
		"has_spool":           session.HasSpool,
		"has_location":        session.HasLocation,
		"spool_id":            session.SpoolID,
		"printer_name":        session.PrinterName,
		"toolhead_id":         session.ToolheadID,
		"location_name":       session.LocationName,
		"is_printer_location": session.IsPrinterLocation,
		"expires_at":          session.ExpiresAt,
	})
}

// Location Management Handlers

// getLocationsHandler returns all Spoolman locations plus virtual printer toolheads
func (ws *WebServer) getLocationsHandler(c *gin.Context) {
	// Get Spoolman locations
	spoolmanLocations, err := ws.bridge.spoolman.GetLocations()
	if err != nil {
		log.Printf("Warning: Failed to get Spoolman locations: %v", err)
		spoolmanLocations = []SpoolmanLocation{}
	}

	// Get printer configurations for virtual toolhead locations
	printerConfigs, err := ws.bridge.GetAllPrinterConfigs()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Create a map of printer toolhead location names for quick lookup
	printerLocationNames := make(map[string]bool)
	for printerID, printerConfig := range printerConfigs {
		// Get toolhead names for this printer
		toolheadNames, err := ws.bridge.GetAllToolheadNames(printerID)
		if err != nil {
			log.Printf("Warning: Failed to get toolhead names for printer %s: %v", printerID, err)
			toolheadNames = make(map[int]string)
		}

		for toolheadID := 0; toolheadID < printerConfig.Toolheads; toolheadID++ {
			// Get display name (custom or default)
			var displayName string
			if name, exists := toolheadNames[toolheadID]; exists {
				displayName = name
			} else {
				displayName = fmt.Sprintf("Toolhead %d", toolheadID)
			}

			locationName := fmt.Sprintf("%s - %s", printerConfig.Name, displayName)
			printerLocationNames[locationName] = true
		}
	}

	// Create virtual printer toolhead locations
	var allLocations []gin.H
	for printerID, printerConfig := range printerConfigs {
		// Get toolhead names for this printer
		toolheadNames, err := ws.bridge.GetAllToolheadNames(printerID)
		if err != nil {
			log.Printf("Warning: Failed to get toolhead names for printer %s: %v", printerID, err)
			toolheadNames = make(map[int]string)
		}

		for toolheadID := 0; toolheadID < printerConfig.Toolheads; toolheadID++ {
			// Get display name (custom or default)
			var displayName string
			if name, exists := toolheadNames[toolheadID]; exists {
				displayName = name
			} else {
				displayName = fmt.Sprintf("Toolhead %d", toolheadID)
			}

			locationName := fmt.Sprintf("%s - %s", printerConfig.Name, displayName)
			allLocations = append(allLocations, gin.H{
				"id":           fmt.Sprintf("printer_%s_%d", printerConfig.Name, toolheadID),
				"name":         locationName,
				"type":         "printer",
				"printer_name": printerConfig.Name,
				"toolhead_id":  toolheadID,
				"is_virtual":   true,
			})
		}
	}

	// Add Spoolman locations (excluding printer toolhead locations which are already added above)
	for _, loc := range spoolmanLocations {
		// Skip archived locations
		if loc.Archived {
			continue
		}

		// Skip if this is a printer toolhead location (already added above)
		if printerLocationNames[loc.Name] {
			continue
		}

		allLocations = append(allLocations, gin.H{
			"name":       loc.Name,
			"type":       "storage",
			"is_virtual": false,
		})
	}

	c.JSON(http.StatusOK, gin.H{"locations": allLocations})
}

// getLocationStatusHandler returns detailed status information for a specific location
func (ws *WebServer) getLocationStatusHandler(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Location name is required"})
		return
	}

	// Check if location exists in Spoolman
	location, err := ws.bridge.spoolman.FindLocationByName(name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if location == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Location not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"name":     location.Name,
		"id":       location.ID,
		"comment":  location.Comment,
		"archived": location.Archived,
	})
}

// createLocationHandler creates a new location in Spoolman
func (ws *WebServer) createLocationHandler(c *gin.Context) {
	var req struct {
		Name        string `json:"name" binding:"required"`
		Type        string `json:"type"`
		PrinterName string `json:"printer_name,omitempty"`
		ToolheadID  int    `json:"toolhead_id,omitempty"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("createLocationHandler: bad request: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	log.Printf("createLocationHandler: creating location name='%s' in Spoolman", req.Name)
	location, err := ws.bridge.spoolman.GetOrCreateLocation(req.Name)
	if err != nil {
		log.Printf("createLocationHandler: failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"name":     location.Name,
		"id":       location.ID,
		"comment":  location.Comment,
		"archived": location.Archived,
	})
}

// updateLocationHandler updates a location in Spoolman
func (ws *WebServer) updateLocationHandler(c *gin.Context) {
	oldName := c.Param("name")
	if oldName == "" {
		log.Printf("updateLocationHandler: missing location name")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Location name is required"})
		return
	}

	var req struct {
		Name string `json:"name" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("updateLocationHandler: bad request for name='%s': %v", oldName, err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	log.Printf("updateLocationHandler: renaming '%s' to '%s' in Spoolman", oldName, req.Name)
	if err := ws.bridge.spoolman.UpdateLocationByName(oldName, req.Name); err != nil {
		log.Printf("updateLocationHandler: failed for name='%s': %v", oldName, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Get updated location
	location, err := ws.bridge.spoolman.FindLocationByName(req.Name)
	if err != nil {
		log.Printf("Warning: Could not get updated location '%s': %v", req.Name, err)
		c.JSON(http.StatusOK, gin.H{"message": "Location updated successfully"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Location updated successfully",
		"location": gin.H{
			"name":     location.Name,
			"id":       location.ID,
			"comment":  location.Comment,
			"archived": location.Archived,
		},
	})
}

// deleteLocationHandler archives a location in Spoolman (locations are archived, not deleted)
func (ws *WebServer) deleteLocationHandler(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		log.Printf("deleteLocationHandler: missing location name")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Location name is required"})
		return
	}

	// Find location by name
	location, err := ws.bridge.spoolman.FindLocationByName(name)
	if err != nil {
		log.Printf("deleteLocationHandler: error finding location '%s': %v", name, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if location == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Location not found"})
		return
	}

	// Archive the location (Spoolman doesn't support deletion, only archiving)
	log.Printf("deleteLocationHandler: archiving location '%s' (ID: %d)", name, location.ID)
	if err := ws.bridge.spoolman.ArchiveLocation(location.ID); err != nil {
		log.Printf("deleteLocationHandler: failed to archive location '%s': %v", name, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to archive location"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Location archived successfully"})
}
