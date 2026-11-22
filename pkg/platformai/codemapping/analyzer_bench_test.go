package codemapping

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Internal package so the recommendation step (unexported) can be measured.
// That also rules out reusing writeFiles, which lives in codemapping_test.

const benchMinFiles = 10_000

var benchExtensions = []string{".go", ".js", ".ts", ".py", ".md", ".json", ".yaml", ".txt"}

// buildBenchRepo writes 10,000 counted files in 10x10 nested directories,
// three manifests, and files under skipped directories that must not be counted.
func buildBenchRepo(b *testing.B) string {
	b.Helper()
	root := b.TempDir()

	write := func(rel, content string) {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			b.Fatal(err)
		}
	}

	write("go.mod", "module example.com/bench\n\ngo 1.24\n\nrequire (\n\tgithub.com/lib/pq v1.10.9\n\tgolang.org/x/net v0.10.0\n)\n")
	write("package.json", `{"dependencies":{"lodash":"^4.17.21"},"engines":{"node":">=18"}}`)
	write("Dockerfile", "FROM golang:1.24-alpine\nCOPY . .\nRUN go build ./...\n")

	for top := 0; top < 10; top++ {
		for sub := 0; sub < 10; sub++ {
			for f := 0; f < 100; f++ {
				ext := benchExtensions[f%len(benchExtensions)]
				write(fmt.Sprintf("src%02d/mod%02d/file%03d%s", top, sub, f, ext), "x")
			}
		}
	}

	for _, skipped := range []string{"node_modules", ".git", "vendor"} {
		for f := 0; f < 100; f++ {
			write(fmt.Sprintf("%s/pkg%02d/file%03d.js", skipped, f%10, f), "x")
		}
	}

	return root
}

func analyzeBenchRepo(b *testing.B, root string) *RepositoryAnalysis {
	b.Helper()
	analysis, err := NewAnalyzer().Analyze(context.Background(), root)
	if err != nil {
		b.Fatal(err)
	}
	// 10,000 generated files plus go.mod, package.json and Dockerfile.
	if got, want := len(analysis.Files), benchMinFiles+3; got != want {
		b.Fatalf("len(Files) = %d, want %d", got, want)
	}
	return analysis
}

// b.Loop runs the function body once per -count and times only the loop,
// so the tree is generated once and excluded from the measurement.

func BenchmarkAnalyzerAnalyze(b *testing.B) {
	root := buildBenchRepo(b)
	analyzeBenchRepo(b, root)
	analyzer := NewAnalyzer()
	ctx := context.Background()
	b.ReportAllocs()

	for b.Loop() {
		if _, err := analyzer.Analyze(ctx, root); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDetectAndRecommend(b *testing.B) {
	analysis := analyzeBenchRepo(b, buildBenchRepo(b))
	detector := NewDetector()
	module := &Module{}
	b.ReportAllocs()

	for b.Loop() {
		analysis.PrimaryLanguage = detector.DetectLanguage(analysis)
		analysis.DetectedFramework = detector.DetectFramework(analysis)
		_ = module.generateRecommendations(analysis, &PlatformConfig{})
	}
}
