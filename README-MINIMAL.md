# Platform AI SDK

Go SDK for AI-powered platform engineering. Analyze code and generate configs using Claude AI.

## Install
```bash
go get github.com/philipsahli/innominatus-ai-sdk
```

## Use
```go
import "github.com/philipsahli/innominatus-ai-sdk/pkg/platformai"

sdk, _ := platformai.New(ctx, &platformai.Config{
    LLM: platformai.LLMConfig{
        Provider: "anthropic",
        APIKey:   "your-key",
        Model:    "claude-sonnet-4-5-20250929",
    },
})

result, _ := sdk.CodeMapping().Analyze(ctx, codemapping.AnalyzeRequest{
    RepoPath: "/path/to/repo",
})
```

Full docs: [README.md](README.md)
