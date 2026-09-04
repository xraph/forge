package collector

import (
	"fmt"
	"runtime"
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
		ts.AddSpan(span(fmt.Sprintf("trace-%d", i%1000), "span"))
	}

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	// Without this, ts's last real use is inside the loop above, so the
	// compiler's liveness analysis can treat it as dead before the GC call
	// just above measures "after" — the store (and everything it retains)
	// gets collected early and the test passes for the wrong reason,
	// regardless of whether the gate actually did anything. Keep it alive
	// through the measurement.
	runtime.KeepAlive(ts)

	// Signed: this test retains nothing, so finishing below the baseline is the
	// expected outcome and must not wrap into a huge positive number.
	const ceiling = 1 << 20 // 1 MiB
	if grew := int64(after.HeapAlloc) - int64(before.HeapAlloc); grew > ceiling {
		t.Errorf("200k gated spans retained %d bytes, want at most %d", grew, ceiling)
	}
}
