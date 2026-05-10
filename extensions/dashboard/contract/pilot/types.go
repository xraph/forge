package pilot

import "github.com/xraph/forge/extensions/dashboard/collector"

// ExtensionInfo is a flattened summary of one registered contributor manifest.
type ExtensionInfo struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Version     string `json:"version"`
	Icon        string `json:"icon,omitempty"`
	Layout      string `json:"layout,omitempty"`
	PageCount   int    `json:"pageCount"`
	WidgetCount int    `json:"widgetCount"`
}

// ExtensionsList is the response payload for the extensions.list query.
type ExtensionsList struct {
	Extensions []ExtensionInfo `json:"extensions"`
}

// ServicesList is the response payload for the services.list query.
type ServicesList struct {
	Services []collector.ServiceInfo `json:"services"`
}

// ServiceDetailResponse is the response payload for services.detail.
// (collector.ServiceDetail is reused as-is.)
type ServiceDetailResponse = collector.ServiceDetail

// ServiceDetailInput is the input payload for services.detail.
type ServiceDetailInput struct {
	Name string `json:"name"`
}

// MetricsSummary is the per-event payload for the metrics.summary subscription.
type MetricsSummary struct {
	TotalMetrics int   `json:"totalMetrics"`
	Counters     int   `json:"counters"`
	Gauges       int   `json:"gauges"`
	Histograms   int   `json:"histograms"`
	TS           int64 `json:"ts"` // unix seconds
}

// --- slice (h) wire shapes ---

// OverviewResponse is the wire shape for the overview query.
type OverviewResponse struct {
	OverallHealth   string         `json:"overallHealth"`
	TotalServices   int            `json:"totalServices"`
	HealthyServices int            `json:"healthyServices"`
	TotalMetrics    int            `json:"totalMetrics"`
	UptimeSeconds   int64          `json:"uptimeSeconds"`
	Version         string         `json:"version"`
	Environment     string         `json:"environment"`
	Summary         map[string]any `json:"summary,omitempty"`
}

// HealthEntry is one row of the health.list query — flattened from the per-
// service map the collector returns so resource.list can render it.
type HealthEntry struct {
	Name              string `json:"name"`
	Status            string `json:"status"`
	Message           string `json:"message,omitempty"`
	DurationMs        int64  `json:"durationMs"`
	Critical          bool   `json:"critical"`
}

// HealthList is the response payload for the health query.
type HealthList struct {
	OverallStatus  string        `json:"overallStatus"`
	HealthySummary int           `json:"healthySummary"`
	Total          int           `json:"total"`
	Services       []HealthEntry `json:"services"`
}

// CollectorEntry is one row of the metrics-report collectors list.
type CollectorEntry struct {
	Name           string `json:"name"`
	Type           string `json:"type"`
	MetricsCount   int    `json:"metricsCount"`
	Status         string `json:"status"`
	LastCollection string `json:"lastCollection,omitempty"`
}

// MetricEntryDTO is one row of the metrics-report top metrics list.
type MetricEntryDTO struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value any    `json:"value,omitempty"`
}

// MetricsReportResponse is the wire shape for the metrics-report query.
type MetricsReportResponse struct {
	TotalMetrics  int               `json:"totalMetrics"`
	MetricsByType map[string]int    `json:"metricsByType"`
	Collectors    []CollectorEntry  `json:"collectors"`
	TopMetrics    []MetricEntryDTO  `json:"topMetrics"`
}

// TraceSummaryDTO is one row of traces.list.
type TraceSummaryDTO struct {
	TraceID      string `json:"traceID"`
	RootSpanName string `json:"rootSpanName"`
	SpanCount    int    `json:"spanCount"`
	DurationMs   int64  `json:"durationMs"`
	Status       string `json:"status"`
	StartTime    string `json:"startTime"`
	Protocol     string `json:"protocol"`
}

// TracesList is the response for traces.list.
type TracesList struct {
	Traces []TraceSummaryDTO `json:"traces"`
	Total  int               `json:"total"`
}

// TraceDetailInput is the input for traces.detail.
type TraceDetailInput struct {
	ID string `json:"id"`
}

// TraceDetailResponse is the wire shape for traces.detail. We reuse the
// collector's TraceDetail because its JSON tags are stable.
type TraceDetailResponse = collector.TraceDetail
