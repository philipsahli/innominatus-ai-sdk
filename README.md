# Platform AI SDK

AI-powered platform engineering automation for Go. Analyze code repositories and generate optimized platform configurations using Claude AI.

## Installation

```bash
go get github.com/philipsahli/innominatus-ai-sdk
```

## Quick Start

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

	sdk, _ := platformai.New(ctx, &platformai.Config{
		LLM: platformai.LLMConfig{
			Provider: "anthropic",
			APIKey:   "your-api-key",
			Model:    "claude-sonnet-4-5-20250929",
		},
	})

	result, err := sdk.CodeMapping().Analyze(ctx, codemapping.AnalyzeRequest{
		RepoPath: "/path/to/repo",
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Language: %s, Framework: %s\n",
		result.Analysis.PrimaryLanguage,
		result.Analysis.DetectedFramework)
}
```

## Features

- **Code Analysis** - Detects languages, frameworks, dependencies
- **AI Config Generation** - Creates optimized platform configurations
- **RAG Support** - Build AI assistants with custom knowledge bases

## Examples

See [`examples/`](examples/) for complete working examples.

## Documentation

- [Architecture & Development Guide](CLAUDE.md)
- [Examples](examples/)
