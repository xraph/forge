package collector

import (
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// OnTraceAddedFunc is called when a new span is added to the store.
type OnTraceAddedFunc func(traceID string, spanCount int)

// defaultMaxSpansPerTrace bounds a single trace. A websocket or SSE connection
// reuses one TraceID for its whole life, so without this a single connection
// grows the store until the retention window rolls.
const defaultMaxSpansPerTrace = 200

// notifyBuffer is how many trace-added events queue before new ones are
// dropped. Notifications are advisory: the dashboard re-reads the store on the
// next poll, so dropping one costs a moment of staleness and nothing more.
const notifyBuffer = 256

type traceNotification struct {
	traceID   string
	spanCount int
}

// TraceStore is a thread-safe in-memory store for completed traces.
// It organises spans by TraceID and supports listing, filtering, and
// detail retrieval for the dashboard tracing UI.
type TraceStore struct {
	maxTraces        int
	maxSpansPerTrace int
	retention        time.Duration

	// traces maps TraceID → ordered list of spans in that trace.
	traces map[string][]*SpanView
	// order tracks insertion order of TraceIDs for FIFO eviction.
	order []string

	mu           sync.RWMutex
	onTraceAdded OnTraceAddedFunc

	// droppedSpans counts spans refused because their trace was already at
	// maxSpansPerTrace. Read it with DroppedSpans.
	droppedSpans atomic.Uint64

	// notify carries trace-added events to the single drain goroutine. AddSpan
	// sends without blocking, so a stuck callback costs dropped notifications
	// instead of goroutines on the request path.
	notify    chan traceNotification
	startOnce sync.Once
	closeOnce sync.Once
	stop      chan struct{}

	droppedNotifications atomic.Uint64
}

// TraceStoreOption configures a TraceStore at construction.
type TraceStoreOption func(*TraceStore)

// WithMaxSpansPerTrace caps how many spans a single trace retains. Spans
// arriving after the cap are dropped and counted. Values below 1 are ignored.
func WithMaxSpansPerTrace(n int) TraceStoreOption {
	return func(ts *TraceStore) {
		if n > 0 {
			ts.maxSpansPerTrace = n
		}
	}
}

// NewTraceStore creates a new TraceStore.
// maxTraces controls the maximum number of distinct traces kept in memory.
// retention controls the maximum age of traces before they are evicted.
func NewTraceStore(maxTraces int, retention time.Duration, opts ...TraceStoreOption) *TraceStore {
	if maxTraces <= 0 {
		maxTraces = 1000
	}
	if retention <= 0 {
		retention = time.Hour
	}
	ts := &TraceStore{
		maxTraces:        maxTraces,
		maxSpansPerTrace: defaultMaxSpansPerTrace,
		retention:        retention,
		traces:           make(map[string][]*SpanView),
		order:            make([]string, 0, maxTraces),
		notify:           make(chan traceNotification, notifyBuffer),
		stop:             make(chan struct{}),
	}
	for _, opt := range opts {
		opt(ts)
	}
	return ts
}

// DroppedSpans returns the number of spans refused because their trace had
// already reached maxSpansPerTrace.
func (ts *TraceStore) DroppedSpans() uint64 {
	return ts.droppedSpans.Load()
}

// SetOnTraceAdded registers a callback invoked when a new span is added.
// The callback runs on a single drain goroutine, started on first use, and is
// never invoked from the request path. Call Close to stop that goroutine.
func (ts *TraceStore) SetOnTraceAdded(fn OnTraceAddedFunc) {
	ts.mu.Lock()
	ts.onTraceAdded = fn
	ts.mu.Unlock()

	ts.startOnce.Do(func() { go ts.drainNotifications() })
}

// drainNotifications runs the registered callback for queued events, one at a
// time, until Close is called.
func (ts *TraceStore) drainNotifications() {
	for {
		select {
		case <-ts.stop:
			return
		case n := <-ts.notify:
			ts.mu.RLock()
			cb := ts.onTraceAdded
			ts.mu.RUnlock()
			if cb != nil {
				cb(n.traceID, n.spanCount)
			}
		}
	}
}

// Close stops the drain goroutine. It is safe to call more than once, and safe
// to call on a store that never had a callback registered.
func (ts *TraceStore) Close() {
	ts.closeOnce.Do(func() { close(ts.stop) })
}

// DroppedNotifications returns the number of trace-added events discarded
// because the drain queue was full.
func (ts *TraceStore) DroppedNotifications() uint64 {
	return ts.droppedNotifications.Load()
}

// AddSpan adds a completed span to the store, grouping it under its TraceID.
func (ts *TraceStore) AddSpan(span *SpanView) {
	if span == nil || span.TraceID == "" {
		return
	}

	ts.mu.Lock()

	// If this is a new trace, track its order.
	if _, exists := ts.traces[span.TraceID]; !exists {
		ts.order = append(ts.order, span.TraceID)
	}

	existing := ts.traces[span.TraceID]
	if len(existing) >= ts.maxSpansPerTrace {
		// Still evict. Retention is a separate guarantee from the cap, and a
		// store whose only traffic is one saturated trace would otherwise stop
		// reclaiming expired traces entirely.
		ts.evict()
		ts.mu.Unlock()
		ts.droppedSpans.Add(1)
		return
	}
	ts.traces[span.TraceID] = append(existing, span)

	// Evict old traces.
	ts.evict()

	hasCallback := ts.onTraceAdded != nil
	traceID := span.TraceID
	spanCount := len(ts.traces[traceID])

	ts.mu.Unlock()

	if !hasCallback {
		return
	}

	// Never block the request path. A full queue means the consumer is behind,
	// and a dropped notification costs staleness until the next poll.
	select {
	case ts.notify <- traceNotification{traceID: traceID, spanCount: spanCount}:
	default:
		ts.droppedNotifications.Add(1)
	}
}

// ListTraces returns a filtered, paginated list of trace summaries plus
// aggregate statistics.
func (ts *TraceStore) ListTraces(filter TraceFilter) ([]TraceSummary, TraceStats) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	// Build summaries for all traces (newest first).
	summaries := make([]TraceSummary, 0, len(ts.traces))
	var totalDuration time.Duration
	errorCount := 0

	for i := len(ts.order) - 1; i >= 0; i-- {
		traceID := ts.order[i]
		spans := ts.traces[traceID]
		if len(spans) == 0 {
			continue
		}

		summary := ts.buildSummary(traceID, spans)

		// Apply filters.
		if !ts.matchesFilter(summary, filter) {
			continue
		}

		summaries = append(summaries, summary)
		totalDuration += summary.Duration
		if summary.Status == SpanStatusError {
			errorCount++
		}
	}

	// Compute stats from the full filtered set.
	stats := TraceStats{
		TotalTraces:  len(summaries),
		ActiveTraces: 0, // We only store completed spans.
	}
	if len(summaries) > 0 {
		stats.AvgDuration = totalDuration / time.Duration(len(summaries))
		stats.ErrorRate = float64(errorCount) / float64(len(summaries))
	}

	// Apply pagination.
	offset := filter.Offset
	if offset > len(summaries) {
		offset = len(summaries)
	}
	summaries = summaries[offset:]

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > len(summaries) {
		limit = len(summaries)
	}
	summaries = summaries[:limit]

	return summaries, stats
}

// GetTrace returns the full detail for a single trace, including computed
// waterfall display fields.
func (ts *TraceStore) GetTrace(traceID string) *TraceDetail {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	spans, exists := ts.traces[traceID]
	if !exists || len(spans) == 0 {
		return nil
	}

	return ts.buildTraceDetail(traceID, spans)
}

// --- internal helpers ---

// buildSummary builds a TraceSummary from a set of spans.
func (ts *TraceStore) buildSummary(traceID string, spans []*SpanView) TraceSummary {
	var (
		rootName string
		earliest = spans[0].StartTime
		latest   = spans[0].EndTime
		status   = SpanStatusUnset
		protocol string
	)

	for _, s := range spans {
		if s.ParentSpanID == "" {
			rootName = s.Name
			protocol = s.Attributes["protocol"]
		}
		if s.StartTime.Before(earliest) {
			earliest = s.StartTime
		}
		if s.EndTime.After(latest) {
			latest = s.EndTime
		}
		if s.Status == SpanStatusError {
			status = SpanStatusError
		} else if s.Status == SpanStatusOK && status != SpanStatusError {
			status = SpanStatusOK
		}
	}

	if rootName == "" {
		rootName = spans[0].Name
	}
	if protocol == "" {
		protocol = inferProtocol(spans)
	}

	return TraceSummary{
		TraceID:      traceID,
		RootSpanName: rootName,
		SpanCount:    len(spans),
		Duration:     latest.Sub(earliest),
		Status:       status,
		StartTime:    earliest,
		Protocol:     protocol,
	}
}

// buildTraceDetail builds a full TraceDetail with computed waterfall positions.
func (ts *TraceStore) buildTraceDetail(traceID string, spans []*SpanView) *TraceDetail {
	// Sort spans by start time.
	sorted := make([]*SpanView, len(spans))
	copy(sorted, spans)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].StartTime.Before(sorted[j].StartTime)
	})

	// Find trace-level boundaries.
	var (
		earliest = sorted[0].StartTime
		latest   = sorted[0].EndTime
		status   = SpanStatusUnset
		protocol string
		root     *SpanView
	)

	for _, s := range sorted {
		if s.StartTime.Before(earliest) {
			earliest = s.StartTime
		}
		if s.EndTime.After(latest) {
			latest = s.EndTime
		}
		if s.Status == SpanStatusError {
			status = SpanStatusError
		} else if s.Status == SpanStatusOK && status != SpanStatusError {
			status = SpanStatusOK
		}
		if s.ParentSpanID == "" {
			root = s
			protocol = s.Attributes["protocol"]
		}
	}

	if root == nil {
		root = sorted[0]
	}
	if protocol == "" {
		protocol = inferProtocol(sorted)
	}

	totalDuration := latest.Sub(earliest)

	// Build parent → children index for depth computation.
	children := make(map[string][]string)
	spanByID := make(map[string]*SpanView, len(sorted))
	for _, s := range sorted {
		spanByID[s.SpanID] = s
		children[s.ParentSpanID] = append(children[s.ParentSpanID], s.SpanID)
	}

	// Compute depth via BFS from root.
	type entry struct {
		spanID string
		depth  int
	}
	queue := []entry{{spanID: root.SpanID, depth: 0}}
	visited := make(map[string]bool)
	for len(queue) > 0 {
		e := queue[0]
		queue = queue[1:]
		if visited[e.spanID] {
			continue
		}
		visited[e.spanID] = true
		if s, ok := spanByID[e.spanID]; ok {
			s.Depth = e.depth
		}
		for _, childID := range children[e.spanID] {
			queue = append(queue, entry{spanID: childID, depth: e.depth + 1})
		}
	}

	// Assign depth 0 for any orphan spans not reachable from root.
	for _, s := range sorted {
		if !visited[s.SpanID] {
			s.Depth = 0
		}
	}

	// Compute waterfall percentages.
	if totalDuration > 0 {
		for _, s := range sorted {
			s.OffsetPercent = float64(s.StartTime.Sub(earliest)) / float64(totalDuration) * 100
			s.WidthPercent = float64(s.Duration) / float64(totalDuration) * 100
			if s.WidthPercent < 0.5 {
				s.WidthPercent = 0.5 // minimum visible width
			}
		}
	}

	return &TraceDetail{
		TraceID:   traceID,
		RootSpan:  root,
		Spans:     sorted,
		SpanCount: len(sorted),
		Duration:  totalDuration,
		Status:    status,
		StartTime: earliest,
		EndTime:   latest,
		Protocol:  protocol,
	}
}

// matchesFilter checks if a summary passes the given filter.
func (ts *TraceStore) matchesFilter(s TraceSummary, f TraceFilter) bool {
	if f.Search != "" {
		search := strings.ToLower(f.Search)
		if !strings.Contains(strings.ToLower(s.RootSpanName), search) &&
			!strings.Contains(strings.ToLower(s.TraceID), search) {
			return false
		}
	}
	if f.Status != "" && f.Status != "all" {
		switch f.Status {
		case "ok":
			if s.Status != SpanStatusOK {
				return false
			}
		case "error":
			if s.Status != SpanStatusError {
				return false
			}
		}
	}
	if f.Protocol != "" && f.Protocol != "all" {
		if !strings.EqualFold(s.Protocol, f.Protocol) {
			return false
		}
	}
	if f.MinDuration > 0 && s.Duration < f.MinDuration {
		return false
	}
	return true
}

// evict removes traces that exceed the capacity or retention limits.
// It copies remaining order entries to a new slice to release the old backing array.
func (ts *TraceStore) evict() {
	cutoff := time.Now().Add(-ts.retention)

	// Remove expired traces (walk from oldest).
	newStart := 0
	for newStart < len(ts.order) {
		traceID := ts.order[newStart]
		spans := ts.traces[traceID]
		if len(spans) == 0 || spans[0].StartTime.Before(cutoff) {
			delete(ts.traces, traceID)
			newStart++
		} else {
			break
		}
	}

	// Remove oldest traces if over capacity.
	remaining := len(ts.order) - newStart
	for remaining > ts.maxTraces {
		traceID := ts.order[newStart]
		delete(ts.traces, traceID)
		newStart++
		remaining--
	}

	// Copy to new slice to release old backing array.
	if newStart > 0 {
		newOrder := make([]string, remaining, ts.maxTraces)
		copy(newOrder, ts.order[newStart:])
		ts.order = newOrder
	}
}

// inferProtocol attempts to determine the protocol from span attributes.
func inferProtocol(spans []*SpanView) string {
	for _, s := range spans {
		if p, ok := s.Attributes["protocol"]; ok && p != "" {
			return p
		}
		if _, ok := s.Attributes["http.method"]; ok {
			return "REST"
		}
		if _, ok := s.Attributes["ws.type"]; ok {
			return "WS"
		}
		if _, ok := s.Attributes["sse.event"]; ok {
			return "SSE"
		}
		if _, ok := s.Attributes["event.type"]; ok {
			return "Event"
		}
	}
	return "Internal"
}
