package llm

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// recordingTransport captures the request URL and answers locally, so the
// default public endpoint can be asserted without network access.
type recordingTransport struct {
	gotURL string
}

func (rt *recordingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	rt.gotURL = r.URL.String()
	body := `{"type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    r,
	}, nil
}

func TestAnthropicClient_BaseURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		wantURL string
	}{
		{"empty uses public API", "", "https://api.anthropic.com/v1/messages"},
		{"host without trailing slash", "http://proxy.local:8080", "http://proxy.local:8080/v1/messages"},
		{"host with trailing slash", "http://proxy.local:8080/", "http://proxy.local:8080/v1/messages"},
		{"path prefix without trailing slash", "http://proxy.local/anthropic", "http://proxy.local/anthropic/v1/messages"},
		{"path prefix with trailing slash", "http://proxy.local/anthropic/", "http://proxy.local/anthropic/v1/messages"},
	}
	for _, m := range clientMethods {
		for _, tt := range tests {
			t.Run(m.name+"/"+tt.name, func(t *testing.T) {
				c := NewAnthropicClient(Config{APIKey: "k", Model: "m", MaxTokens: 10, BaseURL: tt.baseURL})
				rt := &recordingTransport{}
				c.httpClient.Transport = rt

				if err := m.call(context.Background(), c, 0, 0); err != nil {
					t.Fatalf("call failed: %v", err)
				}
				if rt.gotURL != tt.wantURL {
					t.Errorf("request URL = %q, want %q", rt.gotURL, tt.wantURL)
				}
			})
		}
	}
}
