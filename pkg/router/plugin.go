package router

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// PluginManager manages plugins for the router
type PluginManager struct {
	router  *ForgeRouter
	plugins map[string]*PluginEntry
	started bool
	mu      sync.RWMutex
	logger  common.Logger
	metrics common.Metrics
}

// PluginEntry represents a plugin entry
type PluginEntry struct {
	Plugin          common.Plugin
	Initialized     bool
	Started         bool
	InitializedAt   time.Time
	StartedAt       time.Time
	RouteCount      int
	MiddlewareCount int
	LastError       error
	CallCount       int64
	ErrorCount      int64
	mu              sync.RWMutex
}

// PluginStats contains plugin statistics
type PluginStats struct {
	Name            string    `json:"name"`
	Version         string    `json:"version"`
	Description     string    `json:"description"`
	Initialized     bool      `json:"initialized"`
	Started         bool      `json:"started"`
	InitializedAt   time.Time `json:"initialized_at"`
	StartedAt       time.Time `json:"started_at"`
	RouteCount      int       `json:"route_count"`
	MiddlewareCount int       `json:"middleware_count"`
	CallCount       int64     `json:"call_count"`
	ErrorCount      int64     `json:"error_count"`
	LastError       string    `json:"last_error,omitempty"`
	Dependencies    []string  `json:"dependencies"`
}

// NewPluginManager creates a new plugin manager
func NewPluginManager(router *ForgeRouter) *PluginManager {
	return &PluginManager{
		router:  router,
		plugins: make(map[string]*PluginEntry),
		logger:  router.logger,
		metrics: router.metrics,
	}
}

// AddPlugin adds a plugin to the manager
func (pm *PluginManager) AddPlugin(plugin common.Plugin) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.started {
		return common.ErrLifecycleError("add_plugin", fmt.Errorf("cannot add plugin after manager has started"))
	}

	pluginName := plugin.Name()
	if _, exists := pm.plugins[pluginName]; exists {
		return common.ErrServiceAlreadyExists(pluginName)
	}

	entry := &PluginEntry{
		Plugin:      plugin,
		Initialized: false,
		Started:     false,
	}

	pm.plugins[pluginName] = entry

	if pm.logger != nil {
		pm.logger.Info("plugin added",
			logger.String("name", pluginName),
			logger.String("version", plugin.Version()),
			logger.String("description", plugin.Description()),
			logger.String("dependencies", fmt.Sprintf("%v", plugin.Dependencies())),
		)
	}

	if pm.metrics != nil {
		pm.metrics.Counter("forge.router.plugin_added").Inc()
		pm.metrics.Gauge("forge.router.plugin_count").Set(float64(len(pm.plugins)))
	}

	return nil
}

// RemovePlugin removes a plugin from the manager
func (pm *PluginManager) RemovePlugin(pluginName string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.started {
		return common.ErrLifecycleError("remove_plugin", fmt.Errorf("cannot remove plugin after manager has started"))
	}

	entry, exists := pm.plugins[pluginName]
	if !exists {
		return common.ErrServiceNotFound(pluginName)
	}

	// Stop plugin if it's running
	if entry.Started {
		if err := entry.Plugin.OnStop(context.Background()); err != nil {
			if pm.logger != nil {
				pm.logger.Error("failed to stop plugin during removal",
					logger.String("plugin", pluginName),
					logger.Error(err),
				)
			}
		}
	}

	delete(pm.plugins, pluginName)

	if pm.logger != nil {
		pm.logger.Info("plugin removed",
			logger.String("name", pluginName),
		)
	}

	if pm.metrics != nil {
		pm.metrics.Counter("forge.router.plugin_removed").Inc()
		pm.metrics.Gauge("forge.router.plugin_count").Set(float64(len(pm.plugins)))
	}

	return nil
}

// GetPlugin returns a plugin by name
func (pm *PluginManager) GetPlugin(pluginName string) (*PluginEntry, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	entry, exists := pm.plugins[pluginName]
	if !exists {
		return nil, common.ErrServiceNotFound(pluginName)
	}

	return entry, nil
}

// GetAllPlugins returns all plugins
func (pm *PluginManager) GetAllPlugins() map[string]*PluginEntry {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	plugins := make(map[string]*PluginEntry)
	for name, entry := range pm.plugins {
		plugins[name] = entry
	}

	return plugins
}

// Start starts the plugin manager
func (pm *PluginManager) Start(ctx context.Context) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.started {
		return common.ErrLifecycleError("start", fmt.Errorf("plugin manager already started"))
	}

	// Validate dependencies
	if err := pm.validateDependencies(); err != nil {
		return err
	}

	// Initialize plugins in dependency order
	initOrder, err := pm.calculateInitOrder()
	if err != nil {
		return err
	}

	for _, pluginName := range initOrder {
		entry := pm.plugins[pluginName]
		if err := pm.initializePlugin(ctx, entry); err != nil {
			return err
		}
	}

	// Start plugins in dependency order
	for _, pluginName := range initOrder {
		entry := pm.plugins[pluginName]
		if err := pm.startPlugin(ctx, entry); err != nil {
			return err
		}
	}

	pm.started = true

	if pm.logger != nil {
		pm.logger.Info("plugin manager started",
			logger.Int("plugin_count", len(pm.plugins)),
			logger.String("init_order", fmt.Sprintf("%v", initOrder)),
		)
	}

	if pm.metrics != nil {
		pm.metrics.Counter("forge.router.plugin_manager_started").Inc()
	}

	return nil
}

// Stop stops the plugin manager
func (pm *PluginManager) Stop(ctx context.Context) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if !pm.started {
		return common.ErrLifecycleError("stop", fmt.Errorf("plugin manager not started"))
	}

	// Stop plugins in reverse dependency order
	stopOrder, err := pm.calculateStopOrder()
	if err != nil {
		return err
	}

	for _, pluginName := range stopOrder {
		entry := pm.plugins[pluginName]
		if err := pm.stopPlugin(ctx, entry); err != nil {
			if pm.logger != nil {
				pm.logger.Error("failed to stop plugin",
					logger.String("plugin", pluginName),
					logger.Error(err),
				)
			}
		}
	}

	pm.started = false

	if pm.logger != nil {
		pm.logger.Info("plugin manager stopped")
	}

	if pm.metrics != nil {
		pm.metrics.Counter("forge.router.plugin_manager_stopped").Inc()
	}

	return nil
}

// HealthCheck performs a health check on the plugin manager
func (pm *PluginManager) HealthCheck(ctx context.Context) error {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if !pm.started {
		return common.ErrHealthCheckFailed("plugin_manager", fmt.Errorf("plugin manager not started"))
	}

	// Check each plugin
	for name, entry := range pm.plugins {
		entry.mu.RLock()
		if entry.Started {
			// Check for high error rates
			if entry.CallCount > 0 {
				errorRate := float64(entry.ErrorCount) / float64(entry.CallCount)
				if errorRate > 0.3 { // 30% error rate threshold
					entry.mu.RUnlock()
					return common.ErrHealthCheckFailed("plugin_manager", fmt.Errorf("plugin %s has high error rate: %.2f%%", name, errorRate*100))
				}
			}
		}
		entry.mu.RUnlock()
	}

	return nil
}

// ApplyRoutes applies routes from all plugins
func (pm *PluginManager) ApplyRoutes() error {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if !pm.started {
		return common.ErrLifecycleError("apply_routes", fmt.Errorf("plugin manager not started"))
	}

	totalRoutes := 0
	for name, entry := range pm.plugins {
		if !entry.Started {
			continue
		}

		routes := entry.Plugin.Routes()
		for _, route := range routes {
			if err := pm.applyRoute(entry, route); err != nil {
				return common.ErrPluginInitFailed(name, err)
			}
		}

		entry.mu.Lock()
		entry.RouteCount = len(routes)
		entry.mu.Unlock()

		totalRoutes += len(routes)
	}

	if pm.logger != nil {
		pm.logger.Info("plugin routes applied",
			logger.Int("total_routes", totalRoutes),
		)
	}

	return nil
}

// ApplyMiddleware applies middleware from all plugins
func (pm *PluginManager) ApplyMiddleware() error {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if !pm.started {
		return common.ErrLifecycleError("apply_middleware", fmt.Errorf("plugin manager not started"))
	}

	totalMiddleware := 0
	for name, entry := range pm.plugins {
		if !entry.Started {
			continue
		}

		middleware := entry.Plugin.Middleware()
		for _, mw := range middleware {
			if err := pm.router.middlewareManager.Register(mw); err != nil {
				return common.ErrPluginInitFailed(name, err)
			}
		}

		entry.mu.Lock()
		entry.MiddlewareCount = len(middleware)
		entry.mu.Unlock()

		totalMiddleware += len(middleware)
	}

	if pm.logger != nil {
		pm.logger.Info("plugin middleware applied",
			logger.Int("total_middleware", totalMiddleware),
		)
	}

	return nil
}

// initializePlugin initializes a single plugin
func (pm *PluginManager) initializePlugin(ctx context.Context, entry *PluginEntry) error {
	entry.mu.Lock()
	defer entry.mu.Unlock()

	if entry.Initialized {
		return nil
	}

	pluginName := entry.Plugin.Name()

	if pm.logger != nil {
		pm.logger.Info("initializing plugin",
			logger.String("plugin", pluginName),
		)
	}

	start := time.Now()
	if err := entry.Plugin.Initialize(pm.router.container); err != nil {
		entry.LastError = err
		return common.ErrPluginInitFailed(pluginName, err)
	}

	entry.Initialized = true
	entry.InitializedAt = time.Now()

	if pm.logger != nil {
		pm.logger.Info("plugin initialized",
			logger.String("plugin", pluginName),
			logger.Duration("duration", time.Since(start)),
		)
	}

	if pm.metrics != nil {
		pm.metrics.Counter("forge.router.plugin_initialized").Inc()
		pm.metrics.Histogram("forge.router.plugin_init_duration").Observe(time.Since(start).Seconds())
	}

	return nil
}

// startPlugin starts a single plugin
func (pm *PluginManager) startPlugin(ctx context.Context, entry *PluginEntry) error {
	entry.mu.Lock()
	defer entry.mu.Unlock()

	if entry.Started {
		return nil
	}

	if !entry.Initialized {
		return common.ErrPluginInitFailed(entry.Plugin.Name(), fmt.Errorf("plugin not initialized"))
	}

	pluginName := entry.Plugin.Name()

	if pm.logger != nil {
		pm.logger.Info("starting plugin",
			logger.String("plugin", pluginName),
		)
	}

	start := time.Now()
	if err := entry.Plugin.OnStart(ctx); err != nil {
		entry.LastError = err
		return common.ErrPluginInitFailed(pluginName, err)
	}

	entry.Started = true
	entry.StartedAt = time.Now()

	if pm.logger != nil {
		pm.logger.Info("plugin started",
			logger.String("plugin", pluginName),
			logger.Duration("duration", time.Since(start)),
		)
	}

	if pm.metrics != nil {
		pm.metrics.Counter("forge.router.plugin_started").Inc()
		pm.metrics.Histogram("forge.router.plugin_start_duration").Observe(time.Since(start).Seconds())
	}

	return nil
}

// stopPlugin stops a single plugin
func (pm *PluginManager) stopPlugin(ctx context.Context, entry *PluginEntry) error {
	entry.mu.Lock()
	defer entry.mu.Unlock()

	if !entry.Started {
		return nil
	}

	pluginName := entry.Plugin.Name()

	if pm.logger != nil {
		pm.logger.Info("stopping plugin",
			logger.String("plugin", pluginName),
		)
	}

	start := time.Now()
	if err := entry.Plugin.OnStop(ctx); err != nil {
		entry.LastError = err
		return common.ErrPluginInitFailed(pluginName, err)
	}

	entry.Started = false

	if pm.logger != nil {
		pm.logger.Info("plugin stopped",
			logger.String("plugin", pluginName),
			logger.Duration("duration", time.Since(start)),
		)
	}

	if pm.metrics != nil {
		pm.metrics.Counter("forge.router.plugin_stopped").Inc()
		pm.metrics.Histogram("forge.router.plugin_stop_duration").Observe(time.Since(start).Seconds())
	}

	return nil
}

// applyRoute applies a route from a plugin
func (pm *PluginManager) applyRoute(entry *PluginEntry, route common.RouteDefinition) error {
	// Create instrumented handler
	instrumentedHandler := pm.createInstrumentedRouteHandler(entry, route)

	// Apply to router based on method
	switch route.Method {
	case "GET":
		return pm.router.registerServiceHandler("GET", route.Pattern, instrumentedHandler)
	case "POST":
		return pm.router.registerServiceHandler("POST", route.Pattern, instrumentedHandler)
	case "PUT":
		return pm.router.registerServiceHandler("PUT", route.Pattern, instrumentedHandler)
	case "DELETE":
		return pm.router.registerServiceHandler("DELETE", route.Pattern, instrumentedHandler)
	case "PATCH":
		return pm.router.registerServiceHandler("PATCH", route.Pattern, instrumentedHandler)
	default:
		return common.ErrInvalidConfig("method", fmt.Errorf("unsupported HTTP method: %s", route.Method))
	}
}

// createInstrumentedRouteHandler creates an instrumented route handler
func (pm *PluginManager) createInstrumentedRouteHandler(entry *PluginEntry, route common.RouteDefinition) func(*common.ForgeContext, interface{}) (interface{}, error) {
	return func(ctx *common.ForgeContext, req interface{}) (interface{}, error) {
		start := time.Now()
		pluginName := entry.Plugin.Name()

		// Update call count
		entry.mu.Lock()
		entry.CallCount++
		entry.mu.Unlock()

		// Call the original handler
		result, err := pm.callRouteHandler(route.Handler, ctx, req)

		// Record latency
		latency := time.Since(start)

		// Handle errors
		if err != nil {
			entry.mu.Lock()
			entry.ErrorCount++
			entry.LastError = err
			entry.mu.Unlock()

			if pm.metrics != nil {
				pm.metrics.Counter("forge.router.plugin_route_error", "plugin", pluginName).Inc()
			}

			return nil, err
		}

		// Record success metrics
		if pm.metrics != nil {
			pm.metrics.Counter("forge.router.plugin_route_call", "plugin", pluginName).Inc()
			pm.metrics.Histogram("forge.router.plugin_route_duration", "plugin", pluginName).Observe(latency.Seconds())
		}

		return result, nil
	}
}

// callRouteHandler calls the route handler with proper signature
func (pm *PluginManager) callRouteHandler(handler interface{}, ctx *common.ForgeContext, req interface{}) (interface{}, error) {
	// This is a simplified version - in practice, you'd need proper reflection
	// to handle different handler signatures
	if h, ok := handler.(func(*common.ForgeContext, interface{}) (interface{}, error)); ok {
		return h(ctx, req)
	}

	return nil, common.ErrInvalidConfig("handler", fmt.Errorf("invalid handler signature"))
}

// validateDependencies validates plugin dependencies
func (pm *PluginManager) validateDependencies() error {
	allPlugins := make(map[string]bool)
	for name := range pm.plugins {
		allPlugins[name] = true
	}

	for name, entry := range pm.plugins {
		for _, dep := range entry.Plugin.Dependencies() {
			if !allPlugins[dep] {
				return common.ErrDependencyNotFound(name, dep)
			}
		}
	}

	return nil
}

// calculateInitOrder calculates the initialization order based on dependencies
func (pm *PluginManager) calculateInitOrder() ([]string, error) {
	// Build dependency graph
	graph := make(map[string][]string)
	for name, entry := range pm.plugins {
		graph[name] = entry.Plugin.Dependencies()
	}

	// Topological sort
	visited := make(map[string]bool)
	tempMarked := make(map[string]bool)
	var result []string

	var visit func(string) error
	visit = func(name string) error {
		if tempMarked[name] {
			return common.ErrCircularDependency([]string{name})
		}
		if visited[name] {
			return nil
		}

		tempMarked[name] = true
		for _, dep := range graph[name] {
			if err := visit(dep); err != nil {
				return err
			}
		}
		tempMarked[name] = false
		visited[name] = true
		result = append(result, name)
		return nil
	}

	for name := range graph {
		if !visited[name] {
			if err := visit(name); err != nil {
				return nil, err
			}
		}
	}

	return result, nil
}

// calculateStopOrder calculates the stop order (reverse of init order)
func (pm *PluginManager) calculateStopOrder() ([]string, error) {
	initOrder, err := pm.calculateInitOrder()
	if err != nil {
		return nil, err
	}

	// Reverse the order
	stopOrder := make([]string, len(initOrder))
	for i, j := 0, len(initOrder)-1; i < j; i, j = i+1, j-1 {
		stopOrder[i], stopOrder[j] = initOrder[j], initOrder[i]
	}

	return stopOrder, nil
}

// Count returns the number of plugins
func (pm *PluginManager) Count() int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return len(pm.plugins)
}

// GetStats returns statistics for all plugins
func (pm *PluginManager) GetStats() map[string]PluginStats {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	stats := make(map[string]PluginStats)
	for name, entry := range pm.plugins {
		entry.mu.RLock()

		var lastError string
		if entry.LastError != nil {
			lastError = entry.LastError.Error()
		}

		stats[name] = PluginStats{
			Name:            name,
			Version:         entry.Plugin.Version(),
			Description:     entry.Plugin.Description(),
			Initialized:     entry.Initialized,
			Started:         entry.Started,
			InitializedAt:   entry.InitializedAt,
			StartedAt:       entry.StartedAt,
			RouteCount:      entry.RouteCount,
			MiddlewareCount: entry.MiddlewareCount,
			CallCount:       entry.CallCount,
			ErrorCount:      entry.ErrorCount,
			LastError:       lastError,
			Dependencies:    entry.Plugin.Dependencies(),
		}
		entry.mu.RUnlock()
	}

	return stats
}

// GetPluginStats returns statistics for a specific plugin
func (pm *PluginManager) GetPluginStats(pluginName string) (*PluginStats, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	entry, exists := pm.plugins[pluginName]
	if !exists {
		return nil, common.ErrServiceNotFound(pluginName)
	}

	entry.mu.RLock()
	defer entry.mu.RUnlock()

	var lastError string
	if entry.LastError != nil {
		lastError = entry.LastError.Error()
	}

	return &PluginStats{
		Name:            pluginName,
		Version:         entry.Plugin.Version(),
		Description:     entry.Plugin.Description(),
		Initialized:     entry.Initialized,
		Started:         entry.Started,
		InitializedAt:   entry.InitializedAt,
		StartedAt:       entry.StartedAt,
		RouteCount:      entry.RouteCount,
		MiddlewareCount: entry.MiddlewareCount,
		CallCount:       entry.CallCount,
		ErrorCount:      entry.ErrorCount,
		LastError:       lastError,
		Dependencies:    entry.Plugin.Dependencies(),
	}, nil
}

// IsStarted returns true if the plugin manager has been started
func (pm *PluginManager) IsStarted() bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.started
}
