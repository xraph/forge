package collector

import (
	"testing"
	"time"
)

func span(traceID, spanID string) *SpanView {
	now := time.Now()
	return &SpanView{
		SpanID:     spanID,
		TraceID:    traceID,
		Name:       "GET /x",
		Kind:       SpanKindServer,
		Status:     SpanStatusOK,
		StartTime:  now,
		EndTime:    now.Add(time.Millisecond),
		Duration:   time.Millisecond,
		Attributes: map[string]string{},
		Events:     []SpanEventView{},
	}
}

// One long-lived trace must not grow without limit. This is the websocket case:
// a single TraceID that keeps receiving spans for as long as the connection is up.
func TestTraceStore_CapsSpansWithinOneTrace(t *testing.T) {
	ts := NewTraceStore(10, time.Hour, WithMaxSpansPerTrace(5))

	for i := 0; i < 100; i++ {
		ts.AddSpan(span("trace-a", "span"))
	}

	ts.mu.RLock()
	got := len(ts.traces["trace-a"])
	ts.mu.RUnlock()

	if got != 5 {
		t.Errorf("kept %d spans in one trace, want the cap of 5", got)
	}
	if dropped := ts.DroppedSpans(); dropped != 95 {
		t.Errorf("counted %d dropped spans, want 95", dropped)
	}
}

// The cap is per trace, not global. Two traces under the cap both survive intact.
func TestTraceStore_CapIsPerTraceNotGlobal(t *testing.T) {
	ts := NewTraceStore(10, time.Hour, WithMaxSpansPerTrace(5))

	for i := 0; i < 4; i++ {
		ts.AddSpan(span("trace-a", "span"))
		ts.AddSpan(span("trace-b", "span"))
	}

	ts.mu.RLock()
	a, b := len(ts.traces["trace-a"]), len(ts.traces["trace-b"])
	ts.mu.RUnlock()

	if a != 4 || b != 4 {
		t.Errorf("kept %d and %d spans, want 4 and 4", a, b)
	}
	if dropped := ts.DroppedSpans(); dropped != 0 {
		t.Errorf("dropped %d spans while under the cap, want 0", dropped)
	}
}

// Callers that predate the option keep working and get the default cap.
func TestTraceStore_DefaultCapAppliesWithoutOption(t *testing.T) {
	ts := NewTraceStore(10, time.Hour)

	if ts.maxSpansPerTrace != defaultMaxSpansPerTrace {
		t.Errorf("default cap is %d, want %d", ts.maxSpansPerTrace, defaultMaxSpansPerTrace)
	}
}
