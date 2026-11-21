package codemapping

import (
	"reflect"
	"testing"
)

// Internal test: generateRecommendations is unexported and reaching every
// branch through Module.Analyze would need a fixture repo per case.
func TestGenerateRecommendations(t *testing.T) {
	const (
		noHealth    = "No health check endpoint found"
		noDocker    = "No Dockerfile found"
		noTests     = "No test files detected"
		dockerOK    = "Dockerfile present"
		testsOK     = "Test files detected"
		noGoSum     = "No go.sum found"
		noLockfile  = "No lockfile found"
		pythonDeps  = "Python dependency management detected"
		twoDepsSeen = "Detected 2 dependencies"
	)

	tests := []struct {
		name     string
		analysis *RepositoryAnalysis
		want     []string
	}{
		{
			name:     "bare go repo",
			analysis: &RepositoryAnalysis{PrimaryLanguage: "go", Files: []string{"main.go"}},
			want:     []string{noHealth, noDocker, noTests, noGoSum},
		},
		{
			name: "complete go repo",
			analysis: &RepositoryAnalysis{
				PrimaryLanguage: "go",
				Files:           []string{"main.go", "health.go", "main_test.go", "go.sum"},
				HasDockerfile:   true,
				Dependencies:    map[string]string{"a": "1", "b": "2"},
			},
			want: []string{dockerOK, twoDepsSeen, testsOK},
		},
		{
			name: "node without lockfile",
			analysis: &RepositoryAnalysis{
				PrimaryLanguage: "nodejs",
				Files:           []string{"index.js", "routes/Health.js", "index.spec.js"},
				HasDockerfile:   true,
			},
			want: []string{dockerOK, testsOK, noLockfile},
		},
		{
			name: "node with lockfile",
			analysis: &RepositoryAnalysis{
				PrimaryLanguage: "nodejs",
				Files:           []string{"index.js", "health.js", "a.test.js", "yarn.lock"},
				HasDockerfile:   true,
			},
			want: []string{dockerOK, testsOK},
		},
		{
			name: "python with requirements",
			analysis: &RepositoryAnalysis{
				PrimaryLanguage: "python",
				Files:           []string{"health.py", "test_app.py", "requirements.txt"},
				HasDockerfile:   true,
			},
			want: []string{dockerOK, testsOK, pythonDeps},
		},
		{
			name: "python without dependency file",
			analysis: &RepositoryAnalysis{
				PrimaryLanguage: "python",
				Files:           []string{"health.py", "test_app.py"},
				HasDockerfile:   true,
			},
			want: []string{dockerOK, testsOK},
		},
		{
			name: "unknown language gets no language-specific advice",
			analysis: &RepositoryAnalysis{
				PrimaryLanguage: "unknown",
				Files:           []string{"health.txt", "tests/run.sh"},
				HasDockerfile:   true,
			},
			want: []string{dockerOK, testsOK},
		},
		{
			// Pins today's behaviour, see #28: any path containing "test",
			// "spec" or "health" matches the substring heuristics.
			name: "substring heuristics (#28)",
			analysis: &RepositoryAnalysis{
				PrimaryLanguage: "unknown",
				Files:           []string{"latest.go", "inspect.go", "unhealthy.go"},
				HasDockerfile:   true,
			},
			want: []string{dockerOK, testsOK},
		},
	}

	m := &Module{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recs := m.generateRecommendations(tt.analysis, &PlatformConfig{})
			got := make([]string, 0, len(recs))
			for _, r := range recs {
				got = append(got, r.Title)
				if r.Level != "info" && r.Level != "warning" {
					t.Errorf("recommendation %q has level %q", r.Title, r.Level)
				}
				if r.Message == "" {
					t.Errorf("recommendation %q has no message", r.Title)
				}
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("titles = %v, want %v", got, tt.want)
			}
		})
	}
}
