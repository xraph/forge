package gateway

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/xraph/forge"
)

// Extension implements the gateway extension for Forge.
type Extension struct {
	*forge.BaseExtension

	config Config
	app    forge.App

	// Core components
	routeManager *RouteManager
	healthMon    *HealthMonitor
	cbManager    *CircuitBreakerManager
	rateLimiter  *RateLimiter
	proxyEngine  *ProxyEngine
	stats        *StatsCollector
	hooks        *HookEngine
	accessLog    *AccessLogger
	gwMetrics    *GatewayMetrics
	retryExec    *RetryExecutor
	trafficSplit *TrafficSplitter
	disc         *ServiceDiscovery

	// New components
	gwAuth       *GatewayAuth
	respCache    *ResponseCache
	tlsManager   *TLSManager
	openAPI      *OpenAPIAggregator

	// WebSocket hub for dashboard real-time updates
	hub *Hub

	// ForgeUI integration
	forgeUI *ForgeUIIntegration

	// Lifecycle
	draining         atomic.Bool
	routesRegistered bool
}

// NewExtension creates a new gateway extension.
func NewExtension(opts ...ConfigOption) forge.Extension {
	config := DefaultConfig()
	for _, opt := range opts {
		opt(&config)
	}

	base := forge.NewBaseExtension(
		"gateway",
		"1.0.0",
		"Production-grade API gateway with FARP auto-discovery, multi-protocol proxying, and admin dashboard",
	)
	base.SetDependencies([]string{"discovery"})

	return &Extension{
		BaseExtension: base,
		config:        config,
	}
}

// Register registers the gateway extension.
func (e *Extension) Register(app forge.App) error {
	if err := e.BaseExtension.Register(app); err != nil {
		return err
	}

	e.app = app

	// Load config from ConfigManager
	programmaticConfig := e.config
	finalConfig := DefaultConfig()

	if err := e.LoadConfig("gateway", &finalConfig, programmaticConfig, DefaultConfig(), false); err != nil {
		e.Logger().Warn("gateway: using programmatic config",
			forge.F("error", err.Error()),
		)
	}

	e.config = finalConfig

	if !e.config.Enabled {
		e.Logger().Info("gateway extension disabled")

		return nil
	}

	// Initialize core components
	e.routeManager = NewRouteManager()
	e.stats = NewStatsCollector()
	e.hooks = NewHookEngine()
	e.retryExec = NewRetryExecutor(e.config.Retry)
	e.trafficSplit = NewTrafficSplitter(e.config.TrafficSplit.Enabled)
	e.accessLog = NewAccessLogger(e.config.AccessLog, e.Logger())
	e.cbManager = NewCircuitBreakerManager(e.config.CircuitBreaker)
	e.rateLimiter = NewRateLimiter(e.config.RateLimiting)
	e.healthMon = NewHealthMonitor(e.config.HealthCheck, e.Logger())

	// Initialize auth
	e.gwAuth = NewGatewayAuth(e.config.Auth, e.Logger())

	// Initialize TLS manager
	e.tlsManager = NewTLSManager(e.config.TLS, e.Logger())

	// Initialize response cache (external store resolved in Start)
	e.respCache = NewResponseCache(e.config.Caching, e.Logger(), nil)

	// Initialize metrics
	e.gwMetrics = NewGatewayMetrics(app.Metrics(), e.config.Metrics)

	// Wire up hooks for observability
	e.cbManager.SetOnStateChange(func(targetID string, from, to CircuitState) {
		e.gwMetrics.SetCircuitBreakerState(targetID, to)
		e.hooks.RunOnCircuitBreak(targetID, from, to)
	})

	e.healthMon.SetOnHealthChange(func(event UpstreamHealthEvent) {
		e.gwMetrics.SetUpstreamHealth(event.TargetID, event.Healthy)
		e.hooks.RunOnUpstreamHealth(event)
	})

	e.routeManager.OnRouteChange(func(event RouteEvent) {
		e.hooks.RunOnRouteChange(event)
	})

	// Initialize proxy engine
	e.proxyEngine = NewProxyEngine(
		e.config,
		e.Logger(),
		e.routeManager,
		e.healthMon,
		e.cbManager,
		e.rateLimiter,
		e.stats,
		e.hooks,
	)

	// Wire auth, cache, and TLS into proxy engine
	e.proxyEngine.SetAuth(e.gwAuth)
	e.proxyEngine.SetCache(e.respCache)
	e.proxyEngine.SetTLSManager(e.tlsManager)

	// Initialize dashboard WebSocket hub
	if e.config.Dashboard.Enabled && e.config.Dashboard.Realtime {
		e.hub = NewHub(e.Logger())
	}

	// Register the gateway service with DI
	if err := forge.RegisterValue[*Extension](app.Container(), ServiceKey, e); err != nil {
		return fmt.Errorf("failed to register gateway service: %w", err)
	}

	// Health check is provided via the extension's Health() method

	e.Logger().Info("gateway extension registered",
		forge.F("base_path", e.config.BasePath),
		forge.F("routes", len(e.config.Routes)),
		forge.F("discovery_enabled", e.config.Discovery.Enabled),
		forge.F("dashboard_enabled", e.config.Dashboard.Enabled),
	)

	return nil
}

// Start starts the gateway extension.
func (e *Extension) Start(ctx context.Context) error {
	if !e.config.Enabled {
		return nil
	}

	e.Logger().Info("starting gateway extension")

	// Load manual routes from config
	e.loadManualRoutes()

	// Try to resolve discovery service (optional dependency)
	// The discovery service is resolved as DiscoveryService interface
	discSvc, discErr := forge.Resolve[DiscoveryService](e.app.Container(), "discovery")
	if discErr == nil && discSvc != nil {
		e.Logger().Info("discovery service resolved for FARP integration")

		if e.config.Discovery.Enabled {
			e.disc = NewServiceDiscovery(
				e.config.Discovery,
				e.Logger(),
				e.routeManager,
				discSvc,
			)

			if startErr := e.disc.Start(ctx); startErr != nil {
				e.Logger().Warn("failed to start service discovery", forge.F("error", startErr))
			}
		}
	} else {
		e.Logger().Info("discovery service not available, manual routes only")
	}

	// Try to resolve cache store from DI (optional)
	if e.config.Caching.Enabled {
		cacheStore, cacheErr := forge.Resolve[CacheStore](e.app.Container(), "cache")
		if cacheErr == nil && cacheStore != nil {
			e.respCache = NewResponseCache(e.config.Caching, e.Logger(), cacheStore)
			e.Logger().Info("cache store resolved for response caching")
		} else {
			e.Logger().Info("using in-memory cache store for response caching")
		}
	}

	// Start TLS certificate reload
	if e.config.TLS.Enabled {
		e.tlsManager.Start()
	}

	// Initialize and start OpenAPI aggregator
	if e.config.OpenAPI.Enabled {
		e.openAPI = NewOpenAPIAggregator(e.config.OpenAPI, e.Logger(), e.routeManager, e.disc)
		e.openAPI.Start(ctx)
	}

	// Start health monitor
	e.healthMon.Start(ctx)

	// Register routes (only once)
	if !e.routesRegistered {
		e.registerRoutes()
		e.routesRegistered = true
	}

	// Start WebSocket hub for dashboard
	if e.hub != nil {
		go e.hub.Run()
		go e.broadcastUpdates(ctx)
	}

	// Start rate limiter cleanup
	go e.rateLimiterCleanup(ctx)

	e.MarkStarted()
	e.Logger().Info("gateway extension started",
		forge.F("routes_count", e.routeManager.RouteCount()),
	)

	return nil
}

// Stop stops the gateway extension.
func (e *Extension) Stop(_ context.Context) error {
	if !e.config.Enabled {
		return nil
	}

	e.Logger().Info("stopping gateway extension")

	// Set draining
	e.draining.Store(true)

	// Stop discovery
	if e.disc != nil {
		e.disc.Stop()
	}

	// Stop health monitor
	if e.healthMon != nil {
		e.healthMon.Stop()
	}

	// Stop TLS manager
	if e.tlsManager != nil {
		e.tlsManager.Stop()
	}

	e.MarkStopped()
	e.Logger().Info("gateway extension stopped")

	return nil
}

// Health checks if the gateway is healthy.
func (e *Extension) Health(ctx context.Context) error {
	if !e.config.Enabled {
		return nil
	}

	if e.routeManager.RouteCount() == 0 {
		return fmt.Errorf("no routes configured")
	}

	return e.healthMon.Health(ctx)
}

// Dependencies returns extension dependencies.
func (e *Extension) Dependencies() []string {
	return []string{"discovery"}
}

// RouteManager returns the route manager.
func (e *Extension) RouteManager() *RouteManager { return e.routeManager }

// HealthMonitor returns the health monitor.
func (e *Extension) HealthMonitor() *HealthMonitor { return e.healthMon }

// Stats returns the stats collector.
func (e *Extension) Stats() *StatsCollector { return e.stats }

// Hooks returns the hook engine.
func (e *Extension) Hooks() *HookEngine { return e.hooks }

// Auth returns the gateway auth handler.
func (e *Extension) Auth() *GatewayAuth { return e.gwAuth }

// Cache returns the response cache.
func (e *Extension) Cache() *ResponseCache { return e.respCache }

// TLS returns the TLS manager.
func (e *Extension) TLS() *TLSManager { return e.tlsManager }

// OpenAPI returns the OpenAPI aggregator.
func (e *Extension) OpenAPI() *OpenAPIAggregator { return e.openAPI }

// loadManualRoutes adds manually configured routes.
func (e *Extension) loadManualRoutes() {
	for _, rc := range e.config.Routes {
		route := configToRoute(rc, e.config.BasePath)

		if err := e.routeManager.AddRoute(route); err != nil {
			e.Logger().Warn("failed to add manual route",
				forge.F("path", rc.Path),
				forge.F("error", err),
			)
		}

		// Register targets with health monitor
		for _, target := range route.Targets {
			e.healthMon.Register(route.ID, target)
		}
	}
}

// registerRoutes registers the gateway catch-all handler and admin routes.
func (e *Extension) registerRoutes() {
	router := e.app.Router()

	mustRegister := func(err error) {
		if err != nil {
			e.Logger().Error("failed to register gateway route", forge.F("error", err))
		}
	}

	// Register admin API handlers
	if e.config.Dashboard.Enabled {
		adminBase := e.config.Dashboard.BasePath + "/api"
		h := NewAdminHandlers(e)

		mustRegister(router.GET(adminBase+"/routes", h.handleListRoutes))
		mustRegister(router.GET(adminBase+"/routes/:id", h.handleGetRoute))
		mustRegister(router.POST(adminBase+"/routes", h.handleCreateRoute))
		mustRegister(router.PUT(adminBase+"/routes/:id", h.handleUpdateRoute))
		mustRegister(router.DELETE(adminBase+"/routes/:id", h.handleDeleteRoute))
		mustRegister(router.POST(adminBase+"/routes/:id/enable", h.handleEnableRoute))
		mustRegister(router.POST(adminBase+"/routes/:id/disable", h.handleDisableRoute))
		mustRegister(router.GET(adminBase+"/upstreams", h.handleListUpstreams))
		mustRegister(router.GET(adminBase+"/stats", h.handleGetStats))
		mustRegister(router.GET(adminBase+"/stats/routes", h.handleGetRouteStats))
		mustRegister(router.GET(adminBase+"/config", h.handleGetConfig))
		mustRegister(router.GET(adminBase+"/discovery/services", h.handleListDiscoveredServices))
		mustRegister(router.POST(adminBase+"/discovery/refresh", h.handleRefreshDiscovery))

		// Dashboard WebSocket
		if e.hub != nil {
			mustRegister(router.GET(e.config.Dashboard.BasePath+"/ws", h.handleWebSocket))
		}

		// Dashboard page (catch-all for UI)
		mustRegister(router.GET(e.config.Dashboard.BasePath, h.handleDashboardPage))
		mustRegister(router.GET(e.config.Dashboard.BasePath+"/*path", h.handleDashboardPage))
	}

	// Register OpenAPI aggregation endpoints
	if e.openAPI != nil {
		specBase := e.config.Dashboard.BasePath

		// Aggregated OpenAPI spec (JSON)
		mustRegister(router.GET(specBase+e.config.OpenAPI.Path, e.openAPI.HandleMergedSpec))

		// Swagger UI
		if e.config.OpenAPI.UIPath != "" {
			mustRegister(router.GET(specBase+e.config.OpenAPI.UIPath, e.openAPI.HandleSwaggerUI))
		}

		// Per-service spec
		mustRegister(router.GET(specBase+"/api/openapi/services", e.openAPI.HandleServiceList))
		mustRegister(router.GET(specBase+"/api/openapi/services/:service", e.openAPI.HandleServiceSpec))

		// Refresh endpoint
		mustRegister(router.POST(specBase+"/api/openapi/refresh", e.openAPI.HandleRefresh))
	}

	// Register the catch-all proxy handler
	// This must be registered last to not interfere with admin routes
	basePath := e.config.BasePath
	if basePath == "" {
		basePath = "/"
	}

	// Use a custom handler that wraps the proxy engine with middleware
	proxyHandler := GatewayMiddleware(e.config, e.proxyEngine)

	// Register catch-all for all methods
	catchAll := func(ctx forge.Context) error {
		if e.draining.Load() {
			return ctx.JSON(http.StatusServiceUnavailable, map[string]string{"error": "gateway is draining"})
		}

		proxyHandler.ServeHTTP(ctx.Response(), ctx.Request())

		return nil
	}

	// Register for common HTTP methods
	for _, method := range []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"} {
		var regErr error

		switch method {
		case "GET":
			regErr = router.GET(basePath+"/*path", catchAll)
		case "POST":
			regErr = router.POST(basePath+"/*path", catchAll)
		case "PUT":
			regErr = router.PUT(basePath+"/*path", catchAll)
		case "DELETE":
			regErr = router.DELETE(basePath+"/*path", catchAll)
		case "PATCH":
			regErr = router.PATCH(basePath+"/*path", catchAll)
		case "HEAD":
			regErr = router.HEAD(basePath+"/*path", catchAll)
		case "OPTIONS":
			regErr = router.OPTIONS(basePath+"/*path", catchAll)
		}

		if regErr != nil {
			e.Logger().Warn("failed to register catch-all route",
				forge.F("method", method),
				forge.F("error", regErr),
			)
		}
	}

	e.Logger().Info("gateway routes registered",
		forge.F("base_path", basePath),
		forge.F("dashboard", e.config.Dashboard.Enabled),
	)
}

func (e *Extension) broadcastUpdates(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if e.hub == nil || e.hub.ClientCount() == 0 {
				continue
			}

			// Broadcast stats
			stats := e.stats.Snapshot(e.routeManager, e.healthMon)
			msg := NewWSMessage("stats", stats)

			if err := e.hub.Broadcast(msg); err != nil {
				e.Logger().Error("failed to broadcast stats", forge.F("error", err))
			}

			// Broadcast routes
			routes := e.routeManager.ListRoutes()
			routesMsg := NewWSMessage("routes", routes)

			if err := e.hub.Broadcast(routesMsg); err != nil {
				e.Logger().Error("failed to broadcast routes", forge.F("error", err))
			}
		}
	}
}

func (e *Extension) rateLimiterCleanup(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.rateLimiter.Cleanup(10 * time.Minute)
		}
	}
}

// configToRoute converts a RouteConfig to a Route.
func configToRoute(rc RouteConfig, basePath string) *Route {
	path := basePath + rc.Path

	targets := make([]*Target, 0, len(rc.Targets))
	for _, tc := range rc.Targets {
		t := &Target{
			ID:       fmt.Sprintf("target-%s-%s", rc.Path, tc.URL),
			URL:      tc.URL,
			Weight:   tc.Weight,
			Healthy:  true,
			Tags:     tc.Tags,
			Metadata: tc.Metadata,
			TLS:      tc.TLS,
		}

		if t.Weight <= 0 {
			t.Weight = 1
		}

		targets = append(targets, t)
	}

	protocol := rc.Protocol
	if protocol == "" {
		protocol = ProtocolHTTP
	}

	return &Route{
		ID:            fmt.Sprintf("manual-%s", rc.Path),
		Path:          path,
		Methods:       rc.Methods,
		Targets:       targets,
		StripPrefix:   rc.StripPrefix,
		AddPrefix:     rc.AddPrefix,
		RewritePath:   rc.RewritePath,
		Headers:       rc.Headers,
		Protocol:      protocol,
		Source:        SourceManual,
		Priority:      rc.Priority + 100, // Manual routes get higher base priority
		Enabled:       rc.Enabled,
		Retry:         rc.Retry,
		Timeout:       rc.Timeout,
		RateLimit:     rc.RateLimit,
		Auth:          rc.Auth,
		Cache:         rc.Cache,
		TrafficPolicy: rc.TrafficPolicy,
		Metadata:      rc.Metadata,
	}
}
