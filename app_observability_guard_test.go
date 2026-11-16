package forge

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xraph/go-utils/log"

	forgelogger "github.com/xraph/forge/internal/logger"
)

const leakyDSN = "postgres://app_rw:hunter2@10.0.3.17:5432/prod"

// leakyExtension fails its health check with the kind of error a database
// driver really returns: host, user and the DSN with its password.
type leakyExtension struct{ *BaseExtension }

func (l *leakyExtension) Health(context.Context) error {
	return errors.New(`failed to connect to "host=10.0.3.17 user=app_rw database=prod": password authentication failed (dsn ` + leakyDSN + `)`)
}

func newExposureApp(t *testing.T, env string, opts ...AppOption) (App, *log.TestLogger) {
	t.Helper()
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	logs := forgelogger.NewTestLogger().(*log.TestLogger)

	cfg := DefaultAppConfig()
	cfg.Name = "prodapp"
	cfg.Environment = env
	cfg.HealthGracePeriod = time.Nanosecond
	cfg.EnableConfigAutoDiscovery = false
	cfg.Logger = logs
	cfg.Extensions = []Extension{&leakyExtension{NewBaseExtension("database", "1.2.3", "db")}}

	a := New(append([]AppOption{WithConfig(cfg)}, opts...)...)
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = a.Stop(context.Background()) })

	// Run a round the way the check loop does, so the cached report covers
	// the extension check whatever Start already ran.
	a.HealthManager().Check(context.Background())

	return a, logs
}

func obsGet(t *testing.T, a App, path string, hdr ...string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}

	w := httptest.NewRecorder()
	a.Router().ServeHTTP(w, req)

	return w
}

// In production with no guard, nothing an extension's health error says may
// reach a client, /_/info does not exist, and readiness still works.
func TestObservability_ProductionDefaultsDoNotLeak(t *testing.T) {
	a, logs := newExposureApp(t, "production")

	for _, p := range []string{"/_/info", "/_/health", "/_/health/ready", "/_/health/live", "/_/metrics"} {
		w := obsGet(t, a, p)
		if strings.Contains(w.Body.String(), "hunter2") || strings.Contains(w.Body.String(), "10.0.3.17") {
			t.Errorf("%s leaks the extension error: %s", p, w.Body.String())
		}
	}

	if w := obsGet(t, a, "/_/info"); w.Code != http.StatusNotFound {
		t.Errorf("/_/info = %d, want 404 because it is opt-in", w.Code)
	}

	w := obsGet(t, a, "/_/health")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("/_/health = %d, want 503; body %s", w.Code, w.Body.String())
	}

	var summary struct {
		Overall  string                    `json:"overall"`
		Services map[string]map[string]any `json:"services"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}

	if summary.Overall != string(HealthStatusUnhealthy) {
		t.Errorf("overall = %q, want unhealthy", summary.Overall)
	}

	ext, ok := summary.Services["extension:database"]
	if !ok {
		t.Fatalf("summary has no extension:database check: %s", w.Body.String())
	}

	if len(ext) != 1 || ext["status"] != string(HealthStatusUnhealthy) {
		t.Errorf("an unauthorized caller should see only the check status, got %v", ext)
	}

	if w := obsGet(t, a, "/_/health/ready"); w.Code != http.StatusServiceUnavailable {
		t.Errorf("/_/health/ready = %d, want 503 without any guard", w.Code)
	}

	if w := obsGet(t, a, "/_/health/live"); w.Code != http.StatusOK {
		t.Errorf("/_/health/live = %d, want 200 without any guard", w.Code)
	}

	// The operator still gets the cause, in the log.
	found := false

	for _, e := range logs.GetLogsByLevel("WARN") {
		if e.Message != "extension health check failed" {
			continue
		}

		if v, _ := e.Field("error"); strings.Contains(v.(string), "password authentication failed") {
			found = true
		}
	}

	if !found {
		t.Error("the raw extension error is not in the log")
	}
}

// A guard decides who sees details. Denied callers get statuses only and
// 404s for info and metrics; allowed callers get the full report, which still
// carries no raw extension error.
func TestObservability_GuardControlsDetail(t *testing.T) {
	guard := func(r *http.Request) bool { return r.Header.Get("X-Ops") == "yes" }
	a, _ := newExposureApp(t, "production", WithObservabilityGuard(guard), WithInfoEndpoint())

	if w := obsGet(t, a, "/_/info"); w.Code != http.StatusNotFound {
		t.Errorf("denied /_/info = %d, want 404", w.Code)
	}

	if w := obsGet(t, a, "/_/metrics"); w.Code != http.StatusNotFound {
		t.Errorf("denied /_/metrics = %d, want 404", w.Code)
	}

	if w := obsGet(t, a, "/_/info", "X-Ops", "yes"); w.Code != http.StatusOK {
		t.Errorf("allowed /_/info = %d, want 200", w.Code)
	}

	if w := obsGet(t, a, "/_/metrics", "X-Ops", "yes"); w.Code != http.StatusOK {
		t.Errorf("allowed /_/metrics = %d, want 200", w.Code)
	}

	denied := obsGet(t, a, "/_/health")
	if strings.Contains(denied.Body.String(), "extension unhealthy") {
		t.Errorf("denied /_/health carries check messages: %s", denied.Body.String())
	}

	allowed := obsGet(t, a, "/_/health", "X-Ops", "yes")
	if !strings.Contains(allowed.Body.String(), "database extension unhealthy") {
		t.Errorf("allowed /_/health should carry the full report: %s", allowed.Body.String())
	}

	if strings.Contains(allowed.Body.String(), "hunter2") {
		t.Errorf("even an allowed /_/health must not carry the raw extension error: %s", allowed.Body.String())
	}

	// Probes never need the guard.
	if w := obsGet(t, a, "/_/health/live"); w.Code != http.StatusOK {
		t.Errorf("/_/health/live = %d, want 200", w.Code)
	}
}

// Without a guard an explicit development environment keeps the full report,
// so local debugging is unchanged.
func TestObservability_DevelopmentKeepsDetails(t *testing.T) {
	a, _ := newExposureApp(t, "development", WithAppEnvironment("development"))

	w := obsGet(t, a, "/_/health")
	if !strings.Contains(w.Body.String(), "database extension unhealthy") {
		t.Errorf("development /_/health should carry check messages: %s", w.Body.String())
	}
}

// Probes answer from one cached report, so an unhealthy app used to log a
// warning per probe. It now logs once per report.
func TestObservability_UnhealthyWarningIsNotPerRequest(t *testing.T) {
	a, logs := newExposureApp(t, "production")

	for range 50 {
		obsGet(t, a, "/_/health")
	}

	n := 0

	for _, e := range logs.GetLogsByLevel("WARN") {
		if e.Message == "health check reporting unhealthy status" {
			n++
		}
	}

	if n != 1 {
		t.Errorf("50 probes of one report logged %d unhealthy warnings, want 1", n)
	}

	// The extension error itself is logged once, not once per check round.
	a.HealthManager().Check(context.Background())
	a.HealthManager().Check(context.Background())

	n = 0

	for _, e := range logs.GetLogsByLevel("WARN") {
		if e.Message == "extension health check failed" {
			n++
		}
	}

	if n != 1 {
		t.Errorf("the same extension error was logged %d times, want 1", n)
	}
}
