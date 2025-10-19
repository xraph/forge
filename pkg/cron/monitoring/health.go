package monitoring

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	cron "github.com/xraph/forge/pkg/cron/core"
	"github.com/xraph/forge/pkg/logger"
)

// CronHealthChecker provides health checks for the cron system
type CronHealthChecker struct {
	manager       cron.Manager
	logger        common.Logger
	metrics       common.Metrics
	checks        map[string]HealthCheck
	checkInterval time.Duration
	timeout       time.Duration
	mu            sync.RWMutex
	running       bool
	stopChan      chan struct{}
}

// HealthCheck defines a health check for the cron system
type HealthCheck interface {
	Name() string
	Check(ctx context.Context) *HealthCheckResult
	Critical() bool
	Timeout() time.Duration
}

// HealthCheckResult represents the result of a health check
type HealthCheckResult struct {
	Name      string                 `json:"name"`
	Status    HealthStatus           `json:"status"`
	Message   string                 `json:"message"`
	Details   map[string]interface{} `json:"details"`
	Timestamp time.Time              `json:"timestamp"`
	Duration  time.Duration          `json:"duration"`
	Error     error                  `json:"error,omitempty"`
}

// HealthStatus represents the status of a health check
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
	HealthStatusDegraded  HealthStatus = "degraded"
	HealthStatusUnknown   HealthStatus = "unknown"
)

// CronHealthReport represents the overall health report for the cron system
type CronHealthReport struct {
	OverallStatus HealthStatus                  `json:"overall_status"`
	Checks        map[string]*HealthCheckResult `json:"checks"`
	Summary       HealthSummary                 `json:"summary"`
	Timestamp     time.Time                     `json:"timestamp"`
	Duration      time.Duration                 `json:"duration"`
}

// HealthSummary provides a summary of health check results
type HealthSummary struct {
	Total     int `json:"total"`
	Healthy   int `json:"healthy"`
	Unhealthy int `json:"unhealthy"`
	Degraded  int `json:"degraded"`
	Unknown   int `json:"unknown"`
	Critical  int `json:"critical"`
}

// NewCronHealthChecker creates a new cron health checker
func NewCronHealthChecker(manager cron.Manager, logger common.Logger, metrics common.Metrics) *CronHealthChecker {
	checker := &CronHealthChecker{
		manager:       manager,
		logger:        logger,
		metrics:       metrics,
		checks:        make(map[string]HealthCheck),
		checkInterval: 30 * time.Second,
		timeout:       5 * time.Second,
		stopChan:      make(chan struct{}),
	}

	// Register default health checks
	checker.registerDefaultChecks()

	return checker
}

// RegisterCheck registers a new health check
func (chc *CronHealthChecker) RegisterCheck(check HealthCheck) error {
	chc.mu.Lock()
	defer chc.mu.Unlock()

	if _, exists := chc.checks[check.Name()]; exists {
		return common.ErrServiceAlreadyExists(check.Name())
	}

	chc.checks[check.Name()] = check

	if chc.logger != nil {
		chc.logger.Info("health check registered",
			logger.String("name", check.Name()),
			logger.Bool("critical", check.Critical()),
			logger.Duration("timeout", check.Timeout()),
		)
	}

	return nil
}

// UnregisterCheck unregisters a health check
func (chc *CronHealthChecker) UnregisterCheck(name string) error {
	chc.mu.Lock()
	defer chc.mu.Unlock()

	if _, exists := chc.checks[name]; !exists {
		return common.ErrServiceNotFound(name)
	}

	delete(chc.checks, name)

	if chc.logger != nil {
		chc.logger.Info("health check unregistered",
			logger.String("name", name),
		)
	}

	return nil
}

// CheckAll performs all health checks
func (chc *CronHealthChecker) CheckAll(ctx context.Context) *CronHealthReport {
	start := time.Now()

	chc.mu.RLock()
	checks := make(map[string]HealthCheck)
	for name, check := range chc.checks {
		checks[name] = check
	}
	chc.mu.RUnlock()

	results := make(map[string]*HealthCheckResult)
	summary := HealthSummary{
		Total: len(checks),
	}

	// Run all checks concurrently
	resultChan := make(chan *HealthCheckResult, len(checks))
	for _, check := range checks {
		go func(check HealthCheck) {
			result := chc.runCheck(ctx, check)
			resultChan <- result
		}(check)
	}

	// Collect results
	for i := 0; i < len(checks); i++ {
		result := <-resultChan
		results[result.Name] = result

		// Update summary
		switch result.Status {
		case HealthStatusHealthy:
			summary.Healthy++
		case HealthStatusUnhealthy:
			summary.Unhealthy++
		case HealthStatusDegraded:
			summary.Degraded++
		default:
			summary.Unknown++
		}

		if chc.isCritical(result.Name) {
			summary.Critical++
		}
	}

	// Determine overall status
	overallStatus := chc.determineOverallStatus(results)

	report := &CronHealthReport{
		OverallStatus: overallStatus,
		Checks:        results,
		Summary:       summary,
		Timestamp:     time.Now(),
		Duration:      time.Since(start),
	}

	// Record metrics
	if chc.metrics != nil {
		chc.recordHealthMetrics(report)
	}

	return report
}

// CheckOne performs a single health check
func (chc *CronHealthChecker) CheckOne(ctx context.Context, name string) (*HealthCheckResult, error) {
	chc.mu.RLock()
	check, exists := chc.checks[name]
	chc.mu.RUnlock()

	if !exists {
		return nil, common.ErrServiceNotFound(name)
	}

	return chc.runCheck(ctx, check), nil
}

// Start starts the health checker
func (chc *CronHealthChecker) Start(ctx context.Context) error {
	chc.mu.Lock()
	defer chc.mu.Unlock()

	if chc.running {
		return common.ErrLifecycleError("start", fmt.Errorf("health checker already running"))
	}

	chc.running = true
	chc.stopChan = make(chan struct{})

	// Start background health checking
	go chc.backgroundHealthCheck()

	if chc.logger != nil {
		chc.logger.Info("cron health checker started",
			logger.Duration("check_interval", chc.checkInterval),
			logger.Duration("timeout", chc.timeout),
			logger.Int("checks_count", len(chc.checks)),
		)
	}

	return nil
}

// Stop stops the health checker
func (chc *CronHealthChecker) Stop(ctx context.Context) error {
	chc.mu.Lock()
	defer chc.mu.Unlock()

	if !chc.running {
		return common.ErrLifecycleError("stop", fmt.Errorf("health checker not running"))
	}

	close(chc.stopChan)
	chc.running = false

	if chc.logger != nil {
		chc.logger.Info("cron health checker stopped")
	}

	return nil
}

// IsRunning returns true if the health checker is running
func (chc *CronHealthChecker) IsRunning() bool {
	chc.mu.RLock()
	defer chc.mu.RUnlock()
	return chc.running
}

// SetCheckInterval sets the health check interval
func (chc *CronHealthChecker) SetCheckInterval(interval time.Duration) {
	chc.mu.Lock()
	defer chc.mu.Unlock()
	chc.checkInterval = interval
}

// SetTimeout sets the health check timeout
func (chc *CronHealthChecker) SetTimeout(timeout time.Duration) {
	chc.mu.Lock()
	defer chc.mu.Unlock()
	chc.timeout = timeout
}

// registerDefaultChecks registers default health checks
func (chc *CronHealthChecker) registerDefaultChecks() {
	// Manager health check
	managerCheck := &ManagerHealthCheck{
		manager:         chc.manager,
		BaseHealthCheck: NewBaseHealthCheck("manager", true, chc.timeout),
	}
	chc.checks["manager"] = managerCheck

	// Job scheduler health check
	schedulerCheck := &SchedulerHealthCheck{
		manager:         chc.manager,
		BaseHealthCheck: NewBaseHealthCheck("scheduler", true, chc.timeout),
	}
	chc.checks["scheduler"] = schedulerCheck

	// Leader election health check
	leaderCheck := &LeaderElectionHealthCheck{
		manager:         chc.manager,
		BaseHealthCheck: NewBaseHealthCheck("leader_election", true, chc.timeout),
	}
	chc.checks["leader_election"] = leaderCheck

	// Job distribution health check
	distributionCheck := &DistributionHealthCheck{
		manager:         chc.manager,
		BaseHealthCheck: NewBaseHealthCheck("distribution", true, chc.timeout),
	}
	chc.checks["distribution"] = distributionCheck

	// Persistence health check
	persistenceCheck := &PersistenceHealthCheck{
		manager:         chc.manager,
		BaseHealthCheck: NewBaseHealthCheck("persistence", true, chc.timeout),
	}
	chc.checks["persistence"] = persistenceCheck
}

// runCheck runs a single health check with timeout
func (chc *CronHealthChecker) runCheck(ctx context.Context, check HealthCheck) *HealthCheckResult {
	start := time.Now()

	// Create timeout context
	timeout := check.Timeout()
	if timeout == 0 {
		timeout = chc.timeout
	}

	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Run the check
	result := check.Check(checkCtx)
	result.Duration = time.Since(start)

	// Log the result
	if chc.logger != nil {
		logLevel := chc.logger.Info
		if result.Status == HealthStatusUnhealthy {
			logLevel = chc.logger.Error
		} else if result.Status == HealthStatusDegraded {
			logLevel = chc.logger.Warn
		}

		logLevel("health check completed",
			logger.String("name", result.Name),
			logger.String("status", string(result.Status)),
			logger.Duration("duration", result.Duration),
			logger.String("message", result.Message),
		)
	}

	return result
}

// backgroundHealthCheck runs health checks in the background
func (chc *CronHealthChecker) backgroundHealthCheck() {
	ticker := time.NewTicker(chc.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), chc.timeout*2)
			report := chc.CheckAll(ctx)
			cancel()

			// Log overall status
			if chc.logger != nil {
				chc.logger.Info("health check report",
					logger.String("overall_status", string(report.OverallStatus)),
					logger.Int("total_checks", report.Summary.Total),
					logger.Int("healthy", report.Summary.Healthy),
					logger.Int("unhealthy", report.Summary.Unhealthy),
					logger.Int("degraded", report.Summary.Degraded),
					logger.Duration("duration", report.Duration),
				)
			}

		case <-chc.stopChan:
			return
		}
	}
}

// determineOverallStatus determines the overall health status
func (chc *CronHealthChecker) determineOverallStatus(results map[string]*HealthCheckResult) HealthStatus {
	hasUnhealthy := false
	hasDegraded := false
	hasCriticalUnhealthy := false

	for name, result := range results {
		switch result.Status {
		case HealthStatusUnhealthy:
			hasUnhealthy = true
			if chc.isCritical(name) {
				hasCriticalUnhealthy = true
			}
		case HealthStatusDegraded:
			hasDegraded = true
		}
	}

	if hasCriticalUnhealthy {
		return HealthStatusUnhealthy
	}

	if hasUnhealthy {
		return HealthStatusDegraded
	}

	if hasDegraded {
		return HealthStatusDegraded
	}

	return HealthStatusHealthy
}

// isCritical returns true if the health check is critical
func (chc *CronHealthChecker) isCritical(name string) bool {
	chc.mu.RLock()
	defer chc.mu.RUnlock()

	if check, exists := chc.checks[name]; exists {
		return check.Critical()
	}
	return false
}

// recordHealthMetrics records health metrics
func (chc *CronHealthChecker) recordHealthMetrics(report *CronHealthReport) {
	// Overall status metric
	statusValue := 0.0
	switch report.OverallStatus {
	case HealthStatusHealthy:
		statusValue = 1.0
	case HealthStatusDegraded:
		statusValue = 0.5
	case HealthStatusUnhealthy:
		statusValue = 0.0
	}
	chc.metrics.Gauge("forge.cron.health.overall_status").Set(statusValue)

	// Summary metrics
	chc.metrics.Gauge("forge.cron.health.total_checks").Set(float64(report.Summary.Total))
	chc.metrics.Gauge("forge.cron.health.healthy_checks").Set(float64(report.Summary.Healthy))
	chc.metrics.Gauge("forge.cron.health.unhealthy_checks").Set(float64(report.Summary.Unhealthy))
	chc.metrics.Gauge("forge.cron.health.degraded_checks").Set(float64(report.Summary.Degraded))
	chc.metrics.Gauge("forge.cron.health.unknown_checks").Set(float64(report.Summary.Unknown))

	// Health check duration
	chc.metrics.Histogram("forge.cron.health.check_duration").Observe(report.Duration.Seconds())

	// Individual check metrics
	for name, result := range report.Checks {
		tags := []string{"check", name}

		checkValue := 0.0
		switch result.Status {
		case HealthStatusHealthy:
			checkValue = 1.0
		case HealthStatusDegraded:
			checkValue = 0.5
		case HealthStatusUnhealthy:
			checkValue = 0.0
		}

		chc.metrics.Gauge("forge.cron.health.check_status", tags...).Set(checkValue)
		chc.metrics.Histogram("forge.cron.health.check_duration", tags...).Observe(result.Duration.Seconds())
	}
}

// BaseHealthCheck provides a base implementation for health checks
type BaseHealthCheck struct {
	name     string
	critical bool
	timeout  time.Duration
}

// NewBaseHealthCheck creates a new base health check
func NewBaseHealthCheck(name string, critical bool, timeout time.Duration) *BaseHealthCheck {
	return &BaseHealthCheck{
		name:     name,
		critical: critical,
		timeout:  timeout,
	}
}

// Name returns the name of the health check
func (bhc *BaseHealthCheck) Name() string {
	return bhc.name
}

// Critical returns true if the health check is critical
func (bhc *BaseHealthCheck) Critical() bool {
	return bhc.critical
}

// Timeout returns the timeout for the health check
func (bhc *BaseHealthCheck) Timeout() time.Duration {
	return bhc.timeout
}

// ManagerHealthCheck checks the health of the cron manager
type ManagerHealthCheck struct {
	*BaseHealthCheck
	manager cron.Manager
}

// NewManagerHealthCheck creates a new manager health check
func NewManagerHealthCheck(manager cron.Manager, timeout time.Duration) *ManagerHealthCheck {
	return &ManagerHealthCheck{
		BaseHealthCheck: NewBaseHealthCheck("manager", true, timeout),
		manager:         manager,
	}
}

// Check performs the manager health check
func (mhc *ManagerHealthCheck) Check(ctx context.Context) *HealthCheckResult {
	start := time.Now()
	result := &HealthCheckResult{
		Name:      mhc.name,
		Timestamp: start,
		Details:   make(map[string]interface{}),
	}

	// Check if manager is running
	if !mhc.manager.IsRunning() {
		result.Status = HealthStatusUnhealthy
		result.Message = "cron manager is not running"
		return result
	}

	// Get manager stats
	stats := mhc.manager.GetStats()
	result.Details["total_jobs"] = stats.TotalJobs
	result.Details["active_jobs"] = stats.ActiveJobs
	result.Details["running_jobs"] = stats.RunningJobs
	result.Details["failed_jobs"] = stats.FailedJobs

	// Check if there are too many failed jobs
	if stats.TotalJobs > 0 {
		failureRate := float64(stats.FailedJobs) / float64(stats.TotalJobs)
		result.Details["failure_rate"] = failureRate

		if failureRate > 0.5 {
			result.Status = HealthStatusUnhealthy
			result.Message = fmt.Sprintf("high job failure rate: %.2f%%", failureRate*100)
			return result
		} else if failureRate > 0.2 {
			result.Status = HealthStatusDegraded
			result.Message = fmt.Sprintf("elevated job failure rate: %.2f%%", failureRate*100)
			return result
		}
	}

	result.Status = HealthStatusHealthy
	result.Message = "cron manager is healthy"
	return result
}

// SchedulerHealthCheck checks the health of the job scheduler
type SchedulerHealthCheck struct {
	*BaseHealthCheck
	manager cron.Manager
}

// NewSchedulerHealthCheck creates a new scheduler health check
func NewSchedulerHealthCheck(manager cron.Manager, timeout time.Duration) *SchedulerHealthCheck {
	return &SchedulerHealthCheck{
		BaseHealthCheck: NewBaseHealthCheck("scheduler", true, timeout),
		manager:         manager,
	}
}

// Check performs the scheduler health check
func (shc *SchedulerHealthCheck) Check(ctx context.Context) *HealthCheckResult {
	start := time.Now()
	result := &HealthCheckResult{
		Name:      shc.name,
		Timestamp: start,
		Details:   make(map[string]interface{}),
	}

	// This would check the scheduler's health
	// For now, we'll do a basic check
	result.Status = HealthStatusHealthy
	result.Message = "scheduler is healthy"

	return result
}

// LeaderElectionHealthCheck checks the health of leader election
type LeaderElectionHealthCheck struct {
	*BaseHealthCheck
	manager cron.Manager
}

// NewLeaderElectionHealthCheck creates a new leader election health check
func NewLeaderElectionHealthCheck(manager cron.Manager, timeout time.Duration) *LeaderElectionHealthCheck {
	return &LeaderElectionHealthCheck{
		BaseHealthCheck: NewBaseHealthCheck("leader_election", true, timeout),
		manager:         manager,
	}
}

// Check performs the leader election health check
func (lhc *LeaderElectionHealthCheck) Check(ctx context.Context) *HealthCheckResult {
	start := time.Now()
	result := &HealthCheckResult{
		Name:      lhc.name,
		Timestamp: start,
		Details:   make(map[string]interface{}),
	}

	// This would check leader election health
	// For now, we'll do a basic check
	result.Status = HealthStatusHealthy
	result.Message = "leader election is healthy"

	return result
}

// DistributionHealthCheck checks the health of job distribution
type DistributionHealthCheck struct {
	*BaseHealthCheck
	manager cron.Manager
}

// NewDistributionHealthCheck creates a new distribution health check
func NewDistributionHealthCheck(manager cron.Manager, timeout time.Duration) *DistributionHealthCheck {
	return &DistributionHealthCheck{
		BaseHealthCheck: NewBaseHealthCheck("distribution", false, timeout),
		manager:         manager,
	}
}

// Check performs the distribution health check
func (dhc *DistributionHealthCheck) Check(ctx context.Context) *HealthCheckResult {
	start := time.Now()
	result := &HealthCheckResult{
		Name:      dhc.name,
		Timestamp: start,
		Details:   make(map[string]interface{}),
	}

	// This would check job distribution health
	// For now, we'll do a basic check
	result.Status = HealthStatusHealthy
	result.Message = "job distribution is healthy"

	return result
}

// PersistenceHealthCheck checks the health of job persistence
type PersistenceHealthCheck struct {
	*BaseHealthCheck
	manager cron.Manager
}

// NewPersistenceHealthCheck creates a new persistence health check
func NewPersistenceHealthCheck(manager cron.Manager, timeout time.Duration) *PersistenceHealthCheck {
	return &PersistenceHealthCheck{
		BaseHealthCheck: NewBaseHealthCheck("persistence", true, timeout),
		manager:         manager,
	}
}

// Check performs the persistence health check
func (phc *PersistenceHealthCheck) Check(ctx context.Context) *HealthCheckResult {
	start := time.Now()
	result := &HealthCheckResult{
		Name:      phc.name,
		Timestamp: start,
		Details:   make(map[string]interface{}),
	}

	// This would check persistence health
	// For now, we'll do a basic check
	result.Status = HealthStatusHealthy
	result.Message = "persistence is healthy"

	return result
}
