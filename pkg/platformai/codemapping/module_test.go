package codemapping_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai/codemapping"
)

func TestModuleAnalyzeFixtures(t *testing.T) {
	tests := []struct {
		repo          string
		wantLanguage  string
		wantFramework string
		wantRecTitle  string
	}{
		{"sample-go-repo", "go", "gin", "No go.sum found"},
		{"sample-node-repo", "nodejs", "express", "No lockfile found"},
		{"sample-python-repo", "python", "fastapi", "Python dependency management detected"},
	}

	for _, tt := range tests {
		t.Run(tt.repo, func(t *testing.T) {
			client := newFakeClient(validConfigJSON)
			got, err := codemapping.NewModule(client).Analyze(context.Background(), codemapping.AnalyzeRequest{
				RepoPath: filepath.Join(fixturesDir, tt.repo),
			})
			if err != nil {
				t.Fatalf("Analyze: %v", err)
			}
			if got.Analysis.PrimaryLanguage != tt.wantLanguage {
				t.Errorf("PrimaryLanguage = %q, want %q", got.Analysis.PrimaryLanguage, tt.wantLanguage)
			}
			if got.Analysis.DetectedFramework != tt.wantFramework {
				t.Errorf("DetectedFramework = %q, want %q", got.Analysis.DetectedFramework, tt.wantFramework)
			}
			if got.Config == nil || got.Config.Service.Name != "sample-api" {
				t.Errorf("Config = %+v, want the parsed fake response", got.Config)
			}
			if !hasTitle(got.Recommendations, tt.wantRecTitle) {
				t.Errorf("recommendations %v missing %q", got.Recommendations, tt.wantRecTitle)
			}
			if len(client.requests) != 1 {
				t.Fatalf("Generate called %d times, want 1", len(client.requests))
			}
			prompt := client.requests[0].Turns[0].Text
			if !strings.Contains(prompt, "Primary Language: "+tt.wantLanguage) ||
				!strings.Contains(prompt, "Framework: "+tt.wantFramework) {
				t.Error("detected language/framework not passed to the LLM prompt")
			}
		})
	}
}

func TestModuleAnalyzeErrors(t *testing.T) {
	errLLM := errors.New("upstream unavailable")
	goRepo := filepath.Join(fixturesDir, "sample-go-repo")

	tests := []struct {
		name      string
		client    *fakeClient
		nilClient bool
		repoPath  string
		wantIs    error
		wantMsg   string
		wantCalls int
	}{
		{
			name:     "missing path",
			client:   newFakeClient(validConfigJSON),
			repoPath: filepath.Join(t.TempDir(), "missing"),
			wantMsg:  "repository analysis failed",
		},
		{
			name:      "LLM error",
			client:    &fakeClient{err: errLLM},
			repoPath:  goRepo,
			wantIs:    errLLM,
			wantMsg:   "config generation failed",
			wantCalls: 1,
		},
		{
			name:      "non-JSON response",
			client:    newFakeClient("not json"),
			repoPath:  goRepo,
			wantMsg:   "failed to parse LLM response as JSON",
			wantCalls: 1,
		},
		{
			name:      "nil LLM client",
			nilClient: true,
			repoPath:  goRepo,
			wantMsg:   "no LLM client",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var m *codemapping.Module
			if tt.nilClient {
				// Passing tt.client would wrap a typed nil pointer in a non-nil interface.
				m = codemapping.NewModule(nil)
			} else {
				m = codemapping.NewModule(tt.client)
			}
			got, err := m.Analyze(context.Background(), codemapping.AnalyzeRequest{RepoPath: tt.repoPath})
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if got != nil {
				t.Errorf("expected nil result, got %+v", got)
			}
			if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
				t.Errorf("errors.Is(%v, %v) = false", err, tt.wantIs)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error %q does not contain %q", err, tt.wantMsg)
			}
			if tt.client != nil && len(tt.client.requests) != tt.wantCalls {
				t.Errorf("Generate called %d times, want %d", len(tt.client.requests), tt.wantCalls)
			}
		})
	}
}

func TestModuleAnalyzeCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client := newFakeClient(validConfigJSON)
	_, err := codemapping.NewModule(client).Analyze(ctx, codemapping.AnalyzeRequest{
		RepoPath: filepath.Join(fixturesDir, "sample-go-repo"),
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("errors.Is(%v, context.Canceled) = false", err)
	}
	if len(client.requests) != 0 {
		t.Errorf("Generate called %d times, want 0", len(client.requests))
	}
}

func hasTitle(recs []codemapping.Recommendation, title string) bool {
	for _, r := range recs {
		if r.Title == title {
			return true
		}
	}
	return false
}
