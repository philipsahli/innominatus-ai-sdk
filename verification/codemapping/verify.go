package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai"
	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai/codemapping"
	"github.com/philipsahli/innominatus-ai-sdk/verification/common"
)

func main() {
	ctx := context.Background()

	common.PrintHeader("🔧 Code Mapping Verification")

	// Setup
	fmt.Println("1. Setting up SDK...")
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		common.Fail("ANTHROPIC_API_KEY not set")
	}

	sdk, err := platformai.New(ctx, &platformai.Config{
		LLM: platformai.LLMConfig{
			Provider: "anthropic",
			APIKey:   apiKey,
			Model:    "claude-sonnet-4-5-20250929",
		},
	})
	if err != nil {
		common.Fail("SDK initialization failed: %v", err)
	}
	common.Pass("SDK initialized")

	// Execute
	fmt.Println("\n2. Analyzing test repository...")
	testRepoPath := "../../testdata/sample-go-repo"
	if _, err := os.Stat(testRepoPath); os.IsNotExist(err) {
		common.Fail("Test repository not found at %s", testRepoPath)
	}

	mapper := sdk.CodeMapping()
	result, err := mapper.Analyze(ctx, codemapping.AnalyzeRequest{
		RepoPath: testRepoPath,
		Options: codemapping.AnalyzeOptions{
			Verbose: false,
		},
	})
	if err != nil {
		common.Fail("Analysis failed: %v", err)
	}
	common.Pass("Analysis completed")

	// Verify
	fmt.Println("\n3. Verifying results...")

	if result.Analysis.PrimaryLanguage == "" {
		common.Fail("No primary language detected")
	}
	common.Pass("Primary language detected: %s", result.Analysis.PrimaryLanguage)

	if len(result.Analysis.Files) == 0 {
		common.Fail("No files analyzed")
	}
	common.Pass("Analyzed %d files", len(result.Analysis.Files))

	if result.Config == nil {
		common.Fail("No platform config generated")
	}
	common.Pass("Platform config generated")

	if len(result.Recommendations) == 0 {
		fmt.Println("  ⚠️  Warning: No recommendations generated")
	} else {
		common.Pass("%d recommendations generated", len(result.Recommendations))
	}

	// Save artifacts
	fmt.Println("\n4. Saving artifacts...")
	path, err := common.SaveJSON("codemapping", buildArtifact(result))
	if err != nil {
		common.Fail("Failed to save artifacts: %v", err)
	}
	fmt.Printf("  Saved: %s\n", path)

	// Summary
	fmt.Println()
	common.PrintHeader("📊 Verification Summary")
	fmt.Printf("Repository: %s\n", testRepoPath)
	fmt.Printf("Language:   %s\n", result.Analysis.PrimaryLanguage)
	fmt.Printf("Framework:  %s\n", result.Analysis.DetectedFramework)
	fmt.Printf("Files:      %d\n", len(result.Analysis.Files))
	if len(result.Analysis.Dependencies) > 0 {
		fmt.Printf("Dependencies: %d\n", len(result.Analysis.Dependencies))
	}
	fmt.Printf("Recommendations: %d\n", len(result.Recommendations))

	fmt.Println("\n✅ PASS: Code mapping verification completed successfully")
}

func buildArtifact(result *codemapping.AnalyzeResult) map[string]interface{} {
	return map[string]interface{}{
		"timestamp":       time.Now(),
		"verification":    "code-mapping",
		"analysis":        result.Analysis,
		"config":          result.Config,
		"recommendations": result.Recommendations,
	}
}
