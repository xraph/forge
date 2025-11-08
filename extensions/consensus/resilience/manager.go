package resilience

import (
	"context"
	"fmt"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/resilience"
	"github.com/xraph/forge/internal/shared"
)

// Manager provides resilience features for consensus operations.
type Manager struct {
	// Retry strategies
	rpcRetry      *resilience.Retry
	commitRetry   *resilience.Retry
	snapshotRetry *resilience.Retry

	// Circuit breakers
	transportCB   *resilience.CircuitBreaker
	storageCB     *resilience.CircuitBreaker
	replicationCB *resilience.CircuitBreaker

	logger forge.Logger
}

// ManagerConfig contains resilience manager configuration.
type ManagerConfig struct {
	// Retry configuration
	MaxRPCAttempts      int
	MaxCommitAttempts   int
	MaxSnapshotAttempts int
	RetryInitialDelay   time.Duration
	RetryMaxDelay       time.Duration

	// Circuit breaker configuration
	MaxFailures         int
	FailureThreshold    float64
	RecoveryTimeout     time.Duration
	HalfOpenMaxRequests int
}

// NewManager creates a new resilience manager.
func NewManager(config ManagerConfig, forgeLogger forge.Logger, metrics shared.Metrics) *Manager {
	// Set defaults
	if config.MaxRPCAttempts == 0 {
		config.MaxRPCAttempts = 3
	}

	if config.MaxCommitAttempts == 0 {
		config.MaxCommitAttempts = 5
	}

	if config.MaxSnapshotAttempts == 0 {
		config.MaxSnapshotAttempts = 3
	}

	if config.RetryInitialDelay == 0 {
		config.RetryInitialDelay = 100 * time.Millisecond
	}

	if config.RetryMaxDelay == 0 {
		config.RetryMaxDelay = 30 * time.Second
	}

	if config.MaxFailures == 0 {
		config.MaxFailures = 5
	}

	if config.FailureThreshold == 0 {
		config.FailureThreshold = 0.5
	}

	if config.RecoveryTimeout == 0 {
		config.RecoveryTimeout = 30 * time.Second
	}

	if config.HalfOpenMaxRequests == 0 {
		config.HalfOpenMaxRequests = 3
	}

	// Use default internal logger (Forge's logger interface is not directly compatible)
	var internalLogger logger.Logger = nil

	// Create retry strategies
	rpcRetry := resilience.NewRetry(resilience.RetryConfig{
		Name:            "consensus-rpc",
		MaxAttempts:     config.MaxRPCAttempts,
		InitialDelay:    config.RetryInitialDelay,
		MaxDelay:        config.RetryMaxDelay,
		Multiplier:      2.0,
		Jitter:          true,
		BackoffStrategy: resilience.BackoffStrategyExponential,
		EnableMetrics:   true,
		Logger:          internalLogger,
		Metrics:         metrics,
	})

	commitRetry := resilience.NewRetry(resilience.RetryConfig{
		Name:            "consensus-commit",
		MaxAttempts:     config.MaxCommitAttempts,
		InitialDelay:    config.RetryInitialDelay,
		MaxDelay:        config.RetryMaxDelay,
		Multiplier:      2.0,
		Jitter:          true,
		BackoffStrategy: resilience.BackoffStrategyExponential,
		EnableMetrics:   true,
		Logger:          internalLogger,
		Metrics:         metrics,
	})

	snapshotRetry := resilience.NewRetry(resilience.RetryConfig{
		Name:            "consensus-snapshot",
		MaxAttempts:     config.MaxSnapshotAttempts,
		InitialDelay:    config.RetryInitialDelay,
		MaxDelay:        config.RetryMaxDelay,
		Multiplier:      2.0,
		Jitter:          true,
		BackoffStrategy: resilience.BackoffStrategyExponential,
		EnableMetrics:   true,
		Logger:          internalLogger,
		Metrics:         metrics,
	})

	// Create circuit breakers
	transportCB := resilience.NewCircuitBreaker(resilience.CircuitBreakerConfig{
		Name:                "consensus-transport",
		MaxFailures:         config.MaxFailures,
		FailureThreshold:    config.FailureThreshold,
		RecoveryTimeout:     config.RecoveryTimeout,
		HalfOpenMaxRequests: config.HalfOpenMaxRequests,
		EnableMetrics:       true,
		Logger:              internalLogger,
		Metrics:             metrics,
	})

	storageCB := resilience.NewCircuitBreaker(resilience.CircuitBreakerConfig{
		Name:                "consensus-storage",
		MaxFailures:         config.MaxFailures,
		FailureThreshold:    config.FailureThreshold,
		RecoveryTimeout:     config.RecoveryTimeout,
		HalfOpenMaxRequests: config.HalfOpenMaxRequests,
		EnableMetrics:       true,
		Logger:              internalLogger,
		Metrics:             metrics,
	})

	replicationCB := resilience.NewCircuitBreaker(resilience.CircuitBreakerConfig{
		Name:                "consensus-replication",
		MaxFailures:         config.MaxFailures,
		FailureThreshold:    config.FailureThreshold,
		RecoveryTimeout:     config.RecoveryTimeout,
		HalfOpenMaxRequests: config.HalfOpenMaxRequests,
		EnableMetrics:       true,
		Logger:              internalLogger,
		Metrics:             metrics,
	})

	return &Manager{
		rpcRetry:      rpcRetry,
		commitRetry:   commitRetry,
		snapshotRetry: snapshotRetry,
		transportCB:   transportCB,
		storageCB:     storageCB,
		replicationCB: replicationCB,
		logger:        forgeLogger,
	}
}

// ExecuteRPC executes an RPC with retry logic.
func (m *Manager) ExecuteRPC(ctx context.Context, fn func() error) error {
	_, err := m.rpcRetry.Execute(ctx, func() (any, error) {
		return nil, fn()
	})

	return err
}

// ExecuteRPCWithCircuitBreaker executes an RPC with circuit breaker and retry.
func (m *Manager) ExecuteRPCWithCircuitBreaker(ctx context.Context, fn func() error) error {
	_, err := m.transportCB.Execute(ctx, func() (any, error) {
		_, retryErr := m.rpcRetry.Execute(ctx, func() (any, error) {
			return nil, fn()
		})

		return nil, retryErr
	})

	return err
}

// ExecuteCommit executes a commit operation with retry logic.
func (m *Manager) ExecuteCommit(ctx context.Context, fn func() error) error {
	_, err := m.commitRetry.Execute(ctx, func() (any, error) {
		return nil, fn()
	})

	return err
}

// ExecuteStorage executes a storage operation with circuit breaker.
func (m *Manager) ExecuteStorage(ctx context.Context, fn func() error) error {
	_, err := m.storageCB.Execute(ctx, func() (any, error) {
		return nil, fn()
	})

	return err
}

// ExecuteReplication executes a replication operation with circuit breaker.
func (m *Manager) ExecuteReplication(ctx context.Context, fn func() error) error {
	_, err := m.replicationCB.Execute(ctx, func() (any, error) {
		return nil, fn()
	})

	return err
}

// ExecuteSnapshot executes a snapshot operation with retry logic.
func (m *Manager) ExecuteSnapshot(ctx context.Context, fn func() error) error {
	_, err := m.snapshotRetry.Execute(ctx, func() (any, error) {
		return nil, fn()
	})

	return err
}

// IsRetryableError checks if an error is retryable.
func (m *Manager) IsRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Check for specific consensus errors that are retryable
	if internal.IsNoLeaderError(err) {
		return true
	}

	if internal.IsNoQuorumError(err) {
		return true
	}

	if internal.IsStaleTermError(err) {
		return true
	}

	if internal.IsRetryable(err) {
		return true
	}

	return false
}

// GetStats returns resilience statistics.
func (m *Manager) GetStats() map[string]any {
	stats := make(map[string]any)

	stats["retry"] = map[string]any{
		"rpc":      m.rpcRetry.GetStats(),
		"commit":   m.commitRetry.GetStats(),
		"snapshot": m.snapshotRetry.GetStats(),
	}

	stats["circuit_breaker"] = map[string]any{
		"transport":   m.getCircuitBreakerStats(m.transportCB),
		"storage":     m.getCircuitBreakerStats(m.storageCB),
		"replication": m.getCircuitBreakerStats(m.replicationCB),
	}

	return stats
}

// getCircuitBreakerStats gets circuit breaker stats.
func (m *Manager) getCircuitBreakerStats(cb *resilience.CircuitBreaker) map[string]any {
	stats := cb.GetStats()

	return map[string]any{
		"state":             cb.GetState(),
		"total_requests":    stats.TotalRequests,
		"successful":        stats.SuccessfulRequests,
		"failed":            stats.FailedRequests,
		"state_changes":     stats.StateChanges,
		"last_state_change": stats.LastStateChange,
	}
}

// Reset resets all resilience components.
func (m *Manager) Reset() {
	m.rpcRetry.Reset()
	m.commitRetry.Reset()
	m.snapshotRetry.Reset()
	m.transportCB.Reset()
	m.storageCB.Reset()
	m.replicationCB.Reset()

	m.logger.Info("resilience manager reset")
}

// Helper function to check error message patterns.
func containsErrorPattern(err error, patterns []string) bool {
	if err == nil {
		return false
	}

	errMsg := err.Error()
	for _, pattern := range patterns {
		if len(errMsg) >= len(pattern) && errMsg[:len(pattern)] == pattern {
			return true
		}
	}

	return false
}

// ExecuteWithTimeout executes a function with timeout.
func (m *Manager) ExecuteWithTimeout(ctx context.Context, timeout time.Duration, fn func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resultCh := make(chan error, 1)

	go func() {
		resultCh <- fn(ctx)
	}()

	select {
	case err := <-resultCh:
		return err
	case <-ctx.Done():
		return fmt.Errorf("operation timed out after %v: %w", timeout, ctx.Err())
	}
}
