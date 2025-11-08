package redis

import (
	"crypto/tls"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// ClientConfig holds Redis client configuration.
type ClientConfig struct {
	URLs     []string
	Username string
	Password string
	DB       int

	// TLS
	TLSEnabled  bool
	TLSCertFile string
	TLSKeyFile  string
	TLSCAFile   string

	// Prefix for all keys
	Prefix string
}

// NewClient creates a new Redis client from configuration.
func NewClient(config ClientConfig) (*redis.Client, error) {
	if len(config.URLs) == 0 {
		return nil, errors.New("no Redis URLs provided")
	}

	// Parse first URL (for now, simple single-node setup)
	url := config.URLs[0]

	opts := &redis.Options{
		Addr:     url,
		Username: config.Username,
		Password: config.Password,
		DB:       config.DB,
	}

	// Configure TLS if enabled
	if config.TLSEnabled {
		tlsConfig := &tls.Config{
			MinVersion: tls.VersionTLS12,
		}

		// Load certificates if provided
		if config.TLSCertFile != "" && config.TLSKeyFile != "" {
			cert, err := tls.LoadX509KeyPair(config.TLSCertFile, config.TLSKeyFile)
			if err != nil {
				return nil, fmt.Errorf("failed to load TLS certificates: %w", err)
			}

			tlsConfig.Certificates = []tls.Certificate{cert}
		}

		opts.TLSConfig = tlsConfig
	}

	return redis.NewClient(opts), nil
}

// CreateStores creates all Redis-backed stores from a single client.
func CreateStores(client *redis.Client, prefix string) (
	roomStore any,
	channelStore any,
	messageStore any,
	presenceStore any,
	typingStore any,
	distributed any,
) {
	if prefix == "" {
		prefix = "streaming"
	}

	roomStore = NewRoomStore(client, prefix+":rooms")
	channelStore = NewChannelStore(client, prefix+":channels")
	messageStore = NewMessageStore(client, prefix+":messages")
	presenceStore = NewPresenceStore(client, prefix+":presence")
	typingStore = NewTypingStore(client, prefix+":typing")
	distributed = NewDistributedBackend(client, "", prefix+":distributed")

	return
}
