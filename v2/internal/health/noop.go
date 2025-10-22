package health

import (
	"context"
	"time"

	"github.com/xraph/forge/v2/internal/shared"
)

// =============================================================================
// NO-OP HEALTH MANAGER
// =============================================================================

// noOpHealthManager implements HealthManager interface with no-op operations
// for testing and scenarios where health checking is disabled
type noOpHealthManager struct{}

// NewNoOpHealthManager creates a no-op health manager that implements the full
// HealthManager interface but performs no actual health checking or monitoring.
// Useful for testing, benchmarking, or when health checks are disabled.
func NewNoOpHealthManager() shared.HealthManager {
	return &noOpHealthManager{}
}

// =============================================================================
// SERVICE LIFECYCLE IMPLEMENTATION (NO-OP)
// =============================================================================

func (m *noOpHealthManager) Name() string {
	return "noop-health"
}

func (m *noOpHealthManager) Start(ctx context.Context) error {
	return nil
}

func (m *noOpHealthManager) Stop(ctx context.Context) error {
	return nil
}

func (m *noOpHealthManager) OnHealthCheck(ctx context.Context) error {
	return nil
}

// =============================================================================
// HEALTH CHECK MANAGEMENT (NO-OP)
// =============================================================================

func (m *noOpHealthManager) Register(check shared.HealthCheck) error {
	return nil
}

func (m *noOpHealthManager) RegisterFn(name string, check shared.HealthCheckFn) error {
	return nil
}

func (m *noOpHealthManager) Unregister(name string) error {
	return nil
}

// =============================================================================
// HEALTH CHECK EXECUTION (NO-OP)
// =============================================================================

func (m *noOpHealthManager) Check(ctx context.Context) *shared.HealthReport {
	return shared.NewHealthReport().WithVersion("noop")
}

func (m *noOpHealthManager) CheckOne(ctx context.Context, name string) *shared.HealthResult {
	return shared.NewHealthResult(name, shared.HealthStatusHealthy, "noop check")
}

// =============================================================================
// STATUS AND REPORTING (NO-OP)
// =============================================================================

func (m *noOpHealthManager) GetStatus() shared.HealthStatus {
	return shared.HealthStatusHealthy
}

func (m *noOpHealthManager) GetLastReport() *shared.HealthReport {
	return shared.NewHealthReport()
}

func (m *noOpHealthManager) GetChecks() map[string]shared.HealthCheck {
	return make(map[string]shared.HealthCheck)
}

func (m *noOpHealthManager) GetStats() *shared.HealthCheckerStats {
	return &shared.HealthCheckerStats{
		RegisteredChecks: 0,
		Subscribers:      0,
		Started:          false,
		Uptime:           0,
		LastReportTime:   time.Time{},
		OverallStatus:    shared.HealthStatusHealthy,
		LastReport:       nil,
	}
}

// =============================================================================
// SUBSCRIPTION (NO-OP)
// =============================================================================

func (m *noOpHealthManager) Subscribe(callback shared.HealthCallback) error {
	return nil
}

// =============================================================================
// CONFIGURATION (NO-OP)
// =============================================================================

func (m *noOpHealthManager) SetEnvironment(name string) {}

func (m *noOpHealthManager) SetVersion(version string) {}

func (m *noOpHealthManager) SetHostname(hostname string) {}

func (m *noOpHealthManager) Environment() string {
	return "noop"
}

func (m *noOpHealthManager) Hostname() string {
	return "noop"
}

func (m *noOpHealthManager) Version() string {
	return "noop"
}

func (m *noOpHealthManager) StartTime() time.Time {
	return time.Time{}
}

// =============================================================================
// RELOAD CONFIGURATION (NO-OP)
// =============================================================================

func (m *noOpHealthManager) Reload(config *shared.HealthConfig) error {
	return nil
}
