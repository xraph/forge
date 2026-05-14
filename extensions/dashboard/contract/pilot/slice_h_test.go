package pilot

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/dashboard/collector"
	"github.com/xraph/forge/extensions/dashboard/contract"
)

// --- stub providers --------------------------------------------------------

type stubOverview struct{ data *collector.OverviewData }

func (s *stubOverview) CollectOverview(_ context.Context) *collector.OverviewData { return s.data }

type stubHealth struct{ data *collector.HealthData }

func (s *stubHealth) CollectHealth(_ context.Context) *collector.HealthData { return s.data }

type stubMetricsReport struct{ data *collector.MetricsReport }

func (s *stubMetricsReport) CollectMetricsReport(_ context.Context) *collector.MetricsReport {
	return s.data
}

type stubTraces struct {
	list  []collector.TraceSummary
	stats collector.TraceStats
	byID  map[string]*collector.TraceDetail
}

func (s *stubTraces) ListTraces(_ collector.TraceFilter) ([]collector.TraceSummary, collector.TraceStats) {
	return s.list, s.stats
}
func (s *stubTraces) GetTrace(id string) *collector.TraceDetail { return s.byID[id] }

// --- tests -----------------------------------------------------------------

func TestOverviewHandler(t *testing.T) {
	p := &stubOverview{data: &collector.OverviewData{
		OverallHealth: "healthy", TotalServices: 5, HealthyServices: 4,
		TotalMetrics: 42, Uptime: 90 * time.Second,
		Version: "v1", Environment: "dev",
	}}
	h := overviewHandler(p)
	got, err := h(context.Background(), struct{}{}, contract.Principal{})
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if got.OverallHealth != "healthy" || got.TotalServices != 5 || got.HealthyServices != 4 || got.UptimeSeconds != 90 {
		t.Errorf("projection wrong: %+v", got)
	}
}

func TestOverviewHandler_NilProviderIsUnavailable(t *testing.T) {
	_, err := overviewHandler(nil)(context.Background(), struct{}{}, contract.Principal{})
	if ce, ok := err.(*contract.Error); !ok || ce.Code != contract.CodeUnavailable {
		t.Errorf("expected CodeUnavailable, got %v", err)
	}
}

func TestHealthHandler_FlattensServicesMap(t *testing.T) {
	p := &stubHealth{data: &collector.HealthData{
		OverallStatus: "degraded",
		Services: map[string]collector.ServiceHealth{
			"db":    {Name: "db", Status: "healthy", Duration: 12 * time.Millisecond},
			"cache": {Name: "cache", Status: "degraded", Message: "slow", Duration: 200 * time.Millisecond, Critical: true},
		},
		Summary: collector.HealthSummary{Healthy: 1, Degraded: 1, Total: 2},
	}}
	h := healthHandler(p)
	got, err := h(context.Background(), struct{}{}, contract.Principal{})
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	if got.OverallStatus != "degraded" || got.Total != 2 || got.HealthySummary != 1 {
		t.Errorf("summary wrong: %+v", got)
	}
	if len(got.Services) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got.Services))
	}
	for _, e := range got.Services {
		if e.Name == "cache" {
			if e.DurationMs != 200 || !e.Critical || e.Message != "slow" {
				t.Errorf("cache entry projected wrong: %+v", e)
			}
		}
	}
}

func TestMetricsReportHandler(t *testing.T) {
	p := &stubMetricsReport{data: &collector.MetricsReport{
		TotalMetrics:  3,
		MetricsByType: map[string]int{"counter": 2, "gauge": 1},
		Collectors: []collector.CollectorInfo{
			{Name: "default", Type: "internal", MetricsCount: 3, Status: "active", LastCollection: time.Now()},
		},
		TopMetrics: []collector.MetricEntry{
			{Name: "requests", Type: "counter", Value: 100},
		},
	}}
	h := metricsReportHandler(p)
	got, err := h(context.Background(), struct{}{}, contract.Principal{})
	if err != nil {
		t.Fatalf("metrics-report: %v", err)
	}
	if got.TotalMetrics != 3 || len(got.Collectors) != 1 || len(got.TopMetrics) != 1 {
		t.Errorf("projection wrong: %+v", got)
	}
	if got.Collectors[0].LastCollection == "" {
		t.Errorf("LastCollection should be RFC3339 formatted, got empty")
	}
}

func TestTracesListHandler(t *testing.T) {
	p := &stubTraces{
		list: []collector.TraceSummary{
			{TraceID: "t1", RootSpanName: "GET /x", SpanCount: 5, Duration: 25 * time.Millisecond, Protocol: "REST"},
			{TraceID: "t2", RootSpanName: "ws.connect", SpanCount: 2, Duration: 10 * time.Millisecond, Protocol: "WS"},
		},
		stats: collector.TraceStats{TotalTraces: 2},
	}
	h := tracesListHandler(p)
	got, err := h(context.Background(), struct{}{}, contract.Principal{})
	if err != nil {
		t.Fatalf("traces.list: %v", err)
	}
	if len(got.Traces) != 2 || got.Total != 2 {
		t.Errorf("projection wrong: %+v", got)
	}
	if got.Traces[0].DurationMs != 25 {
		t.Errorf("duration projection wrong: %+v", got.Traces[0])
	}
}

func TestTraceDetailHandler_Found(t *testing.T) {
	p := &stubTraces{byID: map[string]*collector.TraceDetail{
		"t1": {TraceID: "t1", SpanCount: 3},
	}}
	h := traceDetailHandler(p)
	got, err := h(context.Background(), TraceDetailInput{ID: "t1"}, contract.Principal{})
	if err != nil {
		t.Fatalf("trace.detail: %v", err)
	}
	if got == nil || got.TraceID != "t1" {
		t.Errorf("expected detail for t1, got %+v", got)
	}
}

func TestTraceDetailHandler_NotFound(t *testing.T) {
	p := &stubTraces{byID: map[string]*collector.TraceDetail{}}
	h := traceDetailHandler(p)
	_, err := h(context.Background(), TraceDetailInput{ID: "missing"}, contract.Principal{})
	if ce, ok := err.(*contract.Error); !ok || ce.Code != contract.CodeNotFound {
		t.Errorf("expected CodeNotFound, got %v", err)
	}
}

func TestTraceDetailHandler_BadRequestOnEmptyID(t *testing.T) {
	p := &stubTraces{}
	_, err := traceDetailHandler(p)(context.Background(), TraceDetailInput{ID: ""}, contract.Principal{})
	if ce, ok := err.(*contract.Error); !ok || ce.Code != contract.CodeBadRequest {
		t.Errorf("expected CodeBadRequest, got %v", err)
	}
}
