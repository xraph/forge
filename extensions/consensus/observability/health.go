package observability

import (
	"context"
	"fmt"
	"maps"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// HealthChecker performs comprehensive health checks.
type HealthChecker struct {
	logger forge.Logger
	checks map[string]HealthCheckFunc
	mu     sync.RWMutex

	// Dependencies
	raftNode       internal.RaftNode
	clusterManager internal.ClusterManager
	storage        internal.Storage
	transport      internal.Transport

	// Configuration
	timeout time.Duration
}

// HealthCheckFunc is a function that performs a health check.
type HealthCheckFunc func(ctx context.Context) internal.HealthCheck

// HealthCheckerConfig contains health checker configuration.
type HealthCheckerConfig struct {
	Timeout time.Duration
}

// NewHealthChecker creates a new health checker.
func NewHealthChecker(
	config HealthCheckerConfig,
	logger forge.Logger,
	raftNode internal.RaftNode,
	clusterManager internal.ClusterManager,
	storage internal.Storage,
	transport internal.Transport,
) *HealthChecker {
	if config.Timeout == 0 {
		config.Timeout = 5 * time.Second
	}

	hc := &HealthChecker{
		logger:         logger,
		checks:         make(map[string]HealthCheckFunc),
		raftNode:       raftNode,
		clusterManager: clusterManager,
		storage:        storage,
		transport:      transport,
		timeout:        config.Timeout,
	}

	// Register default health checks
	hc.registerDefaultChecks()

	return hc
}

// registerDefaultChecks registers the default health checks.
func (hc *HealthChecker) registerDefaultChecks() {
	hc.RegisterCheck("raft_node", hc.checkRaftNode)
	hc.RegisterCheck("quorum", hc.checkQuorum)
	hc.RegisterCheck("storage", hc.checkStorage)
	hc.RegisterCheck("transport", hc.checkTransport)
	hc.RegisterCheck("replication", hc.checkReplication)
}

// RegisterCheck registers a custom health check.
func (hc *HealthChecker) RegisterCheck(name string, fn HealthCheckFunc) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	hc.checks[name] = fn
}

// UnregisterCheck unregisters a health check.
func (hc *HealthChecker) UnregisterCheck(name string) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	delete(hc.checks, name)
}

// Check performs all health checks.
func (hc *HealthChecker) Check(ctx context.Context) internal.HealthStatus {
	hc.mu.RLock()

	checks := make(map[string]HealthCheckFunc, len(hc.checks))
	maps.Copy(checks, hc.checks)

	hc.mu.RUnlock()

	// Run all checks concurrently
	results := make([]internal.HealthCheck, 0, len(checks))
	resultCh := make(chan internal.HealthCheck, len(checks))

	for name, fn := range checks {
		go func(checkName string, checkFn HealthCheckFunc) {
			checkCtx, cancel := context.WithTimeout(ctx, hc.timeout)
			defer cancel()

			result := checkFn(checkCtx)
			resultCh <- result
		}(name, fn)
	}

	// Collect results
	for range len(checks) {
		result := <-resultCh
		results = append(results, result)
	}

	// Aggregate results
	return hc.aggregateResults(results)
}

// aggregateResults aggregates health check results.
func (hc *HealthChecker) aggregateResults(results []internal.HealthCheck) internal.HealthStatus {
	now := time.Now()
	healthy := true
	status := "healthy"

	// Check if any check failed
	for _, result := range results {
		if !result.Healthy {
			healthy = false
			status = "unhealthy"

			break
		}
	}

	// Get cluster info
	isLeader := false
	hasQuorum := false
	totalNodes := 0
	activeNodes := 0

	if hc.raftNode != nil {
		isLeader = hc.raftNode.IsLeader()
	}

	if hc.clusterManager != nil {
		hasQuorum = hc.clusterManager.HasQuorum()
		totalNodes = hc.clusterManager.GetClusterSize()
		activeNodes = hc.clusterManager.GetHealthyNodes()
	}

	// Adjust status based on cluster state
	if !hasQuorum {
		status = "degraded"
		if status == "healthy" {
			healthy = false
		}
	}

	return internal.HealthStatus{
		Healthy:     healthy,
		Status:      status,
		Leader:      isLeader,
		HasQuorum:   hasQuorum,
		TotalNodes:  totalNodes,
		ActiveNodes: activeNodes,
		Details:     results,
		LastCheck:   now,
		Checks:      make(map[string]any),
	}
}

// checkRaftNode checks Raft node health.
func (hc *HealthChecker) checkRaftNode(ctx context.Context) internal.HealthCheck {
	now := time.Now()

	if hc.raftNode == nil {
		return internal.HealthCheck{
			Name:      "raft_node",
			Healthy:   false,
			Message:   "Raft node not initialized",
			CheckedAt: now,
		}
	}

	// Check if node has a valid term
	stats := hc.raftNode.GetStats()
	if stats.Term == 0 {
		return internal.HealthCheck{
			Name:      "raft_node",
			Healthy:   false,
			Message:   "Raft node has invalid term",
			CheckedAt: now,
		}
	}

	// Check if node is in a valid role
	role := hc.raftNode.GetRole()
	if role != internal.RoleLeader && role != internal.RoleFollower && role != internal.RoleCandidate {
		return internal.HealthCheck{
			Name:      "raft_node",
			Healthy:   false,
			Message:   fmt.Sprintf("Invalid role: %s", role),
			CheckedAt: now,
		}
	}

	return internal.HealthCheck{
		Name:      "raft_node",
		Healthy:   true,
		Message:   fmt.Sprintf("Raft node healthy (role: %s, term: %d)", role, stats.Term),
		CheckedAt: now,
	}
}

// checkQuorum checks if cluster has quorum.
func (hc *HealthChecker) checkQuorum(ctx context.Context) internal.HealthCheck {
	now := time.Now()

	if hc.clusterManager == nil {
		return internal.HealthCheck{
			Name:      "quorum",
			Healthy:   false,
			Message:   "Cluster manager not initialized",
			CheckedAt: now,
		}
	}

	hasQuorum := hc.clusterManager.HasQuorum()
	if !hasQuorum {
		totalNodes := hc.clusterManager.GetClusterSize()
		healthyNodes := hc.clusterManager.GetHealthyNodes()

		return internal.HealthCheck{
			Name:      "quorum",
			Healthy:   false,
			Message:   fmt.Sprintf("No quorum (healthy: %d/%d)", healthyNodes, totalNodes),
			CheckedAt: now,
		}
	}

	return internal.HealthCheck{
		Name:      "quorum",
		Healthy:   true,
		Message:   "Cluster has quorum",
		CheckedAt: now,
	}
}

// checkStorage checks storage health.
func (hc *HealthChecker) checkStorage(ctx context.Context) internal.HealthCheck {
	now := time.Now()

	if hc.storage == nil {
		return internal.HealthCheck{
			Name:      "storage",
			Healthy:   true,
			Message:   "Storage not configured",
			CheckedAt: now,
		}
	}

	// Try a simple write and read
	testKey := []byte("_health_check_")
	testValue := []byte("ok")

	if err := hc.storage.Set(testKey, testValue); err != nil {
		return internal.HealthCheck{
			Name:      "storage",
			Healthy:   false,
			Message:   fmt.Sprintf("Storage write failed: %v", err),
			Error:     err.Error(),
			CheckedAt: now,
		}
	}

	if _, err := hc.storage.Get(testKey); err != nil {
		return internal.HealthCheck{
			Name:      "storage",
			Healthy:   false,
			Message:   fmt.Sprintf("Storage read failed: %v", err),
			Error:     err.Error(),
			CheckedAt: now,
		}
	}

	// Clean up test key
	hc.storage.Delete(testKey)

	return internal.HealthCheck{
		Name:      "storage",
		Healthy:   true,
		Message:   "Storage operational",
		CheckedAt: now,
	}
}

// checkTransport checks transport health.
func (hc *HealthChecker) checkTransport(ctx context.Context) internal.HealthCheck {
	now := time.Now()

	if hc.transport == nil {
		return internal.HealthCheck{
			Name:      "transport",
			Healthy:   true,
			Message:   "Transport not configured",
			CheckedAt: now,
		}
	}

	// Check if transport is reachable
	address := hc.transport.GetAddress()
	if address == "" {
		return internal.HealthCheck{
			Name:      "transport",
			Healthy:   false,
			Message:   "Transport has no address",
			CheckedAt: now,
		}
	}

	return internal.HealthCheck{
		Name:      "transport",
		Healthy:   true,
		Message:   fmt.Sprintf("Transport operational (address: %s)", address),
		CheckedAt: now,
	}
}

// checkReplication checks replication health.
func (hc *HealthChecker) checkReplication(ctx context.Context) internal.HealthCheck {
	now := time.Now()

	if hc.raftNode == nil {
		return internal.HealthCheck{
			Name:      "replication",
			Healthy:   false,
			Message:   "Raft node not initialized",
			CheckedAt: now,
		}
	}

	stats := hc.raftNode.GetStats()

	// Check if commitIndex is advancing
	// In a real implementation, we'd track previous values and check for progress
	if stats.CommitIndex > 0 && stats.LastApplied > 0 {
		lag := stats.CommitIndex - stats.LastApplied
		if lag > 1000 {
			return internal.HealthCheck{
				Name:      "replication",
				Healthy:   false,
				Message:   fmt.Sprintf("High replication lag: %d entries", lag),
				CheckedAt: now,
			}
		}
	}

	return internal.HealthCheck{
		Name:      "replication",
		Healthy:   true,
		Message:   fmt.Sprintf("Replication healthy (commit: %d, applied: %d)", stats.CommitIndex, stats.LastApplied),
		CheckedAt: now,
	}
}

// GetHealthStatus returns the current health status.
func (hc *HealthChecker) GetHealthStatus(ctx context.Context) internal.HealthStatus {
	return hc.Check(ctx)
}

// IsHealthy returns true if the system is healthy.
func (hc *HealthChecker) IsHealthy(ctx context.Context) bool {
	status := hc.Check(ctx)

	return status.Healthy
}
