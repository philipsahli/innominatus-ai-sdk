package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAnthropicClient_Generate(t *testing.T) {
	tests := []struct {
		name           string
		mockResponse   anthropicResponse
		mockStatusCode int
		request        GenerateRequest
		wantErr        bool
		wantText       string
	}{
		{
			name: "successful text generation",
			mockResponse: anthropicResponse{
				ID:   "msg_123",
				Type: "message",
				Role: "assistant",
				Content: []anthropicContentBlock{
					{
						Type: "text",
						Text: "Hello, I am Claude!",
					},
				},
				Model:      "claude-sonnet-4-5-20250929",
				StopReason: "end_turn",
				Usage: struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				}{
					InputTokens:  10,
					OutputTokens: 15,
				},
			},
			mockStatusCode: http.StatusOK,
			request: GenerateRequest{
				SystemPrompt: "You are a helpful assistant",
				UserPrompt:   "Hello",
				Temperature:  0.7,
				MaxTokens:    100,
			},
			wantErr:  false,
			wantText: "Hello, I am Claude!",
		},
		{
			name: "generation with markdown code block cleanup",
			mockResponse: anthropicResponse{
				ID:   "msg_124",
				Type: "message",
				Role: "assistant",
				Content: []anthropicContentBlock{
					{
						Type: "text",
						Text: "```json\n{\"key\": \"value\"}\n```",
					},
				},
				Model:      "claude-sonnet-4-5-20250929",
				StopReason: "end_turn",
				Usage: struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				}{
					InputTokens:  5,
					OutputTokens: 10,
				},
			},
			mockStatusCode: http.StatusOK,
			request: GenerateRequest{
				UserPrompt: "Generate JSON",
				MaxTokens:  50,
			},
			wantErr:  false,
			wantText: "{\"key\": \"value\"}",
		},
		{
			name:           "API error - rate limit",
			mockResponse:   anthropicResponse{},
			mockStatusCode: http.StatusTooManyRequests,
			request: GenerateRequest{
				UserPrompt: "test",
				MaxTokens:  50,
			},
			wantErr: true,
		},
		{
			name:           "API error - unauthorized",
			mockResponse:   anthropicResponse{},
			mockStatusCode: http.StatusUnauthorized,
			request: GenerateRequest{
				UserPrompt: "test",
				MaxTokens:  50,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify headers
				if r.Header.Get("anthropic-version") != anthropicAPIVersion {
					t.Errorf("Expected anthropic-version header %s, got %s",
						anthropicAPIVersion, r.Header.Get("anthropic-version"))
				}

				w.WriteHeader(tt.mockStatusCode)
				if tt.mockStatusCode == http.StatusOK {
					if err := json.NewEncoder(w).Encode(tt.mockResponse); err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
				} else {
					if err := json.NewEncoder(w).Encode(anthropicError{
						Type: "error",
						Error: struct {
							Type    string `json:"type"`
							Message string `json:"message"`
						}{
							Type:    "rate_limit_error",
							Message: "Rate limit exceeded",
						},
					}); err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
				}
			}))
			defer server.Close()

			// Create client with mock server URL
			client := &AnthropicClient{
				apiKey: "test-key",
				model:  "claude-sonnet-4-5-20250929",
				apiURL: server.URL,
				httpClient: &http.Client{
					Timeout: defaultTimeout,
				},
			}

			ctx := context.Background()
			resp, err := client.Generate(ctx, tt.request)

			if (err != nil) != tt.wantErr {
				t.Errorf("Generate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if resp.Text != tt.wantText {
					t.Errorf("Generate() text = %v, want %v", resp.Text, tt.wantText)
				}
				if resp.Usage.TotalTokens == 0 {
					t.Error("Generate() usage tokens should not be zero")
				}
			}
		})
	}
}

func TestAnthropicClient_GenerateWithContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(anthropicResponse{
			ID:   "msg_125",
			Type: "message",
			Role: "assistant",
			Content: []anthropicContentBlock{
				{
					Type: "text",
					Text: "Response with context",
				},
			},
			Model:      "claude-sonnet-4-5-20250929",
			StopReason: "end_turn",
			Usage: struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			}{
				InputTokens:  20,
				OutputTokens: 10,
			},
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}))
	defer server.Close()

	client := &AnthropicClient{
		apiKey: "test-key",
		model:  "claude-sonnet-4-5-20250929",
		apiURL: server.URL,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
	}

	ctx := context.Background()
	req := GenerateRequest{
		UserPrompt: "What is the answer?",
		MaxTokens:  50,
	}

	resp, err := client.GenerateWithContext(ctx, req, "Context: The answer is 42")
	if err != nil {
		t.Fatalf("GenerateWithContext() error = %v", err)
	}

	if resp.Text != "Response with context" {
		t.Errorf("GenerateWithContext() text = %v, want %v", resp.Text, "Response with context")
	}
}

func TestAnthropicClient_GenerateWithTools(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(anthropicResponse{
			ID:   "msg_126",
			Type: "message",
			Role: "assistant",
			Content: []anthropicContentBlock{
				{
					Type: "text",
					Text: "I'll use the calculator",
				},
				{
					Type:  "tool_use",
					ID:    "tool_123",
					Name:  "calculator",
					Input: map[string]interface{}{"operation": "add", "a": 2, "b": 3},
				},
			},
			Model:      "claude-sonnet-4-5-20250929",
			StopReason: "tool_use",
			Usage: struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			}{
				InputTokens:  15,
				OutputTokens: 20,
			},
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}))
	defer server.Close()

	client := &AnthropicClient{
		apiKey: "test-key",
		model:  "claude-sonnet-4-5-20250929",
		apiURL: server.URL,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
	}

	ctx := context.Background()
	req := GenerateWithToolsRequest{
		SystemPrompt: "You are a calculator assistant",
		Messages: []Message{
			{
				Role: "user",
				Content: []ContentBlock{
					{
						Type: "text",
						Text: "What is 2 + 3?",
					},
				},
			},
		},
		Tools: []Tool{
			{
				Name:        "calculator",
				Description: "Performs calculations",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"operation": map[string]interface{}{"type": "string"},
						"a":         map[string]interface{}{"type": "number"},
						"b":         map[string]interface{}{"type": "number"},
					},
				},
			},
		},
		MaxTokens:   100,
		Temperature: 0.5,
	}

	resp, err := client.GenerateWithTools(ctx, req)
	if err != nil {
		t.Fatalf("GenerateWithTools() error = %v", err)
	}

	if len(resp.ToolUses) != 1 {
		t.Errorf("GenerateWithTools() tool uses count = %d, want 1", len(resp.ToolUses))
	}

	if resp.ToolUses[0].Name != "calculator" {
		t.Errorf("GenerateWithTools() tool name = %v, want calculator", resp.ToolUses[0].Name)
	}
}

func TestAnthropicClient_ContextTimeout(t *testing.T) {
	// Create a slow server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := &AnthropicClient{
		apiKey: "test-key",
		model:  "claude-sonnet-4-5-20250929",
		apiURL: server.URL,
		httpClient: &http.Client{
			Timeout: 1 * time.Second,
		},
	}

	ctx := context.Background()
	req := GenerateRequest{
		UserPrompt: "test",
		MaxTokens:  50,
	}

	_, err := client.Generate(ctx, req)
	if err == nil {
		t.Error("Expected timeout error, got nil")
	}
}

func TestCleanLLMResponse(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no markdown",
			input: "plain text",
			want:  "plain text",
		},
		{
			name:  "json markdown block",
			input: "```json\n{\"key\": \"value\"}\n```",
			want:  "{\"key\": \"value\"}",
		},
		{
			name:  "yaml markdown block",
			input: "```yaml\nkey: value\n```",
			want:  "key: value",
		},
		{
			name:  "text with leading/trailing whitespace",
			input: "\n\n  some text  \n\n",
			want:  "some text",
		},
		{
			name:  "markdown with language",
			input: "```go\nfunc main() {}\n```",
			want:  "func main() {}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanLLMResponse(tt.input)
			if got != tt.want {
				t.Errorf("cleanLLMResponse() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewAnthropicClient(t *testing.T) {
	config := Config{
		Provider:    "anthropic",
		APIKey:      "test-key",
		Model:       "claude-sonnet-4-5-20250929",
		Temperature: 0.7,
		MaxTokens:   1000,
	}

	client := NewAnthropicClient(config)

	if client.apiKey != config.APIKey {
		t.Errorf("NewAnthropicClient() apiKey = %v, want %v", client.apiKey, config.APIKey)
	}

	if client.model != config.Model {
		t.Errorf("NewAnthropicClient() model = %v, want %v", client.model, config.Model)
	}

	if client.httpClient == nil {
		t.Error("NewAnthropicClient() httpClient is nil")
	}

	if client.httpClient.Timeout != defaultTimeout {
		t.Errorf("NewAnthropicClient() timeout = %v, want %v",
			client.httpClient.Timeout, defaultTimeout)
	}
}
