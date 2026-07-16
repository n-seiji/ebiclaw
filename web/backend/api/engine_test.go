package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/n-seiji/ebiclaw/pkg/config"
)

func resetEngineHooks(t *testing.T) {
	t.Helper()

	orig := lookPathFunc
	t.Cleanup(func() {
		lookPathFunc = orig
	})
}

func TestHandleGetEngine_PipeDisabledUsesDefaultModelReadiness(t *testing.T) {
	configPath, cleanup := setupOAuthTestEnv(t)
	defer cleanup()
	resetOAuthHooks(t)
	resetEngineHooks(t)

	lookPathFunc = func(string) (string, error) { return "/usr/bin/codex", nil }

	h := NewHandler(configPath)
	mux := http.NewServeMux()
	h.registerEngineRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/engine", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp engineResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if resp.Backend != "codex" {
		t.Errorf("Backend = %q; want %q", resp.Backend, "codex")
	}
	if resp.Enabled {
		t.Errorf("Enabled = true; want false")
	}
	if !resp.ChatReady {
		t.Errorf("ChatReady = false; want true (default model is configured)")
	}
	if len(resp.AvailableBackends) != 2 {
		t.Fatalf("AvailableBackends = %v; want 2 entries", resp.AvailableBackends)
	}
	if !resp.AvailableBackends[0].Available {
		t.Errorf("codex Available = false; want true")
	}
	if resp.AvailableBackends[1].Available {
		t.Errorf("claude-code Available = true; want false")
	}
}

func TestHandleGetEngine_PipeEnabledRequiresCodexAvailability(t *testing.T) {
	configPath, cleanup := setupOAuthTestEnv(t)
	defer cleanup()
	resetOAuthHooks(t)
	resetEngineHooks(t)

	lookPathFunc = func(string) (string, error) { return "", &fakeLookPathError{} }

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}
	cfg.CodexPipe.Enabled = true
	if err := config.SaveConfig(configPath, cfg); err != nil {
		t.Fatalf("SaveConfig error: %v", err)
	}

	h := NewHandler(configPath)
	mux := http.NewServeMux()
	h.registerEngineRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/engine", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var resp engineResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if resp.ChatReady {
		t.Errorf("ChatReady = true; want false (codex not on PATH)")
	}
}

type fakeLookPathError struct{}

func (e *fakeLookPathError) Error() string { return "not found" }

func TestHandleUpdateEngine_RejectsUnsupportedBackend(t *testing.T) {
	configPath, cleanup := setupOAuthTestEnv(t)
	defer cleanup()
	resetOAuthHooks(t)
	resetEngineHooks(t)

	h := NewHandler(configPath)
	mux := http.NewServeMux()
	h.registerEngineRoutes(mux)

	body, _ := json.Marshal(map[string]string{"backend": "claude-code"})
	req := httptest.NewRequest(http.MethodPut, "/api/engine", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d; want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleUpdateEngine_RejectsUnsupportedSandbox(t *testing.T) {
	configPath, cleanup := setupOAuthTestEnv(t)
	defer cleanup()
	resetOAuthHooks(t)
	resetEngineHooks(t)

	h := NewHandler(configPath)
	mux := http.NewServeMux()
	h.registerEngineRoutes(mux)

	body, _ := json.Marshal(map[string]string{"sandbox": "root-access"})
	req := httptest.NewRequest(http.MethodPut, "/api/engine", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d; want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleUpdateEngine_UpdatesConfig(t *testing.T) {
	configPath, cleanup := setupOAuthTestEnv(t)
	defer cleanup()
	resetOAuthHooks(t)
	resetEngineHooks(t)

	h := NewHandler(configPath)
	mux := http.NewServeMux()
	h.registerEngineRoutes(mux)

	body, _ := json.Marshal(map[string]any{
		"backend":   "codex",
		"model":     "gpt-5",
		"workspace": "/tmp/work",
		"sandbox":   "read-only",
		"enabled":   true,
	})
	req := httptest.NewRequest(http.MethodPut, "/api/engine", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}

	if cfg.CodexPipe.GetBackend() != "codex" {
		t.Errorf("Backend = %q; want %q", cfg.CodexPipe.GetBackend(), "codex")
	}
	if cfg.CodexPipe.Model != "gpt-5" {
		t.Errorf("Model = %q; want %q", cfg.CodexPipe.Model, "gpt-5")
	}
	if cfg.CodexPipe.Workspace != "/tmp/work" {
		t.Errorf("Workspace = %q; want %q", cfg.CodexPipe.Workspace, "/tmp/work")
	}
	if cfg.CodexPipe.Sandbox != "read-only" {
		t.Errorf("Sandbox = %q; want %q", cfg.CodexPipe.Sandbox, "read-only")
	}
	if !cfg.CodexPipe.Enabled {
		t.Errorf("Enabled = false; want true")
	}
}
