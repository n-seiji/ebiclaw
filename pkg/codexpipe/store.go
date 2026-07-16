package codexpipe

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// ThreadStore persists the sessionKey -> codex thread ID mapping as JSON.
type ThreadStore struct {
	mu      sync.Mutex
	path    string
	threads map[string]string
}

// NewThreadStore loads (or lazily creates) the store at path.
func NewThreadStore(path string) *ThreadStore {
	s := &ThreadStore{path: path, threads: map[string]string{}}
	data, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(data, &s.threads)
	} else if !errors.Is(err, fs.ErrNotExist) {
		// unreadable store: start empty; Set will attempt to rewrite
		s.threads = map[string]string{}
	}
	return s
}

// Get returns the thread ID for sessionKey.
func (s *ThreadStore) Get(sessionKey string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.threads[sessionKey]
	return id, ok
}

// Set records the thread ID for sessionKey and saves to disk.
func (s *ThreadStore) Set(sessionKey, threadID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.threads[sessionKey] = threadID
	return s.save()
}

// Delete removes sessionKey and saves to disk.
func (s *ThreadStore) Delete(sessionKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.threads, sessionKey)
	return s.save()
}

func (s *ThreadStore) save() error {
	data, err := json.MarshalIndent(s.threads, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal thread store: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create thread store dir: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write thread store: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("rename thread store: %w", err)
	}
	return nil
}
