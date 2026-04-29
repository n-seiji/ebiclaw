package archiver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeLLM struct {
	out string
	err error
}

func (f *fakeLLM) Distill(ctx context.Context, prompt string) (string, error) {
	return f.out, f.err
}

func TestStripCodeFence(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"plain content", "plain content"},
		{"```json\n[]\n```", "[]"},
		{"  ```\nfoo\n```  ", "foo"},
		{"```json\n[1]```", "[1]"},
	}
	for _, c := range cases {
		if got := stripCodeFence(c.in); got != c.want {
			t.Errorf("stripCodeFence(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestRenderBody_AllSections(t *testing.T) {
	body := renderBody(patchSpec{
		TLDR:      "summary",
		Timeline:  []string{"a", "b"},
		Decisions: []string{"do x"},
		Open:      []string{"q1"},
	}, "previous")
	for _, want := range []string{"TL;DR", "summary", "経緯", "- a", "決定事項", "do x", "未解決事項", "q1"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q: %s", want, body)
		}
	}
	if got := renderBody(patchSpec{}, "previous"); got != "previous" {
		t.Errorf("empty patch should preserve prev body, got %q", got)
	}
}

func TestPickPlatform(t *testing.T) {
	if got := pickPlatform("slack", "fallback"); got != "slack" {
		t.Errorf("got %q", got)
	}
	if got := pickPlatform("", "fallback"); got != "fallback" {
		t.Errorf("got %q", got)
	}
}

func TestDistiller_CreatesNewTopic(t *testing.T) {
	dir := t.TempDir()
	rawDir := filepath.Join(dir, "raw", "slack", "C1")
	if err := os.MkdirAll(rawDir, 0o755); err != nil {
		t.Fatal(err)
	}
	rec := RawRecord{
		Timestamp: time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC),
		Role:      "user", Platform: "slack", ChatID: "C1",
		Sender: Sender{Username: "alice"}, Text: "discuss new onboarding",
	}
	line, _ := json.Marshal(rec)
	if err := os.WriteFile(filepath.Join(rawDir, "2026-04-29.jsonl"), append(line, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	llm := &fakeLLM{out: `[
        {"action":"create","slug":"new-onboarding-flow","title":"新オンボーディング設計",
         "channels":["slack/C1"],"body":"## TL;DR\n\nshort.\n",
         "source_refs":[{"file":"raw/slack/C1/2026-04-29.jsonl","lines":"L1"}],
         "confidence":"medium","primarily_human":true}
    ]`}

	d := NewDistiller(dir, llm)
	res, err := d.Run(context.Background(), time.Time{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Created != 1 {
		t.Fatalf("expected 1 created, got %+v", res)
	}
	got, err := os.ReadFile(filepath.Join(dir, "topics", "new-onboarding-flow.md"))
	if err != nil {
		t.Fatalf("topic file: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("topic file empty")
	}
}

func TestDistiller_UpdatesExistingTopic(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "topics"), 0o755); err != nil {
		t.Fatal(err)
	}
	existing := Topic{Title: "Old", Slug: "x", Status: "open", Created: "2026-04-28", Updated: "2026-04-28"}
	if err := os.WriteFile(filepath.Join(dir, "topics", "x.md"), []byte(existing.Render()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteState(dir, State{Version: 1, TopicIndex: []TopicIndexEntry{{Slug: "x", Title: "Old", Status: "open", Updated: "2026-04-28"}}}); err != nil {
		t.Fatal(err)
	}
	rawDir := filepath.Join(dir, "raw", "slack", "C1")
	os.MkdirAll(rawDir, 0o755)
	rec := RawRecord{Timestamp: time.Now(), Role: "user", Platform: "slack", ChatID: "C1", Text: "more details"}
	line, _ := json.Marshal(rec)
	os.WriteFile(filepath.Join(rawDir, "2026-04-29.jsonl"), append(line, '\n'), 0o644)

	llm := &fakeLLM{out: `[{"action":"update","slug":"x",
        "patch":{"tldr":"updated tldr","timeline":["new entry"]},
        "source_refs":[{"file":"raw/slack/C1/2026-04-29.jsonl","lines":"L1"}],
        "confidence":"high","primarily_human":true}]`}

	d := NewDistiller(dir, llm)
	res, err := d.Run(context.Background(), time.Time{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Updated != 1 {
		t.Fatalf("got %+v", res)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "topics", "x.md"))
	if !strings.Contains(string(data), "updated tldr") {
		t.Fatalf("body not updated: %s", data)
	}
}
