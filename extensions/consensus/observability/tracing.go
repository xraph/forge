package observability

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge"
)

// TracingManager manages distributed tracing for consensus operations.
type TracingManager struct {
	nodeID string
	logger forge.Logger

	// Trace storage
	traces   map[string]*Trace
	tracesMu sync.RWMutex

	// Configuration
	config TracingConfig

	// Statistics
	stats TracingStatistics
}

// Trace represents a distributed trace.
type Trace struct {
	TraceID     string
	OperationID string
	Operation   string
	StartTime   time.Time
	EndTime     time.Time
	Duration    time.Duration
	Spans       []*Span
	Tags        map[string]any
	Status      TraceStatus
	mu          sync.RWMutex
}

// Span represents a span within a trace.
type Span struct {
	SpanID    string
	ParentID  string
	TraceID   string
	Operation string
	StartTime time.Time
	EndTime   time.Time
	Duration  time.Duration
	Tags      map[string]any
	Logs      []SpanLog
	Status    SpanStatus
}

// SpanLog represents a log entry within a span.
type SpanLog struct {
	Timestamp time.Time
	Message   string
	Fields    map[string]any
}

// TraceStatus represents trace status.
type TraceStatus string

const (
	// TraceStatusActive trace is active.
	TraceStatusActive TraceStatus = "active"
	// TraceStatusComplete trace completed successfully.
	TraceStatusComplete TraceStatus = "complete"
	// TraceStatusError trace completed with error.
	TraceStatusError TraceStatus = "error"
)

// SpanStatus represents span status.
type SpanStatus string

const (
	// SpanStatusActive span is active.
	SpanStatusActive SpanStatus = "active"
	// SpanStatusComplete span completed successfully.
	SpanStatusComplete SpanStatus = "complete"
	// SpanStatusError span completed with error.
	SpanStatusError SpanStatus = "error"
)

// TracingConfig contains tracing configuration.
type TracingConfig struct {
	Enabled          bool
	SampleRate       float64 // 0.0 to 1.0
	MaxTraces        int
	MaxSpansPerTrace int
}

// TracingStatistics contains tracing statistics.
type TracingStatistics struct {
	TotalTraces     int64
	ActiveTraces    int64
	CompletedTraces int64
	ErrorTraces     int64
	TotalSpans      int64
	AverageDuration time.Duration
}

// NewTracingManager creates a new tracing manager.
func NewTracingManager(config TracingConfig, logger forge.Logger) *TracingManager {
	// Set defaults
	if config.MaxTraces == 0 {
		config.MaxTraces = 1000
	}

	if config.MaxSpansPerTrace == 0 {
		config.MaxSpansPerTrace = 100
	}

	if config.SampleRate == 0 {
		config.SampleRate = 1.0 // Trace everything by default
	}

	return &TracingManager{
		logger: logger,
		traces: make(map[string]*Trace),
		config: config,
	}
}

// StartTrace starts a new trace.
func (tm *TracingManager) StartTrace(ctx context.Context, operation string) (*Trace, context.Context) {
	if !tm.config.Enabled {
		return nil, ctx
	}

	// Check sample rate
	if !tm.shouldSample() {
		return nil, ctx
	}

	traceID := tm.generateTraceID()
	operationID := fmt.Sprintf("%s-%d", operation, time.Now().UnixNano())

	trace := &Trace{
		TraceID:     traceID,
		OperationID: operationID,
		Operation:   operation,
		StartTime:   time.Now(),
		Status:      TraceStatusActive,
		Tags:        make(map[string]any),
		Spans:       make([]*Span, 0),
	}

	tm.tracesMu.Lock()
	tm.traces[traceID] = trace
	tm.stats.TotalTraces++
	tm.stats.ActiveTraces++
	tm.tracesMu.Unlock()

	// Store trace in context
	ctx = context.WithValue(ctx, "trace_id", traceID)
	ctx = context.WithValue(ctx, "trace", trace)

	tm.logger.Debug("trace started",
		forge.F("trace_id", traceID),
		forge.F("operation", operation),
	)

	return trace, ctx
}

// EndTrace ends a trace.
func (tm *TracingManager) EndTrace(trace *Trace, err error) {
	if !tm.config.Enabled || trace == nil {
		return
	}

	trace.mu.Lock()
	trace.EndTime = time.Now()
	trace.Duration = trace.EndTime.Sub(trace.StartTime)

	if err != nil {
		trace.Status = TraceStatusError
		trace.Tags["error"] = err.Error()
		tm.stats.ErrorTraces++
	} else {
		trace.Status = TraceStatusComplete
		tm.stats.CompletedTraces++
	}

	trace.mu.Unlock()

	tm.tracesMu.Lock()
	tm.stats.ActiveTraces--

	// Update average duration
	if tm.stats.CompletedTraces > 0 {
		totalDuration := time.Duration(tm.stats.AverageDuration.Nanoseconds() * int64(tm.stats.CompletedTraces-1))
		totalDuration += trace.Duration
		tm.stats.AverageDuration = totalDuration / time.Duration(tm.stats.CompletedTraces)
	}

	tm.tracesMu.Unlock()

	tm.logger.Debug("trace ended",
		forge.F("trace_id", trace.TraceID),
		forge.F("operation", trace.Operation),
		forge.F("duration", trace.Duration),
		forge.F("status", trace.Status),
		forge.F("spans", len(trace.Spans)),
	)
}

// StartSpan starts a new span within a trace.
func (tm *TracingManager) StartSpan(ctx context.Context, operation string) (*Span, context.Context) {
	if !tm.config.Enabled {
		return nil, ctx
	}

	// Get trace from context
	trace, ok := ctx.Value("trace").(*Trace)
	if !ok || trace == nil {
		return nil, ctx
	}

	// Get parent span if exists
	parentSpanID := ""
	if parentSpan, ok := ctx.Value("span").(*Span); ok {
		parentSpanID = parentSpan.SpanID
	}

	spanID := tm.generateSpanID()

	span := &Span{
		SpanID:    spanID,
		ParentID:  parentSpanID,
		TraceID:   trace.TraceID,
		Operation: operation,
		StartTime: time.Now(),
		Status:    SpanStatusActive,
		Tags:      make(map[string]any),
		Logs:      make([]SpanLog, 0),
	}

	trace.mu.Lock()

	if len(trace.Spans) < tm.config.MaxSpansPerTrace {
		trace.Spans = append(trace.Spans, span)
		tm.stats.TotalSpans++
	}

	trace.mu.Unlock()

	// Store span in context
	ctx = context.WithValue(ctx, "span", span)
	ctx = context.WithValue(ctx, "span_id", spanID)

	return span, ctx
}

// EndSpan ends a span.
func (tm *TracingManager) EndSpan(span *Span, err error) {
	if !tm.config.Enabled || span == nil {
		return
	}

	span.EndTime = time.Now()
	span.Duration = span.EndTime.Sub(span.StartTime)

	if err != nil {
		span.Status = SpanStatusError
		span.Tags["error"] = err.Error()
	} else {
		span.Status = SpanStatusComplete
	}
}

// AddSpanLog adds a log to a span.
func (tm *TracingManager) AddSpanLog(span *Span, message string, fields map[string]any) {
	if !tm.config.Enabled || span == nil {
		return
	}

	span.Logs = append(span.Logs, SpanLog{
		Timestamp: time.Now(),
		Message:   message,
		Fields:    fields,
	})
}

// SetTraceTag sets a tag on a trace.
func (tm *TracingManager) SetTraceTag(trace *Trace, key string, value any) {
	if !tm.config.Enabled || trace == nil {
		return
	}

	trace.mu.Lock()
	defer trace.mu.Unlock()

	trace.Tags[key] = value
}

// SetSpanTag sets a tag on a span.
func (tm *TracingManager) SetSpanTag(span *Span, key string, value any) {
	if !tm.config.Enabled || span == nil {
		return
	}

	span.Tags[key] = value
}

// GetTrace retrieves a trace by ID.
func (tm *TracingManager) GetTrace(traceID string) (*Trace, error) {
	tm.tracesMu.RLock()
	defer tm.tracesMu.RUnlock()

	trace, exists := tm.traces[traceID]
	if !exists {
		return nil, fmt.Errorf("trace not found: %s", traceID)
	}

	return trace, nil
}

// GetActiveTraces returns all active traces.
func (tm *TracingManager) GetActiveTraces() []*Trace {
	tm.tracesMu.RLock()
	defer tm.tracesMu.RUnlock()

	var active []*Trace

	for _, trace := range tm.traces {
		trace.mu.RLock()

		if trace.Status == TraceStatusActive {
			active = append(active, trace)
		}

		trace.mu.RUnlock()
	}

	return active
}

// GetStatistics returns tracing statistics.
func (tm *TracingManager) GetStatistics() TracingStatistics {
	tm.tracesMu.RLock()
	defer tm.tracesMu.RUnlock()

	return tm.stats
}

// ClearOldTraces clears traces older than duration.
func (tm *TracingManager) ClearOldTraces(olderThan time.Duration) int {
	tm.tracesMu.Lock()
	defer tm.tracesMu.Unlock()

	cleared := 0
	cutoff := time.Now().Add(-olderThan)

	for traceID, trace := range tm.traces {
		trace.mu.RLock()

		if trace.EndTime.Before(cutoff) && trace.Status != TraceStatusActive {
			delete(tm.traces, traceID)

			cleared++
		}

		trace.mu.RUnlock()
	}

	tm.logger.Debug("old traces cleared",
		forge.F("cleared", cleared),
		forge.F("cutoff", cutoff),
	)

	return cleared
}

// ExportTrace exports a trace in a structured format.
func (tm *TracingManager) ExportTrace(traceID string) (map[string]any, error) {
	trace, err := tm.GetTrace(traceID)
	if err != nil {
		return nil, err
	}

	trace.mu.RLock()
	defer trace.mu.RUnlock()

	spans := make([]map[string]any, len(trace.Spans))
	for i, span := range trace.Spans {
		spans[i] = map[string]any{
			"span_id":    span.SpanID,
			"parent_id":  span.ParentID,
			"operation":  span.Operation,
			"start_time": span.StartTime,
			"end_time":   span.EndTime,
			"duration":   span.Duration,
			"status":     span.Status,
			"tags":       span.Tags,
			"logs":       span.Logs,
		}
	}

	return map[string]any{
		"trace_id":     trace.TraceID,
		"operation_id": trace.OperationID,
		"operation":    trace.Operation,
		"start_time":   trace.StartTime,
		"end_time":     trace.EndTime,
		"duration":     trace.Duration,
		"status":       trace.Status,
		"tags":         trace.Tags,
		"spans":        spans,
	}, nil
}

// shouldSample determines if an operation should be sampled.
func (tm *TracingManager) shouldSample() bool {
	// Simple implementation - can be enhanced with more sophisticated sampling
	if tm.config.SampleRate >= 1.0 {
		return true
	}

	if tm.config.SampleRate <= 0.0 {
		return false
	}

	// Use time-based sampling for simplicity
	return (time.Now().UnixNano() % 100) < int64(tm.config.SampleRate*100)
}

// generateTraceID generates a unique trace ID.
func (tm *TracingManager) generateTraceID() string {
	return fmt.Sprintf("trace-%d-%d", time.Now().UnixNano(), time.Now().Unix())
}

// generateSpanID generates a unique span ID.
func (tm *TracingManager) generateSpanID() string {
	return fmt.Sprintf("span-%d", time.Now().UnixNano())
}

// GetTraceContext retrieves trace context from context.Context.
func GetTraceContext(ctx context.Context) (string, bool) {
	traceID, ok := ctx.Value("trace_id").(string)

	return traceID, ok
}

// GetSpanContext retrieves span context from context.Context.
func GetSpanContext(ctx context.Context) (string, bool) {
	spanID, ok := ctx.Value("span_id").(string)

	return spanID, ok
}
