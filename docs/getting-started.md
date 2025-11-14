# Getting Started

## Install
```bash
go get github.com/philipsahli/innominatus-ai-sdk
```

## Setup
```bash
export ANTHROPIC_API_KEY="your-key"
```

## Use
```go
sdk, _ := platformai.New(ctx, &platformai.Config{
    LLM: platformai.LLMConfig{
        Provider: "anthropic",
        APIKey:   os.Getenv("ANTHROPIC_API_KEY"),
        Model:    "claude-sonnet-4-5-20250929",
    },
})

result, _ := sdk.CodeMapping().Analyze(ctx, codemapping.AnalyzeRequest{
    RepoPath: "/path/to/repo",
})
```

See README.md for full documentation.
