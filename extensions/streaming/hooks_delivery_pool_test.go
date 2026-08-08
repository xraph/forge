package streaming

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"
)

// deliveryRecorder is a MessageHook that records every delivery it observes.
type deliveryRecorder struct {
	hookName string

	gate chan struct{} // if non-nil, OnMessageDelivered blocks until it is closed

	mu       sync.Mutex
	count    int
	ctxErrs  []error
	messages []string

	delivered chan struct{}
}

func newDeliveryRecorder(name string, capacity int) *deliveryRecorder {
	return &deliveryRecorder{
		hookName:  name,
		delivered: make(chan struct{}, capacity),
	}
}

func (h *deliveryRecorder) Name() string { return h.hookName }

func (h *deliveryRecorder) OnMessageReceived(ctx context.Context, conn Connection, msg *Message) (*Message, error) {
	return msg, nil
}

func (h *deliveryRecorder) OnMessageDelivered(ctx context.Context, conn Connection, msg *Message) {
	if h.gate != nil {
		<-h.gate
	}

	h.mu.Lock()
	h.count++
	h.ctxErrs = append(h.ctxErrs, ctx.Err())

	if msg != nil {
		h.messages = append(h.messages, msg.ID)
	}
	h.mu.Unlock()

	select {
	case h.delivered <- struct{}{}:
	default:
	}
}

func (h *deliveryRecorder) snapshot() (int, []error, []string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	errs := append([]error(nil), h.ctxErrs...)
	msgs := append([]string(nil), h.messages...)

	return h.count, errs, msgs
}

// waitForDeliveries blocks until n deliveries land or the deadline expires.
func (h *deliveryRecorder) waitForDeliveries(t *testing.T, n int) {
	t.Helper()

	timeout := time.After(3 * time.Second)

	for i := 0; i < n; i++ {
		select {
		case <-h.delivered:
		case <-timeout:
			got, _, _ := h.snapshot()
			t.Fatalf("timed out waiting for %d deliveries, got %d", n, got)
		}
	}
}

func TestHookRegistry_FireOnMessageDeliveredReachesEveryHook(t *testing.T) {
	tests := []struct {
		name     string
		hooks    int
		messages int
	}{
		{name: "single hook single message", hooks: 1, messages: 1},
		{name: "single hook many messages", hooks: 1, messages: 25},
		{name: "several hooks single message", hooks: 3, messages: 1},
		{name: "several hooks many messages", hooks: 3, messages: 25},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewHookRegistry()
			defer r.Close()

			recorders := make([]*deliveryRecorder, tt.hooks)
			for i := range recorders {
				recorders[i] = newDeliveryRecorder(string(rune('a'+i)), tt.messages)
				r.Add(recorders[i])
			}

			for i := 0; i < tt.messages; i++ {
				r.FireOnMessageDelivered(context.Background(), nil, &Message{ID: "m"})
			}

			for i, rec := range recorders {
				rec.waitForDeliveries(t, tt.messages)

				if got, _, _ := rec.snapshot(); got != tt.messages {
					t.Errorf("hook %d saw %d deliveries, want %d", i, got, tt.messages)
				}
			}
		})
	}
}

// TestHookRegistry_FireOnMessageDeliveredWithNoHooksSpawnsNothing pins the common
// case from P3: with no message hooks registered there is nothing to copy and
// nothing to schedule.
func TestHookRegistry_FireOnMessageDeliveredWithNoHooksSpawnsNothing(t *testing.T) {
	r := NewHookRegistry()
	defer r.Close()

	before := runtime.NumGoroutine()

	for i := 0; i < 5000; i++ {
		r.FireOnMessageDelivered(context.Background(), nil, &Message{ID: "m"})
	}

	if n := waitForGoroutineCount(before); n > before {
		t.Errorf("goroutines after 5000 hookless deliveries = %d, want <= %d", n, before)
	}
}

// TestHookRegistry_FireOnMessageDeliveredIsBounded is the core P3 regression: the
// old implementation spawned one goroutine per delivery, so a large room
// broadcast produced thousands of concurrent goroutines for a single message.
func TestHookRegistry_FireOnMessageDeliveredIsBounded(t *testing.T) {
	r := NewHookRegistry()
	defer r.Close()

	rec := newDeliveryRecorder("recorder", 1)
	rec.gate = make(chan struct{})
	r.Add(rec)

	const recipients = 10000

	before := runtime.NumGoroutine()

	peak := peakGoroutines(func(sample func()) {
		for i := 0; i < recipients; i++ {
			r.FireOnMessageDelivered(context.Background(), nil, &Message{ID: "m"})

			if i%100 == 0 {
				sample()
			}
		}

		sample()
	})

	close(rec.gate)

	// A bounded pool tops out at its worker count; one-goroutine-per-delivery
	// would be several thousand over the baseline.
	limit := before + runtime.NumCPU() + 8
	if peak > limit {
		t.Errorf("peak goroutines during %d deliveries = %d, want <= %d (baseline %d)", recipients, peak, limit, before)
	}
}

// TestHookRegistry_DroppedDeliveriesIsZeroUnderLightLoad checks the counter does
// not cry wolf: an unsaturated pool runs every batch.
func TestHookRegistry_DroppedDeliveriesIsZeroUnderLightLoad(t *testing.T) {
	r := NewHookRegistry()
	defer r.Close()

	rec := newDeliveryRecorder("recorder", 32)
	r.Add(rec)

	for i := 0; i < 32; i++ {
		r.FireOnMessageDelivered(context.Background(), nil, &Message{ID: "m"})
	}

	rec.waitForDeliveries(t, 32)

	if got := r.DroppedDeliveries(); got != 0 {
		t.Errorf("DroppedDeliveries() = %d, want 0", got)
	}
}

// TestHookRegistry_DroppedDeliveriesCountsSaturationDrops makes the silent drop
// observable. Dropping is the right call — blocking here would stall delivery —
// but a hook that feeds billing or audit needs to know it happened.
func TestHookRegistry_DroppedDeliveriesCountsSaturationDrops(t *testing.T) {
	r := NewHookRegistry()

	rec := newDeliveryRecorder("recorder", 1)
	rec.gate = make(chan struct{}) // every worker parks on the first job
	r.Add(rec)

	// Enough to fill the queue behind the parked workers and then overflow it.
	fired := deliveryQueueSize + maxDeliveryWorkers + 500
	for i := 0; i < fired; i++ {
		r.FireOnMessageDelivered(context.Background(), nil, &Message{ID: "m"})
	}

	dropped := r.DroppedDeliveries()
	if dropped == 0 {
		t.Errorf("DroppedDeliveries() = 0 after firing %d into a %d-slot queue, want > 0", fired, deliveryQueueSize)
	}

	if dropped > uint64(fired) {
		t.Errorf("DroppedDeliveries() = %d, want <= %d (cannot drop more than were fired)", dropped, fired)
	}

	close(rec.gate)

	if err := r.Close(); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}
}

func TestHookRegistry_DroppedDeliveriesCountsPostCloseFires(t *testing.T) {
	r := NewHookRegistry()
	r.Add(newDeliveryRecorder("recorder", 1))

	if err := r.Close(); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}

	for i := 0; i < 10; i++ {
		r.FireOnMessageDelivered(context.Background(), nil, &Message{ID: "m"})
	}

	if got := r.DroppedDeliveries(); got != 10 {
		t.Errorf("DroppedDeliveries() = %d, want 10 (fires after Close are dropped)", got)
	}
}

func TestHookRegistry_DroppedDeliveriesIgnoresHooklessFires(t *testing.T) {
	// With no message hooks there is nothing to run, so nothing is "dropped".
	r := NewHookRegistry()
	defer r.Close()

	for i := 0; i < 100; i++ {
		r.FireOnMessageDelivered(context.Background(), nil, &Message{ID: "m"})
	}

	if got := r.DroppedDeliveries(); got != 0 {
		t.Errorf("DroppedDeliveries() = %d, want 0", got)
	}
}

// TestHookRegistry_FireOnMessageDeliveredSurvivesCancelledContext pins the
// context.WithoutCancel requirement: the request context is routinely cancelled
// before the detached hook work runs.
func TestHookRegistry_FireOnMessageDeliveredSurvivesCancelledContext(t *testing.T) {
	r := NewHookRegistry()
	defer r.Close()

	rec := newDeliveryRecorder("recorder", 1)
	rec.gate = make(chan struct{})
	r.Add(rec)

	ctx, cancel := context.WithCancel(context.Background())
	ctx = context.WithValue(ctx, testCtxKey{}, "carried")

	r.FireOnMessageDelivered(ctx, nil, &Message{ID: "m"})

	// The request finishes before the hook gets to run.
	cancel()
	close(rec.gate)

	rec.waitForDeliveries(t, 1)

	_, errs, _ := rec.snapshot()
	if len(errs) != 1 {
		t.Fatalf("recorded %d contexts, want 1", len(errs))
	}

	if errs[0] != nil {
		t.Errorf("hook saw ctx.Err() = %v, want nil (delivery hooks must outlive the request context)", errs[0])
	}
}

type testCtxKey struct{}

func TestHookRegistry_FireOnMessageDeliveredPreservesContextValues(t *testing.T) {
	r := NewHookRegistry()
	defer r.Close()

	var (
		mu     sync.Mutex
		seen   any
		gotOne = make(chan struct{}, 1)
	)

	r.Add(&valueRecorder{
		fn: func(ctx context.Context) {
			mu.Lock()
			seen = ctx.Value(testCtxKey{})
			mu.Unlock()

			select {
			case gotOne <- struct{}{}:
			default:
			}
		},
	})

	ctx := context.WithValue(context.Background(), testCtxKey{}, "carried")
	r.FireOnMessageDelivered(ctx, nil, &Message{ID: "m"})

	select {
	case <-gotOne:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for delivery")
	}

	mu.Lock()
	defer mu.Unlock()

	if seen != "carried" {
		t.Errorf("hook saw ctx value %v, want %q (WithoutCancel must keep values)", seen, "carried")
	}
}

type valueRecorder struct {
	fn func(context.Context)
}

func (h *valueRecorder) Name() string { return "value-recorder" }

func (h *valueRecorder) OnMessageReceived(ctx context.Context, conn Connection, msg *Message) (*Message, error) {
	return msg, nil
}

func (h *valueRecorder) OnMessageDelivered(ctx context.Context, conn Connection, msg *Message) {
	h.fn(ctx)
}

func TestHookRegistry_CloseStopsWorkers(t *testing.T) {
	before := runtime.NumGoroutine()

	r := NewHookRegistry()

	rec := newDeliveryRecorder("recorder", 4)
	r.Add(rec)

	for i := 0; i < 4; i++ {
		r.FireOnMessageDelivered(context.Background(), nil, &Message{ID: "m"})
	}

	rec.waitForDeliveries(t, 4)

	if err := r.Close(); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}

	if n := waitForGoroutineCount(before); n > before {
		t.Errorf("goroutines after Close = %d, want <= %d (worker pool leaked)", n, before)
	}
}

func TestHookRegistry_CloseIsIdempotent(t *testing.T) {
	r := NewHookRegistry()
	r.Add(newDeliveryRecorder("recorder", 1))
	r.FireOnMessageDelivered(context.Background(), nil, &Message{ID: "m"})

	for i := 0; i < 3; i++ {
		if err := r.Close(); err != nil {
			t.Fatalf("Close() call %d = %v, want nil", i, err)
		}
	}
}

func TestHookRegistry_FireOnMessageDeliveredAfterCloseDoesNotPanic(t *testing.T) {
	r := NewHookRegistry()
	r.Add(newDeliveryRecorder("recorder", 1))

	if err := r.Close(); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}

	// Deliveries racing a shutdown must be dropped, not panic on a closed channel.
	for i := 0; i < 100; i++ {
		r.FireOnMessageDelivered(context.Background(), nil, &Message{ID: "m"})
	}
}

func TestHookRegistry_CloseWithNeverFiredPoolIsCheap(t *testing.T) {
	before := runtime.NumGoroutine()

	for i := 0; i < 50; i++ {
		r := NewHookRegistry()

		if err := r.Close(); err != nil {
			t.Fatalf("cycle %d: Close() = %v, want nil", i, err)
		}
	}

	if n := waitForGoroutineCount(before); n > before {
		t.Errorf("goroutines after 50 registry lifecycles = %d, want <= %d", n, before)
	}
}
