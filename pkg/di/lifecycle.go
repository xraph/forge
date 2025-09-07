package di

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// LifecycleManager manages the lifecycle of services
type LifecycleManager struct {
	container       *Container
	services        map[string]*ServiceLifecycleEntry
	dependencyGraph *DependencyGraph
	startOrder      []string
	stopOrder       []string
	mu              sync.RWMutex
	logger          common.Logger
	metrics         common.Metrics
}

// ServiceLifecycleEntry represents a service in the lifecycle manager
type ServiceLifecycleEntry struct {
	Name         string
	Service      common.Service
	Registration *ServiceRegistration
	Instance     interface{}
	State        ServiceState
	StartTime    time.Time
	StopTime     time.Time
	LastHealth   time.Time
	HealthStatus common.HealthStatus
	StartCount   int
	StopCount    int
	mu           sync.RWMutex
}

// ServiceState represents the state of a service
type ServiceState string

const (
	ServiceStateNotStarted ServiceState = "not_started"
	ServiceStateStarting   ServiceState = "starting"
	ServiceStateRunning    ServiceState = "running"
	ServiceStateStopping   ServiceState = "stopping"
	ServiceStateStopped    ServiceState = "stopped"
	ServiceStateError      ServiceState = "error"
)

// DependencyGraph represents the dependency relationships between services
type DependencyGraph struct {
	nodes map[string]*DependencyNode
	edges map[string][]string
	mu    sync.RWMutex
}

// DependencyNode represents a node in the dependency graph
type DependencyNode struct {
	Name         string
	Dependencies []string
	Dependents   []string
	Level        int
}

// NewLifecycleManager creates a new lifecycle manager
func NewLifecycleManager(container *Container) *LifecycleManager {
	return &LifecycleManager{
		container:       container,
		services:        make(map[string]*ServiceLifecycleEntry),
		dependencyGraph: NewDependencyGraph(),
		logger:          container.logger,
		metrics:         container.metrics,
	}
}

// AddService adds a service to the lifecycle manager
func (lm *LifecycleManager) AddService(service common.Service) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	serviceName := service.Name()
	if _, exists := lm.services[serviceName]; exists {
		return common.ErrServiceAlreadyExists(serviceName)
	}

	entry := &ServiceLifecycleEntry{
		Name:         serviceName,
		Service:      service,
		State:        ServiceStateNotStarted,
		HealthStatus: common.HealthStatusUnknown,
	}

	lm.services[serviceName] = entry

	// Add to dependency graph
	dependencies := service.Dependencies()
	if err := lm.dependencyGraph.AddNode(serviceName, dependencies); err != nil {
		delete(lm.services, serviceName)
		return err
	}

	// Recalculate start/stop order
	lm.calculateOrder()

	if lm.logger != nil {
		lm.logger.Info("service added to lifecycle manager",
			logger.String("service", serviceName),
			logger.String("dependencies", fmt.Sprintf("%v", dependencies)),
		)
	}

	return nil
}

// RemoveService removes a service from the lifecycle manager
func (lm *LifecycleManager) RemoveService(serviceName string) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	entry, exists := lm.services[serviceName]
	if !exists {
		return common.ErrServiceNotFound(serviceName)
	}

	// Check if service is running
	if entry.State == ServiceStateRunning {
		return common.ErrLifecycleError("remove_service", fmt.Errorf("cannot remove running service %s", serviceName))
	}

	// Remove from dependency graph
	if err := lm.dependencyGraph.RemoveNode(serviceName); err != nil {
		return err
	}

	delete(lm.services, serviceName)

	// Recalculate start/stop order
	lm.calculateOrder()

	if lm.logger != nil {
		lm.logger.Info("service removed from lifecycle manager",
			logger.String("service", serviceName),
		)
	}

	return nil
}

// Start starts the lifecycle manager
func (lm *LifecycleManager) Start(ctx context.Context) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	if lm.logger != nil {
		lm.logger.Info("lifecycle manager starting")
	}

	if lm.metrics != nil {
		lm.metrics.Counter("forge.di.lifecycle_manager_started").Inc()
	}

	return nil
}

// Stop stops the lifecycle manager
func (lm *LifecycleManager) Stop(ctx context.Context) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	if lm.logger != nil {
		lm.logger.Info("lifecycle manager stopping")
	}

	if lm.metrics != nil {
		lm.metrics.Counter("forge.di.lifecycle_manager_stopped").Inc()
	}

	return nil
}

// StartServices starts all services in dependency order
func (lm *LifecycleManager) StartServices(ctx context.Context) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	startTime := time.Now()

	if lm.logger != nil {
		lm.logger.Info("starting services",
			logger.Int("service_count", len(lm.services)),
			logger.String("start_order", fmt.Sprintf("%v", lm.startOrder)),
		)
	}

	// Start services in dependency order
	for _, serviceName := range lm.startOrder {
		entry, exists := lm.services[serviceName]
		if !exists {
			continue
		}

		if entry.State != ServiceStateNotStarted {
			continue
		}

		if err := lm.startService(ctx, entry); err != nil {
			// Stop already started services
			lm.stopStartedServices(ctx)
			return err
		}
	}

	if lm.logger != nil {
		lm.logger.Info("all services started",
			logger.Duration("startup_time", time.Since(startTime)),
		)
	}

	if lm.metrics != nil {
		lm.metrics.Counter("forge.di.services_started").Add(float64(len(lm.services)))
		lm.metrics.Histogram("forge.di.services_startup_time").Observe(time.Since(startTime).Seconds())
	}

	return nil
}

// StopServices stops all services in reverse dependency order
func (lm *LifecycleManager) StopServices(ctx context.Context) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	stopTime := time.Now()

	if lm.logger != nil {
		lm.logger.Info("stopping services",
			logger.Int("service_count", len(lm.services)),
			logger.String("stop_order", fmt.Sprintf("%v", lm.stopOrder)),
		)
	}

	// Stop services in reverse dependency order
	for _, serviceName := range lm.stopOrder {
		entry, exists := lm.services[serviceName]
		if !exists {
			continue
		}

		if entry.State != ServiceStateRunning {
			continue
		}

		if err := lm.stopService(ctx, entry); err != nil {
			if lm.logger != nil {
				lm.logger.Error("failed to stop service",
					logger.String("service", serviceName),
					logger.Error(err),
				)
			}
		}
	}

	if lm.logger != nil {
		lm.logger.Info("all services stopped",
			logger.Duration("shutdown_time", time.Since(stopTime)),
		)
	}

	if lm.metrics != nil {
		lm.metrics.Counter("forge.di.services_stopped").Add(float64(len(lm.services)))
		lm.metrics.Histogram("forge.di.services_shutdown_time").Observe(time.Since(stopTime).Seconds())
	}

	return nil
}

// HealthCheck performs health checks on all services
func (lm *LifecycleManager) HealthCheck(ctx context.Context) error {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	var errors []error

	for _, entry := range lm.services {
		if entry.State != ServiceStateRunning {
			continue
		}

		if err := lm.healthCheckService(ctx, entry); err != nil {
			errors = append(errors, err)
		}
	}

	if len(errors) > 0 {
		return common.ErrHealthCheckFailed("services", fmt.Errorf("health check failed for %d services", len(errors)))
	}

	return nil
}

// GetService returns a service by name
func (lm *LifecycleManager) GetService(name string) (common.Service, error) {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	entry, exists := lm.services[name]
	if !exists {
		return nil, common.ErrServiceNotFound(name)
	}

	return entry.Service, nil
}

// GetServices returns all services
func (lm *LifecycleManager) GetServices() []common.Service {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	services := make([]common.Service, 0, len(lm.services))
	for _, entry := range lm.services {
		services = append(services, entry.Service)
	}

	return services
}

// GetServiceState returns the state of a service
func (lm *LifecycleManager) GetServiceState(serviceName string) (ServiceState, error) {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	entry, exists := lm.services[serviceName]
	if !exists {
		return ServiceStateNotStarted, common.ErrServiceNotFound(serviceName)
	}

	entry.mu.RLock()
	defer entry.mu.RUnlock()
	return entry.State, nil
}

// GetServiceHealth returns the health status of a service
func (lm *LifecycleManager) GetServiceHealth(serviceName string) (common.HealthStatus, error) {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	entry, exists := lm.services[serviceName]
	if !exists {
		return common.HealthStatusUnknown, common.ErrServiceNotFound(serviceName)
	}

	entry.mu.RLock()
	defer entry.mu.RUnlock()
	return entry.HealthStatus, nil
}

// GetServiceStats returns statistics for a service
func (lm *LifecycleManager) GetServiceStats(serviceName string) (*ServiceStats, error) {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	entry, exists := lm.services[serviceName]
	if !exists {
		return nil, common.ErrServiceNotFound(serviceName)
	}

	entry.mu.RLock()
	defer entry.mu.RUnlock()

	return &ServiceStats{
		Name:         entry.Name,
		State:        entry.State,
		HealthStatus: entry.HealthStatus,
		StartTime:    entry.StartTime,
		StopTime:     entry.StopTime,
		LastHealth:   entry.LastHealth,
		StartCount:   entry.StartCount,
		StopCount:    entry.StopCount,
		Uptime:       lm.calculateUptime(entry),
	}, nil
}

// GetAllServiceStats returns statistics for all services
func (lm *LifecycleManager) GetAllServiceStats() map[string]*ServiceStats {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	stats := make(map[string]*ServiceStats)
	for name, entry := range lm.services {
		entry.mu.RLock()
		stats[name] = &ServiceStats{
			Name:         entry.Name,
			State:        entry.State,
			HealthStatus: entry.HealthStatus,
			StartTime:    entry.StartTime,
			StopTime:     entry.StopTime,
			LastHealth:   entry.LastHealth,
			StartCount:   entry.StartCount,
			StopCount:    entry.StopCount,
			Uptime:       lm.calculateUptime(entry),
		}
		entry.mu.RUnlock()
	}

	return stats
}

// startService starts a single service
func (lm *LifecycleManager) startService(ctx context.Context, entry *ServiceLifecycleEntry) error {
	entry.mu.Lock()
	defer entry.mu.Unlock()

	if entry.State != ServiceStateNotStarted {
		return nil
	}

	entry.State = ServiceStateStarting
	entry.StartTime = time.Now()

	if lm.logger != nil {
		lm.logger.Info("starting service",
			logger.String("service", entry.Name),
		)
	}

	// todo!: remove forced failure for duplicate services
	// // Start the service
	// if err := entry.Service.OnStart(ctx); err != nil {
	// 	entry.State = ServiceStateError
	// 	return common.ErrServiceStartFailed(entry.Name, err)
	// }

	// Start the service
	if err := entry.Service.OnStart(ctx); err != nil {
		// Check if it's a "service already exists" error
		var forgeErr *common.ForgeError
		if errors.As(err, &forgeErr) && forgeErr.Code == common.ErrCodeServiceAlreadyExists {
			// Treat as already started - just log a warning and continue
			entry.State = ServiceStateRunning
			entry.StartCount++

			if lm.logger != nil {
				lm.logger.Warn("service already started, continuing",
					logger.String("service", entry.Name),
					logger.String("error", err.Error()),
				)
			}

			return nil
		}

		// For all other errors, fail as before
		entry.State = ServiceStateError
		return common.ErrServiceStartFailed(entry.Name, err)
	}

	entry.State = ServiceStateRunning
	entry.StartCount++

	if lm.logger != nil {
		lm.logger.Info("service started",
			logger.String("service", entry.Name),
			logger.Duration("startup_time", time.Since(entry.StartTime)),
		)
	}

	if lm.metrics != nil {
		lm.metrics.Counter("forge.di.service_started").Inc()
		lm.metrics.Histogram("forge.di.service_startup_time").Observe(time.Since(entry.StartTime).Seconds())
	}

	return nil
}

// stopService stops a single service
func (lm *LifecycleManager) stopService(ctx context.Context, entry *ServiceLifecycleEntry) error {
	entry.mu.Lock()
	defer entry.mu.Unlock()

	if entry.State != ServiceStateRunning {
		return nil
	}

	entry.State = ServiceStateStopping
	entry.StopTime = time.Now()

	if lm.logger != nil {
		lm.logger.Info("stopping service",
			logger.String("service", entry.Name),
		)
	}

	// Stop the service
	if err := entry.Service.OnStop(ctx); err != nil {
		entry.State = ServiceStateError
		return common.ErrServiceStopFailed(entry.Name, err)
	}

	entry.State = ServiceStateStopped
	entry.StopCount++

	if lm.logger != nil {
		lm.logger.Info("service stopped",
			logger.String("service", entry.Name),
			logger.Duration("shutdown_time", time.Since(entry.StopTime)),
		)
	}

	if lm.metrics != nil {
		lm.metrics.Counter("forge.di.service_stopped").Inc()
		lm.metrics.Histogram("forge.di.service_shutdown_time").Observe(time.Since(entry.StopTime).Seconds())
	}

	return nil
}

// healthCheckService performs a health check on a single service
func (lm *LifecycleManager) healthCheckService(ctx context.Context, entry *ServiceLifecycleEntry) error {
	entry.mu.Lock()
	defer entry.mu.Unlock()

	entry.LastHealth = time.Now()

	if err := entry.Service.OnHealthCheck(ctx); err != nil {
		entry.HealthStatus = common.HealthStatusUnhealthy
		return common.ErrHealthCheckFailed(entry.Name, err)
	}

	entry.HealthStatus = common.HealthStatusHealthy
	return nil
}

// stopStartedServices stops all services that have been started
func (lm *LifecycleManager) stopStartedServices(ctx context.Context) {
	for i := len(lm.stopOrder) - 1; i >= 0; i-- {
		serviceName := lm.stopOrder[i]
		entry, exists := lm.services[serviceName]
		if !exists {
			continue
		}

		if entry.State == ServiceStateRunning {
			if err := lm.stopService(ctx, entry); err != nil {
				if lm.logger != nil {
					lm.logger.Error("failed to stop service during cleanup",
						logger.String("service", serviceName),
						logger.Error(err),
					)
				}
			}
		}
	}
}

// calculateOrder calculates the start and stop order for services
func (lm *LifecycleManager) calculateOrder() {
	lm.startOrder = lm.dependencyGraph.GetTopologicalOrder()
	lm.stopOrder = make([]string, len(lm.startOrder))

	// Reverse the start order for stop order
	for i, j := 0, len(lm.startOrder)-1; i < j; i, j = i+1, j-1 {
		lm.stopOrder[i], lm.stopOrder[j] = lm.startOrder[j], lm.startOrder[i]
	}
}

// calculateUptime calculates the uptime of a service
func (lm *LifecycleManager) calculateUptime(entry *ServiceLifecycleEntry) time.Duration {
	if entry.State == ServiceStateRunning {
		return time.Since(entry.StartTime)
	}
	if !entry.StopTime.IsZero() && !entry.StartTime.IsZero() {
		return entry.StopTime.Sub(entry.StartTime)
	}
	return 0
}

// ServiceStats contains statistics about a service
type ServiceStats struct {
	Name         string              `json:"name"`
	State        ServiceState        `json:"state"`
	HealthStatus common.HealthStatus `json:"health_status"`
	StartTime    time.Time           `json:"start_time"`
	StopTime     time.Time           `json:"stop_time"`
	LastHealth   time.Time           `json:"last_health"`
	StartCount   int                 `json:"start_count"`
	StopCount    int                 `json:"stop_count"`
	Uptime       time.Duration       `json:"uptime"`
}

// NewDependencyGraph creates a new dependency graph
func NewDependencyGraph() *DependencyGraph {
	return &DependencyGraph{
		nodes: make(map[string]*DependencyNode),
		edges: make(map[string][]string),
	}
}

// AddNode adds a node to the dependency graph
func (dg *DependencyGraph) AddNode(name string, dependencies []string) error {
	dg.mu.Lock()
	defer dg.mu.Unlock()

	if _, exists := dg.nodes[name]; exists {
		return common.ErrServiceAlreadyExists(name)
	}

	node := &DependencyNode{
		Name:         name,
		Dependencies: dependencies,
		Dependents:   make([]string, 0),
	}

	dg.nodes[name] = node
	dg.edges[name] = dependencies

	// Update dependents
	for _, dep := range dependencies {
		if depNode, exists := dg.nodes[dep]; exists {
			depNode.Dependents = append(depNode.Dependents, name)
		}
	}

	// Check for circular dependencies
	if err := dg.checkCircularDependencies(); err != nil {
		// Rollback
		delete(dg.nodes, name)
		delete(dg.edges, name)
		return err
	}

	return nil
}

// RemoveNode removes a node from the dependency graph
func (dg *DependencyGraph) RemoveNode(name string) error {
	dg.mu.Lock()
	defer dg.mu.Unlock()

	node, exists := dg.nodes[name]
	if !exists {
		return common.ErrServiceNotFound(name)
	}

	// Remove from dependents
	for _, dep := range node.Dependencies {
		if depNode, exists := dg.nodes[dep]; exists {
			for i, dependent := range depNode.Dependents {
				if dependent == name {
					depNode.Dependents = append(depNode.Dependents[:i], depNode.Dependents[i+1:]...)
					break
				}
			}
		}
	}

	delete(dg.nodes, name)
	delete(dg.edges, name)

	return nil
}

// GetTopologicalOrder returns the topological order of nodes
func (dg *DependencyGraph) GetTopologicalOrder() []string {
	dg.mu.RLock()
	defer dg.mu.RUnlock()

	// Kahn's algorithm for topological sorting
	inDegree := make(map[string]int)
	for name := range dg.nodes {
		inDegree[name] = 0
	}

	for _, dependencies := range dg.edges {
		for _, dep := range dependencies {
			inDegree[dep]++
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

		for _, dep := range dg.edges[current] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}

	return result
}

// checkCircularDependencies checks for circular dependencies
func (dg *DependencyGraph) checkCircularDependencies() error {
	visited := make(map[string]bool)
	recStack := make(map[string]bool)

	for name := range dg.nodes {
		if !visited[name] {
			if path := dg.hasCycle(name, visited, recStack, []string{}); path != nil {
				return common.ErrCircularDependency(path)
			}
		}
	}

	return nil
}

// hasCycle checks if there's a cycle starting from the given node
func (dg *DependencyGraph) hasCycle(name string, visited, recStack map[string]bool, path []string) []string {
	visited[name] = true
	recStack[name] = true
	path = append(path, name)

	for _, dep := range dg.edges[name] {
		if !visited[dep] {
			if cyclePath := dg.hasCycle(dep, visited, recStack, path); cyclePath != nil {
				return cyclePath
			}
		} else if recStack[dep] {
			// Found cycle
			cycleStart := -1
			for i, node := range path {
				if node == dep {
					cycleStart = i
					break
				}
			}
			if cycleStart != -1 {
				return append(path[cycleStart:], dep)
			}
		}
	}

	recStack[name] = false
	return nil
}

// GetDependencies returns the dependencies of a node
func (dg *DependencyGraph) GetDependencies(name string) []string {
	dg.mu.RLock()
	defer dg.mu.RUnlock()

	if node, exists := dg.nodes[name]; exists {
		return node.Dependencies
	}
	return nil
}

// GetDependents returns the dependents of a node
func (dg *DependencyGraph) GetDependents(name string) []string {
	dg.mu.RLock()
	defer dg.mu.RUnlock()

	if node, exists := dg.nodes[name]; exists {
		return node.Dependents
	}
	return nil
}

// GetNodes returns all nodes in the graph
func (dg *DependencyGraph) GetNodes() map[string]*DependencyNode {
	dg.mu.RLock()
	defer dg.mu.RUnlock()

	nodes := make(map[string]*DependencyNode)
	for name, node := range dg.nodes {
		nodes[name] = node
	}
	return nodes
}
