package archiver

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestState_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := State{
		Version:                 1,
		LastDistilledAt:         time.Date(2026, 4, 29, 3, 0, 0, 0, time.UTC),
		LastPushedAt:            time.Date(2026, 4, 29, 3, 0, 42, 0, time.UTC),
		ConsecutivePushFailures: 0,
		TopicIndex: []TopicIndexEntry{
			{Slug: "login-flow-bug", Title: "ログインフロー", Channels: []string{"slack/C1"}, Status: "open", Updated: "2026-04-30"},
		},
	}
	if err := WriteState(dir, s); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := ReadState(dir)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got.TopicIndex) != 1 || got.TopicIndex[0].Slug != "login-flow-bug" {
		t.Fatalf("unexpected: %+v", got)
	}
	want := filepath.Join(dir, ".picoclaw-archive", "state.json")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("file missing: %v", err)
	}
}

func TestState_DefaultsWhenMissing(t *testing.T) {
	dir := t.TempDir()
	got, err := ReadState(dir)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.Version != 1 || len(got.TopicIndex) != 0 {
		t.Fatalf("expected defaults, got %+v", got)
	}
}
