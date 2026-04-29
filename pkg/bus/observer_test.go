package bus

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestObserver_ReceivesInbound(t *testing.T) {
	mb := NewMessageBus()
	defer mb.Close()

	got := make(chan InboundMessage, 1)
	cancel := mb.Subscribe(ObserverFuncs{
		InboundFn: func(_ context.Context, m InboundMessage) {
			got <- m
		},
	})
	defer cancel()

	// drain default consumer so PublishInbound does not block
	go func() {
		for range mb.InboundChan() {
		}
	}()

	ctx, cancelCtx := context.WithTimeout(context.Background(), time.Second)
	defer cancelCtx()
	if err := mb.PublishInbound(ctx, InboundMessage{Channel: "slack", ChatID: "C1", Content: "hi"}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case m := <-got:
		if m.ChatID != "C1" || m.Content != "hi" {
			t.Fatalf("unexpected msg: %+v", m)
		}
	case <-time.After(time.Second):
		t.Fatal("observer did not receive inbound message")
	}
}

func TestObserver_ReceivesOutbound(t *testing.T) {
	mb := NewMessageBus()
	defer mb.Close()

	got := make(chan OutboundMessage, 1)
	cancel := mb.Subscribe(ObserverFuncs{
		OutboundFn: func(_ context.Context, m OutboundMessage) {
			got <- m
		},
	})
	defer cancel()

	go func() {
		for range mb.OutboundChan() {
		}
	}()

	ctx, cancelCtx := context.WithTimeout(context.Background(), time.Second)
	defer cancelCtx()
	if err := mb.PublishOutbound(ctx, OutboundMessage{Channel: "slack", ChatID: "C1", Content: "out"}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case m := <-got:
		if m.ChatID != "C1" || m.Content != "out" {
			t.Fatalf("unexpected msg: %+v", m)
		}
	case <-time.After(time.Second):
		t.Fatal("observer did not receive outbound message")
	}
}

func TestObserver_MultipleAndCancel(t *testing.T) {
	mb := NewMessageBus()
	defer mb.Close()
	go func() {
		for range mb.InboundChan() {
		}
	}()
	go func() {
		for range mb.OutboundChan() {
		}
	}()

	var aHits, bHits atomicCounter
	cancelA := mb.Subscribe(ObserverFuncs{InboundFn: func(_ context.Context, _ InboundMessage) { aHits.inc() }})
	cancelB := mb.Subscribe(ObserverFuncs{InboundFn: func(_ context.Context, _ InboundMessage) { bHits.inc() }})

	ctx := context.Background()
	_ = mb.PublishInbound(ctx, InboundMessage{ChatID: "1"})
	time.Sleep(20 * time.Millisecond)

	cancelA()
	_ = mb.PublishInbound(ctx, InboundMessage{ChatID: "2"})
	time.Sleep(20 * time.Millisecond)

	cancelB()

	if got := aHits.get(); got != 1 {
		t.Fatalf("aHits=%d, want 1", got)
	}
	if got := bHits.get(); got != 2 {
		t.Fatalf("bHits=%d, want 2", got)
	}
}

type atomicCounter struct {
	mu sync.Mutex
	n  int
}

func (c *atomicCounter) inc()     { c.mu.Lock(); c.n++; c.mu.Unlock() }
func (c *atomicCounter) get() int { c.mu.Lock(); defer c.mu.Unlock(); return c.n }
