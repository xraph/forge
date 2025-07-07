package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"syscall"
	"time"
)

// health implements the Health interface
type health struct {
	config  HealthConfig
	checks  map[string]HealthCheck
	timeout time.Duration
}

// NewHealth creates a new health checker instance
func NewHealth(config HealthConfig) Health {
	if !config.Enabled {
		return &noopHealth{}
	}

	timeout := config.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &health{
		config:  config,
		checks:  make(map[string]HealthCheck),
		timeout: timeout,
	}
}

// RegisterCheck registers a health check
func (h *health) RegisterCheck(name string, check HealthCheck) error {
	if _, exists := h.checks[name]; exists {
		return fmt.Errorf("health check %s already registered", name)
	}

	h.checks[name] = check
	return nil
}

// UnregisterCheck unregisters a health check
func (h *health) UnregisterCheck(name string) error {
	if _, exists := h.checks[name]; !exists {
		return ErrCheckNotRegistered
	}

	delete(h.checks, name)
	return nil
}

// Check performs all health checks
func (h *health) Check(ctx context.Context) HealthStatus {
	start := time.Now()
	results := make(map[string]HealthCheckResult)
	overallStatus := HealthStatusUp

	// Create a context with timeout
	checkCtx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()

	// Run all checks
	for name, check := range h.checks {
		result := h.runSingleCheck(checkCtx, name, check)
		results[name] = result

		// Update overall status
		if result.Status == HealthStatusDown {
			overallStatus = HealthStatusDown
		} else if result.Status == HealthStatusWarning && overallStatus == HealthStatusUp {
			overallStatus = HealthStatusWarning
		}
	}

	// If no checks registered, status is unknown
	if len(results) == 0 {
		overallStatus = HealthStatusUnknown
	}

	return HealthStatus{
		Status:    overallStatus,
		Checks:    results,
		Timestamp: time.Now(),
		Duration:  time.Since(start),
		Details:   h.getSystemDetails(),
	}
}

// CheckSingle performs a single health check
func (h *health) CheckSingle(ctx context.Context, name string) HealthCheckResult {
	check, exists := h.checks[name]
	if !exists {
		return HealthCheckResult{
			Status:    HealthStatusDown,
			Message:   "Health check not found",
			Error:     ErrCheckNotRegistered.Error(),
			Timestamp: time.Now(),
			Duration:  0,
		}
	}

	checkCtx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()

	return h.runSingleCheck(checkCtx, name, check)
}

// runSingleCheck executes a single health check
func (h *health) runSingleCheck(ctx context.Context, name string, check HealthCheck) HealthCheckResult {
	start := time.Now()

	// Use check's timeout if specified
	timeout := check.Timeout()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	err := check.Check(ctx)
	duration := time.Since(start)

	result := HealthCheckResult{
		Timestamp: time.Now(),
		Duration:  duration,
		Details:   make(map[string]interface{}),
	}

	if err != nil {
		result.Status = HealthStatusDown
		result.Error = err.Error()
		result.Message = fmt.Sprintf("Health check %s failed", name)
	} else {
		result.Status = HealthStatusUp
		result.Message = fmt.Sprintf("Health check %s passed", name)
	}

	// Add check metadata
	result.Details["name"] = check.Name()
	result.Details["description"] = check.Description()
	result.Details["timeout"] = check.Timeout().String()

	return result
}

// Handler returns an HTTP handler for health checks
func (h *health) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status := h.Check(r.Context())
		h.writeResponse(w, status)
	}
}

// ReadinessHandler returns a readiness probe handler
func (h *health) ReadinessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// For readiness, we only check critical services
		status := h.Check(r.Context())

		// Filter out non-critical checks for readiness
		criticalStatus := HealthStatusUp
		criticalChecks := make(map[string]HealthCheckResult)

		for name, result := range status.Checks {
			// Consider database and cache as critical for readiness
			if isCriticalCheck(name) {
				criticalChecks[name] = result
				if result.Status == HealthStatusDown {
					criticalStatus = HealthStatusDown
				}
			}
		}

		readinessStatus := HealthStatus{
			Status:    criticalStatus,
			Checks:    criticalChecks,
			Timestamp: status.Timestamp,
			Duration:  status.Duration,
		}

		h.writeResponse(w, readinessStatus)
	}
}

// LivenessHandler returns a liveness probe handler
func (h *health) LivenessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// For liveness, we just check if the application is running
		livenessStatus := HealthStatus{
			Status:    HealthStatusUp,
			Checks:    make(map[string]HealthCheckResult),
			Timestamp: time.Now(),
			Duration:  0,
			Details:   h.getSystemDetails(),
		}

		h.writeResponse(w, livenessStatus)
	}
}

// writeResponse writes the health status response
func (h *health) writeResponse(w http.ResponseWriter, status HealthStatus) {
	// Set content type
	w.Header().Set("Content-Type", "application/json")

	// Set status code based on health status
	switch status.Status {
	case HealthStatusUp:
		w.WriteHeader(http.StatusOK)
	case HealthStatusWarning:
		w.WriteHeader(http.StatusOK) // 200 for warnings
	case HealthStatusDown:
		w.WriteHeader(http.StatusServiceUnavailable)
	default:
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	// Write JSON response
	if err := json.NewEncoder(w).Encode(status); err != nil {
		http.Error(w, "Failed to encode health status", http.StatusInternalServerError)
	}
}

// SetTimeout sets the default timeout for health checks
func (h *health) SetTimeout(timeout time.Duration) {
	h.timeout = timeout
}

// SetInterval sets the check interval (not used in this implementation)
func (h *health) SetInterval(interval time.Duration) {
	// This could be used for periodic health checks in the future
}

// getSystemDetails returns system information for health status
func (h *health) getSystemDetails() map[string]interface{} {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return map[string]interface{}{
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
		"uptime":     time.Since(startTime).String(),
		"goroutines": runtime.NumGoroutine(),
		"memory": map[string]interface{}{
			"alloc":      m.Alloc,
			"total":      m.TotalAlloc,
			"sys":        m.Sys,
			"gc_cycles":  m.NumGC,
			"heap_alloc": m.HeapAlloc,
			"heap_sys":   m.HeapSys,
		},
		"go_version": runtime.Version(),
	}
}

// isCriticalCheck determines if a check is critical for readiness
func isCriticalCheck(name string) bool {
	criticalChecks := []string{
		"database", "db", "postgres", "mysql", "sqlite",
		"cache", "redis", "memcache",
		"queue", "jobs", "worker",
	}

	for _, critical := range criticalChecks {
		if name == critical {
			return true
		}
	}

	return false
}

// Standard health check implementations

// Check method for DatabaseHealthCheck
func (d *DatabaseHealthCheck) Check(ctx context.Context) error {
	if d.database == nil {
		return fmt.Errorf("database not configured")
	}
	return d.database.Ping(ctx)
}

func (d *DatabaseHealthCheck) Name() string {
	return d.name
}

func (d *DatabaseHealthCheck) Description() string {
	return fmt.Sprintf("Database connectivity check for %s", d.name)
}

func (d *DatabaseHealthCheck) Timeout() time.Duration {
	if d.timeout > 0 {
		return d.timeout
	}
	return 5 * time.Second
}

// Check method for RedisHealthCheck
func (r *RedisHealthCheck) Check(ctx context.Context) error {
	if r.client == nil {
		return fmt.Errorf("redis client not configured")
	}
	return r.client.Ping(ctx)
}

func (r *RedisHealthCheck) Name() string {
	return r.name
}

func (r *RedisHealthCheck) Description() string {
	return fmt.Sprintf("Redis connectivity check for %s", r.name)
}

func (r *RedisHealthCheck) Timeout() time.Duration {
	if r.timeout > 0 {
		return r.timeout
	}
	return 5 * time.Second
}

// Check method for HTTPHealthCheck
func (h *HTTPHealthCheck) Check(ctx context.Context) error {
	if h.client == nil {
		h.client = &http.Client{
			Timeout: h.timeout,
		}
	}

	req, err := http.NewRequestWithContext(ctx, "GET", h.url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	expected := h.expected
	if expected == 0 {
		expected = http.StatusOK
	}

	if resp.StatusCode != expected {
		return fmt.Errorf("expected status %d, got %d", expected, resp.StatusCode)
	}

	return nil
}

func (h *HTTPHealthCheck) Name() string {
	return h.name
}

func (h *HTTPHealthCheck) Description() string {
	return fmt.Sprintf("HTTP endpoint health check for %s", h.url)
}

func (h *HTTPHealthCheck) Timeout() time.Duration {
	if h.timeout > 0 {
		return h.timeout
	}
	return 10 * time.Second
}

// Check method for FileSystemHealthCheck
func (f *FileSystemHealthCheck) Check(ctx context.Context) error {
	// Check if path exists
	if _, err := os.Stat(f.path); err != nil {
		return fmt.Errorf("path %s not accessible: %w", f.path, err)
	}

	// Try to create a temporary file to test write permissions
	tempFile := fmt.Sprintf("%s/.health_check_%d", f.path, time.Now().UnixNano())
	file, err := os.Create(tempFile)
	if err != nil {
		return fmt.Errorf("cannot write to %s: %w", f.path, err)
	}
	file.Close()

	// Clean up the temporary file
	if err := os.Remove(tempFile); err != nil {
		// Log the error but don't fail the health check
		fmt.Printf("Warning: failed to cleanup temp file %s: %v\n", tempFile, err)
	}

	return nil
}

func (f *FileSystemHealthCheck) Name() string {
	return f.name
}

func (f *FileSystemHealthCheck) Description() string {
	return fmt.Sprintf("File system access check for %s", f.path)
}

func (f *FileSystemHealthCheck) Timeout() time.Duration {
	if f.timeout > 0 {
		return f.timeout
	}
	return 5 * time.Second
}

// Check method for MemoryHealthCheck
func (m *MemoryHealthCheck) Check(ctx context.Context) error {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	if int64(memStats.Alloc) > m.threshold {
		return fmt.Errorf("memory usage %d bytes exceeds threshold %d bytes", memStats.Alloc, m.threshold)
	}

	return nil
}

func (m *MemoryHealthCheck) Name() string {
	return m.name
}

func (m *MemoryHealthCheck) Description() string {
	return fmt.Sprintf("Memory usage health check (threshold: %d bytes)", m.threshold)
}

func (m *MemoryHealthCheck) Timeout() time.Duration {
	if m.timeout > 0 {
		return m.timeout
	}
	return 1 * time.Second
}

// Check method for DiskSpaceHealthCheck
func (d *DiskSpaceHealthCheck) Check(ctx context.Context) error {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(d.path, &stat); err != nil {
		return fmt.Errorf("failed to get disk stats for %s: %w", d.path, err)
	}

	// Calculate free space
	freeSpace := int64(stat.Bavail) * int64(stat.Bsize)

	if freeSpace < d.threshold {
		return fmt.Errorf("free disk space %d bytes is below threshold %d bytes", freeSpace, d.threshold)
	}

	return nil
}

func (d *DiskSpaceHealthCheck) Name() string {
	return d.name
}

func (d *DiskSpaceHealthCheck) Description() string {
	return fmt.Sprintf("Disk space health check for %s (threshold: %d bytes)", d.path, d.threshold)
}

func (d *DiskSpaceHealthCheck) Timeout() time.Duration {
	if d.timeout > 0 {
		return d.timeout
	}
	return 5 * time.Second
}

// Helper functions for creating health checks

// NewDatabaseHealthCheck creates a database health check
func NewDatabaseHealthCheck(name string, db interface{ Ping(context.Context) error }) *DatabaseHealthCheck {
	return &DatabaseHealthCheck{
		name:     name,
		database: db,
		timeout:  5 * time.Second,
	}
}

// NewRedisHealthCheck creates a Redis health check
func NewRedisHealthCheck(name string, client interface{ Ping(context.Context) error }) *RedisHealthCheck {
	return &RedisHealthCheck{
		name:    name,
		client:  client,
		timeout: 5 * time.Second,
	}
}

// NewHTTPHealthCheck creates an HTTP health check
func NewHTTPHealthCheck(name, url string, expectedStatus int) *HTTPHealthCheck {
	return &HTTPHealthCheck{
		name:     name,
		url:      url,
		timeout:  10 * time.Second,
		expected: expectedStatus,
	}
}

// NewFileSystemHealthCheck creates a file system health check
func NewFileSystemHealthCheck(name, path string) *FileSystemHealthCheck {
	return &FileSystemHealthCheck{
		name:    name,
		path:    path,
		timeout: 5 * time.Second,
	}
}

// NewMemoryHealthCheck creates a memory health check
func NewMemoryHealthCheck(name string, thresholdBytes int64) *MemoryHealthCheck {
	return &MemoryHealthCheck{
		name:      name,
		threshold: thresholdBytes,
		timeout:   1 * time.Second,
	}
}

// NewDiskSpaceHealthCheck creates a disk space health check
func NewDiskSpaceHealthCheck(name, path string, minFreeBytes int64) *DiskSpaceHealthCheck {
	return &DiskSpaceHealthCheck{
		name:      name,
		path:      path,
		threshold: minFreeBytes,
		timeout:   5 * time.Second,
	}
}

// noopHealth is a no-op implementation for when health checks are disabled
type noopHealth struct{}

func (n *noopHealth) RegisterCheck(name string, check HealthCheck) error { return nil }
func (n *noopHealth) UnregisterCheck(name string) error                  { return nil }
func (n *noopHealth) Check(ctx context.Context) HealthStatus {
	return HealthStatus{Status: HealthStatusUp}
}
func (n *noopHealth) CheckSingle(ctx context.Context, name string) HealthCheckResult {
	return HealthCheckResult{Status: HealthStatusUp}
}
func (n *noopHealth) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
}
func (n *noopHealth) ReadinessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
}
func (n *noopHealth) LivenessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
}
func (n *noopHealth) SetTimeout(timeout time.Duration)   {}
func (n *noopHealth) SetInterval(interval time.Duration) {}

// Global variable to track application start time
var startTime = time.Now()
