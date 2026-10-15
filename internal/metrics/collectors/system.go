package collectors

import (
	"errors"
	"maps"
	"strings"
	"sync"
	"time"

	metrics "github.com/xraph/go-utils/metrics"
)

// =============================================================================
// SYSTEM COLLECTOR
// =============================================================================

// errUnsupported is what a platform probe returns when the platform has no
// implementation. The collector then reports nothing for that probe, never a
// made-up value.
var errUnsupported = errors.New("not supported on this platform")

// SystemCollector collects host CPU, memory, swap, load and filesystem metrics.
//
// It reports only what the platform can measure. Linux reads /proc for CPU,
// memory, swap and load. Every Unix reads filesystem usage with statfs.
// Windows reports memory and filesystem usage.
//
// Series names carry no host detail. The mount point and the CPU mode are
// labels, in the key form the Prometheus bridge parses: name{label="value"}.
type SystemCollector struct {
	mu                 sync.Mutex
	name               string
	config             SystemCollectorConfig
	lastCPUStats       *CPUStats
	lastCollectionTime time.Time
	metrics            map[string]any
	enabled            bool
}

// SystemCollectorConfig contains configuration for the system collector.
type SystemCollectorConfig struct {
	Interval        time.Duration `json:"interval"          yaml:"interval"`
	CollectCPU      bool          `json:"collect_cpu"       yaml:"collect_cpu"`
	CollectMemory   bool          `json:"collect_memory"    yaml:"collect_memory"`
	CollectDisk     bool          `json:"collect_disk"      yaml:"collect_disk"`
	CollectLoad     bool          `json:"collect_load"      yaml:"collect_load"`
	DiskMountPoints []string      `json:"disk_mount_points" yaml:"disk_mount_points"`
}

// CPUStats holds cumulative CPU time in clock ticks, as /proc/stat reports it.
type CPUStats struct {
	User      uint64
	Nice      uint64
	System    uint64
	Idle      uint64
	IOWait    uint64
	IRQ       uint64
	SoftIRQ   uint64
	Steal     uint64
	Guest     uint64
	GuestNice uint64

	// Total is every mode except guest and guest_nice, which the kernel
	// already counts inside user and nice.
	Total uint64
}

// MemoryStats represents memory statistics in bytes.
type MemoryStats struct {
	Total     uint64
	Available uint64
	Used      uint64
	Free      uint64
	Buffers   uint64
	Cached    uint64
	SwapTotal uint64
	SwapFree  uint64
	SwapUsed  uint64

	// HasPageCache is set when Buffers and Cached were measured.
	HasPageCache bool

	// HasSwap is set when the swap fields were measured.
	HasSwap bool
}

// DiskStats represents filesystem usage for one mount point, in bytes.
type DiskStats struct {
	MountPoint string
	Total      uint64
	Used       uint64
	Available  uint64
}

// LoadStats represents system load statistics.
type LoadStats struct {
	Load1  float64
	Load5  float64
	Load15 float64
}

// DefaultSystemCollectorConfig returns default configuration.
func DefaultSystemCollectorConfig() *SystemCollectorConfig {
	return &SystemCollectorConfig{
		Interval:        time.Second * 30,
		CollectCPU:      true,
		CollectMemory:   true,
		CollectDisk:     true,
		CollectLoad:     true,
		DiskMountPoints: defaultMountPoints(),
	}
}

// NewSystemCollector creates a new system collector.
func NewSystemCollector() metrics.CustomCollector {
	return NewSystemCollectorWithConfig(DefaultSystemCollectorConfig())
}

// NewSystemCollectorWithConfig creates a new system collector with configuration.
func NewSystemCollectorWithConfig(config *SystemCollectorConfig) metrics.CustomCollector {
	if config == nil {
		config = DefaultSystemCollectorConfig()
	}

	return &SystemCollector{
		name:    "system",
		config:  *config,
		metrics: make(map[string]any),
		enabled: true,
	}
}

// =============================================================================
// CUSTOM COLLECTOR INTERFACE IMPLEMENTATION
// =============================================================================

// Name returns the collector name.
func (sc *SystemCollector) Name() string {
	return sc.name
}

// Collect collects system metrics. Between two collections closer together
// than the interval it returns the previous result, so a scrape and the
// scheduler loop can both call it without reading /proc twice.
func (sc *SystemCollector) Collect() map[string]any {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	now := time.Now()

	fresh := sc.lastCollectionTime.IsZero() || now.Sub(sc.lastCollectionTime) >= sc.config.Interval
	if sc.enabled && fresh {
		sc.lastCollectionTime = now
		sc.metrics = sc.collect()
	}

	result := make(map[string]any, len(sc.metrics))
	maps.Copy(result, sc.metrics)

	return result
}

// collect runs every enabled probe into a new map, so a mount point that goes
// away stops being reported.
func (sc *SystemCollector) collect() map[string]any {
	out := make(map[string]any)

	if sc.config.CollectCPU {
		if stats, err := readCPUStats(); err == nil {
			sc.addCPUMetrics(out, stats)
		}
	}

	if sc.config.CollectMemory {
		if stats, err := readMemoryStats(); err == nil {
			addMemoryMetrics(out, stats)
		}
	}

	if sc.config.CollectDisk {
		addDiskMetrics(out, collectDiskStats(sc.config.DiskMountPoints))
	}

	if sc.config.CollectLoad {
		if stats, err := readLoadStats(); err == nil {
			addLoadMetrics(out, stats)
		}
	}

	return out
}

// Reset resets the collector.
func (sc *SystemCollector) Reset() error {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	sc.metrics = make(map[string]any)
	sc.lastCPUStats = nil
	sc.lastCollectionTime = time.Time{}

	return nil
}

// =============================================================================
// SERIES
// =============================================================================

// addCPUMetrics reports the share of CPU time each mode took since the
// previous collection, as a ratio from 0 to 1. The first collection has
// nothing to compare against and reports no CPU series.
func (sc *SystemCollector) addCPUMetrics(out map[string]any, stats *CPUStats) {
	prev := sc.lastCPUStats
	sc.lastCPUStats = stats

	if prev == nil || stats.Total <= prev.Total {
		return
	}

	total := float64(stats.Total - prev.Total)

	modes := []struct {
		name      string
		cur, last uint64
	}{
		{"user", stats.User, prev.User},
		{"nice", stats.Nice, prev.Nice},
		{"system", stats.System, prev.System},
		{"idle", stats.Idle, prev.Idle},
		{"iowait", stats.IOWait, prev.IOWait},
		{"irq", stats.IRQ, prev.IRQ},
		{"softirq", stats.SoftIRQ, prev.SoftIRQ},
		{"steal", stats.Steal, prev.Steal},
	}

	for _, m := range modes {
		if m.cur < m.last {
			continue
		}

		out[labelled("system.cpu.utilization", "mode", m.name)] = float64(m.cur-m.last) / total
	}
}

// addMemoryMetrics adds memory and swap metrics to the collection.
func addMemoryMetrics(out map[string]any, stats *MemoryStats) {
	out["system.memory.size_bytes"] = stats.Total
	out["system.memory.available_bytes"] = stats.Available
	out["system.memory.used_bytes"] = stats.Used
	out["system.memory.free_bytes"] = stats.Free

	if stats.HasPageCache {
		out["system.memory.buffers_bytes"] = stats.Buffers
		out["system.memory.cached_bytes"] = stats.Cached
	}

	if stats.HasSwap {
		out["system.swap.size_bytes"] = stats.SwapTotal
		out["system.swap.free_bytes"] = stats.SwapFree
		out["system.swap.used_bytes"] = stats.SwapUsed
	}
}

// addDiskMetrics adds one labelled series per mount point and measure.
func addDiskMetrics(out map[string]any, stats []DiskStats) {
	for _, stat := range stats {
		out[labelled("system.filesystem.size_bytes", "mountpoint", stat.MountPoint)] = stat.Total
		out[labelled("system.filesystem.used_bytes", "mountpoint", stat.MountPoint)] = stat.Used
		out[labelled("system.filesystem.available_bytes", "mountpoint", stat.MountPoint)] = stat.Available
	}
}

// addLoadMetrics adds load metrics to the collection.
func addLoadMetrics(out map[string]any, stats *LoadStats) {
	out["system.load1"] = stats.Load1
	out["system.load5"] = stats.Load5
	out["system.load15"] = stats.Load15
}

// collectDiskStats measures each path that is a mount point of its own. A
// path on the same filesystem as its parent (/home on a container's overlay
// root, say) would only repeat the parent's numbers under another label.
func collectDiskStats(paths []string) []DiskStats {
	stats := make([]DiskStats, 0, len(paths))

	for _, path := range paths {
		if !labelSafe(path) || !isMountPoint(path) {
			continue
		}

		if s, err := readDiskStats(path); err == nil {
			stats = append(stats, *s)
		}
	}

	return stats
}

// labelled builds a metric key with one label, the form parseMetricKey in
// the Prometheus bridge reads back.
func labelled(name, key, value string) string {
	return name + "{" + key + `="` + value + `"}`
}

// labelSafe reports whether value survives the bridge's key parser, which
// splits on these characters and has no escaping.
func labelSafe(value string) bool {
	return value != "" && !strings.ContainsAny(value, `",={}`)
}

// =============================================================================
// UTILITY METHODS
// =============================================================================

// Enable enables the collector.
func (sc *SystemCollector) Enable() {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	sc.enabled = true
}

// Disable disables the collector. Collect keeps returning the last result.
func (sc *SystemCollector) Disable() {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	sc.enabled = false
}

// IsEnabled returns whether the collector is enabled.
func (sc *SystemCollector) IsEnabled() bool {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	return sc.enabled
}

// SetInterval sets the collection interval.
func (sc *SystemCollector) SetInterval(interval time.Duration) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	sc.config.Interval = interval
}

// GetInterval returns the collection interval.
func (sc *SystemCollector) GetInterval() time.Duration {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	return sc.config.Interval
}

// GetLastCollectionTime returns the last collection time.
func (sc *SystemCollector) GetLastCollectionTime() time.Time {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	return sc.lastCollectionTime
}

// GetMetricsCount returns the number of metrics collected.
func (sc *SystemCollector) GetMetricsCount() int {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	return len(sc.metrics)
}
