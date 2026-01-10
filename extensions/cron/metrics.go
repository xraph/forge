package cron

import (
	"context"
	"maps"
	"sync"
	"time"

	"github.com/xraph/forge"
)

// MetricsCollector collects and exposes metrics for the cron extension.
type MetricsCollector struct {
	metrics forge.Metrics
	logger  forge.Logger

	// Metric counters
	jobsTotal          int64
	executionsTotal    map[string]int64 // status -> count
	executionDurations []time.Duration
	schedulerLag       []time.Duration
	queueSize          int64
	leaderStatus       int64 // 0 or 1
	mu                 sync.RWMutex
}

// NewMetricsCollector creates a new metrics collector.
func NewMetricsCollector(metrics forge.Metrics, logger forge.Logger) *MetricsCollector {
	return &MetricsCollector{
		metrics:            metrics,
		logger:             logger,
		executionsTotal:    make(map[string]int64),
		executionDurations: make([]time.Duration, 0),
		schedulerLag:       make([]time.Duration, 0),
	}
}

// RecordJobRegistered records that a job was registered.
func (m *MetricsCollector) RecordJobRegistered() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.jobsTotal++

	// TODO: Report to metrics when Metrics interface supports Inc method
	// if m.metrics != nil {
	// 	m.metrics.Inc("cron_jobs_total", forge.F("type", "registered"))
	// }
}

// RecordJobUnregistered records that a job was unregistered.
func (m *MetricsCollector) RecordJobUnregistered() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.jobsTotal > 0 {
		m.jobsTotal--
	}

	// TODO: Report to metrics when Metrics interface supports Inc method
}

// RecordExecution records a job execution completion.
func (m *MetricsCollector) RecordExecution(jobID, jobName string, status ExecutionStatus, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Increment execution counter
	m.executionsTotal[string(status)]++

	// Track duration
	if duration > 0 {
		m.executionDurations = append(m.executionDurations, duration)

		// Keep only last 1000 durations for memory efficiency
		if len(m.executionDurations) > 1000 {
			m.executionDurations = m.executionDurations[len(m.executionDurations)-1000:]
		}
	}

	// TODO: Report to metrics when Metrics interface supports these methods
}

// RecordSchedulerLag records the lag between scheduled and actual execution time.
func (m *MetricsCollector) RecordSchedulerLag(jobID string, lag time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.schedulerLag = append(m.schedulerLag, lag)

	// Keep only last 1000 lag measurements
	if len(m.schedulerLag) > 1000 {
		m.schedulerLag = m.schedulerLag[len(m.schedulerLag)-1000:]
	}

	// TODO: Report to metrics when Metrics interface supports Observe method
}

// RecordQueueSize records the current executor queue size.
func (m *MetricsCollector) RecordQueueSize(size int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.queueSize = size

	// TODO: Report to metrics when Metrics interface supports Set method
}

// RecordLeaderStatus records the leader status (0 = follower, 1 = leader).
func (m *MetricsCollector) RecordLeaderStatus(isLeader bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if isLeader {
		m.leaderStatus = 1
	} else {
		m.leaderStatus = 0
	}

	// TODO: Report to metrics when Metrics interface supports Set method
}

// GetJobsTotal returns the total number of registered jobs.
func (m *MetricsCollector) GetJobsTotal() int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.jobsTotal
}

// GetExecutionsTotal returns the total executions by status.
func (m *MetricsCollector) GetExecutionsTotal() map[string]int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]int64)
	maps.Copy(result, m.executionsTotal)

	return result
}

// GetAverageExecutionDuration returns the average execution duration.
func (m *MetricsCollector) GetAverageExecutionDuration() time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.executionDurations) == 0 {
		return 0
	}

	var total time.Duration
	for _, d := range m.executionDurations {
		total += d
	}

	return total / time.Duration(len(m.executionDurations))
}

// GetAverageSchedulerLag returns the average scheduler lag.
func (m *MetricsCollector) GetAverageSchedulerLag() time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.schedulerLag) == 0 {
		return 0
	}

	var total time.Duration
	for _, lag := range m.schedulerLag {
		total += lag
	}

	return total / time.Duration(len(m.schedulerLag))
}

// GetQueueSize returns the current executor queue size.
func (m *MetricsCollector) GetQueueSize() int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.queueSize
}

// GetLeaderStatus returns the current leader status.
func (m *MetricsCollector) GetLeaderStatus() int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.leaderStatus
}

// Reset resets all metrics.
func (m *MetricsCollector) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.jobsTotal = 0
	m.executionsTotal = make(map[string]int64)
	m.executionDurations = make([]time.Duration, 0)
	m.schedulerLag = make([]time.Duration, 0)
	m.queueSize = 0
	m.leaderStatus = 0
}

// GetMetricsSummary returns a summary of all metrics.
func (m *MetricsCollector) GetMetricsSummary() map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return map[string]any{
		"jobs_total":                 m.jobsTotal,
		"executions_total":           m.executionsTotal,
		"average_execution_duration": m.GetAverageExecutionDuration().String(),
		"average_scheduler_lag":      m.GetAverageSchedulerLag().String(),
		"queue_size":                 m.queueSize,
		"leader_status":              m.leaderStatus,
	}
}

// ExportMetrics exports metrics to the provided metrics system.
// This can be called periodically to update gauge metrics.
func (m *MetricsCollector) ExportMetrics(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.metrics == nil {
		return nil
	}

	// TODO: Export gauge metrics when Metrics interface supports Set method
	// m.metrics.Set("cron_jobs_registered", float64(m.jobsTotal))
	// m.metrics.Set("cron_executor_queue_size", float64(m.queueSize))
	// m.metrics.Set("cron_leader_status", float64(m.leaderStatus))

	return nil
}
