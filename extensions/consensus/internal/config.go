package internal

import (
	"fmt"
	"time"
)

// Config contains all consensus configuration
type Config struct {
	// Node configuration
	NodeID    string `yaml:"node_id" json:"node_id"`
	ClusterID string `yaml:"cluster_id" json:"cluster_id"`
	BindAddr  string `yaml:"bind_addr" json:"bind_addr" default:"0.0.0.0"`
	BindPort  int    `yaml:"bind_port" json:"bind_port" default:"7000"`

	// Peers - initial cluster members
	Peers []PeerConfig `yaml:"peers" json:"peers"`

	// Raft configuration
	Raft RaftConfig `yaml:"raft" json:"raft"`

	// Transport configuration
	Transport TransportConfig `yaml:"transport" json:"transport"`

	// Discovery configuration
	Discovery DiscoveryConfig `yaml:"discovery" json:"discovery"`

	// Storage configuration
	Storage StorageConfig `yaml:"storage" json:"storage"`

	// Election configuration
	Election ElectionConfig `yaml:"election" json:"election"`

	// Health check configuration
	Health HealthConfig `yaml:"health" json:"health"`

	// Observability configuration
	Observability ObservabilityConfig `yaml:"observability" json:"observability"`

	// Security configuration
	Security SecurityConfig `yaml:"security" json:"security"`

	// Resilience configuration
	Resilience ResilienceConfig `yaml:"resilience" json:"resilience"`

	// Admin API configuration
	AdminAPI AdminAPIConfig `yaml:"admin_api" json:"admin_api"`

	// Events configuration
	Events EventsConfig `yaml:"events" json:"events"`

	// Advanced settings
	Advanced AdvancedConfig `yaml:"advanced" json:"advanced"`

	// Internal flag
	RequireConfig bool `yaml:"-" json:"-"`
}

// PeerConfig represents a cluster peer
type PeerConfig struct {
	ID      string `yaml:"id" json:"id"`
	Address string `yaml:"address" json:"address"`
	Port    int    `yaml:"port" json:"port"`
}

// RaftConfig contains Raft-specific configuration
type RaftConfig struct {
	HeartbeatInterval    time.Duration `yaml:"heartbeat_interval" json:"heartbeat_interval" default:"1s"`
	ElectionTimeoutMin   time.Duration `yaml:"election_timeout_min" json:"election_timeout_min" default:"5s"`
	ElectionTimeoutMax   time.Duration `yaml:"election_timeout_max" json:"election_timeout_max" default:"10s"`
	SnapshotInterval     time.Duration `yaml:"snapshot_interval" json:"snapshot_interval" default:"30m"`
	SnapshotThreshold    uint64        `yaml:"snapshot_threshold" json:"snapshot_threshold" default:"10000"`
	LogCacheSize         int           `yaml:"log_cache_size" json:"log_cache_size" default:"1024"`
	MaxAppendEntries     int           `yaml:"max_append_entries" json:"max_append_entries" default:"64"`
	TrailingLogs         uint64        `yaml:"trailing_logs" json:"trailing_logs" default:"10000"`
	ReplicationBatchSize int           `yaml:"replication_batch_size" json:"replication_batch_size" default:"100"`
	LeaderLeaseTimeout   time.Duration `yaml:"leader_lease_timeout" json:"leader_lease_timeout" default:"500ms"`
	PreVote              bool          `yaml:"pre_vote" json:"pre_vote" default:"true"`
	CheckQuorum          bool          `yaml:"check_quorum" json:"check_quorum" default:"true"`
	DisablePipeline      bool          `yaml:"disable_pipeline" json:"disable_pipeline" default:"false"`
}

// TransportConfig contains transport layer configuration
type TransportConfig struct {
	Type               string        `yaml:"type" json:"type" default:"grpc"`                            // grpc, tcp
	MaxMessageSize     int           `yaml:"max_message_size" json:"max_message_size" default:"4194304"` // 4MB
	Timeout            time.Duration `yaml:"timeout" json:"timeout" default:"10s"`
	KeepAlive          bool          `yaml:"keep_alive" json:"keep_alive" default:"true"`
	KeepAliveInterval  time.Duration `yaml:"keep_alive_interval" json:"keep_alive_interval" default:"30s"`
	KeepAliveTimeout   time.Duration `yaml:"keep_alive_timeout" json:"keep_alive_timeout" default:"10s"`
	MaxConnections     int           `yaml:"max_connections" json:"max_connections" default:"100"`
	ConnectionTimeout  time.Duration `yaml:"connection_timeout" json:"connection_timeout" default:"5s"`
	IdleTimeout        time.Duration `yaml:"idle_timeout" json:"idle_timeout" default:"5m"`
	EnableCompression  bool          `yaml:"enable_compression" json:"enable_compression" default:"true"`
	CompressionLevel   int           `yaml:"compression_level" json:"compression_level" default:"6"`
	EnableMultiplexing bool          `yaml:"enable_multiplexing" json:"enable_multiplexing" default:"true"`
}

// DiscoveryConfig contains service discovery configuration
type DiscoveryConfig struct {
	Type            string        `yaml:"type" json:"type" default:"static"` // static, dns, consul, etcd, kubernetes
	Endpoints       []string      `yaml:"endpoints" json:"endpoints"`
	Namespace       string        `yaml:"namespace" json:"namespace" default:"default"`
	ServiceName     string        `yaml:"service_name" json:"service_name" default:"forge-consensus"`
	RefreshInterval time.Duration `yaml:"refresh_interval" json:"refresh_interval" default:"30s"`
	Timeout         time.Duration `yaml:"timeout" json:"timeout" default:"10s"`
	TTL             time.Duration `yaml:"ttl" json:"ttl" default:"60s"`
	EnableWatch     bool          `yaml:"enable_watch" json:"enable_watch" default:"true"`
}

// StorageConfig contains storage backend configuration
type StorageConfig struct {
	Type           string        `yaml:"type" json:"type" default:"badger"` // badger, boltdb, pebble, postgres
	Path           string        `yaml:"path" json:"path" default:"./data/consensus"`
	SyncWrites     bool          `yaml:"sync_writes" json:"sync_writes" default:"true"`
	MaxBatchSize   int           `yaml:"max_batch_size" json:"max_batch_size" default:"1000"`
	MaxBatchDelay  time.Duration `yaml:"max_batch_delay" json:"max_batch_delay" default:"10ms"`
	CompactOnStart bool          `yaml:"compact_on_start" json:"compact_on_start" default:"false"`
	// BadgerDB specific
	BadgerOptions BadgerOptions `yaml:"badger_options" json:"badger_options"`
	// BoltDB specific
	BoltOptions BoltOptions `yaml:"bolt_options" json:"bolt_options"`
}

// BadgerOptions contains BadgerDB-specific options
type BadgerOptions struct {
	ValueLogMaxEntries uint32 `yaml:"value_log_max_entries" json:"value_log_max_entries" default:"1000000"`
	MemTableSize       int64  `yaml:"mem_table_size" json:"mem_table_size" default:"67108864"` // 64MB
	NumMemTables       int    `yaml:"num_mem_tables" json:"num_mem_tables" default:"5"`
	NumLevelZeroTables int    `yaml:"num_level_zero_tables" json:"num_level_zero_tables" default:"5"`
	NumCompactors      int    `yaml:"num_compactors" json:"num_compactors" default:"4"`
}

// BoltOptions contains BoltDB-specific options
type BoltOptions struct {
	NoSync          bool          `yaml:"no_sync" json:"no_sync" default:"false"`
	NoGrowSync      bool          `yaml:"no_grow_sync" json:"no_grow_sync" default:"false"`
	InitialMmapSize int           `yaml:"initial_mmap_size" json:"initial_mmap_size" default:"0"`
	Timeout         time.Duration `yaml:"timeout" json:"timeout" default:"1s"`
}

// ElectionConfig contains leader election configuration
type ElectionConfig struct {
	Enabled           bool          `yaml:"enabled" json:"enabled" default:"true"`
	RandomizedTimeout bool          `yaml:"randomized_timeout" json:"randomized_timeout" default:"true"`
	PreVote           bool          `yaml:"pre_vote" json:"pre_vote" default:"true"`
	PriorityElection  bool          `yaml:"priority_election" json:"priority_election" default:"false"`
	Priority          int           `yaml:"priority" json:"priority" default:"0"`
	StepDownOnRemove  bool          `yaml:"step_down_on_remove" json:"step_down_on_remove" default:"true"`
	LeaderStickiness  time.Duration `yaml:"leader_stickiness" json:"leader_stickiness" default:"10s"`
}

// HealthConfig contains health check configuration
type HealthConfig struct {
	Enabled            bool          `yaml:"enabled" json:"enabled" default:"true"`
	CheckInterval      time.Duration `yaml:"check_interval" json:"check_interval" default:"10s"`
	Timeout            time.Duration `yaml:"timeout" json:"timeout" default:"5s"`
	UnhealthyThreshold int           `yaml:"unhealthy_threshold" json:"unhealthy_threshold" default:"3"`
	HealthyThreshold   int           `yaml:"healthy_threshold" json:"healthy_threshold" default:"2"`
}

// ObservabilityConfig contains observability configuration
type ObservabilityConfig struct {
	Metrics MetricsConfig `yaml:"metrics" json:"metrics"`
	Tracing TracingConfig `yaml:"tracing" json:"tracing"`
	Logging LoggingConfig `yaml:"logging" json:"logging"`
}

// MetricsConfig contains metrics configuration
type MetricsConfig struct {
	Enabled               bool          `yaml:"enabled" json:"enabled" default:"true"`
	CollectionInterval    time.Duration `yaml:"collection_interval" json:"collection_interval" default:"15s"`
	Namespace             string        `yaml:"namespace" json:"namespace" default:"forge_consensus"`
	EnableDetailedMetrics bool          `yaml:"enable_detailed_metrics" json:"enable_detailed_metrics" default:"true"`
}

// TracingConfig contains tracing configuration
type TracingConfig struct {
	Enabled     bool    `yaml:"enabled" json:"enabled" default:"false"`
	ServiceName string  `yaml:"service_name" json:"service_name" default:"forge-consensus"`
	SampleRate  float64 `yaml:"sample_rate" json:"sample_rate" default:"0.1"`
}

// LoggingConfig contains logging configuration
type LoggingConfig struct {
	Level            string `yaml:"level" json:"level" default:"info"`
	EnableStructured bool   `yaml:"enable_structured" json:"enable_structured" default:"true"`
	LogRaftDetails   bool   `yaml:"log_raft_details" json:"log_raft_details" default:"false"`
}

// SecurityConfig contains security configuration
type SecurityConfig struct {
	EnableTLS        bool   `yaml:"enable_tls" json:"enable_tls" default:"false"`
	EnableMTLS       bool   `yaml:"enable_mtls" json:"enable_mtls" default:"false"`
	CertFile         string `yaml:"cert_file" json:"cert_file"`
	KeyFile          string `yaml:"key_file" json:"key_file"`
	CAFile           string `yaml:"ca_file" json:"ca_file"`
	SkipVerify       bool   `yaml:"skip_verify" json:"skip_verify" default:"false"`
	EnableEncryption bool   `yaml:"enable_encryption" json:"enable_encryption" default:"false"`
	EncryptionKey    string `yaml:"encryption_key" json:"encryption_key"`
}

// ResilienceConfig contains resilience configuration
type ResilienceConfig struct {
	EnableRetry             bool          `yaml:"enable_retry" json:"enable_retry" default:"true"`
	MaxRetries              int           `yaml:"max_retries" json:"max_retries" default:"3"`
	RetryDelay              time.Duration `yaml:"retry_delay" json:"retry_delay" default:"100ms"`
	RetryBackoffFactor      float64       `yaml:"retry_backoff_factor" json:"retry_backoff_factor" default:"2.0"`
	MaxRetryDelay           time.Duration `yaml:"max_retry_delay" json:"max_retry_delay" default:"5s"`
	EnableCircuitBreaker    bool          `yaml:"enable_circuit_breaker" json:"enable_circuit_breaker" default:"true"`
	CircuitBreakerThreshold int           `yaml:"circuit_breaker_threshold" json:"circuit_breaker_threshold" default:"5"`
	CircuitBreakerTimeout   time.Duration `yaml:"circuit_breaker_timeout" json:"circuit_breaker_timeout" default:"30s"`
}

// AdminAPIConfig contains admin API configuration
type AdminAPIConfig struct {
	Enabled    bool   `yaml:"enabled" json:"enabled" default:"true"`
	PathPrefix string `yaml:"path_prefix" json:"path_prefix" default:"/consensus"`
	EnableAuth bool   `yaml:"enable_auth" json:"enable_auth" default:"false"`
	APIKey     string `yaml:"api_key" json:"api_key"`
}

// EventsConfig contains events configuration
type EventsConfig struct {
	Enabled           bool `yaml:"enabled" json:"enabled" default:"true"`
	EmitLeaderChange  bool `yaml:"emit_leader_change" json:"emit_leader_change" default:"true"`
	EmitNodeEvents    bool `yaml:"emit_node_events" json:"emit_node_events" default:"true"`
	EmitClusterEvents bool `yaml:"emit_cluster_events" json:"emit_cluster_events" default:"true"`
}

// AdvancedConfig contains advanced settings
type AdvancedConfig struct {
	EnableAutoSnapshot   bool          `yaml:"enable_auto_snapshot" json:"enable_auto_snapshot" default:"true"`
	EnableAutoCompaction bool          `yaml:"enable_auto_compaction" json:"enable_auto_compaction" default:"true"`
	CompactionInterval   time.Duration `yaml:"compaction_interval" json:"compaction_interval" default:"1h"`
	MaxMemoryUsage       int64         `yaml:"max_memory_usage" json:"max_memory_usage" default:"1073741824"` // 1GB
	GCInterval           time.Duration `yaml:"gc_interval" json:"gc_interval" default:"5m"`
	EnableReadIndex      bool          `yaml:"enable_read_index" json:"enable_read_index" default:"true"`
	EnableLeasedReads    bool          `yaml:"enable_leased_reads" json:"enable_leased_reads" default:"true"`
}

// DefaultConfig returns default configuration
func DefaultConfig() Config {
	return Config{
		NodeID:    "",
		ClusterID: "default",
		BindAddr:  "0.0.0.0",
		BindPort:  7000,
		Peers:     []PeerConfig{},
		Raft: RaftConfig{
			HeartbeatInterval:    1 * time.Second,
			ElectionTimeoutMin:   5 * time.Second,
			ElectionTimeoutMax:   10 * time.Second,
			SnapshotInterval:     30 * time.Minute,
			SnapshotThreshold:    10000,
			LogCacheSize:         1024,
			MaxAppendEntries:     64,
			TrailingLogs:         10000,
			ReplicationBatchSize: 100,
			LeaderLeaseTimeout:   500 * time.Millisecond,
			PreVote:              true,
			CheckQuorum:          true,
			DisablePipeline:      false,
		},
		Transport: TransportConfig{
			Type:               "grpc",
			MaxMessageSize:     4 * 1024 * 1024, // 4MB
			Timeout:            10 * time.Second,
			KeepAlive:          true,
			KeepAliveInterval:  30 * time.Second,
			KeepAliveTimeout:   10 * time.Second,
			MaxConnections:     100,
			ConnectionTimeout:  5 * time.Second,
			IdleTimeout:        5 * time.Minute,
			EnableCompression:  true,
			CompressionLevel:   6,
			EnableMultiplexing: true,
		},
		Discovery: DiscoveryConfig{
			Type:            "static",
			Endpoints:       []string{},
			Namespace:       "default",
			ServiceName:     "forge-consensus",
			RefreshInterval: 30 * time.Second,
			Timeout:         10 * time.Second,
			TTL:             60 * time.Second,
			EnableWatch:     true,
		},
		Storage: StorageConfig{
			Type:           "badger",
			Path:           "./data/consensus",
			SyncWrites:     true,
			MaxBatchSize:   1000,
			MaxBatchDelay:  10 * time.Millisecond,
			CompactOnStart: false,
			BadgerOptions: BadgerOptions{
				ValueLogMaxEntries: 1000000,
				MemTableSize:       64 * 1024 * 1024, // 64MB
				NumMemTables:       5,
				NumLevelZeroTables: 5,
				NumCompactors:      4,
			},
			BoltOptions: BoltOptions{
				NoSync:          false,
				NoGrowSync:      false,
				InitialMmapSize: 0,
				Timeout:         1 * time.Second,
			},
		},
		Election: ElectionConfig{
			Enabled:           true,
			RandomizedTimeout: true,
			PreVote:           true,
			PriorityElection:  false,
			Priority:          0,
			StepDownOnRemove:  true,
			LeaderStickiness:  10 * time.Second,
		},
		Health: HealthConfig{
			Enabled:            true,
			CheckInterval:      10 * time.Second,
			Timeout:            5 * time.Second,
			UnhealthyThreshold: 3,
			HealthyThreshold:   2,
		},
		Observability: ObservabilityConfig{
			Metrics: MetricsConfig{
				Enabled:               true,
				CollectionInterval:    15 * time.Second,
				Namespace:             "forge_consensus",
				EnableDetailedMetrics: true,
			},
			Tracing: TracingConfig{
				Enabled:     false,
				ServiceName: "forge-consensus",
				SampleRate:  0.1,
			},
			Logging: LoggingConfig{
				Level:            "info",
				EnableStructured: true,
				LogRaftDetails:   false,
			},
		},
		Security: SecurityConfig{
			EnableTLS:        false,
			EnableMTLS:       false,
			SkipVerify:       false,
			EnableEncryption: false,
		},
		Resilience: ResilienceConfig{
			EnableRetry:             true,
			MaxRetries:              3,
			RetryDelay:              100 * time.Millisecond,
			RetryBackoffFactor:      2.0,
			MaxRetryDelay:           5 * time.Second,
			EnableCircuitBreaker:    true,
			CircuitBreakerThreshold: 5,
			CircuitBreakerTimeout:   30 * time.Second,
		},
		AdminAPI: AdminAPIConfig{
			Enabled:    true,
			PathPrefix: "/consensus",
			EnableAuth: false,
		},
		Events: EventsConfig{
			Enabled:           true,
			EmitLeaderChange:  true,
			EmitNodeEvents:    true,
			EmitClusterEvents: true,
		},
		Advanced: AdvancedConfig{
			EnableAutoSnapshot:   true,
			EnableAutoCompaction: true,
			CompactionInterval:   1 * time.Hour,
			MaxMemoryUsage:       1024 * 1024 * 1024, // 1GB
			GCInterval:           5 * time.Minute,
			EnableReadIndex:      true,
			EnableLeasedReads:    true,
		},
		RequireConfig: false,
	}
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.NodeID == "" {
		return fmt.Errorf("%w: node_id is required", ErrInvalidConfig)
	}

	if c.ClusterID == "" {
		return fmt.Errorf("%w: cluster_id is required", ErrInvalidConfig)
	}

	if c.BindPort < 1 || c.BindPort > 65535 {
		return fmt.Errorf("%w: invalid bind_port: %d", ErrInvalidConfig, c.BindPort)
	}

	// Validate Raft config
	if c.Raft.HeartbeatInterval <= 0 {
		return fmt.Errorf("%w: raft.heartbeat_interval must be positive", ErrInvalidConfig)
	}

	if c.Raft.ElectionTimeoutMin <= c.Raft.HeartbeatInterval {
		return fmt.Errorf("%w: raft.election_timeout_min must be greater than heartbeat_interval", ErrInvalidConfig)
	}

	if c.Raft.ElectionTimeoutMax <= c.Raft.ElectionTimeoutMin {
		return fmt.Errorf("%w: raft.election_timeout_max must be greater than election_timeout_min", ErrInvalidConfig)
	}

	// Validate transport config
	if c.Transport.Type != "grpc" && c.Transport.Type != "tcp" {
		return fmt.Errorf("%w: invalid transport type: %s", ErrInvalidConfig, c.Transport.Type)
	}

	if c.Transport.MaxMessageSize <= 0 {
		return fmt.Errorf("%w: transport.max_message_size must be positive", ErrInvalidConfig)
	}

	// Validate discovery config
	validDiscoveryTypes := map[string]bool{
		"static": true, "dns": true, "consul": true, "etcd": true, "kubernetes": true,
	}
	if !validDiscoveryTypes[c.Discovery.Type] {
		return fmt.Errorf("%w: invalid discovery type: %s", ErrInvalidConfig, c.Discovery.Type)
	}

	// Validate storage config
	validStorageTypes := map[string]bool{
		"badger": true, "boltdb": true, "pebble": true, "postgres": true,
	}
	if !validStorageTypes[c.Storage.Type] {
		return fmt.Errorf("%w: invalid storage type: %s", ErrInvalidConfig, c.Storage.Type)
	}

	if c.Storage.Path == "" {
		return fmt.Errorf("%w: storage.path is required", ErrInvalidConfig)
	}

	// Validate security config
	if c.Security.EnableTLS {
		if c.Security.CertFile == "" || c.Security.KeyFile == "" {
			return fmt.Errorf("%w: security.cert_file and security.key_file are required when TLS is enabled", ErrInvalidConfig)
		}
	}

	if c.Security.EnableMTLS && !c.Security.EnableTLS {
		return fmt.Errorf("%w: security.enable_tls must be true when enable_mtls is true", ErrInvalidConfig)
	}

	if c.Security.EnableMTLS && c.Security.CAFile == "" {
		return fmt.Errorf("%w: security.ca_file is required when mTLS is enabled", ErrInvalidConfig)
	}

	return nil
}

// ConfigOption is a functional option for Config
type ConfigOption func(*Config)

// WithNodeID sets the node ID
func WithNodeID(id string) ConfigOption {
	return func(c *Config) {
		c.NodeID = id
	}
}

// WithClusterID sets the cluster ID
func WithClusterID(id string) ConfigOption {
	return func(c *Config) {
		c.ClusterID = id
	}
}

// WithBindAddress sets the bind address and port
func WithBindAddress(addr string, port int) ConfigOption {
	return func(c *Config) {
		c.BindAddr = addr
		c.BindPort = port
	}
}

// WithPeers sets the initial peer list
func WithPeers(peers []PeerConfig) ConfigOption {
	return func(c *Config) {
		c.Peers = peers
	}
}

// WithTransportType sets the transport type
func WithTransportType(transportType string) ConfigOption {
	return func(c *Config) {
		c.Transport.Type = transportType
	}
}

// WithDiscoveryType sets the discovery type
func WithDiscoveryType(discoveryType string) ConfigOption {
	return func(c *Config) {
		c.Discovery.Type = discoveryType
	}
}

// WithStorageType sets the storage type
func WithStorageType(storageType string) ConfigOption {
	return func(c *Config) {
		c.Storage.Type = storageType
	}
}

// WithStoragePath sets the storage path
func WithStoragePath(path string) ConfigOption {
	return func(c *Config) {
		c.Storage.Path = path
	}
}

// WithTLS enables TLS with the given cert and key files
func WithTLS(certFile, keyFile string) ConfigOption {
	return func(c *Config) {
		c.Security.EnableTLS = true
		c.Security.CertFile = certFile
		c.Security.KeyFile = keyFile
	}
}

// WithMTLS enables mutual TLS with the given CA file
func WithMTLS(caFile string) ConfigOption {
	return func(c *Config) {
		c.Security.EnableMTLS = true
		c.Security.CAFile = caFile
	}
}

// WithConfig sets the entire config (for backward compatibility)
func WithConfig(cfg Config) ConfigOption {
	return func(c *Config) {
		*c = cfg
	}
}

// WithRequireConfig sets whether config is required from ConfigManager
func WithRequireConfig(require bool) ConfigOption {
	return func(c *Config) {
		c.RequireConfig = require
	}
}
