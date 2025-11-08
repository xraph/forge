package security

import (
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if !config.Enabled {
		t.Error("expected Enabled to be true")
	}

	if !config.Session.Enabled {
		t.Error("expected Session.Enabled to be true")
	}

	if config.Session.Store != "inmemory" {
		t.Errorf("expected Session.Store to be 'inmemory', got %s", config.Session.Store)
	}

	if config.Session.CookieName != "forge_session" {
		t.Errorf("expected Session.CookieName to be 'forge_session', got %s", config.Session.CookieName)
	}

	if config.Session.TTL != 24*time.Hour {
		t.Errorf("expected Session.TTL to be 24h, got %v", config.Session.TTL)
	}

	if !config.Cookie.Enabled {
		t.Error("expected Cookie.Enabled to be true")
	}

	if !config.Cookie.Secure {
		t.Error("expected Cookie.Secure to be true")
	}

	if !config.Cookie.HttpOnly {
		t.Error("expected Cookie.HttpOnly to be true")
	}

	if config.Cookie.SameSite != "lax" {
		t.Errorf("expected Cookie.SameSite to be 'lax', got %s", config.Cookie.SameSite)
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name      string
		config    Config
		expectErr bool
	}{
		{
			name:      "valid default config",
			config:    DefaultConfig(),
			expectErr: false,
		},
		{
			name: "disabled config",
			config: Config{
				Enabled: false,
			},
			expectErr: false,
		},
		{
			name: "missing session store",
			config: Config{
				Enabled: true,
				Session: SessionConfig{
					Enabled:    true,
					Store:      "",
					CookieName: "test",
					TTL:        1 * time.Hour,
				},
			},
			expectErr: true,
		},
		{
			name: "missing cookie name",
			config: Config{
				Enabled: true,
				Session: SessionConfig{
					Enabled:    true,
					Store:      "inmemory",
					CookieName: "",
					TTL:        1 * time.Hour,
				},
			},
			expectErr: true,
		},
		{
			name: "invalid TTL",
			config: Config{
				Enabled: true,
				Session: SessionConfig{
					Enabled:    true,
					Store:      "inmemory",
					CookieName: "test",
					TTL:        0,
				},
			},
			expectErr: true,
		},
		{
			name: "invalid session store",
			config: Config{
				Enabled: true,
				Session: SessionConfig{
					Enabled:    true,
					Store:      "invalid",
					CookieName: "test",
					TTL:        1 * time.Hour,
				},
			},
			expectErr: true,
		},
		{
			name: "invalid cookie SameSite",
			config: Config{
				Enabled: true,
				Session: SessionConfig{
					Enabled: false,
				},
				Cookie: CookieConfig{
					Enabled:  true,
					SameSite: "invalid",
				},
			},
			expectErr: true,
		},
		{
			name: "SameSite=none without Secure",
			config: Config{
				Enabled: true,
				Session: SessionConfig{
					Enabled: false,
				},
				Cookie: CookieConfig{
					Enabled:  true,
					SameSite: "none",
					Secure:   false,
				},
			},
			expectErr: true,
		},
		{
			name: "redis without address",
			config: Config{
				Enabled: true,
				Session: SessionConfig{
					Enabled:    true,
					Store:      "redis",
					CookieName: "test",
					TTL:        1 * time.Hour,
					Redis: RedisConfig{
						Address: "",
					},
				},
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.expectErr && err == nil {
				t.Error("expected error, got nil")
			}

			if !tt.expectErr && err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}

func TestConfigOptions(t *testing.T) {
	config := DefaultConfig()

	// Apply options
	opts := []ConfigOption{
		WithEnabled(false),
		WithSessionEnabled(false),
		WithSessionStore("redis"),
		WithSessionCookieName("custom_session"),
		WithSessionTTL(2 * time.Hour),
		WithSessionIdleTimeout(15 * time.Minute),
		WithSessionAutoRenew(false),
		WithRedisAddress("redis://custom:6379"),
		WithRedisPassword("secret"),
		WithRedisDB(1),
		WithCookieEnabled(false),
		WithCookieSecure(false),
		WithCookieHttpOnly(false),
		WithCookieSameSite("strict"),
		WithCookiePath("/api"),
		WithCookieDomain("example.com"),
	}

	for _, opt := range opts {
		opt(&config)
	}

	if config.Enabled {
		t.Error("expected Enabled to be false")
	}

	if config.Session.Enabled {
		t.Error("expected Session.Enabled to be false")
	}

	if config.Session.Store != "redis" {
		t.Errorf("expected Session.Store to be 'redis', got %s", config.Session.Store)
	}

	if config.Session.CookieName != "custom_session" {
		t.Errorf("expected Session.CookieName to be 'custom_session', got %s", config.Session.CookieName)
	}

	if config.Session.TTL != 2*time.Hour {
		t.Errorf("expected Session.TTL to be 2h, got %v", config.Session.TTL)
	}

	if config.Session.IdleTimeout != 15*time.Minute {
		t.Errorf("expected Session.IdleTimeout to be 15m, got %v", config.Session.IdleTimeout)
	}

	if config.Session.AutoRenew {
		t.Error("expected Session.AutoRenew to be false")
	}

	if config.Session.Redis.Address != "redis://custom:6379" {
		t.Errorf("expected Redis.Address to be 'redis://custom:6379', got %s", config.Session.Redis.Address)
	}

	if config.Session.Redis.Password != "secret" {
		t.Errorf("expected Redis.Password to be 'secret', got %s", config.Session.Redis.Password)
	}

	if config.Session.Redis.DB != 1 {
		t.Errorf("expected Redis.DB to be 1, got %d", config.Session.Redis.DB)
	}

	if config.Cookie.Enabled {
		t.Error("expected Cookie.Enabled to be false")
	}

	if config.Cookie.Secure {
		t.Error("expected Cookie.Secure to be false")
	}

	if config.Cookie.HttpOnly {
		t.Error("expected Cookie.HttpOnly to be false")
	}

	if config.Cookie.SameSite != "strict" {
		t.Errorf("expected Cookie.SameSite to be 'strict', got %s", config.Cookie.SameSite)
	}

	if config.Cookie.Path != "/api" {
		t.Errorf("expected Cookie.Path to be '/api', got %s", config.Cookie.Path)
	}

	if config.Cookie.Domain != "example.com" {
		t.Errorf("expected Cookie.Domain to be 'example.com', got %s", config.Cookie.Domain)
	}
}

func TestWithConfig(t *testing.T) {
	originalConfig := Config{
		Enabled: false,
		Session: SessionConfig{
			Store: "redis",
		},
	}

	newConfig := DefaultConfig()
	WithConfig(originalConfig)(&newConfig)

	if newConfig.Enabled {
		t.Error("expected Enabled to be false")
	}

	if newConfig.Session.Store != "redis" {
		t.Errorf("expected Session.Store to be 'redis', got %s", newConfig.Session.Store)
	}
}
