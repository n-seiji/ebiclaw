package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/sipeed/picoclaw/pkg/archiver"
	"github.com/sipeed/picoclaw/web/backend/launcherconfig"
)

// registerArchiverRoutes mounts the conversation-archiver endpoints. The
// concrete archiver.Service runs inside the gateway process, so the launcher
// can read/write the config block but cannot drive runs directly; runner is
// nil here and the run/status endpoints behave accordingly.
func (h *Handler) registerArchiverRoutes(mux *http.ServeMux) {
	store := launcherconfig.NewArchiverStore(h.configPath)
	ah := NewArchiverHandler(store, nil)
	mux.Handle("/api/archiver/config", ah)
	mux.Handle("/api/archiver/status", ah)
	mux.Handle("/api/archiver/run", ah)
}

// ArchiverConfigStore reads and writes the archiver block of config.json.
// The concrete implementation is supplied by web/backend/launcherconfig.
type ArchiverConfigStore interface {
	Get() map[string]any
	Put(c map[string]any) error
}

// ArchiverRunner exposes the live archiver.Service for status and on-demand
// runs. nil is allowed: status returns minimal info; run returns 503.
type ArchiverRunner interface {
	RunOnce(ctx context.Context) error
	Status() ArchiverStatusSnapshot
}

type ArchiverStatusSnapshot struct {
	Running                 bool      `json:"running"`
	LastDistilledAt         time.Time `json:"last_distilled_at,omitempty"`
	LastPushedAt            time.Time `json:"last_pushed_at,omitempty"`
	ConsecutivePushFailures int       `json:"consecutive_push_failures"`
}

type ArchiverHandler struct {
	store  ArchiverConfigStore
	runner ArchiverRunner
}

func NewArchiverHandler(store ArchiverConfigStore, runner ArchiverRunner) *ArchiverHandler {
	return &ArchiverHandler{store: store, runner: runner}
}

func (h *ArchiverHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/archiver/config":
		h.getConfig(w, r)
	case r.Method == http.MethodPut && r.URL.Path == "/api/archiver/config":
		h.putConfig(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/api/archiver/status":
		h.status(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/archiver/run":
		h.run(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *ArchiverHandler) getConfig(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(h.store.Get())
}

func (h *ArchiverHandler) putConfig(w http.ResponseWriter, r *http.Request) {
	var cfg map[string]any
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.store.Put(cfg); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *ArchiverHandler) status(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if h.runner == nil {
		_ = json.NewEncoder(w).Encode(ArchiverStatusSnapshot{Running: false})
		return
	}
	_ = json.NewEncoder(w).Encode(h.runner.Status())
}

func (h *ArchiverHandler) run(w http.ResponseWriter, r *http.Request) {
	if h.runner == nil {
		http.Error(w, "archiver not bound", http.StatusServiceUnavailable)
		return
	}
	if err := h.runner.RunOnce(r.Context()); err != nil {
		if errors.Is(err, archiver.ErrBusy) {
			http.Error(w, "busy", http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}
