package pages

import (
	"fmt"
	"sort"
	"time"

	"github.com/xraph/forgeui/components/chart"

	"github.com/xraph/forge/extensions/dashboard/collector"
)

// formatMetricValue formats a metric value for display.
func formatMetricValue(value any) string {
	switch v := value.(type) {
	case float64:
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d", int64(v))
		}

		return fmt.Sprintf("%.4f", v)
	case float32:
		return fmt.Sprintf("%.4f", v)
	case int:
		return fmt.Sprintf("%d", v)
	case int64:
		return fmt.Sprintf("%d", v)
	case uint64:
		return fmt.Sprintf("%d", v)
	case uint:
		return fmt.Sprintf("%d", v)
	case time.Duration:
		return v.String()
	case string:
		return v
	case map[string]any:
		return fmt.Sprintf("%v", v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// formatFloat formats a float for display with appropriate precision.
func formatFloat(v float64) string {
	if v == 0 {
		return "0"
	}

	if v == float64(int64(v)) {
		return fmt.Sprintf("%d", int64(v))
	}

	return fmt.Sprintf("%.4f", v)
}

// formatDurationValue formats a time.Duration for display.
func formatDurationValue(d time.Duration) string {
	if d == 0 {
		return "0s"
	}

	return d.String()
}

// formatBytesValue formats bytes to a human-readable string.
func formatBytesValue(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}

	div, exp := uint64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	units := []string{"KiB", "MiB", "GiB", "TiB"}
	if exp >= len(units) {
		exp = len(units) - 1
	}

	return fmt.Sprintf("%.1f %s", float64(bytes)/float64(div), units[exp])
}

// metricTypeBadgeVariant returns a badge variant string for a metric type.
func metricTypeBadgeVariant(metricType string) string {
	switch metricType {
	case "counter":
		return "default"
	case "gauge":
		return "secondary"
	case "histogram":
		return "outline"
	case "timer":
		return "outline"
	default:
		return "secondary"
	}
}

// buildBucketChartData builds a bar chart from histogram bucket data.
func buildBucketChartData(buckets map[string]uint64) chart.Data {
	if len(buckets) == 0 {
		return emptyChartData()
	}

	// Sort bucket boundaries
	keys := make([]string, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	labels := make([]string, len(keys))
	data := make([]float64, len(keys))

	for i, k := range keys {
		labels[i] = k
		data[i] = float64(buckets[k])
	}

	return chart.Data{
		Labels: labels,
		Datasets: []chart.Dataset{
			{
				Label:           "Count",
				Data:            data,
				BackgroundColor: "rgba(99, 102, 241, 0.6)",
				BorderColor:     "#6366f1",
				BorderWidth:     1,
			},
		},
	}
}

// buildStatusCodeChartData builds a doughnut chart from HTTP status code data.
func buildStatusCodeChartData(metrics map[string]any) chart.Data {
	labels := make([]string, 0)
	data := make([]float64, 0)
	colors := []interface{}{"#10b981", "#3b82f6", "#f59e0b", "#ef4444", "#8b5cf6"}

	statusGroups := map[string]float64{
		"2xx": 0,
		"3xx": 0,
		"4xx": 0,
		"5xx": 0,
	}

	for key, value := range metrics {
		if !isStatusMetric(key) {
			continue
		}

		var count float64

		switch v := value.(type) {
		case int64:
			count = float64(v)
		case float64:
			count = v
		case int:
			count = float64(v)
		default:
			continue
		}

		if contains(key, "status.2") {
			statusGroups["2xx"] += count
		} else if contains(key, "status.3") {
			statusGroups["3xx"] += count
		} else if contains(key, "status.4") {
			statusGroups["4xx"] += count
		} else if contains(key, "status.5") {
			statusGroups["5xx"] += count
		}
	}

	for _, group := range []string{"2xx", "3xx", "4xx", "5xx"} {
		if statusGroups[group] > 0 {
			labels = append(labels, group)
			data = append(data, statusGroups[group])
		}
	}

	if len(data) == 0 {
		return emptyChartData()
	}

	if len(colors) > len(data) {
		colors = colors[:len(data)]
	}

	return chart.Data{
		Labels: labels,
		Datasets: []chart.Dataset{
			{
				Data:            data,
				BackgroundColor: colors,
				BorderWidth:     0,
			},
		},
	}
}

// buildMethodChartData builds a doughnut chart from HTTP method data.
func buildMethodChartData(metrics map[string]any) chart.Data {
	labels := make([]string, 0)
	data := make([]float64, 0)
	colors := []interface{}{"#3b82f6", "#10b981", "#f59e0b", "#ef4444", "#8b5cf6", "#06b6d4"}

	for key, value := range metrics {
		if !isMethodMetric(key) {
			continue
		}

		var count float64

		switch v := value.(type) {
		case int64:
			count = float64(v)
		case float64:
			count = v
		case int:
			count = float64(v)
		default:
			continue
		}

		// Extract method name from key like "http.requests.method.GET"
		method := key[len(key)-3:]
		if len(key) > 25 {
			method = key[len("http.requests.method."):]
		}

		labels = append(labels, method)
		data = append(data, count)
	}

	if len(data) == 0 {
		return emptyChartData()
	}

	if len(colors) > len(data) {
		colors = colors[:len(data)]
	}

	return chart.Data{
		Labels: labels,
		Datasets: []chart.Dataset{
			{
				Data:            data,
				BackgroundColor: colors,
				BorderWidth:     0,
			},
		},
	}
}

// buildMetricTimeSeriesChartData builds a line chart from metric time series data points.
func buildMetricTimeSeriesChartData(points []collector.DataPoint, metricName string) chart.Data {
	if len(points) == 0 {
		return emptyChartData()
	}

	labels := make([]string, len(points))
	data := make([]float64, len(points))

	for i, p := range points {
		labels[i] = p.Timestamp.Format("15:04:05")
		data[i] = p.Value
	}

	return chart.Data{
		Labels: labels,
		Datasets: []chart.Dataset{
			{
				Label:           metricName,
				Data:            data,
				BorderColor:     "#8b5cf6",
				BackgroundColor: "rgba(139, 92, 246, 0.1)",
				BorderWidth:     2,
				Tension:         0.3,
				Fill:            true,
			},
		},
	}
}

// HTTPSummary contains summary stats for the HTTP collector.
type HTTPSummary struct {
	ErrorRate       string
	RPS             string
	AvgDuration     string
	ActiveRequests  string
	AvgRequestSize  string
	AvgResponseSize string
	TotalRequests   string
}

// extractHTTPSummary extracts HTTP summary metrics from the collector metrics map.
func extractHTTPSummary(metrics map[string]any) HTTPSummary {
	s := HTTPSummary{
		ErrorRate:       "0%",
		RPS:             "0",
		AvgDuration:     "0ms",
		ActiveRequests:  "0",
		AvgRequestSize:  "0 B",
		AvgResponseSize: "0 B",
		TotalRequests:   "0",
	}

	if v, ok := extractFloat(metrics, "http.requests.error_rate"); ok {
		s.ErrorRate = fmt.Sprintf("%.1f%%", v*100)
	}
	if v, ok := extractFloat(metrics, "http.requests.rps"); ok {
		s.RPS = fmt.Sprintf("%.1f", v)
	}
	if v, ok := extractFloat(metrics, "http.requests.avg_duration"); ok {
		s.AvgDuration = fmt.Sprintf("%.2fms", v*1000)
	}
	if v, ok := extractFloat(metrics, "http.requests.active"); ok {
		s.ActiveRequests = fmt.Sprintf("%d", int64(v))
	}
	if v, ok := extractFloat(metrics, "http.requests.avg_request_size"); ok {
		s.AvgRequestSize = formatBytesValue(uint64(v))
	}
	if v, ok := extractFloat(metrics, "http.requests.avg_response_size"); ok {
		s.AvgResponseSize = formatBytesValue(uint64(v))
	}
	if v, ok := extractFloat(metrics, "http.requests.total"); ok {
		s.TotalRequests = fmt.Sprintf("%d", int64(v))
	}

	return s
}

// PathMetricEntry contains per-path HTTP metric data.
type PathMetricEntry struct {
	Path  string
	Count float64
}

// extractPathMetrics extracts per-path metrics from the HTTP collector.
func extractPathMetrics(metrics map[string]any) []PathMetricEntry {
	entries := make([]PathMetricEntry, 0)
	prefix := "http.requests.path."

	for key, value := range metrics {
		if !contains(key, prefix) {
			continue
		}

		path := key[len(prefix):]

		var count float64

		switch v := value.(type) {
		case float64:
			count = v
		case int64:
			count = float64(v)
		case int:
			count = float64(v)
		default:
			continue
		}

		entries = append(entries, PathMetricEntry{
			Path:  path,
			Count: count,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Count > entries[j].Count
	})

	return entries
}

// SystemSummary contains summary stats for the system collector.
type SystemSummary struct {
	CPUUsage    string
	MemoryUsage string
	MemoryTotal string
	LoadAvg1    string
	LoadAvg5    string
	LoadAvg15   string
	DiskUsage   string
	NumCPU      string
}

// extractSystemSummary extracts system summary metrics from the collector metrics map.
func extractSystemSummary(metrics map[string]any) SystemSummary {
	s := SystemSummary{
		CPUUsage:    "N/A",
		MemoryUsage: "N/A",
		MemoryTotal: "N/A",
		LoadAvg1:    "N/A",
		LoadAvg5:    "N/A",
		LoadAvg15:   "N/A",
		DiskUsage:   "N/A",
		NumCPU:      "N/A",
	}

	if v, ok := extractFloat(metrics, "system.cpu.usage_percent"); ok {
		s.CPUUsage = fmt.Sprintf("%.1f%%", v)
	}
	if v, ok := extractFloat(metrics, "system.memory.used_bytes"); ok {
		s.MemoryUsage = formatBytesValue(uint64(v))
	}
	if v, ok := extractFloat(metrics, "system.memory.total_bytes"); ok {
		s.MemoryTotal = formatBytesValue(uint64(v))
	}
	if v, ok := extractFloat(metrics, "system.load.avg_1"); ok {
		s.LoadAvg1 = fmt.Sprintf("%.2f", v)
	}
	if v, ok := extractFloat(metrics, "system.load.avg_5"); ok {
		s.LoadAvg5 = fmt.Sprintf("%.2f", v)
	}
	if v, ok := extractFloat(metrics, "system.load.avg_15"); ok {
		s.LoadAvg15 = fmt.Sprintf("%.2f", v)
	}
	if v, ok := extractFloat(metrics, "system.disk.usage_percent"); ok {
		s.DiskUsage = fmt.Sprintf("%.1f%%", v)
	}
	if v, ok := extractFloat(metrics, "system.cpu.num_cpu"); ok {
		s.NumCPU = fmt.Sprintf("%d", int64(v))
	}

	return s
}

// RuntimeSummary contains summary stats for the runtime collector.
type RuntimeSummary struct {
	Goroutines  string
	HeapAlloc   string
	HeapSys     string
	HeapObjects string
	GCCount     string
	GCPauseAvg  string
	StackInUse  string
	NumCPU      string
}

// extractRuntimeSummary extracts runtime summary metrics from the collector metrics map.
func extractRuntimeSummary(metrics map[string]any) RuntimeSummary {
	s := RuntimeSummary{
		Goroutines:  "N/A",
		HeapAlloc:   "N/A",
		HeapSys:     "N/A",
		HeapObjects: "N/A",
		GCCount:     "N/A",
		GCPauseAvg:  "N/A",
		StackInUse:  "N/A",
		NumCPU:      "N/A",
	}

	if v, ok := extractFloat(metrics, "runtime.goroutines"); ok {
		s.Goroutines = fmt.Sprintf("%d", int64(v))
	}
	if v, ok := extractFloat(metrics, "runtime.memory.heap_alloc"); ok {
		s.HeapAlloc = formatBytesValue(uint64(v))
	}
	if v, ok := extractFloat(metrics, "runtime.memory.heap_sys"); ok {
		s.HeapSys = formatBytesValue(uint64(v))
	}
	if v, ok := extractFloat(metrics, "runtime.memory.heap_objects"); ok {
		s.HeapObjects = fmt.Sprintf("%d", int64(v))
	}
	if v, ok := extractFloat(metrics, "runtime.gc.count"); ok {
		s.GCCount = fmt.Sprintf("%d", int64(v))
	}
	if v, ok := extractFloat(metrics, "runtime.gc.pause_avg"); ok {
		s.GCPauseAvg = fmt.Sprintf("%.2fms", v/1e6)
	}
	if v, ok := extractFloat(metrics, "runtime.memory.stack_inuse"); ok {
		s.StackInUse = formatBytesValue(uint64(v))
	}
	if v, ok := extractFloat(metrics, "runtime.num_cpu"); ok {
		s.NumCPU = fmt.Sprintf("%d", int64(v))
	}

	return s
}

// extractFloat extracts a float64 from a metrics map, handling various numeric types.
func extractFloat(metrics map[string]any, key string) (float64, bool) {
	val, ok := metrics[key]
	if !ok {
		return 0, false
	}

	switch v := val.(type) {
	case float64:
		return v, true
	case int64:
		return float64(v), true
	case int:
		return float64(v), true
	case uint64:
		return float64(v), true
	case float32:
		return float64(v), true
	default:
		return 0, false
	}
}

// isStatusMetric checks if a metric key is an HTTP status code metric.
func isStatusMetric(key string) bool {
	return contains(key, "http.requests.status.")
}

// isMethodMetric checks if a metric key is an HTTP method metric.
func isMethodMetric(key string) bool {
	return contains(key, "http.requests.method.")
}

// isPathMetric checks if a metric key is an HTTP path metric.
func isPathMetric(key string) bool {
	return contains(key, "http.requests.path.")
}

// collectorMetricEntries converts a collector's metrics map into sorted MetricEntry slices.
func collectorMetricEntries(metrics map[string]any) []collector.MetricEntry {
	entries := make([]collector.MetricEntry, 0, len(metrics))

	for name, value := range metrics {
		entries = append(entries, collector.MetricEntry{
			Name:      name,
			Type:      inferMetricType(name),
			Value:     value,
			Timestamp: time.Now(),
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})

	return entries
}

// inferMetricType infers metric type from name (mirrors collector package).
func inferMetricType(name string) string {
	if contains(name, "_total") || contains(name, "_count") || contains(name, ".total") {
		return "counter"
	}

	if contains(name, "_bucket") || contains(name, "_histogram") || contains(name, ".duration") {
		return "histogram"
	}

	if contains(name, "_duration") || contains(name, "_latency") {
		return "timer"
	}

	return "gauge"
}

// contains checks if a string contains a substring.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && findSubstring(s, substr)
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}

	return false
}
