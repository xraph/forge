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
type Deps struct {
	ExtensionsRegistry *contributor.ContributorRegistry
	Services           ServicesProvider
	Metrics            MetricsProvider
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
	return nil
}
