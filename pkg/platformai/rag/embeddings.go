package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	voyageBaseURL = "https://api.voyageai.com"
	openAIBaseURL = "https://api.openai.com"
)

// embeddingsURL accepts a base URL with or without a trailing slash, so both
// "https://proxy/prefix" and "https://proxy/prefix/" reach the same endpoint.
func embeddingsURL(baseURL string) string {
	return strings.TrimRight(baseURL, "/") + "/v1/embeddings"
}

// VoyageEmbeddingClient implements EmbeddingProvider using Voyage AI
type VoyageEmbeddingClient struct {
	apiKey     string
	model      string
	baseURL    string
	httpClient *http.Client
}

// NewVoyageEmbeddingClient creates a new Voyage AI embedding client
func NewVoyageEmbeddingClient(apiKey, model string) *VoyageEmbeddingClient {
	if model == "" {
		model = "voyage-3" // Default model
	}
	return &VoyageEmbeddingClient{
		apiKey:  apiKey,
		model:   model,
		baseURL: voyageBaseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
			},
		},
	}
}

// GenerateEmbedding generates an embedding for a single text
func (c *VoyageEmbeddingClient) GenerateEmbedding(ctx context.Context, text string) ([]float32, error) {
	embeddings, err := c.GenerateEmbeddings(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(embeddings) == 0 {
		return nil, fmt.Errorf("no embeddings returned")
	}
	return embeddings[0], nil
}

// GenerateEmbeddings generates embeddings for multiple texts
func (c *VoyageEmbeddingClient) GenerateEmbeddings(ctx context.Context, texts []string) ([][]float32, error) {
	reqBody := map[string]interface{}{
		"input": texts,
		"model": c.model,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", embeddingsURL(c.baseURL), bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	embeddings := make([][]float32, len(result.Data))
	for i, data := range result.Data {
		embeddings[i] = data.Embedding
	}

	return embeddings, nil
}

// OpenAIEmbeddingClient implements EmbeddingProvider using OpenAI
type OpenAIEmbeddingClient struct {
	apiKey     string
	model      string
	baseURL    string
	httpClient *http.Client
}

// NewOpenAIEmbeddingClient creates a new OpenAI embedding client
func NewOpenAIEmbeddingClient(apiKey, model string) *OpenAIEmbeddingClient {
	if model == "" {
		model = "text-embedding-3-small" // Default model
	}
	return &OpenAIEmbeddingClient{
		apiKey:  apiKey,
		model:   model,
		baseURL: openAIBaseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
			},
		},
	}
}

// GenerateEmbedding generates an embedding for a single text
func (c *OpenAIEmbeddingClient) GenerateEmbedding(ctx context.Context, text string) ([]float32, error) {
	embeddings, err := c.GenerateEmbeddings(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(embeddings) == 0 {
		return nil, fmt.Errorf("no embeddings returned")
	}
	return embeddings[0], nil
}

// GenerateEmbeddings generates embeddings for multiple texts
func (c *OpenAIEmbeddingClient) GenerateEmbeddings(ctx context.Context, texts []string) ([][]float32, error) {
	reqBody := map[string]interface{}{
		"input": texts,
		"model": c.model,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", embeddingsURL(c.baseURL), bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	embeddings := make([][]float32, len(result.Data))
	for i, data := range result.Data {
		embeddings[i] = data.Embedding
	}

	return embeddings, nil
}

// applyTimeout overrides the constructor's default timeout only for a positive
// value, so a zero Config.Timeout keeps the 30s default.
func applyTimeout(c *http.Client, timeout time.Duration) {
	if timeout > 0 {
		c.Timeout = timeout
	}
}

// NewEmbeddingProvider creates an embedding provider based on the config
func NewEmbeddingProvider(config Config) (EmbeddingProvider, error) {
	switch config.EmbeddingProvider {
	case "voyageai", "voyage":
		c := NewVoyageEmbeddingClient(config.APIKey, config.Model)
		applyTimeout(c.httpClient, config.Timeout)
		if config.BaseURL != "" {
			c.baseURL = config.BaseURL
		}
		return c, nil
	case "openai":
		c := NewOpenAIEmbeddingClient(config.APIKey, config.Model)
		applyTimeout(c.httpClient, config.Timeout)
		if config.BaseURL != "" {
			c.baseURL = config.BaseURL
		}
		return c, nil
	default:
		return nil, fmt.Errorf("unsupported embedding provider: %s (supported: voyageai, openai)", config.EmbeddingProvider)
	}
}
