package graphql

import (
	"fmt"
	"time"
)

// Config contains configuration for the GraphQL extension
type Config struct {
	// Server settings
	Endpoint            string `json:"endpoint" yaml:"endpoint" mapstructure:"endpoint"`
	PlaygroundEndpoint  string `json:"playground_endpoint" yaml:"playground_endpoint" mapstructure:"playground_endpoint"`
	EnablePlayground    bool   `json:"enable_playground" yaml:"enable_playground" mapstructure:"enable_playground"`
	EnableIntrospection bool   `json:"enable_introspection" yaml:"enable_introspection" mapstructure:"enable_introspection"`

	// Schema
	AutoGenerateSchema bool   `json:"auto_generate_schema" yaml:"auto_generate_schema" mapstructure:"auto_generate_schema"`
	SchemaFile         string `json:"schema_file,omitempty" yaml:"schema_file,omitempty" mapstructure:"schema_file"`

	// Performance
	MaxComplexity       int           `json:"max_complexity" yaml:"max_complexity" mapstructure:"max_complexity"`
	MaxDepth            int           `json:"max_depth" yaml:"max_depth" mapstructure:"max_depth"`
	QueryTimeout        time.Duration `json:"query_timeout" yaml:"query_timeout" mapstructure:"query_timeout"`
	EnableDataLoader    bool          `json:"enable_dataloader" yaml:"enable_dataloader" mapstructure:"enable_dataloader"`
	DataLoaderBatchSize int           `json:"dataloader_batch_size" yaml:"dataloader_batch_size" mapstructure:"dataloader_batch_size"`
	DataLoaderWait      time.Duration `json:"dataloader_wait" yaml:"dataloader_wait" mapstructure:"dataloader_wait"`

	// Caching
	EnableQueryCache bool          `json:"enable_query_cache" yaml:"enable_query_cache" mapstructure:"enable_query_cache"`
	QueryCacheTTL    time.Duration `json:"query_cache_ttl" yaml:"query_cache_ttl" mapstructure:"query_cache_ttl"`
	MaxCacheSize     int           `json:"max_cache_size" yaml:"max_cache_size" mapstructure:"max_cache_size"`

	// Security
	EnableCORS     bool     `json:"enable_cors" yaml:"enable_cors" mapstructure:"enable_cors"`
	AllowedOrigins []string `json:"allowed_origins,omitempty" yaml:"allowed_origins,omitempty" mapstructure:"allowed_origins"`
	MaxUploadSize  int64    `json:"max_upload_size" yaml:"max_upload_size" mapstructure:"max_upload_size"`

	// Observability
	EnableMetrics      bool          `json:"enable_metrics" yaml:"enable_metrics" mapstructure:"enable_metrics"`
	EnableTracing      bool          `json:"enable_tracing" yaml:"enable_tracing" mapstructure:"enable_tracing"`
	EnableLogging      bool          `json:"enable_logging" yaml:"enable_logging" mapstructure:"enable_logging"`
	LogSlowQueries     bool          `json:"log_slow_queries" yaml:"log_slow_queries" mapstructure:"log_slow_queries"`
	SlowQueryThreshold time.Duration `json:"slow_query_threshold" yaml:"slow_query_threshold" mapstructure:"slow_query_threshold"`

	// Config loading flags
	RequireConfig bool `json:"-" yaml:"-" mapstructure:"-"`
}

// DefaultConfig returns default GraphQL configuration
func DefaultConfig() Config {
	return Config{
		Endpoint:            "/graphql",
		PlaygroundEndpoint:  "/playground",
		EnablePlayground:    true,
		EnableIntrospection: true,
		AutoGenerateSchema:  true,
		MaxComplexity:       1000,
		MaxDepth:            15,
		QueryTimeout:        30 * time.Second,
		EnableDataLoader:    true,
		DataLoaderBatchSize: 100,
		DataLoaderWait:      10 * time.Millisecond,
		EnableQueryCache:    true,
		QueryCacheTTL:       5 * time.Minute,
		MaxCacheSize:        1000,
		EnableCORS:          true,
		AllowedOrigins:      []string{"*"},
		MaxUploadSize:       10 * 1024 * 1024, // 10MB
		EnableMetrics:       true,
		EnableTracing:       true,
		EnableLogging:       true,
		LogSlowQueries:      true,
		SlowQueryThreshold:  1 * time.Second,
		RequireConfig:       false,
	}
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.Endpoint == "" {
		return fmt.Errorf("endpoint is required")
	}

	if c.MaxComplexity < 0 {
		return fmt.Errorf("max_complexity must be non-negative")
	}

	if c.MaxDepth < 1 {
		return fmt.Errorf("max_depth must be at least 1")
	}

	if c.QueryTimeout <= 0 {
		return fmt.Errorf("query_timeout must be positive")
	}

	if c.DataLoaderBatchSize < 1 {
		return fmt.Errorf("dataloader_batch_size must be at least 1")
	}

	if c.MaxUploadSize < 0 {
		return fmt.Errorf("max_upload_size must be non-negative")
	}

	return nil
}

// ConfigOption is a functional option for Config
type ConfigOption func(*Config)

func WithEndpoint(endpoint string) ConfigOption {
	return func(c *Config) { c.Endpoint = endpoint }
}

func WithPlayground(enable bool) ConfigOption {
	return func(c *Config) { c.EnablePlayground = enable }
}

func WithIntrospection(enable bool) ConfigOption {
	return func(c *Config) { c.EnableIntrospection = enable }
}

func WithMaxComplexity(max int) ConfigOption {
	return func(c *Config) { c.MaxComplexity = max }
}

func WithMaxDepth(max int) ConfigOption {
	return func(c *Config) { c.MaxDepth = max }
}

func WithTimeout(timeout time.Duration) ConfigOption {
	return func(c *Config) { c.QueryTimeout = timeout }
}

func WithDataLoader(enable bool) ConfigOption {
	return func(c *Config) { c.EnableDataLoader = enable }
}

func WithQueryCache(enable bool, ttl time.Duration) ConfigOption {
	return func(c *Config) {
		c.EnableQueryCache = enable
		c.QueryCacheTTL = ttl
	}
}

func WithCORS(origins ...string) ConfigOption {
	return func(c *Config) {
		c.EnableCORS = true
		c.AllowedOrigins = origins
	}
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
