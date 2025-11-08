package middleware

import (
	"fmt"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/cluster"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// RedirectMiddleware redirects requests to the leader.
type RedirectMiddleware struct {
	raftNode internal.RaftNode
	manager  *cluster.Manager
	logger   forge.Logger
}

// NewRedirectMiddleware creates redirect middleware.
func NewRedirectMiddleware(raftNode internal.RaftNode, manager *cluster.Manager, logger forge.Logger) *RedirectMiddleware {
	return &RedirectMiddleware{
		raftNode: raftNode,
		manager:  manager,
		logger:   logger,
	}
}

// Handle redirects non-leader requests to the leader.
func (rm *RedirectMiddleware) Handle() func(forge.Context) error {
	return func(ctx forge.Context) error {
		// If this node is the leader, continue
		if rm.raftNode.IsLeader() {
			return nil
		}

		// Get current leader
		leaderID := rm.raftNode.GetLeader()
		if leaderID == "" {
			return ctx.JSON(503, map[string]string{
				"error": "no leader available",
			})
		}

		// Get leader info
		leader, err := rm.manager.GetNode(leaderID)
		if err != nil || leader == nil {
			return ctx.JSON(503, map[string]string{
				"error": "leader information unavailable",
			})
		}

		// Construct leader URL
		leaderURL := fmt.Sprintf("http://%s:%d%s", leader.Address, leader.Port, ctx.Request().URL.Path)

		// Return redirect response
		return ctx.JSON(307, map[string]any{
			"error":      "not leader",
			"leader_id":  leaderID,
			"leader_url": leaderURL,
			"redirect":   true,
		})
	}
}

// HandleWithAutoRedirect performs HTTP redirect.
func (rm *RedirectMiddleware) HandleWithAutoRedirect() func(forge.Context) error {
	return func(ctx forge.Context) error {
		// If this node is the leader, continue
		if rm.raftNode.IsLeader() {
			return nil
		}

		// Get current leader
		leaderID := rm.raftNode.GetLeader()
		if leaderID == "" {
			return ctx.JSON(503, map[string]string{
				"error": "no leader available",
			})
		}

		// Get leader info
		leader, err := rm.manager.GetNode(leaderID)
		if err != nil || leader == nil {
			return ctx.JSON(503, map[string]string{
				"error": "leader information unavailable",
			})
		}

		// Construct leader URL
		leaderURL := fmt.Sprintf("http://%s:%d%s", leader.Address, leader.Port, ctx.Request().URL.Path)

		// Return HTTP redirect
		return ctx.Redirect(307, leaderURL)
	}
}

// HandleWithForward forwards request to leader (proxy).
func (rm *RedirectMiddleware) HandleWithForward() func(forge.Context) error {
	return func(ctx forge.Context) error {
		// If this node is the leader, continue
		if rm.raftNode.IsLeader() {
			return nil
		}

		// Get current leader
		leaderID := rm.raftNode.GetLeader()
		if leaderID == "" {
			return ctx.JSON(503, map[string]string{
				"error": "no leader available",
			})
		}

		// Get leader info
		leader, err := rm.manager.GetNode(leaderID)
		if err != nil || leader == nil {
			return ctx.JSON(503, map[string]string{
				"error": "leader information unavailable",
			})
		}

		// TODO: Implement actual request forwarding
		// For now, return redirect info
		leaderURL := fmt.Sprintf("http://%s:%d%s", leader.Address, leader.Port, ctx.Request().URL.Path)

		rm.logger.Debug("forwarding request to leader",
			forge.F("leader_id", leaderID),
			forge.F("leader_url", leaderURL),
		)

		return ctx.JSON(307, map[string]any{
			"error":      "not leader - forwarding required",
			"leader_url": leaderURL,
		})
	}
}
