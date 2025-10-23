package monitoring

import (
	"context"
	"fmt"
	"sync"
	"time"

	cachecore "github.com/xraph/forge/v0/pkg/cache/core"
	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
)

// CacheHealthChecker provides comprehensive health checking for cache systems
type CacheHealthChecker struct {
	cacheManager  cachecore.CacheManager
	healthChecker common.HealthChecker
	logger        common.Logger
	metrics       common.Metrics
	config        *CacheHealthConfig
	checks        map[string]*CacheHealthCheck
	results       map[string]*CacheHealthResult
	mu            sync.RWMutex
	started       bool
	stopChan      chan struct{}
}

// CacheHealthConfig contains configuration for cache health checking
type CacheHealthConfig struct {
	CheckInterval             time.Duration          `yaml:"check_interval" json:"check_interval" default:"30s"`
	HealthTimeout             time.Duration          `yaml:"health_timeout" json:"health_timeout" default:"5s"`
	FailureThreshold          int                    `yaml:"failure_threshold" json:"failure_threshold" default:"3"`
	RecoveryThreshold         int                    `yaml:"recovery_threshold" json:"recovery_threshold" default:"2"`
	EnableDetailedChecks      bool                   `yaml:"enable_detailed_checks" json:"enable_detailed_checks" default:"true"`
	EnablePerformanceCheck    bool                   `yaml:"enable_performance_check" json:"enable_performance_check" default:"true"`
	EnableConnectivityCheck   bool                   `yaml:"enable_connectivity_check" json:"enable_connectivity_check" default:"true"`
	EnableCapacityCheck       bool                   `yaml:"enable_capacity_check" json:"enable_capacity_check" default:"true"`
	CapacityWarningThreshold  float64                `yaml:"capacity_warning_threshold" json:"capacity_warning_threshold" default:"0.8"`
	CapacityCriticalThreshold float64                `yaml:"capacity_critical_threshold" json:"capacity_critical_threshold" default:"0.95"`
	LatencyWarningThreshold   time.Duration          `yaml:"latency_warning_threshold" json:"latency_warning_threshold" default:"100ms"`
	LatencyCriticalThreshold  time.Duration          `yaml:"latency_critical_threshold" json:"latency_critical_threshold" default:"500ms"`
	HitRateWarningThreshold   float64                `yaml:"hit_rate_warning_threshold" json:"hit_rate_warning_threshold" default:"0.7"`
	HitRateCriticalThreshold  float64                `yaml:"hit_rate_critical_threshold" json:"hit_rate_critical_threshold" default:"0.5"`
	CheckTypes                []string               `yaml:"check_types" json:"check_types"`
	CustomChecks              map[string]interface{} `yaml:"custom_checks" json:"custom_checks"`
	AlertOnFailure            bool                   `yaml:"alert_on_failure" json:"alert_on_failure" default:"true"`
	AlertOnRecovery           bool                   `yaml:"alert_on_recovery" json:"alert_on_recovery" default:"true"`
	EnableTrends              bool                   `yaml:"enable_trends" json:"enable_trends" default:"true"`
	TrendWindow               time.Duration          `yaml:"trend_window" json:"trend_window" default:"1h"`
	MaxHistorySize            int                    `yaml:"max_history_size" json:"max_history_size" default:"100"`
}

// CacheHealthCheck represents a health check for a cache
type CacheHealthCheck struct {
	Name                 string                 `json:"name"`
	CacheName            string                 `json:"cache_name"`
	CheckType            CacheHealthCheckType   `json:"check_type"`
	Description          string                 `json:"description"`
	Critical             bool                   `json:"critical"`
	Timeout              time.Duration          `json:"timeout"`
	Interval             time.Duration          `json:"interval"`
	FailureThreshold     int                    `json:"failure_threshold"`
	RecoveryThreshold    int                    `json:"recovery_threshold"`
	Enabled              bool                   `json:"enabled"`
	Tags                 map[string]string      `json:"tags"`
	Metadata             map[string]interface{} `json:"metadata"`
	LastCheck            time.Time              `json:"last_check"`
	LastResult           *CacheHealthResult     `json:"last_result"`
	ConsecutiveFailures  int                    `json:"consecutive_failures"`
	ConsecutiveSuccesses int                    `json:"consecutive_successes"`
	TotalChecks          int64                  `json:"total_checks"`
	TotalFailures        int64                  `json:"total_failures"`
	mu                   sync.RWMutex           `json:"-"`
}

// CacheHealthCheckType defines types of cache health checks
type CacheHealthCheckType string

const (
	CacheHealthCheckTypeConnectivity CacheHealthCheckType = "connectivity"
	CacheHealthCheckTypePerformance  CacheHealthCheckType = "performance"
	CacheHealthCheckTypeCapacity     CacheHealthCheckType = "capacity"
	CacheHealthCheckTypeConsistency  CacheHealthCheckType = "consistency"
	CacheHealthCheckTypeReplication  CacheHealthCheckType = "replication"
	CacheHealthCheckTypeCustom       CacheHealthCheckType = "custom"
	CacheHealthCheckTypeBasic        CacheHealthCheckType = "basic"
	CacheHealthCheckTypeDeep         CacheHealthCheckType = "deep"
)

// CacheHealthResult represents the result of a cache health check
type CacheHealthResult struct {
	CheckName   string                 `json:"check_name"`
	CacheName   string                 `json:"cache_name"`
	CheckType   CacheHealthCheckType   `json:"check_type"`
	Status      CacheHealthStatus      `json:"status"`
	Message     string                 `json:"message"`
	Details     map[string]interface{} `json:"details"`
	Metrics     CacheHealthMetrics     `json:"metrics"`
	Timestamp   time.Time              `json:"timestamp"`
	Duration    time.Duration          `json:"duration"`
	Error       string                 `json:"error,omitempty"`
	Warnings    []string               `json:"warnings,omitempty"`
	Suggestions []string               `json:"suggestions,omitempty"`
	Trends      *CacheHealthTrends     `json:"trends,omitempty"`
}

// CacheHealthStatus represents the health status of a cache
type CacheHealthStatus string

const (
	CacheHealthStatusHealthy  CacheHealthStatus = "healthy"
	CacheHealthStatusWarning  CacheHealthStatus = "warning"
	CacheHealthStatusCritical CacheHealthStatus = "critical"
	CacheHealthStatusUnknown  CacheHealthStatus = "unknown"
	CacheHealthStatusTimeout  CacheHealthStatus = "timeout"
)

// CacheHealthMetrics contains health-related metrics
type CacheHealthMetrics struct {
	ConnectionCount   int           `json:"connection_count"`
	MemoryUsage       int64         `json:"memory_usage"`
	MemoryLimit       int64         `json:"memory_limit"`
	MemoryUtilization float64       `json:"memory_utilization"`
	ItemCount         int64         `json:"item_count"`
	ItemLimit         int64         `json:"item_limit"`
	ItemUtilization   float64       `json:"item_utilization"`
	HitRate           float64       `json:"hit_rate"`
	MissRate          float64       `json:"miss_rate"`
	EvictionRate      float64       `json:"eviction_rate"`
	ResponseTime      time.Duration `json:"response_time"`
	ThroughputRPS     float64       `json:"throughput_rps"`
	ErrorRate         float64       `json:"error_rate"`
	AvailabilityPct   float64       `json:"availability_pct"`
}

// CacheHealthTrends contains trend information
type CacheHealthTrends struct {
	MemoryTrend     TrendDirection `json:"memory_trend"`
	HitRateTrend    TrendDirection `json:"hit_rate_trend"`
	LatencyTrend    TrendDirection `json:"latency_trend"`
	ThroughputTrend TrendDirection `json:"throughput_trend"`
	ErrorRateTrend  TrendDirection `json:"error_rate_trend"`
	TrendWindow     time.Duration  `json:"trend_window"`
	TrendConfidence float64        `json:"trend_confidence"`
}

// CacheHealthSummary provides an overall health summary
type CacheHealthSummary struct {
	OverallStatus  CacheHealthStatus             `json:"overall_status"`
	TotalCaches    int                           `json:"total_caches"`
	HealthyCaches  int                           `json:"healthy_caches"`
	WarningCaches  int                           `json:"warning_caches"`
	CriticalCaches int                           `json:"critical_caches"`
	UnknownCaches  int                           `json:"unknown_caches"`
	TotalChecks    int                           `json:"total_checks"`
	PassingChecks  int                           `json:"passing_checks"`
	WarningChecks  int                           `json:"warning_checks"`
	FailingChecks  int                           `json:"failing_checks"`
	LastUpdate     time.Time                     `json:"last_update"`
	CheckDetails   map[string]*CacheHealthResult `json:"check_details"`
	TrendSummary   *CacheHealthTrends            `json:"trend_summary,omitempty"`
}

// NewCacheHealthChecker creates a new cache health checker
func NewCacheHealthChecker(cacheManager cachecore.CacheManager, healthChecker common.HealthChecker, logger common.Logger, metrics common.Metrics, config *CacheHealthConfig) *CacheHealthChecker {
	if config == nil {
		config = &CacheHealthConfig{
			CheckInterval:             30 * time.Second,
			HealthTimeout:             5 * time.Second,
			FailureThreshold:          3,
			RecoveryThreshold:         2,
			EnableDetailedChecks:      true,
			EnablePerformanceCheck:    true,
			EnableConnectivityCheck:   true,
			EnableCapacityCheck:       true,
			CapacityWarningThreshold:  0.8,
			CapacityCriticalThreshold: 0.95,
			LatencyWarningThreshold:   100 * time.Millisecond,
			LatencyCriticalThreshold:  500 * time.Millisecond,
			HitRateWarningThreshold:   0.7,
			HitRateCriticalThreshold:  0.5,
			AlertOnFailure:            true,
			AlertOnRecovery:           true,
			EnableTrends:              true,
			TrendWindow:               time.Hour,
			MaxHistorySize:            100,
		}
	}

	return &CacheHealthChecker{
		cacheManager:  cacheManager,
		healthChecker: healthChecker,
		logger:        logger,
		metrics:       metrics,
		config:        config,
		checks:        make(map[string]*CacheHealthCheck),
		results:       make(map[string]*CacheHealthResult),
		stopChan:      make(chan struct{}),
	}
}

// Start starts the cache health checker
func (chc *CacheHealthChecker) Start(ctx context.Context) error {
	chc.mu.Lock()
	defer chc.mu.Unlock()

	if chc.started {
		return fmt.Errorf("cache health checker already started")
	}

	chc.started = true

	// Register cache health checks with the main health checker
	if err := chc.registerHealthChecks(); err != nil {
		return fmt.Errorf("failed to register health checks: %w", err)
	}

	// Start monitoring goroutine
	go chc.monitoringLoop(ctx)

	if chc.logger != nil {
		chc.logger.Info("cache health checker started",
			logger.Duration("check_interval", chc.config.CheckInterval),
			logger.Int("registered_checks", len(chc.checks)),
		)
	}

	if chc.metrics != nil {
		chc.metrics.Counter("forge.cache.health.checker_started").Inc()
	}

	return nil
}

// Stop stops the cache health checker
func (chc *CacheHealthChecker) Stop() error {
	chc.mu.Lock()
	defer chc.mu.Unlock()

	if !chc.started {
		return fmt.Errorf("cache health checker not started")
	}

	close(chc.stopChan)
	chc.started = false

	if chc.logger != nil {
		chc.logger.Info("cache health checker stopped")
	}

	if chc.metrics != nil {
		chc.metrics.Counter("forge.cache.health.checker_stopped").Inc()
	}

	return nil
}

// RegisterCacheCheck registers a health check for a cache
func (chc *CacheHealthChecker) RegisterCacheCheck(check *CacheHealthCheck) error {
	chc.mu.Lock()
	defer chc.mu.Unlock()

	checkKey := fmt.Sprintf("%s_%s", check.CacheName, check.CheckType)

	if _, exists := chc.checks[checkKey]; exists {
		return fmt.Errorf("health check already exists: %s", checkKey)
	}

	// Set defaults
	if check.Timeout == 0 {
		check.Timeout = chc.config.HealthTimeout
	}
	if check.Interval == 0 {
		check.Interval = chc.config.CheckInterval
	}
	if check.FailureThreshold == 0 {
		check.FailureThreshold = chc.config.FailureThreshold
	}
	if check.RecoveryThreshold == 0 {
		check.RecoveryThreshold = chc.config.RecoveryThreshold
	}

	chc.checks[checkKey] = check

	if chc.logger != nil {
		chc.logger.Info("cache health check registered",
			logger.String("cache_name", check.CacheName),
			logger.String("check_type", string(check.CheckType)),
			logger.Bool("critical", check.Critical),
		)
	}

	return nil
}

// UnregisterCacheCheck unregisters a health check for a cache
func (chc *CacheHealthChecker) UnregisterCacheCheck(cacheName string, checkType CacheHealthCheckType) error {
	chc.mu.Lock()
	defer chc.mu.Unlock()

	checkKey := fmt.Sprintf("%s_%s", cacheName, checkType)

	if _, exists := chc.checks[checkKey]; !exists {
		return fmt.Errorf("health check not found: %s", checkKey)
	}

	delete(chc.checks, checkKey)
	delete(chc.results, checkKey)

	if chc.logger != nil {
		chc.logger.Info("cache health check unregistered",
			logger.String("cache_name", cacheName),
			logger.String("check_type", string(checkType)),
		)
	}

	return nil
}

// GetCacheHealth returns the health status for a specific cache
func (chc *CacheHealthChecker) GetCacheHealth(cacheName string) (*CacheHealthResult, error) {
	chc.mu.RLock()
	defer chc.mu.RUnlock()

	// Find the most critical health result for the cache
	var overallResult *CacheHealthResult
	var worstStatus CacheHealthStatus = CacheHealthStatusHealthy

	for _, result := range chc.results {
		if result.CacheName == cacheName {
			if chc.isWorse(result.Status, worstStatus) {
				worstStatus = result.Status
				overallResult = result
			}
		}
	}

	if overallResult == nil {
		return nil, fmt.Errorf("no health checks found for cache %s", cacheName)
	}

	// Create aggregate result
	aggregateResult := &CacheHealthResult{
		CheckName: fmt.Sprintf("aggregate_%s", cacheName),
		CacheName: cacheName,
		Status:    worstStatus,
		Message:   chc.generateAggregateMessage(cacheName),
		Details:   chc.aggregateDetails(cacheName),
		Timestamp: time.Now(),
	}

	return aggregateResult, nil
}

// GetAllCacheHealth returns health status for all caches
func (chc *CacheHealthChecker) GetAllCacheHealth() *CacheHealthSummary {
	chc.mu.RLock()
	defer chc.mu.RUnlock()

	summary := &CacheHealthSummary{
		CheckDetails: make(map[string]*CacheHealthResult),
		LastUpdate:   time.Now(),
	}

	cacheStatuses := make(map[string]CacheHealthStatus)

	// Process all results
	for checkKey, result := range chc.results {
		summary.CheckDetails[checkKey] = result
		summary.TotalChecks++

		// Track per-cache status
		currentStatus, exists := cacheStatuses[result.CacheName]
		if !exists || chc.isWorse(result.Status, currentStatus) {
			cacheStatuses[result.CacheName] = result.Status
		}

		// Count check statuses
		switch result.Status {
		case CacheHealthStatusHealthy:
			summary.PassingChecks++
		case CacheHealthStatusWarning:
			summary.WarningChecks++
		case CacheHealthStatusCritical:
			summary.FailingChecks++
		}
	}

	// Count cache statuses
	summary.TotalCaches = len(cacheStatuses)
	var worstOverallStatus CacheHealthStatus = CacheHealthStatusHealthy

	for _, status := range cacheStatuses {
		switch status {
		case CacheHealthStatusHealthy:
			summary.HealthyCaches++
		case CacheHealthStatusWarning:
			summary.WarningCaches++
			if chc.isWorse(CacheHealthStatusWarning, worstOverallStatus) {
				worstOverallStatus = CacheHealthStatusWarning
			}
		case CacheHealthStatusCritical:
			summary.CriticalCaches++
			if chc.isWorse(CacheHealthStatusCritical, worstOverallStatus) {
				worstOverallStatus = CacheHealthStatusCritical
			}
		case CacheHealthStatusUnknown:
			summary.UnknownCaches++
			if chc.isWorse(CacheHealthStatusUnknown, worstOverallStatus) {
				worstOverallStatus = CacheHealthStatusUnknown
			}
		}
	}

	summary.OverallStatus = worstOverallStatus

	return summary
}

// registerHealthChecks registers cache health checks with the main health checker
func (chc *CacheHealthChecker) registerHealthChecks() error {
	// Get all caches and register basic health checks
	caches := chc.cacheManager.ListCaches()

	for _, cache := range caches {
		// Register basic connectivity check
		if chc.config.EnableConnectivityCheck {
			check := &CacheHealthCheck{
				Name:        fmt.Sprintf("cache_connectivity_%s", cache.Name()),
				CacheName:   cache.Name(),
				CheckType:   CacheHealthCheckTypeConnectivity,
				Description: fmt.Sprintf("Connectivity check for cache %s", cache.Name()),
				Critical:    true,
				Enabled:     true,
				Tags:        map[string]string{"cache": cache.Name(), "type": "connectivity"},
				Metadata:    make(map[string]interface{}),
			}

			if err := chc.RegisterCacheCheck(check); err != nil {
				return err
			}
		}

		// Register performance check
		if chc.config.EnablePerformanceCheck {
			check := &CacheHealthCheck{
				Name:        fmt.Sprintf("cache_performance_%s", cache.Name()),
				CacheName:   cache.Name(),
				CheckType:   CacheHealthCheckTypePerformance,
				Description: fmt.Sprintf("Performance check for cache %s", cache.Name()),
				Critical:    false,
				Enabled:     true,
				Tags:        map[string]string{"cache": cache.Name(), "type": "performance"},
				Metadata:    make(map[string]interface{}),
			}

			if err := chc.RegisterCacheCheck(check); err != nil {
				return err
			}
		}

		// Register capacity check
		if chc.config.EnableCapacityCheck {
			check := &CacheHealthCheck{
				Name:        fmt.Sprintf("cache_capacity_%s", cache.Name()),
				CacheName:   cache.Name(),
				CheckType:   CacheHealthCheckTypeCapacity,
				Description: fmt.Sprintf("Capacity check for cache %s", cache.Name()),
				Critical:    false,
				Enabled:     true,
				Tags:        map[string]string{"cache": cache.Name(), "type": "capacity"},
				Metadata:    make(map[string]interface{}),
			}

			if err := chc.RegisterCacheCheck(check); err != nil {
				return err
			}
		}
	}

	// Register with main health checker
	if chc.healthChecker != nil {
		// return chc.healthChecker.RegisterCheck("forge.cache..cache_health", chc.performOverallHealthCheck)
	}

	return nil
}

// performOverallHealthCheck performs an overall cache health check
func (chc *CacheHealthChecker) performOverallHealthCheck(ctx context.Context) common.HealthResult {
	summary := chc.GetAllCacheHealth()

	var status common.HealthStatus
	switch summary.OverallStatus {
	case CacheHealthStatusHealthy:
		status = common.HealthStatusHealthy
	case CacheHealthStatusWarning:
		status = common.HealthStatusDegraded
	case CacheHealthStatusCritical:
		status = common.HealthStatusUnhealthy
	default:
		status = common.HealthStatusUnknown
	}

	details := map[string]interface{}{
		"total_caches":    summary.TotalCaches,
		"healthy_caches":  summary.HealthyCaches,
		"warning_caches":  summary.WarningCaches,
		"critical_caches": summary.CriticalCaches,
		"total_checks":    summary.TotalChecks,
		"passing_checks":  summary.PassingChecks,
		"failing_checks":  summary.FailingChecks,
	}

	message := fmt.Sprintf("%d/%d caches healthy", summary.HealthyCaches, summary.TotalCaches)

	return common.HealthResult{
		Status:    status,
		Message:   message,
		Details:   details,
		Timestamp: time.Now(),
		Duration:  0, // Will be set by health checker
	}
}

// monitoringLoop runs the continuous health monitoring
func (chc *CacheHealthChecker) monitoringLoop(ctx context.Context) {
	ticker := time.NewTicker(chc.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			chc.performAllChecks(ctx)

		case <-chc.stopChan:
			return

		case <-ctx.Done():
			return
		}
	}
}

// performAllChecks performs all registered health checks
func (chc *CacheHealthChecker) performAllChecks(ctx context.Context) {
	chc.mu.RLock()
	checks := make([]*CacheHealthCheck, 0, len(chc.checks))
	for _, check := range chc.checks {
		if check.Enabled {
			checks = append(checks, check)
		}
	}
	chc.mu.RUnlock()

	// Perform checks concurrently
	var wg sync.WaitGroup
	for _, check := range checks {
		wg.Add(1)
		go func(c *CacheHealthCheck) {
			defer wg.Done()
			chc.performCheck(ctx, c)
		}(check)
	}

	wg.Wait()
}

// performCheck performs a single health check
func (chc *CacheHealthChecker) performCheck(ctx context.Context, check *CacheHealthCheck) {
	startTime := time.Now()

	// Create timeout context
	checkCtx, cancel := context.WithTimeout(ctx, check.Timeout)
	defer cancel()

	// Perform the actual check
	result := chc.executeCheck(checkCtx, check)
	result.Duration = time.Since(startTime)
	result.Timestamp = startTime

	// Update check statistics
	check.mu.Lock()
	check.LastCheck = startTime
	check.LastResult = result
	check.TotalChecks++

	if result.Status == CacheHealthStatusHealthy {
		check.ConsecutiveFailures = 0
		check.ConsecutiveSuccesses++
	} else {
		check.ConsecutiveSuccesses = 0
		check.ConsecutiveFailures++
		check.TotalFailures++
	}
	check.mu.Unlock()

	// Store result
	checkKey := fmt.Sprintf("%s_%s", check.CacheName, check.CheckType)
	chc.mu.Lock()
	chc.results[checkKey] = result
	chc.mu.Unlock()

	// Handle alerts
	chc.handleCheckResult(check, result)

	// Update metrics
	if chc.metrics != nil {
		chc.metrics.Counter("forge.cache.health.checks_performed",
			"cache", check.CacheName,
			"type", string(check.CheckType),
		).Inc()

		chc.metrics.Histogram("forge.cache.health.check_duration",
			"cache", check.CacheName,
			"type", string(check.CheckType),
		).Observe(result.Duration.Seconds())

		if result.Status != CacheHealthStatusHealthy {
			chc.metrics.Counter("forge.cache.health.check_failures",
				"cache", check.CacheName,
				"type", string(check.CheckType),
				"status", string(result.Status),
			).Inc()
		}
	}
}

// executeCheck executes the actual health check logic
func (chc *CacheHealthChecker) executeCheck(ctx context.Context, check *CacheHealthCheck) *CacheHealthResult {
	result := &CacheHealthResult{
		CheckName: check.Name,
		CacheName: check.CacheName,
		CheckType: check.CheckType,
		Details:   make(map[string]interface{}),
	}

	// Get cache instance
	cache, err := chc.cacheManager.GetCache(check.CacheName)
	if err != nil {
		result.Status = CacheHealthStatusCritical
		result.Message = fmt.Sprintf("Cache not found: %s", check.CacheName)
		result.Error = err.Error()
		return result
	}

	switch check.CheckType {
	case CacheHealthCheckTypeConnectivity:
		return chc.checkConnectivity(ctx, cache, check)
	case CacheHealthCheckTypePerformance:
		return chc.checkPerformance(ctx, cache, check)
	case CacheHealthCheckTypeCapacity:
		return chc.checkCapacity(ctx, cache, check)
	case CacheHealthCheckTypeConsistency:
		return chc.checkConsistency(ctx, cache, check)
	case CacheHealthCheckTypeReplication:
		return chc.checkReplication(ctx, cache, check)
	case CacheHealthCheckTypeBasic:
		return chc.checkBasic(ctx, cache, check)
	case CacheHealthCheckTypeDeep:
		return chc.checkDeep(ctx, cache, check)
	default:
		result.Status = CacheHealthStatusUnknown
		result.Message = fmt.Sprintf("Unknown check type: %s", check.CheckType)
		return result
	}
}

// checkConnectivity performs connectivity health check
func (chc *CacheHealthChecker) checkConnectivity(ctx context.Context, cache cachecore.Cache, check *CacheHealthCheck) *CacheHealthResult {
	result := &CacheHealthResult{
		CheckName: check.Name,
		CacheName: check.CacheName,
		CheckType: check.CheckType,
		Details:   make(map[string]interface{}),
	}

	// Basic connectivity test
	err := cache.HealthCheck(ctx)
	if err != nil {
		result.Status = CacheHealthStatusCritical
		result.Message = "Cache connectivity failed"
		result.Error = err.Error()
		return result
	}

	// Test basic operations
	testKey := fmt.Sprintf("__health_check_%d", time.Now().UnixNano())
	testValue := "health_check_value"

	// Test SET operation
	if err := cache.Set(ctx, testKey, testValue, time.Minute); err != nil {
		result.Status = CacheHealthStatusCritical
		result.Message = "Cache SET operation failed"
		result.Error = err.Error()
		return result
	}

	// Test GET operation
	retrievedValue, err := cache.Get(ctx, testKey)
	if err != nil {
		result.Status = CacheHealthStatusCritical
		result.Message = "Cache GET operation failed"
		result.Error = err.Error()
		// Try to clean up
		cache.Delete(ctx, testKey)
		return result
	}

	// Verify value
	if retrievedValue != testValue {
		result.Status = CacheHealthStatusCritical
		result.Message = "Cache data integrity issue"
		result.Details["expected"] = testValue
		result.Details["actual"] = retrievedValue
		// Clean up
		cache.Delete(ctx, testKey)
		return result
	}

	// Test DELETE operation
	if err := cache.Delete(ctx, testKey); err != nil {
		result.Status = CacheHealthStatusWarning
		result.Message = "Cache DELETE operation failed"
		result.Error = err.Error()
		result.Warnings = []string{"Failed to clean up test key"}
		return result
	}

	result.Status = CacheHealthStatusHealthy
	result.Message = "Cache connectivity is healthy"
	return result
}

// checkPerformance performs performance health check
func (chc *CacheHealthChecker) checkPerformance(ctx context.Context, cache cachecore.Cache, check *CacheHealthCheck) *CacheHealthResult {
	result := &CacheHealthResult{
		CheckName: check.Name,
		CacheName: check.CacheName,
		CheckType: check.CheckType,
		Details:   make(map[string]interface{}),
	}

	// Get cache stats
	stats := cache.Stats()

	// Calculate metrics
	metrics := CacheHealthMetrics{
		HitRate:         cachecore.CalculateHitRatio(stats.Hits, stats.Misses),
		MissRate:        float64(stats.Misses) / float64(stats.Hits+stats.Misses),
		ItemCount:       stats.Size,
		MemoryUsage:     stats.Memory,
		ConnectionCount: stats.Connections,
	}

	// Test response time
	testKey := fmt.Sprintf("__perf_check_%d", time.Now().UnixNano())
	testValue := "performance_test"

	start := time.Now()
	err := cache.Set(ctx, testKey, testValue, time.Minute)
	setLatency := time.Since(start)

	if err != nil {
		result.Status = CacheHealthStatusCritical
		result.Message = "Performance check failed during SET operation"
		result.Error = err.Error()
		return result
	}

	start = time.Now()
	_, err = cache.Get(ctx, testKey)
	getLatency := time.Since(start)

	if err != nil {
		result.Status = CacheHealthStatusWarning
		result.Message = "Performance check failed during GET operation"
		result.Error = err.Error()
		cache.Delete(ctx, testKey) // Try to clean up
		return result
	}

	// Clean up
	cache.Delete(ctx, testKey)

	// Update metrics
	metrics.ResponseTime = (setLatency + getLatency) / 2
	result.Metrics = metrics

	// Evaluate performance
	var status CacheHealthStatus = CacheHealthStatusHealthy
	var warnings []string
	var suggestions []string

	// Check latency
	if metrics.ResponseTime > chc.config.LatencyCriticalThreshold {
		status = CacheHealthStatusCritical
		result.Message = fmt.Sprintf("High latency detected: %v", metrics.ResponseTime)
	} else if metrics.ResponseTime > chc.config.LatencyWarningThreshold {
		if status == CacheHealthStatusHealthy {
			status = CacheHealthStatusWarning
		}
		warnings = append(warnings, fmt.Sprintf("Elevated latency: %v", metrics.ResponseTime))
		suggestions = append(suggestions, "Consider optimizing cache configuration or scaling")
	}

	// Check hit rate
	if metrics.HitRate < chc.config.HitRateCriticalThreshold {
		status = CacheHealthStatusCritical
		result.Message = fmt.Sprintf("Low hit rate: %.2f%%", metrics.HitRate*100)
	} else if metrics.HitRate < chc.config.HitRateWarningThreshold {
		if status == CacheHealthStatusHealthy {
			status = CacheHealthStatusWarning
		}
		warnings = append(warnings, fmt.Sprintf("Low hit rate: %.2f%%", metrics.HitRate*100))
		suggestions = append(suggestions, "Consider cache warming or reviewing cache strategy")
	}

	result.Status = status
	result.Warnings = warnings
	result.Suggestions = suggestions

	if result.Message == "" {
		result.Message = fmt.Sprintf("Performance healthy - Latency: %v, Hit Rate: %.2f%%",
			metrics.ResponseTime, metrics.HitRate*100)
	}

	result.Details["set_latency"] = setLatency
	result.Details["get_latency"] = getLatency
	result.Details["hit_rate"] = metrics.HitRate
	result.Details["item_count"] = metrics.ItemCount

	return result
}

// checkCapacity performs capacity health check
func (chc *CacheHealthChecker) checkCapacity(ctx context.Context, cache cachecore.Cache, check *CacheHealthCheck) *CacheHealthResult {
	result := &CacheHealthResult{
		CheckName: check.Name,
		CacheName: check.CacheName,
		CheckType: check.CheckType,
		Details:   make(map[string]interface{}),
	}

	stats := cache.Stats()

	// Calculate utilization
	var memoryUtilization, itemUtilization float64

	// Memory utilization (if limits are available)
	// This would need to be enhanced based on specific cache implementation
	if stats.Memory > 0 {
		// Assume some reasonable limits for demonstration
		estimatedLimit := int64(1024 * 1024 * 1024) // 1GB
		memoryUtilization = float64(stats.Memory) / float64(estimatedLimit)
	}

	// Item utilization
	if stats.Size > 0 {
		estimatedItemLimit := int64(100000) // 100k items
		itemUtilization = float64(stats.Size) / float64(estimatedItemLimit)
	}

	metrics := CacheHealthMetrics{
		MemoryUsage:       stats.Memory,
		MemoryUtilization: memoryUtilization,
		ItemCount:         stats.Size,
		ItemUtilization:   itemUtilization,
	}

	result.Metrics = metrics

	// Evaluate capacity
	var status CacheHealthStatus = CacheHealthStatusHealthy
	var warnings []string
	var suggestions []string

	// Check memory utilization
	if memoryUtilization > chc.config.CapacityCriticalThreshold {
		status = CacheHealthStatusCritical
		result.Message = fmt.Sprintf("Memory utilization critical: %.1f%%", memoryUtilization*100)
		suggestions = append(suggestions, "Increase memory limits or implement more aggressive eviction")
	} else if memoryUtilization > chc.config.CapacityWarningThreshold {
		if status == CacheHealthStatusHealthy {
			status = CacheHealthStatusWarning
		}
		warnings = append(warnings, fmt.Sprintf("High memory utilization: %.1f%%", memoryUtilization*100))
		suggestions = append(suggestions, "Monitor memory usage and consider scaling")
	}

	// Check item utilization
	if itemUtilization > chc.config.CapacityCriticalThreshold {
		if status != CacheHealthStatusCritical {
			status = CacheHealthStatusCritical
			result.Message = fmt.Sprintf("Item count critical: %.1f%%", itemUtilization*100)
		}
		suggestions = append(suggestions, "Increase item limits or optimize data structures")
	} else if itemUtilization > chc.config.CapacityWarningThreshold {
		if status == CacheHealthStatusHealthy {
			status = CacheHealthStatusWarning
		}
		warnings = append(warnings, fmt.Sprintf("High item utilization: %.1f%%", itemUtilization*100))
	}

	result.Status = status
	result.Warnings = warnings
	result.Suggestions = suggestions

	if result.Message == "" {
		result.Message = fmt.Sprintf("Capacity healthy - Memory: %.1f%%, Items: %.1f%%",
			memoryUtilization*100, itemUtilization*100)
	}

	result.Details["memory_usage"] = stats.Memory
	result.Details["memory_utilization"] = memoryUtilization
	result.Details["item_count"] = stats.Size
	result.Details["item_utilization"] = itemUtilization

	return result
}

// checkConsistency performs consistency health check
func (chc *CacheHealthChecker) checkConsistency(ctx context.Context, cache cachecore.Cache, check *CacheHealthCheck) *CacheHealthResult {
	result := &CacheHealthResult{
		CheckName: check.Name,
		CacheName: check.CacheName,
		CheckType: check.CheckType,
		Details:   make(map[string]interface{}),
		Status:    CacheHealthStatusHealthy,
		Message:   "Consistency check not implemented for this cache type",
	}

	// TODO: Implement consistency checks for distributed caches
	// This would involve checking data consistency across replicas

	return result
}

// checkReplication performs replication health check
func (chc *CacheHealthChecker) checkReplication(ctx context.Context, cache cachecore.Cache, check *CacheHealthCheck) *CacheHealthResult {
	result := &CacheHealthResult{
		CheckName: check.Name,
		CacheName: check.CacheName,
		CheckType: check.CheckType,
		Details:   make(map[string]interface{}),
		Status:    CacheHealthStatusHealthy,
		Message:   "Replication check not implemented for this cache type",
	}

	// TODO: Implement replication checks for distributed caches
	// This would involve checking replication status and lag

	return result
}

// checkBasic performs basic health check
func (chc *CacheHealthChecker) checkBasic(ctx context.Context, cache cachecore.Cache, check *CacheHealthCheck) *CacheHealthResult {
	return chc.checkConnectivity(ctx, cache, check)
}

// checkDeep performs deep health check
func (chc *CacheHealthChecker) checkDeep(ctx context.Context, cache cachecore.Cache, check *CacheHealthCheck) *CacheHealthResult {
	// Perform all checks and aggregate results
	connectivityResult := chc.checkConnectivity(ctx, cache, check)
	if connectivityResult.Status != CacheHealthStatusHealthy {
		return connectivityResult
	}

	performanceResult := chc.checkPerformance(ctx, cache, check)
	capacityResult := chc.checkCapacity(ctx, cache, check)

	// Aggregate results
	result := &CacheHealthResult{
		CheckName: check.Name,
		CacheName: check.CacheName,
		CheckType: check.CheckType,
		Details:   make(map[string]interface{}),
		Metrics:   performanceResult.Metrics,
	}

	// Determine worst status
	statuses := []CacheHealthStatus{
		connectivityResult.Status,
		performanceResult.Status,
		capacityResult.Status,
	}

	worstStatus := CacheHealthStatusHealthy
	for _, status := range statuses {
		if chc.isWorse(status, worstStatus) {
			worstStatus = status
		}
	}

	result.Status = worstStatus
	result.Message = "Deep health check completed"

	// Aggregate warnings and suggestions
	result.Warnings = append(result.Warnings, connectivityResult.Warnings...)
	result.Warnings = append(result.Warnings, performanceResult.Warnings...)
	result.Warnings = append(result.Warnings, capacityResult.Warnings...)

	result.Suggestions = append(result.Suggestions, connectivityResult.Suggestions...)
	result.Suggestions = append(result.Suggestions, performanceResult.Suggestions...)
	result.Suggestions = append(result.Suggestions, capacityResult.Suggestions...)

	// Aggregate details
	result.Details["connectivity"] = connectivityResult.Details
	result.Details["performance"] = performanceResult.Details
	result.Details["capacity"] = capacityResult.Details

	return result
}

// handleCheckResult handles the result of a health check
func (chc *CacheHealthChecker) handleCheckResult(check *CacheHealthCheck, result *CacheHealthResult) {
	// Handle state transitions
	wasHealthy := check.LastResult != nil && check.LastResult.Status == CacheHealthStatusHealthy
	isHealthy := result.Status == CacheHealthStatusHealthy

	// Check for failure threshold
	if !isHealthy && check.ConsecutiveFailures >= check.FailureThreshold {
		if chc.config.AlertOnFailure {
			chc.sendFailureAlert(check, result)
		}
	}

	// Check for recovery threshold
	if isHealthy && !wasHealthy && check.ConsecutiveSuccesses >= check.RecoveryThreshold {
		if chc.config.AlertOnRecovery {
			chc.sendRecoveryAlert(check, result)
		}
	}
}

// sendFailureAlert sends a failure alert
func (chc *CacheHealthChecker) sendFailureAlert(check *CacheHealthCheck, result *CacheHealthResult) {
	if chc.logger != nil {
		chc.logger.Error("cache health check failure",
			logger.String("check_name", check.Name),
			logger.String("cache_name", check.CacheName),
			logger.String("check_type", string(check.CheckType)),
			logger.String("status", string(result.Status)),
			logger.String("message", result.Message),
			logger.Int("consecutive_failures", check.ConsecutiveFailures),
		)
	}

	// TODO: Implement alert notification system
}

// sendRecoveryAlert sends a recovery alert
func (chc *CacheHealthChecker) sendRecoveryAlert(check *CacheHealthCheck, result *CacheHealthResult) {
	if chc.logger != nil {
		chc.logger.Info("cache health check recovery",
			logger.String("check_name", check.Name),
			logger.String("cache_name", check.CacheName),
			logger.String("check_type", string(check.CheckType)),
			logger.Int("consecutive_successes", check.ConsecutiveSuccesses),
		)
	}

	// TODO: Implement alert notification system
}

// isWorse compares two health statuses to determine which is worse
func (chc *CacheHealthChecker) isWorse(status1, status2 CacheHealthStatus) bool {
	priority := map[CacheHealthStatus]int{
		CacheHealthStatusHealthy:  0,
		CacheHealthStatusWarning:  1,
		CacheHealthStatusUnknown:  2,
		CacheHealthStatusTimeout:  3,
		CacheHealthStatusCritical: 4,
	}

	return priority[status1] > priority[status2]
}

// generateAggregateMessage generates an aggregate health message for a cache
func (chc *CacheHealthChecker) generateAggregateMessage(cacheName string) string {
	checkCount := 0
	healthyCount := 0
	warningCount := 0
	criticalCount := 0

	for _, result := range chc.results {
		if result.CacheName == cacheName {
			checkCount++
			switch result.Status {
			case CacheHealthStatusHealthy:
				healthyCount++
			case CacheHealthStatusWarning:
				warningCount++
			case CacheHealthStatusCritical:
				criticalCount++
			}
		}
	}

	if criticalCount > 0 {
		return fmt.Sprintf("%d critical, %d warning, %d healthy checks", criticalCount, warningCount, healthyCount)
	} else if warningCount > 0 {
		return fmt.Sprintf("%d warning, %d healthy checks", warningCount, healthyCount)
	} else {
		return fmt.Sprintf("All %d checks healthy", healthyCount)
	}
}

// aggregateDetails aggregates health check details for a cache
func (chc *CacheHealthChecker) aggregateDetails(cacheName string) map[string]interface{} {
	details := make(map[string]interface{})

	for _, result := range chc.results {
		if result.CacheName == cacheName {
			details[string(result.CheckType)] = result
		}
	}

	return details
}
