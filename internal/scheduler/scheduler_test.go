package scheduler

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestScheduler_RunsJobsOnTheirIntervals(t *testing.T) {
	s := New("test")

	var fast, slow atomic.Int64

	s.Every("fast", 10*time.Millisecond, func(context.Context) { fast.Add(1) })
	s.Every("slow", 100*time.Millisecond, func(context.Context) { slow.Add(1) })

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	time.Sleep(120 * time.Millisecond)

	if err := s.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Timing is loose on a loaded machine; the point is the relative rates.
	if got := fast.Load(); got < 5 {
		t.Errorf("fast job ran %d times in 120ms at a 10ms interval, want at least 5", got)
	}

	if got := slow.Load(); got > 3 {
		t.Errorf("slow job ran %d times in 120ms at a 100ms interval, want at most 3", got)
	}
}

// The whole point: many jobs, one goroutine and one timer between them.
func TestScheduler_UsesOneGoroutineForManyJobs(t *testing.T) {
	runtime.GC()
	time.Sleep(20 * time.Millisecond)

	before := runtime.NumGoroutine()

	s := New("test")
	for range 50 {
		s.Every("noop", time.Hour, func(context.Context) {})
	}

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	after := runtime.NumGoroutine()

	if err := s.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if grew := after - before; grew > 2 {
		t.Errorf("50 jobs added %d goroutines, want at most 2", grew)
	}
}

// A job that outlives its interval must not stack up behind itself.
func TestScheduler_SkipsOverlappingRuns(t *testing.T) {
	s := New("test")

	var (
		mu       sync.Mutex
		inFlight int
		peak     int
		runs     int
	)

	s.Every("slow", 5*time.Millisecond, func(context.Context) {
		mu.Lock()
		inFlight++
		runs++

		if inFlight > peak {
			peak = inFlight
		}

		mu.Unlock()

		time.Sleep(40 * time.Millisecond)

		mu.Lock()
		inFlight--
		mu.Unlock()
	})

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	time.Sleep(150 * time.Millisecond)

	if err := s.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if peak > 1 {
		t.Errorf("job overlapped itself: peak %d concurrent runs", peak)
	}

	if runs == 0 {
		t.Error("job never ran")
	}
}

// A cancelled job stops running; the others carry on.
func TestScheduler_CancelStopsOneJob(t *testing.T) {
	s := New("test")

	var cancelled, kept atomic.Int64

	stop := s.Every("cancelled", 10*time.Millisecond, func(context.Context) { cancelled.Add(1) })
	s.Every("kept", 10*time.Millisecond, func(context.Context) { kept.Add(1) })

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	time.Sleep(60 * time.Millisecond)
	stop()

	atCancel := cancelled.Load()

	time.Sleep(80 * time.Millisecond)

	if err := s.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if after := cancelled.Load(); after > atCancel {
		t.Errorf("cancelled job ran %d more times after cancel", after-atCancel)
	}

	if kept.Load() <= atCancel {
		t.Error("the other job stopped running too")
	}
}

// A panicking job must not take the process, or the scheduler, down.
func TestScheduler_SurvivesAPanickingJob(t *testing.T) {
	s := New("test")

	var healthy atomic.Int64

	s.Every("exploding", 10*time.Millisecond, func(context.Context) { panic("boom") })
	s.Every("healthy", 10*time.Millisecond, func(context.Context) { healthy.Add(1) })

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	time.Sleep(80 * time.Millisecond)

	if err := s.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if healthy.Load() < 2 {
		t.Errorf("healthy job ran %d times alongside a panicking one", healthy.Load())
	}
}

// Stop cancels the context handed to jobs, so a long job can bail out.
func TestScheduler_StopCancelsJobContext(t *testing.T) {
	s := New("test")

	observed := make(chan struct{})

	var once sync.Once

	s.Every("watcher", 10*time.Millisecond, func(ctx context.Context) {
		<-ctx.Done()
		once.Do(func() { close(observed) })
	})

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	time.Sleep(30 * time.Millisecond)

	if err := s.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	select {
	case <-observed:
	case <-time.After(time.Second):
		t.Error("job context was not cancelled on Stop")
	}
}

// A job registered after Start still runs, and one registered ahead of the
// current sleep does not wait for it.
func TestScheduler_AcceptsJobsAfterStart(t *testing.T) {
	s := New("test")

	s.Every("distant", time.Hour, func(context.Context) {})

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	var ran atomic.Int64

	s.Every("soon", 10*time.Millisecond, func(context.Context) { ran.Add(1) })

	time.Sleep(60 * time.Millisecond)

	if err := s.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if ran.Load() == 0 {
		t.Error("a job registered after Start never ran")
	}
}

func TestScheduler_IgnoresNonPositiveInterval(t *testing.T) {
	s := New("test")

	cancel := s.Every("bad", 0, func(context.Context) {})
	cancel() // must not panic

	if s.Len() != 0 {
		t.Errorf("registered %d jobs for a zero interval, want 0", s.Len())
	}
}

func TestScheduler_StartStopAreIdempotent(t *testing.T) {
	s := New("test")

	ctx := context.Background()

	if err := s.Start(ctx); err != nil {
		t.Fatalf("first Start: %v", err)
	}

	if err := s.Start(ctx); err != nil {
		t.Fatalf("second Start: %v", err)
	}

	if err := s.Stop(ctx); err != nil {
		t.Fatalf("first Stop: %v", err)
	}

	if err := s.Stop(ctx); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
}
