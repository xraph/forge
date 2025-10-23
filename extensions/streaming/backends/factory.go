package backends

import (
	"fmt"

	"github.com/xraph/forge/extensions/streaming/backends/local"
	"github.com/xraph/forge/extensions/streaming/backends/redis"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// BackendConfig holds configuration for creating backend stores.
type BackendConfig struct {
	Type     string
	URLs     []string
	Username string
	Password string
	NodeID   string
	Prefix   string

	// TLS
	TLSEnabled  bool
	TLSCertFile string
	TLSKeyFile  string
	TLSCAFile   string
}

// CreateStores creates backend stores based on the backend type and configuration.
func CreateStores(config BackendConfig) (
	roomStore streaming.RoomStore,
	channelStore streaming.ChannelStore,
	messageStore streaming.MessageStore,
	presenceStore streaming.PresenceStore,
	typingStore streaming.TypingStore,
	distributed streaming.DistributedBackend,
	err error,
) {
	switch config.Type {
	case "local", "":
		roomStore = local.NewRoomStore()
		channelStore = local.NewChannelStore()
		messageStore = local.NewMessageStore()
		presenceStore = local.NewPresenceStore()
		typingStore = local.NewTypingStore()
		distributed = nil // No distributed backend for local

	case "redis":
		// Create Redis client
		client, err := redis.NewClient(redis.ClientConfig{
			URLs:        config.URLs,
			Username:    config.Username,
			Password:    config.Password,
			TLSEnabled:  config.TLSEnabled,
			TLSCertFile: config.TLSCertFile,
			TLSKeyFile:  config.TLSKeyFile,
			TLSCAFile:   config.TLSCAFile,
			Prefix:      config.Prefix,
		})
		if err != nil {
			return nil, nil, nil, nil, nil, nil, fmt.Errorf("failed to create Redis client: %w", err)
		}

		// Create stores
		rs, cs, ms, ps, ts, ds := redis.CreateStores(client, config.Prefix)
		roomStore = rs.(streaming.RoomStore)
		channelStore = cs.(streaming.ChannelStore)
		messageStore = ms.(streaming.MessageStore)
		presenceStore = ps.(streaming.PresenceStore)
		typingStore = ts.(streaming.TypingStore)
		distributed = ds.(streaming.DistributedBackend)

	case "nats":
		// TODO: Implement NATS backends
		err = ErrBackendNotImplemented

	default:
		err = ErrUnsupportedBackend
	}

	return
}

// Backend errors
var (
	ErrBackendNotImplemented = streaming.ErrBackendNotConnected
	ErrUnsupportedBackend    = streaming.ErrInvalidConfig
)
