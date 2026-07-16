package archiver

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/adhocore/gronx"
	"github.com/n-seiji/ebiclaw/pkg/logger"
)

// ErrBusy is returned when a manual trigger arrives while a batch is running.
var ErrBusy = errors.New("archiver busy")

type Service struct {
	mu        sync.Mutex
	cfg       atomic.Pointer[Config]
	llm       LLMClient
	rawWriter atomic.Pointer[RawWriter]
	observer  atomic.Pointer[Observer]

	logMu sync.Mutex
	logs  []LogEntry

	running       atomic.Bool
	runInProgress atomic.Bool
	stop          chan struct{}
	stopped       chan struct{}
}

type LogEntry struct {
	Time    time.Time      `json:"time"`
	Level   string         `json:"level"`
	Message string         `json:"message"`
	Fields  map[string]any `json:"fields,omitempty"`
}

type Status struct {
	Running                 bool       `json:"running"`
	ServiceRunning          bool       `json:"service_running"`
	RunInProgress           bool       `json:"run_in_progress"`
	LastDistilledAt         *time.Time `json:"last_distilled_at,omitempty"`
	LastPushedAt            *time.Time `json:"last_pushed_at,omitempty"`
	ConsecutivePushFailures int        `json:"consecutive_push_failures"`
	Logs                    []LogEntry `json:"logs,omitempty"`
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
		logger.WarnC("archiver", "Run skipped: archiver busy")
		s.addLog("warn", "Run skipped: archiver busy", nil)
		return ErrBusy
	}
	defer s.mu.Unlock()
	s.runInProgress.Store(true)
	defer s.runInProgress.Store(false)

	cfg := s.cfg.Load()
	if cfg == nil || !cfg.Active() {
		logger.WarnC("archiver", "Run skipped: archiver inactive")
		s.addLog("warn", "Run skipped: archiver inactive", nil)
		return nil
	}
	startFields := map[string]any{
		"repository_path": cfg.RepositoryPath,
		"allowlist_count": len(cfg.Allowlist),
		"model_name":      cfg.Distill.ModelName,
	}
	logger.InfoCF("archiver", "Run started", startFields)
	s.addLog("info", "Run started", startFields)

	d := NewDistiller(cfg.RepositoryPath, s.llm)
	s.addLog("info", "Distill started; waiting for LLM", map[string]any{
		"repository_path": cfg.RepositoryPath,
	})
	res, err := d.Run(ctx, time.Time{})
	if err != nil {
		fields := map[string]any{
			"error":           err.Error(),
			"repository_path": cfg.RepositoryPath,
		}
		logger.ErrorCF("archiver", "Run failed during distill", fields)
		s.addLog("error", "Run failed during distill", fields)
		return fmt.Errorf("distill: %w", err)
	}
	if res.Skipped {
		fields := map[string]any{
			"repository_path": cfg.RepositoryPath,
		}
		logger.InfoCF("archiver", "Run skipped: no new raw messages", fields)
		s.addLog("info", "Run skipped: no new raw messages", fields)
		return nil
	}
	s.addLog("info", "Distill completed", map[string]any{
		"created": res.Created,
		"updated": res.Updated,
		"merged":  res.Merged,
	})

	// Prune raw entries that were just distilled, before the git commit, so
	// the deletion lands in the same commit as the topic updates. Cleanup
	// failures are surfaced (not swallowed) but do not block the push: the
	// distilled output is still committed and push results take precedence
	// for the returned error.
	var cleanupErr error
	if rw := s.rawWriter.Load(); rw != nil && !res.CutoffAt.IsZero() {
		deleted, cerr := rw.CleanupBefore(res.CutoffAt)
		if cerr != nil {
			cleanupErr = fmt.Errorf("cleanup raw: %w", cerr)
			fields := map[string]any{
				"error":           cerr.Error(),
				"repository_path": cfg.RepositoryPath,
			}
			logger.WarnCF("archiver", "Raw cleanup failed", fields)
			s.addLog("warn", "Raw cleanup failed", fields)
		} else {
			fields := map[string]any{
				"deleted":         deleted,
				"repository_path": cfg.RepositoryPath,
			}
			logger.InfoCF("archiver", "Raw cleanup completed", fields)
			s.addLog("info", "Raw cleanup completed", fields)
		}
	}

	pusher := NewGitPusher(cfg.RepositoryPath)
	summary := fmt.Sprintf("archive: %d created, %d updated, %d merged", res.Created, res.Updated, res.Merged)
	s.addLog("info", "Git commit/push started", map[string]any{
		"summary": summary,
	})
	pr, err := pusher.Run(summary)
	st, _ := ReadState(cfg.RepositoryPath)
	if err != nil {
		st.ConsecutivePushFailures++
		_ = WriteState(cfg.RepositoryPath, st)
		fields := map[string]any{
			"error":                     err.Error(),
			"repository_path":           cfg.RepositoryPath,
			"consecutive_push_failures": st.ConsecutivePushFailures,
			"created":                   res.Created,
			"updated":                   res.Updated,
			"merged":                    res.Merged,
		}
		logger.ErrorCF("archiver", "Git push failed", fields)
		s.addLog("error", "Git push failed", fields)
		return err
	}
	if pr.Pushed {
		st.ConsecutivePushFailures = 0
		st.LastPushedAt = time.Now().UTC()
		_ = WriteState(cfg.RepositoryPath, st)
	}
	if !pr.Committed {
		s.addLog("info", "Git skipped: no changes to commit", map[string]any{
			"repository_path": cfg.RepositoryPath,
		})
	}
	fields := map[string]any{
		"repository_path": cfg.RepositoryPath,
		"created":         res.Created,
		"updated":         res.Updated,
		"merged":          res.Merged,
		"pushed":          pr.Pushed,
		"committed":       pr.Committed,
	}
	logger.InfoCF("archiver", "Run completed", fields)
	s.addLog("info", "Run completed", fields)
	return cleanupErr
}

func (s *Service) Status() Status {
	status := Status{
		Running:        s.running.Load(),
		ServiceRunning: s.running.Load(),
		RunInProgress:  s.runInProgress.Load(),
		Logs:           s.recentLogs(),
	}

	cfg := s.cfg.Load()
	if cfg == nil || cfg.RepositoryPath == "" {
		return status
	}

	st, err := ReadState(cfg.RepositoryPath)
	if err != nil {
		return status
	}

	status.LastDistilledAt = timePtrIfNonZero(st.LastDistilledAt)
	status.LastPushedAt = timePtrIfNonZero(st.LastPushedAt)
	status.ConsecutivePushFailures = st.ConsecutivePushFailures
	return status
}

func timePtrIfNonZero(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func (s *Service) addLog(level, message string, fields map[string]any) {
	entry := LogEntry{
		Time:    time.Now().UTC(),
		Level:   level,
		Message: message,
		Fields:  fields,
	}
	s.logMu.Lock()
	defer s.logMu.Unlock()
	s.logs = append(s.logs, entry)
	const maxLogs = 80
	if len(s.logs) > maxLogs {
		copy(s.logs, s.logs[len(s.logs)-maxLogs:])
		s.logs = s.logs[:maxLogs]
	}
}

func (s *Service) recentLogs() []LogEntry {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	logs := make([]LogEntry, len(s.logs))
	copy(logs, s.logs)
	return logs
}

// Start launches the cron loop. Calling Start more than once is a no-op.
func (s *Service) Start(ctx context.Context) {
	if !s.running.CompareAndSwap(false, true) {
		return
	}
	go s.loop(ctx)
}

// Stop signals the cron loop and waits for it to exit. Idempotent: a second
// call returns immediately without re-closing channels.
func (s *Service) Stop() {
	if !s.running.CompareAndSwap(true, false) {
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
