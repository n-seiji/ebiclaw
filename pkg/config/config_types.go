package config

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"

	"github.com/n-seiji/ebiclaw/pkg/archiver"
)

// Config is the current config structure with version support.
type Config struct {
	Version   int             `json:"version"             yaml:"-"` // Config schema version for migration
	Isolation IsolationConfig `json:"isolation,omitempty" yaml:"-"`
	Agents    AgentsConfig    `json:"agents"              yaml:"-"`
	Bindings  []AgentBinding  `json:"bindings,omitempty"  yaml:"-"`
	Session   SessionConfig   `json:"session,omitempty"   yaml:"-"`
	Channels  ChannelsConfig  `json:"channels"            yaml:"channels"`
	ModelList SecureModelList `json:"model_list"          yaml:"model_list"` // New model-centric provider configuration
	Gateway   GatewayConfig   `json:"gateway"             yaml:"-"`
	Hooks     HooksConfig     `json:"hooks,omitempty"     yaml:"-"`
	Tools     ToolsConfig     `json:"tools"               yaml:",inline"`
	Heartbeat HeartbeatConfig `json:"heartbeat"           yaml:"-"`
	Devices   DevicesConfig   `json:"devices"             yaml:"-"`
	Voice     VoiceConfig     `json:"voice"               yaml:"-"`
	Archiver  archiver.Config `json:"archiver,omitempty" yaml:"-"`
	// BuildInfo contains build-time version information
	BuildInfo BuildInfo `json:"build_info,omitempty" yaml:"-"`

	// cache for sensitive values and compiled regex (computed once)
	sensitiveCache *SensitiveDataCache
}

// IsolationConfig controls subprocess isolation for commands started by EbiClaw.
// It is applied by the isolation package rather than by sandboxing the main process.
type IsolationConfig struct {
	Enabled     bool         `json:"enabled,omitempty"`
	ExposePaths []ExposePath `json:"expose_paths,omitempty"`
}

// ExposePath describes a host path that should remain visible inside the isolated
// child-process environment. This is currently implemented on Linux only.
type ExposePath struct {
	Source string `json:"source"`
	Target string `json:"target,omitempty"`
	Mode   string `json:"mode"`
}

// FilterSensitiveData filters sensitive values from content before sending to LLM.
// This prevents the LLM from seeing its own credentials.
// Uses strings.Replacer for O(n+m) performance (computed once per SecurityConfig).
// Short content (below FilterMinLength) is returned unchanged for performance.
func (c *Config) FilterSensitiveData(content string) string {
	// Check if filtering is enabled (default: true)
	if !c.Tools.IsFilterSensitiveDataEnabled() {
		return content
	}
	// Fast path: skip filtering for short content
	if len(content) < c.Tools.GetFilterMinLength() {
		return content
	}
	return c.SensitiveDataReplacer().Replace(content)
}

type HooksConfig struct {
	Enabled   bool                         `json:"enabled"`
	Defaults  HookDefaultsConfig           `json:"defaults,omitempty"`
	Builtins  map[string]BuiltinHookConfig `json:"builtins,omitempty"`
	Processes map[string]ProcessHookConfig `json:"processes,omitempty"`
}

type HookDefaultsConfig struct {
	ObserverTimeoutMS    int `json:"observer_timeout_ms,omitempty"`
	InterceptorTimeoutMS int `json:"interceptor_timeout_ms,omitempty"`
	ApprovalTimeoutMS    int `json:"approval_timeout_ms,omitempty"`
}

type BuiltinHookConfig struct {
	Enabled  bool            `json:"enabled"`
	Priority int             `json:"priority,omitempty"`
	Config   json.RawMessage `json:"config,omitempty"`
}

type ProcessHookConfig struct {
	Enabled   bool              `json:"enabled"`
	Priority  int               `json:"priority,omitempty"`
	Transport string            `json:"transport,omitempty"`
	Command   []string          `json:"command,omitempty"`
	Dir       string            `json:"dir,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Observe   []string          `json:"observe,omitempty"`
	Intercept []string          `json:"intercept,omitempty"`
}

// BuildInfo contains build-time version information
type BuildInfo struct {
	Version   string `json:"version"`
	GitCommit string `json:"git_commit"`
	BuildTime string `json:"build_time"`
	GoVersion string `json:"go_version"`
}

// MarshalJSON implements custom JSON marshaling for Config
// to omit providers section when empty and session when empty
func (c *Config) MarshalJSON() ([]byte, error) {
	type Alias Config
	aux := &struct {
		Session *SessionConfig `json:"session,omitempty"`
		*Alias
	}{
		Alias: (*Alias)(c),
	}

	// Only include session if not empty
	if c.Session.DMScope != "" || len(c.Session.IdentityLinks) > 0 {
		aux.Session = &c.Session
	}

	return json.Marshal(aux)
}

type AgentsConfig struct {
	Defaults AgentDefaults `json:"defaults"`
	List     []AgentConfig `json:"list,omitempty"`
}

// AgentModelConfig supports both string and structured model config.
// String format: "gpt-4" (just primary, no fallbacks)
// Object format: {"primary": "gpt-4", "fallbacks": ["claude-haiku"]}
type AgentModelConfig struct {
	Primary   string   `json:"primary,omitempty"`
	Fallbacks []string `json:"fallbacks,omitempty"`
}

func (m *AgentModelConfig) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		m.Primary = s
		m.Fallbacks = nil
		return nil
	}
	type raw struct {
		Primary   string   `json:"primary"`
		Fallbacks []string `json:"fallbacks"`
	}
	var r raw
	if err := json.Unmarshal(data, &r); err != nil {
		return err
	}
	m.Primary = r.Primary
	m.Fallbacks = r.Fallbacks
	return nil
}

func (m AgentModelConfig) MarshalJSON() ([]byte, error) {
	if len(m.Fallbacks) == 0 && m.Primary != "" {
		return json.Marshal(m.Primary)
	}
	type raw struct {
		Primary   string   `json:"primary,omitempty"`
		Fallbacks []string `json:"fallbacks,omitempty"`
	}
	return json.Marshal(raw{Primary: m.Primary, Fallbacks: m.Fallbacks})
}

type AgentConfig struct {
	ID        string            `json:"id"`
	Default   bool              `json:"default,omitempty"`
	Name      string            `json:"name,omitempty"`
	Workspace string            `json:"workspace,omitempty"`
	Model     *AgentModelConfig `json:"model,omitempty"`
	Skills    []string          `json:"skills,omitempty"`
	Subagents *SubagentsConfig  `json:"subagents,omitempty"`
}

type SubagentsConfig struct {
	AllowAgents []string          `json:"allow_agents,omitempty"`
	Model       *AgentModelConfig `json:"model,omitempty"`
}

type PeerMatch struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type BindingMatch struct {
	Channel   string     `json:"channel"`
	AccountID string     `json:"account_id,omitempty"`
	Peer      *PeerMatch `json:"peer,omitempty"`
	GuildID   string     `json:"guild_id,omitempty"`
	TeamID    string     `json:"team_id,omitempty"`
}

type AgentBinding struct {
	AgentID string       `json:"agent_id"`
	Match   BindingMatch `json:"match"`
}

type SessionConfig struct {
	DMScope       string              `json:"dm_scope,omitempty"`
	IdentityLinks map[string][]string `json:"identity_links,omitempty"`
}

// RoutingConfig controls the intelligent model routing feature.
// When enabled, each incoming message is scored against structural features
// (message length, code blocks, tool call history, conversation depth, attachments).
// Messages scoring below Threshold are sent to LightModel; all others use the
// agent's primary model. This reduces cost and latency for simple tasks without
// requiring any keyword matching — all scoring is language-agnostic.
type RoutingConfig struct {
	Enabled    bool    `json:"enabled"`
	LightModel string  `json:"light_model"` // model_name from model_list to use for simple tasks
	Threshold  float64 `json:"threshold"`   // complexity score in [0,1]; score >= threshold → primary model
}

// SubTurnConfig configures the SubTurn execution system.
type SubTurnConfig struct {
	MaxDepth              int `json:"max_depth"               env:"EBICLAW_AGENTS_DEFAULTS_SUBTURN_MAX_DEPTH"`
	MaxConcurrent         int `json:"max_concurrent"          env:"EBICLAW_AGENTS_DEFAULTS_SUBTURN_MAX_CONCURRENT"`
	DefaultTimeoutMinutes int `json:"default_timeout_minutes" env:"EBICLAW_AGENTS_DEFAULTS_SUBTURN_DEFAULT_TIMEOUT_MINUTES"`
	DefaultTokenBudget    int `json:"default_token_budget"    env:"EBICLAW_AGENTS_DEFAULTS_SUBTURN_DEFAULT_TOKEN_BUDGET"`
	ConcurrencyTimeoutSec int `json:"concurrency_timeout_sec" env:"EBICLAW_AGENTS_DEFAULTS_SUBTURN_CONCURRENCY_TIMEOUT_SEC"`
}

type ToolFeedbackConfig struct {
	Enabled       bool `json:"enabled"         env:"EBICLAW_AGENTS_DEFAULTS_TOOL_FEEDBACK_ENABLED"`
	MaxArgsLength int  `json:"max_args_length" env:"EBICLAW_AGENTS_DEFAULTS_TOOL_FEEDBACK_MAX_ARGS_LENGTH"`
}

type AgentDefaults struct {
	Workspace                 string             `json:"workspace"                        env:"EBICLAW_AGENTS_DEFAULTS_WORKSPACE"`
	RestrictToWorkspace       bool               `json:"restrict_to_workspace"            env:"EBICLAW_AGENTS_DEFAULTS_RESTRICT_TO_WORKSPACE"`
	AllowReadOutsideWorkspace bool               `json:"allow_read_outside_workspace"     env:"EBICLAW_AGENTS_DEFAULTS_ALLOW_READ_OUTSIDE_WORKSPACE"`
	Provider                  string             `json:"provider"                         env:"EBICLAW_AGENTS_DEFAULTS_PROVIDER"`
	ModelName                 string             `json:"model_name"                       env:"EBICLAW_AGENTS_DEFAULTS_MODEL_NAME"`
	ModelFallbacks            []string           `json:"model_fallbacks,omitempty"`
	ImageModel                string             `json:"image_model,omitempty"            env:"EBICLAW_AGENTS_DEFAULTS_IMAGE_MODEL"`
	ImageModelFallbacks       []string           `json:"image_model_fallbacks,omitempty"`
	MaxTokens                 int                `json:"max_tokens"                       env:"EBICLAW_AGENTS_DEFAULTS_MAX_TOKENS"`
	ContextWindow             int                `json:"context_window,omitempty"         env:"EBICLAW_AGENTS_DEFAULTS_CONTEXT_WINDOW"`
	Temperature               *float64           `json:"temperature,omitempty"            env:"EBICLAW_AGENTS_DEFAULTS_TEMPERATURE"`
	MaxToolIterations         int                `json:"max_tool_iterations"              env:"EBICLAW_AGENTS_DEFAULTS_MAX_TOOL_ITERATIONS"`
	SummarizeMessageThreshold int                `json:"summarize_message_threshold"      env:"EBICLAW_AGENTS_DEFAULTS_SUMMARIZE_MESSAGE_THRESHOLD"`
	SummarizeTokenPercent     int                `json:"summarize_token_percent"          env:"EBICLAW_AGENTS_DEFAULTS_SUMMARIZE_TOKEN_PERCENT"`
	MaxMediaSize              int                `json:"max_media_size,omitempty"         env:"EBICLAW_AGENTS_DEFAULTS_MAX_MEDIA_SIZE"`
	Routing                   *RoutingConfig     `json:"routing,omitempty"`
	SteeringMode              string             `json:"steering_mode,omitempty"          env:"EBICLAW_AGENTS_DEFAULTS_STEERING_MODE"` // "one-at-a-time" (default) or "all"
	SubTurn                   SubTurnConfig      `json:"subturn"                                                                                      envPrefix:"EBICLAW_AGENTS_DEFAULTS_SUBTURN_"`
	ToolFeedback              ToolFeedbackConfig `json:"tool_feedback,omitempty"`
	SplitOnMarker             bool               `json:"split_on_marker"                  env:"EBICLAW_AGENTS_DEFAULTS_SPLIT_ON_MARKER"` // split messages on <|[SPLIT]|> marker
	ContextManager            string             `json:"context_manager,omitempty"        env:"EBICLAW_AGENTS_DEFAULTS_CONTEXT_MANAGER"`
	ContextManagerConfig      json.RawMessage    `json:"context_manager_config,omitempty" env:"EBICLAW_AGENTS_DEFAULTS_CONTEXT_MANAGER_CONFIG"`
}

const DefaultMaxMediaSize = 20 * 1024 * 1024 // 20 MB

func (d *AgentDefaults) GetMaxMediaSize() int {
	if d.MaxMediaSize > 0 {
		return d.MaxMediaSize
	}
	return DefaultMaxMediaSize
}

// GetToolFeedbackMaxArgsLength returns the max args preview length for tool feedback messages.
func (d *AgentDefaults) GetToolFeedbackMaxArgsLength() int {
	if d.ToolFeedback.MaxArgsLength > 0 {
		return d.ToolFeedback.MaxArgsLength
	}
	return 300
}

// IsToolFeedbackEnabled returns true when tool feedback messages should be sent to the chat.
func (d *AgentDefaults) IsToolFeedbackEnabled() bool {
	return d.ToolFeedback.Enabled
}

// GetModelName returns the effective model name for the agent defaults.
// It prefers the new "model_name" field but falls back to "model" for backward compatibility.
func (d *AgentDefaults) GetModelName() string {
	return d.ModelName
}

type ChannelsConfig struct {
	WhatsApp     WhatsAppConfig     `json:"whatsapp"      yaml:"-"`
	Telegram     TelegramConfig     `json:"telegram"      yaml:"telegram,omitempty"`
	Feishu       FeishuConfig       `json:"feishu"        yaml:"feishu,omitempty"`
	Discord      DiscordConfig      `json:"discord"       yaml:"discord,omitempty"`
	MaixCam      MaixCamConfig      `json:"maixcam"       yaml:"-"`
	QQ           QQConfig           `json:"qq"            yaml:"qq,omitempty"`
	DingTalk     DingTalkConfig     `json:"dingtalk"      yaml:"dingtalk,omitempty"`
	Slack        SlackConfig        `json:"slack"         yaml:"slack,omitempty"`
	Matrix       MatrixConfig       `json:"matrix"        yaml:"matrix,omitempty"`
	LINE         LINEConfig         `json:"line"          yaml:"line,omitempty"`
	OneBot       OneBotConfig       `json:"onebot"        yaml:"onebot,omitempty"`
	WeCom        WeComConfig        `json:"wecom"         yaml:"wecom,omitempty"         envPrefix:"EBICLAW_CHANNELS_WECOM_"`
	Weixin       WeixinConfig       `json:"weixin"        yaml:"weixin,omitempty"`
	Pico         PicoConfig         `json:"pico"          yaml:"pico,omitempty"`
	PicoClient   PicoClientConfig   `json:"pico_client"   yaml:"pico_client,omitempty"`
	IRC          IRCConfig          `json:"irc"           yaml:"irc,omitempty"`
	VK           VKConfig           `json:"vk"            yaml:"vk,omitempty"`
	TeamsWebhook TeamsWebhookConfig `json:"teams_webhook" yaml:"teams_webhook,omitempty"`
}

// GroupTriggerConfig controls when the bot responds in group chats.
type GroupTriggerConfig struct {
	MentionOnly bool     `json:"mention_only,omitempty"`
	Prefixes    []string `json:"prefixes,omitempty"`
}

// TypingConfig controls typing indicator behavior (Phase 10).
type TypingConfig struct {
	Enabled bool `json:"enabled,omitempty"`
}

// PlaceholderConfig controls placeholder message behavior (Phase 10).
type PlaceholderConfig struct {
	Enabled bool                `json:"enabled"`
	Text    FlexibleStringSlice `json:"text,omitempty"`
}

// GetRandomText returns a random placeholder text, or default if none set.
func (p *PlaceholderConfig) GetRandomText() string {
	if len(p.Text) == 0 {
		return "Thinking..."
	}
	if len(p.Text) == 1 {
		return p.Text[0]
	}
	idx := rand.Intn(len(p.Text))
	return p.Text[idx]
}

type StreamingConfig struct {
	Enabled         bool `json:"enabled,omitempty"          env:"EBICLAW_CHANNELS_TELEGRAM_STREAMING_ENABLED"`
	ThrottleSeconds int  `json:"throttle_seconds,omitempty" env:"EBICLAW_CHANNELS_TELEGRAM_STREAMING_THROTTLE_SECONDS"`
	MinGrowthChars  int  `json:"min_growth_chars,omitempty" env:"EBICLAW_CHANNELS_TELEGRAM_STREAMING_MIN_GROWTH_CHARS"`
}

type WhatsAppConfig struct {
	Enabled            bool                `json:"enabled"              yaml:"-" env:"EBICLAW_CHANNELS_WHATSAPP_ENABLED"`
	BridgeURL          string              `json:"bridge_url"           yaml:"-" env:"EBICLAW_CHANNELS_WHATSAPP_BRIDGE_URL"`
	UseNative          bool                `json:"use_native"           yaml:"-" env:"EBICLAW_CHANNELS_WHATSAPP_USE_NATIVE"`
	SessionStorePath   string              `json:"session_store_path"   yaml:"-" env:"EBICLAW_CHANNELS_WHATSAPP_SESSION_STORE_PATH"`
	AllowFrom          FlexibleStringSlice `json:"allow_from"           yaml:"-" env:"EBICLAW_CHANNELS_WHATSAPP_ALLOW_FROM"`
	ReasoningChannelID string              `json:"reasoning_channel_id" yaml:"-" env:"EBICLAW_CHANNELS_WHATSAPP_REASONING_CHANNEL_ID"`
}

type TelegramConfig struct {
	Enabled            bool                `json:"enabled"                 yaml:"-"               env:"EBICLAW_CHANNELS_TELEGRAM_ENABLED"`
	Token              SecureString        `json:"token,omitzero"          yaml:"token,omitempty" env:"EBICLAW_CHANNELS_TELEGRAM_TOKEN"`
	BaseURL            string              `json:"base_url"                yaml:"-"               env:"EBICLAW_CHANNELS_TELEGRAM_BASE_URL"`
	Proxy              string              `json:"proxy"                   yaml:"-"               env:"EBICLAW_CHANNELS_TELEGRAM_PROXY"`
	AllowFrom          FlexibleStringSlice `json:"allow_from"              yaml:"-"               env:"EBICLAW_CHANNELS_TELEGRAM_ALLOW_FROM"`
	GroupTrigger       GroupTriggerConfig  `json:"group_trigger,omitempty" yaml:"-"`
	Typing             TypingConfig        `json:"typing,omitempty"        yaml:"-"`
	Placeholder        PlaceholderConfig   `json:"placeholder,omitempty"   yaml:"-"`
	Streaming          StreamingConfig     `json:"streaming,omitempty"     yaml:"-"`
	ReasoningChannelID string              `json:"reasoning_channel_id"    yaml:"-"               env:"EBICLAW_CHANNELS_TELEGRAM_REASONING_CHANNEL_ID"`
	UseMarkdownV2      bool                `json:"use_markdown_v2"         yaml:"-"               env:"EBICLAW_CHANNELS_TELEGRAM_USE_MARKDOWN_V2"`
}

func (c *TelegramConfig) SetToken(token string) {
	c.Token = *NewSecureString(token)
}

type FeishuConfig struct {
	Enabled             bool                `json:"enabled"                     yaml:"-"                            env:"EBICLAW_CHANNELS_FEISHU_ENABLED"`
	AppID               string              `json:"app_id"                      yaml:"-"                            env:"EBICLAW_CHANNELS_FEISHU_APP_ID"`
	AppSecret           SecureString        `json:"app_secret,omitzero"         yaml:"app_secret,omitempty"         env:"EBICLAW_CHANNELS_FEISHU_APP_SECRET"`
	EncryptKey          SecureString        `json:"encrypt_key,omitzero"        yaml:"encrypt_key,omitempty"        env:"EBICLAW_CHANNELS_FEISHU_ENCRYPT_KEY"`
	VerificationToken   SecureString        `json:"verification_token,omitzero" yaml:"verification_token,omitempty" env:"EBICLAW_CHANNELS_FEISHU_VERIFICATION_TOKEN"`
	AllowFrom           FlexibleStringSlice `json:"allow_from"                  yaml:"-"                            env:"EBICLAW_CHANNELS_FEISHU_ALLOW_FROM"`
	GroupTrigger        GroupTriggerConfig  `json:"group_trigger,omitempty"     yaml:"-"`
	Placeholder         PlaceholderConfig   `json:"placeholder,omitempty"       yaml:"-"`
	ReasoningChannelID  string              `json:"reasoning_channel_id"        yaml:"-"                            env:"EBICLAW_CHANNELS_FEISHU_REASONING_CHANNEL_ID"`
	RandomReactionEmoji FlexibleStringSlice `json:"random_reaction_emoji"       yaml:"-"                            env:"EBICLAW_CHANNELS_FEISHU_RANDOM_REACTION_EMOJI"`
	IsLark              bool                `json:"is_lark"                     yaml:"-"                            env:"EBICLAW_CHANNELS_FEISHU_IS_LARK"`
}

type DiscordConfig struct {
	Enabled            bool                `json:"enabled"                 yaml:"-"               env:"EBICLAW_CHANNELS_DISCORD_ENABLED"`
	Token              SecureString        `json:"token,omitzero"          yaml:"token,omitempty" env:"EBICLAW_CHANNELS_DISCORD_TOKEN"`
	Proxy              string              `json:"proxy"                   yaml:"-"               env:"EBICLAW_CHANNELS_DISCORD_PROXY"`
	AllowFrom          FlexibleStringSlice `json:"allow_from"              yaml:"-"               env:"EBICLAW_CHANNELS_DISCORD_ALLOW_FROM"`
	MentionOnly        bool                `json:"mention_only"            yaml:"-"               env:"EBICLAW_CHANNELS_DISCORD_MENTION_ONLY"`
	GroupTrigger       GroupTriggerConfig  `json:"group_trigger,omitempty" yaml:"-"`
	Typing             TypingConfig        `json:"typing,omitempty"        yaml:"-"`
	Placeholder        PlaceholderConfig   `json:"placeholder,omitempty"   yaml:"-"`
	ReasoningChannelID string              `json:"reasoning_channel_id"    yaml:"-"               env:"EBICLAW_CHANNELS_DISCORD_REASONING_CHANNEL_ID"`
}

type MaixCamConfig struct {
	Enabled            bool                `json:"enabled"              env:"EBICLAW_CHANNELS_MAIXCAM_ENABLED"`
	Host               string              `json:"host"                 env:"EBICLAW_CHANNELS_MAIXCAM_HOST"`
	Port               int                 `json:"port"                 env:"EBICLAW_CHANNELS_MAIXCAM_PORT"`
	AllowFrom          FlexibleStringSlice `json:"allow_from"           env:"EBICLAW_CHANNELS_MAIXCAM_ALLOW_FROM"`
	ReasoningChannelID string              `json:"reasoning_channel_id" env:"EBICLAW_CHANNELS_MAIXCAM_REASONING_CHANNEL_ID"`
}

type QQConfig struct {
	Enabled              bool                `json:"enabled"                  yaml:"-"                    env:"EBICLAW_CHANNELS_QQ_ENABLED"`
	AppID                string              `json:"app_id"                   yaml:"-"                    env:"EBICLAW_CHANNELS_QQ_APP_ID"`
	AppSecret            SecureString        `json:"app_secret,omitzero"      yaml:"app_secret,omitempty" env:"EBICLAW_CHANNELS_QQ_APP_SECRET"`
	AllowFrom            FlexibleStringSlice `json:"allow_from"               yaml:"-"                    env:"EBICLAW_CHANNELS_QQ_ALLOW_FROM"`
	GroupTrigger         GroupTriggerConfig  `json:"group_trigger,omitempty"  yaml:"-"`
	MaxMessageLength     int                 `json:"max_message_length"       yaml:"-"                    env:"EBICLAW_CHANNELS_QQ_MAX_MESSAGE_LENGTH"`
	MaxBase64FileSizeMiB int64               `json:"max_base64_file_size_mib" yaml:"-"                    env:"EBICLAW_CHANNELS_QQ_MAX_BASE64_FILE_SIZE_MIB"`
	SendMarkdown         bool                `json:"send_markdown"            yaml:"-"                    env:"EBICLAW_CHANNELS_QQ_SEND_MARKDOWN"`
	ReasoningChannelID   string              `json:"reasoning_channel_id"     yaml:"-"                    env:"EBICLAW_CHANNELS_QQ_REASONING_CHANNEL_ID"`
}

type DingTalkConfig struct {
	Enabled            bool                `json:"enabled"                 yaml:"-"                       env:"EBICLAW_CHANNELS_DINGTALK_ENABLED"`
	ClientID           string              `json:"client_id"               yaml:"-"                       env:"EBICLAW_CHANNELS_DINGTALK_CLIENT_ID"`
	ClientSecret       SecureString        `json:"client_secret,omitzero"  yaml:"client_secret,omitempty" env:"EBICLAW_CHANNELS_DINGTALK_CLIENT_SECRET"`
	AllowFrom          FlexibleStringSlice `json:"allow_from"              yaml:"-"                       env:"EBICLAW_CHANNELS_DINGTALK_ALLOW_FROM"`
	GroupTrigger       GroupTriggerConfig  `json:"group_trigger,omitempty" yaml:"-"`
	ReasoningChannelID string              `json:"reasoning_channel_id"    yaml:"-"                       env:"EBICLAW_CHANNELS_DINGTALK_REASONING_CHANNEL_ID"`
}

type SlackConfig struct {
	Enabled            bool                `json:"enabled"                 yaml:"-"                   env:"EBICLAW_CHANNELS_SLACK_ENABLED"`
	BotToken           SecureString        `json:"bot_token,omitzero"      yaml:"bot_token,omitempty" env:"EBICLAW_CHANNELS_SLACK_BOT_TOKEN"`
	AppToken           SecureString        `json:"app_token,omitzero"      yaml:"app_token,omitempty" env:"EBICLAW_CHANNELS_SLACK_APP_TOKEN"`
	AllowFrom          FlexibleStringSlice `json:"allow_from"              yaml:"-"                   env:"EBICLAW_CHANNELS_SLACK_ALLOW_FROM"`
	GroupTrigger       GroupTriggerConfig  `json:"group_trigger,omitempty" yaml:"-"`
	Typing             TypingConfig        `json:"typing,omitempty"        yaml:"-"`
	Placeholder        PlaceholderConfig   `json:"placeholder,omitempty"   yaml:"-"`
	ReasoningChannelID string              `json:"reasoning_channel_id"    yaml:"-"                   env:"EBICLAW_CHANNELS_SLACK_REASONING_CHANNEL_ID"`
}

type MatrixConfig struct {
	Enabled            bool                `json:"enabled"                        yaml:"-"                      env:"EBICLAW_CHANNELS_MATRIX_ENABLED"`
	Homeserver         string              `json:"homeserver"                     yaml:"-"                      env:"EBICLAW_CHANNELS_MATRIX_HOMESERVER"`
	UserID             string              `json:"user_id"                        yaml:"-"                      env:"EBICLAW_CHANNELS_MATRIX_USER_ID"`
	AccessToken        SecureString        `json:"access_token,omitzero"          yaml:"access_token,omitempty" env:"EBICLAW_CHANNELS_MATRIX_ACCESS_TOKEN"`
	DeviceID           string              `json:"device_id,omitempty"            yaml:"-"`
	JoinOnInvite       bool                `json:"join_on_invite"                 yaml:"-"`
	MessageFormat      string              `json:"message_format,omitempty"       yaml:"-"`
	AllowFrom          FlexibleStringSlice `json:"allow_from"                     yaml:"-"`
	GroupTrigger       GroupTriggerConfig  `json:"group_trigger,omitempty"        yaml:"-"`
	Placeholder        PlaceholderConfig   `json:"placeholder,omitempty"          yaml:"-"`
	ReasoningChannelID string              `json:"reasoning_channel_id"           yaml:"-"`
	CryptoDatabasePath string              `json:"crypto_database_path,omitempty" yaml:"-"`
	CryptoPassphrase   string              `json:"crypto_passphrase,omitempty"    yaml:"-"`
}

type LINEConfig struct {
	Enabled            bool                `json:"enabled"                       yaml:"-"                              env:"EBICLAW_CHANNELS_LINE_ENABLED"`
	ChannelSecret      SecureString        `json:"channel_secret,omitzero"       yaml:"channel_secret,omitempty"       env:"EBICLAW_CHANNELS_LINE_CHANNEL_SECRET"`
	ChannelAccessToken SecureString        `json:"channel_access_token,omitzero" yaml:"channel_access_token,omitempty" env:"EBICLAW_CHANNELS_LINE_CHANNEL_ACCESS_TOKEN"`
	WebhookHost        string              `json:"webhook_host"                  yaml:"-"                              env:"EBICLAW_CHANNELS_LINE_WEBHOOK_HOST"`
	WebhookPort        int                 `json:"webhook_port"                  yaml:"-"                              env:"EBICLAW_CHANNELS_LINE_WEBHOOK_PORT"`
	WebhookPath        string              `json:"webhook_path"                  yaml:"-"                              env:"EBICLAW_CHANNELS_LINE_WEBHOOK_PATH"`
	AllowFrom          FlexibleStringSlice `json:"allow_from"                    yaml:"-"                              env:"EBICLAW_CHANNELS_LINE_ALLOW_FROM"`
	GroupTrigger       GroupTriggerConfig  `json:"group_trigger,omitempty"       yaml:"-"`
	Typing             TypingConfig        `json:"typing,omitempty"              yaml:"-"`
	Placeholder        PlaceholderConfig   `json:"placeholder,omitempty"         yaml:"-"`
	ReasoningChannelID string              `json:"reasoning_channel_id"          yaml:"-"`
}

type OneBotConfig struct {
	Enabled            bool                `json:"enabled"                 yaml:"-"                      env:"EBICLAW_CHANNELS_ONEBOT_ENABLED"`
	WSUrl              string              `json:"ws_url"                  yaml:"-"                      env:"EBICLAW_CHANNELS_ONEBOT_WS_URL"`
	AccessToken        SecureString        `json:"access_token,omitzero"   yaml:"access_token,omitempty" env:"EBICLAW_CHANNELS_ONEBOT_ACCESS_TOKEN"`
	ReconnectInterval  int                 `json:"reconnect_interval"      yaml:"-"                      env:"EBICLAW_CHANNELS_ONEBOT_RECONNECT_INTERVAL"`
	GroupTriggerPrefix []string            `json:"group_trigger_prefix"    yaml:"-"                      env:"EBICLAW_CHANNELS_ONEBOT_GROUP_TRIGGER_PREFIX"`
	AllowFrom          FlexibleStringSlice `json:"allow_from"              yaml:"-"                      env:"EBICLAW_CHANNELS_ONEBOT_ALLOW_FROM"`
	GroupTrigger       GroupTriggerConfig  `json:"group_trigger,omitempty" yaml:"-"`
	Typing             TypingConfig        `json:"typing,omitempty"        yaml:"-"`
	Placeholder        PlaceholderConfig   `json:"placeholder,omitempty"   yaml:"-"`
	ReasoningChannelID string              `json:"reasoning_channel_id"    yaml:"-"`
}

type WeComGroupConfig struct {
	AllowFrom FlexibleStringSlice `json:"allow_from,omitempty"`
}

type WeComConfig struct {
	Enabled             bool                `json:"enabled"                 yaml:"-"                env:"ENABLED"`
	BotID               string              `json:"bot_id"                  yaml:"-"                env:"BOT_ID"`
	Secret              SecureString        `json:"secret,omitzero"         yaml:"secret,omitempty" env:"SECRET"`
	WebSocketURL        string              `json:"websocket_url,omitempty" yaml:"-"                env:"WEBSOCKET_URL"`
	SendThinkingMessage bool                `json:"send_thinking_message"   yaml:"-"                env:"SEND_THINKING_MESSAGE"`
	AllowFrom           FlexibleStringSlice `json:"allow_from"              yaml:"-"                env:"ALLOW_FROM"`
	ReasoningChannelID  string              `json:"reasoning_channel_id"    yaml:"-"                env:"REASONING_CHANNEL_ID"`
}

func (c *WeComConfig) SetSecret(secret string) {
	c.Secret = *NewSecureString(secret)
}

type WeixinConfig struct {
	Enabled            bool                `json:"enabled"              yaml:"-"               env:"EBICLAW_CHANNELS_WEIXIN_ENABLED"`
	Token              SecureString        `json:"token,omitzero"       yaml:"token,omitempty" env:"EBICLAW_CHANNELS_WEIXIN_TOKEN"`
	AccountID          string              `json:"account_id,omitempty" yaml:"-"               env:"EBICLAW_CHANNELS_WEIXIN_ACCOUNT_ID"`
	BaseURL            string              `json:"base_url"             yaml:"-"               env:"EBICLAW_CHANNELS_WEIXIN_BASE_URL"`
	CDNBaseURL         string              `json:"cdn_base_url"         yaml:"-"               env:"EBICLAW_CHANNELS_WEIXIN_CDN_BASE_URL"`
	Proxy              string              `json:"proxy"                yaml:"-"               env:"EBICLAW_CHANNELS_WEIXIN_PROXY"`
	AllowFrom          FlexibleStringSlice `json:"allow_from"           yaml:"-"               env:"EBICLAW_CHANNELS_WEIXIN_ALLOW_FROM"`
	ReasoningChannelID string              `json:"reasoning_channel_id" yaml:"-"               env:"EBICLAW_CHANNELS_WEIXIN_REASONING_CHANNEL_ID"`
}

// SetToken sets the Weixin token and marks it as dirty for security saving
func (c *WeixinConfig) SetToken(token string) {
	c.Token = *NewSecureString(token)
}

type PicoConfig struct {
	Enabled         bool                `json:"enabled"                     yaml:"-"               env:"EBICLAW_CHANNELS_PICO_ENABLED"`
	Token           SecureString        `json:"token,omitzero"              yaml:"token,omitempty" env:"EBICLAW_CHANNELS_PICO_TOKEN"`
	AllowTokenQuery bool                `json:"allow_token_query,omitempty" yaml:"-"`
	AllowOrigins    []string            `json:"allow_origins,omitempty"     yaml:"-"`
	PingInterval    int                 `json:"ping_interval,omitempty"     yaml:"-"`
	ReadTimeout     int                 `json:"read_timeout,omitempty"      yaml:"-"`
	WriteTimeout    int                 `json:"write_timeout,omitempty"     yaml:"-"`
	MaxConnections  int                 `json:"max_connections,omitempty"   yaml:"-"`
	AllowFrom       FlexibleStringSlice `json:"allow_from"                  yaml:"-"               env:"EBICLAW_CHANNELS_PICO_ALLOW_FROM"`
	Placeholder     PlaceholderConfig   `json:"placeholder,omitempty"       yaml:"-"`
}

// SetToken sets the Pico token and marks it as dirty for security saving
func (c *PicoConfig) SetToken(token string) {
	c.Token = *NewSecureString(token)
}

type PicoClientConfig struct {
	Enabled      bool                `json:"enabled"                 yaml:"-"               env:"EBICLAW_CHANNELS_PICO_CLIENT_ENABLED"`
	URL          string              `json:"url"                     yaml:"-"               env:"EBICLAW_CHANNELS_PICO_CLIENT_URL"`
	Token        SecureString        `json:"token,omitzero"          yaml:"token,omitempty" env:"EBICLAW_CHANNELS_PICO_CLIENT_TOKEN"`
	SessionID    string              `json:"session_id,omitempty"    yaml:"-"`
	PingInterval int                 `json:"ping_interval,omitempty" yaml:"-"`
	ReadTimeout  int                 `json:"read_timeout,omitempty"  yaml:"-"`
	AllowFrom    FlexibleStringSlice `json:"allow_from"              yaml:"-"               env:"EBICLAW_CHANNELS_PICO_CLIENT_ALLOW_FROM"`
}

type IRCConfig struct {
	Enabled            bool                `json:"enabled"                    yaml:"-"                           env:"EBICLAW_CHANNELS_IRC_ENABLED"`
	Server             string              `json:"server"                     yaml:"-"                           env:"EBICLAW_CHANNELS_IRC_SERVER"`
	TLS                bool                `json:"tls"                        yaml:"-"                           env:"EBICLAW_CHANNELS_IRC_TLS"`
	Nick               string              `json:"nick"                       yaml:"-"                           env:"EBICLAW_CHANNELS_IRC_NICK"`
	User               string              `json:"user,omitempty"             yaml:"-"                           env:"EBICLAW_CHANNELS_IRC_USER"`
	RealName           string              `json:"real_name,omitempty"        yaml:"-"`
	Password           SecureString        `json:"password,omitzero"          yaml:"password,omitempty"          env:"EBICLAW_CHANNELS_IRC_PASSWORD"`
	NickServPassword   SecureString        `json:"nickserv_password,omitzero" yaml:"nickserv_password,omitempty" env:"EBICLAW_CHANNELS_IRC_NICKSERV_PASSWORD"`
	SASLUser           string              `json:"sasl_user"                  yaml:"-"                           env:"EBICLAW_CHANNELS_IRC_SASL_USER"`
	SASLPassword       SecureString        `json:"sasl_password,omitzero"     yaml:"sasl_password,omitempty"     env:"EBICLAW_CHANNELS_IRC_SASL_PASSWORD"`
	Channels           FlexibleStringSlice `json:"channels"                   yaml:"-"                           env:"EBICLAW_CHANNELS_IRC_CHANNELS"`
	RequestCaps        FlexibleStringSlice `json:"request_caps,omitempty"     yaml:"-"`
	AllowFrom          FlexibleStringSlice `json:"allow_from"                 yaml:"-"                           env:"EBICLAW_CHANNELS_IRC_ALLOW_FROM"`
	GroupTrigger       GroupTriggerConfig  `json:"group_trigger,omitempty"    yaml:"-"`
	Typing             TypingConfig        `json:"typing,omitempty"           yaml:"-"`
	ReasoningChannelID string              `json:"reasoning_channel_id"       yaml:"-"`
}

type VKConfig struct {
	Enabled            bool                `json:"enabled"                 yaml:"-"               env:"EBICLAW_CHANNELS_VK_ENABLED"`
	Token              SecureString        `json:"token,omitzero"          yaml:"token,omitempty" env:"EBICLAW_CHANNELS_VK_TOKEN"`
	GroupID            int                 `json:"group_id"                yaml:"-"               env:"EBICLAW_CHANNELS_VK_GROUP_ID"`
	AllowFrom          FlexibleStringSlice `json:"allow_from"              yaml:"-"               env:"EBICLAW_CHANNELS_VK_ALLOW_FROM"`
	GroupTrigger       GroupTriggerConfig  `json:"group_trigger,omitempty" yaml:"-"`
	Typing             TypingConfig        `json:"typing,omitempty"        yaml:"-"`
	Placeholder        PlaceholderConfig   `json:"placeholder,omitempty"   yaml:"-"`
	ReasoningChannelID string              `json:"reasoning_channel_id"    yaml:"-"               env:"EBICLAW_CHANNELS_VK_REASONING_CHANNEL_ID"`
}

func (c *VKConfig) SetToken(token string) {
	c.Token = *NewSecureString(token)
}

// TeamsWebhookConfig configures the output-only Microsoft Teams webhook channel.
// Multiple webhook targets can be configured and selected via ChatID at send time.
type TeamsWebhookConfig struct {
	Enabled  bool                          `json:"enabled"  yaml:"-"                  env:"EBICLAW_CHANNELS_TEAMS_WEBHOOK_ENABLED"`
	Webhooks map[string]TeamsWebhookTarget `json:"webhooks" yaml:"webhooks,omitempty"`
}

// TeamsWebhookTarget represents a single Teams webhook destination.
type TeamsWebhookTarget struct {
	WebhookURL SecureString `json:"webhook_url,omitzero" yaml:"webhook_url,omitempty"`
	Title      string       `json:"title,omitempty"      yaml:"-"`
}

type HeartbeatConfig struct {
	Enabled  bool `json:"enabled"  env:"EBICLAW_HEARTBEAT_ENABLED"`
	Interval int  `json:"interval" env:"EBICLAW_HEARTBEAT_INTERVAL"` // minutes, min 5
}

type DevicesConfig struct {
	Enabled    bool `json:"enabled"     env:"EBICLAW_DEVICES_ENABLED"`
	MonitorUSB bool `json:"monitor_usb" env:"EBICLAW_DEVICES_MONITOR_USB"`
}

type VoiceConfig struct {
	ModelName         string `json:"model_name,omitempty"     env:"EBICLAW_VOICE_MODEL_NAME"`
	TTSModelName      string `json:"tts_model_name,omitempty" env:"EBICLAW_VOICE_TTS_MODEL_NAME"`
	EchoTranscription bool   `json:"echo_transcription"       env:"EBICLAW_VOICE_ECHO_TRANSCRIPTION"`
}

// ModelConfig represents a model-centric provider configuration.
// It allows adding new providers (especially OpenAI-compatible ones) via configuration only.
// The model field uses protocol prefix format: [protocol/]model-identifier
// Supported protocols include openai, anthropic, antigravity, claude-cli,
// codex-cli, github-copilot, and named OpenAI-compatible protocols such as
// groq, deepseek, modelscope, and novita.
// Default protocol is "openai" if no prefix is specified.
type ModelConfig struct {
	// Required fields
	ModelName string `json:"model_name"` // User-facing alias for the model
	Model     string `json:"model"`      // Protocol/model-identifier (e.g., "openai/gpt-4o", "anthropic/claude-sonnet-4.6")

	// HTTP-based providers
	APIBase   string   `json:"api_base,omitempty"`  // API endpoint URL
	Proxy     string   `json:"proxy,omitempty"`     // HTTP proxy URL
	Fallbacks []string `json:"fallbacks,omitempty"` // Fallback model names for failover

	// Special providers (CLI-based, OAuth, etc.)
	AuthMethod  string `json:"auth_method,omitempty"`  // Authentication method: oauth, token
	ConnectMode string `json:"connect_mode,omitempty"` // Connection mode: stdio, grpc
	Workspace   string `json:"workspace,omitempty"`    // Workspace path for CLI-based providers

	// Optional optimizations
	RPM            int               `json:"rpm,omitempty"`              // Requests per minute limit
	MaxTokensField string            `json:"max_tokens_field,omitempty"` // Field name for max tokens (e.g., "max_completion_tokens")
	RequestTimeout int               `json:"request_timeout,omitempty"`
	ThinkingLevel  string            `json:"thinking_level,omitempty"` // Extended thinking: off|low|medium|high|xhigh|adaptive
	ExtraBody      map[string]any    `json:"extra_body,omitempty"`     // Additional fields to inject into request body
	CustomHeaders  map[string]string `json:"custom_headers,omitempty"` // Additional headers to inject into every HTTP request

	APIKeys SecureStrings `json:"api_keys,omitzero" yaml:"api_keys,omitempty"` // API authentication keys (multiple keys for failover)

	// Enabled indicates whether this model entry is active. When omitted in
	// existing configs, the field is inferred during load: models with API keys
	// or the reserved "local-model" name are auto-enabled.
	Enabled bool `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	// UserAgent is the user agent string to use for HTTP requests.
	UserAgent string `json:"user_agent,omitempty" yaml:"-"`

	// isVirtual marks this model as a virtual model generated from multi-key expansion.
	// Virtual models should not be persisted to config files.
	isVirtual bool
}

// APIKey returns the first API key from apiKeys
func (c *ModelConfig) APIKey() string {
	if len(c.APIKeys) > 0 {
		return c.APIKeys[0].String()
	}
	return ""
}

// IsVirtual returns true if this model was generated from multi-key expansion.
func (c *ModelConfig) IsVirtual() bool {
	return c.isVirtual
}

// Validate checks if the ModelConfig has all required fields.
func (c *ModelConfig) Validate() error {
	if c.ModelName == "" {
		return fmt.Errorf("model_name is required")
	}
	if c.Model == "" {
		return fmt.Errorf("model is required")
	}
	return nil
}

func (c *ModelConfig) SetAPIKey(value string) {
	if len(c.APIKeys) > 0 {
		c.APIKeys[0].Set(value)
	} else {
		c.APIKeys = append(c.APIKeys, NewSecureString(value))
	}
}

type ToolDiscoveryConfig struct {
	Enabled          bool `json:"enabled"            env:"EBICLAW_TOOLS_DISCOVERY_ENABLED"`
	TTL              int  `json:"ttl"                env:"EBICLAW_TOOLS_DISCOVERY_TTL"`
	MaxSearchResults int  `json:"max_search_results" env:"EBICLAW_MAX_SEARCH_RESULTS"`
	UseBM25          bool `json:"use_bm25"           env:"EBICLAW_TOOLS_DISCOVERY_USE_BM25"`
	UseRegex         bool `json:"use_regex"          env:"EBICLAW_TOOLS_DISCOVERY_USE_REGEX"`
}

type ToolConfig struct {
	Enabled bool `json:"enabled" yaml:"-" env:"ENABLED"`
}

type BraveConfig struct {
	Enabled    bool          `json:"enabled"           yaml:"-"                  env:"EBICLAW_TOOLS_WEB_BRAVE_ENABLED"`
	APIKeys    SecureStrings `json:"api_keys,omitzero" yaml:"api_keys,omitempty" env:"EBICLAW_TOOLS_WEB_BRAVE_API_KEYS"`
	MaxResults int           `json:"max_results"       yaml:"-"                  env:"EBICLAW_TOOLS_WEB_BRAVE_MAX_RESULTS"`
}

// APIKey returns the Brave API key
func (c *BraveConfig) APIKey() string {
	if len(c.APIKeys) == 0 {
		return ""
	}
	return c.APIKeys[0].String()
}

// SetAPIKey sets the Brave API key
func (c *BraveConfig) SetAPIKey(key string) {
	c.APIKeys = SimpleSecureStrings(key)
}

func (c *BraveConfig) SetAPIKeys(keys []string) {
	c.APIKeys = SimpleSecureStrings(keys...)
}

type TavilyConfig struct {
	Enabled    bool          `json:"enabled"           yaml:"-"                  env:"EBICLAW_TOOLS_WEB_TAVILY_ENABLED"`
	APIKeys    SecureStrings `json:"api_keys,omitzero" yaml:"api_keys,omitempty" env:"EBICLAW_TOOLS_WEB_TAVILY_API_KEYS"`
	BaseURL    string        `json:"base_url"          yaml:"-"                  env:"EBICLAW_TOOLS_WEB_TAVILY_BASE_URL"`
	MaxResults int           `json:"max_results"       yaml:"-"                  env:"EBICLAW_TOOLS_WEB_TAVILY_MAX_RESULTS"`
}

// APIKey returns the Tavily API key
func (c *TavilyConfig) APIKey() string {
	if len(c.APIKeys) == 0 {
		return ""
	}
	return c.APIKeys[0].String()
}

// SetAPIKey sets the Tavily API key
func (c *TavilyConfig) SetAPIKey(key string) {
	c.APIKeys = SimpleSecureStrings(key)
}

// SetAPIKeys sets the Tavily API keys
func (c *TavilyConfig) SetAPIKeys(keys []string) {
	c.APIKeys = make(SecureStrings, len(keys))
	for i, k := range keys {
		c.APIKeys[i] = NewSecureString(k)
	}
}

type DuckDuckGoConfig struct {
	Enabled    bool `json:"enabled"     env:"EBICLAW_TOOLS_WEB_DUCKDUCKGO_ENABLED"`
	MaxResults int  `json:"max_results" env:"EBICLAW_TOOLS_WEB_DUCKDUCKGO_MAX_RESULTS"`
}

type PerplexityConfig struct {
	Enabled    bool          `json:"enabled"           yaml:"-"                  env:"EBICLAW_TOOLS_WEB_PERPLEXITY_ENABLED"`
	APIKeys    SecureStrings `json:"api_keys,omitzero" yaml:"api_keys,omitempty" env:"EBICLAW_TOOLS_WEB_PERPLEXITY_API_KEYS"`
	MaxResults int           `json:"max_results"       yaml:"-"                  env:"EBICLAW_TOOLS_WEB_PERPLEXITY_MAX_RESULTS"`
}

// APIKey returns the Perplexity API key
func (c *PerplexityConfig) APIKey() string {
	if len(c.APIKeys) == 0 {
		return ""
	}
	return c.APIKeys[0].String()
}

// SetAPIKey sets the Perplexity API key
func (c *PerplexityConfig) SetAPIKey(key string) {
	c.APIKeys = SimpleSecureStrings(key)
}

type SearXNGConfig struct {
	Enabled    bool   `json:"enabled"     env:"EBICLAW_TOOLS_WEB_SEARXNG_ENABLED"`
	BaseURL    string `json:"base_url"    env:"EBICLAW_TOOLS_WEB_SEARXNG_BASE_URL"`
	MaxResults int    `json:"max_results" env:"EBICLAW_TOOLS_WEB_SEARXNG_MAX_RESULTS"`
}

type GLMSearchConfig struct {
	Enabled bool         `json:"enabled"          yaml:"-"                 env:"EBICLAW_TOOLS_WEB_GLM_ENABLED"`
	APIKey  SecureString `json:"api_key,omitzero" yaml:"api_key,omitempty" env:"EBICLAW_TOOLS_WEB_GLM_API_KEY"`
	BaseURL string       `json:"base_url"         yaml:"-"                 env:"EBICLAW_TOOLS_WEB_GLM_BASE_URL"`
	// SearchEngine specifies the search backend: "search_std" (default),
	// "search_pro", "search_pro_sogou", or "search_pro_quark".
	SearchEngine string `json:"search_engine" yaml:"-" env:"EBICLAW_TOOLS_WEB_GLM_SEARCH_ENGINE"`
	MaxResults   int    `json:"max_results"   yaml:"-" env:"EBICLAW_TOOLS_WEB_GLM_MAX_RESULTS"`
}

type BaiduSearchConfig struct {
	Enabled    bool         `json:"enabled"          yaml:"-"                 env:"EBICLAW_TOOLS_WEB_BAIDU_ENABLED"`
	APIKey     SecureString `json:"api_key,omitzero" yaml:"api_key,omitempty" env:"EBICLAW_TOOLS_WEB_BAIDU_API_KEY"`
	BaseURL    string       `json:"base_url"         yaml:"-"                 env:"EBICLAW_TOOLS_WEB_BAIDU_BASE_URL"`
	MaxResults int          `json:"max_results"      yaml:"-"                 env:"EBICLAW_TOOLS_WEB_BAIDU_MAX_RESULTS"`
}

type WebToolsConfig struct {
	ToolConfig  `                  yaml:"-"                      envPrefix:"EBICLAW_TOOLS_WEB_"`
	Brave       BraveConfig       `yaml:"brave,omitempty"                                        json:"brave"`
	Tavily      TavilyConfig      `yaml:"tavily,omitempty"                                       json:"tavily"`
	DuckDuckGo  DuckDuckGoConfig  `yaml:"-"                                                      json:"duckduckgo"`
	Perplexity  PerplexityConfig  `yaml:"perplexity,omitempty"                                   json:"perplexity"`
	SearXNG     SearXNGConfig     `yaml:"-"                                                      json:"searxng"`
	GLMSearch   GLMSearchConfig   `yaml:"glm_search,omitempty"                                   json:"glm_search"`
	BaiduSearch BaiduSearchConfig `yaml:"baidu_search,omitempty"                                 json:"baidu_search"`
	// PreferNative controls whether to use provider-native web search when
	// the active LLM supports it (e.g. OpenAI web_search_preview). When true,
	// the client-side web_search tool is hidden to avoid duplicate search surfaces,
	// and the provider's built-in search is used instead. Falls back to client-side
	// search when the provider does not support native search.
	PreferNative bool `yaml:"-" json:"prefer_native" env:"EBICLAW_TOOLS_WEB_PREFER_NATIVE"`
	// Proxy is an optional proxy URL for web tools (http/https/socks5/socks5h).
	// For authenticated proxies, prefer HTTP_PROXY/HTTPS_PROXY env vars instead of embedding credentials in config.
	Proxy                string              `yaml:"-" json:"proxy,omitempty"                  env:"EBICLAW_TOOLS_WEB_PROXY"`
	FetchLimitBytes      int64               `yaml:"-" json:"fetch_limit_bytes,omitempty"      env:"EBICLAW_TOOLS_WEB_FETCH_LIMIT_BYTES"`
	Format               string              `yaml:"-" json:"format,omitempty"                 env:"EBICLAW_TOOLS_WEB_FORMAT"`
	PrivateHostWhitelist FlexibleStringSlice `yaml:"-" json:"private_host_whitelist,omitempty" env:"EBICLAW_TOOLS_WEB_PRIVATE_HOST_WHITELIST"`
}

type CronToolsConfig struct {
	ToolConfig         `     envPrefix:"EBICLAW_TOOLS_CRON_"`
	ExecTimeoutMinutes int  `                                 json:"exec_timeout_minutes" env:"EBICLAW_TOOLS_CRON_EXEC_TIMEOUT_MINUTES"` // 0 means no timeout
	AllowCommand       bool `                                 json:"allow_command"        env:"EBICLAW_TOOLS_CRON_ALLOW_COMMAND"`
}

type ExecConfig struct {
	ToolConfig          `         envPrefix:"EBICLAW_TOOLS_EXEC_"`
	EnableDenyPatterns  bool     `                                 json:"enable_deny_patterns"  env:"EBICLAW_TOOLS_EXEC_ENABLE_DENY_PATTERNS"`
	AllowRemote         bool     `                                 json:"allow_remote"          env:"EBICLAW_TOOLS_EXEC_ALLOW_REMOTE"`
	CustomDenyPatterns  []string `                                 json:"custom_deny_patterns"  env:"EBICLAW_TOOLS_EXEC_CUSTOM_DENY_PATTERNS"`
	CustomAllowPatterns []string `                                 json:"custom_allow_patterns" env:"EBICLAW_TOOLS_EXEC_CUSTOM_ALLOW_PATTERNS"`
	TimeoutSeconds      int      `                                 json:"timeout_seconds"       env:"EBICLAW_TOOLS_EXEC_TIMEOUT_SECONDS"` // 0 means use default (60s)
}

type SkillsToolsConfig struct {
	ToolConfig            `                       yaml:"-"                 envPrefix:"EBICLAW_TOOLS_SKILLS_"`
	Registries            SkillsRegistriesConfig `yaml:",inline,omitempty"                                    json:"registries"`
	Github                SkillsGithubConfig     `yaml:"github,omitempty"                                     json:"github"`
	MaxConcurrentSearches int                    `yaml:"-"                                                    json:"max_concurrent_searches" env:"EBICLAW_TOOLS_SKILLS_MAX_CONCURRENT_SEARCHES"`
	SearchCache           SearchCacheConfig      `yaml:"-"                                                    json:"search_cache"`
}

type MediaCleanupConfig struct {
	ToolConfig `    envPrefix:"EBICLAW_MEDIA_CLEANUP_"`
	MaxAge     int `                                    json:"max_age_minutes"  env:"EBICLAW_MEDIA_CLEANUP_MAX_AGE"`
	Interval   int `                                    json:"interval_minutes" env:"EBICLAW_MEDIA_CLEANUP_INTERVAL"`
}

type ReadFileToolConfig struct {
	Enabled         bool   `json:"enabled"`
	Mode            string `json:"mode"`
	MaxReadFileSize int    `json:"max_read_file_size"`
}

const (
	ReadFileModeBytes = "bytes"
	ReadFileModeLines = "lines"
)

func (c ReadFileToolConfig) EffectiveMode() string {
	switch strings.ToLower(strings.TrimSpace(c.Mode)) {
	case ReadFileModeLines:
		return ReadFileModeLines
	case "", ReadFileModeBytes:
		return ReadFileModeBytes
	default:
		return ReadFileModeBytes
	}
}

type ToolsConfig struct {
	AllowReadPaths  []string `json:"allow_read_paths"  yaml:"-" env:"EBICLAW_TOOLS_ALLOW_READ_PATHS"`
	AllowWritePaths []string `json:"allow_write_paths" yaml:"-" env:"EBICLAW_TOOLS_ALLOW_WRITE_PATHS"`
	// ForbiddenCommands is a single, central list of shell command snippets that
	// must never run. Each entry is propagated to two enforcement points:
	//   1. ebiclaw's exec deny patterns (matched as a regex with word-boundary
	//      anchors and shell metachars escaped).
	//   2. claude CLI subprocess via --disallowed-tools "Bash(*<entry>*)" so the
	//      same command is also blocked when claude reaches for its native Bash.
	// Entries flagged by ForbiddenCommandsBypassInWorkspace get bypassed at the
	// ebi exec layer when the command is run with a cwd inside WorkspaceDirs.
	ForbiddenCommands []string `json:"forbidden_commands" yaml:"-" env:"EBICLAW_TOOLS_FORBIDDEN_COMMANDS"`
	// WorkspaceDirs lists trusted absolute directories. Inside any of these:
	//   - ebi exec skips ForbiddenCommands matching for commands launched with a
	//     cwd that falls under one of these paths.
	//   - claude CLI is launched with --add-dir <dir> so its Edit/Write are
	//     allowed for those paths.
	// Entries should be plain absolute paths (no globs, no regex). Unlike
	// AllowReadPaths/AllowWritePaths these are used as literal prefixes.
	WorkspaceDirs []string `json:"workspace_dirs" yaml:"-" env:"EBICLAW_TOOLS_WORKSPACE_DIRS"`
	// FilterSensitiveData controls whether to filter sensitive values (API keys,
	// tokens, secrets) from tool results before sending to the LLM.
	// Default: true (enabled)
	FilterSensitiveData bool `json:"filter_sensitive_data" yaml:"-" env:"EBICLAW_TOOLS_FILTER_SENSITIVE_DATA"`
	// FilterMinLength is the minimum content length required for filtering.
	// Content shorter than this will be returned unchanged for performance.
	// Default: 8
	FilterMinLength int                `json:"filter_min_length" yaml:"-"                env:"EBICLAW_TOOLS_FILTER_MIN_LENGTH"`
	Web             WebToolsConfig     `json:"web"               yaml:"web,omitempty"`
	Cron            CronToolsConfig    `json:"cron"              yaml:"-"`
	Exec            ExecConfig         `json:"exec"              yaml:"-"`
	Skills          SkillsToolsConfig  `json:"skills"            yaml:"skills,omitempty"`
	MediaCleanup    MediaCleanupConfig `json:"media_cleanup"     yaml:"-"`
	MCP             MCPConfig          `json:"mcp"               yaml:"-"`
	AppendFile      ToolConfig         `json:"append_file"       yaml:"-"                                                       envPrefix:"EBICLAW_TOOLS_APPEND_FILE_"`
	EditFile        ToolConfig         `json:"edit_file"         yaml:"-"                                                       envPrefix:"EBICLAW_TOOLS_EDIT_FILE_"`
	FindSkills      ToolConfig         `json:"find_skills"       yaml:"-"                                                       envPrefix:"EBICLAW_TOOLS_FIND_SKILLS_"`
	I2C             ToolConfig         `json:"i2c"               yaml:"-"                                                       envPrefix:"EBICLAW_TOOLS_I2C_"`
	InstallSkill    ToolConfig         `json:"install_skill"     yaml:"-"                                                       envPrefix:"EBICLAW_TOOLS_INSTALL_SKILL_"`
	ListDir         ToolConfig         `json:"list_dir"          yaml:"-"                                                       envPrefix:"EBICLAW_TOOLS_LIST_DIR_"`
	Message         ToolConfig         `json:"message"           yaml:"-"                                                       envPrefix:"EBICLAW_TOOLS_MESSAGE_"`
	ReadFile        ReadFileToolConfig `json:"read_file"         yaml:"-"                                                       envPrefix:"EBICLAW_TOOLS_READ_FILE_"`
	SendFile        ToolConfig         `json:"send_file"         yaml:"-"                                                       envPrefix:"EBICLAW_TOOLS_SEND_FILE_"`
	SendTTS         ToolConfig         `json:"send_tts"          yaml:"-"                                                       envPrefix:"EBICLAW_TOOLS_SEND_TTS_"`
	Spawn           ToolConfig         `json:"spawn"             yaml:"-"                                                       envPrefix:"EBICLAW_TOOLS_SPAWN_"`
	SpawnStatus     ToolConfig         `json:"spawn_status"      yaml:"-"                                                       envPrefix:"EBICLAW_TOOLS_SPAWN_STATUS_"`
	SPI             ToolConfig         `json:"spi"               yaml:"-"                                                       envPrefix:"EBICLAW_TOOLS_SPI_"`
	Subagent        ToolConfig         `json:"subagent"          yaml:"-"                                                       envPrefix:"EBICLAW_TOOLS_SUBAGENT_"`
	WebFetch        ToolConfig         `json:"web_fetch"         yaml:"-"                                                       envPrefix:"EBICLAW_TOOLS_WEB_FETCH_"`
	WriteFile       ToolConfig         `json:"write_file"        yaml:"-"                                                       envPrefix:"EBICLAW_TOOLS_WRITE_FILE_"`
}

// IsFilterSensitiveDataEnabled returns true if sensitive data filtering is enabled
func (c *ToolsConfig) IsFilterSensitiveDataEnabled() bool {
	return c.FilterSensitiveData
}

// GetFilterMinLength returns the minimum content length for filtering (default: 8)
func (c *ToolsConfig) GetFilterMinLength() int {
	if c.FilterMinLength <= 0 {
		return 8
	}
	return c.FilterMinLength
}

type SearchCacheConfig struct {
	MaxSize    int `json:"max_size"    env:"EBICLAW_SKILLS_SEARCH_CACHE_MAX_SIZE"`
	TTLSeconds int `json:"ttl_seconds" env:"EBICLAW_SKILLS_SEARCH_CACHE_TTL_SECONDS"`
}

type SkillsRegistriesConfig struct {
	ClawHub ClawHubRegistryConfig `json:"clawhub" yaml:"clawhub,omitempty"`
}

type SkillsGithubConfig struct {
	Token SecureString `json:"token,omitzero"  yaml:"token,omitempty" env:"EBICLAW_TOOLS_SKILLS_GITHUB_TOKEN"`
	Proxy string       `json:"proxy,omitempty" yaml:"-"               env:"EBICLAW_TOOLS_SKILLS_GITHUB_PROXY"`
}

type ClawHubRegistryConfig struct {
	Enabled         bool         `json:"enabled"             yaml:"-"                    env:"EBICLAW_SKILLS_REGISTRIES_CLAWHUB_ENABLED"`
	BaseURL         string       `json:"base_url"            yaml:"-"                    env:"EBICLAW_SKILLS_REGISTRIES_CLAWHUB_BASE_URL"`
	AuthToken       SecureString `json:"auth_token,omitzero" yaml:"auth_token,omitempty" env:"EBICLAW_SKILLS_REGISTRIES_CLAWHUB_AUTH_TOKEN"`
	SearchPath      string       `json:"search_path"         yaml:"-"                    env:"EBICLAW_SKILLS_REGISTRIES_CLAWHUB_SEARCH_PATH"`
	SkillsPath      string       `json:"skills_path"         yaml:"-"                    env:"EBICLAW_SKILLS_REGISTRIES_CLAWHUB_SKILLS_PATH"`
	DownloadPath    string       `json:"download_path"       yaml:"-"                    env:"EBICLAW_SKILLS_REGISTRIES_CLAWHUB_DOWNLOAD_PATH"`
	Timeout         int          `json:"timeout"             yaml:"-"                    env:"EBICLAW_SKILLS_REGISTRIES_CLAWHUB_TIMEOUT"`
	MaxZipSize      int          `json:"max_zip_size"        yaml:"-"                    env:"EBICLAW_SKILLS_REGISTRIES_CLAWHUB_MAX_ZIP_SIZE"`
	MaxResponseSize int          `json:"max_response_size"   yaml:"-"                    env:"EBICLAW_SKILLS_REGISTRIES_CLAWHUB_MAX_RESPONSE_SIZE"`
}

// MCPServerConfig defines configuration for a single MCP server
type MCPServerConfig struct {
	// Enabled indicates whether this MCP server is active
	Enabled bool `json:"enabled"`
	// Deferred controls whether this server's tools are registered as hidden (deferred/discovery mode).
	// When nil, the global Discovery.Enabled setting applies.
	// When explicitly set to true or false, it overrides the global setting for this server only.
	Deferred *bool `json:"deferred,omitempty"`
	// Command is the executable to run (e.g., "npx", "python", "/path/to/server")
	Command string `json:"command"`
	// Args are the arguments to pass to the command
	Args []string `json:"args,omitempty"`
	// Env are environment variables to set for the server process (stdio only)
	Env map[string]string `json:"env,omitempty"`
	// EnvFile is the path to a file containing environment variables (stdio only)
	EnvFile string `json:"env_file,omitempty"`
	// Type is "stdio", "sse", or "http" (default: stdio if command is set, sse if url is set)
	Type string `json:"type,omitempty"`
	// URL is used for SSE/HTTP transport
	URL string `json:"url,omitempty"`
	// Headers are HTTP headers to send with requests (sse/http only)
	Headers map[string]string `json:"headers,omitempty"`
}

// MCPConfig defines configuration for all MCP servers
type MCPConfig struct {
	ToolConfig `                    envPrefix:"EBICLAW_TOOLS_MCP_"`
	Discovery  ToolDiscoveryConfig `                                json:"discovery"`
	// MaxInlineTextChars controls how much MCP text stays inline before it is saved as an artifact.
	MaxInlineTextChars int `json:"max_inline_text_chars,omitempty" env:"EBICLAW_TOOLS_MCP_MAX_INLINE_TEXT_CHARS"`
	// Servers is a map of server name to server configuration
	Servers map[string]MCPServerConfig `json:"servers,omitempty"`
}

const DefaultMCPMaxInlineTextChars = 16 * 1024

func (c *MCPConfig) GetMaxInlineTextChars() int {
	if c.MaxInlineTextChars > 0 {
		return c.MaxInlineTextChars
	}
	return DefaultMCPMaxInlineTextChars
}
