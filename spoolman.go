package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"
)

// SpoolmanClient handles communication with Spoolman API for bridge functionality
type SpoolmanClient struct {
	baseURL    string
	httpClient *http.Client
}

// SpoolmanSpool represents a spool from Spoolman API
type SpoolmanSpool struct {
	ID              int                    `json:"id"`
	Registered      string                 `json:"registered"`
	Filament        *SpoolmanFilament      `json:"filament"`
	RemainingWeight float64                `json:"remaining_weight"`
	InitialWeight   float64                `json:"initial_weight"`
	SpoolWeight     float64                `json:"spool_weight"`
	UsedWeight      float64                `json:"used_weight"`
	RemainingLength float64                `json:"remaining_length"`
	UsedLength      float64                `json:"used_length"`
	FirstUsed       string                 `json:"first_used"`
	LastUsed        string                 `json:"last_used"`
	Archived        bool                   `json:"archived"`
	Extra           map[string]interface{} `json:"extra"`

	// Computed fields for easier access
	Name     string `json:"name"`     // Computed from filament.name
	Brand    string `json:"brand"`    // Computed from filament.vendor.name
	Material string `json:"material"` // Computed from filament.material
}

// SpoolmanFilament represents a filament type from Spoolman
type SpoolmanFilament struct {
	ID                   int                    `json:"id"`
	Registered           string                 `json:"registered"`
	Name                 string                 `json:"name"`
	Vendor               *SpoolmanVendor        `json:"vendor"`
	Material             string                 `json:"material"`
	Density              float64                `json:"density"`
	Diameter             float64                `json:"diameter"`
	Weight               float64                `json:"weight"`
	SpoolWeight          float64                `json:"spool_weight"`
	SettingsExtruderTemp int                    `json:"settings_extruder_temp"`
	SettingsBedTemp      int                    `json:"settings_bed_temp"`
	ColorHex             string                 `json:"color_hex"`
	ExternalID           string                 `json:"external_id"`
	Extra                map[string]interface{} `json:"extra"`
	Archived             bool                   `json:"archived"`
}

// SpoolmanVendor represents a vendor from Spoolman
type SpoolmanVendor struct {
	ID         int                    `json:"id"`
	Registered string                 `json:"registered"`
	Name       string                 `json:"name"`
	ExternalID string                 `json:"external_id"`
	Extra      map[string]interface{} `json:"extra"`
	Archived   bool                   `json:"archived"`
}

// SpoolmanError represents an error response from Spoolman API
type SpoolmanError struct {
	Detail string `json:"detail"`
	Title  string `json:"title"`
	Type   string `json:"type"`
}

// NewSpoolmanClient creates a new Spoolman client
func NewSpoolmanClient(baseURL string, timeout int) *SpoolmanClient {
	return &SpoolmanClient{
		baseURL: baseURL,
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

// handleAPIError handles API error responses from Spoolman
func (c *SpoolmanClient) handleAPIError(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("error reading error response body: %w", err)
	}

	// Try to parse as Spoolman error format
	var spoolmanErr SpoolmanError
	if err := json.Unmarshal(body, &spoolmanErr); err == nil && spoolmanErr.Detail != "" {
		return fmt.Errorf("spoolman API error (HTTP %d): %s - %s", resp.StatusCode, spoolmanErr.Title, spoolmanErr.Detail)
	}

	// Fallback to generic error
	return fmt.Errorf("spoolman API error (HTTP %d): %s", resp.StatusCode, string(body))
}

// normalizeSpoolData normalizes spool data to extract information from nested structures
func (c *SpoolmanClient) normalizeSpoolData(spool SpoolmanSpool) SpoolmanSpool {
	// Extract data from nested filament and vendor structures
	if spool.Filament != nil {
		spool.Name = spool.Filament.Name
		spool.Material = spool.Filament.Material

		if spool.Filament.Vendor != nil {
			spool.Brand = spool.Filament.Vendor.Name
		}
	}

	// If name is still empty, create a default name
	if spool.Name == "" {
		spool.Name = fmt.Sprintf("Spool %d", spool.ID)
	}

	return spool
}

// getSpoolDisplayName returns the display name for sorting purposes
func (spool *SpoolmanSpool) getSpoolDisplayName() string {
	material := "Unknown Material"
	brand := "Unknown Brand"
	name := "Unnamed Spool"

	if spool.Filament != nil {
		if spool.Filament.Material != "" {
			material = spool.Filament.Material
		}
		if spool.Filament.Vendor != nil && spool.Filament.Vendor.Name != "" {
			brand = spool.Filament.Vendor.Name
		}
		if spool.Filament.Name != "" {
			name = spool.Filament.Name
		}
	}

	return fmt.Sprintf("%s - %s - %s", material, brand, name)
}

// GetAllSpools gets all filament spools from Spoolman
func (c *SpoolmanClient) GetAllSpools() ([]SpoolmanSpool, error) {
	resp, err := c.httpClient.Get(c.baseURL + "/api/v1/spool")
	if err != nil {
		return nil, fmt.Errorf("error getting spools from Spoolman: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.handleAPIError(resp)
	}

	var spools []SpoolmanSpool
	if err := json.NewDecoder(resp.Body).Decode(&spools); err != nil {
		return nil, fmt.Errorf("error decoding spools from Spoolman: %w", err)
	}

	// Normalize spool data to extract information from nested structures
	for i := range spools {
		spools[i] = c.normalizeSpoolData(spools[i])
	}

	// Filter out spools with 0g remaining weight
	filteredSpools := make([]SpoolmanSpool, 0, len(spools))
	for _, spool := range spools {
		if spool.RemainingWeight > 0 {
			filteredSpools = append(filteredSpools, spool)
		}
	}
	spools = filteredSpools

	// Sort spools: first alphabetically by display name, then by remaining weight (descending)
	sort.Slice(spools, func(i, j int) bool {
		// First sort by display name (Material - Brand - Name)
		nameI := spools[i].getSpoolDisplayName()
		nameJ := spools[j].getSpoolDisplayName()

		if nameI != nameJ {
			return nameI < nameJ
		}

		// If display names are the same, sort by remaining weight (ascending - use less filament first)
		return spools[i].RemainingWeight < spools[j].RemainingWeight
	})

	return spools, nil
}

// UpdateSpool updates spool information (used for filament usage tracking)
func (c *SpoolmanClient) UpdateSpool(spoolID int, data map[string]interface{}) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("error marshaling spool update data: %w", err)
	}

	req, err := http.NewRequest("PATCH", fmt.Sprintf("%s/api/v1/spool/%d", c.baseURL, spoolID), bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("error creating PUT request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("error updating spool %d in Spoolman: %w", spoolID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.handleAPIError(resp)
	}

	return nil
}

// UpdateSpoolUsage updates spool used weight based on usage (core bridge functionality)
func (c *SpoolmanClient) UpdateSpoolUsage(spoolID int, filamentUsed float64) error {
	// Get current spool data
	resp, err := c.httpClient.Get(fmt.Sprintf("%s/api/v1/spool/%d", c.baseURL, spoolID))
	if err != nil {
		return fmt.Errorf("error getting spool %d from Spoolman: %w", spoolID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("spool %d not found in Spoolman: %w", spoolID, c.handleAPIError(resp))
	}

	var spool SpoolmanSpool
	if err := json.NewDecoder(resp.Body).Decode(&spool); err != nil {
		return fmt.Errorf("error decoding spool %d from Spoolman: %w", spoolID, err)
	}

	// Calculate new used weight
	newUsedWeight := spool.UsedWeight + filamentUsed
	currentTime := time.Now().UTC().Format(time.RFC3339)

	// Update used_weight and timestamps
	updateData := map[string]interface{}{
		"used_weight": newUsedWeight,
		"last_used":   currentTime,
	}

	// Set first_used if it's not already set
	if spool.FirstUsed == "" {
		updateData["first_used"] = currentTime
	}

	if err := c.UpdateSpool(spoolID, updateData); err != nil {
		return fmt.Errorf("failed to update spool %d: %w", spoolID, err)
	}

	fmt.Printf("Updated spool %d: used_weight %.2fg -> %.2fg (added %.2fg)\n",
		spoolID, spool.UsedWeight, newUsedWeight, filamentUsed)

	return nil
}

// TestConnection tests the connection to Spoolman
func (c *SpoolmanClient) TestConnection() error {
	resp, err := c.httpClient.Get(c.baseURL + "/api/v1/info")
	if err != nil {
		return fmt.Errorf("error testing connection to Spoolman: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.handleAPIError(resp)
	}

	return nil
}
