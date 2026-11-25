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

// NewModuleWithEmbedders builds a Module on an in-memory store with one
// embedder for documents and one for queries, so the two can be governed
// apart (for example, only queries count against a user's budget). A nil
// query embedder means documents and queries share index.
func NewModuleWithEmbedders(index, query EmbeddingProvider) *Module {
	store := NewInMemoryVectorStore()
	if query == nil {
		query = index
	}
	return &Module{embedder: query, store: store, retriever: NewRetriever(query, store), indexer: NewRetriever(index, store)}
}
