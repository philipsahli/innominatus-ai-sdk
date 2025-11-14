package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai"
	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai/codemapping"
)

func main() {
	ctx := context.Background()

	fmt.Println("🔧 Code Mapping Verification")
	fmt.Println("===========================")

	// Setup
	fmt.Println("1. Setting up SDK...")
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		fail("ANTHROPIC_API_KEY not set")
	}

	sdk, err := platformai.New(ctx, &platformai.Config{
		LLM: platformai.LLMConfig{
			Provider: "anthropic",
			APIKey:   apiKey,
			Model:    "claude-sonnet-4-5-20250929",
		},
	})
	if err != nil {
		fail("SDK initialization failed: %v", err)
	}
	pass("SDK initialized")

	// Execute
	fmt.Println("\n2. Analyzing test repository...")
	testRepoPath := "../../testdata/sample-go-repo"
	if _, err := os.Stat(testRepoPath); os.IsNotExist(err) {
		fail("Test repository not found at %s", testRepoPath)
	}

	mapper := sdk.CodeMapping()
	result, err := mapper.Analyze(ctx, codemapping.AnalyzeRequest{
		RepoPath: testRepoPath,
		Options: codemapping.AnalyzeOptions{
			Verbose: false,
		},
	})
	if err != nil {
		fail("Analysis failed: %v", err)
	}
	pass("Analysis completed")

	// Verify
	fmt.Println("\n3. Verifying results...")

	if result.Analysis.PrimaryLanguage == "" {
		fail("No primary language detected")
	}
	pass("Primary language detected: %s", result.Analysis.PrimaryLanguage)

	if len(result.Analysis.Files) == 0 {
		fail("No files analyzed")
	}
	pass("Analyzed %d files", len(result.Analysis.Files))

	if result.Config == nil {
		fail("No platform config generated")
	}
	pass("Platform config generated")

	if len(result.Recommendations) == 0 {
		fmt.Println("  ⚠️  Warning: No recommendations generated")
	} else {
		pass("%d recommendations generated", len(result.Recommendations))
	}

	// Save artifacts
	fmt.Println("\n4. Saving artifacts...")
	if err := saveArtifact(result); err != nil {
		fail("Failed to save artifacts: %v", err)
	}

	// Summary
	fmt.Println("\n📊 Verification Summary")
	fmt.Println("======================")
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

func saveArtifact(result *codemapping.AnalyzeResult) error {
	outputDir := "../../docs/verification"
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return err
	}

	timestamp := time.Now().Format("20060102-150405")
	filename := filepath.Join(outputDir, fmt.Sprintf("codemapping-%s.json", timestamp))

	artifact := map[string]interface{}{
		"timestamp":       time.Now(),
		"verification":    "code-mapping",
		"analysis":        result.Analysis,
		"config":          result.Config,
		"recommendations": result.Recommendations,
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
