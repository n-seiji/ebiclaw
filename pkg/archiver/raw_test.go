package archiver

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRawWriter_AppendsAllowed(t *testing.T) {
	dir := t.TempDir()
	w := NewRawWriter(dir, []string{"slack/C1"})

	rec := RawRecord{
		Timestamp: time.Date(2026, 4, 29, 10, 15, 32, 0, time.UTC),
		Role:      "user",
		Platform:  "slack",
		ChatID:    "C1",
		Sender:    Sender{Username: "alice"},
		Text:      "hello",
	}
	if err := w.Append(rec); err != nil {
		t.Fatalf("append: %v", err)
	}

	path := filepath.Join(dir, "raw", "slack", "C1", "2026-04-29.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	if !scanner.Scan() {
		t.Fatal("no line written")
	}
	var got RawRecord
	if err := json.Unmarshal(scanner.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Text != "hello" || got.Role != "user" {
		t.Fatalf("unexpected record: %+v", got)
	}
}

func TestRawWriter_RejectsNonAllowlisted(t *testing.T) {
	dir := t.TempDir()
	w := NewRawWriter(dir, []string{"slack/C1"})
	if err := w.Append(RawRecord{Platform: "slack", ChatID: "OTHER", Text: "x", Timestamp: time.Now()}); err != nil {
		t.Fatalf("append should silently skip, got err: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "raw"))
	if len(entries) != 0 {
		t.Fatalf("expected no files written, got %d", len(entries))
	}
}

func TestRawWriter_AllowsThreadedChatIDWhenChannelIsAllowlisted(t *testing.T) {
	dir := t.TempDir()
	w := NewRawWriter(dir, []string{"slack/C1"})
	if err := w.Append(RawRecord{
		Platform:  "slack",
		ChatID:    "C1/1700000000.000100",
		ThreadID:  "1700000000.000100",
		Text:      "threaded",
		Timestamp: time.Now(),
	}); err != nil {
		t.Fatalf("append: %v", err)
	}

	files, err := filepath.Glob(filepath.Join(dir, "raw", "slack", "C1", "1700000000.000100", "*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 threaded file, got %v", files)
	}
}

func TestRawWriter_CleanupBeforeRewritesFileKeepingNewerRecords(t *testing.T) {
	dir := t.TempDir()
	w := NewRawWriter(dir, []string{"slack/C1"})

	// Three records on the same day: two old (<= cutoff), one new (> cutoff).
	day := time.Date(2026, 4, 29, 0, 0, 0, 0, time.UTC)
	old1 := RawRecord{Timestamp: day.Add(1 * time.Hour), Platform: "slack", ChatID: "C1", Text: "old-1"}
	old2 := RawRecord{Timestamp: day.Add(2 * time.Hour), Platform: "slack", ChatID: "C1", Text: "old-2"}
	newer := RawRecord{Timestamp: day.Add(5 * time.Hour), Platform: "slack", ChatID: "C1", Text: "newer"}
	for _, r := range []RawRecord{old1, old2, newer} {
		if err := w.Append(r); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	cutoff := day.Add(3 * time.Hour)
	deleted, err := w.CleanupBefore(cutoff)
	if err != nil {
		t.Fatalf("CleanupBefore: %v", err)
	}
	if deleted != 2 {
		t.Errorf("deleted = %d, want 2", deleted)
	}

	path := filepath.Join(dir, "raw", "slack", "C1", "2026-04-29.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after cleanup: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("kept lines = %d, want 1; data=%q", len(lines), string(data))
	}
	var kept RawRecord
	if err := json.Unmarshal([]byte(lines[0]), &kept); err != nil {
		t.Fatalf("unmarshal kept: %v", err)
	}
	if kept.Text != "newer" {
		t.Errorf("kept.Text = %q, want %q", kept.Text, "newer")
	}
}

func TestRawWriter_CleanupBeforeRemovesEmptyFile(t *testing.T) {
	dir := t.TempDir()
	w := NewRawWriter(dir, []string{"slack/C1"})

	day := time.Date(2026, 4, 29, 0, 0, 0, 0, time.UTC)
	if err := w.Append(RawRecord{Timestamp: day.Add(1 * time.Hour), Platform: "slack", ChatID: "C1", Text: "x"}); err != nil {
		t.Fatalf("append: %v", err)
	}

	cutoff := day.Add(2 * time.Hour) // strictly after the only record
	if _, err := w.CleanupBefore(cutoff); err != nil {
		t.Fatalf("CleanupBefore: %v", err)
	}

	path := filepath.Join(dir, "raw", "slack", "C1", "2026-04-29.jsonl")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file should be removed; stat err = %v", err)
	}
}

func TestRawWriter_CleanupBeforePreservesMalformedLines(t *testing.T) {
	dir := t.TempDir()
	w := NewRawWriter(dir, []string{"slack/C1"})

	day := time.Date(2026, 4, 29, 0, 0, 0, 0, time.UTC)
	if err := w.Append(RawRecord{Timestamp: day.Add(1 * time.Hour), Platform: "slack", ChatID: "C1", Text: "old"}); err != nil {
		t.Fatalf("append: %v", err)
	}

	// Inject a malformed line directly to the same file.
	path := filepath.Join(dir, "raw", "slack", "C1", "2026-04-29.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := f.WriteString("not-valid-json\n"); err != nil {
		t.Fatalf("write malformed: %v", err)
	}
	f.Close()

	cutoff := day.Add(2 * time.Hour)
	if _, err := w.CleanupBefore(cutoff); err != nil {
		t.Fatalf("CleanupBefore: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after cleanup: %v", err)
	}
	if !strings.Contains(string(data), "not-valid-json") {
		t.Errorf("malformed line should be preserved; got %q", string(data))
	}
	if strings.Contains(string(data), "\"text\":\"old\"") {
		t.Errorf("old record should be removed; got %q", string(data))
	}
}
