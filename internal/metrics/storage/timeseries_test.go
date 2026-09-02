package storage

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func newTestStorage(maxSeries, maxPoints int) *TimeSeriesStorage {
	return NewTimeSeriesStorageWithConfig(&TimeSeriesStorageConfig{
		Retention:          time.Hour,
		Resolution:         30 * time.Second,
		MaxSeries:          maxSeries,
		MaxPointsPerSeries: maxPoints,
	})
}

func stamps(t *testing.T, ts *TimeSeriesStorage, name string) []time.Time {
	t.Helper()

	ts.mu.RLock()
	defer ts.mu.RUnlock()

	series, ok := ts.series[ts.generateSeriesKey(name, nil)]
	if !ok {
		t.Fatalf("series %q not found", name)
	}

	series.mu.RLock()
	defer series.mu.RUnlock()

	out := make([]time.Time, len(series.Points))
	for i, p := range series.Points {
		out[i] = p.Timestamp
	}

	return out
}

// Store appends in the common case but must still order a point that arrives
// late. Points used to be kept in order by re-sorting the whole slice on every
// insert; the ordered insert that replaced it has to hold the same guarantee.
func TestStore_KeepsPointsOrdered(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ctx := context.Background()

	cases := []struct {
		name    string
		offsets []int // seconds from base, in arrival order
	}{
		{"in order", []int{0, 1, 2, 3, 4}},
		{"reversed", []int{4, 3, 2, 1, 0}},
		{"late arrival in the middle", []int{0, 10, 20, 15, 5}},
		{"duplicate timestamps", []int{0, 5, 5, 5, 1}},
		{"single point", []int{7}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ts := newTestStorage(10, 100)

			for _, off := range c.offsets {
				err := ts.Store(ctx, &MetricEntry{
					Name:      "m",
					Value:     float64(off),
					Timestamp: base.Add(time.Duration(off) * time.Second),
				})
				if err != nil {
					t.Fatalf("Store: %v", err)
				}
			}

			got := stamps(t, ts, "m")
			if len(got) != len(c.offsets) {
				t.Fatalf("got %d points, want %d", len(got), len(c.offsets))
			}

			for i := 1; i < len(got); i++ {
				if got[i].Before(got[i-1]) {
					t.Fatalf("points out of order at %d: %v before %v", i, got[i], got[i-1])
				}
			}
		})
	}
}

// A full series drops its oldest points, and the ones it keeps stay the newest.
func TestStore_TrimsOldestPastCap(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ctx := context.Background()
	ts := newTestStorage(10, 3)

	for i := range 6 {
		if err := ts.Store(ctx, &MetricEntry{
			Name:      "m",
			Value:     float64(i),
			Timestamp: base.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatalf("Store: %v", err)
		}
	}

	got := stamps(t, ts, "m")
	if len(got) != 3 {
		t.Fatalf("got %d points, want 3", len(got))
	}

	for i, want := range []int{3, 4, 5} {
		if !got[i].Equal(base.Add(time.Duration(want) * time.Second)) {
			t.Errorf("point %d = %v, want offset %d", i, got[i], want)
		}
	}
}

// Store keeps the point counters incrementally instead of rescanning the whole
// store on every write. They must still agree with what is actually held.
func TestStore_StatsTrackActualPoints(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ctx := context.Background()
	ts := newTestStorage(10, 3)

	// Two series, each pushed past the per-series cap.
	for i := range 5 {
		for _, name := range []string{"a", "b"} {
			if err := ts.Store(ctx, &MetricEntry{
				Name:      name,
				Value:     1,
				Timestamp: base.Add(time.Duration(i) * time.Second),
			}); err != nil {
				t.Fatalf("Store: %v", err)
			}
		}
	}

	raw, err := ts.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}

	got := raw.(TimeSeriesStorageStats)

	if got.SeriesCount != 2 {
		t.Errorf("SeriesCount = %d, want 2", got.SeriesCount)
	}

	// Two series capped at three points each.
	if got.TotalPoints != 6 {
		t.Errorf("TotalPoints = %d, want 6", got.TotalPoints)
	}

	if got.TotalWrites != 10 {
		t.Errorf("TotalWrites = %d, want 10", got.TotalWrites)
	}

	if want := base.Add(4 * time.Second); !got.NewestPoint.Equal(want) {
		t.Errorf("NewestPoint = %v, want %v", got.NewestPoint, want)
	}

	if want := 3.0; got.AverageSeriesSize != want {
		t.Errorf("AverageSeriesSize = %v, want %v", got.AverageSeriesSize, want)
	}
}

// A point carrying no metadata should not allocate a map for it. The collection
// loop stores one point per metric per tick, so an empty map per point was an
// allocation per metric per tick that nothing read.
func TestStore_LeavesEmptyMetadataNil(t *testing.T) {
	ctx := context.Background()
	ts := newTestStorage(10, 10)

	if err := ts.Store(ctx, &MetricEntry{Name: "m", Value: 1, Timestamp: time.Now()}); err != nil {
		t.Fatalf("Store: %v", err)
	}

	ts.mu.RLock()
	series := ts.series[ts.generateSeriesKey("m", nil)]
	ts.mu.RUnlock()

	if series.Points[0].Metadata != nil {
		t.Errorf("Metadata = %v, want nil", series.Points[0].Metadata)
	}
}

func TestStore_CopiesMetadataWhenPresent(t *testing.T) {
	ctx := context.Background()
	ts := newTestStorage(10, 10)

	meta := map[string]any{"region": "eu"}
	if err := ts.Store(ctx, &MetricEntry{
		Name: "m", Value: 1, Timestamp: time.Now(), Metadata: meta,
	}); err != nil {
		t.Fatalf("Store: %v", err)
	}

	ts.mu.RLock()
	series := ts.series[ts.generateSeriesKey("m", nil)]
	ts.mu.RUnlock()

	if got := series.Points[0].Metadata["region"]; got != "eu" {
		t.Errorf("Metadata[region] = %v, want eu", got)
	}

	// The stored point must not alias the caller's map.
	meta["region"] = "us"

	if got := series.Points[0].Metadata["region"]; got != "eu" {
		t.Errorf("stored metadata aliases the caller's map: got %v", got)
	}
}

// benchStoreCycle simulates one metrics collection tick: one point stored for
// each of `metrics` series against a storage capped at `maxSeries`.
//
// This is the shape that scales with extension count: every extension
// contributes metrics, and the collection loop stores all of them every tick.
// Store used to rescan the entire store on each point, making a tick quadratic
// in the metric count; keep an eye on the per-op time as `metrics` grows.
func benchStoreCycle(b *testing.B, metrics, maxSeries int) {
	ts := newTestStorage(maxSeries, 120)

	names := make([]string, metrics)
	for i := range names {
		names[i] = fmt.Sprintf("forge_ext_%d_requests_total", i)
	}

	ctx := context.Background()
	now := time.Now()

	// Warm up so series carry a realistic number of points.
	for tick := range 60 {
		for _, n := range names {
			_ = ts.Store(ctx, &MetricEntry{
				Name: n, Value: 1.0,
				Timestamp: now.Add(time.Duration(tick) * time.Second),
			})
		}
	}

	b.ReportAllocs()
	b.ResetTimer()

	tick := 60
	for b.Loop() {
		stamp := now.Add(time.Duration(tick) * time.Second)
		for _, n := range names {
			_ = ts.Store(ctx, &MetricEntry{Name: n, Value: 1.0, Timestamp: stamp})
		}

		tick++
	}
}

func BenchmarkTimeSeries_StoreCycle_200(b *testing.B) { benchStoreCycle(b, 200, 500) }

// Over the series cap: every store evicts, which is what an app with many
// extensions hits against the default 500-series limit.
func BenchmarkTimeSeries_StoreCycle_800(b *testing.B) { benchStoreCycle(b, 800, 500) }

// At capacity the store refuses new series instead of evicting a live one.
// Evicting made the store thrash once the installed extensions' metrics
// outnumbered the cap: every write scanned all series, dropped one and created
// another, so nothing ever accumulated history.
func TestStore_RefusesNewSeriesAtCapacity(t *testing.T) {
	ctx := context.Background()
	ts := newTestStorage(3, 10)
	now := time.Now()

	for i := range 3 {
		if err := ts.Store(ctx, &MetricEntry{
			Name: fmt.Sprintf("m%d", i), Value: 1, Timestamp: now,
		}); err != nil {
			t.Fatalf("Store(m%d): %v", i, err)
		}
	}

	// A fourth series does not fit.
	if err := ts.Store(ctx, &MetricEntry{Name: "m3", Value: 1, Timestamp: now}); err == nil {
		t.Fatal("Store(m3) succeeded, want an at-capacity error")
	}

	// The existing series are untouched, and still accept points.
	for i := range 3 {
		name := fmt.Sprintf("m%d", i)
		if err := ts.Store(ctx, &MetricEntry{
			Name: name, Value: 2, Timestamp: now.Add(time.Second),
		}); err != nil {
			t.Errorf("Store(%s) after capacity: %v", name, err)
		}

		if got := len(stamps(t, ts, name)); got != 2 {
			t.Errorf("%s has %d points, want 2", name, got)
		}
	}

	raw, err := ts.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}

	if got := raw.(TimeSeriesStorageStats); got.RejectedSeries != 1 {
		t.Errorf("RejectedSeries = %d, want 1", got.RejectedSeries)
	}
}

// A slot freed by retention cleanup is usable again.
func TestStore_AdmitsNewSeriesAfterCleanupFreesASlot(t *testing.T) {
	ctx := context.Background()
	ts := newTestStorage(2, 10)
	ts.retention = time.Minute

	old := time.Now().Add(-time.Hour)
	fresh := time.Now()

	if err := ts.Store(ctx, &MetricEntry{Name: "stale", Value: 1, Timestamp: old}); err != nil {
		t.Fatalf("Store(stale): %v", err)
	}

	if err := ts.Store(ctx, &MetricEntry{Name: "live", Value: 1, Timestamp: fresh}); err != nil {
		t.Fatalf("Store(live): %v", err)
	}

	if err := ts.Store(ctx, &MetricEntry{Name: "new", Value: 1, Timestamp: fresh}); err == nil {
		t.Fatal("Store(new) succeeded at capacity, want an error")
	}

	ts.cleanup()

	if err := ts.Store(ctx, &MetricEntry{Name: "new", Value: 1, Timestamp: fresh}); err != nil {
		t.Errorf("Store(new) after cleanup: %v", err)
	}
}
