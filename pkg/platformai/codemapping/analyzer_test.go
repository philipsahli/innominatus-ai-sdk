package codemapping_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai/codemapping"
)

func TestAnalyzerFixtures(t *testing.T) {
	tests := []struct {
		name           string
		repo           string
		wantFiles      []string
		wantVersion    string
		wantDeps       map[string]string // subset; fixtures carry more
		wantDepCount   int
		wantDockerfile string // substring of DockerfileContent
	}{
		{
			name:        "go",
			repo:        "sample-go-repo",
			wantFiles:   []string{"Dockerfile", "go.mod", "main.go"},
			wantVersion: "1.21",
			wantDeps: map[string]string{
				"github.com/gin-gonic/gin": "v1.9.1",
				"github.com/lib/pq":        "v1.10.9",
				"golang.org/x/net":         "v0.10.0",
			},
			wantDepCount:   28,
			wantDockerfile: "FROM golang:1.21-alpine",
		},
		{
			name:        "node",
			repo:        "sample-node-repo",
			wantFiles:   []string{"Dockerfile", "index.js", "package.json"},
			wantVersion: ">=18.0.0",
			wantDeps: map[string]string{
				"express": "^4.18.2",
				"jest":    "^29.7.0",
			},
			wantDepCount:   9,
			wantDockerfile: "FROM node:18-alpine",
		},
		{
			name:        "python",
			repo:        "sample-python-repo",
			wantFiles:   []string{"Dockerfile", "main.py", "requirements.txt"},
			wantVersion: "",
			wantDeps: map[string]string{
				"fastapi":           "0.104.1",
				"uvicorn[standard]": "0.24.0",
			},
			wantDepCount:   8,
			wantDockerfile: "FROM python:3.11-slim",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := codemapping.NewAnalyzer().Analyze(context.Background(), filepath.Join(fixturesDir, tt.repo))
			if err != nil {
				t.Fatalf("Analyze: %v", err)
			}
			assertFiles(t, got.Files, tt.wantFiles)
			if got.LanguageVersion != tt.wantVersion {
				t.Errorf("LanguageVersion = %q, want %q", got.LanguageVersion, tt.wantVersion)
			}
			if len(got.Dependencies) != tt.wantDepCount {
				t.Errorf("got %d dependencies, want %d", len(got.Dependencies), tt.wantDepCount)
			}
			for name, version := range tt.wantDeps {
				if got.Dependencies[name] != version {
					t.Errorf("dependency %q = %q, want %q", name, got.Dependencies[name], version)
				}
			}
			if !got.HasDockerfile {
				t.Error("HasDockerfile = false, want true")
			}
			if !strings.Contains(got.DockerfileContent, tt.wantDockerfile) {
				t.Errorf("DockerfileContent does not contain %q", tt.wantDockerfile)
			}
			if got.PrimaryLanguage != "" || got.DetectedFramework != "" {
				t.Errorf("Analyze should leave detection to Detector, got %q/%q", got.PrimaryLanguage, got.DetectedFramework)
			}
		})
	}
}

func TestAnalyzerParsers(t *testing.T) {
	tests := []struct {
		name        string
		files       map[string]string
		wantFiles   []string
		wantVersion string
		wantDeps    map[string]string
	}{
		{
			name:      "empty repo",
			files:     map[string]string{},
			wantFiles: []string{},
			wantDeps:  map[string]string{},
		},
		{
			name: "go.mod single-line require and comments in block",
			files: map[string]string{"go.mod": `module example.com/x

go 1.22

require github.com/labstack/echo/v4 v4.11.0

require (
	// a comment line
	github.com/go-chi/chi/v5 v5.0.10
	lonely
)
`},
			wantFiles:   []string{"go.mod"},
			wantVersion: "1.22",
			wantDeps: map[string]string{
				"github.com/labstack/echo/v4": "v4.11.0",
				"github.com/go-chi/chi/v5":    "v5.0.10",
			},
		},
		{
			name:      "package.json that is not valid JSON",
			files:     map[string]string{"package.json": "{not json"},
			wantFiles: []string{"package.json"},
			wantDeps:  map[string]string{},
		},
		{
			name:      "package.json without engines",
			files:     map[string]string{"package.json": `{"dependencies": {"fastify": "^4.0.0"}}`},
			wantFiles: []string{"package.json"},
			wantDeps:  map[string]string{"fastify": "^4.0.0"},
		},
		{
			name: "requirements.txt operators, comments and bare names",
			files: map[string]string{"requirements.txt": `# comment

flask==2.3.0
gunicorn>=21.0
requests
`},
			wantFiles: []string{"requirements.txt"},
			wantDeps: map[string]string{
				"flask":    "2.3.0",
				"gunicorn": ">=21.0",
				"requests": "*",
			},
		},
		{
			// Pins today's behaviour, see #28: unsupported operators and pip
			// options become dependency names, markers stay in the version.
			name: "requirements.txt edge cases (#28)",
			files: map[string]string{"requirements.txt": `django~=4.2
-r base.txt
--index-url https://example.com/simple
pytest<8
attrs==23.1; python_version<"3.8"
`},
			wantFiles: []string{"requirements.txt"},
			wantDeps: map[string]string{
				"django~=4.2":                            "*",
				"-r base.txt":                            "*",
				"--index-url https://example.com/simple": "*",
				"pytest<8":                               "*",
				"attrs":                                  `23.1; python_version<"3.8"`,
			},
		},
		{
			name: "pyproject.toml poetry dependencies",
			files: map[string]string{"pyproject.toml": `[tool.poetry]
name = "svc"

[tool.poetry.dependencies]
python = "^3.11"
fastapi = "^0.100.0"
uvicorn = '0.23'

[tool.poetry.dev-dependencies]
pytest = "^7.0"
`},
			wantFiles:   []string{"pyproject.toml"},
			wantVersion: "^3.11",
			wantDeps: map[string]string{
				"fastapi": "^0.100.0",
				"uvicorn": "0.23",
			},
		},
		{
			name: "pyproject.toml project.dependencies table",
			files: map[string]string{"pyproject.toml": `[project.dependencies]
flask = ">=2"
`},
			wantFiles: []string{"pyproject.toml"},
			wantDeps:  map[string]string{"flask": ">=2"},
		},
		{
			// Pins today's behaviour, see #28: PEP 621 arrays under [project]
			// are not read, and a commented line inside a deps table is.
			name: "pyproject.toml PEP 621 and comments (#28)",
			files: map[string]string{"pyproject.toml": `[project]
name = "svc"
dependencies = ["flask>=2"]

[tool.poetry.dependencies]
# old = "1.0"
`},
			wantFiles: []string{"pyproject.toml"},
			wantDeps:  map[string]string{"# old": "1.0"},
		},
		{
			name: "skipped directories",
			files: map[string]string{
				"main.py":                 "",
				".git/config":             "",
				"node_modules/x/index.js": "",
				"vendor/lib.go":           "",
				"__pycache__/a.pyc":       "",
				"dist/app.js":             "",
				"build/out":               "",
				".next/cache":             "",
			},
			wantFiles: []string{"main.py"},
			wantDeps:  map[string]string{},
		},
		{
			// Pins today's behaviour, see #28: manifests at any depth are
			// merged into the root's dependencies.
			name: "nested manifests are merged (#28)",
			files: map[string]string{
				"go.mod":             "module a\n\nrequire github.com/gin-gonic/gin v1.9.1\n",
				"web/package.json":   `{"dependencies": {"react": "^18.0.0"}}`,
				"web/sub/Dockerfile": "FROM node:20",
			},
			wantFiles: []string{"go.mod", "web/package.json", "web/sub/Dockerfile"},
			wantDeps: map[string]string{
				"github.com/gin-gonic/gin": "v1.9.1",
				"react":                    "^18.0.0",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := writeRepo(t, tt.files)
			got, err := codemapping.NewAnalyzer().Analyze(context.Background(), root)
			if err != nil {
				t.Fatalf("Analyze: %v", err)
			}
			assertFiles(t, got.Files, tt.wantFiles)
			if got.LanguageVersion != tt.wantVersion {
				t.Errorf("LanguageVersion = %q, want %q", got.LanguageVersion, tt.wantVersion)
			}
			assertDeps(t, got.Dependencies, tt.wantDeps)
		})
	}
}

func TestAnalyzerRootEdgeCases(t *testing.T) {
	t.Run("root named like a skipped directory returns no files (#28)", func(t *testing.T) {
		// Pins today's behaviour, see #28.
		root := filepath.Join(t.TempDir(), "build")
		writeFiles(t, root, map[string]string{"main.go": "package main"})

		got, err := codemapping.NewAnalyzer().Analyze(context.Background(), root)
		if err != nil {
			t.Fatalf("Analyze: %v", err)
		}
		assertFiles(t, got.Files, []string{})
	})

	t.Run("regular file as root (#28)", func(t *testing.T) {
		// Pins today's behaviour, see #28.
		path := filepath.Join(t.TempDir(), "main.go")
		if err := os.WriteFile(path, []byte("package main"), 0o600); err != nil {
			t.Fatal(err)
		}

		got, err := codemapping.NewAnalyzer().Analyze(context.Background(), path)
		if err != nil {
			t.Fatalf("Analyze: %v", err)
		}
		assertFiles(t, got.Files, []string{"."})
	})
}

func TestAnalyzerErrors(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name    string
		ctx     context.Context
		path    string
		wantIs  error
		wantMsg string
	}{
		{
			name:    "missing path",
			ctx:     context.Background(),
			path:    filepath.Join(t.TempDir(), "does-not-exist"),
			wantMsg: "does not exist",
		},
		{
			name:   "cancelled context",
			ctx:    cancelled,
			path:   filepath.Join(fixturesDir, "sample-go-repo"),
			wantIs: context.Canceled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := codemapping.NewAnalyzer().Analyze(tt.ctx, tt.path)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if got != nil {
				t.Errorf("expected nil analysis, got %+v", got)
			}
			if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
				t.Errorf("errors.Is(%v, %v) = false", err, tt.wantIs)
			}
			if tt.wantMsg != "" && !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error %q does not contain %q", err, tt.wantMsg)
			}
		})
	}
}
