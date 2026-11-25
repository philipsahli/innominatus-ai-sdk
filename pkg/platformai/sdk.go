package platformai

import (
	"context"
	"fmt"

	"github.com/philipsahli/innominatus-ai-sdk/pkg/model"
	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai/codemapping"
	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai/rag"
)

// SDK is the main entry point for the Platform AI SDK
type SDK struct {
	config    *Config
	model     model.Model
	ragModule *rag.Module
}

// New creates a new SDK instance. The model, when configured, is wrapped by
// model.Instrument, so every prompt is redacted before it leaves.
func New(ctx context.Context, config *Config) (*SDK, error) {
	if config == nil {
		return nil, fmt.Errorf("invalid config: %w: config is nil", ErrInvalidConfig)
	}
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	var m model.Model
	switch config.LLM.Provider {
	case model.ProviderAnthropic:
		m = model.NewAnthropic(config.LLM.APIKey, config.LLM.BaseURL, config.LLM.Model)
	case model.ProviderOpenAI:
		m = model.NewOpenAI(config.LLM.APIKey, config.LLM.BaseURL, config.LLM.Model)
	case model.ProviderOllama:
		m = model.NewOllama(config.LLM.BaseURL, config.LLM.Model)
	}
	if m != nil {
		m = model.Instrument(m, "platformai")
	}

	var ragModule *rag.Module
	if config.RAG != nil {
		var err error
		ragModule, err = rag.NewModule(*config.RAG)
		if err != nil {
			return nil, fmt.Errorf("failed to create RAG module: %w", err)
		}
	}

	return &SDK{config: config, model: m, ragModule: ragModule}, nil
}

// CodeMapping returns the code mapping module. Without a configured model
// its Analyze fails.
func (s *SDK) CodeMapping() *codemapping.Module {
	return codemapping.NewModule(s.model)
}

// RAG returns the RAG module, nil when not configured.
func (s *SDK) RAG() *rag.Module {
	return s.ragModule
}

// Model returns the configured model, nil when LLM.Provider is empty.
func (s *SDK) Model() model.Model {
	return s.model
}
