package discord

import (
	"github.com/n-seiji/ebiclaw/pkg/audio/tts"
	"github.com/n-seiji/ebiclaw/pkg/bus"
	"github.com/n-seiji/ebiclaw/pkg/channels"
	"github.com/n-seiji/ebiclaw/pkg/config"
)

func init() {
	channels.RegisterFactory("discord", func(cfg *config.Config, b *bus.MessageBus) (channels.Channel, error) {
		ch, err := NewDiscordChannel(cfg.Channels.Discord, b)
		if err == nil {
			ch.tts = tts.DetectTTS(cfg)
		}
		return ch, err
	})
}
