package main

import (
	"encoding/json"
	"fmt"
	"io"
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
func NewPrusaLinkClient(ipAddress, apiKey string) *PrusaLinkClient {
	return &PrusaLinkClient{
		baseURL: fmt.Sprintf("http://%s", ipAddress),
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
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
	req, err := http.NewRequest("GET", c.baseURL+"/api/v1/info", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create printer info request: %w", err)
	}

	// Add API key authentication
	c.addAPIKey(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get printer info from PrusaLink: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("PrusaLink API error: %d - %s", resp.StatusCode, string(body))
	}

	var info PrusaLinkInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("failed to decode printer info response: %w", err)
	}

	return &info, nil
}

// GetToolheadCount determines the number of toolheads by examining the raw JSON response
func (c *PrusaLinkClient) GetToolheadCount() (int, error) {
	// Try the /api/printer endpoint first as it may have more detailed tool information
	req, err := http.NewRequest("GET", c.baseURL+"/api/printer", nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create printer request: %w", err)
	}

	// Add API key authentication
	c.addAPIKey(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to get status from PrusaLink: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("PrusaLink API error: %d - %s", resp.StatusCode, string(body))
	}

	// Read the raw response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read response body: %w", err)
	}

	// Parse as generic JSON to count tool fields
	var rawResponse map[string]interface{}
	if err := json.Unmarshal(body, &rawResponse); err != nil {
		return 0, fmt.Errorf("failed to parse JSON response: %w", err)
	}

	// Look for temperature section with individual tool data
	temperature, ok := rawResponse["temperature"].(map[string]interface{})
	if !ok {
		// Fallback to printer section if temperature not found
		printer, ok := rawResponse["printer"].(map[string]interface{})
		if !ok {
			return 1, nil // Default to 1 if structure is unexpected
		}
		temperature = printer
	}

	// Count toolheads by looking for tool0, tool1, tool2, etc. in temperature data
	toolheadCount := 0
	toolRegex := regexp.MustCompile(`^tool\d+$`)

	for key := range temperature {
		if toolRegex.MatchString(key) {
			toolheadCount++
		}
	}

	// Ensure we have at least 1 toolhead (tool0 should always be present)
	if toolheadCount == 0 {
		toolheadCount = 1
	}

	return toolheadCount, nil
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
