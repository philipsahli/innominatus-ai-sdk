package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// sentPayload is the part of the request body these tests inspect.
type sentPayload struct {
	MaxTokens   int      `json:"max_tokens"`
	Temperature *float32 `json:"temperature"`
}

func newCapturingServer(t *testing.T, got *sentPayload, calls *int32) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(calls, 1)
		if err := json.NewDecoder(r.Body).Decode(got); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	t.Cleanup(server.Close)
	return server
}

type callFunc func(ctx context.Context, c *AnthropicClient, temperature float32, maxTokens int) error

var clientMethods = []struct {
	name string
	call callFunc
}{
	{"Generate", func(ctx context.Context, c *AnthropicClient, temperature float32, maxTokens int) error {
		_, err := c.Generate(ctx, GenerateRequest{UserPrompt: "hi", Temperature: temperature, MaxTokens: maxTokens})
		return err
	}},
	{"GenerateWithContext", func(ctx context.Context, c *AnthropicClient, temperature float32, maxTokens int) error {
		_, err := c.GenerateWithContext(ctx, GenerateRequest{UserPrompt: "hi", Temperature: temperature, MaxTokens: maxTokens}, "context")
		return err
	}},
	{"GenerateWithTools", func(ctx context.Context, c *AnthropicClient, temperature float32, maxTokens int) error {
		_, err := c.GenerateWithTools(ctx, GenerateWithToolsRequest{
			Messages:    []Message{{Role: "user", Content: []ContentBlock{{Type: "text", Text: "hi"}}}},
			Temperature: temperature,
			MaxTokens:   maxTokens,
		})
		return err
	}},
}

func TestAnthropicClient_ConfigDefaults(t *testing.T) {
	cases := []struct {
		name            string
		reqTemperature  float32
		reqMaxTokens    int
		wantTemperature float32
		wantMaxTokens   int
	}{
		{"zero request values use config", 0, 0, 0.3, 4096},
		{"non-zero request values win", 0.9, 128, 0.9, 128},
		{"only max tokens set in request", 0, 256, 0.3, 256},
		{"only temperature set in request", 0.7, 0, 0.7, 4096},
	}

	for _, m := range clientMethods {
		for _, tc := range cases {
			t.Run(m.name+"/"+tc.name, func(t *testing.T) {
				var got sentPayload
				var calls int32
				server := newCapturingServer(t, &got, &calls)

				c := NewAnthropicClient(Config{APIKey: "test-key", Model: "m", Temperature: 0.3, MaxTokens: 4096})
				c.baseURL = server.URL

				if err := m.call(context.Background(), c, tc.reqTemperature, tc.reqMaxTokens); err != nil {
					t.Fatalf("call failed: %v", err)
				}
				if got.MaxTokens != tc.wantMaxTokens {
					t.Errorf("max_tokens = %d, want %d", got.MaxTokens, tc.wantMaxTokens)
				}
				if got.Temperature == nil {
					t.Fatalf("temperature not sent, want %v", tc.wantTemperature)
				}
				if *got.Temperature != tc.wantTemperature {
					t.Errorf("temperature = %v, want %v", *got.Temperature, tc.wantTemperature)
				}
			})
		}
	}
}

func TestAnthropicClient_ZeroMaxTokensEverywhere(t *testing.T) {
	for _, m := range clientMethods {
		t.Run(m.name, func(t *testing.T) {
			var got sentPayload
			var calls int32
			server := newCapturingServer(t, &got, &calls)

			c := NewAnthropicClient(Config{APIKey: "test-key", Model: "m"})
			c.baseURL = server.URL

			if err := m.call(context.Background(), c, 0, 0); err == nil {
				t.Fatal("expected error for max tokens 0 in request and config, got nil")
			}
			if n := atomic.LoadInt32(&calls); n != 0 {
				t.Errorf("server received %d requests, want 0", n)
			}
		})
	}
}
