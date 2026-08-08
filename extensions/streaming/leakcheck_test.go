package streaming

import (
	"runtime"
	"time"
)

// waitForGoroutineCount polls the live goroutine count until it drops to target
// or the deadline expires, then returns the last observed count.
//
// A bare runtime.NumGoroutine() read immediately after Close is racy: closing a
// stop channel only schedules the loop to return, it does not wait for it. Tests
// that assert "no goroutine leaked" must give the runtime a chance to reap.
func waitForGoroutineCount(target int) int {
	deadline := time.Now().Add(2 * time.Second)

	n := runtime.NumGoroutine()
	for n > target && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
		n = runtime.NumGoroutine()
	}

	return n
}

// peakGoroutines runs fn while sampling the goroutine count, returning the
// highest count observed. Used to prove a worker pool stays bounded instead of
// spawning one goroutine per unit of work.
func peakGoroutines(fn func(sample func())) int {
	peak := runtime.NumGoroutine()

	fn(func() {
		if n := runtime.NumGoroutine(); n > peak {
			peak = n
		}
	})

	return peak
}
