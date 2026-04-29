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
