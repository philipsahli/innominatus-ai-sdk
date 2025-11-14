package rag

import (
	"context"
	"fmt"
	"strings"
)

// Retriever handles document retrieval and context formatting
type Retriever struct {
	embedder EmbeddingProvider
	store    VectorStore
}

// NewRetriever creates a new retriever
func NewRetriever(embedder EmbeddingProvider, store VectorStore) *Retriever {
	return &Retriever{
		embedder: embedder,
		store:    store,
	}
}

// Retrieve retrieves relevant documents for a query
func (r *Retriever) Retrieve(ctx context.Context, req RetrieveRequest) (*RetrieveResponse, error) {
	// Set defaults
	if req.TopK <= 0 {
		req.TopK = 3
	}

	// Generate query embedding
	queryEmbedding, err := r.embedder.GenerateEmbedding(ctx, req.Query)
	if err != nil {
		return nil, fmt.Errorf("failed to generate query embedding: %w", err)
	}

	// Search for similar documents
	results, err := r.store.Search(ctx, queryEmbedding, req.TopK, req.MinScore)
	if err != nil {
		return nil, fmt.Errorf("failed to search documents: %w", err)
	}

	// Format context for LLM
	context := r.formatContext(results)

	return &RetrieveResponse{
		Results:        results,
		Context:        context,
		QueryEmbedding: queryEmbedding,
	}, nil
}

// formatContext formats search results into a context string for the LLM
func (r *Retriever) formatContext(results []SearchResult) string {
	if len(results) == 0 {
		return ""
	}

	var builder strings.Builder
	builder.WriteString("Relevant context from knowledge base:\n\n")

	for i, result := range results {
		builder.WriteString(fmt.Sprintf("--- Document %d (Relevance: %.2f) ---\n", i+1, result.Score))

		// Add metadata if available
		if len(result.Document.Metadata) > 0 {
			if title, ok := result.Document.Metadata["title"]; ok {
				builder.WriteString(fmt.Sprintf("Title: %s\n", title))
			}
			if source, ok := result.Document.Metadata["source"]; ok {
				builder.WriteString(fmt.Sprintf("Source: %s\n", source))
			}
		}

		builder.WriteString("\n")
		builder.WriteString(result.Document.Content)
		builder.WriteString("\n\n")
	}

	return builder.String()
}

// AddDocument adds a document to the retriever's store with automatic embedding
func (r *Retriever) AddDocument(ctx context.Context, id, content string, metadata map[string]string) error {
	// Generate embedding for the document
	embedding, err := r.embedder.GenerateEmbedding(ctx, content)
	if err != nil {
		return fmt.Errorf("failed to generate embedding: %w", err)
	}

	// Create document
	doc := Document{
		ID:        id,
		Content:   content,
		Metadata:  metadata,
		Embedding: embedding,
	}

	// Add to store
	if err := r.store.Add(ctx, doc); err != nil {
		return fmt.Errorf("failed to add document: %w", err)
	}

	return nil
}

// AddDocuments adds multiple documents with automatic embedding
func (r *Retriever) AddDocuments(ctx context.Context, docs []struct {
	ID       string
	Content  string
	Metadata map[string]string
}) error {
	// Extract content for batch embedding
	contents := make([]string, len(docs))
	for i, doc := range docs {
		contents[i] = doc.Content
	}

	// Generate embeddings in batch
	embeddings, err := r.embedder.GenerateEmbeddings(ctx, contents)
	if err != nil {
		return fmt.Errorf("failed to generate embeddings: %w", err)
	}

	// Create documents with embeddings
	documents := make([]Document, len(docs))
	for i, doc := range docs {
		documents[i] = Document{
			ID:        doc.ID,
			Content:   doc.Content,
			Metadata:  doc.Metadata,
			Embedding: embeddings[i],
		}
	}

	// Add to store
	if err := r.store.AddBatch(ctx, documents); err != nil {
		return fmt.Errorf("failed to add documents: %w", err)
	}

	return nil
}
