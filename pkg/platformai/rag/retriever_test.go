package rag

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

var errBoom = errors.New("boom")

// docInput names the anonymous struct that Retriever.AddDocuments takes.
type docInput = struct {
	ID       string
	Content  string
	Metadata map[string]string
}

// fakeEmbedder returns fixed vectors per text, so tests control similarity scores.
type fakeEmbedder struct {
	vectors map[string][]float32
	err     error
	// extra is added to the length of every batch response; negative drops embeddings.
	extra int
	// batchCalls records the texts of every GenerateEmbeddings call.
	batchCalls [][]string
}

func (f *fakeEmbedder) GenerateEmbedding(ctx context.Context, text string) ([]float32, error) {
	if f.err != nil {
		return nil, f.err
	}
	v, ok := f.vectors[text]
	if !ok {
		return nil, fmt.Errorf("no vector for %q", text)
	}
	return v, nil
}

func (f *fakeEmbedder) GenerateEmbeddings(ctx context.Context, texts []string) ([][]float32, error) {
	f.batchCalls = append(f.batchCalls, texts)
	out := make([][]float32, 0, len(texts))
	for _, text := range texts {
		v, err := f.GenerateEmbedding(ctx, text)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	switch {
	case f.extra < 0:
		out = out[:max(0, len(out)+f.extra)]
	case f.extra > 0:
		for i := 0; i < f.extra; i++ {
			out = append(out, []float32{1, 0})
		}
	}
	return out, nil
}

// fakeStore wraps the in-memory store and fails selected calls.
type fakeStore struct {
	*InMemoryVectorStore
	addErr    error
	searchErr error
}

func (s *fakeStore) Add(ctx context.Context, doc Document) error {
	if s.addErr != nil {
		return s.addErr
	}
	return s.InMemoryVectorStore.Add(ctx, doc)
}

func (s *fakeStore) AddBatch(ctx context.Context, docs []Document) error {
	if s.addErr != nil {
		return s.addErr
	}
	return s.InMemoryVectorStore.AddBatch(ctx, docs)
}

func (s *fakeStore) Search(ctx context.Context, q []float32, topK int, minScore float32) ([]SearchResult, error) {
	if s.searchErr != nil {
		return nil, s.searchErr
	}
	return s.InMemoryVectorStore.Search(ctx, q, topK, minScore)
}

// testVectors gives each document a distinct cosine score against "query":
// a=1, b≈0.894, c≈0.707, d=0.
func testVectors() map[string][]float32 {
	return map[string][]float32{
		"query": {1, 0},
		"a":     {1, 0},
		"b":     {2, 1},
		"c":     {1, 1},
		"d":     {0, 1},
	}
}

func resultIDs(results []SearchResult) []string {
	ids := make([]string, len(results))
	for i, r := range results {
		ids[i] = r.Document.ID
	}
	return ids
}

func TestRetrieverRetrieve(t *testing.T) {
	tests := []struct {
		name     string
		req      RetrieveRequest
		wantIDs  []string
		embedErr error
		storeErr error
		wantErr  error
	}{
		{name: "TopK zero defaults to 3", req: RetrieveRequest{Query: "query"}, wantIDs: []string{"a", "b", "c"}},
		{name: "negative TopK defaults to 3", req: RetrieveRequest{Query: "query", TopK: -1}, wantIDs: []string{"a", "b", "c"}},
		{name: "explicit TopK", req: RetrieveRequest{Query: "query", TopK: 1}, wantIDs: []string{"a"}},
		{name: "TopK above count returns all", req: RetrieveRequest{Query: "query", TopK: 10}, wantIDs: []string{"a", "b", "c", "d"}},
		{name: "MinScore filters", req: RetrieveRequest{Query: "query", TopK: 10, MinScore: 0.8}, wantIDs: []string{"a", "b"}},
		{name: "embedding error", req: RetrieveRequest{Query: "query"}, embedErr: errBoom, wantErr: errBoom},
		{name: "search error", req: RetrieveRequest{Query: "query"}, storeErr: errBoom, wantErr: errBoom},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			store := &fakeStore{InMemoryVectorStore: NewInMemoryVectorStore()}
			embedder := &fakeEmbedder{vectors: testVectors()}
			r := NewRetriever(embedder, store)
			for _, id := range []string{"a", "b", "c", "d"} {
				if err := r.AddDocument(ctx, id, id, nil); err != nil {
					t.Fatalf("AddDocument(%s): %v", id, err)
				}
			}
			embedder.err = tt.embedErr
			store.searchErr = tt.storeErr

			resp, err := r.Retrieve(ctx, tt.req)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Retrieve() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Retrieve() error = %v", err)
			}
			if got := strings.Join(resultIDs(resp.Results), ","); got != strings.Join(tt.wantIDs, ",") {
				t.Errorf("result IDs = %s, want %s", got, strings.Join(tt.wantIDs, ","))
			}
			if len(resp.QueryEmbedding) != 2 || resp.QueryEmbedding[0] != 1 || resp.QueryEmbedding[1] != 0 {
				t.Errorf("QueryEmbedding = %v, want [1 0]", resp.QueryEmbedding)
			}
			if resp.Context != r.formatContext(resp.Results) {
				t.Errorf("Context does not match formatContext(Results)")
			}
		})
	}
}

func TestRetrieverFormatContext(t *testing.T) {
	tests := []struct {
		name    string
		results []SearchResult
		want    string
	}{
		{name: "no results", results: nil, want: ""},
		{
			name: "title and source",
			results: []SearchResult{{
				Document: Document{Content: "body", Metadata: map[string]string{"title": "T", "source": "S", "other": "ignored"}},
				Score:    0.876,
			}},
			want: "Relevant context from knowledge base:\n\n" +
				"--- Document 1 (Relevance: 0.88) ---\nTitle: T\nSource: S\n\nbody\n\n",
		},
		{
			name: "no metadata",
			results: []SearchResult{{
				Document: Document{Content: "body"},
				Score:    1,
			}},
			want: "Relevant context from knowledge base:\n\n" +
				"--- Document 1 (Relevance: 1.00) ---\n\nbody\n\n",
		},
		{
			name: "other metadata only",
			results: []SearchResult{{
				Document: Document{Content: "body", Metadata: map[string]string{"category": "x"}},
				Score:    0.5,
			}},
			want: "Relevant context from knowledge base:\n\n" +
				"--- Document 1 (Relevance: 0.50) ---\n\nbody\n\n",
		},
		{
			name: "numbering follows input order",
			results: []SearchResult{
				{Document: Document{Content: "first", Metadata: map[string]string{"source": "s1"}}, Score: 0.9},
				{Document: Document{Content: "second", Metadata: map[string]string{"title": "t2"}}, Score: 0.1},
			},
			want: "Relevant context from knowledge base:\n\n" +
				"--- Document 1 (Relevance: 0.90) ---\nSource: s1\n\nfirst\n\n" +
				"--- Document 2 (Relevance: 0.10) ---\nTitle: t2\n\nsecond\n\n",
		},
	}

	r := NewRetriever(&fakeEmbedder{}, NewInMemoryVectorStore())
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := r.formatContext(tt.results); got != tt.want {
				t.Errorf("formatContext() =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

func TestRetrieverAddDocument(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		embedErr error
		storeErr error
		wantErr  error
	}{
		{name: "stores document with embedding", content: "a"},
		{name: "embedding error", content: "a", embedErr: errBoom, wantErr: errBoom},
		{name: "store error", content: "a", storeErr: errBoom, wantErr: errBoom},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			store := &fakeStore{InMemoryVectorStore: NewInMemoryVectorStore(), addErr: tt.storeErr}
			r := NewRetriever(&fakeEmbedder{vectors: testVectors(), err: tt.embedErr}, store)

			err := r.AddDocument(ctx, "id1", tt.content, map[string]string{"title": "T"})
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("AddDocument() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("AddDocument() error = %v", err)
			}
			doc, err := store.Get(ctx, "id1")
			if err != nil {
				t.Fatalf("Get() error = %v", err)
			}
			if doc.Content != tt.content || doc.Metadata["title"] != "T" || len(doc.Embedding) != 2 {
				t.Errorf("stored doc = %+v", doc)
			}
		})
	}
}

func TestRetrieverAddDocuments(t *testing.T) {
	docs := []docInput{
		{ID: "1", Content: "a", Metadata: map[string]string{"k": "v"}},
		{ID: "2", Content: "b"},
		{ID: "3", Content: "c"},
	}

	tests := []struct {
		name        string
		extra       int
		embedErr    error
		storeErr    error
		wantErr     error
		wantErrText string
	}{
		{name: "stores all documents"},
		{name: "short embedding response", extra: -1, wantErrText: "got 2 embeddings for 3 documents"},
		{name: "empty embedding response", extra: -3, wantErrText: "got 0 embeddings for 3 documents"},
		{name: "long embedding response", extra: 1, wantErrText: "got 4 embeddings for 3 documents"},
		{name: "embedding error", embedErr: errBoom, wantErr: errBoom},
		{name: "store error", storeErr: errBoom, wantErr: errBoom},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			store := &fakeStore{InMemoryVectorStore: NewInMemoryVectorStore(), addErr: tt.storeErr}
			r := NewRetriever(&fakeEmbedder{vectors: testVectors(), err: tt.embedErr, extra: tt.extra}, store)

			err := r.AddDocuments(ctx, docs)
			switch {
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("AddDocuments() error = %v, want %v", err, tt.wantErr)
				}
				return
			case tt.wantErrText != "":
				if err == nil || !strings.Contains(err.Error(), tt.wantErrText) {
					t.Fatalf("AddDocuments() error = %v, want containing %q", err, tt.wantErrText)
				}
				if n, _ := store.Count(ctx); n != 0 {
					t.Errorf("Count() = %d after mismatch, want 0", n)
				}
				return
			}
			if err != nil {
				t.Fatalf("AddDocuments() error = %v", err)
			}
			for _, d := range docs {
				got, err := store.Get(ctx, d.ID)
				if err != nil {
					t.Fatalf("Get(%s) error = %v", d.ID, err)
				}
				want := testVectors()[d.Content]
				if got.Content != d.Content || len(got.Embedding) != len(want) || got.Embedding[0] != want[0] || got.Embedding[1] != want[1] {
					t.Errorf("Get(%s) = %+v, want content %q embedding %v", d.ID, got, d.Content, want)
				}
			}
			if got, _ := store.Get(ctx, "1"); got.Metadata["k"] != "v" {
				t.Errorf("metadata not stored: %v", got.Metadata)
			}
		})
	}
}
