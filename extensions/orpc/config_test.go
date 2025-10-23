package orpc

import (
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Endpoint != "/rpc" {
		t.Errorf("expected endpoint /rpc, got %s", config.Endpoint)
	}
	if !config.Enabled {
		t.Error("expected enabled true")
	}
	if config.BatchLimit != 10 {
		t.Errorf("expected batch limit 10, got %d", config.BatchLimit)
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name:    "default config",
			config:  DefaultConfig(),
			wantErr: false,
		},
		{
			name: "disabled config",
			config: Config{
				Enabled: false,
			},
			wantErr: false,
		},
		{
			name: "empty endpoint",
			config: Config{
				Enabled:  true,
				Endpoint: "",
			},
			wantErr: true,
		},
		{
			name: "empty openrpc endpoint with enabled",
			config: Config{
				Enabled:         true,
				Endpoint:        "/rpc",
				EnableOpenRPC:   true,
				OpenRPCEndpoint: "",
				BatchLimit:      10,
				MaxRequestSize:  1024,
				RequestTimeout:  30,
				NamingStrategy:  "path",
			},
			wantErr: true,
		},
		{
			name: "invalid batch limit",
			config: Config{
				Enabled:    true,
				Endpoint:   "/rpc",
				BatchLimit: 0,
			},
			wantErr: true,
		},
		{
			name: "invalid max request size",
			config: Config{
				Enabled:        true,
				Endpoint:       "/rpc",
				BatchLimit:     10,
				MaxRequestSize: 100,
			},
			wantErr: true,
		},
		{
			name: "invalid request timeout",
			config: Config{
				Enabled:        true,
				Endpoint:       "/rpc",
				BatchLimit:     10,
				MaxRequestSize: 1024,
				RequestTimeout: 0,
			},
			wantErr: true,
		},
		{
			name: "invalid naming strategy",
			config: Config{
				Enabled:        true,
				Endpoint:       "/rpc",
				BatchLimit:     10,
				MaxRequestSize: 1024,
				RequestTimeout: 30,
				NamingStrategy: "invalid",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfigOptions(t *testing.T) {
	config := DefaultConfig()

	WithEnabled(false)(&config)
	if config.Enabled {
		t.Error("WithEnabled failed")
	}

	WithEndpoint("/api/rpc")(&config)
	if config.Endpoint != "/api/rpc" {
		t.Error("WithEndpoint failed")
	}

	WithOpenRPCEndpoint("/api/schema")(&config)
	if config.OpenRPCEndpoint != "/api/schema" {
		t.Error("WithOpenRPCEndpoint failed")
	}

	WithServerInfo("my-app", "2.0.0")(&config)
	if config.ServerName != "my-app" || config.ServerVersion != "2.0.0" {
		t.Error("WithServerInfo failed")
	}

	WithAutoExposeRoutes(false)(&config)
	if config.AutoExposeRoutes {
		t.Error("WithAutoExposeRoutes failed")
	}

	WithMethodPrefix("api.")(&config)
	if config.MethodPrefix != "api." {
		t.Error("WithMethodPrefix failed")
	}

	WithExcludePatterns([]string{"/test"})(&config)
	if len(config.ExcludePatterns) != 1 || config.ExcludePatterns[0] != "/test" {
		t.Error("WithExcludePatterns failed")
	}

	WithIncludePatterns([]string{"/api/*"})(&config)
	if len(config.IncludePatterns) != 1 || config.IncludePatterns[0] != "/api/*" {
		t.Error("WithIncludePatterns failed")
	}

	WithOpenRPC(false)(&config)
	if config.EnableOpenRPC {
		t.Error("WithOpenRPC failed")
	}

	WithDiscovery(false)(&config)
	if config.EnableDiscovery {
		t.Error("WithDiscovery failed")
	}

	WithBatch(false)(&config)
	if config.EnableBatch {
		t.Error("WithBatch failed")
	}

	WithBatchLimit(20)(&config)
	if config.BatchLimit != 20 {
		t.Error("WithBatchLimit failed")
	}

	WithNamingStrategy("method")(&config)
	if config.NamingStrategy != "method" {
		t.Error("WithNamingStrategy failed")
	}

	WithAuth("X-API-Key", []string{"token1"})(&config)
	if !config.RequireAuth || config.AuthHeader != "X-API-Key" || len(config.AuthTokens) != 1 {
		t.Error("WithAuth failed")
	}

	WithRateLimit(100)(&config)
	if config.RateLimitPerMinute != 100 {
		t.Error("WithRateLimit failed")
	}

	WithMaxRequestSize(2048)(&config)
	if config.MaxRequestSize != 2048 {
		t.Error("WithMaxRequestSize failed")
	}

	WithRequestTimeout(60)(&config)
	if config.RequestTimeout != 60 {
		t.Error("WithRequestTimeout failed")
	}

	WithSchemaCache(false)(&config)
	if config.SchemaCache {
		t.Error("WithSchemaCache failed")
	}

	WithMetrics(false)(&config)
	if config.EnableMetrics {
		t.Error("WithMetrics failed")
	}

	WithRequireConfig(true)(&config)
	if !config.RequireConfig {
		t.Error("WithRequireConfig failed")
	}
}

func TestWithConfig(t *testing.T) {
	original := Config{
		Enabled:  true,
		Endpoint: "/custom",
	}

	config := DefaultConfig()
	WithConfig(original)(&config)

	if config.Endpoint != "/custom" {
		t.Error("WithConfig failed to apply config")
	}
}

func TestConfig_ShouldExpose(t *testing.T) {
	tests := []struct {
		name   string
		config Config
		path   string
		want   bool
	}{
		{
			name: "no patterns",
			config: Config{
				ExcludePatterns: []string{},
				IncludePatterns: []string{},
			},
			path: "/users",
			want: true,
		},
		{
			name: "exclude internal",
			config: Config{
				ExcludePatterns: []string{"/_/*"},
				IncludePatterns: []string{},
			},
			path: "/_/health",
			want: false,
		},
		{
			name: "include api only",
			config: Config{
				ExcludePatterns: []string{},
				IncludePatterns: []string{"/api/*"},
			},
			path: "/api/users",
			want: true,
		},
		{
			name: "include api but not match",
			config: Config{
				ExcludePatterns: []string{},
				IncludePatterns: []string{"/api/*"},
			},
			path: "/users",
			want: false,
		},
		{
			name: "exclude takes precedence",
			config: Config{
				ExcludePatterns: []string{"/internal/*"},
				IncludePatterns: []string{"/internal/*"},
			},
			path: "/internal/admin",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.ShouldExpose(tt.path)
			if got != tt.want {
				t.Errorf("ShouldExpose() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchPath(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		path    string
		want    bool
	}{
		{
			name:    "exact match",
			pattern: "/users",
			path:    "/users",
			want:    true,
		},
		{
			name:    "wildcard match",
			pattern: "/api/*",
			path:    "/api/users",
			want:    true,
		},
		{
			name:    "wildcard no match",
			pattern: "/api/*",
			path:    "/users",
			want:    false,
		},
		{
			name:    "glob pattern",
			pattern: "/*.json",
			path:    "/config.json",
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchPath(tt.pattern, tt.path)
			if got != tt.want {
				t.Errorf("matchPath() = %v, want %v", got, tt.want)
			}
		})
	}
}
