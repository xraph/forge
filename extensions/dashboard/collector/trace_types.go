package collector

import "time"

// SpanKind represents the kind of span.
type SpanKind int

const (
	SpanKindUnspecified SpanKind = iota
	SpanKindInternal
	SpanKindServer
	SpanKindClient
	SpanKindProducer
	SpanKindConsumer
)

// SpanStatus represents the status of a span.
type SpanStatus int

const (
	SpanStatusUnset SpanStatus = iota
	SpanStatusOK
	SpanStatusError
)

// TraceSummary is a condensed representation of a complete trace for list views.
type TraceSummary struct {
	TraceID      string        `json:"trace_id"`
	RootSpanName string        `json:"root_span_name"`
	SpanCount    int           `json:"span_count"`
	Duration     time.Duration `json:"duration"`
	Status       SpanStatus    `json:"status"`
	StartTime    time.Time     `json:"start_time"`
	Protocol     string        `json:"protocol"` // REST, WS, SSE, Event, Internal
}

// TraceDetail contains a full trace with all spans for the detail view.
type TraceDetail struct {
	TraceID   string        `json:"trace_id"`
	RootSpan  *SpanView     `json:"root_span"`
	Spans     []*SpanView   `json:"spans"`
	SpanCount int           `json:"span_count"`
	Duration  time.Duration `json:"duration"`
	Status    SpanStatus    `json:"status"`
	StartTime time.Time     `json:"start_time"`
	EndTime   time.Time     `json:"end_time"`
	Protocol  string        `json:"protocol"`
}

// SpanView is a display-friendly span with computed fields for waterfall rendering.
type SpanView struct {
	SpanID       string            `json:"span_id"`
	ParentSpanID string            `json:"parent_span_id,omitempty"`
	TraceID      string            `json:"trace_id"`
	Name         string            `json:"name"`
	Kind         SpanKind          `json:"kind"`
	Status       SpanStatus        `json:"status"`
	StartTime    time.Time         `json:"start_time"`
	EndTime      time.Time         `json:"end_time"`
	Duration     time.Duration     `json:"duration"`
	Attributes   map[string]string `json:"attributes"`
	Events       []SpanEventView   `json:"events"`

	// Computed display fields (populated by TraceStore.buildTraceDetail)
	Depth         int     `json:"depth"`
	OffsetPercent float64 `json:"offset_percent"`
	WidthPercent  float64 `json:"width_percent"`
}

// SpanEventView is a display-friendly span event.
type SpanEventView struct {
	Name       string            `json:"name"`
	Timestamp  time.Time         `json:"timestamp"`
	Attributes map[string]string `json:"attributes"`
}

// TraceFilter holds filtering options for trace queries.
type TraceFilter struct {
	Search      string        `json:"search,omitempty"`
	Status      string        `json:"status,omitempty"`   // "ok", "error", "all"
	Protocol    string        `json:"protocol,omitempty"` // "REST", "WS", "SSE", "Event", "all"
	MinDuration time.Duration `json:"min_duration,omitempty"`
	Limit       int           `json:"limit,omitempty"`
	Offset      int           `json:"offset,omitempty"`
}

// TraceStats holds aggregate trace statistics.
type TraceStats struct {
	TotalTraces  int           `json:"total_traces"`
	AvgDuration  time.Duration `json:"avg_duration"`
	ErrorRate    float64       `json:"error_rate"`
	ActiveTraces int           `json:"active_traces"`
}
