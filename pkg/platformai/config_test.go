package platformai_test

import (
	"errors"
	"testing"

	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai"
	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai/rag"
)

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     platformai.Config
		want    platformai.LLMConfig
		wantErr bool
	}{
		{name: "nothing configured", wantErr: true},
		{name: "RAG only needs no model", cfg: platformai.Config{RAG: &rag.Config{}}},
		{name: "anthropic without key", cfg: platformai.Config{LLM: platformai.LLMConfig{Provider: "anthropic"}}, wantErr: true},
		{name: "unknown provider", cfg: platformai.Config{LLM: platformai.LLMConfig{Provider: "mistral", APIKey: "k"}}, wantErr: true},
		{name: "openai without model", cfg: platformai.Config{LLM: platformai.LLMConfig{Provider: "openai", APIKey: "k"}}, wantErr: true},
		{name: "negative max tokens", cfg: platformai.Config{LLM: platformai.LLMConfig{Provider: "ollama", Model: "m", MaxTokens: -1}}, wantErr: true},
		{
			name: "anthropic defaults",
			cfg:  platformai.Config{LLM: platformai.LLMConfig{Provider: "anthropic", APIKey: "k"}},
			want: platformai.LLMConfig{Provider: "anthropic", APIKey: "k", Model: platformai.DefaultAnthropicModel, MaxTokens: 4096, BaseURL: "https://api.anthropic.com"},
		},
		{
			name: "ollama needs no key",
			cfg:  platformai.Config{LLM: platformai.LLMConfig{Provider: "ollama", Model: "llama3.1:8b"}},
			want: platformai.LLMConfig{Provider: "ollama", Model: "llama3.1:8b", MaxTokens: 4096, BaseURL: "http://localhost:11434"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.cfg
			err := cfg.Validate()
			if tt.wantErr {
				if !errors.Is(err, platformai.ErrInvalidConfig) {
					t.Fatalf("Validate() = %v, want ErrInvalidConfig", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Validate() = %v", err)
			}
			if cfg.LLM != tt.want {
				t.Errorf("LLM = %+v, want %+v", cfg.LLM, tt.want)
			}
		})
	}
}
