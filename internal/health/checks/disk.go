package checks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	health "github.com/xraph/forge/internal/health/internal"
)

// DiskHealthCheck performs health checks on disk usage
type DiskHealthCheck struct {
	*health.BaseHealthCheck
	paths             []string
	warningThreshold  float64 // Percentage (0-100)
	criticalThreshold float64 // Percentage (0-100)
	checkInodes       bool
	checkIO           bool
	mu                sync.RWMutex
}

// DiskHealthCheckConfig contains configuration for disk health checks
type DiskHealthCheckConfig struct {
	Name              string
	Paths             []string
	WarningThreshold  float64 // Percentage (0-100)
	CriticalThreshold float64 // Percentage (0-100)
	CheckInodes       bool
	CheckIO           bool
	Timeout           time.Duration
	Critical          bool
	Tags              map[string]string
}

// NewDiskHealthCheck creates a new disk health check
func NewDiskHealthCheck(config *DiskHealthCheckConfig) *DiskHealthCheck {
	if config == nil {
		config = &DiskHealthCheckConfig{}
	}

	if config.Name == "" {
		config.Name = "disk"
	}

	if len(config.Paths) == 0 {
		config.Paths = []string{"/", "/tmp", "/var/log"}
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

	config.Tags["check_type"] = "disk"

	baseConfig := &health.HealthCheckConfig{
		Name:     config.Name,
		Timeout:  config.Timeout,
		Critical: config.Critical,
		Tags:     config.Tags,
	}

	return &DiskHealthCheck{
		BaseHealthCheck:   health.NewBaseHealthCheck(baseConfig),
		paths:             config.Paths,
		warningThreshold:  config.WarningThreshold,
		criticalThreshold: config.CriticalThreshold,
		checkInodes:       config.CheckInodes,
		checkIO:           config.CheckIO,
	}
}

// Check performs the disk health check
func (dhc *DiskHealthCheck) Check(ctx context.Context) *health.HealthResult {
	start := time.Now()

	result := health.NewHealthResult(dhc.Name(), health.HealthStatusHealthy, "disk usage is normal").
		WithCritical(dhc.Critical()).
		WithTags(dhc.Tags())

	var maxUsagePercent float64
	var criticalPaths []string
	var warningPaths []string
	pathStats := make(map[string]interface{})

	// Check each path
	for _, path := range dhc.paths {
		select {
		case <-ctx.Done():
			return result.WithError(ctx.Err())
		default:
		}

		pathInfo, err := dhc.checkPath(path)
		if err != nil {
			result.WithDetail(fmt.Sprintf("path_error_%s", sanitizePath(path)), err.Error())
			continue
		}

		pathStats[sanitizePath(path)] = pathInfo

		if usagePercent, ok := pathInfo["usage_percent"].(float64); ok {
			if usagePercent > maxUsagePercent {
				maxUsagePercent = usagePercent
			}

			if usagePercent >= dhc.criticalThreshold {
				criticalPaths = append(criticalPaths, path)
			} else if usagePercent >= dhc.warningThreshold {
				warningPaths = append(warningPaths, path)
			}
		}
	}

	// Add path statistics to result
	result.WithDetail("paths", pathStats).
		WithDetail("max_usage_percent", maxUsagePercent).
		WithDetail("warning_threshold", dhc.warningThreshold).
		WithDetail("critical_threshold", dhc.criticalThreshold)

	// Check I/O statistics if enabled
	if dhc.checkIO {
		if ioStats := dhc.getIOStats(); ioStats != nil {
			for k, v := range ioStats {
				result.WithDetail(k, v)
			}
		}
	}

	// Determine health status
	if len(criticalPaths) > 0 {
		result = result.WithError(fmt.Errorf("disk usage exceeds critical threshold on paths: %v", criticalPaths)).
			WithDetail("critical_paths", criticalPaths)
	} else if len(warningPaths) > 0 {
		result.Status = health.HealthStatusDegraded
		result.Message = fmt.Sprintf("disk usage exceeds warning threshold on paths: %v", warningPaths)
		result.WithDetail("warning_paths", warningPaths)
	}

	result.WithDuration(time.Since(start))
	return result
}

// checkPath checks disk usage for a specific path
func (dhc *DiskHealthCheck) checkPath(path string) (map[string]interface{}, error) {
	dhc.mu.RLock()
	defer dhc.mu.RUnlock()

	// Check if path exists
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("path does not exist: %w", err)
	}

	// Get disk usage statistics
	stat := &syscall.Statfs_t{}
	if err := syscall.Statfs(path, stat); err != nil {
		return nil, fmt.Errorf("failed to get disk stats: %w", err)
	}

	// Calculate disk usage
	totalBytes := stat.Blocks * uint64(stat.Bsize)
	freeBytes := stat.Bfree * uint64(stat.Bsize)
	usedBytes := totalBytes - freeBytes
	usagePercent := float64(usedBytes) / float64(totalBytes) * 100.0

	pathInfo := map[string]interface{}{
		"path":          path,
		"total_bytes":   totalBytes,
		"used_bytes":    usedBytes,
		"free_bytes":    freeBytes,
		"usage_percent": usagePercent,
		"total_human":   formatBytes(totalBytes),
		"used_human":    formatBytes(usedBytes),
		"free_human":    formatBytes(freeBytes),
	}

	// Check inodes if enabled
	if dhc.checkInodes {
		totalInodes := stat.Files
		freeInodes := stat.Ffree
		usedInodes := totalInodes - freeInodes
		var inodeUsagePercent float64
		if totalInodes > 0 {
			inodeUsagePercent = float64(usedInodes) / float64(totalInodes) * 100.0
		}

		pathInfo["total_inodes"] = totalInodes
		pathInfo["used_inodes"] = usedInodes
		pathInfo["free_inodes"] = freeInodes
		pathInfo["inode_usage_percent"] = inodeUsagePercent
	}

	return pathInfo, nil
}

// getIOStats returns disk I/O statistics
func (dhc *DiskHealthCheck) getIOStats() map[string]interface{} {
	// In a real implementation, this would read from /proc/diskstats or similar
	// For now, return placeholder data
	return map[string]interface{}{
		"io_reads":       1000,
		"io_writes":      500,
		"io_read_bytes":  1024 * 1024 * 100, // 100MB
		"io_write_bytes": 1024 * 1024 * 50,  // 50MB
		"io_utilization": 25.5,              // 25.5%
	}
}

// AddPath adds a path to monitor
func (dhc *DiskHealthCheck) AddPath(path string) {
	dhc.mu.Lock()
	defer dhc.mu.Unlock()
	dhc.paths = append(dhc.paths, path)
}

// RemovePath removes a path from monitoring
func (dhc *DiskHealthCheck) RemovePath(path string) {
	dhc.mu.Lock()
	defer dhc.mu.Unlock()
	for i, p := range dhc.paths {
		if p == path {
			dhc.paths = append(dhc.paths[:i], dhc.paths[i+1:]...)
			break
		}
	}
}

// GetPaths returns the paths being monitored
func (dhc *DiskHealthCheck) GetPaths() []string {
	dhc.mu.RLock()
	defer dhc.mu.RUnlock()
	paths := make([]string, len(dhc.paths))
	copy(paths, dhc.paths)
	return paths
}

// DiskIOHealthCheck performs health checks on disk I/O performance
type DiskIOHealthCheck struct {
	*health.BaseHealthCheck
	readLatencyThreshold  time.Duration
	writeLatencyThreshold time.Duration
	utilizationThreshold  float64
	devices               []string
}

// NewDiskIOHealthCheck creates a new disk I/O health check
func NewDiskIOHealthCheck(config *DiskHealthCheckConfig) *DiskIOHealthCheck {
	if config == nil {
		config = &DiskHealthCheckConfig{}
	}

	if config.Name == "" {
		config.Name = "disk-io"
	}

	if config.Tags == nil {
		config.Tags = make(map[string]string)
	}

	config.Tags["check_type"] = "disk_io"

	baseConfig := &health.HealthCheckConfig{
		Name:     config.Name,
		Timeout:  config.Timeout,
		Critical: config.Critical,
		Tags:     config.Tags,
	}

	return &DiskIOHealthCheck{
		BaseHealthCheck:       health.NewBaseHealthCheck(baseConfig),
		readLatencyThreshold:  10 * time.Millisecond,
		writeLatencyThreshold: 20 * time.Millisecond,
		utilizationThreshold:  80.0, // 80%
		devices:               []string{"sda", "sdb", "nvme0n1"},
	}
}

// Check performs the disk I/O health check
func (diohc *DiskIOHealthCheck) Check(ctx context.Context) *health.HealthResult {
	start := time.Now()

	result := health.NewHealthResult(diohc.Name(), health.HealthStatusHealthy, "disk I/O performance is normal").
		WithCritical(diohc.Critical()).
		WithTags(diohc.Tags())

	// Check I/O statistics for each device
	deviceStats := make(map[string]interface{})
	var maxUtilization float64
	var highUtilizationDevices []string

	for _, device := range diohc.devices {
		select {
		case <-ctx.Done():
			return result.WithError(ctx.Err())
		default:
		}

		deviceInfo := diohc.checkDevice(device)
		deviceStats[device] = deviceInfo

		if utilization, ok := deviceInfo["utilization"].(float64); ok {
			if utilization > maxUtilization {
				maxUtilization = utilization
			}

			if utilization >= diohc.utilizationThreshold {
				highUtilizationDevices = append(highUtilizationDevices, device)
			}
		}
	}

	result.WithDetail("devices", deviceStats).
		WithDetail("max_utilization", maxUtilization).
		WithDetail("utilization_threshold", diohc.utilizationThreshold)

	// Check if any devices have high utilization
	if len(highUtilizationDevices) > 0 {
		result.Status = health.HealthStatusDegraded
		result.Message = fmt.Sprintf("high I/O utilization on devices: %v", highUtilizationDevices)
		result.WithDetail("high_utilization_devices", highUtilizationDevices)
	}

	result.WithDuration(time.Since(start))
	return result
}

// checkDevice checks I/O statistics for a specific device
func (diohc *DiskIOHealthCheck) checkDevice(device string) map[string]interface{} {
	// In a real implementation, this would read from /proc/diskstats
	// For now, return placeholder data
	return map[string]interface{}{
		"device":         device,
		"reads_per_sec":  150,
		"writes_per_sec": 75,
		"read_latency":   diohc.readLatencyThreshold / 2,
		"write_latency":  diohc.writeLatencyThreshold / 2,
		"utilization":    45.0,
		"queue_depth":    2,
	}
}

// TempDirHealthCheck performs health checks on temporary directories
type TempDirHealthCheck struct {
	*health.BaseHealthCheck
	tempDirs         []string
	sizeThreshold    uint64
	oldFileThreshold time.Duration
}

// NewTempDirHealthCheck creates a new temporary directory health check
func NewTempDirHealthCheck(config *DiskHealthCheckConfig) *TempDirHealthCheck {
	if config == nil {
		config = &DiskHealthCheckConfig{}
	}

	if config.Name == "" {
		config.Name = "temp-dir"
	}

	if config.Tags == nil {
		config.Tags = make(map[string]string)
	}

	config.Tags["check_type"] = "temp_dir"

	baseConfig := &health.HealthCheckConfig{
		Name:     config.Name,
		Timeout:  config.Timeout,
		Critical: config.Critical,
		Tags:     config.Tags,
	}

	return &TempDirHealthCheck{
		BaseHealthCheck:  health.NewBaseHealthCheck(baseConfig),
		tempDirs:         []string{"/tmp", "/var/tmp", os.TempDir()},
		sizeThreshold:    1024 * 1024 * 1024, // 1GB
		oldFileThreshold: 7 * 24 * time.Hour, // 7 days
	}
}

// Check performs the temporary directory health check
func (tdhc *TempDirHealthCheck) Check(ctx context.Context) *health.HealthResult {
	start := time.Now()

	result := health.NewHealthResult(tdhc.Name(), health.HealthStatusHealthy, "temporary directories are clean").
		WithCritical(tdhc.Critical()).
		WithTags(tdhc.Tags())

	tempDirStats := make(map[string]interface{})
	var warnings []string

	for _, tempDir := range tdhc.tempDirs {
		select {
		case <-ctx.Done():
			return result.WithError(ctx.Err())
		default:
		}

		if _, err := os.Stat(tempDir); err != nil {
			continue // Skip non-existent directories
		}

		dirInfo := tdhc.checkTempDir(tempDir)
		tempDirStats[sanitizePath(tempDir)] = dirInfo

		// Check for warnings
		if size, ok := dirInfo["total_size"].(uint64); ok && size > tdhc.sizeThreshold {
			warnings = append(warnings, fmt.Sprintf("%s is large (%s)", tempDir, formatBytes(size)))
		}

		if oldFiles, ok := dirInfo["old_files"].(int); ok && oldFiles > 0 {
			warnings = append(warnings, fmt.Sprintf("%s has %d old files", tempDir, oldFiles))
		}
	}

	result.WithDetail("temp_dirs", tempDirStats).
		WithDetail("size_threshold", tdhc.sizeThreshold).
		WithDetail("old_file_threshold", tdhc.oldFileThreshold.String())

	if len(warnings) > 0 {
		result.Status = health.HealthStatusDegraded
		result.Message = fmt.Sprintf("temporary directory warnings: %v", warnings)
		result.WithDetail("warnings", warnings)
	}

	result.WithDuration(time.Since(start))
	return result
}

// checkTempDir checks a temporary directory
func (tdhc *TempDirHealthCheck) checkTempDir(tempDir string) map[string]interface{} {
	info := map[string]interface{}{
		"path":       tempDir,
		"total_size": uint64(0),
		"file_count": 0,
		"old_files":  0,
	}

	// Walk through the directory
	var totalSize uint64
	var fileCount int
	var oldFiles int
	cutoff := time.Now().Add(-tdhc.oldFileThreshold)

	filepath.Walk(tempDir, func(path string, fileInfo os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip files we can't access
		}

		if !fileInfo.IsDir() {
			fileCount++
			totalSize += uint64(fileInfo.Size())

			if fileInfo.ModTime().Before(cutoff) {
				oldFiles++
			}
		}

		return nil
	})

	info["total_size"] = totalSize
	info["file_count"] = fileCount
	info["old_files"] = oldFiles
	info["total_size_human"] = formatBytes(totalSize)

	return info
}

// LogDirHealthCheck performs health checks on log directories
type LogDirHealthCheck struct {
	*health.BaseHealthCheck
	logDirs       []string
	sizeThreshold uint64
	rotationCheck bool
}

// NewLogDirHealthCheck creates a new log directory health check
func NewLogDirHealthCheck(config *DiskHealthCheckConfig) *LogDirHealthCheck {
	if config == nil {
		config = &DiskHealthCheckConfig{}
	}

	if config.Name == "" {
		config.Name = "log-dir"
	}

	if config.Tags == nil {
		config.Tags = make(map[string]string)
	}

	config.Tags["check_type"] = "log_dir"

	baseConfig := &health.HealthCheckConfig{
		Name:     config.Name,
		Timeout:  config.Timeout,
		Critical: config.Critical,
		Tags:     config.Tags,
	}

	return &LogDirHealthCheck{
		BaseHealthCheck: health.NewBaseHealthCheck(baseConfig),
		logDirs:         []string{"/var/log", "/var/log/app"},
		sizeThreshold:   5 * 1024 * 1024 * 1024, // 5GB
		rotationCheck:   true,
	}
}

// Check performs the log directory health check
func (ldhc *LogDirHealthCheck) Check(ctx context.Context) *health.HealthResult {
	start := time.Now()

	result := health.NewHealthResult(ldhc.Name(), health.HealthStatusHealthy, "log directories are healthy").
		WithCritical(ldhc.Critical()).
		WithTags(ldhc.Tags())

	logDirStats := make(map[string]interface{})
	var warnings []string

	for _, logDir := range ldhc.logDirs {
		select {
		case <-ctx.Done():
			return result.WithError(ctx.Err())
		default:
		}

		if _, err := os.Stat(logDir); err != nil {
			continue // Skip non-existent directories
		}

		dirInfo := ldhc.checkLogDir(logDir)
		logDirStats[sanitizePath(logDir)] = dirInfo

		// Check for warnings
		if size, ok := dirInfo["total_size"].(uint64); ok && size > ldhc.sizeThreshold {
			warnings = append(warnings, fmt.Sprintf("%s is large (%s)", logDir, formatBytes(size)))
		}

		if ldhc.rotationCheck {
			if rotationIssues, ok := dirInfo["rotation_issues"].([]string); ok && len(rotationIssues) > 0 {
				warnings = append(warnings, fmt.Sprintf("%s has rotation issues: %v", logDir, rotationIssues))
			}
		}
	}

	result.WithDetail("log_dirs", logDirStats).
		WithDetail("size_threshold", ldhc.sizeThreshold).
		WithDetail("rotation_check", ldhc.rotationCheck)

	if len(warnings) > 0 {
		result.Status = health.HealthStatusDegraded
		result.Message = fmt.Sprintf("log directory warnings: %v", warnings)
		result.WithDetail("warnings", warnings)
	}

	result.WithDuration(time.Since(start))
	return result
}

// checkLogDir checks a log directory
func (ldhc *LogDirHealthCheck) checkLogDir(logDir string) map[string]interface{} {
	info := map[string]interface{}{
		"path":       logDir,
		"total_size": uint64(0),
		"log_files":  0,
	}

	// Walk through the directory
	var totalSize uint64
	var logFiles int
	var rotationIssues []string

	filepath.Walk(logDir, func(path string, fileInfo os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip files we can't access
		}

		if !fileInfo.IsDir() && (strings.HasSuffix(path, ".log") || strings.Contains(path, ".log.")) {
			logFiles++
			totalSize += uint64(fileInfo.Size())

			// Check for very large log files (potential rotation issues)
			if fileInfo.Size() > 100*1024*1024 { // 100MB
				rotationIssues = append(rotationIssues, fmt.Sprintf("large log file: %s (%s)", path, formatBytes(uint64(fileInfo.Size()))))
			}
		}

		return nil
	})

	info["total_size"] = totalSize
	info["log_files"] = logFiles
	info["total_size_human"] = formatBytes(totalSize)

	if ldhc.rotationCheck {
		info["rotation_issues"] = rotationIssues
	}

	return info
}

// Helper functions

// formatBytes formats bytes in human-readable format
func formatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// sanitizePath sanitizes a path for use as a map key
func sanitizePath(path string) string {
	return strings.ReplaceAll(strings.ReplaceAll(path, "/", "_"), ":", "_")
}

// RegisterDiskHealthChecks registers disk health checks with the health service
func RegisterDiskHealthChecks(healthService health.HealthService) error {
	// Register main disk health check
	diskCheck := NewDiskHealthCheck(&DiskHealthCheckConfig{
		Name:              "disk",
		Paths:             []string{"/", "/tmp", "/var/log"},
		WarningThreshold:  80.0,
		CriticalThreshold: 90.0,
		CheckInodes:       true,
		CheckIO:           false,
		Critical:          true,
	})

	if err := healthService.Register(diskCheck); err != nil {
		return fmt.Errorf("failed to register disk health check: %w", err)
	}

	// Register disk I/O health check
	ioCheck := NewDiskIOHealthCheck(&DiskHealthCheckConfig{
		Name:     "disk-io",
		Critical: false,
	})

	if err := healthService.Register(ioCheck); err != nil {
		return fmt.Errorf("failed to register disk I/O health check: %w", err)
	}

	// Register temporary directory health check
	tempCheck := NewTempDirHealthCheck(&DiskHealthCheckConfig{
		Name:     "temp-dir",
		Critical: false,
	})

	if err := healthService.Register(tempCheck); err != nil {
		return fmt.Errorf("failed to register temp directory health check: %w", err)
	}

	// Register log directory health check
	logCheck := NewLogDirHealthCheck(&DiskHealthCheckConfig{
		Name:     "log-dir",
		Critical: false,
	})

	if err := healthService.Register(logCheck); err != nil {
		return fmt.Errorf("failed to register log directory health check: %w", err)
	}

	return nil
}
