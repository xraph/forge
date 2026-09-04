package collector

import (
	"runtime"
	"sync"
	"testing"
	"time"
)

// Retained heap must not scale with the number of spans pushed through the
// store. The per-trace cap and the trace cap together bound it.
//
// The ceiling below is generous on purpose. It is here to catch an unbounded
// regression, not to measure allocation precisely, so treat a failure as
// "something is now unbounded" rather than "this got 5% worse".
func TestTraceStore_RetainedHeapIsBounded(t *testing.T) {
	ts := NewTraceStore(50, time.Hour, WithMaxSpansPerTrace(20))

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	for i := 0; i < 200000; i++ {
		ts.AddSpan(span("trace-a", "span"))
	}

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	// Signed, because HeapAlloc can finish BELOW the baseline once GC runs and
	// an unsigned subtraction would wrap to ~2^64 and fail absurdly.
	const ceiling = 8 << 20 // 8 MiB
	if grew := int64(after.HeapAlloc) - int64(before.HeapAlloc); grew > ceiling {
		t.Errorf("200k spans retained %d bytes, want at most %d", grew, ceiling)
	}

	if ts.DroppedSpans() == 0 {
		t.Error("no spans dropped after 200k pushes into a 20-span cap")
	}
}

// The closed gate is the path most services take. It must cost nothing.
func TestTraceStore_ClosedGateRetainsNothing(t *testing.T) {
	ts := NewTraceStore(1000, time.Hour)
	ts.SetIngestGate(func() bool { return false })

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	for i := 0; i < 200000; i++ {
		ts.AddSpan(span("trace-a", "span"))
	}

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	// Signed: this test retains nothing, so finishing below the baseline is the
	// expected outcome and must not wrap into a huge positive number.
	const ceiling = 1 << 20 // 1 MiB
	if grew := int64(after.HeapAlloc) - int64(before.HeapAlloc); grew > ceiling {
		t.Errorf("200k gated spans retained %d bytes, want at most %d", grew, ceiling)
	}
}

// Mirrors idle_footprint_test.go at the repo root. Goroutines must not scale
// with request volume. See the comment there for why this ceiling exists.
//
// This samples the peak goroutine count across the loop, rather than once
// after it, and uses a slow callback so that spawned goroutines would pile up
// visibly instead of retiring before the next sample. A single post-loop
// reading with an instant callback caught the one-goroutine-per-span
// regression only 4 times in 5 runs; this shape caught it 5/5.
func TestTraceStore_GoroutinesDoNotScaleWithSpans(t *testing.T) {
	ts := NewTraceStore(1000, time.Hour, WithMaxSpansPerTrace(100000))

	var mu sync.Mutex
	seen := 0
	ts.SetOnTraceAdded(func(string, int) {
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

	for i := 0; i < 500; i++ {
		ts.AddSpan(span("trace-a", "span"))
		if i%100 == 0 {
			if n := runtime.NumGoroutine(); n > peak {
				peak = n
			}
		}
	}

	if grew := peak - before; grew > 1 {
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		t.Errorf("500 spans added %d goroutines at peak, want at most 1\n%s", grew, buf[:n])
	}
}
