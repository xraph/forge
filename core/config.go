package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
	"github.com/xraph/forge/logger"
	"gopkg.in/yaml.v3"
)

// config implements the Config interface
type config struct {
	viper        *viper.Viper
	mu           sync.RWMutex
	watchers     []func(Config)
	keyCallbacks map[string][]func(interface{})
}

// ConfigFile represents the complete configuration structure
type ConfigFile struct {
	App           AppConfig              `mapstructure:"app" yaml:"app"`
	Server        ServerConfig           `mapstructure:"server" yaml:"server"`
	Logging       logger.LoggingConfig   `mapstructure:"logging" yaml:"logging"`
	Database      map[string]interface{} `mapstructure:"database" yaml:"database"`
	Cache         map[string]interface{} `mapstructure:"cache" yaml:"cache"`
	Observability ObservabilityConfig    `mapstructure:"observability" yaml:"observability"`
	Jobs          JobsConfig             `mapstructure:"jobs" yaml:"jobs"`
	Security      SecurityConfig         `mapstructure:"security" yaml:"security"`
	Plugins       map[string]interface{} `mapstructure:"plugins" yaml:"plugins"`
	Custom        map[string]interface{} `mapstructure:"custom" yaml:"custom"`
}

// ObservabilityConfig represents observability configuration
type ObservabilityConfig struct {
	Tracing TracingConfig `mapstructure:"tracing" yaml:"tracing"`
	Metrics MetricsConfig `mapstructure:"metrics" yaml:"metrics"`
	Health  HealthConfig  `mapstructure:"health" yaml:"health"`
}

// TracingConfig represents tracing configuration
type TracingConfig struct {
	Enabled     bool    `mapstructure:"enabled" yaml:"enabled" env:"FORGE_TRACING_ENABLED"`
	ServiceName string  `mapstructure:"service_name" yaml:"service_name" env:"FORGE_TRACING_SERVICE_NAME"`
	Endpoint    string  `mapstructure:"endpoint" yaml:"endpoint" env:"FORGE_TRACING_ENDPOINT"`
	SampleRate  float64 `mapstructure:"sample_rate" yaml:"sample_rate" env:"FORGE_TRACING_SAMPLE_RATE"`
}

// MetricsConfig represents metrics configuration
type MetricsConfig struct {
	Enabled     bool   `mapstructure:"enabled" yaml:"enabled" env:"FORGE_METRICS_ENABLED"`
	Port        int    `mapstructure:"port" yaml:"port" env:"FORGE_METRICS_PORT"`
	Path        string `mapstructure:"path" yaml:"path" env:"FORGE_METRICS_PATH"`
	ServiceName string `mapstructure:"service_name" yaml:"service_name" env:"FORGE_METRICS_SERVICE_NAME"`
}

// HealthConfig represents health check configuration
type HealthConfig struct {
	Enabled  bool          `mapstructure:"enabled" yaml:"enabled" env:"FORGE_HEALTH_ENABLED"`
	Path     string        `mapstructure:"path" yaml:"path" env:"FORGE_HEALTH_PATH"`
	Timeout  time.Duration `mapstructure:"timeout" yaml:"timeout" env:"FORGE_HEALTH_TIMEOUT"`
	Interval time.Duration `mapstructure:"interval" yaml:"interval" env:"FORGE_HEALTH_INTERVAL"`
}

// JobsConfig represents job processing configuration
type JobsConfig struct {
	Enabled     bool            `mapstructure:"enabled" yaml:"enabled" env:"FORGE_JOBS_ENABLED"`
	Backend     string          `mapstructure:"backend" yaml:"backend" env:"FORGE_JOBS_BACKEND"`
	Concurrency int             `mapstructure:"concurrency" yaml:"concurrency" env:"FORGE_JOBS_CONCURRENCY"`
	Queues      map[string]int  `mapstructure:"queues" yaml:"queues"`
	Redis       RedisJobsConfig `mapstructure:"redis" yaml:"redis"`
}

// RedisJobsConfig represents Redis-specific job configuration
type RedisJobsConfig struct {
	URL      string `mapstructure:"url" yaml:"url" env:"FORGE_JOBS_REDIS_URL"`
	Database int    `mapstructure:"database" yaml:"database" env:"FORGE_JOBS_REDIS_DATABASE"`
	Prefix   string `mapstructure:"prefix" yaml:"prefix" env:"FORGE_JOBS_REDIS_PREFIX"`
}

// SecurityConfig represents security configuration
type SecurityConfig struct {
	AllowedOrigins        []string `mapstructure:"allowed_origins" yaml:"allowed_origins" env:"FORGE_SECURITY_ALLOWED_ORIGINS"`
	AllowedMethods        []string `mapstructure:"allowed_methods" yaml:"allowed_methods"`
	AllowedHeaders        []string `mapstructure:"allowed_headers" yaml:"allowed_headers"`
	ExposedHeaders        []string `mapstructure:"exposed_headers" yaml:"exposed_headers"`
	AllowCredentials      bool     `mapstructure:"allow_credentials" yaml:"allow_credentials" env:"FORGE_SECURITY_ALLOW_CREDENTIALS"`
	ContentTypeOptions    string   `mapstructure:"content_type_options" yaml:"content_type_options"`
	XFrameOptions         string   `mapstructure:"x_frame_options" yaml:"x_frame_options"`
	XSSProtection         string   `mapstructure:"xss_protection" yaml:"xss_protection"`
	ReferrerPolicy        string   `mapstructure:"referrer_policy" yaml:"referrer_policy"`
	ContentSecurityPolicy string   `mapstructure:"content_security_policy" yaml:"content_security_policy"`
	HSTSMaxAge            int      `mapstructure:"hsts_max_age" yaml:"hsts_max_age"`
	HSTSIncludeSubdomains bool     `mapstructure:"hsts_include_subdomains" yaml:"hsts_include_subdomains"`
}

// NewConfig creates a new configuration instance
func NewConfig() Config {
	v := viper.New()

	// Set defaults
	setDefaults(v)

	// Setup environment variable handling
	v.SetEnvPrefix("FORGE")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	return &config{
		viper:        v,
		keyCallbacks: make(map[string][]func(interface{})),
	}
}

// LoadConfig loads configuration from file and environment
func LoadConfig(paths ...string) (Config, error) {
	cfg := NewConfig().(*config)

	// Determine config paths
	configPaths := []string{".", "./config", "/etc/forge"}
	if len(paths) > 0 {
		configPaths = paths
	}

	// Try to find and load config file
	for _, path := range configPaths {
		cfg.viper.AddConfigPath(path)
	}

	cfg.viper.SetConfigName("config")
	cfg.viper.SetConfigType("yaml")

	// Try to read config file (don't error if not found)
	if err := cfg.viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
	}

	return cfg, nil
}

// LoadConfigFromFile loads configuration from a specific file
func LoadConfigFromFile(path string) (Config, error) {
	cfg := NewConfig().(*config)

	cfg.viper.SetConfigFile(path)

	if err := cfg.viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	return cfg, nil
}

// LoadConfigFromBytes loads configuration from a specific file
func LoadConfigFromBytes(data []byte) (Config, error) {
	var cfg config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if err := cfg.viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	return &cfg, nil
}

// setDefaults sets default configuration values
func setDefaults(v *viper.Viper) {
	// App defaults
	v.SetDefault("app.name", "forge-app")
	v.SetDefault("app.version", "1.0.0")
	v.SetDefault("app.environment", "development")
	v.SetDefault("app.debug", false)

	// Server defaults
	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.port", 8080)
	v.SetDefault("server.read_timeout", "30s")
	v.SetDefault("server.write_timeout", "30s")
	v.SetDefault("server.idle_timeout", "120s")
	v.SetDefault("server.shutdown_timeout", "30s")
	v.SetDefault("server.enable_http2", true)
	v.SetDefault("server.graceful_shutdown", true)
	v.SetDefault("server.tls.enabled", false)

	// Logging defaults
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.format", "json")
	v.SetDefault("logging.output", "stdout")

	// Observability defaults
	v.SetDefault("observability.tracing.enabled", false)
	v.SetDefault("observability.tracing.sample_rate", 0.1)
	v.SetDefault("observability.metrics.enabled", true)
	v.SetDefault("observability.metrics.port", 9090)
	v.SetDefault("observability.metrics.path", "/metrics")
	v.SetDefault("observability.health.enabled", true)
	v.SetDefault("observability.health.path", "/health")
	v.SetDefault("observability.health.timeout", "30s")
	v.SetDefault("observability.health.interval", "30s")

	// Jobs defaults
	v.SetDefault("jobs.enabled", false)
	v.SetDefault("jobs.backend", "memory")
	v.SetDefault("jobs.concurrency", 10)
	v.SetDefault("jobs.queues.default", 5)
	v.SetDefault("jobs.queues.critical", 10)
	v.SetDefault("jobs.redis.database", 0)
	v.SetDefault("jobs.redis.prefix", "forge:jobs")

	// Security defaults
	v.SetDefault("security.allowed_origins", []string{"*"})
	v.SetDefault("security.allowed_methods", []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"})
	v.SetDefault("security.allowed_headers", []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"})
	v.SetDefault("security.allow_credentials", true)
	v.SetDefault("security.content_type_options", "nosniff")
	v.SetDefault("security.x_frame_options", "DENY")
	v.SetDefault("security.xss_protection", "1; mode=block")
	v.SetDefault("security.referrer_policy", "strict-origin-when-cross-origin")
	v.SetDefault("security.hsts_max_age", 31536000) // 1 year
	v.SetDefault("security.hsts_include_subdomains", true)
}

// Implementation of Config interface

func (c *config) Get(key string) interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.viper.Get(key)
}

func (c *config) GetString(key string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.viper.GetString(key)
}

func (c *config) GetInt(key string) int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.viper.GetInt(key)
}

func (c *config) GetBool(key string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.viper.GetBool(key)
}

func (c *config) GetDuration(key string) time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.viper.GetDuration(key)
}

func (c *config) GetStringSlice(key string) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.viper.GetStringSlice(key)
}

func (c *config) GetFloat64(key string) float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.viper.GetFloat64(key)
}

func (c *config) Sub(key string) Config {
	c.mu.RLock()
	defer c.mu.RUnlock()

	sub := c.viper.Sub(key)
	if sub == nil {
		// Return empty config if sub doesn't exist
		sub = viper.New()
	}

	return &config{
		viper:        sub,
		keyCallbacks: make(map[string][]func(interface{})),
	}
}

func (c *config) Validate() error {
	// Basic validation
	if c.GetString("app.name") == "" {
		return fmt.Errorf("app.name is required")
	}

	if c.GetInt("server.port") <= 0 || c.GetInt("server.port") > 65535 {
		return fmt.Errorf("server.port must be between 1 and 65535")
	}

	// Validate TLS configuration
	if c.GetBool("server.tls.enabled") {
		certFile := c.GetString("server.tls.cert_file")
		keyFile := c.GetString("server.tls.key_file")

		if certFile == "" || keyFile == "" {
			return fmt.Errorf("TLS cert_file and key_file are required when TLS is enabled")
		}

		if _, err := os.Stat(certFile); os.IsNotExist(err) {
			return fmt.Errorf("TLS cert_file does not exist: %s", certFile)
		}

		if _, err := os.Stat(keyFile); os.IsNotExist(err) {
			return fmt.Errorf("TLS key_file does not exist: %s", keyFile)
		}
	}

	// Validate logging level
	validLevels := []string{"debug", "info", "warn", "error", "fatal"}
	level := c.GetString("logging.level")
	isValid := false
	for _, validLevel := range validLevels {
		if level == validLevel {
			isValid = true
			break
		}
	}
	if !isValid {
		return fmt.Errorf("logging.level must be one of: %s", strings.Join(validLevels, ", "))
	}

	return nil
}

func (c *config) Environment() string {
	return c.GetString("app.environment")
}

func (c *config) IsDevelopment() bool {
	return c.Environment() == "development"
}

func (c *config) IsProduction() bool {
	return c.Environment() == "production"
}

func (c *config) Watch(callback func(Config)) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.watchers = append(c.watchers, callback)

	// Setup viper watcher if this is the first callback
	if len(c.watchers) == 1 {
		c.viper.WatchConfig()
		c.viper.OnConfigChange(func(e fsnotify.Event) {
			c.mu.RLock()
			watchers := c.watchers
			c.mu.RUnlock()

			for _, watcher := range watchers {
				watcher(c)
			}
		})
	}

	return nil
}

func (c *config) OnChange(key string, callback func(interface{})) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.keyCallbacks[key] == nil {
		c.keyCallbacks[key] = make([]func(interface{}), 0)
	}

	c.keyCallbacks[key] = append(c.keyCallbacks[key], callback)

	return nil
}

func (c *config) AllKeys() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.viper.AllKeys()
}

func (c *config) All() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.viper.AllSettings()
}

// Helper functions

// SaveConfigToFile saves the current configuration to a file
func SaveConfigToFile(cfg Config, path string) error {
	configData := extractConfigData(cfg)

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Marshal to YAML
	data, err := yaml.Marshal(configData)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write to file
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// extractConfigData extracts config data into a structured format
func extractConfigData(cfg Config) ConfigFile {
	return ConfigFile{
		App: AppConfig{
			Name:        cfg.GetString("app.name"),
			Version:     cfg.GetString("app.version"),
			Environment: cfg.GetString("app.environment"),
			Debug:       cfg.GetBool("app.debug"),
			BaseURL:     cfg.GetString("app.base_url"),
		},
		Server: ServerConfig{
			Host:             cfg.GetString("server.host"),
			Port:             cfg.GetInt("server.port"),
			ReadTimeout:      cfg.GetDuration("server.read_timeout"),
			WriteTimeout:     cfg.GetDuration("server.write_timeout"),
			IdleTimeout:      cfg.GetDuration("server.idle_timeout"),
			ShutdownTimeout:  cfg.GetDuration("server.shutdown_timeout"),
			EnableHTTP2:      cfg.GetBool("server.enable_http2"),
			GracefulShutdown: cfg.GetBool("server.graceful_shutdown"),
			TLS: TLSConfig{
				Enabled:  cfg.GetBool("server.tls.enabled"),
				CertFile: cfg.GetString("server.tls.cert_file"),
				KeyFile:  cfg.GetString("server.tls.key_file"),
			},
		},
		Logging: logger.LoggingConfig{
			Level:       cfg.GetString("logging.level"),
			Format:      cfg.GetString("logging.format"),
			Environment: cfg.GetString("logging.environment"),
			Output:      cfg.GetString("logging.output"),
		},
		// Add other sections as needed
	}
}

// MergeConfigs merges multiple configurations with the latter taking precedence
func MergeConfigs(configs ...Config) Config {
	if len(configs) == 0 {
		return NewConfig()
	}

	if len(configs) == 1 {
		return configs[0]
	}

	// Create new viper instance
	merged := viper.New()

	// Merge each config
	for _, cfg := range configs {
		if c, ok := cfg.(*config); ok {
			mergeViper(merged, c.viper)
		}
	}

	return &config{
		viper:        merged,
		keyCallbacks: make(map[string][]func(interface{})),
	}
}

// mergeViper merges one viper instance into another
func mergeViper(dst, src *viper.Viper) {
	for _, key := range src.AllKeys() {
		dst.Set(key, src.Get(key))
	}
}
