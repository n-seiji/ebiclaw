package archiver

import (
	"context"
	"strings"
	"time"

	"github.com/n-seiji/ebiclaw/pkg/bus"
)

// Observer adapts pkg/bus.Observer to a RawWriter.
type Observer struct {
	rw *RawWriter
}

func NewObserver(rw *RawWriter) *Observer {
	return &Observer{rw: rw}
}

func (o *Observer) OnInbound(_ context.Context, m bus.InboundMessage) {
	rec := RawRecord{
		Timestamp: time.Now().UTC(),
		Role:      "user",
		Platform:  pickPlatform(m.Sender.Platform, m.Channel),
		ChatID:    m.ChatID,
		ThreadID:  pickThreadID(m.ChatID, m.Metadata),
		MessageID: m.MessageID,
		Sender: Sender{
			PlatformID:  m.Sender.PlatformID,
			Username:    m.Sender.Username,
			DisplayName: m.Sender.DisplayName,
		},
		Text: m.Content,
	}
	_ = o.rw.Append(rec)
}

func (o *Observer) OnOutbound(_ context.Context, m bus.OutboundMessage) {
	rec := RawRecord{
		Timestamp: time.Now().UTC(),
		Role:      "assistant",
		Platform:  m.Channel,
		ChatID:    m.ChatID,
		ThreadID:  pickThreadID(m.ChatID, m.Metadata),
		Text:      m.Content,
	}
	_ = o.rw.Append(rec)
}

func pickPlatform(senderPlatform, channel string) string {
	if senderPlatform != "" {
		return senderPlatform
	}
	return channel
}

func pickThreadID(chatID string, metadata map[string]string) string {
	if metadata != nil {
		if threadID := metadata["thread_id"]; threadID != "" {
			return threadID
		}
		if threadTS := metadata["thread_ts"]; threadTS != "" {
			return threadTS
		}
	}
	if _, threadID, ok := strings.Cut(chatID, "/"); ok {
		return threadID
	}
	return ""
}
