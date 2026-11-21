package rag

import (
	"net/http"
	"testing"
	"time"
)

func TestNewEmbeddingProvider_Timeout(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		timeout  time.Duration
		want     time.Duration
	}{
		{"voyage zero uses 30s default", "voyageai", 0, 30 * time.Second},
		{"voyage configured value is used", "voyageai", 5 * time.Second, 5 * time.Second},
		{"openai zero uses 30s default", "openai", 0, 30 * time.Second},
		{"openai configured value is used", "openai", 7 * time.Second, 7 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := NewEmbeddingProvider(Config{EmbeddingProvider: tt.provider, APIKey: "k", Timeout: tt.timeout})
			if err != nil {
				t.Fatalf("NewEmbeddingProvider: %v", err)
			}
			var hc *http.Client
			switch c := p.(type) {
			case *VoyageEmbeddingClient:
				hc = c.httpClient
			case *OpenAIEmbeddingClient:
				hc = c.httpClient
			default:
				t.Fatalf("unexpected provider type %T", p)
			}
			if hc.Timeout != tt.want {
				t.Errorf("httpClient.Timeout = %v, want %v", hc.Timeout, tt.want)
			}
		})
	}
}
