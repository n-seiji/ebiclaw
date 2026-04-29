package archiver

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/adhocore/gronx"
)

// ErrBusy is returned when a manual trigger arrives while a batch is running.
var ErrBusy = errors.New("archiver busy")

type Service struct {
	mu        sync.Mutex
	cfg       atomic.Pointer[Config]
	llm       LLMClient
	rawWriter atomic.Pointer[RawWriter]
	observer  atomic.Pointer[Observer]

	running atomic.Bool
	stop    chan struct{}
	stopped chan struct{}
}

func NewService(cfg Config, llm LLMClient) *Service {
	s := &Service{llm: llm, stop: make(chan struct{}), stopped: make(chan struct{})}
	c := cfg
	s.cfg.Store(&c)
	if c.Active() {
		rw := NewRawWriter(c.RepositoryPath, c.Allowlist)
		s.rawWriter.Store(rw)
		s.observer.Store(NewObserver(rw))
	}
	return s
}

// Observer returns the bus.Observer-compatible adapter, or nil if inactive.
func (s *Service) Observer() *Observer {
	return s.observer.Load()
}

// Reload swaps in a new config. The caller is responsible for re-registering
// the observer with the bus when Observer() identity changes.
func (s *Service) Reload(cfg Config) {
	c := cfg
	s.cfg.Store(&c)
	if c.Active() {
		rw := NewRawWriter(c.RepositoryPath, c.Allowlist)
		s.rawWriter.Store(rw)
		s.observer.Store(NewObserver(rw))
	} else {
		s.rawWriter.Store(nil)
		s.observer.Store(nil)
	}
}

// RunOnce executes one distill+push cycle. Returns ErrBusy if another cycle is in flight.
func (s *Service) RunOnce(ctx context.Context) error {
	if !s.mu.TryLock() {
		return ErrBusy
	}
	defer s.mu.Unlock()

	cfg := s.cfg.Load()
	if cfg == nil || !cfg.Active() {
		return nil
	}

	d := NewDistiller(cfg.RepositoryPath, s.llm)
	res, err := d.Run(ctx, time.Time{})
	if err != nil {
		return fmt.Errorf("distill: %w", err)
	}
	if res.Skipped {
		return nil
	}

	pusher := NewGitPusher(cfg.RepositoryPath)
	summary := fmt.Sprintf("archive: %d created, %d updated, %d merged", res.Created, res.Updated, res.Merged)
	pr, err := pusher.Run(summary)
	st, _ := ReadState(cfg.RepositoryPath)
	if err != nil {
		st.ConsecutivePushFailures++
		_ = WriteState(cfg.RepositoryPath, st)
		return err
	}
	if pr.Pushed {
		st.ConsecutivePushFailures = 0
		st.LastPushedAt = time.Now().UTC()
		_ = WriteState(cfg.RepositoryPath, st)
	}
	return nil
}

// Start launches the cron loop. Calling Start more than once is a no-op.
func (s *Service) Start(ctx context.Context) {
	if !s.running.CompareAndSwap(false, true) {
		return
	}
	go s.loop(ctx)
}

// Stop signals the cron loop and waits for it to exit.
func (s *Service) Stop() {
	if !s.running.Load() {
		return
	}
	close(s.stop)
	<-s.stopped
}

func (s *Service) loop(ctx context.Context) {
	defer close(s.stopped)
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	last := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stop:
			return
		case now := <-ticker.C:
			cfg := s.cfg.Load()
			if cfg == nil || !cfg.Active() {
				continue
			}
			loc, err := time.LoadLocation(cfg.Schedule.Timezone)
			if err != nil {
				loc = time.UTC
			}
			due, err := gronx.NextTickAfter(cfg.Schedule.Cron, last.In(loc), false)
			if err != nil {
				continue
			}
			if !now.In(loc).Before(due) {
				_ = s.RunOnce(ctx)
				last = now
			}
		}
	}
}
