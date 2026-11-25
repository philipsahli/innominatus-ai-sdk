package platformai_test

import (
	"context"
	"errors"
	"testing"

	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai"
	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai/rag"
)

// The constructors only store keys and build clients; no request is sent,
// so fake keys are enough and the tests stay offline.
func validLLMConfig() platformai.LLMConfig {
	return platformai.LLMConfig{Provider: "anthropic", APIKey: "test-key"}
}

func TestNew(t *testing.T) {
	tests := []struct {
		name       string
		config     *platformai.Config
		wantErr    bool
		wantErrIs  error
		wantRAGSet bool
	}{
		{
			name:      "nil config",
			config:    nil,
			wantErr:   true,
			wantErrIs: platformai.ErrInvalidConfig,
		},
		{
			name:      "invalid config",
			config:    &platformai.Config{LLM: platformai.LLMConfig{Provider: "anthropic"}},
			wantErr:   true,
			wantErrIs: platformai.ErrInvalidConfig,
		},
		{
			name: "unsupported LLM provider",
			config: &platformai.Config{
				LLM: platformai.LLMConfig{Provider: "unknown", APIKey: "test-key"},
			},
			wantErr: true,
		},
		{
			name:   "without RAG",
			config: &platformai.Config{LLM: validLLMConfig()},
		},
		{
			name: "with RAG",
			config: &platformai.Config{
				LLM: validLLMConfig(),
				RAG: &rag.Config{EmbeddingProvider: "openai", APIKey: "test-key"},
			},
			wantRAGSet: true,
		},
		{
			name: "RAG with unsupported embedding provider",
			config: &platformai.Config{
				LLM: validLLMConfig(),
				RAG: &rag.Config{EmbeddingProvider: "unknown", APIKey: "test-key"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sdk, err := platformai.New(context.Background(), tt.config)

			if tt.wantErr {
				if err == nil {
					t.Fatal("New() error = nil, want error")
				}
				if tt.wantErrIs != nil && !errors.Is(err, tt.wantErrIs) {
					t.Errorf("New() error = %v, want errors.Is %v", err, tt.wantErrIs)
				}
				if sdk != nil {
					t.Errorf("New() SDK = %v, want nil on error", sdk)
				}
				return
			}
			if err != nil {
				t.Fatalf("New() unexpected error: %v", err)
			}
			if sdk.Model() == nil {
				t.Error("Model() = nil, want a model")
			}
			if sdk.CodeMapping() == nil {
				t.Error("CodeMapping() = nil, want module")
			}
			if gotRAG := sdk.RAG() != nil; gotRAG != tt.wantRAGSet {
				t.Errorf("RAG() set = %v, want %v", gotRAG, tt.wantRAGSet)
			}
		})
	}
}
