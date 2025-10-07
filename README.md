# Platform AI SDK

A Go SDK for AI-powered platform engineering automation. Analyze code repositories and automatically generate optimized platform configurations using Claude AI.

## Features

- 🔍 **Smart Repository Analysis** - Automatically detects programming languages, frameworks, and dependencies
- 🤖 **AI-Powered Configuration** - Uses Claude Sonnet 4.5 to generate optimal platform configurations
- 📦 **Multi-Language Support** - Supports Go, Node.js, Python, and more
- 🎯 **Framework Detection** - Recognizes popular frameworks (Gin, Express, FastAPI, etc.)
- 💡 **Actionable Recommendations** - Provides best practice suggestions
- 📚 **RAG (Retrieval-Augmented Generation)** - Build AI assistants with custom knowledge bases
- 🔎 **Semantic Search** - Vector-based document retrieval with similarity scoring
- 🧠 **Multiple Embedding Providers** - Supports OpenAI and Voyage AI embeddings
- 🔧 **Extensible Architecture** - Easy to add new languages and frameworks

## Installation

```bash
go get github.com/philipsahli/innominatus-ai-sdk
```

## Quick Start

### Code Analysis & Configuration

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai"
	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai/codemapping"
)

func main() {
	ctx := context.Background()

	// Initialize SDK
	sdk, err := platformai.New(ctx, &platformai.Config{
		LLM: platformai.LLMConfig{
			Provider: "anthropic",
			APIKey:   "your-api-key",
			Model:    "claude-sonnet-4-5-20250929",
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	// Analyze repository
	mapper := sdk.CodeMapping()
	result, err := mapper.Analyze(ctx, codemapping.AnalyzeRequest{
		RepoPath: "/path/to/your/repo",
	})
	if err != nil {
		log.Fatal(err)
	}

	// Use the results
	fmt.Printf("Language: %s\n", result.Analysis.PrimaryLanguage)
	fmt.Printf("Framework: %s\n", result.Analysis.DetectedFramework)
	fmt.Printf("Generated Config: %+v\n", result.Config)
}
```

### RAG (Retrieval-Augmented Generation)

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai"
	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai/llm"
	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai/rag"
)

func main() {
	ctx := context.Background()

	// Initialize SDK with RAG support
	sdk, err := platformai.New(ctx, &platformai.Config{
		LLM: platformai.LLMConfig{
			Provider: "anthropic",
			APIKey:   "your-anthropic-key",
			Model:    "claude-sonnet-4-5-20250929",
		},
		RAG: &rag.Config{
			EmbeddingProvider: "openai",
			APIKey:            "your-openai-key",
			Model:             "text-embedding-3-small",
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	// Add documents to knowledge base
	ragModule := sdk.RAG()
	err = ragModule.AddDocument(ctx, "doc1",
		"Kubernetes best practices: Always set resource limits and requests.",
		map[string]string{"source": "k8s-guide"})
	if err != nil {
		log.Fatal(err)
	}

	// Query with context
	ragResponse, err := ragModule.Retrieve(ctx, rag.RetrieveRequest{
		Query:    "What are Kubernetes best practices?",
		TopK:     2,
		MinScore: 0.3,
	})
	if err != nil {
		log.Fatal(err)
	}

	// Generate AI response with context
	response, err := sdk.LLM().GenerateWithContext(ctx, llm.GenerateRequest{
		SystemPrompt: "You are a helpful assistant. Use the provided context to answer questions.",
		UserPrompt:   "What are Kubernetes best practices?",
		Temperature:  0.7,
		MaxTokens:    500,
	}, ragResponse.Context)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(response.Text)
}
```

### As a CLI Tool

```bash
# Build the example CLI
cd examples/code-analyzer
go build -o platform-ai-example

# Set your API key
export ANTHROPIC_API_KEY="your-api-key"

# Analyze a repository
./platform-ai-example analyze /path/to/your/repo

# Specify custom output location
./platform-ai-example analyze /path/to/your/repo -o custom-config.yaml

# Verbose output
./platform-ai-example analyze /path/to/your/repo -v
```

## Example Output

```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📊 Platform AI - Repository Analysis Report
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

📁 Repository: /path/to/sample-go-repo

🔍 Analyzing repository...

📦 Stack Detection:
  ✓ Language: go (1.21)
  ✓ Framework: gin
  ✓ Files Analyzed: 15
  ✓ Dockerfile: Present

📚 Detected Dependencies:
  → github.com/gin-gonic/gin: v1.9.1
  → github.com/lib/pq: v1.10.9
  → github.com/redis/go-redis/v9: v9.3.0
  ... and 22 more

🔧 Platform Services:
  Service: sample-api
  Template: microservice
  Runtime: go1.21
  Port: 8080

  → Database: postgresql 15
    Storage: 10Gi

  → Cache: redis 7
    Memory: 256Mi

💾 Resource Recommendations:
  CPU: 500m
  Memory: 512Mi
  Scaling: 2-10 replicas (target: 70% CPU)

📊 Monitoring:
  Metrics: true
  Logs: true
  Traces: true

💡 Recommendations:
  ✅ Dockerfile present
     Good! Your service is ready for containerization
  ✅ Detected 25 dependencies
     Dependencies configured in platform config
  ⚠️  No health check endpoint found
     Consider adding a /health endpoint for monitoring

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

📝 Generated configuration: /path/to/sample-go-repo/.platform/config.yaml
```

## Generated Configuration Format

```yaml
# Platform Configuration
# Auto-generated by Platform AI SDK

service:
  name: sample-api
  template: microservice
  runtime: go1.21
  framework: gin
  port: 8080

resources:
  cpu: 500m
  memory: 512Mi
  scaling:
    min_replicas: 2
    max_replicas: 10
    target_cpu_percent: 70

database:
  type: postgresql
  version: "15"
  storage: 10Gi

cache:
  type: redis
  version: "7"
  memory: 256Mi

monitoring:
  metrics: true
  logs: true
  traces: true

security:
  health_check:
    path: /health
    port: 8080
```

## Supported Languages & Frameworks

### Languages
- **Go** (1.18+)
- **Node.js** (14+)
- **Python** (3.8+)
- **Rust**
- **Java**
- **Ruby**
- **PHP**

### Frameworks

**Go:**
- Gin
- Echo
- Fiber
- Chi
- Gorilla Mux

**Node.js:**
- Express
- NestJS
- Fastify
- Next.js
- React
- Vue

**Python:**
- FastAPI
- Flask
- Django

## Configuration

### SDK Configuration

```go
config := &platformai.Config{
	LLM: platformai.LLMConfig{
		Provider:    "anthropic",
		APIKey:      "your-api-key",
		Model:       "claude-sonnet-4-5-20250929",
		Temperature: 0.3,  // Optional, default: 0.3
		MaxTokens:   4096, // Optional, default: 4096
	},
}
```

### RAG Configuration

```go
config := &platformai.Config{
	LLM: platformai.LLMConfig{
		Provider: "anthropic",
		APIKey:   "your-anthropic-key",
		Model:    "claude-sonnet-4-5-20250929",
	},
	RAG: &rag.Config{
		EmbeddingProvider: "openai",          // "openai" or "voyageai"
		APIKey:            "your-openai-key", // API key for embedding provider
		Model:             "text-embedding-3-small", // Embedding model
	},
}
```

**Supported Embedding Providers:**
- **OpenAI**: `text-embedding-3-small`, `text-embedding-3-large`, `text-embedding-ada-002`
- **Voyage AI**: `voyage-3`, `voyage-3-lite`

### Environment Variables

- `ANTHROPIC_API_KEY` - Your Anthropic API key (required for LLM)
- `OPENAI_API_KEY` - Your OpenAI API key (required if using OpenAI embeddings)
- `VOYAGEAI_API_KEY` - Your Voyage AI API key (required if using Voyage AI embeddings)

## Architecture

```
innominatus-ai-sdk/
├── pkg/platformai/           # Core SDK package
│   ├── sdk.go                # Main SDK entry point
│   ├── config.go             # Configuration management
│   ├── types.go              # Shared types
│   ├── errors.go             # Error definitions
│   │
│   ├── llm/                  # LLM integration
│   │   ├── client.go         # LLM client interface
│   │   ├── anthropic.go      # Anthropic implementation
│   │   └── types.go          # LLM types
│   │
│   └── codemapping/          # Code analysis module
│       ├── module.go         # Main module
│       ├── analyzer.go       # Repository analyzer
│       ├── detector.go       # Language/framework detector
│       ├── config_generator.go # AI config generator
│       └── types.go          # Module types
│
├── examples/                 # Example applications
│   └── code-analyzer/        # CLI tool
│
└── testdata/                 # Sample repositories
    ├── sample-go-repo/
    ├── sample-node-repo/
    └── sample-python-repo/
```

## API Documentation

### Core SDK

#### `New(ctx context.Context, config *Config) (*SDK, error)`
Creates a new SDK instance.

```go
sdk, err := platformai.New(ctx, config)
```

#### `SDK.CodeMapping() *codemapping.Module`
Returns the code mapping module.

```go
mapper := sdk.CodeMapping()
```

### Code Mapping Module

#### `Module.Analyze(ctx context.Context, req AnalyzeRequest) (*AnalyzeResult, error)`
Analyzes a repository and generates platform configuration.

```go
result, err := mapper.Analyze(ctx, codemapping.AnalyzeRequest{
    RepoPath: "/path/to/repo",
    Options: codemapping.AnalyzeOptions{
        Verbose: true,
    },
})
```

**Returns:**
- `Analysis` - Repository analysis results
- `Config` - Generated platform configuration
- `Recommendations` - List of actionable recommendations

### RAG Module

#### `Module.AddDocument(ctx context.Context, id, content string, metadata map[string]string) error`
Adds a single document to the knowledge base.

```go
err := ragModule.AddDocument(ctx, "doc-id",
    "Your document content here",
    map[string]string{"title": "Doc Title", "source": "docs"})
```

#### `Module.AddDocuments(ctx context.Context, docs []struct{...}) error`
Adds multiple documents in batch.

```go
docs := []struct {
    ID       string
    Content  string
    Metadata map[string]string
}{
    {ID: "doc1", Content: "Content 1", Metadata: map[string]string{"source": "api"}},
    {ID: "doc2", Content: "Content 2", Metadata: map[string]string{"source": "docs"}},
}
err := ragModule.AddDocuments(ctx, docs)
```

#### `Module.Retrieve(ctx context.Context, req RetrieveRequest) (*RetrieveResponse, error)`
Retrieves relevant documents for a query.

```go
result, err := ragModule.Retrieve(ctx, rag.RetrieveRequest{
    Query:    "your search query",
    TopK:     3,        // Number of results (default: 3)
    MinScore: 0.3,      // Minimum similarity score 0-1 (default: 0.0)
})
```

**Returns:**
- `Results` - Array of documents with similarity scores
- `Context` - Formatted context string for LLM
- `QueryEmbedding` - Vector embedding of the query

#### `Module.Query(ctx context.Context, query string, topK int) (string, error)`
Simplified retrieval that returns formatted context directly.

```go
context, err := ragModule.Query(ctx, "your query", 3)
```

#### `Module.GetDocument(ctx context.Context, id string) (*Document, error)`
Retrieves a specific document by ID.

#### `Module.DeleteDocument(ctx context.Context, id string) error`
Removes a document from the knowledge base.

#### `Module.Count(ctx context.Context) (int, error)`
Returns the total number of documents in the knowledge base.

## Development

### Prerequisites
- Go 1.21 or later
- Anthropic API key

### Building

```bash
# Install dependencies
go mod download

# Build the SDK
go build ./pkg/platformai/...

# Build the example CLI
cd examples/code-analyzer
go build -o platform-ai-example
```

### Testing

Test the SDK with the provided sample repositories:

```bash
export ANTHROPIC_API_KEY="your-api-key"

# Test with Go repository
./platform-ai-example analyze ../../testdata/sample-go-repo

# Test with Node.js repository
./platform-ai-example analyze ../../testdata/sample-node-repo

# Test with Python repository
./platform-ai-example analyze ../../testdata/sample-python-repo
```

## Error Handling

The SDK uses wrapped errors for better context:

```go
result, err := mapper.Analyze(ctx, req)
if err != nil {
    if errors.Is(err, platformai.ErrRepositoryNotFound) {
        // Handle repository not found
    } else if errors.Is(err, platformai.ErrLLMGeneration) {
        // Handle LLM generation error
    } else {
        // Handle other errors
    }
}
```

## Best Practices

1. **Context Management** - Always pass context for cancellation support
2. **Error Handling** - Check and handle errors appropriately
3. **API Key Security** - Never commit API keys; use environment variables
4. **Timeouts** - LLM calls have a 60-second timeout by default
5. **Rate Limiting** - Be aware of Anthropic API rate limits

## Working with Different Document Formats

The RAG module accepts plain text strings. For other formats, you'll need to extract text first:

### PDF Documents

Use a PDF library to extract text:

```go
import "github.com/ledongthuc/pdf"

func extractPDFText(path string) (string, error) {
    f, r, err := pdf.Open(path)
    if err != nil {
        return "", err
    }
    defer f.Close()

    var text string
    totalPage := r.NumPage()
    for pageIndex := 1; pageIndex <= totalPage; pageIndex++ {
        p := r.Page(pageIndex)
        if p.V.IsNull() {
            continue
        }
        text += p.Content().Text
    }
    return text, nil
}

// Use with RAG
text, _ := extractPDFText("document.pdf")
ragModule.AddDocument(ctx, "pdf-doc-1", text, map[string]string{
    "source": "document.pdf",
    "type":   "pdf",
})
```

### Confluence Documents

Use the Confluence REST API:

```go
import (
    "encoding/json"
    "net/http"
)

func fetchConfluencePage(baseURL, pageID, token string) (string, error) {
    url := fmt.Sprintf("%s/rest/api/content/%s?expand=body.storage", baseURL, pageID)

    req, _ := http.NewRequest("GET", url, nil)
    req.Header.Set("Authorization", "Bearer "+token)

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()

    var result struct {
        Body struct {
            Storage struct {
                Value string `json:"value"`
            } `json:"storage"`
        } `json:"body"`
    }

    json.NewDecoder(resp.Body).Decode(&result)

    // Strip HTML tags if needed
    return result.Body.Storage.Value, nil
}

// Use with RAG
content, _ := fetchConfluencePage("https://your-domain.atlassian.net", "page-id", "token")
ragModule.AddDocument(ctx, "confluence-1", content, map[string]string{
    "source": "Confluence",
    "pageID": "page-id",
})
```

### Other Formats

- **Markdown**: Read directly as text
- **HTML**: Use `golang.org/x/net/html` to parse and extract text
- **Word/DOCX**: Use `github.com/nguyenthenguyen/docx` or similar
- **CSV/JSON**: Parse and convert to text representation

## Limitations

- Repository analysis is limited to files on disk
- Very large repositories may take longer to analyze
- Requires internet connection for LLM and embedding API calls
- Subject to Anthropic API rate limits and costs
- RAG module uses in-memory vector store (not persistent)
- Document formats other than plain text require external libraries for text extraction

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Acknowledgments

- Built with [Anthropic Claude](https://www.anthropic.com/claude) AI
- Uses [Cobra](https://github.com/spf13/cobra) for CLI
- Uses [go-yaml](https://github.com/go-yaml/yaml) for YAML handling

## Support

For issues, questions, or contributions, please open an issue on GitHub.

---

**Built with ❤️ using Claude Sonnet 4.5**