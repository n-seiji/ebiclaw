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

func TestService_ReloadTogglesObserver(t *testing.T) {
	cfg := Defaults()
	svc := NewService(cfg, &fakeLLM{})
	if svc.Observer() != nil {
		t.Fatal("expected nil Observer when inactive")
	}

	work := t.TempDir()
	cfg.Enabled = true
	cfg.RepositoryPath = work
	cfg.Allowlist = []string{"slack/C1"}
	svc.Reload(cfg)
	if svc.Observer() == nil {
		t.Fatal("expected non-nil Observer after activating Reload")
	}

	cfg.Enabled = false
	svc.Reload(cfg)
	if svc.Observer() != nil {
		t.Fatal("expected nil Observer after deactivating Reload")
	}
}

func TestService_StartStop_InactiveIsSafe(t *testing.T) {
	svc := NewService(Defaults(), &fakeLLM{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc.Start(ctx)
	// Second Start is a no-op (CompareAndSwap fails) — must not panic.
	svc.Start(ctx)

	done := make(chan struct{})
	go func() { svc.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return")
	}

	// Stop on already-stopped service must be a no-op.
	svc.Stop()
}

func TestService_RunOnce_PushFailureIncrementsCounter(t *testing.T) {
	work := newGitWorkRepo(t)
	// Break the remote so push fails.
	if err := os.RemoveAll(filepath.Join(filepath.Dir(work), "remote.git")); err != nil {
		t.Fatal(err)
	}

	cfg := Defaults()
	cfg.Enabled = true
	cfg.RepositoryPath = work
	cfg.Allowlist = []string{"slack/C1"}

	rawDir := filepath.Join(work, "raw", "slack", "C1")
	if err := os.MkdirAll(rawDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rawDir, "2026-04-29.jsonl"),
		[]byte(`{"ts":"2026-04-29T10:00:00Z","role":"user","platform":"slack","chat_id":"C1","text":"hi"}`+"\n"),
		0o644); err != nil {
		t.Fatal(err)
	}

	llm := &fakeLLM{out: `[{"action":"create","slug":"hello","title":"Hello",
        "channels":["slack/C1"],"body":"## TL;DR\n\n.\n",
        "source_refs":[{"file":"raw/slack/C1/2026-04-29.jsonl","lines":"L1"}],
        "confidence":"high","primarily_human":true}]`}
	svc := NewService(cfg, llm)

	if err := svc.RunOnce(context.Background()); err == nil {
		t.Fatal("expected push failure, got nil")
	}
	st, _ := ReadState(work)
	if st.ConsecutivePushFailures != 1 {
		t.Fatalf("ConsecutivePushFailures=%d, want 1", st.ConsecutivePushFailures)
	}
}

func TestDistiller_MergeAction(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "topics"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, slug := range []string{"a", "b"} {
		topic := Topic{
			Title: slug, Slug: slug, Status: "open",
			Sources:  []string{"raw/slack/C1/2026-04-29.jsonl#L" + slug},
			Channels: []string{"slack/C1"},
			Created:  "2026-04-28", Updated: "2026-04-28",
		}
		if err := os.WriteFile(filepath.Join(dir, "topics", slug+".md"), []byte(topic.Render()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := WriteState(dir, State{Version: 1, TopicIndex: []TopicIndexEntry{
		{Slug: "a", Title: "a", Status: "open", Updated: "2026-04-28"},
		{Slug: "b", Title: "b", Status: "open", Updated: "2026-04-28"},
	}}); err != nil {
		t.Fatal(err)
	}

	rawDir := filepath.Join(dir, "raw", "slack", "C1")
	os.MkdirAll(rawDir, 0o755)
	os.WriteFile(filepath.Join(rawDir, "2026-04-29.jsonl"),
		[]byte(`{"ts":"2026-04-29T10:00:00Z","role":"user","platform":"slack","chat_id":"C1","text":"merge them"}`+"\n"),
		0o644)

	llm := &fakeLLM{out: `[{"action":"merge","slugs":["a","b"],"into":"a","reason":"duplicate"}]`}
	d := NewDistiller(dir, llm)
	res, err := d.Run(context.Background(), time.Time{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Merged != 1 {
		t.Fatalf("want Merged=1, got %+v", res)
	}
	if _, err := os.Stat(filepath.Join(dir, "topics", "b.md")); !os.IsNotExist(err) {
		t.Fatalf("b.md should be removed after merge, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "topics", "a.md")); err != nil {
		t.Fatalf("a.md missing after merge: %v", err)
	}
	// Exercise the contains helper directly so it is covered.
	if !contains([]string{"x", "y"}, "y") {
		t.Fatal("contains failed for matching element")
	}
	if contains([]string{"x"}, "z") {
		t.Fatal("contains returned true for non-member")
	}
}
