package archiver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Sender carries a subset of bus.SenderInfo persisted to raw.
type Sender struct {
	PlatformID  string `json:"platform_id,omitempty"`
	Username    string `json:"username,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
}

// RawRecord is one line in a raw/<platform>/<chat_id>/YYYY-MM-DD.jsonl file.
type RawRecord struct {
	Timestamp      time.Time `json:"ts"`
	Role           string    `json:"role"` // "user" | "assistant" | "system"
	Platform       string    `json:"platform"`
	ChatID         string    `json:"chat_id"`
	ChannelDisplay string    `json:"channel_display,omitempty"`
	ThreadID       string    `json:"thread_id,omitempty"`
	MessageID      string    `json:"message_id,omitempty"`
	Sender         Sender    `json:"sender,omitempty"`
	Text           string    `json:"text"`
}

// RawWriter appends RawRecord lines to a per-day jsonl file under repoRoot/raw/.
type RawWriter struct {
	mu        sync.Mutex
	repoRoot  string
	allowlist map[string]struct{}
}

// NewRawWriter constructs a writer rooted at repoRoot. Allowlist entries are
// canonical "<platform>/<chat_id>" keys.
func NewRawWriter(repoRoot string, allowlist []string) *RawWriter {
	set := make(map[string]struct{}, len(allowlist))
	for _, k := range allowlist {
		set[k] = struct{}{}
	}
	return &RawWriter{repoRoot: repoRoot, allowlist: set}
}

// Append writes one record as a JSON line. Records that are not allowlisted
// are silently dropped (no error).
func (w *RawWriter) Append(rec RawRecord) error {
	if rec.Platform == "" || rec.ChatID == "" {
		return nil
	}
	key := ChannelKey(rec.Platform, rec.ChatID)
	if _, ok := w.allowlist[key]; !ok {
		return nil
	}

	day := rec.Timestamp.UTC().Format("2006-01-02")
	dir := filepath.Join(w.repoRoot, "raw", rec.Platform, rec.ChatID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, day+".jsonl")

	w.mu.Lock()
	defer w.mu.Unlock()

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}
	return nil
}
