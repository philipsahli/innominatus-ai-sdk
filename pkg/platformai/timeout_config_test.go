package platformai_test

import (
	"errors"
	"testing"
	"time"

	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai"
	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai/rag"
)

func TestConfigValidate_Timeout(t *testing.T) {
	tests := []struct {
		name       string
		llmTimeout time.Duration
		rag        *rag.Config
		wantErr    bool
	}{
		{"zero timeouts are valid", 0, &rag.Config{EmbeddingProvider: "openai"}, false},
		{"positive timeouts are valid", time.Second, &rag.Config{EmbeddingProvider: "openai", Timeout: time.Second}, false},
		{"negative LLM timeout", -time.Second, nil, true},
		{"negative RAG timeout", 0, &rag.Config{EmbeddingProvider: "openai", Timeout: -time.Second}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &platformai.Config{
				LLM: platformai.LLMConfig{Provider: "anthropic", APIKey: "k", Timeout: tt.llmTimeout},
				RAG: tt.rag,
			}
			err := cfg.Validate()
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, platformai.ErrInvalidConfig) {
				t.Errorf("Validate() = %v, want ErrInvalidConfig", err)
			}
		})
	}
}
