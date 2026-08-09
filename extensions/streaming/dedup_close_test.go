package streaming

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"
)

// These tests pin the dedup half of C6: newMessageDedup starts a cleanup
// goroutine that nothing could ever stop, so every manager start/stop cycle
// stranded one for the life of the process.

func TestMessageDedup_CloseStopsCleanupLoop(t *testing.T) {
	before := runtime.NumGoroutine()

	d := newMessageDedup(1024, 10*time.Millisecond)
	d.IsDuplicate("seen")

	if err := d.Close(); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}

	if n := waitForGoroutineCount(before); n > before {
		t.Errorf("goroutines after Close = %d, want <= %d (cleanup loop leaked)", n, before)
	}
}

func TestMessageDedup_CloseIsIdempotent(t *testing.T) {
	d := newMessageDedup(1024, time.Minute)

	for i := 0; i < 3; i++ {
		if err := d.Close(); err != nil {
			t.Fatalf("Close() call %d = %v, want nil", i, err)
		}
	}
}

func TestMessageDedup_ConcurrentCloseIsSafe(t *testing.T) {
	d := newMessageDedup(1024, time.Minute)

	var wg sync.WaitGroup

	for i := 0; i < 8; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			if err := d.Close(); err != nil {
				t.Errorf("Close() = %v, want nil", err)
			}
		}()
	}

	wg.Wait()
}

func TestMessageDedup_StartStopCyclesDoNotLeak(t *testing.T) {
	before := runtime.NumGoroutine()

	for i := 0; i < 20; i++ {
		d := newMessageDedup(1024, 10*time.Millisecond)
		d.IsDuplicate(fmt.Sprintf("msg-%d", i))

		if err := d.Close(); err != nil {
			t.Fatalf("cycle %d: Close() = %v, want nil", i, err)
		}
	}

	if n := waitForGoroutineCount(before); n > before {
		t.Errorf("goroutines after 20 start/stop cycles = %d, want <= %d", n, before)
	}
}

// TestMessageDedup_IsDuplicateAfterCloseStillWorks checks Close only stops the
// background sweep — lookups remain correct so an in-flight delivery racing
// shutdown does not panic or start admitting duplicates.
func TestMessageDedup_IsDuplicateAfterCloseStillWorks(t *testing.T) {
	d := newMessageDedup(1024, time.Minute)

	if err := d.Close(); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}

	if got := d.IsDuplicate("a"); got {
		t.Errorf("first IsDuplicate(%q) after Close = true, want false", "a")
	}

	if got := d.IsDuplicate("a"); !got {
		t.Errorf("second IsDuplicate(%q) after Close = false, want true", "a")
	}
}
