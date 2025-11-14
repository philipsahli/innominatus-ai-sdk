package platformai

import (
	"fmt"

	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai/rag"
)

// Config holds SDK configuration
type Config struct {
	LLM LLMConfig
	RAG *rag.Config // Optional RAG configuration
}

// LLMConfig holds LLM provider configuration
type LLMConfig struct {
	Provider    string // "anthropic"
	APIKey      string
	Model       string  // "claude-sonnet-4-5-20250929"
	Temperature float32 // default: 0.3
	MaxTokens   int     // default: 4096
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.LLM.Provider == "" {
		return fmt.Errorf("%w: LLM provider is required", ErrInvalidConfig)
	}
	if c.LLM.APIKey == "" {
		return fmt.Errorf("%w: LLM API key is required", ErrInvalidConfig)
	}

	// Set defaults
	if c.LLM.Model == "" {
		c.LLM.Model = "claude-sonnet-4-5-20250929"
	}
	if c.LLM.Temperature == 0 {
		c.LLM.Temperature = 0.3
	}
	if c.LLM.MaxTokens == 0 {
		c.LLM.MaxTokens = 4096
	}

	return nil
}
