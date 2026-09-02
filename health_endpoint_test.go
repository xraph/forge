package forge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/xraph/forge/internal/logger"
)

// The health endpoints answer from the report the background check loop cached
// rather than running every registered check inline. A readiness probe fires
// every few seconds, and a live check fans out across one check per extension
// plus one per DI service, doing real database and cache round trips, to
// recompute an answer that was already sitting in lastReport.
func newHealthTestApp(t *testing.T) (App, func()) {
	t.Helper()

	app := NewApp(AppConfig{
		Name:              "health-test",
		Version:           "1.0.0",
		Environment:       "test",
		Logger:            logger.NewTestLogger(),
		HealthConfig:      DefaultHealthConfig(),
		HealthGracePeriod: 0,
	})

	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	return app, func() { _ = app.Stop(context.Background()) }
}

func get(t *testing.T, a App, path string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	a.Router().ServeHTTP(w, req)

	return w
}

func TestHealthEndpoints_ServeCachedReportWithoutRerunningChecks(t *testing.T) {
	app, stop := newHealthTestApp(t)
	defer stop()

	var checks int

	if err := app.HealthManager().RegisterFn("counted", func(ctx context.Context) *HealthResult {
		checks++

		return &HealthResult{Status: HealthStatusHealthy, Message: "ok"}
	}); err != nil {
		t.Fatalf("RegisterFn: %v", err)
	}

	// Populate the cache the way the background loop does.
	app.HealthManager().Check(context.Background())

	before := checks

	for range 5 {
		if w := get(t, app, "/_/health"); w.Code != http.StatusOK {
			t.Fatalf("/_/health = %d, want 200; body: %s", w.Code, w.Body.String())
		}

		if w := get(t, app, "/_/health/ready"); w.Code != http.StatusOK {
			t.Fatalf("/_/health/ready = %d, want 200; body: %s", w.Code, w.Body.String())
		}
	}

	if checks != before {
		t.Errorf("ten probes ran the check %d extra times, want 0", checks-before)
	}
}

// The cached report still has to carry the real answer, not a placeholder.
func TestHealthEndpoints_ReportUnhealthyFromCache(t *testing.T) {
	app, stop := newHealthTestApp(t)
	defer stop()

	if err := app.HealthManager().RegisterFn("broken", func(ctx context.Context) *HealthResult {
		return &HealthResult{Status: HealthStatusUnhealthy, Message: "down"}
	}); err != nil {
		t.Fatalf("RegisterFn: %v", err)
	}

	app.HealthManager().Check(context.Background())

	if w := get(t, app, "/_/health"); w.Code != http.StatusServiceUnavailable {
		t.Errorf("/_/health = %d, want 503", w.Code)
	}

	if w := get(t, app, "/_/health/ready"); w.Code != http.StatusServiceUnavailable {
		t.Errorf("/_/health/ready = %d, want 503", w.Code)
	}
}

// A cache the check loop has stopped refreshing must not read as healthy
// forever. Past the staleness bound the endpoints report failure instead.
func TestHealthEndpoints_RejectStaleReport(t *testing.T) {
	testApp, stop := newHealthTestApp(t)
	defer stop()

	impl := testApp.(*app)

	report := impl.healthManager.Check(context.Background())
	report.Timestamp = time.Now().Add(-healthStalenessFactor*impl.config.HealthConfig.Intervals.Check - time.Second)

	if _, fresh := impl.healthReport(context.Background()); fresh {
		t.Fatal("healthReport reported a stale report as fresh")
	}

	w := get(t, testApp, "/_/health")
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("/_/health = %d, want 503", w.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if body["status"] != "stale" {
		t.Errorf("status = %v, want stale", body["status"])
	}

	if w := get(t, testApp, "/_/health/ready"); w.Code != http.StatusServiceUnavailable {
		t.Errorf("/_/health/ready = %d, want 503", w.Code)
	}
}

// Liveness never runs checks at all, and never should.
func TestHealthEndpoints_LivenessAlwaysAnswers(t *testing.T) {
	app, stop := newHealthTestApp(t)
	defer stop()

	if w := get(t, app, "/_/health/live"); w.Code != http.StatusOK {
		t.Errorf("/_/health/live = %d, want 200", w.Code)
	}
}
