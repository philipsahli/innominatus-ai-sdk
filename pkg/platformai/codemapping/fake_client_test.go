package codemapping_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/philipsahli/innominatus-ai-sdk/pkg/model"
)

// fakeClient stands in for a model backend so tests need no API key or network.
type fakeClient struct {
	reply model.Reply
	err   error

	requests []model.Request
}

var _ model.Model = (*fakeClient)(nil)

func (f *fakeClient) Call(_ context.Context, req model.Request) (model.Reply, error) {
	f.requests = append(f.requests, req)
	return f.reply, f.err
}

func (f *fakeClient) Name() (string, string) { return "fake", "fake-model" }

func newFakeClient(text string) *fakeClient {
	return &fakeClient{reply: model.Reply{Text: text, Stop: model.StopEnd}}
}

const validConfigJSON = `{
  "service": {"name": "sample-api", "template": "microservice", "runtime": "go1.21", "framework": "gin", "port": 8080},
  "resources": {"cpu": "500m", "memory": "512Mi", "scaling": {"min_replicas": 2, "max_replicas": 10, "target_cpu_percent": 70}},
  "database": {"type": "postgresql", "version": "15", "storage": "10Gi"},
  "cache": null,
  "monitoring": {"metrics": true, "logs": true, "traces": false},
  "security": {"health_check": {"path": "/health", "port": 8080}}
}`

const fixturesDir = "../../../testdata"

// writeRepo creates files (slash-separated relative path -> content) under a fresh temp directory.
func writeRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	writeFiles(t, root, files)
	return root
}

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
}

func assertDeps(t *testing.T, got, want map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("got %d dependencies %v, want %d %v", len(got), got, len(want), want)
	}
	for name, version := range want {
		v, ok := got[name]
		if !ok {
			t.Errorf("dependency %q missing", name)
			continue
		}
		if v != version {
			t.Errorf("dependency %q = %q, want %q", name, v, version)
		}
	}
}

func assertFiles(t *testing.T, got, want []string) {
	t.Helper()
	gotSet := make(map[string]bool, len(got))
	for _, f := range got {
		gotSet[filepath.ToSlash(f)] = true
	}
	if len(got) != len(want) {
		t.Errorf("got files %v, want %v", got, want)
	}
	for _, f := range want {
		if !gotSet[f] {
			t.Errorf("file %q missing from %v", f, got)
		}
	}
}
