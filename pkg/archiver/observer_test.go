package archiver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/bus"
)

func TestObserver_RecordsInboundAndOutbound(t *testing.T) {
	dir := t.TempDir()
	rw := NewRawWriter(dir, []string{"slack/C1"})
	obs := NewObserver(rw)

	obs.OnInbound(context.Background(), bus.InboundMessage{
		Channel: "slack", ChatID: "C1", Content: "hi",
		Sender: bus.SenderInfo{Platform: "slack", PlatformID: "U1", Username: "alice"},
	})
	obs.OnOutbound(context.Background(), bus.OutboundMessage{
		Channel: "slack", ChatID: "C1", Content: "hello back",
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
}
