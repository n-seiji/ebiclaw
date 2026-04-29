package archiver

import "testing"

func TestChannelKey(t *testing.T) {
	if got := ChannelKey("slack", "C0123ABC"); got != "slack/C0123ABC" {
		t.Fatalf("got %q", got)
	}
}

func TestConfig_ShouldArchive(t *testing.T) {
	c := &Config{
		Enabled:        true,
		RepositoryPath: "/tmp/x",
		Allowlist:      []string{"slack/C1", "pico/main"},
	}
	cases := []struct {
		platform string
		chatID   string
		want     bool
	}{
		{"slack", "C1", true},
		{"slack", "C2", false},
		{"pico", "main", true},
		{"discord", "X", false},
	}
	for _, tc := range cases {
		if got := c.ShouldArchive(tc.platform, tc.chatID); got != tc.want {
			t.Fatalf("ShouldArchive(%q,%q)=%v want %v", tc.platform, tc.chatID, got, tc.want)
		}
	}
}

func TestConfig_DisabledIfRepoMissing(t *testing.T) {
	c := &Config{Enabled: true, Allowlist: []string{"slack/C1"}}
	if c.Active() {
		t.Fatal("expected Active=false when RepositoryPath empty")
	}
}
