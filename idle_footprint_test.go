package forge

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/xraph/forge/internal/logger"
)

// A running app should not park a goroutine per piece of periodic work.
//
// Health checks, health reports, metric collection, time-series cleanup and
// compression are all periodic, and each used to hold a goroutine blocked on
// its own time.Ticker. That is a cost per subsystem, and it accumulates with
// every extension installed, so an app was never fully idle. They now share one
// scheduler goroutine and one timer between them; see internal/scheduler.
//
// The number below is a ceiling, not a target. If it has to go up, check that
// the new goroutine is genuinely event-driven rather than another ticker.
func TestApp_IdleGoroutineFootprint(t *testing.T) {
	const ceiling = 4

	runtime.GC()
	time.Sleep(50 * time.Millisecond)

	before := runtime.NumGoroutine()

	app := NewApp(AppConfig{
		Name:          "idle-test",
		Version:       "1.0.0",
		Environment:   "test",
		Logger:        logger.NewTestLogger(),
		MetricsConfig: DefaultMetricsConfig(),
		HealthConfig:  DefaultHealthConfig(),
	})

	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	defer func() { _ = app.Stop(context.Background()) }()

	// Let every background routine reach its parked state.
	time.Sleep(200 * time.Millisecond)

	grew := runtime.NumGoroutine() - before
	if grew > ceiling {
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)

		t.Errorf("a started app added %d goroutines, want at most %d\n%s",
			grew, ceiling, buf[:n])
	}
}

// Every periodic job in the framework core should be on the shared scheduler.
// A stray time.Ticker shows up here as a goroutine parked in time.NewTicker's
// receive, so name the ones that are allowed.
func TestApp_NoStrayTickerGoroutines(t *testing.T) {
	app := NewApp(AppConfig{
		Name:          "ticker-test",
		Version:       "1.0.0",
		Environment:   "test",
		Logger:        logger.NewTestLogger(),
		MetricsConfig: DefaultMetricsConfig(),
		HealthConfig:  DefaultHealthConfig(),
	})

	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	defer func() { _ = app.Stop(context.Background()) }()

	time.Sleep(200 * time.Millisecond)

	buf := make([]byte, 1<<20)
	stacks := string(buf[:runtime.Stack(buf, true)])

	for _, banned := range []string{
		"internal/health.(*ManagerImpl).checkLoop",
		"internal/health.(*ManagerImpl).reportLoop",
		"internal/metrics.(*collector).collectionLoop",
		"internal/metrics/storage.(*TimeSeriesStorage).cleanupLoop",
		"internal/metrics/storage.(*TimeSeriesStorage).compressionLoop",
		"internal/metrics.(*registry).cleanupLoop",
	} {
		if strings.Contains(stacks, banned) {
			t.Errorf("%s is running on its own goroutine; it belongs on the shared scheduler", banned)
		}
	}
}
