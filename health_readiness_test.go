package forge

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

// Readiness answers 503 as soon as shutdown starts.
func TestReadiness_TurnsUnreadyOnShutdown(t *testing.T) {
	a := newRunApp(t)
	a.config.HealthGracePeriod = 0

	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = a.Stop(context.Background()) })

	if w := get(t, a, "/_/health/ready"); w.Code != http.StatusOK {
		t.Fatalf("/_/health/ready = %d before shutdown, want 200", w.Code)
	}

	a.shuttingDown.Store(true)

	if w := get(t, a, "/_/health/ready"); w.Code != http.StatusServiceUnavailable {
		t.Errorf("/_/health/ready = %d during shutdown, want 503", w.Code)
	}

	if w := get(t, a, "/_/health/live"); w.Code != http.StatusOK {
		t.Errorf("/_/health/live = %d during shutdown, want 200", w.Code)
	}
}

// unhealthyExtension fails its health check.
type unhealthyExtension struct{ *BaseExtension }

func (u *unhealthyExtension) Health(context.Context) error { return errors.New("down") }

// Readiness used to answer "ready" for the whole grace period whatever the
// checks said, and the extension checks only reached the cached report on
// the first scheduled round, 30s after start.
func TestReadiness_IgnoresGraceAndSeesExtensionChecksAtOnce(t *testing.T) {
	a := newRunApp(t, &unhealthyExtension{NewBaseExtension("db", "1.0.0", "d")})
	a.config.HealthGracePeriod = time.Minute

	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = a.Stop(context.Background()) })

	report := a.healthManager.LastReport()
	if report == nil || report.Services["extension:db"] == nil {
		t.Fatal("the cached report after Start does not include the extension check")
	}

	if w := get(t, a, "/_/health/ready"); w.Code != http.StatusServiceUnavailable {
		t.Errorf("/_/health/ready = %d inside the grace period with a failing extension, want 503", w.Code)
	}

	// The liveness-style endpoint keeps its grace period.
	if w := get(t, a, "/_/health"); w.Code != http.StatusOK {
		t.Errorf("/_/health = %d inside the grace period, want 200", w.Code)
	}
}
