package codexpipe

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/n-seiji/ebiclaw/pkg/bus"
)

type fakeTurner struct {
	mu               sync.Mutex
	calls            []struct{ ThreadID, Sandbox, Prompt string }
	resp             *Result
	err              error
	blockUntilCancel bool
}

func (f *fakeTurner) Run(ctx context.Context, threadID, sandbox, prompt string) (*Result, error) {
	f.mu.Lock()
	f.calls = append(f.calls, struct{ ThreadID, Sandbox, Prompt string }{threadID, sandbox, prompt})
	blockUntilCancel := f.blockUntilCancel
	f.mu.Unlock()
	if blockUntilCancel {
		<-ctx.Done()
	}
	return f.resp, f.err
}

func waitOutbound(t *testing.T, b *bus.MessageBus) bus.OutboundMessage {
	t.Helper()
	select {
	case msg := <-b.OutboundChan():
		return msg
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for outbound message")
		return bus.OutboundMessage{}
	}
}

func TestPipeRepliesAndPersistsThread(t *testing.T) {
	b := bus.NewMessageBus()
	store := NewThreadStore(filepath.Join(t.TempDir(), "threads.json"))
	turner := &fakeTurner{resp: &Result{Text: "answer", ThreadID: "th-1"}}
	p := NewPipe(b, turner, store)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)

	in := bus.InboundMessage{Channel: "slack", ChatID: "C1", Content: "hi", MessageID: "m1"}
	if err := b.PublishInbound(ctx, in); err != nil {
		t.Fatalf("PublishInbound: %v", err)
	}

	out := waitOutbound(t, b)
	if out.Content != "answer" {
		t.Errorf("Content = %q, want %q", out.Content, "answer")
	}
	if out.Channel != "slack" || out.ChatID != "C1" {
		t.Errorf("route = %s/%s, want slack/C1", out.Channel, out.ChatID)
	}
	if id, ok := store.Get("slack:C1"); !ok || id != "th-1" {
		t.Errorf("stored thread = %q,%v, want %q,true", id, ok, "th-1")
	}
}

func TestPipeResumesExistingThread(t *testing.T) {
	b := bus.NewMessageBus()
	store := NewThreadStore(filepath.Join(t.TempDir(), "threads.json"))
	if err := store.Set("slack:C1", "th-old"); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	turner := &fakeTurner{resp: &Result{Text: "ok", ThreadID: "th-old"}}
	p := NewPipe(b, turner, store)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)

	_ = b.PublishInbound(ctx, bus.InboundMessage{Channel: "slack", ChatID: "C1", Content: "again"})
	waitOutbound(t, b)

	turner.mu.Lock()
	defer turner.mu.Unlock()
	if len(turner.calls) != 1 || turner.calls[0].ThreadID != "th-old" {
		t.Errorf("calls = %+v, want one call with ThreadID th-old", turner.calls)
	}
}

func TestPipeReportsErrors(t *testing.T) {
	b := bus.NewMessageBus()
	store := NewThreadStore(filepath.Join(t.TempDir(), "threads.json"))
	turner := &fakeTurner{err: context.DeadlineExceeded}
	p := NewPipe(b, turner, store)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)

	_ = b.PublishInbound(ctx, bus.InboundMessage{Channel: "slack", ChatID: "C1", Content: "hi"})
	out := waitOutbound(t, b)
	if out.Content == "" {
		t.Errorf("error reply is empty, want non-empty error message")
	}
}

func TestPipeDeliversReplyComputedDuringShutdown(t *testing.T) {
	b := bus.NewMessageBus()
	store := NewThreadStore(filepath.Join(t.TempDir(), "threads.json"))
	turner := &fakeTurner{resp: &Result{Text: "late answer", ThreadID: "th-1"}, blockUntilCancel: true}
	p := NewPipe(b, turner, store)

	ctx, cancel := context.WithCancel(context.Background())

	go p.Run(ctx)

	in := bus.InboundMessage{Channel: "slack", ChatID: "C1", Content: "hi", MessageID: "m1"}
	if err := b.PublishInbound(ctx, in); err != nil {
		t.Fatalf("PublishInbound: %v", err)
	}

	// Wait until the turner has been invoked, then cancel the run ctx
	// while it's still blocked, simulating shutdown mid-flight.
	deadline := time.After(2 * time.Second)
	for {
		turner.mu.Lock()
		called := len(turner.calls) > 0
		turner.mu.Unlock()
		if called {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for turner to be called")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()

	out := waitOutbound(t, b)
	if out.Content != "late answer" {
		t.Errorf("Content = %q, want %q", out.Content, "late answer")
	}
}

func waitNoOutbound(t *testing.T, b *bus.MessageBus) {
	t.Helper()
	select {
	case msg := <-b.OutboundChan():
		t.Fatalf("unexpected outbound message: %+v", msg)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestPipeSkipsEmptyContentWithMedia(t *testing.T) {
	b := bus.NewMessageBus()
	store := NewThreadStore(filepath.Join(t.TempDir(), "threads.json"))
	turner := &fakeTurner{resp: &Result{Text: "answer", ThreadID: "th-1"}}
	p := NewPipe(b, turner, store)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)

	in := bus.InboundMessage{Channel: "slack", ChatID: "C1", Content: "  ", Media: []string{"file1.png"}, MessageID: "m1"}
	if err := b.PublishInbound(ctx, in); err != nil {
		t.Fatalf("PublishInbound: %v", err)
	}

	out := waitOutbound(t, b)
	if out.Content != "⚠️ 添付ファイルは codex pipe モードでは未対応です" {
		t.Errorf("Content = %q, want warning message", out.Content)
	}

	turner.mu.Lock()
	defer turner.mu.Unlock()
	if len(turner.calls) != 0 {
		t.Errorf("calls = %d, want 0 (turner should not be invoked)", len(turner.calls))
	}
}

func TestPipeSkipsEmptyContentWithoutMedia(t *testing.T) {
	b := bus.NewMessageBus()
	store := NewThreadStore(filepath.Join(t.TempDir(), "threads.json"))
	turner := &fakeTurner{resp: &Result{Text: "answer", ThreadID: "th-1"}}
	p := NewPipe(b, turner, store)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)

	in := bus.InboundMessage{Channel: "slack", ChatID: "C1", Content: "", MessageID: "m1"}
	if err := b.PublishInbound(ctx, in); err != nil {
		t.Fatalf("PublishInbound: %v", err)
	}

	waitNoOutbound(t, b)

	turner.mu.Lock()
	defer turner.mu.Unlock()
	if len(turner.calls) != 0 {
		t.Errorf("calls = %d, want 0 (turner should not be invoked)", len(turner.calls))
	}
}

func TestPipeSkipsObserveOnlyMessages(t *testing.T) {
	b := bus.NewMessageBus()
	store := NewThreadStore(filepath.Join(t.TempDir(), "threads.json"))
	turner := &fakeTurner{resp: &Result{Text: "answer", ThreadID: "th-1"}}
	p := NewPipe(b, turner, store)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)

	in := bus.InboundMessage{
		Channel:   "slack",
		ChatID:    "C1",
		Content:   "not addressed to the bot",
		MessageID: "m1",
		Metadata:  map[string]string{"observe_only": "true"},
	}
	if err := b.PublishInbound(ctx, in); err != nil {
		t.Fatalf("PublishInbound: %v", err)
	}

	waitNoOutbound(t, b)

	turner.mu.Lock()
	defer turner.mu.Unlock()
	if len(turner.calls) != 0 {
		t.Errorf("calls = %d, want 0 (observe-only must not trigger a turn)", len(turner.calls))
	}
}
