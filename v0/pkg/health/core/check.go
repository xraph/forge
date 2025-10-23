package core

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
)

// HealthCheck defines the interface for health checks
type HealthCheck = common.HealthCheck

// HealthCheckConfig contains configuration for health checks
type HealthCheckConfig struct {
	Name         string
	Timeout      time.Duration
	Critical     bool
	Dependencies []string
	Interval     time.Duration
	Retries      int
	RetryDelay   time.Duration
	Tags         map[string]string
	Metadata     map[string]interface{}
}

// DefaultHealthCheckConfig returns default configuration for health checks
func DefaultHealthCheckConfig() *HealthCheckConfig {
	return &HealthCheckConfig{
		Timeout:      5 * time.Second,
		Critical:     false,
		Dependencies: []string{},
		Interval:     30 * time.Second,
		Retries:      3,
		RetryDelay:   1 * time.Second,
		Tags:         make(map[string]string),
		Metadata:     make(map[string]interface{}),
	}
}

// HealthCheckFunc is a function type for simple health checks
type HealthCheckFunc func(ctx context.Context) *common.HealthResult

// BaseHealthCheck provides base functionality for health checks
type BaseHealthCheck struct {
	name         string
	timeout      time.Duration
	critical     bool
	dependencies []string
	interval     time.Duration
	retries      int
	retryDelay   time.Duration
	tags         map[string]string
	metadata     map[string]interface{}
	lastResult   *common.HealthResult
	lastCheck    time.Time
	mu           sync.RWMutex
}

// NewBaseHealthCheck creates a new base health check
func NewBaseHealthCheck(config *HealthCheckConfig) *BaseHealthCheck {
	if config == nil {
		config = DefaultHealthCheckConfig()
	}

	return &BaseHealthCheck{
		name:         config.Name,
		timeout:      config.Timeout,
		critical:     config.Critical,
		dependencies: config.Dependencies,
		interval:     config.Interval,
		retries:      config.Retries,
		retryDelay:   config.RetryDelay,
		tags:         config.Tags,
		metadata:     config.Metadata,
	}
}

// Name returns the name of the health check
func (bhc *BaseHealthCheck) Name() string {
	return bhc.name
}

// Timeout returns the timeout for the health check
func (bhc *BaseHealthCheck) Timeout() time.Duration {
	return bhc.timeout
}

// Critical returns whether the health check is critical
func (bhc *BaseHealthCheck) Critical() bool {
	return bhc.critical
}

// Dependencies returns the dependencies of the health check
func (bhc *BaseHealthCheck) Dependencies() []string {
	return bhc.dependencies
}

// Interval returns the check interval
func (bhc *BaseHealthCheck) Interval() time.Duration {
	return bhc.interval
}

// Retries returns the number of retries
func (bhc *BaseHealthCheck) Retries() int {
	return bhc.retries
}

// RetryDelay returns the retry delay
func (bhc *BaseHealthCheck) RetryDelay() time.Duration {
	return bhc.retryDelay
}

// Tags returns the tags for the health check
func (bhc *BaseHealthCheck) Tags() map[string]string {
	return bhc.tags
}

// Metadata returns the metadata for the health check
func (bhc *BaseHealthCheck) Metadata() map[string]interface{} {
	return bhc.metadata
}

// GetLastResult returns the last health check result
func (bhc *BaseHealthCheck) GetLastResult() *common.HealthResult {
	bhc.mu.RLock()
	defer bhc.mu.RUnlock()
	return bhc.lastResult
}

// GetLastCheck returns the time of the last health check
func (bhc *BaseHealthCheck) GetLastCheck() time.Time {
	bhc.mu.RLock()
	defer bhc.mu.RUnlock()
	return bhc.lastCheck
}

// SetLastResult sets the last health check result
func (bhc *BaseHealthCheck) SetLastResult(result *common.HealthResult) {
	bhc.mu.Lock()
	defer bhc.mu.Unlock()
	bhc.lastResult = result
	bhc.lastCheck = time.Now()
}

// ShouldCheck returns true if the health check should be performed
func (bhc *BaseHealthCheck) ShouldCheck() bool {
	bhc.mu.RLock()
	defer bhc.mu.RUnlock()
	return time.Since(bhc.lastCheck) >= bhc.interval
}

// Check performs the health check (to be implemented by concrete types)
func (bhc *BaseHealthCheck) Check(ctx context.Context) *common.HealthResult {
	return common.NewHealthResult(bhc.name, common.HealthStatusUnknown, "base health check - should be overridden")
}

// SimpleHealthCheck implements a simple function-based health check
type SimpleHealthCheck struct {
	*BaseHealthCheck
	checkFunc HealthCheckFunc
}

// NewSimpleHealthCheck creates a new simple health check
func NewSimpleHealthCheck(config *HealthCheckConfig, checkFunc HealthCheckFunc) *SimpleHealthCheck {
	return &SimpleHealthCheck{
		BaseHealthCheck: NewBaseHealthCheck(config),
		checkFunc:       checkFunc,
	}
}

// Check performs the health check
func (shc *SimpleHealthCheck) Check(ctx context.Context) *common.HealthResult {
	start := time.Now()

	// Create context with timeout
	checkCtx, cancel := context.WithTimeout(ctx, shc.timeout)
	defer cancel()

	// Perform the check with retries
	var result *common.HealthResult
	var lastErr error

	for attempt := 0; attempt <= shc.retries; attempt++ {
		if attempt > 0 {
			select {
			case <-checkCtx.Done():
				// Context cancelled or timed out
				result = common.NewHealthResult(shc.name, common.HealthStatusUnhealthy, "health check timed out")
				result.WithError(checkCtx.Err())
				break
			case <-time.After(shc.retryDelay):
				// Wait before retry
			}
		}

		// Perform the actual check
		result = shc.checkFunc(checkCtx)
		if result == nil {
			result = common.NewHealthResult(shc.name, common.HealthStatusUnhealthy, "health check returned nil result")
		}

		// If successful, break out of retry loop
		if result.IsHealthy() {
			break
		}

		lastErr = fmt.Errorf("health check failed (attempt %d/%d): %s", attempt+1, shc.retries+1, result.Message)
	}

	// Set additional metadata
	duration := time.Since(start)
	result.WithDuration(duration)
	result.WithCritical(shc.critical)
	result.WithTags(shc.tags)

	// Add metadata
	for k, v := range shc.metadata {
		result.WithDetail(k, v)
	}

	// Add attempt information if retries were performed
	if shc.retries > 0 {
		result.WithDetail("max_retries", shc.retries)
		result.WithDetail("retry_delay", shc.retryDelay.String())
	}

	if lastErr != nil {
		result.WithError(lastErr)
	}

	// Cache the result
	shc.SetLastResult(result)

	return result
}

// AsyncHealthCheck implements an asynchronous health check
type AsyncHealthCheck struct {
	*BaseHealthCheck
	checkFunc HealthCheckFunc
	running   bool
	stopCh    chan struct{}
	resultCh  chan *common.HealthResult
	mu        sync.RWMutex
}

// NewAsyncHealthCheck creates a new asynchronous health check
func NewAsyncHealthCheck(config *HealthCheckConfig, checkFunc HealthCheckFunc) *AsyncHealthCheck {
	return &AsyncHealthCheck{
		BaseHealthCheck: NewBaseHealthCheck(config),
		checkFunc:       checkFunc,
		stopCh:          make(chan struct{}),
		resultCh:        make(chan *common.HealthResult, 1),
	}
}

// Start starts the asynchronous health check
func (ahc *AsyncHealthCheck) Start(ctx context.Context) {
	ahc.mu.Lock()
	if ahc.running {
		ahc.mu.Unlock()
		return
	}
	ahc.running = true
	ahc.mu.Unlock()

	go ahc.run(ctx)
}

// Stop stops the asynchronous health check
func (ahc *AsyncHealthCheck) Stop() {
	ahc.mu.Lock()
	if !ahc.running {
		ahc.mu.Unlock()
		return
	}
	ahc.running = false
	close(ahc.stopCh)
	ahc.mu.Unlock()
}

// IsRunning returns true if the health check is running
func (ahc *AsyncHealthCheck) IsRunning() bool {
	ahc.mu.RLock()
	defer ahc.mu.RUnlock()
	return ahc.running
}

// run runs the health check loop
func (ahc *AsyncHealthCheck) run(ctx context.Context) {
	ticker := time.NewTicker(ahc.interval)
	defer ticker.Stop()

	// Perform initial check
	ahc.performCheck(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ahc.stopCh:
			return
		case <-ticker.C:
			ahc.performCheck(ctx)
		}
	}
}

// performCheck performs a single health check
func (ahc *AsyncHealthCheck) performCheck(ctx context.Context) {
	result := ahc.checkFunc(ctx)
	if result == nil {
		result = common.NewHealthResult(ahc.name, common.HealthStatusUnhealthy, "async health check returned nil result")
	}

	result.WithCritical(ahc.critical)
	result.WithTags(ahc.tags)

	// Add metadata
	for k, v := range ahc.metadata {
		result.WithDetail(k, v)
	}

	// Cache the result
	ahc.SetLastResult(result)

	// Send result to channel (non-blocking)
	select {
	case ahc.resultCh <- result:
	default:
		// Channel full, skip
	}
}

// Check returns the last health check result
func (ahc *AsyncHealthCheck) Check(ctx context.Context) *common.HealthResult {
	result := ahc.GetLastResult()
	if result == nil {
		return common.NewHealthResult(ahc.name, common.HealthStatusUnknown, "async health check not started")
	}

	// Check if result is stale
	if time.Since(result.Timestamp) > ahc.interval*2 {
		return common.NewHealthResult(ahc.name, common.HealthStatusUnhealthy, "async health check result is stale")
	}

	return result
}

// GetResultChannel returns the result channel for async updates
func (ahc *AsyncHealthCheck) GetResultChannel() <-chan *common.HealthResult {
	return ahc.resultCh
}

// CompositeHealthCheck implements a health check that combines multiple checks
type CompositeHealthCheck struct {
	*BaseHealthCheck
	checks []HealthCheck
}

// NewCompositeHealthCheck creates a new composite health check
func NewCompositeHealthCheck(config *HealthCheckConfig, checks ...HealthCheck) *CompositeHealthCheck {
	return &CompositeHealthCheck{
		BaseHealthCheck: NewBaseHealthCheck(config),
		checks:          checks,
	}
}

// AddCheck adds a health check to the composite
func (chc *CompositeHealthCheck) AddCheck(check HealthCheck) {
	chc.checks = append(chc.checks, check)
}

// Check performs all health checks and aggregates the results
func (chc *CompositeHealthCheck) Check(ctx context.Context) *common.HealthResult {
	start := time.Now()

	// Create context with timeout
	checkCtx, cancel := context.WithTimeout(ctx, chc.timeout)
	defer cancel()

	results := make([]*common.HealthResult, len(chc.checks))

	// Perform all checks
	for i, check := range chc.checks {
		results[i] = check.Check(checkCtx)
	}

	// Aggregate results
	overall := chc.aggregateResults(results)
	overall.WithDuration(time.Since(start))
	overall.WithCritical(chc.critical)
	overall.WithTags(chc.tags)

	// Add individual results as details
	details := make(map[string]interface{})
	for _, result := range results {
		details[result.Name] = map[string]interface{}{
			"status":   result.Status,
			"message":  result.Message,
			"duration": result.Duration,
			"critical": result.Critical,
		}
	}
	overall.WithDetails(details)

	// Cache the result
	chc.SetLastResult(overall)

	return overall
}

// aggregateResults aggregates multiple health results into a single result
func (chc *CompositeHealthCheck) aggregateResults(results []*common.HealthResult) *common.HealthResult {
	if len(results) == 0 {
		return common.NewHealthResult(chc.name, common.HealthStatusUnknown, "no health checks configured")
	}

	var overallStatus = common.HealthStatusHealthy
	var messages []string
	var criticalFailed bool

	for _, result := range results {
		if result.Critical && result.IsUnhealthy() {
			criticalFailed = true
		}

		if result.Status.Severity() > overallStatus.Severity() {
			overallStatus = result.Status
		}

		if !result.IsHealthy() {
			messages = append(messages, fmt.Sprintf("%s: %s", result.Name, result.Message))
		}
	}

	// If any critical check failed, overall status is unhealthy
	if criticalFailed {
		overallStatus = common.HealthStatusUnhealthy
	}

	message := fmt.Sprintf("Composite check with %d sub-checks", len(results))
	if len(messages) > 0 {
		message = fmt.Sprintf("%s. Failed: %s", message, fmt.Sprintf("[%s]", fmt.Sprintf("%v", messages)))
	}

	return common.NewHealthResult(chc.name, overallStatus, message)
}

// HealthCheckWrapper wraps a health check with additional functionality
type HealthCheckWrapper struct {
	check      HealthCheck
	beforeFunc func(ctx context.Context) error
	afterFunc  func(ctx context.Context, result *common.HealthResult) error
}

// NewHealthCheckWrapper creates a new health check wrapper
func NewHealthCheckWrapper(check HealthCheck) *HealthCheckWrapper {
	return &HealthCheckWrapper{
		check: check,
	}
}

// WithBefore adds a before function to the wrapper
func (hcw *HealthCheckWrapper) WithBefore(beforeFunc func(ctx context.Context) error) *HealthCheckWrapper {
	hcw.beforeFunc = beforeFunc
	return hcw
}

// WithAfter adds an after function to the wrapper
func (hcw *HealthCheckWrapper) WithAfter(afterFunc func(ctx context.Context, result *common.HealthResult) error) *HealthCheckWrapper {
	hcw.afterFunc = afterFunc
	return hcw
}

// Name returns the name of the wrapped health check
func (hcw *HealthCheckWrapper) Name() string {
	return hcw.check.Name()
}

// Timeout returns the timeout of the wrapped health check
func (hcw *HealthCheckWrapper) Timeout() time.Duration {
	return hcw.check.Timeout()
}

// Critical returns whether the wrapped health check is critical
func (hcw *HealthCheckWrapper) Critical() bool {
	return hcw.check.Critical()
}

// Dependencies returns the dependencies of the wrapped health check
func (hcw *HealthCheckWrapper) Dependencies() []string {
	return hcw.check.Dependencies()
}

// Check performs the wrapped health check
func (hcw *HealthCheckWrapper) Check(ctx context.Context) *common.HealthResult {
	// Execute before function if provided
	if hcw.beforeFunc != nil {
		if err := hcw.beforeFunc(ctx); err != nil {
			return common.NewHealthResult(hcw.check.Name(), common.HealthStatusUnhealthy, "before function failed").WithError(err)
		}
	}

	// Perform the actual check
	result := hcw.check.Check(ctx)

	// Execute after function if provided
	if hcw.afterFunc != nil {
		if err := hcw.afterFunc(ctx, result); err != nil {
			result.WithError(err)
			result.WithDetail("after_function_error", err.Error())
		}
	}

	return result
}
