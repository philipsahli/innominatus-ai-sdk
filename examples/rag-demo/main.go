package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai"
	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai/llm"
	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai/rag"
)

func main() {
	ctx := context.Background()

	// Get API keys from environment
	anthropicKey := os.Getenv("ANTHROPIC_API_KEY")
	if anthropicKey == "" {
		log.Fatal("ANTHROPIC_API_KEY environment variable is required")
	}

	openaiKey := os.Getenv("OPENAI_API_KEY")
	if openaiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	// Initialize SDK with RAG support
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
		log.Fatal(err)
	}

	fmt.Println("=================================================================")
	fmt.Println("RAG Demo - AI Conversation with Document Context")
	fmt.Println("=================================================================")
	fmt.Println()

	// Get RAG module
	ragModule := sdk.RAG()

	// Step 1: Add documents to the knowledge base
	fmt.Println("📚 Step 1: Adding documents to knowledge base...")
	fmt.Println()

	documents := []struct {
		ID       string
		Content  string
		Metadata map[string]string
	}{
		{
			ID: "k8s-resources",
			Content: `# Kubernetes Resource Best Practices

When configuring Kubernetes resources for production workloads:

**CPU Limits:**
- Web services: 500m - 1000m (0.5-1 CPU cores)
- Background workers: 200m - 500m
- Never set CPU limits too low as it can cause throttling

**Memory Limits:**
- Small services: 512Mi - 1Gi
- Medium services: 1Gi - 2Gi
- Large services: 2Gi - 4Gi
- Always set memory limits to prevent OOM kills

**Autoscaling:**
- Minimum replicas: 2 (for high availability)
- Maximum replicas: Based on load testing
- Target CPU utilization: 70-80%
- Target memory utilization: 80-85%

**Health Checks:**
- Liveness probe: Checks if container is alive
- Readiness probe: Checks if container can accept traffic
- Startup probe: For slow-starting containers
- Recommended initial delay: 10-30 seconds`,
			Metadata: map[string]string{
				"title":    "Kubernetes Resource Best Practices",
				"category": "infrastructure",
				"source":   "platform-engineering-guide",
			},
		},
		{
			ID: "database-config",
			Content: `# Database Configuration Guidelines

**PostgreSQL Production Settings:**
- Version: Use PostgreSQL 15 or later
- Connection pooling: Use PgBouncer with pool size 20-50
- Storage: Minimum 20Gi for production, 50Gi+ for high-traffic apps
- Backup: Enable automated daily backups with 7-day retention
- High availability: Use replication with at least 1 standby

**Redis Cache Configuration:**
- Version: Redis 7.x for latest features
- Memory: Start with 512Mi, scale based on cache hit ratio
- Eviction policy: allkeys-lru for general caching
- Persistence: Enable AOF for important data, disable for pure cache
- Max connections: 1000 for small deployments

**Connection Management:**
- Use connection pooling (max pool size: 10-20 per service instance)
- Set connection timeout to 30 seconds
- Enable connection retry with exponential backoff
- Monitor connection count and query performance`,
			Metadata: map[string]string{
				"title":    "Database Configuration Guidelines",
				"category": "databases",
				"source":   "database-ops-handbook",
			},
		},
		{
			ID: "monitoring-setup",
			Content: `# Monitoring and Observability

**Essential Metrics to Track:**
- Request rate (requests per second)
- Error rate (percentage of failed requests)
- Response time (p50, p95, p99 latencies)
- CPU and memory utilization
- Database query performance
- Cache hit ratio

**Logging Best Practices:**
- Use structured logging (JSON format)
- Include correlation IDs for request tracing
- Log levels: DEBUG, INFO, WARN, ERROR, FATAL
- Centralize logs using ELK stack or similar
- Retention: 30 days for standard logs, 90 days for audit logs

**Alerting Strategy:**
- Error rate > 5%: Page on-call engineer
- Response time p95 > 1s: Send warning notification
- CPU utilization > 85%: Auto-scale if possible
- Memory utilization > 90%: Immediate alert
- Failed health checks: Auto-restart container

**Distributed Tracing:**
- Enable for all microservices
- Use OpenTelemetry or similar standard
- Sample 10-20% of requests in production
- 100% sampling for errors`,
			Metadata: map[string]string{
				"title":    "Monitoring and Observability",
				"category": "operations",
				"source":   "sre-playbook",
			},
		},
	}

	// Add documents with embeddings
	err = ragModule.AddDocuments(ctx, documents)
	if err != nil {
		log.Fatalf("Failed to add documents: %v", err)
	}

	count, _ := ragModule.Count(ctx)
	fmt.Printf("✅ Successfully added %d documents to knowledge base\n", count)
	fmt.Println()

	// Step 2: Ask questions that require document knowledge
	fmt.Println("💬 Step 2: Asking AI questions using RAG context...")
	fmt.Println()

	questions := []string{
		"What CPU and memory limits should I use for a medium-sized web service in Kubernetes?",
		"How should I configure PostgreSQL for a production application with high traffic?",
		"What metrics are most important to track for monitoring a web service?",
	}

	successCount := 0
	for i, question := range questions {
		fmt.Printf("❓ Question %d: %s\n", i+1, question)
		fmt.Println()

		// Retrieve relevant context from RAG
		ragResponse, err := ragModule.Retrieve(ctx, rag.RetrieveRequest{
			Query:    question,
			TopK:     2,
			MinScore: 0.3,
		})
		if err != nil {
			log.Printf("Failed to retrieve context: %v", err)
			continue
		}

		fmt.Printf("📖 Retrieved %d relevant documents (showing relevance scores):\n", len(ragResponse.Results))
		for j, result := range ragResponse.Results {
			title := result.Document.Metadata["title"]
			fmt.Printf("   %d. %s (%.2f relevance)\n", j+1, title, result.Score)
		}
		fmt.Println()

		// Generate answer using LLM with RAG context
		response, err := sdk.LLM().GenerateWithContext(ctx, llm.GenerateRequest{
			SystemPrompt: "You are a helpful platform engineering assistant. Use the provided context to answer questions accurately. If the context doesn't contain relevant information, say so.",
			UserPrompt:   question,
			Temperature:  0.7,
			MaxTokens:    500,
		}, ragResponse.Context)
		if err != nil {
			log.Printf("Failed to generate response: %v", err)
			continue
		}

		successCount++
		fmt.Printf("🤖 AI Response:\n%s\n", response.Text)
		fmt.Println()
		fmt.Printf("📊 Tokens used: %d (prompt: %d, completion: %d)\n",
			response.Usage.TotalTokens,
			response.Usage.PromptTokens,
			response.Usage.CompletionTokens)
		fmt.Println()
		fmt.Println("-----------------------------------------------------------------")
		fmt.Println()
	}

	fmt.Println("=================================================================")
	if successCount > 0 {
		fmt.Println("✅ RAG Demo Complete!")
	} else {
		fmt.Println("❌ RAG Demo Failed - No questions were successfully answered")
	}
	fmt.Println("=================================================================")
}
