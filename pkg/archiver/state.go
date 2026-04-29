package archiver

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

const stateRelDir = ".picoclaw-archive"
const stateFileName = "state.json"

type TopicIndexEntry struct {
	Slug     string   `json:"slug"`
	Title    string   `json:"title"`
	Channels []string `json:"channels"`
	Status   string   `json:"status"`
	Updated  string   `json:"updated"`
}

type State struct {
	Version                 int               `json:"version"`
	LastDistilledAt         time.Time         `json:"last_distilled_at"`
	LastPushedAt            time.Time         `json:"last_pushed_at"`
	ConsecutivePushFailures int               `json:"consecutive_push_failures"`
	TopicIndex              []TopicIndexEntry `json:"topic_index"`
}

func StatePath(repoRoot string) string {
	return filepath.Join(repoRoot, stateRelDir, stateFileName)
}

// ReadState loads state.json. If the file is missing, it returns a
// freshly-initialized State with Version=1.
func ReadState(repoRoot string) (State, error) {
	p := StatePath(repoRoot)
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return State{Version: 1}, nil
	}
	if err != nil {
		return State{}, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, err
	}
	if s.Version == 0 {
		s.Version = 1
	}
	return s, nil
}

// WriteState atomically writes state.json (write-then-rename).
func WriteState(repoRoot string, s State) error {
	if s.Version == 0 {
		s.Version = 1
	}
	dir := filepath.Join(repoRoot, stateRelDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "state-*.json")
	if err != nil {
		return err
	}
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(s); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, stateFileName))
}
