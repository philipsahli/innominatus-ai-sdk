package main

import (
	"context"
	"fmt"
	"os"

	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai"
	"github.com/philipsahli/innominatus-ai-sdk/verification/common"
)

// Verification script template for testing features end-to-end.
// Copy it to verification/<feature>/verify.go and run it from that directory:
// common.SaveJSON writes artifacts relative to it, to docs/verification/.

func main() {
	ctx := context.Background()

	// 1. SETUP: Validate environment and initialize
	fmt.Println("🔧 Setting up verification...")

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		common.Fail("ANTHROPIC_API_KEY environment variable not set")
	}

	config := &platformai.Config{
		LLM: platformai.LLMConfig{
			Provider: "anthropic",
			APIKey:   apiKey,
			Model:    "claude-sonnet-5-5",
		},
	}

	sdk, err := platformai.New(ctx, config)
	if err != nil {
		common.Fail("Failed to initialize SDK: %v", err)
	}

	// 2. EXECUTE: Run actual feature code (not mocks!)
	fmt.Println("\n▶️  Executing feature...")

	// Example: Analyze a repository
	// Customize this section for your specific feature
	result, err := performFeatureOperation(ctx, sdk)
	if err != nil {
		common.Fail("Feature operation failed: %v", err)
	}

	// 3. VERIFY: Check outputs meet requirements
	fmt.Println("\n✓ Verifying results...")

	if err := verifyResults(result); err != nil {
		common.Fail("Verification failed: %v", err)
	}

	// 4. SAVE ARTIFACTS: Capture outputs for review
	fmt.Println("\n💾 Saving artifacts...")

	// CUSTOMIZE the artifact name
	path, err := common.SaveJSON("my-feature", result)
	if err != nil {
		common.Fail("Failed to save artifacts: %v", err)
	}
	fmt.Printf("  Saved: %s\n", path)

	// 5. REPORT SUCCESS
	common.Pass("Verification completed successfully")
	printSummary(result)
}

// performFeatureOperation runs the actual feature code
// CUSTOMIZE THIS for your specific feature
func performFeatureOperation(ctx context.Context, sdk *platformai.SDK) (interface{}, error) {
	// Example implementation - replace with your feature
	/*
		mapper := sdk.CodeMapping()
		result, err := mapper.Analyze(ctx, codemapping.AnalyzeRequest{
			RepoPath: "/path/to/test/repo",
		})
		return result, err
	*/

	// Placeholder - replace with actual implementation
	return map[string]string{
		"status": "success",
		"result": "feature executed",
	}, nil
}

// verifyResults checks that outputs meet requirements
// CUSTOMIZE THIS for your specific feature
func verifyResults(result interface{}) error {
	// Example verification - replace with your checks
	/*
		analysis := result.(*codemapping.AnalyzeResult)

		if analysis.Analysis.PrimaryLanguage == "" {
			return fmt.Errorf("no primary language detected")
		}

		if analysis.Config == nil {
			return fmt.Errorf("no config generated")
		}

		if len(analysis.Recommendations) == 0 {
			return fmt.Errorf("no recommendations generated")
		}
	*/

	// Placeholder - replace with actual verification
	if result == nil {
		return fmt.Errorf("result is nil")
	}

	return nil
}

// printSummary displays verification results
// CUSTOMIZE THIS for your specific feature
func printSummary(result interface{}) {
	fmt.Println("\n📊 Summary:")

	// Example summary - replace with your details
	/*
		analysis := result.(*codemapping.AnalyzeResult)
		fmt.Printf("  Language: %s\n", analysis.Analysis.PrimaryLanguage)
		fmt.Printf("  Framework: %s\n", analysis.Analysis.DetectedFramework)
		fmt.Printf("  Files Analyzed: %d\n", len(analysis.Analysis.Files))
		fmt.Printf("  Recommendations: %d\n", len(analysis.Recommendations))
	*/

	// Placeholder - replace with actual summary
	fmt.Printf("  Result: %+v\n", result)
}
