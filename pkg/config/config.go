package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/caarlos0/env/v11"

	"github.com/n-seiji/ebiclaw/pkg"
	"github.com/n-seiji/ebiclaw/pkg/fileutil"
	"github.com/n-seiji/ebiclaw/pkg/logger"
)

// rrCounter is a global counter for round-robin load balancing across models.
var rrCounter atomic.Uint64

// CurrentVersion is the latest config schema version
const CurrentVersion = 2

func LoadConfig(path string) (*Config, error) {
	logger.Debugf("loading config from %s", path)

	updateResolver(filepath.Dir(path))

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			logger.WarnF(
				"config file not found, using default config",
				map[string]any{"path": path},
			)
			return DefaultConfig(), nil
		}
		logger.Errorf("failed to read config file: %v", err)
		return nil, err
	}

	// First, try to detect config version by reading the version field
	var versionInfo struct {
		Version int `json:"version"`
	}
	if e := json.Unmarshal(data, &versionInfo); e != nil {
		return nil, fmt.Errorf("failed to detect config version: %w", e)
	}
	if len(data) <= 10 {
		logger.Warn(fmt.Sprintf("content is [%s]", string(data)))
		return DefaultConfig(), nil
	}

	// Load config based on detected version
	var cfg *Config
	switch versionInfo.Version {
	case 0:
		logger.InfoF(
			"config migrate start",
			map[string]any{"from": versionInfo.Version, "to": CurrentVersion},
		)
		// Legacy config (no version field)
		v, e := loadConfigV0(data)
		if e != nil {
			return nil, e
		}
		cfg, e = v.Migrate()
		if e != nil {
			logger.ErrorF(
				"config migrate fail",
				map[string]any{"from": versionInfo.Version, "to": CurrentVersion},
			)
			return nil, e
		}
		logger.InfoF(
			"config migrate success",
			map[string]any{"from": versionInfo.Version, "to": CurrentVersion},
		)
		err = makeBackup(path)
		if err != nil {
			return nil, err
		}
		// Load existing security config and merge with migrated one to prevent data loss
		secErr := loadSecurityConfig(cfg, securityPath(path))
		if secErr != nil && !os.IsNotExist(secErr) {
			logger.WarnF(
				"failed to load existing security config during migration",
				map[string]any{"error": secErr},
			)
			return nil, fmt.Errorf("failed to load existing security config: %w", secErr)
		}
		defer func(cfg *Config) {
			_ = SaveConfig(path, cfg)
		}(cfg)
	case 1:
		// V1→V2 migration: infer Enabled and migrate channel config fields
		logger.InfoF(
			"config migrate start",
			map[string]any{"from": versionInfo.Version, "to": CurrentVersion},
		)
		cfg, err = loadConfig(data)
		if err != nil {
			return nil, err
		}
		secPath := securityPath(path)
		err = loadSecurityConfig(cfg, secPath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("failed to load security config: %w", err)
		}

		oldCfg := &configV1{Config: *cfg}
		cfg, err = oldCfg.Migrate()
		if err != nil {
			logger.ErrorF(
				"config migrate fail",
				map[string]any{"from": versionInfo.Version, "to": CurrentVersion},
			)
			return nil, err
		}

		err = makeBackup(path)
		if err != nil {
			return nil, err
		}

		defer func(cfg *Config) {
			_ = SaveConfig(path, cfg)
		}(cfg)
		logger.InfoF(
			"config migrate success",
			map[string]any{"from": versionInfo.Version, "to": CurrentVersion},
		)
	case CurrentVersion:
		// Current version
		cfg, err = loadConfig(data)
		if err != nil {
			return nil, err
		}
		// Load security configuration
		secPath := securityPath(path)
		err = loadSecurityConfig(cfg, secPath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("failed to load security config: %w", err)
		}

	default:
		return nil, fmt.Errorf("unsupported config version: %d", versionInfo.Version)
	}

	if err = env.Parse(cfg); err != nil {
		return nil, err
	}

	// Expand multi-key configs into separate entries for key-level failover
	cfg.ModelList = expandMultiKeyModels(cfg.ModelList)

	// Validate model_list for uniqueness and required fields
	if err = cfg.ValidateModelList(); err != nil {
		return nil, err
	}

	// Ensure Workspace has a default if not set
	if cfg.Agents.Defaults.Workspace == "" {
		homePath := GetHome()
		cfg.Agents.Defaults.Workspace = filepath.Join(homePath, pkg.WorkspaceName)
	}

	return cfg, nil
}

func makeBackup(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	dateSuffix := time.Now().Format(".20060102.bak")
	// Backup config file
	bakPath := path + dateSuffix
	if err := fileutil.CopyFile(path, bakPath, 0o600); err != nil {
		logger.ErrorF("failed to create config backup", map[string]any{"error": err})
		return fmt.Errorf("failed to create config backup: %w", err)
	}
	// Backup security config file
	secPath := securityPath(path)
	if _, err := os.Stat(secPath); err == nil {
		secBakPath := secPath + dateSuffix
		if secErr := fileutil.CopyFile(secPath, secBakPath, 0o600); secErr != nil {
			logger.ErrorF("failed to create security backup", map[string]any{"error": secErr})
			return fmt.Errorf("failed to create security backup: %w", secErr)
		}
	}
	return nil
}

func toNameIndex(list []*ModelConfig) []string {
	nameList := make([]string, 0, len(list))
	countMap := make(map[string]int)
	for _, model := range list {
		name := model.ModelName
		index := countMap[name]
		nameList = append(nameList, fmt.Sprintf("%s:%d", name, index))
		countMap[name]++
	}
	return nameList
}

func SaveConfig(path string, cfg *Config) error {
	if cfg.Version < CurrentVersion {
		cfg.Version = CurrentVersion
	}
	// Filter out virtual models before serializing to config file
	nonVirtualModels := make([]*ModelConfig, 0, len(cfg.ModelList))
	for _, m := range cfg.ModelList {
		if !m.isVirtual {
			nonVirtualModels = append(nonVirtualModels, m)
		}
	}
	// Temporarily replace ModelList with filtered version for serialization
	originalModelList := cfg.ModelList
	defer func() {
		// Restore original ModelList after serialization
		cfg.ModelList = originalModelList
	}()
	cfg.ModelList = nonVirtualModels

	if err := saveSecurityConfig(securityPath(path), cfg); err != nil {
		logger.ErrorCF("config", "cannot save .security.yml", map[string]any{"error": err})
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	logger.Infof("saving config to %s", path)
	return fileutil.WriteFileAtomic(path, data, 0o600)
}

func (c *Config) WorkspacePath() string {
	return expandHome(c.Agents.Defaults.Workspace)
}

func expandHome(path string) string {
	if path == "" {
		return path
	}
	if path[0] == '~' {
		home, _ := os.UserHomeDir()
		if len(path) > 1 && path[1] == '/' {
			return home + path[1:]
		}
		return home
	}
	return path
}

// GetModelConfig returns the ModelConfig for the given model name.
// If multiple configs exist with the same model_name, it uses round-robin
// selection for load balancing. Returns an error if the model is not found.
func (c *Config) GetModelConfig(modelName string) (*ModelConfig, error) {
	matches := c.findMatches(modelName)
	if len(matches) == 0 {
		return nil, fmt.Errorf("model %q not found in model_list or providers", modelName)
	}
	if len(matches) == 1 {
		return matches[0], nil
	}

	// Multiple configs - use round-robin for load balancing
	idx := (rrCounter.Add(1) - 1) % uint64(len(matches))
	return matches[idx], nil
}

// findMatches finds all ModelConfig entries with the given model_name.
func (c *Config) findMatches(modelName string) []*ModelConfig {
	var matches []*ModelConfig
	for i := range c.ModelList {
		if c.ModelList[i].ModelName == modelName {
			matches = append(matches, c.ModelList[i])
		}
	}
	return matches
}

// ValidateModelList validates all ModelConfig entries in the model_list.
// It checks that each model config is valid.
// Note: Multiple entries with the same model_name are allowed for load balancing.
func (c *Config) ValidateModelList() error {
	for i := range c.ModelList {
		if err := c.ModelList[i].Validate(); err != nil {
			return fmt.Errorf("model_list[%d]: %w", i, err)
		}
	}
	return nil
}

func (c *Config) SecurityCopyFrom(path string) error {
	return loadSecurityConfig(c, securityPath(path))
}

// expandMultiKeyModels expands ModelConfig entries with multiple API keys into
// separate entries for key-level failover. Each key gets its own ModelConfig entry,
// and the original entry's fallbacks are set up to chain through the expanded entries.
//
// Example: {"model_name": "gpt-4", "api_keys": ["k1", "k2", "k3"]}
// Becomes:
//   - {"model_name": "gpt-4", "api_keys": ["k1"], "fallbacks": ["gpt-4__key_1", "gpt-4__key_2"]}
//   - {"model_name": "gpt-4__key_1", "api_keys": {"k2"}}
//   - {"model_name": "gpt-4__key_2", "api_keys": {"k3"}}
func expandMultiKeyModels(models []*ModelConfig) []*ModelConfig {
	var expanded []*ModelConfig

	for _, m := range models {
		keys := m.APIKeys.Values()

		// Single key or no keys: keep as-is
		if len(keys) <= 1 {
			expanded = append(expanded, m)
			continue
		}

		// Multiple keys: expand
		originalName := m.ModelName

		// Create entries for additional keys (key_1, key_2, ...)
		var fallbackNames []string
		for i := 1; i < len(keys); i++ {
			suffix := fmt.Sprintf("__key_%d", i)
			expandedName := originalName + suffix

			// Create a copy for the additional key
			additionalEntry := &ModelConfig{
				ModelName:      expandedName,
				Model:          m.Model,
				APIBase:        m.APIBase,
				APIKeys:        SimpleSecureStrings(keys[i]),
				Proxy:          m.Proxy,
				AuthMethod:     m.AuthMethod,
				ConnectMode:    m.ConnectMode,
				Workspace:      m.Workspace,
				RPM:            m.RPM,
				MaxTokensField: m.MaxTokensField,
				RequestTimeout: m.RequestTimeout,
				ThinkingLevel:  m.ThinkingLevel,
				ExtraBody:      m.ExtraBody,
				CustomHeaders:  m.CustomHeaders,
				isVirtual:      true,
			}
			expanded = append(expanded, additionalEntry)
			fallbackNames = append(fallbackNames, expandedName)
		}

		// Create the primary entry with first key and fallbacks
		primaryEntry := &ModelConfig{
			ModelName:      originalName,
			Model:          m.Model,
			APIBase:        m.APIBase,
			Proxy:          m.Proxy,
			AuthMethod:     m.AuthMethod,
			ConnectMode:    m.ConnectMode,
			Workspace:      m.Workspace,
			RPM:            m.RPM,
			MaxTokensField: m.MaxTokensField,
			RequestTimeout: m.RequestTimeout,
			ThinkingLevel:  m.ThinkingLevel,
			ExtraBody:      m.ExtraBody,
			CustomHeaders:  m.CustomHeaders,
			APIKeys:        SimpleSecureStrings(keys[0]),
		}

		// Prepend new fallbacks to existing ones
		if len(fallbackNames) > 0 {
			primaryEntry.Fallbacks = append(fallbackNames, m.Fallbacks...)
		} else if len(m.Fallbacks) > 0 {
			primaryEntry.Fallbacks = m.Fallbacks
		}

		expanded = append(expanded, primaryEntry)
	}

	return expanded
}

func (t *ToolsConfig) IsToolEnabled(name string) bool {
	switch name {
	case "web":
		return t.Web.Enabled
	case "cron":
		return t.Cron.Enabled
	case "exec":
		return t.Exec.Enabled
	case "skills":
		return t.Skills.Enabled
	case "media_cleanup":
		return t.MediaCleanup.Enabled
	case "append_file":
		return t.AppendFile.Enabled
	case "edit_file":
		return t.EditFile.Enabled
	case "find_skills":
		return t.FindSkills.Enabled
	case "i2c":
		return t.I2C.Enabled
	case "install_skill":
		return t.InstallSkill.Enabled
	case "list_dir":
		return t.ListDir.Enabled
	case "message":
		return t.Message.Enabled
	case "read_file":
		return t.ReadFile.Enabled
	case "spawn":
		return t.Spawn.Enabled
	case "spawn_status":
		return t.SpawnStatus.Enabled
	case "spi":
		return t.SPI.Enabled
	case "subagent":
		return t.Subagent.Enabled
	case "web_fetch":
		return t.WebFetch.Enabled
	case "send_file":
		return t.SendFile.Enabled
	case "send_tts":
		return t.SendTTS.Enabled
	case "write_file":
		return t.WriteFile.Enabled
	case "mcp":
		return t.MCP.Enabled
	default:
		return true
	}
}
