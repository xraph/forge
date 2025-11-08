package lb

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// HealthChecker performs health checks on nodes.
type HealthChecker interface {
	// Check performs health check
	Check(ctx context.Context, node *NodeInfo) error

	// Start begins periodic health checks
	Start(ctx context.Context)

	// Stop stops health checks
	Stop(ctx context.Context)

	// RegisterNode adds node to health monitoring
	RegisterNode(node *NodeInfo)

	// UnregisterNode removes node from health monitoring
	UnregisterNode(nodeID string)
}

// healthChecker implements HealthChecker.
type healthChecker struct {
	config   HealthCheckConfig
	nodes    map[string]*nodeHealth
	mu       sync.RWMutex
	balancer LoadBalancer
	stopCh   chan struct{}
	running  bool

	// HTTP client for health checks
	httpClient *http.Client
}

type nodeHealth struct {
	node              *NodeInfo
	consecutiveFails  int
	consecutivePasses int
}

// NewHealthChecker creates a health checker.
func NewHealthChecker(config HealthCheckConfig, balancer LoadBalancer) HealthChecker {
	return &healthChecker{
		config:   config,
		nodes:    make(map[string]*nodeHealth),
		balancer: balancer,
		stopCh:   make(chan struct{}),
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
	}
}

// Check performs single health check.
func (hc *healthChecker) Check(ctx context.Context, node *NodeInfo) error {
	// Build health check URL
	url := fmt.Sprintf("http://%s:%d/health", node.Address, node.Port)

	// Create request with context
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create health check request: %w", err)
	}

	// Perform request
	resp, err := hc.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("health check request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unhealthy status code: %d", resp.StatusCode)
	}

	return nil
}

// Start begins periodic health checks.
func (hc *healthChecker) Start(ctx context.Context) {
	hc.mu.Lock()

	if hc.running {
		hc.mu.Unlock()

		return
	}

	hc.running = true
	hc.stopCh = make(chan struct{})
	hc.mu.Unlock()

	ticker := time.NewTicker(hc.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-hc.stopCh:
			return
		case <-ticker.C:
			hc.performHealthChecks(ctx)
		}
	}
}

// Stop stops health checks.
func (hc *healthChecker) Stop(ctx context.Context) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	if !hc.running {
		return
	}

	close(hc.stopCh)
	hc.running = false
}

// RegisterNode adds node to monitoring.
func (hc *healthChecker) RegisterNode(node *NodeInfo) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	hc.nodes[node.ID] = &nodeHealth{
		node:              node,
		consecutiveFails:  0,
		consecutivePasses: 0,
	}
}

// UnregisterNode removes node from monitoring.
func (hc *healthChecker) UnregisterNode(nodeID string) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	delete(hc.nodes, nodeID)
}

func (hc *healthChecker) performHealthChecks(ctx context.Context) {
	hc.mu.RLock()

	nodes := make([]*nodeHealth, 0, len(hc.nodes))
	for _, nh := range hc.nodes {
		nodes = append(nodes, nh)
	}

	hc.mu.RUnlock()

	// Check each node
	for _, nh := range nodes {
		hc.checkNode(ctx, nh)
	}
}

func (hc *healthChecker) checkNode(ctx context.Context, nh *nodeHealth) {
	// Create timeout context
	checkCtx, cancel := context.WithTimeout(ctx, hc.config.Timeout)
	defer cancel()

	// Perform check
	err := hc.Check(checkCtx, nh.node)

	hc.mu.Lock()
	defer hc.mu.Unlock()

	if err != nil {
		// Health check failed
		nh.consecutiveFails++
		nh.consecutivePasses = 0

		// Mark unhealthy if threshold reached
		if nh.consecutiveFails >= hc.config.FailThreshold && nh.node.Healthy {
			nh.node.Healthy = false
			nh.node.FailureCount++

			// Update in balancer (method doesn't return error)
			if updater, ok := hc.balancer.(interface{ UpdateNodeHealth(string, bool) }); ok {
				updater.UpdateNodeHealth(nh.node.ID, false)
			}
		}
	} else {
		// Health check passed
		nh.consecutivePasses++
		nh.consecutiveFails = 0

		// Mark healthy if threshold reached
		if nh.consecutivePasses >= hc.config.PassThreshold && !nh.node.Healthy {
			nh.node.Healthy = true
			nh.node.FailureCount = 0

			// Update in balancer (method doesn't return error)
			if updater, ok := hc.balancer.(interface{ UpdateNodeHealth(string, bool) }); ok {
				updater.UpdateNodeHealth(nh.node.ID, true)
			}
		}
	}

	nh.node.LastHealthCheck = time.Now()
}

// GetNodeHealth returns health info for a node.
func (hc *healthChecker) GetNodeHealth(nodeID string) (*NodeHealth, error) {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	nh, ok := hc.nodes[nodeID]
	if !ok {
		return nil, fmt.Errorf("node not found: %s", nodeID)
	}

	return &NodeHealth{
		NodeID:            nh.node.ID,
		Healthy:           nh.node.Healthy,
		LastCheck:         nh.node.LastHealthCheck,
		ConsecutiveFails:  nh.consecutiveFails,
		ConsecutivePasses: nh.consecutivePasses,
		TotalFailures:     nh.node.FailureCount,
	}, nil
}

// NodeHealth represents node health status.
type NodeHealth struct {
	NodeID            string
	Healthy           bool
	LastCheck         time.Time
	ConsecutiveFails  int
	ConsecutivePasses int
	TotalFailures     int
}

// DefaultHealthCheckConfig returns default health check configuration.
func DefaultHealthCheckConfig() HealthCheckConfig {
	return HealthCheckConfig{
		Enabled:       true,
		Interval:      10 * time.Second,
		Timeout:       5 * time.Second,
		FailThreshold: 3,
		PassThreshold: 2,
	}
}
