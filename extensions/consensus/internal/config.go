package internal

import (
	"fmt"
	"time"
)

// Config contains all consensus configuration.
type Config struct {
	// Node configuration
	NodeID    string `json:"node_id"    yaml:"node_id"`
	ClusterID string `json:"cluster_id" yaml:"cluster_id"`
	BindAddr  string `default:"0.0.0.0" json:"bind_addr"  yaml:"bind_addr"`
	BindPort  int    `default:"7000"    json:"bind_port"  yaml:"bind_port"`

	// Peers - initial cluster members
	Peers []PeerConfig `json:"peers" yaml:"peers"`

	// Raft configuration
	Raft RaftConfig `json:"raft" yaml:"raft"`

	// Transport configuration
	Transport TransportConfig `json:"transport" yaml:"transport"`

	// Discovery configuration
	Discovery DiscoveryConfig `json:"discovery" yaml:"discovery"`

	// Storage configuration
	Storage StorageConfig `json:"storage" yaml:"storage"`

	// Election configuration
	Election ElectionConfig `json:"election" yaml:"election"`

	// Health check configuration
	Health HealthConfig `json:"health" yaml:"health"`

	// Observability configuration
	Observability ObservabilityConfig `json:"observability" yaml:"observability"`

	// Security configuration
	Security SecurityConfig `json:"security" yaml:"security"`

	// Resilience configuration
	Resilience ResilienceConfig `json:"resilience" yaml:"resilience"`

	// Admin API configuration
	AdminAPI AdminAPIConfig `json:"admin_api" yaml:"admin_api"`

	// Events configuration
	Events EventsConfig `json:"events" yaml:"events"`

	// Advanced settings
	Advanced AdvancedConfig `json:"advanced" yaml:"advanced"`

	// Internal flag
	RequireConfig bool `json:"-" yaml:"-"`
}

// PeerConfig represents a cluster peer.
type PeerConfig struct {
	ID      string `json:"id"      yaml:"id"`
	Address string `json:"address" yaml:"address"`
	Port    int    `json:"port"    yaml:"port"`
}

// RaftConfig contains Raft-specific configuration.
type RaftConfig struct {
	HeartbeatInterval    time.Duration `default:"1s"    json:"heartbeat_interval"     yaml:"heartbeat_interval"`
	ElectionTimeoutMin   time.Duration `default:"5s"    json:"election_timeout_min"   yaml:"election_timeout_min"`
	ElectionTimeoutMax   time.Duration `default:"10s"   json:"election_timeout_max"   yaml:"election_timeout_max"`
	SnapshotInterval     time.Duration `default:"30m"   json:"snapshot_interval"      yaml:"snapshot_interval"`
	SnapshotThreshold    uint64        `default:"10000" json:"snapshot_threshold"     yaml:"snapshot_threshold"`
	LogCacheSize         int           `default:"1024"  json:"log_cache_size"         yaml:"log_cache_size"`
	MaxAppendEntries     int           `default:"64"    json:"max_append_entries"     yaml:"max_append_entries"`
	TrailingLogs         uint64        `default:"10000" json:"trailing_logs"          yaml:"trailing_logs"`
	ReplicationBatchSize int           `default:"100"   json:"replication_batch_size" yaml:"replication_batch_size"`
	LeaderLeaseTimeout   time.Duration `default:"500ms" json:"leader_lease_timeout"   yaml:"leader_lease_timeout"`
	PreVote              bool          `default:"true"  json:"pre_vote"               yaml:"pre_vote"`
	CheckQuorum          bool          `default:"true"  json:"check_quorum"           yaml:"check_quorum"`
	DisablePipeline      bool          `default:"false" json:"disable_pipeline"       yaml:"disable_pipeline"`
}

// TransportConfig contains transport layer configuration.
type TransportConfig struct {
	Type               string        `default:"grpc"    json:"type"                yaml:"type"`             // grpc, tcp
	MaxMessageSize     int           `default:"4194304" json:"max_message_size"    yaml:"max_message_size"` // 4MB
	Timeout            time.Duration `default:"10s"     json:"timeout"             yaml:"timeout"`
	KeepAlive          bool          `default:"true"    json:"keep_alive"          yaml:"keep_alive"`
	KeepAliveInterval  time.Duration `default:"30s"     json:"keep_alive_interval" yaml:"keep_alive_interval"`
	KeepAliveTimeout   time.Duration `default:"10s"     json:"keep_alive_timeout"  yaml:"keep_alive_timeout"`
	MaxConnections     int           `default:"100"     json:"max_connections"     yaml:"max_connections"`
	ConnectionTimeout  time.Duration `default:"5s"      json:"connection_timeout"  yaml:"connection_timeout"`
	IdleTimeout        time.Duration `default:"5m"      json:"idle_timeout"        yaml:"idle_timeout"`
	EnableCompression  bool          `default:"true"    json:"enable_compression"  yaml:"enable_compression"`
	CompressionLevel   int           `default:"6"       json:"compression_level"   yaml:"compression_level"`
	EnableMultiplexing bool          `default:"true"    json:"enable_multiplexing" yaml:"enable_multiplexing"`
}

// DiscoveryConfig contains service discovery configuration.
type DiscoveryConfig struct {
	Type            string        `default:"static"          json:"type"             yaml:"type"` // static, dns, consul, etcd, kubernetes
	Endpoints       []string      `json:"endpoints"          yaml:"endpoints"`
	Namespace       string        `default:"default"         json:"namespace"        yaml:"namespace"`
	ServiceName     string        `default:"forge-consensus" json:"service_name"     yaml:"service_name"`
	RefreshInterval time.Duration `default:"30s"             json:"refresh_interval" yaml:"refresh_interval"`
	Timeout         time.Duration `default:"10s"             json:"timeout"          yaml:"timeout"`
	TTL             time.Duration `default:"60s"             json:"ttl"              yaml:"ttl"`
	EnableWatch     bool          `default:"true"            json:"enable_watch"     yaml:"enable_watch"`
}

// StorageConfig contains storage backend configuration.
type StorageConfig struct {
	Type           string        `default:"badger"           json:"type"             yaml:"type"` // badger, boltdb, pebble, postgres
	Path           string        `default:"./data/consensus" json:"path"             yaml:"path"`
	SyncWrites     bool          `default:"true"             json:"sync_writes"      yaml:"sync_writes"`
	MaxBatchSize   int           `default:"1000"             json:"max_batch_size"   yaml:"max_batch_size"`
	MaxBatchDelay  time.Duration `default:"10ms"             json:"max_batch_delay"  yaml:"max_batch_delay"`
	CompactOnStart bool          `default:"false"            json:"compact_on_start" yaml:"compact_on_start"`
	// BadgerDB specific
	BadgerOptions BadgerOptions `json:"badger_options" yaml:"badger_options"`
	// BoltDB specific
	BoltOptions BoltOptions `json:"bolt_options" yaml:"bolt_options"`
}

// BadgerOptions contains BadgerDB-specific options.
type BadgerOptions struct {
	ValueLogMaxEntries uint32 `default:"1000000"  json:"value_log_max_entries" yaml:"value_log_max_entries"`
	MemTableSize       int64  `default:"67108864" json:"mem_table_size"        yaml:"mem_table_size"` // 64MB
	NumMemTables       int    `default:"5"        json:"num_mem_tables"        yaml:"num_mem_tables"`
	NumLevelZeroTables int    `default:"5"        json:"num_level_zero_tables" yaml:"num_level_zero_tables"`
	NumCompactors      int    `default:"4"        json:"num_compactors"        yaml:"num_compactors"`
}

// BoltOptions contains BoltDB-specific options.
type BoltOptions struct {
	NoSync          bool          `default:"false" json:"no_sync"           yaml:"no_sync"`
	NoGrowSync      bool          `default:"false" json:"no_grow_sync"      yaml:"no_grow_sync"`
	InitialMmapSize int           `default:"0"     json:"initial_mmap_size" yaml:"initial_mmap_size"`
	Timeout         time.Duration `default:"1s"    json:"timeout"           yaml:"timeout"`
}

// ElectionConfig contains leader election configuration.
type ElectionConfig struct {
	Enabled           bool          `default:"true"  json:"enabled"             yaml:"enabled"`
	RandomizedTimeout bool          `default:"true"  json:"randomized_timeout"  yaml:"randomized_timeout"`
	PreVote           bool          `default:"true"  json:"pre_vote"            yaml:"pre_vote"`
	PriorityElection  bool          `default:"false" json:"priority_election"   yaml:"priority_election"`
	Priority          int           `default:"0"     json:"priority"            yaml:"priority"`
	StepDownOnRemove  bool          `default:"true"  json:"step_down_on_remove" yaml:"step_down_on_remove"`
	LeaderStickiness  time.Duration `default:"10s"   json:"leader_stickiness"   yaml:"leader_stickiness"`
}

// HealthConfig contains health check configuration.
type HealthConfig struct {
	Enabled            bool          `default:"true" json:"enabled"             yaml:"enabled"`
	CheckInterval      time.Duration `default:"10s"  json:"check_interval"      yaml:"check_interval"`
	Timeout            time.Duration `default:"5s"   json:"timeout"             yaml:"timeout"`
	UnhealthyThreshold int           `default:"3"    json:"unhealthy_threshold" yaml:"unhealthy_threshold"`
	HealthyThreshold   int           `default:"2"    json:"healthy_threshold"   yaml:"healthy_threshold"`
}

// ObservabilityConfig contains observability configuration.
type ObservabilityConfig struct {
	Metrics MetricsConfig `json:"metrics" yaml:"metrics"`
	Tracing TracingConfig `json:"tracing" yaml:"tracing"`
	Logging LoggingConfig `json:"logging" yaml:"logging"`
}

// MetricsConfig contains metrics configuration.
type MetricsConfig struct {
	Enabled               bool          `default:"true"            json:"enabled"                 yaml:"enabled"`
	CollectionInterval    time.Duration `default:"15s"             json:"collection_interval"     yaml:"collection_interval"`
	Namespace             string        `default:"forge_consensus" json:"namespace"               yaml:"namespace"`
	EnableDetailedMetrics bool          `default:"true"            json:"enable_detailed_metrics" yaml:"enable_detailed_metrics"`
}

// TracingConfig contains tracing configuration.
type TracingConfig struct {
	Enabled     bool    `default:"false"           json:"enabled"      yaml:"enabled"`
	ServiceName string  `default:"forge-consensus" json:"service_name" yaml:"service_name"`
	SampleRate  float64 `default:"0.1"             json:"sample_rate"  yaml:"sample_rate"`
}

// LoggingConfig contains logging configuration.
type LoggingConfig struct {
	Level            string `default:"info"  json:"level"             yaml:"level"`
	EnableStructured bool   `default:"true"  json:"enable_structured" yaml:"enable_structured"`
	LogRaftDetails   bool   `default:"false" json:"log_raft_details"  yaml:"log_raft_details"`
}

// SecurityConfig contains security configuration.
type SecurityConfig struct {
	EnableTLS        bool   `default:"false"       json:"enable_tls"        yaml:"enable_tls"`
	EnableMTLS       bool   `default:"false"       json:"enable_mtls"       yaml:"enable_mtls"`
	CertFile         string `json:"cert_file"      yaml:"cert_file"`
	KeyFile          string `json:"key_file"       yaml:"key_file"`
	CAFile           string `json:"ca_file"        yaml:"ca_file"`
	SkipVerify       bool   `default:"false"       json:"skip_verify"       yaml:"skip_verify"`
	EnableEncryption bool   `default:"false"       json:"enable_encryption" yaml:"enable_encryption"`
	EncryptionKey    string `json:"encryption_key" yaml:"encryption_key"`
}

// ResilienceConfig contains resilience configuration.
type ResilienceConfig struct {
	EnableRetry             bool          `default:"true"  json:"enable_retry"              yaml:"enable_retry"`
	MaxRetries              int           `default:"3"     json:"max_retries"               yaml:"max_retries"`
	RetryDelay              time.Duration `default:"100ms" json:"retry_delay"               yaml:"retry_delay"`
	RetryBackoffFactor      float64       `default:"2.0"   json:"retry_backoff_factor"      yaml:"retry_backoff_factor"`
	MaxRetryDelay           time.Duration `default:"5s"    json:"max_retry_delay"           yaml:"max_retry_delay"`
	EnableCircuitBreaker    bool          `default:"true"  json:"enable_circuit_breaker"    yaml:"enable_circuit_breaker"`
	CircuitBreakerThreshold int           `default:"5"     json:"circuit_breaker_threshold" yaml:"circuit_breaker_threshold"`
	CircuitBreakerTimeout   time.Duration `default:"30s"   json:"circuit_breaker_timeout"   yaml:"circuit_breaker_timeout"`
}

// AdminAPIConfig contains admin API configuration.
type AdminAPIConfig struct {
	Enabled    bool   `default:"true"       json:"enabled"     yaml:"enabled"`
	PathPrefix string `default:"/consensus" json:"path_prefix" yaml:"path_prefix"`
	EnableAuth bool   `default:"false"      json:"enable_auth" yaml:"enable_auth"`
	APIKey     string `json:"api_key"       yaml:"api_key"`
}

// EventsConfig contains events configuration.
type EventsConfig struct {
	Enabled           bool `default:"true" json:"enabled"             yaml:"enabled"`
	EmitLeaderChange  bool `default:"true" json:"emit_leader_change"  yaml:"emit_leader_change"`
	EmitNodeEvents    bool `default:"true" json:"emit_node_events"    yaml:"emit_node_events"`
	EmitClusterEvents bool `default:"true" json:"emit_cluster_events" yaml:"emit_cluster_events"`
}

// AdvancedConfig contains advanced settings.
type AdvancedConfig struct {
	EnableAutoSnapshot   bool          `default:"true"       json:"enable_auto_snapshot"   yaml:"enable_auto_snapshot"`
	EnableAutoCompaction bool          `default:"true"       json:"enable_auto_compaction" yaml:"enable_auto_compaction"`
	CompactionInterval   time.Duration `default:"1h"         json:"compaction_interval"    yaml:"compaction_interval"`
	MaxMemoryUsage       int64         `default:"1073741824" json:"max_memory_usage"       yaml:"max_memory_usage"` // 1GB
	GCInterval           time.Duration `default:"5m"         json:"gc_interval"            yaml:"gc_interval"`
	EnableReadIndex      bool          `default:"true"       json:"enable_read_index"      yaml:"enable_read_index"`
	EnableLeasedReads    bool          `default:"true"       json:"enable_leased_reads"    yaml:"enable_leased_reads"`
}

// DefaultConfig returns default configuration.
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

// Validate validates the configuration.
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

// ConfigOption is a functional option for Config.
type ConfigOption func(*Config)

// WithNodeID sets the node ID.
func WithNodeID(id string) ConfigOption {
	return func(c *Config) {
		c.NodeID = id
	}
}

// WithClusterID sets the cluster ID.
func WithClusterID(id string) ConfigOption {
	return func(c *Config) {
		c.ClusterID = id
	}
}

// WithBindAddress sets the bind address and port.
func WithBindAddress(addr string, port int) ConfigOption {
	return func(c *Config) {
		c.BindAddr = addr
		c.BindPort = port
	}
}

// WithPeers sets the initial peer list.
func WithPeers(peers []PeerConfig) ConfigOption {
	return func(c *Config) {
		c.Peers = peers
	}
}

// WithTransportType sets the transport type.
func WithTransportType(transportType string) ConfigOption {
	return func(c *Config) {
		c.Transport.Type = transportType
	}
}

// WithDiscoveryType sets the discovery type.
func WithDiscoveryType(discoveryType string) ConfigOption {
	return func(c *Config) {
		c.Discovery.Type = discoveryType
	}
}

// WithStorageType sets the storage type.
func WithStorageType(storageType string) ConfigOption {
	return func(c *Config) {
		c.Storage.Type = storageType
	}
}

// WithStoragePath sets the storage path.
func WithStoragePath(path string) ConfigOption {
	return func(c *Config) {
		c.Storage.Path = path
	}
}

// WithTLS enables TLS with the given cert and key files.
func WithTLS(certFile, keyFile string) ConfigOption {
	return func(c *Config) {
		c.Security.EnableTLS = true
		c.Security.CertFile = certFile
		c.Security.KeyFile = keyFile
	}
}

// WithMTLS enables mutual TLS with the given CA file.
func WithMTLS(caFile string) ConfigOption {
	return func(c *Config) {
		c.Security.EnableMTLS = true
		c.Security.CAFile = caFile
	}
}

// WithConfig sets the entire config (for backward compatibility).
func WithConfig(cfg Config) ConfigOption {
	return func(c *Config) {
		*c = cfg
	}
}

// WithRequireConfig sets whether config is required from ConfigManager.
func WithRequireConfig(require bool) ConfigOption {
	return func(c *Config) {
		c.RequireConfig = require
	}
}
