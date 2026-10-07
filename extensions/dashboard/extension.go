package dashboard

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/vessel"
	"go.opentelemetry.io/otel"

	internalmetrics "github.com/xraph/forge/internal/metrics"

	dashauth "github.com/xraph/forge/extensions/dashboard/auth"
	"github.com/xraph/forge/extensions/dashboard/collector"
	"github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"
	"github.com/xraph/forge/extensions/dashboard/contract/idempotency"
	"github.com/xraph/forge/extensions/dashboard/contract/loader"
	"github.com/xraph/forge/extensions/dashboard/contract/pilot"
	contractremote "github.com/xraph/forge/extensions/dashboard/contract/remote"
	"github.com/xraph/forge/extensions/dashboard/contract/transport"
	dashboarddiscovery "github.com/xraph/forge/extensions/dashboard/discovery"
	"github.com/xraph/forge/extensions/dashboard/handlers"
	"github.com/xraph/forge/extensions/dashboard/security"
)

// Extension serves the dashboard's data plane: the contract API under
// {BasePath}/api/dashboard/v1 that every dashboard client talks to, plus the
// collector, trace store and export endpoints behind it. It serves no web
// pages. The dashboard UI is a separate React app built from the
// @forge-go/dashboard-* packages, and extensions contribute to it by
// registering contract intents (ContractContributorAware) and shipping a
// plugin.
type Extension struct {
	*forge.BaseExtension

	config         Config
	app            forge.App
	collector      *collector.DataCollector
	history        *collector.DataHistory
	csrfMgr        *security.CSRFManager
	traceStore     *collector.TraceStore
	authChecker    dashauth.AuthChecker
	tenantResolver dashauth.TenantResolver

	discoverySvc   dashboarddiscovery.DiscoveryService
	discoveryInteg *dashboarddiscovery.Integration

	routesRegistered bool

	// Contract state. The dashboard contract provides an envelope-based transport
	// (POST /api/dashboard/v1) and an audit/warden pipeline. All four fields
	// (contractRegistry, wardenRegistry, streamBroker, auditEmitter) plus the
	// dispatcher below are non-nil after construction; the dispatcher acts as
	// the StreamBroker's SubscriptionSource so the SSE multiplex routes serve
	// real subscriptions registered via dispatcher.RegisterSubscription.
	contractRegistry contract.Registry
	wardenRegistry   contract.WardenRegistry
	streamBroker     *transport.StreamBroker
	auditEmitter     contract.AuditEmitter
	auditStore       contract.AuditStore
	dispatcher       *dispatcher.Dispatcher

	// forwardingInstalled is set the first time a remote contract
	// contributor is registered; the forwarding dispatcher itself reads
	// endpoints from the registry on every dispatch so one instance covers
	// all remotes. forwardingMu guards both this flag and any future
	// SetRemoteDispatcher reconfiguration.
	forwardingMu        sync.Mutex
	forwardingInstalled bool

	// contributorStatus maps a contract contributor's name to the extension
	// that registered it, for extensions implementing DashboardStatusAware.
	// The interface value is stored rather than a DashboardStatus snapshot so
	// the capabilities endpoint reads the live answer — Configured can flip
	// after boot when an extension is configured at runtime.
	//
	// contributorStatusMu guards the map and nothing else. Writers are
	// attributeContributorStatus and forgetContributorStatus; the capabilities
	// handler reads through contributorStatusFor while serving. Discovery
	// writes during Start, but an extension may hold the registry it was
	// handed and register more contributors later from its own goroutine, so
	// "writes finish before serving begins" is not an invariant we have.
	// What the lock does not cover, because the recorder made it moot: the
	// registry state around a registration. Attribution is now observed
	// inside the Register call rather than reconstructed from before/after
	// snapshots, so no cross-registry window needs guarding.
	contributorStatusMu sync.Mutex
	contributorStatus   map[string]DashboardStatusAware
}

// NewExtension creates a new dashboard extension.
func NewExtension(opts ...ConfigOption) forge.Extension {
	config := DefaultConfig()
	for _, opt := range opts {
		opt(&config)
	}

	base := forge.NewBaseExtension(
		"dashboard",
		"3.0.0",
		"Dashboard contract API and data collection",
	)

	auditStore := contract.NewInMemoryAuditStore(0)
	ext := &Extension{
		BaseExtension:    base,
		config:           config,
		contractRegistry: contract.NewRegistry(),
		wardenRegistry:   contract.NewWardenRegistry(),
		// auditEmitter records to the in-memory store before chaining to the
		// log emitter so the /audit page sees every command run end-to-end.
		// app.Logger() isn't available yet here; Register() upgrades the inner
		// emitter to the structured logger variant.
		auditStore:   auditStore,
		auditEmitter: contract.NewRecordingAuditEmitter(contract.NewLogAuditEmitter(os.Stdout), auditStore),
		dispatcher:   dispatcher.New(dispatcher.NoopMetricsEmitter{}),
	}
	// The stream broker uses the dispatcher as its SubscriptionSource so the
	// SSE multiplex routes serve real subscriptions registered via the
	// dispatcher (see registerRoutes — the broker-bound stream/control routes
	// activate now that streamBroker is non-nil).
	ext.streamBroker = transport.NewStreamBroker(ext.contractRegistry, ext.wardenRegistry, ext.dispatcher)
	return ext
}

// Register registers the dashboard extension.
func (e *Extension) Register(app forge.App) error {
	if err := e.BaseExtension.Register(app); err != nil {
		return err
	}

	e.app = app

	// Load config from ConfigManager with dual-key support
	programmaticConfig := e.config

	finalConfig := DefaultConfig()
	if err := e.LoadConfig("dashboard", &finalConfig, programmaticConfig, DefaultConfig(), programmaticConfig.RequireConfig); err != nil {
		if programmaticConfig.RequireConfig {
			return fmt.Errorf("dashboard: failed to load required config: %w", err)
		}

		e.Logger().Warn("dashboard: using default/programmatic config",
			forge.F("error", err.Error()),
		)
	}

	e.config = finalConfig

	// Validate config
	if err := e.config.Validate(); err != nil {
		return fmt.Errorf("dashboard config validation failed: %w", err)
	}

	// Initialize data history. If the metrics provider supports time-series
	// queries, pass it to DataHistory so metric charts query the provider
	// directly instead of maintaining a parallel copy.
	var tsProvider internalmetrics.TimeSeriesQueryProvider
	if tp, ok := app.Metrics().(internalmetrics.TimeSeriesQueryProvider); ok {
		tsProvider = tp
	}
	e.history = collector.NewDataHistory(e.config.MaxDataPoints, e.config.HistoryDuration, tsProvider)

	// Initialize data collector
	e.collector = collector.NewDataCollector(
		app.HealthManager(),
		app.Metrics(),
		app.Container(),
		app.Logger(),
		e.history,
	)
	e.collector.SetCacheTTL(e.config.RefreshInterval)

	// Initialize trace store for dashboard tracing UI
	e.traceStore = collector.NewTraceStore(e.config.TraceMaxCount, e.config.TraceRetention, collector.WithMaxSpansPerTrace(e.config.TraceMaxSpansPerTrace))

	// Retain spans only while somebody is actually using the dashboard. The
	// marker is stamped by TracingMiddleware on any request under BasePath, and
	// an open contract stream counts too: a client that only streams issues no
	// further requests after it connects, so without this the gate would shut
	// under an active viewer. A service nobody ever visits pays nothing.
	//
	// The closure reads e.streamBroker when it runs rather than capturing it
	// here, because the broker is rebuilt against the upgraded dispatcher
	// further down this method.
	if ttl := e.config.TraceIdleTTL; ttl > 0 {
		ts := e.traceStore
		ts.SetIngestGate(func() bool {
			if b := e.streamBroker; b != nil && b.ConnectionCount() > 0 {
				return true
			}
			return time.Since(ts.LastAccessed()) < ttl
		})
	}

	if e.config.EnableCSRF {
		e.csrfMgr = security.NewCSRFManager()
		e.Logger().Debug("CSRF protection initialized")
	}

	// Slice (b) Phase 6: replace the safe defaults wired in NewExtension with
	// production-grade contract plumbing — Prometheus metrics emission, OTel
	// tracing, idempotency dedup, and structured audit logging. The swap is
	// gated by EnableContractSecurity so deployments mid-rollout (clients not
	// yet sending CSRF tokens / idempotency keys) can opt out without losing
	// the rest of the contract path. Must run before pilot.Register so the
	// pilot binds against the upgraded dispatcher.
	var metricsEmitter dispatcher.MetricsEmitter = dispatcher.NoopMetricsEmitter{}
	if app != nil && app.Metrics() != nil {
		metricsEmitter = dispatcher.NewPrometheusMetricsEmitter(app.Metrics())
	}
	var dispOpts []dispatcher.Option
	if e.config.EnableContractSecurity {
		dispOpts = append(dispOpts,
			dispatcher.WithTracer(otel.Tracer("forge.dashboard.contract")),
			dispatcher.WithIdempotencyStore(adaptIdempotencyStore(idempotency.NewInMemoryStore())),
		)
	}
	e.dispatcher = dispatcher.NewWithOptions(metricsEmitter, dispOpts...)

	var inner contract.AuditEmitter = contract.NewLogAuditEmitter(os.Stdout)
	if app != nil && app.Logger() != nil {
		inner = dispatcher.NewLoggerAuditEmitter(app.Logger())
	}
	// Slice (k): always wrap the chosen logger emitter with the recording
	// emitter so the /audit page mirrors what's logged.
	e.auditEmitter = contract.NewRecordingAuditEmitter(inner, e.auditStore)

	// The streamBroker captured the old dispatcher at NewExtension time;
	// rebind it to the upgraded dispatcher so SSE subscriptions resolve
	// against the same handler registry the POST endpoint uses.
	e.streamBroker = transport.NewStreamBroker(e.contractRegistry, e.wardenRegistry, e.dispatcher)

	// Register the contract-track pilot contributor (core-contract). This
	// loads the embedded manifest, validates it against the warden registry,
	// and binds the pilot handlers against the dispatcher. Must run after
	// e.collector and e.traceStore are initialised, since both feed the pilot
	// Deps directly.
	if err := pilot.Register(e.dispatcher, e.contractRegistry, e.wardenRegistry, pilot.Deps{
		Extensions: appExtensions{app: app},
		Services:   e.collector,
		Metrics:    e.collector,
		// Slice (h): wire the remaining data sources (the same ones the old
		// core pages rendered) so the pilot covers Overview / Health /
		// Metrics report / Traces.
		Overview:      e.collector,
		Health:        e.collector,
		MetricsReport: e.collector,
		Traces:        e.traceStore,
		// Slice (k) — back the audit.list query and audit.tail subscription.
		Audit: e.auditStore,
	}); err != nil {
		return fmt.Errorf("dashboard: registering contract pilot: %w", err)
	}

	// Register dashboard extension with DI container.
	// Use ProvideValue with WithAliases so it's resolvable by both type and name.
	if err := vessel.ProvideValue[*Extension](app.Container(), e, vessel.WithAliases("dashboard")); err != nil {
		return fmt.Errorf("failed to register dashboard service: %w", err)
	}

	e.Logger().Info("dashboard extension registered",
		forge.F("base_path", e.config.BasePath),
		forge.F("export", e.config.EnableExport),
		forge.F("auth", e.config.EnableAuth),
	)

	return nil
}

// Start starts the dashboard extension.
func (e *Extension) Start(ctx context.Context) error {
	// Guard against duplicate Start() — the DI container may also call Start()
	// on type-registry services implementing di.Starter.
	if e.IsStarted() {
		return nil
	}

	e.Logger().Info("starting dashboard extension")

	// Auto-discover contract contributors and auth providers. Must run before
	// registerRoutes: an auth provider sets the AuthChecker here, and the
	// routes read it when they attach the auth middleware.
	e.discoverExtensionContributors()

	// Register tracing middleware to auto-capture request traces
	if e.traceStore != nil {
		e.app.Router().UseGlobal(TracingMiddleware(e.traceStore, e.config.BasePath, e.config.TraceCaptureRequestBody))
		e.Logger().Debug("dashboard tracing middleware registered")
	}

	// Register routes (only once)
	if !e.routesRegistered {
		e.registerRoutes()
		e.routesRegistered = true
	}

	// Start data collection
	go e.collector.Start(ctx, e.config.RefreshInterval)

	// Discovery must start after PhaseAfterRegister, where consumers wire
	// SetDiscoveryService, and that phase fires only once every extension's
	// Start has returned. A BeforeRun hook gives the wiring its turn first;
	// starting here would find the service still nil and skip discovery.
	if e.config.EnableDiscovery {
		if err := forge.OnBeforeRun(e.app, "dashboard-discovery-startup", func(hookCtx context.Context, _ forge.App) error {
			e.startDiscovery(hookCtx)

			return nil
		}); err != nil {
			e.Logger().Warn("failed to register discovery startup hook",
				forge.F("error", err.Error()),
			)
		}
	}

	e.MarkStarted()
	e.Logger().Info("dashboard extension started",
		forge.F("base_path", e.config.BasePath),
		forge.F("contract_contributors", len(e.contractRegistry.All())),
	)

	return nil
}

// discoverExtensionContributors scans all registered extensions for
// ContractContributorAware and DashboardAuthAware, registering their contract
// contributors and auth providers.
func (e *Extension) discoverExtensionContributors() {
	extensions := e.app.Extensions()
	for _, ext := range extensions {
		// Skip ourselves
		if ext.Name() == e.Name() {
			continue
		}

		// Extensions that report a dashboard status get a recorder, which
		// captures the name of every contract contributor they register as
		// they register it. Nothing is inferred: attribution is a fact
		// observed at the registration call, so it does not depend on start
		// order and has no window between observing and recording.
		var statusRec *contributorStatusRecorder
		if sa, ok := ext.(DashboardStatusAware); ok {
			statusRec = &contributorStatusRecorder{ext: sa, host: e}
		}

		// Auto-register DashboardAuthAware auth providers
		if authAware, ok := ext.(DashboardAuthAware); ok {
			authAware.RegisterDashboardAuth(e)
			e.Logger().Info("auto-discovered dashboard auth provider",
				forge.F("extension", ext.Name()),
			)
		}

		// Auto-discover ContractContributorAware (slice f+). Extensions that
		// publish a contract-based contributor get their handlers wired into
		// the dispatcher and their manifest registered with the contract
		// registry. Failure is logged but doesn't abort the dashboard.
		if cca, ok := ext.(ContractContributorAware); ok &&
			e.dispatcher != nil && e.contractRegistry != nil && e.wardenRegistry != nil {
			// The extension registers its own manifest, so the contributor
			// name is only knowable from inside the call. Wrapping the
			// registry is what makes it knowable; unwrapped extensions get
			// the bare registry so their path is unchanged.
			reg := e.contractRegistry
			if statusRec != nil {
				reg = &recordingRegistry{Registry: e.contractRegistry, rec: statusRec}
			}
			if err := cca.RegisterContractContributor(e.dispatcher, reg, e.wardenRegistry); err != nil {
				e.Logger().Error("failed to register contract contributor",
					forge.F("extension", ext.Name()),
					forge.F("error", err.Error()),
				)
			} else {
				e.Logger().Info("auto-discovered contract contributor",
					forge.F("extension", ext.Name()),
				)
			}
		}

		// An extension that reports a status but registered no contract
		// contributor here has nothing to attach that status to, so the
		// capabilities endpoint will report its contributors with the
		// permissive default. Silently rendering an unconfigured extension
		// as ready is the failure the four-state design exists to prevent,
		// so say so at startup.
		if statusRec != nil && statusRec.attributed() == 0 {
			e.Logger().Warn("extension reports a dashboard status but registered no contract contributor to attach it to; its plugins will render as configured",
				forge.F("extension", ext.Name()),
			)
		}
	}
}

// contributorStatusRecorder attributes contract contributors to the one
// extension that registered them. A nil recorder is a working no-op, which is
// what extensions that do not implement DashboardStatusAware get.
//
// Attribution happens inside the registration call, not around it, so there is
// no window during which another registration could be miscredited and no
// dependence on whether the extension was already in the registry when
// discovery reached it.
type contributorStatusRecorder struct {
	ext  DashboardStatusAware
	host *Extension

	// mu guards count only. It is separate from the host's
	// contributorStatusMu because an extension is free to keep the registry
	// it was handed and register more contributors later, from its own
	// goroutine.
	mu    sync.Mutex
	count int
}

func (r *contributorStatusRecorder) record(name string) {
	if r == nil || r.ext == nil || name == "" {
		return
	}
	r.host.attributeContributorStatus(name, r.ext)
	r.mu.Lock()
	r.count++
	r.mu.Unlock()
}

// attributed reports how many contributors this extension registered.
func (r *contributorStatusRecorder) attributed() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.count
}

// recordingRegistry is the contract.Registry handed to an extension that
// reports a dashboard status. Both registration methods record the manifest's
// contributor name after the underlying registry accepts it, and Unregister
// drops it again; every other method delegates untouched through the embedded
// interface.
type recordingRegistry struct {
	contract.Registry

	rec *contributorStatusRecorder
}

func (r *recordingRegistry) Register(m *contract.ContractManifest) error {
	if err := r.Registry.Register(m); err != nil {
		return err
	}
	if m != nil {
		r.rec.record(m.Contributor.Name)
	}

	return nil
}

func (r *recordingRegistry) RegisterRemote(m *contract.ContractManifest, endpoint contract.RemoteEndpoint) error {
	if err := r.Registry.RegisterRemote(m, endpoint); err != nil {
		return err
	}
	if m != nil {
		r.rec.record(m.Contributor.Name)
	}

	return nil
}

// Unregister drops the contributor's status attribution along with the
// registration itself. Contributors are keyed by name and a freed name can be
// claimed by a different party later; without this the new owner would serve
// the previous owner's live status. This is the same hazard
// UnregisterRemoteContractContributor closes, reached through the other of the
// two routes that free a name.
func (r *recordingRegistry) Unregister(contributor string) {
	r.Registry.Unregister(contributor)
	if r.rec != nil && r.rec.host != nil {
		r.rec.host.forgetContributorStatus(contributor)
	}
}

// attributeContributorStatus records that the named contract contributor is
// served by sa, so the capabilities endpoint can ask sa for its live status.
func (e *Extension) attributeContributorStatus(name string, sa DashboardStatusAware) {
	if sa == nil || name == "" {
		return
	}
	e.contributorStatusMu.Lock()
	defer e.contributorStatusMu.Unlock()
	if e.contributorStatus == nil {
		e.contributorStatus = make(map[string]DashboardStatusAware)
	}
	e.contributorStatus[name] = sa
}

// forgetContributorStatus drops a contributor's attribution. Contributors are
// keyed by name, and a name freed by an unregister can be claimed by a
// different party later; without this the new owner would serve the old
// owner's live status.
func (e *Extension) forgetContributorStatus(name string) {
	e.contributorStatusMu.Lock()
	defer e.contributorStatusMu.Unlock()
	delete(e.contributorStatus, name)
}

// contributorStatusFor is the transport.ContributorStatusFunc the capabilities
// endpoint reads. ok is false for contributors whose extension does not report
// a status — including the dashboard's own contributors and remotes — and the
// handler then applies the permissive default.
func (e *Extension) contributorStatusFor(name string) (transport.ContributorStatus, bool) {
	e.contributorStatusMu.Lock()
	sa, ok := e.contributorStatus[name]
	e.contributorStatusMu.Unlock()
	if !ok {
		return transport.ContributorStatus{}, false
	}
	st := sa.DashboardStatus()
	return transport.ContributorStatus{
		Version:    st.Version,
		Configured: st.Configured,
		Message:    st.Message,
	}, true
}

// Stop stops the dashboard extension.
func (e *Extension) Stop(ctx context.Context) error {
	e.Logger().Info("stopping dashboard extension")

	if e.discoveryInteg != nil {
		e.discoveryInteg.Stop()
	}

	// Stop data collector
	if e.collector != nil {
		e.collector.Stop()
	}

	if e.traceStore != nil {
		e.traceStore.Close()
	}

	e.MarkStopped()
	e.Logger().Info("dashboard extension stopped")

	return nil
}

// Health checks if the dashboard is healthy.
func (e *Extension) Health(ctx context.Context) error {
	if e.collector == nil {
		return ErrCollectorNotInitialized
	}

	return nil
}

// Dependencies returns extension dependencies.
func (e *Extension) Dependencies() []string {
	return []string{} // No hard dependencies
}

// Collector returns the data collector instance.
func (e *Extension) Collector() *collector.DataCollector {
	return e.collector
}

// History returns the data history instance.
func (e *Extension) History() *collector.DataHistory {
	return e.history
}

// TraceStore returns the trace store instance.
func (e *Extension) TraceStore() *collector.TraceStore {
	return e.traceStore
}

// RegisterRemoteContractContributor registers a contract contributor whose
// handlers live in another service. The dashboard fetches the upstream's
// manifest, validates it, records the endpoint, and installs a forwarding
// dispatcher (idempotently) so subsequent requests for any of the remote's
// intents are proxied to the upstream over HTTP.
//
// Slice (m) added this so a single dashboard can aggregate contributors
// from multiple microservices.
//
// baseURL is the upstream service root (e.g. https://svc.internal:8443);
// the manifest endpoint is fetched at <baseURL>/_forge/contract/manifest
// and envelopes are POSTed to <baseURL>/_forge/contract/dispatch. apiKey,
// when non-empty, is sent as Authorization: Bearer on dashboard→service
// hops; end-user identity flows in parallel via X-Forwarded-Authorization
// and X-Forwarded-Cookie.
func (e *Extension) RegisterRemoteContractContributor(ctx context.Context, baseURL, apiKey string) error {
	if e.contractRegistry == nil {
		return fmt.Errorf("dashboard: contract registry not initialised")
	}
	m, err := contractremote.FetchManifest(ctx, baseURL, apiKey, nil)
	if err != nil {
		return fmt.Errorf("dashboard: fetch remote manifest: %w", err)
	}

	return e.registerRemoteManifest(m, contract.RemoteEndpoint{BaseURL: baseURL, APIKey: apiKey})
}

// registerRemoteManifest validates an already-fetched remote manifest,
// registers it with its endpoint, and makes sure envelopes for it are
// forwarded. Both RegisterRemoteContractContributor and discovery go through
// here, so a discovered contributor is held to exactly the same checks.
func (e *Extension) registerRemoteManifest(m *contract.ContractManifest, endpoint contract.RemoteEndpoint) error {
	if e.contractRegistry == nil {
		return fmt.Errorf("dashboard: contract registry not initialised")
	}

	if err := loader.Validate(m, e.wardenRegistry); err != nil {
		return fmt.Errorf("dashboard: validate remote manifest from %s: %w", endpoint.BaseURL, err)
	}

	if err := e.contractRegistry.RegisterRemote(m, endpoint); err != nil {
		return fmt.Errorf("dashboard: register remote contributor: %w", err)
	}
	// Idempotently install the forwarding dispatcher on first remote
	// registration. The dispatcher consults it only when no local handler
	// matches, so installing it is safe even when most contributors are
	// in-process.
	e.installForwardingDispatcherOnce()
	e.Logger().Info("dashboard: remote contract contributor registered",
		forge.F("contributor", m.Contributor.Name),
		forge.F("base_url", endpoint.BaseURL),
	)
	return nil
}

// SetDiscoveryService sets the discovery service used to find remote
// contributors. Call it before the app runs, typically from a
// PhaseAfterRegister hook, and turn discovery on with WithDiscovery(true).
//
// A remote service takes part by serving its intents with contract/server
// and registering itself in discovery under DiscoveryTag. An instance's
// "forge-api-key" metadata, when set, is sent to it as a bearer token.
func (e *Extension) SetDiscoveryService(svc dashboarddiscovery.DiscoveryService) {
	e.discoverySvc = svc
}

// startDiscovery starts polling the discovery service, if one was set.
func (e *Extension) startDiscovery(ctx context.Context) {
	if e.discoverySvc == nil {
		e.Logger().Warn("dashboard discovery: no discovery service configured, skipping",
			forge.F("hint", "call SetDiscoveryService() in a PhaseAfterRegister or earlier hook"),
		)

		return
	}

	e.discoveryInteg = dashboarddiscovery.NewIntegration(e.discoverySvc, dashboarddiscovery.Config{
		Tag:          e.config.DiscoveryTag,
		PollInterval: e.config.DiscoveryPollInterval,
		Register:     e.registerRemoteManifest,
		Unregister:   e.UnregisterRemoteContractContributor,
		Logger:       e.Logger(),
	})
	e.discoveryInteg.Start(ctx)
}

// UnregisterRemoteContractContributor removes a previously registered
// remote. Safe to call for unknown names; future dispatches to the
// contributor will fall through to CodeNotFound.
func (e *Extension) UnregisterRemoteContractContributor(name string) {
	if e.contractRegistry != nil {
		e.contractRegistry.Unregister(name)
	}
	e.forgetContributorStatus(name)
}

// installForwardingDispatcherOnce wires a ForwardingDispatcher into the
// dispatcher's RemoteDispatcher slot the first time it's called. Subsequent
// calls are no-ops because the same forwarding dispatcher works for every
// remote (it reads endpoints from the registry per request).
func (e *Extension) installForwardingDispatcherOnce() {
	e.forwardingMu.Lock()
	defer e.forwardingMu.Unlock()
	if e.forwardingInstalled {
		return
	}
	if e.dispatcher == nil || e.contractRegistry == nil {
		return
	}
	e.dispatcher.SetRemoteDispatcher(contractremote.NewForwardingDispatcher(e.contractRegistry))
	e.forwardingInstalled = true
}

// CSRFManager returns the CSRF token manager. Returns nil if CSRF is disabled.
func (e *Extension) CSRFManager() *security.CSRFManager {
	return e.csrfMgr
}

// SetAuthChecker configures the authentication checker used to validate
// requests. Call this after Register() and before Start(). When auth is enabled,
// the checker is invoked on every request to populate the user context.
//
// Example using the adapter for the forge auth extension:
//
//	checker := dashauth.NewAuthExtensionChecker(authRegistry, "oidc")
//	dashExt.SetAuthChecker(checker)
func (e *Extension) SetAuthChecker(checker dashauth.AuthChecker) {
	e.authChecker = checker
}

// AuthChecker returns the configured authentication checker. Returns nil if none is set.
func (e *Extension) AuthChecker() dashauth.AuthChecker {
	return e.authChecker
}

// EnableAuth turns on authentication support. Auth extensions such as authsome
// call this from RegisterDashboardAuth. With auth on, the principal endpoint
// answers 401 for a signed-out caller instead of an anonymous 200.
func (e *Extension) EnableAuth() {
	e.config.EnableAuth = true
}

// SetRequiredRoles restricts dashboard access to authenticated users that
// hold at least one of the given roles. Pass nil/empty to clear the gate.
// Auth extensions like authsome call this from RegisterDashboardAuth when
// their own configuration declares a role list. The principal endpoint
// returns 403 PERMISSION_DENIED for users who don't qualify. Rendering that
// as an "access denied" screen is the client's job.
func (e *Extension) SetRequiredRoles(roles []string) {
	e.config.RequiredRoles = append([]string(nil), roles...)
}

// SetTenantResolver configures the tenant resolver used to populate tenant
// context on every request. Call this after Register() and before Start().
// When configured, the contract handlers can read tenant info via
// dashauth.TenantFromContext(ctx).
//
// A default ScopeTenantResolver is available that reads forge.Scope from
// the request context:
//
//	dashExt.SetTenantResolver(dashauth.ScopeTenantResolver{})
func (e *Extension) SetTenantResolver(resolver dashauth.TenantResolver) {
	e.tenantResolver = resolver
}

// TenantResolver returns the configured tenant resolver. Returns nil if none is set.
func (e *Extension) TenantResolver() dashauth.TenantResolver {
	return e.tenantResolver
}

// BasePath returns the prefix every dashboard route is mounted under.
func (e *Extension) BasePath() string {
	return e.config.BasePath
}

// registerRoutes registers the dashboard's routes with the app's router: the
// contract endpoints and, when enabled, the export endpoints.
func (e *Extension) registerRoutes() {
	router := e.app.Router()
	base := e.config.BasePath

	must := func(err error) {
		if err != nil {
			panic(fmt.Sprintf("dashboard: failed to register route: %v", err))
		}
	}

	// The principal endpoint and the contract dispatch endpoint both call
	// dashauth.UserFromContext at request time; without ForgeMiddleware on
	// those routes the user is always nil and /principal 401s even with a
	// valid auth_token cookie. The middleware is non-blocking (populates
	// context on success, falls through silently on failure), so attaching
	// it broadly is safe.
	var routeOpts []forge.RouteOption
	if e.config.EnableAuth && e.authChecker != nil {
		routeOpts = append(routeOpts, forge.WithMiddleware(dashauth.ForgeMiddleware(e.authChecker)))
	}
	if e.tenantResolver != nil {
		routeOpts = append(routeOpts, forge.WithMiddleware(dashauth.TenantMiddleware(e.tenantResolver)))
	}

	// Contract envelope endpoints, gated on the contract registry being
	// initialised. The stream + control routes only register when a
	// StreamBroker is wired (slice (c) supplies the SubscriptionSource).
	//
	// Note on EventStream: the contract StreamBroker manages its own SSE
	// framing in ServeStream (an http.HandlerFunc), so we register it via
	// router.GET rather than router.EventStream — the latter expects the
	// SSEHandler shape (func(Context, Stream) error), which the broker
	// deliberately doesn't adopt because it owns the per-event fan-out.
	if e.contractRegistry != nil {
		must(router.POST(base+"/api/dashboard/v1", e.handleContractPOST(), routeOpts...))
		must(router.GET(base+"/api/dashboard/v1/capabilities", e.handleContractCapabilities(), routeOpts...))
		if e.streamBroker != nil {
			must(router.GET(base+"/api/dashboard/v1/stream", http.HandlerFunc(e.streamBroker.ServeStream), routeOpts...))
			must(router.POST(base+"/api/dashboard/v1/stream/control", http.HandlerFunc(e.streamBroker.ServeControl), routeOpts...))
		}
		// Slice (b) Phase 6: surface CSRF tokens to the client only when the
		// security stack is wired (csrfMgr is non-nil iff EnableCSRF is true,
		// and EnableContractSecurity gates the contract path's enforcement).
		if e.csrfMgr != nil && e.config.EnableContractSecurity {
			must(router.GET(base+"/api/dashboard/v1/csrf",
				transport.NewCSRFTokenHandler(e.csrfMgr, 12*time.Hour).ServeHTTP))
		}

		// The principal endpoint surfaces auth state to whatever client is
		// driving the dashboard. Reads from dashauth.UserFromContext, so it
		// honors whatever auth middleware the deployment has wired upstream.
		// Auth-disabled deployments get a 200 anonymous response, so a client can
		// skip its login gate; auth-enabled deployments get a 401 carrying the
		// loginPath to send the user to. RequiredRoles, if set, gets a 403 for
		// authenticated users without a matching role.

		loginPath := e.config.BasePath + e.config.LoginPath
		must(router.GET(base+"/api/dashboard/v1/principal", handlers.NewPrincipalHandler(handlers.PrincipalOptions{
			AuthEnabled:   e.config.EnableAuth,
			LoginPath:     loginPath,
			RequiredRoles: append([]string(nil), e.config.RequiredRoles...),
		}), routeOpts...))
	}

	// Export endpoints
	if e.config.EnableExport {
		deps := &handlers.Deps{Collector: e.collector}
		must(router.GET(base+"/export/json", handlers.HandleExportJSON(deps)))
		must(router.GET(base+"/export/csv", handlers.HandleExportCSV(deps)))
		must(router.GET(base+"/export/prometheus", handlers.HandleExportPrometheus(deps)))
	}

	e.Logger().Debug("dashboard routes registered",
		forge.F("base_path", base),
		forge.F("export", e.config.EnableExport),
		forge.F("auth", e.config.EnableAuth),
	)
}

// handleContractPOST returns the http.HandlerFunc that serves
// POST /api/dashboard/v1 — the contract envelope endpoint. The handler
// validates the inbound envelope, looks up the intent in the contract
// registry, and dispatches via the configured Dispatcher. Slice (c) Phase 11
// replaces slice (a)'s safe NilDispatcher with the real dispatcher wired in
// NewExtension; intent handlers are bound by pilot.Register during
// Extension.Register so requests resolve to live data instead of CodeUnavailable.
//
// Slice (b) Phase 6 routes CSRF validation through the handler when the
// extension's CSRF manager is configured AND EnableContractSecurity is on.
// Passing nil to NewHandlerWithCSRF preserves the slice-(a) behaviour — useful
// during a rollout window where clients have not yet adopted CSRF tokens.
func (e *Extension) handleContractPOST() http.HandlerFunc {
	var mgr *security.CSRFManager
	if e.config.EnableContractSecurity && e.csrfMgr != nil {
		mgr = e.csrfMgr
	}

	h := transport.NewHandlerWithCSRF(e.contractRegistry, e.wardenRegistry, e.dispatcher, e.auditEmitter, mgr,
		transport.WithMaxBodyBytes(e.config.ContractMaxBodyBytes))
	return h.ServeHTTP
}

// handleContractCapabilities returns the http.HandlerFunc that serves
// GET /api/dashboard/v1/capabilities — the discovery endpoint that advertises
// which envelope versions the shell supports and which contributors are
// currently registered with contract manifests. The shell envelope list here
// must stay in sync with transport.NewHandler's supported set.
func (e *Extension) handleContractCapabilities() http.HandlerFunc {
	return transport.NewCapabilitiesHandler(e.contractRegistry, []string{"v1"}, e.contributorStatusFor).ServeHTTP
}

// idempotencyAdapter bridges idempotency.Store (the production interface) to
// dispatcher.IdempotencyStore (the dispatcher-private surface). The two types
// are intentionally separate: the dispatcher defines its own minimal
// IdempotencyStore + IdempotencyCached pair to avoid an import cycle with
// the contract/idempotency sub-package, which itself imports nothing from
// dispatcher. The conversion is lossless.
type idempotencyAdapter struct{ inner idempotency.Store }

// adaptIdempotencyStore returns a dispatcher.IdempotencyStore backed by an
// idempotency.Store. Used at NewExtension/Register time to wire the in-memory
// store into the dispatcher. When s is also an idempotency.Claimer, the
// result is a dispatcher.IdempotencyClaimer, so the dispatcher holds a
// command's key while its handler runs.
func adaptIdempotencyStore(s idempotency.Store) dispatcher.IdempotencyStore {
	a := &idempotencyAdapter{inner: s}

	if c, ok := s.(idempotency.Claimer); ok {
		return &claimingIdempotencyAdapter{idempotencyAdapter: a, claimer: c}
	}

	return a
}

// claimingIdempotencyAdapter is idempotencyAdapter over a store that can also
// claim. It is a separate type so the dispatcher's type assertion finds a
// claimer only when the store underneath is one.
type claimingIdempotencyAdapter struct {
	*idempotencyAdapter

	claimer idempotency.Claimer
}

// Claim forwards to the underlying claimer, converting the entry and the End
// function between the two packages' types, and idempotency.ErrClaimHeld to
// dispatcher.ErrIdempotencyClaimHeld.
func (a *claimingIdempotencyAdapter) Claim(ctx context.Context, key, identity string) (dispatcher.IdempotencyClaim, error) {
	claim, err := a.claimer.Claim(ctx, key, identity)
	if errors.Is(err, idempotency.ErrClaimHeld) {
		return dispatcher.IdempotencyClaim{}, fmt.Errorf("%w: %w", dispatcher.ErrIdempotencyClaimHeld, err)
	}

	if err != nil {
		return dispatcher.IdempotencyClaim{}, err
	}

	var out dispatcher.IdempotencyClaim

	if claim.Cached != nil {
		c := claim.Cached
		out.Cached = &dispatcher.IdempotencyCached{Status: c.Status, WireBody: c.WireBody, StoredAt: c.StoredAt, TTL: c.TTL}
	}

	if end := claim.End; end != nil {
		out.End = func(ctx context.Context, c *dispatcher.IdempotencyCached) error {
			if c == nil {
				return end(ctx, nil)
			}

			return end(ctx, &idempotency.Cached{Status: c.Status, WireBody: c.WireBody, StoredAt: c.StoredAt, TTL: c.TTL})
		}
	}

	return out, nil
}

// Lookup forwards to the underlying store, converting the cached envelope
// shape between the two types.
func (a *idempotencyAdapter) Lookup(ctx context.Context, key, identity string) (*dispatcher.IdempotencyCached, bool) {
	c, ok := a.inner.Lookup(ctx, key, identity)
	if !ok {
		return nil, false
	}
	return &dispatcher.IdempotencyCached{
		Status:   c.Status,
		WireBody: c.WireBody,
		StoredAt: c.StoredAt,
		TTL:      c.TTL,
	}, true
}

// Store forwards to the underlying store, converting the cached envelope
// shape between the two types.
func (a *idempotencyAdapter) Store(ctx context.Context, key, identity string, c dispatcher.IdempotencyCached) error {
	return a.inner.Store(ctx, key, identity, idempotency.Cached{
		Status:   c.Status,
		WireBody: c.WireBody,
		StoredAt: c.StoredAt,
		TTL:      c.TTL,
	})
}

// appExtensions serves the extensions.list intent from the extensions
// registered with the app.
type appExtensions struct{ app forge.App }

// ListExtensions implements pilot.ExtensionsProvider.
func (a appExtensions) ListExtensions() []pilot.ExtensionInfo {
	exts := a.app.Extensions()
	out := make([]pilot.ExtensionInfo, 0, len(exts))

	for _, ext := range exts {
		out = append(out, pilot.ExtensionInfo{
			Name:        ext.Name(),
			Version:     ext.Version(),
			Description: ext.Description(),
		})
	}

	return out
}
