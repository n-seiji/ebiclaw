package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"

	"github.com/n-seiji/ebiclaw/pkg/config"
)

// registerEngineRoutes binds CLI engine selection endpoints to the ServeMux.
func (h *Handler) registerEngineRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/engine", h.handleGetEngine)
	mux.HandleFunc("PUT /api/engine", h.handleUpdateEngine)
}

// engineBackend describes a CLI engine backend and its current availability.
type engineBackend struct {
	ID        string `json:"id"`
	Available bool   `json:"available"`
}

// engineResponse is the JSON structure returned for the current engine settings.
type engineResponse struct {
	Backend           string          `json:"backend"`
	Model             string          `json:"model"`
	Workspace         string          `json:"workspace"`
	Sandbox           string          `json:"sandbox"`
	AvailableBackends []engineBackend `json:"available_backends"`
	ChatReady         bool            `json:"chat_ready"`
}

// lookPathFunc is overridable in tests.
var lookPathFunc = exec.LookPath

// handleGetEngine returns the current CLI engine configuration and whether
// chat is ready to accept messages.
//
//	GET /api/engine
func (h *Handler) handleGetEngine(w http.ResponseWriter, r *http.Request) {
	cfg, err := config.LoadConfig(h.configPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to load config: %v", err), http.StatusInternalServerError)
		return
	}

	codexAvailable := isCodexAvailable(cfg)

	resp := engineResponse{
		Backend:   cfg.CodexPipe.GetBackend(),
		Model:     cfg.CodexPipe.Model,
		Workspace: cfg.CodexPipe.Workspace,
		Sandbox:   cfg.CodexPipe.GetSandbox(),
		AvailableBackends: []engineBackend{
			{ID: "codex", Available: codexAvailable},
			{ID: "claude-code", Available: false},
		},
		ChatReady: isChatReady(cfg, codexAvailable),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// isCodexAvailable reports whether the configured codex binary is on PATH.
func isCodexAvailable(cfg *config.Config) bool {
	_, err := lookPathFunc(cfg.CodexPipe.GetCommand())
	return err == nil
}

// isChatReady determines whether chat can accept a message given the
// current configuration: the codex pipe is the only message path, so the
// selected backend must be available.
func isChatReady(cfg *config.Config, codexAvailable bool) bool {
	switch cfg.CodexPipe.GetBackend() {
	case "codex":
		return codexAvailable
	default:
		return false
	}
}

// engineUpdateRequest is the payload accepted by PUT /api/engine. All fields
// are optional; omitted fields leave the existing value unchanged.
type engineUpdateRequest struct {
	Backend   *string `json:"backend,omitempty"`
	Model     *string `json:"model,omitempty"`
	Workspace *string `json:"workspace,omitempty"`
	Sandbox   *string `json:"sandbox,omitempty"`
}

var validSandboxModes = map[string]bool{
	"read-only":          true,
	"workspace-write":    true,
	"danger-full-access": true,
}

// handleUpdateEngine updates the codex_pipe engine configuration.
//
//	PUT /api/engine
func (h *Handler) handleUpdateEngine(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req engineUpdateRequest
	if err = json.Unmarshal(body, &req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	if req.Backend != nil && *req.Backend != "codex" {
		http.Error(w, fmt.Sprintf("Unsupported backend %q", *req.Backend), http.StatusBadRequest)
		return
	}
	if req.Sandbox != nil && !validSandboxModes[*req.Sandbox] {
		http.Error(w, fmt.Sprintf("Unsupported sandbox mode %q", *req.Sandbox), http.StatusBadRequest)
		return
	}

	cfg, err := config.LoadConfig(h.configPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to load config: %v", err), http.StatusInternalServerError)
		return
	}

	if req.Backend != nil {
		cfg.CodexPipe.Backend = *req.Backend
	}
	if req.Model != nil {
		cfg.CodexPipe.Model = *req.Model
	}
	if req.Workspace != nil {
		cfg.CodexPipe.Workspace = *req.Workspace
	}
	if req.Sandbox != nil {
		cfg.CodexPipe.Sandbox = *req.Sandbox
	}

	if err := config.SaveConfig(h.configPath, cfg); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save config: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
