package resilience

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// TimeoutManager manages operation timeouts
type TimeoutManager struct {
	nodeID string
	logger forge.Logger

	// Timeout configurations
	rpcTimeout         time.Duration
	replicationTimeout time.Duration
	snapshotTimeout    time.Duration
	electionTimeout    time.Duration

	// Adaptive timeouts
	adaptiveEnabled  bool
	rpcStats         *OperationStats
	replicationStats *OperationStats

	mu sync.RWMutex
}

// OperationStats tracks operation timing statistics
type OperationStats struct {
	samples    []time.Duration
	maxSamples int
	mu         sync.RWMutex
}

// TimeoutManagerConfig contains timeout manager configuration
type TimeoutManagerConfig struct {
	NodeID string

	// Timeout values
	RPCTimeout         time.Duration
	ReplicationTimeout time.Duration
	SnapshotTimeout    time.Duration
	ElectionTimeout    time.Duration

	// Adaptive configuration
	AdaptiveEnabled bool
	MaxSamples      int
}

// NewTimeoutManager creates a new timeout manager
func NewTimeoutManager(config TimeoutManagerConfig, logger forge.Logger) *TimeoutManager {
	// Set defaults
	if config.RPCTimeout == 0 {
		config.RPCTimeout = 1 * time.Second
	}
	if config.ReplicationTimeout == 0 {
		config.ReplicationTimeout = 500 * time.Millisecond
	}
	if config.SnapshotTimeout == 0 {
		config.SnapshotTimeout = 30 * time.Second
	}
	if config.ElectionTimeout == 0 {
		config.ElectionTimeout = 300 * time.Millisecond
	}
	if config.MaxSamples == 0 {
		config.MaxSamples = 100
	}

	return &TimeoutManager{
		nodeID:             config.NodeID,
		logger:             logger,
		rpcTimeout:         config.RPCTimeout,
		replicationTimeout: config.ReplicationTimeout,
		snapshotTimeout:    config.SnapshotTimeout,
		electionTimeout:    config.ElectionTimeout,
		adaptiveEnabled:    config.AdaptiveEnabled,
		rpcStats:           newOperationStats(config.MaxSamples),
		replicationStats:   newOperationStats(config.MaxSamples),
	}
}

// newOperationStats creates new operation stats
func newOperationStats(maxSamples int) *OperationStats {
	return &OperationStats{
		samples:    make([]time.Duration, 0, maxSamples),
		maxSamples: maxSamples,
	}
}

// WithRPCTimeout executes operation with RPC timeout
func (tm *TimeoutManager) WithRPCTimeout(ctx context.Context, fn func(context.Context) error) error {
	timeout := tm.getRPCTimeout()
	return tm.executeWithTimeout(ctx, "rpc", timeout, fn, tm.rpcStats)
}

// WithReplicationTimeout executes operation with replication timeout
func (tm *TimeoutManager) WithReplicationTimeout(ctx context.Context, fn func(context.Context) error) error {
	timeout := tm.getReplicationTimeout()
	return tm.executeWithTimeout(ctx, "replication", timeout, fn, tm.replicationStats)
}

// WithSnapshotTimeout executes operation with snapshot timeout
func (tm *TimeoutManager) WithSnapshotTimeout(ctx context.Context, fn func(context.Context) error) error {
	timeout := tm.getSnapshotTimeout()
	return tm.executeWithTimeout(ctx, "snapshot", timeout, fn, nil)
}

// WithCustomTimeout executes operation with custom timeout
func (tm *TimeoutManager) WithCustomTimeout(
	ctx context.Context,
	timeout time.Duration,
	fn func(context.Context) error,
) error {
	return tm.executeWithTimeout(ctx, "custom", timeout, fn, nil)
}

// executeWithTimeout executes a function with timeout
func (tm *TimeoutManager) executeWithTimeout(
	ctx context.Context,
	operation string,
	timeout time.Duration,
	fn func(context.Context) error,
	stats *OperationStats,
) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	errChan := make(chan error, 1)

	go func() {
		errChan <- fn(timeoutCtx)
	}()

	select {
	case err := <-errChan:
		duration := time.Since(start)

		// Record successful duration for adaptive timeout
		if err == nil && stats != nil {
			stats.recordDuration(duration)
		}

		return err

	case <-timeoutCtx.Done():
		duration := time.Since(start)

		tm.logger.Warn("operation timeout",
			forge.F("operation", operation),
			forge.F("timeout", timeout),
			forge.F("elapsed", duration),
		)

		return internal.ErrTimeout.WithContext("operation", operation)
	}
}

// getRPCTimeout gets RPC timeout (adaptive if enabled)
func (tm *TimeoutManager) getRPCTimeout() time.Duration {
	if !tm.adaptiveEnabled {
		return tm.rpcTimeout
	}

	adaptive := tm.rpcStats.getAdaptiveTimeout(tm.rpcTimeout, 2.0)
	return adaptive
}

// getReplicationTimeout gets replication timeout (adaptive if enabled)
func (tm *TimeoutManager) getReplicationTimeout() time.Duration {
	if !tm.adaptiveEnabled {
		return tm.replicationTimeout
	}

	adaptive := tm.replicationStats.getAdaptiveTimeout(tm.replicationTimeout, 2.0)
	return adaptive
}

// getSnapshotTimeout gets snapshot timeout
func (tm *TimeoutManager) getSnapshotTimeout() time.Duration {
	return tm.snapshotTimeout
}

// GetTimeouts returns current timeout values
func (tm *TimeoutManager) GetTimeouts() map[string]time.Duration {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	return map[string]time.Duration{
		"rpc":         tm.getRPCTimeout(),
		"replication": tm.getReplicationTimeout(),
		"snapshot":    tm.snapshotTimeout,
		"election":    tm.electionTimeout,
	}
}

// SetTimeout sets a timeout value
func (tm *TimeoutManager) SetTimeout(operation string, timeout time.Duration) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	switch operation {
	case "rpc":
		tm.rpcTimeout = timeout
	case "replication":
		tm.replicationTimeout = timeout
	case "snapshot":
		tm.snapshotTimeout = timeout
	case "election":
		tm.electionTimeout = timeout
	default:
		return fmt.Errorf("unknown operation: %s", operation)
	}

	tm.logger.Info("timeout updated",
		forge.F("operation", operation),
		forge.F("timeout", timeout),
	)

	return nil
}

// EnableAdaptive enables or disables adaptive timeouts
func (tm *TimeoutManager) EnableAdaptive(enable bool) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.adaptiveEnabled = enable

	tm.logger.Info("adaptive timeouts changed",
		forge.F("enabled", enable),
	)
}

// IsAdaptiveEnabled returns whether adaptive timeouts are enabled
func (tm *TimeoutManager) IsAdaptiveEnabled() bool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.adaptiveEnabled
}

// GetStatistics returns timeout statistics
func (tm *TimeoutManager) GetStatistics() TimeoutStatistics {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	return TimeoutStatistics{
		RPCTimeout:         tm.rpcTimeout,
		ReplicationTimeout: tm.replicationTimeout,
		SnapshotTimeout:    tm.snapshotTimeout,
		ElectionTimeout:    tm.electionTimeout,
		AdaptiveEnabled:    tm.adaptiveEnabled,
		RPCAverage:         tm.rpcStats.getAverage(),
		ReplicationAverage: tm.replicationStats.getAverage(),
		RPCP99:             tm.rpcStats.getPercentile(99),
		ReplicationP99:     tm.replicationStats.getPercentile(99),
	}
}

// TimeoutStatistics contains timeout statistics
type TimeoutStatistics struct {
	RPCTimeout         time.Duration
	ReplicationTimeout time.Duration
	SnapshotTimeout    time.Duration
	ElectionTimeout    time.Duration
	AdaptiveEnabled    bool
	RPCAverage         time.Duration
	ReplicationAverage time.Duration
	RPCP99             time.Duration
	ReplicationP99     time.Duration
}

// recordDuration records an operation duration
func (os *OperationStats) recordDuration(duration time.Duration) {
	os.mu.Lock()
	defer os.mu.Unlock()

	os.samples = append(os.samples, duration)

	// Keep only recent samples
	if len(os.samples) > os.maxSamples {
		os.samples = os.samples[len(os.samples)-os.maxSamples:]
	}
}

// getAverage calculates average duration
func (os *OperationStats) getAverage() time.Duration {
	os.mu.RLock()
	defer os.mu.RUnlock()

	if len(os.samples) == 0 {
		return 0
	}

	var total time.Duration
	for _, d := range os.samples {
		total += d
	}

	return total / time.Duration(len(os.samples))
}

// getPercentile calculates percentile duration
func (os *OperationStats) getPercentile(percentile int) time.Duration {
	os.mu.RLock()
	defer os.mu.RUnlock()

	if len(os.samples) == 0 {
		return 0
	}

	// Sort samples (simple bubble sort for small samples)
	sorted := make([]time.Duration, len(os.samples))
	copy(sorted, os.samples)

	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[i] > sorted[j] {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	// Calculate percentile index
	index := (percentile * len(sorted)) / 100
	if index >= len(sorted) {
		index = len(sorted) - 1
	}

	return sorted[index]
}

// getAdaptiveTimeout calculates adaptive timeout
func (os *OperationStats) getAdaptiveTimeout(baseTimeout time.Duration, multiplier float64) time.Duration {
	avg := os.getAverage()

	if avg == 0 {
		return baseTimeout
	}

	// Use average × multiplier, but not less than base timeout
	adaptive := time.Duration(float64(avg) * multiplier)

	if adaptive < baseTimeout {
		return baseTimeout
	}

	// Cap at 10x base timeout to prevent runaway
	maxTimeout := baseTimeout * 10
	if adaptive > maxTimeout {
		return maxTimeout
	}

	return adaptive
}

// ResetStatistics resets operation statistics
func (tm *TimeoutManager) ResetStatistics() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.rpcStats = newOperationStats(tm.rpcStats.maxSamples)
	tm.replicationStats = newOperationStats(tm.replicationStats.maxSamples)

	tm.logger.Debug("timeout statistics reset")
}
