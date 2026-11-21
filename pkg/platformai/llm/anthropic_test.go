package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
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
				apiKey:  "test-key",
				model:   "claude-sonnet-4-5-20250929",
				baseURL: server.URL,
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
		apiKey:  "test-key",
		model:   "claude-sonnet-4-5-20250929",
		baseURL: server.URL,
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
		apiKey:  "test-key",
		model:   "claude-sonnet-4-5-20250929",
		baseURL: server.URL,
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

	// JSON numbers decode as float64.
	wantInput := map[string]interface{}{"operation": "add", "a": float64(2), "b": float64(3)}
	if !reflect.DeepEqual(resp.ToolUses[0].Input, wantInput) {
		t.Errorf("GenerateWithTools() tool input = %v, want %v", resp.ToolUses[0].Input, wantInput)
	}
}

func TestAnthropicContentBlock_RoundTrip(t *testing.T) {
	in := anthropicContentBlock{
		Type:  "tool_use",
		ID:    "toolu_9",
		Name:  "get_service_owner",
		Input: map[string]interface{}{"service": "api"},
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var out anthropicContentBlock
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if !reflect.DeepEqual(out, in) {
		t.Errorf("round trip = %+v, want %+v", out, in)
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
		apiKey:  "test-key",
		model:   "claude-sonnet-4-5-20250929",
		baseURL: server.URL,
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

func newTestClient(baseURL string) *AnthropicClient {
	return &AnthropicClient{
		apiKey:     "test-key",
		model:      "claude-sonnet-4-5-20250929",
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

// generateFuncs runs each error case against both entry points, because
// Generate and GenerateWithTools each carry their own copy of the HTTP and
// response handling.
var generateFuncs = map[string]func(context.Context, *AnthropicClient) (*GenerateResponse, error){
	"Generate": func(ctx context.Context, c *AnthropicClient) (*GenerateResponse, error) {
		return c.Generate(ctx, GenerateRequest{UserPrompt: "test", MaxTokens: 50})
	},
	"GenerateWithTools": func(ctx context.Context, c *AnthropicClient) (*GenerateResponse, error) {
		return c.GenerateWithTools(ctx, GenerateWithToolsRequest{
			Messages:  []Message{{Role: "user", Content: []ContentBlock{{Type: "text", Text: "test"}}}},
			MaxTokens: 50,
		})
	},
}

func TestAnthropicClient_ErrorResponses(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		body         string
		wantErrParts []string
	}{
		{
			name:         "non-200 with JSON error body",
			status:       http.StatusBadRequest,
			body:         `{"type":"error","error":{"type":"invalid_request_error","message":"max_tokens is required"}}`,
			wantErrParts: []string{"invalid_request_error", "max_tokens is required"},
		},
		{
			name:         "non-200 with non-JSON body",
			status:       http.StatusBadGateway,
			body:         "<html>bad gateway</html>",
			wantErrParts: []string{"status 502", "<html>bad gateway</html>"},
		},
		{
			name:         "200 with non-JSON body",
			status:       http.StatusOK,
			body:         "not json",
			wantErrParts: []string{"failed to parse response"},
		},
	}

	for _, tt := range tests {
		for fnName, call := range generateFuncs {
			t.Run(fnName+"/"+tt.name, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(tt.status)
					_, _ = w.Write([]byte(tt.body))
				}))
				defer server.Close()

				resp, err := call(context.Background(), newTestClient(server.URL))
				if err == nil {
					t.Fatalf("%s() error = nil, want error", fnName)
				}
				if resp != nil {
					t.Errorf("%s() response = %+v, want nil", fnName, resp)
				}
				for _, part := range tt.wantErrParts {
					if !strings.Contains(err.Error(), part) {
						t.Errorf("%s() error = %q, want it to contain %q", fnName, err, part)
					}
				}
			})
		}
	}
}

func TestAnthropicClient_RequestFailure(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(t *testing.T) (context.Context, string)
		wantIs error
	}{
		{
			name: "server closed",
			setup: func(t *testing.T) (context.Context, string) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
				url := server.URL
				server.Close()
				return context.Background(), url
			},
		},
		{
			name: "cancelled context",
			setup: func(t *testing.T) (context.Context, string) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					t.Error("request reached the server despite a cancelled context")
				}))
				t.Cleanup(server.Close)
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, server.URL
			},
			wantIs: context.Canceled,
		},
	}

	for _, tt := range tests {
		for fnName, call := range generateFuncs {
			t.Run(fnName+"/"+tt.name, func(t *testing.T) {
				ctx, url := tt.setup(t)

				_, err := call(ctx, newTestClient(url))
				if err == nil {
					t.Fatalf("%s() error = nil, want error", fnName)
				}
				if !strings.Contains(err.Error(), "failed to send request") {
					t.Errorf("%s() error = %q, want it to contain %q", fnName, err, "failed to send request")
				}
				if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
					t.Errorf("%s() error = %v, want errors.Is(err, %v)", fnName, err, tt.wantIs)
				}
			})
		}
	}
}

func TestAnthropicClient_Generate_MixedContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"id": "msg_127",
			"type": "message",
			"role": "assistant",
			"model": "claude-sonnet-4-5-20250929",
			"stop_reason": "tool_use",
			"content": [
				{"type": "text", "text": "Checking "},
				{"type": "tool_use", "id": "toolu_1", "name": "lookup", "input": {"q": "a"}},
				{"type": "text", "text": "two tools."},
				{"type": "tool_use", "id": "toolu_2", "name": "fetch", "input": {}}
			],
			"usage": {"input_tokens": 12, "output_tokens": 30}
		}`))
	}))
	defer server.Close()

	resp, err := newTestClient(server.URL).Generate(context.Background(), GenerateRequest{UserPrompt: "test", MaxTokens: 50})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if resp.Text != "Checking two tools." {
		t.Errorf("Text = %q, want %q", resp.Text, "Checking two tools.")
	}
	if resp.StopReason != "tool_use" {
		t.Errorf("StopReason = %q, want %q", resp.StopReason, "tool_use")
	}
	wantUsage := Usage{PromptTokens: 12, CompletionTokens: 30, TotalTokens: 42}
	if resp.Usage != wantUsage {
		t.Errorf("Usage = %+v, want %+v", resp.Usage, wantUsage)
	}

	wantTools := []ToolUse{
		{ID: "toolu_1", Name: "lookup", Input: map[string]interface{}{"q": "a"}},
		{ID: "toolu_2", Name: "fetch", Input: map[string]interface{}{}},
	}
	if !reflect.DeepEqual(resp.ToolUses, wantTools) {
		t.Errorf("ToolUses = %+v, want %+v", resp.ToolUses, wantTools)
	}
}

func TestAnthropicContentBlock_MarshalJSON(t *testing.T) {
	tests := []struct {
		name  string
		block anthropicContentBlock
		want  string
	}{
		{
			name:  "text only",
			block: anthropicContentBlock{Type: "text", Text: "hi"},
			want:  `{"text":"hi","type":"text"}`,
		},
		{
			name:  "tool_use with nil input emits empty object",
			block: anthropicContentBlock{Type: "tool_use", ID: "toolu_1", Name: "lookup"},
			want:  `{"id":"toolu_1","input":{},"name":"lookup","type":"tool_use"}`,
		},
		{
			name:  "tool_use with input",
			block: anthropicContentBlock{Type: "tool_use", ID: "toolu_1", Name: "lookup", Input: map[string]interface{}{"q": "a"}},
			want:  `{"id":"toolu_1","input":{"q":"a"},"name":"lookup","type":"tool_use"}`,
		},
		{
			name:  "tool_result with error",
			block: anthropicContentBlock{Type: "tool_result", ToolUseID: "toolu_1", Content: "boom", IsError: true},
			want:  `{"content":"boom","is_error":true,"tool_use_id":"toolu_1","type":"tool_result"}`,
		},
		{
			name:  "tool_result without error omits is_error",
			block: anthropicContentBlock{Type: "tool_result", ToolUseID: "toolu_1", Content: "ok"},
			want:  `{"content":"ok","tool_use_id":"toolu_1","type":"tool_result"}`,
		},
		{
			name:  "non tool_use with input keeps it",
			block: anthropicContentBlock{Type: "text", Input: map[string]interface{}{"k": "v"}},
			want:  `{"input":{"k":"v"},"type":"text"}`,
		},
		{
			name:  "non tool_use with empty input omits it",
			block: anthropicContentBlock{Type: "text", Input: map[string]interface{}{}},
			want:  `{"type":"text"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.block)
			if err != nil {
				t.Fatalf("MarshalJSON() error = %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("MarshalJSON() = %s, want %s", got, tt.want)
			}
		})
	}
}
