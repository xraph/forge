package discovery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/xraph/farp"
	farpdiscovery "github.com/xraph/farp/discovery"
	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/extensions/discovery/backends"
	"github.com/xraph/vessel"
)

// Extension implements forge.Extension for service discovery.
// The extension manages route registration and self-registration,
// while Service lifecycle is managed by Vessel.
type Extension struct {
	*forge.BaseExtension

	config           Config
	appConfig        forge.AppConfig // Store app config for accessing HTTPAddress
	stopCh           chan struct{}
	wg               sync.WaitGroup
	mu               sync.RWMutex
	serviceNode      *farpdiscovery.ServiceNode // FARP service node for schema publishing
	serviceInstance  *ServiceInstance           // Store the registered service instance
	discoveryService *Service                   // Resolved discovery service (stored for deferred FARP init)
}

// NewExtension creates a new service discovery extension.
func NewExtension(opts ...ConfigOption) forge.Extension {
	config := DefaultConfig()
	for _, opt := range opts {
		opt(&config)
	}

	base := forge.NewBaseExtension("discovery", "1.0.0", "Service Discovery & Registry")

	return &Extension{
		BaseExtension: base,
		config:        config,
		stopCh:        make(chan struct{}),
	}
}

// NewExtensionWithConfig creates a new extension with complete config.
func NewExtensionWithConfig(config Config) forge.Extension {
	return NewExtension(WithConfig(config))
}

// Register registers the extension with the app.
func (e *Extension) Register(app forge.App) error {
	if err := e.BaseExtension.Register(app); err != nil {
		return err
	}

	// Try to extract app config from metadata (if provided via WithAppConfig)
	if e.config.Service.Metadata != nil {
		if name, ok := e.config.Service.Metadata["_app_config_name"]; ok {
			e.appConfig.Name = name
		}

		if version, ok := e.config.Service.Metadata["_app_config_version"]; ok {
			e.appConfig.Version = version
		}

		if httpAddr, ok := e.config.Service.Metadata["_app_config_http_address"]; ok {
			e.appConfig.HTTPAddress = httpAddr
		}
	}

	// If still empty, try to get from app directly
	if e.appConfig.HTTPAddress == "" {
		// Try to access from underlying implementation
		if appImpl, ok := app.(interface{ GetConfig() forge.AppConfig }); ok {
			e.appConfig = appImpl.GetConfig()
		}
	}

	if !e.config.Enabled {
		e.Logger().Info("discovery extension disabled")
		return nil
	}

	cfg := e.config

	// Create backend based on configuration
	backend, err := e.createBackend()
	if err != nil {
		return fmt.Errorf("failed to create service discovery backend: %w", err)
	}

	// Register Service constructor with Vessel using vessel.WithAliases for backward compatibility
	if err := e.RegisterConstructor(func(logger forge.Logger) (*Service, error) {
		return NewService(cfg, backend, logger), nil
	}, vessel.WithAliases(ServiceKey, ServiceKeyLegacy)); err != nil {
		return fmt.Errorf("failed to register discovery service: %w", err)
	}

	// Register FARP HTTP endpoints eagerly so they are available
	// immediately (returning 503 until the ServiceNode is initialized
	// in PhaseAfterStart). This ensures endpoints exist even if
	// Start() hasn't been called yet.
	if cfg.FARP.Enabled {
		e.registerFARPEndpoints()

		e.Logger().Info("FARP enabled, ServiceNode will be started on Start()",
			forge.F("auto_register", cfg.FARP.AutoRegister),
			forge.F("strategy", cfg.FARP.Strategy),
			forge.F("exclude_internal_paths", cfg.FARP.ShouldExcludeInternalPaths()),
			forge.F("collapse_service_tags", cfg.FARP.CollapseServiceTags),
		)
	}

	e.Logger().Info("discovery extension registered",
		forge.F("backend", cfg.Backend),
		forge.F("service", cfg.Service.Name),
		forge.F("farp_enabled", cfg.FARP.Enabled),
	)

	return nil
}

// Start starts the extension and handles self-registration.
func (e *Extension) Start(ctx context.Context) error {
	if !e.config.Enabled {
		return nil
	}

	// Resolve discovery service from DI
	discoveryService, err := forge.InjectType[*Service](e.App().Container())
	if err != nil {
		return fmt.Errorf("failed to resolve discovery service: %w", err)
	}

	instance := e.createServiceInstance()

	// Store for later use (deferred FARP init, Run(), Stop())
	e.mu.Lock()
	e.serviceInstance = instance
	e.discoveryService = discoveryService
	e.mu.Unlock()

	if e.config.FARP.Enabled {
		// Defer FARP ServiceNode initialization to PhaseAfterStart.
		// At Start() time, other extensions may not have registered their routes yet
		// and observability endpoints (health, metrics) aren't set up. Deferring
		// ensures the OpenAPI/AsyncAPI schemas capture ALL business routes, not just
		// the framework's introspection endpoints.
		if err := e.App().RegisterHookFn(forge.PhaseAfterStart, "discovery-farp-init", func(ctx context.Context, _ forge.App) error {
			return e.initFARPServiceNode(ctx)
		}); err != nil {
			return fmt.Errorf("failed to register FARP init hook: %w", err)
		}

		// Register directly with the backend for now (non-FARP discovery).
		// The FARP ServiceNode will re-register with full schema info in PhaseAfterStart.
		if err := discoveryService.Backend().Register(ctx, instance); err != nil {
			return fmt.Errorf("failed to register service: %w", err)
		}
	} else {
		// No FARP — register directly with the backend
		if err := discoveryService.Backend().Register(ctx, instance); err != nil {
			return fmt.Errorf("failed to register service: %w", err)
		}
	}

	e.Logger().Info("service registered",
		forge.F("service_id", instance.ID),
		forge.F("service_name", instance.Name),
		forge.F("address", instance.Address),
		forge.F("port", instance.Port),
		forge.F("farp", e.config.FARP.Enabled),
	)

	// Start watching for service changes (if enabled)
	if e.config.Watch.Enabled && len(e.config.Watch.Services) > 0 {
		e.wg.Add(1)

		go e.watchServices(discoveryService)
	}

	// Start health check updater (if enabled and not using FARP ServiceNode,
	// which handles its own health loop).
	// For FARP mode, this is started in initFARPServiceNode after the node is ready.
	if e.config.HealthCheck.Enabled && !e.config.FARP.Enabled {
		e.wg.Add(1)

		go e.healthCheckLoop(discoveryService.Backend())
	}

	e.Logger().Info("discovery extension started")

	return nil
}

// initFARPServiceNode initializes the FARP ServiceNode.
// Called from PhaseAfterStart hook, when all extensions are started and all routes
// (including business routes and observability endpoints) are registered.
func (e *Extension) initFARPServiceNode(ctx context.Context) error {
	e.mu.RLock()
	discoveryService := e.discoveryService
	instance := e.serviceInstance
	e.mu.RUnlock()

	if discoveryService == nil || instance == nil {
		return fmt.Errorf("discovery service or instance not initialized")
	}

	if err := e.startServiceNode(ctx, discoveryService, instance); err != nil {
		e.Logger().Warn("failed to start FARP ServiceNode, falling back to direct registration",
			forge.F("error", err),
		)
		// Already registered with backend in Start(), so just log the FARP failure.
		// Health check loop fallback for non-FARP mode:
		if e.config.HealthCheck.Enabled {
			e.wg.Add(1)
			go e.healthCheckLoop(discoveryService.Backend())
		}
		return nil
	}

	e.Logger().Info("FARP ServiceNode initialized",
		forge.F("service_name", instance.Name),
		forge.F("routes", len(e.App().Router().Routes())),
		forge.F("exclude_internal_paths", e.config.FARP.ShouldExcludeInternalPaths()),
		forge.F("collapse_service_tags", e.config.FARP.CollapseServiceTags),
	)

	return nil
}

// startServiceNode creates and starts a FARP ServiceNode for this service.
// The ServiceNode handles: registration, schema generation, manifest building,
// HTTP endpoints (/_farp/manifest, /_farp/health, /_farp/schemas/*), health loop,
// and push-to-gateway (via GatewayURL config).
func (e *Extension) startServiceNode(ctx context.Context, discoveryService *Service, instance *ServiceInstance) error {
	// Build the forge app adapter for schema providers
	app := newForgeApp(
		instance.Name,
		instance.Version,
		e.App().Router(),
	)

	// Build schema providers
	providers := buildServiceNodeProviders(app, e.config.FARP)

	// Convert forge routes to FARP route descriptors for the route table
	pathRules := farp.BuildPathRules(e.config.FARP.ShouldExcludeInternalPaths(), e.config.FARP.PathRules)
	routeDescriptors := forgeRoutesToRouteDescriptors(e.App().Router().Routes(), pathRules)

	// Determine address for ServiceNode
	addr := fmt.Sprintf("%s:%d", instance.Address, instance.Port)

	// Build ServiceNode config
	nodeConfig := farpdiscovery.ServiceNodeConfig{
		ServiceName:    instance.Name,
		ServiceVersion: instance.Version,
		InstanceID:     instance.ID,
		Address:        addr,
		Tags:           instance.Tags,
		Providers:      providers,
		Routes:         routeDescriptors,
		MountStrategy:  farp.MountStrategyService,
		BasePath:       "",
		PathRules:      e.config.FARP.PathRules,
		Metadata:       instance.Metadata, // pass through forge metadata (tags, custom keys, etc.)
		Endpoints: farp.SchemaEndpoints{
			Health:         e.config.FARP.Endpoints.Health,
			OpenAPI:        e.config.FARP.Endpoints.OpenAPI,
			AsyncAPI:       e.config.FARP.Endpoints.AsyncAPI,
			GraphQL:        e.config.FARP.Endpoints.GraphQL,
			GRPCReflection: e.config.FARP.Endpoints.GRPCReflection,
		},
	}

	// Always use the forge backend for ServiceNode's registration.
	// This ensures the instance is registered in the local discovery backend
	// (memory, mdns, etc.) and schemas/manifests are properly generated.
	//
	// Gateway push is handled separately after ServiceNode.Start() below,
	// using the FARP v1 push protocol (POST /_farp/v1/register).
	nodeConfig.Discovery = newBackendToDiscovery(discoveryService.Backend())

	node, err := farpdiscovery.NewServiceNode(nodeConfig)
	if err != nil {
		return fmt.Errorf("failed to create FARP ServiceNode: %w", err)
	}

	if err := node.Start(ctx); err != nil {
		return fmt.Errorf("failed to start FARP ServiceNode: %w", err)
	}

	e.serviceNode = node

	// FARP HTTP endpoints were already registered in Start() via
	// registerFARPEndpoints(). They dynamically delegate to e.serviceNode,
	// which is now set — no additional mounting needed.

	// Gateway push has been moved to Run() (PhaseAfterRun)
	// so that the HTTP server is fully listening before Octopus tries
	// to fetch the manifest.

	return nil
}

// registerFARPEndpoints registers the FARP HTTP endpoints on the forge router.
// The endpoints delegate to the ServiceNode's handler once it is initialized
// (after PhaseAfterStart), returning 503 until then for endpoints that
// require the node.
func (e *Extension) registerFARPEndpoints() {
	router := e.App().Router()
	if router == nil {
		e.Logger().Warn("no router available, FARP HTTP endpoints not registered")
		return
	}

	// Helper that forwards to the ServiceNode's HTTP handler if available.
	serveNode := func(ctx forge.Context) error {
		e.mu.RLock()
		node := e.serviceNode
		e.mu.RUnlock()

		if node == nil {
			return ctx.JSON(503, map[string]any{
				"error":        "service not registered",
				"farp_enabled": e.config.FARP.Enabled,
				"backend":      e.config.Backend,
			})
		}

		node.HTTPHandler().ServeHTTP(ctx.Response(), ctx.Request())
		return nil
	}

	router.GET("/_farp/manifest", serveNode)
	router.GET("/_farp/health", serveNode)
	router.GET("/_farp/schemas/:type", serveNode)

	// Discovery info endpoint — works immediately (reads serviceInstance).
	router.GET("/_farp/discovery", func(ctx forge.Context) error {
		e.mu.RLock()
		inst := e.serviceInstance
		e.mu.RUnlock()

		if inst == nil {
			return ctx.JSON(503, map[string]any{
				"error":        "service not registered",
				"farp_enabled": e.config.FARP.Enabled,
				"backend":      e.config.Backend,
			})
		}

		return ctx.JSON(200, map[string]any{
			"service_id":      inst.ID,
			"service_name":    inst.Name,
			"service_version": inst.Version,
			"address":         inst.Address,
			"port":            inst.Port,
			"tags":            inst.Tags,
			"metadata":        inst.Metadata,
			"farp_enabled":    e.config.FARP.Enabled,
			"backend":         e.config.Backend,
		})
	})

	e.Logger().Info("FARP HTTP endpoints registered",
		forge.F("manifest", "/_farp/manifest"),
		forge.F("health", "/_farp/health"),
		forge.F("schemas", "/_farp/schemas/:type"),
		forge.F("discovery", "/_farp/discovery"),
	)
}

// Stop stops the extension gracefully.
func (e *Extension) Stop(ctx context.Context) error {
	if !e.config.Enabled {
		return nil
	}

	// Signal stop
	close(e.stopCh)

	// Stop FARP ServiceNode (handles backend deregistration)
	if e.serviceNode != nil {
		if err := e.serviceNode.Stop(ctx); err != nil {
			e.Logger().Warn("failed to stop FARP ServiceNode",
				forge.F("error", err),
			)
		} else {
			e.Logger().Info("FARP ServiceNode stopped")
		}

		// Also deregister from gateway if push was configured
		if e.config.FARP.GatewayURL != "" {
			instance := e.createServiceInstance()
			if err := e.deregisterFromGateway(ctx, instance.Name); err != nil {
				e.Logger().Warn("failed to deregister from gateway",
					forge.F("error", err),
				)
			}
		}
	} else if e.config.Service.EnableAutoDeregister {
		// No ServiceNode — deregister manually
		discoveryService, err := forge.InjectType[*Service](e.App().Container())
		if err == nil {
			instance := e.createServiceInstance()
			if err := discoveryService.Backend().Deregister(ctx, instance.ID); err != nil {
				e.Logger().Warn("failed to deregister service",
					forge.F("service_id", instance.ID),
					forge.F("error", err),
				)
			} else {
				e.Logger().Info("service deregistered", forge.F("service_id", instance.ID))
			}
		}
	}

	// Wait for goroutines with timeout
	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		e.Logger().Info("discovery extension stopped gracefully")
	case <-ctx.Done():
		e.Logger().Warn("discovery extension stop timed out")
	}

	e.MarkStopped()
	return nil
}

// Run executes background tasks after the app server is running.
// Implements forge.RunnableExtension.
func (e *Extension) Run(ctx context.Context) error {
	if !e.config.Enabled {
		return nil
	}

	e.mu.RLock()
	instance := e.serviceInstance
	node := e.serviceNode
	e.mu.RUnlock()

	// Push registration to gateway if configured.
	// We do this in Run() instead of Start() because Run() executes in PhaseAfterRun,
	// meaning the HTTP server is now listening and can serve the /_farp/manifest
	// request that the gateway will immediately make upon receiving the push.
	// Retries continuously with backoff until success or shutdown.
	if e.config.FARP.Enabled && e.config.FARP.GatewayURL != "" && instance != nil && node != nil {
		enriched := e.enrichInstanceFromManifest(instance, node)

		e.wg.Add(1)
		go e.pushToGatewayWithRetry(ctx, enriched)
	}

	return nil
}

// pushToGatewayWithRetry continuously attempts to push registration to the gateway
// with exponential backoff until it succeeds or the service shuts down.
func (e *Extension) pushToGatewayWithRetry(ctx context.Context, instance *ServiceInstance) {
	defer e.wg.Done()

	const (
		initialDelay = 2 * time.Second
		maxDelay     = 60 * time.Second
	)

	delay := initialDelay
	attempt := 0

	for {
		if err := e.pushToGateway(ctx, instance); err != nil {
			attempt++
			e.Logger().Warn("failed to push registration to gateway, will retry",
				forge.F("gateway_url", e.config.FARP.GatewayURL),
				forge.F("error", err),
				forge.F("attempt", attempt),
				forge.F("retry_in", delay.String()),
			)

			select {
			case <-time.After(delay):
				// Double the delay, capped at maxDelay
				delay = delay * 2
				if delay > maxDelay {
					delay = maxDelay
				}
			case <-e.stopCh:
				return
			case <-ctx.Done():
				return
			}
		} else {
			e.Logger().Info("service registered with gateway",
				forge.F("gateway_url", e.config.FARP.GatewayURL),
				forge.F("service_name", instance.Name),
			)
			return
		}
	}
}

// Shutdown gracefully stops background tasks before the app shuts down.
// Implements forge.RunnableExtension.
func (e *Extension) Shutdown(ctx context.Context) error {
	// Let Stop() handle the actual cleanup to ensure proper ordering
	return nil
}

// Health checks the extension health.
// Service health is managed by Vessel through Service.Health().
func (e *Extension) Health(ctx context.Context) error {
	if !e.config.Enabled {
		return nil
	}

	// Health is now managed by Vessel through Service.Health()
	return nil
}

// Dependencies returns extension dependencies.
func (e *Extension) Dependencies() []string {
	return []string{} // No dependencies
}

// createBackend creates the appropriate backend based on configuration.
func (e *Extension) createBackend() (Backend, error) {
	switch e.config.Backend {
	case "memory":
		return backends.NewMemoryBackend()
	case "consul":
		return backends.NewConsulBackend(e.config.Consul)
	case "etcd":
		return backends.NewEtcdBackend(e.config.Etcd)
	case "mdns":
		// mDNS backend for native OS-level service discovery
		// Works on macOS (Bonjour), Linux (Avahi), Windows (DNS-SD)
		logger := e.Logger()
		return backends.NewMDNSBackend(backends.MDNSConfig{
			Domain:        e.config.MDNS.Domain,
			ServiceType:   e.config.MDNS.ServiceType,
			ServiceTypes:  e.config.MDNS.ServiceTypes,
			WatchInterval: e.config.MDNS.WatchInterval,
			Interface:     e.config.MDNS.Interface,
			IPv6:          e.config.MDNS.IPv6,
			BrowseTimeout: e.config.MDNS.BrowseTimeout,
			TTL:           e.config.MDNS.TTL,
			Logger: func(format string, args ...any) {
				logger.Debug(fmt.Sprintf(format, args...))
			},
		})
	case "http":
		return backends.NewHTTPBackend(e.config.HTTP)
	case "kubernetes":
		// TODO: Implement Kubernetes backend
		return nil, errors.New("kubernetes backend not yet implemented")
	case "eureka":
		// TODO: Implement Eureka backend
		return nil, errors.New("eureka backend not yet implemented")
	default:
		return nil, fmt.Errorf("unknown service discovery backend: %s", e.config.Backend)
	}
}

// createServiceInstance creates a service instance from config.
func (e *Extension) createServiceInstance() *ServiceInstance {
	// Use configured values or fallback to app values
	serviceName := e.config.Service.Name
	if serviceName == "" && e.App() != nil {
		serviceName = e.App().Name()
	}

	serviceVersion := e.config.Service.Version
	if serviceVersion == "" && e.App() != nil {
		serviceVersion = e.App().Version()
	}

	// Get address and port from config or extract from app's HTTPAddress
	serviceAddress := e.config.Service.Address
	servicePort := e.config.Service.Port

	// Try to get HTTPAddress from multiple sources
	if serviceAddress == "" || servicePort == 0 {
		httpAddr := e.getHTTPAddress()
		if httpAddr != "" {
			addr, port := parseHTTPAddress(httpAddr)
			if serviceAddress == "" {
				serviceAddress = addr
			}

			if servicePort == 0 {
				servicePort = port
			}
		}
	}

	// Default address if still empty
	if serviceAddress == "" {
		serviceAddress = "localhost"
	}

	// Generate ID
	id := e.config.Service.ID
	if id == "" {
		id = fmt.Sprintf("%s-%d", serviceName, time.Now().UnixNano())
	}

	// Start with configured metadata
	metadata := make(map[string]string)

	if e.config.Service.Metadata != nil {
		maps.Copy(metadata, e.config.Service.Metadata)
	}

	// Add FARP metadata for discovery backends (mDNS TXT records, etc.)
	if e.config.FARP.Enabled {
		baseURL := ""
		if serviceAddress != "" && servicePort > 0 {
			scheme := "http"
			if e.config.Service.Metadata != nil && e.config.Service.Metadata["scheme"] != "" {
				scheme = e.config.Service.Metadata["scheme"]
			}
			baseURL = fmt.Sprintf("%s://%s:%d", scheme, serviceAddress, servicePort)
		}

		metadata["farp.enabled"] = "true"
		if baseURL != "" {
			metadata["farp.manifest"] = baseURL + "/_farp/manifest"
		}
		healthPath := e.config.FARP.Endpoints.Health
		if healthPath == "" {
			healthPath = "/_/health"
		}
		metadata["farp.health"] = healthPath

		// Advertise configured schema endpoints so discovery metadata
		// is available before the ServiceNode enriches it post-start.
		if e.config.FARP.Endpoints.OpenAPI != "" {
			if baseURL != "" {
				metadata["farp.openapi"] = baseURL + e.config.FARP.Endpoints.OpenAPI
			}
			metadata["farp.openapi.path"] = e.config.FARP.Endpoints.OpenAPI
		}
		if e.config.FARP.Endpoints.AsyncAPI != "" {
			if baseURL != "" {
				metadata["farp.asyncapi"] = baseURL + e.config.FARP.Endpoints.AsyncAPI
			}
			metadata["farp.asyncapi.path"] = e.config.FARP.Endpoints.AsyncAPI
		}
		if e.config.FARP.Endpoints.GraphQL != "" {
			if baseURL != "" {
				metadata["farp.graphql"] = baseURL + e.config.FARP.Endpoints.GraphQL
			}
			metadata["farp.graphql.path"] = e.config.FARP.Endpoints.GraphQL
		}
		if e.config.FARP.Endpoints.GRPCReflection {
			metadata["farp.grpc.reflection"] = "true"
		}

		if e.config.FARP.CollapseServiceTags {
			metadata["farp.collapse_service_tags"] = "true"
		}
		if !e.config.FARP.ShouldExcludeInternalPaths() {
			metadata["farp.include_internal_paths"] = "true"
		}
	}

	return &ServiceInstance{
		ID:       id,
		Name:     serviceName,
		Version:  serviceVersion,
		Address:  serviceAddress,
		Port:     servicePort,
		Tags:     e.config.Service.Tags,
		Metadata: metadata,
		Status:   HealthStatusPassing,
	}
}

// getHTTPAddress tries to get the HTTPAddress from multiple sources.
func (e *Extension) getHTTPAddress() string {
	// 1. Try from stored appConfig
	if e.appConfig.HTTPAddress != "" {
		return e.appConfig.HTTPAddress
	}

	// 2. Try from metadata (if set via WithAppConfig)
	if e.config.Service.Metadata != nil {
		if httpAddr, ok := e.config.Service.Metadata["_app_config_http_address"]; ok && httpAddr != "" {
			return httpAddr
		}
	}

	// 3. Try to access from app implementation directly
	if e.App() != nil {
		// Try type assertion to access internal config
		if appImpl, ok := e.App().(interface{ GetHTTPAddress() string }); ok {
			return appImpl.GetHTTPAddress()
		}
	}

	return ""
}

// parseHTTPAddress parses an HTTPAddress string and returns address and port
// Handles formats like ":4400", "localhost:4400", "0.0.0.0:8080".
func parseHTTPAddress(httpAddr string) (address string, port int) {
	// Default values
	address = "localhost"
	port = 8080

	if httpAddr == "" {
		return address, port
	}

	// Split by ":"
	parts := strings.Split(httpAddr, ":")
	if len(parts) == 2 {
		// Has both address and port
		if parts[0] != "" {
			address = parts[0]
		}
		// Parse port
		if p, err := fmt.Sscanf(parts[1], "%d", &port); err != nil || p != 1 {
			port = 8080 // fallback
		}
	} else if len(parts) == 1 {
		// Just port (e.g., "4400")
		if p, err := fmt.Sscanf(parts[0], "%d", &port); err != nil || p != 1 {
			// Not a port, treat as address
			address = parts[0]
			port = 8080
		}
	}

	// Handle special cases
	if address == "" || address == "0.0.0.0" {
		address = "localhost"
	}

	return address, port
}

// watchServices watches for service changes.
func (e *Extension) watchServices(discoveryService *Service) {
	defer e.wg.Done()

	backend := discoveryService.Backend()

	for _, serviceName := range e.config.Watch.Services {
		err := backend.Watch(context.Background(), serviceName, func(instances []*ServiceInstance) {
			e.Logger().Info("service instances changed",
				forge.F("service", serviceName),
				forge.F("count", len(instances)),
			)

			if e.config.Watch.OnChange != nil {
				e.config.Watch.OnChange(instances)
			}
		})
		if err != nil {
			e.Logger().Warn("failed to watch service",
				forge.F("service", serviceName),
				forge.F("error", err),
			)
		}
	}

	<-e.stopCh
}

// healthCheckLoop periodically updates health status.
func (e *Extension) healthCheckLoop(backend Backend) {
	defer e.wg.Done()

	ticker := time.NewTicker(e.config.HealthCheck.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Check service health using app health checks
			ctx, cancel := context.WithTimeout(context.Background(), e.config.HealthCheck.Timeout)
			report := e.App().HealthManager().Check(ctx)

			cancel()

			status := HealthStatusPassing
			if report.Overall != "healthy" {
				status = HealthStatusCritical

				e.Logger().Debug("service health check: not healthy", forge.F("overall", report.Overall))
			}

			// Update service status (implementation depends on backend)
			instance := e.createServiceInstance()
			instance.Status = status
			instance.LastHeartbeat = time.Now().Unix()

			// Re-register to update status
			regCtx, regCancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := backend.Register(regCtx, instance); err != nil {
				e.Logger().Warn("failed to update service health",
					forge.F("error", err),
				)
			}

			regCancel()

		case <-e.stopCh:
			return
		}
	}
}

// Service returns the service discovery service.
func (e *Extension) Service() *Service {
	discoveryService, _ := forge.InjectType[*Service](e.App().Container())
	return discoveryService
}

// ServiceNode returns the FARP ServiceNode if active, or nil.
func (e *Extension) ServiceNode() *farpdiscovery.ServiceNode {
	return e.serviceNode
}

// enrichInstanceFromManifest copies the instance and merges farp.* metadata
// keys derived from the ServiceNode's manifest. This ensures the pushed
// registration carries the same metadata that buildInstance() would produce.
func (e *Extension) enrichInstanceFromManifest(inst *ServiceInstance, node *farpdiscovery.ServiceNode) *ServiceInstance {
	manifest := node.Manifest()
	if manifest == nil {
		return inst
	}

	// Copy the instance to avoid mutating the original
	enriched := *inst
	metadata := make(map[string]string)
	for k, v := range inst.Metadata {
		metadata[k] = v
	}

	baseURL := fmt.Sprintf("http://%s:%d", inst.Address, inst.Port)

	metadata["farp.enabled"] = "true"
	metadata["farp.manifest"] = baseURL + "/_farp/manifest"

	eps := manifest.Endpoints
	if eps.Health != "" {
		metadata["farp.health"] = eps.Health
		metadata["farp.health.url"] = baseURL + eps.Health
	}
	if eps.OpenAPI != "" {
		metadata["farp.openapi"] = baseURL + eps.OpenAPI
		metadata["farp.openapi.path"] = eps.OpenAPI
	}
	if eps.AsyncAPI != "" {
		metadata["farp.asyncapi"] = baseURL + eps.AsyncAPI
		metadata["farp.asyncapi.path"] = eps.AsyncAPI
	}
	if eps.GraphQL != "" {
		metadata["farp.graphql"] = baseURL + eps.GraphQL
		metadata["farp.graphql.path"] = eps.GraphQL
	}
	if eps.GRPCReflection {
		metadata["farp.grpc.reflection"] = "true"
	}

	if len(manifest.Capabilities) > 0 {
		metadata["farp.capabilities"] = strings.Join(manifest.Capabilities, ",")
	}

	enriched.Metadata = metadata
	return &enriched
}

// pushToGateway POSTs the service registration to the configured gateway URL
// using the FARP v1 push protocol (spec section 17.4):
//
//	POST /_farp/v1/register  — {instance, manifest?}
//
// The gateway fetches the full manifest from the service's /_farp/manifest
// endpoint if not included in the payload.
func (e *Extension) pushToGateway(ctx context.Context, instance *ServiceInstance) error {
	gatewayURL := strings.TrimRight(e.config.FARP.GatewayURL, "/") + "/_farp/v1/register"

	// Build FARP spec push payload: {instance: ServiceInstance}
	// The gateway will fetch the manifest from /_farp/manifest on the service.
	farpInstance := map[string]any{
		"id":              instance.ID,
		"service_name":    instance.Name,
		"service_version": instance.Version,
		"address":         instance.Address,
		"port":            instance.Port,
		"tags":            instance.Tags,
		"metadata":        instance.Metadata,
	}

	payload := map[string]any{
		"instance": farpInstance,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal registration payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, gatewayURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create registration request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("push registration to gateway: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("gateway returned status %d", resp.StatusCode)
	}

	return nil
}

// deregisterFromGateway sends a DELETE to the gateway to remove this service.
// Uses the FARP v1 push protocol (spec section 17.4):
//
//	DELETE /_farp/v1/deregister/{id}
func (e *Extension) deregisterFromGateway(ctx context.Context, serviceName string) error {
	gatewayURL := strings.TrimRight(e.config.FARP.GatewayURL, "/") + "/_farp/v1/deregister/" + serviceName

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, gatewayURL, nil)
	if err != nil {
		return fmt.Errorf("create deregistration request: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("push deregistration to gateway: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("gateway returned status %d", resp.StatusCode)
	}

	return nil
}
