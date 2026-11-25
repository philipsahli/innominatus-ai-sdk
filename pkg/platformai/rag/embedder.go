package rag

import (
	"context"

	"github.com/philipsahli/innominatus-ai-sdk/pkg/model"
)

// FromEmbedder adapts a model.Embedder to EmbeddingProvider, so a Module or
// Retriever can embed through a governed embedder
// (model.InstrumentEmbedder over model.GuardEmbedder).
func FromEmbedder(e model.Embedder) EmbeddingProvider { return embedderProvider{e} }

type embedderProvider struct{ e model.Embedder }

func (p embedderProvider) GenerateEmbedding(ctx context.Context, text string) ([]float32, error) {
	out, err := p.e.Embed(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	return out.Vectors[0], nil
}

func (p embedderProvider) GenerateEmbeddings(ctx context.Context, texts []string) ([][]float32, error) {
	out, err := p.e.Embed(ctx, texts)
	if err != nil {
		return nil, err
	}
	return out.Vectors, nil
}

// NewModuleWithEmbedder is NewModule with a given embedder and an in-memory
// store.
func NewModuleWithEmbedder(embedder EmbeddingProvider) *Module {
	store := NewInMemoryVectorStore()
	return &Module{embedder: embedder, store: store, retriever: NewRetriever(embedder, store)}
}
