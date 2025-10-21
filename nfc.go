package main

import (
	"crypto/md5"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"time"
)

// NFCSession represents an active NFC scanning session
type NFCSession struct {
	SessionID         string    `json:"session_id"`
	SpoolID           int       `json:"spool_id"`
	PrinterName       string    `json:"printer_name"`
	ToolheadID        int       `json:"toolhead_id"`
	LocationName      string    `json:"location_name"`
	IsPrinterLocation bool      `json:"is_printer_location"`
	CreatedAt         time.Time `json:"created_at"`
	ExpiresAt         time.Time `json:"expires_at"`
	HasSpool          bool      `json:"has_spool"`
	HasLocation       bool      `json:"has_location"`
}

// parseLocationParam extracts location information from location parameter
// Supports two formats:
// 1. "PrinterName - Toolhead N" - printer toolhead locations
// 2. "LocationName" - non-printer locations (drybox, storage, etc.)
func parseLocationParam(location string) (printerName string, toolheadID int, locationName string, isPrinterLocation bool, err error) {
	// Check if it's a printer toolhead location (format: "PrinterName - Toolhead N")
	if strings.Contains(location, " - Toolhead ") {
		parts := strings.Split(location, " - Toolhead ")
		if len(parts) == 2 {
			printerName = strings.TrimSpace(parts[0])
			toolheadIDStr := strings.TrimSpace(parts[1])
			toolheadID, err = strconv.Atoi(toolheadIDStr)
			if err != nil {
				return "", 0, "", false, fmt.Errorf("invalid toolhead ID in location '%s': %w", location, err)
			}
			return printerName, toolheadID, location, true, nil
		}
	}

	// For all other cases, treat as a location name
	return "", 0, location, false, nil
}

// generateSessionID creates a unique session ID based on client IP only
// This ensures all scans from the same device use the same session
func generateSessionID(clientIP string) string {
	hash := md5.Sum([]byte(clientIP))
	return fmt.Sprintf("%x", hash)[:16] // Use first 16 characters of MD5
}

// getClientIP extracts the real client IP from the request
func getClientIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		// If SplitHostPort fails, assume the whole string is the IP
		return remoteAddr
	}
	return host
}

// createOrUpdateSession creates a new session or updates an existing one
func (b *FilamentBridge) createOrUpdateSession(sessionID string, spoolID int, printerName string, toolheadID int, locationName string, isPrinterLocation bool) (*NFCSession, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	// Check if session already exists
	var existingSession NFCSession
	err := b.db.QueryRow(
		"SELECT session_id, spool_id, printer_name, toolhead_id, location_name, is_printer_location, created_at, expires_at FROM nfc_sessions WHERE session_id = ?",
		sessionID,
	).Scan(&existingSession.SessionID, &existingSession.SpoolID, &existingSession.PrinterName,
		&existingSession.ToolheadID, &existingSession.LocationName, &existingSession.IsPrinterLocation, &existingSession.CreatedAt, &existingSession.ExpiresAt)

	if err == nil {
		// Session exists, update it
		now := time.Now()
		if now.After(existingSession.ExpiresAt) {
			// Session expired, create new one
			return b.createNewSession(sessionID, spoolID, printerName, toolheadID, locationName, isPrinterLocation)
		}

		// Update existing session - only update fields that are actually being set
		// This prevents overwriting existing data when scanning tags in sequence

		// Update spool data only if a new spool is being scanned
		if spoolID > 0 {
			existingSession.SpoolID = spoolID
			existingSession.HasSpool = true

			// Update only spool_id in database, preserve other fields
			_, err = b.db.Exec(
				"UPDATE nfc_sessions SET spool_id = ? WHERE session_id = ?",
				spoolID, sessionID,
			)
			if err != nil {
				return nil, fmt.Errorf("failed to update spool in NFC session: %w", err)
			}
		}

		// Update location data only if a new location is being scanned
		if (isPrinterLocation && printerName != "" && toolheadID >= 0) || (!isPrinterLocation && locationName != "") {
			existingSession.PrinterName = printerName
			existingSession.ToolheadID = toolheadID
			existingSession.LocationName = locationName
			existingSession.IsPrinterLocation = isPrinterLocation
			existingSession.HasLocation = true

			// Update only location fields in database, preserve spool_id
			_, err = b.db.Exec(
				"UPDATE nfc_sessions SET printer_name = ?, toolhead_id = ?, location_name = ?, is_printer_location = ? WHERE session_id = ?",
				printerName, toolheadID, locationName, isPrinterLocation, sessionID,
			)
			if err != nil {
				return nil, fmt.Errorf("failed to update location in NFC session: %w", err)
			}
		}

		// Recalculate flags based on current session data
		existingSession.HasSpool = existingSession.SpoolID > 0
		existingSession.HasLocation = (existingSession.IsPrinterLocation && existingSession.PrinterName != "" && existingSession.ToolheadID >= 0) ||
			(!existingSession.IsPrinterLocation && existingSession.LocationName != "")

		return &existingSession, nil
	}

	// Create new session
	return b.createNewSession(sessionID, spoolID, printerName, toolheadID, locationName, isPrinterLocation)
}

// createNewSession creates a new NFC session
func (b *FilamentBridge) createNewSession(sessionID string, spoolID int, printerName string, toolheadID int, locationName string, isPrinterLocation bool) (*NFCSession, error) {
	now := time.Now()
	expiresAt := now.Add(5 * time.Minute) // 5 minute expiration

	session := &NFCSession{
		SessionID:         sessionID,
		SpoolID:           spoolID,
		PrinterName:       printerName,
		ToolheadID:        toolheadID,
		LocationName:      locationName,
		IsPrinterLocation: isPrinterLocation,
		CreatedAt:         now,
		ExpiresAt:         expiresAt,
		HasSpool:          spoolID > 0,
		HasLocation:       (isPrinterLocation && printerName != "" && toolheadID >= 0) || (!isPrinterLocation && locationName != ""),
	}

	_, err := b.db.Exec(
		"INSERT INTO nfc_sessions (session_id, spool_id, printer_name, toolhead_id, location_name, is_printer_location, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		session.SessionID, session.SpoolID, session.PrinterName, session.ToolheadID, session.LocationName, session.IsPrinterLocation, session.CreatedAt, session.ExpiresAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create NFC session: %w", err)
	}

	return session, nil
}

// getSession retrieves an existing NFC session
func (b *FilamentBridge) getSession(sessionID string) (*NFCSession, error) {
	var session NFCSession
	err := b.db.QueryRow(
		"SELECT session_id, spool_id, printer_name, toolhead_id, location_name, is_printer_location, created_at, expires_at FROM nfc_sessions WHERE session_id = ?",
		sessionID,
	).Scan(&session.SessionID, &session.SpoolID, &session.PrinterName,
		&session.ToolheadID, &session.LocationName, &session.IsPrinterLocation, &session.CreatedAt, &session.ExpiresAt)

	if err != nil {
		return nil, err
	}

	// Check if session is expired
	if time.Now().After(session.ExpiresAt) {
		// Clean up expired session
		b.deleteSession(sessionID)
		return nil, fmt.Errorf("session expired")
	}

	// Set flags based on data
	session.HasSpool = session.SpoolID > 0
	session.HasLocation = (session.IsPrinterLocation && session.PrinterName != "" && session.ToolheadID >= 0) || (!session.IsPrinterLocation && session.LocationName != "")

	return &session, nil
}

// isSessionComplete checks if both spool and location are set
func (s *NFCSession) isSessionComplete() bool {
	return s.HasSpool && s.HasLocation
}

// deleteSession removes a session from the database
func (b *FilamentBridge) deleteSession(sessionID string) error {
	_, err := b.db.Exec("DELETE FROM nfc_sessions WHERE session_id = ?", sessionID)
	return err
}

// cleanupExpiredSessions removes sessions older than their expiration time
func (b *FilamentBridge) cleanupExpiredSessions() error {
	now := time.Now()
	_, err := b.db.Exec("DELETE FROM nfc_sessions WHERE expires_at < ?", now)
	if err != nil {
		log.Printf("Error cleaning up expired NFC sessions: %v", err)
		return err
	}
	return nil
}

// AssignSpoolToLocation assigns a spool to a location and updates Spoolman
func (b *FilamentBridge) AssignSpoolToLocation(spoolID int, printerName string, toolheadID int, locationName string, isPrinterLocation bool) error {
	if isPrinterLocation {
		// This is a printer toolhead location
		// Update FilaBridge toolhead mapping
		if err := b.SetToolheadMapping(printerName, toolheadID, spoolID); err != nil {
			return fmt.Errorf("failed to set toolhead mapping: %w", err)
		}

		// Update Spoolman location using proper location entities
		locationName := fmt.Sprintf("%s - Toolhead %d", printerName, toolheadID)
		if err := b.spoolman.UpdateSpoolLocation(spoolID, locationName); err != nil {
			// If Spoolman update fails, we should still log it but not fail the entire operation
			// since the FilaBridge mapping is more critical
			log.Printf("Warning: Failed to update Spoolman location for spool %d: %v", spoolID, err)
		}

		log.Printf("Successfully assigned spool %d to %s toolhead %d", spoolID, printerName, toolheadID)
	} else {
		// This is a non-printer location (drybox, storage, etc.)
		// First, check if this spool is currently assigned to any toolhead and clear it
		if err := b.clearSpoolFromAllToolheads(spoolID); err != nil {
			log.Printf("Warning: Failed to clear spool %d from toolheads: %v", spoolID, err)
		}

		// Use the location name directly with Spoolman
		if locationName == "" {
			return fmt.Errorf("location name cannot be empty")
		}

		// Update Spoolman location
		if err := b.spoolman.UpdateSpoolLocation(spoolID, locationName); err != nil {
			return fmt.Errorf("failed to update Spoolman location for spool %d: %w", spoolID, err)
		}

		log.Printf("Successfully assigned spool %d to location '%s'", spoolID, locationName)
	}

	return nil
}

// clearSpoolFromAllToolheads removes a spool from all toolhead mappings
func (b *FilamentBridge) clearSpoolFromAllToolheads(spoolID int) error {
	// Get all current toolhead mappings
	allMappings, err := b.GetAllToolheadMappings()
	if err != nil {
		return fmt.Errorf("failed to get toolhead mappings: %w", err)
	}

	// Find and clear any mappings for this spool
	for printerName, printerMappings := range allMappings {
		for toolheadID, mapping := range printerMappings {
			if mapping.SpoolID == spoolID {
				// Clear this toolhead mapping
				if err := b.UnmapToolhead(printerName, toolheadID); err != nil {
					log.Printf("Warning: Failed to unmap spool %d from %s toolhead %d: %v", spoolID, printerName, toolheadID, err)
				} else {
					log.Printf("Cleared spool %d from %s toolhead %d", spoolID, printerName, toolheadID)
				}
			}
		}
	}

	return nil
}
