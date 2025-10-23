package mqtt

import (
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Broker != "tcp://localhost:1883" {
		t.Errorf("expected default broker 'tcp://localhost:1883', got %s", config.Broker)
	}

	if config.ClientID != "forge-mqtt-client" {
		t.Errorf("expected default client_id 'forge-mqtt-client', got %s", config.ClientID)
	}

	if !config.CleanSession {
		t.Error("expected clean_session to be true")
	}

	if config.DefaultQoS != 1 {
		t.Errorf("expected default_qos 1, got %d", config.DefaultQoS)
	}

	if !config.AutoReconnect {
		t.Error("expected auto_reconnect to be true")
	}

	if config.MessageStore != "memory" {
		t.Errorf("expected message_store 'memory', got %s", config.MessageStore)
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
				Broker:        "tcp://localhost:1883",
				ClientID:      "test-client",
				DefaultQoS:    1,
				MessageStore:  "memory",
			},
			expectError: false,
		},
		{
			name: "missing broker",
			config: Config{
				ClientID:     "test-client",
				DefaultQoS:   1,
				MessageStore: "memory",
			},
			expectError: true,
			errorMsg:    "broker is required",
		},
		{
			name: "missing client_id",
			config: Config{
				Broker:       "tcp://localhost:1883",
				DefaultQoS:   1,
				MessageStore: "memory",
			},
			expectError: true,
			errorMsg:    "client_id is required",
		},
		{
			name: "invalid qos",
			config: Config{
				Broker:       "tcp://localhost:1883",
				ClientID:     "test-client",
				DefaultQoS:   3,
				MessageStore: "memory",
			},
			expectError: true,
			errorMsg:    "default_qos must be 0, 1, or 2",
		},
		{
			name: "tls without certs",
			config: Config{
				Broker:        "tcp://localhost:1883",
				ClientID:      "test-client",
				DefaultQoS:    1,
				EnableTLS:     true,
				TLSSkipVerify: false,
				MessageStore:  "memory",
			},
			expectError: true,
			errorMsg:    "tls requires cert_file and key_file when not skipping verification",
		},
		{
			name: "tls with skip verify",
			config: Config{
				Broker:        "tcp://localhost:1883",
				ClientID:      "test-client",
				DefaultQoS:    1,
				EnableTLS:     true,
				TLSSkipVerify: true,
				MessageStore:  "memory",
			},
			expectError: false,
		},
		{
			name: "will enabled without topic",
			config: Config{
				Broker:       "tcp://localhost:1883",
				ClientID:     "test-client",
				DefaultQoS:   1,
				WillEnabled:  true,
				MessageStore: "memory",
			},
			expectError: true,
			errorMsg:    "will_topic is required when will is enabled",
		},
		{
			name: "will with invalid qos",
			config: Config{
				Broker:       "tcp://localhost:1883",
				ClientID:     "test-client",
				DefaultQoS:   1,
				WillEnabled:  true,
				WillTopic:    "test/will",
				WillQoS:      3,
				MessageStore: "memory",
			},
			expectError: true,
			errorMsg:    "will_qos must be 0, 1, or 2",
		},
		{
			name: "invalid message store",
			config: Config{
				Broker:       "tcp://localhost:1883",
				ClientID:     "test-client",
				DefaultQoS:   1,
				MessageStore: "redis",
			},
			expectError: true,
			errorMsg:    "message_store must be 'memory' or 'file'",
		},
		{
			name: "file store without directory",
			config: Config{
				Broker:       "tcp://localhost:1883",
				ClientID:     "test-client",
				DefaultQoS:   1,
				MessageStore: "file",
			},
			expectError: true,
			errorMsg:    "store_directory is required when message_store is 'file'",
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

func TestConfigOptions(t *testing.T) {
	config := DefaultConfig()

	// Test WithBroker
	WithBroker("tcp://test:1883")(&config)
	if config.Broker != "tcp://test:1883" {
		t.Errorf("expected broker 'tcp://test:1883', got %s", config.Broker)
	}

	// Test WithClientID
	WithClientID("custom-client")(&config)
	if config.ClientID != "custom-client" {
		t.Errorf("expected client_id 'custom-client', got %s", config.ClientID)
	}

	// Test WithCredentials
	WithCredentials("user", "pass")(&config)
	if config.Username != "user" || config.Password != "pass" {
		t.Errorf("expected credentials user/pass, got %s/%s", config.Username, config.Password)
	}

	// Test WithTLS
	WithTLS("cert.pem", "key.pem", "ca.pem", true)(&config)
	if !config.EnableTLS {
		t.Error("expected enable_tls to be true")
	}
	if config.TLSCertFile != "cert.pem" {
		t.Errorf("expected cert file 'cert.pem', got %s", config.TLSCertFile)
	}
	if config.TLSKeyFile != "key.pem" {
		t.Errorf("expected key file 'key.pem', got %s", config.TLSKeyFile)
	}
	if config.TLSCAFile != "ca.pem" {
		t.Errorf("expected CA file 'ca.pem', got %s", config.TLSCAFile)
	}
	if !config.TLSSkipVerify {
		t.Error("expected tls_skip_verify to be true")
	}

	// Test WithQoS
	WithQoS(2)(&config)
	if config.DefaultQoS != 2 {
		t.Errorf("expected default_qos 2, got %d", config.DefaultQoS)
	}

	// Test WithCleanSession
	WithCleanSession(false)(&config)
	if config.CleanSession {
		t.Error("expected clean_session to be false")
	}

	// Test WithKeepAlive
	WithKeepAlive(30 * time.Second)(&config)
	if config.KeepAlive != 30*time.Second {
		t.Errorf("expected keep_alive 30s, got %v", config.KeepAlive)
	}

	// Test WithAutoReconnect
	WithAutoReconnect(false)(&config)
	if config.AutoReconnect {
		t.Error("expected auto_reconnect to be false")
	}

	// Test WithWill
	WithWill("status/topic", "offline", 1, true)(&config)
	if !config.WillEnabled {
		t.Error("expected will_enabled to be true")
	}
	if config.WillTopic != "status/topic" {
		t.Errorf("expected will_topic 'status/topic', got %s", config.WillTopic)
	}
	if config.WillPayload != "offline" {
		t.Errorf("expected will_payload 'offline', got %s", config.WillPayload)
	}
	if config.WillQoS != 1 {
		t.Errorf("expected will_qos 1, got %d", config.WillQoS)
	}
	if !config.WillRetained {
		t.Error("expected will_retained to be true")
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
		Broker:   "tcp://custom:1883",
		ClientID: "custom",
	}
	WithConfig(customConfig)(&config)
	if config.Broker != "tcp://custom:1883" {
		t.Errorf("expected broker 'tcp://custom:1883', got %s", config.Broker)
	}
	if config.ClientID != "custom" {
		t.Errorf("expected client_id 'custom', got %s", config.ClientID)
	}
}
