package codemapping

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Analyzer analyzes repository structure and content
type Analyzer struct{}

// NewAnalyzer creates a new repository analyzer
func NewAnalyzer() *Analyzer {
	return &Analyzer{}
}

// Analyze scans a repository and extracts relevant information
func (a *Analyzer) Analyze(ctx context.Context, repoPath string) (*RepositoryAnalysis, error) {
	// Check if path exists
	if _, err := os.Stat(repoPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("repository path does not exist: %s", repoPath)
	}

	analysis := &RepositoryAnalysis{
		Files:        []string{},
		Dependencies: make(map[string]string),
	}

	// Walk directory and detect files
	err := filepath.Walk(repoPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Check for context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Skip common directories
		if info.IsDir() {
			name := info.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" || name == "__pycache__" || name == "dist" || name == "build" || name == ".next" {
				return filepath.SkipDir
			}
			return nil
		}

		relPath, _ := filepath.Rel(repoPath, path)
		analysis.Files = append(analysis.Files, relPath)

		// Process special files
		switch info.Name() {
		case "go.mod":
			a.parseGoMod(path, analysis)
		case "package.json":
			a.parsePackageJSON(path, analysis)
		case "requirements.txt":
			a.parseRequirementsTxt(path, analysis)
		case "pyproject.toml":
			a.parsePyprojectToml(path, analysis)
		case "Dockerfile":
			analysis.HasDockerfile = true
			// #nosec G304 - path is validated by filepath.Walk and comes from repository scan
			content, _ := os.ReadFile(path)
			analysis.DockerfileContent = string(content)
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk repository: %w", err)
	}

	return analysis, nil
}

// parseGoMod extracts Go module information
func (a *Analyzer) parseGoMod(path string, analysis *RepositoryAnalysis) {
	// #nosec G304 - path is validated by filepath.Walk and comes from repository scan
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	inRequireBlock := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Extract Go version
		if strings.HasPrefix(line, "go ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				analysis.LanguageVersion = parts[1]
			}
		}

		// Parse dependencies
		if strings.HasPrefix(line, "require (") {
			inRequireBlock = true
			continue
		}

		if inRequireBlock {
			if line == ")" {
				inRequireBlock = false
				continue
			}

			parts := strings.Fields(line)
			if len(parts) >= 2 {
				dep := strings.TrimSpace(parts[0])
				version := strings.TrimSpace(parts[1])
				if !strings.HasPrefix(dep, "//") {
					analysis.Dependencies[dep] = version
				}
			}
		} else if strings.HasPrefix(line, "require ") {
			// Single line require
			parts := strings.Fields(strings.TrimPrefix(line, "require "))
			if len(parts) >= 2 {
				analysis.Dependencies[parts[0]] = parts[1]
			}
		}
	}
}

// parsePackageJSON extracts Node.js package information
func (a *Analyzer) parsePackageJSON(path string, analysis *RepositoryAnalysis) {
	// #nosec G304 - path is validated by filepath.Walk and comes from repository scan
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}

	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
		Engines         struct {
			Node string `json:"node"`
		} `json:"engines"`
	}

	if err := json.Unmarshal(data, &pkg); err != nil {
		return
	}

	// Extract Node version
	if pkg.Engines.Node != "" {
		analysis.LanguageVersion = pkg.Engines.Node
	}

	// Merge dependencies
	for name, version := range pkg.Dependencies {
		analysis.Dependencies[name] = version
	}
	for name, version := range pkg.DevDependencies {
		analysis.Dependencies[name] = version
	}
}

// parseRequirementsTxt extracts Python dependencies from requirements.txt
func (a *Analyzer) parseRequirementsTxt(path string, analysis *RepositoryAnalysis) {
	// #nosec G304 - path is validated by filepath.Walk and comes from repository scan
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse package==version or package>=version
		var name, version string
		if strings.Contains(line, "==") {
			parts := strings.Split(line, "==")
			name = strings.TrimSpace(parts[0])
			if len(parts) > 1 {
				version = strings.TrimSpace(parts[1])
			}
		} else if strings.Contains(line, ">=") {
			parts := strings.Split(line, ">=")
			name = strings.TrimSpace(parts[0])
			if len(parts) > 1 {
				version = ">=" + strings.TrimSpace(parts[1])
			}
		} else {
			name = line
			version = "*"
		}

		if name != "" {
			analysis.Dependencies[name] = version
		}
	}
}

// parsePyprojectToml extracts Python project information from pyproject.toml
func (a *Analyzer) parsePyprojectToml(path string, analysis *RepositoryAnalysis) {
	// #nosec G304 - path is validated by filepath.Walk and comes from repository scan
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}

	content := string(data)
	lines := strings.Split(content, "\n")

	inDependencies := false
	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Detect dependencies section
		if strings.HasPrefix(line, "[tool.poetry.dependencies]") || strings.HasPrefix(line, "[project.dependencies]") {
			inDependencies = true
			continue
		}

		// End of section
		if strings.HasPrefix(line, "[") {
			inDependencies = false
		}

		// Parse dependencies
		if inDependencies && strings.Contains(line, "=") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				name := strings.TrimSpace(parts[0])
				version := strings.Trim(strings.TrimSpace(parts[1]), "\"'")

				// Extract Python version
				if name == "python" {
					analysis.LanguageVersion = version
				} else {
					analysis.Dependencies[name] = version
				}
			}
		}
	}
}
