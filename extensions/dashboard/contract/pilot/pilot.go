package pilot

import (
	"bytes"
	_ "embed"
	"fmt"
	"time"

	"github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"
	"github.com/xraph/forge/extensions/dashboard/contract/loader"
	"github.com/xraph/forge/extensions/dashboard/contributor"
)

//go:embed manifest.yaml
var manifestYAML []byte

// DefaultMetricsInterval is the production tick rate for metrics.summary.
const DefaultMetricsInterval = 5 * time.Second

// Deps bundles the data sources the pilot handlers need. The dashboard
// extension constructs this when it wires the pilot at startup.
//
// Slice (c) introduced ExtensionsRegistry / Services / Metrics. Slice (h)
// adds Overview / Health / MetricsReport / Traces so the pilot covers every
// page CoreContributor serves today; nil providers are tolerated and the
// corresponding handlers return CodeUnavailable.
type Deps struct {
	ExtensionsRegistry *contributor.ContributorRegistry
	Services           ServicesProvider
	Metrics            MetricsProvider
	Overview           OverviewProvider
	Health             HealthProvider
	MetricsReport      MetricsReportProvider
	Traces             TracesProvider
	// Audit is the slice (k) audit store. nil yields CodeUnavailable on
	// audit.list / audit.tail; the rest of the pilot stays functional.
	Audit AuditProvider
	// MetricsInterval is how often metrics.summary emits. Zero defaults to
	// DefaultMetricsInterval. Tests use millisecond values.
	MetricsInterval time.Duration
}

// Register loads the embedded pilot manifest, validates it, registers it with
// the contract registry, and binds the four handlers against the dispatcher.
// Idempotent: calling twice on the same registries returns the duplicate-
// registration error from the second call.
func Register(d *dispatcher.Dispatcher, contractReg contract.Registry, wreg contract.WardenRegistry, deps Deps) error {
	if deps.ExtensionsRegistry == nil {
		return fmt.Errorf("pilot: ExtensionsRegistry is required")
	}
	if deps.Services == nil {
		return fmt.Errorf("pilot: Services is required")
	}
	if deps.Metrics == nil {
		return fmt.Errorf("pilot: Metrics is required")
	}
	interval := deps.MetricsInterval
	if interval <= 0 {
		interval = DefaultMetricsInterval
	}

	m, err := loader.Load(bytes.NewReader(manifestYAML), "pilot/manifest.yaml")
	if err != nil {
		return fmt.Errorf("pilot: loading manifest: %w", err)
	}
	if err := loader.Validate(m, wreg); err != nil {
		return fmt.Errorf("pilot: validating manifest: %w", err)
	}
	if err := contractReg.Register(m); err != nil {
		return fmt.Errorf("pilot: contract registry: %w", err)
	}

	const c = "core-contract"
	if err := dispatcher.RegisterQuery(d, c, "extensions.list", 1, extensionsListHandler(deps.ExtensionsRegistry)); err != nil {
		return err
	}
	if err := dispatcher.RegisterQuery(d, c, "services.list", 1, servicesListHandler(deps.Services)); err != nil {
		return err
	}
	if err := dispatcher.RegisterQuery(d, c, "services.detail", 1, servicesDetailHandler(deps.Services)); err != nil {
		return err
	}
	if err := dispatcher.RegisterSubscription(d, c, "metrics.summary", 1, metricsSummarySub(deps.Metrics, interval)); err != nil {
		return err
	}

	// Slice (h) registrations. Provider nil-checks happen inside each handler
	// (returning CodeUnavailable), so partial wiring during a rollout is OK.
	if err := dispatcher.RegisterQuery(d, c, "overview", 1, overviewHandler(deps.Overview)); err != nil {
		return err
	}
	if err := dispatcher.RegisterQuery(d, c, "health", 1, healthHandler(deps.Health)); err != nil {
		return err
	}
	if err := dispatcher.RegisterQuery(d, c, "metrics-report", 1, metricsReportHandler(deps.MetricsReport)); err != nil {
		return err
	}
	if err := dispatcher.RegisterQuery(d, c, "traces.list", 1, tracesListHandler(deps.Traces)); err != nil {
		return err
	}
	if err := dispatcher.RegisterQuery(d, c, "traces.detail", 1, traceDetailHandler(deps.Traces)); err != nil {
		return err
	}

	// Slice (k) audit. nil-tolerant: handlers return CodeUnavailable if the
	// store wasn't wired (e.g. tests of unrelated handlers).
	if err := dispatcher.RegisterQuery(d, c, "audit.list", 1, auditListHandler(deps.Audit)); err != nil {
		return err
	}
	if err := dispatcher.RegisterSubscription(d, c, "audit.tail", 1, auditTailSub(deps.Audit)); err != nil {
		return err
	}
	// Slice (l) navigation. Walks the merged contract registry to produce the
	// pre-grouped sidebar payload the React shell renders.
	if err := dispatcher.RegisterQuery(d, c, "navigation", 1, navigationHandler(contractReg)); err != nil {
		return err
	}
	// apps.list: feeds the React shell's app switcher with every
	// contributor that opted into being a switchable app. Separate from
	// extensions.list (the broader catalog) so the switcher's dropdown
	// stays focused on user-facing apps.
	if err := dispatcher.RegisterQuery(d, c, "apps.list", 1, appsListHandler(contractReg)); err != nil {
		return err
	}
	return nil
}
