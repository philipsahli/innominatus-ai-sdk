package llm

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
	anthropicAPIURL     = "https://api.anthropic.com/v1/messages"
	anthropicAPIVersion = "2023-06-01"
	defaultTimeout      = 60 * time.Second
)

// AnthropicClient implements the Client interface for Anthropic's Claude API
type AnthropicClient struct {
	apiKey     string
	model      string
	httpClient *http.Client
	apiURL     string // Override for testing
}

// NewAnthropicClient creates a new Anthropic client
func NewAnthropicClient(config Config) *AnthropicClient {
	return &AnthropicClient{
		apiKey: config.APIKey,
		model:  config.Model,
		apiURL: anthropicAPIURL,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

// anthropicRequest represents the request format for Anthropic API
type anthropicRequest struct {
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"`
	Temperature float32            `json:"temperature,omitempty"`
	System      string             `json:"system,omitempty"`
	Messages    []anthropicMessage `json:"messages"`
	Tools       []Tool             `json:"tools,omitempty"`
}

// anthropicMessage represents a message in the conversation
type anthropicMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // Can be string or []anthropicContentBlock
}

// anthropicContentBlock represents content in a message
type anthropicContentBlock struct {
	Type      string                 `json:"type"` // "text", "tool_use", "tool_result"
	Text      string                 `json:"text,omitempty"`
	ID        string                 `json:"id,omitempty"`
	Name      string                 `json:"name,omitempty"`
	Input     map[string]interface{} `json:"-"` // Custom marshaling - must be present for tool_use
	ToolUseID string                 `json:"tool_use_id,omitempty"`
	Content   string                 `json:"content,omitempty"`
	IsError   bool                   `json:"is_error,omitempty"`
}

// MarshalJSON implements custom JSON marshaling to ensure input is always included for tool_use
func (b anthropicContentBlock) MarshalJSON() ([]byte, error) {
	// Create a map for manual JSON construction
	result := make(map[string]interface{})
	result["type"] = b.Type

	if b.Text != "" {
		result["text"] = b.Text
	}
	if b.ID != "" {
		result["id"] = b.ID
	}
	if b.Name != "" {
		result["name"] = b.Name
	}

	// For tool_use, always include input even if empty
	if b.Type == "tool_use" {
		if b.Input == nil {
			result["input"] = map[string]interface{}{}
		} else {
			result["input"] = b.Input
		}
	} else if len(b.Input) > 0 {
		result["input"] = b.Input
	}

	if b.ToolUseID != "" {
		result["tool_use_id"] = b.ToolUseID
	}
	if b.Content != "" {
		result["content"] = b.Content
	}
	if b.IsError {
		result["is_error"] = b.IsError
	}

	return json.Marshal(result)
}

// anthropicResponse represents the response format from Anthropic API
type anthropicResponse struct {
	ID           string                  `json:"id"`
	Type         string                  `json:"type"`
	Role         string                  `json:"role"`
	Content      []anthropicContentBlock `json:"content"`
	Model        string                  `json:"model"`
	StopReason   string                  `json:"stop_reason"`
	StopSequence string                  `json:"stop_sequence,omitempty"`
	Usage        struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// anthropicError represents an error response from Anthropic API
type anthropicError struct {
	Type  string `json:"type"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// Generate sends a request to the Anthropic API and returns the response
func (c *AnthropicClient) Generate(ctx context.Context, req GenerateRequest) (*GenerateResponse, error) {
	// Build request payload
	payload := anthropicRequest{
		Model:       c.model,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		System:      req.SystemPrompt,
		Messages: []anthropicMessage{
			{
				Role:    "user",
				Content: req.UserPrompt,
			},
		},
		Tools: req.Tools,
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicAPIVersion)
	httpReq.Header.Set("content-type", "application/json")

	// Send request
	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer httpResp.Body.Close()

	// Read response body
	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Handle non-200 status codes
	if httpResp.StatusCode != http.StatusOK {
		var apiErr anthropicError
		if err := json.Unmarshal(body, &apiErr); err != nil {
			return nil, fmt.Errorf("API error (status %d): %s", httpResp.StatusCode, string(body))
		}
		return nil, fmt.Errorf("API error: %s - %s", apiErr.Error.Type, apiErr.Error.Message)
	}

	// Parse response
	var apiResp anthropicResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Extract text and tool uses from content
	var text string
	var toolUses []ToolUse
	for _, content := range apiResp.Content {
		if content.Type == "text" {
			text += content.Text
		} else if content.Type == "tool_use" {
			toolUses = append(toolUses, ToolUse{
				ID:    content.ID,
				Name:  content.Name,
				Input: content.Input,
			})
		}
	}

	// Clean up potential markdown formatting from LLM response
	text = cleanLLMResponse(text)

	return &GenerateResponse{
		Text:       text,
		ToolUses:   toolUses,
		StopReason: apiResp.StopReason,
		Usage: Usage{
			PromptTokens:     apiResp.Usage.InputTokens,
			CompletionTokens: apiResp.Usage.OutputTokens,
			TotalTokens:      apiResp.Usage.InputTokens + apiResp.Usage.OutputTokens,
		},
	}, nil
}

// GenerateWithContext sends a request with additional context prepended to the user prompt
func (c *AnthropicClient) GenerateWithContext(ctx context.Context, req GenerateRequest, additionalContext string) (*GenerateResponse, error) {
	// Prepend additional context to the user prompt
	enhancedPrompt := req.UserPrompt
	if additionalContext != "" {
		enhancedPrompt = additionalContext + "\n\n" + req.UserPrompt
	}

	// Create new request with enhanced prompt
	enhancedReq := GenerateRequest{
		SystemPrompt: req.SystemPrompt,
		UserPrompt:   enhancedPrompt,
		Temperature:  req.Temperature,
		MaxTokens:    req.MaxTokens,
	}

	return c.Generate(ctx, enhancedReq)
}

// GenerateWithTools sends a multi-turn conversation request with tool support
func (c *AnthropicClient) GenerateWithTools(ctx context.Context, req GenerateWithToolsRequest) (*GenerateResponse, error) {
	// Convert messages to anthropic format
	var messages []anthropicMessage
	for _, msg := range req.Messages {
		// Convert content blocks
		var content interface{}
		if len(msg.Content) == 1 && msg.Content[0].Type == "text" {
			// Simple text message
			content = msg.Content[0].Text
		} else {
			// Complex message with multiple content blocks
			var blocks []anthropicContentBlock
			for _, block := range msg.Content {
				blocks = append(blocks, anthropicContentBlock(block))
			}
			content = blocks
		}

		messages = append(messages, anthropicMessage{
			Role:    msg.Role,
			Content: content,
		})
	}

	// Build request payload
	payload := anthropicRequest{
		Model:       c.model,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		System:      req.SystemPrompt,
		Messages:    messages,
		Tools:       req.Tools,
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicAPIVersion)
	httpReq.Header.Set("content-type", "application/json")

	// Send request
	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer httpResp.Body.Close()

	// Read response body
	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Handle non-200 status codes
	if httpResp.StatusCode != http.StatusOK {
		var apiErr anthropicError
		if err := json.Unmarshal(body, &apiErr); err != nil {
			return nil, fmt.Errorf("API error (status %d): %s", httpResp.StatusCode, string(body))
		}
		return nil, fmt.Errorf("API error: %s - %s", apiErr.Error.Type, apiErr.Error.Message)
	}

	// Parse response
	var apiResp anthropicResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Extract text and tool uses from content
	var text string
	var toolUses []ToolUse
	for _, content := range apiResp.Content {
		if content.Type == "text" {
			text += content.Text
		} else if content.Type == "tool_use" {
			toolUses = append(toolUses, ToolUse{
				ID:    content.ID,
				Name:  content.Name,
				Input: content.Input,
			})
		}
	}

	return &GenerateResponse{
		Text:       text,
		ToolUses:   toolUses,
		StopReason: apiResp.StopReason,
		Usage: Usage{
			PromptTokens:     apiResp.Usage.InputTokens,
			CompletionTokens: apiResp.Usage.OutputTokens,
			TotalTokens:      apiResp.Usage.InputTokens + apiResp.Usage.OutputTokens,
		},
	}, nil
}

// cleanLLMResponse removes markdown code blocks and extra whitespace
func cleanLLMResponse(text string) string {
	// Remove markdown code blocks (```json ... ```)
	text = strings.TrimSpace(text)

	// Check if wrapped in markdown code block
	if strings.HasPrefix(text, "```") {
		lines := strings.Split(text, "\n")
		if len(lines) > 2 {
			// Remove first line (```json or ```yaml)
			lines = lines[1:]
			// Remove last line (```)
			if strings.HasPrefix(lines[len(lines)-1], "```") {
				lines = lines[:len(lines)-1]
			}
			text = strings.Join(lines, "\n")
		}
	}

	return strings.TrimSpace(text)
}
