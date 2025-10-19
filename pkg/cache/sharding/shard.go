package sharding

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// ShardManager manages different sharding strategies for cache distribution
type ShardManager struct {
	strategy      ShardingStrategy
	consistent    *ConsistentHash
	hash          *HashSharding
	rangeSharding *RangeSharding
	config        ShardingConfig
	logger        common.Logger
	metrics       common.Metrics
	mu            sync.RWMutex
	started       bool
	stats         ShardManagerStats
}

// ShardingStrategy defines the sharding strategy
type ShardingStrategy string

const (
	ShardingStrategyConsistent ShardingStrategy = "consistent"
	ShardingStrategyHash       ShardingStrategy = "hash"
	ShardingStrategyRange      ShardingStrategy = "range"
)

// ShardingConfig contains configuration for the shard manager
type ShardingConfig struct {
	Strategy          ShardingStrategy       `yaml:"strategy" json:"strategy"`
	ReplicationFactor int                    `yaml:"replication_factor" json:"replication_factor"`
	AutoRebalance     bool                   `yaml:"auto_rebalance" json:"auto_rebalance"`
	RebalanceInterval time.Duration          `yaml:"rebalance_interval" json:"rebalance_interval"`
	ConsistentConfig  ConsistentHashConfig   `yaml:"consistent" json:"consistent"`
	HashConfig        HashShardingConfig     `yaml:"hash" json:"hash"`
	RangeConfig       RangeShardingConfig    `yaml:"range" json:"range"`
	HealthCheck       ShardHealthCheckConfig `yaml:"health_check" json:"health_check"`
	Monitoring        ShardMonitoringConfig  `yaml:"monitoring" json:"monitoring"`
}

// ShardHealthCheckConfig contains health check configuration
type ShardHealthCheckConfig struct {
	Enabled   bool          `yaml:"enabled" json:"enabled"`
	Interval  time.Duration `yaml:"interval" json:"interval"`
	Timeout   time.Duration `yaml:"timeout" json:"timeout"`
	Threshold float64       `yaml:"threshold" json:"threshold"`
}

// ShardMonitoringConfig contains monitoring configuration
type ShardMonitoringConfig struct {
	Enabled         bool          `yaml:"enabled" json:"enabled"`
	MetricsInterval time.Duration `yaml:"metrics_interval" json:"metrics_interval"`
	StatsRetention  time.Duration `yaml:"stats_retention" json:"stats_retention"`
}

// ShardManagerStats contains statistics for the shard manager
type ShardManagerStats struct {
	Strategy           ShardingStrategy      `json:"strategy"`
	TotalNodes         int                   `json:"total_nodes"`
	ActiveNodes        int                   `json:"active_nodes"`
	TotalShards        int                   `json:"total_shards"`
	KeysDistributed    int64                 `json:"keys_distributed"`
	RebalanceCount     int64                 `json:"rebalance_count"`
	LastRebalance      time.Time             `json:"last_rebalance"`
	HealthCheckCount   int64                 `json:"health_check_count"`
	FailedHealthChecks int64                 `json:"failed_health_checks"`
	NodeStats          map[string]*NodeStats `json:"node_stats"`
	LoadDistribution   map[string]float64    `json:"load_distribution"`
	PerformanceMetrics *PerformanceMetrics   `json:"performance_metrics"`
	StartTime          time.Time             `json:"start_time"`
	Uptime             time.Duration         `json:"uptime"`
}

// NodeStats contains statistics for individual nodes
type NodeStats struct {
	NodeID           string        `json:"node_id"`
	Status           NodeStatus    `json:"status"`
	KeysHandled      int64         `json:"keys_handled"`
	Load             float64       `json:"load"`
	ResponseTime     time.Duration `json:"response_time"`
	ErrorRate        float64       `json:"error_rate"`
	LastHealthCheck  time.Time     `json:"last_health_check"`
	HealthCheckCount int64         `json:"health_check_count"`
	FailedChecks     int64         `json:"failed_checks"`
	Capacity         int64         `json:"capacity"`
	Used             int64         `json:"used"`
	Utilization      float64       `json:"utilization"`
}

// PerformanceMetrics contains performance metrics
type PerformanceMetrics struct {
	AverageLatency    time.Duration `json:"average_latency"`
	P50Latency        time.Duration `json:"p50_latency"`
	P95Latency        time.Duration `json:"p95_latency"`
	P99Latency        time.Duration `json:"p99_latency"`
	RequestsPerSecond float64       `json:"requests_per_second"`
	ErrorRate         float64       `json:"error_rate"`
	ThroughputMBps    float64       `json:"throughput_mbps"`
}

// ShardingInterface defines the common interface for all sharding strategies
type ShardingInterface interface {
	GetNode(key string) (interface{}, error)
	GetNodes(key string, count int) ([]interface{}, error)
	GetAllNodes() []interface{}
	GetActiveNodes() []interface{}
	UpdateNodeStatus(nodeID string, status NodeStatus) error
	GetStats() interface{}
	Rebalance() error
}

// NewShardManager creates a new shard manager
func NewShardManager(config ShardingConfig, logger common.Logger, metrics common.Metrics) (*ShardManager, error) {
	sm := &ShardManager{
		strategy: config.Strategy,
		config:   config,
		logger:   logger,
		metrics:  metrics,
		stats: ShardManagerStats{
			Strategy:           config.Strategy,
			NodeStats:          make(map[string]*NodeStats),
			LoadDistribution:   make(map[string]float64),
			PerformanceMetrics: &PerformanceMetrics{},
			StartTime:          time.Now(),
		},
	}

	// Initialize the specific sharding implementation
	if err := sm.initializeSharding(); err != nil {
		return nil, fmt.Errorf("failed to initialize sharding: %w", err)
	}

	return sm, nil
}

// Start starts the shard manager
func (sm *ShardManager) Start(ctx context.Context) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.started {
		return fmt.Errorf("shard manager already started")
	}

	// Start auto-rebalancing if enabled
	if sm.config.AutoRebalance {
		go sm.autoRebalanceLoop(ctx)
	}

	// Start health checking if enabled
	if sm.config.HealthCheck.Enabled {
		go sm.healthCheckLoop(ctx)
	}

	// Start metrics collection if enabled
	if sm.config.Monitoring.Enabled {
		go sm.metricsLoop(ctx)
	}

	sm.started = true

	sm.logger.Info("shard manager started",
		logger.String("strategy", string(sm.strategy)),
		logger.Bool("auto_rebalance", sm.config.AutoRebalance),
		logger.Bool("health_check", sm.config.HealthCheck.Enabled),
	)

	if sm.metrics != nil {
		sm.metrics.Counter("forge.cache.sharding.manager.started").Inc()
	}

	return nil
}

// Stop stops the shard manager
func (sm *ShardManager) Stop(ctx context.Context) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if !sm.started {
		return fmt.Errorf("shard manager not started")
	}

	sm.started = false

	sm.logger.Info("shard manager stopped")

	if sm.metrics != nil {
		sm.metrics.Counter("forge.cache.sharding.manager.stopped").Inc()
	}

	return nil
}

// AddNode adds a node to the sharding system
func (sm *ShardManager) AddNode(nodeConfig interface{}) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	switch sm.strategy {
	case ShardingStrategyConsistent:
		if node, ok := nodeConfig.(*Node); ok {
			return sm.consistent.AddNode(node)
		}
	case ShardingStrategyHash:
		if node, ok := nodeConfig.(*ShardNode); ok {
			return sm.hash.AddNode(node)
		}
	case ShardingStrategyRange:
		if node, ok := nodeConfig.(*RangeNode); ok {
			return sm.rangeSharding.AddNode(node)
		}
	}

	return fmt.Errorf("invalid node configuration for strategy %s", sm.strategy)
}

// RemoveNode removes a node from the sharding system
func (sm *ShardManager) RemoveNode(nodeID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	switch sm.strategy {
	case ShardingStrategyConsistent:
		return sm.consistent.RemoveNode(nodeID)
	case ShardingStrategyHash:
		return sm.hash.RemoveNode(nodeID)
	case ShardingStrategyRange:
		return sm.rangeSharding.RemoveNode(nodeID)
	}

	return fmt.Errorf("unsupported strategy: %s", sm.strategy)
}

// GetNode returns the node responsible for a given key
func (sm *ShardManager) GetNode(key string) (interface{}, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	start := time.Now()
	defer func() {
		latency := time.Since(start)
		sm.updatePerformanceMetrics("get_node", latency, nil)
	}()

	switch sm.strategy {
	case ShardingStrategyConsistent:
		return sm.consistent.GetNode(key)
	case ShardingStrategyHash:
		return sm.hash.GetNode(key)
	case ShardingStrategyRange:
		return sm.rangeSharding.GetNode(key)
	}

	return nil, fmt.Errorf("unsupported strategy: %s", sm.strategy)
}

// GetNodes returns multiple nodes for replication
func (sm *ShardManager) GetNodes(key string, count int) ([]interface{}, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	start := time.Now()
	defer func() {
		latency := time.Since(start)
		sm.updatePerformanceMetrics("get_nodes", latency, nil)
	}()

	var nodes []interface{}

	switch sm.strategy {
	case ShardingStrategyConsistent:
		consistentNodes, err := sm.consistent.GetNodes(key, count)
		if err != nil {
			return nil, err
		}
		for _, node := range consistentNodes {
			nodes = append(nodes, node)
		}
	case ShardingStrategyHash:
		hashNodes, err := sm.hash.GetNodes(key, count)
		if err != nil {
			return nil, err
		}
		for _, node := range hashNodes {
			nodes = append(nodes, node)
		}
	case ShardingStrategyRange:
		rangeNodes, err := sm.rangeSharding.GetNodes(key, count)
		if err != nil {
			return nil, err
		}
		for _, node := range rangeNodes {
			nodes = append(nodes, node)
		}
	default:
		return nil, fmt.Errorf("unsupported strategy: %s", sm.strategy)
	}

	return nodes, nil
}

// GetAllNodes returns all nodes in the sharding system
func (sm *ShardManager) GetAllNodes() []interface{} {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var nodes []interface{}

	switch sm.strategy {
	case ShardingStrategyConsistent:
		consistentNodes := sm.consistent.GetAllNodes()
		for _, node := range consistentNodes {
			nodes = append(nodes, node)
		}
	case ShardingStrategyHash:
		hashNodes := sm.hash.GetAllNodes()
		for _, node := range hashNodes {
			nodes = append(nodes, node)
		}
	case ShardingStrategyRange:
		rangeNodes := sm.rangeSharding.GetAllNodes()
		for _, node := range rangeNodes {
			nodes = append(nodes, node)
		}
	}

	return nodes
}

// GetActiveNodes returns all active nodes
func (sm *ShardManager) GetActiveNodes() []interface{} {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var nodes []interface{}

	switch sm.strategy {
	case ShardingStrategyConsistent:
		consistentNodes := sm.consistent.GetActiveNodes()
		for _, node := range consistentNodes {
			nodes = append(nodes, node)
		}
	case ShardingStrategyHash:
		hashNodes := sm.hash.GetActiveNodes()
		for _, node := range hashNodes {
			nodes = append(nodes, node)
		}
	case ShardingStrategyRange:
		rangeNodes := sm.rangeSharding.GetActiveNodes()
		for _, node := range rangeNodes {
			nodes = append(nodes, node)
		}
	}

	return nodes
}

// UpdateNodeStatus updates the status of a node
func (sm *ShardManager) UpdateNodeStatus(nodeID string, status NodeStatus) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Update node status in the appropriate sharding implementation
	var err error
	switch sm.strategy {
	case ShardingStrategyConsistent:
		err = sm.consistent.UpdateNodeStatus(nodeID, status)
	case ShardingStrategyHash:
		err = sm.hash.UpdateNodeStatus(nodeID, status)
	case ShardingStrategyRange:
		err = sm.rangeSharding.UpdateNodeStatus(nodeID, status)
	default:
		err = fmt.Errorf("unsupported strategy: %s", sm.strategy)
	}

	if err != nil {
		return err
	}

	// Update local stats
	if nodeStats, exists := sm.stats.NodeStats[nodeID]; exists {
		nodeStats.Status = status
	}

	sm.logger.Info("node status updated",
		logger.String("node_id", nodeID),
		logger.String("status", string(status)),
		logger.String("strategy", string(sm.strategy)),
	)

	if sm.metrics != nil {
		sm.metrics.Counter("forge.cache.sharding.node.status.updated", "node", nodeID, "status", string(status)).Inc()
	}

	return nil
}

// Rebalance triggers a rebalance of the sharding system
func (sm *ShardManager) Rebalance() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	start := time.Now()

	var err error
	switch sm.strategy {
	case ShardingStrategyConsistent:
		err = sm.consistent.Rebalance()
	case ShardingStrategyHash:
		err = sm.hash.Rebalance()
	case ShardingStrategyRange:
		err = sm.rangeSharding.Rebalance()
	default:
		err = fmt.Errorf("unsupported strategy: %s", sm.strategy)
	}

	if err != nil {
		return err
	}

	sm.stats.RebalanceCount++
	sm.stats.LastRebalance = time.Now()

	duration := time.Since(start)
	sm.logger.Info("rebalance completed",
		logger.String("strategy", string(sm.strategy)),
		logger.Duration("duration", duration),
		logger.Int64("rebalance_count", sm.stats.RebalanceCount),
	)

	if sm.metrics != nil {
		sm.metrics.Counter("forge.cache.sharding.rebalances").Inc()
		sm.metrics.Histogram("forge.cache.sharding.rebalance.duration").Observe(duration.Seconds())
	}

	return nil
}

// GetStats returns shard manager statistics
func (sm *ShardManager) GetStats() ShardManagerStats {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	// Update uptime
	sm.stats.Uptime = time.Since(sm.stats.StartTime)

	// Get stats from the underlying sharding implementation
	switch sm.strategy {
	case ShardingStrategyConsistent:
		consistentStats := sm.consistent.GetStats()
		sm.stats.TotalNodes = consistentStats.TotalNodes
		sm.stats.ActiveNodes = consistentStats.ActiveNodes
		sm.stats.KeysDistributed = consistentStats.KeysDistributed
	case ShardingStrategyHash:
		hashStats := sm.hash.GetStats()
		sm.stats.TotalNodes = hashStats.TotalNodes
		sm.stats.ActiveNodes = hashStats.ActiveNodes
		sm.stats.KeysDistributed = hashStats.KeysDistributed
	case ShardingStrategyRange:
		rangeStats := sm.rangeSharding.GetStats()
		sm.stats.TotalNodes = rangeStats.TotalNodes
		sm.stats.ActiveNodes = rangeStats.ActiveNodes
		sm.stats.KeysDistributed = rangeStats.KeysDistributed
	}

	return sm.stats
}

// GetNodeDistribution returns the distribution of keys across nodes
func (sm *ShardManager) GetNodeDistribution() map[string]int64 {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	switch sm.strategy {
	case ShardingStrategyConsistent:
		return sm.consistent.GetNodeDistribution()
	case ShardingStrategyHash:
		return sm.hash.GetNodeDistribution()
	case ShardingStrategyRange:
		return sm.rangeSharding.GetNodeDistribution()
	}

	return make(map[string]int64)
}

// HealthCheck performs health checks on all nodes
func (sm *ShardManager) HealthCheck(ctx context.Context) error {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if !sm.config.HealthCheck.Enabled {
		return nil
	}

	sm.stats.HealthCheckCount++

	activeNodes := sm.GetActiveNodes()
	if len(activeNodes) == 0 {
		sm.stats.FailedHealthChecks++
		return fmt.Errorf("no active nodes available")
	}

	// Check if we have enough active nodes
	healthyRatio := float64(len(activeNodes)) / float64(sm.stats.TotalNodes)
	if healthyRatio < sm.config.HealthCheck.Threshold {
		sm.stats.FailedHealthChecks++
		return fmt.Errorf("healthy node ratio %.2f below threshold %.2f", healthyRatio, sm.config.HealthCheck.Threshold)
	}

	return nil
}

// IsStarted returns true if the shard manager is started
func (sm *ShardManager) IsStarted() bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.started
}

// GetStrategy returns the current sharding strategy
func (sm *ShardManager) GetStrategy() ShardingStrategy {
	return sm.strategy
}

// Private methods

func (sm *ShardManager) initializeSharding() error {
	switch sm.strategy {
	case ShardingStrategyConsistent:
		sm.consistent = NewConsistentHash(sm.config.ConsistentConfig, sm.logger, sm.metrics)
	case ShardingStrategyHash:
		sm.hash = NewHashSharding(sm.config.HashConfig, sm.logger, sm.metrics)
	case ShardingStrategyRange:
		sm.rangeSharding = NewRangeSharding(sm.config.RangeConfig, sm.logger, sm.metrics)
	default:
		return fmt.Errorf("unsupported sharding strategy: %s", sm.strategy)
	}

	return nil
}

func (sm *ShardManager) autoRebalanceLoop(ctx context.Context) {
	ticker := time.NewTicker(sm.config.RebalanceInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := sm.Rebalance(); err != nil {
				sm.logger.Error("auto-rebalance failed", logger.Error(err))
			}
		}
	}
}

func (sm *ShardManager) healthCheckLoop(ctx context.Context) {
	ticker := time.NewTicker(sm.config.HealthCheck.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := sm.performHealthCheck(ctx); err != nil {
				sm.logger.Error("health check failed", logger.Error(err))
			}
		}
	}
}

func (sm *ShardManager) metricsLoop(ctx context.Context) {
	ticker := time.NewTicker(sm.config.Monitoring.MetricsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sm.collectMetrics()
		}
	}
}

func (sm *ShardManager) performHealthCheck(ctx context.Context) error {
	checkCtx, cancel := context.WithTimeout(ctx, sm.config.HealthCheck.Timeout)
	defer cancel()

	return sm.HealthCheck(checkCtx)
}

func (sm *ShardManager) collectMetrics() {
	// Update node statistics
	for nodeID, nodeStats := range sm.stats.NodeStats {
		if nodeStats.Capacity > 0 {
			nodeStats.Utilization = float64(nodeStats.Used) / float64(nodeStats.Capacity)
		}

		if nodeStats.KeysHandled > 0 {
			nodeStats.ErrorRate = float64(nodeStats.FailedChecks) / float64(nodeStats.KeysHandled)
		}

		sm.stats.LoadDistribution[nodeID] = nodeStats.Load
	}

	// Update performance metrics
	if sm.metrics != nil {
		sm.metrics.Gauge("forge.cache.sharding.nodes.total").Set(float64(sm.stats.TotalNodes))
		sm.metrics.Gauge("forge.cache.sharding.nodes.active").Set(float64(sm.stats.ActiveNodes))
		sm.metrics.Counter("forge.cache.sharding.keys.distributed").Add(float64(sm.stats.KeysDistributed))
	}
}

func (sm *ShardManager) updatePerformanceMetrics(operation string, latency time.Duration, err error) {
	// Update performance metrics
	if sm.stats.PerformanceMetrics.AverageLatency == 0 {
		sm.stats.PerformanceMetrics.AverageLatency = latency
	} else {
		// Simple moving average
		sm.stats.PerformanceMetrics.AverageLatency = (sm.stats.PerformanceMetrics.AverageLatency + latency) / 2
	}

	if err != nil {
		sm.stats.PerformanceMetrics.ErrorRate = (sm.stats.PerformanceMetrics.ErrorRate + 1.0) / 2.0
	} else {
		sm.stats.PerformanceMetrics.ErrorRate = sm.stats.PerformanceMetrics.ErrorRate * 0.99 // Decay error rate
	}

	if sm.metrics != nil {
		sm.metrics.Histogram("forge.cache.sharding.operation.duration", "operation", operation).Observe(latency.Seconds())
		if err != nil {
			sm.metrics.Counter("forge.cache.sharding.operation.errors", "operation", operation).Inc()
		}
	}
}

// Helper functions

// DefaultShardingConfig returns a default sharding configuration
func DefaultShardingConfig() ShardingConfig {
	return ShardingConfig{
		Strategy:          ShardingStrategyConsistent,
		ReplicationFactor: 3,
		AutoRebalance:     true,
		RebalanceInterval: 5 * time.Minute,
		ConsistentConfig: ConsistentHashConfig{
			VirtualNodes: 150,
			HashFunction: "sha256",
			Replicas:     3,
		},
		HashConfig: HashShardingConfig{
			Algorithm:          HashAlgorithmFNV64,
			ReplicationFactor:  3,
			LoadBalancing:      true,
			AutoRebalance:      true,
			RebalanceThreshold: 0.2,
		},
		RangeConfig: RangeShardingConfig{
			Strategy:           RangeStrategyAlphabetic,
			InitialRanges:      16,
			AutoRebalance:      true,
			RebalanceThreshold: 0.2,
			RangeSize:          1000000,
			SplitThreshold:     500000,
			MergeThreshold:     100000,
		},
		HealthCheck: ShardHealthCheckConfig{
			Enabled:   true,
			Interval:  30 * time.Second,
			Timeout:   5 * time.Second,
			Threshold: 0.7,
		},
		Monitoring: ShardMonitoringConfig{
			Enabled:         true,
			MetricsInterval: 30 * time.Second,
			StatsRetention:  24 * time.Hour,
		},
	}
}
