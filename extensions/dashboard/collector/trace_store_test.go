package collector

import (
	"fmt"
	"runtime"
	"sync"
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

	for range 100 {
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

	for range 4 {
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

// Adding spans must not scale goroutines with the number of spans. Before this
// fix AddSpan spawned one goroutine per span to notify SSE subscribers. The
// callback sleeps so that, under the old per-span-goroutine code, spawned
// goroutines would visibly pile up instead of retiring before the next
// sample; the peak is tracked across the loop rather than sampled once at
// the end, since accumulation is transient and a single post-loop reading
// can miss it.
func TestTraceStore_NotificationDoesNotSpawnGoroutinePerSpan(t *testing.T) {
	ts := NewTraceStore(1000, time.Hour, WithMaxSpansPerTrace(10000))

	var mu sync.Mutex

	seen := 0

	ts.SetOnTraceAdded(func(traceID string, spanCount int) {
		time.Sleep(time.Millisecond)
		mu.Lock()
		seen++
		mu.Unlock()
	})
	defer ts.Close()

	// Let the drain goroutine reach its parked state before measuring.
	time.Sleep(50 * time.Millisecond)
	runtime.GC()

	before := runtime.NumGoroutine()
	peak := before

	for i := range 500 {
		ts.AddSpan(span("trace-a", "span"))

		if i%100 == 0 {
			if n := runtime.NumGoroutine(); n > peak {
				peak = n
			}
		}
	}

	grew := peak - before
	if grew > 1 {
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		t.Errorf("500 spans added %d goroutines at peak, want at most 1\n%s", grew, buf[:n])
	}

	// The callback should have run for at least some spans. Notifications are
	// allowed to drop under pressure, so this asserts liveness, not a count.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := seen
		mu.Unlock()

		if n > 0 {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Error("callback never fired for any span")
}

// A blocked callback must not block the request path or accumulate goroutines.
func TestTraceStore_SlowCallbackDropsRatherThanBlocks(t *testing.T) {
	ts := NewTraceStore(1000, time.Hour, WithMaxSpansPerTrace(10000))

	release := make(chan struct{})

	ts.SetOnTraceAdded(func(traceID string, spanCount int) { <-release })

	defer func() { close(release); ts.Close() }()

	done := make(chan struct{})

	go func() {
		for range 5000 {
			ts.AddSpan(span("trace-a", "span"))
		}

		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("AddSpan blocked behind a stuck callback")
	}

	if ts.DroppedNotifications() == 0 {
		t.Error("no notifications dropped while the callback was stuck, want some")
	}
}

// Close must be safe to call more than once.
func TestTraceStore_CloseIsIdempotent(t *testing.T) {
	ts := NewTraceStore(10, time.Hour)
	ts.SetOnTraceAdded(func(string, int) {})
	ts.Close()
	ts.Close()
}

// With the gate closed, spans must not reach the store at all. This is the
// common case: a service nobody has the dashboard open against.
func TestTraceStore_ClosedGateRejectsSpans(t *testing.T) {
	ts := NewTraceStore(10, time.Hour)
	ts.SetIngestGate(func() bool { return false })

	for range 50 {
		ts.AddSpan(span("trace-a", "span"))
	}

	ts.mu.RLock()
	n := len(ts.traces)
	ts.mu.RUnlock()

	if n != 0 {
		t.Errorf("stored %d traces with the gate closed, want 0", n)
	}
}

// Clearing the gate restores accept-all, and swapping it while spans arrive
// must be race free now that Accepting reads it without a lock.
func TestTraceStore_NilGateAcceptsAndSwapIsRaceFree(t *testing.T) {
	ts := NewTraceStore(10, time.Hour)
	ts.SetIngestGate(func() bool { return false })

	if ts.Accepting() {
		t.Fatal("closed gate is accepting")
	}

	ts.SetIngestGate(nil)

	if !ts.Accepting() {
		t.Fatal("nil gate must accept everything")
	}

	done := make(chan struct{})

	go func() {
		defer close(done)

		for i := range 1000 {
			if i%2 == 0 {
				ts.SetIngestGate(func() bool { return true })
			} else {
				ts.SetIngestGate(nil)
			}
		}
	}()

	for !isClosed(done) {
		_ = ts.Accepting()
	}
}

func isClosed(c <-chan struct{}) bool {
	select {
	case <-c:
		return true
	default:
		return false
	}
}

func TestTraceStore_OpenGateAcceptsSpans(t *testing.T) {
	ts := NewTraceStore(10, time.Hour)
	ts.SetIngestGate(func() bool { return true })

	ts.AddSpan(span("trace-a", "span"))

	ts.mu.RLock()
	n := len(ts.traces["trace-a"])
	ts.mu.RUnlock()

	if n != 1 {
		t.Errorf("stored %d spans with the gate open, want 1", n)
	}
}

// A store with no gate set must keep its current behaviour, so that callers
// which never call SetIngestGate are unaffected.
func TestTraceStore_NoGateAcceptsSpans(t *testing.T) {
	ts := NewTraceStore(10, time.Hour)

	ts.AddSpan(span("trace-a", "span"))

	ts.mu.RLock()
	n := len(ts.traces["trace-a"])
	ts.mu.RUnlock()

	if n != 1 {
		t.Errorf("stored %d spans with no gate set, want 1", n)
	}
}

// Gate rejections are not span drops. DroppedSpans counts spans refused by the
// per-trace cap, and conflating the two would hide a real capacity problem.
func TestTraceStore_GateRejectionIsNotCountedAsDroppedSpan(t *testing.T) {
	ts := NewTraceStore(10, time.Hour)
	ts.SetIngestGate(func() bool { return false })

	for range 50 {
		ts.AddSpan(span("trace-a", "span"))
	}

	if got := ts.DroppedSpans(); got != 0 {
		t.Errorf("counted %d dropped spans from gate rejections, want 0", got)
	}
}

// A store nobody has touched reports the zero time, so a TTL gate built on it
// is closed at startup. A service that never opens the dashboard retains nothing.
func TestTraceStore_LastAccessedIsZeroBeforeAnyAccess(t *testing.T) {
	ts := NewTraceStore(10, time.Hour)

	if got := ts.LastAccessed(); !got.IsZero() {
		t.Errorf("LastAccessed on a fresh store is %v, want the zero time", got)
	}
}

func TestTraceStore_MarkAccessedRecordsTheTime(t *testing.T) {
	ts := NewTraceStore(10, time.Hour)

	before := time.Now()

	ts.MarkAccessed()

	after := time.Now()

	got := ts.LastAccessed()
	if got.Before(before) || got.After(after) {
		t.Errorf("LastAccessed is %v, want between %v and %v", got, before, after)
	}
}

// The composed TTL gate: shut before anyone looks, open just after, shut again
// once the window lapses.
func TestTraceStore_TTLGateOpensOnAccessAndLapses(t *testing.T) {
	ts := NewTraceStore(10, time.Hour)

	const ttl = 50 * time.Millisecond

	gate := func() bool { return time.Since(ts.LastAccessed()) < ttl }
	ts.SetIngestGate(gate)

	ts.AddSpan(span("trace-a", "span"))
	ts.mu.RLock()
	n := len(ts.traces)
	ts.mu.RUnlock()

	if n != 0 {
		t.Errorf("stored %d traces before any dashboard access, want 0", n)
	}

	ts.MarkAccessed()
	ts.AddSpan(span("trace-b", "span"))
	ts.mu.RLock()
	n = len(ts.traces)
	ts.mu.RUnlock()

	if n != 1 {
		t.Errorf("stored %d traces just after access, want 1", n)
	}

	time.Sleep(2 * ttl)
	ts.AddSpan(span("trace-c", "span"))
	ts.mu.RLock()
	n = len(ts.traces)
	ts.mu.RUnlock()

	if n != 1 {
		t.Errorf("stored %d traces after the window lapsed, want the original 1", n)
	}
}

// TestTraceStore_EvictsExpiredTraceEvenWhileAnotherTraceIsSaturated pins down
// the guarantee documented on the ts.evict() call inside AddSpan's
// cap-exceeded branch: retention must not become conditional on a successful
// insert. Without that call, a store whose only traffic is one saturated
// trace ("hot") would never call evict() again — every further AddSpan on
// "hot" takes the early-return cap-rejection branch — so an unrelated expired
// trace ("old") would sit in memory forever instead of aging out.
//
// "old" is seeded directly into the store's internals (this test lives in
// package collector, so it can) rather than through AddSpan, for two reasons.
// First, the span() helper always stamps StartTime with time.Now(), so an
// already-expired span has to be built by hand. Second, and more subtly, if
// "old" were seeded before "hot" existed, the unconditional ts.evict() call
// at the end of AddSpan's normal (non-cap) path — the one that runs on every
// successful insert, not only the cap branch — would reclaim it the moment
// "hot"'s first (still normal-path) span landed, long before "hot" ever
// saturates. So "hot" is saturated first, while "old" does not exist yet,
// and "old" is only seeded afterward, bypassing AddSpan entirely so its
// insertion triggers no evict() call of its own. It is placed ahead of
// "hot" in eviction order, because evict() walks oldest-first and stops at
// the first non-expired trace — "hot" is fresh, so if it came first, evict()
// would never even look at "old".
func TestTraceStore_EvictsExpiredTraceEvenWhileAnotherTraceIsSaturated(t *testing.T) {
	const (
		retention = 200 * time.Millisecond
		spanCap   = 3
	)

	ts := NewTraceStore(10, retention, WithMaxSpansPerTrace(spanCap))

	// Saturate "hot" first, before "old" exists, so nothing here can
	// accidentally evict "old".
	for range spanCap {
		ts.AddSpan(span("hot", "span"))
	}

	ts.mu.RLock()
	hotCount := len(ts.traces["hot"])
	ts.mu.RUnlock()

	if hotCount != spanCap {
		t.Fatalf("trace \"hot\" has %d spans after saturating, want %d", hotCount, spanCap)
	}

	expiredStart := time.Now().Add(-time.Hour)
	oldSpan := &SpanView{
		SpanID:     "span",
		TraceID:    "old",
		Name:       "GET /x",
		Kind:       SpanKindServer,
		Status:     SpanStatusOK,
		StartTime:  expiredStart,
		EndTime:    expiredStart.Add(time.Millisecond),
		Duration:   time.Millisecond,
		Attributes: map[string]string{},
		Events:     []SpanEventView{},
	}

	ts.mu.Lock()
	ts.traces["old"] = []*SpanView{oldSpan}
	ts.order = append([]string{"old"}, ts.order...)
	ts.mu.Unlock()

	// From here, every AddSpan on "hot" takes the cap-rejection branch
	// exclusively (its span count is already at the cap and no insert can
	// ever succeed again). That branch's evict() call is the only thing
	// that can still reclaim "old".
	for range 20 {
		ts.AddSpan(span("hot", "span"))

		ts.mu.RLock()
		_, stillPresent := ts.traces["old"]
		ts.mu.RUnlock()

		if !stillPresent {
			return // "old" was reclaimed: the guarantee holds.
		}
	}

	t.Error("trace \"old\" was never evicted while \"hot\" stayed saturated at its cap")
}

// MarkAccessed runs on the request path alongside AddSpan. It must not race.
func TestTraceStore_MarkAccessedIsRaceFree(t *testing.T) {
	ts := NewTraceStore(100, time.Hour)
	ts.SetIngestGate(func() bool { return !ts.LastAccessed().IsZero() })

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 200 {
				ts.MarkAccessed()
				ts.AddSpan(span("trace-a", "span"))
			}
		})
	}

	wg.Wait()
}

func TestTraceStore_GetTraceConcurrentReadersDoNotRace(t *testing.T) {
	ts := NewTraceStore(10, time.Hour)
	now := time.Now()

	ts.AddSpan(&SpanView{TraceID: "t1", SpanID: "root", Name: "root", StartTime: now, EndTime: now.Add(time.Millisecond), Duration: time.Millisecond})
	ts.AddSpan(&SpanView{TraceID: "t1", SpanID: "child", ParentSpanID: "root", Name: "child", StartTime: now, EndTime: now.Add(time.Microsecond), Duration: time.Microsecond})

	var wg sync.WaitGroup

	for range 4 {
		wg.Go(func() {
			for range 200 {
				if d := ts.GetTrace("t1"); d == nil || d.SpanCount != 2 {
					t.Error("trace missing")

					return
				}
			}
		})
	}

	wg.Wait()
}

func TestTraceStore_GetTraceLeavesStoredSpansUntouched(t *testing.T) {
	ts := NewTraceStore(10, time.Hour)
	now := time.Now()
	child := &SpanView{TraceID: "t1", SpanID: "child", ParentSpanID: "root", StartTime: now.Add(time.Millisecond), EndTime: now.Add(2 * time.Millisecond), Duration: time.Millisecond}

	ts.AddSpan(&SpanView{TraceID: "t1", SpanID: "root", StartTime: now, EndTime: now.Add(4 * time.Millisecond), Duration: 4 * time.Millisecond})
	ts.AddSpan(child)

	d := ts.GetTrace("t1")

	var got *SpanView

	for _, s := range d.Spans {
		if s.SpanID == "child" {
			got = s
		}
	}

	if got == nil || got.Depth != 1 || got.OffsetPercent == 0 {
		t.Fatalf("detail child = %+v, want depth 1 and a non-zero offset", got)
	}

	if child.Depth != 0 || child.OffsetPercent != 0 {
		t.Fatalf("stored span was written by GetTrace: %+v", child)
	}
}

func TestTraceStore_AttachExchangeJoinsOnAddSpan(t *testing.T) {
	ts := NewTraceStore(10, time.Hour)
	ex := &HTTPExchange{Request: HTTPMessage{Headers: map[string]string{"X-Request-Id": "r1"}}}

	ts.AttachExchange("s1", ex)
	ts.AddSpan(&SpanView{TraceID: "t1", SpanID: "s1", StartTime: time.Now(), EndTime: time.Now()})

	d := ts.GetTrace("t1")
	if d == nil || d.Spans[0].HTTP == nil || d.Spans[0].HTTP.Request.Headers["X-Request-Id"] != "r1" {
		t.Fatalf("exchange not joined: %+v", d)
	}

	// Consumed: a second span with the same ID gets nothing.
	ts.AddSpan(&SpanView{TraceID: "t2", SpanID: "s1", StartTime: time.Now(), EndTime: time.Now()})

	if ts.GetTrace("t2").Spans[0].HTTP != nil {
		t.Fatal("exchange was joined twice")
	}
}

func TestTraceStore_AttachExchangeIsBounded(t *testing.T) {
	ts := NewTraceStore(10, time.Hour)

	for i := range maxPendingExchanges + 50 {
		ts.AttachExchange(fmt.Sprintf("s%d", i), &HTTPExchange{})
	}

	if n := ts.pendingExchangeCount(); n > maxPendingExchanges {
		t.Fatalf("pending exchanges = %d, cap %d", n, maxPendingExchanges)
	}
}

func TestTraceStore_ClosedGateStillConsumesExchange(t *testing.T) {
	ts := NewTraceStore(10, time.Hour)
	ts.SetIngestGate(func() bool { return false })
	ts.AttachExchange("s1", &HTTPExchange{})
	ts.AddSpan(&SpanView{TraceID: "t1", SpanID: "s1"})

	if n := ts.pendingExchangeCount(); n != 0 {
		t.Fatalf("closed gate stranded %d exchanges", n)
	}
}

func TestTraceStore_Accepting(t *testing.T) {
	ts := NewTraceStore(10, time.Hour)
	if !ts.Accepting() {
		t.Fatal("no gate should accept")
	}

	ts.SetIngestGate(func() bool { return false })

	if ts.Accepting() {
		t.Fatal("closed gate accepted")
	}
}
