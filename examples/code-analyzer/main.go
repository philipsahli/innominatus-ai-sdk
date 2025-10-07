package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai"
	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai/codemapping"
)

func main() {
	var (
		outputPath string
		verbose    bool
		format     string
	)

	rootCmd := &cobra.Command{
		Use:     "platform-ai-example",
		Short:   "Platform AI SDK Example - Code Analysis Tool",
		Long:    "Analyze code repositories and generate platform configurations using AI",
		Version: platformai.Version,
	}

	analyzeCmd := &cobra.Command{
		Use:   "analyze [repository-path]",
		Short: "Analyze a repository and generate platform configuration",
		Long: `Analyze a code repository to detect its language, framework, and dependencies,
then use AI to generate an optimized platform configuration.

The analyzer will:
  • Detect programming language and framework
  • Extract dependencies and versions
  • Generate platform configuration with resource recommendations
  • Provide actionable recommendations for improvements`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoPath := args[0]

			// Validate repository path
			if _, err := os.Stat(repoPath); os.IsNotExist(err) {
				return fmt.Errorf("repository path does not exist: %s", repoPath)
			}

			// Initialize SDK
			apiKey := os.Getenv("ANTHROPIC_API_KEY")
			if apiKey == "" {
				return fmt.Errorf("ANTHROPIC_API_KEY environment variable is required")
			}

			ctx := context.Background()
			sdk, err := platformai.New(ctx, &platformai.Config{
				LLM: platformai.LLMConfig{
					Provider: "anthropic",
					APIKey:   apiKey,
					Model:    "claude-sonnet-4-5-20250929",
				},
			})
			if err != nil {
				return fmt.Errorf("failed to initialize SDK: %w", err)
			}

			// Print analysis header
			printHeader("Platform AI - Repository Analysis Report")

			fmt.Printf("\n📁 Repository: %s\n\n", repoPath)
			fmt.Println("🔍 Analyzing repository...")

			// Perform analysis
			mapper := sdk.CodeMapping()
			result, err := mapper.Analyze(ctx, codemapping.AnalyzeRequest{
				RepoPath: repoPath,
				Options: codemapping.AnalyzeOptions{
					Verbose: verbose,
				},
			})
			if err != nil {
				return fmt.Errorf("analysis failed: %w", err)
			}

			// Print detailed report
			printAnalysisReport(result)

			// Write config file
			if outputPath == "" {
				outputPath = filepath.Join(repoPath, ".platform", "config.yaml")
			}

			if err := writeConfigFile(result.Config, outputPath, format); err != nil {
				return fmt.Errorf("failed to write config: %w", err)
			}

			fmt.Printf("\n📝 Generated configuration: %s\n\n", outputPath)
			fmt.Printf("View config: cat %s\n", outputPath)

			return nil
		},
	}

	analyzeCmd.Flags().StringVarP(&outputPath, "output", "o", "", "Output path for config file (default: .platform/config.yaml)")
	analyzeCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Verbose output")
	analyzeCmd.Flags().StringVarP(&format, "format", "f", "yaml", "Output format (yaml)")

	rootCmd.AddCommand(analyzeCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func printHeader(title string) {
	line := strings.Repeat("━", 80)
	fmt.Printf("\n%s\n", line)
	fmt.Printf("📊 %s\n", title)
	fmt.Printf("%s\n", line)
}

func printAnalysisReport(result *codemapping.AnalyzeResult) {
	analysis := result.Analysis
	config := result.Config

	// Stack Detection Section
	fmt.Println("\n📦 Stack Detection:")
	fmt.Printf("  ✓ Language: %s", analysis.PrimaryLanguage)
	if analysis.LanguageVersion != "" {
		fmt.Printf(" (%s)", analysis.LanguageVersion)
	}
	fmt.Println()

	fmt.Printf("  ✓ Framework: %s\n", analysis.DetectedFramework)
	fmt.Printf("  ✓ Files Analyzed: %d\n", len(analysis.Files))

	if analysis.HasDockerfile {
		fmt.Printf("  ✓ Dockerfile: Present\n")
	}

	// Dependencies Section
	if len(analysis.Dependencies) > 0 {
		fmt.Println("\n📚 Detected Dependencies:")
		count := 0
		for name, version := range analysis.Dependencies {
			if count < 5 { // Show first 5
				fmt.Printf("  → %s: %s\n", name, version)
			}
			count++
		}
		if count > 5 {
			fmt.Printf("  ... and %d more\n", count-5)
		}
	}

	// Detected Services Section
	fmt.Println("\n🔧 Platform Services:")
	fmt.Printf("  Service: %s\n", config.Service.Name)
	fmt.Printf("  Template: %s\n", config.Service.Template)
	fmt.Printf("  Runtime: %s\n", config.Service.Runtime)
	fmt.Printf("  Port: %d\n", config.Service.Port)

	if config.Database != nil {
		fmt.Printf("\n  → Database: %s %s\n", config.Database.Type, config.Database.Version)
		fmt.Printf("    Storage: %s\n", config.Database.Storage)
	}
	if config.Cache != nil {
		fmt.Printf("\n  → Cache: %s %s\n", config.Cache.Type, config.Cache.Version)
		fmt.Printf("    Memory: %s\n", config.Cache.Memory)
	}

	// Resource Recommendations Section
	fmt.Println("\n💾 Resource Recommendations:")
	fmt.Printf("  CPU: %s\n", config.Resources.CPU)
	fmt.Printf("  Memory: %s\n", config.Resources.Memory)
	fmt.Printf("  Scaling: %d-%d replicas (target: %d%% CPU)\n",
		config.Resources.Scaling.MinReplicas,
		config.Resources.Scaling.MaxReplicas,
		config.Resources.Scaling.TargetCPUPercent,
	)

	// Monitoring Section
	fmt.Println("\n📊 Monitoring:")
	fmt.Printf("  Metrics: %v\n", config.Monitoring.Metrics)
	fmt.Printf("  Logs: %v\n", config.Monitoring.Logs)
	fmt.Printf("  Traces: %v\n", config.Monitoring.Traces)

	// Recommendations Section
	if len(result.Recommendations) > 0 {
		fmt.Println("\n💡 Recommendations:")
		for _, rec := range result.Recommendations {
			icon := "ℹ️"
			switch rec.Level {
			case "warning":
				icon = "⚠️"
			case "critical":
				icon = "🔴"
			case "info":
				icon = "✅"
			}
			fmt.Printf("  %s %s\n", icon, rec.Title)
			if rec.Message != "" {
				fmt.Printf("     %s\n", rec.Message)
			}
		}
	}

	fmt.Printf("\n%s\n", strings.Repeat("━", 80))
}

func writeConfigFile(config *codemapping.PlatformConfig, outputPath, format string) error {
	// Ensure directory exists
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}

	// Add header comment
	header := `# Platform Configuration
# Auto-generated by Platform AI SDK
# Generated: ` + time.Now().Format(time.RFC3339) + `

`

	var data []byte
	var err error

	switch format {
	case "yaml":
		data, err = yaml.Marshal(config)
		if err != nil {
			return err
		}
		data = append([]byte(header), data...)
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}

	return os.WriteFile(outputPath, data, 0600)
}