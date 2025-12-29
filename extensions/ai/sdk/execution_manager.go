package sdk

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/xraph/forge"
)

// ExecutionState represents the state of a stream execution.
type ExecutionState string

const (
	ExecutionStatePending   ExecutionState = "pending"
	ExecutionStateRunning   ExecutionState = "running"
	ExecutionStateCompleted ExecutionState = "completed"
	ExecutionStateCancelled ExecutionState = "cancelled"
	ExecutionStateError     ExecutionState = "error"
)

// ExecutionInfo contains information about an active stream execution.
type ExecutionInfo struct {
	// ExecutionID uniquely identifies this execution
	ExecutionID string `json:"executionId"`

	// State is the current execution state
	State ExecutionState `json:"state"`

	// Model being used
	Model string `json:"model,omitempty"`

	// Provider being used
	Provider string `json:"provider,omitempty"`

	// StartTime when the execution started
	StartTime time.Time `json:"startTime"`

	// EndTime when the execution ended (zero if still running)
	EndTime time.Time `json:"endTime,omitempty"`

	// TokensGenerated counts tokens generated so far
	TokensGenerated int64 `json:"tokensGenerated"`

	// LastActivity timestamp of last event
	LastActivity time.Time `json:"lastActivity"`

	// Error message if state is error
	Error string `json:"error,omitempty"`

	// Metadata for custom data
	Metadata map[string]any `json:"metadata,omitempty"`

	// cancel function for cancellation
	cancel context.CancelFunc
	ctx    context.Context
}

// ExecutionManager tracks and manages active stream executions.
// It provides:
// - Active stream tracking by executionId
// - Cancellation support
// - Cleanup of stale executions
// - Metrics and observability
type ExecutionManager struct {
	// executions maps executionId to ExecutionInfo
	executions map[string]*ExecutionInfo

	// mu protects the executions map
	mu sync.RWMutex

	// logger for logging
	logger forge.Logger

	// metrics for observability
	metrics forge.Metrics

	// config for the manager
	config ExecutionManagerConfig

	// cleanupTicker for periodic cleanup
	cleanupTicker *time.Ticker
	cleanupDone   chan struct{}
}

// ExecutionManagerConfig configures the execution manager.
type ExecutionManagerConfig struct {
	// MaxActiveExecutions is the maximum number of concurrent executions
	// 0 means unlimited
	MaxActiveExecutions int

	// ExecutionTimeout is the maximum duration for a single execution
	// 0 means no timeout (not recommended)
	ExecutionTimeout time.Duration

	// StaleExecutionAge is the age after which completed executions are cleaned up
	StaleExecutionAge time.Duration

	// CleanupInterval is how often to run cleanup
	CleanupInterval time.Duration
}

// DefaultExecutionManagerConfig returns sensible defaults.
func DefaultExecutionManagerConfig() ExecutionManagerConfig {
	return ExecutionManagerConfig{
		MaxActiveExecutions: 100,
		ExecutionTimeout:    5 * time.Minute,
		StaleExecutionAge:   10 * time.Minute,
		CleanupInterval:     1 * time.Minute,
	}
}

// NewExecutionManager creates a new execution manager.
func NewExecutionManager(logger forge.Logger, metrics forge.Metrics, config ExecutionManagerConfig) *ExecutionManager {
	em := &ExecutionManager{
		executions:  make(map[string]*ExecutionInfo),
		logger:      logger,
		metrics:     metrics,
		config:      config,
		cleanupDone: make(chan struct{}),
	}

	// Start cleanup goroutine
	if config.CleanupInterval > 0 {
		em.cleanupTicker = time.NewTicker(config.CleanupInterval)
		go em.cleanupLoop()
	}

	return em
}

// StartExecution creates and tracks a new execution.
// Returns the execution info and a context that will be cancelled when the execution
// is cancelled or times out.
func (em *ExecutionManager) StartExecution(ctx context.Context, model, provider string) (*ExecutionInfo, context.Context, error) {
	em.mu.Lock()
	defer em.mu.Unlock()

	// Check max executions
	if em.config.MaxActiveExecutions > 0 {
		activeCount := 0
		for _, info := range em.executions {
			if info.State == ExecutionStateRunning {
				activeCount++
			}
		}
		if activeCount >= em.config.MaxActiveExecutions {
			if em.metrics != nil {
				em.metrics.Counter("forge.ai.sdk.execution.rejected", "reason", "max_executions").Inc()
			}
			return nil, nil, ErrMaxExecutionsReached{}
		}
	}

	// Create execution context with timeout
	execCtx := ctx
	var cancel context.CancelFunc
	if em.config.ExecutionTimeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, em.config.ExecutionTimeout)
	} else {
		execCtx, cancel = context.WithCancel(ctx)
	}

	// Create execution info
	execID := uuid.New().String()
	now := time.Now()

	info := &ExecutionInfo{
		ExecutionID:  execID,
		State:        ExecutionStateRunning,
		Model:        model,
		Provider:     provider,
		StartTime:    now,
		LastActivity: now,
		Metadata:     make(map[string]any),
		cancel:       cancel,
		ctx:          execCtx,
	}

	em.executions[execID] = info

	if em.logger != nil {
		em.logger.Debug("Execution started",
			forge.F("execution_id", execID),
			forge.F("model", model),
			forge.F("provider", provider),
		)
	}

	if em.metrics != nil {
		em.metrics.Counter("forge.ai.sdk.execution.started").Inc()
		em.metrics.Gauge("forge.ai.sdk.execution.active").Inc()
	}

	return info, execCtx, nil
}

// GetExecution returns the execution info for the given ID.
func (em *ExecutionManager) GetExecution(executionID string) (*ExecutionInfo, bool) {
	em.mu.RLock()
	defer em.mu.RUnlock()

	info, exists := em.executions[executionID]
	return info, exists
}

// UpdateActivity updates the last activity time for an execution.
func (em *ExecutionManager) UpdateActivity(executionID string, tokensGenerated int64) {
	em.mu.Lock()
	defer em.mu.Unlock()

	if info, exists := em.executions[executionID]; exists {
		info.LastActivity = time.Now()
		info.TokensGenerated = tokensGenerated
	}
}

// CompleteExecution marks an execution as completed.
func (em *ExecutionManager) CompleteExecution(executionID string, err error) {
	em.mu.Lock()
	defer em.mu.Unlock()

	info, exists := em.executions[executionID]
	if !exists {
		return
	}

	info.EndTime = time.Now()

	if err != nil {
		info.State = ExecutionStateError
		info.Error = err.Error()
	} else {
		info.State = ExecutionStateCompleted
	}

	if em.logger != nil {
		em.logger.Debug("Execution completed",
			forge.F("execution_id", executionID),
			forge.F("state", info.State),
			forge.F("duration", info.EndTime.Sub(info.StartTime)),
			forge.F("tokens", info.TokensGenerated),
		)
	}

	if em.metrics != nil {
		em.metrics.Counter("forge.ai.sdk.execution.completed", "state", string(info.State)).Inc()
		em.metrics.Gauge("forge.ai.sdk.execution.active").Dec()
		em.metrics.Histogram("forge.ai.sdk.execution.duration").Observe(info.EndTime.Sub(info.StartTime).Seconds())
	}
}

// CancelExecution cancels an execution by ID.
func (em *ExecutionManager) CancelExecution(executionID string) bool {
	em.mu.Lock()
	defer em.mu.Unlock()

	info, exists := em.executions[executionID]
	if !exists {
		return false
	}

	if info.State != ExecutionStateRunning {
		return false
	}

	// Call cancel function
	if info.cancel != nil {
		info.cancel()
	}

	info.State = ExecutionStateCancelled
	info.EndTime = time.Now()

	if em.logger != nil {
		em.logger.Info("Execution cancelled",
			forge.F("execution_id", executionID),
		)
	}

	if em.metrics != nil {
		em.metrics.Counter("forge.ai.sdk.execution.cancelled").Inc()
		em.metrics.Gauge("forge.ai.sdk.execution.active").Dec()
	}

	return true
}

// ListExecutions returns all executions, optionally filtered by state.
func (em *ExecutionManager) ListExecutions(state *ExecutionState) []*ExecutionInfo {
	em.mu.RLock()
	defer em.mu.RUnlock()

	results := make([]*ExecutionInfo, 0)
	for _, info := range em.executions {
		if state == nil || info.State == *state {
			results = append(results, info)
		}
	}

	return results
}

// ActiveCount returns the number of active executions.
func (em *ExecutionManager) ActiveCount() int {
	em.mu.RLock()
	defer em.mu.RUnlock()

	count := 0
	for _, info := range em.executions {
		if info.State == ExecutionStateRunning {
			count++
		}
	}

	return count
}

// cleanupLoop periodically cleans up stale executions.
func (em *ExecutionManager) cleanupLoop() {
	for {
		select {
		case <-em.cleanupTicker.C:
			em.cleanup()
		case <-em.cleanupDone:
			return
		}
	}
}

// cleanup removes stale completed executions.
func (em *ExecutionManager) cleanup() {
	em.mu.Lock()
	defer em.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-em.config.StaleExecutionAge)
	toDelete := make([]string, 0)

	for id, info := range em.executions {
		// Skip running executions
		if info.State == ExecutionStateRunning {
			continue
		}

		// Check if stale
		if !info.EndTime.IsZero() && info.EndTime.Before(cutoff) {
			toDelete = append(toDelete, id)
		}
	}

	for _, id := range toDelete {
		delete(em.executions, id)
	}

	if len(toDelete) > 0 && em.logger != nil {
		em.logger.Debug("Cleaned up stale executions",
			forge.F("count", len(toDelete)),
		)
	}
}

// Stop stops the execution manager and cancels all active executions.
func (em *ExecutionManager) Stop() {
	// Stop cleanup goroutine
	if em.cleanupTicker != nil {
		em.cleanupTicker.Stop()
		close(em.cleanupDone)
	}

	// Cancel all active executions
	em.mu.Lock()
	defer em.mu.Unlock()

	for _, info := range em.executions {
		if info.State == ExecutionStateRunning && info.cancel != nil {
			info.cancel()
			info.State = ExecutionStateCancelled
			info.EndTime = time.Now()
		}
	}

	if em.logger != nil {
		em.logger.Info("Execution manager stopped")
	}
}

// ErrMaxExecutionsReached is returned when the maximum number of concurrent
// executions has been reached.
type ErrMaxExecutionsReached struct{}

func (e ErrMaxExecutionsReached) Error() string {
	return "maximum number of concurrent executions reached"
}

var _ error = ErrMaxExecutionsReached{}
