package health

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/xraph/forge/internal/errors"
	health "github.com/xraph/forge/internal/health/internal"
	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/shared"
)

// TestHealthService is a mock health service for testing
type TestHealthService struct {
	name          string
	status        health.HealthStatus
	checks        map[string]health.HealthCheck
	lastReport    *health.HealthReport
	started       bool
	callbacks     []health.HealthCallback
	configuration *health.HealthConfig
	mu            sync.RWMutex
	callCounts    map[string]int
	methodCalls   []string
	errors        map[string]error
}

// NewTestHealthService creates a new test health service
func NewTestHealthService(name string) *TestHealthService {
	return &TestHealthService{
		name:          name,
		status:        health.HealthStatusHealthy,
		checks:        make(map[string]health.HealthCheck),
		started:       false,
		callbacks:     make([]health.HealthCallback, 0),
		callCounts:    make(map[string]int),
		methodCalls:   make([]string, 0),
		errors:        make(map[string]error),
		configuration: health.DefaultHealthCheckerConfig(),
	}
}

// Service lifecycle methods
func (ths *TestHealthService) Name() string {
	ths.mu.Lock()
	defer ths.mu.Unlock()
	ths.recordMethodCall("Name")
	return ths.name
}

func (ths *TestHealthService) Dependencies() []string {
	ths.mu.Lock()
	defer ths.mu.Unlock()
	ths.recordMethodCall("Dependencies")
	return []string{}
}

func (ths *TestHealthService) Start(ctx context.Context) error {
	ths.mu.Lock()
	defer ths.mu.Unlock()
	ths.recordMethodCall("Start")

	if err := ths.errors["Start"]; err != nil {
		return err
	}

	ths.started = true
	return nil
}

func (ths *TestHealthService) Stop(ctx context.Context) error {
	ths.mu.Lock()
	defer ths.mu.Unlock()
	ths.recordMethodCall("Stop")

	if err := ths.errors["Stop"]; err != nil {
		return err
	}

	ths.started = false
	return nil
}

func (ths *TestHealthService) OnHealthCheck(ctx context.Context) error {
	ths.mu.Lock()
	defer ths.mu.Unlock()
	ths.recordMethodCall("OnHealthCheck")

	if err := ths.errors["OnHealthCheck"]; err != nil {
		return err
	}

	return nil
}

// Health check management
func (ths *TestHealthService) RegisterCheck(name string, check health.HealthCheck) error {
	ths.mu.Lock()
	defer ths.mu.Unlock()
	ths.recordMethodCall("Register")

	if err := ths.errors["Register"]; err != nil {
		return err
	}

	ths.checks[name] = check
	return nil
}

func (ths *TestHealthService) UnregisterCheck(name string) error {
	ths.mu.Lock()
	defer ths.mu.Unlock()
	ths.recordMethodCall("Unregister")

	if err := ths.errors["Unregister"]; err != nil {
		return err
	}

	delete(ths.checks, name)
	return nil
}

func (ths *TestHealthService) ListChecks() []string {
	ths.mu.Lock()
	defer ths.mu.Unlock()
	ths.recordMethodCall("ListChecks")

	names := make([]string, 0, len(ths.checks))
	for name := range ths.checks {
		names = append(names, name)
	}
	return names
}

func (ths *TestHealthService) GetCheck(name string) (health.HealthCheck, error) {
	ths.mu.Lock()
	defer ths.mu.Unlock()
	ths.recordMethodCall("GetCheck")

	if err := ths.errors["GetCheck"]; err != nil {
		return nil, err
	}

	if check, exists := ths.checks[name]; exists {
		return check, nil
	}
	return nil, errors.ErrServiceNotFound(name)
}

// Health checking
func (ths *TestHealthService) CheckAll(ctx context.Context) *health.HealthReport {
	ths.mu.Lock()
	defer ths.mu.Unlock()
	ths.recordMethodCall("Check")

	if ths.lastReport != nil {
		return ths.lastReport
	}

	report := health.NewHealthReport()
	report.Overall = ths.status

	// Add mock results for registered checks
	for _, check := range ths.checks {
		result := check.Check(ctx)
		report.AddResult(result)
	}

	ths.lastReport = report
	return report
}

func (ths *TestHealthService) CheckOne(ctx context.Context, name string) *health.HealthResult {
	ths.mu.Lock()
	defer ths.mu.Unlock()
	ths.recordMethodCall("CheckOne")

	if check, exists := ths.checks[name]; exists {
		return check.Check(ctx)
	}

	return health.NewHealthResult(name, health.HealthStatusUnknown, "check not found")
}

func (ths *TestHealthService) GetStatus() health.HealthStatus {
	ths.mu.Lock()
	defer ths.mu.Unlock()
	ths.recordMethodCall("GetStatus")
	return ths.status
}

func (ths *TestHealthService) IsHealthy() bool {
	return ths.GetStatus() == health.HealthStatusHealthy
}

func (ths *TestHealthService) IsDegraded() bool {
	return ths.GetStatus() == health.HealthStatusDegraded
}

func (ths *TestHealthService) Checker() shared.HealthManager {
	// Return a mock checker for testing
	return &ManagerImpl{}
}

// Health monitoring
func (ths *TestHealthService) Subscribe(callback health.HealthCallback) error {
	ths.mu.Lock()
	defer ths.mu.Unlock()
	ths.recordMethodCall("Subscribe")

	if err := ths.errors["Subscribe"]; err != nil {
		return err
	}

	ths.callbacks = append(ths.callbacks, callback)
	return nil
}

func (ths *TestHealthService) GetLastReport() *health.HealthReport {
	ths.mu.Lock()
	defer ths.mu.Unlock()
	ths.recordMethodCall("GetLastReport")
	return ths.lastReport
}

func (ths *TestHealthService) GetStats() *health.HealthCheckerStats {
	ths.mu.Lock()
	defer ths.mu.Unlock()
	ths.recordMethodCall("GetStats")

	return &health.HealthCheckerStats{
		RegisteredChecks: len(ths.checks),
		Started:          ths.started,
		Uptime:           time.Minute,
		OverallStatus:    ths.status,
	}
}

// Service info
func (ths *TestHealthService) GetVersion() string {
	ths.mu.Lock()
	defer ths.mu.Unlock()
	ths.recordMethodCall("GetVersion")
	return "test-version"
}

func (ths *TestHealthService) GetEnvironment() string {
	ths.mu.Lock()
	defer ths.mu.Unlock()
	ths.recordMethodCall("Environment")
	return "test"
}

func (ths *TestHealthService) GetConfiguration() *health.HealthConfig {
	ths.mu.Lock()
	defer ths.mu.Unlock()
	ths.recordMethodCall("GetConfiguration")
	return ths.configuration
}

// Test helpers
func (ths *TestHealthService) SetStatus(status health.HealthStatus) {
	ths.mu.Lock()
	defer ths.mu.Unlock()
	ths.status = status
}

func (ths *TestHealthService) SetLastReport(report *health.HealthReport) {
	ths.mu.Lock()
	defer ths.mu.Unlock()
	ths.lastReport = report
}

func (ths *TestHealthService) SetError(method string, err error) {
	ths.mu.Lock()
	defer ths.mu.Unlock()
	ths.errors[method] = err
}

func (ths *TestHealthService) GetCallCount(method string) int {
	ths.mu.Lock()
	defer ths.mu.Unlock()
	return ths.callCounts[method]
}

func (ths *TestHealthService) GetMethodCalls() []string {
	ths.mu.Lock()
	defer ths.mu.Unlock()
	calls := make([]string, len(ths.methodCalls))
	copy(calls, ths.methodCalls)
	return calls
}

func (ths *TestHealthService) ResetCalls() {
	ths.mu.Lock()
	defer ths.mu.Unlock()
	ths.callCounts = make(map[string]int)
	ths.methodCalls = make([]string, 0)
}

func (ths *TestHealthService) recordMethodCall(method string) {
	ths.callCounts[method]++
	ths.methodCalls = append(ths.methodCalls, method)
}

// MockHealthCheck is a mock health check for testing
type MockHealthCheck struct {
	name         string
	status       health.HealthStatus
	message      string
	timeout      time.Duration
	critical     bool
	dependencies []string
	callCount    int
	lastCall     time.Time
	result       *health.HealthResult
	checkFunc    func(ctx context.Context) *health.HealthResult
	mu           sync.RWMutex
}

// NewMockHealthCheck creates a new mock health check
func NewMockHealthCheck(name string, status health.HealthStatus, message string) *MockHealthCheck {
	return &MockHealthCheck{
		name:         name,
		status:       status,
		message:      message,
		timeout:      5 * time.Second,
		critical:     false,
		dependencies: []string{},
	}
}

func (mhc *MockHealthCheck) Name() string {
	return mhc.name
}

func (mhc *MockHealthCheck) Timeout() time.Duration {
	return mhc.timeout
}

func (mhc *MockHealthCheck) Critical() bool {
	return mhc.critical
}

func (mhc *MockHealthCheck) Dependencies() []string {
	return mhc.dependencies
}

func (mhc *MockHealthCheck) Check(ctx context.Context) *health.HealthResult {
	mhc.mu.Lock()
	defer mhc.mu.Unlock()

	mhc.callCount++
	mhc.lastCall = time.Now()

	if mhc.checkFunc != nil {
		return mhc.checkFunc(ctx)
	}

	if mhc.result != nil {
		return mhc.result
	}

	result := health.NewHealthResult(mhc.name, mhc.status, mhc.message)
	result.WithCritical(mhc.critical)
	result.WithDuration(time.Millisecond * 10)

	return result
}

// Test helpers for MockHealthCheck
func (mhc *MockHealthCheck) SetStatus(status health.HealthStatus) {
	mhc.mu.Lock()
	defer mhc.mu.Unlock()
	mhc.status = status
}

func (mhc *MockHealthCheck) SetMessage(message string) {
	mhc.mu.Lock()
	defer mhc.mu.Unlock()
	mhc.message = message
}

func (mhc *MockHealthCheck) SetCritical(critical bool) {
	mhc.mu.Lock()
	defer mhc.mu.Unlock()
	mhc.critical = critical
}

func (mhc *MockHealthCheck) SetTimeout(timeout time.Duration) {
	mhc.mu.Lock()
	defer mhc.mu.Unlock()
	mhc.timeout = timeout
}

func (mhc *MockHealthCheck) SetDependencies(deps []string) {
	mhc.mu.Lock()
	defer mhc.mu.Unlock()
	mhc.dependencies = deps
}

func (mhc *MockHealthCheck) SetResult(result *health.HealthResult) {
	mhc.mu.Lock()
	defer mhc.mu.Unlock()
	mhc.result = result
}

func (mhc *MockHealthCheck) SetCheckFunc(fn func(ctx context.Context) *health.HealthResult) {
	mhc.mu.Lock()
	defer mhc.mu.Unlock()
	mhc.checkFunc = fn
}

func (mhc *MockHealthCheck) GetCallCount() int {
	mhc.mu.Lock()
	defer mhc.mu.Unlock()
	return mhc.callCount
}

func (mhc *MockHealthCheck) GetLastCall() time.Time {
	mhc.mu.Lock()
	defer mhc.mu.Unlock()
	return mhc.lastCall
}

func (mhc *MockHealthCheck) ResetCallCount() {
	mhc.mu.Lock()
	defer mhc.mu.Unlock()
	mhc.callCount = 0
}

// TestHealthChecker is a test-friendly health checker
type TestHealthChecker struct {
	shared.HealthManager
	checkResults map[string]*health.HealthResult
	mu           sync.RWMutex
}

// NewTestHealthChecker creates a new test health checker
func NewTestHealthChecker() *TestHealthChecker {
	config := health.DefaultHealthCheckerConfig()
	config.CheckInterval = 100 * time.Millisecond // Fast interval for testing

	return &TestHealthChecker{
		HealthManager: New(config, logger.NewNoopLogger(), &testMetrics{}, nil),
		checkResults:  make(map[string]*health.HealthResult),
	}
}

// SetCheckResult sets a predefined result for a specific check
func (thc *TestHealthChecker) SetCheckResult(name string, result *health.HealthResult) {
	thc.mu.Lock()
	defer thc.mu.Unlock()
	thc.checkResults[name] = result
}

// testMetrics is a simple metrics collector for testing
type testMetrics struct{}

func (tm *testMetrics) RegisterCollector(collector shared.CustomCollector) error {
	// Test implementation - no-op for testing
	return nil
}

func (tm *testMetrics) UnregisterCollector(name string) error {
	// Test implementation - no-op for testing
	return nil
}

func (tm *testMetrics) Counter(name string, tags ...string) shared.Counter { return &testCounter{} }
func (tm *testMetrics) Gauge(name string, tags ...string) shared.Gauge     { return &testGauge{} }
func (tm *testMetrics) Histogram(name string, tags ...string) shared.Histogram {
	return &testHistogram{}
}
func (tm *testMetrics) Timer(name string, tags ...string) shared.Timer { return &testTimer{} }

func (tm *testMetrics) Name() string {
	// Test implementation - return test name
	return "test-metrics"
}

func (tm *testMetrics) Dependencies() []string {
	// Test implementation - return empty dependencies
	return []string{}
}

func (tm *testMetrics) Start(ctx context.Context) error {
	// Test implementation - no-op for testing
	return nil
}

func (tm *testMetrics) Stop(ctx context.Context) error {
	// Test implementation - no-op for testing
	return nil
}

func (tm *testMetrics) Health(ctx context.Context) error {
	// Test implementation - always healthy for testing
	return nil
}

func (tm *testMetrics) Register(collector shared.CustomCollector) error {
	// Test implementation - no-op for testing
	return nil
}

func (tm *testMetrics) Unregister(name string) error {
	// Test implementation - no-op for testing
	return nil
}

func (tm *testMetrics) GetCollectors() []shared.CustomCollector {
	// Test implementation - return empty list
	return []shared.CustomCollector{}
}

func (tm *testMetrics) GetMetrics() map[string]interface{} {
	// Test implementation - return empty metrics
	return map[string]interface{}{}
}

func (tm *testMetrics) GetMetricsByType(metricType shared.MetricType) map[string]interface{} {
	// Test implementation - return empty metrics
	return map[string]interface{}{}
}

func (tm *testMetrics) GetMetricsByTag(tagKey, tagValue string) map[string]interface{} {
	// Test implementation - return empty metrics
	return map[string]interface{}{}
}

func (tm *testMetrics) Export(format shared.ExportFormat) ([]byte, error) {
	// Test implementation - return empty export
	return []byte("{}"), nil
}

func (tm *testMetrics) ExportToFile(format shared.ExportFormat, filename string) error {
	// Test implementation - no-op for testing
	return nil
}

func (tm *testMetrics) Reset() error {
	// Test implementation - no-op for testing
	return nil
}

func (tm *testMetrics) ResetMetric(name string) error {
	// Test implementation - no-op for testing
	return nil
}

func (tm *testMetrics) GetStats() shared.CollectorStats {
	// Test implementation - return empty stats
	return shared.CollectorStats{}
}

func (tm *testMetrics) Reload(config *shared.MetricsConfig) error {
	return nil
}

type testCounter struct{}

func (tc *testCounter) WithLabels(labels map[string]string) shared.Counter {
	return tc
}

func (tc *testCounter) Dec() {
}

func (tc *testCounter) Get() float64 {
	return 0
}

func (tc *testCounter) Reset() error {
	return nil
}

func (tc *testCounter) Inc()              {}
func (tc *testCounter) Add(value float64) {}

type testGauge struct{}

func (tg *testGauge) WithLabels(labels map[string]string) shared.Gauge {
	// TODO implement me
	panic("implement me")
}

func (tg *testGauge) Get() float64 {
	return 0
}

func (tg *testGauge) Reset() error {
	return nil
}

func (tg *testGauge) Set(value float64) {}
func (tg *testGauge) Inc()              {}
func (tg *testGauge) Dec()              {}
func (tg *testGauge) Add(value float64) {}

type testHistogram struct{}

func (th *testHistogram) ObserveDuration(start time.Time) {
	// Test implementation - no-op
}

func (th *testHistogram) WithLabels(labels map[string]string) shared.Histogram {
	return th
}

func (th *testHistogram) GetBuckets() map[float64]uint64 {
	return make(map[float64]uint64)
}

func (th *testHistogram) GetCount() uint64 {
	return 0
}

func (th *testHistogram) GetSum() float64 {
	return 0
}

func (th *testHistogram) GetMean() float64 {
	return 0
}

func (th *testHistogram) GetPercentile(percentile float64) float64 {
	return 0
}

func (th *testHistogram) Reset() error {
	return nil
}

func (th *testHistogram) Observe(value float64) {}

type testTimer struct{}

func (tt *testTimer) Get() time.Duration {
	return 0
}

func (tt *testTimer) GetCount() uint64 {
	return 0
}

func (tt *testTimer) GetMean() time.Duration {
	return 0
}

func (tt *testTimer) GetPercentile(percentile float64) time.Duration {
	// TODO implement me
	panic("implement me")
}

func (tt *testTimer) GetMin() time.Duration {
	// TODO implement me
	panic("implement me")
}

func (tt *testTimer) GetMax() time.Duration {
	// TODO implement me
	panic("implement me")
}

func (tt *testTimer) Reset() {
	// TODO implement me
	panic("implement me")
}

func (tt *testTimer) Record(duration time.Duration) {}
func (tt *testTimer) Time() func()                  { return func() {} }

// HealthTestSuite provides a test suite for health checks
type HealthTestSuite struct {
	t             *testing.T
	healthService *TestHealthService
	checks        map[string]*MockHealthCheck
	started       bool
}

// NewHealthTestSuite creates a new health test suite
func NewHealthTestSuite(t *testing.T) *HealthTestSuite {
	return &HealthTestSuite{
		t:             t,
		healthService: NewTestHealthService("test-service"),
		checks:        make(map[string]*MockHealthCheck),
	}
}

// AddHealthCheck adds a health check to the test suite
func (hts *HealthTestSuite) AddHealthCheck(name string, status health.HealthStatus, message string) *MockHealthCheck {
	check := NewMockHealthCheck(name, status, message)
	hts.checks[name] = check

	if err := hts.healthService.RegisterCheck(name, check); err != nil {
		hts.t.Fatalf("Failed to register health check %s: %v", name, err)
	}

	return check
}

// Start starts the health service
func (hts *HealthTestSuite) Start() {
	if hts.started {
		return
	}

	ctx := context.Background()
	if err := hts.healthService.Start(ctx); err != nil {
		hts.t.Fatalf("Failed to start health service: %v", err)
	}

	hts.started = true
}

// Stop stops the health service
func (hts *HealthTestSuite) Stop() {
	if !hts.started {
		return
	}

	ctx := context.Background()
	if err := hts.healthService.Stop(ctx); err != nil {
		hts.t.Fatalf("Failed to stop health service: %v", err)
	}

	hts.started = false
}

// AssertHealthy asserts that the service is healthy
func (hts *HealthTestSuite) AssertHealthy() {
	status := hts.healthService.GetStatus()
	if status != health.HealthStatusHealthy {
		hts.t.Errorf("Expected healthy status, got %s", status)
	}
}

// AssertUnhealthy asserts that the service is unhealthy
func (hts *HealthTestSuite) AssertUnhealthy() {
	status := hts.healthService.GetStatus()
	if status != health.HealthStatusUnhealthy {
		hts.t.Errorf("Expected unhealthy status, got %s", status)
	}
}

// AssertDegraded asserts that the service is degraded
func (hts *HealthTestSuite) AssertDegraded() {
	status := hts.healthService.GetStatus()
	if status != health.HealthStatusDegraded {
		hts.t.Errorf("Expected degraded status, got %s", status)
	}
}

// AssertCheckCalled asserts that a specific check was called
func (hts *HealthTestSuite) AssertCheckCalled(name string, expectedCalls int) {
	if check, exists := hts.checks[name]; exists {
		actualCalls := check.GetCallCount()
		if actualCalls != expectedCalls {
			hts.t.Errorf("Expected check %s to be called %d times, got %d", name, expectedCalls, actualCalls)
		}
	} else {
		hts.t.Errorf("Check %s not found", name)
	}
}

// AssertMethodCalled asserts that a service method was called
func (hts *HealthTestSuite) AssertMethodCalled(method string, expectedCalls int) {
	actualCalls := hts.healthService.GetCallCount(method)
	if actualCalls != expectedCalls {
		hts.t.Errorf("Expected method %s to be called %d times, got %d", method, expectedCalls, actualCalls)
	}
}

// GetHealthService returns the test health service
func (hts *HealthTestSuite) GetHealthService() *TestHealthService {
	return hts.healthService
}

// GetHealthCheck returns a specific health check
func (hts *HealthTestSuite) GetHealthCheck(name string) *MockHealthCheck {
	return hts.checks[name]
}

// Cleanup cleans up the test suite
func (hts *HealthTestSuite) Cleanup() {
	if hts.started {
		hts.Stop()
	}

	// Reset all checks
	for _, check := range hts.checks {
		check.ResetCallCount()
	}

	hts.healthService.ResetCalls()
}

// HealthTestBuilder helps build health test scenarios
type HealthTestBuilder struct {
	suite *HealthTestSuite
}

// NewHealthTestBuilder creates a new health test builder
func NewHealthTestBuilder(t *testing.T) *HealthTestBuilder {
	return &HealthTestBuilder{
		suite: NewHealthTestSuite(t),
	}
}

// WithHealthyCheck adds a healthy check
func (htb *HealthTestBuilder) WithHealthyCheck(name string) *HealthTestBuilder {
	htb.suite.AddHealthCheck(name, health.HealthStatusHealthy, "healthy")
	return htb
}

// WithUnhealthyCheck adds an unhealthy check
func (htb *HealthTestBuilder) WithUnhealthyCheck(name string) *HealthTestBuilder {
	htb.suite.AddHealthCheck(name, health.HealthStatusUnhealthy, "unhealthy")
	return htb
}

// WithDegradedCheck adds a degraded check
func (htb *HealthTestBuilder) WithDegradedCheck(name string) *HealthTestBuilder {
	htb.suite.AddHealthCheck(name, health.HealthStatusDegraded, "degraded")
	return htb
}

// WithCriticalCheck adds a critical check
func (htb *HealthTestBuilder) WithCriticalCheck(name string, status health.HealthStatus) *HealthTestBuilder {
	check := htb.suite.AddHealthCheck(name, status, "critical check")
	check.SetCritical(true)
	return htb
}

// WithCustomCheck adds a custom check
func (htb *HealthTestBuilder) WithCustomCheck(name string, checkFunc func(ctx context.Context) *health.HealthResult) *HealthTestBuilder {
	check := htb.suite.AddHealthCheck(name, health.HealthStatusHealthy, "custom check")
	check.SetCheckFunc(checkFunc)
	return htb
}

// WithSlowCheck adds a slow check for timeout testing
func (htb *HealthTestBuilder) WithSlowCheck(name string, delay time.Duration) *HealthTestBuilder {
	check := htb.suite.AddHealthCheck(name, health.HealthStatusHealthy, "slow check")
	check.SetCheckFunc(func(ctx context.Context) *health.HealthResult {
		select {
		case <-time.After(delay):
			return health.NewHealthResult(name, health.HealthStatusHealthy, "slow but healthy")
		case <-ctx.Done():
			return health.NewHealthResult(name, health.HealthStatusUnhealthy, "timeout")
		}
	})
	return htb
}

// Build builds the test suite
func (htb *HealthTestBuilder) Build() *HealthTestSuite {
	return htb.suite
}

// Test helper functions

// WaitForHealthStatus waits for a specific health status with timeout
func WaitForHealthStatus(t *testing.T, service health.HealthService, expected health.HealthStatus, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			t.Fatalf("Timeout waiting for health status %s", expected)
		case <-ticker.C:
			if service.GetStatus() == expected {
				return
			}
		}
	}
}

// AssertHealthResultEqual asserts that two health results are equal
func AssertHealthResultEqual(t *testing.T, expected, actual *health.HealthResult) {
	if expected.Name != actual.Name {
		t.Errorf("Expected name %s, got %s", expected.Name, actual.Name)
	}
	if expected.Status != actual.Status {
		t.Errorf("Expected status %s, got %s", expected.Status, actual.Status)
	}
	if expected.Message != actual.Message {
		t.Errorf("Expected message %s, got %s", expected.Message, actual.Message)
	}
	if expected.Critical != actual.Critical {
		t.Errorf("Expected critical %t, got %t", expected.Critical, actual.Critical)
	}
}

// AssertHealthReportEqual asserts that two health reports are equal
func AssertHealthReportEqual(t *testing.T, expected, actual *health.HealthReport) {
	if expected.Overall != actual.Overall {
		t.Errorf("Expected overall status %s, got %s", expected.Overall, actual.Overall)
	}
	if len(expected.Services) != len(actual.Services) {
		t.Errorf("Expected %d services, got %d", len(expected.Services), len(actual.Services))
	}
	for name, expectedResult := range expected.Services {
		if actualResult, exists := actual.Services[name]; exists {
			AssertHealthResultEqual(t, expectedResult, actualResult)
		} else {
			t.Errorf("Missing service %s in actual report", name)
		}
	}
}

// CreateTestHealthResult creates a test health result
func CreateTestHealthResult(name string, status health.HealthStatus, message string) *health.HealthResult {
	return health.NewHealthResult(name, status, message).
		WithDuration(time.Millisecond*10).
		WithDetail("test", true).
		WithTag("environment", "test")
}

// CreateTestHealthReport creates a test health report
func CreateTestHealthReport(overall health.HealthStatus, services map[string]*health.HealthResult) *health.HealthReport {
	report := health.NewHealthReport()
	report.Overall = overall

	for name, result := range services {
		report.Services[name] = result
	}

	return report.
		WithVersion("test-version").
		WithEnvironment("test").
		WithHostname("test-host").
		WithUptime(time.Hour).
		WithDuration(time.Millisecond * 50)
}

// Benchmarking helpers

// BenchmarkHealthCheck benchmarks a health check
func BenchmarkHealthCheck(b *testing.B, check health.HealthCheck) {
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		check.Check(ctx)
	}
}

// BenchmarkHealthService benchmarks a health service
func BenchmarkHealthService(b *testing.B, service health.HealthService) {
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		service.Check(ctx)
	}
}

// Example usage functions

// ExampleHealthTest demonstrates how to use the health testing utilities
func ExampleHealthTest(t *testing.T) {
	// Create a test suite
	suite := NewHealthTestBuilder(t).
		WithHealthyCheck("database").
		WithUnhealthyCheck("cache").
		WithDegradedCheck("external-api").
		WithCriticalCheck("auth-service", health.HealthStatusHealthy).
		Build()

	// Start the service
	suite.Start()
	defer suite.Cleanup()

	// Test overall health
	suite.AssertUnhealthy() // Should be unhealthy due to cache

	// Test individual checks
	suite.AssertCheckCalled("database", 0) // Not called yet

	// Perform health check
	ctx := context.Background()
	report := suite.GetHealthService().CheckAll(ctx)

	// Assert results
	if report.Overall != health.HealthStatusUnhealthy {
		t.Errorf("Expected unhealthy overall status")
	}

	// Check specific services
	if report.Services["database"].Status != health.HealthStatusHealthy {
		t.Errorf("Expected database to be healthy")
	}

	if report.Services["cache"].Status != health.HealthStatusUnhealthy {
		t.Errorf("Expected cache to be unhealthy")
	}
}
