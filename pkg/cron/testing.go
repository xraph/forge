package cron

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/cron/election"
	"github.com/xraph/forge/pkg/cron/persistence"
	"github.com/xraph/forge/pkg/events"
	"github.com/xraph/forge/pkg/health"
	"github.com/xraph/forge/pkg/logger"
)

// TestManager provides a test-friendly cron manager implementation
type TestManager struct {
	*CronManager
	testConfig *TestConfig
	cleanup    []func()
	mu         sync.RWMutex
}

// TestConfig contains configuration for testing
type TestConfig struct {
	NodeID            string
	ClusterID         string
	MemoryStore       bool
	DisableElection   bool
	DisableMetrics    bool
	FastScheduling    bool
	JobTimeout        time.Duration
	MaxConcurrentJobs int
}

// NewTestManager creates a new test cron manager
func NewTestManager(config *TestConfig) (*TestManager, error) {
	if config == nil {
		config = DefaultTestConfig()
	}

	// Create test logger
	testLogger := logger.NewTestLogger()

	// Create test metrics
	testMetrics := &TestMetrics{}

	// Create test event bus
	testEventBus := &TestEventBus{}

	// Create test health checker
	testHealthChecker := &TestHealthChecker{}

	// Create cron config
	cronConfig := &CronConfig{
		NodeID:             config.NodeID,
		ClusterID:          config.ClusterID,
		MaxConcurrentJobs:  config.MaxConcurrentJobs,
		JobCheckInterval:   time.Millisecond * 100, // Fast for testing
		EnableMetrics:      !config.DisableMetrics,
		EnableHealthChecks: true,
	}

	// Configure components for testing
	if config.MemoryStore {
		cronConfig.StoreConfig = &persistence.StoreConfig{
			DatabaseURL: "memory://",
		}
	}

	if config.DisableElection {
		cronConfig.LeaderElectionConfig = &election.Config{
			BackendType: "raft",
			// Type:     "single",
			HeartbeatInterval: time.Millisecond * 50,
		}
	}

	if config.FastScheduling {
		cronConfig.JobCheckInterval = time.Millisecond * 10
	}

	// Create cron manager
	manager, err := NewCronManager(cronConfig, testLogger, testMetrics, testEventBus, health.NewTestHealthChecker())
	if err != nil {
		return nil, err
	}

	testManager := &TestManager{
		CronManager: manager,
		testConfig:  config,
		cleanup:     make([]func(), 0),
	}

	return testManager, nil
}

// RegisterTestJob registers a test job with the manager
func (tm *TestManager) RegisterTestJob(id, name, schedule string, handler JobHandler) error {
	jobDef := JobDefinition{
		ID:         id,
		Name:       name,
		Schedule:   schedule,
		Handler:    handler,
		Enabled:    true,
		MaxRetries: 3,
		Timeout:    tm.testConfig.JobTimeout,
		Priority:   0,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	return tm.RegisterJob(jobDef)
}

// RegisterTestHandler registers a test handler
func (tm *TestManager) RegisterTestHandler(id string, handler JobHandler) error {
	return tm.handlerRegistry.Register(id, handler)
}

// WaitForJobExecution waits for a job to be executed
func (tm *TestManager) WaitForJobExecution(jobID string, timeout time.Duration) (*JobExecution, error) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		history, err := tm.GetJobHistory(jobID, 1)
		if err != nil {
			return nil, err
		}

		if len(history) > 0 && history[0].IsComplete() {
			return &history[0], nil
		}

		time.Sleep(time.Millisecond * 10)
	}

	return nil, fmt.Errorf("job %s did not complete within timeout", jobID)
}

// WaitForJobStatus waits for a job to reach a specific status
func (tm *TestManager) WaitForJobStatus(jobID string, status JobStatus, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		job, err := tm.GetJob(jobID)
		if err != nil {
			return err
		}

		if job.Status == status {
			return nil
		}

		time.Sleep(time.Millisecond * 10)
	}

	return fmt.Errorf("job %s did not reach status %s within timeout", jobID, status)
}

// GetExecutionCount returns the number of executions for a job
func (tm *TestManager) GetExecutionCount(jobID string) (int, error) {
	history, err := tm.GetJobHistory(jobID, 1000)
	if err != nil {
		return 0, err
	}
	return len(history), nil
}

// GetSuccessfulExecutions returns successful executions for a job
func (tm *TestManager) GetSuccessfulExecutions(jobID string) ([]JobExecution, error) {
	history, err := tm.GetJobHistory(jobID, 1000)
	if err != nil {
		return nil, err
	}

	var successful []JobExecution
	for _, exec := range history {
		if exec.IsSuccess() {
			successful = append(successful, exec)
		}
	}

	return successful, nil
}

// GetFailedExecutions returns failed executions for a job
func (tm *TestManager) GetFailedExecutions(jobID string) ([]JobExecution, error) {
	history, err := tm.GetJobHistory(jobID, 1000)
	if err != nil {
		return nil, err
	}

	var failed []JobExecution
	for _, exec := range history {
		if exec.IsFailure() {
			failed = append(failed, exec)
		}
	}

	return failed, nil
}

// Cleanup performs cleanup operations
func (tm *TestManager) Cleanup() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	for _, cleanup := range tm.cleanup {
		cleanup()
	}
	tm.cleanup = nil
}

// AddCleanup adds a cleanup function
func (tm *TestManager) AddCleanup(cleanup func()) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.cleanup = append(tm.cleanup, cleanup)
}

// MockJobHandler provides a mock job handler for testing
type MockJobHandler struct {
	*BaseJobHandler
	executions     []JobExecution
	shouldFail     bool
	failureError   error
	executionDelay time.Duration
	callCount      int
	mu             sync.RWMutex
}

// NewMockJobHandler creates a new mock job handler
func NewMockJobHandler(name string) *MockJobHandler {
	return &MockJobHandler{
		BaseJobHandler: NewBaseJobHandler(name, "Mock job handler for testing"),
		executions:     make([]JobExecution, 0),
	}
}

// Execute executes the mock job
func (mjh *MockJobHandler) Execute(ctx context.Context, job *Job) error {
	mjh.mu.Lock()
	defer mjh.mu.Unlock()

	mjh.callCount++

	if mjh.executionDelay > 0 {
		time.Sleep(mjh.executionDelay)
	}

	if mjh.shouldFail {
		return mjh.failureError
	}

	return nil
}

// SetShouldFail sets whether the handler should fail
func (mjh *MockJobHandler) SetShouldFail(shouldFail bool, err error) {
	mjh.mu.Lock()
	defer mjh.mu.Unlock()
	mjh.shouldFail = shouldFail
	mjh.failureError = err
}

// SetExecutionDelay sets the execution delay
func (mjh *MockJobHandler) SetExecutionDelay(delay time.Duration) {
	mjh.mu.Lock()
	defer mjh.mu.Unlock()
	mjh.executionDelay = delay
}

// GetCallCount returns the number of times Execute was called
func (mjh *MockJobHandler) GetCallCount() int {
	mjh.mu.RLock()
	defer mjh.mu.RUnlock()
	return mjh.callCount
}

// Reset resets the mock handler state
func (mjh *MockJobHandler) Reset() {
	mjh.mu.Lock()
	defer mjh.mu.Unlock()
	mjh.callCount = 0
	mjh.shouldFail = false
	mjh.failureError = nil
	mjh.executionDelay = 0
	mjh.executions = make([]JobExecution, 0)
}

// TestJobHandler provides a test job handler
type TestJobHandler struct {
	*BaseJobHandler
	ExecuteFunc   func(ctx context.Context, job *Job) error
	ValidateFunc  func(job *Job) error
	OnSuccessFunc func(ctx context.Context, job *Job, execution *JobExecution) error
	OnFailureFunc func(ctx context.Context, job *Job, execution *JobExecution, err error) error
	OnTimeoutFunc func(ctx context.Context, job *Job, execution *JobExecution) error
	OnRetryFunc   func(ctx context.Context, job *Job, execution *JobExecution, attempt int) error
}

// NewTestJobHandler creates a new test job handler
func NewTestJobHandler(name string) *TestJobHandler {
	return &TestJobHandler{
		BaseJobHandler: NewBaseJobHandler(name, "Test job handler"),
	}
}

// Execute executes the test job
func (tjh *TestJobHandler) Execute(ctx context.Context, job *Job) error {
	if tjh.ExecuteFunc != nil {
		return tjh.ExecuteFunc(ctx, job)
	}
	return tjh.BaseJobHandler.Execute(ctx, job)
}

// Validate validates the test job
func (tjh *TestJobHandler) Validate(job *Job) error {
	if tjh.ValidateFunc != nil {
		return tjh.ValidateFunc(job)
	}
	return tjh.BaseJobHandler.Validate(job)
}

// OnSuccess handles successful execution
func (tjh *TestJobHandler) OnSuccess(ctx context.Context, job *Job, execution *JobExecution) error {
	if tjh.OnSuccessFunc != nil {
		return tjh.OnSuccessFunc(ctx, job, execution)
	}
	return tjh.BaseJobHandler.OnSuccess(ctx, job, execution)
}

// OnFailure handles failed execution
func (tjh *TestJobHandler) OnFailure(ctx context.Context, job *Job, execution *JobExecution, err error) error {
	if tjh.OnFailureFunc != nil {
		return tjh.OnFailureFunc(ctx, job, execution, err)
	}
	return tjh.BaseJobHandler.OnFailure(ctx, job, execution, err)
}

// OnTimeout handles timeout
func (tjh *TestJobHandler) OnTimeout(ctx context.Context, job *Job, execution *JobExecution) error {
	if tjh.OnTimeoutFunc != nil {
		return tjh.OnTimeoutFunc(ctx, job, execution)
	}
	return tjh.BaseJobHandler.OnTimeout(ctx, job, execution)
}

// OnRetry handles retry
func (tjh *TestJobHandler) OnRetry(ctx context.Context, job *Job, execution *JobExecution, attempt int) error {
	if tjh.OnRetryFunc != nil {
		return tjh.OnRetryFunc(ctx, job, execution, attempt)
	}
	return tjh.BaseJobHandler.OnRetry(ctx, job, execution, attempt)
}

// TestMetrics provides a test metrics implementation
type TestMetrics struct {
	counters   map[string]float64
	gauges     map[string]float64
	histograms map[string][]float64
	timers     map[string][]time.Duration
	mu         sync.RWMutex
}

// NewTestMetrics creates a new test metrics instance
func NewTestMetrics() *TestMetrics {
	return &TestMetrics{
		counters:   make(map[string]float64),
		gauges:     make(map[string]float64),
		histograms: make(map[string][]float64),
		timers:     make(map[string][]time.Duration),
	}
}

// Counter returns a test counter
func (tm *TestMetrics) Counter(name string, tags ...string) common.Counter {
	return &TestCounter{
		name:    name,
		metrics: tm,
	}
}

// Gauge returns a test gauge
func (tm *TestMetrics) Gauge(name string, tags ...string) common.Gauge {
	return &TestGauge{
		name:    name,
		metrics: tm,
	}
}

// Histogram returns a test histogram
func (tm *TestMetrics) Histogram(name string, tags ...string) common.Histogram {
	return &TestHistogram{
		name:    name,
		metrics: tm,
	}
}

// Timer returns a test timer
func (tm *TestMetrics) Timer(name string, tags ...string) common.Timer {
	return &TestTimer{
		name:    name,
		metrics: tm,
	}
}

// GetCounterValue returns the value of a counter
func (tm *TestMetrics) GetCounterValue(name string) float64 {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.counters[name]
}

// GetGaugeValue returns the value of a gauge
func (tm *TestMetrics) GetGaugeValue(name string) float64 {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.gauges[name]
}

// GetHistogramValues returns the values of a histogram
func (tm *TestMetrics) GetHistogramValues(name string) []float64 {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	values := tm.histograms[name]
	result := make([]float64, len(values))
	copy(result, values)
	return result
}

// GetTimerValues returns the values of a timer
func (tm *TestMetrics) GetTimerValues(name string) []time.Duration {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	values := tm.timers[name]
	result := make([]time.Duration, len(values))
	copy(result, values)
	return result
}

// TestCounter implements core.Counter for testing
type TestCounter struct {
	name    string
	metrics *TestMetrics
}

// Inc increments the counter
func (tc *TestCounter) Inc() {
	tc.Add(1)
}

// Add adds to the counter
func (tc *TestCounter) Add(value float64) {
	tc.metrics.mu.Lock()
	defer tc.metrics.mu.Unlock()
	tc.metrics.counters[tc.name] += value
}

// TestGauge implements core.Gauge for testing
type TestGauge struct {
	name    string
	metrics *TestMetrics
}

// Set sets the gauge value
func (tg *TestGauge) Set(value float64) {
	tg.metrics.mu.Lock()
	defer tg.metrics.mu.Unlock()
	tg.metrics.gauges[tg.name] = value
}

// Inc increments the gauge
func (tg *TestGauge) Inc() {
	tg.Add(1)
}

// Dec decrements the gauge
func (tg *TestGauge) Dec() {
	tg.Add(-1)
}

// Add adds to the gauge
func (tg *TestGauge) Add(value float64) {
	tg.metrics.mu.Lock()
	defer tg.metrics.mu.Unlock()
	tg.metrics.gauges[tg.name] += value
}

// TestHistogram implements core.Histogram for testing
type TestHistogram struct {
	name    string
	metrics *TestMetrics
}

// Observe records an observation
func (th *TestHistogram) Observe(value float64) {
	th.metrics.mu.Lock()
	defer th.metrics.mu.Unlock()
	th.metrics.histograms[th.name] = append(th.metrics.histograms[th.name], value)
}

// TestTimer implements core.Timer for testing
type TestTimer struct {
	name    string
	metrics *TestMetrics
}

// Record records a duration
func (tt *TestTimer) Record(duration time.Duration) {
	tt.metrics.mu.Lock()
	defer tt.metrics.mu.Unlock()
	tt.metrics.timers[tt.name] = append(tt.metrics.timers[tt.name], duration)
}

// Time returns a function that records the duration when called
func (tt *TestTimer) Time() func() {
	start := time.Now()
	return func() {
		tt.Record(time.Since(start))
	}
}

// TestEventBus provides a test event bus implementation
type TestEventBus struct {
	events []events.Event
	mu     sync.RWMutex
}

// Publish publishes an event
func (teb *TestEventBus) Publish(ctx context.Context, event events.Event) error {
	teb.mu.Lock()
	defer teb.mu.Unlock()
	teb.events = append(teb.events, event)
	return nil
}

// Subscribe subscribes to events
func (teb *TestEventBus) Subscribe(eventType string, handler events.EventHandler) error {
	// No-op for testing
	return nil
}

// GetEvents returns all published events
func (teb *TestEventBus) GetEvents() []events.Event {
	teb.mu.RLock()
	defer teb.mu.RUnlock()

	events := make([]events.Event, len(teb.events))
	copy(events, teb.events)
	return events
}

// GetEventsByType returns events filtered by type
func (teb *TestEventBus) GetEventsByType(eventType string) []events.Event {
	teb.mu.RLock()
	defer teb.mu.RUnlock()

	var filtered []events.Event
	for _, event := range teb.events {
		if event.Type == eventType {
			filtered = append(filtered, event)
		}
	}
	return filtered
}

// Clear clears all events
func (teb *TestEventBus) Clear() {
	teb.mu.Lock()
	defer teb.mu.Unlock()
	teb.events = nil
}

// TestHealthChecker provides a test health checker implementation
type TestHealthChecker struct {
	healthy bool
	mu      sync.RWMutex
}

// Check performs a health check
func (thc *TestHealthChecker) Check(ctx context.Context) common.HealthResult {
	thc.mu.RLock()
	defer thc.mu.RUnlock()

	status := common.HealthStatusHealthy
	if !thc.healthy {
		status = common.HealthStatusUnhealthy
	}

	return common.HealthResult{
		Status:    status,
		Message:   "Test health check",
		Timestamp: time.Now(),
	}
}

// SetHealthy sets the health status
func (thc *TestHealthChecker) SetHealthy(healthy bool) {
	thc.mu.Lock()
	defer thc.mu.Unlock()
	thc.healthy = healthy
}

// TestDatabase provides a test database implementation
type TestDatabase struct {
	*sql.DB
	queries map[string]interface{}
	mu      sync.RWMutex
}

// NewTestDatabase creates a new test database
func NewTestDatabase() *TestDatabase {
	return &TestDatabase{
		queries: make(map[string]interface{}),
	}
}

// RecordQuery records a query execution
func (tdb *TestDatabase) RecordQuery(query string, args ...interface{}) {
	tdb.mu.Lock()
	defer tdb.mu.Unlock()
	tdb.queries[query] = args
}

// GetQueries returns all recorded queries
func (tdb *TestDatabase) GetQueries() map[string]interface{} {
	tdb.mu.RLock()
	defer tdb.mu.RUnlock()

	queries := make(map[string]interface{})
	for k, v := range tdb.queries {
		queries[k] = v
	}
	return queries
}

// TestStore provides a test store implementation
type TestStore struct {
	jobs       map[string]*Job
	executions map[string]*JobExecution
	events     map[string]*JobEvent
	mu         sync.RWMutex
}

// NewTestStore creates a new test store
func NewTestStore() *TestStore {
	return &TestStore{
		jobs:       make(map[string]*Job),
		executions: make(map[string]*JobExecution),
		events:     make(map[string]*JobEvent),
	}
}

// CreateJob creates a job
func (ts *TestStore) CreateJob(ctx context.Context, job *Job) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.jobs[job.Definition.ID] = job
	return nil
}

// UpdateJob updates a job
func (ts *TestStore) UpdateJob(ctx context.Context, job *Job) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if _, exists := ts.jobs[job.Definition.ID]; !exists {
		return common.ErrServiceNotFound(job.Definition.ID)
	}
	ts.jobs[job.Definition.ID] = job
	return nil
}

// GetJob retrieves a job
func (ts *TestStore) GetJob(ctx context.Context, jobID string) (*Job, error) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	job, exists := ts.jobs[jobID]
	if !exists {
		return nil, common.ErrServiceNotFound(jobID)
	}
	return job, nil
}

// GetJobs retrieves all jobs
func (ts *TestStore) GetJobs(ctx context.Context, filter *JobFilter) ([]*Job, error) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	var jobs []*Job
	for _, job := range ts.jobs {
		jobs = append(jobs, job)
	}
	return jobs, nil
}

// DeleteJob deletes a job
func (ts *TestStore) DeleteJob(ctx context.Context, jobID string) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if _, exists := ts.jobs[jobID]; !exists {
		return common.ErrServiceNotFound(jobID)
	}
	delete(ts.jobs, jobID)
	return nil
}

// CreateExecution creates an execution
func (ts *TestStore) CreateExecution(ctx context.Context, execution *JobExecution) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.executions[execution.ID] = execution
	return nil
}

// UpdateExecution updates an execution
func (ts *TestStore) UpdateExecution(ctx context.Context, execution *JobExecution) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if _, exists := ts.executions[execution.ID]; !exists {
		return common.ErrServiceNotFound(execution.ID)
	}
	ts.executions[execution.ID] = execution
	return nil
}

// GetExecution retrieves an execution
func (ts *TestStore) GetExecution(ctx context.Context, executionID string) (*JobExecution, error) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	execution, exists := ts.executions[executionID]
	if !exists {
		return nil, common.ErrServiceNotFound(executionID)
	}
	return execution, nil
}

// GetExecutionsByJob retrieves executions for a job
func (ts *TestStore) GetExecutionsByJob(ctx context.Context, jobID string, limit int) ([]*JobExecution, error) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	var executions []*JobExecution
	for _, execution := range ts.executions {
		if execution.JobID == jobID {
			executions = append(executions, execution)
			if len(executions) >= limit {
				break
			}
		}
	}
	return executions, nil
}

// Helper functions

// DefaultTestConfig returns default test configuration
func DefaultTestConfig() *TestConfig {
	return &TestConfig{
		NodeID:            "test-node-1",
		ClusterID:         "test-cluster",
		MemoryStore:       true,
		DisableElection:   true,
		DisableMetrics:    false,
		FastScheduling:    true,
		JobTimeout:        time.Second * 30,
		MaxConcurrentJobs: 5,
	}
}

// CreateTestJobDefinition creates a test job definition
func CreateTestJobDefinition(id, name, schedule string) JobDefinition {
	return JobDefinition{
		ID:         id,
		Name:       name,
		Schedule:   schedule,
		Handler:    NewMockJobHandler(name),
		Enabled:    true,
		MaxRetries: 3,
		Timeout:    time.Second * 10,
		Priority:   0,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
}

// WaitForCondition waits for a condition to be true
func WaitForCondition(condition func() bool, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if condition() {
			return nil
		}
		time.Sleep(time.Millisecond * 10)
	}

	return fmt.Errorf("condition not met within timeout")
}

// AssertEventuallyTrue asserts that a condition becomes true within a timeout
func AssertEventuallyTrue(condition func() bool, timeout time.Duration, message string) error {
	if err := WaitForCondition(condition, timeout); err != nil {
		return fmt.Errorf("%s: %w", message, err)
	}
	return nil
}

// AssertJobExists asserts that a job exists
func AssertJobExists(manager *TestManager, jobID string) error {
	_, err := manager.GetJob(jobID)
	if err != nil {
		return fmt.Errorf("job %s does not exist: %w", jobID, err)
	}
	return nil
}

// AssertJobStatus asserts that a job has a specific status
func AssertJobStatus(manager *TestManager, jobID string, expectedStatus JobStatus) error {
	job, err := manager.GetJob(jobID)
	if err != nil {
		return fmt.Errorf("failed to get job %s: %w", jobID, err)
	}

	if job.Status != expectedStatus {
		return fmt.Errorf("job %s has status %s, expected %s", jobID, job.Status, expectedStatus)
	}

	return nil
}

// AssertExecutionCount asserts that a job has a specific number of executions
func AssertExecutionCount(manager *TestManager, jobID string, expectedCount int) error {
	count, err := manager.GetExecutionCount(jobID)
	if err != nil {
		return fmt.Errorf("failed to get execution count for job %s: %w", jobID, err)
	}

	if count != expectedCount {
		return fmt.Errorf("job %s has %d executions, expected %d", jobID, count, expectedCount)
	}

	return nil
}

// AssertMetricValue asserts that a metric has a specific value
func AssertMetricValue(metrics *TestMetrics, metricName string, expectedValue float64) error {
	value := metrics.GetCounterValue(metricName)
	if value != expectedValue {
		return fmt.Errorf("metric %s has value %f, expected %f", metricName, value, expectedValue)
	}
	return nil
}

// AssertLogContains asserts that the log contains a specific message
func AssertLogContains(l logger.Logger, level, message string) error {
	logs := l.(*logger.TestLogger).GetLogsByLevel(level)
	for _, log := range logs {
		if log.Message == message {
			return nil
		}
	}
	return fmt.Errorf("log does not contain %s message: %s", level, message)
}

// AssertEventPublished asserts that an event was published
func AssertEventPublished(eventBus *TestEventBus, eventType string) error {
	events := eventBus.GetEventsByType(eventType)
	if len(events) == 0 {
		return fmt.Errorf("no events of type %s were published", eventType)
	}
	return nil
}
