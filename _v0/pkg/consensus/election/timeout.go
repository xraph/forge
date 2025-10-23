package election

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
)

// TimeoutManager manages election timeouts
type TimeoutManager struct {
	minTimeout     time.Duration
	maxTimeout     time.Duration
	currentTimeout time.Duration
	randomizer     *rand.Rand
	logger         common.Logger
	metrics        common.Metrics
	mu             sync.RWMutex

	// Timer and state
	timer        *time.Timer
	active       bool
	resetCount   int64
	timeoutCount int64
	lastReset    time.Time
	callbacks    []TimeoutCallback
	callbackMu   sync.RWMutex
}

// TimeoutCallback is called when a timeout occurs
type TimeoutCallback func(ctx context.Context, timeout time.Duration)

// TimeoutConfig contains configuration for timeout management
type TimeoutConfig struct {
	MinTimeout time.Duration `json:"min_timeout"`
	MaxTimeout time.Duration `json:"max_timeout"`
	Jitter     float64       `json:"jitter"`
}

// NewTimeoutManager creates a new timeout manager
func NewTimeoutManager(config TimeoutConfig, l common.Logger, metrics common.Metrics) *TimeoutManager {
	if config.MinTimeout <= 0 {
		config.MinTimeout = 150 * time.Millisecond
	}
	if config.MaxTimeout <= 0 {
		config.MaxTimeout = 300 * time.Millisecond
	}
	if config.Jitter <= 0 {
		config.Jitter = 0.1
	}

	// Ensure min <= max
	if config.MinTimeout > config.MaxTimeout {
		config.MaxTimeout = config.MinTimeout
	}

	tm := &TimeoutManager{
		minTimeout: config.MinTimeout,
		maxTimeout: config.MaxTimeout,
		randomizer: rand.New(rand.NewSource(time.Now().UnixNano())),
		logger:     l,
		metrics:    metrics,
		callbacks:  make([]TimeoutCallback, 0),
	}

	// Calculate initial timeout
	tm.currentTimeout = tm.calculateTimeout()

	if l != nil {
		l.Info("timeout manager created",
			logger.Duration("min_timeout", config.MinTimeout),
			logger.Duration("max_timeout", config.MaxTimeout),
			logger.Float64("jitter", config.Jitter),
		)
	}

	return tm
}

// Start starts the timeout manager
func (tm *TimeoutManager) Start(ctx context.Context) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.active {
		return
	}

	tm.active = true
	tm.lastReset = time.Now()
	tm.timer = time.NewTimer(tm.currentTimeout)

	go tm.run(ctx)

	if tm.logger != nil {
		tm.logger.Info("timeout manager started",
			logger.Duration("initial_timeout", tm.currentTimeout),
		)
	}

	if tm.metrics != nil {
		tm.metrics.Counter("forge.consensus.election.timeout_manager_started").Inc()
	}
}

// Stop stops the timeout manager
func (tm *TimeoutManager) Stop() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if !tm.active {
		return
	}

	tm.active = false
	if tm.timer != nil {
		tm.timer.Stop()
		tm.timer = nil
	}

	if tm.logger != nil {
		tm.logger.Info("timeout manager stopped")
	}

	if tm.metrics != nil {
		tm.metrics.Counter("forge.consensus.election.timeout_manager_stopped").Inc()
	}
}

// Reset resets the timeout timer
func (tm *TimeoutManager) Reset() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if !tm.active {
		return
	}

	tm.resetCount++
	tm.lastReset = time.Now()
	tm.currentTimeout = tm.calculateTimeout()

	if tm.timer != nil {
		tm.timer.Stop()
		tm.timer = time.NewTimer(tm.currentTimeout)
	}

	if tm.logger != nil {
		tm.logger.Debug("timeout reset",
			logger.Duration("new_timeout", tm.currentTimeout),
			logger.Int64("reset_count", tm.resetCount),
		)
	}

	if tm.metrics != nil {
		tm.metrics.Counter("forge.consensus.election.timeout_resets").Inc()
		tm.metrics.Histogram("forge.consensus.election.timeout_duration").Observe(tm.currentTimeout.Seconds())
	}
}

// ResetWithBackoff resets the timeout with exponential backoff
func (tm *TimeoutManager) ResetWithBackoff(attempt int) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if !tm.active {
		return
	}

	tm.resetCount++
	tm.lastReset = time.Now()

	// Calculate backoff timeout
	backoffTimeout := tm.calculateBackoffTimeout(attempt)
	tm.currentTimeout = backoffTimeout

	if tm.timer != nil {
		tm.timer.Stop()
		tm.timer = time.NewTimer(tm.currentTimeout)
	}

	if tm.logger != nil {
		tm.logger.Debug("timeout reset with backoff",
			logger.Duration("new_timeout", tm.currentTimeout),
			logger.Int("attempt", attempt),
			logger.Int64("reset_count", tm.resetCount),
		)
	}

	if tm.metrics != nil {
		tm.metrics.Counter("forge.consensus.election.timeout_resets_backoff").Inc()
		tm.metrics.Histogram("forge.consensus.election.timeout_duration").Observe(tm.currentTimeout.Seconds())
	}
}

// AddCallback adds a timeout callback
func (tm *TimeoutManager) AddCallback(callback TimeoutCallback) {
	tm.callbackMu.Lock()
	defer tm.callbackMu.Unlock()

	tm.callbacks = append(tm.callbacks, callback)
}

// RemoveCallback removes a timeout callback
func (tm *TimeoutManager) RemoveCallback(callback TimeoutCallback) {
	tm.callbackMu.Lock()
	defer tm.callbackMu.Unlock()

	for i, cb := range tm.callbacks {
		// Compare function pointers (this is a simplification)
		if &cb == &callback {
			tm.callbacks = append(tm.callbacks[:i], tm.callbacks[i+1:]...)
			break
		}
	}
}

// run runs the timeout manager loop
func (tm *TimeoutManager) run(ctx context.Context) {
	for {
		tm.mu.RLock()
		active := tm.active
		timer := tm.timer
		tm.mu.RUnlock()

		if !active {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			tm.handleTimeout(ctx)
		}
	}
}

// handleTimeout handles a timeout event
func (tm *TimeoutManager) handleTimeout(ctx context.Context) {
	tm.mu.Lock()
	timeout := tm.currentTimeout
	tm.timeoutCount++
	tm.mu.Unlock()

	if tm.logger != nil {
		tm.logger.Debug("election timeout occurred",
			logger.Duration("timeout", timeout),
			logger.Int64("timeout_count", tm.timeoutCount),
		)
	}

	if tm.metrics != nil {
		tm.metrics.Counter("forge.consensus.election.timeouts").Inc()
		tm.metrics.Histogram("forge.consensus.election.timeout_duration").Observe(timeout.Seconds())
	}

	// Execute callbacks
	tm.callbackMu.RLock()
	callbacks := make([]TimeoutCallback, len(tm.callbacks))
	copy(callbacks, tm.callbacks)
	tm.callbackMu.RUnlock()

	for _, callback := range callbacks {
		go func(cb TimeoutCallback) {
			defer func() {
				if r := recover(); r != nil {
					if tm.logger != nil {
						tm.logger.Error("timeout callback panic",
							logger.String("panic", fmt.Sprintf("%v", r)),
						)
					}
				}
			}()

			cb(ctx, timeout)
		}(callback)
	}

	// Reset timer for next timeout
	tm.Reset()
}

// calculateTimeout calculates a random timeout between min and max
func (tm *TimeoutManager) calculateTimeout() time.Duration {
	if tm.minTimeout == tm.maxTimeout {
		return tm.minTimeout
	}

	// Calculate random timeout between min and max
	delta := tm.maxTimeout - tm.minTimeout
	randomDelta := time.Duration(tm.randomizer.Int63n(int64(delta)))

	timeout := tm.minTimeout + randomDelta

	// Add jitter to prevent thundering herd
	jitter := time.Duration(float64(timeout) * 0.1 * tm.randomizer.Float64())
	if tm.randomizer.Float64() > 0.5 {
		timeout += jitter
	} else {
		timeout -= jitter
	}

	// Ensure timeout is within bounds
	if timeout < tm.minTimeout {
		timeout = tm.minTimeout
	}
	if timeout > tm.maxTimeout {
		timeout = tm.maxTimeout
	}

	return timeout
}

// calculateBackoffTimeout calculates timeout with exponential backoff
func (tm *TimeoutManager) calculateBackoffTimeout(attempt int) time.Duration {
	// Base timeout
	baseTimeout := tm.calculateTimeout()

	// Apply exponential backoff
	backoffFactor := 1 << uint(attempt) // 2^attempt
	if backoffFactor > 16 {             // Cap at 16x
		backoffFactor = 16
	}

	backoffTimeout := baseTimeout * time.Duration(backoffFactor)

	// Cap at reasonable maximum (e.g., 30 seconds)
	maxBackoffTimeout := 30 * time.Second
	if backoffTimeout > maxBackoffTimeout {
		backoffTimeout = maxBackoffTimeout
	}

	return backoffTimeout
}

// IsActive returns true if the timeout manager is active
func (tm *TimeoutManager) IsActive() bool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	return tm.active
}

// GetCurrentTimeout returns the current timeout duration
func (tm *TimeoutManager) GetCurrentTimeout() time.Duration {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	return tm.currentTimeout
}

// GetStats returns timeout statistics
func (tm *TimeoutManager) GetStats() TimeoutStats {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	return TimeoutStats{
		MinTimeout:     tm.minTimeout,
		MaxTimeout:     tm.maxTimeout,
		CurrentTimeout: tm.currentTimeout,
		ResetCount:     tm.resetCount,
		TimeoutCount:   tm.timeoutCount,
		LastReset:      tm.lastReset,
		Active:         tm.active,
	}
}

// TimeoutStats contains timeout statistics
type TimeoutStats struct {
	MinTimeout     time.Duration `json:"min_timeout"`
	MaxTimeout     time.Duration `json:"max_timeout"`
	CurrentTimeout time.Duration `json:"current_timeout"`
	ResetCount     int64         `json:"reset_count"`
	TimeoutCount   int64         `json:"timeout_count"`
	LastReset      time.Time     `json:"last_reset"`
	Active         bool          `json:"active"`
}

// SetMinTimeout sets the minimum timeout
func (tm *TimeoutManager) SetMinTimeout(timeout time.Duration) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.minTimeout = timeout
	if tm.maxTimeout < tm.minTimeout {
		tm.maxTimeout = tm.minTimeout
	}

	// Recalculate current timeout
	tm.currentTimeout = tm.calculateTimeout()
}

// SetMaxTimeout sets the maximum timeout
func (tm *TimeoutManager) SetMaxTimeout(timeout time.Duration) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.maxTimeout = timeout
	if tm.minTimeout > tm.maxTimeout {
		tm.minTimeout = tm.maxTimeout
	}

	// Recalculate current timeout
	tm.currentTimeout = tm.calculateTimeout()
}

// GetMinTimeout returns the minimum timeout
func (tm *TimeoutManager) GetMinTimeout() time.Duration {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	return tm.minTimeout
}

// GetMaxTimeout returns the maximum timeout
func (tm *TimeoutManager) GetMaxTimeout() time.Duration {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	return tm.maxTimeout
}

// GetResetCount returns the number of resets
func (tm *TimeoutManager) GetResetCount() int64 {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	return tm.resetCount
}

// GetTimeoutCount returns the number of timeouts
func (tm *TimeoutManager) GetTimeoutCount() int64 {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	return tm.timeoutCount
}

// GetLastReset returns the time of the last reset
func (tm *TimeoutManager) GetLastReset() time.Time {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	return tm.lastReset
}

// GetTimeSinceLastReset returns the time since the last reset
func (tm *TimeoutManager) GetTimeSinceLastReset() time.Duration {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	if tm.lastReset.IsZero() {
		return 0
	}

	return time.Since(tm.lastReset)
}

// GetRemainingTimeout returns the remaining timeout duration
func (tm *TimeoutManager) GetRemainingTimeout() time.Duration {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	if !tm.active || tm.lastReset.IsZero() {
		return 0
	}

	elapsed := time.Since(tm.lastReset)
	if elapsed >= tm.currentTimeout {
		return 0
	}

	return tm.currentTimeout - elapsed
}

// IsTimeoutImminent returns true if timeout is imminent (within 10% of timeout)
func (tm *TimeoutManager) IsTimeoutImminent() bool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	if !tm.active || tm.lastReset.IsZero() {
		return false
	}

	elapsed := time.Since(tm.lastReset)
	threshold := time.Duration(float64(tm.currentTimeout) * 0.9)

	return elapsed >= threshold
}

// PreventSplitVote adds additional randomization to prevent split votes
func (tm *TimeoutManager) PreventSplitVote() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Add extra jitter to prevent split votes
	extraJitter := time.Duration(tm.randomizer.Int63n(int64(50 * time.Millisecond)))
	tm.currentTimeout += extraJitter

	if tm.timer != nil {
		tm.timer.Stop()
		tm.timer = time.NewTimer(tm.currentTimeout)
	}

	if tm.logger != nil {
		tm.logger.Debug("added split vote prevention jitter",
			logger.Duration("extra_jitter", extraJitter),
			logger.Duration("new_timeout", tm.currentTimeout),
		)
	}
}

// AdaptiveTimeout adjusts timeout based on network conditions
func (tm *TimeoutManager) AdaptiveTimeout(networkLatency time.Duration) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Adjust timeout based on network latency
	// Rule: timeout should be at least 10x network latency
	minAdaptiveTimeout := networkLatency * 10

	if minAdaptiveTimeout > tm.minTimeout {
		tm.minTimeout = minAdaptiveTimeout
	}

	// Adjust max timeout proportionally
	if tm.maxTimeout < tm.minTimeout*2 {
		tm.maxTimeout = tm.minTimeout * 2
	}

	// Recalculate current timeout
	tm.currentTimeout = tm.calculateTimeout()

	if tm.logger != nil {
		tm.logger.Debug("adaptive timeout adjustment",
			logger.Duration("network_latency", networkLatency),
			logger.Duration("new_min_timeout", tm.minTimeout),
			logger.Duration("new_max_timeout", tm.maxTimeout),
			logger.Duration("new_current_timeout", tm.currentTimeout),
		)
	}
}

// Emergency timeout for urgent situations
func (tm *TimeoutManager) EmergencyTimeout() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if !tm.active {
		return
	}

	// Set very short timeout for emergency
	emergencyTimeout := 10 * time.Millisecond
	tm.currentTimeout = emergencyTimeout

	if tm.timer != nil {
		tm.timer.Stop()
		tm.timer = time.NewTimer(tm.currentTimeout)
	}

	if tm.logger != nil {
		tm.logger.Warn("emergency timeout triggered",
			logger.Duration("emergency_timeout", emergencyTimeout),
		)
	}

	if tm.metrics != nil {
		tm.metrics.Counter("forge.consensus.election.emergency_timeouts").Inc()
	}
}

// GetTimeoutHealth returns health information about the timeout manager
func (tm *TimeoutManager) GetTimeoutHealth() TimeoutHealth {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	health := TimeoutHealth{
		Active:         tm.active,
		CurrentTimeout: tm.currentTimeout,
		TimeSinceReset: time.Since(tm.lastReset),
		ResetCount:     tm.resetCount,
		TimeoutCount:   tm.timeoutCount,
	}

	// Determine health status
	if !tm.active {
		health.Status = "inactive"
	} else if tm.timeoutCount > tm.resetCount*2 {
		health.Status = "unhealthy" // Too many timeouts
	} else if time.Since(tm.lastReset) > tm.currentTimeout*2 {
		health.Status = "stale" // Reset too long ago
	} else {
		health.Status = "healthy"
	}

	return health
}

// TimeoutHealth contains health information
type TimeoutHealth struct {
	Status         string        `json:"status"`
	Active         bool          `json:"active"`
	CurrentTimeout time.Duration `json:"current_timeout"`
	TimeSinceReset time.Duration `json:"time_since_reset"`
	ResetCount     int64         `json:"reset_count"`
	TimeoutCount   int64         `json:"timeout_count"`
}

// GetTimeoutPattern returns analysis of timeout patterns
func (tm *TimeoutManager) GetTimeoutPattern() TimeoutPattern {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	pattern := TimeoutPattern{
		MinTimeout:     tm.minTimeout,
		MaxTimeout:     tm.maxTimeout,
		CurrentTimeout: tm.currentTimeout,
		ResetCount:     tm.resetCount,
		TimeoutCount:   tm.timeoutCount,
	}

	if tm.resetCount > 0 {
		pattern.AverageResetInterval = time.Since(tm.lastReset) / time.Duration(tm.resetCount)
	}

	if tm.timeoutCount > 0 {
		pattern.TimeoutFrequency = float64(tm.timeoutCount) / time.Since(tm.lastReset).Seconds()
	}

	// Classify pattern
	if tm.timeoutCount == 0 {
		pattern.Classification = "stable"
	} else if tm.timeoutCount > tm.resetCount {
		pattern.Classification = "unstable"
	} else {
		pattern.Classification = "normal"
	}

	return pattern
}

// TimeoutPattern contains timeout pattern analysis
type TimeoutPattern struct {
	MinTimeout           time.Duration `json:"min_timeout"`
	MaxTimeout           time.Duration `json:"max_timeout"`
	CurrentTimeout       time.Duration `json:"current_timeout"`
	ResetCount           int64         `json:"reset_count"`
	TimeoutCount         int64         `json:"timeout_count"`
	AverageResetInterval time.Duration `json:"average_reset_interval"`
	TimeoutFrequency     float64       `json:"timeout_frequency"`
	Classification       string        `json:"classification"`
}

func init() {
	// Ensure proper random seed
	rand.Seed(time.Now().UnixNano())
}
