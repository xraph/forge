package forge

import (
	"context"
	"sync"
	"time"
)

// simpleHealthManager implements HealthManager
type simpleHealthManager struct {
	checks  map[string]HealthCheck
	mu      sync.RWMutex
	timeout time.Duration
}

// NewHealthManager creates a new health manager
func NewHealthManager(timeout time.Duration) HealthManager {
	return &simpleHealthManager{
		checks:  make(map[string]HealthCheck),
		timeout: timeout,
	}
}

// Register registers a health check
func (h *simpleHealthManager) Register(name string, check HealthCheck) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.checks[name] = check
}

// Check performs all health checks
func (h *simpleHealthManager) Check(ctx context.Context) HealthReport {
	start := time.Now()

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()

	h.mu.RLock()
	checksToRun := make(map[string]HealthCheck, len(h.checks))
	for name, check := range h.checks {
		checksToRun[name] = check
	}
	h.mu.RUnlock()

	// Run checks in parallel
	results := make(map[string]HealthResult)
	var resultsMu sync.Mutex
	var wg sync.WaitGroup

	for name, check := range checksToRun {
		name := name
		check := check

		wg.Add(1)
		go func() {
			defer wg.Done()

			result := check(ctx)

			resultsMu.Lock()
			results[name] = result
			resultsMu.Unlock()
		}()
	}

	wg.Wait()

	// Determine overall status
	overallStatus := HealthStatusHealthy
	for _, result := range results {
		if result.Status == HealthStatusUnhealthy {
			overallStatus = HealthStatusUnhealthy
			break
		} else if result.Status == HealthStatusDegraded && overallStatus == HealthStatusHealthy {
			overallStatus = HealthStatusDegraded
		}
	}

	return HealthReport{
		Status:    overallStatus,
		Timestamp: time.Now(),
		Duration:  time.Since(start),
		Checks:    results,
	}
}
