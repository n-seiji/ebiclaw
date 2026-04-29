package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArchiveRead_Tool(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "topics"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "topics", "x.md"), []byte("hello body"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewArchiveReadTool(dir, true)
	res := tool.Execute(context.Background(), map[string]any{"slug": "x"})
	if res == nil {
		t.Fatalf("execute returned nil result")
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "hello body") {
		t.Fatalf("body missing: %s", res.ForLLM)
	}
}

func TestArchiveRead_NotFound(t *testing.T) {
	tool := NewArchiveReadTool(t.TempDir(), true)
	res := tool.Execute(context.Background(), map[string]any{"slug": "missing"})
	if res == nil {
		t.Fatalf("execute returned nil result")
	}
	if !res.IsError {
		t.Fatalf("expected IsError=true for missing topic, got %+v", res)
	}
}

func TestArchiveRead_RejectsPathTraversal(t *testing.T) {
	tool := NewArchiveReadTool(t.TempDir(), true)
	for _, slug := range []string{"", "../etc/passwd", "a/b", "a\\b"} {
		res := tool.Execute(context.Background(), map[string]any{"slug": slug})
		if res == nil {
			t.Fatalf("execute returned nil result for slug=%q", slug)
		}
		if !res.IsError {
			t.Fatalf("expected IsError=true for slug=%q, got %+v", slug, res)
		}
	}
}

func TestArchiveRead_DisabledNoOp(t *testing.T) {
	tool := NewArchiveReadTool(t.TempDir(), false)
	if tool.Enabled() {
		t.Fatal("should be disabled")
	}
	res := tool.Execute(context.Background(), map[string]any{"slug": "x"})
	if res == nil {
		t.Fatalf("execute returned nil result")
	}
	if res.IsError {
		t.Fatalf("disabled tool should not return error result, got %+v", res)
	}
}
