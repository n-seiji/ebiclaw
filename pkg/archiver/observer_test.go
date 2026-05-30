package archiver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/n-seiji/ebiclaw/pkg/bus"
)

func TestObserver_RecordsInboundAndOutbound(t *testing.T) {
	dir := t.TempDir()
	rw := NewRawWriter(dir, []string{"slack/C1"})
	obs := NewObserver(rw)

	obs.OnInbound(context.Background(), bus.InboundMessage{
		Channel: "slack", ChatID: "C1", Content: "hi",
		Sender: bus.SenderInfo{Platform: "slack", PlatformID: "U1", Username: "alice"},
		Metadata: map[string]string{
			"thread_ts": "1700000000.000100",
		},
	})
	obs.OnOutbound(context.Background(), bus.OutboundMessage{
		Channel: "slack", ChatID: "C1", Content: "hello back",
		Metadata: map[string]string{
			"thread_ts": "1700000000.000100",
		},
	})

	files, _ := filepath.Glob(filepath.Join(dir, "raw", "slack", "C1", "*.jsonl"))
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %v", files)
	}
	data, _ := os.ReadFile(files[0])
	s := string(data)
	if !strings.Contains(s, `"role":"user"`) || !strings.Contains(s, `"role":"assistant"`) {
		t.Fatalf("missing both roles: %s", s)
	}
	if strings.Count(s, `"thread_id":"1700000000.000100"`) != 2 {
		t.Fatalf("expected thread_id recorded for both messages: %s", s)
	}
}
