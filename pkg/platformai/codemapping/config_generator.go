package codemapping

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai/llm"
)

// ConfigGenerator generates platform configuration using LLM
type ConfigGenerator struct {
	llm llm.Client
}

// NewConfigGenerator creates a new config generator
func NewConfigGenerator(llmClient llm.Client) *ConfigGenerator {
	return &ConfigGenerator{llm: llmClient}
}

// Generate creates platform configuration based on repository analysis
func (g *ConfigGenerator) Generate(ctx context.Context, analysis *RepositoryAnalysis) (*PlatformConfig, error) {
	systemPrompt := `You are a Platform Engineering expert who generates optimal platform configurations.

Your task: Analyze repository information and generate a complete platform configuration.

Guidelines:
- Choose appropriate resource allocations based on tech stack
- Set realistic scaling parameters
- Configure monitoring and health checks
- Add necessary dependencies (database, cache, etc.)
- Follow platform best practices

Output: Valid JSON matching the PlatformConfig schema.`

	// Prepare file list summary
	fileList := analysis.Files
	if len(fileList) > 15 {
		fileList = fileList[:15]
	}

	// Prepare dependency summary
	depSummary := []string{}
	count := 0
	for name, version := range analysis.Dependencies {
		if count < 10 {
			depSummary = append(depSummary, fmt.Sprintf("  - %s: %s", name, version))
			count++
		}
	}

	userPrompt := fmt.Sprintf(`Analyze this repository and generate platform configuration:

Repository Analysis:
- Primary Language: %s
- Framework: %s
- Language Version: %s
- Has Dockerfile: %v
- Total Files: %d
- Total Dependencies: %d

Key Dependencies:
%s

Sample Files:
%s

Generate a complete platform configuration as JSON with these fields:
{
  "service": {
    "name": "string (infer from repo, use lowercase with hyphens)",
    "template": "string (e.g., 'microservice', 'web-app', 'api')",
    "runtime": "string (e.g., 'go1.21', 'node20', 'python3.11')",
    "framework": "string (detected framework)",
    "port": 8080
  },
  "resources": {
    "cpu": "string (e.g., '500m', '1000m')",
    "memory": "string (e.g., '512Mi', '1Gi')",
    "scaling": {
      "min_replicas": 2,
      "max_replicas": 10,
      "target_cpu_percent": 70
    }
  },
  "database": {
    "type": "string (e.g., 'postgresql', 'mysql', 'mongodb' or null if not needed)",
    "version": "string",
    "storage": "string (e.g., '10Gi')"
  },
  "cache": {
    "type": "string (e.g., 'redis', 'memcached' or null if not needed)",
    "version": "string",
    "memory": "string (e.g., '256Mi')"
  },
  "monitoring": {
    "metrics": true,
    "logs": true,
    "traces": true
  },
  "security": {
    "health_check": {
      "path": "/health",
      "port": 8080
    }
  }
}

Rules:
1. If no database/cache dependencies detected, set those fields to null
2. Use appropriate resource sizes based on language (Go: smaller, Node/Python: larger)
3. Set port based on framework defaults
4. Ensure JSON is valid and properly formatted

Respond with ONLY valid JSON, no markdown or explanation.`,
		analysis.PrimaryLanguage,
		analysis.DetectedFramework,
		analysis.LanguageVersion,
		analysis.HasDockerfile,
		len(analysis.Files),
		len(analysis.Dependencies),
		strings.Join(depSummary, "\n"),
		strings.Join(fileList, "\n"),
	)

	response, err := g.llm.Generate(ctx, llm.GenerateRequest{
		SystemPrompt: systemPrompt,
		UserPrompt:   userPrompt,
		Temperature:  0.3,
		MaxTokens:    4096,
	})
	if err != nil {
		return nil, fmt.Errorf("LLM generation failed: %w", err)
	}

	// Parse JSON response
	var config PlatformConfig
	if err := json.Unmarshal([]byte(response.Text), &config); err != nil {
		return nil, fmt.Errorf("failed to parse LLM response as JSON: %w (response: %s)", err, response.Text)
	}

	return &config, nil
}