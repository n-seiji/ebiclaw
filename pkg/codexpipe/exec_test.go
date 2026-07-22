package codexpipe

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseEvents(t *testing.T) {
	tests := []struct {
		name       string
		output     string
		wantText   string
		wantThread string
		wantErr    string
	}{
		{
			name: "agent message and thread id",
			output: strings.Join([]string{
				`{"type":"thread.started","thread_id":"0196-abc"}`,
				`{"type":"item.completed","item":{"id":"i1","type":"agent_message","text":"hello"}}`,
				`{"type":"turn.completed","usage":{"input_tokens":10,"output_tokens":5}}`,
			}, "\n"),
			wantText:   "hello",
			wantThread: "0196-abc",
		},
		{
			name: "multiple agent messages joined",
			output: strings.Join([]string{
				`{"type":"item.completed","item":{"id":"i1","type":"agent_message","text":"one"}}`,
				`{"type":"item.completed","item":{"id":"i2","type":"agent_message","text":"two"}}`,
			}, "\n"),
			wantText: "one\ntwo",
		},
		{
			name:    "error event with no content",
			output:  `{"type":"error","message":"boom"}`,
			wantErr: "boom",
		},
		{
			name: "malformed lines are skipped",
			output: strings.Join([]string{
				`not json`,
				`{"type":"item.completed","item":{"id":"i1","type":"agent_message","text":"ok"}}`,
			}, "\n"),
			wantText: "ok",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseEvents(tt.output)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("parseEvents() err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseEvents() err = %v, want nil", err)
			}
			if got.Text != tt.wantText {
				t.Errorf("Text = %q, want %q", got.Text, tt.wantText)
			}
			if tt.wantThread != "" && got.ThreadID != tt.wantThread {
				t.Errorf("ThreadID = %q, want %q", got.ThreadID, tt.wantThread)
			}
		})
	}
}

// recordedInvocation captures what the stub codex script observed for one
// invocation: the args it was called with and the stdin it received.
type recordedInvocation struct {
	Args  []string `json:"args"`
	Stdin string   `json:"stdin"`
}

// newStubCodex writes a shell script that records its argv + stdin to
// recordPath as JSON, then emits stdout/stderr and exits with exitCode.
func newStubCodex(t *testing.T, stdout, stderr string, exitCode int) (scriptPath, recordPath string) {
	t.Helper()
	dir := t.TempDir()
	scriptPath = filepath.Join(dir, "codex-stub.sh")
	recordPath = filepath.Join(dir, "record.json")
	stdoutPath := filepath.Join(dir, "stdout.txt")
	stderrPath := filepath.Join(dir, "stderr.txt")

	if err := os.WriteFile(stdoutPath, []byte(stdout), 0o644); err != nil {
		t.Fatalf("write stdout fixture: %v", err)
	}
	if err := os.WriteFile(stderrPath, []byte(stderr), 0o644); err != nil {
		t.Fatalf("write stderr fixture: %v", err)
	}

	script := fmt.Sprintf(`#!/bin/sh
stdin_content=$(cat)
printf '{"args":[' > %q
first=1
for a in "$@"; do
  if [ "$first" -eq 0 ]; then printf ',' >> %q; fi
  first=0
  esc=$(printf '%%s' "$a" | sed 's/\\/\\\\/g; s/"/\\"/g')
  printf '"%%s"' "$esc" >> %q
done
printf '],"stdin":"' >> %q
printf '%%s' "$stdin_content" | sed 's/\\/\\\\/g; s/"/\\"/g' | tr -d '\n' >> %q
printf '"}' >> %q
cat %q
cat %q >&2
exit %d
`, recordPath, recordPath, recordPath, recordPath, recordPath, recordPath, stdoutPath, stderrPath, exitCode)

	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub script: %v", err)
	}
	return scriptPath, recordPath
}

func readInvocation(t *testing.T, recordPath string) recordedInvocation {
	t.Helper()
	data, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("read record file: %v", err)
	}
	var inv recordedInvocation
	if err := json.Unmarshal(data, &inv); err != nil {
		t.Fatalf("unmarshal record %q: %v", string(data), err)
	}
	return inv
}

func indexOf(args []string, want string) int {
	for i, a := range args {
		if a == want {
			return i
		}
	}
	return -1
}

func TestRun_NewThreadArgOrder(t *testing.T) {
	stdout := `{"type":"item.completed","item":{"type":"agent_message","text":"hi"}}` + "\n"
	script, recordPath := newStubCodex(t, stdout, "", 0)

	r := &Runner{Command: script, Workspace: "/work/dir", Sandbox: "read-only"}
	res, err := r.Run(context.Background(), "", "", "hello there")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if res.Text != "hi" {
		t.Fatalf("Text = %q, want %q", res.Text, "hi")
	}

	inv := readInvocation(t, recordPath)
	args := inv.Args

	if len(args) == 0 || args[0] != "exec" {
		t.Fatalf("args[0] = %v, want %q; full args: %v", args, "exec", args)
	}
	if idx := indexOf(args, "resume"); idx != -1 {
		t.Fatalf("new-thread invocation must not include resume: %v", args)
	}

	cIdx := indexOf(args, "-C")
	if cIdx == -1 || cIdx+1 >= len(args) || args[cIdx+1] != "/work/dir" {
		t.Fatalf("expected -C /work/dir in args, got: %v", args)
	}

	sandboxIdx := -1
	for i, a := range args {
		if a == "-c" && i+1 < len(args) && strings.HasPrefix(args[i+1], "sandbox_mode=") {
			sandboxIdx = i
			break
		}
	}
	if sandboxIdx == -1 {
		t.Fatalf("expected -c sandbox_mode=... in args, got: %v", args)
	}
	if want := `sandbox_mode="read-only"`; args[sandboxIdx+1] != want {
		t.Errorf("sandbox_mode arg = %q, want %q", args[sandboxIdx+1], want)
	}

	// options must precede the trailing "-" (stdin marker).
	dashIdx := indexOf(args, "-")
	if dashIdx == -1 {
		t.Fatalf("expected trailing - marker for stdin, got: %v", args)
	}
	if cIdx > dashIdx || sandboxIdx > dashIdx {
		t.Fatalf("options must precede positional/stdin marker: %v", args)
	}

	if inv.Stdin != "hello there" {
		t.Errorf("stdin = %q, want %q", inv.Stdin, "hello there")
	}
}

func TestRun_ResumeArgOrder(t *testing.T) {
	stdout := `{"type":"item.completed","item":{"type":"agent_message","text":"resumed"}}` + "\n"
	script, recordPath := newStubCodex(t, stdout, "", 0)

	r := &Runner{Command: script, Workspace: "/work/dir", Sandbox: "workspace-write"}
	res, err := r.Run(context.Background(), "thread-123", "", "continue")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if res.Text != "resumed" {
		t.Fatalf("Text = %q, want %q", res.Text, "resumed")
	}

	inv := readInvocation(t, recordPath)
	args := inv.Args

	if len(args) < 2 || args[0] != "exec" || args[1] != "resume" {
		t.Fatalf("expected [exec resume ...], got: %v", args)
	}

	if idx := indexOf(args, "-C"); idx != -1 {
		t.Fatalf("resume must not include -C, got: %v", args)
	}

	threadIdx := indexOf(args, "thread-123")
	if threadIdx == -1 {
		t.Fatalf("expected threadID positional in args, got: %v", args)
	}

	dashIdx := indexOf(args, "-")
	if dashIdx == -1 {
		t.Fatalf("expected trailing - marker for stdin, got: %v", args)
	}

	// The threadID is a positional argument that must appear before any
	// option flags (options precede positional SESSION_ID/PROMPT per usage,
	// but resume <threadID> itself comes right after "resume").
	if threadIdx != 2 {
		t.Fatalf("expected threadID immediately after resume (index 2), got index %d in %v", threadIdx, args)
	}
}

func TestRun_NonZeroExitWithValidStdout_IsSuccess(t *testing.T) {
	stdout := `{"type":"item.completed","item":{"type":"agent_message","text":"still worked"}}` + "\n"
	script, _ := newStubCodex(t, stdout, "some diagnostic noise", 1)

	r := &Runner{Command: script, Sandbox: "workspace-write"}
	res, err := r.Run(context.Background(), "", "", "prompt")
	if err != nil {
		t.Fatalf("Run() error = %v, want nil (non-zero exit but valid stdout)", err)
	}
	if res.Text != "still worked" {
		t.Fatalf("Text = %q, want %q", res.Text, "still worked")
	}
}

func TestRun_NonZeroExitWithEmptyStdout_ReturnsStderr(t *testing.T) {
	script, _ := newStubCodex(t, "", "boom: something broke", 1)

	r := &Runner{Command: script, Sandbox: "workspace-write"}
	_, err := r.Run(context.Background(), "", "", "prompt")
	if err == nil {
		t.Fatal("Run() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "boom: something broke") {
		t.Errorf("error = %v, want containing stderr text", err)
	}
}

func TestRun_DefaultSandboxWhenEmpty(t *testing.T) {
	stdout := `{"type":"item.completed","item":{"type":"agent_message","text":"ok"}}` + "\n"
	script, recordPath := newStubCodex(t, stdout, "", 0)

	r := &Runner{Command: script}
	if _, err := r.Run(context.Background(), "", "", "prompt"); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	inv := readInvocation(t, recordPath)
	args := inv.Args
	found := false
	for i, a := range args {
		if a == "-c" && i+1 < len(args) && args[i+1] == `sandbox_mode="workspace-write"` {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected default sandbox_mode=\"workspace-write\" when both sandbox and r.Sandbox are empty, got: %v", args)
	}
	netFound := false
	for i, a := range args {
		if a == "-c" && i+1 < len(args) && args[i+1] == "sandbox_workspace_write.network_access=true" {
			netFound = true
			break
		}
	}
	if !netFound {
		t.Errorf("expected network_access override for workspace-write sandbox, got: %v", args)
	}
}

func TestRun_CancelledContextTakesPrecedenceOverPartialOutput(t *testing.T) {
	// The stub sleeps briefly before flushing output so the context can be
	// cancelled while it is still running; exec.CommandContext then kills
	// the process, producing a non-zero exit alongside valid stdout.
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "slow-codex.sh")
	script := `#!/bin/sh
cat >/dev/null
sleep 0.2
printf '{"type":"item.completed","item":{"type":"agent_message","text":"too late"}}\n'
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub script: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	r := &Runner{Command: scriptPath, Sandbox: "workspace-write"}
	_, err := r.Run(ctx, "", "", "prompt")
	if err == nil {
		t.Fatal("Run() error = nil, want context deadline exceeded")
	}
	if !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
		t.Errorf("error = %v, want containing %q", err, context.DeadlineExceeded.Error())
	}
}
