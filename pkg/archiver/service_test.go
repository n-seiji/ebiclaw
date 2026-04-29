package archiver

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func newGitWorkRepo(t *testing.T) string {
	t.Helper()
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
	return work
}

func TestService_RunOnce_NoRaw_NoCommit(t *testing.T) {
	work := newGitWorkRepo(t)
	cfg := Defaults()
	cfg.Enabled = true
	cfg.RepositoryPath = work
	cfg.Allowlist = []string{"slack/C1"}

	llm := &fakeLLM{}
	svc := NewService(cfg, llm)
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(work, "topics")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no topics dir, err=%v", err)
	}
}

func TestService_Mutex(t *testing.T) {
	work := newGitWorkRepo(t)
	cfg := Defaults()
	cfg.Enabled = true
	cfg.RepositoryPath = work
	cfg.Allowlist = []string{"slack/C1"}

	llm := &slowLLM{out: "[]", delay: 200 * time.Millisecond}
	svc := NewService(cfg, llm)

	// seed one raw line so distiller actually invokes LLM
	rawDir := filepath.Join(work, "raw", "slack", "C1")
	if err := os.MkdirAll(rawDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rawDir, "2026-04-29.jsonl"),
		[]byte(`{"ts":"2026-04-29T10:00:00Z","role":"user","platform":"slack","chat_id":"C1","text":"hi"}`+"\n"),
		0o644); err != nil {
		t.Fatal(err)
	}

	var firstErr, secondErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); firstErr = svc.RunOnce(context.Background()) }()
	time.Sleep(20 * time.Millisecond)
	go func() { defer wg.Done(); secondErr = svc.RunOnce(context.Background()) }()
	wg.Wait()
	if firstErr != nil {
		t.Fatalf("first should succeed, got %v", firstErr)
	}
	if !errors.Is(secondErr, ErrBusy) {
		t.Fatalf("second should be ErrBusy, got %v", secondErr)
	}
}

type slowLLM struct {
	out   string
	delay time.Duration
}

func (s *slowLLM) Distill(ctx context.Context, _ string) (string, error) {
	select {
	case <-time.After(s.delay):
		return s.out, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}
