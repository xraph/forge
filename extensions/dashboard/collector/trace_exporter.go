package collector

import (
	"context"

	"github.com/xraph/forge/internal/observability"
)

// DashboardSpanExporter implements observability.SpanExporter and feeds
// completed spans into a TraceStore for the dashboard tracing UI.
type DashboardSpanExporter struct {
	store *TraceStore
}

// NewDashboardSpanExporter creates a new exporter backed by the given TraceStore.
func NewDashboardSpanExporter(store *TraceStore) *DashboardSpanExporter {
	return &DashboardSpanExporter{store: store}
}

// ExportSpans converts observability spans to SpanView and adds them to the store.
func (e *DashboardSpanExporter) ExportSpans(_ context.Context, spans []*observability.Span) error {
	for _, s := range spans {
		view := convertSpan(s)
		e.store.AddSpan(view)
	}
	return nil
}

// Shutdown is a no-op for the in-memory dashboard exporter.
func (e *DashboardSpanExporter) Shutdown(_ context.Context) error {
	return nil
}

// convertSpan maps an observability.Span to a collector.SpanView.
func convertSpan(s *observability.Span) *SpanView {
	attrs := make(map[string]string, len(s.Attributes))
	for k, v := range s.Attributes {
		attrs[k] = v
	}

	events := make([]SpanEventView, 0, len(s.Events))
	for _, ev := range s.Events {
		evAttrs := make(map[string]string, len(ev.Attributes))
		for k, v := range ev.Attributes {
			evAttrs[k] = v
		}
		events = append(events, SpanEventView{
			Name:       ev.Name,
			Timestamp:  ev.Timestamp,
			Attributes: evAttrs,
		})
	}

	return &SpanView{
		SpanID:       s.SpanID,
		ParentSpanID: s.ParentSpanID,
		TraceID:      s.TraceID,
		Name:         s.Name,
		Kind:         SpanKind(s.Kind),
		Status:       SpanStatus(s.Status),
		StartTime:    s.StartTime,
		EndTime:      s.EndTime,
		Duration:     s.Duration,
		Attributes:   attrs,
		Events:       events,
	}
}
