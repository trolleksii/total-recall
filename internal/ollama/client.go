package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"total-recall/internal/config"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

// EmbedRequest represents the request body for the /api/embed endpoint
type EmbedRequest struct {
	Model     string   `json:"model"`
	KeepAlive string   `json:"keep_alive"`
	Input     []string `json:"input"`
}

// EmbedResponse represents the response from the /api/embed endpoint
type EmbedResponse struct {
	Model           string        `json:"model"`
	Embeddings      [][]float32   `json:"embeddings"`
	TotalDuration   time.Duration `json:"total_duration"`
	LoadDuration    time.Duration `json:"load_duration"`
	PromptEvalCount int           `json:"prompt_eval_count"`
}

// GenerateRequest represents the request body for the /api/generate endpoint
type GenerateRequest struct {
	Model     string  `json:"model"`
	Prompt    string  `json:"prompt"`
	Stream    bool    `json:"stream"`
	Think     bool    `json:"think"`
	KeepAlive string  `json:"keep_alive"`
	Options   Options `json:"options"`
}

type Options struct {
	Temperature float32 `json:"temperature"`
}

// GenerateResponse represents the response from the /api/generate endpoint
type GenerateResponse struct {
	Model    string `json:"model"`
	Response string `json:"response"`
	Done     bool   `json:"done"`
}

// NewClient creates a new Ollama client
func NewClient() *Client {
	cfg := config.Get()
	return &Client{
		baseURL: cfg.Ollama.URL,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// GenerateEmbedding generates an embedding for the given text
func (c *Client) GenerateEmbeddings(ctx context.Context, textBatch []string) ([][]float32, error) {
	cfg := config.Get()
	request := EmbedRequest{
		Model:     cfg.Ollama.EmbeddingModel,
		KeepAlive: "60m",
		Input:     textBatch,
	}

	jsonData, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/api/embed", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var response EmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return response.Embeddings, nil
}

// Generate uses the LLM to rank commands by relevance to the query
func (c *Client) GenerateResponse(ctx context.Context, prompt string) (string, error) {
	cfg := config.Get()
	request := GenerateRequest{
		Model:     cfg.Ollama.GenerationModel,
		Prompt:    prompt,
		Stream:    false,
		Think:     false,
		KeepAlive: "60m",
		Options: Options{
			Temperature: 0.2,
		},
	}

	jsonData, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/api/generate", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var genResponse GenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&genResponse); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return genResponse.Response, nil
}
