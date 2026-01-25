package gen

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeTemplateMatchesRuntime(t *testing.T) {
	path := filepath.Join("..", "ddb", "runtime.go")
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read runtime.go: %v", err)
	}
	if string(want) != runtimeTemplate {
		t.Fatalf("runtime template is out of sync with internal/ddb/runtime.go")
	}
}
