package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/n-seiji/ebiclaw/pkg/config"
)

func TestHandleGetChannelConfig_ReturnsSecretPresenceWithoutLeakingSecrets(t *testing.T) {
	configPath, cleanup := setupOAuthTestEnv(t)
	defer cleanup()

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.Channels.Slack.Enabled = true
	cfg.Channels.Slack.BotToken = *config.NewSecureString("xoxb-secret-from-security")
	cfg.Channels.Slack.AppToken = *config.NewSecureString("xapp-secret-from-security")
	if err := config.SaveConfig(configPath, cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	h := NewHandler(configPath)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/channels/slack/config", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf(
			"GET /api/channels/slack/config status = %d, want %d, body=%s",
			rec.Code,
			http.StatusOK,
			rec.Body.String(),
		)
	}
	if strings.Contains(rec.Body.String(), "xoxb-secret-from-security") ||
		strings.Contains(rec.Body.String(), "xapp-secret-from-security") {
		t.Fatalf("response leaked secret value: %s", rec.Body.String())
	}

	var resp struct {
		Config            map[string]any `json:"config"`
		ConfiguredSecrets []string       `json:"configured_secrets"`
		ConfigKey         string         `json:"config_key"`
		Variant           string         `json:"variant"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if got := resp.ConfigKey; got != "slack" {
		t.Fatalf("config_key = %q, want %q", got, "slack")
	}
	if enabled, ok := resp.Config["enabled"].(bool); !ok || !enabled {
		t.Fatalf("config.enabled = %#v, want true", resp.Config["enabled"])
	}
	if _, exists := resp.Config["bot_token"]; exists {
		t.Fatalf("config should omit bot_token, got %#v", resp.Config["bot_token"])
	}
	if _, exists := resp.Config["app_token"]; exists {
		t.Fatalf("config should omit app_token, got %#v", resp.Config["app_token"])
	}
	if len(resp.ConfiguredSecrets) != 2 ||
		resp.ConfiguredSecrets[0] != "bot_token" ||
		resp.ConfiguredSecrets[1] != "app_token" {
		t.Fatalf("configured_secrets = %#v, want [\"bot_token\", \"app_token\"]", resp.ConfiguredSecrets)
	}
}

func TestHandleGetChannelConfig_ReturnsNotFoundForUnknownChannel(t *testing.T) {
	configPath, cleanup := setupOAuthTestEnv(t)
	defer cleanup()

	h := NewHandler(configPath)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/channels/not-a-channel/config", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /api/channels/not-a-channel/config status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
