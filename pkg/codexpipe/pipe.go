package codexpipe

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/n-seiji/ebiclaw/pkg/bus"
	"github.com/n-seiji/ebiclaw/pkg/logger"
)

// minStreamedReplyInterval is the safety valve against Slack rate limiting
// (429) when codex emits several agent_message events in quick succession.
// Content-level coalescing is intentionally not done here; that grain of
// what gets reported is an AGENTS.md/prompt concern, not a code concern.
const minStreamedReplyInterval = time.Second

// Turner runs one Codex turn, invoking onMessage for each agent_message as
// it arrives. Implemented by *Runner.
type Turner interface {
	Run(ctx context.Context, threadID, sandbox, prompt string, onMessage func(text string)) (*Result, error)
}

// Pipe consumes inbound messages and pipes them to the Codex CLI.
type Pipe struct {
	bus    *bus.MessageBus
	runner Turner
	store  *ThreadStore

	mu       sync.Mutex
	sessions map[string]*sync.Mutex
	wg       sync.WaitGroup
}

// NewPipe creates a Pipe.
func NewPipe(b *bus.MessageBus, runner Turner, store *ThreadStore) *Pipe {
	return &Pipe{
		bus:      b,
		runner:   runner,
		store:    store,
		sessions: map[string]*sync.Mutex{},
	}
}

// Run consumes inbound messages until ctx is cancelled, then waits for
// in-flight turns to finish.
func (p *Pipe) Run(ctx context.Context) {
	defer p.wg.Wait()
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-p.bus.InboundChan():
			p.wg.Add(1)
			go func() {
				defer p.wg.Done()
				p.handle(ctx, msg)
			}()
		}
	}
}

func (p *Pipe) sessionKey(msg bus.InboundMessage) string {
	if msg.SessionKey != "" {
		return msg.SessionKey
	}
	return msg.Channel + ":" + msg.ChatID
}

func (p *Pipe) sessionLock(key string) *sync.Mutex {
	p.mu.Lock()
	defer p.mu.Unlock()
	if m, ok := p.sessions[key]; ok {
		return m
	}
	m := &sync.Mutex{}
	p.sessions[key] = m
	return m
}

func (p *Pipe) handle(ctx context.Context, msg bus.InboundMessage) {
	// Channels mark non-addressed messages (e.g. Slack messages without a
	// bot mention) as observe-only so the archiver can ingest them; they
	// must not trigger a codex turn or a reply.
	if msg.Metadata["observe_only"] == "true" {
		return
	}

	key := p.sessionKey(msg)
	lock := p.sessionLock(key)
	lock.Lock()
	defer lock.Unlock()

	threadID, _ := p.store.Get(key)

	if strings.TrimSpace(msg.Content) == "" {
		if len(msg.Media) > 0 {
			p.reply(ctx, msg, "⚠️ 添付ファイルは codex pipe モードでは未対応です")
		}
		return
	}

	sent := false
	var lastSent time.Time
	onMessage := func(text string) {
		if text == "" {
			return
		}
		if !lastSent.IsZero() {
			if wait := minStreamedReplyInterval - time.Since(lastSent); wait > 0 {
				time.Sleep(wait)
			}
		}
		lastSent = time.Now()
		sent = true
		p.reply(ctx, msg, text)
	}

	res, err := p.runner.Run(ctx, threadID, "", msg.Content, onMessage)
	if err != nil {
		logger.ErrorCF("codexpipe", "codex turn failed",
			map[string]any{"session": key, "error": err.Error()})
		if sent {
			p.reply(ctx, msg, fmt.Sprintf("⚠️ codex error after partial reply: %v", err))
		} else {
			p.reply(ctx, msg, fmt.Sprintf("⚠️ codex error: %v", err))
		}
	}
	if res != nil && res.ThreadID != "" && res.ThreadID != threadID {
		if err := p.store.Set(key, res.ThreadID); err != nil {
			logger.ErrorCF("codexpipe", "persist thread failed",
				map[string]any{"session": key, "error": err.Error()})
		}
	}
}

func (p *Pipe) reply(ctx context.Context, msg bus.InboundMessage, content string) {
	if content == "" {
		return
	}
	// Use a context detached from the run ctx so replies computed
	// during shutdown (run ctx cancelled) still get delivered while
	// the bus is open, instead of being silently dropped.
	pubCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	err := p.bus.PublishOutbound(pubCtx, bus.OutboundMessage{
		Channel:          msg.Channel,
		ChatID:           msg.ChatID,
		Content:          content,
		ReplyToMessageID: msg.MessageID,
	})
	if err != nil {
		logger.ErrorCF("codexpipe", "publish outbound failed",
			map[string]any{"channel": msg.Channel, "error": err.Error()})
	}
}
