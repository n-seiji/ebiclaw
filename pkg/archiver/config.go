package archiver

// Config controls the archiver. Active() returns true only when the feature
// is fully wired (enabled + repository_path set).
type Config struct {
	Enabled        bool        `json:"enabled"`
	RepositoryPath string      `json:"repository_path"`
	Allowlist      []string    `json:"allowlist"` // entries are "<platform>/<chat_id>"
	Schedule       Schedule    `json:"schedule"`
	Distill        DistillConf `json:"distill"`
	Push           PushConf    `json:"push"`
	ToolsReadOnly  bool        `json:"tools_readonly_enabled"`
}

type Schedule struct {
	Cron     string `json:"cron"`     // e.g., "0 3 * * *"
	Timezone string `json:"timezone"` // IANA name, e.g., "Asia/Tokyo"
}

type DistillConf struct {
	MaxInputTokens int    `json:"max_input_tokens"`
	ModelName      string `json:"model_name"`
	MaxRetries     int    `json:"max_retries"`
}

type PushConf struct {
	WarnAfterConsecutiveFailures int `json:"warn_after_consecutive_failures"`
}

// ChannelKey builds the canonical "<platform>/<chat_id>" key used in allowlist
// and on-disk paths.
func ChannelKey(platform, chatID string) string {
	return platform + "/" + chatID
}

// Active reports whether the archiver should attach observers and run batches.
func (c *Config) Active() bool {
	return c != nil && c.Enabled && c.RepositoryPath != ""
}

// ShouldArchive returns true if the given (platform, chatID) is allowlisted.
func (c *Config) ShouldArchive(platform, chatID string) bool {
	if !c.Active() {
		return false
	}
	key := ChannelKey(platform, chatID)
	for _, k := range c.Allowlist {
		if k == key {
			return true
		}
	}
	return false
}

// Defaults returns a Config with sensible defaults filled in.
func Defaults() Config {
	return Config{
		Schedule: Schedule{Cron: "0 3 * * *", Timezone: "Asia/Tokyo"},
		Distill:  DistillConf{MaxInputTokens: 50000, MaxRetries: 3},
		Push:     PushConf{WarnAfterConsecutiveFailures: 7},
	}
}
