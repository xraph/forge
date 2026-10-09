package conduit

import (
	"context"
	"fmt"

	"github.com/xraph/forge"
	conduitcontract "github.com/xraph/forge/extensions/conduit/contract"
	"github.com/xraph/forge/extensions/conduit/core"
	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"
	"github.com/xraph/vessel"
)

// ServiceKey identifies the runtime in Forge's dependency container.
const ServiceKey = "conduit"

// Extension owns the runtime and its Forge lifecycle.
type Extension struct {
	*forge.BaseExtension

	runtime *Runtime
}

// NewExtension constructs a Conduit extension with explicit service identity.
func NewExtension(opts ...Option) (*Extension, error) {
	runtime, err := core.NewDeferred(opts...)
	if err != nil {
		return nil, err
	}

	return &Extension{BaseExtension: forge.NewBaseExtension("conduit", "1.0.0", "Service communication and durable messaging"), runtime: runtime}, nil
}

// NewExtensionWithConfig supplies explicit topology while allowing identity inference.
func NewExtensionWithConfig(cfg Config, opts ...Option) (*Extension, error) {
	return NewExtension(append([]Option{WithConfig(cfg)}, opts...)...)
}

// DepsSpec starts Forge discovery before Conduit when it is installed.
func (e *Extension) DepsSpec() []forge.Dep { return []forge.Dep{forge.DepOptionalSpec("discovery")} }

// Runtime exposes typed handler registration before application startup.
func (e *Extension) Runtime() *Runtime { return e.runtime }

// Register provides the runtime to Forge's dependency container.
func (e *Extension) Register(app forge.App) error {
	if err := e.BaseExtension.Register(app); err != nil {
		return err
	}

	if err := e.configure(app); err != nil {
		return err
	}

	if e.Metrics() != nil {
		if err := e.runtime.RegisterHook(HookFuncs{HookName: "forge.metrics", OnEvent: func(_ context.Context, event HookEvent) {
			e.Metrics().Counter("forge_conduit_outcomes_total", forge.WithLabels(map[string]string{"namespace": event.Identity.Namespace, "service": event.Identity.ServiceID, "outcome": string(event.Stage)})).Inc()
		}}); err != nil {
			return err
		}
	}

	if err := e.RegisterConstructor(func() (*Runtime, error) { return e.runtime, nil }, vessel.WithAliases(ServiceKey)); err != nil {
		return fmt.Errorf("conduit: register runtime: %w", err)
	}

	return nil
}

// Start connects providers before accepting service messages.
func (e *Extension) Start(ctx context.Context) error {
	if err := e.runtime.Start(ctx); err != nil {
		return err
	}

	e.MarkStarted()

	return nil
}

// Stop drains workers and closes this instance's connections.
func (e *Extension) Stop(ctx context.Context) error {
	err := e.runtime.Stop(ctx)
	e.MarkStopped()

	return err
}

// Health checks the active provider connections.
func (e *Extension) Health(ctx context.Context) error { return e.runtime.Health(ctx) }

// HookObserverDrops reports bounded observer backpressure independently of handler outcomes.
func (e *Extension) HookObserverDrops(ctx context.Context) (uint64, error) {
	snapshot, err := e.runtime.Snapshot(ctx)

	return snapshot.ObserverDrops, err
}

var _ core.Hook = HookFuncs{}

// RegisterContractContributor registers the conduit React dashboard's intent surface.
func (e *Extension) RegisterContractContributor(disp *dispatcher.Dispatcher, registry dashcontract.Registry, wardens dashcontract.WardenRegistry) error {
	return conduitcontract.Register(disp, registry, wardens, conduitcontract.Deps{Runtime: func() *Runtime { return e.runtime }})
}
