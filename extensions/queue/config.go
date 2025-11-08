package queue

import (
	"fmt"
	"time"

	"github.com/xraph/forge/internal/errors"
)

// Config contains configuration for the queue extension.
type Config struct {
	// Driver specifies the queue backend: "inmemory", "redis", "rabbitmq", "nats"
	Driver string `json:"driver" mapstructure:"driver" yaml:"driver"`

	// Connection settings
	URL      string   `json:"url,omitempty"      mapstructure:"url"      yaml:"url,omitempty"`
	Hosts    []string `json:"hosts,omitempty"    mapstructure:"hosts"    yaml:"hosts,omitempty"`
	Username string   `json:"username,omitempty" mapstructure:"username" yaml:"username,omitempty"`
	Password string   `json:"password,omitempty" mapstructure:"password" yaml:"password,omitempty"`
	VHost    string   `json:"vhost,omitempty"    mapstructure:"vhost"    yaml:"vhost,omitempty"` // RabbitMQ only

	// Connection pool
	MaxConnections     int           `json:"max_connections"      mapstructure:"max_connections"      yaml:"max_connections"`
	MaxIdleConnections int           `json:"max_idle_connections" mapstructure:"max_idle_connections" yaml:"max_idle_connections"`
	ConnectTimeout     time.Duration `json:"connect_timeout"      mapstructure:"connect_timeout"      yaml:"connect_timeout"`
	ReadTimeout        time.Duration `json:"read_timeout"         mapstructure:"read_timeout"         yaml:"read_timeout"`
	WriteTimeout       time.Duration `json:"write_timeout"        mapstructure:"write_timeout"        yaml:"write_timeout"`
	KeepAlive          time.Duration `json:"keep_alive"           mapstructure:"keep_alive"           yaml:"keep_alive"`

	// Retry policy
	MaxRetries      int           `json:"max_retries"       mapstructure:"max_retries"       yaml:"max_retries"`
	RetryBackoff    time.Duration `json:"retry_backoff"     mapstructure:"retry_backoff"     yaml:"retry_backoff"`
	RetryMultiplier float64       `json:"retry_multiplier"  mapstructure:"retry_multiplier"  yaml:"retry_multiplier"`
	MaxRetryBackoff time.Duration `json:"max_retry_backoff" mapstructure:"max_retry_backoff" yaml:"max_retry_backoff"`

	// Default queue settings
	DefaultPrefetch    int           `json:"default_prefetch"    mapstructure:"default_prefetch"    yaml:"default_prefetch"`
	DefaultConcurrency int           `json:"default_concurrency" mapstructure:"default_concurrency" yaml:"default_concurrency"`
	DefaultTimeout     time.Duration `json:"default_timeout"     mapstructure:"default_timeout"     yaml:"default_timeout"`
	EnableDeadLetter   bool          `json:"enable_dead_letter"  mapstructure:"enable_dead_letter"  yaml:"enable_dead_letter"`
	DeadLetterSuffix   string        `json:"dead_letter_suffix"  mapstructure:"dead_letter_suffix"  yaml:"dead_letter_suffix"`

	// Performance
	EnablePersistence bool  `json:"enable_persistence" mapstructure:"enable_persistence" yaml:"enable_persistence"`
	EnablePriority    bool  `json:"enable_priority"    mapstructure:"enable_priority"    yaml:"enable_priority"`
	EnableDelayed     bool  `json:"enable_delayed"     mapstructure:"enable_delayed"     yaml:"enable_delayed"`
	MaxMessageSize    int64 `json:"max_message_size"   mapstructure:"max_message_size"   yaml:"max_message_size"`

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

// DefaultConfig returns default queue configuration.
func DefaultConfig() Config {
	return Config{
		Driver:             "inmemory",
		URL:                "",
		MaxConnections:     10,
		MaxIdleConnections: 5,
		ConnectTimeout:     10 * time.Second,
		ReadTimeout:        30 * time.Second,
		WriteTimeout:       30 * time.Second,
		KeepAlive:          60 * time.Second,
		MaxRetries:         3,
		RetryBackoff:       100 * time.Millisecond,
		RetryMultiplier:    2.0,
		MaxRetryBackoff:    30 * time.Second,
		DefaultPrefetch:    10,
		DefaultConcurrency: 1,
		DefaultTimeout:     30 * time.Second,
		EnableDeadLetter:   true,
		DeadLetterSuffix:   ".dlq",
		EnablePersistence:  true,
		EnablePriority:     false,
		EnableDelayed:      false,
		MaxMessageSize:     1048576, // 1MB
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
	case "redis":
		if c.URL == "" && len(c.Hosts) == 0 {
			return errors.New("redis requires url or hosts")
		}
	case "rabbitmq":
		if c.URL == "" && len(c.Hosts) == 0 {
			return errors.New("rabbitmq requires url or hosts")
		}
	case "nats":
		if c.URL == "" && len(c.Hosts) == 0 {
			return errors.New("nats requires url or hosts")
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

	if c.DefaultPrefetch < 0 {
		return errors.New("default_prefetch must be non-negative")
	}

	if c.DefaultConcurrency <= 0 {
		return errors.New("default_concurrency must be positive")
	}

	if c.MaxMessageSize <= 0 {
		return errors.New("max_message_size must be positive")
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

// WithVHost sets the virtual host (RabbitMQ only).
func WithVHost(vhost string) ConfigOption {
	return func(c *Config) {
		c.VHost = vhost
	}
}

// WithMaxConnections sets max connections.
func WithMaxConnections(max int) ConfigOption {
	return func(c *Config) {
		c.MaxConnections = max
	}
}

// WithPrefetch sets default prefetch count.
func WithPrefetch(prefetch int) ConfigOption {
	return func(c *Config) {
		c.DefaultPrefetch = prefetch
	}
}

// WithConcurrency sets default concurrency.
func WithConcurrency(concurrency int) ConfigOption {
	return func(c *Config) {
		c.DefaultConcurrency = concurrency
	}
}

// WithTimeout sets default timeout.
func WithTimeout(timeout time.Duration) ConfigOption {
	return func(c *Config) {
		c.DefaultTimeout = timeout
	}
}

// WithDeadLetter enables/disables dead letter queue.
func WithDeadLetter(enable bool) ConfigOption {
	return func(c *Config) {
		c.EnableDeadLetter = enable
	}
}

// WithPersistence enables/disables message persistence.
func WithPersistence(enable bool) ConfigOption {
	return func(c *Config) {
		c.EnablePersistence = enable
	}
}

// WithPriority enables/disables priority queues.
func WithPriority(enable bool) ConfigOption {
	return func(c *Config) {
		c.EnablePriority = enable
	}
}

// WithDelayed enables/disables delayed messages.
func WithDelayed(enable bool) ConfigOption {
	return func(c *Config) {
		c.EnableDelayed = enable
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
