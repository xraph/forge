package raft

import (
	"fmt"
	"time"

	"github.com/xraph/forge/internal/errors"
)

// Config contains Raft algorithm configuration.
type Config struct {
	// Node identification
	NodeID    string `json:"node_id"    yaml:"node_id"`
	ClusterID string `json:"cluster_id" yaml:"cluster_id"`

	// Timing parameters
	ElectionTimeoutMin time.Duration `json:"election_timeout_min" yaml:"election_timeout_min"`
	ElectionTimeoutMax time.Duration `json:"election_timeout_max" yaml:"election_timeout_max"`
	HeartbeatInterval  time.Duration `json:"heartbeat_interval"   yaml:"heartbeat_interval"`
	HeartbeatTimeout   time.Duration `json:"heartbeat_timeout"    yaml:"heartbeat_timeout"`
	ApplyInterval      time.Duration `json:"apply_interval"       yaml:"apply_interval"`
	CommitTimeout      time.Duration `json:"commit_timeout"       yaml:"commit_timeout"`

	// Snapshot configuration
	SnapshotInterval  time.Duration `json:"snapshot_interval"  yaml:"snapshot_interval"`
	SnapshotThreshold uint64        `json:"snapshot_threshold" yaml:"snapshot_threshold"`
	EnableSnapshots   bool          `json:"enable_snapshots"   yaml:"enable_snapshots"`

	// Replication configuration
	MaxAppendEntries     int           `json:"max_append_entries"     yaml:"max_append_entries"`
	ReplicationBatchSize int           `json:"replication_batch_size" yaml:"replication_batch_size"`
	ReplicationTimeout   time.Duration `json:"replication_timeout"    yaml:"replication_timeout"`
	EnablePipeline       bool          `json:"enable_pipeline"        yaml:"enable_pipeline"`

	// Performance tuning
	MaxInflightReplications int           `json:"max_inflight_replications" yaml:"max_inflight_replications"`
	BatchInterval           time.Duration `json:"batch_interval"            yaml:"batch_interval"`
	MaxBatchSize            int           `json:"max_batch_size"            yaml:"max_batch_size"`

	// Log configuration
	MaxLogEntries        uint64 `json:"max_log_entries"        yaml:"max_log_entries"`
	LogCacheSize         int    `json:"log_cache_size"         yaml:"log_cache_size"`
	LogCompactionEnabled bool   `json:"log_compaction_enabled" yaml:"log_compaction_enabled"`
	TrailingLogs         uint64 `json:"trailing_logs"          yaml:"trailing_logs"`

	// Safety
	PreVoteEnabled bool `json:"pre_vote_enabled" yaml:"pre_vote_enabled"`
	CheckQuorum    bool `json:"check_quorum"     yaml:"check_quorum"`

	// Observability
	EnableMetrics bool `json:"enable_metrics" yaml:"enable_metrics"`
	EnableTracing bool `json:"enable_tracing" yaml:"enable_tracing"`
}

// DefaultConfig returns default Raft configuration.
func DefaultConfig() Config {
	return Config{
		NodeID:    "",
		ClusterID: "",

		// Timing - following Raft paper recommendations
		ElectionTimeoutMin: 150 * time.Millisecond,
		ElectionTimeoutMax: 300 * time.Millisecond,
		HeartbeatInterval:  50 * time.Millisecond,
		HeartbeatTimeout:   500 * time.Millisecond,
		ApplyInterval:      10 * time.Millisecond,
		CommitTimeout:      50 * time.Millisecond,

		// Snapshots
		SnapshotInterval:  5 * time.Minute,
		SnapshotThreshold: 10000,
		EnableSnapshots:   true,

		// Replication
		MaxAppendEntries:     100,
		ReplicationBatchSize: 64,
		ReplicationTimeout:   2 * time.Second,
		EnablePipeline:       true,

		// Performance
		MaxInflightReplications: 10,
		BatchInterval:           10 * time.Millisecond,
		MaxBatchSize:            100,

		// Log
		MaxLogEntries:        100000,
		LogCacheSize:         1024,
		LogCompactionEnabled: true,
		TrailingLogs:         10000,

		// Safety
		PreVoteEnabled: true,
		CheckQuorum:    true,

		// Observability
		EnableMetrics: true,
		EnableTracing: false,
	}
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.NodeID == "" {
		return errors.New("node ID is required")
	}

	// Validate timing parameters
	if c.ElectionTimeoutMin <= 0 {
		return errors.New("election timeout min must be positive")
	}

	if c.ElectionTimeoutMax <= c.ElectionTimeoutMin {
		return errors.New("election timeout max must be greater than min")
	}

	if c.HeartbeatInterval <= 0 {
		return errors.New("heartbeat interval must be positive")
	}

	// Heartbeat interval should be significantly less than election timeout
	if c.HeartbeatInterval >= c.ElectionTimeoutMin {
		return errors.New("heartbeat interval must be less than election timeout min")
	}

	// Recommended: heartbeat interval should be 1/10 of election timeout
	recommendedHeartbeat := c.ElectionTimeoutMin / 10
	if c.HeartbeatInterval > recommendedHeartbeat {
		// This is a warning, not an error
		fmt.Printf("WARNING: heartbeat interval (%v) should be <= %v for optimal performance\n",
			c.HeartbeatInterval, recommendedHeartbeat)
	}

	if c.ApplyInterval <= 0 {
		return errors.New("apply interval must be positive")
	}

	if c.CommitTimeout <= 0 {
		return errors.New("commit timeout must be positive")
	}

	// Validate snapshot configuration
	if c.EnableSnapshots {
		if c.SnapshotInterval <= 0 {
			return errors.New("snapshot interval must be positive")
		}

		if c.SnapshotThreshold == 0 {
			return errors.New("snapshot threshold must be positive")
		}
	}

	// Validate replication configuration
	if c.MaxAppendEntries <= 0 {
		return errors.New("max append entries must be positive")
	}

	if c.ReplicationBatchSize <= 0 {
		return errors.New("replication batch size must be positive")
	}

	if c.ReplicationBatchSize > c.MaxAppendEntries {
		return errors.New("replication batch size cannot exceed max append entries")
	}

	if c.ReplicationTimeout <= 0 {
		return errors.New("replication timeout must be positive")
	}

	// Validate performance tuning
	if c.MaxInflightReplications <= 0 {
		return errors.New("max inflight replications must be positive")
	}

	if c.BatchInterval <= 0 {
		return errors.New("batch interval must be positive")
	}

	if c.MaxBatchSize <= 0 {
		return errors.New("max batch size must be positive")
	}

	// Validate log configuration
	if c.MaxLogEntries == 0 {
		return errors.New("max log entries must be positive")
	}

	if c.LogCompactionEnabled && c.TrailingLogs == 0 {
		return errors.New("trailing logs must be positive when log compaction is enabled")
	}

	if c.TrailingLogs >= c.MaxLogEntries {
		return errors.New("trailing logs must be less than max log entries")
	}

	return nil
}

// WithNodeID sets the node ID.
func (c *Config) WithNodeID(nodeID string) *Config {
	c.NodeID = nodeID

	return c
}

// WithElectionTimeout sets the election timeout range.
func (c *Config) WithElectionTimeout(min, max time.Duration) *Config {
	c.ElectionTimeoutMin = min
	c.ElectionTimeoutMax = max

	return c
}

// WithHeartbeatInterval sets the heartbeat interval.
func (c *Config) WithHeartbeatInterval(interval time.Duration) *Config {
	c.HeartbeatInterval = interval

	return c
}

// WithSnapshotSettings sets snapshot configuration.
func (c *Config) WithSnapshotSettings(interval time.Duration, threshold uint64) *Config {
	c.SnapshotInterval = interval
	c.SnapshotThreshold = threshold
	c.EnableSnapshots = true

	return c
}

// WithReplicationSettings sets replication configuration.
func (c *Config) WithReplicationSettings(maxEntries, batchSize int, timeout time.Duration) *Config {
	c.MaxAppendEntries = maxEntries
	c.ReplicationBatchSize = batchSize
	c.ReplicationTimeout = timeout

	return c
}

// WithPerformanceSettings sets performance tuning parameters.
func (c *Config) WithPerformanceSettings(maxInflight int, batchInterval time.Duration, maxBatchSize int) *Config {
	c.MaxInflightReplications = maxInflight
	c.BatchInterval = batchInterval
	c.MaxBatchSize = maxBatchSize

	return c
}

// WithLogSettings sets log configuration.
func (c *Config) WithLogSettings(maxEntries, trailingLogs uint64, compactionEnabled bool) *Config {
	c.MaxLogEntries = maxEntries
	c.TrailingLogs = trailingLogs
	c.LogCompactionEnabled = compactionEnabled

	return c
}

// WithSafetySettings sets safety configuration.
func (c *Config) WithSafetySettings(preVote, checkQuorum bool) *Config {
	c.PreVoteEnabled = preVote
	c.CheckQuorum = checkQuorum

	return c
}

// WithObservability sets observability configuration.
func (c *Config) WithObservability(metrics, tracing bool) *Config {
	c.EnableMetrics = metrics
	c.EnableTracing = tracing

	return c
}

// ProductionConfig returns a production-ready configuration.
func ProductionConfig() Config {
	config := DefaultConfig()

	// Production timings
	config.ElectionTimeoutMin = 300 * time.Millisecond
	config.ElectionTimeoutMax = 600 * time.Millisecond
	config.HeartbeatInterval = 100 * time.Millisecond

	// Enable all safety features
	config.PreVoteEnabled = true
	config.CheckQuorum = true

	// Aggressive snapshotting
	config.EnableSnapshots = true
	config.SnapshotThreshold = 5000
	config.SnapshotInterval = 2 * time.Minute

	// Full observability
	config.EnableMetrics = true
	config.EnableTracing = true

	return config
}

// DevelopmentConfig returns a development-friendly configuration.
func DevelopmentConfig() Config {
	config := DefaultConfig()

	// Faster timings for development
	config.ElectionTimeoutMin = 100 * time.Millisecond
	config.ElectionTimeoutMax = 200 * time.Millisecond
	config.HeartbeatInterval = 30 * time.Millisecond

	// Less aggressive snapshotting
	config.SnapshotThreshold = 1000
	config.SnapshotInterval = 1 * time.Minute

	// Enable metrics but not tracing
	config.EnableMetrics = true
	config.EnableTracing = false

	return config
}

// TestConfig returns a configuration optimized for testing.
func TestConfig() Config {
	config := DefaultConfig()

	// Very fast timings for tests
	config.ElectionTimeoutMin = 50 * time.Millisecond
	config.ElectionTimeoutMax = 100 * time.Millisecond
	config.HeartbeatInterval = 10 * time.Millisecond
	config.ApplyInterval = 5 * time.Millisecond

	// Minimal snapshotting
	config.EnableSnapshots = false

	// Disable observability overhead
	config.EnableMetrics = false
	config.EnableTracing = false

	return config
}
