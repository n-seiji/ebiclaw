package codexpipe

import (
	"path/filepath"
	"testing"
)

func TestThreadStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "threads.json")

	s := NewThreadStore(path)
	if _, ok := s.Get("slack:C1"); ok {
		t.Fatalf("Get on empty store = ok, want !ok")
	}
	if err := s.Set("slack:C1", "thread-1"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got, ok := s.Get("slack:C1"); !ok || got != "thread-1" {
		t.Errorf("Get = %q,%v, want %q,true", got, ok, "thread-1")
	}

	// reload from disk
	s2 := NewThreadStore(path)
	if got, ok := s2.Get("slack:C1"); !ok || got != "thread-1" {
		t.Errorf("reloaded Get = %q,%v, want %q,true", got, ok, "thread-1")
	}

	if err := s2.Delete("slack:C1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := s2.Get("slack:C1"); ok {
		t.Errorf("Get after Delete = ok, want !ok")
	}
}
