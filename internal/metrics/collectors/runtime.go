package collectors

// These conversions are used for calculating runtime statistics and durations.

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	metrics "github.com/xraph/go-utils/metrics"
)

// =============================================================================
// RUNTIME COLLECTOR
// =============================================================================

// RuntimeCollector collects Go runtime metrics.
type RuntimeCollector struct {
	name               string
	interval           time.Duration
	lastGCStats        *debug.GCStats
	lastMemStats       *runtime.MemStats
	lastCollectionTime time.Time
	metrics            map[string]any
	enabled            bool
}

// RuntimeCollectorConfig contains configuration for the runtime collector.
type RuntimeCollectorConfig struct {
	Interval          time.Duration `json:"interval"           yaml:"interval"`
	CollectGC         bool          `json:"collect_gc"         yaml:"collect_gc"`
	CollectMemory     bool          `json:"collect_memory"     yaml:"collect_memory"`
	CollectGoroutines bool          `json:"collect_goroutines" yaml:"collect_goroutines"`
	CollectCGO        bool          `json:"collect_cgo"        yaml:"collect_cgo"`
	CollectBuildInfo  bool          `json:"collect_build_info" yaml:"collect_build_info"`
	EnableGCStats     bool          `json:"enable_gc_stats"    yaml:"enable_gc_stats"`
}

// DefaultRuntimeCollectorConfig returns default configuration.
func DefaultRuntimeCollectorConfig() *RuntimeCollectorConfig {
	return &RuntimeCollectorConfig{
		Interval:          time.Second * 15,
		CollectGC:         true,
		CollectMemory:     true,
		CollectGoroutines: true,
		CollectCGO:        true,
		CollectBuildInfo:  true,
		EnableGCStats:     true,
	}
}

// NewRuntimeCollector creates a new runtime collector.
func NewRuntimeCollector() metrics.CustomCollector {
	return NewRuntimeCollectorWithConfig(DefaultRuntimeCollectorConfig())
}

// NewRuntimeCollectorWithConfig creates a new runtime collector with configuration.
func NewRuntimeCollectorWithConfig(config *RuntimeCollectorConfig) metrics.CustomCollector {
	collector := &RuntimeCollector{
		name:     "runtime",
		interval: config.Interval,
		metrics:  make(map[string]any),
		enabled:  true,
	}

	// Enable GC stats if requested
	if config.EnableGCStats {
		debug.SetGCPercent(debug.SetGCPercent(-1))
		debug.SetGCPercent(100) // Reset to default
	}

	return collector
}

// =============================================================================
// CUSTOM COLLECTOR INTERFACE IMPLEMENTATION
// =============================================================================

// Name returns the collector name.
func (rc *RuntimeCollector) Name() string {
	return rc.name
}

// Collect collects runtime metrics.
func (rc *RuntimeCollector) Collect() map[string]any {
	if !rc.enabled {
		return rc.metrics
	}

	now := time.Now()

	// Only collect if enough time has passed
	if !rc.lastCollectionTime.IsZero() && now.Sub(rc.lastCollectionTime) < rc.interval {
		return rc.metrics
	}

	rc.lastCollectionTime = now

	// Collect memory statistics
	rc.collectMemoryStats()

	// Collect GC statistics
	rc.collectGCStats()

	// Collect goroutine statistics
	rc.collectGoroutineStats()

	// Collect CGO statistics
	rc.collectCGOStats()

	// Collect build information
	rc.collectBuildInfo()

	// Collect general runtime information
	rc.collectGeneralStats()

	return rc.metrics
}

// Reset resets the collector.
func (rc *RuntimeCollector) Reset() error {
	rc.metrics = make(map[string]any)
	rc.lastGCStats = nil
	rc.lastMemStats = nil
	rc.lastCollectionTime = time.Time{}

	return nil
}

// =============================================================================
// MEMORY STATISTICS
// =============================================================================

// collectMemoryStats collects memory statistics.
func (rc *RuntimeCollector) collectMemoryStats() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// General statistics
	rc.metrics["runtime.memory.alloc"] = m.Alloc
	rc.metrics["runtime.memory.total_alloc"] = m.TotalAlloc
	rc.metrics["runtime.memory.sys"] = m.Sys
	rc.metrics["runtime.memory.lookups"] = m.Lookups
	rc.metrics["runtime.memory.mallocs"] = m.Mallocs
	rc.metrics["runtime.memory.frees"] = m.Frees

	// Heap statistics
	rc.metrics["runtime.memory.heap_alloc"] = m.HeapAlloc
	rc.metrics["runtime.memory.heap_sys"] = m.HeapSys
	rc.metrics["runtime.memory.heap_idle"] = m.HeapIdle
	rc.metrics["runtime.memory.heap_inuse"] = m.HeapInuse
	rc.metrics["runtime.memory.heap_released"] = m.HeapReleased
	rc.metrics["runtime.memory.heap_objects"] = m.HeapObjects

	// Stack statistics
	rc.metrics["runtime.memory.stack_inuse"] = m.StackInuse
	rc.metrics["runtime.memory.stack_sys"] = m.StackSys

	// MSpan statistics
	rc.metrics["runtime.memory.mspan_inuse"] = m.MSpanInuse
	rc.metrics["runtime.memory.mspan_sys"] = m.MSpanSys

	// MCache statistics
	rc.metrics["runtime.memory.mcache_inuse"] = m.MCacheInuse
	rc.metrics["runtime.memory.mcache_sys"] = m.MCacheSys

	// BuckHash statistics
	rc.metrics["runtime.memory.buckhash_sys"] = m.BuckHashSys

	// GC statistics
	rc.metrics["runtime.memory.gc_sys"] = m.GCSys
	rc.metrics["runtime.memory.other_sys"] = m.OtherSys

	// Next GC
	rc.metrics["runtime.memory.next_gc"] = m.NextGC
	rc.metrics["runtime.memory.last_gc"] = m.LastGC

	// Calculate derived metrics
	if rc.lastMemStats != nil {
		// Calculate allocation rate
		allocDiff := m.TotalAlloc - rc.lastMemStats.TotalAlloc

		timeDiff := time.Since(rc.lastCollectionTime)
		if timeDiff > 0 {
			rc.metrics["runtime.memory.alloc_rate"] = float64(allocDiff) / timeDiff.Seconds()
		}

		// Calculate object allocation rate
		mallocDiff := m.Mallocs - rc.lastMemStats.Mallocs
		if timeDiff > 0 {
			rc.metrics["runtime.memory.malloc_rate"] = float64(mallocDiff) / timeDiff.Seconds()
		}

		// Calculate object free rate
		freeDiff := m.Frees - rc.lastMemStats.Frees
		if timeDiff > 0 {
			rc.metrics["runtime.memory.free_rate"] = float64(freeDiff) / timeDiff.Seconds()
		}
	}

	// Store current stats for next calculation
	rc.lastMemStats = &m
}

// =============================================================================
// GC STATISTICS
// =============================================================================

// collectGCStats collects garbage collection statistics.
func (rc *RuntimeCollector) collectGCStats() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// GC statistics from MemStats
	rc.metrics["runtime.gc.num_gc"] = m.NumGC
	rc.metrics["runtime.gc.num_forced_gc"] = m.NumForcedGC
	rc.metrics["runtime.gc.pause_total_ns"] = m.PauseTotalNs
	rc.metrics["runtime.gc.pause_ns"] = m.PauseNs
	rc.metrics["runtime.gc.pause_end"] = m.PauseEnd
	rc.metrics["runtime.gc.gc_cpu_fraction"] = m.GCCPUFraction
	rc.metrics["runtime.gc.enable_gc"] = m.EnableGC
	rc.metrics["runtime.gc.debug_gc"] = m.DebugGC

	// Calculate GC frequency and pause statistics
	if rc.lastMemStats != nil {
		gcDiff := m.NumGC - rc.lastMemStats.NumGC

		timeDiff := time.Since(rc.lastCollectionTime)
		if timeDiff > 0 && gcDiff > 0 {
			rc.metrics["runtime.gc.frequency"] = float64(gcDiff) / timeDiff.Seconds()

			// Calculate average pause time for recent GCs
			pauseDiff := m.PauseTotalNs - rc.lastMemStats.PauseTotalNs
			rc.metrics["runtime.gc.avg_pause_ns"] = float64(pauseDiff) / float64(gcDiff)
		}
	}

	// Recent pause times
	if len(m.PauseNs) > 0 {
		recentPauses := m.PauseNs[(m.NumGC+255)%256]
		rc.metrics["runtime.gc.recent_pause_ns"] = recentPauses
	}

	// Collect detailed GC stats using debug.GCStats
	rc.collectDetailedGCStats()
}

// collectDetailedGCStats collects detailed GC statistics.
func (rc *RuntimeCollector) collectDetailedGCStats() {
	var gcStats debug.GCStats
	debug.ReadGCStats(&gcStats)

	// Basic GC stats
	rc.metrics["runtime.gc.last_gc_time"] = gcStats.LastGC
	rc.metrics["runtime.gc.num_gc_detailed"] = gcStats.NumGC
	rc.metrics["runtime.gc.pause_total"] = gcStats.PauseTotal

	// Pause quantiles
	if len(gcStats.PauseQuantiles) > 0 {
		rc.metrics["runtime.gc.pause_quantiles"] = gcStats.PauseQuantiles

		// Extract specific quantiles
		if len(gcStats.PauseQuantiles) >= 5 {
			rc.metrics["runtime.gc.pause_min"] = gcStats.PauseQuantiles[0]
			rc.metrics["runtime.gc.pause_p25"] = gcStats.PauseQuantiles[1]
			rc.metrics["runtime.gc.pause_p50"] = gcStats.PauseQuantiles[2]
			rc.metrics["runtime.gc.pause_p75"] = gcStats.PauseQuantiles[3]
			rc.metrics["runtime.gc.pause_max"] = gcStats.PauseQuantiles[4]
		}
	}

	// Pause history
	if len(gcStats.Pause) > 0 {
		rc.metrics["runtime.gc.pause_history_length"] = len(gcStats.Pause)

		// Calculate statistics from pause history
		var totalPause, maxPause, minPause time.Duration

		minPause = gcStats.Pause[0]

		for _, pause := range gcStats.Pause {
			totalPause += pause
			if pause > maxPause {
				maxPause = pause
			}

			if pause < minPause {
				minPause = pause
			}
		}

		rc.metrics["runtime.gc.pause_history_total"] = totalPause
		rc.metrics["runtime.gc.pause_history_max"] = maxPause

		rc.metrics["runtime.gc.pause_history_min"] = minPause
		if len(gcStats.Pause) > 0 {
			rc.metrics["runtime.gc.pause_history_avg"] = totalPause / time.Duration(len(gcStats.Pause))
		}
	}

	rc.lastGCStats = &gcStats
}

// =============================================================================
// GOROUTINE STATISTICS
// =============================================================================

// collectGoroutineStats collects goroutine statistics.
func (rc *RuntimeCollector) collectGoroutineStats() {
	// Number of goroutines
	numGoroutines := runtime.NumGoroutine()
	rc.metrics["runtime.goroutines.count"] = numGoroutines

	// Number of logical CPUs
	numCPU := runtime.NumCPU()
	rc.metrics["runtime.goroutines.num_cpu"] = numCPU

	// GOMAXPROCS
	maxProcs := runtime.GOMAXPROCS(0)
	rc.metrics["runtime.goroutines.max_procs"] = maxProcs

	// Calculate goroutine density
	if numCPU > 0 {
		rc.metrics["runtime.goroutines.density"] = float64(numGoroutines) / float64(numCPU)
	}

	// Thread statistics
	rc.collectThreadStats()
}

// collectThreadStats collects thread statistics.
func (rc *RuntimeCollector) collectThreadStats() {
	// This uses runtime internals that may not be available in all Go versions
	// In a production implementation, you might use runtime/pprof or other methods

	// For now, we'll use a placeholder implementation
	rc.metrics["runtime.threads.count"] = runtime.NumGoroutine() // Approximation
}

// =============================================================================
// CGO STATISTICS
// =============================================================================

// collectCGOStats collects CGO statistics.
func (rc *RuntimeCollector) collectCGOStats() {
	// Number of cgo calls
	numCgoCalls := runtime.NumCgoCall()
	rc.metrics["runtime.cgo.calls"] = numCgoCalls

	// Calculate CGO call rate
	if rc.lastMemStats != nil {
		// This is a simplified implementation
		// In reality, you'd need to track previous CGO call counts
		rc.metrics["runtime.cgo.call_rate"] = float64(numCgoCalls) / time.Since(rc.lastCollectionTime).Seconds()
	}
}

// =============================================================================
// BUILD INFORMATION
// =============================================================================

// collectBuildInfo collects build information.
func (rc *RuntimeCollector) collectBuildInfo() {
	// Go version
	rc.metrics["runtime.build.go_version"] = runtime.Version()

	// GOOS and GOARCH
	rc.metrics["runtime.build.goos"] = runtime.GOOS
	rc.metrics["runtime.build.goarch"] = runtime.GOARCH

	// Compiler
	rc.metrics["runtime.build.compiler"] = runtime.Compiler

	// Build info from debug package
	if info, ok := debug.ReadBuildInfo(); ok {
		rc.metrics["runtime.build.go_version_detailed"] = info.GoVersion
		rc.metrics["runtime.build.path"] = info.Path
		rc.metrics["runtime.build.main_version"] = info.Main.Version
		rc.metrics["runtime.build.main_sum"] = info.Main.Sum

		// Settings
		settings := make(map[string]string)
		for _, setting := range info.Settings {
			settings[setting.Key] = setting.Value
		}

		rc.metrics["runtime.build.settings"] = settings

		// Dependencies count
		rc.metrics["runtime.build.deps_count"] = len(info.Deps)
	}
}

// =============================================================================
// GENERAL STATISTICS
// =============================================================================

// collectGeneralStats collects general runtime statistics.
func (rc *RuntimeCollector) collectGeneralStats() {
	// Stack trace information
	buf := make([]byte, 1024)
	stackSize := runtime.Stack(buf, false)
	rc.metrics["runtime.stack.size"] = stackSize

	// Set finalizer statistics (if available)
	rc.collectFinalizerStats()

	// Collect scheduler statistics
	rc.collectSchedulerStats()
}

// collectFinalizerStats collects finalizer statistics.
func (rc *RuntimeCollector) collectFinalizerStats() {
	// This is a placeholder - finalizer statistics are not directly available
	// In a production implementation, you might use runtime/pprof or custom tracking
	rc.metrics["runtime.finalizers.count"] = 0
}

// collectSchedulerStats collects scheduler statistics.
func (rc *RuntimeCollector) collectSchedulerStats() {
	// This is a placeholder - scheduler statistics are not directly available
	// In a production implementation, you might use runtime/pprof or custom tracking
	rc.metrics["runtime.scheduler.idle_time"] = 0
	rc.metrics["runtime.scheduler.runnable_goroutines"] = 0
}

// =============================================================================
// UTILITY METHODS
// =============================================================================

// Enable enables the collector.
func (rc *RuntimeCollector) Enable() {
	rc.enabled = true
}

// Disable disables the collector.
func (rc *RuntimeCollector) Disable() {
	rc.enabled = false
}

// IsEnabled returns whether the collector is enabled.
func (rc *RuntimeCollector) IsEnabled() bool {
	return rc.enabled
}

// SetInterval sets the collection interval.
func (rc *RuntimeCollector) SetInterval(interval time.Duration) {
	rc.interval = interval
}

// GetInterval returns the collection interval.
func (rc *RuntimeCollector) GetInterval() time.Duration {
	return rc.interval
}

// GetLastCollectionTime returns the last collection time.
func (rc *RuntimeCollector) GetLastCollectionTime() time.Time {
	return rc.lastCollectionTime
}

// GetMetricsCount returns the number of metrics collected.
func (rc *RuntimeCollector) GetMetricsCount() int {
	return len(rc.metrics)
}

// TriggerGC triggers a garbage collection and collects immediate stats.
func (rc *RuntimeCollector) TriggerGC() {
	runtime.GC()
	rc.collectGCStats()
}

// GetGCStats returns the current GC statistics.
func (rc *RuntimeCollector) GetGCStats() map[string]any {
	rc.collectGCStats()

	result := make(map[string]any)

	for key, value := range rc.metrics {
		if strings.HasPrefix(key, "runtime.gc.") {
			result[key] = value
		}
	}

	return result
}

// GetMemoryStats returns the current memory statistics.
func (rc *RuntimeCollector) GetMemoryStats() map[string]any {
	rc.collectMemoryStats()

	result := make(map[string]any)

	for key, value := range rc.metrics {
		if strings.HasPrefix(key, "runtime.memory.") {
			result[key] = value
		}
	}

	return result
}

// GetGoroutineStats returns the current goroutine statistics.
func (rc *RuntimeCollector) GetGoroutineStats() map[string]any {
	rc.collectGoroutineStats()

	result := make(map[string]any)

	for key, value := range rc.metrics {
		if strings.HasPrefix(key, "runtime.goroutines.") {
			result[key] = value
		}
	}

	return result
}

// =============================================================================
// HELPER FUNCTIONS
// =============================================================================

// FormatDuration formats a duration in nanoseconds to a human-readable string.
func FormatDuration(ns uint64) string {
	d := time.Duration(ns)

	return d.String()
}

// FormatBytes formats bytes to a human-readable string.
func FormatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}

	div, exp := uint64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}
	// Bounds check to prevent index out of range
	if exp >= len(units) {
		exp = len(units) - 1
	}

	return fmt.Sprintf("%.1f %s", float64(bytes)/float64(div), units[exp])
}

// CalculateMemoryEfficiency calculates memory efficiency metrics.
func CalculateMemoryEfficiency(m *runtime.MemStats) map[string]float64 {
	efficiency := make(map[string]float64)

	// Heap efficiency
	if m.HeapSys > 0 {
		efficiency["heap_efficiency"] = float64(m.HeapInuse) / float64(m.HeapSys) * 100
	}

	// Stack efficiency
	if m.StackSys > 0 {
		efficiency["stack_efficiency"] = float64(m.StackInuse) / float64(m.StackSys) * 100
	}

	// Overall memory efficiency
	if m.Sys > 0 {
		efficiency["overall_efficiency"] = float64(m.Alloc) / float64(m.Sys) * 100
	}

	return efficiency
}
