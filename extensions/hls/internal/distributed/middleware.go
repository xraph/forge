package distributed

import (
	"fmt"

	"github.com/xraph/forge"
)

// LeadershipMiddleware ensures operations are executed on the leader node
type LeadershipMiddleware struct {
	coordinator *Coordinator
}

// NewLeadershipMiddleware creates a new leadership middleware
func NewLeadershipMiddleware(coordinator *Coordinator) *LeadershipMiddleware {
	return &LeadershipMiddleware{
		coordinator: coordinator,
	}
}

// CheckLeader checks if this node is the leader and returns error if not
func (m *LeadershipMiddleware) CheckLeader(c forge.Context) error {
	if !m.coordinator.IsLeader() {
		leader := m.coordinator.GetLeader()
		if leader == "" {
			return forge.NewHTTPError(503, "no leader available")
		}

		// Return redirect to leader
		return forge.NewHTTPError(307, fmt.Sprintf("not leader, redirect to: %s", leader))
	}
	return nil
}

// CheckStreamOwner checks if this node owns the stream
func (m *LeadershipMiddleware) CheckStreamOwner(c forge.Context, streamID string) error {
	if streamID == "" {
		return forge.BadRequest("stream ID required")
	}

	isOwner, err := m.coordinator.IsStreamOwner(c.Request().Context(), streamID)
	if err != nil {
		return forge.InternalError(fmt.Errorf("failed to check stream ownership: %w", err))
	}

	if !isOwner {
		owner, _ := m.coordinator.GetStreamOwner(c.Request().Context(), streamID)
		return forge.NewHTTPError(307, fmt.Sprintf("stream owned by: %s", owner))
	}

	return nil
}

// AddClusterHeaders adds cluster information to response headers
func (m *LeadershipMiddleware) AddClusterHeaders(c forge.Context) {
	c.Response().Header().Set("X-HLS-Node", m.coordinator.nodeID)
	c.Response().Header().Set("X-HLS-Leader", m.coordinator.GetLeader())

	if m.coordinator.IsLeader() {
		c.Response().Header().Set("X-HLS-Is-Leader", "true")
	} else {
		c.Response().Header().Set("X-HLS-Is-Leader", "false")
	}
}

// Note: Full middleware integration requires compatible types with forge router.
// The above helper methods can be called directly from handlers for now.
// Future enhancement: Create proper middleware adapters when type system is unified.
