package storage

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/logger"
	metrics "github.com/xraph/forge/v0/pkg/metrics/core"
)

// =============================================================================
// TIME-SERIES STORAGE
// =============================================================================

// TimeSeriesStorage provides time-series storage for metrics with efficient time-based operations
type TimeSeriesStorage struct {
	name               string
	logger             logger.Logger
	mu                 sync.RWMutex
	series             map[string]*TimeSeries
	retention          time.Duration
	resolution         time.Duration
	maxSeries          int
	maxPointsPerSeries int
	compressionEnabled bool
	compressionDelay   time.Duration
	cleanupInterval    time.Duration
	started            bool
	stopCh             chan struct{}
	stats              *TimeSeriesStorageStats
}

// TimeSeries represents a time-series of metric data points
type TimeSeries struct {
	Name        string                 `json:"name"`
	Tags        map[string]string      `json:"tags"`
	Type        metrics.MetricType     `json:"type"`
	Unit        string                 `json:"unit"`
	Points      []*TimeSeriesPoint     `json:"points"`
	Buckets     map[int64]*TimeWindow  `json:"buckets"`
	Created     time.Time              `json:"created"`
	Updated     time.Time              `json:"updated"`
	AccessCount int64                  `json:"access_count"`
	Metadata    map[string]interface{} `json:"metadata"`
	mu          sync.RWMutex
}

// TimeSeriesPoint represents a single data point in a time series
type TimeSeriesPoint struct {
	Timestamp time.Time              `json:"timestamp"`
	Value     float64                `json:"value"`
	Tags      map[string]string      `json:"tags,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// TimeWindow represents aggregated data for a time window
type TimeWindow struct {
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	Count      int64     `json:"count"`
	Sum        float64   `json:"sum"`
	Min        float64   `json:"min"`
	Max        float64   `json:"max"`
	Mean       float64   `json:"mean"`
	StdDev     float64   `json:"stddev"`
	Compressed bool      `json:"compressed"`
}

// TimeSeriesStorageConfig contains configuration for time-series storage
type TimeSeriesStorageConfig struct {
	Retention          time.Duration `yaml:"retention" json:"retention"`
	Resolution         time.Duration `yaml:"resolution" json:"resolution"`
	MaxSeries          int           `yaml:"max_series" json:"max_series"`
	MaxPointsPerSeries int           `yaml:"max_points_per_series" json:"max_points_per_series"`
	CompressionEnabled bool          `yaml:"compression_enabled" json:"compression_enabled"`
	CompressionDelay   time.Duration `yaml:"compression_delay" json:"compression_delay"`
	CleanupInterval    time.Duration `yaml:"cleanup_interval" json:"cleanup_interval"`
	EnableStats        bool          `yaml:"enable_stats" json:"enable_stats"`
}

// TimeSeriesStorageStats contains statistics about time-series storage
type TimeSeriesStorageStats struct {
	SeriesCount       int64         `json:"series_count"`
	TotalPoints       int64         `json:"total_points"`
	CompressedPoints  int64         `json:"compressed_points"`
	TotalWrites       int64         `json:"total_writes"`
	TotalReads        int64         `json:"total_reads"`
	TotalDeletes      int64         `json:"total_deletes"`
	CompressionRuns   int64         `json:"compression_runs"`
	CleanupRuns       int64         `json:"cleanup_runs"`
	LastCleanup       time.Time     `json:"last_cleanup"`
	LastCompression   time.Time     `json:"last_compression"`
	AverageSeriesSize float64       `json:"average_series_size"`
	OldestPoint       time.Time     `json:"oldest_point"`
	NewestPoint       time.Time     `json:"newest_point"`
	CompressionRatio  float64       `json:"compression_ratio"`
	MemoryUsage       int64         `json:"memory_usage"`
	Uptime            time.Duration `json:"uptime"`
	startTime         time.Time
}

// DefaultTimeSeriesStorageConfig returns default configuration
func DefaultTimeSeriesStorageConfig() *TimeSeriesStorageConfig {
	return &TimeSeriesStorageConfig{
		Retention:          time.Hour * 24 * 7, // 7 days
		Resolution:         time.Minute,        // 1 minute resolution
		MaxSeries:          10000,
		MaxPointsPerSeries: 10080, // 7 days at 1 minute resolution
		CompressionEnabled: true,
		CompressionDelay:   time.Hour * 6, // Compress after 6 hours
		CleanupInterval:    time.Hour,
		EnableStats:        true,
	}
}

// NewTimeSeriesStorage creates a new time-series storage instance
func NewTimeSeriesStorage() MetricsStorage {
	return NewTimeSeriesStorageWithConfig(DefaultTimeSeriesStorageConfig())
}

// NewTimeSeriesStorageWithConfig creates a new time-series storage instance with configuration
func NewTimeSeriesStorageWithConfig(config *TimeSeriesStorageConfig) *TimeSeriesStorage {
	return &TimeSeriesStorage{
		name:               "timeseries",
		series:             make(map[string]*TimeSeries),
		retention:          config.Retention,
		resolution:         config.Resolution,
		maxSeries:          config.MaxSeries,
		maxPointsPerSeries: config.MaxPointsPerSeries,
		compressionEnabled: config.CompressionEnabled,
		compressionDelay:   config.CompressionDelay,
		cleanupInterval:    config.CleanupInterval,
		stopCh:             make(chan struct{}),
		stats: &TimeSeriesStorageStats{
			startTime: time.Now(),
		},
	}
}

// =============================================================================
// METRICS STORAGE INTERFACE IMPLEMENTATION
// =============================================================================

// Store stores a metric entry as a time-series point
func (ts *TimeSeriesStorage) Store(ctx context.Context, entry *MetricEntry) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	// Check if we're at capacity
	if len(ts.series) >= ts.maxSeries {
		if err := ts.evictOldestSeries(); err != nil {
			return fmt.Errorf("failed to evict oldest series: %w", err)
		}
	}

	// Generate series key
	seriesKey := ts.generateSeriesKey(entry.Name, entry.Tags)

	// Get or create series
	series, exists := ts.series[seriesKey]
	if !exists {
		series = &TimeSeries{
			Name:     entry.Name,
			Tags:     make(map[string]string),
			Type:     entry.Type,
			Points:   make([]*TimeSeriesPoint, 0),
			Buckets:  make(map[int64]*TimeWindow),
			Created:  time.Now(),
			Metadata: make(map[string]interface{}),
		}

		// Copy tags
		for k, v := range entry.Tags {
			series.Tags[k] = v
		}

		ts.series[seriesKey] = series
	}

	// Extract numeric value
	value, ok := ts.extractNumericValue(entry.Value)
	if !ok {
		return fmt.Errorf("cannot store non-numeric value in time series: %T", entry.Value)
	}

	// Create time-series point
	point := &TimeSeriesPoint{
		Timestamp: entry.Timestamp,
		Value:     value,
		Tags:      make(map[string]string),
		Metadata:  make(map[string]interface{}),
	}

	// Copy point-specific tags and metadata if any
	if entry.Metadata != nil {
		for k, v := range entry.Metadata {
			point.Metadata[k] = v
		}
	}

	// Add point to series
	series.mu.Lock()
	series.Points = append(series.Points, point)
	series.Updated = time.Now()
	series.AccessCount++

	// Sort points by timestamp
	sort.Slice(series.Points, func(i, j int) bool {
		return series.Points[i].Timestamp.Before(series.Points[j].Timestamp)
	})

	// Check if series is getting too large
	if len(series.Points) > ts.maxPointsPerSeries {
		// Remove oldest points
		excess := len(series.Points) - ts.maxPointsPerSeries
		series.Points = series.Points[excess:]
	}

	series.mu.Unlock()

	// Update time-based bucket for efficient querying
	ts.updateBucket(series, point)

	// Update stats
	ts.stats.TotalWrites++
	ts.stats.TotalPoints++
	ts.updateStorageStats()

	return nil
}

// Retrieve retrieves a metric entry (latest point) from time-series
func (ts *TimeSeriesStorage) Retrieve(ctx context.Context, name string, tags map[string]string) (*MetricEntry, error) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	seriesKey := ts.generateSeriesKey(name, tags)

	series, exists := ts.series[seriesKey]
	if !exists {
		return nil, fmt.Errorf("time series not found: %s", seriesKey)
	}

	series.mu.RLock()
	defer series.mu.RUnlock()

	if len(series.Points) == 0 {
		return nil, fmt.Errorf("no points in time series: %s", seriesKey)
	}

	// Get the latest point
	latestPoint := series.Points[len(series.Points)-1]

	// Create metric entry
	entry := &MetricEntry{
		Name:        series.Name,
		Type:        series.Type,
		Value:       latestPoint.Value,
		Tags:        make(map[string]string),
		Timestamp:   latestPoint.Timestamp,
		LastUpdated: series.Updated,
		AccessCount: series.AccessCount,
		Metadata:    make(map[string]interface{}),
	}

	// Copy tags
	for k, v := range series.Tags {
		entry.Tags[k] = v
	}

	// Copy metadata
	for k, v := range series.Metadata {
		entry.Metadata[k] = v
	}

	ts.stats.TotalReads++
	return entry, nil
}

// Delete deletes a time series
func (ts *TimeSeriesStorage) Delete(ctx context.Context, name string, tags map[string]string) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	seriesKey := ts.generateSeriesKey(name, tags)

	series, exists := ts.series[seriesKey]
	if !exists {
		return fmt.Errorf("time series not found: %s", seriesKey)
	}

	// Update stats
	series.mu.RLock()
	pointCount := len(series.Points)
	series.mu.RUnlock()

	ts.stats.TotalPoints -= int64(pointCount)
	ts.stats.TotalDeletes++

	delete(ts.series, seriesKey)
	ts.updateStorageStats()

	return nil
}

// List lists all time series matching filters
func (ts *TimeSeriesStorage) List(ctx context.Context, filters map[string]string) ([]*MetricEntry, error) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	var entries []*MetricEntry

	for _, series := range ts.series {
		if ts.matchesFilters(series, filters) {
			series.mu.RLock()

			if len(series.Points) > 0 {
				// Get the latest point
				latestPoint := series.Points[len(series.Points)-1]

				entry := &MetricEntry{
					Name:        series.Name,
					Type:        series.Type,
					Value:       latestPoint.Value,
					Tags:        make(map[string]string),
					Timestamp:   latestPoint.Timestamp,
					LastUpdated: series.Updated,
					AccessCount: series.AccessCount,
					Metadata:    make(map[string]interface{}),
				}

				// Copy tags
				for k, v := range series.Tags {
					entry.Tags[k] = v
				}

				// Copy metadata
				for k, v := range series.Metadata {
					entry.Metadata[k] = v
				}

				entries = append(entries, entry)
			}

			series.mu.RUnlock()
		}
	}

	ts.stats.TotalReads += int64(len(entries))
	return entries, nil
}

// Count returns the number of time series
func (ts *TimeSeriesStorage) Count(ctx context.Context) (int64, error) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	return int64(len(ts.series)), nil
}

// Clear clears all time series
func (ts *TimeSeriesStorage) Clear(ctx context.Context) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	ts.series = make(map[string]*TimeSeries)
	ts.stats.SeriesCount = 0
	ts.stats.TotalPoints = 0
	ts.stats.CompressedPoints = 0
	ts.updateStorageStats()

	return nil
}

// Start starts the time-series storage system
func (ts *TimeSeriesStorage) Start(ctx context.Context) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if ts.started {
		return fmt.Errorf("time-series storage already started")
	}

	ts.started = true
	ts.stats.startTime = time.Now()

	// Start cleanup goroutine
	go ts.cleanupLoop()

	// Start compression goroutine if enabled
	if ts.compressionEnabled {
		go ts.compressionLoop()
	}

	return nil
}

// Stop stops the time-series storage system
func (ts *TimeSeriesStorage) Stop(ctx context.Context) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if !ts.started {
		return fmt.Errorf("time-series storage not started")
	}

	ts.started = false
	close(ts.stopCh)

	return nil
}

// Health checks the health of the time-series storage system
func (ts *TimeSeriesStorage) Health(ctx context.Context) error {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	if !ts.started {
		return fmt.Errorf("time-series storage not started")
	}

	// Check if storage is within limits
	if len(ts.series) > ts.maxSeries {
		return fmt.Errorf("storage exceeded maximum series: %d > %d", len(ts.series), ts.maxSeries)
	}

	// Check for excessive memory usage
	if ts.stats.MemoryUsage > int64(ts.maxSeries*ts.maxPointsPerSeries*100) {
		return fmt.Errorf("excessive memory usage: %d bytes", ts.stats.MemoryUsage)
	}

	return nil
}

// Stats returns storage statistics
func (ts *TimeSeriesStorage) Stats(ctx context.Context) (interface{}, error) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	// Update dynamic stats
	stats := *ts.stats
	stats.Uptime = time.Since(stats.startTime)
	stats.SeriesCount = int64(len(ts.series))

	// Calculate compression ratio
	if stats.TotalPoints > 0 {
		stats.CompressionRatio = float64(stats.CompressedPoints) / float64(stats.TotalPoints) * 100
	}

	return stats, nil
}

// =============================================================================
// TIME-SERIES SPECIFIC OPERATIONS
// =============================================================================

// QueryRange queries time series data within a time range
func (ts *TimeSeriesStorage) QueryRange(ctx context.Context, query TimeRangeQuery) (*TimeRangeResult, error) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	var results []*TimeSeriesData

	for _, series := range ts.series {
		if ts.matchesFilters(series, query.Filters) {
			seriesData := ts.querySeriesRange(series, query.Start, query.End, query.Step)
			if seriesData != nil {
				results = append(results, seriesData)
			}
		}
	}

	result := &TimeRangeResult{
		Data:      results,
		Query:     query,
		Timestamp: time.Now(),
	}

	return result, nil
}

// AggregateRange aggregates time series data over time windows
func (ts *TimeSeriesStorage) AggregateRange(ctx context.Context, query TimeAggregationQuery) (*TimeAggregationResult, error) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	var results []*TimeAggregationData

	for _, series := range ts.series {
		if ts.matchesFilters(series, query.Filters) {
			aggData := ts.aggregateSeriesRange(series, query.Start, query.End, query.Step, query.Function)
			if aggData != nil {
				results = append(results, aggData)
			}
		}
	}

	result := &TimeAggregationResult{
		Data:      results,
		Query:     query,
		Timestamp: time.Now(),
	}

	return result, nil
}

// =============================================================================
// PRIVATE METHODS
// =============================================================================

// generateSeriesKey generates a unique key for a time series
func (ts *TimeSeriesStorage) generateSeriesKey(name string, tags map[string]string) string {
	if len(tags) == 0 {
		return name
	}
	return name + "{" + metrics.TagsToString(tags) + "}"
}

// updateBucket updates the time-based bucket for efficient querying
func (ts *TimeSeriesStorage) updateBucket(series *TimeSeries, point *TimeSeriesPoint) {
	// Calculate bucket timestamp based on resolution
	bucketTime := point.Timestamp.Truncate(ts.resolution)
	bucketKey := bucketTime.Unix()

	series.mu.Lock()
	defer series.mu.Unlock()

	bucket, exists := series.Buckets[bucketKey]
	if !exists {
		bucket = &TimeWindow{
			Start: bucketTime,
			End:   bucketTime.Add(ts.resolution),
			Min:   point.Value,
			Max:   point.Value,
		}
		series.Buckets[bucketKey] = bucket
	}

	// Update bucket statistics
	bucket.Count++
	bucket.Sum += point.Value
	bucket.Mean = bucket.Sum / float64(bucket.Count)

	if point.Value < bucket.Min {
		bucket.Min = point.Value
	}
	if point.Value > bucket.Max {
		bucket.Max = point.Value
	}

	// Update standard deviation (simplified calculation)
	bucket.StdDev = ts.calculateStdDev(series, bucketTime, ts.resolution)
}

// calculateStdDev calculates standard deviation for a time window
func (ts *TimeSeriesStorage) calculateStdDev(series *TimeSeries, start time.Time, duration time.Duration) float64 {
	end := start.Add(duration)
	var values []float64

	for _, point := range series.Points {
		if point.Timestamp.After(start) && point.Timestamp.Before(end) {
			values = append(values, point.Value)
		}
	}

	if len(values) < 2 {
		return 0
	}

	// Calculate mean
	var sum float64
	for _, v := range values {
		sum += v
	}
	mean := sum / float64(len(values))

	// Calculate variance
	var variance float64
	for _, v := range values {
		variance += (v - mean) * (v - mean)
	}
	variance /= float64(len(values))

	return math.Sqrt(variance)
}

// querySeriesRange queries a specific series within a time range
func (ts *TimeSeriesStorage) querySeriesRange(series *TimeSeries, start, end time.Time, step time.Duration) *TimeSeriesData {
	series.mu.RLock()
	defer series.mu.RUnlock()

	var points []*TimeSeriesPoint

	for _, point := range series.Points {
		if point.Timestamp.After(start) && point.Timestamp.Before(end) {
			points = append(points, point)
		}
	}

	if len(points) == 0 {
		return nil
	}

	// Downsample if step is provided
	if step > 0 {
		points = ts.downsamplePoints(points, step)
	}

	return &TimeSeriesData{
		Name:   series.Name,
		Tags:   series.Tags,
		Points: points,
	}
}

// aggregateSeriesRange aggregates a series over time windows
func (ts *TimeSeriesStorage) aggregateSeriesRange(series *TimeSeries, start, end time.Time, step time.Duration, function string) *TimeAggregationData {
	series.mu.RLock()
	defer series.mu.RUnlock()

	var windows []*TimeWindow

	// Generate time windows
	for current := start; current.Before(end); current = current.Add(step) {
		windowEnd := current.Add(step)
		if windowEnd.After(end) {
			windowEnd = end
		}

		// Find points in this window
		var windowPoints []*TimeSeriesPoint
		for _, point := range series.Points {
			if point.Timestamp.After(current) && point.Timestamp.Before(windowEnd) {
				windowPoints = append(windowPoints, point)
			}
		}

		if len(windowPoints) == 0 {
			continue
		}

		// Calculate aggregation
		window := &TimeWindow{
			Start: current,
			End:   windowEnd,
			Count: int64(len(windowPoints)),
		}

		switch function {
		case "sum":
			for _, point := range windowPoints {
				window.Sum += point.Value
			}
			window.Mean = window.Sum
		case "avg", "mean":
			for _, point := range windowPoints {
				window.Sum += point.Value
			}
			window.Mean = window.Sum / float64(len(windowPoints))
		case "min":
			window.Min = windowPoints[0].Value
			for _, point := range windowPoints {
				if point.Value < window.Min {
					window.Min = point.Value
				}
			}
			window.Mean = window.Min
		case "max":
			window.Max = windowPoints[0].Value
			for _, point := range windowPoints {
				if point.Value > window.Max {
					window.Max = point.Value
				}
			}
			window.Mean = window.Max
		}

		windows = append(windows, window)
	}

	if len(windows) == 0 {
		return nil
	}

	return &TimeAggregationData{
		Name:     series.Name,
		Tags:     series.Tags,
		Function: function,
		Windows:  windows,
	}
}

// downsamplePoints reduces the number of points by sampling
func (ts *TimeSeriesStorage) downsamplePoints(points []*TimeSeriesPoint, step time.Duration) []*TimeSeriesPoint {
	if len(points) == 0 {
		return points
	}

	var downsampled []*TimeSeriesPoint
	lastTimestamp := points[0].Timestamp

	for _, point := range points {
		if point.Timestamp.Sub(lastTimestamp) >= step {
			downsampled = append(downsampled, point)
			lastTimestamp = point.Timestamp
		}
	}

	return downsampled
}

// matchesFilters checks if a series matches the given filters
func (ts *TimeSeriesStorage) matchesFilters(series *TimeSeries, filters map[string]string) bool {
	if filters == nil {
		return true
	}

	for key, value := range filters {
		switch key {
		case "name":
			if series.Name != value {
				return false
			}
		case "type":
			if string(series.Type) != value {
				return false
			}
		default:
			// Check tags
			if tagValue, exists := series.Tags[key]; !exists || tagValue != value {
				return false
			}
		}
	}

	return true
}

// evictOldestSeries evicts the oldest series to make space
func (ts *TimeSeriesStorage) evictOldestSeries() error {
	if len(ts.series) == 0 {
		return nil
	}

	var oldestKey string
	var oldestTime time.Time

	for key, series := range ts.series {
		if oldestTime.IsZero() || series.Created.Before(oldestTime) {
			oldestKey = key
			oldestTime = series.Created
		}
	}

	if oldestKey != "" {
		series := ts.series[oldestKey]
		series.mu.RLock()
		pointCount := len(series.Points)
		series.mu.RUnlock()

		ts.stats.TotalPoints -= int64(pointCount)
		delete(ts.series, oldestKey)
		ts.stats.TotalDeletes++
	}

	return nil
}

// cleanupLoop runs periodic cleanup
func (ts *TimeSeriesStorage) cleanupLoop() {
	ticker := time.NewTicker(ts.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if !ts.started {
				return
			}
			ts.cleanup()
		case <-ts.stopCh:
			return
		}
	}
}

// cleanup removes expired data
func (ts *TimeSeriesStorage) cleanup() {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if ts.retention <= 0 {
		return
	}

	cutoff := time.Now().Add(-ts.retention)
	var deletedPoints int64

	for key, series := range ts.series {
		series.mu.Lock()

		// Remove old points
		var validPoints []*TimeSeriesPoint
		for _, point := range series.Points {
			if point.Timestamp.After(cutoff) {
				validPoints = append(validPoints, point)
			} else {
				deletedPoints++
			}
		}

		series.Points = validPoints

		// Remove old buckets
		for bucketKey, bucket := range series.Buckets {
			if bucket.Start.Before(cutoff) {
				delete(series.Buckets, bucketKey)
			}
		}

		// Remove series if no points left
		if len(series.Points) == 0 {
			series.mu.Unlock()
			delete(ts.series, key)
		} else {
			series.mu.Unlock()
		}
	}

	ts.stats.CleanupRuns++
	ts.stats.LastCleanup = time.Now()
	ts.stats.TotalPoints -= deletedPoints
	ts.updateStorageStats()
}

// compressionLoop runs periodic compression
func (ts *TimeSeriesStorage) compressionLoop() {
	ticker := time.NewTicker(ts.compressionDelay)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if !ts.started {
				return
			}
			ts.compressOldData()
		case <-ts.stopCh:
			return
		}
	}
}

// compressOldData compresses old data points into time windows
func (ts *TimeSeriesStorage) compressOldData() {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	cutoff := time.Now().Add(-ts.compressionDelay)
	var compressedPoints int64

	for _, series := range ts.series {
		series.mu.Lock()

		// Find points to compress
		var toCompress []*TimeSeriesPoint
		var toKeep []*TimeSeriesPoint

		for _, point := range series.Points {
			if point.Timestamp.Before(cutoff) {
				toCompress = append(toCompress, point)
			} else {
				toKeep = append(toKeep, point)
			}
		}

		// Compress points into time windows
		if len(toCompress) > 0 {
			windows := ts.compressPointsToWindows(toCompress, ts.resolution)
			for _, window := range windows {
				bucketKey := window.Start.Unix()
				series.Buckets[bucketKey] = window
			}

			series.Points = toKeep
			compressedPoints += int64(len(toCompress))
		}

		series.mu.Unlock()
	}

	ts.stats.CompressionRuns++
	ts.stats.LastCompression = time.Now()
	ts.stats.CompressedPoints += compressedPoints
	ts.updateStorageStats()
}

// compressPointsToWindows compresses points into time windows
func (ts *TimeSeriesStorage) compressPointsToWindows(points []*TimeSeriesPoint, resolution time.Duration) []*TimeWindow {
	windowMap := make(map[int64]*TimeWindow)

	for _, point := range points {
		bucketTime := point.Timestamp.Truncate(resolution)
		bucketKey := bucketTime.Unix()

		window, exists := windowMap[bucketKey]
		if !exists {
			window = &TimeWindow{
				Start:      bucketTime,
				End:        bucketTime.Add(resolution),
				Min:        point.Value,
				Max:        point.Value,
				Compressed: true,
			}
			windowMap[bucketKey] = window
		}

		window.Count++
		window.Sum += point.Value
		window.Mean = window.Sum / float64(window.Count)

		if point.Value < window.Min {
			window.Min = point.Value
		}
		if point.Value > window.Max {
			window.Max = point.Value
		}
	}

	var windows []*TimeWindow
	for _, window := range windowMap {
		windows = append(windows, window)
	}

	return windows
}

// updateStorageStats updates storage statistics
func (ts *TimeSeriesStorage) updateStorageStats() {
	ts.stats.SeriesCount = int64(len(ts.series))

	var totalPoints int64
	var memoryUsage int64
	var oldestPoint, newestPoint time.Time

	for _, series := range ts.series {
		series.mu.RLock()
		totalPoints += int64(len(series.Points))

		// Estimate memory usage
		memoryUsage += int64(len(series.Points) * 64) // Rough estimate

		// Find oldest and newest points
		for _, point := range series.Points {
			if oldestPoint.IsZero() || point.Timestamp.Before(oldestPoint) {
				oldestPoint = point.Timestamp
			}
			if newestPoint.IsZero() || point.Timestamp.After(newestPoint) {
				newestPoint = point.Timestamp
			}
		}
		series.mu.RUnlock()
	}

	ts.stats.TotalPoints = totalPoints
	ts.stats.MemoryUsage = memoryUsage
	ts.stats.OldestPoint = oldestPoint
	ts.stats.NewestPoint = newestPoint

	if ts.stats.SeriesCount > 0 {
		ts.stats.AverageSeriesSize = float64(totalPoints) / float64(ts.stats.SeriesCount)
	}
}

// extractNumericValue extracts numeric value from interface{}
func (ts *TimeSeriesStorage) extractNumericValue(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case int:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case float32:
		return float64(v), true
	case float64:
		return v, true
	default:
		return 0, false
	}
}

// SetLogger sets the logger for the time-series storage
func (ts *TimeSeriesStorage) SetLogger(logger logger.Logger) {
	ts.logger = logger
}

// =============================================================================
// SUPPORTING TYPES
// =============================================================================

// TimeRangeQuery represents a time range query
type TimeRangeQuery struct {
	Filters map[string]string `json:"filters"`
	Start   time.Time         `json:"start"`
	End     time.Time         `json:"end"`
	Step    time.Duration     `json:"step"`
}

// TimeRangeResult represents the result of a time range query
type TimeRangeResult struct {
	Data      []*TimeSeriesData `json:"data"`
	Query     TimeRangeQuery    `json:"query"`
	Timestamp time.Time         `json:"timestamp"`
}

// TimeSeriesData represents time series data
type TimeSeriesData struct {
	Name   string             `json:"name"`
	Tags   map[string]string  `json:"tags"`
	Points []*TimeSeriesPoint `json:"points"`
}

// TimeAggregationQuery represents a time aggregation query
type TimeAggregationQuery struct {
	Filters  map[string]string `json:"filters"`
	Start    time.Time         `json:"start"`
	End      time.Time         `json:"end"`
	Step     time.Duration     `json:"step"`
	Function string            `json:"function"`
}

// TimeAggregationResult represents the result of a time aggregation query
type TimeAggregationResult struct {
	Data      []*TimeAggregationData `json:"data"`
	Query     TimeAggregationQuery   `json:"query"`
	Timestamp time.Time              `json:"timestamp"`
}

// TimeAggregationData represents aggregated time series data
type TimeAggregationData struct {
	Name     string            `json:"name"`
	Tags     map[string]string `json:"tags"`
	Function string            `json:"function"`
	Windows  []*TimeWindow     `json:"windows"`
}
