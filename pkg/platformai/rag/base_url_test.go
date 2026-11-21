package rag

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// recordingTransport captures the request URL and answers locally, so the
// default public endpoints can be asserted without network access.
type recordingTransport struct {
	gotURL string
}

func (rt *recordingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	rt.gotURL = r.URL.String()
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"data":[{"embedding":[1,0]}]}`)),
		Request:    r,
	}, nil
}

func TestNewEmbeddingProvider_BaseURL(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		baseURL  string
		wantURL  string
	}{
		{"voyage empty uses public API", "voyageai", "", "https://api.voyageai.com/v1/embeddings"},
		{"voyage without trailing slash", "voyageai", "http://proxy.local/voyage", "http://proxy.local/voyage/v1/embeddings"},
		{"voyage with trailing slash", "voyageai", "http://proxy.local/voyage/", "http://proxy.local/voyage/v1/embeddings"},
		{"openai empty uses public API", "openai", "", "https://api.openai.com/v1/embeddings"},
		{"openai without trailing slash", "openai", "http://proxy.local:9000", "http://proxy.local:9000/v1/embeddings"},
		{"openai with trailing slash", "openai", "http://proxy.local:9000/", "http://proxy.local:9000/v1/embeddings"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := NewEmbeddingProvider(Config{EmbeddingProvider: tt.provider, APIKey: "k", BaseURL: tt.baseURL})
			if err != nil {
				t.Fatalf("NewEmbeddingProvider: %v", err)
			}
			rt := &recordingTransport{}
			switch c := p.(type) {
			case *VoyageEmbeddingClient:
				c.httpClient.Transport = rt
			case *OpenAIEmbeddingClient:
				c.httpClient.Transport = rt
			default:
				t.Fatalf("unexpected provider type %T", p)
			}

			if _, err := p.GenerateEmbedding(context.Background(), "hello"); err != nil {
				t.Fatalf("GenerateEmbedding: %v", err)
			}
			if rt.gotURL != tt.wantURL {
				t.Errorf("request URL = %q, want %q", rt.gotURL, tt.wantURL)
			}
		})
	}
}
