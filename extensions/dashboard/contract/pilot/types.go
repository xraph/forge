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
