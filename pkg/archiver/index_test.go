package archiver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRenderIndex(t *testing.T) {
	entries := []TopicIndexEntry{
		{Slug: "a", Title: "A", Status: "open", Updated: "2026-04-30", Channels: []string{"slack/C1"}},
		{Slug: "b", Title: "B", Status: "resolved", Updated: "2026-04-25", Channels: []string{"pico/main"}},
	}
	got := RenderIndex(entries)
	if !strings.Contains(got, "[A](topics/a.md)") {
		t.Fatalf("missing link to a: %s", got)
	}
	if !strings.Contains(got, "[B](topics/b.md)") {
		t.Fatalf("missing link to b: %s", got)
	}
	if strings.Index(got, "[A]") > strings.Index(got, "[B]") {
		t.Fatalf("open should appear before resolved")
	}
}

func TestAppendLog(t *testing.T) {
	dir := t.TempDir()
	ts := time.Date(2026, 4, 29, 3, 0, 0, 0, time.UTC)
	if err := AppendLog(dir, ts, "distilled: 1 created, 2 updated"); err != nil {
		t.Fatalf("append: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "log.md"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(data), "2026-04-29T03:00:00Z") {
		t.Fatalf("missing ts: %s", data)
	}
	if !strings.Contains(string(data), "1 created, 2 updated") {
		t.Fatalf("missing summary: %s", data)
	}
}
