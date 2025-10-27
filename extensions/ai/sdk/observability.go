package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/xraph/forge"
)

// Tracer provides distributed tracing capabilities
type TracerImpl struct {
	logger  forge.Logger
	metrics forge.Metrics
	
	mu     sync.RWMutex
	spans  map[string]*SpanImpl
	active map[string]*SpanImpl
}

// SpanImpl represents a trace span
type SpanImpl struct {
	TraceID    string
	SpanID     string
	ParentID   string
	Name       string
	StartTime  time.Time
	EndTime    time.Time
	Tags       map[string]string
	Logs       []SpanLog
	Status     SpanStatus
	Error      error
	
	mu sync.Mutex
}

// SpanLog represents a log entry in a span
type SpanLog struct {
	Timestamp time.Time
	Level     string
	Message   string
	Fields    map[string]interface{}
}

// SpanStatus represents the status of a span
type SpanStatus string

const (
	SpanStatusOK    SpanStatus = "ok"
	SpanStatusError SpanStatus = "error"
)

// NewTracer creates a new tracer
func NewTracer(logger forge.Logger, metrics forge.Metrics) *TracerImpl {
	return &TracerImpl{
		logger:  logger,
		metrics: metrics,
		spans:   make(map[string]*SpanImpl),
		active:  make(map[string]*SpanImpl),
	}
}

// StartSpan starts a new trace span
func (t *TracerImpl) StartSpan(ctx context.Context, name string) *SpanImpl {
	span := &SpanImpl{
		TraceID:   generateID(),
		SpanID:    generateID(),
		Name:      name,
		StartTime: time.Now(),
		Tags:      make(map[string]string),
		Logs:      make([]SpanLog, 0),
		Status:    SpanStatusOK,
	}

	// Check for parent span in context
	if parentSpan := SpanFromContext(ctx); parentSpan != nil {
		span.ParentID = parentSpan.SpanID
		span.TraceID = parentSpan.TraceID
	}

	t.mu.Lock()
	t.spans[span.SpanID] = span
	t.active[span.SpanID] = span
	t.mu.Unlock()

	if t.logger != nil {
		t.logger.Debug("Span started",
			F("trace_id", span.TraceID),
			F("span_id", span.SpanID),
			F("name", name),
		)
	}

	if t.metrics != nil {
		t.metrics.Counter("forge.ai.sdk.trace.spans_started", "name", name).Inc()
	}

	return span
}

// Finish completes a span
func (s *SpanImpl) Finish() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.EndTime = time.Now()

	// Log span completion
	if s.Error != nil {
		s.Status = SpanStatusError
	}
}

// SetTag sets a tag on the span
func (s *SpanImpl) SetTag(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Tags[key] = value
}

// SetError sets an error on the span
func (s *SpanImpl) SetError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Error = err
	s.Status = SpanStatusError
}

// LogEvent logs an event in the span
func (s *SpanImpl) LogEvent(level, message string, fields map[string]interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Logs = append(s.Logs, SpanLog{
		Timestamp: time.Now(),
		Level:     level,
		Message:   message,
		Fields:    fields,
	})
}

// Duration returns the span duration
func (s *SpanImpl) Duration() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	if s.EndTime.IsZero() {
		return time.Since(s.StartTime)
	}
	return s.EndTime.Sub(s.StartTime)
}

// SpanFromContext retrieves a span from context
func SpanFromContext(ctx context.Context) *SpanImpl {
	if span, ok := ctx.Value(spanKey).(*SpanImpl); ok {
		return span
	}
	return nil
}

// ContextWithSpan adds a span to context
func ContextWithSpan(ctx context.Context, span *SpanImpl) context.Context {
	return context.WithValue(ctx, spanKey, span)
}

type contextKey string

const spanKey contextKey = "span"

// generateID generates a unique ID
func generateID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// DebugInfo provides debugging information
type DebugInfo struct {
	Timestamp     time.Time
	Goroutines    int
	MemoryStats   RuntimeMemoryStats
	ActiveSpans   int
	RecentErrors  []ErrorInfo
	RequestStats  RequestStats
}

// RuntimeMemoryStats provides memory usage statistics
type RuntimeMemoryStats struct {
	Alloc      uint64
	TotalAlloc uint64
	Sys        uint64
	NumGC      uint32
}

// ErrorInfo represents error information
type ErrorInfo struct {
	Timestamp time.Time
	Message   string
	Stack     string
	Context   map[string]interface{}
}

// RequestStats provides request statistics
type RequestStats struct {
	Total       int64
	Success     int64
	Failed      int64
	AvgDuration time.Duration
	P50Duration time.Duration
	P95Duration time.Duration
	P99Duration time.Duration
}

// Debugger provides debugging capabilities
type Debugger struct {
	logger forge.Logger
	
	mu           sync.RWMutex
	recentErrors []ErrorInfo
	maxErrors    int
}

// NewDebugger creates a new debugger
func NewDebugger(logger forge.Logger) *Debugger {
	return &Debugger{
		logger:       logger,
		recentErrors: make([]ErrorInfo, 0),
		maxErrors:    100,
	}
}

// GetDebugInfo retrieves current debug information
func (d *Debugger) GetDebugInfo() *DebugInfo {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	d.mu.RLock()
	errorCount := len(d.recentErrors)
	recentErrors := make([]ErrorInfo, 0)
	if errorCount > 0 {
		// Get last 10 errors
		start := 0
		if errorCount > 10 {
			start = errorCount - 10
		}
		recentErrors = d.recentErrors[start:]
	}
	d.mu.RUnlock()

	return &DebugInfo{
		Timestamp:  time.Now(),
		Goroutines: runtime.NumGoroutine(),
		MemoryStats: RuntimeMemoryStats{
			Alloc:      memStats.Alloc,
			TotalAlloc: memStats.TotalAlloc,
			Sys:        memStats.Sys,
			NumGC:      memStats.NumGC,
		},
		RecentErrors: recentErrors,
	}
}

// RecordError records an error for debugging
func (d *Debugger) RecordError(err error, context map[string]interface{}) {
	if err == nil {
		return
	}

	errorInfo := ErrorInfo{
		Timestamp: time.Now(),
		Message:   err.Error(),
		Stack:     getStackTrace(),
		Context:   context,
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	d.recentErrors = append(d.recentErrors, errorInfo)

	// Keep only last N errors
	if len(d.recentErrors) > d.maxErrors {
		d.recentErrors = d.recentErrors[1:]
	}
}

// GetRecentErrors retrieves recent errors
func (d *Debugger) GetRecentErrors(count int) []ErrorInfo {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if count <= 0 || count > len(d.recentErrors) {
		count = len(d.recentErrors)
	}

	start := len(d.recentErrors) - count
	errors := make([]ErrorInfo, count)
	copy(errors, d.recentErrors[start:])

	return errors
}

// ClearErrors clears all recorded errors
func (d *Debugger) ClearErrors() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.recentErrors = make([]ErrorInfo, 0)
}

// getStackTrace captures the current stack trace
func getStackTrace() string {
	buf := make([]byte, 4096)
	n := runtime.Stack(buf, false)
	return string(buf[:n])
}

// Profiler provides performance profiling
type Profiler struct {
	logger  forge.Logger
	metrics forge.Metrics
	
	mu         sync.RWMutex
	profiles   map[string]*Profile
}

// Profile represents a performance profile
type Profile struct {
	Name       string
	Count      int64
	TotalTime  time.Duration
	MinTime    time.Duration
	MaxTime    time.Duration
	AvgTime    time.Duration
	Percentiles map[int]time.Duration
	
	mu       sync.Mutex
	durations []time.Duration
}

// NewProfiler creates a new profiler
func NewProfiler(logger forge.Logger, metrics forge.Metrics) *Profiler {
	return &Profiler{
		logger:   logger,
		metrics:  metrics,
		profiles: make(map[string]*Profile),
	}
}

// StartProfile starts profiling an operation
func (p *Profiler) StartProfile(name string) *ProfileSession {
	return &ProfileSession{
		profiler:  p,
		name:      name,
		startTime: time.Now(),
	}
}

// ProfileSession represents an active profiling session
type ProfileSession struct {
	profiler  *Profiler
	name      string
	startTime time.Time
}

// End ends the profiling session
func (ps *ProfileSession) End() {
	duration := time.Since(ps.startTime)
	ps.profiler.recordDuration(ps.name, duration)
}

// recordDuration records a duration for a profile
func (p *Profiler) recordDuration(name string, duration time.Duration) {
	p.mu.Lock()
	profile, exists := p.profiles[name]
	if !exists {
		profile = &Profile{
			Name:        name,
			MinTime:     duration,
			MaxTime:     duration,
			Percentiles: make(map[int]time.Duration),
			durations:   make([]time.Duration, 0),
		}
		p.profiles[name] = profile
	}
	p.mu.Unlock()

	profile.mu.Lock()
	defer profile.mu.Unlock()

	profile.Count++
	profile.TotalTime += duration
	profile.AvgTime = time.Duration(int64(profile.TotalTime) / profile.Count)

	if duration < profile.MinTime {
		profile.MinTime = duration
	}
	if duration > profile.MaxTime {
		profile.MaxTime = duration
	}

	profile.durations = append(profile.durations, duration)

	if p.metrics != nil {
		p.metrics.Histogram("forge.ai.sdk.profile.duration", "operation", name).Observe(duration.Seconds())
	}
}

// GetProfile retrieves a profile by name
func (p *Profiler) GetProfile(name string) *Profile {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	if profile, exists := p.profiles[name]; exists {
		// Return a copy without the mutex
		profile.mu.Lock()
		defer profile.mu.Unlock()
		
		// Copy only the public fields to avoid copying the mutex
		profileCopy := &Profile{
			Name:        profile.Name,
			Count:       profile.Count,
			TotalTime:   profile.TotalTime,
			MinTime:     profile.MinTime,
			MaxTime:     profile.MaxTime,
			AvgTime:     profile.AvgTime,
			Percentiles: profile.Percentiles,
			// Don't copy mu or durations as they're private implementation details
		}
		return profileCopy
	}
	
	return nil
}

// GetAllProfiles returns all profiles
func (p *Profiler) GetAllProfiles() map[string]*Profile {
	p.mu.RLock()
	defer p.mu.RUnlock()

	profiles := make(map[string]*Profile)
	for name, profile := range p.profiles {
		profile.mu.Lock()
		// Copy only the public fields to avoid copying the mutex
		profileCopy := &Profile{
			Name:        profile.Name,
			Count:       profile.Count,
			TotalTime:   profile.TotalTime,
			MinTime:     profile.MinTime,
			MaxTime:     profile.MaxTime,
			AvgTime:     profile.AvgTime,
			Percentiles: profile.Percentiles,
			// Don't copy mu or durations as they're private implementation details
		}
		profile.mu.Unlock()
		profiles[name] = profileCopy
	}

	return profiles
}

// Reset resets all profiles
func (p *Profiler) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.profiles = make(map[string]*Profile)
}

// ExportJSON exports profiles as JSON
func (p *Profiler) ExportJSON() (string, error) {
	profiles := p.GetAllProfiles()
	
	data, err := json.MarshalIndent(profiles, "", "  ")
	if err != nil {
		return "", fmt.Errorf("export failed: %w", err)
	}

	return string(data), nil
}

// HealthChecker provides health check capabilities
type HealthChecker struct {
	logger forge.Logger
	
	mu     sync.RWMutex
	checks map[string]HealthCheckFunc
}

// HealthCheckFunc is a function that performs a health check
type HealthCheckFunc func(context.Context) error

// HealthCheckResult represents the result of a health check
type HealthCheckResult struct {
	Name      string
	Status    string // "healthy", "degraded", "unhealthy"
	Message   string
	Timestamp time.Time
	Duration  time.Duration
	Error     error
}

// NewHealthChecker creates a new health checker
func NewHealthChecker(logger forge.Logger) *HealthChecker {
	return &HealthChecker{
		logger: logger,
		checks: make(map[string]HealthCheckFunc),
	}
}

// RegisterCheck registers a health check
func (hc *HealthChecker) RegisterCheck(name string, check HealthCheckFunc) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.checks[name] = check
}

// RunChecks runs all registered health checks
func (hc *HealthChecker) RunChecks(ctx context.Context) map[string]*HealthCheckResult {
	hc.mu.RLock()
	checks := make(map[string]HealthCheckFunc)
	for name, check := range hc.checks {
		checks[name] = check
	}
	hc.mu.RUnlock()

	results := make(map[string]*HealthCheckResult)
	var wg sync.WaitGroup
	var resultsMu sync.Mutex

	for name, check := range checks {
		wg.Add(1)
		go func(n string, c HealthCheckFunc) {
			defer wg.Done()
			
			result := &HealthCheckResult{
				Name:      n,
				Timestamp: time.Now(),
			}

			start := time.Now()
			err := c(ctx)
			result.Duration = time.Since(start)

			if err != nil {
				result.Status = "unhealthy"
				result.Error = err
				result.Message = err.Error()
			} else {
				result.Status = "healthy"
				result.Message = "OK"
			}

			resultsMu.Lock()
			results[n] = result
			resultsMu.Unlock()
		}(name, check)
	}

	wg.Wait()

	return results
}

// GetOverallHealth returns the overall health status
func (hc *HealthChecker) GetOverallHealth(ctx context.Context) string {
	results := hc.RunChecks(ctx)

	unhealthy := 0
	for _, result := range results {
		if result.Status == "unhealthy" {
			unhealthy++
		}
	}

	if unhealthy == 0 {
		return "healthy"
	} else if unhealthy < len(results) {
		return "degraded"
	}
	return "unhealthy"
}

