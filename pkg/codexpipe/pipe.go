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

// Turner runs one Codex turn. Implemented by *Runner.
type Turner interface {
	Run(ctx context.Context, threadID, sandbox, prompt string) (*Result, error)
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

	res, err := p.runner.Run(ctx, threadID, "", msg.Content)
	if err != nil {
		logger.ErrorCF("codexpipe", "codex turn failed",
			map[string]any{"session": key, "error": err.Error()})
		p.reply(ctx, msg, fmt.Sprintf("⚠️ codex error: %v", err))
		return
	}
	if res.ThreadID != "" && res.ThreadID != threadID {
		if err := p.store.Set(key, res.ThreadID); err != nil {
			logger.ErrorCF("codexpipe", "persist thread failed",
				map[string]any{"session": key, "error": err.Error()})
		}
	}
	p.reply(ctx, msg, res.Text)
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
