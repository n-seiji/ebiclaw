package providers

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func createMockFailingCodexCLI(t *testing.T, stdout, stderr string, exitCode int) string {
	t.Helper()

	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "codex")

	if err := os.WriteFile(filepath.Join(dir, "stdout.txt"), []byte(stdout), 0o644); err != nil {
		t.Fatal(err)
	}
	if stderr != "" {
		if err := os.WriteFile(filepath.Join(dir, "stderr.txt"), []byte(stderr), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	script := "#!/bin/bash\n" +
		"cat '" + filepath.Join(dir, "stdout.txt") + "'\n"
	if stderr != "" {
		script += "cat '" + filepath.Join(dir, "stderr.txt") + "' >&2\n"
	}
	script += "exit " + string(rune('0'+exitCode)) + "\n"

	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return scriptPath
}

func TestCodexCliProvider_Chat_PropagatesStructuredErrorExitStatusAndStderr(t *testing.T) {
	stdout := strings.Join([]string{
		`{"type":"thread.started","thread_id":"test-err"}`,
		`{"type":"turn.started"}`,
		`{"type":"error","message":"auth token expired"}`,
		`{"type":"turn.failed","error":{"message":"auth token expired"}}`,
	}, "\n") + "\n"

	scriptPath := createMockFailingCodexCLI(t, stdout, "stderr detail", 7)

	p := &CodexCliProvider{
		command:   scriptPath,
		workspace: "",
	}

	_, err := p.Chat(context.Background(), []Message{{Role: "user", Content: "Hello"}}, nil, "", nil)
	if err == nil {
		t.Fatal("expected error")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "auth token expired") {
		t.Fatalf("error should contain structured CLI failure, got: %q", errMsg)
	}
	if !strings.Contains(errMsg, "exit status 7") {
		t.Fatalf("error should contain process exit status, got: %q", errMsg)
	}
	if !strings.Contains(errMsg, "stderr detail") {
		t.Fatalf("error should contain stderr output, got: %q", errMsg)
	}
}
