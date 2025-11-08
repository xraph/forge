package storage

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/xraph/forge"
)

// HealthChecker performs comprehensive health checks on storage backends.
type HealthChecker struct {
	backends map[string]Storage
	logger   forge.Logger
	metrics  forge.Metrics
	config   HealthCheckConfig
}

// HealthCheckConfig configures health check behavior.
type HealthCheckConfig struct {
	Timeout       time.Duration `default:"5s"            json:"timeout"         yaml:"timeout"`
	WriteTestFile bool          `default:"true"          json:"write_test_file" yaml:"write_test_file"`
	TestKey       string        `default:".health_check" json:"test_key"        yaml:"test_key"`
	CheckAll      bool          `default:"false"         json:"check_all"       yaml:"check_all"`
	EnableMetrics bool          `default:"true"          json:"enable_metrics"  yaml:"enable_metrics"`
}

// DefaultHealthCheckConfig returns default health check configuration.
func DefaultHealthCheckConfig() HealthCheckConfig {
	return HealthCheckConfig{
		Timeout:       5 * time.Second,
		WriteTestFile: true,
		TestKey:       ".health_check",
		CheckAll:      false,
		EnableMetrics: true,
	}
}

// BackendHealth represents health status of a backend.
type BackendHealth struct {
	Name         string        `json:"name"`
	Healthy      bool          `json:"healthy"`
	ResponseTime time.Duration `json:"response_time"`
	Error        string        `json:"error,omitempty"`
	LastChecked  time.Time     `json:"last_checked"`
	CheckType    string        `json:"check_type"`
}

// OverallHealth represents overall storage health.
type OverallHealth struct {
	Healthy        bool                     `json:"healthy"`
	BackendCount   int                      `json:"backend_count"`
	HealthyCount   int                      `json:"healthy_count"`
	UnhealthyCount int                      `json:"unhealthy_count"`
	Backends       map[string]BackendHealth `json:"backends"`
	CheckedAt      time.Time                `json:"checked_at"`
}

// NewHealthChecker creates a new health checker.
func NewHealthChecker(backends map[string]Storage, logger forge.Logger, metrics forge.Metrics, config HealthCheckConfig) *HealthChecker {
	return &HealthChecker{
		backends: backends,
		logger:   logger,
		metrics:  metrics,
		config:   config,
	}
}

// CheckHealth performs health check on default backend or all backends.
func (hc *HealthChecker) CheckHealth(ctx context.Context, defaultBackend string, checkAll bool) (*OverallHealth, error) {
	ctx, cancel := context.WithTimeout(ctx, hc.config.Timeout)
	defer cancel()

	health := &OverallHealth{
		Healthy:      true,
		BackendCount: len(hc.backends),
		Backends:     make(map[string]BackendHealth),
		CheckedAt:    time.Now(),
	}

	// Determine which backends to check
	backendsToCheck := make(map[string]Storage)
	if checkAll || hc.config.CheckAll {
		backendsToCheck = hc.backends
	} else if defaultBackend != "" {
		if backend, exists := hc.backends[defaultBackend]; exists {
			backendsToCheck[defaultBackend] = backend
		} else {
			return nil, fmt.Errorf("default backend %s not found", defaultBackend)
		}
	} else {
		return nil, errors.New("no backend specified for health check")
	}

	// Check backends concurrently
	var (
		wg sync.WaitGroup
		mu sync.Mutex
	)

	for name, backend := range backendsToCheck {
		wg.Add(1)

		go func(name string, backend Storage) {
			defer wg.Done()

			backendHealth := hc.checkBackend(ctx, name, backend)

			mu.Lock()

			health.Backends[name] = backendHealth
			if backendHealth.Healthy {
				health.HealthyCount++
			} else {
				health.UnhealthyCount++
				health.Healthy = false
			}

			mu.Unlock()
		}(name, backend)
	}

	wg.Wait()

	// Update metrics if enabled
	if hc.config.EnableMetrics {
		for name, backendHealth := range health.Backends {
			status := "healthy"
			if !backendHealth.Healthy {
				status = "unhealthy"
			}

			hc.metrics.Gauge("storage_backend_health", "backend", name, "status", status).Set(
				map[bool]float64{true: 1.0, false: 0.0}[backendHealth.Healthy],
			)
			hc.metrics.Histogram("storage_health_check_duration", "backend", name).Observe(
				backendHealth.ResponseTime.Seconds(),
			)
		}
	}

	return health, nil
}

// checkBackend checks health of a single backend.
func (hc *HealthChecker) checkBackend(ctx context.Context, name string, backend Storage) BackendHealth {
	start := time.Now()

	health := BackendHealth{
		Name:        name,
		Healthy:     false,
		LastChecked: start,
	}

	// Try different check types based on configuration
	var err error
	if hc.config.WriteTestFile {
		err = hc.writeCheckBackend(ctx, backend)
		health.CheckType = "write"
	} else {
		err = hc.listCheckBackend(ctx, backend)
		health.CheckType = "list"
	}

	health.ResponseTime = time.Since(start)

	if err != nil {
		health.Error = err.Error()
		hc.logger.Warn("backend health check failed",
			forge.F("backend", name),
			forge.F("error", err.Error()),
			forge.F("duration", health.ResponseTime),
		)
	} else {
		health.Healthy = true
		hc.logger.Debug("backend health check passed",
			forge.F("backend", name),
			forge.F("duration", health.ResponseTime),
		)
	}

	return health
}

// writeCheckBackend performs write-based health check.
func (hc *HealthChecker) writeCheckBackend(ctx context.Context, backend Storage) error {
	testKey := fmt.Sprintf("%s/%d", hc.config.TestKey, time.Now().Unix())
	testData := []byte("health_check")

	// Write test file
	if err := backend.Upload(ctx, testKey, &readSeeker{data: testData}, WithContentType("text/plain")); err != nil {
		return fmt.Errorf("upload failed: %w", err)
	}

	// Verify it exists
	exists, err := backend.Exists(ctx, testKey)
	if err != nil {
		return fmt.Errorf("exists check failed: %w", err)
	}

	if !exists {
		return errors.New("uploaded file does not exist")
	}

	// Clean up test file (best effort)
	backend.Delete(ctx, testKey)

	return nil
}

// listCheckBackend performs list-based health check.
func (hc *HealthChecker) listCheckBackend(ctx context.Context, backend Storage) error {
	// Try to list with a limit
	_, err := backend.List(ctx, "", WithLimit(1))
	if err != nil {
		return fmt.Errorf("list operation failed: %w", err)
	}

	return nil
}

// GetBackendHealth gets health status of a specific backend.
func (hc *HealthChecker) GetBackendHealth(ctx context.Context, name string) (*BackendHealth, error) {
	backend, exists := hc.backends[name]
	if !exists {
		return nil, fmt.Errorf("backend %s not found", name)
	}

	ctx, cancel := context.WithTimeout(ctx, hc.config.Timeout)
	defer cancel()

	health := hc.checkBackend(ctx, name, backend)

	return &health, nil
}

// readSeeker implements io.Reader and io.Seeker for byte slices.
type readSeeker struct {
	data   []byte
	offset int
}

func (rs *readSeeker) Read(p []byte) (n int, err error) {
	if rs.offset >= len(rs.data) {
		return 0, io.EOF
	}

	n = copy(p, rs.data[rs.offset:])
	rs.offset += n

	return n, nil
}

func (rs *readSeeker) Seek(offset int64, whence int) (int64, error) {
	var newOffset int64

	switch whence {
	case io.SeekStart:
		newOffset = offset
	case io.SeekCurrent:
		newOffset = int64(rs.offset) + offset
	case io.SeekEnd:
		newOffset = int64(len(rs.data)) + offset
	default:
		return 0, errors.New("invalid whence")
	}

	if newOffset < 0 {
		return 0, errors.New("negative position")
	}

	rs.offset = int(newOffset)

	return newOffset, nil
}
