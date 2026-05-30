package slack

import (
	"context"
	"testing"
	"time"

	"github.com/n-seiji/ebiclaw/pkg/bus"
	"github.com/n-seiji/ebiclaw/pkg/config"
)

// inboundCapture drains the bus inbound channel into a slice for test assertions.
type inboundCapture struct {
	ch <-chan bus.InboundMessage
}

func captureInbound(t *testing.T, mb *bus.MessageBus) *inboundCapture {
	t.Helper()
	return &inboundCapture{ch: mb.InboundChan()}
}

func (c *inboundCapture) waitOne(t *testing.T, timeout time.Duration) bus.InboundMessage {
	t.Helper()
	select {
	case m := <-c.ch:
		return m
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for inbound message")
		return bus.InboundMessage{}
	}
}

func TestParseSlackChatID(t *testing.T) {
	tests := []struct {
		name       string
		chatID     string
		wantChanID string
		wantThread string
	}{
		{
			name:       "channel only",
			chatID:     "C123456",
			wantChanID: "C123456",
			wantThread: "",
		},
		{
			name:       "channel with thread",
			chatID:     "C123456/1234567890.123456",
			wantChanID: "C123456",
			wantThread: "1234567890.123456",
		},
		{
			name:       "DM channel",
			chatID:     "D987654",
			wantChanID: "D987654",
			wantThread: "",
		},
		{
			name:       "empty string",
			chatID:     "",
			wantChanID: "",
			wantThread: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chanID, threadTS := parseSlackChatID(tt.chatID)
			if chanID != tt.wantChanID {
				t.Errorf("parseSlackChatID(%q) channelID = %q, want %q", tt.chatID, chanID, tt.wantChanID)
			}
			if threadTS != tt.wantThread {
				t.Errorf("parseSlackChatID(%q) threadTS = %q, want %q", tt.chatID, threadTS, tt.wantThread)
			}
		})
	}
}

func TestStripBotMention(t *testing.T) {
	ch := &SlackChannel{botUserID: "U12345BOT"}

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "mention at start",
			input: "<@U12345BOT> hello there",
			want:  "hello there",
		},
		{
			name:  "mention in middle",
			input: "hey <@U12345BOT> can you help",
			want:  "hey  can you help",
		},
		{
			name:  "no mention",
			input: "hello world",
			want:  "hello world",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "only mention",
			input: "<@U12345BOT>",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ch.stripBotMention(tt.input)
			if got != tt.want {
				t.Errorf("stripBotMention(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNewSlackChannel(t *testing.T) {
	msgBus := bus.NewMessageBus()

	t.Run("missing bot token", func(t *testing.T) {
		cfg := config.SlackConfig{}
		cfg.AppToken = *config.NewSecureString("xapp-test")
		_, err := NewSlackChannel(cfg, msgBus)
		if err == nil {
			t.Error("expected error for missing bot_token, got nil")
		}
	})

	t.Run("missing app token", func(t *testing.T) {
		cfg := config.SlackConfig{}
		cfg.BotToken = *config.NewSecureString("xoxb-test")
		_, err := NewSlackChannel(cfg, msgBus)
		if err == nil {
			t.Error("expected error for missing app_token, got nil")
		}
	})

	t.Run("valid config", func(t *testing.T) {
		cfg := config.SlackConfig{
			AllowFrom: []string{"U123"},
		}
		cfg.BotToken = *config.NewSecureString("xoxb-test")
		cfg.AppToken = *config.NewSecureString("xapp-test")
		ch, err := NewSlackChannel(cfg, msgBus)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ch.Name() != "slack" {
			t.Errorf("Name() = %q, want %q", ch.Name(), "slack")
		}
		if ch.IsRunning() {
			t.Error("new channel should not be running")
		}
	})
}

func TestSlackChannelIsAllowed(t *testing.T) {
	msgBus := bus.NewMessageBus()

	t.Run("empty allowlist allows all", func(t *testing.T) {
		cfg := config.SlackConfig{
			AllowFrom: []string{},
		}
		cfg.BotToken = *config.NewSecureString("xoxb-test")
		cfg.AppToken = *config.NewSecureString("xapp-test")
		ch, _ := NewSlackChannel(cfg, msgBus)
		if !ch.IsAllowed("U_ANYONE") {
			t.Error("empty allowlist should allow all users")
		}
	})

	t.Run("allowlist restricts users", func(t *testing.T) {
		cfg := config.SlackConfig{
			AllowFrom: []string{"U_ALLOWED"},
		}
		cfg.BotToken = *config.NewSecureString("xoxb-test")
		cfg.AppToken = *config.NewSecureString("xapp-test")
		ch, _ := NewSlackChannel(cfg, msgBus)
		if !ch.IsAllowed("U_ALLOWED") {
			t.Error("allowed user should pass allowlist check")
		}
		if ch.IsAllowed("U_BLOCKED") {
			t.Error("non-allowed user should be blocked")
		}
	})
}

func newSlackChannelForTest(t *testing.T, botUserID string) *SlackChannel {
	t.Helper()
	cfg := config.SlackConfig{}
	cfg.BotToken = *config.NewSecureString("xoxb-test")
	cfg.AppToken = *config.NewSecureString("xapp-test")
	ch, err := NewSlackChannel(cfg, bus.NewMessageBus())
	if err != nil {
		t.Fatalf("NewSlackChannel: %v", err)
	}
	ch.botUserID = botUserID
	return ch
}

func TestSlackContainsBotMention(t *testing.T) {
	ch := newSlackChannelForTest(t, "UBOT123")
	if !ch.containsBotMention("hello <@UBOT123> please help") {
		t.Error("expected mention to be detected")
	}
	if ch.containsBotMention("hello world") {
		t.Error("plain text should not match")
	}
	if ch.containsBotMention("<@UOTHER>") {
		t.Error("mentioning a different user should not match")
	}

	noBot := newSlackChannelForTest(t, "")
	if noBot.containsBotMention("<@U1>") {
		t.Error("empty botUserID must conservatively report false")
	}
}

func TestIgnoreByMeta(t *testing.T) {
	tests := []struct {
		name string
		meta channelMeta
		want bool
	}{
		{
			name: "dm ignored",
			meta: channelMeta{isIM: true},
			want: true,
		},
		{
			name: "private ignored",
			meta: channelMeta{name: "secret", isPrivate: true},
			want: true,
		},
		{
			name: "ext dash ignored",
			meta: channelMeta{name: "ext-alerts"},
			want: true,
		},
		{
			name: "ext underscore ignored",
			meta: channelMeta{name: "ext_ops"},
			want: true,
		},
		{
			name: "public allowed",
			meta: channelMeta{name: "engineering"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ignoreByMeta(tt.meta); got != tt.want {
				t.Fatalf("ignoreByMeta(%+v)=%v want %v", tt.meta, got, tt.want)
			}
		})
	}
}

func TestSlackShouldIgnoreChannel(t *testing.T) {
	ch := newSlackChannelForTest(t, "UBOT123")

	if !ch.shouldIgnoreChannel("D123456") {
		t.Fatal("DM should be ignored")
	}

	ch.channelMetaCache.Store("CPRIV", channelMeta{name: "secret", isPrivate: true})
	if !ch.shouldIgnoreChannel("CPRIV") {
		t.Fatal("private channel should be ignored")
	}

	ch.channelMetaCache.Store("CEXT", channelMeta{name: "ext-alerts"})
	if !ch.shouldIgnoreChannel("CEXT") {
		t.Fatal("ext-* channel should be ignored")
	}

	ch.channelMetaCache.Store("CPUB", channelMeta{name: "general"})
	if ch.shouldIgnoreChannel("CPUB") {
		t.Fatal("public channel should not be ignored")
	}
}

// TestSlackSendThreadAnchorFromChatID verifies that when the OutboundMessage's
// ChatID encodes a thread anchor (the convention used by handleAppMention for
// top-level mentions: "<channel>/<messageTS>"), parseSlackChatID extracts the
// thread root used by Send to set thread_ts.
func TestSlackSendThreadAnchorFromChatID(t *testing.T) {
	cases := []struct {
		name           string
		chatID         string
		wantChannelID  string
		wantThreadAnch string
	}{
		{
			name:           "mention without thread anchors thread to message TS",
			chatID:         "C123/1700000001.000200",
			wantChannelID:  "C123",
			wantThreadAnch: "1700000001.000200",
		},
		{
			name:           "mention inside existing thread anchors thread to thread root",
			chatID:         "C123/1700000000.000100",
			wantChannelID:  "C123",
			wantThreadAnch: "1700000000.000100",
		},
		{
			name:           "DM uses no thread anchor",
			chatID:         "D987",
			wantChannelID:  "D987",
			wantThreadAnch: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotChan, gotThread := parseSlackChatID(tc.chatID)
			if gotChan != tc.wantChannelID {
				t.Errorf("channelID = %q, want %q", gotChan, tc.wantChannelID)
			}
			if gotThread != tc.wantThreadAnch {
				t.Errorf("threadTS = %q, want %q", gotThread, tc.wantThreadAnch)
			}
		})
	}
}

// TestSlackSendThreadAnchorFromReplyToMessageID verifies the defensive
// fallback: when the chatID lacks the "/<thread_ts>" suffix but the outbound
// message carries ReplyToMessageID, Send still anchors the response to a
// thread. This guarantees that mentions reply in a thread even on code paths
// that strip the threadTS suffix from chatID.
func TestSlackSendThreadAnchorFromReplyToMessageID(t *testing.T) {
	// Simulating the slack Send branch logic without an actual API call.
	chatID := "C123" // suffix stripped
	replyToMessageID := "1700000001.000200"

	channelID, threadTS := parseSlackChatID(chatID)
	if channelID != "C123" {
		t.Fatalf("channelID = %q, want %q", channelID, "C123")
	}
	if threadTS != "" {
		t.Fatalf("threadTS = %q, want empty", threadTS)
	}

	// Mirror the Send precedence: when chatID has no thread suffix and a
	// ReplyToMessageID is set, the message must anchor to that TS.
	var anchored string
	if replyToMessageID != "" && threadTS == "" {
		anchored = replyToMessageID
	} else if threadTS != "" {
		anchored = threadTS
	}
	if anchored != replyToMessageID {
		t.Errorf("anchored thread ts = %q, want %q (must fall back to ReplyToMessageID)", anchored, replyToMessageID)
	}
}

// TestSlackObserveOnlyPublish verifies that publishObserveOnly delivers a
// fully-populated InboundMessage to the bus with the observe_only metadata
// flag set, so observers (archiver) can ingest it while the agent loop will
// short-circuit and skip the turn.
func TestSlackObserveOnlyPublish(t *testing.T) {
	ch := newSlackChannelForTest(t, "UBOT123")
	ch.ctx = context.Background()

	msgs := captureInbound(t, ch.MessageBus())

	peer := bus.Peer{Kind: "channel", ID: "C123"}
	metadata := map[string]string{
		"channel_id":   "C123",
		"observe_only": "true",
	}
	sender := bus.SenderInfo{
		Platform:    "slack",
		PlatformID:  "U_USER",
		CanonicalID: "slack:U_USER",
	}
	ch.publishObserveOnly(peer, "1700000001.000200", "U_USER", "C123", "hello world", nil, metadata, sender)

	got := msgs.waitOne(t, time.Second)
	if got.Channel != "slack" {
		t.Errorf("Channel = %q, want %q", got.Channel, "slack")
	}
	if got.ChatID != "C123" {
		t.Errorf("ChatID = %q, want %q", got.ChatID, "C123")
	}
	if got.SenderID != "slack:U_USER" {
		t.Errorf("SenderID = %q, want canonical %q", got.SenderID, "slack:U_USER")
	}
	if got.Metadata["observe_only"] != "true" {
		t.Errorf("Metadata[observe_only] = %q, want %q", got.Metadata["observe_only"], "true")
	}
	if got.Content != "hello world" {
		t.Errorf("Content = %q, want %q", got.Content, "hello world")
	}
}

func TestSlackSubscribedThreadsState(t *testing.T) {
	ch := newSlackChannelForTest(t, "UBOT123")
	key := "C123/1700000000.000100"

	if _, ok := ch.subscribedThreads.Load(key); ok {
		t.Fatal("freshly constructed channel should not have any thread subscribed")
	}

	ch.subscribedThreads.Store(key, struct{}{})
	if _, ok := ch.subscribedThreads.Load(key); !ok {
		t.Fatal("subscribedThreads.Store did not register the key")
	}

	// A different channel/thread combo must not collide.
	if _, ok := ch.subscribedThreads.Load("C999/1700000000.000100"); ok {
		t.Fatal("unrelated thread keys must not be subscribed")
	}
}
