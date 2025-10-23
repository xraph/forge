package checks

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	health "github.com/xraph/forge/internal/health/internal"
)

// CPUHealthCheck performs health checks on CPU usage
type CPUHealthCheck struct {
	*health.BaseHealthCheck
	warningThreshold  float64 // Percentage (0-100)
	criticalThreshold float64 // Percentage (0-100)
	loadAvgThreshold  float64 // Load average threshold
	checkLoadAvg      bool
	checkCPUCount     bool
	samples           int
	sampleInterval    time.Duration
	mu                sync.RWMutex
}

// CPUHealthCheckConfig contains configuration for CPU health checks
type CPUHealthCheckConfig struct {
	Name              string
	WarningThreshold  float64 // Percentage (0-100)
	CriticalThreshold float64 // Percentage (0-100)
	LoadAvgThreshold  float64 // Load average threshold
	CheckLoadAvg      bool
	CheckCPUCount     bool
	Samples           int
	SampleInterval    time.Duration
	Timeout           time.Duration
	Critical          bool
	Tags              map[string]string
}

// NewCPUHealthCheck creates a new CPU health check
func NewCPUHealthCheck(config *CPUHealthCheckConfig) *CPUHealthCheck {
	if config == nil {
		config = &CPUHealthCheckConfig{}
	}

	if config.Name == "" {
		config.Name = "cpu"
	}

	if config.WarningThreshold == 0 {
		config.WarningThreshold = 80.0 // 80%
	}

	if config.CriticalThreshold == 0 {
		config.CriticalThreshold = 95.0 // 95%
	}

	if config.LoadAvgThreshold == 0 {
		config.LoadAvgThreshold = float64(runtime.NumCPU()) * 0.8 // 80% of CPU count
	}

	if config.Samples == 0 {
		config.Samples = 3
	}

	if config.SampleInterval == 0 {
		config.SampleInterval = 1 * time.Second
	}

	if config.Timeout == 0 {
		config.Timeout = 10 * time.Second
	}

	if config.Tags == nil {
		config.Tags = make(map[string]string)
	}

	config.Tags["check_type"] = "cpu"

	baseConfig := &health.HealthCheckConfig{
		Name:     config.Name,
		Timeout:  config.Timeout,
		Critical: config.Critical,
		Tags:     config.Tags,
	}

	return &CPUHealthCheck{
		BaseHealthCheck:   health.NewBaseHealthCheck(baseConfig),
		warningThreshold:  config.WarningThreshold,
		criticalThreshold: config.CriticalThreshold,
		loadAvgThreshold:  config.LoadAvgThreshold,
		checkLoadAvg:      config.CheckLoadAvg,
		checkCPUCount:     config.CheckCPUCount,
		samples:           config.Samples,
		sampleInterval:    config.SampleInterval,
	}
}

// Check performs the CPU health check
func (chc *CPUHealthCheck) Check(ctx context.Context) *health.HealthResult {
	start := time.Now()

	result := health.NewHealthResult(chc.Name(), health.HealthStatusHealthy, "CPU usage is normal").
		WithCritical(chc.Critical()).
		WithTags(chc.Tags())

	// Get CPU usage
	cpuUsage, err := chc.getCPUUsage(ctx)
	if err != nil {
		return result.WithError(err).WithDuration(time.Since(start))
	}

	result.WithDetail("cpu_usage_percent", cpuUsage).
		WithDetail("warning_threshold", chc.warningThreshold).
		WithDetail("critical_threshold", chc.criticalThreshold).
		WithDetail("samples", chc.samples).
		WithDetail("sample_interval", chc.sampleInterval.String())

	// Check load average if enabled
	if chc.checkLoadAvg {
		if loadStats := chc.getLoadAverage(); loadStats != nil {
			for k, v := range loadStats {
				result.WithDetail(k, v)
			}

			// Check load average threshold
			if load1, ok := loadStats["load_1m"].(float64); ok {
				if load1 > chc.loadAvgThreshold {
					result.Status = health.HealthStatusDegraded
					result.Message = fmt.Sprintf("load average %.2f exceeds threshold %.2f", load1, chc.loadAvgThreshold)
					result.WithDetail("load_threshold_exceeded", true)
				}
			}
		}
	}

	// Check CPU count if enabled
	if chc.checkCPUCount {
		cpuInfo := chc.getCPUInfo()
		for k, v := range cpuInfo {
			result.WithDetail(k, v)
		}
	}

	// Determine health status based on CPU usage
	if cpuUsage >= chc.criticalThreshold {
		result = result.WithError(fmt.Errorf("CPU usage %.2f%% exceeds critical threshold %.2f%%", cpuUsage, chc.criticalThreshold)).
			WithDetail("threshold_exceeded", "critical")
	} else if cpuUsage >= chc.warningThreshold {
		result.Status = health.HealthStatusDegraded
		result.Message = fmt.Sprintf("CPU usage %.2f%% exceeds warning threshold %.2f%%", cpuUsage, chc.warningThreshold)
		result.WithDetail("threshold_exceeded", "warning")
	}

	result.WithDuration(time.Since(start))
	return result
}

// getCPUUsage calculates CPU usage percentage
func (chc *CPUHealthCheck) getCPUUsage(ctx context.Context) (float64, error) {
	chc.mu.RLock()
	defer chc.mu.RUnlock()

	// This is a simplified CPU usage calculation
	// In a real implementation, you would read from /proc/stat or use a system library

	var totalUsage float64
	samples := chc.samples
	if samples <= 0 {
		samples = 1
	}

	for i := 0; i < samples; i++ {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
		}

		// Sample CPU usage
		usage := chc.sampleCPUUsage()
		totalUsage += usage

		// Wait for next sample (except for the last one)
		if i < samples-1 {
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-time.After(chc.sampleInterval):
			}
		}
	}

	return totalUsage / float64(samples), nil
}

// sampleCPUUsage samples CPU usage for a single measurement
func (chc *CPUHealthCheck) sampleCPUUsage() float64 {
	// This is a placeholder implementation
	// In a real implementation, you would read from /proc/stat
	// For now, we'll return a simulated value based on goroutine count

	goroutines := runtime.NumGoroutine()
	cpuCount := runtime.NumCPU()

	// Simulate CPU usage based on goroutine count
	baseUsage := float64(goroutines) / float64(cpuCount*10) * 100
	if baseUsage > 100 {
		baseUsage = 100
	}

	return baseUsage
}

// getLoadAverage gets system load average
func (chc *CPUHealthCheck) getLoadAverage() map[string]interface{} {
	// Try to read from /proc/loadavg on Linux
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) >= 3 {
			load1, _ := strconv.ParseFloat(fields[0], 64)
			load5, _ := strconv.ParseFloat(fields[1], 64)
			load15, _ := strconv.ParseFloat(fields[2], 64)

			return map[string]interface{}{
				"load_1m":        load1,
				"load_5m":        load5,
				"load_15m":       load15,
				"load_threshold": chc.loadAvgThreshold,
			}
		}
	}

	// Fallback: simulate load average
	return map[string]interface{}{
		"load_1m":        0.5,
		"load_5m":        0.3,
		"load_15m":       0.2,
		"load_threshold": chc.loadAvgThreshold,
		"simulated":      true,
	}
}

// getCPUInfo gets CPU information
func (chc *CPUHealthCheck) getCPUInfo() map[string]interface{} {
	return map[string]interface{}{
		"cpu_count":    runtime.NumCPU(),
		"gomaxprocs":   runtime.GOMAXPROCS(0),
		"architecture": runtime.GOARCH,
		"os":           runtime.GOOS,
	}
}

// CPULoadHealthCheck performs health checks on CPU load
type CPULoadHealthCheck struct {
	*health.BaseHealthCheck
	load1Threshold  float64
	load5Threshold  float64
	load15Threshold float64
	cpuCount        int
}

// NewCPULoadHealthCheck creates a new CPU load health check
func NewCPULoadHealthCheck(config *CPUHealthCheckConfig) *CPULoadHealthCheck {
	if config == nil {
		config = &CPUHealthCheckConfig{}
	}

	if config.Name == "" {
		config.Name = "cpu-load"
	}

	if config.Tags == nil {
		config.Tags = make(map[string]string)
	}

	config.Tags["check_type"] = "cpu_load"

	baseConfig := &health.HealthCheckConfig{
		Name:     config.Name,
		Timeout:  config.Timeout,
		Critical: config.Critical,
		Tags:     config.Tags,
	}

	cpuCount := runtime.NumCPU()

	return &CPULoadHealthCheck{
		BaseHealthCheck: health.NewBaseHealthCheck(baseConfig),
		load1Threshold:  float64(cpuCount) * 0.8, // 80% of CPU count
		load5Threshold:  float64(cpuCount) * 0.7, // 70% of CPU count
		load15Threshold: float64(cpuCount) * 0.6, // 60% of CPU count
		cpuCount:        cpuCount,
	}
}

// Check performs the CPU load health check
func (clhc *CPULoadHealthCheck) Check(ctx context.Context) *health.HealthResult {
	start := time.Now()

	result := health.NewHealthResult(clhc.Name(), health.HealthStatusHealthy, "CPU load is normal").
		WithCritical(clhc.Critical()).
		WithTags(clhc.Tags())

	// Get load average
	loadStats := clhc.getLoadAverage()
	for k, v := range loadStats {
		result.WithDetail(k, v)
	}

	result.WithDetail("cpu_count", clhc.cpuCount).
		WithDetail("load1_threshold", clhc.load1Threshold).
		WithDetail("load5_threshold", clhc.load5Threshold).
		WithDetail("load15_threshold", clhc.load15Threshold)

	// Check thresholds
	var warnings []string
	var critical bool

	if load1, ok := loadStats["load_1m"].(float64); ok {
		if load1 > clhc.load1Threshold {
			if load1 > clhc.load1Threshold*1.5 {
				critical = true
			}
			warnings = append(warnings, fmt.Sprintf("1m load %.2f exceeds threshold %.2f", load1, clhc.load1Threshold))
		}
	}

	if load5, ok := loadStats["load_5m"].(float64); ok {
		if load5 > clhc.load5Threshold {
			warnings = append(warnings, fmt.Sprintf("5m load %.2f exceeds threshold %.2f", load5, clhc.load5Threshold))
		}
	}

	if load15, ok := loadStats["load_15m"].(float64); ok {
		if load15 > clhc.load15Threshold {
			warnings = append(warnings, fmt.Sprintf("15m load %.2f exceeds threshold %.2f", load15, clhc.load15Threshold))
		}
	}

	if critical {
		result = result.WithError(fmt.Errorf("CPU load is critically high")).
			WithDetail("warnings", warnings)
	} else if len(warnings) > 0 {
		result.Status = health.HealthStatusDegraded
		result.Message = fmt.Sprintf("CPU load warnings: %v", warnings)
		result.WithDetail("warnings", warnings)
	}

	result.WithDuration(time.Since(start))
	return result
}

// getLoadAverage gets system load average
func (clhc *CPULoadHealthCheck) getLoadAverage() map[string]interface{} {
	// Try to read from /proc/loadavg on Linux
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) >= 3 {
			load1, _ := strconv.ParseFloat(fields[0], 64)
			load5, _ := strconv.ParseFloat(fields[1], 64)
			load15, _ := strconv.ParseFloat(fields[2], 64)

			return map[string]interface{}{
				"load_1m":  load1,
				"load_5m":  load5,
				"load_15m": load15,
			}
		}
	}

	// Fallback: simulate load average
	return map[string]interface{}{
		"load_1m":   0.5,
		"load_5m":   0.3,
		"load_15m":  0.2,
		"simulated": true,
	}
}

// CPUContextSwitchHealthCheck performs health checks on CPU context switches
type CPUContextSwitchHealthCheck struct {
	*health.BaseHealthCheck
	contextSwitchThreshold uint64
	interruptThreshold     uint64
}

// NewCPUContextSwitchHealthCheck creates a new CPU context switch health check
func NewCPUContextSwitchHealthCheck(config *CPUHealthCheckConfig) *CPUContextSwitchHealthCheck {
	if config == nil {
		config = &CPUHealthCheckConfig{}
	}

	if config.Name == "" {
		config.Name = "cpu-context-switch"
	}

	if config.Tags == nil {
		config.Tags = make(map[string]string)
	}

	config.Tags["check_type"] = "cpu_context_switch"

	baseConfig := &health.HealthCheckConfig{
		Name:     config.Name,
		Timeout:  config.Timeout,
		Critical: config.Critical,
		Tags:     config.Tags,
	}

	return &CPUContextSwitchHealthCheck{
		BaseHealthCheck:        health.NewBaseHealthCheck(baseConfig),
		contextSwitchThreshold: 100000, // 100k context switches per second
		interruptThreshold:     50000,  // 50k interrupts per second
	}
}

// Check performs the CPU context switch health check
func (ccshc *CPUContextSwitchHealthCheck) Check(ctx context.Context) *health.HealthResult {
	start := time.Now()

	result := health.NewHealthResult(ccshc.Name(), health.HealthStatusHealthy, "CPU context switches are normal").
		WithCritical(ccshc.Critical()).
		WithTags(ccshc.Tags())

	// Get context switch and interrupt statistics
	stats := ccshc.getContextSwitchStats()
	for k, v := range stats {
		result.WithDetail(k, v)
	}

	result.WithDetail("context_switch_threshold", ccshc.contextSwitchThreshold).
		WithDetail("interrupt_threshold", ccshc.interruptThreshold)

	// Check thresholds
	var warnings []string

	if ctxt, ok := stats["context_switches_per_sec"].(uint64); ok {
		if ctxt > ccshc.contextSwitchThreshold {
			warnings = append(warnings, fmt.Sprintf("context switches %d/sec exceeds threshold %d/sec", ctxt, ccshc.contextSwitchThreshold))
		}
	}

	if intr, ok := stats["interrupts_per_sec"].(uint64); ok {
		if intr > ccshc.interruptThreshold {
			warnings = append(warnings, fmt.Sprintf("interrupts %d/sec exceeds threshold %d/sec", intr, ccshc.interruptThreshold))
		}
	}

	if len(warnings) > 0 {
		result.Status = health.HealthStatusDegraded
		result.Message = fmt.Sprintf("CPU context switch warnings: %v", warnings)
		result.WithDetail("warnings", warnings)
	}

	result.WithDuration(time.Since(start))
	return result
}

// getContextSwitchStats gets context switch and interrupt statistics
func (ccshc *CPUContextSwitchHealthCheck) getContextSwitchStats() map[string]interface{} {
	// Try to read from /proc/stat on Linux
	if data, err := os.ReadFile("/proc/stat"); err == nil {
		lines := strings.Split(string(data), "\n")
		stats := make(map[string]interface{})

		for _, line := range lines {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				switch fields[0] {
				case "ctxt":
					if value, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
						stats["context_switches_total"] = value
						stats["context_switches_per_sec"] = value / 60 // Approximate per second
					}
				case "intr":
					if value, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
						stats["interrupts_total"] = value
						stats["interrupts_per_sec"] = value / 60 // Approximate per second
					}
				}
			}
		}

		return stats
	}

	// Fallback: simulate context switch stats
	return map[string]interface{}{
		"context_switches_total":   uint64(1000000),
		"context_switches_per_sec": uint64(50000),
		"interrupts_total":         uint64(500000),
		"interrupts_per_sec":       uint64(25000),
		"simulated":                true,
	}
}

// CPUThrottlingHealthCheck performs health checks on CPU throttling
type CPUThrottlingHealthCheck struct {
	*health.BaseHealthCheck
	throttlingThreshold float64
}

// NewCPUThrottlingHealthCheck creates a new CPU throttling health check
func NewCPUThrottlingHealthCheck(config *CPUHealthCheckConfig) *CPUThrottlingHealthCheck {
	if config == nil {
		config = &CPUHealthCheckConfig{}
	}

	if config.Name == "" {
		config.Name = "cpu-throttling"
	}

	if config.Tags == nil {
		config.Tags = make(map[string]string)
	}

	config.Tags["check_type"] = "cpu_throttling"

	baseConfig := &health.HealthCheckConfig{
		Name:     config.Name,
		Timeout:  config.Timeout,
		Critical: config.Critical,
		Tags:     config.Tags,
	}

	return &CPUThrottlingHealthCheck{
		BaseHealthCheck:     health.NewBaseHealthCheck(baseConfig),
		throttlingThreshold: 10.0, // 10% throttling threshold
	}
}

// Check performs the CPU throttling health check
func (cthc *CPUThrottlingHealthCheck) Check(ctx context.Context) *health.HealthResult {
	start := time.Now()

	result := health.NewHealthResult(cthc.Name(), health.HealthStatusHealthy, "CPU throttling is normal").
		WithCritical(cthc.Critical()).
		WithTags(cthc.Tags())

	// Get throttling statistics
	stats := cthc.getThrottlingStats()
	for k, v := range stats {
		result.WithDetail(k, v)
	}

	result.WithDetail("throttling_threshold", cthc.throttlingThreshold)

	// Check throttling percentage
	if throttling, ok := stats["throttling_percent"].(float64); ok {
		if throttling > cthc.throttlingThreshold {
			result.Status = health.HealthStatusDegraded
			result.Message = fmt.Sprintf("CPU throttling %.2f%% exceeds threshold %.2f%%", throttling, cthc.throttlingThreshold)
			result.WithDetail("throttling_exceeded", true)
		}
	}

	result.WithDuration(time.Since(start))
	return result
}

// getThrottlingStats gets CPU throttling statistics
func (cthc *CPUThrottlingHealthCheck) getThrottlingStats() map[string]interface{} {
	// In a real implementation, this would read from cgroup or thermal files
	// For now, return simulated data
	return map[string]interface{}{
		"throttling_percent":    2.5,
		"thermal_throttling":    false,
		"power_throttling":      false,
		"frequency_scaling":     true,
		"current_frequency_mhz": 2400,
		"max_frequency_mhz":     3000,
		"simulated":             true,
	}
}

// RegisterCPUHealthChecks registers CPU health checks with the health service
func RegisterCPUHealthChecks(healthService health.HealthService) error {
	// Register main CPU health check
	cpuCheck := NewCPUHealthCheck(&CPUHealthCheckConfig{
		Name:              "cpu",
		WarningThreshold:  80.0,
		CriticalThreshold: 95.0,
		CheckLoadAvg:      true,
		CheckCPUCount:     true,
		Samples:           3,
		SampleInterval:    1 * time.Second,
		Critical:          true,
	})

	if err := healthService.Register(cpuCheck); err != nil {
		return fmt.Errorf("failed to register CPU health check: %w", err)
	}

	// Register CPU load health check
	loadCheck := NewCPULoadHealthCheck(&CPUHealthCheckConfig{
		Name:     "cpu-load",
		Critical: false,
	})

	if err := healthService.Register(loadCheck); err != nil {
		return fmt.Errorf("failed to register CPU load health check: %w", err)
	}

	// Register context switch health check
	contextSwitchCheck := NewCPUContextSwitchHealthCheck(&CPUHealthCheckConfig{
		Name:     "cpu-context-switch",
		Critical: false,
	})

	if err := healthService.Register(contextSwitchCheck); err != nil {
		return fmt.Errorf("failed to register CPU context switch health check: %w", err)
	}

	// Register throttling health check
	throttlingCheck := NewCPUThrottlingHealthCheck(&CPUHealthCheckConfig{
		Name:     "cpu-throttling",
		Critical: false,
	})

	if err := healthService.Register(throttlingCheck); err != nil {
		return fmt.Errorf("failed to register CPU throttling health check: %w", err)
	}

	return nil
}

// CPUHealthCheckComposite combines multiple CPU health checks
type CPUHealthCheckComposite struct {
	*health.CompositeHealthCheck
	cpuChecks []health.HealthCheck
}

// NewCPUHealthCheckComposite creates a composite CPU health check
func NewCPUHealthCheckComposite(name string, checks ...health.HealthCheck) *CPUHealthCheckComposite {
	config := &health.HealthCheckConfig{
		Name:     name,
		Timeout:  30 * time.Second,
		Critical: true,
	}

	composite := health.NewCompositeHealthCheck(config, checks...)

	return &CPUHealthCheckComposite{
		CompositeHealthCheck: composite,
		cpuChecks:            checks,
	}
}

// GetCPUChecks returns the individual CPU checks
func (chcc *CPUHealthCheckComposite) GetCPUChecks() []health.HealthCheck {
	return chcc.cpuChecks
}

// AddCPUCheck adds a CPU check to the composite
func (chcc *CPUHealthCheckComposite) AddCPUCheck(check health.HealthCheck) {
	chcc.cpuChecks = append(chcc.cpuChecks, check)
	chcc.CompositeHealthCheck.AddCheck(check)
}

// CreateCPUHealthCheckComposite creates a composite CPU health check with all sub-checks
func CreateCPUHealthCheckComposite() *CPUHealthCheckComposite {
	cpuCheck := NewCPUHealthCheck(&CPUHealthCheckConfig{
		Name:              "cpu",
		WarningThreshold:  80.0,
		CriticalThreshold: 95.0,
		CheckLoadAvg:      true,
		CheckCPUCount:     true,
		Samples:           3,
		SampleInterval:    1 * time.Second,
	})

	loadCheck := NewCPULoadHealthCheck(&CPUHealthCheckConfig{
		Name: "cpu-load",
	})

	contextSwitchCheck := NewCPUContextSwitchHealthCheck(&CPUHealthCheckConfig{
		Name: "cpu-context-switch",
	})

	throttlingCheck := NewCPUThrottlingHealthCheck(&CPUHealthCheckConfig{
		Name: "cpu-throttling",
	})

	return NewCPUHealthCheckComposite("cpu-composite", cpuCheck, loadCheck, contextSwitchCheck, throttlingCheck)
}
