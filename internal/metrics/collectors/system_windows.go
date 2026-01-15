//go:build windows

package collectors

import (
	"fmt"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	metrics "github.com/xraph/go-utils/metrics"
)

// Windows specific constants
const (
	maxPath = 260
)

var (
	kernel32              = syscall.NewLazyDLL("kernel32.dll")
	getDiskFreeSpaceExPtr = kernel32.NewProc("GetDiskFreeSpaceExW")
)

// MemoryStatusEx represents Windows memory status structure
type MemoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

// StatFS represents Windows disk statistics (compatible with Unix version)
type StatFS struct {
	Blocks uint64
	Bavail uint64
	Bsize  uint64
	Files  uint64
	Ffree  uint64
}

// =============================================================================
// SYSTEM COLLECTOR
// =============================================================================

// SystemCollector collects system metrics (CPU, memory, disk)
type SystemCollector struct {
	name               string
	interval           time.Duration
	lastCPUStats       *CPUStats
	lastCollectionTime time.Time
	metrics            map[string]interface{}
	enabled            bool
}

// SystemCollectorConfig contains configuration for the system collector
type SystemCollectorConfig struct {
	Interval          time.Duration `yaml:"interval" json:"interval"`
	CollectCPU        bool          `yaml:"collect_cpu" json:"collect_cpu"`
	CollectMemory     bool          `yaml:"collect_memory" json:"collect_memory"`
	CollectDisk       bool          `yaml:"collect_disk" json:"collect_disk"`
	CollectNetwork    bool          `yaml:"collect_network" json:"collect_network"`
	CollectLoad       bool          `yaml:"collect_load" json:"collect_load"`
	DiskMountPoints   []string      `yaml:"disk_mount_points" json:"disk_mount_points"`
	NetworkInterfaces []string      `yaml:"network_interfaces" json:"network_interfaces"`
}

// CPUStats represents CPU statistics
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
	Total     uint64
}

// MemoryStats represents memory statistics
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
}

// DiskStats represents disk statistics
type DiskStats struct {
	MountPoint  string
	Device      string
	Total       uint64
	Used        uint64
	Available   uint64
	UsedPercent float64
}

// NetworkStats represents network interface statistics
type NetworkStats struct {
	Interface   string
	BytesRecv   uint64
	BytesSent   uint64
	PacketsRecv uint64
	PacketsSent uint64
	ErrorsRecv  uint64
	ErrorsSent  uint64
	DropsRecv   uint64
	DropsSent   uint64
}

// LoadStats represents system load statistics
type LoadStats struct {
	Load1  float64
	Load5  float64
	Load15 float64
}

// DefaultSystemCollectorConfig returns default configuration
func DefaultSystemCollectorConfig() *SystemCollectorConfig {
	return &SystemCollectorConfig{
		Interval:        time.Second * 30,
		CollectCPU:      true,
		CollectMemory:   true,
		CollectDisk:     true,
		CollectNetwork:  false,
		CollectLoad:     true,
		DiskMountPoints: []string{"C:\\", "D:\\"},
	}
}

// NewSystemCollector creates a new system collector
func NewSystemCollector() metrics.CustomCollector {
	return NewSystemCollectorWithConfig(DefaultSystemCollectorConfig())
}

// NewSystemCollectorWithConfig creates a new system collector with configuration
func NewSystemCollectorWithConfig(config *SystemCollectorConfig) metrics.CustomCollector {
	return &SystemCollector{
		name:     "system",
		interval: config.Interval,
		metrics:  make(map[string]interface{}),
		enabled:  true,
	}
}

// =============================================================================
// CUSTOM COLLECTOR INTERFACE IMPLEMENTATION
// =============================================================================

// Name returns the collector name
func (sc *SystemCollector) Name() string {
	return sc.name
}

// Collect collects system metrics
func (sc *SystemCollector) Collect() map[string]interface{} {
	if !sc.enabled {
		return sc.metrics
	}

	now := time.Now()

	// Only collect if enough time has passed
	if !sc.lastCollectionTime.IsZero() && now.Sub(sc.lastCollectionTime) < sc.interval {
		return sc.metrics
	}

	sc.lastCollectionTime = now

	// Collect CPU metrics
	if cpuStats, err := sc.collectCPUStats(); err == nil {
		sc.addCPUMetrics(cpuStats)
	}

	// Collect memory metrics
	if memStats, err := sc.collectMemoryStats(); err == nil {
		sc.addMemoryMetrics(memStats)
	}

	// Collect disk metrics
	if diskStats, err := sc.collectDiskStats(); err == nil {
		sc.addDiskMetrics(diskStats)
	}

	// Collect network metrics (not implemented on Windows yet)
	if networkStats, err := sc.collectNetworkStats(); err == nil {
		sc.addNetworkMetrics(networkStats)
	}

	return sc.metrics
}

// Reset resets the collector
func (sc *SystemCollector) Reset() error {
	sc.metrics = make(map[string]interface{})
	sc.lastCPUStats = nil
	sc.lastCollectionTime = time.Time{}
	return nil
}

// =============================================================================
// CPU METRICS COLLECTION
// =============================================================================

// collectCPUStats collects CPU statistics
func (sc *SystemCollector) collectCPUStats() (*CPUStats, error) {
	// Placeholder implementation for Windows
	// Would need Windows Pdh API or similar
	return &CPUStats{
		User:   1000,
		System: 500,
		Idle:   8500,
		Total:  10000,
	}, nil
}

// addCPUMetrics adds CPU metrics to the collection
func (sc *SystemCollector) addCPUMetrics(stats *CPUStats) {
	// Calculate CPU usage percentages
	if sc.lastCPUStats != nil {
		totalDiff := stats.Total - sc.lastCPUStats.Total
		if totalDiff > 0 {
			userDiff := stats.User - sc.lastCPUStats.User
			systemDiff := stats.System - sc.lastCPUStats.System
			idleDiff := stats.Idle - sc.lastCPUStats.Idle
			iowaitDiff := stats.IOWait - sc.lastCPUStats.IOWait

			sc.metrics["system.cpu.user"] = float64(userDiff) / float64(totalDiff) * 100
			sc.metrics["system.cpu.system"] = float64(systemDiff) / float64(totalDiff) * 100
			sc.metrics["system.cpu.idle"] = float64(idleDiff) / float64(totalDiff) * 100
			sc.metrics["system.cpu.iowait"] = float64(iowaitDiff) / float64(totalDiff) * 100
			sc.metrics["system.cpu.usage"] = float64(totalDiff-(idleDiff+iowaitDiff)) / float64(totalDiff) * 100
		}
	}

	// Store raw values
	sc.metrics["system.cpu.user_raw"] = stats.User
	sc.metrics["system.cpu.system_raw"] = stats.System
	sc.metrics["system.cpu.idle_raw"] = stats.Idle
	sc.metrics["system.cpu.iowait_raw"] = stats.IOWait
	sc.metrics["system.cpu.total_raw"] = stats.Total

	sc.lastCPUStats = stats
}

// =============================================================================
// MEMORY METRICS COLLECTION
// =============================================================================

// collectMemoryStats collects memory statistics
func (sc *SystemCollector) collectMemoryStats() (*MemoryStats, error) {
	var memStatus MemoryStatusEx
	memStatus.Length = uint32(unsafe.Sizeof(memStatus))

	ret, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx").Call(uintptr(unsafe.Pointer(&memStatus)))

	if ret == 0 {
		// Fallback to placeholder data
		return &MemoryStats{
			Total:     8 * 1024 * 1024 * 1024, // 8GB
			Available: 4 * 1024 * 1024 * 1024, // 4GB
			Used:      4 * 1024 * 1024 * 1024, // 4GB
			Free:      4 * 1024 * 1024 * 1024, // 4GB
		}, nil
	}

	return &MemoryStats{
		Total:     memStatus.TotalPhys,
		Available: memStatus.AvailPhys,
		Used:      memStatus.TotalPhys - memStatus.AvailPhys,
		Free:      memStatus.AvailPhys,
		SwapTotal: memStatus.TotalPageFile,
		SwapFree:  memStatus.AvailPageFile,
		SwapUsed:  memStatus.TotalPageFile - memStatus.AvailPageFile,
	}, nil
}

// addMemoryMetrics adds memory metrics to the collection
func (sc *SystemCollector) addMemoryMetrics(stats *MemoryStats) {
	sc.metrics["system.memory.total"] = stats.Total
	sc.metrics["system.memory.available"] = stats.Available
	sc.metrics["system.memory.used"] = stats.Used
	sc.metrics["system.memory.free"] = stats.Free
	sc.metrics["system.memory.buffers"] = stats.Buffers
	sc.metrics["system.memory.cached"] = stats.Cached

	// Calculate percentages
	if stats.Total > 0 {
		sc.metrics["system.memory.used_percent"] = float64(stats.Used) / float64(stats.Total) * 100
		sc.metrics["system.memory.available_percent"] = float64(stats.Available) / float64(stats.Total) * 100
	}

	// Swap metrics
	sc.metrics["system.swap.total"] = stats.SwapTotal
	sc.metrics["system.swap.free"] = stats.SwapFree
	sc.metrics["system.swap.used"] = stats.SwapUsed

	if stats.SwapTotal > 0 {
		sc.metrics["system.swap.used_percent"] = float64(stats.SwapUsed) / float64(stats.SwapTotal) * 100
	}
}

// =============================================================================
// DISK METRICS COLLECTION
// =============================================================================

// collectDiskStats collects disk statistics
func (sc *SystemCollector) collectDiskStats() ([]DiskStats, error) {
	var allStats []DiskStats

	// Common mount points to check on Windows
	mountPoints := []string{"C:\\", "D:\\"}

	for _, mountPoint := range mountPoints {
		if stats, err := sc.collectDiskStatsForPath(mountPoint); err == nil {
			allStats = append(allStats, *stats)
		}
	}

	return allStats, nil
}

// collectDiskStatsForPath collects disk statistics for a specific path
func (sc *SystemCollector) collectDiskStatsForPath(path string) (*DiskStats, error) {
	// Convert to absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Get the root drive letter (e.g., "C:\")
	root := filepath.VolumeName(absPath) + "\\"
	if root == "\\" {
		root = absPath[:2] + "\\"
	}

	// Use GetDiskFreeSpaceEx on Windows
	var freeBytes, totalBytes, availBytes int64

	rootW, err := syscall.UTF16PtrFromString(root)
	if err != nil {
		return nil, fmt.Errorf("failed to convert path to UTF16: %w", err)
	}

	ret, _, _ := getDiskFreeSpaceExPtr.Call(
		uintptr(unsafe.Pointer(rootW)),
		uintptr(unsafe.Pointer(&freeBytes)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&availBytes)),
	)

	if ret == 0 {
		return nil, fmt.Errorf("failed to get disk stats for %s", path)
	}

	used := uint64(totalBytes) - uint64(freeBytes)
	usedPercent := float64(0)
	if totalBytes > 0 {
		usedPercent = float64(used) / float64(totalBytes) * 100
	}

	return &DiskStats{
		MountPoint:  path,
		Device:      root,
		Total:       uint64(totalBytes),
		Used:        used,
		Available:   uint64(freeBytes),
		UsedPercent: usedPercent,
	}, nil
}

// addDiskMetrics adds disk metrics to the collection
func (sc *SystemCollector) addDiskMetrics(stats []DiskStats) {
	for _, stat := range stats {
		// Replace backslashes and colons for metric key
		prefix := fmt.Sprintf("system.disk.%s", filepath.ToSlash(stat.MountPoint))
		prefix = replaceAll(prefix, "/", "_")
		prefix = replaceAll(prefix, ":", "_")

		sc.metrics[prefix+".total"] = stat.Total
		sc.metrics[prefix+".used"] = stat.Used
		sc.metrics[prefix+".available"] = stat.Available
		sc.metrics[prefix+".used_percent"] = stat.UsedPercent
	}
}

// =============================================================================
// NETWORK METRICS COLLECTION
// =============================================================================

// collectNetworkStats collects network statistics
func (sc *SystemCollector) collectNetworkStats() ([]NetworkStats, error) {
	// Placeholder for Windows
	return []NetworkStats{
		{
			Interface:   "eth0",
			BytesRecv:   1024000,
			BytesSent:   512000,
			PacketsRecv: 1000,
			PacketsSent: 800,
		},
	}, nil
}

// addNetworkMetrics adds network metrics to the collection
func (sc *SystemCollector) addNetworkMetrics(stats []NetworkStats) {
	for _, stat := range stats {
		prefix := fmt.Sprintf("system.network.%s", stat.Interface)

		sc.metrics[prefix+".bytes_recv"] = stat.BytesRecv
		sc.metrics[prefix+".bytes_sent"] = stat.BytesSent
		sc.metrics[prefix+".packets_recv"] = stat.PacketsRecv
		sc.metrics[prefix+".packets_sent"] = stat.PacketsSent
		sc.metrics[prefix+".errors_recv"] = stat.ErrorsRecv
		sc.metrics[prefix+".errors_sent"] = stat.ErrorsSent
		sc.metrics[prefix+".drops_recv"] = stat.DropsRecv
		sc.metrics[prefix+".drops_sent"] = stat.DropsSent
	}
}

// =============================================================================
// UTILITY METHODS
// =============================================================================

// Enable enables the collector
func (sc *SystemCollector) Enable() {
	sc.enabled = true
}

// Disable disables the collector
func (sc *SystemCollector) Disable() {
	sc.enabled = false
}

// IsEnabled returns whether the collector is enabled
func (sc *SystemCollector) IsEnabled() bool {
	return sc.enabled
}

// SetInterval sets the collection interval
func (sc *SystemCollector) SetInterval(interval time.Duration) {
	sc.interval = interval
}

// GetInterval returns the collection interval
func (sc *SystemCollector) GetInterval() time.Duration {
	return sc.interval
}

// GetLastCollectionTime returns the last collection time
func (sc *SystemCollector) GetLastCollectionTime() time.Time {
	return sc.lastCollectionTime
}

// GetMetricsCount returns the number of metrics collected
func (sc *SystemCollector) GetMetricsCount() int {
	return len(sc.metrics)
}

// =============================================================================
// HELPER FUNCTIONS
// =============================================================================

// replaceAll replaces all occurrences of old in s with new
func replaceAll(s, old, new string) string {
	result := ""
	for i := 0; i < len(s); i++ {
		if i+len(old) <= len(s) && s[i:i+len(old)] == old {
			result += new
			i += len(old) - 1
		} else {
			result += string(s[i])
		}
	}
	return result
}
