//go:build !windows

package forge

// These tests deliver SIGTERM and SIGINT to the test process with
// syscall.Kill, which Windows does not have. The rest of the shutdown budget
// tests live in lifecycle_shutdown_budget_test.go and run everywhere.

import (
	"syscall"
	"testing"
	"time"
)

// An extension whose Stop never returns used to block Run forever. Shutdown
// is now bounded by ShutdownTimeout plus StopTimeout, and the extensions after
// the stuck one are still stopped.
func TestShutdown_StuckExtensionStopIsAbandoned(t *testing.T) {
	stuck := &stuckExtension{BaseExtension: NewBaseExtension("queue", "1.0.0", "q")}
	other := &stopRecordingExtension{BaseExtension: NewBaseExtension("cache", "1.0.0", "c")}
	a := newRunApp(t, other, stuck)

	returned := runInBackground(t, a)

	start := time.Now()

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}

	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("Run still blocked 5s after SIGTERM with ShutdownTimeout=300ms and StopTimeout=700ms")
	}

	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("shutdown took %v, want about ShutdownTimeout+StopTimeout", elapsed)
	}

	if !stuck.stopCalled.Load() {
		t.Error("the stuck extension's Stop was never called")
	}

	if !other.stopped.Load() {
		t.Error("an extension stopped after the stuck one was skipped")
	}
}

// A second signal during a slow shutdown exits instead of being swallowed.

func TestShutdown_SecondSignalExits(t *testing.T) {
	exited := make(chan int, 1)

	orig := forceExit
	forceExit = func(code int) { exited <- code }

	t.Cleanup(func() { forceExit = orig })

	stuck := &stuckExtension{BaseExtension: NewBaseExtension("queue", "1.0.0", "q")}
	a := newRunApp(t, stuck)
	a.config.StopTimeout = 5 * time.Second

	returned := runInBackground(t, a)

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}

	// Wait until shutdown is inside the stuck Stop.
	deadline := time.Now().Add(5 * time.Second)
	for !stuck.stopCalled.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}

	select {
	case code := <-exited:
		if code == 0 {
			t.Errorf("forced exit code = 0, want non-zero")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the second signal was swallowed")
	}

	// Run still finishes on its own once the stop budget runs out.
	select {
	case <-returned:
	case <-time.After(10 * time.Second):
		t.Fatal("Run never returned")
	}
}

// A BeforeRun hook failure used to return from Run with every extension still
// running.
