package archiver

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
	key := ChannelKey(rec.Platform, archiveChatBase(rec.ChatID))
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

func archiveChatBase(chatID string) string {
	base, _, _ := strings.Cut(chatID, "/")
	return base
}

// CleanupBefore removes raw records whose timestamp is at or before cutoff.
// Files that end up empty are deleted. The writer mutex is held for the
// entire walk so concurrent Append calls do not race with rewrites.
// Malformed JSON lines are kept (we do not silently lose unparsable data).
func (w *RawWriter) CleanupBefore(cutoff time.Time) (deleted int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	rawDir := filepath.Join(w.repoRoot, "raw")
	walkErr := filepath.Walk(rawDir, func(p string, info os.FileInfo, werr error) error {
		if werr != nil {
			if os.IsNotExist(werr) {
				return nil
			}
			return werr
		}
		if info.IsDir() || !strings.HasSuffix(p, ".jsonl") {
			return nil
		}
		n, ferr := cleanupRawFile(p, cutoff)
		deleted += n
		return ferr
	})
	if walkErr != nil && !os.IsNotExist(walkErr) {
		return deleted, walkErr
	}
	return deleted, nil
}

// cleanupRawFile filters one .jsonl, keeping only records with ts > cutoff.
// On empty result the file is removed. Atomic via tmp+rename.
func cleanupRawFile(path string, cutoff time.Time) (int, error) {
	in, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer in.Close()

	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var kept [][]byte
	deleted := 0
	for sc.Scan() {
		raw := sc.Bytes()
		// Scanner reuses its internal buffer; copy before next iteration.
		line := append([]byte(nil), raw...)
		var rec struct {
			Timestamp time.Time `json:"ts"`
		}
		if jerr := json.Unmarshal(line, &rec); jerr != nil {
			// Preserve malformed lines so cleanup is never lossy for data we
			// cannot interpret; an operator can still inspect them.
			kept = append(kept, line)
			continue
		}
		if rec.Timestamp.After(cutoff) {
			kept = append(kept, line)
		} else {
			deleted++
		}
	}
	if scerr := sc.Err(); scerr != nil {
		return deleted, scerr
	}

	if len(kept) == 0 {
		if rerr := os.Remove(path); rerr != nil {
			return deleted, rerr
		}
		return deleted, nil
	}

	tmp := path + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return deleted, err
	}
	for _, line := range kept {
		if _, werr := out.Write(append(line, '\n')); werr != nil {
			out.Close()
			os.Remove(tmp)
			return deleted, werr
		}
	}
	if cerr := out.Close(); cerr != nil {
		os.Remove(tmp)
		return deleted, cerr
	}
	if rerr := os.Rename(tmp, path); rerr != nil {
		os.Remove(tmp)
		return deleted, rerr
	}
	return deleted, nil
}
