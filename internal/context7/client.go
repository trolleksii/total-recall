package context7

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	baseURL = "https://context7.com/api/v1"
)

// Client handles Context7 API interactions
type Client struct {
	httpClient *http.Client
	apiKey     string
}

// SearchResult represents a single library search result
type SearchResult struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	TrustScore  int    `json:"trustScore"`
}

// SearchResponse represents the search API response
type SearchResponse struct {
	Results []SearchResult `json:"results"`
	Error   string         `json:"error,omitempty"`
}

// NewClient creates a new Context7 HTTP client
func NewClient(apiKey string) *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		apiKey: apiKey,
	}
}

// ResolveLibraryID searches for a library and returns the best matching library ID
func (c *Client) ResolveLibraryID(ctx context.Context, libraryName string) (string, error) {
	if libraryName == "" {
		return "", fmt.Errorf("library name is empty")
	}

	// Build search URL
	searchURL := fmt.Sprintf("%s/search?query=%s", baseURL, url.QueryEscape(libraryName))

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Add API key if provided
	if c.apiKey != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiKey))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	// Handle response codes
	if resp.StatusCode == http.StatusTooManyRequests {
		return "", fmt.Errorf("rate limit exceeded")
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return "", fmt.Errorf("unauthorized - check API key")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var searchResp SearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if searchResp.Error != "" {
		return "", fmt.Errorf("API error: %s", searchResp.Error)
	}

	if len(searchResp.Results) == 0 {
		return "", fmt.Errorf("no results found for library: %s", libraryName)
	}

	// Return the first (best) match ID
	return searchResp.Results[0].ID, nil
}

// GetLibraryDocs fetches documentation for a specific library
func (c *Client) GetLibraryDocs(ctx context.Context, libraryID, topic string) (string, error) {
	if libraryID == "" {
		return "", fmt.Errorf("library ID is empty")
	}

	// Clean up library ID (remove leading slash if present)
	libraryID = strings.TrimPrefix(libraryID, "/")

	// Build documentation URL
	docURL := fmt.Sprintf("%s/%s", baseURL, libraryID)

	// Add query parameters
	params := url.Values{}
	params.Set("type", "txt")
	params.Set("tokens", "5000") // Default token limit

	if topic != "" {
		params.Set("topic", topic)
	}

	if len(params) > 0 {
		docURL = fmt.Sprintf("%s?%s", docURL, params.Encode())
	}

	req, err := http.NewRequestWithContext(ctx, "GET", docURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Add API key if provided
	if c.apiKey != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiKey))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	// Handle response codes
	if resp.StatusCode == http.StatusTooManyRequests {
		return "", fmt.Errorf("rate limit exceeded")
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return "", fmt.Errorf("unauthorized - check API key")
	}
	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("documentation not found for library: %s", libraryID)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Read documentation content
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	return string(body), nil
}
