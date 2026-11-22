package rag

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"testing"
)

func TestInMemoryVectorStoreAdd(t *testing.T) {
	tests := []struct {
		name    string
		doc     Document
		wantErr string
	}{
		{name: "valid", doc: Document{ID: "a", Embedding: []float32{1}}},
		{name: "missing ID", doc: Document{Embedding: []float32{1}}, wantErr: "document ID is required"},
		{name: "missing embedding", doc: Document{ID: "a"}, wantErr: "document embedding is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			s := NewInMemoryVectorStore()
			err := s.Add(ctx, tt.doc)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("Add() error = %v, want %q", err, tt.wantErr)
				}
				if n, _ := s.Count(ctx); n != 0 {
					t.Errorf("Count() = %d after rejected Add, want 0", n)
				}
				return
			}
			if err != nil {
				t.Fatalf("Add() error = %v", err)
			}
			if n, _ := s.Count(ctx); n != 1 {
				t.Errorf("Count() = %d, want 1", n)
			}
		})
	}
}

func TestInMemoryVectorStoreAddOverwrites(t *testing.T) {
	ctx := context.Background()
	s := NewInMemoryVectorStore()
	if err := s.Add(ctx, Document{ID: "a", Content: "old", Embedding: []float32{1}}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddBatch(ctx, []Document{{ID: "a", Content: "new", Embedding: []float32{2}}}); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.Count(ctx); n != 1 {
		t.Errorf("Count() = %d, want 1", n)
	}
	got, err := s.Get(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != "new" || got.Embedding[0] != 2 {
		t.Errorf("Get() = %+v, want overwritten document", got)
	}
}

func TestInMemoryVectorStoreAddBatch(t *testing.T) {
	valid := func(id string) Document { return Document{ID: id, Embedding: []float32{1}} }

	tests := []struct {
		name      string
		docs      []Document
		wantErr   string
		wantCount int
	}{
		{name: "valid batch", docs: []Document{valid("a"), valid("b")}, wantCount: 2},
		{name: "empty batch", docs: nil, wantCount: 0},
		{name: "missing ID", docs: []Document{{Embedding: []float32{1}}}, wantErr: "document ID is required"},
		{name: "missing embedding", docs: []Document{{ID: "a"}}, wantErr: "document embedding is required"},
		{name: "invalid document after valid one", docs: []Document{valid("a"), {ID: "b"}, valid("c")}, wantErr: "document embedding is required", wantCount: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			s := NewInMemoryVectorStore()
			err := s.AddBatch(ctx, tt.docs)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("AddBatch() error = %v, want %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("AddBatch() error = %v", err)
			}
			if n, _ := s.Count(ctx); n != tt.wantCount {
				t.Errorf("Count() = %d, want %d", n, tt.wantCount)
			}
		})
	}
}

func TestInMemoryVectorStoreSearch(t *testing.T) {
	// Scores against query [1 0]: a=1, b≈0.707, c=0, d=-1.
	docs := []Document{
		{ID: "a", Embedding: []float32{1, 0}},
		{ID: "b", Embedding: []float32{1, 1}},
		{ID: "c", Embedding: []float32{0, 1}},
		{ID: "d", Embedding: []float32{-1, 0}},
	}

	tests := []struct {
		name     string
		topK     int
		minScore float32
		want     []string
	}{
		{name: "TopK zero returns all", topK: 0, minScore: -1, want: []string{"a", "b", "c", "d"}},
		{name: "negative TopK returns all", topK: -1, minScore: -1, want: []string{"a", "b", "c", "d"}},
		{name: "TopK limits", topK: 2, minScore: -1, want: []string{"a", "b"}},
		{name: "TopK above count", topK: 10, minScore: -1, want: []string{"a", "b", "c", "d"}},
		{name: "MinScore is inclusive at 0", topK: 0, minScore: 0, want: []string{"a", "b", "c"}},
		{name: "MinScore is inclusive at 1", topK: 0, minScore: 1, want: []string{"a"}},
		{name: "MinScore and TopK combined", topK: 1, minScore: 0.5, want: []string{"a"}},
		{name: "MinScore above all scores", topK: 0, minScore: 1.1, want: []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			s := NewInMemoryVectorStore()
			if err := s.AddBatch(ctx, docs); err != nil {
				t.Fatal(err)
			}
			results, err := s.Search(ctx, []float32{1, 0}, tt.topK, tt.minScore)
			if err != nil {
				t.Fatalf("Search() error = %v", err)
			}
			if got := strings.Join(resultIDs(results), ","); got != strings.Join(tt.want, ",") {
				t.Errorf("Search() IDs = %s, want %s", got, strings.Join(tt.want, ","))
			}
			for i := 1; i < len(results); i++ {
				if results[i].Score > results[i-1].Score {
					t.Errorf("results not in descending score order: %v", results)
				}
			}
		})
	}
}

func TestInMemoryVectorStoreSearchNegativeScoreValue(t *testing.T) {
	// SearchResult.Score is cosine similarity, so it goes down to -1.
	ctx := context.Background()
	s := NewInMemoryVectorStore()
	if err := s.Add(ctx, Document{ID: "d", Embedding: []float32{-1, 0}}); err != nil {
		t.Fatal(err)
	}
	results, err := s.Search(ctx, []float32{1, 0}, 0, -1)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Score != -1 {
		t.Errorf("Search() = %v, want one result with score -1", results)
	}
}

func TestInMemoryVectorStoreSearchEqualScores(t *testing.T) {
	ctx := context.Background()
	s := NewInMemoryVectorStore()
	docs := []Document{
		{ID: "x", Embedding: []float32{1, 0}},
		{ID: "y", Embedding: []float32{2, 0}},
		{ID: "z", Embedding: []float32{0, 1}},
	}
	if err := s.AddBatch(ctx, docs); err != nil {
		t.Fatal(err)
	}
	results, err := s.Search(ctx, []float32{1, 0}, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	// x and y tie at 1; map iteration makes their order unspecified, so compare as a set.
	ids := resultIDs(results)
	sort.Strings(ids)
	if strings.Join(ids, ",") != "x,y" {
		t.Errorf("Search() IDs = %v, want x and y in any order", ids)
	}
}

func TestInMemoryVectorStoreSearchEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		docs    []Document
		query   []float32
		wantErr string
		wantLen int
	}{
		{name: "empty store returns empty slice", query: []float32{1}, wantLen: 0},
		{name: "empty query", docs: []Document{{ID: "a", Embedding: []float32{1}}}, query: nil, wantErr: "query embedding is required"},
		{name: "mismatched dimension scores 0", docs: []Document{{ID: "a", Embedding: []float32{1, 0, 0}}}, query: []float32{1, 0}, wantLen: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			s := NewInMemoryVectorStore()
			if err := s.AddBatch(ctx, tt.docs); err != nil {
				t.Fatal(err)
			}
			results, err := s.Search(ctx, tt.query, 0, 0)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("Search() error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Search() error = %v", err)
			}
			if results == nil {
				t.Error("Search() returned nil, want non-nil slice")
			}
			if len(results) != tt.wantLen {
				t.Errorf("len(results) = %d, want %d", len(results), tt.wantLen)
			}
		})
	}
}

func TestInMemoryVectorStoreGetDelete(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		wantErr string
	}{
		{name: "existing", id: "a"},
		{name: "not found", id: "missing", wantErr: "document not found: missing"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			s := NewInMemoryVectorStore()
			if err := s.Add(ctx, Document{ID: "a", Content: "c", Embedding: []float32{1}}); err != nil {
				t.Fatal(err)
			}

			doc, err := s.Get(ctx, tt.id)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Errorf("Get() error = %v, want %q", err, tt.wantErr)
				}
				if doc != nil {
					t.Errorf("Get() doc = %+v, want nil", doc)
				}
			} else if err != nil || doc.ID != tt.id || doc.Content != "c" {
				t.Errorf("Get() = %+v, %v", doc, err)
			}

			err = s.Delete(ctx, tt.id)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Errorf("Delete() error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Delete() error = %v", err)
			}
			if _, err := s.Get(ctx, tt.id); err == nil {
				t.Error("Get() after Delete succeeded, want not found")
			}
			if n, _ := s.Count(ctx); n != 0 {
				t.Errorf("Count() = %d after Delete, want 0", n)
			}
		})
	}
}

// Runs under -race to check the mutex covers every method.
func TestInMemoryVectorStoreConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	s := NewInMemoryVectorStore()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("doc-%d", i)
			_ = s.Add(ctx, Document{ID: id, Embedding: []float32{float32(i), 1}})
			_ = s.AddBatch(ctx, []Document{{ID: id + "-b", Embedding: []float32{1, float32(i)}}})
			_, _ = s.Search(ctx, []float32{1, 1}, 3, 0)
			_, _ = s.Get(ctx, id)
			_, _ = s.Count(ctx)
			_ = s.Delete(ctx, id+"-b")
		}(i)
	}
	wg.Wait()
	if n, _ := s.Count(ctx); n != 20 {
		t.Errorf("Count() = %d, want 20", n)
	}
}

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name string
		a, b []float32
		want float32
	}{
		{name: "identical", a: []float32{1, 2, 3}, b: []float32{1, 2, 3}, want: 1},
		{name: "same direction, different length", a: []float32{1, 2}, b: []float32{2, 4}, want: 1},
		{name: "orthogonal", a: []float32{1, 0}, b: []float32{0, 1}, want: 0},
		{name: "opposite", a: []float32{1, 2}, b: []float32{-1, -2}, want: -1},
		{name: "45 degrees", a: []float32{1, 0}, b: []float32{1, 1}, want: float32(1 / math.Sqrt2)},
		{name: "zero vector", a: []float32{0, 0}, b: []float32{1, 1}, want: 0},
		{name: "both zero", a: []float32{0, 0}, b: []float32{0, 0}, want: 0},
		{name: "mismatched length", a: []float32{1, 0}, b: []float32{1, 0, 0}, want: 0},
		{name: "both empty", a: []float32{}, b: []float32{}, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cosineSimilarity(tt.a, tt.b)
			if math.Abs(float64(got-tt.want)) > 1e-6 {
				t.Errorf("cosineSimilarity(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
