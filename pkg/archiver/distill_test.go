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
	out        string
	outs       []string
	err        error
	lastPrompt string
	calls      int
}

func (f *fakeLLM) Distill(ctx context.Context, prompt string) (string, error) {
	f.lastPrompt = prompt
	f.calls++
	if len(f.outs) > 0 {
		out := f.outs[0]
		f.outs = f.outs[1:]
		return out, f.err
	}
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

func TestRenderPromptRecordsTOON(t *testing.T) {
	records := []promptRecord{
		{
			Timestamp: time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC),
			Role:      "user",
			Chat:      "slack/C1",
			Thread:    "1700.1",
			Sender:    `alice "ops"`,
			Text:      "first line\nsecond, line",
		},
		{
			Timestamp: time.Date(2026, 4, 29, 10, 1, 0, 0, time.UTC),
			Role:      "assistant",
			Chat:      "slack/C1",
			Text:      "ok",
		},
	}

	got := renderPromptRecordsTOON(records)
	for _, want := range []string{
		`messages[2]{ts,role,chat,thread,sender,text}:`,
		`"2026-04-29T10:00:00Z","user","slack/C1","1700.1","alice \"ops\"","first line\nsecond, line"`,
		`"2026-04-29T10:01:00Z","assistant","slack/C1","","","ok"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("TOON output missing %q:\n%s", want, got)
		}
	}
}

func TestCollectRaw_NormalizesAndFilters(t *testing.T) {
	dir := t.TempDir()
	rawDir := filepath.Join(dir, "raw", "slack", "C1")
	if err := os.MkdirAll(rawDir, 0o755); err != nil {
		t.Fatal(err)
	}
	oldRec := RawRecord{
		Timestamp: time.Date(2026, 4, 29, 9, 59, 0, 0, time.UTC),
		Role:      "user",
		Platform:  "slack",
		ChatID:    "C1",
		Sender:    Sender{PlatformID: "U1"},
		Text:      "older",
	}
	newRec := RawRecord{
		Timestamp: time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC),
		Role:      "assistant",
		Platform:  "slack",
		ChatID:    "C1",
		ThreadID:  "1700.1",
		Sender:    Sender{DisplayName: "Pico"},
		Text:      "newer",
	}
	var payload []byte
	for _, rec := range []RawRecord{oldRec, newRec} {
		line, err := json.Marshal(rec)
		if err != nil {
			t.Fatal(err)
		}
		payload = append(payload, append(line, '\n')...)
	}
	if err := os.WriteFile(filepath.Join(rawDir, "2026-04-29.jsonl"), payload, 0o644); err != nil {
		t.Fatal(err)
	}

	d := NewDistiller(dir, &fakeLLM{})
	got, err := d.collectRaw(time.Date(2026, 4, 29, 9, 59, 30, 0, time.UTC))
	if err != nil {
		t.Fatalf("collectRaw: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got)=%d want 1", len(got))
	}
	if got[0].Chat != "slack/C1" || got[0].Thread != "1700.1" || got[0].Sender != "Pico" || got[0].Text != "newer" {
		t.Fatalf("unexpected normalized record: %+v", got[0])
	}
}

func TestCollectRaw_DerivesThreadFromChatIDSuffix(t *testing.T) {
	dir := t.TempDir()
	rawDir := filepath.Join(dir, "raw", "slack", "C1")
	if err := os.MkdirAll(rawDir, 0o755); err != nil {
		t.Fatal(err)
	}
	rec := RawRecord{
		Timestamp: time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC),
		Role:      "user",
		Platform:  "slack",
		ChatID:    "C1/1700.1",
		Text:      "threaded",
	}
	line, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rawDir, "2026-04-29.jsonl"), append(line, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	d := NewDistiller(dir, &fakeLLM{})
	got, err := d.collectRaw(time.Time{})
	if err != nil {
		t.Fatalf("collectRaw: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got)=%d want 1", len(got))
	}
	if got[0].Chat != "slack/C1" || got[0].Thread != "1700.1" {
		t.Fatalf("unexpected normalized threaded chat: %+v", got[0])
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
	if !strings.Contains(llm.lastPrompt, "# Raw messages (toon)") {
		t.Fatalf("prompt should contain TOON header:\n%s", llm.lastPrompt)
	}
	if !strings.Contains(llm.lastPrompt, `messages[1]{ts,role,chat,thread,sender,text}:`) {
		t.Fatalf("prompt should contain TOON table:\n%s", llm.lastPrompt)
	}
	if strings.Contains(llm.lastPrompt, string(line)) {
		t.Fatalf("prompt should not embed raw JSONL directly:\n%s", llm.lastPrompt)
	}
}

func TestDistiller_EmptyLLMOutput(t *testing.T) {
	dir := t.TempDir()
	rawDir := filepath.Join(dir, "raw", "slack", "C1")
	if err := os.MkdirAll(rawDir, 0o755); err != nil {
		t.Fatal(err)
	}
	rec := RawRecord{
		Timestamp: time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC),
		Role:      "user",
		Platform:  "slack",
		ChatID:    "C1",
		Text:      "discuss archive",
	}
	line, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rawDir, "2026-04-29.jsonl"), append(line, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	llm := &fakeLLM{out: "```json\n\n```"}
	d := NewDistiller(dir, llm)
	_, err = d.Run(context.Background(), time.Time{})
	if err == nil || !strings.Contains(err.Error(), "empty llm output") {
		t.Fatalf("err = %v, want empty llm output", err)
	}
	if llm.calls != 2 {
		t.Fatalf("llm calls = %d, want 2", llm.calls)
	}
	if !strings.Contains(llm.lastPrompt, "return exactly []") {
		t.Fatalf("retry prompt missing empty-array instruction:\n%s", llm.lastPrompt)
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
