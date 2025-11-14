# Platform AI SDK - Claude Code Guide

## Project Overview

A Go SDK for AI-powered platform engineering automation. Analyzes code repositories and generates optimized platform configurations using Claude AI. Features include code-to-platform mapping, RAG (Retrieval-Augmented Generation) for custom knowledge bases, and multi-language support.

**Repository:** github.com/philipsahli/innominatus-ai-sdk

## Tech Stack

- **Language:** Go 1.24.1
- **LLM Provider:** Anthropic Claude (Sonnet 4.5)
- **Embedding Providers:** OpenAI, Voyage AI
- **CLI Framework:** Cobra
- **Config Format:** YAML (gopkg.in/yaml.v3)
- **Vector Store:** In-memory (custom implementation)

## Key Commands

```bash
# Development
go mod download              # Install dependencies
go build ./...               # Build all packages
go test ./...                # Run all tests
go mod tidy                  # Clean up dependencies

# Quality
gofmt -w .                   # Format code
golangci-lint run            # Run linter
go vet ./...                 # Static analysis
gosec ./...                  # Security audit

# Examples
cd examples/code-analyzer && go build
cd examples/rag-demo && go build

# Run examples
export ANTHROPIC_API_KEY="your-key"
./examples/code-analyzer/code-analyzer analyze /path/to/repo
./examples/rag-demo/rag-demo
```

## Architecture Rules

### SOLID Principles

**Single Responsibility Principle**
- Each module has one clear purpose: `codemapping`, `rag`, `llm`
- Each package handles one domain concept
- Example: `Analyzer` only analyzes repos, `Detector` only detects languages

**Open/Closed Principle**
- Extend via interfaces, don't modify existing code
- Add new LLM providers by implementing `llm.Client` interface
- Add new embedding providers via `rag.EmbeddingService` interface

**Liskov Substitution Principle**
- Any `llm.Client` implementation is interchangeable
- `AnthropicClient` can replace any `llm.Client` without breaking behavior
- Interfaces define contracts, not implementations

**Interface Segregation Principle**
- Small, focused interfaces over large ones
- `llm.Client` interface is minimal, specific
- Don't force clients to depend on methods they don't use

**Dependency Inversion Principle**
- Depend on abstractions (`llm.Client`), not concretions (`AnthropicClient`)
- Pass interfaces to constructors
- Example: `codemapping.NewModule(llmClient llm.Client)`

### KISS (Keep It Simple, Stupid)

- **Favor simple solutions** - Direct implementations over complex patterns
- **Explicit over implicit** - Clear function names, no magic
- **Avoid premature optimization** - Make it work first, optimize later
- **Self-documenting code** - Use descriptive names instead of comments
- **If it's complex, simplify** - Complexity is a code smell

Examples:
```go
// Good: Simple, clear
func (m *Module) Analyze(ctx context.Context, req AnalyzeRequest) (*AnalyzeResult, error)

// Bad: Over-engineered
func (m *Module) AnalyzeWithStrategyFactoryBuilder(...)
```

### YAGNI (You Aren't Gonna Need It)

- **Build only what's needed now** - No speculative features
- **Don't add unused features** - If not explicitly required, don't build it
- **Avoid over-engineering** - No unnecessary abstractions
- **Refactor when needed** - Not preemptively

Examples:
- ✅ In-memory vector store (current need)
- ❌ Persistent DB, distributed scaling (not needed yet)

### Minimal Documentation Philosophy

**Code Comments:**
- Explain "why", never "what" (code shows what)
- No redundant comments (`// increment i`)
- No TODO comments (use GitHub issues)
- Self-documenting code through clear naming

```go
// Bad
// This function analyzes the repository
func Analyze(ctx context.Context, path string) error {
    // loop through files
    for _, f := range files {
        // ...
    }
}

// Good
func Analyze(ctx context.Context, repoPath string) error {
    // Skip vendor directories to improve performance
    for _, file := range files {
        if isVendorDir(file) {
            continue
        }
    }
}
```

**Documentation Files:**
- README: Core concept + quick start only
- No verbose feature lists
- No contributing/license boilerplate unless needed
- "Telegram style" - ultra-short, actionable

### Domain Structure

```
pkg/platformai/
├── sdk.go              # Main SDK entry point
├── config.go           # Configuration
├── types.go            # Shared types
├── errors.go           # Error definitions
│
├── llm/                # LLM module (abstraction for AI providers)
│   ├── client.go       # Interface definition
│   ├── anthropic.go    # Claude implementation
│   └── types.go        # Request/response types
│
├── codemapping/        # Code analysis and platform config generation
│   ├── module.go       # Module entry point
│   ├── analyzer.go     # Repository analyzer
│   ├── detector.go     # Language/framework detection
│   ├── config_generator.go  # AI-powered config generation
│   └── types.go        # Domain types
│
└── rag/                # RAG (Retrieval-Augmented Generation)
    ├── module.go       # Module entry point
    ├── embeddings.go   # Embedding service abstraction
    ├── store.go        # Vector store (in-memory)
    ├── retriever.go    # Semantic search
    └── types.go        # Domain types
```

**Bounded Contexts:**
- Each module is independent with clear boundaries
- Modules communicate via SDK facade
- Shared types in root, domain-specific in modules

### Code Quality Standards

**Test Coverage:** >80% required
- Unit tests for all core logic
- Integration tests for LLM calls (use mocks/stubs)
- Example tests in examples/ directories

```bash
go test -cover ./...
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

**Error Handling:**
- Always wrap errors with context: `fmt.Errorf("failed to X: %w", err)`
- Define sentinel errors in `errors.go`
- Use `errors.Is()` for error checking

```go
var (
    ErrRepositoryNotFound = errors.New("repository not found")
    ErrLLMGeneration      = errors.New("LLM generation failed")
)
```

**Naming Conventions:**
- Packages: lowercase, single word (or compound: `codemapping`)
- Files: lowercase with underscores (`config_generator.go`)
- Types: PascalCase (`AnalyzeRequest`, `Module`)
- Functions: camelCase for unexported, PascalCase for exported
- Interfaces: Single-method = verb + "er" (`Client`, `Analyzer`)

**Dependency Management:**
- Minimal dependencies - only install what's needed
- Audit regularly: `go mod tidy && go list -m all`
- Avoid large frameworks when simple libraries suffice
- Pin versions in go.mod

### Factory Patterns

Use factory functions for module creation:

```go
// Good: Factory function
func NewModule(llmClient llm.Client) *Module {
    return &Module{
        llm:       llmClient,
        analyzer:  NewAnalyzer(),
        detector:  NewDetector(),
    }
}

// Use constructor injection for dependencies
func NewConfigGenerator(llmClient llm.Client) *ConfigGenerator {
    return &ConfigGenerator{llm: llmClient}
}
```

### Dependency Injection

Pass dependencies via constructors, not global state:

```go
// Good: Dependencies injected
type Module struct {
    llm llm.Client  // Interface, not concrete type
}

// Bad: Global state
var globalLLM *AnthropicClient
```

## Current Focus

**Sprint: Core SDK Stability & Testing**
- Add comprehensive test coverage (currently 0%)
- Implement integration tests for LLM/RAG modules
- Add example verification scripts
- Performance optimization for large repositories

## Verification Protocol

### Verification-First Development

Every feature requires a verification script that:
1. Runs actual code/API calls (not just unit tests)
2. Captures real outputs (API responses, generated configs)
3. Prints clear pass/fail results
4. Saves artifacts to `docs/verification/`

**Template Location:** `verification/template.go`

**Example Verification:**
```go
package main

import (
    "context"
    "fmt"
    "github.com/philipsahli/innominatus-ai-sdk/pkg/platformai"
)

func main() {
    ctx := context.Background()

    // Setup
    sdk, err := platformai.New(ctx, &platformai.Config{...})
    if err != nil {
        fmt.Printf("❌ FAIL: SDK initialization: %v\n", err)
        return
    }

    // Execute
    result, err := sdk.CodeMapping().Analyze(ctx, req)
    if err != nil {
        fmt.Printf("❌ FAIL: Analysis: %v\n", err)
        return
    }

    // Verify
    if result.Analysis.PrimaryLanguage == "" {
        fmt.Println("❌ FAIL: No language detected")
        return
    }

    fmt.Printf("✅ PASS: Detected %s\n", result.Analysis.PrimaryLanguage)

    // Save artifacts
    saveToFile("docs/verification/analysis-output.yaml", result)
}
```

## Best Practices

### Context Management
- Always accept `context.Context` as first parameter
- Respect context cancellation
- Use `context.WithTimeout()` for LLM calls (default: 60s)

### Error Handling
- Check and handle all errors
- Wrap errors with context
- Use sentinel errors for known conditions
- Never panic in library code

### API Key Security
- Never commit API keys
- Use environment variables
- Document required keys in `.env.example`
- Validate keys early in `config.Validate()`

### Interface Design
- Keep interfaces small and focused
- Define interfaces where they're used (consumer-driven)
- Accept interfaces, return structs

```go
// Good: Accept interface, return struct
func NewModule(llmClient llm.Client) *Module

// Bad: Accept struct
func NewModule(llmClient *AnthropicClient) *Module
```

### Go Idioms
- Use `defer` for cleanup
- Prefer composition over inheritance
- Use `_test` package for black-box testing
- Leverage zero values when sensible

## Development Workflow

1. **Before coding:** Write verification script
2. **During coding:** Follow SOLID + KISS + YAGNI
3. **After coding:** Run verification + tests
4. **Before commit:** Format, lint, test coverage check

```bash
# Pre-commit checks (automated via hooks.go)
gofmt -w .
golangci-lint run
go test ./... -cover
go mod tidy
```

## Performance Considerations

- **Large repositories:** Stream file analysis instead of loading all into memory
- **LLM calls:** Cache results where appropriate (e.g., language detection)
- **Vector store:** In-memory is fine for current scale (<10K documents)
- **Embeddings:** Batch API calls when adding multiple documents

## Security

- **API keys:** Never log or expose
- **User input:** Sanitize file paths (prevent directory traversal)
- **LLM prompts:** Validate and sanitize repository content before sending
- **Dependencies:** Regular security audits with `gosec`

## Monitoring & Observability

- **Logs:** Use structured logging (consider adding `slog` in future)
- **Metrics:** Not yet implemented (YAGNI - add when needed)
- **Tracing:** Not yet implemented (YAGNI - add when needed)

## Quick Reference

| Task | Command |
|------|---------|
| Format code | `gofmt -w .` |
| Run tests | `go test ./...` |
| Coverage | `go test -cover ./...` |
| Lint | `golangci-lint run` |
| Security scan | `gosec ./...` |
| Build | `go build ./...` |
| Tidy deps | `go mod tidy` |

## Common Patterns

**Error Wrapping:**
```go
if err != nil {
    return nil, fmt.Errorf("failed to analyze repository: %w", err)
}
```

**Context Timeout:**
```go
ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
defer cancel()
```

**Interface Implementation Check:**
```go
var _ llm.Client = (*AnthropicClient)(nil)
```

**Table-Driven Tests:**
```go
tests := []struct {
    name    string
    input   string
    want    string
    wantErr bool
}{
    {"valid input", "test", "expected", false},
}
for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        // ...
    })
}
```

---

**Remember:** Simple code that works > Complex code that's "clever"
