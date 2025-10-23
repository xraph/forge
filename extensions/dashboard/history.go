package dashboard

import (
	"sync"
	"time"
)

// DataHistory manages historical time-series data with a ring buffer
type DataHistory struct {
	maxPoints       int
	retentionPeriod time.Duration

	overview []OverviewSnapshot
	health   []HealthSnapshot
	metrics  []MetricsSnapshot

	mu sync.RWMutex
}

// OverviewSnapshot is a timestamped overview snapshot
type OverviewSnapshot struct {
	Timestamp    time.Time `json:"timestamp"`
	HealthStatus string    `json:"health_status"`
	ServiceCount int       `json:"service_count"`
	HealthyCount int       `json:"healthy_count"`
	MetricsCount int       `json:"metrics_count"`
}

// HealthSnapshot is a timestamped health snapshot
type HealthSnapshot struct {
	Timestamp      time.Time `json:"timestamp"`
	OverallStatus  string    `json:"overall_status"`
	HealthyCount   int       `json:"healthy_count"`
	DegradedCount  int       `json:"degraded_count"`
	UnhealthyCount int       `json:"unhealthy_count"`
}

// MetricsSnapshot is a timestamped metrics snapshot
type MetricsSnapshot struct {
	Timestamp    time.Time              `json:"timestamp"`
	TotalMetrics int                    `json:"total_metrics"`
	Values       map[string]interface{} `json:"values"`
}

// NewDataHistory creates a new data history manager
func NewDataHistory(maxPoints int, retentionPeriod time.Duration) *DataHistory {
	return &DataHistory{
		maxPoints:       maxPoints,
		retentionPeriod: retentionPeriod,
		overview:        make([]OverviewSnapshot, 0, maxPoints),
		health:          make([]HealthSnapshot, 0, maxPoints),
		metrics:         make([]MetricsSnapshot, 0, maxPoints),
	}
}

// AddOverview adds an overview snapshot
func (dh *DataHistory) AddOverview(snapshot OverviewSnapshot) {
	dh.mu.Lock()
	defer dh.mu.Unlock()

	dh.overview = append(dh.overview, snapshot)
	dh.trimOverview()
}

// AddHealth adds a health snapshot
func (dh *DataHistory) AddHealth(snapshot HealthSnapshot) {
	dh.mu.Lock()
	defer dh.mu.Unlock()

	dh.health = append(dh.health, snapshot)
	dh.trimHealth()
}

// AddMetrics adds a metrics snapshot
func (dh *DataHistory) AddMetrics(snapshot MetricsSnapshot) {
	dh.mu.Lock()
	defer dh.mu.Unlock()

	dh.metrics = append(dh.metrics, snapshot)
	dh.trimMetrics()
}

// GetOverview returns all overview snapshots
func (dh *DataHistory) GetOverview() []OverviewSnapshot {
	dh.mu.RLock()
	defer dh.mu.RUnlock()

	result := make([]OverviewSnapshot, len(dh.overview))
	copy(result, dh.overview)
	return result
}

// GetHealth returns all health snapshots
func (dh *DataHistory) GetHealth() []HealthSnapshot {
	dh.mu.RLock()
	defer dh.mu.RUnlock()

	result := make([]HealthSnapshot, len(dh.health))
	copy(result, dh.health)
	return result
}

// GetMetrics returns all metrics snapshots
func (dh *DataHistory) GetMetrics() []MetricsSnapshot {
	dh.mu.RLock()
	defer dh.mu.RUnlock()

	result := make([]MetricsSnapshot, len(dh.metrics))
	copy(result, dh.metrics)
	return result
}

// GetAll returns all historical data
func (dh *DataHistory) GetAll() HistoryData {
	dh.mu.RLock()
	defer dh.mu.RUnlock()

	var startTime, endTime time.Time

	if len(dh.overview) > 0 {
		startTime = dh.overview[0].Timestamp
		endTime = dh.overview[len(dh.overview)-1].Timestamp
	}

	return HistoryData{
		StartTime: startTime,
		EndTime:   endTime,
		Series:    dh.buildTimeSeries(),
	}
}

// Clear clears all historical data
func (dh *DataHistory) Clear() {
	dh.mu.Lock()
	defer dh.mu.Unlock()

	dh.overview = dh.overview[:0]
	dh.health = dh.health[:0]
	dh.metrics = dh.metrics[:0]
}

// trimOverview trims old overview data based on retention policy
func (dh *DataHistory) trimOverview() {
	now := time.Now()
	cutoff := now.Add(-dh.retentionPeriod)

	// Remove old data based on time
	i := 0
	for i < len(dh.overview) && dh.overview[i].Timestamp.Before(cutoff) {
		i++
	}
	if i > 0 {
		dh.overview = dh.overview[i:]
	}

	// Enforce max points limit
	if len(dh.overview) > dh.maxPoints {
		excess := len(dh.overview) - dh.maxPoints
		dh.overview = dh.overview[excess:]
	}
}

// trimHealth trims old health data based on retention policy
func (dh *DataHistory) trimHealth() {
	now := time.Now()
	cutoff := now.Add(-dh.retentionPeriod)

	i := 0
	for i < len(dh.health) && dh.health[i].Timestamp.Before(cutoff) {
		i++
	}
	if i > 0 {
		dh.health = dh.health[i:]
	}

	if len(dh.health) > dh.maxPoints {
		excess := len(dh.health) - dh.maxPoints
		dh.health = dh.health[excess:]
	}
}

// trimMetrics trims old metrics data based on retention policy
func (dh *DataHistory) trimMetrics() {
	now := time.Now()
	cutoff := now.Add(-dh.retentionPeriod)

	i := 0
	for i < len(dh.metrics) && dh.metrics[i].Timestamp.Before(cutoff) {
		i++
	}
	if i > 0 {
		dh.metrics = dh.metrics[i:]
	}

	if len(dh.metrics) > dh.maxPoints {
		excess := len(dh.metrics) - dh.maxPoints
		dh.metrics = dh.metrics[excess:]
	}
}

// buildTimeSeries converts snapshots to time series format
func (dh *DataHistory) buildTimeSeries() []TimeSeriesData {
	series := make([]TimeSeriesData, 0)

	// Service count time series
	if len(dh.overview) > 0 {
		points := make([]DataPoint, len(dh.overview))
		for i, snap := range dh.overview {
			points[i] = DataPoint{
				Timestamp: snap.Timestamp,
				Value:     float64(snap.ServiceCount),
			}
		}
		series = append(series, TimeSeriesData{
			Name:   "service_count",
			Points: points,
			Unit:   "count",
		})

		// Healthy service count
		healthyPoints := make([]DataPoint, len(dh.overview))
		for i, snap := range dh.overview {
			healthyPoints[i] = DataPoint{
				Timestamp: snap.Timestamp,
				Value:     float64(snap.HealthyCount),
			}
		}
		series = append(series, TimeSeriesData{
			Name:   "healthy_services",
			Points: healthyPoints,
			Unit:   "count",
		})
	}

	return series
}
