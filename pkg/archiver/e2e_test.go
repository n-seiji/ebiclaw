package archiver

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestE2E_FullCycle(t *testing.T) {
	root := t.TempDir()
	bare := filepath.Join(root, "remote.git")
	if out, err := exec.Command("git", "init", "--bare", bare).CombinedOutput(); err != nil {
		t.Fatalf("init bare: %v: %s", err, out)
	}
	work := filepath.Join(root, "work")
	if out, err := exec.Command("git", "init", work).CombinedOutput(); err != nil {
		t.Fatalf("init work: %v: %s", err, out)
	}
	for _, args := range [][]string{
		{"-C", work, "checkout", "-b", "main"},
		{"-C", work, "remote", "add", "origin", bare},
		{"-C", work, "config", "user.email", "t@e"},
		{"-C", work, "config", "user.name", "t"},
		{"-C", work, "commit", "--allow-empty", "-m", "init"},
		{"-C", work, "push", "-u", "origin", "main"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}

	cfg := Defaults()
	cfg.Enabled = true
	cfg.RepositoryPath = work
	cfg.Allowlist = []string{"slack/C1"}

	llm := &fakeLLM{out: `[{"action":"create","slug":"hello","title":"Hello",
        "channels":["slack/C1"],"body":"## TL;DR\n\nfirst topic\n",
        "source_refs":[{"file":"raw/slack/C1/2026-04-29.jsonl","lines":"L1"}],
        "confidence":"high","primarily_human":true}]`}

	svc := NewService(cfg, llm)

	rec := RawRecord{
		Timestamp: time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC),
		Role:      "user", Platform: "slack", ChatID: "C1",
		Sender: Sender{Username: "alice"}, Text: "hello",
	}
	line, _ := json.Marshal(rec)
	rawDir := filepath.Join(work, "raw", "slack", "C1")
	if err := os.MkdirAll(rawDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rawDir, "2026-04-29.jsonl"), append(line, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(work, "topics", "hello.md"))
	if err != nil {
		t.Fatalf("topic missing: %v", err)
	}
	if !strings.Contains(string(body), "first topic") {
		t.Fatalf("topic body wrong: %s", body)
	}

	out, err := exec.Command("git", "-C", bare, "log", "--oneline").CombinedOutput()
	if err != nil {
		t.Fatalf("log: %v: %s", err, out)
	}
	if !strings.Contains(string(out), "archive:") {
		t.Fatalf("expected archive commit in remote log: %s", out)
	}
}
