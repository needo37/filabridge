package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// PrusaLinkClient handles communication with PrusaLink API
type PrusaLinkClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// PrusaLinkStatus represents the status response from PrusaLink
type PrusaLinkStatus struct {
	Printer struct {
		State       string `json:"state"`
		Temperature struct {
			Bed struct {
				Actual float64 `json:"actual"`
				Target float64 `json:"target"`
			} `json:"bed"`
			Tool0 struct {
				Actual float64 `json:"actual"`
				Target float64 `json:"target"`
			} `json:"tool0"`
			Tool1 struct {
				Actual float64 `json:"actual"`
				Target float64 `json:"target"`
			} `json:"tool1,omitempty"`
			Tool2 struct {
				Actual float64 `json:"actual"`
				Target float64 `json:"target"`
			} `json:"tool2,omitempty"`
			Tool3 struct {
				Actual float64 `json:"actual"`
				Target float64 `json:"target"`
			} `json:"tool3,omitempty"`
			Tool4 struct {
				Actual float64 `json:"actual"`
				Target float64 `json:"target"`
			} `json:"tool4,omitempty"`
		} `json:"temperature"`
		Telemetry struct {
			PrintTime     int     `json:"print_time"`
			PrintTimeLeft int     `json:"print_time_left"`
			Progress      float64 `json:"progress"`
		} `json:"telemetry"`
	} `json:"printer"`
}

// PrusaLinkJob represents the job response from PrusaLink
type PrusaLinkJob struct {
	ID            int     `json:"id"`
	State         string  `json:"state"`
	Progress      float64 `json:"progress"`
	TimeRemaining int     `json:"time_remaining"`
	TimePrinting  int     `json:"time_printing"`
	File          struct {
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
		Path        string `json:"path"`
		Size        int    `json:"size"`
		Refs        struct {
			Download string `json:"download"`
		} `json:"refs"`
	} `json:"file"`
	// Filament usage data (if available)
	Filament []struct {
		ToolheadID int     `json:"toolhead_id"`
		Length     float64 `json:"length"`
		Weight     float64 `json:"weight"`
	} `json:"filament,omitempty"`
}

// PrusaLinkInfo represents the printer info response from PrusaLink
type PrusaLinkInfo struct {
	Hostname         string  `json:"hostname"`
	Serial           string  `json:"serial"`
	NozzleDiameter   float64 `json:"nozzle_diameter"`
	MMU              bool    `json:"mmu"`
	MinExtrusionTemp int     `json:"min_extrusion_temp"`
}

// NewPrusaLinkClient creates a new PrusaLink client
func NewPrusaLinkClient(ipAddress, apiKey string, timeout, fileDownloadTimeout int) *PrusaLinkClient {
	return &PrusaLinkClient{
		baseURL: fmt.Sprintf("http://%s", ipAddress),
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: time.Duration(timeout) * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				MaxIdleConnsPerHost: 2,
				IdleConnTimeout:     30 * time.Second,
			},
		},
	}
}

// addAPIKey adds API key authentication to the request
func (c *PrusaLinkClient) addAPIKey(req *http.Request) {
	if c.apiKey != "" {
		req.Header.Set("X-Api-Key", c.apiKey)
	}
}

// GetStatus retrieves the current status of the printer
func (c *PrusaLinkClient) GetStatus() (*PrusaLinkStatus, error) {
	req, err := http.NewRequest("GET", c.baseURL+"/api/v1/status", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create status request: %w", err)
	}

	// Add API key authentication
	c.addAPIKey(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get status from PrusaLink: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("PrusaLink API error: %d - %s", resp.StatusCode, string(body))
	}

	var status PrusaLinkStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("failed to decode status response: %w", err)
	}

	return &status, nil
}

// GetJobInfo retrieves the current job information
func (c *PrusaLinkClient) GetJobInfo() (*PrusaLinkJob, error) {
	req, err := http.NewRequest("GET", c.baseURL+"/api/v1/job", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create job request: %w", err)
	}

	// Add API key authentication
	c.addAPIKey(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get job info from PrusaLink: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("PrusaLink API error: %d - %s", resp.StatusCode, string(body))
	}

	// Handle 204 No Content (no active job)
	if resp.StatusCode == http.StatusNoContent {
		return &PrusaLinkJob{}, nil
	}

	var job PrusaLinkJob
	if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
		return nil, fmt.Errorf("failed to decode job response: %w", err)
	}

	return &job, nil
}

// GetPrinterInfo retrieves the printer information
func (c *PrusaLinkClient) GetPrinterInfo() (*PrusaLinkInfo, error) {
	log.Printf("🔍 [PrusaLink] Getting printer info from %s", c.baseURL)

	req, err := http.NewRequest("GET", c.baseURL+"/api/v1/info", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create printer info request: %w", err)
	}

	// Add API key authentication
	c.addAPIKey(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		log.Printf("❌ [PrusaLink] API call failed for %s: %v", c.baseURL, err)
		return nil, fmt.Errorf("failed to get printer info from PrusaLink: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("❌ [PrusaLink] API error for %s: %d - %s", c.baseURL, resp.StatusCode, string(body))
		return nil, fmt.Errorf("PrusaLink API error: %d - %s", resp.StatusCode, string(body))
	}

	// Read the raw response body for logging
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("❌ [PrusaLink] Failed to read response body from %s: %v", c.baseURL, err)
		return nil, fmt.Errorf("failed to read printer info response: %w", err)
	}

	log.Printf("📥 [PrusaLink] Raw API response from %s: %s", c.baseURL, string(body))

	var info PrusaLinkInfo
	if err := json.Unmarshal(body, &info); err != nil {
		log.Printf("❌ [PrusaLink] JSON unmarshal failed for %s: %v", c.baseURL, err)
		return nil, fmt.Errorf("failed to decode printer info response: %w", err)
	}

	log.Printf("✅ [PrusaLink] Parsed printer info from %s: hostname='%s', serial='%s', nozzle_diameter=%.2f, mmu=%v",
		c.baseURL, info.Hostname, info.Serial, info.NozzleDiameter, info.MMU)

	return &info, nil
}

// GetGcodeFile downloads the G-code file for a completed print job
func (c *PrusaLinkClient) GetGcodeFile(filename string) ([]byte, error) {
	// Use the correct PrusaLink API format: /{filename}
	// The filename should already include the full path (e.g., "usb/SHAPE-~1.BGC")
	req, err := http.NewRequest("GET", c.baseURL+"/"+filename, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create G-code request: %w", err)
	}

	// Add API key authentication
	c.addAPIKey(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get G-code file from PrusaLink: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("PrusaLink API error: %d - %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read G-code file: %w", err)
	}

	return body, nil
}

// GetGcodeFileWithRetry downloads the G-code file with retry logic and exponential backoff
func (c *PrusaLinkClient) GetGcodeFileWithRetry(filename string, fileDownloadTimeout int) ([]byte, error) {
	const maxRetries = 3
	backoffDelays := []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second}

	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		log.Printf("Downloading G-code file attempt %d/%d: %s", attempt+1, maxRetries, filename)

		// Create a new client with extended timeout for file downloads
		fileClient := &http.Client{
			Timeout: time.Duration(fileDownloadTimeout) * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:          10,
				MaxIdleConnsPerHost:   2,
				IdleConnTimeout:       90 * time.Second,
				ResponseHeaderTimeout: 30 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
			},
		}

		// Add diagnostic logging to verify timeout values
		log.Printf("File download client configured with %v timeout", fileClient.Timeout)

		// Use the correct PrusaLink API format: /{filename}
		req, err := http.NewRequest("GET", c.baseURL+"/"+filename, nil)
		if err != nil {
			lastErr = fmt.Errorf("failed to create G-code request: %w", err)
			log.Printf("Attempt %d failed: %v", attempt+1, lastErr)
			if attempt < maxRetries-1 {
				time.Sleep(backoffDelays[attempt])
			}
			continue
		}

		// Add API key authentication
		c.addAPIKey(req)

		resp, err := fileClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("failed to get G-code file from PrusaLink: %w", err)
			log.Printf("Attempt %d failed: %v", attempt+1, lastErr)
			if attempt < maxRetries-1 {
				time.Sleep(backoffDelays[attempt])
			}
			continue
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			lastErr = fmt.Errorf("PrusaLink API error: %d - %s", resp.StatusCode, string(body))
			log.Printf("Attempt %d failed: %v", attempt+1, lastErr)
			if attempt < maxRetries-1 {
				time.Sleep(backoffDelays[attempt])
			}
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("failed to read G-code file: %w", err)
			log.Printf("Attempt %d failed: %v", attempt+1, lastErr)
			if attempt < maxRetries-1 {
				time.Sleep(backoffDelays[attempt])
			}
			continue
		}

		// Success!
		log.Printf("Successfully downloaded G-code file on attempt %d: %s (%d bytes)",
			attempt+1, filename, len(body))
		return body, nil
	}

	return nil, fmt.Errorf("failed to download G-code file after %d attempts: %w", maxRetries, lastErr)
}

// ParseGcodeFilamentUsage extracts filament usage from .bgcode content
func (c *PrusaLinkClient) ParseGcodeFilamentUsage(gcodeContent []byte) (map[int]float64, error) {
	content := string(gcodeContent)
	filamentUsage := make(map[int]float64)

	// Parse .bgcode format (binary format with embedded text)
	// Look for "filament used [g]=" pattern which gives exact weights per toolhead
	bgcodeRegex := regexp.MustCompile(`filament used \[g\]=([0-9.,\s]+)`)
	bgcodeMatch := bgcodeRegex.FindStringSubmatch(content)

	if len(bgcodeMatch) >= 2 {
		// Parse the comma-separated values for each toolhead
		weightsStr := bgcodeMatch[1]
		weights := strings.Split(weightsStr, ",")

		for i, weightStr := range weights {
			weightStr = strings.TrimSpace(weightStr)
			if weight, err := strconv.ParseFloat(weightStr, 64); err == nil && weight > 0 {
				filamentUsage[i] = weight
			}
		}

		if len(filamentUsage) > 0 {
			return filamentUsage, nil
		}
	}

	// If no .bgcode data found, return empty usage (no fallback needed)
	// .bgcode files contain all the precise filament usage data we need

	return filamentUsage, nil
}

// TestConnection tests the connection to PrusaLink
func (c *PrusaLinkClient) TestConnection() error {
	_, err := c.GetStatus()
	return err
}
