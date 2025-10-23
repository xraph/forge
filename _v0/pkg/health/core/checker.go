package core

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
)

// HealthChecker implements comprehensive health monitoring for all services
type HealthChecker struct {
	checks      map[string]HealthCheck
	aggregator  *SmartAggregator
	subscribers []HealthCallback

	// Configuration
	config    *HealthCheckerConfig
	logger    common.Logger
	metrics   common.Metrics
	container common.Container

	// State management
	started    bool
	stopping   bool
	lastReport *HealthReport

	// Async execution
	stopCh   chan struct{}
	resultCh chan *HealthResult
	reportCh chan *HealthReport

	// Synchronization
	mu          sync.RWMutex
	startTime   time.Time
	hostname    string
	version     string
	environment string
}

// HealthCheckerConfig contains configuration for the health checker
type HealthCheckerConfig struct {
	CheckInterval          time.Duration
	ReportInterval         time.Duration
	EnableAutoDiscovery    bool
	EnablePersistence      bool
	EnableAlerting         bool
	MaxConcurrentChecks    int
	DefaultTimeout         time.Duration
	CriticalServices       []string
	DegradedThreshold      float64
	UnhealthyThreshold     float64
	EnableSmartAggregation bool
	EnablePrediction       bool
	HistorySize            int
	Tags                   map[string]string
}

// DefaultHealthCheckerConfig returns default configuration
func DefaultHealthCheckerConfig() *HealthCheckerConfig {
	return &HealthCheckerConfig{
		CheckInterval:          30 * time.Second,
		ReportInterval:         60 * time.Second,
		EnableAutoDiscovery:    true,
		EnablePersistence:      false,
		EnableAlerting:         false,
		MaxConcurrentChecks:    10,
		DefaultTimeout:         5 * time.Second,
		CriticalServices:       []string{},
		DegradedThreshold:      0.1,
		UnhealthyThreshold:     0.05,
		EnableSmartAggregation: true,
		EnablePrediction:       false,
		HistorySize:            100,
		Tags:                   make(map[string]string),
	}
}

// NewHealthChecker creates a new health checker
func NewHealthChecker(config *HealthCheckerConfig, logger common.Logger, metrics common.Metrics, container common.Container) common.HealthChecker {
	if config == nil {
		config = DefaultHealthCheckerConfig()
	}

	// Create aggregator
	aggregatorConfig := &AggregatorConfig{
		CriticalServices:   config.CriticalServices,
		DegradedThreshold:  config.DegradedThreshold,
		UnhealthyThreshold: config.UnhealthyThreshold,
		EnableDependencies: true,
		Weights:            make(map[string]float64),
	}

	var aggregator *SmartAggregator
	if config.EnableSmartAggregation {
		aggregator = NewSmartAggregator(aggregatorConfig)
		aggregator.SetMaxHistorySize(config.HistorySize)
	} else {
		aggregator = &SmartAggregator{
			HealthAggregator: NewHealthAggregator(aggregatorConfig),
		}
	}

	// Get hostname
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown"
	}

	return &HealthChecker{
		checks:      make(map[string]HealthCheck),
		aggregator:  aggregator,
		subscribers: make([]HealthCallback, 0),
		config:      config,
		logger:      logger,
		metrics:     metrics,
		container:   container,
		stopCh:      make(chan struct{}),
		resultCh:    make(chan *HealthResult, 100),
		reportCh:    make(chan *HealthReport, 10),
		startTime:   time.Now(),
		hostname:    hostname,
		version:     "unknown",
		environment: "unknown",
	}
}

// Name returns the service name
func (hc *HealthChecker) Name() string {
	return "health-checker"
}

// Dependencies returns the service dependencies
func (hc *HealthChecker) Dependencies() []string {
	return []string{} // Health checker has no dependencies
}

// OnStart starts the health checker service
func (hc *HealthChecker) Start(ctx context.Context) error {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	if hc.started {
		return common.ErrServiceAlreadyExists("health-checker")
	}

	hc.started = true
	hc.stopping = false

	// Auto-discover services if enabled
	if hc.config.EnableAutoDiscovery {
		hc.autoDiscoverServices()
	}

	// Start background routines
	go hc.checkLoop(ctx)
	go hc.reportLoop(ctx)
	go hc.resultProcessor(ctx)

	if hc.logger != nil {
		hc.logger.Info("health checker started",
			logger.Int("registered_checks", len(hc.checks)),
			logger.Duration("check_interval", hc.config.CheckInterval),
			logger.Duration("report_interval", hc.config.ReportInterval),
			logger.Bool("auto_discovery", hc.config.EnableAutoDiscovery),
		)
	}

	if hc.metrics != nil {
		hc.metrics.Counter("forge.health.checker_started").Inc()
		hc.metrics.Gauge("forge.health.registered_checks").Set(float64(len(hc.checks)))
	}

	return nil
}

// OnStop stops the health checker service
func (hc *HealthChecker) Stop(ctx context.Context) error {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	if !hc.started {
		return nil
	}

	hc.stopping = true
	close(hc.stopCh)

	// Wait for routines to finish (with timeout)
	stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Give routines time to cleanup
	select {
	case <-stopCtx.Done():
		if hc.logger != nil {
			hc.logger.Warn("health checker stop timeout")
		}
	case <-time.After(1 * time.Second):
		// Normal shutdown
	}

	hc.started = false

	if hc.logger != nil {
		hc.logger.Info("health checker stopped")
	}

	if hc.metrics != nil {
		hc.metrics.Counter("forge.health.checker_stopped").Inc()
	}

	return nil
}

// OnHealthCheck performs a health check on the health checker itself
func (hc *HealthChecker) OnHealthCheck(ctx context.Context) error {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	if !hc.started {
		return fmt.Errorf("health checker not started")
	}

	if hc.stopping {
		return fmt.Errorf("health checker is stopping")
	}

	// Check if we have any registered checks
	if len(hc.checks) == 0 {
		return fmt.Errorf("no health checks registered")
	}

	// Check if we've performed any checks recently
	if hc.lastReport != nil && time.Since(hc.lastReport.Timestamp) > hc.config.CheckInterval*2 {
		return fmt.Errorf("health checks are stale")
	}

	return nil
}

// RegisterCheck registers a health check
func (hc *HealthChecker) RegisterCheck(name string, check HealthCheck) error {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	if _, exists := hc.checks[name]; exists {
		return common.ErrServiceAlreadyExists(name)
	}

	hc.checks[name] = check

	if hc.logger != nil {
		hc.logger.Info("health check registered",
			logger.String("name", name),
			logger.Bool("critical", check.Critical()),
			logger.Duration("timeout", check.Timeout()),
			logger.String("dependencies", fmt.Sprintf("%v", check.Dependencies())),
		)
	}

	if hc.metrics != nil {
		hc.metrics.Counter("forge.health.checks_registered").Inc()
		hc.metrics.Gauge("forge.health.registered_checks").Set(float64(len(hc.checks)))
	}

	return nil
}

// UnregisterCheck unregisters a health check
func (hc *HealthChecker) UnregisterCheck(name string) error {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	if _, exists := hc.checks[name]; !exists {
		return common.ErrServiceNotFound(name)
	}

	delete(hc.checks, name)

	if hc.logger != nil {
		hc.logger.Info("health check unregistered",
			logger.String("name", name),
		)
	}

	if hc.metrics != nil {
		hc.metrics.Counter("forge.health.checks_unregistered").Inc()
		hc.metrics.Gauge("forge.health.registered_checks").Set(float64(len(hc.checks)))
	}

	return nil
}

// CheckAll performs all health checks and returns a comprehensive report
func (hc *HealthChecker) CheckAll(ctx context.Context) *HealthReport {
	hc.mu.RLock()
	checks := make(map[string]HealthCheck)
	for name, check := range hc.checks {
		checks[name] = check
	}
	hc.mu.RUnlock()

	start := time.Now()
	results := make(map[string]*HealthResult)

	// Perform checks concurrently with semaphore
	sem := make(chan struct{}, hc.config.MaxConcurrentChecks)
	var wg sync.WaitGroup
	var resultsMu sync.Mutex

	for name, check := range checks {
		wg.Add(1)
		go func(name string, check HealthCheck) {
			defer wg.Done()

			// Acquire semaphore
			sem <- struct{}{}
			defer func() { <-sem }()

			// Create timeout context
			checkCtx, cancel := context.WithTimeout(ctx, check.Timeout())
			defer cancel()

			// Perform the check
			result := check.Check(checkCtx)

			// Store result
			resultsMu.Lock()
			results[name] = result
			resultsMu.Unlock()

			// Send result to processor
			select {
			case hc.resultCh <- result:
			default:
				// Channel full, skip
			}
		}(name, check)
	}

	wg.Wait()

	// Create enriched context
	enrichedCtx := hc.enrichContext(ctx)

	// Aggregate results
	report := hc.aggregator.AggregateWithContext(enrichedCtx, results)
	report.WithDuration(time.Since(start))

	// Cache the report
	hc.mu.Lock()
	hc.lastReport = report
	hc.mu.Unlock()

	// Send report to processor
	select {
	case hc.reportCh <- report:
	default:
		// Channel full, skip
	}

	if hc.metrics != nil {
		hc.metrics.Counter("forge.health.checks_performed").Add(float64(len(results)))
		hc.metrics.Histogram("forge.health.check_duration").Observe(report.Duration.Seconds())
		hc.metrics.Gauge("forge.health.overall_status").Set(float64(report.Overall.Severity()))
	}

	return report
}

// Check performs all health checks and returns a comprehensive report
func (hc *HealthChecker) Check(ctx context.Context) *HealthReport {
	return hc.CheckAll(ctx)
}

// CheckOne performs a single health check
func (hc *HealthChecker) CheckOne(ctx context.Context, name string) *HealthResult {
	hc.mu.RLock()
	check, exists := hc.checks[name]
	hc.mu.RUnlock()

	if !exists {
		return NewHealthResult(name, HealthStatusUnknown, "health check not found")
	}

	// Create timeout context
	checkCtx, cancel := context.WithTimeout(ctx, check.Timeout())
	defer cancel()

	// Perform the check
	result := check.Check(checkCtx)

	// Send result to processor
	select {
	case hc.resultCh <- result:
	default:
		// Channel full, skip
	}

	if hc.metrics != nil {
		hc.metrics.Counter("forge.health.single_check_performed").Inc()
		hc.metrics.Histogram("forge.health.single_check_duration").Observe(result.Duration.Seconds())
	}

	return result
}

// GetStatus returns the current overall health status
func (hc *HealthChecker) GetStatus() HealthStatus {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	if hc.lastReport == nil {
		return HealthStatusUnknown
	}

	return hc.lastReport.Overall
}

// Subscribe adds a callback for health status changes
func (hc *HealthChecker) Subscribe(callback HealthCallback) error {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	hc.subscribers = append(hc.subscribers, callback)

	if hc.logger != nil {
		hc.logger.Info("health callback subscribed",
			logger.Int("total_subscribers", len(hc.subscribers)),
		)
	}

	return nil
}

// GetLastReport returns the last health report
func (hc *HealthChecker) GetLastReport() *HealthReport {
	hc.mu.RLock()
	defer hc.mu.RUnlock()
	return hc.lastReport
}

// GetChecks returns all registered health checks
func (hc *HealthChecker) GetChecks() map[string]HealthCheck {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	checks := make(map[string]HealthCheck)
	for name, check := range hc.checks {
		checks[name] = check
	}
	return checks
}

// GetStats returns health checker statistics
func (hc *HealthChecker) GetStats() *HealthCheckerStats {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	stats := &HealthCheckerStats{
		RegisteredChecks: len(hc.checks),
		Subscribers:      len(hc.subscribers),
		Started:          hc.started,
		Uptime:           time.Since(hc.startTime),
		LastReport:       hc.lastReport,
	}

	if hc.lastReport != nil {
		stats.LastReportTime = hc.lastReport.Timestamp
		stats.OverallStatus = hc.lastReport.Overall
	}

	return stats
}

// autoDiscoverServices automatically discovers services for health checking
func (hc *HealthChecker) autoDiscoverServices() {
	if hc.container == nil {
		return
	}

	// Get all services from the container
	services := hc.container.Services()

	for _, service := range services {
		serviceName := service.Name
		if serviceName == "" {
			serviceName = service.ServiceName()
		}

		// Skip if already registered
		if _, exists := hc.checks[serviceName]; exists {
			continue
		}

		// Create a service health check
		config := &HealthCheckConfig{
			Name:     serviceName,
			Timeout:  hc.config.DefaultTimeout,
			Critical: hc.isCriticalService(serviceName),
			Tags:     hc.config.Tags,
		}

		check := NewSimpleHealthCheck(config, func(ctx context.Context) *HealthResult {
			return hc.checkService(ctx, serviceName)
		})

		hc.checks[serviceName] = check

		if hc.logger != nil {
			hc.logger.Info("auto-discovered service health check",
				logger.String("service", serviceName),
				logger.Bool("critical", check.Critical()),
			)
		}
	}
}

// checkService performs a health check on a service
func (hc *HealthChecker) checkService(ctx context.Context, serviceName string) *HealthResult {
	// Try to resolve the service
	service, err := hc.container.ResolveNamed(serviceName)
	if err != nil {
		return NewHealthResult(serviceName, HealthStatusUnhealthy, "service not found").WithError(err)
	}

	// Check if service implements health check interface
	if healthCheckable, ok := service.(interface{ OnHealthCheck(context.Context) error }); ok {
		if err := healthCheckable.OnHealthCheck(ctx); err != nil {
			return NewHealthResult(serviceName, HealthStatusUnhealthy, "service health check failed").WithError(err)
		}
		return NewHealthResult(serviceName, HealthStatusHealthy, "service health check passed")
	}

	// If no health check method, assume healthy if service exists
	return NewHealthResult(serviceName, HealthStatusHealthy, "service exists and is resolvable")
}

// isCriticalService checks if a service is marked as critical
func (hc *HealthChecker) isCriticalService(serviceName string) bool {
	for _, critical := range hc.config.CriticalServices {
		if critical == serviceName {
			return true
		}
	}
	return false
}

// enrichContext adds framework information to the context
func (hc *HealthChecker) enrichContext(ctx context.Context) context.Context {
	ctx = context.WithValue(ctx, "version", hc.version)
	ctx = context.WithValue(ctx, "environment", hc.environment)
	ctx = context.WithValue(ctx, "hostname", hc.hostname)
	ctx = context.WithValue(ctx, "uptime", time.Since(hc.startTime))
	return ctx
}

// checkLoop runs the periodic health check loop
func (hc *HealthChecker) checkLoop(ctx context.Context) {
	ticker := time.NewTicker(hc.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-hc.stopCh:
			return
		case <-ticker.C:
			hc.CheckAll(ctx)
		}
	}
}

// reportLoop runs the periodic report generation loop
func (hc *HealthChecker) reportLoop(ctx context.Context) {
	ticker := time.NewTicker(hc.config.ReportInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-hc.stopCh:
			return
		case <-ticker.C:
			// Generate comprehensive report
			report := hc.CheckAll(ctx)

			if hc.logger != nil {
				hc.logger.Info("health report generated",
					logger.String("overall_status", report.Overall.String()),
					logger.Int("total_services", len(report.Services)),
					logger.Int("healthy_count", report.GetHealthyCount()),
					logger.Int("degraded_count", report.GetDegradedCount()),
					logger.Int("unhealthy_count", report.GetUnhealthyCount()),
					logger.Duration("report_duration", report.Duration),
				)
			}
		}
	}
}

// resultProcessor processes health check results
func (hc *HealthChecker) resultProcessor(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-hc.stopCh:
			return
		case result := <-hc.resultCh:
			// Notify subscribers
			for _, callback := range hc.subscribers {
				go func(cb HealthCallback) {
					defer func() {
						if r := recover(); r != nil {
							if hc.logger != nil {
								hc.logger.Error("health callback panic",
									logger.String("error", fmt.Sprintf("%v", r)),
								)
							}
						}
					}()
					cb(result)
				}(callback)
			}
		case report := <-hc.reportCh:
			// Process report (e.g., persistence, alerting)
			hc.processReport(report)
		}
	}
}

// processReport processes a health report
func (hc *HealthChecker) processReport(report *HealthReport) {
	// TODO: Implement persistence and alerting
	// This would integrate with the persistence and alerting packages
}

// SetEnvironment sets the environment name for the HealthChecker instance.
func (hc *HealthChecker) SetEnvironment(name string) {
	hc.environment = name
}

func (hc *HealthChecker) SetVersion(version string) {
	hc.version = version
}

func (hc *HealthChecker) SetHostname(hostname string) {
	hc.hostname = hostname
}

func (hc *HealthChecker) Environment() string {
	return hc.environment
}

func (hc *HealthChecker) Version() string {
	return hc.version
}

func (hc *HealthChecker) Hostname() string {
	return hc.hostname
}

func (hc *HealthChecker) StartTime() time.Time {
	return hc.startTime
}

// HealthCheckerStats contains statistics about the health checker
type HealthCheckerStats = common.HealthCheckerStats
