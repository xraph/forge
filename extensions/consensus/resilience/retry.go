package resilience

import (
	"context"
	"fmt"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
	"github.com/xraph/forge/internal/resilience"
)

// RetryManager manages retry logic for consensus operations.
type RetryManager struct {
	nodeID string
	logger forge.Logger

	// Retry components from Forge
	rpcRetry      *resilience.Retry
	replicaRetry  *resilience.Retry
	snapshotRetry *resilience.Retry

	// Retry statistics
	stats     RetryStatistics
	statsChan chan RetryEvent
}

// RetryStatistics contains retry statistics.
type RetryStatistics struct {
	RPCRetries         int64
	ReplicationRetries int64
	SnapshotRetries    int64
	TotalRetries       int64
	SuccessfulRetries  int64
	FailedRetries      int64
}

// RetryEvent represents a retry event.
type RetryEvent struct {
	Operation string
	Attempt   int
	Success   bool
	Error     error
	Duration  time.Duration
	Timestamp time.Time
}

// RetryManagerConfig contains retry manager configuration.
type RetryManagerConfig struct {
	NodeID string

	// RPC retry configuration
	RPCMaxRetries    int
	RPCRetryDelay    time.Duration
	RPCMaxRetryDelay time.Duration

	// Replication retry configuration
	ReplicaMaxRetries    int
	ReplicaRetryDelay    time.Duration
	ReplicaMaxRetryDelay time.Duration

	// Snapshot retry configuration
	SnapshotMaxRetries    int
	SnapshotRetryDelay    time.Duration
	SnapshotMaxRetryDelay time.Duration
}

// NewRetryManager creates a new retry manager.
func NewRetryManager(config RetryManagerConfig, logger forge.Logger) *RetryManager {
	// Set defaults
	if config.RPCMaxRetries == 0 {
		config.RPCMaxRetries = 3
	}

	if config.RPCRetryDelay == 0 {
		config.RPCRetryDelay = 100 * time.Millisecond
	}

	if config.RPCMaxRetryDelay == 0 {
		config.RPCMaxRetryDelay = 1 * time.Second
	}

	if config.ReplicaMaxRetries == 0 {
		config.ReplicaMaxRetries = 5
	}

	if config.ReplicaRetryDelay == 0 {
		config.ReplicaRetryDelay = 50 * time.Millisecond
	}

	if config.ReplicaMaxRetryDelay == 0 {
		config.ReplicaMaxRetryDelay = 500 * time.Millisecond
	}

	if config.SnapshotMaxRetries == 0 {
		config.SnapshotMaxRetries = 3
	}

	if config.SnapshotRetryDelay == 0 {
		config.SnapshotRetryDelay = 1 * time.Second
	}

	if config.SnapshotMaxRetryDelay == 0 {
		config.SnapshotMaxRetryDelay = 5 * time.Second
	}

	// Create retry components
	rpcRetry := resilience.NewRetry(resilience.RetryConfig{
		Name:            "rpc-retry",
		MaxAttempts:     config.RPCMaxRetries,
		InitialDelay:    config.RPCRetryDelay,
		MaxDelay:        config.RPCMaxRetryDelay,
		BackoffStrategy: resilience.BackoffStrategyExponential,
	})

	replicaRetry := resilience.NewRetry(resilience.RetryConfig{
		Name:            "replica-retry",
		MaxAttempts:     config.ReplicaMaxRetries,
		InitialDelay:    config.ReplicaRetryDelay,
		MaxDelay:        config.ReplicaMaxRetryDelay,
		BackoffStrategy: resilience.BackoffStrategyExponential,
	})

	snapshotRetry := resilience.NewRetry(resilience.RetryConfig{
		Name:            "snapshot-retry",
		MaxAttempts:     config.SnapshotMaxRetries,
		InitialDelay:    config.SnapshotRetryDelay,
		MaxDelay:        config.SnapshotMaxRetryDelay,
		BackoffStrategy: resilience.BackoffStrategyExponential,
	})

	rm := &RetryManager{
		nodeID:        config.NodeID,
		logger:        logger,
		rpcRetry:      rpcRetry,
		replicaRetry:  replicaRetry,
		snapshotRetry: snapshotRetry,
		statsChan:     make(chan RetryEvent, 100),
	}

	// Start statistics collector
	go rm.collectStatistics()

	return rm
}

// RetryRPC retries an RPC operation.
func (rm *RetryManager) RetryRPC(ctx context.Context, operation string, fn func() error) error {
	start := time.Now()
	attempt := 0

	result, err := rm.rpcRetry.Execute(ctx, func() (any, error) {
		attempt++
		err := fn()

		return nil, err
	})

	duration := time.Since(start)

	// Record event
	rm.statsChan <- RetryEvent{
		Operation: "rpc:" + operation,
		Attempt:   attempt,
		Success:   err == nil,
		Error:     err,
		Duration:  duration,
		Timestamp: time.Now(),
	}

	if err != nil {
		rm.logger.Warn("rpc retry failed",
			forge.F("operation", operation),
			forge.F("attempts", attempt),
			forge.F("error", err),
		)

		return internal.ErrOperationFailed.WithContext("operation", operation)
	}

	_ = result // Unused but needed for Execute signature

	return nil
}

// RetryReplication retries a replication operation.
func (rm *RetryManager) RetryReplication(ctx context.Context, nodeID string, fn func() error) error {
	start := time.Now()
	attempt := 0

	result, err := rm.replicaRetry.Execute(ctx, func() (any, error) {
		attempt++
		err := fn()

		return nil, err
	})

	duration := time.Since(start)

	// Record event
	rm.statsChan <- RetryEvent{
		Operation: "replication:" + nodeID,
		Attempt:   attempt,
		Success:   err == nil,
		Error:     err,
		Duration:  duration,
		Timestamp: time.Now(),
	}

	if err != nil {
		rm.logger.Warn("replication retry failed",
			forge.F("node", nodeID),
			forge.F("attempts", attempt),
			forge.F("error", err),
		)

		return internal.ErrReplicationFailed.WithContext("node_id", nodeID)
	}

	_ = result // Unused but needed for Execute signature

	return nil
}

// RetrySnapshot retries a snapshot operation.
func (rm *RetryManager) RetrySnapshot(ctx context.Context, operation string, fn func() error) error {
	start := time.Now()
	attempt := 0

	result, err := rm.snapshotRetry.Execute(ctx, func() (any, error) {
		attempt++
		err := fn()

		return nil, err
	})

	duration := time.Since(start)

	// Record event
	rm.statsChan <- RetryEvent{
		Operation: "snapshot:" + operation,
		Attempt:   attempt,
		Success:   err == nil,
		Error:     err,
		Duration:  duration,
		Timestamp: time.Now(),
	}

	if err != nil {
		rm.logger.Warn("snapshot retry failed",
			forge.F("operation", operation),
			forge.F("attempts", attempt),
			forge.F("error", err),
		)

		return internal.ErrSnapshotFailed.WithContext("operation", operation)
	}

	_ = result // Unused but needed for Execute signature

	return nil
}

// collectStatistics collects retry statistics.
func (rm *RetryManager) collectStatistics() {
	for event := range rm.statsChan {
		rm.stats.TotalRetries++

		if event.Success {
			rm.stats.SuccessfulRetries++
		} else {
			rm.stats.FailedRetries++
		}

		// Categorize by operation type
		if len(event.Operation) > 4 && event.Operation[:3] == "rpc" {
			rm.stats.RPCRetries++
		} else if len(event.Operation) > 11 && event.Operation[:11] == "replication" {
			rm.stats.ReplicationRetries++
		} else if len(event.Operation) > 8 && event.Operation[:8] == "snapshot" {
			rm.stats.SnapshotRetries++
		}
	}
}

// GetStatistics returns retry statistics.
func (rm *RetryManager) GetStatistics() RetryStatistics {
	return rm.stats
}

// Close closes the retry manager.
func (rm *RetryManager) Close() {
	close(rm.statsChan)
}

// IsRetryable determines if an error is retryable.
func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Check for retryable consensus errors
	switch {
	case internal.Is(err, internal.ErrNotLeader):
		return false // Don't retry, redirect instead
	case internal.Is(err, internal.ErrTimeout):
		return true
	case internal.Is(err, internal.ErrNodeNotFound):
		return false
	case internal.Is(err, internal.ErrInvalidTerm):
		return false
	case internal.Is(err, internal.ErrStaleRead):
		return true
	case internal.Is(err, internal.ErrReplicationFailed):
		return true
	case internal.Is(err, internal.ErrSnapshotFailed):
		return true
	default:
		return false
	}
}

// RetryWithBackoff retries with custom backoff.
func (rm *RetryManager) RetryWithBackoff(
	ctx context.Context,
	operation string,
	fn func() error,
	backoff BackoffStrategy,
) error {
	var lastErr error

	attempt := 0

	for {
		attempt++
		start := time.Now()

		err := fn()
		duration := time.Since(start)

		if err == nil {
			// Success
			rm.statsChan <- RetryEvent{
				Operation: operation,
				Attempt:   attempt,
				Success:   true,
				Duration:  duration,
				Timestamp: time.Now(),
			}

			return nil
		}

		lastErr = err

		// Check if retryable
		if !IsRetryableError(err) {
			rm.statsChan <- RetryEvent{
				Operation: operation,
				Attempt:   attempt,
				Success:   false,
				Error:     err,
				Duration:  duration,
				Timestamp: time.Now(),
			}

			return err
		}

		// Check context
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Calculate delay
		delay := backoff.NextDelay(attempt)
		if delay == 0 {
			// Max attempts reached
			break
		}

		// Wait
		select {
		case <-time.After(delay):
			continue
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	rm.statsChan <- RetryEvent{
		Operation: operation,
		Attempt:   attempt,
		Success:   false,
		Error:     lastErr,
		Timestamp: time.Now(),
	}

	return lastErr
}
