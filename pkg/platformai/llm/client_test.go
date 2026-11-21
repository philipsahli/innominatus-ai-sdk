package llm

import (
	"strings"
	"testing"
)

func TestNewClient(t *testing.T) {
	tests := []struct {
		name        string
		provider    string
		wantErr     bool
		wantErrText string
	}{
		{name: "anthropic", provider: "anthropic"},
		{name: "unknown provider", provider: "openai", wantErr: true, wantErrText: "openai"},
		{name: "empty provider", provider: "", wantErr: true, wantErrText: "unsupported LLM provider"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(Config{Provider: tt.provider, APIKey: "test-key", Model: "test-model"})

			if tt.wantErr {
				if err == nil {
					t.Fatalf("NewClient() error = nil, want error")
				}
				if !strings.Contains(err.Error(), tt.wantErrText) {
					t.Errorf("NewClient() error = %q, want it to contain %q", err, tt.wantErrText)
				}
				if client != nil {
					t.Errorf("NewClient() client = %v, want nil", client)
				}
				return
			}

			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}
			if _, ok := client.(*AnthropicClient); !ok {
				t.Errorf("NewClient() returned %T, want *AnthropicClient", client)
			}
		})
	}
}
