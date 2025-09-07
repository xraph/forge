package middleware

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// Manager manages all service middleware
type Manager struct {
	middleware map[string]Middleware
	applied    []string
	container  common.Container
	logger     common.Logger
	metrics    common.Metrics
	config     common.ConfigManager
	mu         sync.RWMutex
	started    bool
}

// ManagerConfig contains configuration for the middleware manager
type ManagerConfig struct {
	Middleware map[string]MiddlewareConfig `yaml:"middleware" json:"middleware"`
}

// NewManager creates a new middleware manager
func NewManager(container common.Container, logger common.Logger, metrics common.Metrics, config common.ConfigManager) *Manager {
	return &Manager{
		middleware: make(map[string]Middleware),
		applied:    make([]string, 0),
		container:  container,
		logger:     logger,
		metrics:    metrics,
		config:     config,
	}
}

// Name returns the service name
func (m *Manager) Name() string {
	return "middleware-manager"
}

// Dependencies returns the service dependencies
func (m *Manager) Dependencies() []string {
	return []string{"logger", "metrics", "config-manager"}
}

// OnStart starts the middleware manager
func (m *Manager) OnStart(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return common.ErrLifecycleError("start", fmt.Errorf("middleware manager already started"))
	}

	// Load configuration
	if err := m.loadConfiguration(); err != nil {
		return common.ErrConfigError("failed to load middleware configuration", err)
	}

	// Validate dependencies
	if err := m.validateDependencies(); err != nil {
		return err
	}

	// Initialize all middleware
	if err := m.initializeMiddleware(ctx); err != nil {
		return err
	}

	// Start all middleware
	if err := m.startMiddleware(ctx); err != nil {
		return err
	}

	m.started = true

	if m.logger != nil {
		m.logger.Info("middleware manager started",
			logger.Int("middleware_count", len(m.middleware)),
		)
	}

	if m.metrics != nil {
		m.metrics.Counter("forge.middleware.manager_started").Inc()
		m.metrics.Gauge("forge.middleware.count").Set(float64(len(m.middleware)))
	}

	return nil
}

// OnStop stops the middleware manager
func (m *Manager) OnStop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return common.ErrLifecycleError("stop", fmt.Errorf("middleware manager not started"))
	}

	// Stop all middleware in reverse order
	stopOrder := make([]string, len(m.applied))
	copy(stopOrder, m.applied)
	for i := len(stopOrder)/2 - 1; i >= 0; i-- {
		opp := len(stopOrder) - 1 - i
		stopOrder[i], stopOrder[opp] = stopOrder[opp], stopOrder[i]
	}

	for _, name := range stopOrder {
		if middleware, exists := m.middleware[name]; exists {
			if err := middleware.OnStop(ctx); err != nil {
				if m.logger != nil {
					m.logger.Error("failed to stop middleware",
						logger.String("middleware", name),
						logger.Error(err),
					)
				}
			}
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

	// Check health of all middleware
	for name, middleware := range m.middleware {
		if err := middleware.OnHealthCheck(ctx); err != nil {
			return common.ErrHealthCheckFailed("middleware_manager",
				fmt.Errorf("middleware %s failed health check: %w", name, err))
		}
	}

	return nil
}

// Register registers a new middleware
func (m *Manager) Register(middleware Middleware) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return common.ErrLifecycleError("register", fmt.Errorf("cannot register middleware after manager has started"))
	}

	name := middleware.Name()
	if _, exists := m.middleware[name]; exists {
		return common.ErrServiceAlreadyExists(name)
	}

	m.middleware[name] = middleware

	if m.logger != nil {
		m.logger.Info("middleware registered",
			logger.String("name", name),
			logger.Int("priority", middleware.Priority()),
			logger.String("dependencies", fmt.Sprintf("%v", middleware.Dependencies())),
		)
	}

	if m.metrics != nil {
		m.metrics.Counter("forge.middleware.registered").Inc()
		m.metrics.Gauge("forge.middleware.count").Set(float64(len(m.middleware)))
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

	if _, exists := m.middleware[name]; !exists {
		return common.ErrServiceNotFound(name)
	}

	delete(m.middleware, name)

	if m.logger != nil {
		m.logger.Info("middleware unregistered",
			logger.String("name", name),
		)
	}

	if m.metrics != nil {
		m.metrics.Counter("forge.middleware.unregistered").Inc()
		m.metrics.Gauge("forge.middleware.count").Set(float64(len(m.middleware)))
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
	middlewareList := make([]Middleware, 0, len(m.middleware))
	for _, middleware := range m.middleware {
		middlewareList = append(middlewareList, middleware)
	}

	sort.Slice(middlewareList, func(i, j int) bool {
		return middlewareList[i].Priority() < middlewareList[j].Priority()
	})

	// Apply middleware in priority order
	for _, middleware := range middlewareList {
		handler := middleware.Handler()
		router.UseMiddleware(handler)
		m.applied = append(m.applied, middleware.Name())

		if m.logger != nil {
			m.logger.Info("middleware applied",
				logger.String("middleware", middleware.Name()),
				logger.Int("priority", middleware.Priority()),
			)
		}
	}

	if m.metrics != nil {
		m.metrics.Counter("forge.middleware.applied").Add(float64(len(middlewareList)))
	}

	return nil
}

// GetMiddleware returns a middleware by name
func (m *Manager) GetMiddleware(name string) (Middleware, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	middleware, exists := m.middleware[name]
	if !exists {
		return nil, common.ErrServiceNotFound(name)
	}

	return middleware, nil
}

// GetAllMiddleware returns all middleware
func (m *Manager) GetAllMiddleware() map[string]Middleware {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]Middleware)
	for name, middleware := range m.middleware {
		result[name] = middleware
	}

	return result
}

// GetStats returns statistics for all middleware
func (m *Manager) GetStats() map[string]MiddlewareStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := make(map[string]MiddlewareStats)
	for name, middleware := range m.middleware {
		stats[name] = middleware.GetStats()
	}

	return stats
}

// GetMiddlewareStats returns statistics for a specific middleware
func (m *Manager) GetMiddlewareStats(name string) (*MiddlewareStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	middleware, exists := m.middleware[name]
	if !exists {
		return nil, common.ErrServiceNotFound(name)
	}

	stats := middleware.GetStats()
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
	if m.config == nil {
		return nil
	}

	var config ManagerConfig
	if err := m.config.Bind("middleware", &config); err != nil {
		// Configuration is optional, so we don't fail if it doesn't exist
		if m.logger != nil {
			m.logger.Warn("no middleware configuration found, using defaults")
		}
		return nil
	}

	// TODO: Use configuration to conditionally enable/disable middleware
	// This would be implemented when specific middleware types are added

	return nil
}

// validateDependencies validates that all middleware dependencies exist
func (m *Manager) validateDependencies() error {
	allMiddleware := make(map[string]bool)
	for name := range m.middleware {
		allMiddleware[name] = true
	}

	for name, middleware := range m.middleware {
		for _, dep := range middleware.Dependencies() {
			// Check if dependency is another middleware
			if _, exists := allMiddleware[dep]; !exists {
				// Check if dependency is a service in the container
				if !m.container.HasNamed(dep) {
					return common.ErrDependencyNotFound(name, dep)
				}
			}
		}
	}

	return nil
}

// initializeMiddleware initializes all middleware
func (m *Manager) initializeMiddleware(ctx context.Context) error {
	// Calculate initialization order based on dependencies
	initOrder, err := m.calculateInitOrder()
	if err != nil {
		return err
	}

	for _, name := range initOrder {
		middleware := m.middleware[name]
		if err := middleware.Initialize(m.container); err != nil {
			return common.ErrServiceStartFailed(name, fmt.Errorf("failed to initialize middleware: %w", err))
		}

		if m.logger != nil {
			m.logger.Info("middleware initialized",
				logger.String("middleware", name),
			)
		}
	}

	return nil
}

// startMiddleware starts all middleware
func (m *Manager) startMiddleware(ctx context.Context) error {
	// Calculate start order based on dependencies
	startOrder, err := m.calculateInitOrder()
	if err != nil {
		return err
	}

	for _, name := range startOrder {
		middleware := m.middleware[name]
		if err := middleware.OnStart(ctx); err != nil {
			return common.ErrServiceStartFailed(name, fmt.Errorf("failed to start middleware: %w", err))
		}

		if m.logger != nil {
			m.logger.Info("middleware started",
				logger.String("middleware", name),
			)
		}
	}

	return nil
}

// calculateInitOrder calculates the initialization order based on dependencies
func (m *Manager) calculateInitOrder() ([]string, error) {
	// Build dependency graph
	graph := make(map[string][]string)
	for name, middleware := range m.middleware {
		graph[name] = middleware.Dependencies()
	}

	// Topological sort using Kahn's algorithm
	inDegree := make(map[string]int)
	for name := range graph {
		inDegree[name] = 0
	}

	for _, deps := range graph {
		for _, dep := range deps {
			if _, exists := inDegree[dep]; exists {
				inDegree[dep]++
			}
		}
	}

	queue := make([]string, 0)
	for name, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, name)
		}
	}

	result := make([]string, 0)
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		result = append(result, current)

		for _, dep := range graph[current] {
			if degree, exists := inDegree[dep]; exists {
				inDegree[dep] = degree - 1
				if inDegree[dep] == 0 {
					queue = append(queue, dep)
				}
			}
		}
	}

	// Check for circular dependencies
	if len(result) != len(graph) {
		return nil, common.ErrCircularDependency([]string{"middleware dependencies"})
	}

	return result, nil
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
