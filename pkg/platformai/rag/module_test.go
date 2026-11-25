package rag

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func newTestModule(embedder EmbeddingProvider) *Module {
	store := NewInMemoryVectorStore()
	return &Module{
		embedder:  embedder,
		store:     store,
		retriever: NewRetriever(embedder, store),
	}
}

func TestNewModule(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		wantErr  bool
	}{
		{name: "voyageai", provider: "voyageai"},
		{name: "openai", provider: "openai"},
		{name: "unknown provider", provider: "unknown", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := NewModule(Config{EmbeddingProvider: tt.provider, APIKey: "test-key"})
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), "failed to create embedding provider") {
					t.Fatalf("NewModule() error = %v, want wrapped provider error", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewModule() error = %v", err)
			}
			if m.embedder == nil || m.store == nil || m.retriever == nil {
				t.Errorf("NewModule() left fields nil: %+v", m)
			}
			if n, err := m.Count(context.Background()); err != nil || n != 0 {
				t.Errorf("Count() = %d, %v, want 0", n, err)
			}
		})
	}
}

func TestModuleDocumentLifecycle(t *testing.T) {
	ctx := context.Background()
	m := newTestModule(&fakeEmbedder{vectors: testVectors()})

	if err := m.AddDocument(ctx, "a", "a", map[string]string{"title": "A"}); err != nil {
		t.Fatalf("AddDocument() error = %v", err)
	}
	if err := m.AddDocuments(ctx, []Document{{ID: "b", Content: "b"}, {ID: "d", Content: "d"}}); err != nil {
		t.Fatalf("AddDocuments() error = %v", err)
	}
	if n, _ := m.Count(ctx); n != 3 {
		t.Fatalf("Count() = %d, want 3", n)
	}

	doc, err := m.GetDocument(ctx, "a")
	if err != nil || doc.Metadata["title"] != "A" {
		t.Fatalf("GetDocument() = %+v, %v", doc, err)
	}

	resp, err := m.Retrieve(ctx, RetrieveRequest{Query: "query", TopK: 2})
	if err != nil {
		t.Fatalf("Retrieve() error = %v", err)
	}
	if got := strings.Join(resultIDs(resp.Results), ","); got != "a,b" {
		t.Errorf("Retrieve() IDs = %s, want a,b", got)
	}

	text, err := m.Query(ctx, "query", 1)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if !strings.Contains(text, "Title: A") || strings.Contains(text, "Document 2") {
		t.Errorf("Query() context = %q, want only document a", text)
	}

	if err := m.DeleteDocument(ctx, "a"); err != nil {
		t.Fatalf("DeleteDocument() error = %v", err)
	}
	if _, err := m.GetDocument(ctx, "a"); err == nil {
		t.Error("GetDocument() after delete succeeded, want error")
	}
	if err := m.DeleteDocument(ctx, "a"); err == nil {
		t.Error("second DeleteDocument() succeeded, want error")
	}
}

func TestModuleErrors(t *testing.T) {
	tests := []struct {
		name string
		call func(m *Module) error
	}{
		{name: "AddDocument", call: func(m *Module) error {
			return m.AddDocument(context.Background(), "a", "a", nil)
		}},
		{name: "AddDocuments", call: func(m *Module) error {
			return m.AddDocuments(context.Background(), []Document{{ID: "a", Content: "a"}})
		}},
		{name: "Retrieve", call: func(m *Module) error {
			_, err := m.Retrieve(context.Background(), RetrieveRequest{Query: "query"})
			return err
		}},
		{name: "Query", call: func(m *Module) error {
			_, err := m.Query(context.Background(), "query", 1)
			return err
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModule(&fakeEmbedder{vectors: testVectors(), err: errBoom})
			if err := tt.call(m); !errors.Is(err, errBoom) {
				t.Errorf("%s() error = %v, want %v", tt.name, err, errBoom)
			}
		})
	}
}

func TestModuleAddDocumentsKeepsCallerEmbedding(t *testing.T) {
	ctx := context.Background()
	embedder := &fakeEmbedder{vectors: testVectors()}
	m := newTestModule(embedder)
	docs := []Document{
		{ID: "pre", Content: "not in testVectors", Embedding: []float32{-1, 0}},
		{ID: "b", Content: "b"},
		{ID: "d", Content: "d"},
	}
	if err := m.AddDocuments(ctx, docs); err != nil {
		t.Fatalf("AddDocuments() error = %v", err)
	}

	if len(embedder.batchCalls) != 1 || strings.Join(embedder.batchCalls[0], ",") != "b,d" {
		t.Errorf("GenerateEmbeddings calls = %v, want one call with [b d]", embedder.batchCalls)
	}
	pre, err := m.GetDocument(ctx, "pre")
	if err != nil || len(pre.Embedding) != 2 || pre.Embedding[0] != -1 {
		t.Errorf("GetDocument(pre) = %+v, %v, want caller embedding [-1 0]", pre, err)
	}

	resp, err := m.Retrieve(ctx, RetrieveRequest{Query: "query", TopK: 3, MinScore: -1})
	if err != nil {
		t.Fatalf("Retrieve() error = %v", err)
	}
	if got := strings.Join(resultIDs(resp.Results), ","); got != "b,d,pre" {
		t.Errorf("Retrieve() IDs = %s, want b,d,pre", got)
	}
}

func TestModuleAddDocumentsAllPreEmbeddedSkipsProvider(t *testing.T) {
	ctx := context.Background()
	embedder := &fakeEmbedder{vectors: testVectors()}
	m := newTestModule(embedder)
	if err := m.AddDocuments(ctx, []Document{{ID: "a", Content: "a", Embedding: []float32{1, 0}}}); err != nil {
		t.Fatalf("AddDocuments() error = %v", err)
	}
	if len(embedder.batchCalls) != 0 {
		t.Errorf("GenerateEmbeddings calls = %v, want none", embedder.batchCalls)
	}
	if n, _ := m.Count(ctx); n != 1 {
		t.Errorf("Count() = %d, want 1", n)
	}
}

// Documents go through the index embedder, queries through the query one.
func TestNewModuleWithEmbedders_SplitsIndexAndQuery(t *testing.T) {
	index, query := &countingEmbedder{}, &countingEmbedder{}
	m := NewModuleWithEmbedders(index, query)
	if err := m.AddDocuments(context.Background(), []Document{{ID: "a", Content: "alpha"}, {ID: "b", Content: "beta"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Retrieve(context.Background(), RetrieveRequest{Query: "alpha", TopK: 1}); err != nil {
		t.Fatal(err)
	}
	if index.texts != 2 || query.texts != 1 {
		t.Errorf("index embedded %d texts, query %d; want 2 and 1", index.texts, query.texts)
	}
}

type countingEmbedder struct{ texts int }

func (c *countingEmbedder) GenerateEmbedding(ctx context.Context, text string) ([]float32, error) {
	c.texts++
	return []float32{float32(len(text)), 1}, nil
}

func (c *countingEmbedder) GenerateEmbeddings(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i], _ = c.GenerateEmbedding(ctx, t)
	}
	return out, nil
}
