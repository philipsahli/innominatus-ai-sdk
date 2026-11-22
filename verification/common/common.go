// Package common holds the output and artifact helpers shared by the
// verification scripts under verification/<feature>/.
package common

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// artifactDir is relative because the scripts are run from their own
// directory (verification/<feature>/), two levels below the repo root.
const artifactDir = "../../docs/verification"

// Fail prints a failure message to stderr and exits with status 1.
func Fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "❌ FAIL: "+format+"\n", args...)
	os.Exit(1)
}

// Pass prints a passed check to stdout.
func Pass(format string, args ...any) {
	fmt.Printf("✓ "+format+"\n", args...)
}

// PrintHeader prints title and an underline with one '=' per rune.
func PrintHeader(title string) {
	fmt.Println(title)
	fmt.Println(strings.Repeat("=", utf8.RuneCountInString(title)))
}

// SaveJSON writes v as indented JSON to
// docs/verification/<name>-<YYYYMMDD-HHMMSS>.json and returns the path.
// Artifacts hold LLM output and request data, so only the owner can read them.
func SaveJSON(name string, v any) (string, error) {
	if err := os.MkdirAll(artifactDir, 0o750); err != nil {
		return "", fmt.Errorf("failed to create %s: %w", artifactDir, err)
	}

	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal artifact: %w", err)
	}

	path := filepath.Join(artifactDir, fmt.Sprintf("%s-%s.json", name, time.Now().Format("20060102-150405")))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("failed to write %s: %w", path, err)
	}
	return path, nil
}
