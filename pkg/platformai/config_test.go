package platformai_test

import (
	"errors"
	"testing"

	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai"
)

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		in      platformai.LLMConfig
		want    platformai.LLMConfig
		wantErr error
	}{
		{
			name:    "missing provider",
			in:      platformai.LLMConfig{APIKey: "test-key"},
			wantErr: platformai.ErrInvalidConfig,
		},
		{
			name:    "missing API key",
			in:      platformai.LLMConfig{Provider: "anthropic"},
			wantErr: platformai.ErrInvalidConfig,
		},
		{
			// A zero temperature is replaced by 0.3, so callers cannot ask for 0.
			// This pins today's behaviour; #26 tracks changing it.
			name: "defaults applied to zero fields",
			in:   platformai.LLMConfig{Provider: "anthropic", APIKey: "test-key"},
			want: platformai.LLMConfig{
				Provider:    "anthropic",
				APIKey:      "test-key",
				Model:       "claude-sonnet-4-5-20250929",
				Temperature: 0.3,
				MaxTokens:   4096,
			},
		},
		{
			name: "explicit values preserved",
			in: platformai.LLMConfig{
				Provider:    "anthropic",
				APIKey:      "test-key",
				Model:       "custom-model",
				Temperature: 0.9,
				MaxTokens:   1024,
			},
			want: platformai.LLMConfig{
				Provider:    "anthropic",
				APIKey:      "test-key",
				Model:       "custom-model",
				Temperature: 0.9,
				MaxTokens:   1024,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &platformai.Config{LLM: tt.in}
			err := cfg.Validate()

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Validate() error = %v, want errors.Is %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Validate() unexpected error: %v", err)
			}
			if cfg.LLM != tt.want {
				t.Errorf("Validate() LLM = %+v, want %+v", cfg.LLM, tt.want)
			}
		})
	}
}
