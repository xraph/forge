package internal

import (
	"errors"
	"testing"
	"time"
)

func TestDefaultConfigIsValid(t *testing.T) {
	cfg := DefaultConfig()

	if err := cfg.Validate(); err != nil {
		t.Fatalf("DefaultConfig() does not validate: %v", err)
	}
}

func TestConfigValidate(t *testing.T) {
	// Each case starts from the defaults and breaks exactly one thing, so a
	// failure names the rule that fired rather than an unrelated field.
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr bool
	}{
		{
			name:   "defaults",
			mutate: func(c *Config) {},
		},
		{
			name:   "empty backend is allowed",
			mutate: func(c *Config) { c.Backend = "" },
		},
		{
			name:   "redis backend with URLs",
			mutate: func(c *Config) { c.Backend = "redis"; c.BackendURLs = []string{"redis://localhost:6379"} },
		},
		{
			name:   "nats backend with URLs",
			mutate: func(c *Config) { c.Backend = "nats"; c.BackendURLs = []string{"nats://localhost:4222"} },
		},
		{
			name:    "unsupported backend",
			mutate:  func(c *Config) { c.Backend = "kafka" },
			wantErr: true,
		},
		{
			name:    "redis backend without URLs",
			mutate:  func(c *Config) { c.Backend = "redis" },
			wantErr: true,
		},
		{
			name:    "nats backend without URLs",
			mutate:  func(c *Config) { c.Backend = "nats" },
			wantErr: true,
		},
		{
			name:    "distributed mode on the local backend",
			mutate:  func(c *Config) { c.EnableDistributed = true; c.Backend = "local" },
			wantErr: true,
		},
		{
			name: "distributed mode on redis",
			mutate: func(c *Config) {
				c.EnableDistributed = true
				c.Backend = "redis"
				c.BackendURLs = []string{"redis://localhost:6379"}
			},
		},
		{
			name:    "zero connections per user",
			mutate:  func(c *Config) { c.MaxConnectionsPerUser = 0 },
			wantErr: true,
		},
		{
			name:    "negative connections per user",
			mutate:  func(c *Config) { c.MaxConnectionsPerUser = -1 },
			wantErr: true,
		},
		{
			name:   "one connection per user is the floor",
			mutate: func(c *Config) { c.MaxConnectionsPerUser = 1 },
		},
		{
			name:    "zero max message size",
			mutate:  func(c *Config) { c.MaxMessageSize = 0 },
			wantErr: true,
		},
		{
			name:    "sub-second ping interval",
			mutate:  func(c *Config) { c.PingInterval = 500 * time.Millisecond },
			wantErr: true,
		},
		{
			name:   "exactly one second ping interval",
			mutate: func(c *Config) { c.PingInterval = time.Second },
		},
		{
			name:    "sub-second pong timeout",
			mutate:  func(c *Config) { c.PongTimeout = 999 * time.Millisecond },
			wantErr: true,
		},
		{
			name:    "TLS enabled without a certificate",
			mutate:  func(c *Config) { c.TLSEnabled = true; c.TLSKeyFile = "key.pem" },
			wantErr: true,
		},
		{
			name:    "TLS enabled without a key",
			mutate:  func(c *Config) { c.TLSEnabled = true; c.TLSCertFile = "cert.pem" },
			wantErr: true,
		},
		{
			name:   "TLS enabled with both files",
			mutate: func(c *Config) { c.TLSEnabled = true; c.TLSCertFile = "cert.pem"; c.TLSKeyFile = "key.pem" },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.mutate(&cfg)

			err := cfg.Validate()

			if tt.wantErr {
				if err == nil {
					t.Fatal("Validate() = nil, want an error")
				}

				// Every validation failure is reported as ErrInvalidConfig so
				// callers can branch on it without matching error text.
				if !errors.Is(err, ErrInvalidConfig) {
					t.Errorf("Validate() = %v, want it to wrap ErrInvalidConfig", err)
				}

				return
			}

			if err != nil {
				t.Errorf("Validate() = %v, want nil", err)
			}
		})
	}
}

func TestConfigOptions(t *testing.T) {
	tests := []struct {
		name   string
		option ConfigOption
		check  func(t *testing.T, c Config)
	}{
		{
			name:   "WithBackend",
			option: WithBackend("redis"),
			check: func(t *testing.T, c Config) {
				if c.Backend != "redis" {
					t.Errorf("Backend = %q, want redis", c.Backend)
				}
			},
		},
		{
			name:   "WithBackendURLs",
			option: WithBackendURLs("a", "b"),
			check: func(t *testing.T, c Config) {
				if len(c.BackendURLs) != 2 || c.BackendURLs[0] != "a" {
					t.Errorf("BackendURLs = %v, want [a b]", c.BackendURLs)
				}
			},
		},
		{
			name:   "WithRedisBackend enables distributed mode",
			option: WithRedisBackend("redis://localhost:6379"),
			check: func(t *testing.T, c Config) {
				if c.Backend != "redis" || !c.EnableDistributed {
					t.Errorf("got Backend=%q EnableDistributed=%v, want redis/true", c.Backend, c.EnableDistributed)
				}

				if len(c.BackendURLs) != 1 {
					t.Errorf("BackendURLs = %v, want one entry", c.BackendURLs)
				}
			},
		},
		{
			name:   "WithNATSBackend enables distributed mode",
			option: WithNATSBackend("nats://a", "nats://b"),
			check: func(t *testing.T, c Config) {
				if c.Backend != "nats" || !c.EnableDistributed {
					t.Errorf("got Backend=%q EnableDistributed=%v, want nats/true", c.Backend, c.EnableDistributed)
				}

				if len(c.BackendURLs) != 2 {
					t.Errorf("BackendURLs = %v, want two entries", c.BackendURLs)
				}
			},
		},
		{
			name:   "WithLocalBackend disables distributed mode",
			option: WithLocalBackend(),
			check: func(t *testing.T, c Config) {
				if c.Backend != "local" || c.EnableDistributed {
					t.Errorf("got Backend=%q EnableDistributed=%v, want local/false", c.Backend, c.EnableDistributed)
				}
			},
		},
		{
			name:   "WithFeatures",
			option: WithFeatures(false, false, true, true, false),
			check: func(t *testing.T, c Config) {
				if c.EnableRooms || c.EnableChannels || !c.EnablePresence || !c.EnableTypingIndicators || c.EnableMessageHistory {
					t.Errorf("feature flags = %+v, want rooms/channels/history off and presence/typing on", c)
				}
			},
		},
		{
			name:   "WithConnectionLimits",
			option: WithConnectionLimits(3, 4, 5),
			check: func(t *testing.T, c Config) {
				if c.MaxConnectionsPerUser != 3 || c.MaxRoomsPerUser != 4 || c.MaxChannelsPerUser != 5 {
					t.Errorf("limits = %d/%d/%d, want 3/4/5",
						c.MaxConnectionsPerUser, c.MaxRoomsPerUser, c.MaxChannelsPerUser)
				}
			},
		},
		{
			name:   "WithMessageLimits",
			option: WithMessageLimits(1024, 7),
			check: func(t *testing.T, c Config) {
				if c.MaxMessageSize != 1024 || c.MaxMessagesPerSecond != 7 {
					t.Errorf("message limits = %d/%d, want 1024/7", c.MaxMessageSize, c.MaxMessagesPerSecond)
				}
			},
		},
		{
			name:   "WithTimeouts",
			option: WithTimeouts(time.Second, 2*time.Second, 3*time.Second),
			check: func(t *testing.T, c Config) {
				if c.PingInterval != time.Second || c.PongTimeout != 2*time.Second || c.WriteTimeout != 3*time.Second {
					t.Errorf("timeouts = %v/%v/%v, want 1s/2s/3s", c.PingInterval, c.PongTimeout, c.WriteTimeout)
				}
			},
		},
		{
			name:   "WithBufferSizes",
			option: WithBufferSizes(111, 222),
			check: func(t *testing.T, c Config) {
				if c.ReadBufferSize != 111 || c.WriteBufferSize != 222 {
					t.Errorf("buffers = %d/%d, want 111/222", c.ReadBufferSize, c.WriteBufferSize)
				}
			},
		},
		{
			name:   "WithNodeID",
			option: WithNodeID("node-7"),
			check: func(t *testing.T, c Config) {
				if c.NodeID != "node-7" {
					t.Errorf("NodeID = %q, want node-7", c.NodeID)
				}
			},
		},
		{
			name:   "WithTLS",
			option: WithTLS("cert.pem", "key.pem", "ca.pem"),
			check: func(t *testing.T, c Config) {
				if !c.TLSEnabled || c.TLSCertFile != "cert.pem" || c.TLSKeyFile != "key.pem" || c.TLSCAFile != "ca.pem" {
					t.Errorf("TLS config = %+v, want enabled with all three files", c)
				}
			},
		},
		{
			name:   "WithAuthentication",
			option: WithAuthentication("user", "pass"),
			check: func(t *testing.T, c Config) {
				if c.BackendUsername != "user" || c.BackendPassword != "pass" {
					t.Errorf("credentials = %q/%q, want user/pass", c.BackendUsername, c.BackendPassword)
				}
			},
		},
		{
			name:   "WithSessionResumption sets the TTL",
			option: WithSessionResumption(90 * time.Second),
			check: func(t *testing.T, c Config) {
				if !c.EnableSessionResumption || c.SessionResumptionTTL != 90*time.Second {
					t.Errorf("resumption = %v/%v, want enabled with 90s", c.EnableSessionResumption, c.SessionResumptionTTL)
				}
			},
		},
		{
			name:   "WithSessionResumption keeps the default TTL when given zero",
			option: WithSessionResumption(0),
			check: func(t *testing.T, c Config) {
				if !c.EnableSessionResumption {
					t.Error("EnableSessionResumption = false, want true")
				}

				if c.SessionResumptionTTL != DefaultConfig().SessionResumptionTTL {
					t.Errorf("SessionResumptionTTL = %v, want the default preserved", c.SessionResumptionTTL)
				}
			},
		},
		{
			name:   "WithLoadBalancer sets the strategy",
			option: WithLoadBalancer("consistent_hash"),
			check: func(t *testing.T, c Config) {
				if !c.EnableLoadBalancer || c.LoadBalancerStrategy != "consistent_hash" {
					t.Errorf("load balancer = %v/%q, want enabled with consistent_hash",
						c.EnableLoadBalancer, c.LoadBalancerStrategy)
				}
			},
		},
		{
			name:   "WithLoadBalancer keeps the default strategy when given empty",
			option: WithLoadBalancer(""),
			check: func(t *testing.T, c Config) {
				if c.LoadBalancerStrategy != DefaultConfig().LoadBalancerStrategy {
					t.Errorf("LoadBalancerStrategy = %q, want the default preserved", c.LoadBalancerStrategy)
				}
			},
		},
		{
			name:   "WithRequireConfig",
			option: WithRequireConfig(true),
			check: func(t *testing.T, c Config) {
				if !c.RequireConfig {
					t.Error("RequireConfig = false, want true")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.option(&cfg)
			tt.check(t, cfg)
		})
	}
}

func TestWithConfigReplacesEverything(t *testing.T) {
	// WithConfig assigns wholesale rather than merging, so fields the caller
	// left unset must come back as zero, not as the previous defaults.
	replacement := Config{Backend: "nats", NodeID: "n1", MaxConnectionsPerUser: 9}

	cfg := DefaultConfig()
	WithConfig(replacement)(&cfg)

	if cfg.Backend != "nats" || cfg.NodeID != "n1" || cfg.MaxConnectionsPerUser != 9 {
		t.Errorf("WithConfig did not apply the replacement: %+v", cfg)
	}

	if cfg.MaxMessageSize != 0 || cfg.EnableRooms || cfg.PingInterval != 0 {
		t.Errorf("WithConfig merged instead of replacing; leftover defaults in %+v", cfg)
	}
}

func TestConfigOptionsCompose(t *testing.T) {
	cfg := DefaultConfig()

	for _, opt := range []ConfigOption{
		WithRedisBackend("redis://localhost:6379"),
		WithConnectionLimits(2, 3, 4),
		WithNodeID("node-1"),
	} {
		opt(&cfg)
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("composed config does not validate: %v", err)
	}

	if cfg.Backend != "redis" || cfg.MaxConnectionsPerUser != 2 || cfg.NodeID != "node-1" {
		t.Errorf("composed config = %+v, want redis/2/node-1", cfg)
	}
}

func TestDefaultOptionHelpers(t *testing.T) {
	presence := DefaultPresenceOptions()
	if presence.OfflineTimeout <= 0 || presence.CleanupInterval <= 0 {
		t.Errorf("DefaultPresenceOptions = %+v, want positive timeouts", presence)
	}

	typing := DefaultTypingOptions()
	if typing.TypingTimeout <= 0 || typing.MaxTypingUsers <= 0 {
		t.Errorf("DefaultTypingOptions = %+v, want positive timeout and user cap", typing)
	}

	store := DefaultMessageStoreOptions()
	if !store.Enabled || store.RetentionPeriod <= 0 {
		t.Errorf("DefaultMessageStoreOptions = %+v, want enabled with a retention period", store)
	}
}
