package cron

import (
	"fmt"
	"time"
)

// Config contains configuration for the cron extension.
type Config struct {
	// Mode specifies the scheduler mode: "simple" or "distributed"
	Mode string `json:"mode" mapstructure:"mode" yaml:"mode"`

	// Storage backend: "memory", "database", "redis"
	Storage string `json:"storage" mapstructure:"storage" yaml:"storage"`

	// DatabaseConnection is the name of the database connection to use (when Storage="database")
	DatabaseConnection string `json:"databaseConnection,omitempty" mapstructure:"database_connection" yaml:"database_connection,omitempty"`

	// RedisConnection is the name of the Redis connection to use (when Storage="redis")
	RedisConnection string `json:"redisConnection,omitempty" mapstructure:"redis_connection" yaml:"redis_connection,omitempty"`

	// Scheduler settings
	MaxConcurrentJobs int           `json:"maxConcurrentJobs" mapstructure:"max_concurrent_jobs" yaml:"max_concurrent_jobs"`
	DefaultTimeout    time.Duration `json:"defaultTimeout" mapstructure:"default_timeout" yaml:"default_timeout"`
	DefaultTimezone   string        `json:"defaultTimezone" mapstructure:"default_timezone" yaml:"default_timezone"`

	// Retry policy
	MaxRetries      int           `json:"maxRetries" mapstructure:"max_retries" yaml:"max_retries"`
	RetryBackoff    time.Duration `json:"retryBackoff" mapstructure:"retry_backoff" yaml:"retry_backoff"`
	RetryMultiplier float64       `json:"retryMultiplier" mapstructure:"retry_multiplier" yaml:"retry_multiplier"`
	MaxRetryBackoff time.Duration `json:"maxRetryBackoff" mapstructure:"max_retry_backoff" yaml:"max_retry_backoff"`

	// History retention
	HistoryRetentionDays int `json:"historyRetentionDays" mapstructure:"history_retention_days" yaml:"history_retention_days"`
	MaxHistoryRecords    int `json:"maxHistoryRecords" mapstructure:"max_history_records" yaml:"max_history_records"`

	// Distributed mode settings
	LeaderElection     bool          `json:"leaderElection" mapstructure:"leader_election" yaml:"leader_election"`
	ConsensusExtension string        `json:"consensusExtension,omitempty" mapstructure:"consensus_extension" yaml:"consensus_extension,omitempty"`
	HeartbeatInterval  time.Duration `json:"heartbeatInterval" mapstructure:"heartbeat_interval" yaml:"heartbeat_interval"`
	LockTTL            time.Duration `json:"lockTTL" mapstructure:"lock_ttl" yaml:"lock_ttl"`

	// API settings
	EnableAPI   bool   `json:"enableApi" mapstructure:"enable_api" yaml:"enable_api"`
	APIPrefix   string `json:"apiPrefix" mapstructure:"api_prefix" yaml:"api_prefix"`
	EnableWebUI bool   `json:"enableWebUi" mapstructure:"enable_web_ui" yaml:"enable_web_ui"`

	// Monitoring
	EnableMetrics bool `json:"enableMetrics" mapstructure:"enable_metrics" yaml:"enable_metrics"`

	// Job loading
	ConfigFile string `json:"configFile,omitempty" mapstructure:"config_file" yaml:"config_file,omitempty"`

	// Graceful shutdown timeout
	ShutdownTimeout time.Duration `json:"shutdownTimeout" mapstructure:"shutdown_timeout" yaml:"shutdown_timeout"`

	// Node ID for distributed mode (auto-generated if empty)
	NodeID string `json:"nodeId,omitempty" mapstructure:"node_id" yaml:"node_id,omitempty"`

	// Config loading flags (not serialized)
	RequireConfig bool `json:"-" mapstructure:"-" yaml:"-"`
}

// DefaultConfig returns default cron configuration.
func DefaultConfig() Config {
	return Config{
		Mode:                 "simple",
		Storage:              "memory",
		MaxConcurrentJobs:    10,
		DefaultTimeout:       5 * time.Minute,
		DefaultTimezone:      "UTC",
		MaxRetries:           3,
		RetryBackoff:         1 * time.Second,
		RetryMultiplier:      2.0,
		MaxRetryBackoff:      30 * time.Second,
		HistoryRetentionDays: 30,
		MaxHistoryRecords:    10000,
		LeaderElection:       false,
		ConsensusExtension:   "consensus",
		HeartbeatInterval:    5 * time.Second,
		LockTTL:              30 * time.Second,
		EnableAPI:            true,
		APIPrefix:            "/api/cron",
		EnableWebUI:          true,
		EnableMetrics:        true,
		ShutdownTimeout:      30 * time.Second,
		RequireConfig:        false,
	}
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	// Validate mode
	if c.Mode != "simple" && c.Mode != "distributed" {
		return fmt.Errorf("%w: mode must be 'simple' or 'distributed', got '%s'", ErrInvalidConfig, c.Mode)
	}

	// Validate storage
	switch c.Storage {
	case "memory", "database", "redis":
		// Valid
	default:
		return fmt.Errorf("%w: storage must be 'memory', 'database', or 'redis', got '%s'", ErrInvalidConfig, c.Storage)
	}

	// Validate database connection when using database storage
	if c.Storage == "database" && c.DatabaseConnection == "" {
		return fmt.Errorf("%w: database_connection is required when storage is 'database'", ErrInvalidConfig)
	}

	// Validate Redis connection when using Redis storage
	if c.Storage == "redis" && c.RedisConnection == "" {
		return fmt.Errorf("%w: redis_connection is required when storage is 'redis'", ErrInvalidConfig)
	}

	// Distributed mode requires Redis storage
	if c.Mode == "distributed" && c.Storage != "redis" {
		return fmt.Errorf("%w: distributed mode requires redis storage, got '%s'", ErrInvalidConfig, c.Storage)
	}

	// Validate distributed mode settings
	if c.Mode == "distributed" {
		if c.LeaderElection && c.ConsensusExtension == "" {
			return fmt.Errorf("%w: consensus_extension is required when leader_election is enabled", ErrInvalidConfig)
		}

		if c.HeartbeatInterval <= 0 {
			return fmt.Errorf("%w: heartbeat_interval must be positive", ErrInvalidConfig)
		}

		if c.LockTTL <= 0 {
			return fmt.Errorf("%w: lock_ttl must be positive", ErrInvalidConfig)
		}
	}

	// Validate concurrency
	if c.MaxConcurrentJobs <= 0 {
		return fmt.Errorf("%w: max_concurrent_jobs must be positive", ErrInvalidConfig)
	}

	// Validate timeout
	if c.DefaultTimeout <= 0 {
		return fmt.Errorf("%w: default_timeout must be positive", ErrInvalidConfig)
	}

	// Validate retry settings
	if c.MaxRetries < 0 {
		return fmt.Errorf("%w: max_retries must be non-negative", ErrInvalidConfig)
	}

	if c.RetryBackoff < 0 {
		return fmt.Errorf("%w: retry_backoff must be non-negative", ErrInvalidConfig)
	}

	if c.RetryMultiplier < 1.0 {
		return fmt.Errorf("%w: retry_multiplier must be >= 1.0", ErrInvalidConfig)
	}

	if c.MaxRetryBackoff < 0 {
		return fmt.Errorf("%w: max_retry_backoff must be non-negative", ErrInvalidConfig)
	}

	// Validate history settings
	if c.HistoryRetentionDays < 0 {
		return fmt.Errorf("%w: history_retention_days must be non-negative", ErrInvalidConfig)
	}

	if c.MaxHistoryRecords < 0 {
		return fmt.Errorf("%w: max_history_records must be non-negative", ErrInvalidConfig)
	}

	// Validate shutdown timeout
	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("%w: shutdown_timeout must be positive", ErrInvalidConfig)
	}

	// Validate timezone
	if c.DefaultTimezone != "" {
		if _, err := time.LoadLocation(c.DefaultTimezone); err != nil {
			return fmt.Errorf("%w: invalid default_timezone '%s': %v", ErrInvalidConfig, c.DefaultTimezone, err)
		}
	}

	return nil
}

// ConfigOption is a functional option for Config.
type ConfigOption func(*Config)

// WithMode sets the scheduler mode.
func WithMode(mode string) ConfigOption {
	return func(c *Config) {
		c.Mode = mode
	}
}

// WithStorage sets the storage backend.
func WithStorage(storage string) ConfigOption {
	return func(c *Config) {
		c.Storage = storage
	}
}

// WithDatabaseConnection sets the database connection name.
func WithDatabaseConnection(name string) ConfigOption {
	return func(c *Config) {
		c.DatabaseConnection = name
	}
}

// WithRedisConnection sets the Redis connection name.
func WithRedisConnection(name string) ConfigOption {
	return func(c *Config) {
		c.RedisConnection = name
	}
}

// WithMaxConcurrentJobs sets the maximum concurrent jobs.
func WithMaxConcurrentJobs(max int) ConfigOption {
	return func(c *Config) {
		c.MaxConcurrentJobs = max
	}
}

// WithDefaultTimeout sets the default job timeout.
func WithDefaultTimeout(timeout time.Duration) ConfigOption {
	return func(c *Config) {
		c.DefaultTimeout = timeout
	}
}

// WithDefaultTimezone sets the default timezone.
func WithDefaultTimezone(tz string) ConfigOption {
	return func(c *Config) {
		c.DefaultTimezone = tz
	}
}

// WithMaxRetries sets the maximum retry attempts.
func WithMaxRetries(max int) ConfigOption {
	return func(c *Config) {
		c.MaxRetries = max
	}
}

// WithRetryBackoff sets the retry backoff duration.
func WithRetryBackoff(backoff time.Duration) ConfigOption {
	return func(c *Config) {
		c.RetryBackoff = backoff
	}
}

// WithRetryMultiplier sets the retry multiplier for exponential backoff.
func WithRetryMultiplier(multiplier float64) ConfigOption {
	return func(c *Config) {
		c.RetryMultiplier = multiplier
	}
}

// WithMaxRetryBackoff sets the maximum retry backoff duration.
func WithMaxRetryBackoff(max time.Duration) ConfigOption {
	return func(c *Config) {
		c.MaxRetryBackoff = max
	}
}

// WithHistoryRetention sets the history retention in days.
func WithHistoryRetention(days int) ConfigOption {
	return func(c *Config) {
		c.HistoryRetentionDays = days
	}
}

// WithMaxHistoryRecords sets the maximum history records to keep.
func WithMaxHistoryRecords(max int) ConfigOption {
	return func(c *Config) {
		c.MaxHistoryRecords = max
	}
}

// WithLeaderElection enables/disables leader election.
func WithLeaderElection(enable bool) ConfigOption {
	return func(c *Config) {
		c.LeaderElection = enable
	}
}

// WithConsensusExtension sets the consensus extension name.
func WithConsensusExtension(name string) ConfigOption {
	return func(c *Config) {
		c.ConsensusExtension = name
	}
}

// WithHeartbeatInterval sets the heartbeat interval for distributed mode.
func WithHeartbeatInterval(interval time.Duration) ConfigOption {
	return func(c *Config) {
		c.HeartbeatInterval = interval
	}
}

// WithLockTTL sets the lock TTL for distributed locking.
func WithLockTTL(ttl time.Duration) ConfigOption {
	return func(c *Config) {
		c.LockTTL = ttl
	}
}

// WithAPI enables/disables the REST API.
func WithAPI(enable bool) ConfigOption {
	return func(c *Config) {
		c.EnableAPI = enable
	}
}

// WithAPIPrefix sets the API prefix.
func WithAPIPrefix(prefix string) ConfigOption {
	return func(c *Config) {
		c.APIPrefix = prefix
	}
}

// WithWebUI enables/disables the web UI.
func WithWebUI(enable bool) ConfigOption {
	return func(c *Config) {
		c.EnableWebUI = enable
	}
}

// WithMetrics enables/disables metrics collection.
func WithMetrics(enable bool) ConfigOption {
	return func(c *Config) {
		c.EnableMetrics = enable
	}
}

// WithConfigFile sets the jobs configuration file path.
func WithConfigFile(path string) ConfigOption {
	return func(c *Config) {
		c.ConfigFile = path
	}
}

// WithShutdownTimeout sets the graceful shutdown timeout.
func WithShutdownTimeout(timeout time.Duration) ConfigOption {
	return func(c *Config) {
		c.ShutdownTimeout = timeout
	}
}

// WithNodeID sets the node ID for distributed mode.
func WithNodeID(id string) ConfigOption {
	return func(c *Config) {
		c.NodeID = id
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
