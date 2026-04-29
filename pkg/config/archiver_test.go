package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestArchiverConfig_JSONRoundtrip(t *testing.T) {
	src := `{
        "version": 2,
        "agents": {"defaults": {"model_name":"gpt-5.4"}},
        "model_list": [],
        "channels": {},
        "tools": {},
        "gateway": {},
        "heartbeat": {},
        "devices": {},
        "voice": {},
        "archiver": {
            "enabled": true,
            "repository_path": "/tmp/x",
            "allowlist": ["slack/C1"],
            "schedule": {"cron":"0 3 * * *","timezone":"Asia/Tokyo"},
            "distill": {"max_input_tokens": 50000, "model_name":"gpt-5.4", "max_retries": 3},
            "push": {"warn_after_consecutive_failures": 7},
            "tools_readonly_enabled": false
        }
    }`
	var c Config
	if err := json.NewDecoder(strings.NewReader(src)).Decode(&c); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !c.Archiver.Enabled || c.Archiver.RepositoryPath != "/tmp/x" {
		t.Fatalf("archiver not parsed: %+v", c.Archiver)
	}
	if len(c.Archiver.Allowlist) != 1 || c.Archiver.Allowlist[0] != "slack/C1" {
		t.Fatalf("allowlist: %+v", c.Archiver.Allowlist)
	}
}
