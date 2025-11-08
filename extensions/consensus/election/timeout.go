package election

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"github.com/xraph/forge"
)

// TimeoutManager manages election timeout with adaptive algorithms.
type TimeoutManager struct {
	nodeID string
	logger forge.Logger

	// Timeout configuration
	minTimeout time.Duration
	maxTimeout time.Duration
	multiplier float64

	// Current timeout
	currentTimeout time.Duration
	timer          *time.Timer
	timerMu        sync.Mutex

	// Adaptive timeout
	adaptiveEnabled bool
	recentElections []time.Duration
	electionsMu     sync.RWMutex

	// Timeout history
	timeoutHistory []TimeoutEvent
	historyMu      sync.RWMutex
	maxHistorySize int

	// Callback
	onTimeout func()

	// Lifecycle
	started bool
	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.RWMutex
}

// TimeoutEvent represents a timeout event.
type TimeoutEvent struct {
	Timestamp time.Time
	Timeout   time.Duration
	Fired     bool
	Reset     bool
}

// TimeoutManagerConfig contains timeout manager configuration.
type TimeoutManagerConfig struct {
	NodeID          string
	MinTimeout      time.Duration
	MaxTimeout      time.Duration
	Multiplier      float64
	AdaptiveEnabled bool
	MaxHistorySize  int
	OnTimeout       func()
}

// NewTimeoutManager creates a new timeout manager.
func NewTimeoutManager(config TimeoutManagerConfig, logger forge.Logger) *TimeoutManager {
	if config.MinTimeout == 0 {
		config.MinTimeout = 150 * time.Millisecond
	}

	if config.MaxTimeout == 0 {
		config.MaxTimeout = 300 * time.Millisecond
	}

	if config.Multiplier == 0 {
		config.Multiplier = 1.5
	}

	if config.MaxHistorySize == 0 {
		config.MaxHistorySize = 100
	}

	return &TimeoutManager{
		nodeID:          config.NodeID,
		logger:          logger,
		minTimeout:      config.MinTimeout,
		maxTimeout:      config.MaxTimeout,
		multiplier:      config.Multiplier,
		adaptiveEnabled: config.AdaptiveEnabled,
		recentElections: make([]time.Duration, 0, 10),
		timeoutHistory:  make([]TimeoutEvent, 0, config.MaxHistorySize),
		maxHistorySize:  config.MaxHistorySize,
		onTimeout:       config.OnTimeout,
	}
}

// Start starts the timeout manager.
func (tm *TimeoutManager) Start(ctx context.Context) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.started {
		return nil
	}

	tm.ctx, tm.cancel = context.WithCancel(ctx)
	tm.started = true

	// Calculate initial timeout
	tm.currentTimeout = tm.randomTimeout()

	tm.logger.Info("timeout manager started",
		forge.F("node_id", tm.nodeID),
		forge.F("min_timeout", tm.minTimeout),
		forge.F("max_timeout", tm.maxTimeout),
		forge.F("adaptive", tm.adaptiveEnabled),
	)

	return nil
}

// Stop stops the timeout manager.
func (tm *TimeoutManager) Stop(ctx context.Context) error {
	tm.mu.Lock()

	if !tm.started {
		tm.mu.Unlock()

		return nil
	}

	tm.mu.Unlock()

	if tm.cancel != nil {
		tm.cancel()
	}

	tm.StopTimer()

	tm.logger.Info("timeout manager stopped")

	return nil
}

// StartTimer starts the election timeout timer.
func (tm *TimeoutManager) StartTimer() {
	tm.timerMu.Lock()
	defer tm.timerMu.Unlock()

	if tm.timer != nil {
		tm.timer.Stop()
	}

	timeout := tm.getCurrentTimeout()

	tm.timer = time.AfterFunc(timeout, func() {
		tm.handleTimeout()
	})

	// Record event
	tm.recordEvent(TimeoutEvent{
		Timestamp: time.Now(),
		Timeout:   timeout,
		Fired:     false,
		Reset:     false,
	})

	tm.logger.Debug("election timer started",
		forge.F("timeout", timeout),
	)
}

// StopTimer stops the election timeout timer.
func (tm *TimeoutManager) StopTimer() {
	tm.timerMu.Lock()
	defer tm.timerMu.Unlock()

	if tm.timer != nil {
		tm.timer.Stop()
		tm.timer = nil
	}
}

// ResetTimer resets the election timeout timer.
func (tm *TimeoutManager) ResetTimer() {
	tm.timerMu.Lock()
	defer tm.timerMu.Unlock()

	if tm.timer != nil {
		tm.timer.Stop()
	}

	timeout := tm.getCurrentTimeout()

	tm.timer = time.AfterFunc(timeout, func() {
		tm.handleTimeout()
	})

	// Record event
	tm.recordEvent(TimeoutEvent{
		Timestamp: time.Now(),
		Timeout:   timeout,
		Fired:     false,
		Reset:     true,
	})

	tm.logger.Debug("election timer reset",
		forge.F("timeout", timeout),
	)
}

// handleTimeout handles timeout expiration.
func (tm *TimeoutManager) handleTimeout() {
	tm.logger.Debug("election timeout fired",
		forge.F("node_id", tm.nodeID),
	)

	// Record fired event
	tm.recordEvent(TimeoutEvent{
		Timestamp: time.Now(),
		Timeout:   tm.currentTimeout,
		Fired:     true,
		Reset:     false,
	})

	// Call callback if set
	if tm.onTimeout != nil {
		tm.onTimeout()
	}
}

// getCurrentTimeout returns the current timeout value.
func (tm *TimeoutManager) getCurrentTimeout() time.Duration {
	if tm.adaptiveEnabled {
		return tm.calculateAdaptiveTimeout()
	}

	return tm.randomTimeout()
}

// randomTimeout generates a random timeout between min and max.
func (tm *TimeoutManager) randomTimeout() time.Duration {
	diff := tm.maxTimeout - tm.minTimeout
	random := time.Duration(rand.Int63n(int64(diff)))

	return tm.minTimeout + random
}

// calculateAdaptiveTimeout calculates an adaptive timeout based on recent elections.
func (tm *TimeoutManager) calculateAdaptiveTimeout() time.Duration {
	tm.electionsMu.RLock()
	defer tm.electionsMu.RUnlock()

	if len(tm.recentElections) == 0 {
		return tm.randomTimeout()
	}

	// Calculate average recent election duration
	var total time.Duration
	for _, duration := range tm.recentElections {
		total += duration
	}

	average := total / time.Duration(len(tm.recentElections))

	// Apply multiplier for safety margin
	adaptive := min(
		// Clamp to min/max bounds
		max(

			time.Duration(float64(average)*tm.multiplier), tm.minTimeout), tm.maxTimeout)

	// Add randomization (±10%)
	variance := time.Duration(float64(adaptive) * 0.1)
	random := time.Duration(rand.Int63n(int64(variance*2))) - variance
	adaptive += random

	tm.logger.Debug("calculated adaptive timeout",
		forge.F("average_election", average),
		forge.F("adaptive_timeout", adaptive),
		forge.F("samples", len(tm.recentElections)),
	)

	return adaptive
}

// RecordElectionDuration records an election duration for adaptive timeout.
func (tm *TimeoutManager) RecordElectionDuration(duration time.Duration) {
	if !tm.adaptiveEnabled {
		return
	}

	tm.electionsMu.Lock()
	defer tm.electionsMu.Unlock()

	tm.recentElections = append(tm.recentElections, duration)

	// Keep only recent samples (last 10)
	if len(tm.recentElections) > 10 {
		tm.recentElections = tm.recentElections[len(tm.recentElections)-10:]
	}

	tm.logger.Debug("recorded election duration",
		forge.F("duration", duration),
		forge.F("samples", len(tm.recentElections)),
	)
}

// GetCurrentTimeout returns the current timeout value.
func (tm *TimeoutManager) GetCurrentTimeout() time.Duration {
	return tm.currentTimeout
}

// SetTimeoutRange updates the timeout range.
func (tm *TimeoutManager) SetTimeoutRange(min, max time.Duration) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.minTimeout = min
	tm.maxTimeout = max

	tm.logger.Info("timeout range updated",
		forge.F("min", min),
		forge.F("max", max),
	)
}

// EnableAdaptive enables adaptive timeout.
func (tm *TimeoutManager) EnableAdaptive(enable bool) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.adaptiveEnabled = enable

	tm.logger.Info("adaptive timeout changed",
		forge.F("enabled", enable),
	)
}

// IsAdaptiveEnabled returns whether adaptive timeout is enabled.
func (tm *TimeoutManager) IsAdaptiveEnabled() bool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	return tm.adaptiveEnabled
}

// GetTimeoutStatistics returns timeout statistics.
func (tm *TimeoutManager) GetTimeoutStatistics() TimeoutStatistics {
	tm.historyMu.RLock()
	defer tm.historyMu.RUnlock()

	stats := TimeoutStatistics{
		TotalEvents:    len(tm.timeoutHistory),
		MinTimeout:     tm.minTimeout,
		MaxTimeout:     tm.maxTimeout,
		CurrentTimeout: tm.currentTimeout,
	}

	var totalTimeout time.Duration

	firedCount := 0
	resetCount := 0

	for _, event := range tm.timeoutHistory {
		totalTimeout += event.Timeout
		if event.Fired {
			firedCount++
		}

		if event.Reset {
			resetCount++
		}
	}

	if len(tm.timeoutHistory) > 0 {
		stats.AverageTimeout = totalTimeout / time.Duration(len(tm.timeoutHistory))
	}

	stats.TimeoutsFired = firedCount
	stats.ResetsCount = resetCount

	tm.electionsMu.RLock()

	if len(tm.recentElections) > 0 {
		var total time.Duration
		for _, d := range tm.recentElections {
			total += d
		}

		stats.AverageElectionDuration = total / time.Duration(len(tm.recentElections))
	}

	tm.electionsMu.RUnlock()

	return stats
}

// TimeoutStatistics contains timeout statistics.
type TimeoutStatistics struct {
	TotalEvents             int
	TimeoutsFired           int
	ResetsCount             int
	MinTimeout              time.Duration
	MaxTimeout              time.Duration
	CurrentTimeout          time.Duration
	AverageTimeout          time.Duration
	AverageElectionDuration time.Duration
}

// GetTimeoutHistory returns timeout history.
func (tm *TimeoutManager) GetTimeoutHistory(limit int) []TimeoutEvent {
	tm.historyMu.RLock()
	defer tm.historyMu.RUnlock()

	if limit <= 0 || limit > len(tm.timeoutHistory) {
		limit = len(tm.timeoutHistory)
	}

	start := len(tm.timeoutHistory) - limit
	result := make([]TimeoutEvent, limit)
	copy(result, tm.timeoutHistory[start:])

	return result
}

// recordEvent records a timeout event.
func (tm *TimeoutManager) recordEvent(event TimeoutEvent) {
	tm.historyMu.Lock()
	defer tm.historyMu.Unlock()

	tm.timeoutHistory = append(tm.timeoutHistory, event)

	// Trim history if needed
	if len(tm.timeoutHistory) > tm.maxHistorySize {
		tm.timeoutHistory = tm.timeoutHistory[len(tm.timeoutHistory)-tm.maxHistorySize:]
	}
}

// GetRecentElectionDurations returns recent election durations.
func (tm *TimeoutManager) GetRecentElectionDurations() []time.Duration {
	tm.electionsMu.RLock()
	defer tm.electionsMu.RUnlock()

	result := make([]time.Duration, len(tm.recentElections))
	copy(result, tm.recentElections)

	return result
}

// ClearHistory clears timeout history.
func (tm *TimeoutManager) ClearHistory() {
	tm.historyMu.Lock()
	defer tm.historyMu.Unlock()

	tm.timeoutHistory = tm.timeoutHistory[:0]

	tm.logger.Debug("cleared timeout history")
}

// GetTimeUntilTimeout returns time remaining until timeout.
func (tm *TimeoutManager) GetTimeUntilTimeout() time.Duration {
	tm.timerMu.Lock()
	defer tm.timerMu.Unlock()

	// This is an approximation since we can't get exact time from time.Timer
	// In production, you might want to track start time separately
	return tm.currentTimeout
}

// IsTimerActive returns whether the timer is active.
func (tm *TimeoutManager) IsTimerActive() bool {
	tm.timerMu.Lock()
	defer tm.timerMu.Unlock()

	return tm.timer != nil
}
