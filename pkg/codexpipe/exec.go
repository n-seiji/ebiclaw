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
}

// Run executes one Codex turn. When threadID is empty a new thread is
// started; otherwise the existing thread is resumed. sandbox overrides
// r.Sandbox when non-empty (used by the two-stage planner).
//
// sandbox と承認抑止は -s / --full-auto ではなく -c config override で渡す。
// 理由: `codex exec resume` サブコマンドは -s/--sandbox フラグを受け付けず、
// --full-auto フラグは exec 本体にも存在しない (codex-cli 0.144.4 で確認)。
// -c は exec / resume 両方で使える。
//
// -C (workspace) は `codex exec resume` では受け付けられない
// ("unexpected argument '-C' found", codex-cli 0.144.4 で確認)。resume は
// セッション作成時の cwd をそのまま引き継ぐため、resume 時は付与しない。
func (r *Runner) Run(ctx context.Context, threadID, sandbox, prompt string) (*Result, error) {
	args := []string{"exec"}
	if threadID != "" {
		args = append(args, "resume", threadID)
	}
	// [OPTIONS] を positional args (SESSION_ID, PROMPT) より前に置く。
	// Usage: codex exec resume [OPTIONS] [SESSION_ID] [PROMPT]
	args = append(args, "--json", "--skip-git-repo-check", "--color", "never")
	if sandbox == "" {
		sandbox = r.Sandbox
	}
	args = append(args,
		"-c", fmt.Sprintf("sandbox_mode=%q", sandbox),
		"-c", `approval_policy="never"`,
	)
	if r.Model != "" {
		args = append(args, "-m", r.Model)
	}
	if r.Workspace != "" && threadID == "" {
		args = append(args, "-C", r.Workspace)
	}
	args = append(args, "-") // read prompt from stdin

	cmd := exec.CommandContext(ctx, r.Command, args...)
	cmd.Stdin = bytes.NewReader([]byte(prompt))

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := isolation.Run(cmd)

	// codex writes diagnostics to stderr but still emits valid JSONL on
	// stdout, so prefer parsed output even on non-zero exit.
	if out := stdout.String(); out != "" {
		if res, err := parseEvents(out); err == nil && res.Text != "" {
			return res, nil
		}
	}
	if runErr != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if s := strings.TrimSpace(stderr.String()); s != "" {
			return nil, fmt.Errorf("codex exec: %s", s)
		}
		return nil, fmt.Errorf("codex exec: %w", runErr)
	}
	return parseEvents(stdout.String())
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
