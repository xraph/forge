package cache

import (
	"errors"
	"time"
)

// Config configures the cache extension.
type Config struct {
	// Driver specifies the cache backend: "inmemory", "redis", "memcached"
	Driver string

	// URL is the connection string for the cache backend
	// Examples:
	//   - Redis: "redis://localhost:6379/0"
	//   - Memcached: "memcached://localhost:11211"
	//   - In-Memory: "" (not used)
	URL string

	// DefaultTTL is the default time-to-live for cache entries
	// Used when Set() is called with ttl=0
	DefaultTTL time.Duration

	// MaxSize is the maximum number of items in the cache (in-memory only)
	// 0 means unlimited
	MaxSize int

	// CleanupInterval is how often to clean up expired items (in-memory only)
	CleanupInterval time.Duration

	// MaxKeySize is the maximum allowed key size in bytes
	MaxKeySize int

	// MaxValueSize is the maximum allowed value size in bytes
	MaxValueSize int

	// Prefix is prepended to all keys
	// Useful for namespacing in shared cache instances
	Prefix string

	// ConnectionPoolSize is the number of connections to maintain (Redis/Memcached)
	ConnectionPoolSize int

	// ConnectionTimeout is the timeout for establishing a connection
	ConnectionTimeout time.Duration

	// ReadTimeout is the timeout for read operations
	ReadTimeout time.Duration

	// WriteTimeout is the timeout for write operations
	WriteTimeout time.Duration

	// RequireConfig determines if config must exist in ConfigManager
	// If true, extension fails to start without config
	// If false, uses defaults when config is missing
	RequireConfig bool
}

// DefaultConfig returns a default configuration for in-memory cache.
func DefaultConfig() Config {
	return Config{
		Driver:             "inmemory",
		DefaultTTL:         5 * time.Minute,
		MaxSize:            10000,
		CleanupInterval:    1 * time.Minute,
		MaxKeySize:         250,     // 250 bytes
		MaxValueSize:       1048576, // 1MB
		ConnectionPoolSize: 10,
		ConnectionTimeout:  5 * time.Second,
		ReadTimeout:        3 * time.Second,
		WriteTimeout:       3 * time.Second,
		RequireConfig:      false,
	}
}

// ConfigOption is a functional option for configuring the cache extension.
type ConfigOption func(*Config)

// WithDriver sets the cache driver.
func WithDriver(driver string) ConfigOption {
	return func(c *Config) {
		c.Driver = driver
	}
}

// WithURL sets the cache URL.
func WithURL(url string) ConfigOption {
	return func(c *Config) {
		c.URL = url
	}
}

// WithDefaultTTL sets the default TTL.
func WithDefaultTTL(ttl time.Duration) ConfigOption {
	return func(c *Config) {
		c.DefaultTTL = ttl
	}
}

// WithMaxSize sets the maximum cache size (in-memory only).
func WithMaxSize(size int) ConfigOption {
	return func(c *Config) {
		c.MaxSize = size
	}
}

// WithPrefix sets the key prefix.
func WithPrefix(prefix string) ConfigOption {
	return func(c *Config) {
		c.Prefix = prefix
	}
}

// WithConnectionPoolSize sets the connection pool size.
func WithConnectionPoolSize(size int) ConfigOption {
	return func(c *Config) {
		c.ConnectionPoolSize = size
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
	if c.Driver == "" {
		return errors.New("cache driver cannot be empty")
	}

	if c.Driver != "inmemory" && c.Driver != "redis" && c.Driver != "memcached" {
		return errors.New("cache driver must be 'inmemory', 'redis', or 'memcached'")
	}

	if c.Driver != "inmemory" && c.URL == "" {
		return errors.New("cache URL required for non-inmemory drivers")
	}

	if c.DefaultTTL < 0 {
		return ErrInvalidTTL
	}

	if c.MaxKeySize < 0 {
		return errors.New("max key size cannot be negative")
	}

	if c.MaxValueSize < 0 {
		return errors.New("max value size cannot be negative")
	}

	return nil
}
