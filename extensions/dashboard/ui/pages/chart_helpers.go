package pages

import (
	"sort"
	"time"

	"github.com/xraph/forgeui/components/chart"

	"github.com/xraph/forge/extensions/dashboard/collector"
)

// buildServiceCountChartData converts overview snapshots into a line chart
// showing total service count and healthy service count over time.
func buildServiceCountChartData(snapshots []collector.OverviewSnapshot) chart.Data {
	if len(snapshots) == 0 {
		return emptyChartData()
	}

	labels := make([]string, len(snapshots))
	totalData := make([]float64, len(snapshots))
	healthyData := make([]float64, len(snapshots))

	for i, snap := range snapshots {
		labels[i] = snap.Timestamp.Format("15:04")
		totalData[i] = float64(snap.ServiceCount)
		healthyData[i] = float64(snap.HealthyCount)
	}

	return chart.Data{
		Labels: labels,
		Datasets: []chart.Dataset{
			{
				Label:           "Total Services",
				Data:            totalData,
				BorderColor:     "#3b82f6",
				BackgroundColor: "rgba(59, 130, 246, 0.1)",
				BorderWidth:     2,
				Tension:         0.3,
				Fill:            true,
			},
			{
				Label:           "Healthy Services",
				Data:            healthyData,
				BorderColor:     "#10b981",
				BackgroundColor: "rgba(16, 185, 129, 0.1)",
				BorderWidth:     2,
				Tension:         0.3,
				Fill:            true,
			},
		},
	}
}

// buildHealthHistoryChartData converts health snapshots into a line chart
// showing healthy, degraded, and unhealthy counts over time.
func buildHealthHistoryChartData(snapshots []collector.HealthSnapshot) chart.Data {
	if len(snapshots) == 0 {
		return emptyChartData()
	}

	labels := make([]string, len(snapshots))
	healthyData := make([]float64, len(snapshots))
	degradedData := make([]float64, len(snapshots))
	unhealthyData := make([]float64, len(snapshots))

	for i, snap := range snapshots {
		labels[i] = snap.Timestamp.Format("15:04")
		healthyData[i] = float64(snap.HealthyCount)
		degradedData[i] = float64(snap.DegradedCount)
		unhealthyData[i] = float64(snap.UnhealthyCount)
	}

	return chart.Data{
		Labels: labels,
		Datasets: []chart.Dataset{
			{
				Label:           "Healthy",
				Data:            healthyData,
				BorderColor:     "#10b981",
				BackgroundColor: "rgba(16, 185, 129, 0.1)",
				BorderWidth:     2,
				Tension:         0.3,
				Fill:            true,
			},
			{
				Label:           "Degraded",
				Data:            degradedData,
				BorderColor:     "#f59e0b",
				BackgroundColor: "rgba(245, 158, 11, 0.1)",
				BorderWidth:     2,
				Tension:         0.3,
				Fill:            true,
			},
			{
				Label:           "Unhealthy",
				Data:            unhealthyData,
				BorderColor:     "#ef4444",
				BackgroundColor: "rgba(239, 68, 68, 0.1)",
				BorderWidth:     2,
				Tension:         0.3,
				Fill:            true,
			},
		},
	}
}

// buildMetricsHistoryChartData converts metrics snapshots into a line chart
// showing total metrics count over time.
func buildMetricsHistoryChartData(snapshots []collector.MetricsSnapshot) chart.Data {
	if len(snapshots) == 0 {
		return emptyChartData()
	}

	labels := make([]string, len(snapshots))
	totalData := make([]float64, len(snapshots))

	for i, snap := range snapshots {
		labels[i] = snap.Timestamp.Format("15:04")
		totalData[i] = float64(snap.TotalMetrics)
	}

	return chart.Data{
		Labels: labels,
		Datasets: []chart.Dataset{
			{
				Label:           "Total Metrics",
				Data:            totalData,
				BorderColor:     "#8b5cf6",
				BackgroundColor: "rgba(139, 92, 246, 0.1)",
				BorderWidth:     2,
				Tension:         0.3,
				Fill:            true,
			},
		},
	}
}

// buildMetricsTypeChartData converts a metrics-by-type map into a doughnut chart.
func buildMetricsTypeChartData(byType map[string]int) chart.Data {
	if len(byType) == 0 {
		return chart.Data{
			Labels:   []string{"No data"},
			Datasets: []chart.Dataset{{Data: []float64{1}, BackgroundColor: []interface{}{"#d1d5db"}}},
		}
	}

	// Sort keys for deterministic order
	keys := make([]string, 0, len(byType))
	for k := range byType {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	labels := make([]string, len(keys))
	data := make([]float64, len(keys))
	colors := []interface{}{"#3b82f6", "#10b981", "#f59e0b", "#8b5cf6", "#ef4444", "#06b6d4"}

	for i, k := range keys {
		labels[i] = k
		data[i] = float64(byType[k])
	}

	// Trim colors to match data length
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

// emptyChartData returns chart data with a single "No data" placeholder.
func emptyChartData() chart.Data {
	return chart.Data{
		Labels: []string{"No data yet"},
		Datasets: []chart.Dataset{
			{
				Label:           "Waiting for data",
				Data:            []float64{0},
				BorderColor:     "#d1d5db",
				BackgroundColor: "rgba(209, 213, 219, 0.2)",
				BorderWidth:     1,
			},
		},
	}
}

// hasChartData returns true if there are snapshots with non-zero data points.
func hasOverviewSnapshots(snapshots []collector.OverviewSnapshot) bool {
	return len(snapshots) > 1
}

// hasHealthSnapshots returns true if there is meaningful health history.
func hasHealthSnapshots(snapshots []collector.HealthSnapshot) bool {
	return len(snapshots) > 1
}

// hasMetricsSnapshots returns true if there is meaningful metrics history.
func hasMetricsSnapshots(snapshots []collector.MetricsSnapshot) bool {
	return len(snapshots) > 1
}

// healthPercentage calculates the percentage of healthy services.
func healthPercentage(healthy, total int) int {
	if total == 0 {
		return 0
	}

	return (healthy * 100) / total
}

// recentTime formats a time as relative "Xm ago" or "Xh ago" string.
func recentTime(t time.Time) string {
	if t.IsZero() {
		return "Never"
	}

	d := time.Since(t)

	switch {
	case d < time.Minute:
		return "Just now"
	case d < time.Hour:
		return formatDuration(d) + " ago"
	default:
		return t.Format("15:04")
	}
}
