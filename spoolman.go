package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"time"
)

// SpoolmanClient handles communication with Spoolman API for bridge functionality
type SpoolmanClient struct {
	baseURL    string
	httpClient *http.Client
	username   string
	password   string
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
	LocationID      *int                   `json:"location_id"` // Reference to Spoolman Location entity
	Extra           map[string]interface{} `json:"extra"`

	// Computed fields for easier access
	Name     string `json:"name"`     // Computed from filament.name
	Brand    string `json:"brand"`    // Computed from filament.vendor.name
	Material string `json:"material"` // Computed from filament.material
	Location string `json:"location"` // Spool location (e.g., "Printer1 - Toolhead 0") - kept for backward compatibility
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
func NewSpoolmanClient(baseURL string, timeout int, username, password string) *SpoolmanClient {
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
		username: username,
		password: password,
	}
}

// addAuthHeader adds Basic Authentication header to the request if both username and password are provided
func (c *SpoolmanClient) addAuthHeader(req *http.Request) {
	if c.username != "" && c.password != "" {
		auth := c.username + ":" + c.password
		encoded := base64.StdEncoding.EncodeToString([]byte(auth))
		req.Header.Set("Authorization", "Basic "+encoded)
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
	req, err := http.NewRequest("GET", c.baseURL+"/api/v1/spool", nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}
	c.addAuthHeader(req)

	resp, err := c.httpClient.Do(req)
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

// GetAllFilaments gets all filament types from Spoolman
func (c *SpoolmanClient) GetAllFilaments() ([]SpoolmanFilament, error) {
	req, err := http.NewRequest("GET", c.baseURL+"/api/v1/filament", nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}
	c.addAuthHeader(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error getting filaments from Spoolman: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.handleAPIError(resp)
	}

	var filaments []SpoolmanFilament
	if err := json.NewDecoder(resp.Body).Decode(&filaments); err != nil {
		return nil, fmt.Errorf("error decoding filaments from Spoolman: %w", err)
	}

	// Filter out archived filaments
	filteredFilaments := make([]SpoolmanFilament, 0, len(filaments))
	for _, filament := range filaments {
		if !filament.Archived {
			filteredFilaments = append(filteredFilaments, filament)
		}
	}
	filaments = filteredFilaments

	// Sort filaments by ID
	sort.Slice(filaments, func(i, j int) bool {
		return filaments[i].ID < filaments[j].ID
	})

	return filaments, nil
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
	c.addAuthHeader(req)

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
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/api/v1/spool/%d", c.baseURL, spoolID), nil)
	if err != nil {
		return fmt.Errorf("error creating request: %w", err)
	}
	c.addAuthHeader(req)

	resp, err := c.httpClient.Do(req)
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
	req, err := http.NewRequest("GET", c.baseURL+"/api/v1/info", nil)
	if err != nil {
		return fmt.Errorf("error creating request: %w", err)
	}
	c.addAuthHeader(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("error testing connection to Spoolman: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.handleAPIError(resp)
	}

	return nil
}

// SpoolmanLocation represents a location from Spoolman
type SpoolmanLocation struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Comment  string `json:"comment"`
	Archived bool   `json:"archived"`
}

// GetLocations gets all locations from Spoolman
func (c *SpoolmanClient) GetLocations() ([]SpoolmanLocation, error) {
	req, err := http.NewRequest("GET", c.baseURL+"/api/v1/location", nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}
	c.addAuthHeader(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error getting locations from Spoolman: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.handleAPIError(resp)
	}

	// Read full body so we can retry alternative shapes and log on error
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading locations response from Spoolman: %w", err)
	}

	// 1) Try standard array of objects
	var locations []SpoolmanLocation
	if err := json.Unmarshal(bodyBytes, &locations); err == nil {
		return locations, nil
	}

	// 2) Try { data: [...] } wrapper
	var dataWrapper struct {
		Data    []SpoolmanLocation `json:"data"`
		Results []SpoolmanLocation `json:"results"`
	}
	if err := json.Unmarshal(bodyBytes, &dataWrapper); err == nil {
		if len(dataWrapper.Data) > 0 {
			return dataWrapper.Data, nil
		}
		if len(dataWrapper.Results) > 0 {
			return dataWrapper.Results, nil
		}
	}

	// 3) Try simple array of names like ["Testing", ...]
	var names []string
	if err := json.Unmarshal(bodyBytes, &names); err == nil {
		for _, n := range names {
			locations = append(locations, SpoolmanLocation{Name: n})
		}
		return locations, nil
	}

	// Log snippet for diagnostics and return error
	snippet := string(bodyBytes)
	if len(snippet) > 300 {
		snippet = snippet[:300] + "..."
	}
	log.Printf("Spoolman /location unexpected JSON. Snippet: %s", snippet)
	return nil, fmt.Errorf("error decoding locations from Spoolman: unexpected JSON shape")
}

// GetOrCreateLocation gets an existing location by name or creates it if it doesn't exist
func (c *SpoolmanClient) GetOrCreateLocation(name string) (*SpoolmanLocation, error) {
	// First try to get existing locations
	locations, err := c.GetLocations()
	if err != nil {
		return nil, fmt.Errorf("failed to get locations: %w", err)
	}

	// Look for existing location with this name
	for _, location := range locations {
		if location.Name == name {
			return &location, nil
		}
	}

	// Location doesn't exist, try to create it
	// If creation fails, return a warning but don't fail the entire operation
	createdLocation, err := c.CreateLocation(name)
	if err != nil {
		// Log the error but return a dummy location so the system can continue
		log.Printf("Warning: Could not create location '%s' in Spoolman: %v", name, err)
		// Return a dummy location with the name
		return &SpoolmanLocation{
			ID:   0, // Dummy ID
			Name: name,
		}, nil
	}

	return createdLocation, nil
}

// CreateLocation creates a new location in Spoolman
func (c *SpoolmanClient) CreateLocation(name string) (*SpoolmanLocation, error) {
	locationData := map[string]interface{}{
		"name": name,
	}

	jsonData, err := json.Marshal(locationData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal location data: %w", err)
	}

	req, err := http.NewRequest("POST", c.baseURL+"/api/v1/location", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.addAuthHeader(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error creating location in Spoolman: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return nil, c.handleAPIError(resp)
	}

	var location SpoolmanLocation
	if err := json.NewDecoder(resp.Body).Decode(&location); err != nil {
		return nil, fmt.Errorf("error decoding created location from Spoolman: %w", err)
	}

	return &location, nil
}

// FindLocationByName searches for an existing location by name
func (c *SpoolmanClient) FindLocationByName(name string) (*SpoolmanLocation, error) {
	locations, err := c.GetLocations()
	if err != nil {
		return nil, fmt.Errorf("error getting locations: %w", err)
	}

	for _, location := range locations {
		if location.Name == name {
			return &location, nil
		}
	}

	return nil, nil // Location not found
}

// LocationExistsInSpoolman checks if a location exists in Spoolman
func (c *SpoolmanClient) LocationExistsInSpoolman(name string) (bool, error) {
	location, err := c.FindLocationByName(name)
	if err != nil {
		return false, err
	}
	return location != nil, nil
}

// RenameLocation renames a location in Spoolman using the PATCH API
func (c *SpoolmanClient) RenameLocation(oldName, newName string) error {
	updateData := map[string]interface{}{
		"name": newName,
	}

	jsonData, err := json.Marshal(updateData)
	if err != nil {
		return fmt.Errorf("failed to marshal location rename data: %w", err)
	}

	req, err := http.NewRequest("PATCH", fmt.Sprintf("%s/api/v1/location/%s", c.baseURL, oldName), bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("error creating PATCH request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.addAuthHeader(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("error renaming location in Spoolman: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.handleAPIError(resp)
	}

	log.Printf("Successfully renamed Spoolman location from '%s' to '%s'", oldName, newName)
	return nil
}

// UpdateSpoolLocation updates a spool's location in Spoolman using text-based location field
func (c *SpoolmanClient) UpdateSpoolLocation(spoolID int, locationName string) error {
	// Use text-based location assignment - Spoolman will create the location if it doesn't exist
	return c.updateSpoolLocationText(spoolID, locationName)
}

// UpdateLocation updates a location name in Spoolman
func (c *SpoolmanClient) UpdateLocation(locationID int, newName string) error {
	updateData := map[string]interface{}{
		"name": newName,
	}

	jsonData, err := json.Marshal(updateData)
	if err != nil {
		return fmt.Errorf("failed to marshal location update data: %w", err)
	}

	req, err := http.NewRequest("PATCH", fmt.Sprintf("%s/api/v1/location/%d", c.baseURL, locationID), bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("error creating PATCH request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.addAuthHeader(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("error updating location %d in Spoolman: %w", locationID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.handleAPIError(resp)
	}

	log.Printf("Successfully updated Spoolman location %d to '%s'", locationID, newName)
	return nil
}

// ArchiveLocation archives a location in Spoolman
func (c *SpoolmanClient) ArchiveLocation(locationID int) error {
	updateData := map[string]interface{}{
		"archived": true,
	}

	jsonData, err := json.Marshal(updateData)
	if err != nil {
		return fmt.Errorf("failed to marshal location archive data: %w", err)
	}

	req, err := http.NewRequest("PATCH", fmt.Sprintf("%s/api/v1/location/%d", c.baseURL, locationID), bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("error creating PATCH request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.addAuthHeader(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("error archiving location %d in Spoolman: %w", locationID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.handleAPIError(resp)
	}

	log.Printf("Successfully archived Spoolman location %d", locationID)
	return nil
}

// UpdateLocationByName updates a location in Spoolman by name
func (c *SpoolmanClient) UpdateLocationByName(oldName, newName string) error {
	// First, find the location by name
	locations, err := c.GetLocations()
	if err != nil {
		return fmt.Errorf("failed to get locations: %w", err)
	}

	var locationID int
	found := false
	for _, loc := range locations {
		if loc.Name == oldName && !loc.Archived {
			locationID = loc.ID
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("location '%s' not found in Spoolman", oldName)
	}

	// Update the location using its ID
	return c.UpdateLocation(locationID, newName)
}

// updateSpoolLocationText updates a spool's location using the text field
func (c *SpoolmanClient) updateSpoolLocationText(spoolID int, locationName string) error {
	updateData := map[string]interface{}{
		"location": locationName,
	}

	jsonData, err := json.Marshal(updateData)
	if err != nil {
		return fmt.Errorf("error marshaling location update data: %w", err)
	}

	req, err := http.NewRequest("PATCH", fmt.Sprintf("%s/api/v1/spool/%d", c.baseURL, spoolID), bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("error creating PATCH request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.addAuthHeader(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("error updating spool %d location in Spoolman: %w", spoolID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.handleAPIError(resp)
	}

	log.Printf("Successfully updated spool %d to location '%s' (text-based)", spoolID, locationName)
	return nil
}

// UpdateSpoolmanLocationReferences renames the location in Spoolman using the location rename API
func (c *SpoolmanClient) UpdateSpoolmanLocationReferences(oldName, newName string) error {
	log.Printf("UpdateSpoolmanLocationReferences: Renaming location from '%s' to '%s' in Spoolman", oldName, newName)

	// Check if the old location exists in Spoolman
	exists, err := c.LocationExistsInSpoolman(oldName)
	if err != nil {
		log.Printf("UpdateSpoolmanLocationReferences: Failed to check if location exists: %v", err)
		return fmt.Errorf("failed to check if location exists in Spoolman: %w", err)
	}

	if !exists {
		log.Printf("UpdateSpoolmanLocationReferences: Location '%s' does not exist in Spoolman, skipping rename", oldName)
		return nil
	}

	// Use the location rename API to rename the location directly
	if err := c.RenameLocation(oldName, newName); err != nil {
		log.Printf("UpdateSpoolmanLocationReferences: Failed to rename location in Spoolman: %v", err)
		return fmt.Errorf("failed to rename location in Spoolman: %w", err)
	}

	log.Printf("UpdateSpoolmanLocationReferences: Successfully renamed location from '%s' to '%s' in Spoolman", oldName, newName)
	return nil
}
