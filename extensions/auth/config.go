package auth

// Config holds auth extension configuration.
type Config struct {
	// Enabled determines if auth extension is enabled
	Enabled bool

	// Providers holds provider-specific configurations
	Providers []ProviderConfig

	// DefaultProvider is the default provider name to use
	DefaultProvider string

	// RequireConfig requires config from ConfigManager
	RequireConfig bool
}

// ProviderConfig holds configuration for a single auth provider.
type ProviderConfig struct {
	Name        string
	Type        string
	Enabled     bool
	Description string
	Config      map[string]any
}

// DefaultConfig returns the default auth configuration.
func DefaultConfig() Config {
	return Config{
		Enabled:   true,
		Providers: []ProviderConfig{},
	}
}

// ConfigOption configures the auth extension.
type ConfigOption func(*Config)

// WithEnabled sets whether auth is enabled.
func WithEnabled(enabled bool) ConfigOption {
	return func(c *Config) {
		c.Enabled = enabled
	}
}

// WithProviders sets the provider configurations.
func WithProviders(providers ...ProviderConfig) ConfigOption {
	return func(c *Config) {
		c.Providers = providers
	}
}

// WithDefaultProvider sets the default provider.
func WithDefaultProvider(name string) ConfigOption {
	return func(c *Config) {
		c.DefaultProvider = name
	}
}

// WithRequireConfig requires config from ConfigManager.
func WithRequireConfig(require bool) ConfigOption {
	return func(c *Config) {
		c.RequireConfig = require
	}
}

// WithConfig sets the complete config.
func WithConfig(config Config) ConfigOption {
	return func(c *Config) {
		*c = config
	}
}
