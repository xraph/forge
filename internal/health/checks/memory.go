package checks

// Conversions are used for calculating memory statistics from runtime data.

import (
	"context"
	"fmt"
	"runtime"
	"time"

	health "github.com/xraph/forge/internal/health/internal"
)

// MemoryHealthCheck performs health checks on memory usage.
type MemoryHealthCheck struct {
	*health.BaseHealthCheck

	warningThreshold  float64 // Percentage (0-100)
	criticalThreshold float64 // Percentage (0-100)
	checkGCStats      bool
	checkAllocStats   bool
}

// MemoryHealthCheckConfig contains configuration for memory health checks.
type MemoryHealthCheckConfig struct {
	Name              string
	WarningThreshold  float64 // Percentage (0-100)
	CriticalThreshold float64 // Percentage (0-100)
	CheckGCStats      bool
	CheckAllocStats   bool
	Timeout           time.Duration
	Critical          bool
	Tags              map[string]string
}

// NewMemoryHealthCheck creates a new memory health check.
func NewMemoryHealthCheck(config *MemoryHealthCheckConfig) *MemoryHealthCheck {
	if config == nil {
		config = &MemoryHealthCheckConfig{}
	}

	if config.Name == "" {
		config.Name = "memory"
	}

	if config.WarningThreshold == 0 {
		config.WarningThreshold = 80.0 // 80%
	}

	if config.CriticalThreshold == 0 {
		config.CriticalThreshold = 90.0 // 90%
	}

	if config.Timeout == 0 {
		config.Timeout = 5 * time.Second
	}

	if config.Tags == nil {
		config.Tags = make(map[string]string)
	}

	config.Tags["check_type"] = "memory"

	baseConfig := &health.HealthCheckConfig{
		Name:     config.Name,
		Timeout:  config.Timeout,
		Critical: config.Critical,
		Tags:     config.Tags,
	}

	return &MemoryHealthCheck{
		BaseHealthCheck:   health.NewBaseHealthCheck(baseConfig),
		warningThreshold:  config.WarningThreshold,
		criticalThreshold: config.CriticalThreshold,
		checkGCStats:      config.CheckGCStats,
		checkAllocStats:   config.CheckAllocStats,
	}
}

// Check performs the memory health check.
func (mhc *MemoryHealthCheck) Check(ctx context.Context) *health.HealthResult {
	start := time.Now()

	result := health.NewHealthResult(mhc.Name(), health.HealthStatusHealthy, "memory usage is normal").
		WithCritical(mhc.Critical()).
		WithTags(mhc.Tags())

	// Get memory statistics
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// Calculate memory usage percentage
	usagePercent := mhc.calculateMemoryUsage(&memStats)

	// Add basic memory details
	result.WithDetail("heap_alloc_bytes", memStats.HeapAlloc).
		WithDetail("heap_sys_bytes", memStats.HeapSys).
		WithDetail("heap_idle_bytes", memStats.HeapIdle).
		WithDetail("heap_inuse_bytes", memStats.HeapInuse).
		WithDetail("heap_released_bytes", memStats.HeapReleased).
		WithDetail("heap_objects", memStats.HeapObjects).
		WithDetail("sys_bytes", memStats.Sys).
		WithDetail("usage_percent", fmt.Sprintf("%.2f", usagePercent))

	// Check GC stats if enabled
	if mhc.checkGCStats {
		gcStats := mhc.getGCStats(&memStats)
		for k, v := range gcStats {
			result.WithDetail(k, v)
		}
	}

	// Check allocation stats if enabled
	if mhc.checkAllocStats {
		allocStats := mhc.getAllocStats(&memStats)
		for k, v := range allocStats {
			result.WithDetail(k, v)
		}
	}

	// Determine health status based on thresholds
	if usagePercent >= mhc.criticalThreshold {
		result = result.WithError(fmt.Errorf("memory usage %.2f%% exceeds critical threshold %.2f%%", usagePercent, mhc.criticalThreshold)).
			WithDetail("threshold_exceeded", "critical")
	} else if usagePercent >= mhc.warningThreshold {
		result.Status = health.HealthStatusDegraded
		result.Message = fmt.Sprintf("memory usage %.2f%% exceeds warning threshold %.2f%%", usagePercent, mhc.warningThreshold)
		result.WithDetail("threshold_exceeded", "warning")
	}

	result.WithDuration(time.Since(start))

	return result
}

// calculateMemoryUsage calculates memory usage percentage.
func (mhc *MemoryHealthCheck) calculateMemoryUsage(memStats *runtime.MemStats) float64 {
	// Use heap in-use as a percentage of heap system memory
	if memStats.HeapSys == 0 {
		return 0.0
	}

	return float64(memStats.HeapInuse) / float64(memStats.HeapSys) * 100.0
}

// getGCStats returns garbage collection statistics.
func (mhc *MemoryHealthCheck) getGCStats(memStats *runtime.MemStats) map[string]any {
	return map[string]any{
		"gc_cycles":        memStats.NumGC,
		"gc_cpu_fraction":  memStats.GCCPUFraction,
		"gc_sys_bytes":     memStats.GCSys,
		"next_gc_bytes":    memStats.NextGC,
		"last_gc_time":     time.Unix(0, int64(memStats.LastGC)).Format(time.RFC3339), //nolint:gosec // G115: LastGC is timestamp, safe conversion
		"pause_total_ns":   memStats.PauseTotalNs,
		"pause_ns":         memStats.PauseNs,
		"forced_gc_cycles": memStats.NumForcedGC,
	}
}

// getAllocStats returns allocation statistics.
func (mhc *MemoryHealthCheck) getAllocStats(memStats *runtime.MemStats) map[string]any {
	return map[string]any{
		"total_alloc_bytes":     memStats.TotalAlloc,
		"mallocs":               memStats.Mallocs,
		"frees":                 memStats.Frees,
		"lookups":               memStats.Lookups,
		"stack_inuse_bytes":     memStats.StackInuse,
		"stack_sys_bytes":       memStats.StackSys,
		"mspan_inuse_bytes":     memStats.MSpanInuse,
		"mspan_sys_bytes":       memStats.MSpanSys,
		"mcache_inuse_bytes":    memStats.MCacheInuse,
		"mcache_sys_bytes":      memStats.MCacheSys,
		"bucket_hash_sys_bytes": memStats.BuckHashSys,
		"other_sys_bytes":       memStats.OtherSys,
	}
}

// GoRoutineHealthCheck performs health checks on goroutine usage.
type GoRoutineHealthCheck struct {
	*health.BaseHealthCheck

	warningThreshold  int
	criticalThreshold int
}

// NewGoRoutineHealthCheck creates a new goroutine health check.
func NewGoRoutineHealthCheck(config *MemoryHealthCheckConfig) *GoRoutineHealthCheck {
	if config == nil {
		config = &MemoryHealthCheckConfig{}
	}

	if config.Name == "" {
		config.Name = "goroutines"
	}

	if config.Tags == nil {
		config.Tags = make(map[string]string)
	}

	config.Tags["check_type"] = "goroutines"

	baseConfig := &health.HealthCheckConfig{
		Name:     config.Name,
		Timeout:  config.Timeout,
		Critical: config.Critical,
		Tags:     config.Tags,
	}

	return &GoRoutineHealthCheck{
		BaseHealthCheck:   health.NewBaseHealthCheck(baseConfig),
		warningThreshold:  1000,  // 1000 goroutines
		criticalThreshold: 10000, // 10000 goroutines
	}
}

// Check performs the goroutine health check.
func (ghc *GoRoutineHealthCheck) Check(ctx context.Context) *health.HealthResult {
	start := time.Now()

	result := health.NewHealthResult(ghc.Name(), health.HealthStatusHealthy, "goroutine count is normal").
		WithCritical(ghc.Critical()).
		WithTags(ghc.Tags())

	// Get goroutine count
	goroutineCount := runtime.NumGoroutine()

	result.WithDetail("goroutine_count", goroutineCount).
		WithDetail("warning_threshold", ghc.warningThreshold).
		WithDetail("critical_threshold", ghc.criticalThreshold)

	// Check thresholds
	if goroutineCount >= ghc.criticalThreshold {
		result = result.WithError(fmt.Errorf("goroutine count %d exceeds critical threshold %d", goroutineCount, ghc.criticalThreshold)).
			WithDetail("threshold_exceeded", "critical")
	} else if goroutineCount >= ghc.warningThreshold {
		result.Status = health.HealthStatusDegraded
		result.Message = fmt.Sprintf("goroutine count %d exceeds warning threshold %d", goroutineCount, ghc.warningThreshold)
		result.WithDetail("threshold_exceeded", "warning")
	}

	result.WithDuration(time.Since(start))

	return result
}

// SetWarningThreshold sets the warning threshold for goroutine count.
func (ghc *GoRoutineHealthCheck) SetWarningThreshold(threshold int) {
	ghc.warningThreshold = threshold
}

// SetCriticalThreshold sets the critical threshold for goroutine count.
func (ghc *GoRoutineHealthCheck) SetCriticalThreshold(threshold int) {
	ghc.criticalThreshold = threshold
}

// HeapHealthCheck performs detailed heap health checks.
type HeapHealthCheck struct {
	*health.BaseHealthCheck

	maxHeapSize       uint64
	warningThreshold  float64
	criticalThreshold float64
}

// NewHeapHealthCheck creates a new heap health check.
func NewHeapHealthCheck(config *MemoryHealthCheckConfig) *HeapHealthCheck {
	if config == nil {
		config = &MemoryHealthCheckConfig{}
	}

	if config.Name == "" {
		config.Name = "heap"
	}

	if config.WarningThreshold == 0 {
		config.WarningThreshold = 80.0
	}

	if config.CriticalThreshold == 0 {
		config.CriticalThreshold = 90.0
	}

	if config.Tags == nil {
		config.Tags = make(map[string]string)
	}

	config.Tags["check_type"] = "heap"

	baseConfig := &health.HealthCheckConfig{
		Name:     config.Name,
		Timeout:  config.Timeout,
		Critical: config.Critical,
		Tags:     config.Tags,
	}

	return &HeapHealthCheck{
		BaseHealthCheck:   health.NewBaseHealthCheck(baseConfig),
		maxHeapSize:       1024 * 1024 * 1024, // 1GB default
		warningThreshold:  config.WarningThreshold,
		criticalThreshold: config.CriticalThreshold,
	}
}

// Check performs the heap health check.
func (hhc *HeapHealthCheck) Check(ctx context.Context) *health.HealthResult {
	start := time.Now()

	result := health.NewHealthResult(hhc.Name(), health.HealthStatusHealthy, "heap usage is normal").
		WithCritical(hhc.Critical()).
		WithTags(hhc.Tags())

	// Get memory statistics
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// Calculate heap usage percentage
	heapUsagePercent := float64(memStats.HeapAlloc) / float64(hhc.maxHeapSize) * 100.0

	result.WithDetail("heap_alloc_bytes", memStats.HeapAlloc).
		WithDetail("heap_sys_bytes", memStats.HeapSys).
		WithDetail("heap_idle_bytes", memStats.HeapIdle).
		WithDetail("heap_inuse_bytes", memStats.HeapInuse).
		WithDetail("heap_released_bytes", memStats.HeapReleased).
		WithDetail("heap_objects", memStats.HeapObjects).
		WithDetail("max_heap_size", hhc.maxHeapSize).
		WithDetail("heap_usage_percent", fmt.Sprintf("%.2f", heapUsagePercent))

	// Check thresholds
	if heapUsagePercent >= hhc.criticalThreshold {
		result = result.WithError(fmt.Errorf("heap usage %.2f%% exceeds critical threshold %.2f%%", heapUsagePercent, hhc.criticalThreshold)).
			WithDetail("threshold_exceeded", "critical")
	} else if heapUsagePercent >= hhc.warningThreshold {
		result.Status = health.HealthStatusDegraded
		result.Message = fmt.Sprintf("heap usage %.2f%% exceeds warning threshold %.2f%%", heapUsagePercent, hhc.warningThreshold)
		result.WithDetail("threshold_exceeded", "warning")
	}

	result.WithDuration(time.Since(start))

	return result
}

// SetMaxHeapSize sets the maximum heap size for calculations.
func (hhc *HeapHealthCheck) SetMaxHeapSize(size uint64) {
	hhc.maxHeapSize = size
}

// GCHealthCheck performs garbage collection health checks.
type GCHealthCheck struct {
	*health.BaseHealthCheck

	gcPauseThreshold     time.Duration
	gcCPUThreshold       float64
	gcFrequencyThreshold uint32
}

// NewGCHealthCheck creates a new garbage collection health check.
func NewGCHealthCheck(config *MemoryHealthCheckConfig) *GCHealthCheck {
	if config == nil {
		config = &MemoryHealthCheckConfig{}
	}

	if config.Name == "" {
		config.Name = "gc"
	}

	if config.Tags == nil {
		config.Tags = make(map[string]string)
	}

	config.Tags["check_type"] = "gc"

	baseConfig := &health.HealthCheckConfig{
		Name:     config.Name,
		Timeout:  config.Timeout,
		Critical: config.Critical,
		Tags:     config.Tags,
	}

	return &GCHealthCheck{
		BaseHealthCheck:      health.NewBaseHealthCheck(baseConfig),
		gcPauseThreshold:     100 * time.Millisecond, // 100ms
		gcCPUThreshold:       0.05,                   // 5%
		gcFrequencyThreshold: 1000,                   // 1000 GC cycles
	}
}

// Check performs the garbage collection health check.
func (gchc *GCHealthCheck) Check(ctx context.Context) *health.HealthResult {
	start := time.Now()

	result := health.NewHealthResult(gchc.Name(), health.HealthStatusHealthy, "garbage collection is healthy").
		WithCritical(gchc.Critical()).
		WithTags(gchc.Tags())

	// Get memory statistics
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// Calculate average GC pause time
	avgPauseTime := time.Duration(0)
	if memStats.NumGC > 0 {
		avgPauseTime = time.Duration(memStats.PauseTotalNs / uint64(memStats.NumGC)) //nolint:gosec // G115: pause time is always non-negative
	}

	result.WithDetail("gc_cycles", memStats.NumGC).
		WithDetail("gc_cpu_fraction", memStats.GCCPUFraction).
		WithDetail("gc_sys_bytes", memStats.GCSys).
		WithDetail("next_gc_bytes", memStats.NextGC).
		WithDetail("last_gc_time", time.Unix(0, int64(memStats.LastGC)).Format(time.RFC3339)). //nolint:gosec // G115: LastGC is timestamp, safe conversion
		WithDetail("pause_total_ns", memStats.PauseTotalNs).
		WithDetail("avg_pause_time", avgPauseTime.String()).
		WithDetail("forced_gc_cycles", memStats.NumForcedGC)

	// Check GC pause time threshold
	if avgPauseTime > gchc.gcPauseThreshold {
		result.Status = health.HealthStatusDegraded
		result.Message = fmt.Sprintf("average GC pause time %v exceeds threshold %v", avgPauseTime, gchc.gcPauseThreshold)
		result.WithDetail("threshold_exceeded", "gc_pause")
	}

	// Check GC CPU usage threshold
	if memStats.GCCPUFraction > gchc.gcCPUThreshold {
		result.Status = health.HealthStatusDegraded
		result.Message = fmt.Sprintf("GC CPU usage %.2f%% exceeds threshold %.2f%%", memStats.GCCPUFraction*100, gchc.gcCPUThreshold*100)
		result.WithDetail("threshold_exceeded", "gc_cpu")
	}

	// Check GC frequency threshold
	if memStats.NumGC > gchc.gcFrequencyThreshold {
		result.Status = health.HealthStatusDegraded
		result.Message = fmt.Sprintf("GC frequency %d exceeds threshold %d", memStats.NumGC, gchc.gcFrequencyThreshold)
		result.WithDetail("threshold_exceeded", "gc_frequency")
	}

	result.WithDuration(time.Since(start))

	return result
}

// SetGCPauseThreshold sets the GC pause time threshold.
func (gchc *GCHealthCheck) SetGCPauseThreshold(threshold time.Duration) {
	gchc.gcPauseThreshold = threshold
}

// SetGCCPUThreshold sets the GC CPU usage threshold.
func (gchc *GCHealthCheck) SetGCCPUThreshold(threshold float64) {
	gchc.gcCPUThreshold = threshold
}

// SetGCFrequencyThreshold sets the GC frequency threshold.
func (gchc *GCHealthCheck) SetGCFrequencyThreshold(threshold uint32) {
	gchc.gcFrequencyThreshold = threshold
}

// MemoryHealthCheckComposite combines multiple memory health checks.
type MemoryHealthCheckComposite struct {
	*health.CompositeHealthCheck

	memoryChecks []health.HealthCheck
}

// NewMemoryHealthCheckComposite creates a composite memory health check.
func NewMemoryHealthCheckComposite(name string, checks ...health.HealthCheck) *MemoryHealthCheckComposite {
	config := &health.HealthCheckConfig{
		Name:     name,
		Timeout:  15 * time.Second,
		Critical: true,
	}

	composite := health.NewCompositeHealthCheck(config, checks...)

	return &MemoryHealthCheckComposite{
		CompositeHealthCheck: composite,
		memoryChecks:         checks,
	}
}

// GetMemoryChecks returns the individual memory checks.
func (mhcc *MemoryHealthCheckComposite) GetMemoryChecks() []health.HealthCheck {
	return mhcc.memoryChecks
}

// AddMemoryCheck adds a memory check to the composite.
func (mhcc *MemoryHealthCheckComposite) AddMemoryCheck(check health.HealthCheck) {
	mhcc.memoryChecks = append(mhcc.memoryChecks, check)
	mhcc.AddCheck(check)
}

// RegisterMemoryHealthChecks registers memory health checks with the health service.
func RegisterMemoryHealthChecks(healthService health.HealthService) error {
	// Register memory health check
	memoryCheck := NewMemoryHealthCheck(&MemoryHealthCheckConfig{
		Name:              "memory",
		WarningThreshold:  80.0,
		CriticalThreshold: 90.0,
		CheckGCStats:      true,
		CheckAllocStats:   true,
		Critical:          false,
	})

	if err := healthService.Register(memoryCheck); err != nil {
		return fmt.Errorf("failed to register memory health check: %w", err)
	}

	// Register goroutine health check
	goroutineCheck := NewGoRoutineHealthCheck(&MemoryHealthCheckConfig{
		Name:     "goroutines",
		Critical: false,
	})

	if err := healthService.Register(goroutineCheck); err != nil {
		return fmt.Errorf("failed to register goroutine health check: %w", err)
	}

	// Register heap health check
	heapCheck := NewHeapHealthCheck(&MemoryHealthCheckConfig{
		Name:              "heap",
		WarningThreshold:  80.0,
		CriticalThreshold: 90.0,
		Critical:          false,
	})

	if err := healthService.Register(heapCheck); err != nil {
		return fmt.Errorf("failed to register heap health check: %w", err)
	}

	// Register GC health check
	gcCheck := NewGCHealthCheck(&MemoryHealthCheckConfig{
		Name:     "gc",
		Critical: false,
	})

	if err := healthService.Register(gcCheck); err != nil {
		return fmt.Errorf("failed to register GC health check: %w", err)
	}

	return nil
}

// CreateMemoryHealthCheckComposite creates a composite memory health check with all sub-checks.
func CreateMemoryHealthCheckComposite() *MemoryHealthCheckComposite {
	memoryCheck := NewMemoryHealthCheck(&MemoryHealthCheckConfig{
		Name:              "memory",
		WarningThreshold:  80.0,
		CriticalThreshold: 90.0,
		CheckGCStats:      true,
		CheckAllocStats:   true,
	})

	goroutineCheck := NewGoRoutineHealthCheck(&MemoryHealthCheckConfig{
		Name: "goroutines",
	})

	heapCheck := NewHeapHealthCheck(&MemoryHealthCheckConfig{
		Name:              "heap",
		WarningThreshold:  80.0,
		CriticalThreshold: 90.0,
	})

	gcCheck := NewGCHealthCheck(&MemoryHealthCheckConfig{
		Name: "gc",
	})

	return NewMemoryHealthCheckComposite("memory-composite", memoryCheck, goroutineCheck, heapCheck, gcCheck)
}
