package checks

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	health "github.com/xraph/forge/pkg/health/core"
)

// CustomHealthCheckBuilder provides a fluent interface for building custom health checks
type CustomHealthCheckBuilder struct {
	name         string
	checkFunc    health.HealthCheckFunc
	timeout      time.Duration
	interval     time.Duration
	critical     bool
	retries      int
	retryDelay   time.Duration
	dependencies []string
	tags         map[string]string
	metadata     map[string]interface{}
	beforeFunc   func(ctx context.Context) error
	afterFunc    func(ctx context.Context, result *health.HealthResult) error
	logger       common.Logger
}

// NewCustomHealthCheckBuilder creates a new custom health check builder
func NewCustomHealthCheckBuilder(name string, checkFunc health.HealthCheckFunc) *CustomHealthCheckBuilder {
	return &CustomHealthCheckBuilder{
		name:       name,
		checkFunc:  checkFunc,
		timeout:    5 * time.Second,
		interval:   30 * time.Second,
		critical:   false,
		retries:    3,
		retryDelay: 1 * time.Second,
		tags:       make(map[string]string),
		metadata:   make(map[string]interface{}),
	}
}

// WithTimeout sets the timeout for the health check
func (b *CustomHealthCheckBuilder) WithTimeout(timeout time.Duration) *CustomHealthCheckBuilder {
	b.timeout = timeout
	return b
}

// WithInterval sets the check interval
func (b *CustomHealthCheckBuilder) WithInterval(interval time.Duration) *CustomHealthCheckBuilder {
	b.interval = interval
	return b
}

// WithCritical marks the health check as critical
func (b *CustomHealthCheckBuilder) WithCritical(critical bool) *CustomHealthCheckBuilder {
	b.critical = critical
	return b
}

// WithRetries sets the number of retries
func (b *CustomHealthCheckBuilder) WithRetries(retries int) *CustomHealthCheckBuilder {
	b.retries = retries
	return b
}

// WithRetryDelay sets the retry delay
func (b *CustomHealthCheckBuilder) WithRetryDelay(delay time.Duration) *CustomHealthCheckBuilder {
	b.retryDelay = delay
	return b
}

// WithDependencies sets the dependencies for the health check
func (b *CustomHealthCheckBuilder) WithDependencies(dependencies ...string) *CustomHealthCheckBuilder {
	b.dependencies = dependencies
	return b
}

// WithTags sets tags for the health check
func (b *CustomHealthCheckBuilder) WithTags(tags map[string]string) *CustomHealthCheckBuilder {
	for k, v := range tags {
		b.tags[k] = v
	}
	return b
}

// WithTag adds a single tag
func (b *CustomHealthCheckBuilder) WithTag(key, value string) *CustomHealthCheckBuilder {
	b.tags[key] = value
	return b
}

// WithMetadata sets metadata for the health check
func (b *CustomHealthCheckBuilder) WithMetadata(metadata map[string]interface{}) *CustomHealthCheckBuilder {
	for k, v := range metadata {
		b.metadata[k] = v
	}
	return b
}

// WithLogger sets the logger for the health check
func (b *CustomHealthCheckBuilder) WithLogger(logger common.Logger) *CustomHealthCheckBuilder {
	b.logger = logger
	return b
}

// WithBefore sets a function to run before the health check
func (b *CustomHealthCheckBuilder) WithBefore(beforeFunc func(ctx context.Context) error) *CustomHealthCheckBuilder {
	b.beforeFunc = beforeFunc
	return b
}

// WithAfter sets a function to run after the health check
func (b *CustomHealthCheckBuilder) WithAfter(afterFunc func(ctx context.Context, result *health.HealthResult) error) *CustomHealthCheckBuilder {
	b.afterFunc = afterFunc
	return b
}

// Build creates the custom health check
func (b *CustomHealthCheckBuilder) Build() health.HealthCheck {
	config := &health.HealthCheckConfig{
		Name:         b.name,
		Timeout:      b.timeout,
		Critical:     b.critical,
		Dependencies: b.dependencies,
		Interval:     b.interval,
		Retries:      b.retries,
		RetryDelay:   b.retryDelay,
		Tags:         b.tags,
		Metadata:     b.metadata,
	}

	check := health.NewSimpleHealthCheck(config, b.checkFunc)

	// Wrap with before/after functions if provided
	if b.beforeFunc != nil || b.afterFunc != nil {
		wrapper := health.NewHealthCheckWrapper(check)
		if b.beforeFunc != nil {
			wrapper.WithBefore(b.beforeFunc)
		}
		if b.afterFunc != nil {
			wrapper.WithAfter(b.afterFunc)
		}
		return wrapper
	}

	return check
}

// ServiceHealthCheckBuilder creates health checks for services
type ServiceHealthCheckBuilder struct {
	container common.Container
	logger    common.Logger
}

// NewServiceHealthCheckBuilder creates a new service health check builder
func NewServiceHealthCheckBuilder(container common.Container, logger common.Logger) *ServiceHealthCheckBuilder {
	return &ServiceHealthCheckBuilder{
		container: container,
		logger:    logger,
	}
}

// BuildForService creates a health check for a service
func (b *ServiceHealthCheckBuilder) BuildForService(serviceName string) health.HealthCheck {
	return NewCustomHealthCheckBuilder(fmt.Sprintf("service-%s", serviceName), func(ctx context.Context) *health.HealthResult {
		return b.checkService(ctx, serviceName)
	}).
		WithCritical(true).
		WithTimeout(10 * time.Second).
		WithLogger(b.logger).
		Build()
}

// BuildForAllServices creates health checks for all registered services
func (b *ServiceHealthCheckBuilder) BuildForAllServices() []health.HealthCheck {
	var checks []health.HealthCheck

	services := b.container.Services()
	for _, service := range services {
		serviceName := service.Name
		if serviceName == "" {
			serviceName = service.ServiceName()
		}

		checks = append(checks, b.BuildForService(serviceName))
	}

	return checks
}

// checkService performs a health check on a service
func (b *ServiceHealthCheckBuilder) checkService(ctx context.Context, serviceName string) *health.HealthResult {
	result := health.NewHealthResult(serviceName, health.HealthStatusHealthy, "service is healthy")

	// Try to resolve the service
	service, err := b.container.ResolveNamed(serviceName)
	if err != nil {
		return result.
			WithStatus(health.HealthStatusUnhealthy).
			WithMessage(fmt.Sprintf("failed to resolve service: %v", err)).
			WithError(err)
	}

	// Check if service implements health check interface
	if healthCheckable, ok := service.(interface{ OnHealthCheck(context.Context) error }); ok {
		if err := healthCheckable.OnHealthCheck(ctx); err != nil {
			return result.
				WithStatus(health.HealthStatusUnhealthy).
				WithMessage(fmt.Sprintf("service health check failed: %v", err)).
				WithError(err)
		}
	}

	// Check if service implements the core.Service interface
	if coreService, ok := service.(common.Service); ok {
		result.WithDetail("service_name", coreService.Name())
		result.WithDetail("dependencies", coreService.Dependencies())
	}

	return result
}

// HTTPHealthCheckBuilder creates health checks for HTTP endpoints
type HTTPHealthCheckBuilder struct {
	client *http.Client
	logger common.Logger
}

// NewHTTPHealthCheckBuilder creates a new HTTP health check builder
func NewHTTPHealthCheckBuilder(logger common.Logger) *HTTPHealthCheckBuilder {
	return &HTTPHealthCheckBuilder{
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger: logger,
	}
}

// BuildForEndpoint creates a health check for an HTTP endpoint
func (b *HTTPHealthCheckBuilder) BuildForEndpoint(name, url string, options ...HTTPHealthCheckOption) health.HealthCheck {
	config := &HTTPHealthCheckConfig{
		URL:            url,
		Method:         "GET",
		ExpectedStatus: 200,
		Headers:        make(map[string]string),
		Timeout:        10 * time.Second,
	}

	for _, option := range options {
		option(config)
	}

	return NewCustomHealthCheckBuilder(name, func(ctx context.Context) *health.HealthResult {
		return b.checkHTTPEndpoint(ctx, config)
	}).
		WithTimeout(config.Timeout).
		WithLogger(b.logger).
		Build()
}

// HTTPHealthCheckConfig contains configuration for HTTP health checks
type HTTPHealthCheckConfig struct {
	URL            string
	Method         string
	ExpectedStatus int
	Headers        map[string]string
	Body           string
	Timeout        time.Duration
	ValidateBody   func(body string) error
}

// HTTPHealthCheckOption is a functional option for HTTP health checks
type HTTPHealthCheckOption func(*HTTPHealthCheckConfig)

// WithMethod sets the HTTP method
func WithMethod(method string) HTTPHealthCheckOption {
	return func(config *HTTPHealthCheckConfig) {
		config.Method = method
	}
}

// WithExpectedStatus sets the expected HTTP status code
func WithExpectedStatus(status int) HTTPHealthCheckOption {
	return func(config *HTTPHealthCheckConfig) {
		config.ExpectedStatus = status
	}
}

// WithHeaders sets HTTP headers
func WithHeaders(headers map[string]string) HTTPHealthCheckOption {
	return func(config *HTTPHealthCheckConfig) {
		for k, v := range headers {
			config.Headers[k] = v
		}
	}
}

// WithBody sets the request body
func WithBody(body string) HTTPHealthCheckOption {
	return func(config *HTTPHealthCheckConfig) {
		config.Body = body
	}
}

// WithBodyValidator sets a function to validate the response body
func WithBodyValidator(validator func(body string) error) HTTPHealthCheckOption {
	return func(config *HTTPHealthCheckConfig) {
		config.ValidateBody = validator
	}
}

// checkHTTPEndpoint performs an HTTP health check
func (b *HTTPHealthCheckBuilder) checkHTTPEndpoint(ctx context.Context, config *HTTPHealthCheckConfig) *health.HealthResult {
	result := health.NewHealthResult(config.URL, health.HealthStatusHealthy, "HTTP endpoint is healthy")

	// Create request
	req, err := http.NewRequestWithContext(ctx, config.Method, config.URL, strings.NewReader(config.Body))
	if err != nil {
		return result.
			WithStatus(health.HealthStatusUnhealthy).
			WithMessage(fmt.Sprintf("failed to create request: %v", err)).
			WithError(err)
	}

	// Set headers
	for key, value := range config.Headers {
		req.Header.Set(key, value)
	}

	// Make request
	start := time.Now()
	resp, err := b.client.Do(req)
	duration := time.Since(start)

	result.WithDuration(duration)

	if err != nil {
		return result.
			WithStatus(health.HealthStatusUnhealthy).
			WithMessage(fmt.Sprintf("HTTP request failed: %v", err)).
			WithError(err)
	}
	defer resp.Body.Close()

	// Check status code
	result.WithDetail("status_code", resp.StatusCode)
	result.WithDetail("response_time", duration)

	if resp.StatusCode != config.ExpectedStatus {
		return result.
			WithStatus(health.HealthStatusUnhealthy).
			WithMessage(fmt.Sprintf("unexpected status code: %d (expected: %d)", resp.StatusCode, config.ExpectedStatus))
	}

	// Validate response body if validator is provided
	if config.ValidateBody != nil {
		body := make([]byte, 1024)
		n, _ := resp.Body.Read(body)

		if err := config.ValidateBody(string(body[:n])); err != nil {
			return result.
				WithStatus(health.HealthStatusUnhealthy).
				WithMessage(fmt.Sprintf("body validation failed: %v", err)).
				WithError(err)
		}
	}

	return result
}

// ResourceHealthCheckBuilder creates health checks for system resources
type ResourceHealthCheckBuilder struct {
	logger common.Logger
}

// NewResourceHealthCheckBuilder creates a new resource health check builder
func NewResourceHealthCheckBuilder(logger common.Logger) *ResourceHealthCheckBuilder {
	return &ResourceHealthCheckBuilder{
		logger: logger,
	}
}

// BuildFileSystemCheck creates a filesystem health check
func (b *ResourceHealthCheckBuilder) BuildFileSystemCheck(name, path string, minFreeSpace int64) health.HealthCheck {
	return NewCustomHealthCheckBuilder(fmt.Sprintf("filesystem-%s", name), func(ctx context.Context) *health.HealthResult {
		return checkFileSystem(path, minFreeSpace)
	}).
		WithCritical(true).
		WithLogger(b.logger).
		Build()
}

// BuildPortCheck creates a port availability health check
func (b *ResourceHealthCheckBuilder) BuildPortCheck(name, host string, port int) health.HealthCheck {
	return NewCustomHealthCheckBuilder(fmt.Sprintf("port-%s", name), func(ctx context.Context) *health.HealthResult {
		return checkPort(ctx, host, port)
	}).
		WithTimeout(5 * time.Second).
		WithLogger(b.logger).
		Build()
}

// ScheduledHealthCheck allows health checks to run on a custom schedule
type ScheduledHealthCheck struct {
	check    health.HealthCheck
	schedule Schedule
	lastRun  time.Time
	nextRun  time.Time
	mutex    sync.RWMutex
	logger   common.Logger
}

// Schedule defines when a health check should run
type Schedule interface {
	Next(after time.Time) time.Time
	ShouldRun(now time.Time, lastRun time.Time) bool
}

// CronSchedule implements Schedule using cron expressions
type CronSchedule struct {
	expression string
	// Add cron parsing logic here
}

// IntervalSchedule implements Schedule using fixed intervals
type IntervalSchedule struct {
	interval time.Duration
}

// NewIntervalSchedule creates a new interval schedule
func NewIntervalSchedule(interval time.Duration) *IntervalSchedule {
	return &IntervalSchedule{interval: interval}
}

// Next returns the next time the check should run
func (s *IntervalSchedule) Next(after time.Time) time.Time {
	return after.Add(s.interval)
}

// ShouldRun returns true if the check should run now
func (s *IntervalSchedule) ShouldRun(now time.Time, lastRun time.Time) bool {
	if lastRun.IsZero() {
		return true
	}
	return now.Sub(lastRun) >= s.interval
}

// NewScheduledHealthCheck creates a new scheduled health check
func NewScheduledHealthCheck(check health.HealthCheck, schedule Schedule, logger common.Logger) *ScheduledHealthCheck {
	return &ScheduledHealthCheck{
		check:    check,
		schedule: schedule,
		logger:   logger,
	}
}

// Name returns the name of the health check
func (s *ScheduledHealthCheck) Name() string {
	return s.check.Name()
}

// Timeout returns the timeout for the health check
func (s *ScheduledHealthCheck) Timeout() time.Duration {
	return s.check.Timeout()
}

// Critical returns whether the health check is critical
func (s *ScheduledHealthCheck) Critical() bool {
	return s.check.Critical()
}

// Dependencies returns the dependencies of the health check
func (s *ScheduledHealthCheck) Dependencies() []string {
	return s.check.Dependencies()
}

// Check performs the health check if it's time to run
func (s *ScheduledHealthCheck) Check(ctx context.Context) *health.HealthResult {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	now := time.Now()

	// Check if it's time to run
	if !s.schedule.ShouldRun(now, s.lastRun) {
		// Return last result or create a skipped result
		return health.NewHealthResult(s.Name(), health.HealthStatusHealthy, "scheduled check skipped")
	}

	// Run the check
	result := s.check.Check(ctx)
	s.lastRun = now
	s.nextRun = s.schedule.Next(now)

	return result
}

// ConditionalHealthCheck runs a health check only if a condition is met
type ConditionalHealthCheck struct {
	check     health.HealthCheck
	condition func(ctx context.Context) bool
	logger    common.Logger
}

// NewConditionalHealthCheck creates a new conditional health check
func NewConditionalHealthCheck(check health.HealthCheck, condition func(ctx context.Context) bool, logger common.Logger) *ConditionalHealthCheck {
	return &ConditionalHealthCheck{
		check:     check,
		condition: condition,
		logger:    logger,
	}
}

// Name returns the name of the health check
func (c *ConditionalHealthCheck) Name() string {
	return c.check.Name()
}

// Timeout returns the timeout for the health check
func (c *ConditionalHealthCheck) Timeout() time.Duration {
	return c.check.Timeout()
}

// Critical returns whether the health check is critical
func (c *ConditionalHealthCheck) Critical() bool {
	return c.check.Critical()
}

// Dependencies returns the dependencies of the health check
func (c *ConditionalHealthCheck) Dependencies() []string {
	return c.check.Dependencies()
}

// Check performs the health check if the condition is met
func (c *ConditionalHealthCheck) Check(ctx context.Context) *health.HealthResult {
	if !c.condition(ctx) {
		return health.NewHealthResult(c.Name(), health.HealthStatusHealthy, "condition not met, check skipped")
	}

	return c.check.Check(ctx)
}

// HealthCheckRegistry manages a collection of health checks
type HealthCheckRegistry struct {
	checks   map[string]health.HealthCheck
	builders map[string]func() health.HealthCheck
	mutex    sync.RWMutex
	logger   common.Logger
}

// NewHealthCheckRegistry creates a new health check registry
func NewHealthCheckRegistry(logger common.Logger) *HealthCheckRegistry {
	return &HealthCheckRegistry{
		checks:   make(map[string]health.HealthCheck),
		builders: make(map[string]func() health.HealthCheck),
		logger:   logger,
	}
}

// Register registers a health check
func (r *HealthCheckRegistry) Register(check health.HealthCheck) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	name := check.Name()
	if _, exists := r.checks[name]; exists {
		return fmt.Errorf("health check %s already registered", name)
	}

	r.checks[name] = check
	return nil
}

// RegisterBuilder registers a health check builder
func (r *HealthCheckRegistry) RegisterBuilder(name string, builder func() health.HealthCheck) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if _, exists := r.builders[name]; exists {
		return fmt.Errorf("health check builder %s already registered", name)
	}

	r.builders[name] = builder
	return nil
}

// Get returns a health check by name
func (r *HealthCheckRegistry) Get(name string) (health.HealthCheck, error) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	if check, exists := r.checks[name]; exists {
		return check, nil
	}

	return nil, fmt.Errorf("health check %s not found", name)
}

// GetAll returns all registered health checks
func (r *HealthCheckRegistry) GetAll() []health.HealthCheck {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	checks := make([]health.HealthCheck, 0, len(r.checks))
	for _, check := range r.checks {
		checks = append(checks, check)
	}

	return checks
}

// BuildAll builds all registered health check builders
func (r *HealthCheckRegistry) BuildAll() error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	for name, builder := range r.builders {
		if _, exists := r.checks[name]; !exists {
			check := builder()
			r.checks[name] = check
		}
	}

	return nil
}

// Helper functions for common health checks

// checkFileSystem checks filesystem health
func checkFileSystem(path string, minFreeSpace int64) *health.HealthResult {
	result := health.NewHealthResult(fmt.Sprintf("filesystem-%s", path), health.HealthStatusHealthy, "filesystem is healthy")

	// This is a simplified implementation
	// In practice, you would use syscalls to check disk space
	result.WithDetail("path", path)
	result.WithDetail("min_free_space", minFreeSpace)

	return result
}

// checkPort checks if a port is available
func checkPort(ctx context.Context, host string, port int) *health.HealthResult {
	result := health.NewHealthResult(fmt.Sprintf("port-%s-%d", host, port), health.HealthStatusHealthy, "port is available")

	// This is a simplified implementation
	// In practice, you would actually try to connect to the port
	result.WithDetail("host", host)
	result.WithDetail("port", port)

	return result
}

// CreateCustomHealthCheckFromFunction creates a health check from a simple function
func CreateCustomHealthCheckFromFunction(name string, fn func(ctx context.Context) error) health.HealthCheck {
	return NewCustomHealthCheckBuilder(name, func(ctx context.Context) *health.HealthResult {
		result := health.NewHealthResult(name, health.HealthStatusHealthy, "custom check passed")

		if err := fn(ctx); err != nil {
			return result.
				WithStatus(health.HealthStatusUnhealthy).
				WithMessage(fmt.Sprintf("custom check failed: %v", err)).
				WithError(err)
		}

		return result
	}).Build()
}

// CreateCustomHealthCheckFromMethod creates a health check from a method
func CreateCustomHealthCheckFromMethod(name string, obj interface{}, methodName string) (health.HealthCheck, error) {
	objValue := reflect.ValueOf(obj)
	method := objValue.MethodByName(methodName)

	if !method.IsValid() {
		return nil, fmt.Errorf("method %s not found", methodName)
	}

	return NewCustomHealthCheckBuilder(name, func(ctx context.Context) *health.HealthResult {
		result := health.NewHealthResult(name, health.HealthStatusHealthy, "method check passed")

		// Call the method
		results := method.Call([]reflect.Value{reflect.ValueOf(ctx)})

		// Check if method returned an error
		if len(results) > 0 {
			if err, ok := results[len(results)-1].Interface().(error); ok && err != nil {
				return result.
					WithStatus(health.HealthStatusUnhealthy).
					WithMessage(fmt.Sprintf("method check failed: %v", err)).
					WithError(err)
			}
		}

		return result
	}).Build(), nil
}
