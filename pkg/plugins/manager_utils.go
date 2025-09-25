package plugins

import (
	"context"
	"fmt"

	"github.com/xraph/forge/pkg/logger"
)

// Register all plugin components
func (pm *PluginManagerImpl) registerPluginComponents(ctx context.Context, plugin Plugin) error {
	pluginID := plugin.ID()
	pm.logger.Info("registering plugin components", logger.String("plugin_id", pluginID))

	// Initialize tracking maps if needed (do this with a lock)
	pm.mu.Lock()
	if pm.pluginRoutes == nil {
		pm.pluginRoutes = make(map[string][]string)
	}
	if pm.pluginServices == nil {
		pm.pluginServices = make(map[string][]string)
	}
	if pm.pluginMiddleware == nil {
		pm.pluginMiddleware = make(map[string][]string)
	}
	pm.mu.Unlock()

	// Register in order: Services -> Middleware -> Controllers -> Routes -> Hooks
	// Do this WITHOUT holding the main lock to avoid deadlocks
	if err := pm.registerPluginServices(ctx, plugin); err != nil {
		return fmt.Errorf("failed to register services: %w", err)
	}

	if err := pm.registerPluginMiddleware(ctx, plugin); err != nil {
		pm.unregisterPluginServices(ctx, pluginID) // Cleanup on failure
		return fmt.Errorf("failed to register middleware: %w", err)
	}

	if err := pm.registerPluginControllers(ctx, plugin); err != nil {
		pm.unregisterPluginServices(ctx, pluginID)
		pm.unregisterPluginMiddleware(ctx, pluginID)
		return fmt.Errorf("failed to register controllers: %w", err)
	}

	if err := pm.registerPluginRoutes(ctx, plugin); err != nil {
		pm.unregisterPluginServices(ctx, pluginID)
		pm.unregisterPluginMiddleware(ctx, pluginID)
		pm.unregisterPluginControllers(ctx, pluginID)
		return fmt.Errorf("failed to register routes: %w", err)
	}

	if err := pm.registerPluginHooks(ctx, plugin); err != nil {
		pm.unregisterPluginServices(ctx, pluginID)
		pm.unregisterPluginMiddleware(ctx, pluginID)
		pm.unregisterPluginControllers(ctx, pluginID)
		pm.unregisterPluginRoutes(ctx, pluginID)
		return fmt.Errorf("failed to register hooks: %w", err)
	}

	return nil
}

// Register plugin services with the container
func (pm *PluginManagerImpl) registerPluginServices(ctx context.Context, plugin Plugin) error {
	pluginID := plugin.ID()
	services := plugin.Services()

	if len(services) == 0 {
		return nil
	}

	var registeredServices []string

	for _, serviceDef := range services {
		// Validate service definition
		if serviceDef.Name == "" {
			return fmt.Errorf("service definition missing name")
		}

		// Check if service already exists
		if pm.container.HasNamed(serviceDef.Name) {
			pm.logger.Warn("service already exists, skipping",
				logger.String("plugin_id", pluginID),
				logger.String("service", serviceDef.Name),
			)
			continue
		}

		// Register service with container (no lock needed here)
		if pm.appContainer != nil {
			if err := pm.appContainer.Register(serviceDef); err != nil {
				// Cleanup already registered services
				// for _, svcName := range registeredServices {
				// 	// pm.appContainer.UnregisterService(ctx, svcName)
				// }
				return fmt.Errorf("failed to register service %s: %w", serviceDef.Name, err)
			}
		} else {
			// Fallback to basic container registration
			if err := pm.container.Register(serviceDef); err != nil {
				return fmt.Errorf("failed to register service %s: %w", serviceDef.Name, err)
			}
		}

		registeredServices = append(registeredServices, serviceDef.Name)

		pm.logger.Debug("service registered",
			logger.String("plugin_id", pluginID),
			logger.String("service", serviceDef.Name),
		)
	}

	// Track registered services for cleanup (lock only for this update)
	pm.mu.Lock()
	pm.pluginServices[pluginID] = registeredServices
	pm.mu.Unlock()

	pm.logger.Info("plugin services registered",
		logger.String("plugin_id", pluginID),
		logger.Int("count", len(registeredServices)),
	)

	return nil
}

// Register plugin middleware
func (pm *PluginManagerImpl) registerPluginMiddleware(ctx context.Context, plugin Plugin) error {
	pluginID := plugin.ID()
	middlewares := plugin.Middleware()

	if len(middlewares) == 0 {
		return nil
	}

	if pm.middlewareManager == nil {
		return fmt.Errorf("no middleware manager available")
	}

	var registeredMiddleware []string

	for _, middlewareDef := range middlewares {
		// Register middleware (no lock needed for external call)
		if err := pm.middlewareManager.RegisterAny(middlewareDef); err != nil {
			// Cleanup already registered middleware
			for _, mwName := range registeredMiddleware {
				pm.middlewareManager.Unregister(mwName)
			}
			return fmt.Errorf("failed to register middleware: %w", err)
		}

		pm.logger.Debug("middleware registered",
			logger.String("plugin_id", pluginID),
		)
	}

	// Track registered middleware for cleanup (lock only for this update)
	pm.mu.Lock()
	pm.pluginMiddleware[pluginID] = registeredMiddleware
	pm.mu.Unlock()

	pm.logger.Info("plugin middleware registered",
		logger.String("plugin_id", pluginID),
		logger.Int("count", len(registeredMiddleware)),
	)

	return nil
}

// Register plugin hooks
func (pm *PluginManagerImpl) registerPluginHooks(ctx context.Context, plugin Plugin) error {
	pluginID := plugin.ID()
	hooks := plugin.Hooks()

	if len(hooks) == 0 {
		return nil
	}

	// Register hooks (no lock needed for external call)
	if err := pm.hookSystem.RegisterPluginHooks(ctx, pluginID, hooks); err != nil {
		return fmt.Errorf("failed to register hook %s: %w", plugin, err)
	}

	return nil
}

// Unregister all plugin components during unload
func (pm *PluginManagerImpl) unregisterPluginComponents(ctx context.Context, pluginID string) {
	// Unregister routes
	if routeIDs, exists := pm.pluginRoutes[pluginID]; exists {
		for _, routeID := range routeIDs {
			if pm.router != nil {
				fmt.Println("routeID", routeID)
				// if err := pm.router.UnregisterRoute(ctx, routeID); err != nil {
				// 	pm.logger.Error("failed to unregister route",
				// 		logger.String("plugin_id", pluginID),
				// 		logger.String("route_id", routeID),
				// 		logger.Error(err),
				// 	)
				// }
			}
		}
		delete(pm.pluginRoutes, pluginID)
	}

	// Unregister middleware
	if middlewareNames, exists := pm.pluginMiddleware[pluginID]; exists {
		for _, mwName := range middlewareNames {
			if pm.middlewareManager != nil {
				if err := pm.middlewareManager.Unregister(mwName); err != nil {
					pm.logger.Error("failed to unregister middleware",
						logger.String("plugin_id", pluginID),
						logger.String("middleware", mwName),
						logger.Error(err),
					)
				}
			}
		}
		delete(pm.pluginMiddleware, pluginID)
	}

	// Unregister services
	if serviceNames, exists := pm.pluginServices[pluginID]; exists {
		for _, svcName := range serviceNames {
			if pm.appContainer != nil {
				fmt.Println("unregister service", svcName)
				// if err := pm.appContainer.UnregisterService(ctx, svcName); err != nil {
				// 	pm.logger.Error("failed to unregister service",
				// 		logger.String("plugin_id", pluginID),
				// 		logger.String("service", svcName),
				// 		logger.Error(err),
				// 	)
				// }
			}
		}
		delete(pm.pluginServices, pluginID)
	}

	// Unregister hooks
	if err := pm.hookSystem.UnregisterPluginHooks(ctx, pluginID); err != nil {
		pm.logger.Error("failed to unregister hooks",
			logger.String("plugin_id", pluginID),
			logger.Error(err),
		)
	}

	pm.logger.Info("plugin components unregistered",
		logger.String("plugin_id", pluginID),
	)
}

// Register plugin routes with instrumentation (inspired by router plugin manager)
func (pm *PluginManagerImpl) registerPluginRoutes(ctx context.Context, plugin Plugin) error {
	pluginID := plugin.ID()

	if pm.router == nil {
		return fmt.Errorf("no router available for route registration")
	}

	// Set plugin context in router (no lock needed)
	pm.router.SetCurrentPlugin(pluginID)
	defer pm.router.ClearCurrentPlugin()

	// Let plugin configure routes directly with main router (no lock needed)
	if err := plugin.ConfigureRoutes(pm.router); err != nil {
		// Cleanup any routes that were registered before the error
		routes := pm.router.GetPluginRoutes(pluginID)
		for _, routeID := range routes {
			pm.router.UnregisterPluginRoute(routeID)
		}
		return fmt.Errorf("failed to configure plugin routes: %w", err)
	}

	// Get final route count for metrics (no lock needed)
	routes := pm.router.GetPluginRoutes(pluginID)

	pm.logger.Info("plugin routes registered",
		logger.String("plugin_id", pluginID),
		logger.Int("count", len(routes)),
	)

	return nil
}

// Register plugin routes with instrumentation (inspired by router plugin manager)
func (pm *PluginManagerImpl) registerPluginControllers(ctx context.Context, plugin Plugin) error {
	pluginID := plugin.ID()

	if pm.router == nil {
		return fmt.Errorf("no router available for controller registration")
	}

	controllers := plugin.Controllers()
	if len(controllers) == 0 {
		return nil
	}

	// Set plugin context in router (this should not require the plugin manager lock)
	pm.router.SetCurrentPlugin(pluginID)
	defer pm.router.ClearCurrentPlugin()

	// Register each controller WITHOUT holding the plugin manager lock
	for _, controller := range controllers {
		pm.logger.Debug("registering controller",
			logger.String("plugin_id", pluginID),
			logger.String("controller_name", controller.Name()),
		)

		err := pm.router.RegisterController(controller)
		if err != nil {
			pm.logger.Error("failed to register controller",
				logger.String("plugin_id", pluginID),
				logger.String("controller_name", controller.Name()),
				logger.Error(err),
			)
			return fmt.Errorf("failed to configure controller %s: %w", controller.Name(), err)
		}

		pm.logger.Debug("controller registered successfully",
			logger.String("plugin_id", pluginID),
			logger.String("controller_name", controller.Name()),
		)
	}

	pm.logger.Info("plugin controllers registered",
		logger.String("plugin_id", pluginID),
		logger.Int("count", len(controllers)),
	)

	return nil
}

// Update plugin health score based on error rates (from router plugin manager)
func (pm *PluginManagerImpl) updatePluginHealthScore(entry *PluginEntry) {
	entry.mu.Lock()
	defer entry.mu.Unlock()

	if entry.Metrics.CallCount > 0 {
		errorRate := float64(entry.Metrics.ErrorCount) / float64(entry.Metrics.CallCount)

		// Calculate health score (1.0 is perfect, 0.0 is completely broken)
		healthScore := 1.0 - errorRate
		if healthScore < 0 {
			healthScore = 0
		}

		entry.HealthScore = healthScore

		// Log if health score is critically low
		if healthScore < 0.5 {
			pm.logger.Warn("plugin health score critically low",
				logger.String("plugin_id", entry.Plugin.ID()),
				logger.Float64("health_score", healthScore),
				logger.Float64("error_rate", errorRate),
				logger.Int64("call_count", entry.Metrics.CallCount),
				logger.Int64("error_count", entry.Metrics.ErrorCount),
			)
		}
	}
}

// Enhanced dependency validation with proper topological sorting (from router plugin manager)
func (pm *PluginManagerImpl) validateDependencies(ctx context.Context, plugin Plugin) error {
	// Check if all required dependencies are available
	for _, dep := range plugin.Dependencies() {
		if !dep.Required {
			continue
		}

		switch dep.Type {
		case "plugin":
			if _, exists := pm.plugins[dep.Name]; !exists {
				return fmt.Errorf("required plugin dependency not found: %s", dep.Name)
			}
		case "service":
			if !pm.container.HasNamed(dep.Name) {
				return fmt.Errorf("required service dependency not found: %s", dep.Name)
			}
		default:
			pm.logger.Warn("unknown dependency type",
				logger.String("plugin_id", plugin.ID()),
				logger.String("dependency", dep.Name),
				logger.String("type", dep.Type),
			)
		}
	}

	// Check for circular dependencies
	if err := pm.checkCircularDependencies(plugin); err != nil {
		return err
	}

	return nil
}

// Check for circular dependencies
func (pm *PluginManagerImpl) checkCircularDependencies(newPlugin Plugin) error {
	// Build dependency graph including the new plugin
	graph := make(map[string][]string)

	// Add existing plugins
	for pluginID, entry := range pm.plugins {
		var deps []string
		for _, dep := range entry.Plugin.Dependencies() {
			if dep.Type == "plugin" {
				deps = append(deps, dep.Name)
			}
		}
		graph[pluginID] = deps
	}

	// Add new plugin
	var newDeps []string
	for _, dep := range newPlugin.Dependencies() {
		if dep.Type == "plugin" {
			newDeps = append(newDeps, dep.Name)
		}
	}
	graph[newPlugin.ID()] = newDeps

	// Perform topological sort to detect cycles
	return pm.detectCycles(graph)
}

// Detect cycles in dependency graph
func (pm *PluginManagerImpl) detectCycles(graph map[string][]string) error {
	visited := make(map[string]bool)
	recursionStack := make(map[string]bool)

	var visit func(string) error
	visit = func(node string) error {
		if recursionStack[node] {
			return fmt.Errorf("circular dependency detected involving plugin: %s", node)
		}
		if visited[node] {
			return nil
		}

		recursionStack[node] = true
		for _, dep := range graph[node] {
			if _, exists := graph[dep]; exists {
				if err := visit(dep); err != nil {
					return err
				}
			}
		}
		recursionStack[node] = false
		visited[node] = true
		return nil
	}

	for node := range graph {
		if !visited[node] {
			if err := visit(node); err != nil {
				return err
			}
		}
	}

	return nil
}

func (pm *PluginManagerImpl) unregisterPluginServices(ctx context.Context, pluginID string) {
	if serviceNames, exists := pm.pluginServices[pluginID]; exists {
		for _, svcName := range serviceNames {
			if pm.appContainer != nil {
				pm.logger.Debugf("unregistering plugin service %v", svcName)
				// if err := pm.appContainer.UnregisterService(ctx, svcName); err != nil {
				// 	pm.logger.Error("failed to unregister service",
				// 		logger.String("plugin_id", pluginID),
				// 		logger.String("service", svcName),
				// 		logger.Error(err),
				// 	)
				// }
			}
		}
		delete(pm.pluginServices, pluginID)
	}
}

func (pm *PluginManagerImpl) unregisterPluginMiddleware(ctx context.Context, pluginID string) {
	if middlewareNames, exists := pm.pluginMiddleware[pluginID]; exists {
		for _, mwName := range middlewareNames {
			if pm.middlewareManager != nil {
				pm.logger.Debugf("unregistering middleware %v", mwName)
				// if err := pm.middlewareManager.UnregisterMiddleware(ctx, mwName); err != nil {
				// 	pm.logger.Error("failed to unregister middleware",
				// 		logger.String("plugin_id", pluginID),
				// 		logger.String("middleware", mwName),
				// 		logger.Error(err),
				// 	)
				// }
			}
		}
		delete(pm.pluginMiddleware, pluginID)
	}
}

func (pm *PluginManagerImpl) unregisterPluginRoutes(ctx context.Context, pluginID string) {
	if pm.router == nil {
		return
	}

	routes := pm.router.GetPluginRoutes(pluginID)
	for _, routeID := range routes {
		if err := pm.router.UnregisterPluginRoute(routeID); err != nil {
			pm.logger.Error("failed to unregister plugin route",
				logger.String("plugin_id", pluginID),
				logger.String("route_id", routeID),
				logger.Error(err),
			)
		}
	}
}

func (pm *PluginManagerImpl) unregisterPluginControllers(ctx context.Context, pluginID string) {
	if pm.router == nil {
		return
	}

	controllers := pm.plugins[pluginID].Plugin.Controllers()
	for _, controller := range controllers {
		if err := pm.router.UnregisterController(controller.Name()); err != nil {
			pm.logger.Error("failed to unregister plugin route",
				logger.String("plugin_id", pluginID),
				logger.String("controller_name", controller.Name()),
				logger.Error(err),
			)
		}
	}
}
