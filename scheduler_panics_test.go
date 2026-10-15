package forge

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/scheduler"
)

func TestInstallSchedulerPanicLog(t *testing.T) {
	log, ok := logger.NewTestLogger().(*logger.TestLogger)
	if !ok {
		t.Fatal("logger.NewTestLogger did not return a *logger.TestLogger")
	}

	installSchedulerPanicLog(log)
	t.Cleanup(func() { scheduler.SetPanicHandler(nil) })

	s := scheduler.New("app-panic")
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = s.Stop(context.WithoutCancel(t.Context())) })

	// The job panics on every run, so close done once.
	var once sync.Once

	done := make(chan struct{})
	cancel := s.Every("app-job", 5*time.Millisecond, func(context.Context) {
		once.Do(func() { close(done) })

		panic("app boom")
	})
	t.Cleanup(cancel)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("job never ran")
	}

	// The handler runs after the job returns, so poll for the log line.
	waitFor(t, func() bool {
		for _, e := range log.GetLogsByLevel("ERROR") {
			if !strings.Contains(e.Message, "scheduler: job panicked") {
				continue
			}

			job, _ := e.Field("job")

			return job == "app-job"
		}

		return false
	})
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatal("condition not met within 2s")
}
