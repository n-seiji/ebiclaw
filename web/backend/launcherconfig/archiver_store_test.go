package launcherconfig

import (
	"reflect"
	"testing"

	"github.com/sipeed/picoclaw/pkg/archiver"
	"github.com/sipeed/picoclaw/pkg/config"
)

func newInMemStore(initial *config.Config) *ArchiverStore {
	cur := *initial
	return NewArchiverStoreWithFuncs(
		func() (*config.Config, error) { c := cur; return &c, nil },
		func(c *config.Config) error { cur = *c; return nil },
	)
}

func TestArchiverStore_GetReflectsConfig(t *testing.T) {
	cfg := &config.Config{Archiver: archiver.Config{
		Enabled:        true,
		RepositoryPath: "/tmp/x",
		Allowlist:      []string{"slack/C1"},
		Schedule:       archiver.Schedule{Cron: "0 3 * * *", Timezone: "Asia/Tokyo"},
		Distill:        archiver.DistillConf{ModelName: "gpt-5.4", MaxInputTokens: 50000, MaxRetries: 3},
		Push:           archiver.PushConf{WarnAfterConsecutiveFailures: 7},
		ToolsReadOnly:  true,
	}}
	s := newInMemStore(cfg)

	got := s.Get()
	if got["enabled"] != true || got["repository_path"] != "/tmp/x" || got["tools_readonly_enabled"] != true {
		t.Fatalf("missing fields: %+v", got)
	}
	allow, _ := got["allowlist"].([]string)
	if !reflect.DeepEqual(allow, []string{"slack/C1"}) {
		t.Fatalf("allowlist: %+v", got["allowlist"])
	}
	sched, _ := got["schedule"].(map[string]any)
	if sched["cron"] != "0 3 * * *" || sched["timezone"] != "Asia/Tokyo" {
		t.Fatalf("schedule: %+v", sched)
	}
}

func TestArchiverStore_PutAppliesFields(t *testing.T) {
	cfg := &config.Config{}
	var saved *config.Config
	s := NewArchiverStoreWithFuncs(
		func() (*config.Config, error) { c := *cfg; return &c, nil },
		func(c *config.Config) error { saved = c; return nil },
	)

	err := s.Put(map[string]any{
		"enabled":                true,
		"repository_path":        "/tmp/y",
		"allowlist":              []any{"slack/C2", "pico/main"},
		"schedule":               map[string]any{"cron": "0 4 * * *", "timezone": "UTC"},
		"distill":                map[string]any{"model_name": "haiku-4.5", "max_input_tokens": float64(10000), "max_retries": float64(5)},
		"push":                   map[string]any{"warn_after_consecutive_failures": float64(3)},
		"tools_readonly_enabled": false,
	})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if saved == nil {
		t.Fatal("saver not called")
	}
	if !saved.Archiver.Enabled || saved.Archiver.RepositoryPath != "/tmp/y" {
		t.Fatalf("flags: %+v", saved.Archiver)
	}
	if !reflect.DeepEqual(saved.Archiver.Allowlist, []string{"slack/C2", "pico/main"}) {
		t.Fatalf("allowlist: %+v", saved.Archiver.Allowlist)
	}
	if saved.Archiver.Schedule.Cron != "0 4 * * *" || saved.Archiver.Schedule.Timezone != "UTC" {
		t.Fatalf("schedule: %+v", saved.Archiver.Schedule)
	}
	if saved.Archiver.Distill.ModelName != "haiku-4.5" || saved.Archiver.Distill.MaxInputTokens != 10000 || saved.Archiver.Distill.MaxRetries != 5 {
		t.Fatalf("distill: %+v", saved.Archiver.Distill)
	}
	if saved.Archiver.Push.WarnAfterConsecutiveFailures != 3 {
		t.Fatalf("push: %+v", saved.Archiver.Push)
	}
}

func TestArchiverStore_PutPartialPreservesUnsetFields(t *testing.T) {
	initial := &config.Config{Archiver: archiver.Config{
		Enabled:        true,
		RepositoryPath: "/tmp/orig",
		ToolsReadOnly:  true,
	}}
	var saved *config.Config
	cur := *initial
	s := NewArchiverStoreWithFuncs(
		func() (*config.Config, error) { c := cur; return &c, nil },
		func(c *config.Config) error { saved = c; return nil },
	)

	if err := s.Put(map[string]any{"enabled": false}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if saved.Archiver.Enabled {
		t.Fatalf("enabled should be false")
	}
	if saved.Archiver.RepositoryPath != "/tmp/orig" {
		t.Fatalf("repo path overwritten: %q", saved.Archiver.RepositoryPath)
	}
	if !saved.Archiver.ToolsReadOnly {
		t.Fatalf("tools_readonly should remain true")
	}
}
