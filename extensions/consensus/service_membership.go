package consensus

import (
	"context"
	"time"

	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// clusterChangeTimeout bounds membership changes made through the
// ClusterManager interface, whose methods take no context.
const clusterChangeTimeout = 30 * time.Second

// raftClusterManager implements ClusterManager on top of the raft
// configuration, so the container's ClusterManager reflects real membership.
type raftClusterManager struct {
	service *Service
}

var _ internal.ClusterManager = (*raftClusterManager)(nil)

func (m *raftClusterManager) GetNodes() []internal.NodeInfo {
	node := m.service.raft()
	if node == nil {
		return nil
	}

	return m.service.clusterNodes(node)
}

func (m *raftClusterManager) GetNode(nodeID string) (*internal.NodeInfo, error) {
	for _, n := range m.GetNodes() {
		if n.ID == nodeID {
			return &n, nil
		}
	}

	return nil, internal.ErrNodeNotFound
}

func (m *raftClusterManager) AddNode(nodeID, address string, port int) error {
	ctx, cancel := context.WithTimeout(context.Background(), clusterChangeTimeout)
	defer cancel()

	return m.service.AddNode(ctx, nodeID, address, port)
}

func (m *raftClusterManager) RemoveNode(nodeID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), clusterChangeTimeout)
	defer cancel()

	return m.service.RemoveNode(ctx, nodeID)
}

// UpdateNode is not supported: member details change by removing and adding
// the member again.
func (m *raftClusterManager) UpdateNode(string, internal.NodeInfo) error {
	return errors.New("consensus: members cannot be updated in place; remove and add them again")
}

func (m *raftClusterManager) GetLeader() *internal.NodeInfo {
	for _, n := range m.GetNodes() {
		if n.Role == internal.RoleLeader {
			return &n
		}
	}

	return nil
}

func (m *raftClusterManager) HasQuorum() bool {
	return m.service.hasQuorum()
}

func (m *raftClusterManager) GetClusterSize() int {
	total, _ := m.service.clusterCounts(m.service.raft())

	return total
}

func (m *raftClusterManager) GetHealthyNodes() int {
	_, active := m.service.clusterCounts(m.service.raft())

	return active
}
