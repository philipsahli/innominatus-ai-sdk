package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai"
	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai/rag"
)

func main() {
	ctx := context.Background()

	fmt.Println("🔧 RAG Module Verification")
	fmt.Println("==========================")

	// Setup
	fmt.Println("1. Setting up SDK with RAG...")
	anthropicKey := os.Getenv("ANTHROPIC_API_KEY")
	openaiKey := os.Getenv("OPENAI_API_KEY")

	if anthropicKey == "" {
		fail("ANTHROPIC_API_KEY not set")
	}
	if openaiKey == "" {
		fail("OPENAI_API_KEY not set (required for embeddings)")
	}

	sdk, err := platformai.New(ctx, &platformai.Config{
		LLM: platformai.LLMConfig{
			Provider: "anthropic",
			APIKey:   anthropicKey,
			Model:    "claude-sonnet-4-5-20250929",
		},
		RAG: &rag.Config{
			EmbeddingProvider: "openai",
			APIKey:            openaiKey,
			Model:             "text-embedding-3-small",
		},
	})
	if err != nil {
		fail("SDK initialization failed: %v", err)
	}
	pass("SDK initialized with RAG support")

	ragModule := sdk.RAG()
	if ragModule == nil {
		fail("RAG module not initialized")
	}

	// Add test documents
	fmt.Println("\n2. Adding test documents...")
	testDocs := []struct {
		ID       string
		Content  string
		Metadata map[string]string
	}{
		{
			ID:      "k8s-resources",
			Content: "Kubernetes best practices: Always set resource limits and requests for containers to ensure proper scheduling.",
			Metadata: map[string]string{
				"source": "k8s-guide",
				"topic":  "resources",
			},
		},
		{
			ID:      "k8s-health",
			Content: "Kubernetes health checks: Implement liveness and readiness probes to ensure your application is running correctly.",
			Metadata: map[string]string{
				"source": "k8s-guide",
				"topic":  "health",
			},
		},
		{
			ID:      "docker-layers",
			Content: "Docker best practices: Minimize layer count by combining RUN commands and use multi-stage builds.",
			Metadata: map[string]string{
				"source": "docker-guide",
				"topic":  "optimization",
			},
		},
	}

	for _, doc := range testDocs {
		if err := ragModule.AddDocument(ctx, doc.ID, doc.Content, doc.Metadata); err != nil {
			fail("Failed to add document %s: %v", doc.ID, err)
		}
	}
	pass("Added %d test documents", len(testDocs))

	// Verify document count
	count, err := ragModule.Count(ctx)
	if err != nil {
		fail("Failed to get document count: %v", err)
	}
	if count != len(testDocs) {
		fail("Expected %d documents, got %d", len(testDocs), count)
	}
	pass("Document count verified: %d", count)

	// Execute retrieval
	fmt.Println("\n3. Testing semantic search...")
	query := "What are Kubernetes best practices?"
	retrieveResp, err := ragModule.Retrieve(ctx, rag.RetrieveRequest{
		Query:    query,
		TopK:     2,
		MinScore: 0.0,
	})
	if err != nil {
		fail("Retrieval failed: %v", err)
	}

	if len(retrieveResp.Results) == 0 {
		fail("No results returned for query")
	}
	pass("Retrieved %d relevant documents", len(retrieveResp.Results))

	// Verify results
	fmt.Println("\n4. Verifying search results...")
	topResult := retrieveResp.Results[0]

	if topResult.Score < 0.3 {
		fail("Top result score too low: %f", topResult.Score)
	}
	pass("Top result score: %.4f", topResult.Score)

	if topResult.Document.ID == "" {
		fail("Result document has no ID")
	}
	pass("Result has valid document ID: %s", topResult.Document.ID)

	if retrieveResp.Context == "" {
		fail("No context generated")
	}
	pass("Context generated for LLM")

	// Test retrieval with high threshold
	fmt.Println("\n5. Testing score filtering...")
	strictResp, err := ragModule.Retrieve(ctx, rag.RetrieveRequest{
		Query:    "Docker optimization",
		TopK:     5,
		MinScore: 0.5,
	})
	if err != nil {
		fail("Strict retrieval failed: %v", err)
	}
	pass("Filtered retrieval returned %d results", len(strictResp.Results))

	// Test document retrieval
	fmt.Println("\n6. Testing document retrieval...")
	doc, err := ragModule.GetDocument(ctx, "k8s-resources")
	if err != nil {
		fail("Failed to get document: %v", err)
	}
	if doc.ID != "k8s-resources" {
		fail("Retrieved wrong document: %s", doc.ID)
	}
	pass("Document retrieved by ID")

	// Save artifacts
	fmt.Println("\n7. Saving artifacts...")
	if err := saveArtifact(query, retrieveResp, testDocs); err != nil {
		fail("Failed to save artifacts: %v", err)
	}

	// Summary
	fmt.Println("\n📊 Verification Summary")
	fmt.Println("======================")
	fmt.Printf("Documents indexed: %d\n", count)
	fmt.Printf("Query: %s\n", query)
	fmt.Printf("Results returned: %d\n", len(retrieveResp.Results))
	fmt.Printf("Top result score: %.4f\n", topResult.Score)
	fmt.Printf("Top result: %s\n", topResult.Document.ID)

	fmt.Println("\n✅ PASS: RAG verification completed successfully")
}

func saveArtifact(query string, results *rag.RetrieveResponse, docs []struct {
	ID       string
	Content  string
	Metadata map[string]string
}) error {
	outputDir := "../../docs/verification"
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return err
	}

	timestamp := time.Now().Format("20060102-150405")
	filename := filepath.Join(outputDir, fmt.Sprintf("rag-%s.json", timestamp))

	artifact := map[string]interface{}{
		"timestamp":    time.Now(),
		"verification": "rag-module",
		"input": map[string]interface{}{
			"query":          query,
			"documents":      docs,
			"document_count": len(docs),
		},
		"output": map[string]interface{}{
			"results_count": len(results.Results),
			"results":       results.Results,
			"context":       results.Context,
		},
	}

	data, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return err
	}

	fmt.Printf("  Saved: %s\n", filename)
	return nil
}

func fail(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "❌ FAIL: "+format+"\n", args...)
	os.Exit(1)
}

func pass(format string, args ...interface{}) {
	fmt.Printf("✓ "+format+"\n", args...)
}
