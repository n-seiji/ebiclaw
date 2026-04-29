package archiver

import (
	"context"
	"time"

	"github.com/sipeed/picoclaw/pkg/bus"
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
		ThreadID:  m.Metadata["thread_id"],
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
