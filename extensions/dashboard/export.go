package dashboard

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/xraph/forge"
)

// handleExportJSON exports dashboard data as JSON.
func (ds *DashboardServer) handleExportJSON(w http.ResponseWriter, r *http.Request) {
	ds.setCORSHeaders(w)

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)

		return
	}

	snapshot := ds.createSnapshot(r.Context())

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=dashboard-%s.json", time.Now().Format("20060102-150405")))

	if err := json.NewEncoder(w).Encode(snapshot); err != nil {
		ds.logger.Error("failed to export JSON",
			forge.F("error", err),
		)
		http.Error(w, "Export failed", http.StatusInternalServerError)
	}
}

// handleExportCSV exports dashboard data as CSV.
func (ds *DashboardServer) handleExportCSV(w http.ResponseWriter, r *http.Request) {
	ds.setCORSHeaders(w)

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)

		return
	}

	snapshot := ds.createSnapshot(r.Context())

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=dashboard-%s.csv", time.Now().Format("20060102-150405")))

	writer := csv.NewWriter(w)
	defer writer.Flush()

	// Write overview section
	writer.Write([]string{"Section", "Key", "Value"})
	writer.Write([]string{"Overview", "Timestamp", snapshot.Timestamp.Format(time.RFC3339)})
	writer.Write([]string{"Overview", "Overall Health", snapshot.Overview.OverallHealth})
	writer.Write([]string{"Overview", "Total Services", strconv.Itoa(snapshot.Overview.TotalServices)})
	writer.Write([]string{"Overview", "Healthy Services", strconv.Itoa(snapshot.Overview.HealthyServices)})
	writer.Write([]string{"Overview", "Total Metrics", strconv.Itoa(snapshot.Overview.TotalMetrics)})
	writer.Write([]string{"Overview", "Uptime", snapshot.Overview.Uptime.String()})
	writer.Write([]string{"Overview", "Version", snapshot.Overview.Version})
	writer.Write([]string{"Overview", "Environment", snapshot.Overview.Environment})
	writer.Write([]string{})

	// Write health section
	writer.Write([]string{"Health Checks", "Service", "Status", "Message", "Duration"})

	for name, service := range snapshot.Health.Services {
		writer.Write([]string{
			"Health",
			name,
			service.Status,
			service.Message,
			service.Duration.String(),
		})
	}

	writer.Write([]string{})

	// Write services section
	writer.Write([]string{"Services", "Name", "Type", "Status"})

	for _, service := range snapshot.Services {
		writer.Write([]string{
			"Services",
			service.Name,
			service.Type,
			service.Status,
		})
	}
}

// handleExportPrometheus exports metrics in Prometheus format.
func (ds *DashboardServer) handleExportPrometheus(w http.ResponseWriter, r *http.Request) {
	ds.setCORSHeaders(w)

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)

		return
	}

	snapshot := ds.createSnapshot(r.Context())

	w.Header().Set("Content-Type", "text/plain; version=0.0.4")

	var output strings.Builder

	// Export overview metrics
	output.WriteString("# HELP forge_dashboard_services_total Total number of registered services\n")
	output.WriteString("# TYPE forge_dashboard_services_total gauge\n")
	output.WriteString(fmt.Sprintf("forge_dashboard_services_total %d\n\n", snapshot.Overview.TotalServices))

	output.WriteString("# HELP forge_dashboard_services_healthy Number of healthy services\n")
	output.WriteString("# TYPE forge_dashboard_services_healthy gauge\n")
	output.WriteString(fmt.Sprintf("forge_dashboard_services_healthy %d\n\n", snapshot.Overview.HealthyServices))

	output.WriteString("# HELP forge_dashboard_metrics_total Total number of metrics\n")
	output.WriteString("# TYPE forge_dashboard_metrics_total gauge\n")
	output.WriteString(fmt.Sprintf("forge_dashboard_metrics_total %d\n\n", snapshot.Overview.TotalMetrics))

	output.WriteString("# HELP forge_dashboard_uptime_seconds Uptime in seconds\n")
	output.WriteString("# TYPE forge_dashboard_uptime_seconds gauge\n")
	output.WriteString(fmt.Sprintf("forge_dashboard_uptime_seconds %.0f\n\n", snapshot.Overview.Uptime.Seconds()))

	// Export health status
	output.WriteString("# HELP forge_dashboard_health_status Service health status (1=healthy, 0=unhealthy)\n")
	output.WriteString("# TYPE forge_dashboard_health_status gauge\n")

	for name, service := range snapshot.Health.Services {
		status := 0
		if service.Status == "healthy" {
			status = 1
		}

		output.WriteString(fmt.Sprintf("forge_dashboard_health_status{service=\"%s\"} %d\n", name, status))
	}

	output.WriteString("\n")

	// Export metrics from the metrics data
	for key, value := range snapshot.Metrics.Metrics {
		// Only export numeric values
		switch v := value.(type) {
		case int:
			output.WriteString(fmt.Sprintf("forge_metric_%s %d\n", sanitizeMetricName(key), v))
		case int64:
			output.WriteString(fmt.Sprintf("forge_metric_%s %d\n", sanitizeMetricName(key), v))
		case float64:
			output.WriteString(fmt.Sprintf("forge_metric_%s %f\n", sanitizeMetricName(key), v))
		case float32:
			output.WriteString(fmt.Sprintf("forge_metric_%s %f\n", sanitizeMetricName(key), v))
		}
	}

	w.Write([]byte(output.String()))
}

// createSnapshot creates a complete dashboard snapshot.
func (ds *DashboardServer) createSnapshot(ctx any) DashboardSnapshot {
	ctxTyped, ok := ctx.(interface {
		Context() interface{ Done() <-chan struct{} }
	})

	var actualCtx interface{ Done() <-chan struct{} }
	if ok {
		actualCtx = ctxTyped.Context()
	}

	// Use type assertion to get context.Context
	var realCtx any
	if actualCtx != nil {
		realCtx = actualCtx
	}

	// Collect all data
	var (
		overview *OverviewData
		health   *HealthData
		metrics  *MetricsData
		services []ServiceInfo
	)

	if ctxInterface, ok := realCtx.(interface {
		Done() <-chan struct{}
		Deadline() (time.Time, bool)
		Err() error
		Value(key any) any
	}); ok {
		overview = ds.collector.CollectOverview(ctxInterface)
		health = ds.collector.CollectHealth(ctxInterface)
		metrics = ds.collector.CollectMetrics(ctxInterface)
		services = ds.collector.CollectServices(ctxInterface)
	} else {
		// Fallback: create empty snapshot
		overview = &OverviewData{Timestamp: time.Now()}
		health = &HealthData{CheckedAt: time.Now(), Services: make(map[string]ServiceHealth)}
		metrics = &MetricsData{Timestamp: time.Now(), Metrics: make(map[string]any)}
		services = []ServiceInfo{}
	}

	return DashboardSnapshot{
		Timestamp:   time.Now(),
		Overview:    *overview,
		Health:      *health,
		Metrics:     *metrics,
		Services:    services,
		GeneratedBy: "forge-dashboard",
	}
}

// sanitizeMetricName sanitizes a metric name for Prometheus format.
func sanitizeMetricName(name string) string {
	// Replace invalid characters with underscores
	result := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == ':' {
			return r
		}

		return '_'
	}, name)

	return result
}
