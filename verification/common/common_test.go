package common_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/philipsahli/innominatus-ai-sdk/verification/common"
)

func TestSaveJSON(t *testing.T) {
	root := t.TempDir()
	scriptDir := filepath.Join(root, "verification", "feature")
	if err := os.MkdirAll(scriptDir, 0o750); err != nil {
		t.Fatal(err)
	}
	t.Chdir(scriptDir)

	want := map[string]any{"verification": "test", "count": float64(2)}
	path, err := common.SaveJSON("test", want)
	if err != nil {
		t.Fatalf("SaveJSON: %v", err)
	}

	abs := filepath.Join(scriptDir, path)
	if got := filepath.Dir(abs); got != filepath.Join(root, "docs", "verification") {
		t.Errorf("artifact dir = %s, want <root>/docs/verification", got)
	}
	if base := filepath.Base(abs); !strings.HasPrefix(base, "test-") || !strings.HasSuffix(base, ".json") {
		t.Errorf("artifact name = %s, want test-<timestamp>.json", base)
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("artifact is not valid JSON: %v", err)
	}
	if got["verification"] != want["verification"] || got["count"] != want["count"] {
		t.Errorf("artifact = %v, want %v", got, want)
	}

	fileInfo, err := os.Stat(abs)
	if err != nil {
		t.Fatal(err)
	}
	if mode := fileInfo.Mode().Perm(); mode != 0o600 {
		t.Errorf("file mode = %o, want 600", mode)
	}

	dirInfo, err := os.Stat(filepath.Dir(abs))
	if err != nil {
		t.Fatal(err)
	}
	if mode := dirInfo.Mode().Perm(); mode != 0o750 {
		t.Errorf("dir mode = %o, want 750", mode)
	}
}

func ExamplePrintHeader() {
	common.PrintHeader("🔧 RAG Module Verification")
	// Output:
	// 🔧 RAG Module Verification
	// =========================
}

func ExamplePass() {
	common.Pass("Analyzed %d files", 3)
	// Output: ✓ Analyzed 3 files
}

func TestSaveJSONDirError(t *testing.T) {
	root := t.TempDir()
	scriptDir := filepath.Join(root, "verification", "feature")
	if err := os.MkdirAll(scriptDir, 0o750); err != nil {
		t.Fatal(err)
	}
	// A file where the docs directory should be makes MkdirAll fail.
	if err := os.WriteFile(filepath.Join(root, "docs"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(scriptDir)

	if _, err := common.SaveJSON("test", map[string]string{}); err == nil {
		t.Fatal("expected error when docs/verification cannot be created")
	}
}

func TestSaveJSONMarshalError(t *testing.T) {
	t.Chdir(t.TempDir())
	if _, err := common.SaveJSON("bad", make(chan int)); err == nil {
		t.Fatal("expected error for a value JSON cannot encode")
	}
}
