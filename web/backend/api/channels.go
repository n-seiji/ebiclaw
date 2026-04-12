package api

import (
	"encoding/json"
	"net/http"

	"github.com/sipeed/picoclaw/pkg/config"
)

type channelCatalogItem struct {
	Name      string `json:"name"`
	ConfigKey string `json:"config_key"`
	Variant   string `json:"variant,omitempty"`
}

var channelCatalog = []channelCatalogItem{
	{Name: "discord", ConfigKey: "discord"},
	{Name: "slack", ConfigKey: "slack"},
	{Name: "pico", ConfigKey: "pico"},
}

type channelConfigResponse struct {
	Config            any      `json:"config"`
	ConfiguredSecrets []string `json:"configured_secrets"`
	ConfigKey         string   `json:"config_key"`
	Variant           string   `json:"variant,omitempty"`
}

type channelSecretPresence struct {
	key        string
	configured bool
}

// registerChannelRoutes binds read-only channel catalog endpoints to the ServeMux.
func (h *Handler) registerChannelRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/channels/catalog", h.handleListChannelCatalog)
	mux.HandleFunc("GET /api/channels/{name}/config", h.handleGetChannelConfig)
}

// handleListChannelCatalog returns the channels supported by backend.
//
//	GET /api/channels/catalog
func (h *Handler) handleListChannelCatalog(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"channels": channelCatalog,
	})
}

// handleGetChannelConfig returns safe channel config plus secret presence metadata.
//
//	GET /api/channels/{name}/config
func (h *Handler) handleGetChannelConfig(w http.ResponseWriter, r *http.Request) {
	channelName := r.PathValue("name")
	item, ok := findChannelCatalogItem(channelName)
	if !ok {
		http.Error(w, "Channel not found", http.StatusNotFound)
		return
	}

	cfg, err := config.LoadConfig(h.configPath)
	if err != nil {
		http.Error(w, "Failed to load config", http.StatusInternalServerError)
		return
	}

	resp := buildChannelConfigResponse(cfg, item)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

func findChannelCatalogItem(name string) (channelCatalogItem, bool) {
	for _, item := range channelCatalog {
		if item.Name == name {
			return item, true
		}
	}
	return channelCatalogItem{}, false
}

func buildChannelConfigResponse(cfg *config.Config, item channelCatalogItem) channelConfigResponse {
	resp := channelConfigResponse{
		ConfiguredSecrets: []string{},
		ConfigKey:         item.ConfigKey,
		Variant:           item.Variant,
	}

	switch item.Name {
	case "discord":
		channelCfg := cfg.Channels.Discord
		resp.ConfiguredSecrets = collectConfiguredSecrets(
			channelSecretPresence{key: "token", configured: channelCfg.Token.String() != ""},
		)
		channelCfg.Token = config.SecureString{}
		resp.Config = channelCfg
	case "slack":
		channelCfg := cfg.Channels.Slack
		resp.ConfiguredSecrets = collectConfiguredSecrets(
			channelSecretPresence{key: "bot_token", configured: channelCfg.BotToken.String() != ""},
			channelSecretPresence{key: "app_token", configured: channelCfg.AppToken.String() != ""},
		)
		channelCfg.BotToken = config.SecureString{}
		channelCfg.AppToken = config.SecureString{}
		resp.Config = channelCfg
	case "pico":
		channelCfg := cfg.Channels.Pico
		resp.ConfiguredSecrets = collectConfiguredSecrets(
			channelSecretPresence{key: "token", configured: channelCfg.Token.String() != ""},
		)
		channelCfg.Token = config.SecureString{}
		resp.Config = channelCfg
	default:
		resp.Config = map[string]any{}
	}

	return resp
}

func collectConfiguredSecrets(secrets ...channelSecretPresence) []string {
	configured := make([]string, 0, len(secrets))
	for _, secret := range secrets {
		if secret.configured {
			configured = append(configured, secret.key)
		}
	}
	return configured
}
