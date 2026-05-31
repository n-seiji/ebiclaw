package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/n-seiji/ebiclaw/pkg/archiver"
	"github.com/n-seiji/ebiclaw/pkg/config"
	ppid "github.com/n-seiji/ebiclaw/pkg/pid"
	"github.com/n-seiji/ebiclaw/web/backend/launcherconfig"
)

func (h *Handler) registerArchiverRoutes(mux *http.ServeMux) {
	store := launcherconfig.NewArchiverStore(h.configPath)
	ah := NewArchiverHandler(store, nil, h)
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
	server *Handler
}

func NewArchiverHandler(store ArchiverConfigStore, runner ArchiverRunner, server *Handler) *ArchiverHandler {
	return &ArchiverHandler{store: store, runner: runner, server: server}
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

func (h *ArchiverHandler) status(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if h.runner == nil {
		if h.server != nil {
			if proxied, err := h.server.proxyGatewayArchiverStatus(r.Context()); err == nil {
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(proxied)
				return
			}
		}
		_ = json.NewEncoder(w).Encode(ArchiverStatusSnapshot{Running: false})
		return
	}
	_ = json.NewEncoder(w).Encode(h.runner.Status())
}

func (h *ArchiverHandler) run(w http.ResponseWriter, r *http.Request) {
	if h.runner == nil {
		if h.server != nil {
			statusCode, body, err := h.server.proxyGatewayArchiverRun(r.Context())
			if err == nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(statusCode)
				if len(body) > 0 {
					_, _ = w.Write(body)
				}
				return
			}
		}
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

func (h *Handler) proxyGatewayArchiverStatus(ctx context.Context) (ArchiverStatusSnapshot, error) {
	_, body, err := h.doGatewayArchiverRequest(ctx, http.MethodGet, "/archiver/status")
	if err != nil {
		return ArchiverStatusSnapshot{}, err
	}
	var status ArchiverStatusSnapshot
	if err := json.Unmarshal(body, &status); err != nil {
		return ArchiverStatusSnapshot{}, err
	}
	return status, nil
}

func (h *Handler) proxyGatewayArchiverRun(ctx context.Context) (int, []byte, error) {
	return h.doGatewayArchiverRequest(ctx, http.MethodPost, "/archiver/run")
}

func (h *Handler) doGatewayArchiverRequest(ctx context.Context, method, path string) (int, []byte, error) {
	cfg, err := config.LoadConfig(h.configPath)
	if err != nil {
		return 0, nil, err
	}

	gateway.mu.Lock()
	pidData := gateway.pidData
	gateway.mu.Unlock()
	if pidData == nil {
		pidData = gatewayPidDataByConfig(h.configPath)
		if pidData == nil {
			return 0, nil, fmt.Errorf("gateway pid data unavailable")
		}
	}

	host := gatewayProbeHost(stringsTrimSpaceOr(pidData.Host, h.effectiveGatewayBindHost(cfg)))
	if host == "" {
		host = "127.0.0.1"
	}
	port := pidData.Port
	if port == 0 {
		port = cfg.Gateway.Port
	}
	if port == 0 {
		port = 18790
	}

	url := "http://" + net.JoinHostPort(host, strconv.Itoa(port)) + path
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return 0, nil, err
	}
	if pidData.Token != "" {
		req.Header.Set("Authorization", "Bearer "+pidData.Token)
	}

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, body, nil
}

func gatewayPidDataByConfig(configPath string) *ppid.PidFileData {
	return ppid.ReadPidFileWithCheck(filepath.Dir(configPath))
}

func stringsTrimSpaceOr(v, fallback string) string {
	if s := strings.TrimSpace(v); s != "" {
		return s
	}
	return strings.TrimSpace(fallback)
}
