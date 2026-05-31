package pico

import (
	"github.com/n-seiji/ebiclaw/pkg/bus"
	"github.com/n-seiji/ebiclaw/pkg/channels"
	"github.com/n-seiji/ebiclaw/pkg/config"
)

func init() {
	channels.RegisterFactory("pico", func(cfg *config.Config, b *bus.MessageBus) (channels.Channel, error) {
		return NewPicoChannel(cfg.Channels.Pico, b)
	})
	channels.RegisterFactory("pico_client", func(cfg *config.Config, b *bus.MessageBus) (channels.Channel, error) {
		return NewPicoClientChannel(cfg.Channels.PicoClient, b)
	})
}
