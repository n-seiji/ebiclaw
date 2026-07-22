// Package codexpipe pipes inbound channel messages directly to the Codex CLI
// and returns its final response, bypassing the built-in agent loop.
package codexpipe

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/n-seiji/ebiclaw/pkg/isolation"
)

// Result is the outcome of one Codex CLI turn.
type Result struct {
	Text     string
	ThreadID string
}

// Runner executes codex exec turns.
type Runner struct {
	Command   string
	Model     string
	Workspace string
	Sandbox   string
	// WritableRoots are extra directories writable under the
	// workspace-write sandbox (e.g. ~/.config/gcloud).
	WritableRoots []string
}

// Run executes one Codex turn. When threadID is empty a new thread is
// started; otherwise the existing thread is resumed. sandbox overrides
// r.Sandbox when non-empty (used by the two-stage planner). onMessage, if
// non-nil, is invoked synchronously for each agent_message as it arrives on
// stdout, before the turn completes.
//
// Return contract: once onMessage has fired at least once, Run always
// returns a non-nil *Result alongside any error (so ThreadID can still be
// persisted and callers can report the error as a follow-up, on top of the
// messages already streamed). Before any onMessage fire, an error still
// returns a nil *Result, matching the pre-streaming behavior.
//
// sandbox と承認抑止は -s / --full-auto ではなく -c config override で渡す。
// 理由: `codex exec resume` サブコマンドは -s/--sandbox フラグを受け付けず、
// --full-auto フラグは exec 本体にも存在しない (codex-cli 0.144.4 で確認)。
// -c は exec / resume 両方で使える。
//
// -C (workspace) は `codex exec resume` では受け付けられない
// ("unexpected argument '-C' found", codex-cli 0.144.4 で確認)。resume は
// セッション作成時の cwd をそのまま引き継ぐため、resume 時は付与しない。
func (r *Runner) Run(
	ctx context.Context, threadID, sandbox, prompt string, onMessage func(text string),
) (*Result, error) {
	args := []string{"exec"}
	if threadID != "" {
		args = append(args, "resume", threadID)
	}
	// [OPTIONS] を positional args (SESSION_ID, PROMPT) より前に置く。
	// Usage: codex exec resume [OPTIONS] [SESSION_ID] [PROMPT]
	// --color は付けない: `codex exec resume` が受け付けず
	// ("unexpected argument '--color' found", codex-cli 0.145.0 で確認)、
	// --json 出力では色付けの意味も無い。
	args = append(args, "--json", "--skip-git-repo-check")
	if sandbox == "" {
		sandbox = r.Sandbox
	}
	if sandbox == "" {
		sandbox = "workspace-write"
	}
	args = append(args,
		"-c", fmt.Sprintf("sandbox_mode=%q", sandbox),
		"-c", `approval_policy="never"`,
	)
	if sandbox == "workspace-write" {
		// workspace-write はデフォルトでネットワーク遮断・cwd 外書き込み禁止
		args = append(args, "-c", "sandbox_workspace_write.network_access=true")
		if len(r.WritableRoots) > 0 {
			quoted := make([]string, len(r.WritableRoots))
			for i, root := range r.WritableRoots {
				quoted[i] = strconv.Quote(root)
			}
			args = append(args, "-c",
				"sandbox_workspace_write.writable_roots=["+strings.Join(quoted, ",")+"]")
		}
	}
	if r.Model != "" {
		args = append(args, "-m", r.Model)
	}
	if r.Workspace != "" && threadID == "" {
		args = append(args, "-C", r.Workspace)
	}
	args = append(args, "-") // read prompt from stdin

	cmd := exec.CommandContext(ctx, r.Command, args...)
	cmd.Stdin = bytes.NewReader([]byte(prompt))

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("codex exec: stdout pipe: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := isolation.Start(cmd); err != nil {
		return nil, fmt.Errorf("codex exec: start: %w", err)
	}

	res := &Result{}
	var parts []string
	var lastError string
	fired := false

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var ev event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if ev.ThreadID != "" {
			res.ThreadID = ev.ThreadID
		}
		switch ev.Type {
		case "item.completed":
			if ev.Item != nil && ev.Item.Type == "agent_message" && ev.Item.Text != "" {
				parts = append(parts, ev.Item.Text)
				fired = true
				if onMessage != nil {
					onMessage(ev.Item.Text)
				}
			}
		case "error":
			lastError = ev.Message
		case "turn.failed":
			if ev.Error != nil {
				lastError = ev.Error.Message
			}
		}
	}
	scanErr := scanner.Err()
	waitErr := cmd.Wait()

	res.Text = strings.TrimSpace(strings.Join(parts, "\n"))

	// A cancelled context takes precedence over any partial output.
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if scanErr != nil {
		return nil, fmt.Errorf("parse codex jsonl: %w", scanErr)
	}
	if lastError != "" {
		turnErr := fmt.Errorf("codex: %s", lastError)
		if fired {
			return res, turnErr
		}
		return nil, turnErr
	}
	// codex writes diagnostics to stderr but still emits valid JSONL on
	// stdout, so a non-zero exit is not itself a failure once at least one
	// agent_message has been observed.
	if waitErr != nil && !fired {
		if s := strings.TrimSpace(stderr.String()); s != "" {
			return nil, fmt.Errorf("codex exec: %s", s)
		}
		return nil, fmt.Errorf("codex exec: %w", waitErr)
	}
	return res, nil
}

type event struct {
	Type     string     `json:"type"`
	ThreadID string     `json:"thread_id,omitempty"`
	Message  string     `json:"message,omitempty"`
	Item     *eventItem `json:"item,omitempty"`
	Error    *eventErr  `json:"error,omitempty"`
}

type eventItem struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type eventErr struct {
	Message string `json:"message"`
}

// parseEvents parses a full JSONL transcript at once. Kept for tests that
// exercise the event-parsing rules directly without spawning a process.
func parseEvents(output string) (*Result, error) {
	res := &Result{}
	var parts []string
	var lastError string

	scanner := bufio.NewScanner(strings.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var ev event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if ev.ThreadID != "" {
			res.ThreadID = ev.ThreadID
		}
		switch ev.Type {
		case "item.completed":
			if ev.Item != nil && ev.Item.Type == "agent_message" && ev.Item.Text != "" {
				parts = append(parts, ev.Item.Text)
			}
		case "error":
			lastError = ev.Message
		case "turn.failed":
			if ev.Error != nil {
				lastError = ev.Error.Message
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("parse codex jsonl: %w", err)
	}
	if lastError != "" && len(parts) == 0 {
		return nil, fmt.Errorf("codex: %s", lastError)
	}
	res.Text = strings.TrimSpace(strings.Join(parts, "\n"))
	return res, nil
}
