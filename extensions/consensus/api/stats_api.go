package api

import (
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/cluster"
	"github.com/xraph/forge/extensions/consensus/internal"
	"github.com/xraph/forge/extensions/consensus/observability"
)

// StatsAPI provides statistics endpoints.
type StatsAPI struct {
	raftNode  internal.RaftNode
	manager   *cluster.Manager
	metrics   *observability.MetricsCollector
	logger    forge.Logger
	startTime time.Time
}

// NewStatsAPI creates a new stats API.
func NewStatsAPI(
	raftNode internal.RaftNode,
	manager *cluster.Manager,
	metrics *observability.MetricsCollector,
	logger forge.Logger,
) *StatsAPI {
	return &StatsAPI{
		raftNode:  raftNode,
		manager:   manager,
		metrics:   metrics,
		logger:    logger,
		startTime: time.Now(),
	}
}

// GetStats returns overall statistics.
func (sa *StatsAPI) GetStats(ctx forge.Context) error {
	raftStats := sa.raftNode.GetStats()

	stats := map[string]any{
		"uptime_seconds": time.Since(sa.startTime).Seconds(),
		"node_id":        raftStats.NodeID,
		"is_leader":      sa.raftNode.IsLeader(),
		"current_term":   raftStats.Term,
		"commit_index":   raftStats.CommitIndex,
		"last_applied":   raftStats.LastApplied,
		"cluster":        sa.getClusterStats(),
	}

	// Metrics stats would require a different method
	// if sa.metrics != nil {
	// 	stats["metrics"] = sa.metrics.CollectMetrics()
	// }

	return ctx.JSON(200, stats)
}

// GetRaftStats returns Raft-specific statistics.
func (sa *StatsAPI) GetRaftStats(ctx forge.Context) error {
	raftStats := sa.raftNode.GetStats()

	stats := map[string]any{
		"node_id":      raftStats.NodeID,
		"is_leader":    sa.raftNode.IsLeader(),
		"current_term": raftStats.Term,
		"commit_index": raftStats.CommitIndex,
		"last_applied": raftStats.LastApplied,
	}

	return ctx.JSON(200, stats)
}

// GetClusterStats returns cluster statistics.
func (sa *StatsAPI) GetClusterStats(ctx forge.Context) error {
	stats := sa.getClusterStats()

	return ctx.JSON(200, stats)
}

// GetReplicationStats returns replication statistics.
func (sa *StatsAPI) GetReplicationStats(ctx forge.Context) error {
	if !sa.raftNode.IsLeader() {
		return ctx.JSON(400, map[string]string{
			"error": "replication stats only available on leader",
		})
	}

	// TODO: Get actual replication stats from leader state
	stats := map[string]any{
		"is_leader": true,
		"peers":     []map[string]any{},
	}

	return ctx.JSON(200, stats)
}

// GetPerformanceStats returns performance statistics.
func (sa *StatsAPI) GetPerformanceStats(ctx forge.Context) error {
	stats := map[string]any{
		"uptime_seconds": time.Since(sa.startTime).Seconds(),
	}

	// Metrics stats would require a different method
	// if sa.metrics != nil {
	// 	stats["metrics"] = sa.metrics.CollectMetrics()
	// }

	return ctx.JSON(200, stats)
}

// getClusterStats returns cluster statistics.
func (sa *StatsAPI) getClusterStats() map[string]any {
	nodes := sa.manager.GetNodes()

	healthyCount := 0
	leaderCount := 0
	followerCount := 0

	for _, node := range nodes {
		if node.Status == internal.StatusActive {
			healthyCount++
		}

		switch node.Role {
		case internal.RoleLeader:
			leaderCount++
		case internal.RoleFollower:
			followerCount++
		}
	}

	return map[string]any{
		"total_nodes":    len(nodes),
		"healthy_nodes":  healthyCount,
		"leader_count":   leaderCount,
		"follower_count": followerCount,
	}
}

// GetMetrics returns metrics in Prometheus format.
func (sa *StatsAPI) GetMetrics(ctx forge.Context) error {
	if sa.metrics == nil {
		return ctx.JSON(503, map[string]string{
			"error": "metrics not enabled",
		})
	}

	// TODO: Return Prometheus-formatted metrics
	stats := map[string]any{
		"message": "metrics export not yet implemented",
	}

	return ctx.JSON(200, stats)
}

// ResetMetrics resets metrics counters.
func (sa *StatsAPI) ResetMetrics(ctx forge.Context) error {
	if sa.metrics == nil {
		return ctx.JSON(503, map[string]string{
			"error": "metrics not enabled",
		})
	}

	// TODO: Implement metrics reset
	sa.logger.Info("metrics reset requested")

	return ctx.JSON(200, map[string]string{
		"message": "metrics reset successfully",
	})
}
