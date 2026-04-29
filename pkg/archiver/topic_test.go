package archiver

import (
	"strings"
	"testing"
)

func TestTopic_RoundTrip(t *testing.T) {
	in := Topic{
		Title:          "ログインフローのバグ",
		Slug:           "login-flow-bug",
		Status:         "open",
		Tags:           []string{"auth", "bug"},
		Channels:       []string{"slack/C1", "pico/main"},
		Sources:        []string{"raw/slack/C1/2026-04-29.jsonl#L12-L48"},
		Created:        "2026-04-29",
		Updated:        "2026-04-30",
		Confidence:     "high",
		PrimarilyHuman: true,
		Body:           "## TL;DR\n\nshort.\n",
	}
	rendered := in.Render()
	if !strings.HasPrefix(rendered, "---\n") {
		t.Fatal("missing frontmatter open")
	}

	got, err := ParseTopic(rendered)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Slug != in.Slug || got.Title != in.Title || !got.PrimarilyHuman {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	if len(got.Channels) != 2 || got.Channels[0] != "slack/C1" {
		t.Fatalf("channels mismatch: %+v", got.Channels)
	}
}
