package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newSlowServer answers only after delay. The handler is released at cleanup
// so server.Close does not wait out the full delay.
func newSlowServer(t *testing.T, delay time.Duration) *httptest.Server {
	t.Helper()
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(delay):
		case <-release:
		}
	}))
	// Cleanups run last-in first-out: release the handler, then close.
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(release) })
	return server
}

func TestNewAnthropicClient_Timeout(t *testing.T) {
	tests := []struct {
		name    string
		timeout time.Duration
		want    time.Duration
	}{
		{"zero uses 60s default", 0, 60 * time.Second},
		{"configured value is used", 5 * time.Second, 5 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewAnthropicClient(Config{APIKey: "k", Model: "m", MaxTokens: 10, Timeout: tt.timeout})
			if c.httpClient.Timeout != tt.want {
				t.Errorf("httpClient.Timeout = %v, want %v", c.httpClient.Timeout, tt.want)
			}
		})
	}
}

func TestAnthropicClient_ConfiguredTimeoutEndsCall(t *testing.T) {
	server := newSlowServer(t, 5*time.Second)

	c := NewAnthropicClient(Config{APIKey: "k", Model: "m", MaxTokens: 10, Timeout: 50 * time.Millisecond})
	c.baseURL = server.URL

	start := time.Now()
	_, err := c.Generate(context.Background(), GenerateRequest{UserPrompt: "hi"})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("call took %v, want it to end near the 50ms timeout", elapsed)
	}
}

func TestAnthropicClient_ContextDeadlineShorterThanTimeout(t *testing.T) {
	server := newSlowServer(t, 5*time.Second)

	c := NewAnthropicClient(Config{APIKey: "k", Model: "m", MaxTokens: 10, Timeout: 30 * time.Second})
	c.baseURL = server.URL

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := c.Generate(ctx, GenerateRequest{UserPrompt: "hi"})
	if err == nil {
		t.Fatal("expected context deadline error, got nil")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("call took %v, want it to end near the 50ms context deadline", elapsed)
	}
}
