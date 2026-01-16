package mcp

import "errors"

// DI container keys for MCP extension services.
const (
	// ServiceKey is the DI key for the MCP service.
	ServiceKey = "mcp"
)

// Config configures the MCP extension.
type Config struct {
	// Enabled enables or disables the MCP server
	Enabled bool

	// BasePath is the base path for MCP endpoints
	// Default: "/_/mcp"
	BasePath string

	// ServerName is the name of the MCP server
	// Default: app name
	ServerName string

	// ServerVersion is the version of the MCP server
	// Default: app version
	ServerVersion string

	// AutoExposeRoutes automatically exposes routes as MCP tools
	// Default: true
	AutoExposeRoutes bool

	// ToolPrefix is a prefix to add to all tool names
	// Example: "myapi_" -> "myapi_create_user"
	ToolPrefix string

	// ExcludePatterns are route patterns to exclude from MCP
	// Example: []string{"/_/*", "/internal/*"}
	ExcludePatterns []string

	// IncludePatterns are route patterns to explicitly include in MCP
	// If set, only matching routes are exposed
	IncludePatterns []string

	// MaxToolNameLength is the maximum length for tool names
	// Default: 64
	MaxToolNameLength int

	// EnableResources enables MCP resources (data access)
	// Default: false
	EnableResources bool

	// EnablePrompts enables MCP prompts (templates)
	// Default: false
	EnablePrompts bool

	// RequireAuth requires authentication for MCP endpoints
	// Default: false
	RequireAuth bool

	// AuthHeader is the header to check for authentication
	// Default: "Authorization"
	AuthHeader string

	// AuthTokens are valid authentication tokens
	// Only used if RequireAuth is true
	AuthTokens []string

	// RateLimitPerMinute limits MCP tool calls per minute
	// 0 means unlimited
	// Default: 0
	RateLimitPerMinute int

	// EnableMetrics enables metrics collection for MCP operations
	// Default: true
	EnableMetrics bool

	// SchemaCache enables caching of generated JSON schemas
	// Default: true
	SchemaCache bool

	// RequireConfig determines if config must exist in ConfigManager
	// If true, extension fails to start without config
	// If false, uses defaults when config is missing
	RequireConfig bool
}

// DefaultConfig returns a default MCP configuration.
func DefaultConfig() Config {
	return Config{
		Enabled:            true,
		BasePath:           "/_/mcp",
		AutoExposeRoutes:   true,
		MaxToolNameLength:  64,
		EnableResources:    false,
		EnablePrompts:      false,
		RequireAuth:        false,
		AuthHeader:         "Authorization",
		RateLimitPerMinute: 0,
		EnableMetrics:      true,
		SchemaCache:        true,
		RequireConfig:      false,
	}
}

// ConfigOption is a functional option for configuring the MCP extension.
type ConfigOption func(*Config)

// WithEnabled sets whether MCP is enabled.
func WithEnabled(enabled bool) ConfigOption {
	return func(c *Config) {
		c.Enabled = enabled
	}
}

// WithBasePath sets the base path for MCP endpoints.
func WithBasePath(basePath string) ConfigOption {
	return func(c *Config) {
		c.BasePath = basePath
	}
}

// WithAutoExposeRoutes sets whether to automatically expose routes.
func WithAutoExposeRoutes(autoExpose bool) ConfigOption {
	return func(c *Config) {
		c.AutoExposeRoutes = autoExpose
	}
}

// WithServerInfo sets server name and version.
func WithServerInfo(name, version string) ConfigOption {
	return func(c *Config) {
		c.ServerName = name
		c.ServerVersion = version
	}
}

// WithToolPrefix sets the tool prefix.
func WithToolPrefix(prefix string) ConfigOption {
	return func(c *Config) {
		c.ToolPrefix = prefix
	}
}

// WithExcludePatterns sets patterns to exclude from MCP.
func WithExcludePatterns(patterns []string) ConfigOption {
	return func(c *Config) {
		c.ExcludePatterns = patterns
	}
}

// WithIncludePatterns sets patterns to include in MCP.
func WithIncludePatterns(patterns []string) ConfigOption {
	return func(c *Config) {
		c.IncludePatterns = patterns
	}
}

// WithResources enables MCP resources.
func WithResources(enable bool) ConfigOption {
	return func(c *Config) {
		c.EnableResources = enable
	}
}

// WithPrompts enables MCP prompts.
func WithPrompts(enable bool) ConfigOption {
	return func(c *Config) {
		c.EnablePrompts = enable
	}
}

// WithAuth enables authentication with tokens.
func WithAuth(authHeader string, tokens []string) ConfigOption {
	return func(c *Config) {
		c.RequireAuth = true
		c.AuthHeader = authHeader
		c.AuthTokens = tokens
	}
}

// WithRateLimit sets the rate limit per minute.
func WithRateLimit(limit int) ConfigOption {
	return func(c *Config) {
		c.RateLimitPerMinute = limit
	}
}

// WithRequireConfig sets whether config is required from ConfigManager.
func WithRequireConfig(require bool) ConfigOption {
	return func(c *Config) {
		c.RequireConfig = require
	}
}

// WithConfig applies a complete config.
func WithConfig(config Config) ConfigOption {
	return func(c *Config) {
		*c = config
	}
}

// Validate validates the configuration.
func (c Config) Validate() error {
	if !c.Enabled {
		return nil // Not enabled, validation not needed
	}

	if c.BasePath == "" {
		return errors.New("mcp: base path cannot be empty")
	}

	if c.MaxToolNameLength < 1 {
		return errors.New("mcp: max tool name length must be positive")
	}

	if c.RequireAuth && len(c.AuthTokens) == 0 {
		return errors.New("mcp: auth tokens required when authentication is enabled")
	}

	if c.RateLimitPerMinute < 0 {
		return errors.New("mcp: rate limit cannot be negative")
	}

	return nil
}

// ShouldExpose determines if a route should be exposed as an MCP tool.
func (c Config) ShouldExpose(path string) bool {
	if !c.Enabled || !c.AutoExposeRoutes {
		return false
	}

	// Check exclude patterns first
	for _, pattern := range c.ExcludePatterns {
		if matchPattern(pattern, path) {
			return false
		}
	}

	// If include patterns are set, only expose matching routes
	if len(c.IncludePatterns) > 0 {
		for _, pattern := range c.IncludePatterns {
			if matchPattern(pattern, path) {
				return true
			}
		}

		return false
	}

	return true
}

// matchPattern performs simple glob-style pattern matching.
func matchPattern(pattern, path string) bool {
	// Simple implementation - exact match or prefix with wildcard
	if pattern == path {
		return true
	}

	// Pattern ends with /* - match prefix
	if len(pattern) > 2 && pattern[len(pattern)-2:] == "/*" {
		prefix := pattern[:len(pattern)-2]

		return len(path) >= len(prefix) && path[:len(prefix)] == prefix
	}

	return false
}
