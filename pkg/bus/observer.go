package bus

import (
	"context"
	"sync"
)

// Observer receives copies of messages published to the bus.
// Implementations must be non-blocking; the bus dispatches in a goroutine.
type Observer interface {
	OnInbound(ctx context.Context, m InboundMessage)
	OnOutbound(ctx context.Context, m OutboundMessage)
}

// ObserverFuncs is a convenience adapter so callers can supply only what they need.
// Field names intentionally differ from the method names to avoid Go's
// "field and method with the same name" compile error.
type ObserverFuncs struct {
	InboundFn  func(ctx context.Context, m InboundMessage)
	OutboundFn func(ctx context.Context, m OutboundMessage)
}

func (f ObserverFuncs) OnInbound(ctx context.Context, m InboundMessage) {
	if f.InboundFn != nil {
		f.InboundFn(ctx, m)
	}
}

func (f ObserverFuncs) OnOutbound(ctx context.Context, m OutboundMessage) {
	if f.OutboundFn != nil {
		f.OutboundFn(ctx, m)
	}
}

type observerEntry struct {
	id  uint64
	obs Observer
}

type observerRegistry struct {
	mu      sync.RWMutex
	nextID  uint64
	entries []observerEntry
}

func (r *observerRegistry) add(o Observer) (cancel func()) {
	r.mu.Lock()
	r.nextID++
	id := r.nextID
	r.entries = append(r.entries, observerEntry{id: id, obs: o})
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		for i, e := range r.entries {
			if e.id == id {
				r.entries = append(r.entries[:i], r.entries[i+1:]...)
				return
			}
		}
	}
}

func (r *observerRegistry) snapshot() []Observer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Observer, len(r.entries))
	for i, e := range r.entries {
		out[i] = e.obs
	}
	return out
}
