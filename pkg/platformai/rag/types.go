package rag

import (
	"context"
	"time"
)

// Document represents a document stored in the RAG system
type Document struct {
	ID        string            // Unique identifier
	Content   string            // Document content
	Metadata  map[string]string // Optional metadata (e.g., source, title, category)
	Embedding []float32         // Vector embedding; Module.AddDocuments keeps a non-empty one instead of embedding Content
}

// Query represents a search query
type Query struct {
	Text     string  // Query text
	TopK     int     // Number of results to return
	MinScore float32 // Minimum similarity score threshold (-1 to 1)
}

// SearchResult represents a document with similarity score
type SearchResult struct {
	Document Document
	Score    float32 // Cosine similarity score (-1 to 1)
}

// EmbeddingProvider defines the interface for generating embeddings
type EmbeddingProvider interface {
	// GenerateEmbedding generates an embedding vector for the given text
	GenerateEmbedding(ctx context.Context, text string) ([]float32, error)

	// GenerateEmbeddings generates embedding vectors for multiple texts
	GenerateEmbeddings(ctx context.Context, texts []string) ([][]float32, error)
}

// VectorStore defines the interface for storing and searching documents
type VectorStore interface {
	// Add adds a document to the store
	Add(ctx context.Context, doc Document) error

	// AddBatch adds multiple documents to the store
	AddBatch(ctx context.Context, docs []Document) error

	// Search finds similar documents based on query embedding
	Search(ctx context.Context, queryEmbedding []float32, topK int, minScore float32) ([]SearchResult, error)

	// Get retrieves a document by ID
	Get(ctx context.Context, id string) (*Document, error)

	// Delete removes a document by ID
	Delete(ctx context.Context, id string) error

	// Count returns the total number of documents
	Count(ctx context.Context) (int, error)
}

// Config holds RAG module configuration
type Config struct {
	EmbeddingProvider string        // Provider for embeddings ("voyageai" or "voyage", "openai")
	APIKey            string        // API key for embedding provider
	Model             string        // Model name for embeddings
	EmbeddingDim      int           // Embedding dimension
	Timeout           time.Duration // HTTP client timeout per call; 0 means 30s
	BaseURL           string        // scheme+host and optional path prefix, without /v1/embeddings; empty means the provider's public API
}

// RetrieveRequest represents a request to retrieve relevant documents
type RetrieveRequest struct {
	Query    string  // Query text
	TopK     int     // Number of documents to retrieve (default: 3)
	MinScore float32 // Minimum similarity score (default: 0.0)
}

// RetrieveResponse represents retrieved documents with context
type RetrieveResponse struct {
	Results        []SearchResult // Retrieved documents with scores
	Context        string         // Formatted context for LLM
	QueryEmbedding []float32      // Embedding of the query
}
