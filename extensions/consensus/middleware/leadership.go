package middleware

import (
	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// LeadershipMiddleware provides middleware for leadership enforcement.
type LeadershipMiddleware struct {
	service internal.ConsensusService
	logger  forge.Logger
}

// NewLeadershipMiddleware creates a new leadership middleware.
func NewLeadershipMiddleware(service internal.ConsensusService, logger forge.Logger) *LeadershipMiddleware {
	return &LeadershipMiddleware{
		service: service,
		logger:  logger,
	}
}

// RequireLeader enforces that the current node is the leader.
func (lm *LeadershipMiddleware) RequireLeader(next func(forge.Context) error) func(forge.Context) error {
	return func(ctx forge.Context) error {
		if !lm.service.IsLeader() {
			leaderID := lm.service.GetLeader()

			lm.logger.Debug("rejected request - not leader",
				forge.F("leader_id", leaderID),
				forge.F("path", ctx.Request().URL.Path),
			)

			return ctx.JSON(503, map[string]any{
				"error":     "not the leader",
				"leader_id": leaderID,
				"message":   "this node is not the leader, please redirect to the leader node",
			})
		}

		return next(ctx)
	}
}

// LeaderRedirect redirects to the leader if not leader.
func (lm *LeadershipMiddleware) LeaderRedirect(next func(forge.Context) error) func(forge.Context) error {
	return func(ctx forge.Context) error {
		if !lm.service.IsLeader() {
			leaderID := lm.service.GetLeader()

			if leaderID == "" {
				return ctx.JSON(503, map[string]any{
					"error":   "no leader available",
					"message": "cluster has no leader, please retry later",
				})
			}

			// In a real implementation, you'd construct the leader's URL
			// For now, just return the leader ID
			return ctx.JSON(307, map[string]any{
				"redirect_to": leaderID,
				"message":     "redirecting to leader",
			})
		}

		return next(ctx)
	}
}

// ReadOnlyRouting allows reads on any node but writes only on leader.
func (lm *LeadershipMiddleware) ReadOnlyRouting(next func(forge.Context) error) func(forge.Context) error {
	return func(ctx forge.Context) error {
		method := ctx.Request().Method

		// Allow GET and HEAD on any node
		if method == "GET" || method == "HEAD" {
			return next(ctx)
		}

		// Require leadership for mutations
		if !lm.service.IsLeader() {
			leaderID := lm.service.GetLeader()

			lm.logger.Debug("rejected write request - not leader",
				forge.F("method", method),
				forge.F("leader_id", leaderID),
				forge.F("path", ctx.Request().URL.Path),
			)

			return ctx.JSON(503, map[string]any{
				"error":     "not the leader",
				"leader_id": leaderID,
				"message":   "write operations must be sent to the leader node",
			})
		}

		return next(ctx)
	}
}

// QuorumMiddleware ensures cluster has quorum.
type QuorumMiddleware struct {
	service internal.ConsensusService
	logger  forge.Logger
}

// NewQuorumMiddleware creates a new quorum middleware.
func NewQuorumMiddleware(service internal.ConsensusService, logger forge.Logger) *QuorumMiddleware {
	return &QuorumMiddleware{
		service: service,
		logger:  logger,
	}
}

// RequireQuorum enforces that the cluster has quorum.
func (qm *QuorumMiddleware) RequireQuorum(next func(forge.Context) error) func(forge.Context) error {
	return func(ctx forge.Context) error {
		clusterInfo := qm.service.GetClusterInfo()

		if !clusterInfo.HasQuorum {
			qm.logger.Warn("rejected request - no quorum",
				forge.F("healthy_nodes", clusterInfo.ActiveNodes),
				forge.F("total_nodes", clusterInfo.TotalNodes),
				forge.F("path", ctx.Request().URL.Path),
			)

			return ctx.JSON(503, map[string]any{
				"error":         "no quorum",
				"healthy_nodes": clusterInfo.ActiveNodes,
				"total_nodes":   clusterInfo.TotalNodes,
				"message":       "cluster does not have quorum",
			})
		}

		return next(ctx)
	}
}

// MetricsMiddleware adds consensus metrics to responses.
type MetricsMiddleware struct {
	service internal.ConsensusService
	logger  forge.Logger
}

// NewMetricsMiddleware creates a new metrics middleware.
func NewMetricsMiddleware(service internal.ConsensusService, logger forge.Logger) *MetricsMiddleware {
	return &MetricsMiddleware{
		service: service,
		logger:  logger,
	}
}

// AddConsensusHeaders adds consensus-related headers to responses.
func (mm *MetricsMiddleware) AddConsensusHeaders(next func(forge.Context) error) func(forge.Context) error {
	return func(ctx forge.Context) error {
		// Add consensus state to response headers
		stats := mm.service.GetStats()

		ctx.Response().Header().Set("X-Consensus-Node-ID", stats.NodeID)
		ctx.Response().Header().Set("X-Consensus-Cluster-ID", stats.ClusterID)
		ctx.Response().Header().Set("X-Consensus-Term", string(rune(stats.Term)))
		ctx.Response().Header().Set("X-Consensus-Role", string(stats.Role))

		isLeader := mm.service.IsLeader()
		if isLeader {
			ctx.Response().Header().Set("X-Consensus-Leader", "true")
		} else {
			leaderID := mm.service.GetLeader()

			ctx.Response().Header().Set("X-Consensus-Leader", "false")

			if leaderID != "" {
				ctx.Response().Header().Set("X-Consensus-Leader-ID", leaderID)
			}
		}

		return next(ctx)
	}
}
