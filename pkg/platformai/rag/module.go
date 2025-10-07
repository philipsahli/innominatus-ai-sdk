package rag

import (
	"context"
	"fmt"
)

// Module provides RAG functionality
type Module struct {
	config    Config
	embedder  EmbeddingProvider
	store     VectorStore
	retriever *Retriever
}

// NewModule creates a new RAG module
func NewModule(config Config) (*Module, error) {
	// Create embedding provider
	embedder, err := NewEmbeddingProvider(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create embedding provider: %w", err)
	}

	// Create vector store
	store := NewInMemoryVectorStore()

	// Create retriever
	retriever := NewRetriever(embedder, store)

	return &Module{
		config:    config,
		embedder:  embedder,
		store:     store,
		retriever: retriever,
	}, nil
}

// AddDocument adds a single document to the knowledge base
func (m *Module) AddDocument(ctx context.Context, id, content string, metadata map[string]string) error {
	return m.retriever.AddDocument(ctx, id, content, metadata)
}

// AddDocuments adds multiple documents to the knowledge base
func (m *Module) AddDocuments(ctx context.Context, docs []struct {
	ID       string
	Content  string
	Metadata map[string]string
}) error {
	return m.retriever.AddDocuments(ctx, docs)
}

// Retrieve retrieves relevant documents for a query
func (m *Module) Retrieve(ctx context.Context, req RetrieveRequest) (*RetrieveResponse, error) {
	return m.retriever.Retrieve(ctx, req)
}

// Query retrieves documents and returns formatted context
func (m *Module) Query(ctx context.Context, query string, topK int) (string, error) {
	resp, err := m.Retrieve(ctx, RetrieveRequest{
		Query: query,
		TopK:  topK,
	})
	if err != nil {
		return "", err
	}
	return resp.Context, nil
}

// GetDocument retrieves a document by ID
func (m *Module) GetDocument(ctx context.Context, id string) (*Document, error) {
	return m.store.Get(ctx, id)
}

// DeleteDocument removes a document by ID
func (m *Module) DeleteDocument(ctx context.Context, id string) error {
	return m.store.Delete(ctx, id)
}

// Count returns the total number of documents in the knowledge base
func (m *Module) Count(ctx context.Context) (int, error) {
	return m.store.Count(ctx)
}
