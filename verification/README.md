# Verification-First Development

## Overview

Verification scripts test features end-to-end with **real API calls and actual outputs**. Unlike unit tests, verifications run complete workflows to ensure features work in practice.

## Philosophy

1. **Real-world testing** - Use actual APIs, not mocks
2. **Capture outputs** - Save artifacts for review
3. **Clear pass/fail** - Unambiguous success criteria
4. **Repeatable** - Same inputs → same outputs

## When to Use Verification Scripts

- Testing new SDK features
- Validating LLM integrations
- Checking RAG module workflows
- End-to-end repository analysis
- Regression testing after changes

## Structure

```
verification/
├── template.go           # Template for new verifications
├── README.md             # This file
└── examples/             # Sample verification scripts
    ├── codemapping_verify.go
    └── rag_verify.go
```

## Creating a Verification Script

### 1. Copy Template
```bash
cp verification/template.go verification/my_feature_verify.go
```

### 2. Customize for Your Feature

**Setup Section:**
```go
// Initialize SDK with required config
config := &platformai.Config{
    LLM: platformai.LLMConfig{
        Provider: "anthropic",
        APIKey:   os.Getenv("ANTHROPIC_API_KEY"),
        Model:    "claude-sonnet-4-5-20250929",
    },
    // Add RAG config if needed
    RAG: &rag.Config{...},
}
```

**Execute Section:**
```go
// Run actual feature code
result, err := sdk.MyFeature().DoSomething(ctx, request)
if err != nil {
    fail("Operation failed: %v", err)
}
```

**Verify Section:**
```go
// Check outputs meet requirements
if result.Output == "" {
    return fmt.Errorf("expected output, got empty")
}

if result.Quality < 0.8 {
    return fmt.Errorf("quality too low: %f", result.Quality)
}
```

**Save Artifacts:**
```go
// Outputs saved to docs/verification/
saveArtifacts(result)
```

### 3. Run Verification

```bash
# Set API keys
export ANTHROPIC_API_KEY="your-key"
export OPENAI_API_KEY="your-key"  # if using RAG

# Run verification
go run verification/my_feature_verify.go
```

### 4. Review Artifacts

Check `docs/verification/` for saved outputs:
```bash
ls -la docs/verification/
cat docs/verification/verification-20251019-143022.json
```

## Verification vs Unit Tests

| Aspect | Unit Tests | Verification Scripts |
|--------|------------|---------------------|
| **Scope** | Individual functions | Complete workflows |
| **Dependencies** | Mocked | Real (LLM, APIs) |
| **Speed** | Fast (<1s) | Slower (API calls) |
| **Frequency** | Every commit | Before releases |
| **Purpose** | Code correctness | Feature validation |
| **Artifacts** | None | Saved to disk |

Both are important! Use unit tests for fast feedback, verifications for confidence.

## Example Verification: Code Analysis

```go
package main

import (
    "context"
    "github.com/philipsahli/innominatus-ai-sdk/pkg/platformai"
    "github.com/philipsahli/innominatus-ai-sdk/pkg/platformai/codemapping"
)

func main() {
    ctx := context.Background()

    // Setup
    sdk, _ := platformai.New(ctx, &platformai.Config{...})

    // Execute
    result, err := sdk.CodeMapping().Analyze(ctx, codemapping.AnalyzeRequest{
        RepoPath: "../../testdata/sample-go-repo",
    })
    if err != nil {
        fail("Analysis failed: %v", err)
    }

    // Verify
    if result.Analysis.PrimaryLanguage != "go" {
        fail("Expected language 'go', got '%s'", result.Analysis.PrimaryLanguage)
    }

    if result.Config == nil {
        fail("No config generated")
    }

    // Save
    saveArtifacts(result)

    // Success
    pass("Code analysis verification completed")
}
```

## Best Practices

### 1. Use Real Data
```go
// ✅ Good: Test with actual repository
result, _ := sdk.CodeMapping().Analyze(ctx, codemapping.AnalyzeRequest{
    RepoPath: "../../testdata/sample-go-repo",
})

// ❌ Bad: Mocked or fake data
result := &AnalyzeResult{...}  // hardcoded
```

### 2. Check Multiple Criteria
```go
// Verify multiple aspects of the result
if result.Analysis.PrimaryLanguage == "" {
    return fmt.Errorf("no language detected")
}

if len(result.Analysis.Files) == 0 {
    return fmt.Errorf("no files analyzed")
}

if result.Config == nil {
    return fmt.Errorf("no config generated")
}
```

### 3. Clear Error Messages
```go
// ✅ Good: Specific error with context
if len(results.Results) == 0 {
    fail("Expected at least 1 search result, got 0")
}

// ❌ Bad: Generic error
if len(results.Results) == 0 {
    fail("Verification failed")
}
```

### 4. Save Meaningful Artifacts
```go
// Save complete result for debugging
artifact := map[string]interface{}{
    "timestamp":   time.Now(),
    "input":       request,
    "output":      result,
    "performance": metrics,
}
saveArtifacts(artifact)
```

## Running Verifications in CI

```bash
#!/bin/bash
# ci-verify.sh

set -e

echo "Running verification suite..."

# Run all verification scripts
for script in verification/*_verify.go; do
    echo "▶ $(basename $script)"
    go run "$script"
done

echo "✅ All verifications passed"
```

## Troubleshooting

### Issue: API Key Not Set
```
❌ FAIL: ANTHROPIC_API_KEY environment variable not set
```
**Solution:** Export your API key:
```bash
export ANTHROPIC_API_KEY="your-key"
```

### Issue: Verification Timeout
```
❌ FAIL: context deadline exceeded
```
**Solution:** Increase timeout in context:
```go
ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
defer cancel()
```

### Issue: Different Results Each Run
```
Expected X, got Y (inconsistent)
```
**Solution:** LLM responses vary. Verify patterns, not exact matches:
```go
// ❌ Bad: Exact string match
if result.Text != "expected exact text" { ... }

// ✅ Good: Pattern/structure match
if !strings.Contains(result.Text, "key phrase") { ... }
if result.Language == "" { ... }  // Check presence, not exact value
```

## Integration with Development Workflow

1. **Before coding:** Write verification script defining success
2. **During coding:** Run verification to test progress
3. **After coding:** Verification passes = feature complete
4. **Before release:** Run all verifications as smoke test

## Resources

- See `examples/` for sample verification scripts
- Template: `verification/template.go`
- Artifacts: `docs/verification/`
- CI integration: Add to `.github/workflows/` or CI config
