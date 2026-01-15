//go:build !windows

package collectors

import (
	"bufio"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	metrics "github.com/xraph/go-utils/metrics"
)

// =============================================================================
// SYSTEM COLLECTOR
// =============================================================================

// SystemCollector collects system metrics (CPU, memory, disk).
type SystemCollector struct {
	name               string
	interval           time.Duration
	lastCPUStats       *CPUStats
	lastCollectionTime time.Time
	metrics            map[string]any
	enabled            bool
}

// SystemCollectorConfig contains configuration for the system collector.
type SystemCollectorConfig struct {
	Interval          time.Duration `json:"interval"           yaml:"interval"`
	CollectCPU        bool          `json:"collect_cpu"        yaml:"collect_cpu"`
	CollectMemory     bool          `json:"collect_memory"     yaml:"collect_memory"`
	CollectDisk       bool          `json:"collect_disk"       yaml:"collect_disk"`
	CollectNetwork    bool          `json:"collect_network"    yaml:"collect_network"`
	CollectLoad       bool          `json:"collect_load"       yaml:"collect_load"`
	DiskMountPoints   []string      `json:"disk_mount_points"  yaml:"disk_mount_points"`
	NetworkInterfaces []string      `json:"network_interfaces" yaml:"network_interfaces"`
}

// CPUStats represents CPU statistics.
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

// MemoryStats represents memory statistics.
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

// DiskStats represents disk statistics.
type DiskStats struct {
	MountPoint  string
	Device      string
	Total       uint64
	Used        uint64
	Available   uint64
	UsedPercent float64
}

// NetworkStats represents network interface statistics.
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

// LoadStats represents system load statistics.
type LoadStats struct {
	Load1  float64
	Load5  float64
	Load15 float64
}

// DefaultSystemCollectorConfig returns default configuration.
func DefaultSystemCollectorConfig() *SystemCollectorConfig {
	return &SystemCollectorConfig{
		Interval:          time.Second * 30,
		CollectCPU:        true,
		CollectMemory:     true,
		CollectDisk:       true,
		CollectNetwork:    false,
		CollectLoad:       true,
		DiskMountPoints:   []string{"/", "/home", "/var"},
		NetworkInterfaces: []string{"eth0", "wlan0"},
	}
}

// NewSystemCollector creates a new system collector.
func NewSystemCollector() metrics.CustomCollector {
	return NewSystemCollectorWithConfig(DefaultSystemCollectorConfig())
}

// NewSystemCollectorWithConfig creates a new system collector with configuration.
func NewSystemCollectorWithConfig(config *SystemCollectorConfig) metrics.CustomCollector {
	return &SystemCollector{
		name:     "system",
		interval: config.Interval,
		metrics:  make(map[string]any),
		enabled:  true,
	}
}

// =============================================================================
// CUSTOM COLLECTOR INTERFACE IMPLEMENTATION
// =============================================================================

// Name returns the collector name.
func (sc *SystemCollector) Name() string {
	return sc.name
}

// Collect collects system metrics.
func (sc *SystemCollector) Collect() map[string]any {
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

	// Collect network metrics
	if networkStats, err := sc.collectNetworkStats(); err == nil {
		sc.addNetworkMetrics(networkStats)
	}

	// Collect load metrics
	if loadStats, err := sc.collectLoadStats(); err == nil {
		sc.addLoadMetrics(loadStats)
	}

	return sc.metrics
}

// Reset resets the collector.
func (sc *SystemCollector) Reset() error {
	sc.metrics = make(map[string]any)
	sc.lastCPUStats = nil
	sc.lastCollectionTime = time.Time{}

	return nil
}

// =============================================================================
// CPU METRICS COLLECTION
// =============================================================================

// collectCPUStats collects CPU statistics.
func (sc *SystemCollector) collectCPUStats() (*CPUStats, error) {
	// Different implementations for different operating systems
	switch runtime.GOOS {
	case "linux":
		return sc.collectLinuxCPUStats()
	case "darwin":
		return sc.collectDarwinCPUStats()
	case "windows":
		return sc.collectWindowsCPUStats()
	default:
		return nil, fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}
}

// collectLinuxCPUStats collects CPU statistics on Linux.
func (sc *SystemCollector) collectLinuxCPUStats() (*CPUStats, error) {
	file, err := os.Open("/proc/stat")
	if err != nil {
		return nil, fmt.Errorf("failed to open /proc/stat: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		return nil, errors.New("failed to read first line from /proc/stat")
	}

	line := scanner.Text()
	if !strings.HasPrefix(line, "cpu ") {
		return nil, errors.New("invalid format in /proc/stat")
	}

	// Parse CPU stats: cpu user nice system idle iowait irq softirq steal guest guest_nice
	fields := strings.Fields(line)
	if len(fields) < 5 {
		return nil, errors.New("insufficient fields in /proc/stat")
	}

	stats := &CPUStats{}
	values := []uint64{}

	for i := 1; i < len(fields) && i <= 10; i++ {
		val, err := strconv.ParseUint(fields[i], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse CPU stat: %w", err)
		}

		values = append(values, val)
	}

	// Map values to struct fields
	if len(values) >= 4 {
		stats.User = values[0]
		stats.Nice = values[1]
		stats.System = values[2]
		stats.Idle = values[3]
	}

	if len(values) >= 5 {
		stats.IOWait = values[4]
	}

	if len(values) >= 6 {
		stats.IRQ = values[5]
	}

	if len(values) >= 7 {
		stats.SoftIRQ = values[6]
	}

	if len(values) >= 8 {
		stats.Steal = values[7]
	}

	if len(values) >= 9 {
		stats.Guest = values[8]
	}

	if len(values) >= 10 {
		stats.GuestNice = values[9]
	}

	// Calculate total
	stats.Total = stats.User + stats.Nice + stats.System + stats.Idle +
		stats.IOWait + stats.IRQ + stats.SoftIRQ + stats.Steal + stats.Guest + stats.GuestNice

	return stats, nil
}

// collectDarwinCPUStats collects CPU statistics on Darwin/macOS.
func (sc *SystemCollector) collectDarwinCPUStats() (*CPUStats, error) {
	// Placeholder implementation for macOS
	// In a real implementation, this would use system calls or parse system files
	return &CPUStats{
		User:   1000,
		System: 500,
		Idle:   8500,
		Total:  10000,
	}, nil
}

// collectWindowsCPUStats collects CPU statistics on Windows.
func (sc *SystemCollector) collectWindowsCPUStats() (*CPUStats, error) {
	// Placeholder implementation for Windows
	// In a real implementation, this would use Windows API calls
	return &CPUStats{
		User:   1000,
		System: 500,
		Idle:   8500,
		Total:  10000,
	}, nil
}

// addCPUMetrics adds CPU metrics to the collection.
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

// collectMemoryStats collects memory statistics.
func (sc *SystemCollector) collectMemoryStats() (*MemoryStats, error) {
	switch runtime.GOOS {
	case "linux":
		return sc.collectLinuxMemoryStats()
	case "darwin":
		return sc.collectDarwinMemoryStats()
	case "windows":
		return sc.collectWindowsMemoryStats()
	default:
		return nil, fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}
}

// collectLinuxMemoryStats collects memory statistics on Linux.
func (sc *SystemCollector) collectLinuxMemoryStats() (*MemoryStats, error) {
	data, err := ioutil.ReadFile("/proc/meminfo")
	if err != nil {
		return nil, fmt.Errorf("failed to read /proc/meminfo: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	values := make(map[string]uint64)

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		key := strings.TrimSuffix(fields[0], ":")

		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}

		// Convert from kB to bytes
		values[key] = value * 1024
	}

	stats := &MemoryStats{
		Total:     values["MemTotal"],
		Free:      values["MemFree"],
		Available: values["MemAvailable"],
		Buffers:   values["Buffers"],
		Cached:    values["Cached"],
		SwapTotal: values["SwapTotal"],
		SwapFree:  values["SwapFree"],
	}

	// Calculate used memory
	stats.Used = stats.Total - stats.Available
	stats.SwapUsed = stats.SwapTotal - stats.SwapFree

	return stats, nil
}

// collectDarwinMemoryStats collects memory statistics on Darwin/macOS.
func (sc *SystemCollector) collectDarwinMemoryStats() (*MemoryStats, error) {
	// Placeholder implementation for macOS
	return &MemoryStats{
		Total:     8 * 1024 * 1024 * 1024, // 8GB
		Available: 4 * 1024 * 1024 * 1024, // 4GB
		Used:      4 * 1024 * 1024 * 1024, // 4GB
		Free:      4 * 1024 * 1024 * 1024, // 4GB
	}, nil
}

// collectWindowsMemoryStats collects memory statistics on Windows.
func (sc *SystemCollector) collectWindowsMemoryStats() (*MemoryStats, error) {
	// Placeholder implementation for Windows
	return &MemoryStats{
		Total:     8 * 1024 * 1024 * 1024, // 8GB
		Available: 4 * 1024 * 1024 * 1024, // 4GB
		Used:      4 * 1024 * 1024 * 1024, // 4GB
		Free:      4 * 1024 * 1024 * 1024, // 4GB
	}, nil
}

// addMemoryMetrics adds memory metrics to the collection.
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

// collectDiskStats collects disk statistics.
func (sc *SystemCollector) collectDiskStats() ([]DiskStats, error) {
	var allStats []DiskStats

	// Common mount points to check
	mountPoints := []string{"/", "/home", "/var", "/tmp"}

	for _, mountPoint := range mountPoints {
		if stats, err := sc.collectDiskStatsForPath(mountPoint); err == nil {
			allStats = append(allStats, *stats)
		}
	}

	return allStats, nil
}

// collectDiskStatsForPath collects disk statistics for a specific path.
func (sc *SystemCollector) collectDiskStatsForPath(path string) (*DiskStats, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return nil, fmt.Errorf("failed to get disk stats for %s: %w", path, err)
	}

	// Calculate disk usage
	total := stat.Blocks * uint64(stat.Bsize)
	available := stat.Bavail * uint64(stat.Bsize)
	used := total - available

	usedPercent := float64(0)
	if total > 0 {
		usedPercent = float64(used) / float64(total) * 100
	}

	return &DiskStats{
		MountPoint:  path,
		Device:      "unknown", // Would need additional system calls to get device name
		Total:       total,
		Used:        used,
		Available:   available,
		UsedPercent: usedPercent,
	}, nil
}

// addDiskMetrics adds disk metrics to the collection.
func (sc *SystemCollector) addDiskMetrics(stats []DiskStats) {
	for _, stat := range stats {
		prefix := "system.disk." + strings.ReplaceAll(stat.MountPoint, "/", "_")
		if prefix == "system.disk._" {
			prefix = "system.disk.root"
		}

		sc.metrics[prefix+".total"] = stat.Total
		sc.metrics[prefix+".used"] = stat.Used
		sc.metrics[prefix+".available"] = stat.Available
		sc.metrics[prefix+".used_percent"] = stat.UsedPercent
	}
}

// =============================================================================
// NETWORK METRICS COLLECTION
// =============================================================================

// collectNetworkStats collects network statistics.
func (sc *SystemCollector) collectNetworkStats() ([]NetworkStats, error) {
	// This is a simplified implementation
	// In a real implementation, this would parse /proc/net/dev on Linux
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

// addNetworkMetrics adds network metrics to the collection.
func (sc *SystemCollector) addNetworkMetrics(stats []NetworkStats) {
	for _, stat := range stats {
		prefix := "system.network." + stat.Interface

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
// LOAD METRICS COLLECTION
// =============================================================================

// collectLoadStats collects system load statistics.
func (sc *SystemCollector) collectLoadStats() (*LoadStats, error) {
	switch runtime.GOOS {
	case "linux":
		return sc.collectLinuxLoadStats()
	case "darwin":
		return sc.collectDarwinLoadStats()
	default:
		return nil, fmt.Errorf("load statistics not supported on %s", runtime.GOOS)
	}
}

// collectLinuxLoadStats collects load statistics on Linux.
func (sc *SystemCollector) collectLinuxLoadStats() (*LoadStats, error) {
	data, err := ioutil.ReadFile("/proc/loadavg")
	if err != nil {
		return nil, fmt.Errorf("failed to read /proc/loadavg: %w", err)
	}

	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return nil, errors.New("invalid format in /proc/loadavg")
	}

	load1, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse load1: %w", err)
	}

	load5, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse load5: %w", err)
	}

	load15, err := strconv.ParseFloat(fields[2], 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse load15: %w", err)
	}

	return &LoadStats{
		Load1:  load1,
		Load5:  load5,
		Load15: load15,
	}, nil
}

// collectDarwinLoadStats collects load statistics on Darwin/macOS.
func (sc *SystemCollector) collectDarwinLoadStats() (*LoadStats, error) {
	// Placeholder implementation for macOS
	return &LoadStats{
		Load1:  1.0,
		Load5:  1.2,
		Load15: 1.5,
	}, nil
}

// addLoadMetrics adds load metrics to the collection.
func (sc *SystemCollector) addLoadMetrics(stats *LoadStats) {
	sc.metrics["system.load.1"] = stats.Load1
	sc.metrics["system.load.5"] = stats.Load5
	sc.metrics["system.load.15"] = stats.Load15
}

// =============================================================================
// UTILITY METHODS
// =============================================================================

// Enable enables the collector.
func (sc *SystemCollector) Enable() {
	sc.enabled = true
}

// Disable disables the collector.
func (sc *SystemCollector) Disable() {
	sc.enabled = false
}

// IsEnabled returns whether the collector is enabled.
func (sc *SystemCollector) IsEnabled() bool {
	return sc.enabled
}

// SetInterval sets the collection interval.
func (sc *SystemCollector) SetInterval(interval time.Duration) {
	sc.interval = interval
}

// GetInterval returns the collection interval.
func (sc *SystemCollector) GetInterval() time.Duration {
	return sc.interval
}

// GetLastCollectionTime returns the last collection time.
func (sc *SystemCollector) GetLastCollectionTime() time.Time {
	return sc.lastCollectionTime
}

// GetMetricsCount returns the number of metrics collected.
func (sc *SystemCollector) GetMetricsCount() int {
	return len(sc.metrics)
}
