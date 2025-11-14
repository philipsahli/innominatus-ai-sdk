package rag

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
)

// InMemoryVectorStore is an in-memory implementation of VectorStore
type InMemoryVectorStore struct {
	mu        sync.RWMutex
	documents map[string]Document
}

// NewInMemoryVectorStore creates a new in-memory vector store
func NewInMemoryVectorStore() *InMemoryVectorStore {
	return &InMemoryVectorStore{
		documents: make(map[string]Document),
	}
}

// Add adds a document to the store
func (s *InMemoryVectorStore) Add(ctx context.Context, doc Document) error {
	if doc.ID == "" {
		return fmt.Errorf("document ID is required")
	}
	if len(doc.Embedding) == 0 {
		return fmt.Errorf("document embedding is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.documents[doc.ID] = doc
	return nil
}

// AddBatch adds multiple documents to the store
func (s *InMemoryVectorStore) AddBatch(ctx context.Context, docs []Document) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, doc := range docs {
		if doc.ID == "" {
			return fmt.Errorf("document ID is required")
		}
		if len(doc.Embedding) == 0 {
			return fmt.Errorf("document embedding is required")
		}
		s.documents[doc.ID] = doc
	}
	return nil
}

// Search finds similar documents based on query embedding
func (s *InMemoryVectorStore) Search(ctx context.Context, queryEmbedding []float32, topK int, minScore float32) ([]SearchResult, error) {
	if len(queryEmbedding) == 0 {
		return nil, fmt.Errorf("query embedding is required")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	// Calculate similarity scores for all documents
	results := make([]SearchResult, 0, len(s.documents))
	for _, doc := range s.documents {
		similarity := cosineSimilarity(queryEmbedding, doc.Embedding)
		if similarity >= minScore {
			results = append(results, SearchResult{
				Document: doc,
				Score:    similarity,
			})
		}
	}

	// Sort by similarity (highest first)
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	// Return top K results
	if topK > 0 && topK < len(results) {
		results = results[:topK]
	}

	return results, nil
}

// Get retrieves a document by ID
func (s *InMemoryVectorStore) Get(ctx context.Context, id string) (*Document, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	doc, exists := s.documents[id]
	if !exists {
		return nil, fmt.Errorf("document not found: %s", id)
	}

	return &doc, nil
}

// Delete removes a document by ID
func (s *InMemoryVectorStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.documents[id]; !exists {
		return fmt.Errorf("document not found: %s", id)
	}

	delete(s.documents, id)
	return nil
}

// Count returns the total number of documents
func (s *InMemoryVectorStore) Count(ctx context.Context) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.documents), nil
}

// cosineSimilarity calculates the cosine similarity between two vectors
// Returns a value between -1 and 1, where 1 means identical, 0 means orthogonal, -1 means opposite
func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}

	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return float32(dotProduct / (math.Sqrt(normA) * math.Sqrt(normB)))
}
