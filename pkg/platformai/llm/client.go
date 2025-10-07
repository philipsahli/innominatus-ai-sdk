package llm

import (
	"context"
	"fmt"
)

// Client is the interface for LLM providers
type Client interface {
	Generate(ctx context.Context, req GenerateRequest) (*GenerateResponse, error)
	GenerateWithContext(ctx context.Context, req GenerateRequest, additionalContext string) (*GenerateResponse, error)
	GenerateWithTools(ctx context.Context, req GenerateWithToolsRequest) (*GenerateResponse, error)
}

// NewClient creates a new LLM client based on config
func NewClient(config Config) (Client, error) {
	switch config.Provider {
	case "anthropic":
		return NewAnthropicClient(config), nil
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %s", config.Provider)
	}
}