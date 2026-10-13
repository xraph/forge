package observability

import (
	"context"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// MetricsCollector collects and exports consensus metrics.
type MetricsCollector struct {
	metrics forge.Metrics
	logger  forge.Logger

	// Counters (atomic)
	electionCount       atomic.Int64
	electionFailures    atomic.Int64
	logAppends          atomic.Int64
	logAppendFailures   atomic.Int64
	snapshotCount       atomic.Int64
	snapshotFailures    atomic.Int64
	configChanges       atomic.Int64
	leadershipTransfers atomic.Int64

	// Gauges (protected by mutex)
	currentTerm  uint64
	commitIndex  uint64
	lastApplied  uint64
	logSize      int64
	clusterSize  int64
	healthyNodes int64
	hasQuorum    bool
	currentRole  internal.NodeRole
	mu           sync.RWMutex

	// Histogram data
	replicationLatencies []float64
	applyLatencies       []float64
	histogramMu          sync.Mutex

	// Lifecycle
	started bool
	ctx     context.Context //nolint:containedctx // lifecycle context: created in Start, cancelled in Stop
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// MetricsConfig contains metrics collector configuration.
type MetricsConfig struct {
	NodeID             string
	CollectionInterval time.Duration
	EnableHistograms   bool
	HistogramSize      int
}

// NewMetricsCollector creates a new metrics collector.
func NewMetricsCollector(config MetricsConfig, metrics forge.Metrics, logger forge.Logger) *MetricsCollector {
	if config.CollectionInterval == 0 {
		config.CollectionInterval = 10 * time.Second
	}

	if config.HistogramSize == 0 {
		config.HistogramSize = 1000
	}

	return &MetricsCollector{
		metrics:              metrics,
		logger:               logger,
		replicationLatencies: make([]float64, 0, config.HistogramSize),
		applyLatencies:       make([]float64, 0, config.HistogramSize),
	}
}

// Start starts the metrics collector.
func (mc *MetricsCollector) Start(ctx context.Context) error {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	if mc.started {
		return internal.ErrAlreadyStarted
	}

	mc.ctx, mc.cancel = context.WithCancel(ctx)
	mc.started = true

	// Start collection goroutine
	mc.wg.Add(1)

	go mc.runCollector()

	mc.logger.Info("metrics collector started")

	return nil
}

// Stop stops the metrics collector.
func (mc *MetricsCollector) Stop(ctx context.Context) error {
	mc.mu.Lock()

	if !mc.started {
		mc.mu.Unlock()

		return internal.ErrNotStarted
	}

	mc.mu.Unlock()

	if mc.cancel != nil {
		mc.cancel()
	}

	// Wait for goroutines with timeout
	done := make(chan struct{})

	go func() {
		mc.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		mc.logger.Info("metrics collector stopped")
	case <-ctx.Done():
		mc.logger.Warn("metrics collector stop timed out")
	}

	return nil
}

// RecordElection records an election event.
func (mc *MetricsCollector) RecordElection(success bool) {
	mc.electionCount.Add(1)

	if !success {
		mc.electionFailures.Add(1)
	}
}

// RecordLogAppend records a log append event.
func (mc *MetricsCollector) RecordLogAppend(success bool) {
	mc.logAppends.Add(1)

	if !success {
		mc.logAppendFailures.Add(1)
	}
}

// RecordSnapshot records a snapshot event.
func (mc *MetricsCollector) RecordSnapshot(success bool) {
	mc.snapshotCount.Add(1)

	if !success {
		mc.snapshotFailures.Add(1)
	}
}

// RecordConfigChange records a configuration change.
func (mc *MetricsCollector) RecordConfigChange() {
	mc.configChanges.Add(1)
}

// RecordLeadershipTransfer records a leadership transfer.
func (mc *MetricsCollector) RecordLeadershipTransfer() {
	mc.leadershipTransfers.Add(1)
}

// RecordReplicationLatency records a replication latency.
func (mc *MetricsCollector) RecordReplicationLatency(latencyMs float64) {
	mc.histogramMu.Lock()
	defer mc.histogramMu.Unlock()

	if len(mc.replicationLatencies) >= 1000 {
		copy(mc.replicationLatencies, mc.replicationLatencies[1:])
		mc.replicationLatencies[len(mc.replicationLatencies)-1] = latencyMs
	} else {
		mc.replicationLatencies = append(mc.replicationLatencies, latencyMs)
	}
}

// RecordApplyLatency records a state machine apply latency.
func (mc *MetricsCollector) RecordApplyLatency(latencyMs float64) {
	mc.histogramMu.Lock()
	defer mc.histogramMu.Unlock()

	if len(mc.applyLatencies) >= 1000 {
		copy(mc.applyLatencies, mc.applyLatencies[1:])
		mc.applyLatencies[len(mc.applyLatencies)-1] = latencyMs
	} else {
		mc.applyLatencies = append(mc.applyLatencies, latencyMs)
	}
}

// UpdateGauges updates gauge metrics.
func (mc *MetricsCollector) UpdateGauges(stats internal.ConsensusStats) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.currentTerm = stats.Term
	mc.commitIndex = stats.CommitIndex
	mc.lastApplied = stats.LastApplied
	mc.logSize = stats.LogEntries
	mc.clusterSize = int64(stats.ClusterSize)
	mc.healthyNodes = int64(stats.HealthyNodes)
	mc.hasQuorum = stats.HasQuorum
	mc.currentRole = stats.Role
}

// runCollector runs the metrics collection loop.
func (mc *MetricsCollector) runCollector() {
	defer mc.wg.Done()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-mc.ctx.Done():
			return

		case <-ticker.C:
			mc.exportMetrics()
		}
	}
}

// exportMetrics exports all metrics to the metrics backend.
func (mc *MetricsCollector) exportMetrics() {
	if mc.metrics == nil {
		return
	}

	// Export counters
	// Note: Forge metrics interface uses map[string]any, so we'll log them for now
	// In a real implementation, you'd use the actual metrics interface methods

	mc.logger.Debug("consensus metrics",
		forge.F("elections", mc.electionCount.Load()),
		forge.F("election_failures", mc.electionFailures.Load()),
		forge.F("log_appends", mc.logAppends.Load()),
		forge.F("log_append_failures", mc.logAppendFailures.Load()),
		forge.F("snapshots", mc.snapshotCount.Load()),
		forge.F("snapshot_failures", mc.snapshotFailures.Load()),
		forge.F("config_changes", mc.configChanges.Load()),
		forge.F("leadership_transfers", mc.leadershipTransfers.Load()),
	)

	// Export gauges
	mc.mu.RLock()
	mc.logger.Debug("consensus state",
		forge.F("term", mc.currentTerm),
		forge.F("commit_index", mc.commitIndex),
		forge.F("last_applied", mc.lastApplied),
		forge.F("log_size", mc.logSize),
		forge.F("cluster_size", mc.clusterSize),
		forge.F("healthy_nodes", mc.healthyNodes),
		forge.F("has_quorum", mc.hasQuorum),
		forge.F("role", mc.currentRole),
	)
	mc.mu.RUnlock()

	// Export latency histograms
	mc.histogramMu.Lock()

	if len(mc.replicationLatencies) > 0 {
		avg := calculateAverage(mc.replicationLatencies)
		p95 := calculatePercentile(mc.replicationLatencies, 0.95)
		p99 := calculatePercentile(mc.replicationLatencies, 0.99)

		mc.logger.Debug("replication latency",
			forge.F("avg_ms", avg),
			forge.F("p95_ms", p95),
			forge.F("p99_ms", p99),
		)
	}

	if len(mc.applyLatencies) > 0 {
		avg := calculateAverage(mc.applyLatencies)
		p95 := calculatePercentile(mc.applyLatencies, 0.95)
		p99 := calculatePercentile(mc.applyLatencies, 0.99)

		mc.logger.Debug("apply latency",
			forge.F("avg_ms", avg),
			forge.F("p95_ms", p95),
			forge.F("p99_ms", p99),
		)
	}

	mc.histogramMu.Unlock()
}

// GetMetrics returns current metrics as a map.
func (mc *MetricsCollector) GetMetrics() map[string]any {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	metrics := map[string]any{
		// Counters
		"elections":            mc.electionCount.Load(),
		"election_failures":    mc.electionFailures.Load(),
		"log_appends":          mc.logAppends.Load(),
		"log_append_failures":  mc.logAppendFailures.Load(),
		"snapshots":            mc.snapshotCount.Load(),
		"snapshot_failures":    mc.snapshotFailures.Load(),
		"config_changes":       mc.configChanges.Load(),
		"leadership_transfers": mc.leadershipTransfers.Load(),

		// Gauges
		"current_term":  mc.currentTerm,
		"commit_index":  mc.commitIndex,
		"last_applied":  mc.lastApplied,
		"log_size":      mc.logSize,
		"cluster_size":  mc.clusterSize,
		"healthy_nodes": mc.healthyNodes,
		"has_quorum":    mc.hasQuorum,
		"current_role":  string(mc.currentRole),
	}

	// Add latency stats
	mc.histogramMu.Lock()

	if len(mc.replicationLatencies) > 0 {
		metrics["replication_latency_avg"] = calculateAverage(mc.replicationLatencies)
		metrics["replication_latency_p95"] = calculatePercentile(mc.replicationLatencies, 0.95)
		metrics["replication_latency_p99"] = calculatePercentile(mc.replicationLatencies, 0.99)
	}

	if len(mc.applyLatencies) > 0 {
		metrics["apply_latency_avg"] = calculateAverage(mc.applyLatencies)
		metrics["apply_latency_p95"] = calculatePercentile(mc.applyLatencies, 0.95)
		metrics["apply_latency_p99"] = calculatePercentile(mc.applyLatencies, 0.99)
	}

	mc.histogramMu.Unlock()

	return metrics
}

// Helper functions for statistics

func calculateAverage(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	sum := 0.0
	for _, v := range values {
		sum += v
	}

	return sum / float64(len(values))
}

func calculatePercentile(values []float64, percentile float64) float64 {
	if len(values) == 0 {
		return 0
	}

	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)

	index := int(float64(len(sorted)-1) * percentile)

	return sorted[index]
}
