package contract

import (
	"context"

	"github.com/xraph/forge/extensions/dashboard/contract"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

func configHandler(cfg ConfigProvider) func(ctx context.Context, _ struct{}, _ contract.Principal) (ConfigSummary, error) {
	return func(_ context.Context, _ struct{}, _ contract.Principal) (ConfigSummary, error) {
		c := cfg()
		return projectConfig(c), nil
	}
}

func projectConfig(c streaming.Config) ConfigSummary {
	return ConfigSummary{
		BackendType: c.Backend,
		Distributed: c.EnableDistributed,
		NodeID:      c.NodeID,
		Features: map[string]any{
			"rooms":             c.EnableRooms,
			"channels":          c.EnableChannels,
			"presence":          c.EnablePresence,
			"typingIndicators":  c.EnableTypingIndicators,
			"messageHistory":    c.EnableMessageHistory,
			"sessionResumption": c.EnableSessionResumption,
			"loadBalancer":      c.EnableLoadBalancer,
			"tls":               c.TLSEnabled,
		},
		Limits: map[string]any{
			"maxConnectionsPerUser": c.MaxConnectionsPerUser,
			"maxRoomsPerUser":       c.MaxRoomsPerUser,
			"maxChannelsPerUser":    c.MaxChannelsPerUser,
			"maxMessageSize":        c.MaxMessageSize,
			"maxMessagesPerSecond":  c.MaxMessagesPerSecond,
			"maxMessagesPerRoom":    c.MaxMessagesPerRoom,
		},
		Timeouts: map[string]any{
			"pingInterval":     c.PingInterval.String(),
			"pongTimeout":      c.PongTimeout.String(),
			"writeTimeout":     c.WriteTimeout.String(),
			"presenceTimeout":  c.PresenceTimeout.String(),
			"typingTimeout":    c.TypingTimeout.String(),
			"messageRetention": c.MessageRetention.String(),
		},
	}
}
