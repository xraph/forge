package monitoring

import (
	"context"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/consensus"
	"github.com/xraph/forge/pkg/logger"
)

// ConsensusMetricsCollector collects comprehensive metrics for the consensus system
type ConsensusMetricsCollector struct {
	consensusManager *consensus.ConsensusManager
	metrics          common.Metrics
	logger           common.Logger
	config           ConsensusMetricsConfig

	// Metric collectors
	clusterMetrics     map[string]*ClusterMetrics
	electionMetrics    *ElectionMetrics
	performanceMetrics *PerformanceMetrics
	mu                 sync.RWMutex

	// Collection state
	started          bool
	stopCh           chan struct{}
	collectionTicker *time.Ticker
}

// ConsensusMetricsConfig contains configuration for metrics collection
type ConsensusMetricsConfig struct {
	CollectionInterval       time.Duration `yaml:"collection_interval" default:"15s"`
	EnableDetailedMetrics    bool          `yaml:"enable_detailed_metrics" default:"true"`
	EnablePerformanceMetrics bool          `yaml:"enable_performance_metrics" default:"true"`
	MetricRetentionPeriod    time.Duration `yaml:"metric_retention_period" default:"24h"`
	BatchSize                int           `yaml:"batch_size" default:"100"`
}

// ClusterMetrics tracks metrics for a specific cluster
type ClusterMetrics struct {
	ClusterID string

	// Node metrics
	TotalNodes     int64
	ActiveNodes    int64
	InactiveNodes  int64
	SuspectedNodes int64
	FailedNodes    int64

	// Leadership metrics
	LeaderPresent    bool
	LeaderID         string
	LeaderElections  int64
	LastElection     time.Time
	ElectionDuration time.Duration

	// Log metrics
	LogSize     int64
	LogEntries  int64
	CommitIndex uint64
	LastApplied uint64

	// Health metrics
	HealthStatus    string
	LastHealthCheck time.Time
	HealthyRatio    float64

	// Performance metrics
	OperationsPerSecond float64
	AverageLatency      time.Duration
	ErrorRate           float64

	mu sync.RWMutex
}

// ElectionMetrics tracks election-related metrics across all clusters
type ElectionMetrics struct {
	TotalElections      int64
	SuccessfulElections int64
	FailedElections     int64
	AverageElectionTime time.Duration
	LongestElection     time.Duration
	ShortestElection    time.Duration
	ElectionsPerHour    float64
	LastElection        time.Time

	mu sync.RWMutex
}

// PerformanceMetrics tracks performance-related metrics
type PerformanceMetrics struct {
	// Throughput metrics
	OperationsPerSecond     float64
	PeakOperationsPerSecond float64

	// Latency metrics
	AverageLatency time.Duration
	P50Latency     time.Duration
	P95Latency     time.Duration
	P99Latency     time.Duration
	MaxLatency     time.Duration

	// Error metrics
	TotalOperations int64
	SuccessfulOps   int64
	FailedOps       int64
	ErrorRate       float64

	// Resource metrics
	MemoryUsage     int64
	CPUUsage        float64
	NetworkBytesIn  int64
	NetworkBytesOut int64

	mu sync.RWMutex
}

// NewConsensusMetricsCollector creates a new consensus metrics collector
func NewConsensusMetricsCollector(consensusManager *consensus.ConsensusManager, metrics common.Metrics, logger common.Logger, config ConsensusMetricsConfig) *ConsensusMetricsCollector {
	return &ConsensusMetricsCollector{
		consensusManager:   consensusManager,
		metrics:            metrics,
		logger:             logger,
		config:             config,
		clusterMetrics:     make(map[string]*ClusterMetrics),
		electionMetrics:    &ElectionMetrics{},
		performanceMetrics: &PerformanceMetrics{},
		stopCh:             make(chan struct{}),
	}
}

// Start starts the metrics collection
func (cmc *ConsensusMetricsCollector) Start(ctx context.Context) error {
	cmc.mu.Lock()
	defer cmc.mu.Unlock()

	if cmc.started {
		return nil
	}

	// Initialize metrics
	cmc.initializeMetrics()

	// Start collection ticker
	cmc.collectionTicker = time.NewTicker(cmc.config.CollectionInterval)
	cmc.started = true

	// Start collection goroutine
	go cmc.collectMetricsLoop(ctx)

	if cmc.logger != nil {
		cmc.logger.Info("consensus metrics collector started",
			logger.Duration("interval", cmc.config.CollectionInterval),
		)
	}

	return nil
}

// Stop stops the metrics collection
func (cmc *ConsensusMetricsCollector) Stop(ctx context.Context) error {
	cmc.mu.Lock()
	defer cmc.mu.Unlock()

	if !cmc.started {
		return nil
	}

	close(cmc.stopCh)

	if cmc.collectionTicker != nil {
		cmc.collectionTicker.Stop()
	}

	cmc.started = false

	if cmc.logger != nil {
		cmc.logger.Info("consensus metrics collector stopped")
	}

	return nil
}

// initializeMetrics initializes all metrics
func (cmc *ConsensusMetricsCollector) initializeMetrics() {
	if cmc.metrics == nil {
		return
	}

	// Cluster metrics
	cmc.metrics.Counter("forge.consensus.clusters_total")
	cmc.metrics.Gauge("forge.consensus.clusters_active")
	cmc.metrics.Gauge("forge.consensus.clusters_healthy")
	cmc.metrics.Gauge("forge.consensus.clusters_degraded")
	cmc.metrics.Gauge("forge.consensus.clusters_unhealthy")

	// Node metrics
	cmc.metrics.Gauge("forge.consensus.nodes_total")
	cmc.metrics.Gauge("forge.consensus.nodes_active")
	cmc.metrics.Gauge("forge.consensus.nodes_inactive")
	cmc.metrics.Gauge("forge.consensus.nodes_failed")

	// Leadership metrics
	cmc.metrics.Counter("forge.consensus.leader_elections_total")
	cmc.metrics.Histogram("forge.consensus.leader_election_duration")
	cmc.metrics.Gauge("forge.consensus.leaders_present")

	// Performance metrics
	cmc.metrics.Histogram("forge.consensus.operation_duration")
	cmc.metrics.Counter("forge.consensus.operations_total")
	cmc.metrics.Counter("forge.consensus.operations_failed")
	cmc.metrics.Gauge("forge.consensus.operations_per_second")

	// Log metrics
	cmc.metrics.Gauge("forge.consensus.log_size")
	cmc.metrics.Gauge("forge.consensus.log_entries")
	cmc.metrics.Counter("forge.consensus.log_entries_appended")
	cmc.metrics.Gauge("forge.consensus.commit_index")
	cmc.metrics.Gauge("forge.consensus.last_applied")

	// Health metrics
	cmc.metrics.Gauge("forge.consensus.health_status")
	cmc.metrics.Counter("forge.consensus.health_checks_total")
	cmc.metrics.Histogram("forge.consensus.health_check_duration")
}

// collectMetricsLoop runs the metrics collection loop
func (cmc *ConsensusMetricsCollector) collectMetricsLoop(ctx context.Context) {
	for {
		select {
		case <-cmc.collectionTicker.C:
			cmc.collectAllMetrics()
		case <-cmc.stopCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

// collectAllMetrics collects all consensus metrics
func (cmc *ConsensusMetricsCollector) collectAllMetrics() {
	startTime := time.Now()

	// Collect cluster metrics
	cmc.collectClusterMetrics()

	// Collect election metrics
	if cmc.config.EnableDetailedMetrics {
		cmc.collectElectionMetrics()
	}

	// Collect performance metrics
	if cmc.config.EnablePerformanceMetrics {
		cmc.collectPerformanceMetrics()
	}

	// Record collection duration
	collectionDuration := time.Since(startTime)
	if cmc.metrics != nil {
		cmc.metrics.Histogram("forge.consensus.metrics_collection_duration").Observe(collectionDuration.Seconds())
	}

	if cmc.logger != nil {
		cmc.logger.Debug("consensus metrics collected",
			logger.Duration("duration", collectionDuration),
		)
	}
}

// collectClusterMetrics collects metrics for all clusters
func (cmc *ConsensusMetricsCollector) collectClusterMetrics() {
	clusters := cmc.consensusManager.GetClusters()

	totalClusters := len(clusters)
	healthyClusters := 0
	degradedClusters := 0
	unhealthyClusters := 0
	totalNodes := 0
	activeNodes := 0
	inactiveNodes := 0
	failedNodes := 0
	leadersPresent := 0

	// Collect metrics for each cluster
	for _, cluster := range clusters {
		clusterID := cluster.ID()

		// Get or create cluster metrics
		cmc.mu.Lock()
		clusterMetrics, exists := cmc.clusterMetrics[clusterID]
		if !exists {
			clusterMetrics = &ClusterMetrics{ClusterID: clusterID}
			cmc.clusterMetrics[clusterID] = clusterMetrics
		}
		cmc.mu.Unlock()

		// Update cluster metrics
		cmc.updateClusterMetrics(cluster, clusterMetrics)

		// Aggregate metrics
		health := cluster.GetHealth()
		switch health.Status {
		case common.HealthStatusHealthy:
			healthyClusters++
		case common.HealthStatusDegraded:
			degradedClusters++
		case common.HealthStatusUnhealthy:
			unhealthyClusters++
		}

		totalNodes += health.TotalNodes
		activeNodes += health.ActiveNodes
		inactiveNodes += (health.TotalNodes - health.ActiveNodes)

		if health.LeaderPresent {
			leadersPresent++
		}

		// Count failed nodes
		for _, nodeHealth := range health.NodeHealth {
			if !nodeHealth.IsReachable {
				failedNodes++
			}
		}
	}

	// Update aggregate metrics
	if cmc.metrics != nil {
		cmc.metrics.Gauge("forge.consensus.clusters_total").Set(float64(totalClusters))
		cmc.metrics.Gauge("forge.consensus.clusters_healthy").Set(float64(healthyClusters))
		cmc.metrics.Gauge("forge.consensus.clusters_degraded").Set(float64(degradedClusters))
		cmc.metrics.Gauge("forge.consensus.clusters_unhealthy").Set(float64(unhealthyClusters))

		cmc.metrics.Gauge("forge.consensus.nodes_total").Set(float64(totalNodes))
		cmc.metrics.Gauge("forge.consensus.nodes_active").Set(float64(activeNodes))
		cmc.metrics.Gauge("forge.consensus.nodes_inactive").Set(float64(inactiveNodes))
		cmc.metrics.Gauge("forge.consensus.nodes_failed").Set(float64(failedNodes))

		cmc.metrics.Gauge("forge.consensus.leaders_present").Set(float64(leadersPresent))
	}
}

// updateClusterMetrics updates metrics for a specific cluster
func (cmc *ConsensusMetricsCollector) updateClusterMetrics(cluster consensus.Cluster, metrics *ClusterMetrics) {
	metrics.mu.Lock()
	defer metrics.mu.Unlock()

	health := cluster.GetHealth()
	nodes := cluster.GetNodes()

	// Update node counts
	metrics.TotalNodes = int64(len(nodes))
	metrics.ActiveNodes = int64(health.ActiveNodes)
	metrics.InactiveNodes = metrics.TotalNodes - metrics.ActiveNodes

	suspectedNodes := int64(0)
	failedNodes := int64(0)

	for _, node := range nodes {
		switch node.Status() {
		case consensus.NodeStatusSuspected:
			suspectedNodes++
		case consensus.NodeStatusFailed:
			failedNodes++
		}
	}

	metrics.SuspectedNodes = suspectedNodes
	metrics.FailedNodes = failedNodes

	// Update leadership info
	leader := cluster.GetLeader()
	metrics.LeaderPresent = leader != nil
	if leader != nil {
		metrics.LeaderID = leader.ID()
	} else {
		metrics.LeaderID = ""
	}

	metrics.LastElection = health.LastElection

	// Update health info
	metrics.HealthStatus = string(health.Status)
	metrics.LastHealthCheck = time.Now()
	if metrics.TotalNodes > 0 {
		metrics.HealthyRatio = float64(metrics.ActiveNodes) / float64(metrics.TotalNodes)
	}

	// Update cluster-specific metrics
	if cmc.metrics != nil {
		tags := []string{"cluster", cluster.ID()}

		cmc.metrics.Gauge("forge.consensus.cluster_nodes_total", tags...).Set(float64(metrics.TotalNodes))
		cmc.metrics.Gauge("forge.consensus.cluster_nodes_active", tags...).Set(float64(metrics.ActiveNodes))
		cmc.metrics.Gauge("forge.consensus.cluster_nodes_inactive", tags...).Set(float64(metrics.InactiveNodes))
		cmc.metrics.Gauge("forge.consensus.cluster_nodes_failed", tags...).Set(float64(metrics.FailedNodes))
		cmc.metrics.Gauge("forge.consensus.cluster_healthy_ratio", tags...).Set(metrics.HealthyRatio)

		leaderPresent := 0.0
		if metrics.LeaderPresent {
			leaderPresent = 1.0
		}
		cmc.metrics.Gauge("forge.consensus.cluster_leader_present", tags...).Set(leaderPresent)
	}
}

// collectElectionMetrics collects election-related metrics
func (cmc *ConsensusMetricsCollector) collectElectionMetrics() {
	cmc.electionMetrics.mu.Lock()
	defer cmc.electionMetrics.mu.Unlock()

	// This would be enhanced with actual election data from the consensus system
	// For now, we'll collect basic aggregated metrics

	clusters := cmc.consensusManager.GetClusters()
	totalElections := int64(0)

	for _, cluster := range clusters {
		// In a real implementation, we'd track elections per cluster
		// For now, we'll use placeholder logic
		health := cluster.GetHealth()
		if !health.LastElection.IsZero() {
			totalElections++
		}
	}

	cmc.electionMetrics.TotalElections = totalElections
	cmc.electionMetrics.LastElection = time.Now()

	// Update election metrics
	if cmc.metrics != nil {
		cmc.metrics.Counter("forge.consensus.leader_elections_total").Add(float64(totalElections))
	}
}

// collectPerformanceMetrics collects performance-related metrics
func (cmc *ConsensusMetricsCollector) collectPerformanceMetrics() {
	cmc.performanceMetrics.mu.Lock()
	defer cmc.performanceMetrics.mu.Unlock()

	// Collect consensus stats
	stats := cmc.consensusManager.GetStats()

	// Update performance metrics based on stats
	// This would be enhanced with actual performance data
	cmc.performanceMetrics.TotalOperations++

	if cmc.metrics != nil {
		cmc.metrics.Gauge("forge.consensus.total_operations").Set(float64(cmc.performanceMetrics.TotalOperations))
		cmc.metrics.Gauge("forge.consensus.successful_operations").Set(float64(cmc.performanceMetrics.SuccessfulOps))
		cmc.metrics.Gauge("forge.consensus.failed_operations").Set(float64(cmc.performanceMetrics.FailedOps))
		cmc.metrics.Gauge("forge.consensus.error_rate").Set(cmc.performanceMetrics.ErrorRate)
		cmc.metrics.Gauge("forge.consensus.operations_per_second").Set(cmc.performanceMetrics.OperationsPerSecond)
		cmc.metrics.Gauge("forge.consensus.total_clusters").Set(float64(stats.Clusters))
		cmc.metrics.Gauge("forge.consensus.total_nodes").Set(float64(stats.TotalNodes))
	}
}

// GetClusterMetrics returns metrics for a specific cluster
func (cmc *ConsensusMetricsCollector) GetClusterMetrics(clusterID string) *ClusterMetrics {
	cmc.mu.RLock()
	defer cmc.mu.RUnlock()

	metrics, exists := cmc.clusterMetrics[clusterID]
	if !exists {
		return nil
	}

	// Return a copy to avoid concurrent access issues
	metrics.mu.RLock()
	defer metrics.mu.RUnlock()

	return &ClusterMetrics{
		ClusterID:           metrics.ClusterID,
		TotalNodes:          metrics.TotalNodes,
		ActiveNodes:         metrics.ActiveNodes,
		InactiveNodes:       metrics.InactiveNodes,
		SuspectedNodes:      metrics.SuspectedNodes,
		FailedNodes:         metrics.FailedNodes,
		LeaderPresent:       metrics.LeaderPresent,
		LeaderID:            metrics.LeaderID,
		LeaderElections:     metrics.LeaderElections,
		LastElection:        metrics.LastElection,
		ElectionDuration:    metrics.ElectionDuration,
		LogSize:             metrics.LogSize,
		LogEntries:          metrics.LogEntries,
		CommitIndex:         metrics.CommitIndex,
		LastApplied:         metrics.LastApplied,
		HealthStatus:        metrics.HealthStatus,
		LastHealthCheck:     metrics.LastHealthCheck,
		HealthyRatio:        metrics.HealthyRatio,
		OperationsPerSecond: metrics.OperationsPerSecond,
		AverageLatency:      metrics.AverageLatency,
		ErrorRate:           metrics.ErrorRate,
	}
}

// GetElectionMetrics returns election metrics
func (cmc *ConsensusMetricsCollector) GetElectionMetrics() *ElectionMetrics {
	cmc.electionMetrics.mu.RLock()
	defer cmc.electionMetrics.mu.RUnlock()

	return &ElectionMetrics{
		TotalElections:      cmc.electionMetrics.TotalElections,
		SuccessfulElections: cmc.electionMetrics.SuccessfulElections,
		FailedElections:     cmc.electionMetrics.FailedElections,
		AverageElectionTime: cmc.electionMetrics.AverageElectionTime,
		LongestElection:     cmc.electionMetrics.LongestElection,
		ShortestElection:    cmc.electionMetrics.ShortestElection,
		ElectionsPerHour:    cmc.electionMetrics.ElectionsPerHour,
		LastElection:        cmc.electionMetrics.LastElection,
	}
}

// GetPerformanceMetrics returns performance metrics
func (cmc *ConsensusMetricsCollector) GetPerformanceMetrics() *PerformanceMetrics {
	cmc.performanceMetrics.mu.RLock()
	defer cmc.performanceMetrics.mu.RUnlock()

	return &PerformanceMetrics{
		OperationsPerSecond:     cmc.performanceMetrics.OperationsPerSecond,
		PeakOperationsPerSecond: cmc.performanceMetrics.PeakOperationsPerSecond,
		AverageLatency:          cmc.performanceMetrics.AverageLatency,
		P50Latency:              cmc.performanceMetrics.P50Latency,
		P95Latency:              cmc.performanceMetrics.P95Latency,
		P99Latency:              cmc.performanceMetrics.P99Latency,
		MaxLatency:              cmc.performanceMetrics.MaxLatency,
		TotalOperations:         cmc.performanceMetrics.TotalOperations,
		SuccessfulOps:           cmc.performanceMetrics.SuccessfulOps,
		FailedOps:               cmc.performanceMetrics.FailedOps,
		ErrorRate:               cmc.performanceMetrics.ErrorRate,
		MemoryUsage:             cmc.performanceMetrics.MemoryUsage,
		CPUUsage:                cmc.performanceMetrics.CPUUsage,
		NetworkBytesIn:          cmc.performanceMetrics.NetworkBytesIn,
		NetworkBytesOut:         cmc.performanceMetrics.NetworkBytesOut,
	}
}

// RecordOperation records an operation for metrics tracking
func (cmc *ConsensusMetricsCollector) RecordOperation(duration time.Duration, success bool) {
	if cmc.metrics != nil {
		cmc.metrics.Histogram("forge.consensus.operation_duration").Observe(duration.Seconds())
		cmc.metrics.Counter("forge.consensus.operations_total").Inc()

		if !success {
			cmc.metrics.Counter("forge.consensus.operations_failed").Inc()
		}
	}

	// Update performance metrics
	cmc.performanceMetrics.mu.Lock()
	cmc.performanceMetrics.TotalOperations++
	if success {
		cmc.performanceMetrics.SuccessfulOps++
	} else {
		cmc.performanceMetrics.FailedOps++
	}

	// Update error rate
	if cmc.performanceMetrics.TotalOperations > 0 {
		cmc.performanceMetrics.ErrorRate = float64(cmc.performanceMetrics.FailedOps) / float64(cmc.performanceMetrics.TotalOperations)
	}

	cmc.performanceMetrics.mu.Unlock()
}

// RecordElection records an election event
func (cmc *ConsensusMetricsCollector) RecordElection(duration time.Duration, success bool) {
	if cmc.metrics != nil {
		cmc.metrics.Counter("forge.consensus.leader_elections_total").Inc()
		cmc.metrics.Histogram("forge.consensus.leader_election_duration").Observe(duration.Seconds())
	}

	// Update election metrics
	cmc.electionMetrics.mu.Lock()
	cmc.electionMetrics.TotalElections++
	cmc.electionMetrics.LastElection = time.Now()

	if success {
		cmc.electionMetrics.SuccessfulElections++
	} else {
		cmc.electionMetrics.FailedElections++
	}

	// Update duration stats
	if cmc.electionMetrics.ShortestElection == 0 || duration < cmc.electionMetrics.ShortestElection {
		cmc.electionMetrics.ShortestElection = duration
	}
	if duration > cmc.electionMetrics.LongestElection {
		cmc.electionMetrics.LongestElection = duration
	}

	cmc.electionMetrics.mu.Unlock()
}

// GetMetricsSummary returns a summary of all consensus metrics
func (cmc *ConsensusMetricsCollector) GetMetricsSummary() *ConsensusMetricsSummary {
	clusters := cmc.consensusManager.GetClusters()

	summary := &ConsensusMetricsSummary{
		TotalClusters:      len(clusters),
		TotalNodes:         0,
		ActiveNodes:        0,
		LeadersPresent:     0,
		ElectionMetrics:    cmc.GetElectionMetrics(),
		PerformanceMetrics: cmc.GetPerformanceMetrics(),
		LastCollection:     time.Now(),
	}

	for _, cluster := range clusters {
		health := cluster.GetHealth()
		summary.TotalNodes += health.TotalNodes
		summary.ActiveNodes += health.ActiveNodes

		if health.LeaderPresent {
			summary.LeadersPresent++
		}
	}

	return summary
}

// ConsensusMetricsSummary provides a high-level summary of consensus metrics
type ConsensusMetricsSummary struct {
	TotalClusters      int                 `json:"total_clusters"`
	TotalNodes         int                 `json:"total_nodes"`
	ActiveNodes        int                 `json:"active_nodes"`
	LeadersPresent     int                 `json:"leaders_present"`
	ElectionMetrics    *ElectionMetrics    `json:"election_metrics"`
	PerformanceMetrics *PerformanceMetrics `json:"performance_metrics"`
	LastCollection     time.Time           `json:"last_collection"`
}

// RegisterConsensusMetrics registers consensus metrics collection
func RegisterConsensusMetrics(metricsCollector common.Metrics, consensusManager *consensus.ConsensusManager, logger common.Logger) (*ConsensusMetricsCollector, error) {
	config := ConsensusMetricsConfig{
		CollectionInterval:       15 * time.Second,
		EnableDetailedMetrics:    true,
		EnablePerformanceMetrics: true,
		MetricRetentionPeriod:    24 * time.Hour,
		BatchSize:                100,
	}

	collector := NewConsensusMetricsCollector(consensusManager, metricsCollector, logger, config)

	// Register custom metric collector
	if customCollector, ok := metricsCollector.(interface {
		RegisterCollector(name string, collector func() map[string]interface{}) error
	}); ok {
		return collector, customCollector.RegisterCollector("consensus-system", func() map[string]interface{} {
			summary := collector.GetMetricsSummary()
			return map[string]interface{}{
				"total_clusters":   summary.TotalClusters,
				"total_nodes":      summary.TotalNodes,
				"active_nodes":     summary.ActiveNodes,
				"leaders_present":  summary.LeadersPresent,
				"total_elections":  summary.ElectionMetrics.TotalElections,
				"total_operations": summary.PerformanceMetrics.TotalOperations,
				"error_rate":       summary.PerformanceMetrics.ErrorRate,
				"last_collection":  summary.LastCollection,
			}
		})
	}

	return collector, nil
}
