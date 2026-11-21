package codemapping_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai/codemapping"
	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai/llm"
)

func TestConfigGeneratorGenerate(t *testing.T) {
	analysis := &codemapping.RepositoryAnalysis{
		PrimaryLanguage:   "go",
		DetectedFramework: "gin",
		LanguageVersion:   "1.21",
		HasDockerfile:     true,
		Files:             []string{"main.go", "go.mod"},
		Dependencies:      map[string]string{"github.com/gin-gonic/gin": "v1.9.1"},
	}

	client := newFakeClient(validConfigJSON)
	got, err := codemapping.NewConfigGenerator(client).Generate(context.Background(), analysis)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if got.Service.Name != "sample-api" || got.Service.Port != 8080 || got.Service.Framework != "gin" {
		t.Errorf("unexpected service config: %+v", got.Service)
	}
	if got.Resources.Scaling.MaxReplicas != 10 {
		t.Errorf("MaxReplicas = %d, want 10", got.Resources.Scaling.MaxReplicas)
	}
	if got.Database == nil || got.Database.Type != "postgresql" {
		t.Errorf("Database = %+v, want postgresql", got.Database)
	}
	if got.Cache != nil {
		t.Errorf("Cache = %+v, want nil", got.Cache)
	}
	if got.Security.HealthCheck.Path != "/health" {
		t.Errorf("HealthCheck.Path = %q, want /health", got.Security.HealthCheck.Path)
	}

	if len(client.requests) != 1 {
		t.Fatalf("Generate called %d times, want 1", len(client.requests))
	}
	if client.otherMethodCalls != 0 {
		t.Errorf("unexpected calls to other llm.Client methods: %d", client.otherMethodCalls)
	}
	req := client.requests[0]
	if req.Temperature != 0.3 || req.MaxTokens != 4096 {
		t.Errorf("Temperature/MaxTokens = %v/%d, want 0.3/4096", req.Temperature, req.MaxTokens)
	}
	if req.SystemPrompt == "" {
		t.Error("SystemPrompt is empty")
	}
	for _, want := range []string{
		"Primary Language: go",
		"Framework: gin",
		"Language Version: 1.21",
		"Has Dockerfile: true",
		"Total Files: 2",
		"Total Dependencies: 1",
		"  - github.com/gin-gonic/gin: v1.9.1",
		"main.go\ngo.mod",
	} {
		if !strings.Contains(req.UserPrompt, want) {
			t.Errorf("UserPrompt does not contain %q", want)
		}
	}
}

func TestConfigGeneratorPromptLimits(t *testing.T) {
	analysis := &codemapping.RepositoryAnalysis{Dependencies: map[string]string{}}
	for i := 0; i < 20; i++ {
		analysis.Files = append(analysis.Files, fmt.Sprintf("file%02d.go", i))
	}
	for i := 0; i < 12; i++ {
		analysis.Dependencies[fmt.Sprintf("dep%02d", i)] = "v1"
	}

	client := newFakeClient(validConfigJSON)
	if _, err := codemapping.NewConfigGenerator(client).Generate(context.Background(), analysis); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	prompt := client.requests[0].UserPrompt

	for _, want := range []string{"Total Files: 20", "Total Dependencies: 12", "file00.go", "file14.go"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("UserPrompt does not contain %q", want)
		}
	}
	if strings.Contains(prompt, "file15.go") {
		t.Error("UserPrompt lists more than 15 files")
	}

	// Which 10 of the 12 dependencies appear depends on map iteration, so only count them.
	depLines := 0
	for _, line := range strings.Split(prompt, "\n") {
		if strings.HasPrefix(line, "  - dep") {
			depLines++
		}
	}
	if depLines != 10 {
		t.Errorf("prompt lists %d dependencies, want 10", depLines)
	}
}

func TestConfigGeneratorErrors(t *testing.T) {
	errLLM := errors.New("rate limited")
	analysis := &codemapping.RepositoryAnalysis{Dependencies: map[string]string{}}

	tests := []struct {
		name     string
		client   llm.Client
		analysis *codemapping.RepositoryAnalysis
		wantIs   error
		wantMsg  string
	}{
		{
			name:     "nil client",
			client:   nil,
			analysis: analysis,
			wantMsg:  "no LLM client",
		},
		{
			name:     "nil analysis",
			client:   newFakeClient(validConfigJSON),
			analysis: nil,
			wantMsg:  "analysis is nil",
		},
		{
			name:     "nil response without error",
			client:   &fakeClient{},
			analysis: analysis,
			wantMsg:  "no response",
		},
		{
			name:     "LLM error",
			client:   &fakeClient{err: errLLM},
			analysis: analysis,
			wantIs:   errLLM,
			wantMsg:  "LLM generation failed",
		},
		{
			name:     "non-JSON response",
			client:   newFakeClient("Here is your config: ```yaml```"),
			analysis: analysis,
			wantMsg:  "failed to parse LLM response as JSON",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := codemapping.NewConfigGenerator(tt.client).Generate(context.Background(), tt.analysis)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if got != nil {
				t.Errorf("expected nil config, got %+v", got)
			}
			if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
				t.Errorf("errors.Is(%v, %v) = false", err, tt.wantIs)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error %q does not contain %q", err, tt.wantMsg)
			}
		})
	}
}
