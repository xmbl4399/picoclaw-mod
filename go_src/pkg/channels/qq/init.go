package qq

import (
	"github.com/sipeed/picoclaw/pkg/audio/tts"
	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/channels"
	"github.com/sipeed/picoclaw/pkg/config"
)

func init() {
	channels.RegisterFactory(
		config.ChannelQQ,
		func(channelName, channelType string, cfg *config.Config, b *bus.MessageBus) (channels.Channel, error) {
			bc := cfg.Channels[channelName]
			decoded, err := bc.GetDecoded()
			if err != nil {
				return nil, err
			}
			c, ok := decoded.(*config.QQSettings)
			if !ok {
				return nil, channels.ErrSendFailed
			}
			ch, err := NewQQChannel(bc, c, b)
			if err != nil {
				return nil, err
			}
			// Inject TTS provider if configured globally
			ch.ttsProvider = tts.DetectTTS(cfg)
			return ch, nil
		},
	)
}
