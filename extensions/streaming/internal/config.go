package internal

import (
	"fmt"
	"time"
)

// Config contains all configuration for the streaming extension.
type Config struct {
	// Backend configuration
	Backend         string   `json:"backend"          yaml:"backend"`          // "local", "redis", "nats"
	BackendURLs     []string `json:"backend_urls"     yaml:"backend_urls"`     // Connection URLs
	BackendUsername string   `json:"backend_username" yaml:"backend_username"` // Authentication username
	BackendPassword string   `json:"backend_password" yaml:"backend_password"` // Authentication password

	// Feature toggles
	EnableRooms            bool `json:"enable_rooms"             yaml:"enable_rooms"`
	EnableChannels         bool `json:"enable_channels"          yaml:"enable_channels"`
	EnablePresence         bool `json:"enable_presence"          yaml:"enable_presence"`
	EnableTypingIndicators bool `json:"enable_typing_indicators" yaml:"enable_typing_indicators"`
	EnableMessageHistory   bool `json:"enable_message_history"   yaml:"enable_message_history"`
	EnableDistributed      bool `json:"enable_distributed"       yaml:"enable_distributed"`

	// Connection limits
	MaxConnectionsPerUser int `json:"max_connections_per_user" yaml:"max_connections_per_user"`
	MaxRoomsPerUser       int `json:"max_rooms_per_user"       yaml:"max_rooms_per_user"`
	MaxChannelsPerUser    int `json:"max_channels_per_user"    yaml:"max_channels_per_user"`
	MaxMessageSize        int `json:"max_message_size"         yaml:"max_message_size"` // Bytes
	MaxMessagesPerSecond  int `json:"max_messages_per_second"  yaml:"max_messages_per_second"`

	// Timeouts
	PingInterval    time.Duration `json:"ping_interval"     yaml:"ping_interval"`
	PongTimeout     time.Duration `json:"pong_timeout"      yaml:"pong_timeout"`
	WriteTimeout    time.Duration `json:"write_timeout"     yaml:"write_timeout"`
	ReadBufferSize  int           `json:"read_buffer_size"  yaml:"read_buffer_size"`
	WriteBufferSize int           `json:"write_buffer_size" yaml:"write_buffer_size"`

	// Message persistence
	MessageRetention   time.Duration `json:"message_retention"     yaml:"message_retention"`
	MessageCleanup     time.Duration `json:"message_cleanup"       yaml:"message_cleanup"`
	MaxMessagesPerRoom int64         `json:"max_messages_per_room" yaml:"max_messages_per_room"`

	// Presence settings
	PresenceTimeout time.Duration `json:"presence_timeout" yaml:"presence_timeout"`
	PresenceCleanup time.Duration `json:"presence_cleanup" yaml:"presence_cleanup"`

	// Typing settings
	TypingTimeout         time.Duration `json:"typing_timeout"            yaml:"typing_timeout"`
	TypingCleanup         time.Duration `json:"typing_cleanup"            yaml:"typing_cleanup"`
	MaxTypingUsersPerRoom int           `json:"max_typing_users_per_room" yaml:"max_typing_users_per_room"`

	// Distributed settings
	NodeID            string        `json:"node_id"            yaml:"node_id"`
	HeartbeatInterval time.Duration `json:"heartbeat_interval" yaml:"heartbeat_interval"`
	NodeTimeout       time.Duration `json:"node_timeout"       yaml:"node_timeout"`

	// TLS
	TLSEnabled  bool   `json:"tls_enabled"   yaml:"tls_enabled"`
	TLSCertFile string `json:"tls_cert_file" yaml:"tls_cert_file"`
	TLSKeyFile  string `json:"tls_key_file"  yaml:"tls_key_file"`
	TLSCAFile   string `json:"tls_ca_file"   yaml:"tls_ca_file"`

	// Misc
	RequireConfig bool `json:"-" yaml:"-"` // Internal flag for config loading
}

// DefaultConfig returns default configuration.
func DefaultConfig() Config {
	return Config{
		Backend:     "local",
		BackendURLs: []string{},

		EnableRooms:            true,
		EnableChannels:         true,
		EnablePresence:         true,
		EnableTypingIndicators: true,
		EnableMessageHistory:   true,
		EnableDistributed:      false,

		MaxConnectionsPerUser: 5,
		MaxRoomsPerUser:       50,
		MaxChannelsPerUser:    100,
		MaxMessageSize:        64 * 1024, // 64 KB
		MaxMessagesPerSecond:  100,

		PingInterval:    30 * time.Second,
		PongTimeout:     10 * time.Second,
		WriteTimeout:    10 * time.Second,
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,

		MessageRetention:   30 * 24 * time.Hour, // 30 days
		MessageCleanup:     1 * time.Hour,
		MaxMessagesPerRoom: 100000,

		PresenceTimeout: 5 * time.Minute,
		PresenceCleanup: 1 * time.Minute,

		TypingTimeout:         3 * time.Second,
		TypingCleanup:         1 * time.Second,
		MaxTypingUsersPerRoom: 10,

		NodeID:            "", // Auto-generated
		HeartbeatInterval: 10 * time.Second,
		NodeTimeout:       30 * time.Second,

		TLSEnabled: false,

		RequireConfig: false,
	}
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.Backend != "local" && c.Backend != "redis" && c.Backend != "nats" && c.Backend != "" {
		return fmt.Errorf("%w: unsupported backend: %s", ErrInvalidConfig, c.Backend)
	}

	if c.EnableDistributed && c.Backend == "local" {
		return fmt.Errorf("%w: distributed mode requires redis or nats backend", ErrInvalidConfig)
	}

	if (c.Backend == "redis" || c.Backend == "nats") && len(c.BackendURLs) == 0 {
		return fmt.Errorf("%w: backend URLs required for %s backend", ErrInvalidConfig, c.Backend)
	}

	if c.MaxConnectionsPerUser < 1 {
		return fmt.Errorf("%w: max_connections_per_user must be >= 1", ErrInvalidConfig)
	}

	if c.MaxMessageSize < 1 {
		return fmt.Errorf("%w: max_message_size must be >= 1", ErrInvalidConfig)
	}

	if c.PingInterval < 1*time.Second {
		return fmt.Errorf("%w: ping_interval must be >= 1s", ErrInvalidConfig)
	}

	if c.PongTimeout < 1*time.Second {
		return fmt.Errorf("%w: pong_timeout must be >= 1s", ErrInvalidConfig)
	}

	if c.TLSEnabled {
		if c.TLSCertFile == "" || c.TLSKeyFile == "" {
			return fmt.Errorf("%w: TLS cert and key files required when TLS is enabled", ErrInvalidConfig)
		}
	}

	return nil
}

// ConfigOption is a functional option for Config.
type ConfigOption func(*Config)

// WithConfig sets the complete configuration.
func WithConfig(config Config) ConfigOption {
	return func(c *Config) {
		*c = config
	}
}

// WithBackend sets the backend type.
func WithBackend(backend string) ConfigOption {
	return func(c *Config) {
		c.Backend = backend
	}
}

// WithBackendURLs sets the backend connection URLs.
func WithBackendURLs(urls ...string) ConfigOption {
	return func(c *Config) {
		c.BackendURLs = urls
	}
}

// WithRedisBackend configures Redis backend.
func WithRedisBackend(url string) ConfigOption {
	return func(c *Config) {
		c.Backend = "redis"
		c.BackendURLs = []string{url}
		c.EnableDistributed = true
	}
}

// WithNATSBackend configures NATS backend.
func WithNATSBackend(urls ...string) ConfigOption {
	return func(c *Config) {
		c.Backend = "nats"
		c.BackendURLs = urls
		c.EnableDistributed = true
	}
}

// WithLocalBackend configures local in-memory backend.
func WithLocalBackend() ConfigOption {
	return func(c *Config) {
		c.Backend = "local"
		c.EnableDistributed = false
	}
}

// WithFeatures enables/disables features.
func WithFeatures(rooms, channels, presence, typing, history bool) ConfigOption {
	return func(c *Config) {
		c.EnableRooms = rooms
		c.EnableChannels = channels
		c.EnablePresence = presence
		c.EnableTypingIndicators = typing
		c.EnableMessageHistory = history
	}
}

// WithConnectionLimits sets connection limits.
func WithConnectionLimits(perUser, roomsPerUser, channelsPerUser int) ConfigOption {
	return func(c *Config) {
		c.MaxConnectionsPerUser = perUser
		c.MaxRoomsPerUser = roomsPerUser
		c.MaxChannelsPerUser = channelsPerUser
	}
}

// WithMessageLimits sets message limits.
func WithMessageLimits(maxSize, maxPerSecond int) ConfigOption {
	return func(c *Config) {
		c.MaxMessageSize = maxSize
		c.MaxMessagesPerSecond = maxPerSecond
	}
}

// WithTimeouts sets connection timeouts.
func WithTimeouts(ping, pong, write time.Duration) ConfigOption {
	return func(c *Config) {
		c.PingInterval = ping
		c.PongTimeout = pong
		c.WriteTimeout = write
	}
}

// WithBufferSizes sets buffer sizes.
func WithBufferSizes(read, write int) ConfigOption {
	return func(c *Config) {
		c.ReadBufferSize = read
		c.WriteBufferSize = write
	}
}

// WithMessageRetention sets message retention period.
func WithMessageRetention(retention time.Duration) ConfigOption {
	return func(c *Config) {
		c.MessageRetention = retention
	}
}

// WithPresenceTimeout sets presence timeout.
func WithPresenceTimeout(timeout time.Duration) ConfigOption {
	return func(c *Config) {
		c.PresenceTimeout = timeout
	}
}

// WithTypingTimeout sets typing indicator timeout.
func WithTypingTimeout(timeout time.Duration) ConfigOption {
	return func(c *Config) {
		c.TypingTimeout = timeout
	}
}

// WithNodeID sets the node ID for distributed mode.
func WithNodeID(nodeID string) ConfigOption {
	return func(c *Config) {
		c.NodeID = nodeID
	}
}

// WithTLS enables TLS with certificate files.
func WithTLS(certFile, keyFile, caFile string) ConfigOption {
	return func(c *Config) {
		c.TLSEnabled = true
		c.TLSCertFile = certFile
		c.TLSKeyFile = keyFile
		c.TLSCAFile = caFile
	}
}

// WithAuthentication sets backend authentication credentials.
func WithAuthentication(username, password string) ConfigOption {
	return func(c *Config) {
		c.BackendUsername = username
		c.BackendPassword = password
	}
}

// WithRequireConfig requires configuration from ConfigManager.
func WithRequireConfig(require bool) ConfigOption {
	return func(c *Config) {
		c.RequireConfig = require
	}
}
