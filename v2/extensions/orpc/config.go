package orpc

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Config holds the oRPC extension configuration
type Config struct {
	// Core
	Enabled         bool   `json:"enabled" yaml:"enabled" mapstructure:"enabled"`
	Endpoint        string `json:"endpoint" yaml:"endpoint" mapstructure:"endpoint"`
	OpenRPCEndpoint string `json:"openrpc_endpoint" yaml:"openrpc_endpoint" mapstructure:"openrpc_endpoint"`

	// Server info for OpenRPC
	ServerName    string `json:"server_name" yaml:"server_name" mapstructure:"server_name"`
	ServerVersion string `json:"server_version" yaml:"server_version" mapstructure:"server_version"`

	// Auto-exposure
	AutoExposeRoutes bool     `json:"auto_expose_routes" yaml:"auto_expose_routes" mapstructure:"auto_expose_routes"`
	MethodPrefix     string   `json:"method_prefix" yaml:"method_prefix" mapstructure:"method_prefix"`
	ExcludePatterns  []string `json:"exclude_patterns" yaml:"exclude_patterns" mapstructure:"exclude_patterns"`
	IncludePatterns  []string `json:"include_patterns" yaml:"include_patterns" mapstructure:"include_patterns"`

	// Features
	EnableOpenRPC   bool `json:"enable_openrpc" yaml:"enable_openrpc" mapstructure:"enable_openrpc"`
	EnableDiscovery bool `json:"enable_discovery" yaml:"enable_discovery" mapstructure:"enable_discovery"`
	EnableBatch     bool `json:"enable_batch" yaml:"enable_batch" mapstructure:"enable_batch"`
	BatchLimit      int  `json:"batch_limit" yaml:"batch_limit" mapstructure:"batch_limit"`

	// Naming strategy: "path" (default), "method", "custom"
	NamingStrategy string `json:"naming_strategy" yaml:"naming_strategy" mapstructure:"naming_strategy"`

	// Security
	RequireAuth        bool     `json:"require_auth" yaml:"require_auth" mapstructure:"require_auth"`
	AuthHeader         string   `json:"auth_header" yaml:"auth_header" mapstructure:"auth_header"`
	AuthTokens         []string `json:"auth_tokens" yaml:"auth_tokens" mapstructure:"auth_tokens"`
	RateLimitPerMinute int      `json:"rate_limit_per_minute" yaml:"rate_limit_per_minute" mapstructure:"rate_limit_per_minute"`

	// Performance
	MaxRequestSize int64 `json:"max_request_size" yaml:"max_request_size" mapstructure:"max_request_size"`
	RequestTimeout int   `json:"request_timeout" yaml:"request_timeout" mapstructure:"request_timeout"` // seconds
	SchemaCache    bool  `json:"schema_cache" yaml:"schema_cache" mapstructure:"schema_cache"`

	// Observability
	EnableMetrics bool `json:"enable_metrics" yaml:"enable_metrics" mapstructure:"enable_metrics"`

	// Config loading flag (internal use)
	RequireConfig bool `json:"-" yaml:"-" mapstructure:"-"`
}

// DefaultConfig returns the default configuration
func DefaultConfig() Config {
	return Config{
		Enabled:            true,
		Endpoint:           "/rpc",
		OpenRPCEndpoint:    "/rpc/schema",
		ServerName:         "forge-app",
		ServerVersion:      "1.0.0",
		AutoExposeRoutes:   true,
		MethodPrefix:       "",
		ExcludePatterns:    []string{"/_/*"}, // Exclude internal routes
		IncludePatterns:    []string{},
		EnableOpenRPC:      true,
		EnableDiscovery:    true,
		EnableBatch:        true,
		BatchLimit:         10,
		NamingStrategy:     "path",
		RequireAuth:        false,
		AuthHeader:         "Authorization",
		RateLimitPerMinute: 0,
		MaxRequestSize:     1024 * 1024, // 1MB
		RequestTimeout:     30,
		SchemaCache:        true,
		EnableMetrics:      true,
		RequireConfig:      false,
	}
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if !c.Enabled {
		return nil
	}

	if c.Endpoint == "" {
		return fmt.Errorf("orpc: endpoint cannot be empty")
	}

	if c.EnableOpenRPC && c.OpenRPCEndpoint == "" {
		return fmt.Errorf("orpc: openrpc_endpoint cannot be empty when enabled")
	}

	if c.BatchLimit < 1 {
		return fmt.Errorf("orpc: batch_limit must be at least 1")
	}

	if c.MaxRequestSize < 1024 {
		return fmt.Errorf("orpc: max_request_size must be at least 1024 bytes")
	}

	if c.RequestTimeout < 1 {
		return fmt.Errorf("orpc: request_timeout must be at least 1 second")
	}

	validNamingStrategies := map[string]bool{"path": true, "method": true, "custom": true}
	if !validNamingStrategies[c.NamingStrategy] {
		return fmt.Errorf("orpc: invalid naming_strategy '%s', must be 'path', 'method', or 'custom'", c.NamingStrategy)
	}

	return nil
}

// ShouldExpose checks if a route should be exposed based on include/exclude patterns
func (c *Config) ShouldExpose(path string) bool {
	// If include patterns are set, path must match at least one
	if len(c.IncludePatterns) > 0 {
		matched := false
		for _, pattern := range c.IncludePatterns {
			if matchPath(pattern, path) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Check exclude patterns
	for _, pattern := range c.ExcludePatterns {
		if matchPath(pattern, path) {
			return false
		}
	}

	return true
}

// matchPath checks if a path matches a glob pattern
func matchPath(pattern, path string) bool {
	matched, _ := filepath.Match(pattern, path)
	if matched {
		return true
	}

	// Handle wildcard prefix matching like "/api/*"
	if strings.HasSuffix(pattern, "/*") {
		prefix := strings.TrimSuffix(pattern, "/*")
		return strings.HasPrefix(path, prefix)
	}

	return false
}

// ConfigOption is a functional option for configuring the oRPC extension
type ConfigOption func(*Config)

// WithConfig sets the entire config
func WithConfig(cfg Config) ConfigOption {
	return func(c *Config) {
		*c = cfg
	}
}

// WithEnabled sets the enabled flag
func WithEnabled(enabled bool) ConfigOption {
	return func(c *Config) {
		c.Enabled = enabled
	}
}

// WithEndpoint sets the RPC endpoint
func WithEndpoint(endpoint string) ConfigOption {
	return func(c *Config) {
		c.Endpoint = endpoint
	}
}

// WithOpenRPCEndpoint sets the OpenRPC schema endpoint
func WithOpenRPCEndpoint(endpoint string) ConfigOption {
	return func(c *Config) {
		c.OpenRPCEndpoint = endpoint
	}
}

// WithServerInfo sets the server name and version for OpenRPC
func WithServerInfo(name, version string) ConfigOption {
	return func(c *Config) {
		c.ServerName = name
		c.ServerVersion = version
	}
}

// WithAutoExposeRoutes sets auto-exposure of routes
func WithAutoExposeRoutes(enabled bool) ConfigOption {
	return func(c *Config) {
		c.AutoExposeRoutes = enabled
	}
}

// WithMethodPrefix sets the method name prefix
func WithMethodPrefix(prefix string) ConfigOption {
	return func(c *Config) {
		c.MethodPrefix = prefix
	}
}

// WithExcludePatterns sets the patterns to exclude from exposure
func WithExcludePatterns(patterns []string) ConfigOption {
	return func(c *Config) {
		c.ExcludePatterns = patterns
	}
}

// WithIncludePatterns sets the patterns to include for exposure
func WithIncludePatterns(patterns []string) ConfigOption {
	return func(c *Config) {
		c.IncludePatterns = patterns
	}
}

// WithOpenRPC enables/disables OpenRPC schema generation
func WithOpenRPC(enabled bool) ConfigOption {
	return func(c *Config) {
		c.EnableOpenRPC = enabled
	}
}

// WithDiscovery enables/disables method discovery endpoint
func WithDiscovery(enabled bool) ConfigOption {
	return func(c *Config) {
		c.EnableDiscovery = enabled
	}
}

// WithBatch enables/disables batch requests
func WithBatch(enabled bool) ConfigOption {
	return func(c *Config) {
		c.EnableBatch = enabled
	}
}

// WithBatchLimit sets the maximum batch size
func WithBatchLimit(limit int) ConfigOption {
	return func(c *Config) {
		c.BatchLimit = limit
	}
}

// WithNamingStrategy sets the method naming strategy
func WithNamingStrategy(strategy string) ConfigOption {
	return func(c *Config) {
		c.NamingStrategy = strategy
	}
}

// WithAuth configures authentication
func WithAuth(header string, tokens []string) ConfigOption {
	return func(c *Config) {
		c.RequireAuth = true
		c.AuthHeader = header
		c.AuthTokens = tokens
	}
}

// WithRateLimit sets the rate limit per minute
func WithRateLimit(perMinute int) ConfigOption {
	return func(c *Config) {
		c.RateLimitPerMinute = perMinute
	}
}

// WithMaxRequestSize sets the maximum request size
func WithMaxRequestSize(size int64) ConfigOption {
	return func(c *Config) {
		c.MaxRequestSize = size
	}
}

// WithRequestTimeout sets the request timeout in seconds
func WithRequestTimeout(seconds int) ConfigOption {
	return func(c *Config) {
		c.RequestTimeout = seconds
	}
}

// WithSchemaCache enables/disables schema caching
func WithSchemaCache(enabled bool) ConfigOption {
	return func(c *Config) {
		c.SchemaCache = enabled
	}
}

// WithMetrics enables/disables metrics collection
func WithMetrics(enabled bool) ConfigOption {
	return func(c *Config) {
		c.EnableMetrics = enabled
	}
}

// WithRequireConfig sets whether config must be loaded from ConfigManager
func WithRequireConfig(required bool) ConfigOption {
	return func(c *Config) {
		c.RequireConfig = required
	}
}
