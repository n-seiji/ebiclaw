package launcherconfig

import (
	"github.com/sipeed/picoclaw/pkg/config"
)

// ArchiverStore reads and writes the archiver block of the live config.json
// on behalf of web/backend/api.NewArchiverHandler.
type ArchiverStore struct {
	loader func() (*config.Config, error)
	saver  func(*config.Config) error
}

// NewArchiverStore wires up the loader (LoadConfig) and saver (SaveConfig)
// closures around a configPath. The actual archiver.Service lives in the
// gateway process, so this store does not trigger Reload here; the gateway
// picks up changes the next time it reloads its config.
func NewArchiverStore(configPath string) *ArchiverStore {
	return &ArchiverStore{
		loader: func() (*config.Config, error) { return config.LoadConfig(configPath) },
		saver:  func(c *config.Config) error { return config.SaveConfig(configPath, c) },
	}
}

// NewArchiverStoreWithFuncs is the test-friendly constructor that lets callers
// inject custom loader/saver closures.
func NewArchiverStoreWithFuncs(loader func() (*config.Config, error), saver func(*config.Config) error) *ArchiverStore {
	return &ArchiverStore{loader: loader, saver: saver}
}

// Get returns the archiver block of the current config as a generic map.
func (s *ArchiverStore) Get() map[string]any {
	c, err := s.loader()
	if err != nil || c == nil {
		return map[string]any{}
	}
	return map[string]any{
		"enabled":         c.Archiver.Enabled,
		"repository_path": c.Archiver.RepositoryPath,
		"allowlist":       c.Archiver.Allowlist,
		"schedule": map[string]any{
			"cron":     c.Archiver.Schedule.Cron,
			"timezone": c.Archiver.Schedule.Timezone,
		},
		"distill": map[string]any{
			"max_input_tokens": c.Archiver.Distill.MaxInputTokens,
			"model_name":       c.Archiver.Distill.ModelName,
			"max_retries":      c.Archiver.Distill.MaxRetries,
		},
		"push": map[string]any{
			"warn_after_consecutive_failures": c.Archiver.Push.WarnAfterConsecutiveFailures,
		},
		"tools_readonly_enabled": c.Archiver.ToolsReadOnly,
	}
}

// Put applies the incoming map onto the archiver block and saves config.json.
// Unknown keys are ignored; missing keys preserve existing values.
func (s *ArchiverStore) Put(in map[string]any) error {
	c, err := s.loader()
	if err != nil {
		return err
	}
	if c == nil {
		return nil
	}
	if v, ok := in["enabled"].(bool); ok {
		c.Archiver.Enabled = v
	}
	if v, ok := in["repository_path"].(string); ok {
		c.Archiver.RepositoryPath = v
	}
	if v, ok := in["allowlist"].([]any); ok {
		c.Archiver.Allowlist = c.Archiver.Allowlist[:0]
		for _, x := range v {
			if s, ok := x.(string); ok {
				c.Archiver.Allowlist = append(c.Archiver.Allowlist, s)
			}
		}
	}
	if sched, ok := in["schedule"].(map[string]any); ok {
		if v, ok := sched["cron"].(string); ok {
			c.Archiver.Schedule.Cron = v
		}
		if v, ok := sched["timezone"].(string); ok {
			c.Archiver.Schedule.Timezone = v
		}
	}
	if dist, ok := in["distill"].(map[string]any); ok {
		if v, ok := dist["model_name"].(string); ok {
			c.Archiver.Distill.ModelName = v
		}
		if v, ok := dist["max_input_tokens"].(float64); ok {
			c.Archiver.Distill.MaxInputTokens = int(v)
		}
		if v, ok := dist["max_retries"].(float64); ok {
			c.Archiver.Distill.MaxRetries = int(v)
		}
	}
	if push, ok := in["push"].(map[string]any); ok {
		if v, ok := push["warn_after_consecutive_failures"].(float64); ok {
			c.Archiver.Push.WarnAfterConsecutiveFailures = int(v)
		}
	}
	if v, ok := in["tools_readonly_enabled"].(bool); ok {
		c.Archiver.ToolsReadOnly = v
	}
	return s.saver(c)
}
