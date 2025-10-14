package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

//go:embed templates/*
var templatesFS embed.FS

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

	// Static files
	ws.router.Static("/static", "./static")

	// Main dashboard
	ws.router.GET("/", ws.dashboardHandler)

	// API routes
	api := ws.router.Group("/api")
	{
		api.GET("/status", ws.statusHandler)
		api.GET("/spools", ws.spoolsHandler)
		api.POST("/map_toolhead", ws.mapToolheadHandler)
		api.GET("/available_spools", ws.availableSpoolsHandler)
		api.GET("/spoolman/test", ws.testSpoolmanConnectionHandler)
		api.GET("/spoolman/debug", ws.debugSpoolmanHandler)
		api.POST("/test/print_complete", ws.testPrintCompleteHandler)
		api.GET("/config", ws.getConfigHandler)
		api.POST("/config", ws.updateConfigHandler)
		api.GET("/printers", ws.getPrintersHandler)
		api.POST("/printers", ws.addPrinterHandler)
		api.PUT("/printers/:id", ws.updatePrinterHandler)
		api.DELETE("/printers/:id", ws.deletePrinterHandler)
		api.POST("/detect_printer", ws.detectPrinterHandler)
		api.GET("/print-errors", ws.getPrintErrorsHandler)
		api.POST("/print-errors/:id/acknowledge", ws.acknowledgePrintErrorHandler)
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

// validatePrinterConfig validates printer configuration input
func validatePrinterConfig(config PrinterConfig) error {
	if config.Name == "" {
		return fmt.Errorf("printer name is required")
	}
	if config.IPAddress == "" {
		return fmt.Errorf("IP address is required")
	}
	if config.Toolheads < 1 {
		return fmt.Errorf("toolheads must be at least 1")
	}
	if config.Toolheads > 10 {
		return fmt.Errorf("toolheads cannot exceed 10")
	}
	return nil
}

// validateIPAddress validates IP address format
func validateIPAddress(ip string) error {
	if ip == "" {
		return fmt.Errorf("IP address cannot be empty")
	}
	// Basic IP validation - could be enhanced with proper regex
	if len(ip) < 7 || len(ip) > 15 {
		return fmt.Errorf("invalid IP address format")
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

// getPrintersHandler returns all configured printers
func (ws *WebServer) getPrintersHandler(c *gin.Context) {
	printerConfigs, err := ws.bridge.GetAllPrinterConfigs()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"printers": printerConfigs})
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

	// Validate IP address
	if err := validateIPAddress(printerConfig.IPAddress); err != nil {
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

	// Validate IP address
	if err := validateIPAddress(printerConfig.IPAddress); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Auto-detect model if IP address or API key changed, or if model is currently "Unknown"
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
			// Determine model based on hostname (same logic as detectPrinterHandler)
			hostname := strings.ToLower(printerInfo.Hostname)
			hostname = strings.TrimSpace(hostname)

			detectedModel := ModelUnknown
			if strings.Contains(hostname, ModelCorePattern) {
				detectedModel = ModelCoreOne
			} else if strings.Contains(hostname, ModelXLPattern) {
				detectedModel = ModelXL
			} else if strings.Contains(hostname, ModelMK4Pattern) {
				detectedModel = ModelMK4
			} else if strings.Contains(hostname, ModelMK3Pattern) {
				detectedModel = ModelMK35
			} else if strings.Contains(hostname, ModelMiniPattern) {
				detectedModel = ModelMiniPlus
			}

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

	// Validate IP address
	if err := validateIPAddress(req.IPAddress); err != nil {
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

	// Determine model based on hostname
	model := ModelUnknown
	hostname := strings.ToLower(printerInfo.Hostname)
	hostname = strings.TrimSpace(hostname) // Clean up any whitespace

	log.Printf("🔍 [Detection] Checking hostname '%s' against patterns:", hostname)

	if strings.Contains(hostname, ModelCorePattern) {
		model = ModelCoreOne
		log.Printf("✅ [Detection] Matched pattern '%s' -> %s", ModelCorePattern, model)
	} else if strings.Contains(hostname, ModelXLPattern) {
		model = ModelXL
		log.Printf("✅ [Detection] Matched pattern '%s' -> %s", ModelXLPattern, model)
	} else if strings.Contains(hostname, ModelMK4Pattern) {
		model = ModelMK4
		log.Printf("✅ [Detection] Matched pattern '%s' -> %s", ModelMK4Pattern, model)
	} else if strings.Contains(hostname, ModelMK3Pattern) {
		model = ModelMK35
		log.Printf("✅ [Detection] Matched pattern '%s' -> %s", ModelMK3Pattern, model)
	} else if strings.Contains(hostname, ModelMiniPattern) {
		model = ModelMiniPlus
		log.Printf("✅ [Detection] Matched pattern '%s' -> %s", ModelMiniPattern, model)
	} else {
		log.Printf("❌ [Detection] No pattern matched for hostname '%s'. Available patterns: %s, %s, %s, %s, %s",
			hostname, ModelCorePattern, ModelXLPattern, ModelMK4Pattern, ModelMK3Pattern, ModelMiniPattern)
	}

	log.Printf("🎯 [Detection] Final result: hostname='%s' -> model='%s'", printerInfo.Hostname, model)

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
