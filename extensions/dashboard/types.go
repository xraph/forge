package dashboard

import (
	"time"
)

// OverviewData contains dashboard overview information.
type OverviewData struct {
	Timestamp       time.Time      `json:"timestamp"`
	OverallHealth   string         `json:"overall_health"`
	TotalServices   int            `json:"total_services"`
	HealthyServices int            `json:"healthy_services"`
	TotalMetrics    int            `json:"total_metrics"`
	Uptime          time.Duration  `json:"uptime"`
	Version         string         `json:"version"`
	Environment     string         `json:"environment"`
	Summary         map[string]any `json:"summary"`
}

// HealthData contains health check results.
type HealthData struct {
	OverallStatus string                   `json:"overall_status"`
	Services      map[string]ServiceHealth `json:"services"`
	CheckedAt     time.Time                `json:"checked_at"`
	Duration      time.Duration            `json:"duration"`
	Summary       HealthSummary            `json:"summary"`
}

// ServiceHealth contains individual service health information.
type ServiceHealth struct {
	Name      string         `json:"name"`
	Status    string         `json:"status"`
	Message   string         `json:"message"`
	Duration  time.Duration  `json:"duration"`
	Critical  bool           `json:"critical"`
	Timestamp time.Time      `json:"timestamp"`
	Details   map[string]any `json:"details,omitempty"`
}

// HealthSummary provides count of services by status.
type HealthSummary struct {
	Healthy   int `json:"healthy"`
	Degraded  int `json:"degraded"`
	Unhealthy int `json:"unhealthy"`
	Unknown   int `json:"unknown"`
	Total     int `json:"total"`
}

// MetricsData contains current metrics information.
type MetricsData struct {
	Timestamp time.Time      `json:"timestamp"`
	Metrics   map[string]any `json:"metrics"`
	Stats     MetricsStats   `json:"stats"`
}

// MetricsStats contains metrics statistics.
type MetricsStats struct {
	TotalMetrics int       `json:"total_metrics"`
	Counters     int       `json:"counters"`
	Gauges       int       `json:"gauges"`
	Histograms   int       `json:"histograms"`
	LastUpdate   time.Time `json:"last_update"`
}

// ServiceInfo contains information about a registered service.
type ServiceInfo struct {
	Name         string    `json:"name"`
	Type         string    `json:"type"`
	Status       string    `json:"status"`
	RegisteredAt time.Time `json:"registered_at,omitempty"` //nolint:modernize // omitempty intentional for API compat
}

// DataPoint represents a time-series data point.
type DataPoint struct {
	Timestamp time.Time         `json:"timestamp"`
	Value     float64           `json:"value"`
	Labels    map[string]string `json:"labels,omitempty"`
	Metadata  map[string]any    `json:"metadata,omitempty"`
}

// TimeSeriesData contains time-series data.
type TimeSeriesData struct {
	Name        string      `json:"name"`
	Points      []DataPoint `json:"points"`
	Unit        string      `json:"unit,omitempty"`
	Aggregation string      `json:"aggregation,omitempty"`
}

// HistoryData contains historical dashboard data.
type HistoryData struct {
	StartTime time.Time        `json:"start_time"`
	EndTime   time.Time        `json:"end_time"`
	Series    []TimeSeriesData `json:"series"`
}

// ExportFormat represents data export format.
type ExportFormat string

const (
	ExportFormatJSON       ExportFormat = "json"
	ExportFormatCSV        ExportFormat = "csv"
	ExportFormatPrometheus ExportFormat = "prometheus"
)

// DashboardSnapshot contains complete dashboard state for export.
type DashboardSnapshot struct {
	Timestamp   time.Time     `json:"timestamp"`
	Overview    OverviewData  `json:"overview"`
	Health      HealthData    `json:"health"`
	Metrics     MetricsData   `json:"metrics"`
	Services    []ServiceInfo `json:"services"`
	GeneratedBy string        `json:"generated_by"`
}

// ServiceDetail contains detailed information about a specific service.
type ServiceDetail struct {
	Name            string         `json:"name"`
	Type            string         `json:"type"`
	Status          string         `json:"status"`
	Health          *ServiceHealth `json:"health,omitempty"`
	Metrics         map[string]any `json:"metrics"`
	Dependencies    []string       `json:"dependencies"`
	Configuration   map[string]any `json:"configuration,omitempty"`
	LastHealthCheck time.Time      `json:"last_health_check"`
	Uptime          time.Duration  `json:"uptime,omitempty"`
}

// MetricsReport contains comprehensive metrics information.
type MetricsReport struct {
	Timestamp      time.Time          `json:"timestamp"`
	TotalMetrics   int                `json:"total_metrics"`
	MetricsByType  map[string]int     `json:"metrics_by_type"`
	Collectors     []CollectorInfo    `json:"collectors"`
	Stats          MetricsReportStats `json:"stats"`
	TopMetrics     []MetricEntry      `json:"top_metrics"`
	RecentActivity []MetricActivity   `json:"recent_activity"`
}

// CollectorInfo contains information about a metrics collector.
type CollectorInfo struct {
	Name           string    `json:"name"`
	Type           string    `json:"type"`
	MetricsCount   int       `json:"metrics_count"`
	LastCollection time.Time `json:"last_collection"`
	Status         string    `json:"status"`
}

// MetricsReportStats contains statistics about metrics.
type MetricsReportStats struct {
	CollectionInterval time.Duration `json:"collection_interval"`
	LastCollection     time.Time     `json:"last_collection"`
	TotalCollections   int64         `json:"total_collections"`
	ErrorCount         int           `json:"error_count"`
	Uptime             time.Duration `json:"uptime"`
}

// MetricEntry represents a single metric entry.
type MetricEntry struct {
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	Value     any       `json:"value"`
	Tags      []string  `json:"tags,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// MetricActivity represents recent metric activity.
type MetricActivity struct {
	Metric    string    `json:"metric"`
	Action    string    `json:"action"`
	Value     float64   `json:"value"`
	Timestamp time.Time `json:"timestamp"`
}
