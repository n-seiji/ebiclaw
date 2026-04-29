package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArchiveSearch_Tool(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "topics"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\ntitle: Login Bug\nslug: login-bug\nstatus: open\n---\n\n## TL;DR\n\nbug in login\n"
	if err := os.WriteFile(filepath.Join(dir, "topics", "login-bug.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewArchiveSearchTool(dir, true)
	res := tool.Execute(context.Background(), map[string]any{"query": "login"})
	if res == nil {
		t.Fatalf("execute returned nil result")
	}
	if res.IsError {
		t.Fatalf("execute returned error result: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "login-bug") {
		t.Fatalf("missing slug in output: %s", res.ForLLM)
	}
}

func TestArchiveSearch_DisabledNoOp(t *testing.T) {
	tool := NewArchiveSearchTool(t.TempDir(), false)
	if tool.Enabled() {
		t.Fatal("should be disabled")
	}
}
