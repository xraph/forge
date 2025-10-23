package monitoring

import (
	"context"
	"fmt"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/consensus"
	"github.com/xraph/forge/pkg/logger"
)

// ConsensusHealthChecker performs health checks on consensus components
type ConsensusHealthChecker struct {
	consensusManager *consensus.ConsensusManager
	logger           common.Logger
	metrics          common.Metrics
	config           ConsensusHealthConfig
}

// ConsensusHealthConfig contains configuration for consensus health checks
type ConsensusHealthConfig struct {
	CheckInterval          time.Duration `yaml:"check_interval" default:"30s"`
	Timeout                time.Duration `yaml:"timeout" default:"5s"`
	LeaderElectionTimeout  time.Duration `yaml:"leader_election_timeout" default:"30s"`
	NodeUnhealthyThreshold time.Duration `yaml:"node_unhealthy_threshold" default:"60s"`
	MinHealthyNodes        float64       `yaml:"min_healthy_nodes" default:"0.5"`
	EnableDetailedChecks   bool          `yaml:"enable_detailed_checks" default:"true"`
}

// NewConsensusHealthChecker creates a new consensus health checker
func NewConsensusHealthChecker(consensusManager *consensus.ConsensusManager, logger common.Logger, metrics common.Metrics, config ConsensusHealthConfig) *ConsensusHealthChecker {
	return &ConsensusHealthChecker{
		consensusManager: consensusManager,
		logger:           logger,
		metrics:          metrics,
		config:           config,
	}
}

// CheckConsensusHealth performs comprehensive health checks on the consensus system
func (chc *ConsensusHealthChecker) CheckConsensusHealth(ctx context.Context) *common.HealthResult {
	startTime := time.Now()

	result := &common.HealthResult{
		Name:      "consensus-system",
		Status:    common.HealthStatusHealthy,
		Message:   "Consensus system healthy",
		Details:   make(map[string]interface{}),
		Timestamp: startTime,
	}

	// Check if consensus manager is started
	if !chc.consensusManager.IsStarted() {
		result.Status = common.HealthStatusUnhealthy
		result.Message = "Consensus manager not started"
		result.Duration = time.Since(startTime)
		return result
	}

	// Get consensus statistics
	stats := chc.consensusManager.GetStats()
	result.Details["total_clusters"] = stats.Clusters
	result.Details["active_clusters"] = stats.ActiveClusters
	result.Details["total_nodes"] = stats.TotalNodes

	// Check cluster health
	clusterHealth := chc.checkClusterHealth(ctx)
	result.Details["cluster_health"] = clusterHealth

	// Determine overall health status
	unhealthyClusters := 0
	degradedClusters := 0

	for _, health := range clusterHealth {
		switch health.Status {
		case common.HealthStatusUnhealthy:
			unhealthyClusters++
		case common.HealthStatusDegraded:
			degradedClusters++
		}
	}

	// Set overall status based on cluster health
	if unhealthyClusters > 0 {
		result.Status = common.HealthStatusUnhealthy
		result.Message = fmt.Sprintf("Consensus system unhealthy: %d unhealthy clusters", unhealthyClusters)
	} else if degradedClusters > 0 {
		result.Status = common.HealthStatusDegraded
		result.Message = fmt.Sprintf("Consensus system degraded: %d degraded clusters", degradedClusters)
	}

	result.Duration = time.Since(startTime)

	// Record health check metrics
	if chc.metrics != nil {
		chc.metrics.Counter("forge.consensus.health_checks_total").Inc()
		chc.metrics.Histogram("forge.consensus.health_check_duration").Observe(result.Duration.Seconds())

		statusMetric := 0.0
		switch result.Status {
		case common.HealthStatusHealthy:
			statusMetric = 1.0
		case common.HealthStatusDegraded:
			statusMetric = 0.5
		case common.HealthStatusUnhealthy:
			statusMetric = 0.0
		}
		chc.metrics.Gauge("forge.consensus.health_status").Set(statusMetric)
	}

	return result
}

// checkClusterHealth checks the health of all clusters
func (chc *ConsensusHealthChecker) checkClusterHealth(ctx context.Context) map[string]*ClusterHealthResult {
	clusters := chc.consensusManager.GetClusters()
	clusterHealth := make(map[string]*ClusterHealthResult)

	for _, cluster := range clusters {
		health := chc.checkSingleClusterHealth(ctx, cluster)
		clusterHealth[cluster.ID()] = health
	}

	return clusterHealth
}

// ClusterHealthResult represents the health of a single cluster
type ClusterHealthResult struct {
	ClusterID     string                 `json:"cluster_id"`
	Status        common.HealthStatus    `json:"status"`
	Message       string                 `json:"message"`
	LeaderPresent bool                   `json:"leader_present"`
	LeaderID      string                 `json:"leader_id,omitempty"`
	TotalNodes    int                    `json:"total_nodes"`
	ActiveNodes   int                    `json:"active_nodes"`
	HealthyNodes  int                    `json:"healthy_nodes"`
	LastElection  time.Time              `json:"last_election"`
	NodeHealth    map[string]*NodeHealth `json:"node_health"`
	Issues        []string               `json:"issues"`
	CheckDuration time.Duration          `json:"check_duration"`
}

// NodeHealth represents the health of a single node
type NodeHealth struct {
	NodeID        string        `json:"node_id"`
	Status        string        `json:"status"`
	Role          string        `json:"role"`
	LastHeartbeat time.Time     `json:"last_heartbeat"`
	IsReachable   bool          `json:"is_reachable"`
	Latency       time.Duration `json:"latency"`
	Issues        []string      `json:"issues"`
}

// checkSingleClusterHealth checks the health of a single cluster
func (chc *ConsensusHealthChecker) checkSingleClusterHealth(ctx context.Context, cluster consensus.Cluster) *ClusterHealthResult {
	startTime := time.Now()

	result := &ClusterHealthResult{
		ClusterID:  cluster.ID(),
		Status:     common.HealthStatusHealthy,
		Message:    "Cluster healthy",
		NodeHealth: make(map[string]*NodeHealth),
		Issues:     make([]string, 0),
	}

	// Get cluster health from the cluster itself
	clusterHealth := cluster.GetHealth()
	result.LeaderPresent = clusterHealth.LeaderPresent
	result.TotalNodes = clusterHealth.TotalNodes
	result.ActiveNodes = clusterHealth.ActiveNodes
	result.LastElection = clusterHealth.LastElection

	// Get leader information
	if leader := cluster.GetLeader(); leader != nil {
		result.LeaderID = leader.ID()
	}

	// Check node health
	nodes := cluster.GetNodes()
	healthyNodes := 0

	for _, node := range nodes {
		nodeHealth := chc.checkNodeHealth(ctx, node)
		result.NodeHealth[node.ID()] = nodeHealth

		if nodeHealth.IsReachable {
			healthyNodes++
		}
	}

	result.HealthyNodes = healthyNodes

	// Determine cluster health status
	requiredNodes := (result.TotalNodes / 2) + 1
	healthyRatio := float64(healthyNodes) / float64(result.TotalNodes)

	if !result.LeaderPresent {
		result.Status = common.HealthStatusDegraded
		result.Message = "No leader present"
		result.Issues = append(result.Issues, "no_leader")

		// Check if leader election is taking too long
		if !result.LastElection.IsZero() && time.Since(result.LastElection) > chc.config.LeaderElectionTimeout {
			result.Status = common.HealthStatusUnhealthy
			result.Message = "Leader election timeout"
			result.Issues = append(result.Issues, "leader_election_timeout")
		}
	}

	if healthyNodes < requiredNodes {
		result.Status = common.HealthStatusUnhealthy
		result.Message = fmt.Sprintf("Insufficient healthy nodes: %d/%d (required: %d)", healthyNodes, result.TotalNodes, requiredNodes)
		result.Issues = append(result.Issues, "insufficient_nodes")
	} else if healthyRatio < chc.config.MinHealthyNodes {
		if result.Status == common.HealthStatusHealthy {
			result.Status = common.HealthStatusDegraded
			result.Message = fmt.Sprintf("Low healthy node ratio: %.2f", healthyRatio)
			result.Issues = append(result.Issues, "low_healthy_ratio")
		}
	}

	result.CheckDuration = time.Since(startTime)

	if chc.logger != nil {
		chc.logger.Debug("cluster health check completed",
			logger.String("cluster_id", cluster.ID()),
			logger.String("status", string(result.Status)),
			logger.Int("healthy_nodes", healthyNodes),
			logger.Int("total_nodes", result.TotalNodes),
			logger.Duration("duration", result.CheckDuration),
		)
	}

	return result
}

// checkNodeHealth checks the health of a single node
func (chc *ConsensusHealthChecker) checkNodeHealth(ctx context.Context, node consensus.Node) *NodeHealth {
	nodeHealth := &NodeHealth{
		NodeID:        node.ID(),
		Status:        string(node.Status()),
		Role:          string(node.Role()),
		LastHeartbeat: node.LastHeartbeat(),
		IsReachable:   true,
		Issues:        make([]string, 0),
	}

	// Check if node is responsive
	if chc.config.EnableDetailedChecks {
		pingStart := time.Now()
		if err := chc.pingNode(ctx, node); err != nil {
			nodeHealth.IsReachable = false
			nodeHealth.Issues = append(nodeHealth.Issues, fmt.Sprintf("ping_failed: %v", err))
		} else {
			nodeHealth.Latency = time.Since(pingStart)
		}
	}

	// Check heartbeat freshness
	if !nodeHealth.LastHeartbeat.IsZero() {
		heartbeatAge := time.Since(nodeHealth.LastHeartbeat)
		if heartbeatAge > chc.config.NodeUnhealthyThreshold {
			nodeHealth.IsReachable = false
			nodeHealth.Issues = append(nodeHealth.Issues, fmt.Sprintf("stale_heartbeat: %v", heartbeatAge))
		}
	}

	// Check node status
	switch node.Status() {
	case consensus.NodeStatusInactive, consensus.NodeStatusFailed:
		nodeHealth.IsReachable = false
		nodeHealth.Issues = append(nodeHealth.Issues, "node_inactive")
	case consensus.NodeStatusSuspected:
		nodeHealth.Issues = append(nodeHealth.Issues, "node_suspected")
	}

	return nodeHealth
}

// pingNode attempts to ping a node to check connectivity
func (chc *ConsensusHealthChecker) pingNode(ctx context.Context, node consensus.Node) error {
	// This is a simplified ping - in practice, you'd use the transport layer
	// to send a ping message and wait for a response

	// For now, we'll simulate a ping based on node status
	if node.Status() == consensus.NodeStatusActive {
		return nil
	}

	return fmt.Errorf("node not active: %s", node.Status())
}

// GetHealthSummary returns a summary of consensus system health
func (chc *ConsensusHealthChecker) GetHealthSummary() *ConsensusHealthSummary {
	clusters := chc.consensusManager.GetClusters()
	summary := &ConsensusHealthSummary{
		TotalClusters:     len(clusters),
		HealthyClusters:   0,
		DegradedClusters:  0,
		UnhealthyClusters: 0,
		TotalNodes:        0,
		HealthyNodes:      0,
		LastCheck:         time.Now(),
	}

	for _, cluster := range clusters {
		health := cluster.GetHealth()
		summary.TotalNodes += health.TotalNodes
		summary.HealthyNodes += health.ActiveNodes

		switch health.Status {
		case common.HealthStatusHealthy:
			summary.HealthyClusters++
		case common.HealthStatusDegraded:
			summary.DegradedClusters++
		case common.HealthStatusUnhealthy:
			summary.UnhealthyClusters++
		}
	}

	return summary
}

// ConsensusHealthSummary provides a high-level summary of consensus health
type ConsensusHealthSummary struct {
	TotalClusters     int       `json:"total_clusters"`
	HealthyClusters   int       `json:"healthy_clusters"`
	DegradedClusters  int       `json:"degraded_clusters"`
	UnhealthyClusters int       `json:"unhealthy_clusters"`
	TotalNodes        int       `json:"total_nodes"`
	HealthyNodes      int       `json:"healthy_nodes"`
	LastCheck         time.Time `json:"last_check"`
}

// StartPeriodicHealthChecks starts periodic health checks
func (chc *ConsensusHealthChecker) StartPeriodicHealthChecks(ctx context.Context) {
	ticker := time.NewTicker(chc.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			result := chc.CheckConsensusHealth(ctx)

			if chc.logger != nil {
				logLevel := chc.logger.Info
				if result.Status == common.HealthStatusUnhealthy {
					logLevel = chc.logger.Error
				} else if result.Status == common.HealthStatusDegraded {
					logLevel = chc.logger.Warn
				}

				logLevel("periodic consensus health check",
					logger.String("status", string(result.Status)),
					logger.String("message", result.Message),
					logger.Duration("check_duration", result.Duration),
				)
			}

		case <-ctx.Done():
			if chc.logger != nil {
				chc.logger.Info("stopping periodic consensus health checks")
			}
			return
		}
	}
}

// RegisterConsensusHealthChecks registers consensus health checks with the health checker
func RegisterConsensusHealthChecks(healthChecker common.HealthChecker, consensusManager *consensus.ConsensusManager, logger common.Logger, metrics common.Metrics) error {
	config := ConsensusHealthConfig{
		CheckInterval:          30 * time.Second,
		Timeout:                5 * time.Second,
		LeaderElectionTimeout:  30 * time.Second,
		NodeUnhealthyThreshold: 60 * time.Second,
		MinHealthyNodes:        0.5,
		EnableDetailedChecks:   true,
	}

	checker := NewConsensusHealthChecker(consensusManager, logger, metrics, config)

	// Register main consensus health check
	return healthChecker.RegisterCheck("consensus-system", checker)
}

func (chc *ConsensusHealthChecker) Name() string {
	return "consensus-system"
}

func (chc *ConsensusHealthChecker) Check(ctx context.Context) *common.HealthResult {
	return chc.CheckConsensusHealth(ctx)
}

func (chc *ConsensusHealthChecker) Timeout() time.Duration {
	return chc.config.Timeout
}

func (chc *ConsensusHealthChecker) Critical() bool {
	return true
}

func (chc *ConsensusHealthChecker) Dependencies() []string {
	return []string{}
}
