package streaming

import (
	"time"

	"github.com/xraph/forge/extensions/streaming/internal"
)

// DI container keys for streaming extension services.
const (
	// ManagerKey is the DI key for the streaming manager.
	ManagerKey = "streaming"
)

func DefaultConfig() Config {
	return internal.DefaultConfig()
}

func WithConfig(config Config) ConfigOption {
	return internal.WithConfig(config)
}

func WithAuthentication(username, password string) ConfigOption {
	return internal.WithAuthentication(username, password)
}

func WithBackend(backend string) ConfigOption {
	return internal.WithBackend(backend)
}

func WithBackendURLs(urls ...string) ConfigOption {
	return internal.WithBackendURLs(urls...)
}

func WithRedisBackend(url string) ConfigOption {
	return internal.WithRedisBackend(url)
}

func WithNATSBackend(urls ...string) ConfigOption {
	return internal.WithNATSBackend(urls...)
}

func WithFeatures(rooms, channels, presence, typing, history bool) ConfigOption {
	return internal.WithFeatures(rooms, channels, presence, typing, history)
}

func WithConnectionLimits(perUser, roomsPerUser, channelsPerUser int) ConfigOption {
	return internal.WithConnectionLimits(perUser, roomsPerUser, channelsPerUser)
}

func WithMessageLimits(maxSize, maxPerSecond int) ConfigOption {
	return internal.WithMessageLimits(maxSize, maxPerSecond)
}

func WithTimeouts(ping, pong, write time.Duration) ConfigOption {
	return internal.WithTimeouts(ping, pong, write)
}

func WithBufferSizes(read, write int) ConfigOption {
	return internal.WithBufferSizes(read, write)
}

func WithMessageRetention(retention time.Duration) ConfigOption {
	return internal.WithMessageRetention(retention)
}

func WithPresenceTimeout(timeout time.Duration) ConfigOption {
	return internal.WithPresenceTimeout(timeout)
}

func WithTypingTimeout(timeout time.Duration) ConfigOption {
	return internal.WithTypingTimeout(timeout)
}

func WithNodeID(nodeID string) ConfigOption {
	return internal.WithNodeID(nodeID)
}

func WithTLS(certFile, keyFile, caFile string) ConfigOption {
	return internal.WithTLS(certFile, keyFile, caFile)
}

func WithRequireConfig(require bool) ConfigOption {
	return internal.WithRequireConfig(require)
}

func WithLocalBackend() ConfigOption {
	return internal.WithLocalBackend()
}
