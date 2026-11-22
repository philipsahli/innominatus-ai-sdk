package rag_test

import (
	"context"
	"fmt"
	"math/rand"
	"testing"

	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai/rag"
)

func BenchmarkInMemoryVectorStoreSearch(b *testing.B) {
	ctx := context.Background()

	for _, docCount := range []int{100, 1_000, 10_000} {
		for _, dim := range []int{384, 1536} {
			// Fixed seed so every run benchmarks the same vectors.
			rng := rand.New(rand.NewSource(42))
			store := rag.NewInMemoryVectorStore()
			docs := make([]rag.Document, docCount)
			for i := range docs {
				docs[i] = rag.Document{
					ID:        fmt.Sprintf("doc-%d", i),
					Embedding: randomEmbedding(rng, dim),
				}
			}
			if err := store.AddBatch(ctx, docs); err != nil {
				b.Fatalf("AddBatch: %v", err)
			}
			query := randomEmbedding(rng, dim)

			for _, topK := range []int{1, 5, 10, 50} {
				name := fmt.Sprintf("docs=%d/dim=%d/topK=%d", docCount, dim, topK)
				b.Run(name, func(b *testing.B) {
					b.ReportAllocs()
					b.ResetTimer()
					for range b.N {
						if _, err := store.Search(ctx, query, topK, 0); err != nil {
							b.Fatalf("Search: %v", err)
						}
					}
				})
			}
		}
	}
}

// Components are in [0, 1) so every cosine score is >= 0 and MinScore 0
// keeps all documents; signed components would drop about half of them.
func randomEmbedding(rng *rand.Rand, dim int) []float32 {
	v := make([]float32, dim)
	for i := range v {
		v[i] = rng.Float32()
	}
	return v
}
