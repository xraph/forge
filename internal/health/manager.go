package health

import (
	"context"
	"fmt"
	"maps"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/xraph/forge/errors"
	healthinternal "github.com/xraph/forge/internal/health/internal"
	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/scheduler"
	"github.com/xraph/forge/internal/shared"
	"github.com/xraph/go-utils/metrics"
)

// Context key types to avoid collisions.
type contextKey string

const (
	contextKeyVersion     contextKey = "version"
	contextKeyEnvironment contextKey = "environment"
	contextKeyHostname    contextKey = "hostname"
	contextKeyUptime      contextKey = "uptime"
)

// maxSubscribers is the maximum number of health subscribers allowed.
const maxSubscribers = 100

// ManagerImpl implements comprehensive health monitoring for all services.
type ManagerImpl struct {
	checks      map[string]healthinternal.HealthCheck
	aggregator  *healthinternal.SmartAggregator
	subscribers []healthinternal.HealthCallback

	// Configuration
	config    *healthinternal.HealthConfig
	logger    logger.Logger
	metrics   shared.Metrics
	container shared.Container

	// State management
	started    bool
	stopping   bool
	lastReport *healthinternal.HealthReport

	// cancelJobs unregisters this manager's scheduler jobs on Stop.
	cancelJobs []func()

	// Async execution
	stopCh   chan struct{}
	resultCh chan []*healthinternal.HealthResult
	reportCh chan *healthinternal.HealthReport

	// Synchronization
	mu          sync.RWMutex
	wg          sync.WaitGroup
	startTime   time.Time
	hostname    string
	version     string
	environment string
}

// HealthConfig contains configuration for the health checker.
type HealthConfig = healthinternal.HealthConfig

// DefaultHealthConfig returns default configuration.
func DefaultHealthConfig() *HealthConfig {
	return healthinternal.DefaultHealthCheckerConfig()
}

// normalizeHealthConfig fills in the fields a caller left at zero.
//
// A partial config is the normal case: forge.DefaultHealthConfig() returns only
// Enabled, and a caller who sets one field in a literal leaves the rest zero.
// Those zeros used to reach time.NewTicker, which panics on a non-positive
// interval. It runs in a background goroutine, so the panic took the process
// down at startup with no way to recover.
func normalizeHealthConfig(config *HealthConfig) {
	defaults := healthinternal.DefaultHealthCheckerConfig()

	if config.Intervals.Check <= 0 {
		config.Intervals.Check = defaults.Intervals.Check
	}

	if config.Intervals.Report <= 0 {
		config.Intervals.Report = defaults.Intervals.Report
	}

	if config.Performance.MaxConcurrentChecks <= 0 {
		config.Performance.MaxConcurrentChecks = defaults.Performance.MaxConcurrentChecks
	}

	if config.Performance.DefaultTimeout <= 0 {
		config.Performance.DefaultTimeout = defaults.Performance.DefaultTimeout
	}

	if config.Performance.HistorySize <= 0 {
		config.Performance.HistorySize = defaults.Performance.HistorySize
	}
}

// New creates a new health checker.
func New(config *HealthConfig, logger logger.Logger, metrics shared.Metrics, container shared.Container) shared.HealthManager {
	if config == nil {
		config = DefaultHealthConfig()
	}

	normalizeHealthConfig(config)

	// Create aggregator
	aggregatorConfig := &healthinternal.AggregatorConfig{
		CriticalServices:   config.CriticalServices,
		DegradedThreshold:  config.Thresholds.Degraded,
		UnhealthyThreshold: config.Thresholds.Unhealthy,
		EnableDependencies: true,
		Weights:            make(map[string]float64),
	}

	var aggregator *healthinternal.SmartAggregator
	if config.Features.Aggregation {
		aggregator = healthinternal.NewSmartAggregator(aggregatorConfig)
		aggregator.SetMaxHistorySize(config.Performance.HistorySize)
	} else {
		aggregator = &healthinternal.SmartAggregator{
			HealthAggregator: healthinternal.NewHealthAggregator(aggregatorConfig),
		}
	}

	// Get hostname
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown"
	}

	return &ManagerImpl{
		checks:      make(map[string]healthinternal.HealthCheck),
		aggregator:  aggregator,
		subscribers: make([]healthinternal.HealthCallback, 0),
		config:      config,
		logger:      logger,
		metrics:     metrics,
		container:   container,
		stopCh:      make(chan struct{}),
		resultCh:    make(chan []*healthinternal.HealthResult, 3),
		reportCh:    make(chan *healthinternal.HealthReport, 10),
		startTime:   time.Now(),
		hostname:    hostname,
		version:     "unknown",
		environment: "unknown",
	}
}

// Name returns the service name.
func (hc *ManagerImpl) Name() string {
	return "forge.health.service"
}

// Start starts the health checker service.
func (hc *ManagerImpl) Start(ctx context.Context) error {
	hc.mu.Lock()

	if hc.started {
		hc.mu.Unlock()
		return nil // idempotent
	}

	hc.started = true
	hc.stopping = false

	autoDiscoveryEnabled := hc.config.Features.AutoDiscovery
	endpointsEnabled := hc.config.Endpoints.Enabled

	// Release lock before auto-discovery which acquires RLock internally.
	hc.mu.Unlock()

	// The framework's own checks are a fixed handful, so they register whatever
	// discovery is set to.
	if err := hc.registerBuiltinChecks(); err != nil {
		if hc.logger != nil {
			hc.logger.Warn("failed to register some built-in health checks",
				logger.Error(err),
			)
		}
	}

	// Per-service discovery is opt-in. It registers a check for every service in
	// the container, not one per extension, so the check count tracks the whole
	// dependency graph. Most of those services cannot answer a health question
	// at all: checkService reports healthy for anything that does not implement
	// OnHealthCheck, so each one costs a goroutine, a context and a result every
	// cycle to say something it was always going to say.
	//
	// Runs synchronously, before the check loop, so the first cycle sees
	// everything it is going to see.
	if autoDiscoveryEnabled {
		hc.autoDiscoverServices()
	}

	if endpointsEnabled {
		if err := hc.registerEndpoints(); err != nil {
			if hc.logger != nil {
				hc.logger.Warn("failed to register health endpoints",
					logger.Error(err),
				)
			}
		}
	}

	// Run initial check so the first dashboard/API access has results
	// instead of waiting for the first scheduled round (30s default).
	hc.Check(ctx)

	checkInterval := hc.config.Intervals.Check
	if checkInterval <= 0 {
		checkInterval = healthinternal.DefaultHealthCheckerConfig().Intervals.Check
	}

	reportInterval := hc.config.Intervals.Report
	if reportInterval <= 0 {
		reportInterval = healthinternal.DefaultHealthCheckerConfig().Intervals.Report
	}

	// The two periodic rounds go on the shared scheduler rather than each
	// taking a goroutine parked on its own ticker. The result processor is
	// channel-driven, not periodic, so it stays a goroutine of its own.
	hc.cancelJobs = append(hc.cancelJobs,
		scheduler.Default().Every(hc.Name()+".check", checkInterval, func(jobCtx context.Context) {
			hc.Check(jobCtx)
		}),
		scheduler.Default().Every(hc.Name()+".report", reportInterval, func(context.Context) {
			hc.logReport()
		}),
	)

	hc.wg.Add(1)
	go hc.resultProcessor(ctx)

	if hc.logger != nil {
		hc.logger.Info(hc.Name()+" started",
			logger.Int("registered_checks", len(hc.checks)),
			logger.Duration("check_interval", hc.config.Intervals.Check),
			logger.Duration("report_interval", hc.config.Intervals.Report),
			logger.Bool("auto_discovery", hc.config.Features.AutoDiscovery),
		)
	}

	if hc.metrics != nil {
		hc.metrics.Counter(hc.Name() + ".started").Inc()
		hc.metrics.Gauge("forge.health.registered_checks").Set(float64(len(hc.checks)))
	}

	return nil
}

// IsStarted returns whether the health manager has been started.
func (hc *ManagerImpl) IsStarted() bool {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	return hc.started
}

// Stop stops the health checker service.
func (hc *ManagerImpl) Stop(ctx context.Context) error {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	if !hc.started {
		return nil
	}

	hc.stopping = true
	close(hc.stopCh)

	// Unregister the periodic rounds before releasing the lock, so no new one
	// starts while the manager is shutting down.
	cancels := hc.cancelJobs
	hc.cancelJobs = nil

	// Release lock while waiting for goroutines so they can acquire it if needed
	hc.mu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}

	// Wait for all background goroutines with timeout
	done := make(chan struct{})
	go func() {
		hc.wg.Wait()
		close(done)
	}()

	stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	select {
	case <-done:
		// All goroutines exited cleanly
	case <-stopCtx.Done():
		if hc.logger != nil {
			hc.logger.Warn(hc.Name() + " stop timeout waiting for goroutines")
		}
	}

	hc.mu.Lock()
	hc.started = false

	if hc.logger != nil {
		hc.logger.Info(hc.Name() + " stopped")
	}

	if hc.metrics != nil {
		hc.metrics.Counter(hc.Name() + ".stopped").Inc()
	}

	return nil
}

// Health performs a health check on the health checker itself (implements HealthService interface).
func (hc *ManagerImpl) Health(ctx context.Context) error {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	if !hc.started {
		return fmt.Errorf("%s not started", hc.Name())
	}

	if hc.stopping {
		return fmt.Errorf("%s is stopping", hc.Name())
	}

	// Check if we have any registered checks
	if len(hc.checks) == 0 {
		return fmt.Errorf("%s no health checks registered", hc.Name())
	}

	// Check if we've performed any checks recently
	if hc.lastReport != nil && time.Since(hc.lastReport.Timestamp) > hc.config.Intervals.Check*2 {
		return fmt.Errorf("%s health checks are stale", hc.Name())
	}

	return nil
}

// OnHealthCheck performs a health check on the health checker itself (legacy method).
func (hc *ManagerImpl) OnHealthCheck(ctx context.Context) error {
	return hc.Health(ctx)
}

// Register registers a health check.
func (hc *ManagerImpl) Register(check healthinternal.HealthCheck) error {
	hc.mu.Lock()

	name := check.Name()
	if name == "" {
		hc.mu.Unlock()

		return errors.ErrInvalidConfig("health_check_name",
			fmt.Errorf("health check name cannot be empty"))
	}

	if _, exists := hc.checks[name]; exists {
		hc.mu.Unlock()

		return errors.ErrServiceAlreadyExists(name)
	}

	hc.checks[name] = check

	// Get check count while holding lock
	checkCount := len(hc.checks)
	metrics := hc.metrics
	loggerInstance := hc.logger
	hc.mu.Unlock()

	// Log and update metrics without holding lock to avoid deadlock
	if loggerInstance != nil {
		loggerInstance.Debug(hc.Name()+" health check registered",
			logger.String("name", name),
			logger.Bool("critical", check.Critical()),
			logger.Duration("timeout", check.Timeout()),
			logger.String("dependencies", fmt.Sprintf("%v", check.Dependencies())),
		)
	}

	if metrics != nil {
		metrics.Counter(hc.Name() + ".checks_registered").Inc()
		metrics.Gauge(hc.Name() + ".registered_checks").Set(float64(checkCount))
	}

	return nil
}

// RegisterFn registers a function-based health check.
func (hc *ManagerImpl) RegisterFn(name string, checkFn healthinternal.HealthCheckFunc) error {
	if name == "" {
		return errors.ErrInvalidConfig("health_check_name",
			fmt.Errorf("health check name cannot be empty"))
	}

	config := &healthinternal.HealthCheckConfig{
		Name:    name,
		Timeout: hc.config.Performance.DefaultTimeout,
	}
	check := healthinternal.NewSimpleHealthCheck(config, checkFn)

	return hc.Register(check)
}

// Unregister unregisters a health check.
func (hc *ManagerImpl) Unregister(name string) error {
	hc.mu.Lock()

	if _, exists := hc.checks[name]; !exists {
		hc.mu.Unlock()

		return errors.ErrServiceNotFound(name)
	}

	delete(hc.checks, name)

	// Get check count while holding lock
	checkCount := len(hc.checks)
	metrics := hc.metrics
	loggerInstance := hc.logger
	hc.mu.Unlock()

	// Log and update metrics without holding lock to avoid deadlock
	if loggerInstance != nil {
		loggerInstance.Debug(hc.Name()+" health check unregistered",
			logger.String("name", name),
		)
	}

	if metrics != nil {
		metrics.Counter(hc.Name() + ".checks_unregistered").Inc()
		metrics.Gauge(hc.Name() + ".registered_checks").Set(float64(checkCount))
	}

	return nil
}

// Check performs all health checks and returns a comprehensive report.
func (hc *ManagerImpl) Check(ctx context.Context) *healthinternal.HealthReport {
	// Snapshot the checks into a slice rather than copying the map. Every
	// extension registers a check and auto-discovery registers one per DI
	// service, so this runs over a set that grows with the app; a slice costs
	// one allocation instead of a map's allocation plus a hash per entry.
	hc.mu.RLock()

	type namedCheck struct {
		name  string
		check healthinternal.HealthCheck
	}

	checks := make([]namedCheck, 0, len(hc.checks))
	for name, check := range hc.checks {
		checks = append(checks, namedCheck{name: name, check: check})
	}

	hc.mu.RUnlock()

	start := time.Now()

	// Results are written by index, so the workers need no lock between them.
	batch := make([]*healthinternal.HealthResult, len(checks))

	// The semaphore is acquired before the goroutine is spawned, not inside it.
	// Acquiring inside meant MaxConcurrentChecks bounded how many checks ran at
	// once but not how many goroutines existed: every check got a goroutine
	// immediately and most of them sat blocked on the channel.
	sem := make(chan struct{}, hc.config.Performance.MaxConcurrentChecks)

	var wg sync.WaitGroup

	for i, nc := range checks {
		sem <- struct{}{}

		wg.Add(1)

		go func(i int, name string, check healthinternal.HealthCheck) {
			defer func() {
				// A health check runs arbitrary extension code. Without this,
				// one panicking check took the whole process down from a
				// goroutine no caller could recover in.
				if r := recover(); r != nil {
					batch[i] = healthinternal.NewHealthResult(name,
						healthinternal.HealthStatusUnhealthy,
						fmt.Sprintf("health check panicked: %v", r))

					if hc.logger != nil {
						hc.logger.Error(hc.Name()+" health check panicked",
							logger.String("check", name),
							logger.Any("panic", r),
						)
					}
				}

				<-sem
				wg.Done()
			}()

			// Create timeout context
			checkCtx, cancel := context.WithTimeout(ctx, check.Timeout())
			defer cancel()

			batch[i] = check.Check(checkCtx)
		}(i, nc.name, nc.check)
	}

	wg.Wait()

	results := make(map[string]*healthinternal.HealthResult, len(checks))
	for i, nc := range checks {
		results[nc.name] = batch[i]
	}

	select {
	case hc.resultCh <- batch:
	default:
		if hc.logger != nil {
			hc.logger.Warn(hc.Name()+" result batch channel full, dropping batch",
				logger.Int("batch_size", len(batch)),
			)
		}
	}

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
		if hc.logger != nil {
			hc.logger.Warn(hc.Name() + " report channel full, dropping report")
		}
	}

	if hc.metrics != nil {
		hc.metrics.Counter(hc.Name() + ".checks_performed").Add(float64(len(results)))
		hc.metrics.Histogram(hc.Name() + ".check_duration").Observe(report.Duration.Seconds())
		hc.metrics.Gauge(hc.Name() + ".overall_status").Set(float64(report.Overall.Severity()))
	}

	return report
}

// CheckOne performs a single health check.
func (hc *ManagerImpl) CheckOne(ctx context.Context, name string) *healthinternal.HealthResult {
	hc.mu.RLock()
	check, exists := hc.checks[name]
	hc.mu.RUnlock()

	if !exists {
		return healthinternal.NewHealthResult(name, healthinternal.HealthStatusUnknown, "health check not found")
	}

	// Create timeout context
	checkCtx, cancel := context.WithTimeout(ctx, check.Timeout())
	defer cancel()

	// Perform the check
	result := check.Check(checkCtx)

	// Send result to processor as single-element batch
	select {
	case hc.resultCh <- []*healthinternal.HealthResult{result}:
	default:
		if hc.logger != nil {
			hc.logger.Warn(hc.Name()+" result channel full, dropping result",
				logger.String("check", name),
			)
		}
	}

	if hc.metrics != nil {
		hc.metrics.Counter(hc.Name() + ".single_check_performed").Inc()
		hc.metrics.Histogram(hc.Name() + ".single_check_duration").Observe(result.Duration.Seconds())
	}

	return result
}

// Status returns the current overall health status (implements HealthChecker interface).
func (hc *ManagerImpl) Status() healthinternal.HealthStatus {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	if hc.lastReport == nil {
		return healthinternal.HealthStatusUnknown
	}

	return hc.lastReport.Overall
}

// GetStatus returns the current overall health status (legacy method).
func (hc *ManagerImpl) GetStatus() healthinternal.HealthStatus {
	return hc.Status()
}

// Subscribe adds a callback for health status changes.
func (hc *ManagerImpl) Subscribe(callback healthinternal.HealthCallback) error {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	if len(hc.subscribers) >= maxSubscribers {
		return fmt.Errorf("%s: maximum subscriber limit (%d) reached", hc.Name(), maxSubscribers)
	}

	hc.subscribers = append(hc.subscribers, callback)

	if hc.logger != nil {
		hc.logger.Debug(hc.Name()+" health callback subscribed",
			logger.Int("total_subscribers", len(hc.subscribers)),
		)
	}

	return nil
}

// LastReport returns the last health report (implements HealthReporter interface).
func (hc *ManagerImpl) LastReport() *healthinternal.HealthReport {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	return hc.lastReport
}

// GetLastReport returns the last health report (legacy method).
func (hc *ManagerImpl) GetLastReport() *healthinternal.HealthReport {
	return hc.LastReport()
}

// ListChecks returns all registered health checks (implements HealthCheckRegistry interface).
func (hc *ManagerImpl) ListChecks() map[string]healthinternal.HealthCheck {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	checks := make(map[string]healthinternal.HealthCheck)
	maps.Copy(checks, hc.checks)

	return checks
}

// GetChecks returns all registered health checks (legacy method).
func (hc *ManagerImpl) GetChecks() map[string]healthinternal.HealthCheck {
	return hc.ListChecks()
}

// Stats returns health checker statistics (implements HealthReporter interface).
func (hc *ManagerImpl) Stats() *healthinternal.HealthCheckerStats {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	stats := &healthinternal.HealthCheckerStats{
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

// GetStats returns health checker statistics (legacy method).
func (hc *ManagerImpl) GetStats() *healthinternal.HealthCheckerStats {
	return hc.Stats()
}

// autoDiscoverServices automatically discovers services for health checking.
func (hc *ManagerImpl) autoDiscoverServices() {
	hc.mu.RLock()
	container := hc.container
	hc.mu.RUnlock()

	if container == nil {
		return
	}

	// Get all services from the container
	services := container.Services()

	for _, serviceName := range services {
		// Check if already registered (with lock)
		hc.mu.RLock()
		_, exists := hc.checks[serviceName]
		hc.mu.RUnlock()

		if exists {
			continue
		}

		// Create local copy for closure capture (avoid loop variable capture bug)
		svcName := serviceName

		// Create a service health check
		config := &healthinternal.HealthCheckConfig{
			Name:     serviceName,
			Timeout:  hc.config.Performance.DefaultTimeout,
			Critical: hc.isCriticalService(serviceName),
			Tags:     hc.config.Tags,
		}

		check := healthinternal.NewSimpleHealthCheck(config, func(ctx context.Context) *healthinternal.HealthResult {
			return hc.checkService(ctx, svcName)
		})

		// Register the check (with lock)
		hc.mu.Lock()
		// Double-check after acquiring write lock
		if _, exists := hc.checks[serviceName]; !exists {
			hc.checks[serviceName] = check
		}

		hc.mu.Unlock()

		if hc.logger != nil {
			hc.logger.Debug(hc.Name()+" auto-discovered service health check",
				logger.String("service", serviceName),
				logger.Bool("critical", check.Critical()),
			)
		}
	}
}

// checkService performs a health check on a service.
func (hc *ManagerImpl) checkService(ctx context.Context, serviceName string) *healthinternal.HealthResult {
	// Try to resolve the service
	hc.mu.RLock()
	container := hc.container
	hc.mu.RUnlock()

	if container == nil {
		return healthinternal.NewHealthResult(serviceName, healthinternal.HealthStatusUnhealthy, "container not available")
	}

	service, err := container.Resolve(serviceName)
	if err != nil {
		// Service is known to the container (returned by Services()) but
		// cannot be resolved by name — common for type-registry services.
		// Default to healthy since the service is registered and running.
		return healthinternal.NewHealthResult(serviceName, healthinternal.HealthStatusHealthy, "service registered")
	}

	// Check if service implements health check interface
	if healthCheckable, ok := service.(interface {
		OnHealthCheck(ctx context.Context) error
	}); ok {
		if err := healthCheckable.OnHealthCheck(ctx); err != nil {
			return healthinternal.NewHealthResult(serviceName, healthinternal.HealthStatusUnhealthy, "service health check failed").WithError(err)
		}

		return healthinternal.NewHealthResult(serviceName, healthinternal.HealthStatusHealthy, "service health check passed")
	}

	// If no health check method, assume healthy if service exists
	return healthinternal.NewHealthResult(serviceName, healthinternal.HealthStatusHealthy, "service exists and is resolvable")
}

// isCriticalService checks if a service is marked as critical.
func (hc *ManagerImpl) isCriticalService(serviceName string) bool {
	return slices.Contains(hc.config.CriticalServices, serviceName)
}

// enrichContext adds framework information to the context.
func (hc *ManagerImpl) enrichContext(ctx context.Context) context.Context {
	ctx = context.WithValue(ctx, contextKeyVersion, hc.version)
	ctx = context.WithValue(ctx, contextKeyEnvironment, hc.environment)
	ctx = context.WithValue(ctx, contextKeyHostname, hc.hostname)
	ctx = context.WithValue(ctx, contextKeyUptime, time.Since(hc.startTime))

	return ctx
}

// logReport writes the cached report to the debug log.
//
// It reads the report the check round already produced rather than running a
// second Check, which would double the work and the results the subscribers
// see.
func (hc *ManagerImpl) logReport() {
	hc.mu.RLock()
	report := hc.lastReport
	hc.mu.RUnlock()

	if report == nil || hc.logger == nil {
		return
	}

	healthReportAnalyzer := metrics.NewHealthReportAnalyzer(report)

	hc.logger.Debug(hc.Name()+" health report generated",
		logger.String("overall_status", report.Overall.String()),
		logger.Int("total_services", len(report.Services)),
		logger.Int("healthy_count", healthReportAnalyzer.HealthyCount()),
		logger.Int("degraded_count", healthReportAnalyzer.DegradedCount()),
		logger.Int("unhealthy_count", healthReportAnalyzer.UnhealthyCount()),
		logger.Duration("report_duration", report.Duration),
	)
}

// resultProcessor processes health check results.
func (hc *ManagerImpl) resultProcessor(ctx context.Context) {
	defer hc.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case <-hc.stopCh:
			return
		case batch := <-hc.resultCh:
			for _, result := range batch {
				hc.notifySubscribers(result)
			}
		case report := <-hc.reportCh:
			// Process report (e.g., persistence, alerting)
			hc.processReport(report)
		}
	}
}

// notifySubscribers sends a health result to all subscribers with bounded concurrency.
func (hc *ManagerImpl) notifySubscribers(result *healthinternal.HealthResult) {
	hc.mu.RLock()
	subscribers := make([]healthinternal.HealthCallback, len(hc.subscribers))
	copy(subscribers, hc.subscribers)
	hc.mu.RUnlock()

	if len(subscribers) == 0 {
		return
	}

	// Use bounded concurrency: min(len(subscribers), 10)
	workerCount := len(subscribers)
	if workerCount > 10 {
		workerCount = 10
	}

	var wg sync.WaitGroup
	work := make(chan healthinternal.HealthCallback, len(subscribers))

	// Start workers
	for range workerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for cb := range work {
				func() {
					defer func() {
						if r := recover(); r != nil {
							if hc.logger != nil {
								hc.logger.Error(hc.Name()+" health callback panic",
									logger.String("error", fmt.Sprintf("%v", r)),
								)
							}
						}
					}()
					cb(result)
				}()
			}
		}()
	}

	// Send work
	for _, cb := range subscribers {
		work <- cb
	}
	close(work)

	wg.Wait()
}

// processReport processes a health report.
func (hc *ManagerImpl) processReport(report *healthinternal.HealthReport) {
	// TODO: Implement persistence and alerting
	// This would integrate with the persistence and alerting packages
}

// SetEnvironment sets the environment name for the HealthManagerImpl instance.
func (hc *ManagerImpl) SetEnvironment(name string) {
	hc.environment = name
}

func (hc *ManagerImpl) SetVersion(version string) {
	hc.version = version
}

func (hc *ManagerImpl) SetHostname(hostname string) {
	hc.hostname = hostname
}

func (hc *ManagerImpl) Environment() string {
	return hc.environment
}

func (hc *ManagerImpl) Version() string {
	return hc.version
}

func (hc *ManagerImpl) Hostname() string {
	return hc.hostname
}

func (hc *ManagerImpl) StartTime() time.Time {
	return hc.startTime
}

// =============================================================================
// CONTAINER MANAGEMENT
// =============================================================================

// SetContainer sets the container reference for health checks.
func (hc *ManagerImpl) SetContainer(container shared.Container) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	hc.container = container

	if hc.logger != nil {
		hc.logger.Debug("container reference set for health manager")
	}
}

// =============================================================================
// RELOAD CONFIGURATION
// =============================================================================

// Reload reloads the health configuration at runtime.
func (hc *ManagerImpl) Reload(config *HealthConfig) error {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	if config == nil {
		return errors.ErrInvalidConfig("health_config",
			errors.New("config cannot be nil"))
	}

	if hc.logger != nil {
		hc.logger.Info("reloading health configuration",
			logger.Bool("enabled", config.Enabled),
			logger.Duration("check_interval", config.Intervals.Check),
			logger.Duration("default_timeout", config.Performance.DefaultTimeout),
		)
	}

	oldConfig := hc.config

	// Update config
	hc.config = config

	// Update aggregator if thresholds changed
	aggregatorChanged := oldConfig.Thresholds.Degraded != config.Thresholds.Degraded ||
		oldConfig.Thresholds.Unhealthy != config.Thresholds.Unhealthy ||
		!equalStringSlices(oldConfig.CriticalServices, config.CriticalServices)

	if aggregatorChanged {
		aggregatorConfig := &healthinternal.AggregatorConfig{
			CriticalServices:   config.CriticalServices,
			DegradedThreshold:  config.Thresholds.Degraded,
			UnhealthyThreshold: config.Thresholds.Unhealthy,
			EnableDependencies: true,
			Weights:            make(map[string]float64),
		}

		if config.Features.Aggregation {
			hc.aggregator = healthinternal.NewSmartAggregator(aggregatorConfig)
			hc.aggregator.SetMaxHistorySize(config.Performance.HistorySize)
		} else {
			hc.aggregator = &healthinternal.SmartAggregator{
				HealthAggregator: healthinternal.NewHealthAggregator(aggregatorConfig),
			}
		}

		if hc.logger != nil {
			hc.logger.Debug("health aggregator reinitialized")
		}
	}

	// Update environment metadata if changed
	if config.Environment != "" && config.Environment != hc.environment {
		hc.environment = config.Environment
	}

	if config.Version != "" && config.Version != hc.version {
		hc.version = config.Version
	}

	if hc.logger != nil {
		hc.logger.Info("health configuration reloaded successfully")
	}

	return nil
}

// equalStringSlices checks if two string slices are equal.
func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}
