package codemapping

import (
	"context"
	"fmt"
	"strings"

	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai/llm"
)

// Module handles code-to-platform mapping
type Module struct {
	llm       llm.Client
	analyzer  *Analyzer
	detector  *Detector
	generator *ConfigGenerator
}

// NewModule creates a new code mapping module
func NewModule(llmClient llm.Client) *Module {
	return &Module{
		llm:       llmClient,
		analyzer:  NewAnalyzer(),
		detector:  NewDetector(),
		generator: NewConfigGenerator(llmClient),
	}
}

// AnalyzeRequest contains parameters for analysis
type AnalyzeRequest struct {
	RepoPath string
	Options  AnalyzeOptions
}

// AnalyzeOptions contains optional parameters
type AnalyzeOptions struct {
	Verbose bool
}

// AnalyzeResult contains the analysis results
type AnalyzeResult struct {
	Analysis        *RepositoryAnalysis
	Config          *PlatformConfig
	Recommendations []Recommendation
}

// Analyze performs complete repository analysis and config generation
func (m *Module) Analyze(ctx context.Context, req AnalyzeRequest) (*AnalyzeResult, error) {
	// 1. Analyze repository
	analysis, err := m.analyzer.Analyze(ctx, req.RepoPath)
	if err != nil {
		return nil, fmt.Errorf("repository analysis failed: %w", err)
	}

	// 2. Detect language and framework
	analysis.PrimaryLanguage = m.detector.DetectLanguage(analysis)
	analysis.DetectedFramework = m.detector.DetectFramework(analysis)

	// 3. Generate platform config
	config, err := m.generator.Generate(ctx, analysis)
	if err != nil {
		return nil, fmt.Errorf("config generation failed: %w", err)
	}

	// 4. Generate recommendations
	recommendations := m.generateRecommendations(analysis, config)

	return &AnalyzeResult{
		Analysis:        analysis,
		Config:          config,
		Recommendations: recommendations,
	}, nil
}

// generateRecommendations creates actionable recommendations
func (m *Module) generateRecommendations(analysis *RepositoryAnalysis, config *PlatformConfig) []Recommendation {
	var recommendations []Recommendation

	// Check for health endpoint
	hasHealthCheck := false
	for _, file := range analysis.Files {
		// Simple heuristic - check for health in filenames
		if strings.Contains(strings.ToLower(file), "health") {
			hasHealthCheck = true
			break
		}
	}

	if !hasHealthCheck {
		recommendations = append(recommendations, Recommendation{
			Level:   "warning",
			Title:   "No health check endpoint found",
			Message: "Consider adding a /health endpoint for monitoring",
		})
	}

	// Check for Dockerfile
	if !analysis.HasDockerfile {
		recommendations = append(recommendations, Recommendation{
			Level:   "warning",
			Title:   "No Dockerfile found",
			Message: "Consider adding a Dockerfile for containerization",
		})
	}

	// Check for tests
	hasTests := false
	for _, file := range analysis.Files {
		lower := strings.ToLower(file)
		if strings.Contains(lower, "test") || strings.Contains(lower, "spec") {
			hasTests = true
			break
		}
	}

	if !hasTests {
		recommendations = append(recommendations, Recommendation{
			Level:   "info",
			Title:   "No test files detected",
			Message: "Consider adding tests for better code quality",
		})
	}

	// Add positive recommendations
	if analysis.HasDockerfile {
		recommendations = append(recommendations, Recommendation{
			Level:   "info",
			Title:   "Dockerfile present",
			Message: "Good! Your service is ready for containerization",
		})
	}

	if len(analysis.Dependencies) > 0 {
		recommendations = append(recommendations, Recommendation{
			Level:   "info",
			Title:   fmt.Sprintf("Detected %d dependencies", len(analysis.Dependencies)),
			Message: "Dependencies configured in platform config",
		})
	}

	if hasTests {
		recommendations = append(recommendations, Recommendation{
			Level:   "info",
			Title:   "Test files detected",
			Message: "Great! Tests help ensure code quality",
		})
	}

	// Language-specific recommendations
	switch analysis.PrimaryLanguage {
	case "go":
		// Check for go.sum
		hasGoSum := false
		for _, file := range analysis.Files {
			if file == "go.sum" {
				hasGoSum = true
				break
			}
		}
		if !hasGoSum {
			recommendations = append(recommendations, Recommendation{
				Level:   "warning",
				Title:   "No go.sum found",
				Message: "Run 'go mod tidy' to generate go.sum for dependency verification",
			})
		}

	case "nodejs":
		// Check for lockfile
		hasLockfile := false
		for _, file := range analysis.Files {
			if file == "package-lock.json" || file == "yarn.lock" || file == "pnpm-lock.yaml" {
				hasLockfile = true
				break
			}
		}
		if !hasLockfile {
			recommendations = append(recommendations, Recommendation{
				Level:   "warning",
				Title:   "No lockfile found",
				Message: "Consider committing package-lock.json or yarn.lock for reproducible builds",
			})
		}

	case "python":
		// Check for virtual environment indicator
		hasVenvConfig := false
		for _, file := range analysis.Files {
			if file == "requirements.txt" || file == "pyproject.toml" || file == "Pipfile" {
				hasVenvConfig = true
				break
			}
		}
		if hasVenvConfig {
			recommendations = append(recommendations, Recommendation{
				Level:   "info",
				Title:   "Python dependency management detected",
				Message: "Ensure you're using a virtual environment for development",
			})
		}
	}

	return recommendations
}