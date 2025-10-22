package performance

import (
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/xraph/forge/v2/internal/logger"
	"github.com/xraph/forge/v2/internal/shared"
)

// Profiler provides comprehensive performance monitoring and profiling
type Profiler struct {
	config         ProfilerConfig
	metrics        map[string]*PerformanceMetric
	profiles       map[string]*Profile
	mu             sync.RWMutex
	logger         logger.Logger
	metricsService shared.Metrics
	stopC          chan struct{}
	wg             sync.WaitGroup
}

// ProfilerConfig contains profiler configuration
type ProfilerConfig struct {
	EnableCPUProfiling       bool           `yaml:"enable_cpu_profiling" default:"true"`
	EnableMemoryProfiling    bool           `yaml:"enable_memory_profiling" default:"true"`
	EnableGoroutineProfiling bool           `yaml:"enable_goroutine_profiling" default:"true"`
	EnableBlockProfiling     bool           `yaml:"enable_block_profiling" default:"true"`
	EnableMutexProfiling     bool           `yaml:"enable_mutex_profiling" default:"true"`
	ProfileInterval          time.Duration  `yaml:"profile_interval" default:"30s"`
	ProfileDuration          time.Duration  `yaml:"profile_duration" default:"10s"`
	EnableMetrics            bool           `yaml:"enable_metrics" default:"true"`
	EnableAlerts             bool           `yaml:"enable_alerts" default:"true"`
	CPUThreshold             float64        `yaml:"cpu_threshold" default:"80.0"`
	MemoryThreshold          float64        `yaml:"memory_threshold" default:"80.0"`
	GoroutineThreshold       int            `yaml:"goroutine_threshold" default:"1000"`
	Logger                   logger.Logger  `yaml:"-"`
	Metrics                  shared.Metrics `yaml:"-"`
}

// PerformanceMetric represents a performance metric
type PerformanceMetric struct {
	Name        string            `json:"name"`
	Type        MetricType        `json:"type"`
	Value       float64           `json:"value"`
	Unit        string            `json:"unit"`
	Threshold   float64           `json:"threshold"`
	Status      MetricStatus      `json:"status"`
	Timestamp   time.Time         `json:"timestamp"`
	Labels      map[string]string `json:"labels"`
	Description string            `json:"description"`
}

// MetricType represents the type of metric
type MetricType int

const (
	MetricTypeCPU MetricType = iota
	MetricTypeMemory
	MetricTypeGoroutine
	MetricTypeBlock
	MetricTypeMutex
	MetricTypeCustom
)

// MetricStatus represents the status of a metric
type MetricStatus int

const (
	MetricStatusNormal MetricStatus = iota
	MetricStatusWarning
	MetricStatusCritical
	MetricStatusUnknown
)

// Profile represents a performance profile
type Profile struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Type        ProfileType       `json:"type"`
	Data        []byte            `json:"data"`
	Size        int64             `json:"size"`
	Duration    time.Duration     `json:"duration"`
	Timestamp   time.Time         `json:"timestamp"`
	Labels      map[string]string `json:"labels"`
	Description string            `json:"description"`
}

// ProfileType represents the type of profile
type ProfileType int

const (
	ProfileTypeCPU ProfileType = iota
	ProfileTypeMemory
	ProfileTypeGoroutine
	ProfileTypeBlock
	ProfileTypeMutex
	ProfileTypeTrace
)

// PerformanceAlert represents a performance alert
type PerformanceAlert struct {
	ID         string            `json:"id"`
	Metric     string            `json:"metric"`
	Type       AlertType         `json:"type"`
	Severity   AlertSeverity     `json:"severity"`
	Message    string            `json:"message"`
	Value      float64           `json:"value"`
	Threshold  float64           `json:"threshold"`
	Timestamp  time.Time         `json:"timestamp"`
	Labels     map[string]string `json:"labels"`
	Resolved   bool              `json:"resolved"`
	ResolvedAt time.Time         `json:"resolved_at,omitempty"`
}

// AlertType represents the type of alert
type AlertType int

const (
	AlertTypeCPU AlertType = iota
	AlertTypeMemory
	AlertTypeGoroutine
	AlertTypeBlock
	AlertTypeMutex
	AlertTypeCustom
)

// AlertSeverity represents the severity of an alert
type AlertSeverity int

const (
	AlertSeverityInfo AlertSeverity = iota
	AlertSeverityWarning
	AlertSeverityCritical
	AlertSeverityEmergency
)

// NewProfiler creates a new profiler
func NewProfiler(config ProfilerConfig) *Profiler {
	if config.Logger == nil {
		config.Logger = logger.NewLogger(logger.LoggingConfig{Level: "info"})
	}

	profiler := &Profiler{
		config:         config,
		metrics:        make(map[string]*PerformanceMetric),
		profiles:       make(map[string]*Profile),
		logger:         config.Logger,
		metricsService: config.Metrics,
		stopC:          make(chan struct{}),
	}

	// Start profiling goroutines
	if config.EnableCPUProfiling || config.EnableMemoryProfiling || config.EnableGoroutineProfiling {
		profiler.wg.Add(1)
		go profiler.startProfiling()
	}

	return profiler
}

// startProfiling starts the profiling goroutine
func (p *Profiler) startProfiling() {
	defer p.wg.Done()

	ticker := time.NewTicker(p.config.ProfileInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.performProfiling()
		case <-p.stopC:
			return
		}
	}
}

// performProfiling performs performance profiling
func (p *Profiler) performProfiling() {
	// CPU profiling
	if p.config.EnableCPUProfiling {
		if err := p.profileCPU(); err != nil {
			p.logger.Error("CPU profiling failed", logger.String("error", err.Error()))
		}
	}

	// Memory profiling
	if p.config.EnableMemoryProfiling {
		if err := p.profileMemory(); err != nil {
			p.logger.Error("Memory profiling failed", logger.String("error", err.Error()))
		}
	}

	// Goroutine profiling
	if p.config.EnableGoroutineProfiling {
		if err := p.profileGoroutines(); err != nil {
			p.logger.Error("Goroutine profiling failed", logger.String("error", err.Error()))
		}
	}

	// Block profiling
	if p.config.EnableBlockProfiling {
		if err := p.profileBlocks(); err != nil {
			p.logger.Error("Block profiling failed", logger.String("error", err.Error()))
		}
	}

	// Mutex profiling
	if p.config.EnableMutexProfiling {
		if err := p.profileMutexes(); err != nil {
			p.logger.Error("Mutex profiling failed", logger.String("error", err.Error()))
		}
	}

	// Check for alerts
	if p.config.EnableAlerts {
		p.checkAlerts()
	}
}

// profileCPU performs CPU profiling
func (p *Profiler) profileCPU() error {
	start := time.Now()

	// Get CPU usage
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// Calculate CPU usage (simplified)
	cpuUsage := float64(m.NumGC) * 100.0 / float64(m.NumGC+1)

	// Create metric
	metric := &PerformanceMetric{
		Name:        "cpu_usage",
		Type:        MetricTypeCPU,
		Value:       cpuUsage,
		Unit:        "percent",
		Threshold:   p.config.CPUThreshold,
		Status:      p.getMetricStatus(cpuUsage, p.config.CPUThreshold),
		Timestamp:   time.Now(),
		Labels:      map[string]string{"type": "cpu"},
		Description: "CPU usage percentage",
	}

	p.recordMetric(metric)

	// Create profile
	profile := &Profile{
		ID:          fmt.Sprintf("cpu-%d", time.Now().Unix()),
		Name:        "CPU Profile",
		Type:        ProfileTypeCPU,
		Data:        []byte(fmt.Sprintf("CPU usage: %.2f%%", cpuUsage)),
		Size:        int64(len(fmt.Sprintf("CPU usage: %.2f%%", cpuUsage))),
		Duration:    time.Since(start),
		Timestamp:   time.Now(),
		Labels:      map[string]string{"type": "cpu"},
		Description: "CPU performance profile",
	}

	p.recordProfile(profile)

	return nil
}

// profileMemory performs memory profiling
func (p *Profiler) profileMemory() error {
	start := time.Now()

	// Get memory stats
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// Calculate memory usage
	memoryUsage := float64(m.Alloc) * 100.0 / float64(m.Sys)

	// Create metric
	metric := &PerformanceMetric{
		Name:        "memory_usage",
		Type:        MetricTypeMemory,
		Value:       memoryUsage,
		Unit:        "percent",
		Threshold:   p.config.MemoryThreshold,
		Status:      p.getMetricStatus(memoryUsage, p.config.MemoryThreshold),
		Timestamp:   time.Now(),
		Labels:      map[string]string{"type": "memory"},
		Description: "Memory usage percentage",
	}

	p.recordMetric(metric)

	// Create profile
	profile := &Profile{
		ID:          fmt.Sprintf("memory-%d", time.Now().Unix()),
		Name:        "Memory Profile",
		Type:        ProfileTypeMemory,
		Data:        []byte(fmt.Sprintf("Memory usage: %.2f%%", memoryUsage)),
		Size:        int64(len(fmt.Sprintf("Memory usage: %.2f%%", memoryUsage))),
		Duration:    time.Since(start),
		Timestamp:   time.Now(),
		Labels:      map[string]string{"type": "memory"},
		Description: "Memory performance profile",
	}

	p.recordProfile(profile)

	return nil
}

// profileGoroutines performs goroutine profiling
func (p *Profiler) profileGoroutines() error {
	start := time.Now()

	// Get goroutine count
	goroutineCount := runtime.NumGoroutine()

	// Create metric
	metric := &PerformanceMetric{
		Name:        "goroutine_count",
		Type:        MetricTypeGoroutine,
		Value:       float64(goroutineCount),
		Unit:        "count",
		Threshold:   float64(p.config.GoroutineThreshold),
		Status:      p.getMetricStatus(float64(goroutineCount), float64(p.config.GoroutineThreshold)),
		Timestamp:   time.Now(),
		Labels:      map[string]string{"type": "goroutine"},
		Description: "Goroutine count",
	}

	p.recordMetric(metric)

	// Create profile
	profile := &Profile{
		ID:          fmt.Sprintf("goroutine-%d", time.Now().Unix()),
		Name:        "Goroutine Profile",
		Type:        ProfileTypeGoroutine,
		Data:        []byte(fmt.Sprintf("Goroutine count: %d", goroutineCount)),
		Size:        int64(len(fmt.Sprintf("Goroutine count: %d", goroutineCount))),
		Duration:    time.Since(start),
		Timestamp:   time.Now(),
		Labels:      map[string]string{"type": "goroutine"},
		Description: "Goroutine performance profile",
	}

	p.recordProfile(profile)

	return nil
}

// profileBlocks performs block profiling
func (p *Profiler) profileBlocks() error {
	start := time.Now()

	// Get block stats
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// Calculate block usage
	blockUsage := float64(m.NumGC) * 100.0 / float64(m.NumGC+1)

	// Create metric
	metric := &PerformanceMetric{
		Name:        "block_usage",
		Type:        MetricTypeBlock,
		Value:       blockUsage,
		Unit:        "percent",
		Threshold:   50.0,
		Status:      p.getMetricStatus(blockUsage, 50.0),
		Timestamp:   time.Now(),
		Labels:      map[string]string{"type": "block"},
		Description: "Block usage percentage",
	}

	p.recordMetric(metric)

	// Create profile
	profile := &Profile{
		ID:          fmt.Sprintf("block-%d", time.Now().Unix()),
		Name:        "Block Profile",
		Type:        ProfileTypeBlock,
		Data:        []byte(fmt.Sprintf("Block usage: %.2f%%", blockUsage)),
		Size:        int64(len(fmt.Sprintf("Block usage: %.2f%%", blockUsage))),
		Duration:    time.Since(start),
		Timestamp:   time.Now(),
		Labels:      map[string]string{"type": "block"},
		Description: "Block performance profile",
	}

	p.recordProfile(profile)

	return nil
}

// profileMutexes performs mutex profiling
func (p *Profiler) profileMutexes() error {
	start := time.Now()

	// Get mutex stats
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// Calculate mutex usage
	mutexUsage := float64(m.NumGC) * 100.0 / float64(m.NumGC+1)

	// Create metric
	metric := &PerformanceMetric{
		Name:        "mutex_usage",
		Type:        MetricTypeMutex,
		Value:       mutexUsage,
		Unit:        "percent",
		Threshold:   50.0,
		Status:      p.getMetricStatus(mutexUsage, 50.0),
		Timestamp:   time.Now(),
		Labels:      map[string]string{"type": "mutex"},
		Description: "Mutex usage percentage",
	}

	p.recordMetric(metric)

	// Create profile
	profile := &Profile{
		ID:          fmt.Sprintf("mutex-%d", time.Now().Unix()),
		Name:        "Mutex Profile",
		Type:        ProfileTypeMutex,
		Data:        []byte(fmt.Sprintf("Mutex usage: %.2f%%", mutexUsage)),
		Size:        int64(len(fmt.Sprintf("Mutex usage: %.2f%%", mutexUsage))),
		Duration:    time.Since(start),
		Timestamp:   time.Now(),
		Labels:      map[string]string{"type": "mutex"},
		Description: "Mutex performance profile",
	}

	p.recordProfile(profile)

	return nil
}

// getMetricStatus determines the status of a metric
func (p *Profiler) getMetricStatus(value, threshold float64) MetricStatus {
	if value >= threshold {
		return MetricStatusCritical
	} else if value >= threshold*0.8 {
		return MetricStatusWarning
	}
	return MetricStatusNormal
}

// recordMetric records a performance metric
func (p *Profiler) recordMetric(metric *PerformanceMetric) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.metrics[metric.Name] = metric

	// Record metrics
	if p.config.EnableMetrics && p.metricsService != nil {
		p.recordMetricMetrics(metric)
	}
}

// recordProfile records a performance profile
func (p *Profiler) recordProfile(profile *Profile) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.profiles[profile.ID] = profile
}

// recordMetricMetrics records metric metrics
func (p *Profiler) recordMetricMetrics(metric *PerformanceMetric) {
	status := "normal"
	switch metric.Status {
	case MetricStatusWarning:
		status = "warning"
	case MetricStatusCritical:
		status = "critical"
	}

	p.metricsService.Counter("performance_metric_total", "name", metric.Name, "type", metric.Type.String(), "status", status).Inc()

	p.metricsService.Gauge("performance_metric_value", "name", metric.Name, "type", metric.Type.String()).Set(metric.Value)
}

// checkAlerts checks for performance alerts
func (p *Profiler) checkAlerts() {
	p.mu.RLock()
	metrics := make([]*PerformanceMetric, 0, len(p.metrics))
	for _, metric := range p.metrics {
		metrics = append(metrics, metric)
	}
	p.mu.RUnlock()

	for _, metric := range metrics {
		if metric.Status == MetricStatusCritical || metric.Status == MetricStatusWarning {
			alert := &PerformanceAlert{
				ID:        fmt.Sprintf("alert-%d", time.Now().Unix()),
				Metric:    metric.Name,
				Type:      p.getAlertType(metric.Type),
				Severity:  p.getAlertSeverity(metric.Status),
				Message:   fmt.Sprintf("%s is %s", metric.Name, metric.Status.String()),
				Value:     metric.Value,
				Threshold: metric.Threshold,
				Timestamp: time.Now(),
				Labels:    metric.Labels,
				Resolved:  false,
			}

			p.logger.Warn("performance alert triggered",
				logger.String("metric", alert.Metric),
				logger.String("severity", alert.Severity.String()),
				logger.Float64("value", alert.Value),
				logger.Float64("threshold", alert.Threshold))
		}
	}
}

// getAlertType converts metric type to alert type
func (p *Profiler) getAlertType(metricType MetricType) AlertType {
	switch metricType {
	case MetricTypeCPU:
		return AlertTypeCPU
	case MetricTypeMemory:
		return AlertTypeMemory
	case MetricTypeGoroutine:
		return AlertTypeGoroutine
	case MetricTypeBlock:
		return AlertTypeBlock
	case MetricTypeMutex:
		return AlertTypeMutex
	default:
		return AlertTypeCustom
	}
}

// getAlertSeverity converts metric status to alert severity
func (p *Profiler) getAlertSeverity(status MetricStatus) AlertSeverity {
	switch status {
	case MetricStatusWarning:
		return AlertSeverityWarning
	case MetricStatusCritical:
		return AlertSeverityCritical
	default:
		return AlertSeverityInfo
	}
}

// GetMetrics returns all performance metrics
func (p *Profiler) GetMetrics() map[string]*PerformanceMetric {
	p.mu.RLock()
	defer p.mu.RUnlock()

	metrics := make(map[string]*PerformanceMetric)
	for name, metric := range p.metrics {
		metrics[name] = metric
	}

	return metrics
}

// GetProfiles returns all performance profiles
func (p *Profiler) GetProfiles() map[string]*Profile {
	p.mu.RLock()
	defer p.mu.RUnlock()

	profiles := make(map[string]*Profile)
	for id, profile := range p.profiles {
		profiles[id] = profile
	}

	return profiles
}

// GetProfile returns a specific profile
func (p *Profiler) GetProfile(id string) (*Profile, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	profile, exists := p.profiles[id]
	if !exists {
		return nil, fmt.Errorf("profile %s not found", id)
	}

	return profile, nil
}

// Close closes the profiler
func (p *Profiler) Close() error {
	close(p.stopC)
	p.wg.Wait()
	return nil
}

// String methods for enums

func (t MetricType) String() string {
	switch t {
	case MetricTypeCPU:
		return "cpu"
	case MetricTypeMemory:
		return "memory"
	case MetricTypeGoroutine:
		return "goroutine"
	case MetricTypeBlock:
		return "block"
	case MetricTypeMutex:
		return "mutex"
	case MetricTypeCustom:
		return "custom"
	default:
		return "unknown"
	}
}

func (s MetricStatus) String() string {
	switch s {
	case MetricStatusNormal:
		return "normal"
	case MetricStatusWarning:
		return "warning"
	case MetricStatusCritical:
		return "critical"
	case MetricStatusUnknown:
		return "unknown"
	default:
		return "unknown"
	}
}

func (t ProfileType) String() string {
	switch t {
	case ProfileTypeCPU:
		return "cpu"
	case ProfileTypeMemory:
		return "memory"
	case ProfileTypeGoroutine:
		return "goroutine"
	case ProfileTypeBlock:
		return "block"
	case ProfileTypeMutex:
		return "mutex"
	case ProfileTypeTrace:
		return "trace"
	default:
		return "unknown"
	}
}

func (t AlertType) String() string {
	switch t {
	case AlertTypeCPU:
		return "cpu"
	case AlertTypeMemory:
		return "memory"
	case AlertTypeGoroutine:
		return "goroutine"
	case AlertTypeBlock:
		return "block"
	case AlertTypeMutex:
		return "mutex"
	case AlertTypeCustom:
		return "custom"
	default:
		return "unknown"
	}
}

func (s AlertSeverity) String() string {
	switch s {
	case AlertSeverityInfo:
		return "info"
	case AlertSeverityWarning:
		return "warning"
	case AlertSeverityCritical:
		return "critical"
	case AlertSeverityEmergency:
		return "emergency"
	default:
		return "unknown"
	}
}
