package plugins

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/xraph/forge/logger"
)

// sandbox implements the Sandbox interface
type sandbox struct {
	mu           sync.RWMutex
	logger       logger.Logger
	limits       map[string]*resourceLimits
	violations   map[string][]SecurityViolation
	monitoring   map[string]*resourceMonitor
	enabled      bool
	maxPlugins   int
	globalLimits *resourceLimits
}

// resourceLimits defines resource constraints for a plugin
type resourceLimits struct {
	// Memory limits
	memoryLimit   int64 // Maximum memory in bytes
	memoryWarning int64 // Warning threshold
	memoryUsage   int64 // Current usage
	memoryPeak    int64 // Peak usage

	// CPU limits
	cpuLimit   float64 // Maximum CPU percentage (0.0-1.0)
	cpuWarning float64 // Warning threshold
	cpuUsage   float64 // Current usage

	// Network access
	networkAccess bool     // Allow network access
	allowedHosts  []string // Allowed hosts/domains
	blockedHosts  []string // Blocked hosts/domains

	// File system access
	fileSystemPaths []string // Allowed file system paths
	readOnlyPaths   []string // Read-only paths
	tempDirAccess   bool     // Allow temp directory access

	// Permissions
	permissions     map[string]bool // Granted permissions
	denyPermissions map[string]bool // Explicitly denied permissions

	// Time limits
	executionTimeout time.Duration // Maximum execution time
	startTime        time.Time     // When plugin started

	// Goroutine limits
	maxGoroutines  int // Maximum number of goroutines
	goroutineCount int // Current goroutine count

	// System call restrictions
	allowedSyscalls []string // Allowed system calls
	blockedSyscalls []string // Blocked system calls

	// Resource monitoring
	monitoringEnabled bool
	violationPolicy   ViolationPolicy
}

// resourceMonitor tracks resource usage
type resourceMonitor struct {
	plugin          Plugin
	startTime       time.Time
	lastCheck       time.Time
	samples         []ResourceSample
	alertThresholds map[string]float64
	violations      []SecurityViolation

	// Monitoring goroutine
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

// ResourceSample represents a point-in-time resource measurement
type ResourceSample struct {
	Timestamp       time.Time `json:"timestamp"`
	MemoryUsage     int64     `json:"memory_usage"`
	CPUUsage        float64   `json:"cpu_usage"`
	GoroutineCount  int       `json:"goroutine_count"`
	NetworkActivity int64     `json:"network_activity"`
	DiskActivity    int64     `json:"disk_activity"`
}

// ViolationPolicy defines how to handle security violations
type ViolationPolicy string

const (
	ViolationPolicyWarn  ViolationPolicy = "warn"  // Log warning
	ViolationPolicyBlock ViolationPolicy = "block" // Block operation
	ViolationPolicyKill  ViolationPolicy = "kill"  // Terminate plugin
)

// NewSandbox creates a new plugin sandbox
func NewSandbox() Sandbox {
	s := &sandbox{
		logger:     logger.GetGlobalLogger().Named("plugin-sandbox"),
		limits:     make(map[string]*resourceLimits),
		violations: make(map[string][]SecurityViolation),
		monitoring: make(map[string]*resourceMonitor),
		enabled:    true,
		maxPlugins: 100,
		globalLimits: &resourceLimits{
			memoryLimit:       100 * 1024 * 1024, // 100MB default
			cpuLimit:          0.5,               // 50% CPU default
			networkAccess:     false,             // No network by default
			fileSystemPaths:   []string{},        // No file access by default
			permissions:       make(map[string]bool),
			denyPermissions:   make(map[string]bool),
			executionTimeout:  30 * time.Second, // 30 second default timeout
			maxGoroutines:     10,               // Max 10 goroutines
			monitoringEnabled: true,
			violationPolicy:   ViolationPolicyWarn,
		},
	}

	s.logger.Info("Plugin sandbox initialized",
		logger.Bool("enabled", s.enabled),
		logger.Int("max_plugins", s.maxPlugins),
	)

	return s
}

// Execute executes an operation within the sandbox
func (s *sandbox) Execute(plugin Plugin, operation func() error) error {
	if !s.enabled {
		return operation()
	}

	pluginName := plugin.Name()
	s.logger.Debug("Executing plugin operation in sandbox",
		logger.String("plugin", pluginName),
	)

	// Get or create limits
	limits := s.getOrCreateLimits(pluginName)

	// Start monitoring if enabled
	var monitor *resourceMonitor
	if limits.monitoringEnabled {
		monitor = s.startMonitoring(plugin)
		defer s.stopMonitoring(pluginName)
	}

	// Check pre-execution limits
	if err := s.checkPreExecutionLimits(pluginName, limits); err != nil {
		return err
	}

	// Set execution timeout
	ctx := context.Background()
	if limits.executionTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, limits.executionTimeout)
		defer cancel()
	}

	// Execute operation with monitoring
	errorChan := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				violation := SecurityViolation{
					Type:        "panic",
					Description: fmt.Sprintf("Plugin panicked: %v", r),
					Timestamp:   time.Now(),
					Severity:    "critical",
					Action:      "kill",
				}
				s.recordViolation(pluginName, violation)
				errorChan <- fmt.Errorf("plugin panicked: %v", r)
			}
		}()

		// Check resource limits during execution
		if monitor != nil {
			go s.monitorExecution(pluginName, limits, monitor)
		}

		err := operation()
		errorChan <- err
	}()

	// Wait for completion or timeout
	select {
	case err := <-errorChan:
		// Check post-execution limits
		s.checkPostExecutionLimits(pluginName, limits)
		return err

	case <-ctx.Done():
		violation := SecurityViolation{
			Type:        "timeout",
			Description: fmt.Sprintf("Plugin execution exceeded timeout: %v", limits.executionTimeout),
			Timestamp:   time.Now(),
			Severity:    "high",
			Action:      "kill",
		}
		s.recordViolation(pluginName, violation)
		return fmt.Errorf("plugin execution timed out")
	}
}

// SetMemoryLimit sets the memory limit for a plugin
func (s *sandbox) SetMemoryLimit(plugin Plugin, limit int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	pluginName := plugin.Name()
	limits := s.getOrCreateLimits(pluginName)
	limits.memoryLimit = limit
	limits.memoryWarning = limit * 8 / 10 // 80% warning threshold

	s.logger.Debug("Memory limit set",
		logger.String("plugin", pluginName),
		logger.Int64("limit", limit),
	)

	return nil
}

// SetCPULimit sets the CPU limit for a plugin
func (s *sandbox) SetCPULimit(plugin Plugin, limit float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if limit < 0 || limit > 1 {
		return fmt.Errorf("CPU limit must be between 0.0 and 1.0, got %f", limit)
	}

	pluginName := plugin.Name()
	limits := s.getOrCreateLimits(pluginName)
	limits.cpuLimit = limit
	limits.cpuWarning = limit * 0.8 // 80% warning threshold

	s.logger.Debug("CPU limit set",
		logger.String("plugin", pluginName),
		logger.Float64("limit", limit),
	)

	return nil
}

// SetNetworkAccess sets network access permission for a plugin
func (s *sandbox) SetNetworkAccess(plugin Plugin, allowed bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	pluginName := plugin.Name()
	limits := s.getOrCreateLimits(pluginName)
	limits.networkAccess = allowed

	s.logger.Debug("Network access set",
		logger.String("plugin", pluginName),
		logger.Bool("allowed", allowed),
	)

	return nil
}

// SetFileSystemAccess sets file system access paths for a plugin
func (s *sandbox) SetFileSystemAccess(plugin Plugin, paths []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	pluginName := plugin.Name()
	limits := s.getOrCreateLimits(pluginName)
	limits.fileSystemPaths = make([]string, len(paths))
	copy(limits.fileSystemPaths, paths)

	s.logger.Debug("File system access set",
		logger.String("plugin", pluginName),
		logger.Strings("paths", paths),
	)

	return nil
}

// CheckPermission checks if a plugin has a specific permission
func (s *sandbox) CheckPermission(plugin Plugin, permission string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	pluginName := plugin.Name()
	limits := s.getOrCreateLimits(pluginName)

	// Check if explicitly denied
	if denied, exists := limits.denyPermissions[permission]; exists && denied {
		violation := SecurityViolation{
			Type:        "permission_denied",
			Description: fmt.Sprintf("Permission explicitly denied: %s", permission),
			Timestamp:   time.Now(),
			Severity:    "medium",
			Action:      "block",
		}
		s.recordViolationUnsafe(pluginName, violation)
		return fmt.Errorf("%w: %s", ErrPermissionDenied, permission)
	}

	// Check if explicitly granted
	if granted, exists := limits.permissions[permission]; exists && granted {
		return nil
	}

	// Check default policy
	if s.isDefaultPermissionAllowed(permission) {
		return nil
	}

	violation := SecurityViolation{
		Type:        "permission_missing",
		Description: fmt.Sprintf("Permission not granted: %s", permission),
		Timestamp:   time.Now(),
		Severity:    "low",
		Action:      "warn",
	}
	s.recordViolationUnsafe(pluginName, violation)

	return fmt.Errorf("%w: %s", ErrPermissionDenied, permission)
}

// GrantPermission grants a permission to a plugin
func (s *sandbox) GrantPermission(plugin Plugin, permission string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	pluginName := plugin.Name()
	limits := s.getOrCreateLimits(pluginName)
	limits.permissions[permission] = true

	// Remove from denied list if present
	delete(limits.denyPermissions, permission)

	s.logger.Debug("Permission granted",
		logger.String("plugin", pluginName),
		logger.String("permission", permission),
	)

	return nil
}

// RevokePermission revokes a permission from a plugin
func (s *sandbox) RevokePermission(plugin Plugin, permission string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	pluginName := plugin.Name()
	limits := s.getOrCreateLimits(pluginName)
	limits.permissions[permission] = false
	limits.denyPermissions[permission] = true

	s.logger.Debug("Permission revoked",
		logger.String("plugin", pluginName),
		logger.String("permission", permission),
	)

	return nil
}

// GetResourceUsage returns current resource usage for a plugin
func (s *sandbox) GetResourceUsage(plugin Plugin) ResourceUsage {
	s.mu.RLock()
	defer s.mu.RUnlock()

	pluginName := plugin.Name()
	monitor, exists := s.monitoring[pluginName]
	if !exists {
		return ResourceUsage{}
	}

	// Get current system stats
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return ResourceUsage{
		MemoryUsage: int64(m.Alloc),
		CPUUsage:    s.getCurrentCPUUsage(pluginName),
		NetworkIO:   0, // Would be implemented with proper monitoring
		DiskIO:      0, // Would be implemented with proper monitoring
		Goroutines:  runtime.NumGoroutine(),
		Duration:    time.Since(monitor.startTime),
	}
}

// GetViolations returns security violations for a plugin
func (s *sandbox) GetViolations(plugin Plugin) []SecurityViolation {
	s.mu.RLock()
	defer s.mu.RUnlock()

	pluginName := plugin.Name()
	violations, exists := s.violations[pluginName]
	if !exists {
		return []SecurityViolation{}
	}

	// Return a copy
	result := make([]SecurityViolation, len(violations))
	copy(result, violations)
	return result
}

// Private helper methods

func (s *sandbox) getOrCreateLimits(pluginName string) *resourceLimits {
	if limits, exists := s.limits[pluginName]; exists {
		return limits
	}

	// Create new limits based on global defaults
	limits := &resourceLimits{
		memoryLimit:       s.globalLimits.memoryLimit,
		memoryWarning:     s.globalLimits.memoryWarning,
		cpuLimit:          s.globalLimits.cpuLimit,
		cpuWarning:        s.globalLimits.cpuWarning,
		networkAccess:     s.globalLimits.networkAccess,
		fileSystemPaths:   make([]string, len(s.globalLimits.fileSystemPaths)),
		readOnlyPaths:     make([]string, 0),
		tempDirAccess:     s.globalLimits.tempDirAccess,
		permissions:       make(map[string]bool),
		denyPermissions:   make(map[string]bool),
		executionTimeout:  s.globalLimits.executionTimeout,
		maxGoroutines:     s.globalLimits.maxGoroutines,
		monitoringEnabled: s.globalLimits.monitoringEnabled,
		violationPolicy:   s.globalLimits.violationPolicy,
	}

	copy(limits.fileSystemPaths, s.globalLimits.fileSystemPaths)
	for k, v := range s.globalLimits.permissions {
		limits.permissions[k] = v
	}
	for k, v := range s.globalLimits.denyPermissions {
		limits.denyPermissions[k] = v
	}

	s.limits[pluginName] = limits
	return limits
}

func (s *sandbox) startMonitoring(plugin Plugin) *resourceMonitor {
	pluginName := plugin.Name()

	ctx, cancel := context.WithCancel(context.Background())
	monitor := &resourceMonitor{
		plugin:          plugin,
		startTime:       time.Now(),
		lastCheck:       time.Now(),
		samples:         make([]ResourceSample, 0),
		alertThresholds: make(map[string]float64),
		violations:      make([]SecurityViolation, 0),
		ctx:             ctx,
		cancel:          cancel,
		done:            make(chan struct{}),
	}

	s.monitoring[pluginName] = monitor

	// Start monitoring goroutine
	go s.monitoringLoop(pluginName, monitor)

	return monitor
}

func (s *sandbox) stopMonitoring(pluginName string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if monitor, exists := s.monitoring[pluginName]; exists {
		monitor.cancel()
		<-monitor.done
		delete(s.monitoring, pluginName)
	}
}

func (s *sandbox) monitoringLoop(pluginName string, monitor *resourceMonitor) {
	defer close(monitor.done)

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-monitor.ctx.Done():
			return

		case <-ticker.C:
			s.collectResourceSample(pluginName, monitor)
		}
	}
}

func (s *sandbox) collectResourceSample(pluginName string, monitor *resourceMonitor) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	sample := ResourceSample{
		Timestamp:       time.Now(),
		MemoryUsage:     int64(m.Alloc),
		CPUUsage:        s.getCurrentCPUUsage(pluginName),
		GoroutineCount:  runtime.NumGoroutine(),
		NetworkActivity: 0, // Would be implemented with proper monitoring
		DiskActivity:    0, // Would be implemented with proper monitoring
	}

	monitor.samples = append(monitor.samples, sample)

	// Keep only last 100 samples
	if len(monitor.samples) > 100 {
		monitor.samples = monitor.samples[1:]
	}

	// Check for violations
	s.checkResourceViolations(pluginName, sample)
}

func (s *sandbox) getCurrentCPUUsage(pluginName string) float64 {
	// This is a simplified implementation
	// Real implementation would track CPU usage per plugin
	return 0.0
}

func (s *sandbox) checkPreExecutionLimits(pluginName string, limits *resourceLimits) error {
	// Check if too many plugins are running
	if len(s.monitoring) >= s.maxPlugins {
		return fmt.Errorf("maximum number of plugins exceeded: %d", s.maxPlugins)
	}

	// Check if plugin is already running
	if _, exists := s.monitoring[pluginName]; exists {
		return fmt.Errorf("plugin is already running: %s", pluginName)
	}

	return nil
}

func (s *sandbox) checkPostExecutionLimits(pluginName string, limits *resourceLimits) {
	// Update usage statistics
	limits.startTime = time.Now()
}

func (s *sandbox) monitorExecution(pluginName string, limits *resourceLimits, monitor *resourceMonitor) {
	// This would implement real-time monitoring during execution
	// For now, it's a placeholder
}

func (s *sandbox) checkResourceViolations(pluginName string, sample ResourceSample) {
	limits := s.limits[pluginName]
	if limits == nil {
		return
	}

	// Check memory violations
	if sample.MemoryUsage > limits.memoryLimit {
		violation := SecurityViolation{
			Type:        "memory_limit_exceeded",
			Description: fmt.Sprintf("Memory usage %d exceeds limit %d", sample.MemoryUsage, limits.memoryLimit),
			Timestamp:   sample.Timestamp,
			Severity:    "high",
			Action:      string(limits.violationPolicy),
		}
		s.recordViolation(pluginName, violation)
	} else if sample.MemoryUsage > limits.memoryWarning {
		violation := SecurityViolation{
			Type:        "memory_warning",
			Description: fmt.Sprintf("Memory usage %d exceeds warning threshold %d", sample.MemoryUsage, limits.memoryWarning),
			Timestamp:   sample.Timestamp,
			Severity:    "medium",
			Action:      "warn",
		}
		s.recordViolation(pluginName, violation)
	}

	// Check CPU violations
	if sample.CPUUsage > limits.cpuLimit {
		violation := SecurityViolation{
			Type:        "cpu_limit_exceeded",
			Description: fmt.Sprintf("CPU usage %.2f exceeds limit %.2f", sample.CPUUsage, limits.cpuLimit),
			Timestamp:   sample.Timestamp,
			Severity:    "high",
			Action:      string(limits.violationPolicy),
		}
		s.recordViolation(pluginName, violation)
	}

	// Check goroutine violations
	if sample.GoroutineCount > limits.maxGoroutines {
		violation := SecurityViolation{
			Type:        "goroutine_limit_exceeded",
			Description: fmt.Sprintf("Goroutine count %d exceeds limit %d", sample.GoroutineCount, limits.maxGoroutines),
			Timestamp:   sample.Timestamp,
			Severity:    "medium",
			Action:      string(limits.violationPolicy),
		}
		s.recordViolation(pluginName, violation)
	}
}

func (s *sandbox) recordViolation(pluginName string, violation SecurityViolation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recordViolationUnsafe(pluginName, violation)
}

func (s *sandbox) recordViolationUnsafe(pluginName string, violation SecurityViolation) {
	s.violations[pluginName] = append(s.violations[pluginName], violation)

	// Log violation
	s.logger.Warn("Security violation detected",
		logger.String("plugin", pluginName),
		logger.String("type", violation.Type),
		logger.String("description", violation.Description),
		logger.String("severity", violation.Severity),
		logger.String("action", violation.Action),
	)

	// Take action based on violation policy
	switch violation.Action {
	case "warn":
		// Already logged
	case "block":
		// Would block the current operation
	case "kill":
		// Would terminate the plugin
		s.logger.Error("Terminating plugin due to security violation",
			logger.String("plugin", pluginName),
			logger.String("type", violation.Type),
		)
	}
}

func (s *sandbox) isDefaultPermissionAllowed(permission string) bool {
	// Define safe default permissions
	safePermissions := map[string]bool{
		"read_time":   true,
		"log":         true,
		"read_config": true,
	}

	return safePermissions[permission]
}

// Public utility methods

// GetSandboxStatistics returns sandbox statistics
func (s *sandbox) GetSandboxStatistics() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	totalViolations := 0
	for _, violations := range s.violations {
		totalViolations += len(violations)
	}

	return map[string]interface{}{
		"enabled":           s.enabled,
		"monitored_plugins": len(s.monitoring),
		"total_violations":  totalViolations,
		"max_plugins":       s.maxPlugins,
		"memory_limit":      s.globalLimits.memoryLimit,
		"cpu_limit":         s.globalLimits.cpuLimit,
		"network_access":    s.globalLimits.networkAccess,
	}
}

// SetGlobalLimits sets global default limits
func (s *sandbox) SetGlobalLimits(limits *resourceLimits) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.globalLimits = limits
	s.logger.Info("Global sandbox limits updated")
}

// EnableMonitoring enables or disables monitoring
func (s *sandbox) EnableMonitoring(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, limits := range s.limits {
		limits.monitoringEnabled = enabled
	}

	s.logger.Info("Sandbox monitoring toggled",
		logger.Bool("enabled", enabled),
	)
}

// ClearViolations clears violation history for a plugin
func (s *sandbox) ClearViolations(pluginName string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.violations, pluginName)
	s.logger.Info("Violations cleared",
		logger.String("plugin", pluginName),
	)
}

// SetViolationPolicy sets the violation policy for a plugin
func (s *sandbox) SetViolationPolicy(plugin Plugin, policy ViolationPolicy) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	pluginName := plugin.Name()
	limits := s.getOrCreateLimits(pluginName)
	limits.violationPolicy = policy

	s.logger.Debug("Violation policy set",
		logger.String("plugin", pluginName),
		logger.String("policy", string(policy)),
	)

	return nil
}

// Disable disables the sandbox (for development/testing)
func (s *sandbox) Disable() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.enabled = false
	s.logger.Warn("Sandbox disabled - this should only be used for development!")
}

// Enable enables the sandbox
func (s *sandbox) Enable() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.enabled = true
	s.logger.Info("Sandbox enabled")
}
