package middleware

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
)

// Manager manages all service middleware
type Manager struct {
	entries      map[string]*middlewareEntry
	applied      []string
	container    common.Container
	logger       common.Logger
	metrics      common.Metrics
	configManage common.ConfigManager
	config       ManagerConfig
	mu           sync.RWMutex
	started      bool
}

// ManagerConfig contains configuration for the middleware manager
type ManagerConfig struct {
	Middleware map[string]MiddlewareConfig `yaml:"middleware" json:"middleware"`
}

// NewManager creates a new middleware manager
func NewManager(container common.Container, logger common.Logger, metrics common.Metrics, config common.ConfigManager) *Manager {
	return &Manager{
		entries:      make(map[string]*middlewareEntry),
		applied:      make([]string, 0),
		container:    container,
		logger:       logger,
		metrics:      metrics,
		configManage: config,
	}
}

// Name returns the service name
func (m *Manager) Name() string {
	return common.MiddlewareManagerKey
}

// Dependencies returns the service dependencies
func (m *Manager) Dependencies() []string {
	return []string{common.LoggerKey, common.MetricsKey, common.ConfigKey}
}

// OnStart starts the middleware manager
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return common.ErrLifecycleError("start", fmt.Errorf("middleware manager already started"))
	}

	// Initialize and start stateful middleware
	if err := m.initializeStatefulMiddleware(ctx); err != nil {
		return err
	}

	if err := m.startStatefulMiddleware(ctx); err != nil {
		return err
	}

	m.started = true

	if m.logger != nil {
		m.logger.Info("middleware manager started",
			logger.Int("middleware_count", len(m.entries)),
		)
	}

	if m.metrics != nil {
		m.metrics.Counter("forge.middleware.manager_started").Inc()
		m.metrics.Gauge("forge.middleware.count").Set(float64(len(m.entries)))
	}

	return nil
}

// OnStop stops the middleware manager
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return common.ErrLifecycleError("stop", fmt.Errorf("middleware manager not started"))
	}

	// Stop stateful middleware in reverse order
	if err := m.stopStatefulMiddleware(ctx); err != nil {
		if m.logger != nil {
			m.logger.Error("failed to stop some middleware", logger.Error(err))
		}
	}

	m.started = false

	if m.logger != nil {
		m.logger.Info("middleware manager stopped")
	}

	if m.metrics != nil {
		m.metrics.Counter("forge.middleware.manager_stopped").Inc()
	}

	return nil
}

// OnHealthCheck performs a health check on the middleware manager
func (m *Manager) OnHealthCheck(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.started {
		return common.ErrHealthCheckFailed("middleware_manager", fmt.Errorf("middleware manager not started"))
	}

	// Check health of stateful middleware
	for name, entry := range m.entries {
		// Check error rates
		stats := entry.stats.getStats()
		if stats.CallCount > 0 {
			errorRate := float64(stats.ErrorCount) / float64(stats.CallCount)
			if errorRate > 0.5 { // 50% error rate threshold
				return common.ErrHealthCheckFailed("middleware_manager",
					fmt.Errorf("middleware %s has high error rate: %.2f%%", name, errorRate*100))
			}
		}
	}

	return nil
}

func (m *Manager) RegisterAny(middleware any) error {
	switch mw := middleware.(type) {
	case common.Middleware:
		// Function middleware - generate a unique name
		name := fmt.Sprintf("func_middleware_%d", time.Now().UnixNano())
		return m.RegisterFunction(name, 50, mw)

	case common.NamedMiddleware:
		// Named middleware
		return m.Register(mw)

	case common.StatefulMiddleware:
		// Stateful middleware (which is also NamedMiddleware)
		return m.Register(mw)

	default:
		return common.ErrInvalidConfig("middleware", fmt.Errorf("unsupported middleware type: %T", middleware))
	}
}

// RegisterFunction registers a function middleware with automatic naming
func (m *Manager) RegisterFunction(name string, priority int, handler common.Middleware) error {
	namedMiddleware := NewFunctionMiddleware(name, priority, handler)
	return m.Register(namedMiddleware)
}

// RegisterFunctionWithDefaults registers a function middleware with default priority
func (m *Manager) RegisterFunctionWithDefaults(name string, handler common.Middleware) error {
	return m.RegisterFunction(name, 50, handler) // Default priority
}

// Register registers a new middleware
func (m *Manager) Register(middleware common.NamedMiddleware) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return common.ErrLifecycleError("register", fmt.Errorf("cannot register middleware after manager has started"))
	}

	name := middleware.Name()
	if _, exists := m.entries[name]; exists {
		return common.ErrServiceAlreadyExists(name)
	}

	// Wrap with instrumentation for internal stats
	instrumented := newInstrumentedWrapper(middleware)

	entry := &middlewareEntry{
		middleware: instrumented,
		stats:      instrumented.stats,
	}

	m.entries[name] = entry

	if m.logger != nil {
		m.logger.Info("middleware registered",
			logger.String("name", name),
			logger.Int("priority", middleware.Priority()),
		)
	}

	if m.metrics != nil {
		m.metrics.Counter("forge.middleware.registered").Inc()
		m.metrics.Gauge("forge.middleware.count").Set(float64(len(m.entries)))
	}

	return nil
}

// Unregister unregisters a middleware
func (m *Manager) Unregister(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return common.ErrLifecycleError("unregister", fmt.Errorf("cannot unregister middleware after manager has started"))
	}

	if _, exists := m.entries[name]; !exists {
		return common.ErrServiceNotFound(name)
	}

	delete(m.entries, name)

	if m.logger != nil {
		m.logger.Info("middleware unregistered", logger.String("name", name))
	}

	if m.metrics != nil {
		m.metrics.Counter("forge.middleware.unregistered").Inc()
		m.metrics.Gauge("forge.middleware.count").Set(float64(len(m.entries)))
	}

	return nil
}

// Apply applies all middleware to a router
func (m *Manager) Apply(router common.Router) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return common.ErrLifecycleError("apply", fmt.Errorf("middleware manager not started"))
	}

	// Sort middleware by priority
	middlewareList := make([]*middlewareEntry, 0, len(m.entries))
	for _, entry := range m.entries {
		middlewareList = append(middlewareList, entry)
	}

	sort.Slice(middlewareList, func(i, j int) bool {
		return middlewareList[i].middleware.Priority() < middlewareList[j].middleware.Priority()
	})

	// Apply middleware in priority order
	for _, entry := range middlewareList {
		handler := entry.middleware.Handler()
		router.UseMiddleware(handler)

		entry.applied = true
		entry.appliedAt = time.Now()
		entry.stats.applied = true
		entry.stats.appliedAt = time.Now()
		entry.stats.status = "applied"

		m.applied = append(m.applied, entry.middleware.Name())

		if m.logger != nil {
			m.logger.Info("middleware applied",
				logger.String("middleware", entry.middleware.Name()),
				logger.Int("priority", entry.middleware.Priority()),
			)
		}
	}

	if m.metrics != nil {
		m.metrics.Counter("forge.middleware.applied").Add(float64(len(middlewareList)))
	}

	return nil
}

// GetMiddleware returns a middleware by name
func (m *Manager) GetMiddleware(name string) (common.NamedMiddleware, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entry, exists := m.entries[name]
	if !exists {
		return nil, common.ErrServiceNotFound(name)
	}

	return entry.middleware, nil
}

// GetAllMiddleware returns all middleware
func (m *Manager) GetAllMiddleware() map[string]common.NamedMiddleware {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]common.NamedMiddleware)
	for name, entry := range m.entries {
		result[name] = entry.middleware
	}

	return result
}

// GetStats returns statistics for all middleware
func (m *Manager) GetStats() map[string]MiddlewareStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := make(map[string]common.MiddlewareStats)
	for name, entry := range m.entries {
		stats[name] = entry.stats.getStats()
	}

	return stats
}

// GetMiddlewareStats returns statistics for a specific middleware
func (m *Manager) GetMiddlewareStats(name string) (*MiddlewareStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	middleware, exists := m.entries[name]
	if !exists {
		return nil, common.ErrServiceNotFound(name)
	}

	stats := middleware.stats.getStats()
	return &stats, nil
}

// IsStarted returns true if the manager has been started
func (m *Manager) IsStarted() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.started
}

// GetAppliedOrder returns the order in which middleware was applied
func (m *Manager) GetAppliedOrder() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]string, len(m.applied))
	copy(result, m.applied)
	return result
}

// loadConfiguration loads middleware configuration
func (m *Manager) loadConfiguration() error {
	if m.configManage != nil {
		return nil
	}

	var config ManagerConfig
	if err := m.configManage.Bind("middleware", &config); err != nil {
		// Configuration is optional, so we don't fail if it doesn't exist
		if m.logger != nil {
			m.logger.Warn("no middleware configuration found, using defaults")
		}
		return nil
	}

	m.config = config
	return nil
}

// initializeStatefulMiddleware initializes stateful middleware
func (m *Manager) initializeStatefulMiddleware(ctx context.Context) error {
	for name, entry := range m.entries {
		// Check if the original middleware (not the wrapper) is stateful
		if stateful, ok := entry.middleware.(common.StatefulMiddleware); ok {
			if err := stateful.Initialize(m.container); err != nil {
				return common.ErrServiceStartFailed(name, fmt.Errorf("failed to initialize middleware: %w", err))
			}

			if m.logger != nil {
				m.logger.Info("stateful middleware initialized", logger.String("middleware", name))
			}
		}
	}
	return nil
}

// startStatefulMiddleware starts stateful middleware
func (m *Manager) startStatefulMiddleware(ctx context.Context) error {
	for name, entry := range m.entries {
		// Check if the original middleware (not the wrapper) is stateful
		if stateful, ok := entry.middleware.(common.StatefulMiddleware); ok {
			if err := stateful.OnStart(ctx); err != nil {
				return common.ErrServiceStartFailed(name, fmt.Errorf("failed to start middleware: %w", err))
			}

			entry.stats.status = "started"

			if m.logger != nil {
				m.logger.Info("stateful middleware started", logger.String("middleware", name))
			}
		}
	}
	return nil
}

// stopStatefulMiddleware stops stateful middleware in reverse order
func (m *Manager) stopStatefulMiddleware(ctx context.Context) error {
	// Stop in reverse order of applied
	for i := len(m.applied) - 1; i >= 0; i-- {
		name := m.applied[i]
		if entry, exists := m.entries[name]; exists {
			if stateful, ok := entry.middleware.(common.StatefulMiddleware); ok {
				if err := stateful.OnStop(ctx); err != nil {
					if m.logger != nil {
						m.logger.Error("failed to stop stateful middleware",
							logger.String("middleware", name),
							logger.Error(err),
						)
					}
					// Continue stopping other middleware even if one fails
				} else {
					entry.stats.status = "stopped"
					if m.logger != nil {
						m.logger.Info("stateful middleware stopped", logger.String("middleware", name))
					}
				}
			}
		}
	}
	return nil
}

// NewService creates a new middleware manager service
func NewService(container common.Container, l common.Logger, metrics common.Metrics, config common.ConfigManager) *Manager {
	return NewManager(container, l, metrics, config)
}

// RegisterService registers the streaming manager directly with a DI container
func RegisterService(container common.Container) error {
	return container.Register(common.ServiceDefinition{
		Name: common.MiddlewareManagerKey,
		Type: (*Manager)(nil),
		Constructor: func(l common.Logger, metrics common.Metrics, config common.ConfigManager) *Manager {
			return NewService(container, l, metrics, config)
		},
		Singleton:    true,
		Dependencies: []string{common.LoggerKey, common.MetricsKey, common.ConfigKey},
	})
}
