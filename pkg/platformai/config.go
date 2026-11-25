package platformai

import (
	"fmt"

	"github.com/philipsahli/innominatus-ai-sdk/pkg/model"
	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai/rag"
)

// Config holds SDK configuration. At least one of LLM and RAG is set.
type Config struct {
	LLM LLMConfig   // Optional: empty Provider means no model (RAG only)
	RAG *rag.Config // Optional RAG configuration
}

// LLMConfig selects the model backend CodeMapping uses.
type LLMConfig struct {
	Provider  string // model.ProviderAnthropic, model.ProviderOpenAI or model.ProviderOllama; "" for none
	APIKey    string // required for anthropic and openai
	Model     string // default for anthropic: DefaultAnthropicModel; required otherwise
	MaxTokens int    // cap on a request's MaxTokens; 0 means model.DefaultMaxTokens
	BaseURL   string // empty means the provider's public API (Ollama: model.DefaultOllamaBaseURL)
}

// DefaultAnthropicModel is the model of an anthropic LLMConfig without one.
const DefaultAnthropicModel = "claude-sonnet-5-5"

// Validate checks the configuration and fills in defaults.
func (c *Config) Validate() error {
	if c.LLM.Provider == "" && c.RAG == nil {
		return fmt.Errorf("%w: set an LLM provider, a RAG configuration or both", ErrInvalidConfig)
	}
	if c.RAG != nil && c.RAG.Timeout < 0 {
		return fmt.Errorf("%w: RAG timeout must not be negative", ErrInvalidConfig)
	}
	if c.LLM.MaxTokens < 0 {
		return fmt.Errorf("%w: LLM max tokens must not be negative", ErrInvalidConfig)
	}
	switch c.LLM.Provider {
	case "":
		return nil
	case model.ProviderAnthropic:
		if c.LLM.Model == "" {
			c.LLM.Model = DefaultAnthropicModel
		}
		if c.LLM.BaseURL == "" {
			c.LLM.BaseURL = model.DefaultAnthropicBaseURL
		}
	case model.ProviderOpenAI:
		if c.LLM.BaseURL == "" {
			c.LLM.BaseURL = model.DefaultOpenAIBaseURL
		}
	case model.ProviderOllama:
		if c.LLM.BaseURL == "" {
			c.LLM.BaseURL = model.DefaultOllamaBaseURL
		}
	default:
		return fmt.Errorf("%w: LLM provider %q is not anthropic, openai or ollama", ErrInvalidConfig, c.LLM.Provider)
	}
	if c.LLM.APIKey == "" && c.LLM.Provider != model.ProviderOllama {
		return fmt.Errorf("%w: LLM API key is required for %s", ErrInvalidConfig, c.LLM.Provider)
	}
	if c.LLM.Model == "" {
		return fmt.Errorf("%w: LLM model is required for %s", ErrInvalidConfig, c.LLM.Provider)
	}
	if c.LLM.MaxTokens == 0 {
		c.LLM.MaxTokens = model.DefaultMaxTokens
	}
	return nil
}
