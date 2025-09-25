package monitoring

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/xraph/forge/pkg/common"
	cron "github.com/xraph/forge/pkg/cron/core"
)

// MetricsMiddleware provides metrics collection for job execution
type MetricsMiddleware struct {
	metrics              common.Metrics
	logger               common.Logger
	collectSystemMetrics bool
	collectCustomMetrics bool
	collectTiming        bool
	collectMemory        bool
	collectCPU           bool
	tagExtractor         func(*cron.Job) map[string]string
	metricPrefix         string
}

// MetricsConfig contains configuration for metrics middleware
type MetricsConfig struct {
	CollectSystemMetrics bool                              `json:"collect_system_metrics" yaml:"collect_system_metrics"`
	CollectCustomMetrics bool                              `json:"collect_custom_metrics" yaml:"collect_custom_metrics"`
	CollectTiming        bool                              `json:"collect_timing" yaml:"collect_timing"`
	CollectMemory        bool                              `json:"collect_memory" yaml:"collect_memory"`
	CollectCPU           bool                              `json:"collect_cpu" yaml:"collect_cpu"`
	TagExtractor         func(*cron.Job) map[string]string `json:"-" yaml:"-"`
	MetricPrefix         string                            `json:"metric_prefix" yaml:"metric_prefix"`
}

// JobMetrics holds metrics for a job execution
type JobMetrics struct {
	StartTime       time.Time
	EndTime         time.Time
	Duration        time.Duration
	MemoryStart     runtime.MemStats
	MemoryEnd       runtime.MemStats
	CPUStart        time.Duration
	CPUEnd          time.Duration
	GoroutinesStart int
	GoroutinesEnd   int
	CustomMetrics   map[string]interface{}
	Success         bool
	Error           error
	Attempt         int
	NodeID          string
	ExecutionID     string
}

// NewMetricsMiddleware creates a new metrics middleware
func NewMetricsMiddleware(metrics common.Metrics, logger common.Logger, config *MetricsConfig) *MetricsMiddleware {
	if config == nil {
		config = &MetricsConfig{
			CollectSystemMetrics: true,
			CollectCustomMetrics: true,
			CollectTiming:        true,
			CollectMemory:        true,
			CollectCPU:           true,
			MetricPrefix:         "forge.cron",
		}
	}

	return &MetricsMiddleware{
		metrics:              metrics,
		logger:               logger,
		collectSystemMetrics: config.CollectSystemMetrics,
		collectCustomMetrics: config.CollectCustomMetrics,
		collectTiming:        config.CollectTiming,
		collectMemory:        config.CollectMemory,
		collectCPU:           config.CollectCPU,
		tagExtractor:         config.TagExtractor,
		metricPrefix:         config.MetricPrefix,
	}
}

// Handle handles job execution with metrics collection
func (mm *MetricsMiddleware) Handle(ctx context.Context, job *cron.Job, next func(ctx context.Context, job *cron.Job) error) error {
	// OnStart collecting metrics
	jobMetrics := mm.startMetricsCollection(job)

	// Execute the job
	err := next(ctx, job)

	// Complete metrics collection
	mm.completeMetricsCollection(job, jobMetrics, err)

	// Record metrics
	mm.recordMetrics(job, jobMetrics)

	return err
}

// startMetricsCollection starts collecting metrics for a job
func (mm *MetricsMiddleware) startMetricsCollection(job *cron.Job) *JobMetrics {
	metrics := &JobMetrics{
		StartTime:     time.Now(),
		CustomMetrics: make(map[string]interface{}),
	}

	// Collect system metrics if enabled
	if mm.collectSystemMetrics {
		metrics.GoroutinesStart = runtime.NumGoroutine()

		if mm.collectMemory {
			runtime.GC()
			runtime.ReadMemStats(&metrics.MemoryStart)
		}

		if mm.collectCPU {
			// In a real implementation, you would collect CPU usage here
			// For now, we'll use a placeholder
			metrics.CPUStart = time.Duration(0)
		}
	}

	// Extract execution context if available
	if job.CurrentExecution != nil {
		metrics.ExecutionID = job.CurrentExecution.ID
		metrics.NodeID = job.CurrentExecution.NodeID
		metrics.Attempt = job.CurrentExecution.Attempt
	}

	return metrics
}

// completeMetricsCollection completes metrics collection for a job
func (mm *MetricsMiddleware) completeMetricsCollection(job *cron.Job, jobMetrics *JobMetrics, err error) {
	jobMetrics.EndTime = time.Now()
	jobMetrics.Duration = jobMetrics.EndTime.Sub(jobMetrics.StartTime)
	jobMetrics.Success = (err == nil)
	jobMetrics.Error = err

	// Collect final system metrics
	if mm.collectSystemMetrics {
		jobMetrics.GoroutinesEnd = runtime.NumGoroutine()

		if mm.collectMemory {
			runtime.ReadMemStats(&jobMetrics.MemoryEnd)
		}

		if mm.collectCPU {
			// In a real implementation, you would collect CPU usage here
			jobMetrics.CPUEnd = time.Duration(0)
		}
	}
}

// recordMetrics records all collected metrics
func (mm *MetricsMiddleware) recordMetrics(job *cron.Job, jobMetrics *JobMetrics) {
	// Create base tags
	tags := mm.createBaseTags(job)

	// Add execution-specific tags
	if jobMetrics.NodeID != "" {
		tags = append(tags, "node_id", jobMetrics.NodeID)
	}
	if jobMetrics.Attempt > 0 {
		tags = append(tags, "attempt", fmt.Sprintf("%d", jobMetrics.Attempt))
	}

	// Add status tag
	status := "success"
	if !jobMetrics.Success {
		status = "failure"
	}
	tags = append(tags, "status", status)

	// Record execution metrics
	mm.recordExecutionMetrics(job, jobMetrics, tags)

	// Record timing metrics
	if mm.collectTiming {
		mm.recordTimingMetrics(job, jobMetrics, tags)
	}

	// Record system metrics
	if mm.collectSystemMetrics {
		mm.recordSystemMetrics(job, jobMetrics, tags)
	}

	// Record custom metrics
	if mm.collectCustomMetrics {
		mm.recordCustomMetrics(job, jobMetrics, tags)
	}
}

// recordExecutionMetrics records basic execution metrics
func (mm *MetricsMiddleware) recordExecutionMetrics(job *cron.Job, jobMetrics *JobMetrics, tags []string) {
	// Execution counter
	mm.metrics.Counter(mm.metricName("job.executions"), tags...).Inc()

	// Success/failure counters
	if jobMetrics.Success {
		mm.metrics.Counter(mm.metricName("job.executions.success"), tags...).Inc()
	} else {
		mm.metrics.Counter(mm.metricName("job.executions.failure"), tags...).Inc()

		// Error type counter
		if jobMetrics.Error != nil {
			errorType := fmt.Sprintf("%T", jobMetrics.Error)
			errorTags := append(tags, "error_type", errorType)
			mm.metrics.Counter(mm.metricName("job.executions.errors"), errorTags...).Inc()

			// Specific error counters
			if forgeErr, ok := jobMetrics.Error.(*common.ForgeError); ok {
				errorCodeTags := append(tags, "error_code", forgeErr.Code)
				mm.metrics.Counter(mm.metricName("job.executions.errors.by_code"), errorCodeTags...).Inc()
			}
		}
	}

	// Attempt counter
	if jobMetrics.Attempt > 1 {
		retryTags := append(tags, "retry", "true")
		mm.metrics.Counter(mm.metricName("job.executions.retries"), retryTags...).Inc()
	}
}

// recordTimingMetrics records timing-related metrics
func (mm *MetricsMiddleware) recordTimingMetrics(job *cron.Job, jobMetrics *JobMetrics, tags []string) {
	// Execution duration histogram
	mm.metrics.Histogram(mm.metricName("job.duration"), tags...).Observe(jobMetrics.Duration.Seconds())

	// Execution duration by percentile
	mm.metrics.Histogram(mm.metricName("job.duration.histogram"), tags...).Observe(jobMetrics.Duration.Seconds())

	// Duration gauge for current execution
	mm.metrics.Gauge(mm.metricName("job.duration.current"), tags...).Set(jobMetrics.Duration.Seconds())

	// Timing breakdown
	mm.metrics.Gauge(mm.metricName("job.timing.start_timestamp"), tags...).Set(float64(jobMetrics.StartTime.Unix()))
	mm.metrics.Gauge(mm.metricName("job.timing.end_timestamp"), tags...).Set(float64(jobMetrics.EndTime.Unix()))
	mm.metrics.Gauge(mm.metricName("job.timing.duration_ms"), tags...).Set(float64(jobMetrics.Duration.Milliseconds()))
	mm.metrics.Gauge(mm.metricName("job.timing.duration_us"), tags...).Set(float64(jobMetrics.Duration.Microseconds()))

	// Performance categorization
	performanceCategory := mm.categorizePerformance(jobMetrics.Duration)
	perfTags := append(tags, "performance", performanceCategory)
	mm.metrics.Counter(mm.metricName("job.executions.by_performance"), perfTags...).Inc()
}

// recordSystemMetrics records system resource metrics
func (mm *MetricsMiddleware) recordSystemMetrics(job *cron.Job, jobMetrics *JobMetrics, tags []string) {
	// Goroutine metrics
	mm.metrics.Gauge(mm.metricName("job.goroutines.start"), tags...).Set(float64(jobMetrics.GoroutinesStart))
	mm.metrics.Gauge(mm.metricName("job.goroutines.end"), tags...).Set(float64(jobMetrics.GoroutinesEnd))

	goroutineDiff := jobMetrics.GoroutinesEnd - jobMetrics.GoroutinesStart
	mm.metrics.Gauge(mm.metricName("job.goroutines.delta"), tags...).Set(float64(goroutineDiff))

	// Memory metrics
	if mm.collectMemory {
		mm.recordMemoryMetrics(job, jobMetrics, tags)
	}

	// CPU metrics
	if mm.collectCPU {
		mm.recordCPUMetrics(job, jobMetrics, tags)
	}
}

// recordMemoryMetrics records memory-related metrics
func (mm *MetricsMiddleware) recordMemoryMetrics(job *cron.Job, jobMetrics *JobMetrics, tags []string) {
	// Memory allocation metrics
	allocDiff := int64(jobMetrics.MemoryEnd.Alloc) - int64(jobMetrics.MemoryStart.Alloc)
	mm.metrics.Gauge(mm.metricName("job.memory.alloc.delta"), tags...).Set(float64(allocDiff))
	mm.metrics.Gauge(mm.metricName("job.memory.alloc.start"), tags...).Set(float64(jobMetrics.MemoryStart.Alloc))
	mm.metrics.Gauge(mm.metricName("job.memory.alloc.end"), tags...).Set(float64(jobMetrics.MemoryEnd.Alloc))

	// Total allocation metrics
	totalAllocDiff := int64(jobMetrics.MemoryEnd.TotalAlloc) - int64(jobMetrics.MemoryStart.TotalAlloc)
	mm.metrics.Gauge(mm.metricName("job.memory.total_alloc.delta"), tags...).Set(float64(totalAllocDiff))

	// System memory metrics
	sysDiff := int64(jobMetrics.MemoryEnd.Sys) - int64(jobMetrics.MemoryStart.Sys)
	mm.metrics.Gauge(mm.metricName("job.memory.sys.delta"), tags...).Set(float64(sysDiff))

	// Garbage collection metrics
	gcDiff := jobMetrics.MemoryEnd.NumGC - jobMetrics.MemoryStart.NumGC
	mm.metrics.Gauge(mm.metricName("job.memory.gc.cycles"), tags...).Set(float64(gcDiff))

	// Heap metrics
	heapAllocDiff := int64(jobMetrics.MemoryEnd.HeapAlloc) - int64(jobMetrics.MemoryStart.HeapAlloc)
	mm.metrics.Gauge(mm.metricName("job.memory.heap_alloc.delta"), tags...).Set(float64(heapAllocDiff))

	heapSysDiff := int64(jobMetrics.MemoryEnd.HeapSys) - int64(jobMetrics.MemoryStart.HeapSys)
	mm.metrics.Gauge(mm.metricName("job.memory.heap_sys.delta"), tags...).Set(float64(heapSysDiff))

	// Memory efficiency metrics
	if totalAllocDiff > 0 {
		memoryEfficiency := float64(allocDiff) / float64(totalAllocDiff)
		mm.metrics.Gauge(mm.metricName("job.memory.efficiency"), tags...).Set(memoryEfficiency)
	}

	// Memory pressure categorization
	memoryPressure := mm.categorizeMemoryPressure(allocDiff)
	memTags := append(tags, "memory_pressure", memoryPressure)
	mm.metrics.Counter(mm.metricName("job.executions.by_memory_pressure"), memTags...).Inc()
}

// recordCPUMetrics records CPU-related metrics
func (mm *MetricsMiddleware) recordCPUMetrics(job *cron.Job, jobMetrics *JobMetrics, tags []string) {
	// CPU time metrics
	cpuDiff := jobMetrics.CPUEnd - jobMetrics.CPUStart
	mm.metrics.Gauge(mm.metricName("job.cpu.time.delta"), tags...).Set(cpuDiff.Seconds())

	// CPU utilization as percentage of execution time
	if jobMetrics.Duration > 0 {
		cpuUtilization := cpuDiff.Seconds() / jobMetrics.Duration.Seconds()
		mm.metrics.Gauge(mm.metricName("job.cpu.utilization"), tags...).Set(cpuUtilization)
	}

	// CPU efficiency metrics
	if jobMetrics.Duration > 0 {
		cpuEfficiency := cpuDiff.Seconds() / jobMetrics.Duration.Seconds()
		mm.metrics.Gauge(mm.metricName("job.cpu.efficiency"), tags...).Set(cpuEfficiency)
	}
}

// recordCustomMetrics records custom metrics from job execution
func (mm *MetricsMiddleware) recordCustomMetrics(job *cron.Job, jobMetrics *JobMetrics, tags []string) {
	// Record custom metrics if available
	for key, value := range jobMetrics.CustomMetrics {
		customTags := append(tags, "metric", key)

		switch v := value.(type) {
		case int:
			mm.metrics.Gauge(mm.metricName("job.custom.int"), customTags...).Set(float64(v))
		case int64:
			mm.metrics.Gauge(mm.metricName("job.custom.int64"), customTags...).Set(float64(v))
		case float64:
			mm.metrics.Gauge(mm.metricName("job.custom.float64"), customTags...).Set(v)
		case float32:
			mm.metrics.Gauge(mm.metricName("job.custom.float32"), customTags...).Set(float64(v))
		case bool:
			boolValue := 0.0
			if v {
				boolValue = 1.0
			}
			mm.metrics.Gauge(mm.metricName("job.custom.bool"), customTags...).Set(boolValue)
		case time.Duration:
			mm.metrics.Gauge(mm.metricName("job.custom.duration"), customTags...).Set(v.Seconds())
		case string:
			// For string values, we create a counter
			stringTags := append(customTags, "value", v)
			mm.metrics.Counter(mm.metricName("job.custom.string"), stringTags...).Inc()
		}
	}
}

// OnSuccess records success metrics
func (mm *MetricsMiddleware) OnSuccess(ctx context.Context, job *cron.Job, execution *cron.JobExecution) {
	tags := mm.createBaseTags(job)
	tags = append(tags, "status", "success")

	if execution.NodeID != "" {
		tags = append(tags, "node_id", execution.NodeID)
	}

	// Record success metrics
	mm.metrics.Counter(mm.metricName("job.success"), tags...).Inc()
	mm.metrics.Histogram(mm.metricName("job.success.duration"), tags...).Observe(execution.Duration.Seconds())

	// Record attempt metrics
	if execution.Attempt > 1 {
		attemptTags := append(tags, "final_attempt", fmt.Sprintf("%d", execution.Attempt))
		mm.metrics.Counter(mm.metricName("job.success.after_retry"), attemptTags...).Inc()
	}
}

// OnFailure records failure metrics
func (mm *MetricsMiddleware) OnFailure(ctx context.Context, job *cron.Job, execution *cron.JobExecution, err error) {
	tags := mm.createBaseTags(job)
	tags = append(tags, "status", "failure")

	if execution.NodeID != "" {
		tags = append(tags, "node_id", execution.NodeID)
	}

	// Add error type
	if err != nil {
		errorType := fmt.Sprintf("%T", err)
		tags = append(tags, "error_type", errorType)

		if forgeErr, ok := err.(*common.ForgeError); ok {
			tags = append(tags, "error_code", forgeErr.Code)
		}
	}

	// Record failure metrics
	mm.metrics.Counter(mm.metricName("job.failure"), tags...).Inc()
	mm.metrics.Histogram(mm.metricName("job.failure.duration"), tags...).Observe(execution.Duration.Seconds())

	// Record attempt metrics
	attemptTags := append(tags, "attempt", fmt.Sprintf("%d", execution.Attempt))
	mm.metrics.Counter(mm.metricName("job.failure.by_attempt"), attemptTags...).Inc()
}

// OnTimeout records timeout metrics
func (mm *MetricsMiddleware) OnTimeout(ctx context.Context, job *cron.Job, execution *cron.JobExecution) {
	tags := mm.createBaseTags(job)
	tags = append(tags, "status", "timeout")

	if execution.NodeID != "" {
		tags = append(tags, "node_id", execution.NodeID)
	}

	// Record timeout metrics
	mm.metrics.Counter(mm.metricName("job.timeout"), tags...).Inc()
	mm.metrics.Histogram(mm.metricName("job.timeout.duration"), tags...).Observe(execution.Duration.Seconds())

	// Record timeout vs configured timeout
	if job.Definition.Timeout > 0 {
		timeoutRatio := execution.Duration.Seconds() / job.Definition.Timeout.Seconds()
		mm.metrics.Gauge(mm.metricName("job.timeout.ratio"), tags...).Set(timeoutRatio)
	}
}

// OnRetry records retry metrics
func (mm *MetricsMiddleware) OnRetry(ctx context.Context, job *cron.Job, execution *cron.JobExecution, attempt int) {
	tags := mm.createBaseTags(job)
	tags = append(tags, "attempt", fmt.Sprintf("%d", attempt))

	if execution.NodeID != "" {
		tags = append(tags, "node_id", execution.NodeID)
	}

	// Record retry metrics
	mm.metrics.Counter(mm.metricName("job.retry"), tags...).Inc()

	// Record retry by attempt number
	mm.metrics.Counter(mm.metricName("job.retry.by_attempt"), tags...).Inc()

	// Record retry rate
	if job.RunCount > 0 {
		retryRate := float64(attempt-1) / float64(job.RunCount)
		mm.metrics.Gauge(mm.metricName("job.retry.rate"), tags...).Set(retryRate)
	}
}

// createBaseTags creates base tags for a job
func (mm *MetricsMiddleware) createBaseTags(job *cron.Job) []string {
	tags := []string{
		"job_id", job.Definition.ID,
		"job_name", job.Definition.Name,
		"enabled", fmt.Sprintf("%t", job.Definition.Enabled),
		"singleton", fmt.Sprintf("%t", job.Definition.Singleton),
		"distributed", fmt.Sprintf("%t", job.Definition.Distributed),
		"priority", fmt.Sprintf("%d", job.Definition.Priority),
	}

	// Add job tags
	for key, value := range job.Definition.Tags {
		tags = append(tags, fmt.Sprintf("job_tag_%s", key), value)
	}

	// Add custom tags from extractor
	if mm.tagExtractor != nil {
		customTags := mm.tagExtractor(job)
		for key, value := range customTags {
			tags = append(tags, fmt.Sprintf("custom_%s", key), value)
		}
	}

	return tags
}

// metricName creates a metric name with prefix
func (mm *MetricsMiddleware) metricName(name string) string {
	if mm.metricPrefix != "" {
		return fmt.Sprintf("%s.%s", mm.metricPrefix, name)
	}
	return name
}

// categorizePerformance categorizes job performance based on duration
func (mm *MetricsMiddleware) categorizePerformance(duration time.Duration) string {
	switch {
	case duration < 100*time.Millisecond:
		return "very_fast"
	case duration < 1*time.Second:
		return "fast"
	case duration < 10*time.Second:
		return "normal"
	case duration < 60*time.Second:
		return "slow"
	default:
		return "very_slow"
	}
}

// categorizeMemoryPressure categorizes memory pressure based on allocation
func (mm *MetricsMiddleware) categorizeMemoryPressure(allocDiff int64) string {
	switch {
	case allocDiff < 0:
		return "negative" // Memory was freed
	case allocDiff < 1024*1024: // 1MB
		return "low"
	case allocDiff < 10*1024*1024: // 10MB
		return "medium"
	case allocDiff < 100*1024*1024: // 100MB
		return "high"
	default:
		return "very_high"
	}
}

// AddCustomMetric adds a custom metric to the current execution
func (mm *MetricsMiddleware) AddCustomMetric(ctx context.Context, key string, value interface{}) {
	// Try to get job metrics from context
	if jobMetrics, ok := ctx.Value("job_metrics").(*JobMetrics); ok {
		jobMetrics.CustomMetrics[key] = value
	}
}

// GetExecutionMetrics returns current execution metrics from context
func (mm *MetricsMiddleware) GetExecutionMetrics(ctx context.Context) *JobMetrics {
	if jobMetrics, ok := ctx.Value("job_metrics").(*JobMetrics); ok {
		return jobMetrics
	}
	return nil
}

// MetricsCollector provides a way to collect metrics during job execution
type MetricsCollector struct {
	middleware *MetricsMiddleware
	jobMetrics *JobMetrics
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector(middleware *MetricsMiddleware, jobMetrics *JobMetrics) *MetricsCollector {
	return &MetricsCollector{
		middleware: middleware,
		jobMetrics: jobMetrics,
	}
}

// RecordCounter records a counter metric
func (mc *MetricsCollector) RecordCounter(name string, tags ...string) {
	mc.middleware.metrics.Counter(mc.middleware.metricName(name), tags...).Inc()
}

// RecordGauge records a gauge metric
func (mc *MetricsCollector) RecordGauge(name string, value float64, tags ...string) {
	mc.middleware.metrics.Gauge(mc.middleware.metricName(name), tags...).Set(value)
}

// RecordHistogram records a histogram metric
func (mc *MetricsCollector) RecordHistogram(name string, value float64, tags ...string) {
	mc.middleware.metrics.Histogram(mc.middleware.metricName(name), tags...).Observe(value)
}

// RecordTimer records a timer metric
func (mc *MetricsCollector) RecordTimer(name string, duration time.Duration, tags ...string) {
	mc.middleware.metrics.Histogram(mc.middleware.metricName(name), tags...).Observe(duration.Seconds())
}

// AddCustomMetric adds a custom metric to the job execution
func (mc *MetricsCollector) AddCustomMetric(key string, value interface{}) {
	if mc.jobMetrics != nil {
		mc.jobMetrics.CustomMetrics[key] = value
	}
}

// GetCustomMetrics returns all custom metrics
func (mc *MetricsCollector) GetCustomMetrics() map[string]interface{} {
	if mc.jobMetrics != nil {
		return mc.jobMetrics.CustomMetrics
	}
	return nil
}
