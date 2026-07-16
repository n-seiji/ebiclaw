package codexpipe

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/n-seiji/ebiclaw/pkg/bus"
	"github.com/n-seiji/ebiclaw/pkg/logger"
)

// Turner runs one Codex turn. Implemented by *Runner.
type Turner interface {
	Run(ctx context.Context, threadID, sandbox, prompt string) (*Result, error)
}

// Options configures pipe behavior.
type Options struct {
	Sandbox  string
	TwoStage bool
}

// Pipe consumes inbound messages and pipes them to the Codex CLI.
type Pipe struct {
	bus    *bus.MessageBus
	runner Turner
	store  *ThreadStore
	opts   Options

	mu       sync.Mutex
	sessions map[string]*sync.Mutex
	wg       sync.WaitGroup
}

// NewPipe creates a Pipe.
func NewPipe(b *bus.MessageBus, runner Turner, store *ThreadStore, opts Options) *Pipe {
	return &Pipe{
		bus:      b,
		runner:   runner,
		store:    store,
		opts:     opts,
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

	res, err := p.turn(ctx, threadID, msg.Content)
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

const plannerPrefix = `まず実装計画だけを立ててください。ファイルは変更せず、調査して計画を返答してください。

リクエスト:
`

const executorPrompt = `上記の計画を実行してください。完了したら結果を簡潔に報告してください。`

// turn runs a single- or two-stage turn. In two-stage mode a read-only
// planning turn runs first, then execution resumes the same thread.
func (p *Pipe) turn(ctx context.Context, threadID, content string) (*Result, error) {
	if !p.opts.TwoStage {
		return p.runner.Run(ctx, threadID, "", content)
	}

	plan, err := p.runner.Run(ctx, threadID, "read-only", plannerPrefix+content)
	if err != nil {
		return nil, fmt.Errorf("plan stage: %w", err)
	}
	resumeID := plan.ThreadID
	if resumeID == "" {
		resumeID = threadID
	}
	res, err := p.runner.Run(ctx, resumeID, "", executorPrompt)
	if err != nil {
		return nil, fmt.Errorf("execute stage: %w", err)
	}
	if res.ThreadID == "" {
		res.ThreadID = resumeID
	}
	return res, nil
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
