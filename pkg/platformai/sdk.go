package platformai

import (
	"context"
	"fmt"

	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai/codemapping"
	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai/llm"
	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai/rag"
)

// SDK is the main entry point for the Platform AI SDK
type SDK struct {
	config    *Config
	llmClient llm.Client
	ragModule *rag.Module
}

// New creates a new SDK instance
func New(ctx context.Context, config *Config) (*SDK, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Initialize LLM client
	llmClient, err := llm.NewClient(llm.Config{
		Provider:    config.LLM.Provider,
		APIKey:      config.LLM.APIKey,
		Model:       config.LLM.Model,
		Temperature: config.LLM.Temperature,
		MaxTokens:   config.LLM.MaxTokens,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create LLM client: %w", err)
	}

	// Initialize RAG module if configured
	var ragModule *rag.Module
	if config.RAG != nil {
		ragModule, err = rag.NewModule(*config.RAG)
		if err != nil {
			return nil, fmt.Errorf("failed to create RAG module: %w", err)
		}
	}

	return &SDK{
		config:    config,
		llmClient: llmClient,
		ragModule: ragModule,
	}, nil
}

// CodeMapping returns the code mapping module
func (s *SDK) CodeMapping() *codemapping.Module {
	return codemapping.NewModule(s.llmClient)
}

// RAG returns the RAG module
func (s *SDK) RAG() *rag.Module {
	return s.ragModule
}

// LLM returns the LLM client
func (s *SDK) LLM() llm.Client {
	return s.llmClient
}
