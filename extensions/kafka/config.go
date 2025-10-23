package kafka

import (
	"fmt"
	"time"

	"github.com/IBM/sarama"
)

// Config contains configuration for the Kafka extension
type Config struct {
	// Connection settings
	Brokers       []string      `json:"brokers" yaml:"brokers" mapstructure:"brokers"`
	ClientID      string        `json:"client_id" yaml:"client_id" mapstructure:"client_id"`
	Version       string        `json:"version" yaml:"version" mapstructure:"version"` // Kafka version (e.g., "3.0.0")
	DialTimeout   time.Duration `json:"dial_timeout" yaml:"dial_timeout" mapstructure:"dial_timeout"`
	ReadTimeout   time.Duration `json:"read_timeout" yaml:"read_timeout" mapstructure:"read_timeout"`
	WriteTimeout  time.Duration `json:"write_timeout" yaml:"write_timeout" mapstructure:"write_timeout"`
	KeepAlive     time.Duration `json:"keep_alive" yaml:"keep_alive" mapstructure:"keep_alive"`

	// TLS/SASL
	EnableTLS     bool   `json:"enable_tls" yaml:"enable_tls" mapstructure:"enable_tls"`
	TLSCertFile   string `json:"tls_cert_file,omitempty" yaml:"tls_cert_file,omitempty" mapstructure:"tls_cert_file"`
	TLSKeyFile    string `json:"tls_key_file,omitempty" yaml:"tls_key_file,omitempty" mapstructure:"tls_key_file"`
	TLSCAFile     string `json:"tls_ca_file,omitempty" yaml:"tls_ca_file,omitempty" mapstructure:"tls_ca_file"`
	TLSSkipVerify bool   `json:"tls_skip_verify" yaml:"tls_skip_verify" mapstructure:"tls_skip_verify"`

	EnableSASL   bool   `json:"enable_sasl" yaml:"enable_sasl" mapstructure:"enable_sasl"`
	SASLMechanism string `json:"sasl_mechanism,omitempty" yaml:"sasl_mechanism,omitempty" mapstructure:"sasl_mechanism"` // PLAIN, SCRAM-SHA-256, SCRAM-SHA-512
	SASLUsername  string `json:"sasl_username,omitempty" yaml:"sasl_username,omitempty" mapstructure:"sasl_username"`
	SASLPassword  string `json:"sasl_password,omitempty" yaml:"sasl_password,omitempty" mapstructure:"sasl_password"`

	// Producer settings
	ProducerEnabled         bool          `json:"producer_enabled" yaml:"producer_enabled" mapstructure:"producer_enabled"`
	ProducerMaxMessageBytes int           `json:"producer_max_message_bytes" yaml:"producer_max_message_bytes" mapstructure:"producer_max_message_bytes"`
	ProducerCompression     string        `json:"producer_compression" yaml:"producer_compression" mapstructure:"producer_compression"` // none, gzip, snappy, lz4, zstd
	ProducerFlushMessages   int           `json:"producer_flush_messages" yaml:"producer_flush_messages" mapstructure:"producer_flush_messages"`
	ProducerFlushFrequency  time.Duration `json:"producer_flush_frequency" yaml:"producer_flush_frequency" mapstructure:"producer_flush_frequency"`
	ProducerRetryMax        int           `json:"producer_retry_max" yaml:"producer_retry_max" mapstructure:"producer_retry_max"`
	ProducerIdempotent      bool          `json:"producer_idempotent" yaml:"producer_idempotent" mapstructure:"producer_idempotent"`
	ProducerAcks            string        `json:"producer_acks" yaml:"producer_acks" mapstructure:"producer_acks"` // none, local, all

	// Consumer settings
	ConsumerEnabled   bool          `json:"consumer_enabled" yaml:"consumer_enabled" mapstructure:"consumer_enabled"`
	ConsumerGroupID   string        `json:"consumer_group_id,omitempty" yaml:"consumer_group_id,omitempty" mapstructure:"consumer_group_id"`
	ConsumerOffsets   string        `json:"consumer_offsets" yaml:"consumer_offsets" mapstructure:"consumer_offsets"` // newest, oldest
	ConsumerMaxWait   time.Duration `json:"consumer_max_wait" yaml:"consumer_max_wait" mapstructure:"consumer_max_wait"`
	ConsumerFetchMin  int32         `json:"consumer_fetch_min" yaml:"consumer_fetch_min" mapstructure:"consumer_fetch_min"`
	ConsumerFetchMax  int32         `json:"consumer_fetch_max" yaml:"consumer_fetch_max" mapstructure:"consumer_fetch_max"`
	ConsumerIsolation string        `json:"consumer_isolation" yaml:"consumer_isolation" mapstructure:"consumer_isolation"` // read_uncommitted, read_committed

	// Consumer group settings
	ConsumerGroupRebalance string        `json:"consumer_group_rebalance" yaml:"consumer_group_rebalance" mapstructure:"consumer_group_rebalance"` // range, roundrobin, sticky
	ConsumerGroupSession   time.Duration `json:"consumer_group_session" yaml:"consumer_group_session" mapstructure:"consumer_group_session"`
	ConsumerGroupHeartbeat time.Duration `json:"consumer_group_heartbeat" yaml:"consumer_group_heartbeat" mapstructure:"consumer_group_heartbeat"`

	// Metadata settings
	MetadataRetryMax       int           `json:"metadata_retry_max" yaml:"metadata_retry_max" mapstructure:"metadata_retry_max"`
	MetadataRetryBackoff   time.Duration `json:"metadata_retry_backoff" yaml:"metadata_retry_backoff" mapstructure:"metadata_retry_backoff"`
	MetadataRefreshFreq    time.Duration `json:"metadata_refresh_freq" yaml:"metadata_refresh_freq" mapstructure:"metadata_refresh_freq"`
	MetadataFullRefresh    bool          `json:"metadata_full_refresh" yaml:"metadata_full_refresh" mapstructure:"metadata_full_refresh"`

	// Observability
	EnableMetrics bool `json:"enable_metrics" yaml:"enable_metrics" mapstructure:"enable_metrics"`
	EnableTracing bool `json:"enable_tracing" yaml:"enable_tracing" mapstructure:"enable_tracing"`
	EnableLogging bool `json:"enable_logging" yaml:"enable_logging" mapstructure:"enable_logging"`

	// Config loading flags
	RequireConfig bool `json:"-" yaml:"-" mapstructure:"-"`
}

// DefaultConfig returns default Kafka configuration
func DefaultConfig() Config {
	return Config{
		Brokers:                 []string{"localhost:9092"},
		ClientID:                "forge-kafka-client",
		Version:                 "3.0.0",
		DialTimeout:             30 * time.Second,
		ReadTimeout:             30 * time.Second,
		WriteTimeout:            30 * time.Second,
		KeepAlive:               0, // Disabled by default
		EnableTLS:               false,
		TLSSkipVerify:           false,
		EnableSASL:              false,
		SASLMechanism:           "PLAIN",
		ProducerEnabled:         true,
		ProducerMaxMessageBytes: 1000000, // 1MB
		ProducerCompression:     "snappy",
		ProducerFlushMessages:   10,
		ProducerFlushFrequency:  100 * time.Millisecond,
		ProducerRetryMax:        3,
		ProducerIdempotent:      false,
		ProducerAcks:            "local", // Wait for leader
		ConsumerEnabled:         true,
		ConsumerOffsets:         "newest",
		ConsumerMaxWait:         500 * time.Millisecond,
		ConsumerFetchMin:        1,
		ConsumerFetchMax:        1024 * 1024, // 1MB
		ConsumerIsolation:       "read_uncommitted",
		ConsumerGroupRebalance:  "range",
		ConsumerGroupSession:    10 * time.Second,
		ConsumerGroupHeartbeat:  3 * time.Second,
		MetadataRetryMax:        3,
		MetadataRetryBackoff:    250 * time.Millisecond,
		MetadataRefreshFreq:     10 * time.Minute,
		MetadataFullRefresh:     true,
		EnableMetrics:           true,
		EnableTracing:           true,
		EnableLogging:           true,
		RequireConfig:           false,
	}
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if len(c.Brokers) == 0 {
		return fmt.Errorf("brokers is required")
	}

	if c.ClientID == "" {
		return fmt.Errorf("client_id is required")
	}

	if c.Version == "" {
		return fmt.Errorf("version is required")
	}

	if c.EnableTLS && !c.TLSSkipVerify {
		if c.TLSCertFile == "" || c.TLSKeyFile == "" {
			return fmt.Errorf("tls requires cert_file and key_file when not skipping verification")
		}
	}

	if c.EnableSASL {
		if c.SASLUsername == "" || c.SASLPassword == "" {
			return fmt.Errorf("sasl requires username and password")
		}

		validMechanisms := map[string]bool{
			"PLAIN":          true,
			"SCRAM-SHA-256":  true,
			"SCRAM-SHA-512":  true,
		}

		if !validMechanisms[c.SASLMechanism] {
			return fmt.Errorf("invalid sasl_mechanism: %s", c.SASLMechanism)
		}
	}

	validCompression := map[string]bool{
		"none":   true,
		"gzip":   true,
		"snappy": true,
		"lz4":    true,
		"zstd":   true,
	}

	if !validCompression[c.ProducerCompression] {
		return fmt.Errorf("invalid producer_compression: %s", c.ProducerCompression)
	}

	validAcks := map[string]bool{
		"none":  true,
		"local": true,
		"all":   true,
	}

	if !validAcks[c.ProducerAcks] {
		return fmt.Errorf("invalid producer_acks: %s", c.ProducerAcks)
	}

	validOffsets := map[string]bool{
		"newest": true,
		"oldest": true,
	}

	if !validOffsets[c.ConsumerOffsets] {
		return fmt.Errorf("invalid consumer_offsets: %s", c.ConsumerOffsets)
	}

	validIsolation := map[string]bool{
		"read_uncommitted": true,
		"read_committed":   true,
	}

	if !validIsolation[c.ConsumerIsolation] {
		return fmt.Errorf("invalid consumer_isolation: %s", c.ConsumerIsolation)
	}

	validRebalance := map[string]bool{
		"range":      true,
		"roundrobin": true,
		"sticky":     true,
	}

	if !validRebalance[c.ConsumerGroupRebalance] {
		return fmt.Errorf("invalid consumer_group_rebalance: %s", c.ConsumerGroupRebalance)
	}

	return nil
}

// ToSaramaConfig converts to Sarama configuration
func (c *Config) ToSaramaConfig() (*sarama.Config, error) {
	config := sarama.NewConfig()

	// Parse version
	version, err := sarama.ParseKafkaVersion(c.Version)
	if err != nil {
		return nil, fmt.Errorf("invalid kafka version: %w", err)
	}
	config.Version = version

	config.ClientID = c.ClientID
	config.Net.DialTimeout = c.DialTimeout
	config.Net.ReadTimeout = c.ReadTimeout
	config.Net.WriteTimeout = c.WriteTimeout
	config.Net.KeepAlive = c.KeepAlive

	// TLS configuration handled separately in client.go

	// Producer settings
	if c.ProducerEnabled {
		config.Producer.Return.Successes = true
		config.Producer.Return.Errors = true
		config.Producer.MaxMessageBytes = c.ProducerMaxMessageBytes
		config.Producer.Flush.Messages = c.ProducerFlushMessages
		config.Producer.Flush.Frequency = c.ProducerFlushFrequency
		config.Producer.Retry.Max = c.ProducerRetryMax
		config.Producer.Idempotent = c.ProducerIdempotent

		// Compression
		switch c.ProducerCompression {
		case "none":
			config.Producer.Compression = sarama.CompressionNone
		case "gzip":
			config.Producer.Compression = sarama.CompressionGZIP
		case "snappy":
			config.Producer.Compression = sarama.CompressionSnappy
		case "lz4":
			config.Producer.Compression = sarama.CompressionLZ4
		case "zstd":
			config.Producer.Compression = sarama.CompressionZSTD
		}

		// Acks
		switch c.ProducerAcks {
		case "none":
			config.Producer.RequiredAcks = sarama.NoResponse
		case "local":
			config.Producer.RequiredAcks = sarama.WaitForLocal
		case "all":
			config.Producer.RequiredAcks = sarama.WaitForAll
		}
	}

	// Consumer settings
	if c.ConsumerEnabled {
		config.Consumer.Return.Errors = true
		config.Consumer.MaxWaitTime = c.ConsumerMaxWait
		config.Consumer.Fetch.Min = c.ConsumerFetchMin
		config.Consumer.Fetch.Max = c.ConsumerFetchMax

		// Offsets
		switch c.ConsumerOffsets {
		case "newest":
			config.Consumer.Offsets.Initial = sarama.OffsetNewest
		case "oldest":
			config.Consumer.Offsets.Initial = sarama.OffsetOldest
		}

		// Isolation
		switch c.ConsumerIsolation {
		case "read_uncommitted":
			config.Consumer.IsolationLevel = sarama.ReadUncommitted
		case "read_committed":
			config.Consumer.IsolationLevel = sarama.ReadCommitted
		}

		// Consumer group settings
		config.Consumer.Group.Session.Timeout = c.ConsumerGroupSession
		config.Consumer.Group.Heartbeat.Interval = c.ConsumerGroupHeartbeat

		// Rebalance strategy
		switch c.ConsumerGroupRebalance {
		case "range":
			config.Consumer.Group.Rebalance.Strategy = sarama.NewBalanceStrategyRange()
		case "roundrobin":
			config.Consumer.Group.Rebalance.Strategy = sarama.NewBalanceStrategyRoundRobin()
		case "sticky":
			config.Consumer.Group.Rebalance.Strategy = sarama.NewBalanceStrategySticky()
		}
	}

	// Metadata settings
	config.Metadata.Retry.Max = c.MetadataRetryMax
	config.Metadata.Retry.Backoff = c.MetadataRetryBackoff
	config.Metadata.RefreshFrequency = c.MetadataRefreshFreq
	config.Metadata.Full = c.MetadataFullRefresh

	return config, nil
}

// ConfigOption is a functional option for Config
type ConfigOption func(*Config)

func WithBrokers(brokers ...string) ConfigOption {
	return func(c *Config) { c.Brokers = brokers }
}

func WithClientID(clientID string) ConfigOption {
	return func(c *Config) { c.ClientID = clientID }
}

func WithVersion(version string) ConfigOption {
	return func(c *Config) { c.Version = version }
}

func WithTLS(certFile, keyFile, caFile string, skipVerify bool) ConfigOption {
	return func(c *Config) {
		c.EnableTLS = true
		c.TLSCertFile = certFile
		c.TLSKeyFile = keyFile
		c.TLSCAFile = caFile
		c.TLSSkipVerify = skipVerify
	}
}

func WithSASL(mechanism, username, password string) ConfigOption {
	return func(c *Config) {
		c.EnableSASL = true
		c.SASLMechanism = mechanism
		c.SASLUsername = username
		c.SASLPassword = password
	}
}

func WithProducer(enabled bool) ConfigOption {
	return func(c *Config) { c.ProducerEnabled = enabled }
}

func WithConsumer(enabled bool) ConfigOption {
	return func(c *Config) { c.ConsumerEnabled = enabled }
}

func WithConsumerGroup(groupID string) ConfigOption {
	return func(c *Config) {
		c.ConsumerEnabled = true
		c.ConsumerGroupID = groupID
	}
}

func WithCompression(compression string) ConfigOption {
	return func(c *Config) { c.ProducerCompression = compression }
}

func WithIdempotent(enabled bool) ConfigOption {
	return func(c *Config) { c.ProducerIdempotent = enabled }
}

func WithMetrics(enable bool) ConfigOption {
	return func(c *Config) { c.EnableMetrics = enable }
}

func WithTracing(enable bool) ConfigOption {
	return func(c *Config) { c.EnableTracing = enable }
}

func WithRequireConfig(require bool) ConfigOption {
	return func(c *Config) { c.RequireConfig = require }
}

func WithConfig(config Config) ConfigOption {
	return func(c *Config) { *c = config }
}
