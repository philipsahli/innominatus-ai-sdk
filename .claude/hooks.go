package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Hook represents a Claude Code hook event
type Hook struct {
	Event     string   `json:"event"`
	FilePaths []string `json:"filePaths"`
	Command   string   `json:"command"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: hooks <event-json>\n")
		os.Exit(1)
	}

	var hook Hook
	if err := json.Unmarshal([]byte(os.Args[1]), &hook); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing hook event: %v\n", err)
		os.Exit(1)
	}

	switch hook.Event {
	case "onFileSave":
		handleFileSave(hook.FilePaths)
	case "beforeEdit":
		handleBeforeEdit(hook.FilePaths)
	case "afterEdit":
		handleAfterEdit(hook.FilePaths)
	default:
		// Unknown event, ignore
	}
}

func handleFileSave(filePaths []string) {
	for _, path := range filePaths {
		if !strings.HasSuffix(path, ".go") {
			continue
		}

		// Auto-format with gofmt
		if err := runCommand("gofmt", "-w", path); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: gofmt failed for %s: %v\n", path, err)
		} else {
			fmt.Printf("✓ Formatted: %s\n", path)
		}

		// Run go vet for static analysis
		if err := runCommand("go", "vet", path); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: go vet found issues in %s: %v\n", path, err)
		}
	}
}

func handleBeforeEdit(filePaths []string) {
	// Run linter on files about to be edited
	goFiles := filterGoFiles(filePaths)
	if len(goFiles) == 0 {
		return
	}

	// Run golangci-lint on specific files
	args := append([]string{"run"}, goFiles...)
	if err := runCommand("golangci-lint", args...); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: golangci-lint found issues: %v\n", err)
	}
}

func handleAfterEdit(filePaths []string) {
	goFiles := filterGoFiles(filePaths)
	if len(goFiles) == 0 {
		return
	}

	// Run related tests
	for _, file := range goFiles {
		if strings.HasSuffix(file, "_test.go") {
			continue // Skip test files themselves
		}

		// Find corresponding test file
		testFile := strings.TrimSuffix(file, ".go") + "_test.go"
		if fileExists(testFile) {
			dir := filepath.Dir(file)
			fmt.Printf("Running tests for %s...\n", file)
			if err := runCommand("go", "test", "-v", dir); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: tests failed for %s: %v\n", file, err)
			} else {
				fmt.Printf("✓ Tests passed for %s\n", file)
			}
		}
	}

	// Check test coverage for pkg/ directory changes
	if shouldCheckCoverage(filePaths) {
		checkCoverage()
	}

	// Check if go.mod needs updating
	if containsFile(filePaths, "go.mod") || containsFile(filePaths, "go.sum") {
		fmt.Println("go.mod changed, running go mod tidy...")
		if err := runCommand("go", "mod", "tidy"); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: go mod tidy failed: %v\n", err)
		} else {
			fmt.Println("✓ go.mod tidied")
		}
	}
}

func filterGoFiles(filePaths []string) []string {
	var goFiles []string
	for _, path := range filePaths {
		if strings.HasSuffix(path, ".go") {
			goFiles = append(goFiles, path)
		}
	}
	return goFiles
}

func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func containsFile(files []string, target string) bool {
	for _, f := range files {
		if strings.HasSuffix(f, target) {
			return true
		}
	}
	return false
}

func shouldCheckCoverage(filePaths []string) bool {
	// Check coverage if any file in pkg/ was modified
	for _, path := range filePaths {
		if strings.Contains(path, "/pkg/") && strings.HasSuffix(path, ".go") {
			return true
		}
	}
	return false
}

func checkCoverage() {
	fmt.Println("\nChecking test coverage (requires >80%)...")

	// Run go test with coverage
	cmd := exec.Command("go", "test", "./pkg/...", "-cover")
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  Coverage check failed to run: %v\n", err)
		fmt.Fprintf(os.Stderr, "Output: %s\n", string(output))
		return
	}

	// Parse coverage output
	outputStr := string(output)
	lines := strings.Split(outputStr, "\n")

	belowThreshold := []string{}
	hasTests := false

	for _, line := range lines {
		if strings.Contains(line, "coverage:") {
			hasTests = true
			parts := strings.Fields(line)
			for i, part := range parts {
				if part == "coverage:" && i+1 < len(parts) {
					coverageStr := strings.TrimSuffix(parts[i+1], "%")
					// Extract package name
					pkgName := strings.Fields(line)[1]

					// Simple check: look for coverage percentage
					if strings.Contains(coverageStr, ".") {
						// Has coverage value, check if below 80%
						if !strings.HasPrefix(coverageStr, "8") &&
							!strings.HasPrefix(coverageStr, "9") &&
							!strings.HasPrefix(coverageStr, "10") {
							// Quick check: if it starts with 0-7, it's below 80%
							if len(coverageStr) > 0 && coverageStr[0] >= '0' && coverageStr[0] <= '7' {
								belowThreshold = append(belowThreshold,
									fmt.Sprintf("%s (%s%%)", pkgName, coverageStr))
							}
						}
					} else if coverageStr == "0.0" {
						belowThreshold = append(belowThreshold,
							fmt.Sprintf("%s (0.0%%)", pkgName))
					}
					break
				}
			}
		}
	}

	if !hasTests {
		fmt.Fprintf(os.Stderr, "⚠️  No tests found in pkg/ directory\n")
		fmt.Fprintf(os.Stderr, "   Action: Add test files to achieve >80%% coverage\n")
		return
	}

	if len(belowThreshold) > 0 {
		fmt.Fprintf(os.Stderr, "\n⚠️  Coverage below 80%% threshold:\n")
		for _, pkg := range belowThreshold {
			fmt.Fprintf(os.Stderr, "   • %s\n", pkg)
		}
		fmt.Fprintf(os.Stderr, "\n   Action: Add tests to these packages\n")
		fmt.Fprintf(os.Stderr, "   Target: Each package should have >80%% coverage\n")
		fmt.Fprintf(os.Stderr, "   Run: go test -cover ./pkg/...\n\n")
	} else {
		fmt.Println("✓ All packages have >80% test coverage")
	}
}
