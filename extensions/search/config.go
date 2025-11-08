package search

import (
	"fmt"
	"time"
)

// Config contains configuration for the search extension.
type Config struct {
	// Driver specifies the search backend: "inmemory", "elasticsearch", "meilisearch", "typesense"
	Driver string `json:"driver" mapstructure:"driver" yaml:"driver"`

	// Connection settings
	URL      string   `json:"url,omitempty"      mapstructure:"url"      yaml:"url,omitempty"`
	Hosts    []string `json:"hosts,omitempty"    mapstructure:"hosts"    yaml:"hosts,omitempty"`
	Username string   `json:"username,omitempty" mapstructure:"username" yaml:"username,omitempty"`
	Password string   `json:"password,omitempty" mapstructure:"password" yaml:"password,omitempty"`
	APIKey   string   `json:"api_key,omitempty"  mapstructure:"api_key"  yaml:"api_key,omitempty"`

	// Connection pool
	MaxConnections     int           `json:"max_connections"      mapstructure:"max_connections"      yaml:"max_connections"`
	MaxIdleConnections int           `json:"max_idle_connections" mapstructure:"max_idle_connections" yaml:"max_idle_connections"`
	ConnectTimeout     time.Duration `json:"connect_timeout"      mapstructure:"connect_timeout"      yaml:"connect_timeout"`
	RequestTimeout     time.Duration `json:"request_timeout"      mapstructure:"request_timeout"      yaml:"request_timeout"`
	KeepAlive          time.Duration `json:"keep_alive"           mapstructure:"keep_alive"           yaml:"keep_alive"`

	// Retry policy
	MaxRetries     int           `json:"max_retries"      mapstructure:"max_retries"      yaml:"max_retries"`
	RetryBackoff   time.Duration `json:"retry_backoff"    mapstructure:"retry_backoff"    yaml:"retry_backoff"`
	RetryOnTimeout bool          `json:"retry_on_timeout" mapstructure:"retry_on_timeout" yaml:"retry_on_timeout"`

	// Default search settings
	DefaultLimit    int     `json:"default_limit"     mapstructure:"default_limit"     yaml:"default_limit"`
	MaxLimit        int     `json:"max_limit"         mapstructure:"max_limit"         yaml:"max_limit"`
	DefaultMinScore float64 `json:"default_min_score" mapstructure:"default_min_score" yaml:"default_min_score"`
	EnableHighlight bool    `json:"enable_highlight"  mapstructure:"enable_highlight"  yaml:"enable_highlight"`
	EnableFacets    bool    `json:"enable_facets"     mapstructure:"enable_facets"     yaml:"enable_facets"`

	// Performance
	BulkSize          int           `json:"bulk_size"          mapstructure:"bulk_size"          yaml:"bulk_size"`
	FlushInterval     time.Duration `json:"flush_interval"     mapstructure:"flush_interval"     yaml:"flush_interval"`
	EnableCompression bool          `json:"enable_compression" mapstructure:"enable_compression" yaml:"enable_compression"`

	// Security
	EnableTLS          bool   `json:"enable_tls"              mapstructure:"enable_tls"           yaml:"enable_tls"`
	TLSCertFile        string `json:"tls_cert_file,omitempty" mapstructure:"tls_cert_file"        yaml:"tls_cert_file,omitempty"`
	TLSKeyFile         string `json:"tls_key_file,omitempty"  mapstructure:"tls_key_file"         yaml:"tls_key_file,omitempty"`
	TLSCAFile          string `json:"tls_ca_file,omitempty"   mapstructure:"tls_ca_file"          yaml:"tls_ca_file,omitempty"`
	InsecureSkipVerify bool   `json:"insecure_skip_verify"    mapstructure:"insecure_skip_verify" yaml:"insecure_skip_verify"`

	// Monitoring
	EnableMetrics bool `json:"enable_metrics" mapstructure:"enable_metrics" yaml:"enable_metrics"`
	EnableTracing bool `json:"enable_tracing" mapstructure:"enable_tracing" yaml:"enable_tracing"`

	// Config loading flags (not serialized)
	RequireConfig bool `json:"-" mapstructure:"-" yaml:"-"`
}

// DefaultConfig returns default search configuration.
func DefaultConfig() Config {
	return Config{
		Driver:             "inmemory",
		URL:                "",
		MaxConnections:     10,
		MaxIdleConnections: 5,
		ConnectTimeout:     10 * time.Second,
		RequestTimeout:     30 * time.Second,
		KeepAlive:          60 * time.Second,
		MaxRetries:         3,
		RetryBackoff:       100 * time.Millisecond,
		RetryOnTimeout:     true,
		DefaultLimit:       20,
		MaxLimit:           100,
		DefaultMinScore:    0.0,
		EnableHighlight:    true,
		EnableFacets:       true,
		BulkSize:           100,
		FlushInterval:      5 * time.Second,
		EnableCompression:  false,
		EnableTLS:          false,
		InsecureSkipVerify: false,
		EnableMetrics:      true,
		EnableTracing:      true,
		RequireConfig:      false,
	}
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.Driver == "" {
		return errors.New("driver is required")
	}

	switch c.Driver {
	case "inmemory":
		// No additional validation needed
	case "elasticsearch":
		if c.URL == "" && len(c.Hosts) == 0 {
			return errors.New("elasticsearch requires url or hosts")
		}
	case "meilisearch":
		if c.URL == "" {
			return errors.New("meilisearch requires url")
		}
	case "typesense":
		if c.URL == "" && len(c.Hosts) == 0 {
			return errors.New("typesense requires url or hosts")
		}

		if c.APIKey == "" {
			return errors.New("typesense requires api_key")
		}
	default:
		return fmt.Errorf("unsupported driver: %s", c.Driver)
	}

	if c.MaxConnections <= 0 {
		return errors.New("max_connections must be positive")
	}

	if c.MaxIdleConnections < 0 {
		return errors.New("max_idle_connections must be non-negative")
	}

	if c.MaxIdleConnections > c.MaxConnections {
		return errors.New("max_idle_connections cannot exceed max_connections")
	}

	if c.ConnectTimeout <= 0 {
		return errors.New("connect_timeout must be positive")
	}

	if c.RequestTimeout <= 0 {
		return errors.New("request_timeout must be positive")
	}

	if c.DefaultLimit <= 0 {
		return errors.New("default_limit must be positive")
	}

	if c.MaxLimit < c.DefaultLimit {
		return errors.New("max_limit must be >= default_limit")
	}

	if c.BulkSize <= 0 {
		return errors.New("bulk_size must be positive")
	}

	return nil
}

// ConfigOption is a functional option for Config.
type ConfigOption func(*Config)

// WithDriver sets the driver.
func WithDriver(driver string) ConfigOption {
	return func(c *Config) {
		c.Driver = driver
	}
}

// WithURL sets the URL.
func WithURL(url string) ConfigOption {
	return func(c *Config) {
		c.URL = url
	}
}

// WithHosts sets the hosts.
func WithHosts(hosts ...string) ConfigOption {
	return func(c *Config) {
		c.Hosts = hosts
	}
}

// WithAuth sets authentication credentials.
func WithAuth(username, password string) ConfigOption {
	return func(c *Config) {
		c.Username = username
		c.Password = password
	}
}

// WithAPIKey sets the API key.
func WithAPIKey(apiKey string) ConfigOption {
	return func(c *Config) {
		c.APIKey = apiKey
	}
}

// WithMaxConnections sets max connections.
func WithMaxConnections(max int) ConfigOption {
	return func(c *Config) {
		c.MaxConnections = max
	}
}

// WithTimeout sets request timeout.
func WithTimeout(timeout time.Duration) ConfigOption {
	return func(c *Config) {
		c.RequestTimeout = timeout
	}
}

// WithDefaultLimit sets default result limit.
func WithDefaultLimit(limit int) ConfigOption {
	return func(c *Config) {
		c.DefaultLimit = limit
	}
}

// WithMaxLimit sets maximum result limit.
func WithMaxLimit(limit int) ConfigOption {
	return func(c *Config) {
		c.MaxLimit = limit
	}
}

// WithTLS enables TLS.
func WithTLS(certFile, keyFile, caFile string) ConfigOption {
	return func(c *Config) {
		c.EnableTLS = true
		c.TLSCertFile = certFile
		c.TLSKeyFile = keyFile
		c.TLSCAFile = caFile
	}
}

// WithMetrics enables metrics.
func WithMetrics(enable bool) ConfigOption {
	return func(c *Config) {
		c.EnableMetrics = enable
	}
}

// WithTracing enables tracing.
func WithTracing(enable bool) ConfigOption {
	return func(c *Config) {
		c.EnableTracing = enable
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
