package middleware

import (
	"fmt"
	"runtime"
	"time"

	"github.com/xraph/forge/pkg/cli"
	"github.com/xraph/forge/pkg/common"
)

// MetricsMiddleware collects metrics for CLI commands
type MetricsMiddleware struct {
	*cli.BaseMiddleware
	metrics common.Metrics
	config  MetricsConfig
}

// MetricsConfig contains metrics middleware configuration
type MetricsConfig struct {
	CountCommands   bool     `yaml:"count_commands" json:"count_commands"`
	TrackDuration   bool     `yaml:"track_duration" json:"track_duration"`
	TrackErrors     bool     `yaml:"track_errors" json:"track_errors"`
	TrackSuccess    bool     `yaml:"track_success" json:"track_success"`
	CustomTags      []string `yaml:"custom_tags" json:"custom_tags"`
	ExcludeCommands []string `yaml:"exclude_commands" json:"exclude_commands"`
	MetricPrefix    string   `yaml:"metric_prefix" json:"metric_prefix"`
	TrackMemory     bool     `yaml:"track_memory" json:"track_memory"`
	TrackArgs       bool     `yaml:"track_args" json:"track_args"`
	SampleRate      float64  `yaml:"sample_rate" json:"sample_rate"`
}

// NewMetricsMiddleware creates a new metrics middleware
func NewMetricsMiddleware(metrics common.Metrics) cli.CLIMiddleware {
	return NewMetricsMiddlewareWithConfig(metrics, DefaultMetricsConfig())
}

// NewMetricsMiddlewareWithConfig creates metrics middleware with custom config
func NewMetricsMiddlewareWithConfig(metrics common.Metrics, config MetricsConfig) cli.CLIMiddleware {
	return &MetricsMiddleware{
		BaseMiddleware: cli.NewBaseMiddleware("metrics", 20),
		metrics:        metrics,
		config:         config,
	}
}

// Execute executes the metrics middleware
func (mm *MetricsMiddleware) Execute(ctx cli.CLIContext, next func() error) error {
	// Skip if command is excluded
	if mm.shouldExcludeCommand(ctx) {
		return next()
	}

	// Skip based on sample rate
	if !mm.shouldSample() {
		return next()
	}

	commandName := ctx.Command().Name()
	tags := mm.buildTags(ctx)

	// Track command start
	start := time.Now()
	if mm.config.CountCommands {
		mm.metrics.Counter(mm.metricName("commands.started"), tags...).Inc()
	}

	// Track memory usage if enabled
	var initialMemory *MemoryStats
	if mm.config.TrackMemory {
		initialMemory = GetMemoryStats()
	}

	// Execute command
	err := next()
	duration := time.Since(start)

	// Track completion metrics
	mm.trackCompletion(commandName, err, duration, tags)

	// Track memory usage if enabled
	if mm.config.TrackMemory && initialMemory != nil {
		mm.trackMemoryUsage(commandName, initialMemory, tags)
	}

	return err
}

// trackCompletion tracks command completion metrics
func (mm *MetricsMiddleware) trackCompletion(commandName string, err error, duration time.Duration, tags []string) {
	// Track duration
	if mm.config.TrackDuration {
		mm.metrics.Timer(mm.metricName("commands.duration"), tags...).Record(duration)
		mm.metrics.Histogram(mm.metricName("commands.duration.histogram"), tags...).Observe(duration.Seconds())
	}

	// Track success/error
	if err != nil {
		if mm.config.TrackErrors {
			errorTags := append(tags, "status:error")
			mm.metrics.Counter(mm.metricName("commands.completed"), errorTags...).Inc()
			mm.metrics.Counter(mm.metricName("commands.errors"), tags...).Inc()
		}
	} else {
		if mm.config.TrackSuccess {
			successTags := append(tags, "status:success")
			mm.metrics.Counter(mm.metricName("commands.completed"), successTags...).Inc()
			mm.metrics.Counter(mm.metricName("commands.success"), tags...).Inc()
		}
	}

	// Track command completion regardless of status
	if mm.config.CountCommands {
		mm.metrics.Counter(mm.metricName("commands.total"), tags...).Inc()
	}
}

// trackMemoryUsage tracks memory usage metrics
func (mm *MetricsMiddleware) trackMemoryUsage(commandName string, initial *MemoryStats, tags []string) {
	final := GetMemoryStats()

	memoryUsed := final.HeapAlloc - initial.HeapAlloc
	mm.metrics.Histogram(mm.metricName("commands.memory.used"), tags...).Observe(float64(memoryUsed))

	if final.HeapAlloc > initial.HeapAlloc {
		mm.metrics.Gauge(mm.metricName("commands.memory.peak"), tags...).Set(float64(final.HeapAlloc))
	}
}

// shouldExcludeCommand checks if command should be excluded from metrics
func (mm *MetricsMiddleware) shouldExcludeCommand(ctx cli.CLIContext) bool {
	commandName := ctx.Command().Name()
	for _, excluded := range mm.config.ExcludeCommands {
		if excluded == commandName {
			return true
		}
	}
	return false
}

// shouldSample determines if this execution should be sampled
func (mm *MetricsMiddleware) shouldSample() bool {
	if mm.config.SampleRate <= 0 {
		return false
	}
	if mm.config.SampleRate >= 1.0 {
		return true
	}

	// Simple sampling logic - in production you might want something more sophisticated
	return time.Now().UnixNano()%1000 < int64(mm.config.SampleRate*1000)
}

// buildTags builds metric tags from context
func (mm *MetricsMiddleware) buildTags(ctx cli.CLIContext) []string {
	tags := []string{
		"command:" + ctx.Command().Name(),
	}

	// Add custom tags from context
	for _, tagKey := range mm.config.CustomTags {
		if value := ctx.Get(tagKey); value != nil {
			tags = append(tags, tagKey+":"+fmt.Sprintf("%v", value))
		}
	}

	// Add argument count if tracking args
	if mm.config.TrackArgs {
		argCount := len(ctx.Args())
		tags = append(tags, fmt.Sprintf("arg_count:%d", argCount))
	}

	// Add user context if available
	if userID := ctx.Get("user_id"); userID != nil {
		tags = append(tags, "user_id:"+userID.(string))
	}

	return tags
}

// metricName builds a metric name with optional prefix
func (mm *MetricsMiddleware) metricName(name string) string {
	if mm.config.MetricPrefix != "" {
		return mm.config.MetricPrefix + "." + name
	}
	return name
}

// MemoryStats represents memory statistics
type MemoryStats struct {
	HeapAlloc    uint64
	HeapSys      uint64
	HeapIdle     uint64
	HeapInuse    uint64
	HeapReleased uint64
	StackInuse   uint64
	StackSys     uint64
}

// GetMemoryStats returns current memory statistics
func GetMemoryStats() *MemoryStats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return &MemoryStats{
		HeapAlloc:    m.HeapAlloc,
		HeapSys:      m.HeapSys,
		HeapIdle:     m.HeapIdle,
		HeapInuse:    m.HeapInuse,
		HeapReleased: m.HeapReleased,
		StackInuse:   m.StackInuse,
		StackSys:     m.StackSys,
	}
}

// DefaultMetricsConfig returns default metrics configuration
func DefaultMetricsConfig() MetricsConfig {
	return MetricsConfig{
		CountCommands:   true,
		TrackDuration:   true,
		TrackErrors:     true,
		TrackSuccess:    true,
		CustomTags:      []string{},
		ExcludeCommands: []string{"completion", "help"},
		MetricPrefix:    "cli",
		TrackMemory:     false,
		TrackArgs:       true,
		SampleRate:      1.0,
	}
}

// DetailedMetricsConfig returns configuration for detailed metrics
func DetailedMetricsConfig() MetricsConfig {
	config := DefaultMetricsConfig()
	config.TrackMemory = true
	config.CustomTags = []string{"user_id", "request_id", "session_id"}
	return config
}

// SampledMetricsConfig returns configuration with sampling
func SampledMetricsConfig(sampleRate float64) MetricsConfig {
	config := DefaultMetricsConfig()
	config.SampleRate = sampleRate
	return config
}
