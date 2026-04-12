package collector

import (
	"sync"
	"time"

	internalmetrics "github.com/xraph/forge/internal/metrics"
)

// DataHistory manages historical time-series data with a ring buffer.
// Overview and health snapshots are stored locally. Metric time-series
// data is delegated to the metrics package's TimeSeriesQueryProvider.
type DataHistory struct {
	maxPoints       int
	retentionPeriod time.Duration

	overview []OverviewSnapshot
	health   []HealthSnapshot

	// tsProvider is used for metric time-series queries. May be nil.
	tsProvider internalmetrics.TimeSeriesQueryProvider

	mu sync.RWMutex
}

// OverviewSnapshot is a timestamped overview snapshot.
type OverviewSnapshot struct {
	Timestamp    time.Time `json:"timestamp"`
	HealthStatus string    `json:"health_status"`
	ServiceCount int       `json:"service_count"`
	HealthyCount int       `json:"healthy_count"`
	MetricsCount int       `json:"metrics_count"`
}

// HealthSnapshot is a timestamped health snapshot.
type HealthSnapshot struct {
	Timestamp      time.Time `json:"timestamp"`
	OverallStatus  string    `json:"overall_status"`
	HealthyCount   int       `json:"healthy_count"`
	DegradedCount  int       `json:"degraded_count"`
	UnhealthyCount int       `json:"unhealthy_count"`
}

// NewDataHistory creates a new data history manager.
// tsProvider is optional and used for metric time-series queries.
func NewDataHistory(maxPoints int, retentionPeriod time.Duration, tsProvider internalmetrics.TimeSeriesQueryProvider) *DataHistory {
	return &DataHistory{
		maxPoints:       maxPoints,
		retentionPeriod: retentionPeriod,
		overview:        make([]OverviewSnapshot, 0, maxPoints),
		health:          make([]HealthSnapshot, 0, maxPoints),
		tsProvider:      tsProvider,
	}
}

// AddOverview adds an overview snapshot.
func (dh *DataHistory) AddOverview(snapshot OverviewSnapshot) {
	dh.mu.Lock()
	defer dh.mu.Unlock()

	dh.overview = append(dh.overview, snapshot)
	dh.trimOverview()
}

// AddHealth adds a health snapshot.
func (dh *DataHistory) AddHealth(snapshot HealthSnapshot) {
	dh.mu.Lock()
	defer dh.mu.Unlock()

	dh.health = append(dh.health, snapshot)
	dh.trimHealth()
}

// GetOverview returns all overview snapshots.
func (dh *DataHistory) GetOverview() []OverviewSnapshot {
	dh.mu.RLock()
	defer dh.mu.RUnlock()

	result := make([]OverviewSnapshot, len(dh.overview))
	copy(result, dh.overview)

	return result
}

// GetHealth returns all health snapshots.
func (dh *DataHistory) GetHealth() []HealthSnapshot {
	dh.mu.RLock()
	defer dh.mu.RUnlock()

	result := make([]HealthSnapshot, len(dh.health))
	copy(result, dh.health)

	return result
}

// GetAll returns all historical data.
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

// GetMetricTimeSeries returns time-series data for a single metric by
// querying the metrics package's time-series storage directly.
func (dh *DataHistory) GetMetricTimeSeries(metricName string) []DataPoint {
	if dh.tsProvider == nil {
		return nil
	}

	end := time.Now()
	start := end.Add(-dh.retentionPeriod)
	tsPoints := dh.tsProvider.QueryMetricRange(metricName, start, end)

	if len(tsPoints) == 0 {
		return nil
	}

	points := make([]DataPoint, len(tsPoints))
	for i, p := range tsPoints {
		points[i] = DataPoint{
			Timestamp: p.Timestamp,
			Value:     p.Value,
		}
	}

	return points
}

// Clear clears all historical data.
func (dh *DataHistory) Clear() {
	dh.mu.Lock()
	defer dh.mu.Unlock()

	dh.overview = dh.overview[:0]
	dh.health = dh.health[:0]
}

// trimOverview trims old overview data based on retention policy.
// It copies remaining data to a new slice to release the old backing array.
func (dh *DataHistory) trimOverview() {
	cutoff := time.Now().Add(-dh.retentionPeriod)

	i := 0
	for i < len(dh.overview) && dh.overview[i].Timestamp.Before(cutoff) {
		i++
	}

	if len(dh.overview)-i > dh.maxPoints {
		i = len(dh.overview) - dh.maxPoints
	}

	if i > 0 {
		remaining := len(dh.overview) - i
		newSlice := make([]OverviewSnapshot, remaining, dh.maxPoints)
		copy(newSlice, dh.overview[i:])
		dh.overview = newSlice
	}
}

// trimHealth trims old health data based on retention policy.
func (dh *DataHistory) trimHealth() {
	cutoff := time.Now().Add(-dh.retentionPeriod)

	i := 0
	for i < len(dh.health) && dh.health[i].Timestamp.Before(cutoff) {
		i++
	}

	if len(dh.health)-i > dh.maxPoints {
		i = len(dh.health) - dh.maxPoints
	}

	if i > 0 {
		remaining := len(dh.health) - i
		newSlice := make([]HealthSnapshot, remaining, dh.maxPoints)
		copy(newSlice, dh.health[i:])
		dh.health = newSlice
	}
}

// buildTimeSeries converts snapshots to time series format.
func (dh *DataHistory) buildTimeSeries() []TimeSeriesData {
	series := make([]TimeSeriesData, 0)

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
