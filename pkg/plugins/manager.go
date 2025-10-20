package plugins

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// Manager manages simplified plugins for the framework
type Manager struct {
	plugins map[string]common.Plugin
	router  common.Router
	app     common.Application
	logger  common.Logger
	mu      sync.RWMutex
	started bool
}

// NewManager creates a new plugin manager
func NewManager(app common.Application, router common.Router, log common.Logger) *Manager {
	return &Manager{
		plugins: make(map[string]common.Plugin),
		router:  router,
		app:     app,
		logger:  log,
	}
}

// AddPlugin registers a new plugin with the plugin manager
func (m *Manager) AddPlugin(ctx context.Context, plugin common.Plugin) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	name := plugin.Name()
	if _, exists := m.plugins[name]; exists {
		return fmt.Errorf("plugin %s already registered", name)
	}

	m.plugins[name] = plugin

	if m.logger != nil {
		m.logger.Info("plugin registered",
			logger.String("name", name),
			logger.String("version", plugin.Version()),
		)
	}

	// If already started, start the plugin immediately
	if m.started {
		if err := m.startPlugin(ctx, plugin); err != nil {
			delete(m.plugins, name)
			return fmt.Errorf("failed to start plugin %s: %w", name, err)
		}
	}

	return nil
}

// RemovePlugin removes a plugin by name
func (m *Manager) RemovePlugin(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	plugin, exists := m.plugins[name]
	if !exists {
		return fmt.Errorf("plugin %s not found", name)
	}

	// Stop the plugin if started
	if m.started {
		if err := plugin.Stop(ctx); err != nil {
			if m.logger != nil {
				m.logger.Warn("error stopping plugin during removal",
					logger.String("plugin", name),
					logger.Error(err),
				)
			}
		}
	}

	delete(m.plugins, name)

	if m.logger != nil {
		m.logger.Info("plugin removed", logger.String("name", name))
	}

	return nil
}

// GetPlugin retrieves a plugin by name
func (m *Manager) GetPlugin(name string) (common.Plugin, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	plugin, exists := m.plugins[name]
	if !exists {
		return nil, fmt.Errorf("plugin %s not found", name)
	}

	return plugin, nil
}

// GetPlugins returns all registered plugins
func (m *Manager) GetPlugins() []common.Plugin {
	m.mu.RLock()
	defer m.mu.RUnlock()

	plugins := make([]common.Plugin, 0, len(m.plugins))
	for _, plugin := range m.plugins {
		plugins = append(plugins, plugin)
	}

	return plugins
}

// StartPlugins starts all registered plugins
func (m *Manager) StartPlugins(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return fmt.Errorf("plugins already started")
	}

	for name, plugin := range m.plugins {
		if err := m.startPlugin(ctx, plugin); err != nil {
			return fmt.Errorf("failed to start plugin %s: %w", name, err)
		}
	}

	m.started = true
	return nil
}

// StopPlugins stops all registered plugins
func (m *Manager) StopPlugins(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return nil
	}

	var errors []error
	for name, plugin := range m.plugins {
		if err := plugin.Stop(ctx); err != nil {
			errors = append(errors, fmt.Errorf("plugin %s: %w", name, err))
			if m.logger != nil {
				m.logger.Error("error stopping plugin",
					logger.String("plugin", name),
					logger.Error(err),
				)
			}
		}
	}

	m.started = false

	if len(errors) > 0 {
		return fmt.Errorf("errors stopping plugins: %v", errors)
	}

	return nil
}

// startPlugin starts a single plugin and registers its components
func (m *Manager) startPlugin(ctx context.Context, plugin common.Plugin) error {
	name := plugin.Name()

	// Create PluginContext with container, router, and config manager
	pluginCtx := common.NewPluginContext(ctx, m.app.Container(), m.router, m.app.Config())

	// Step 1: Register services FIRST (before anything else)
	// Controllers, routes, and middleware may depend on these services
	services := plugin.Services()
	if m.app != nil && m.app.Container() != nil {
		for _, svc := range services {
			if err := m.app.Container().Register(svc); err != nil {
				if m.logger != nil {
					m.logger.Warn("failed to register service from plugin",
						logger.String("plugin", name),
						logger.String("service", svc.Name),
						logger.Error(err),
					)
				}
			}
		}
	}

	// Step 2: Start the plugin with PluginContext
	// Plugin can now register additional services if needed
	if err := plugin.Start(pluginCtx); err != nil {
		return fmt.Errorf("start failed: %w", err)
	}

	// Step 3: Register controllers
	// Register controllers directly with router without initializing them yet
	// Controllers will be initialized later after container starts
	controllers := plugin.Controllers()
	if m.router != nil && len(controllers) > 0 {
		// Set plugin context in router if supported
		if setter, ok := m.router.(interface{ SetCurrentPlugin(string) }); ok {
			setter.SetCurrentPlugin(name)
			defer func() {
				if clearer, ok := m.router.(interface{ ClearCurrentPlugin() }); ok {
					clearer.ClearCurrentPlugin()
				}
			}()
		}

		// Register each controller with router
		for _, ctrl := range controllers {
			if m.logger != nil {
				m.logger.Debug("registering controller from plugin",
					logger.String("plugin", name),
					logger.String("controller", ctrl.Name()),
				)
			}

			// Initialize controller with container first
			// Note: This happens before container.Start(), so services cannot be resolved yet
			// Controllers should defer service resolution to their handler methods or
			// implement a lazy initialization pattern
			if err := ctrl.Initialize(m.app.Container()); err != nil {
				if m.logger != nil {
					m.logger.Error("failed to initialize controller from plugin",
						logger.String("plugin", name),
						logger.String("controller", ctrl.Name()),
						logger.Error(err),
					)
				}
				return fmt.Errorf("failed to initialize controller %s: %w", ctrl.Name(), err)
			}

			// Register controller with router
			if err := m.router.RegisterController(ctrl); err != nil {
				if m.logger != nil {
					m.logger.Error("failed to register controller from plugin",
						logger.String("plugin", name),
						logger.String("controller", ctrl.Name()),
						logger.Error(err),
					)
				}
				return fmt.Errorf("failed to register controller %s: %w", ctrl.Name(), err)
			}

			if m.logger != nil {
				m.logger.Debug("controller registered successfully",
					logger.String("plugin", name),
					logger.String("controller", ctrl.Name()),
				)
			}
		}

		if m.logger != nil {
			m.logger.Info("plugin controllers registered",
				logger.String("plugin", name),
				logger.Int("count", len(controllers)),
			)
		}
	}

	// Step 4: Register routes
	// Routes can reference controllers and services
	if m.router != nil {
		if err := plugin.Routes(m.router); err != nil {
			return fmt.Errorf("route registration failed: %w", err)
		}
	}

	// Step 5: Register middleware
	// Middleware is applied last
	middleware := plugin.Middleware()
	if m.router != nil && len(middleware) > 0 {
		for _, mw := range middleware {
			if mwFunc, ok := mw.(func(http.Handler) http.Handler); ok {
				// Register middleware
				m.router.UseMiddleware(mwFunc)
			}
		}
	}

	if m.logger != nil {
		m.logger.Info("plugin started",
			logger.String("name", name),
			logger.String("version", plugin.Version()),
		)
	}

	return nil
}

// HealthCheck performs health checks on all plugins
func (m *Manager) HealthCheck(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var errors []error
	for name, plugin := range m.plugins {
		if err := plugin.HealthCheck(ctx); err != nil {
			errors = append(errors, fmt.Errorf("plugin %s: %w", name, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("plugin health check failures: %v", errors)
	}

	return nil
}
