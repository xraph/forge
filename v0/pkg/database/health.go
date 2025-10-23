package database

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// HealthChecker provides health checking functionality for database connections
type HealthChecker interface {
	// Connection health checks
	CheckConnection(ctx context.Context, conn Connection) HealthCheckResult
	CheckAllConnections(ctx context.Context, manager DatabaseManager) HealthCheckResult

	// Periodic health checking
	StartPeriodicHealthCheck(ctx context.Context, manager DatabaseManager, interval time.Duration)
	StopPeriodicHealthCheck()

	// Health check configuration
	SetTimeout(timeout time.Duration)
	SetHealthyThreshold(threshold int)
	SetUnhealthyThreshold(threshold int)

	// Health check results
	GetLastResult(connectionName string) *HealthCheckResult
	GetAllResults() map[string]*HealthCheckResult
}

// HealthCheckResult represents the result of a health check
type HealthCheckResult struct {
	ConnectionName    string                 `json:"connection_name"`
	Healthy           bool                   `json:"healthy"`
	Status            common.HealthStatus    `json:"status"`
	Message           string                 `json:"message"`
	Details           map[string]interface{} `json:"details"`
	Timestamp         time.Time              `json:"timestamp"`
	Duration          time.Duration          `json:"duration"`
	Error             error                  `json:"error,omitempty"`
	ConsecutivePasses int                    `json:"consecutive_passes"`
	ConsecutiveFails  int                    `json:"consecutive_fails"`
	TotalChecks       int64                  `json:"total_checks"`
	TotalFailures     int64                  `json:"total_failures"`
}

// DatabaseHealthChecker implements HealthChecker
type DatabaseHealthChecker struct {
	timeout            time.Duration
	healthyThreshold   int // Number of consecutive passes required to be healthy
	unhealthyThreshold int // Number of consecutive failures to be unhealthy
	results            map[string]*HealthCheckResult
	ticker             *time.Ticker
	stopCh             chan struct{}
	logger             common.Logger
	metrics            common.Metrics
	mu                 sync.RWMutex
	running            bool
}

// NewDatabaseHealthChecker creates a new database health checker
func NewDatabaseHealthChecker(logger common.Logger, metrics common.Metrics) HealthChecker {
	return &DatabaseHealthChecker{
		timeout:            5 * time.Second,
		healthyThreshold:   1, // Consider healthy after 1 success
		unhealthyThreshold: 3, // Consider unhealthy after 3 failures
		results:            make(map[string]*HealthCheckResult),
		stopCh:             make(chan struct{}),
		logger:             logger,
		metrics:            metrics,
	}
}

// SetTimeout sets the health check timeout
func (hc *DatabaseHealthChecker) SetTimeout(timeout time.Duration) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.timeout = timeout
}

// SetHealthyThreshold sets the number of consecutive passes required to be healthy
func (hc *DatabaseHealthChecker) SetHealthyThreshold(threshold int) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.healthyThreshold = threshold
}

// SetUnhealthyThreshold sets the number of consecutive failures to be unhealthy
func (hc *DatabaseHealthChecker) SetUnhealthyThreshold(threshold int) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.unhealthyThreshold = threshold
}

// CheckConnection performs a health check on a single connection
func (hc *DatabaseHealthChecker) CheckConnection(ctx context.Context, conn Connection) HealthCheckResult {
	start := time.Now()
	connectionName := conn.Name()

	// Create context with timeout
	checkCtx, cancel := context.WithTimeout(ctx, hc.timeout)
	defer cancel()

	// Get previous result for tracking consecutive passes/fails
	hc.mu.RLock()
	previousResult := hc.results[connectionName]
	hc.mu.RUnlock()

	result := HealthCheckResult{
		ConnectionName: connectionName,
		Timestamp:      start,
		TotalChecks:    1,
	}

	// Copy previous stats if they exist
	if previousResult != nil {
		result.TotalChecks = previousResult.TotalChecks + 1
		result.TotalFailures = previousResult.TotalFailures
		result.ConsecutivePasses = previousResult.ConsecutivePasses
		result.ConsecutiveFails = previousResult.ConsecutiveFails
	}

	// Perform the actual health check
	details := make(map[string]interface{})

	// Check if connection is connected
	if !conn.IsConnected() {
		result.Healthy = false
		result.Status = common.HealthStatusUnhealthy
		result.Message = "Connection is not established"
		result.Error = fmt.Errorf("connection not established")
		result.ConsecutiveFails++
		result.ConsecutivePasses = 0
		result.TotalFailures++
	} else {
		// Perform ping test
		if err := conn.Ping(checkCtx); err != nil {
			result.Healthy = false
			result.Status = common.HealthStatusUnhealthy
			result.Message = "Ping test failed"
			result.Error = err
			result.ConsecutiveFails++
			result.ConsecutivePasses = 0
			result.TotalFailures++

			details["ping_error"] = err.Error()
		} else {
			// Health check passed
			result.Healthy = true
			result.Status = common.HealthStatusHealthy
			result.Message = "Connection is healthy"
			result.ConsecutivePasses++
			result.ConsecutiveFails = 0

			// Add connection stats to details
			stats := conn.Stats()
			details["connected_at"] = stats.ConnectedAt
			details["last_ping"] = stats.LastPing
			details["ping_duration"] = stats.PingDuration
			details["transaction_count"] = stats.TransactionCount
			details["query_count"] = stats.QueryCount
			details["error_count"] = stats.ErrorCount
			details["open_connections"] = stats.OpenConnections
			details["idle_connections"] = stats.IdleConnections
		}
	}

	// Apply thresholds to determine final status
	if result.Healthy && result.ConsecutivePasses >= hc.healthyThreshold {
		result.Status = common.HealthStatusHealthy
	} else if !result.Healthy && result.ConsecutiveFails >= hc.unhealthyThreshold {
		result.Status = common.HealthStatusUnhealthy
	} else if result.ConsecutiveFails > 0 && result.ConsecutiveFails < hc.unhealthyThreshold {
		result.Status = common.HealthStatusDegraded
		result.Message = fmt.Sprintf("Connection degraded (%d consecutive failures)", result.ConsecutiveFails)
	}

	result.Duration = time.Since(start)
	result.Details = details

	// Store the result
	hc.mu.Lock()
	hc.results[connectionName] = &result
	hc.mu.Unlock()

	// Log the result
	if result.Healthy {
		hc.logger.Debug("database health check passed",
			logger.String("connection", connectionName),
			logger.Duration("duration", result.Duration),
			logger.Int("consecutive_passes", result.ConsecutivePasses),
		)
	} else {
		hc.logger.Warn("database health check failed",
			logger.String("connection", connectionName),
			logger.String("message", result.Message),
			logger.Duration("duration", result.Duration),
			logger.Int("consecutive_fails", result.ConsecutiveFails),
			logger.Error(result.Error),
		)
	}

	// Record metrics
	if hc.metrics != nil {
		tags := []string{"connection", connectionName, "type", conn.Type()}

		hc.metrics.Counter("forge.database.health_checks_total", tags...).Inc()
		hc.metrics.Histogram("forge.database.health_check_duration", tags...).Observe(result.Duration.Seconds())

		if result.Healthy {
			hc.metrics.Gauge("forge.database.connection_healthy", tags...).Set(1)
		} else {
			hc.metrics.Gauge("forge.database.connection_healthy", tags...).Set(0)
			hc.metrics.Counter("forge.database.health_checks_failed", tags...).Inc()
		}

		hc.metrics.Gauge("forge.database.consecutive_failures", tags...).Set(float64(result.ConsecutiveFails))
		hc.metrics.Gauge("forge.database.consecutive_passes", tags...).Set(float64(result.ConsecutivePasses))
	}

	return result
}

// CheckAllConnections performs health checks on all connections
func (hc *DatabaseHealthChecker) CheckAllConnections(ctx context.Context, manager DatabaseManager) HealthCheckResult {
	start := time.Now()
	connectionNames := manager.ListConnections()

	overallResult := HealthCheckResult{
		ConnectionName: "all_connections",
		Timestamp:      start,
		Healthy:        true,
		Status:         common.HealthStatusHealthy,
		Message:        "All connections are healthy",
		Details:        make(map[string]interface{}),
	}

	results := make(map[string]HealthCheckResult)
	var healthyCount, unhealthyCount, degradedCount int

	// Check each connection
	for _, connectionName := range connectionNames {
		conn, err := manager.GetConnection(connectionName)
		if err != nil {
			hc.logger.Error("failed to get connection for health check",
				logger.String("connection", connectionName),
				logger.Error(err),
			)
			unhealthyCount++
			continue
		}

		result := hc.CheckConnection(ctx, conn)
		results[connectionName] = result

		switch result.Status {
		case common.HealthStatusHealthy:
			healthyCount++
		case common.HealthStatusDegraded:
			degradedCount++
		case common.HealthStatusUnhealthy:
			unhealthyCount++
			overallResult.Healthy = false
		}
	}

	// Determine overall status
	totalConnections := len(connectionNames)
	if unhealthyCount > 0 {
		overallResult.Status = common.HealthStatusUnhealthy
		overallResult.Message = fmt.Sprintf("%d of %d connections are unhealthy", unhealthyCount, totalConnections)
		overallResult.Healthy = false
	} else if degradedCount > 0 {
		overallResult.Status = common.HealthStatusDegraded
		overallResult.Message = fmt.Sprintf("%d of %d connections are degraded", degradedCount, totalConnections)
	}

	overallResult.Duration = time.Since(start)
	overallResult.Details = map[string]interface{}{
		"total_connections":     totalConnections,
		"healthy_connections":   healthyCount,
		"degraded_connections":  degradedCount,
		"unhealthy_connections": unhealthyCount,
		"connection_results":    results,
	}

	// Log overall result
	if overallResult.Healthy {
		hc.logger.Info("all database connections are healthy",
			logger.Int("total", totalConnections),
			logger.Duration("duration", overallResult.Duration),
		)
	} else {
		hc.logger.Error("database health check failed",
			logger.String("message", overallResult.Message),
			logger.Int("unhealthy", unhealthyCount),
			logger.Int("degraded", degradedCount),
			logger.Int("total", totalConnections),
			logger.Duration("duration", overallResult.Duration),
		)
	}

	// Record overall metrics
	if hc.metrics != nil {
		hc.metrics.Counter("forge.database.health_checks_overall_total").Inc()
		hc.metrics.Histogram("forge.database.health_check_overall_duration").Observe(overallResult.Duration.Seconds())
		hc.metrics.Gauge("forge.database.healthy_connections").Set(float64(healthyCount))
		hc.metrics.Gauge("forge.database.degraded_connections").Set(float64(degradedCount))
		hc.metrics.Gauge("forge.database.unhealthy_connections").Set(float64(unhealthyCount))

		if overallResult.Healthy {
			hc.metrics.Gauge("forge.database.overall_healthy").Set(1)
		} else {
			hc.metrics.Gauge("forge.database.overall_healthy").Set(0)
			hc.metrics.Counter("forge.database.health_checks_overall_failed").Inc()
		}
	}

	return overallResult
}

// StartPeriodicHealthCheck starts periodic health checking
func (hc *DatabaseHealthChecker) StartPeriodicHealthCheck(ctx context.Context, manager DatabaseManager, interval time.Duration) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	if hc.running {
		hc.logger.Warn("periodic health check is already running")
		return
	}

	hc.ticker = time.NewTicker(interval)
	hc.running = true

	go func() {
		hc.logger.Info("starting periodic database health checks",
			logger.Duration("interval", interval),
		)

		for {
			select {
			case <-hc.ticker.C:
				// Perform health check with timeout context
				checkCtx, cancel := context.WithTimeout(ctx, hc.timeout*2) // Allow extra time for all connections
				hc.CheckAllConnections(checkCtx, manager)
				cancel()

			case <-hc.stopCh:
				hc.logger.Info("stopping periodic database health checks")
				return

			case <-ctx.Done():
				hc.logger.Info("context cancelled, stopping periodic database health checks")
				return
			}
		}
	}()
}

// StopPeriodicHealthCheck stops periodic health checking
func (hc *DatabaseHealthChecker) StopPeriodicHealthCheck() {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	if !hc.running {
		return
	}

	if hc.ticker != nil {
		hc.ticker.Stop()
		hc.ticker = nil
	}

	close(hc.stopCh)
	hc.stopCh = make(chan struct{}) // Reset for next use
	hc.running = false
}

// GetLastResult returns the last health check result for a connection
func (hc *DatabaseHealthChecker) GetLastResult(connectionName string) *HealthCheckResult {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	if result, exists := hc.results[connectionName]; exists {
		// Return a copy to avoid race conditions
		resultCopy := *result
		return &resultCopy
	}

	return nil
}

// GetAllResults returns all health check results
func (hc *DatabaseHealthChecker) GetAllResults() map[string]*HealthCheckResult {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	results := make(map[string]*HealthCheckResult)
	for name, result := range hc.results {
		// Return copies to avoid race conditions
		resultCopy := *result
		results[name] = &resultCopy
	}

	return results
}

// // HealthCheckConfig represents health check configuration
// type HealthCheckConfig struct {
// 	Enabled            bool          `yaml:"enabled" json:"enabled"`
// 	Interval           time.Duration `yaml:"interval" json:"interval"`
// 	Timeout            time.Duration `yaml:"timeout" json:"timeout"`
// 	HealthyThreshold   int           `yaml:"healthy_threshold" json:"healthy_threshold"`
// 	UnhealthyThreshold int           `yaml:"unhealthy_threshold" json:"unhealthy_threshold"`
// }

// DefaultHealthCheckConfig returns default health check configuration
func DefaultHealthCheckConfig() *HealthCheckConfig {
	return &HealthCheckConfig{
		Enabled:            true,
		Interval:           30 * time.Second,
		Timeout:            5 * time.Second,
		HealthyThreshold:   1,
		UnhealthyThreshold: 3,
	}
}

// ConfigureHealthChecker configures a health checker with the given configuration
func ConfigureHealthChecker(checker HealthChecker, config *HealthCheckConfig) {
	if config == nil {
		config = DefaultHealthCheckConfig()
	}

	checker.SetTimeout(config.Timeout)
	checker.SetHealthyThreshold(config.HealthyThreshold)
	checker.SetUnhealthyThreshold(config.UnhealthyThreshold)
}

// HealthCheckService provides a service wrapper for database health checking
type HealthCheckService struct {
	checker HealthChecker
	manager DatabaseManager
	config  *HealthCheckConfig
	logger  common.Logger
}

// NewHealthCheckService creates a new health check service
func NewHealthCheckService(manager DatabaseManager, config *HealthCheckConfig, logger common.Logger, metrics common.Metrics) *HealthCheckService {
	if config == nil {
		config = DefaultHealthCheckConfig()
	}

	checker := NewDatabaseHealthChecker(logger, metrics)
	ConfigureHealthChecker(checker, config)

	return &HealthCheckService{
		checker: checker,
		manager: manager,
		config:  config,
		logger:  logger,
	}
}

// Name returns the service name
func (hcs *HealthCheckService) Name() string {
	return "database-health-service"
}

// Dependencies returns service dependencies
func (hcs *HealthCheckService) Dependencies() []string {
	return []string{"database-manager", "logger", "metrics"}
}

// OnStart starts the health check service
func (hcs *HealthCheckService) Start(ctx context.Context) error {
	if hcs.config.Enabled {
		hcs.checker.StartPeriodicHealthCheck(ctx, hcs.manager, hcs.config.Interval)
		hcs.logger.Info("database health check service started",
			logger.Duration("interval", hcs.config.Interval),
			logger.Duration("timeout", hcs.config.Timeout),
		)
	} else {
		hcs.logger.Info("database health check service disabled")
	}
	return nil
}

// OnStop stops the health check service
func (hcs *HealthCheckService) Stop(ctx context.Context) error {
	hcs.checker.StopPeriodicHealthCheck()
	hcs.logger.Info("database health check service stopped")
	return nil
}

// OnHealthCheck performs a health check
func (hcs *HealthCheckService) OnHealthCheck(ctx context.Context) error {
	result := hcs.checker.CheckAllConnections(ctx, hcs.manager)
	if !result.Healthy {
		return fmt.Errorf("database health check failed: %s", result.Message)
	}
	return nil
}

// GetChecker returns the underlying health checker
func (hcs *HealthCheckService) GetChecker() HealthChecker {
	return hcs.checker
}
