package kafka

import (
	"testing"
	"time"

	"github.com/IBM/sarama"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if len(config.Brokers) != 1 || config.Brokers[0] != "localhost:9092" {
		t.Errorf("expected default broker 'localhost:9092', got %v", config.Brokers)
	}

	if config.ClientID != "forge-kafka-client" {
		t.Errorf("expected default client_id 'forge-kafka-client', got %s", config.ClientID)
	}

	if config.Version != "3.0.0" {
		t.Errorf("expected default version '3.0.0', got %s", config.Version)
	}

	if !config.ProducerEnabled {
		t.Error("expected producer_enabled to be true")
	}

	if !config.ConsumerEnabled {
		t.Error("expected consumer_enabled to be true")
	}

	if config.ProducerCompression != "snappy" {
		t.Errorf("expected producer_compression 'snappy', got %s", config.ProducerCompression)
	}

	if config.ConsumerOffsets != "newest" {
		t.Errorf("expected consumer_offsets 'newest', got %s", config.ConsumerOffsets)
	}

	if !config.EnableMetrics {
		t.Error("expected enable_metrics to be true")
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      Config
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid config",
			config: Config{
				Brokers:             []string{"localhost:9092"},
				ClientID:            "test-client",
				Version:             "3.0.0",
				ProducerCompression: "snappy",
				ProducerAcks:        "local",
				ConsumerOffsets:     "newest",
				ConsumerIsolation:   "read_uncommitted",
				ConsumerGroupRebalance: "range",
			},
			expectError: false,
		},
		{
			name: "missing brokers",
			config: Config{
				ClientID: "test-client",
				Version:  "3.0.0",
			},
			expectError: true,
			errorMsg:    "brokers is required",
		},
		{
			name: "missing client_id",
			config: Config{
				Brokers: []string{"localhost:9092"},
				Version: "3.0.0",
			},
			expectError: true,
			errorMsg:    "client_id is required",
		},
		{
			name: "missing version",
			config: Config{
				Brokers:  []string{"localhost:9092"},
				ClientID: "test-client",
			},
			expectError: true,
			errorMsg:    "version is required",
		},
		{
			name: "tls without certs",
			config: Config{
				Brokers:       []string{"localhost:9092"},
				ClientID:      "test-client",
				Version:       "3.0.0",
				EnableTLS:     true,
				TLSSkipVerify: false,
			},
			expectError: true,
			errorMsg:    "tls requires cert_file and key_file when not skipping verification",
		},
		{
			name: "sasl without credentials",
			config: Config{
				Brokers:    []string{"localhost:9092"},
				ClientID:   "test-client",
				Version:    "3.0.0",
				EnableSASL: true,
			},
			expectError: true,
			errorMsg:    "sasl requires username and password",
		},
		{
			name: "invalid sasl mechanism",
			config: Config{
				Brokers:       []string{"localhost:9092"},
				ClientID:      "test-client",
				Version:       "3.0.0",
				EnableSASL:    true,
				SASLMechanism: "INVALID",
				SASLUsername:  "user",
				SASLPassword:  "pass",
			},
			expectError: true,
			errorMsg:    "invalid sasl_mechanism: INVALID",
		},
		{
			name: "invalid compression",
			config: Config{
				Brokers:             []string{"localhost:9092"},
				ClientID:            "test-client",
				Version:             "3.0.0",
				ProducerCompression: "invalid",
			},
			expectError: true,
			errorMsg:    "invalid producer_compression: invalid",
		},
		{
			name: "invalid producer acks",
			config: Config{
				Brokers:             []string{"localhost:9092"},
				ClientID:            "test-client",
				Version:             "3.0.0",
				ProducerCompression: "snappy",
				ProducerAcks:        "invalid",
			},
			expectError: true,
			errorMsg:    "invalid producer_acks: invalid",
		},
		{
			name: "invalid consumer offsets",
			config: Config{
				Brokers:             []string{"localhost:9092"},
				ClientID:            "test-client",
				Version:             "3.0.0",
				ProducerCompression: "snappy",
				ProducerAcks:        "local",
				ConsumerOffsets:     "invalid",
			},
			expectError: true,
			errorMsg:    "invalid consumer_offsets: invalid",
		},
		{
			name: "invalid consumer isolation",
			config: Config{
				Brokers:             []string{"localhost:9092"},
				ClientID:            "test-client",
				Version:             "3.0.0",
				ProducerCompression: "snappy",
				ProducerAcks:        "local",
				ConsumerOffsets:     "newest",
				ConsumerIsolation:   "invalid",
			},
			expectError: true,
			errorMsg:    "invalid consumer_isolation: invalid",
		},
		{
			name: "invalid consumer group rebalance",
			config: Config{
				Brokers:             []string{"localhost:9092"},
				ClientID:            "test-client",
				Version:             "3.0.0",
				ProducerCompression: "snappy",
				ProducerAcks:        "local",
				ConsumerOffsets:     "newest",
				ConsumerIsolation:   "read_uncommitted",
				ConsumerGroupRebalance: "invalid",
			},
			expectError: true,
			errorMsg:    "invalid consumer_group_rebalance: invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error '%s', got nil", tt.errorMsg)
				} else if err.Error() != tt.errorMsg {
					t.Errorf("expected error '%s', got '%s'", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestToSaramaConfig(t *testing.T) {
	config := DefaultConfig()

	saramaConfig, err := config.ToSaramaConfig()
	if err != nil {
		t.Fatalf("failed to convert to sarama config: %v", err)
	}

	if saramaConfig.ClientID != config.ClientID {
		t.Errorf("expected client_id %s, got %s", config.ClientID, saramaConfig.ClientID)
	}

	if saramaConfig.Producer.Compression != sarama.CompressionSnappy {
		t.Error("expected compression to be snappy")
	}

	if saramaConfig.Producer.RequiredAcks != sarama.WaitForLocal {
		t.Error("expected acks to be local")
	}

	if saramaConfig.Consumer.Offsets.Initial != sarama.OffsetNewest {
		t.Error("expected offsets to be newest")
	}
}

func TestToSaramaConfigCompressionTypes(t *testing.T) {
	tests := []struct {
		compression string
		expected    sarama.CompressionCodec
	}{
		{"none", sarama.CompressionNone},
		{"gzip", sarama.CompressionGZIP},
		{"snappy", sarama.CompressionSnappy},
		{"lz4", sarama.CompressionLZ4},
		{"zstd", sarama.CompressionZSTD},
	}

	for _, tt := range tests {
		t.Run(tt.compression, func(t *testing.T) {
			config := DefaultConfig()
			config.ProducerCompression = tt.compression

			saramaConfig, err := config.ToSaramaConfig()
			if err != nil {
				t.Fatalf("failed to convert config: %v", err)
			}

			if saramaConfig.Producer.Compression != tt.expected {
				t.Errorf("expected compression %v, got %v", tt.expected, saramaConfig.Producer.Compression)
			}
		})
	}
}

func TestToSaramaConfigAcksTypes(t *testing.T) {
	tests := []struct {
		acks     string
		expected sarama.RequiredAcks
	}{
		{"none", sarama.NoResponse},
		{"local", sarama.WaitForLocal},
		{"all", sarama.WaitForAll},
	}

	for _, tt := range tests {
		t.Run(tt.acks, func(t *testing.T) {
			config := DefaultConfig()
			config.ProducerAcks = tt.acks

			saramaConfig, err := config.ToSaramaConfig()
			if err != nil {
				t.Fatalf("failed to convert config: %v", err)
			}

			if saramaConfig.Producer.RequiredAcks != tt.expected {
				t.Errorf("expected acks %v, got %v", tt.expected, saramaConfig.Producer.RequiredAcks)
			}
		})
	}
}

func TestToSaramaConfigOffsetsTypes(t *testing.T) {
	tests := []struct {
		offsets  string
		expected int64
	}{
		{"newest", sarama.OffsetNewest},
		{"oldest", sarama.OffsetOldest},
	}

	for _, tt := range tests {
		t.Run(tt.offsets, func(t *testing.T) {
			config := DefaultConfig()
			config.ConsumerOffsets = tt.offsets

			saramaConfig, err := config.ToSaramaConfig()
			if err != nil {
				t.Fatalf("failed to convert config: %v", err)
			}

			if saramaConfig.Consumer.Offsets.Initial != tt.expected {
				t.Errorf("expected offsets %v, got %v", tt.expected, saramaConfig.Consumer.Offsets.Initial)
			}
		})
	}
}

func TestToSaramaConfigIsolationTypes(t *testing.T) {
	tests := []struct {
		isolation string
		expected  sarama.IsolationLevel
	}{
		{"read_uncommitted", sarama.ReadUncommitted},
		{"read_committed", sarama.ReadCommitted},
	}

	for _, tt := range tests {
		t.Run(tt.isolation, func(t *testing.T) {
			config := DefaultConfig()
			config.ConsumerIsolation = tt.isolation

			saramaConfig, err := config.ToSaramaConfig()
			if err != nil {
				t.Fatalf("failed to convert config: %v", err)
			}

			if saramaConfig.Consumer.IsolationLevel != tt.expected {
				t.Errorf("expected isolation %v, got %v", tt.expected, saramaConfig.Consumer.IsolationLevel)
			}
		})
	}
}

func TestToSaramaConfigInvalidVersion(t *testing.T) {
	config := DefaultConfig()
	config.Version = "invalid"

	_, err := config.ToSaramaConfig()
	if err == nil {
		t.Error("expected error with invalid version")
	}
}

func TestConfigOptions(t *testing.T) {
	config := DefaultConfig()

	// Test WithBrokers
	WithBrokers("broker1:9092", "broker2:9092")(&config)
	if len(config.Brokers) != 2 {
		t.Errorf("expected 2 brokers, got %d", len(config.Brokers))
	}

	// Test WithClientID
	WithClientID("custom-client")(&config)
	if config.ClientID != "custom-client" {
		t.Errorf("expected client_id 'custom-client', got %s", config.ClientID)
	}

	// Test WithVersion
	WithVersion("2.8.0")(&config)
	if config.Version != "2.8.0" {
		t.Errorf("expected version '2.8.0', got %s", config.Version)
	}

	// Test WithTLS
	WithTLS("cert.pem", "key.pem", "ca.pem", true)(&config)
	if !config.EnableTLS {
		t.Error("expected enable_tls to be true")
	}
	if !config.TLSSkipVerify {
		t.Error("expected tls_skip_verify to be true")
	}

	// Test WithSASL
	WithSASL("SCRAM-SHA-512", "user", "pass")(&config)
	if !config.EnableSASL {
		t.Error("expected enable_sasl to be true")
	}
	if config.SASLMechanism != "SCRAM-SHA-512" {
		t.Errorf("expected mechanism 'SCRAM-SHA-512', got %s", config.SASLMechanism)
	}

	// Test WithProducer
	WithProducer(false)(&config)
	if config.ProducerEnabled {
		t.Error("expected producer_enabled to be false")
	}

	// Test WithConsumer
	WithConsumer(false)(&config)
	if config.ConsumerEnabled {
		t.Error("expected consumer_enabled to be false")
	}

	// Test WithConsumerGroup
	WithConsumerGroup("my-group")(&config)
	if !config.ConsumerEnabled {
		t.Error("expected consumer_enabled to be true")
	}
	if config.ConsumerGroupID != "my-group" {
		t.Errorf("expected consumer_group_id 'my-group', got %s", config.ConsumerGroupID)
	}

	// Test WithCompression
	WithCompression("lz4")(&config)
	if config.ProducerCompression != "lz4" {
		t.Errorf("expected compression 'lz4', got %s", config.ProducerCompression)
	}

	// Test WithIdempotent
	WithIdempotent(true)(&config)
	if !config.ProducerIdempotent {
		t.Error("expected producer_idempotent to be true")
	}

	// Test WithMetrics
	WithMetrics(false)(&config)
	if config.EnableMetrics {
		t.Error("expected enable_metrics to be false")
	}

	// Test WithTracing
	WithTracing(false)(&config)
	if config.EnableTracing {
		t.Error("expected enable_tracing to be false")
	}

	// Test WithRequireConfig
	WithRequireConfig(true)(&config)
	if !config.RequireConfig {
		t.Error("expected require_config to be true")
	}

	// Test WithConfig
	customConfig := Config{
		Brokers:  []string{"custom:9092"},
		ClientID: "custom",
		Version:  "3.0.0",
	}
	WithConfig(customConfig)(&config)
	if config.Brokers[0] != "custom:9092" {
		t.Errorf("expected broker 'custom:9092', got %s", config.Brokers[0])
	}
}
