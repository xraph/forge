package discovery

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// EtcdDiscovery implements service discovery using etcd
type EtcdDiscovery struct {
	nodeID string
	logger forge.Logger

	// Etcd configuration
	endpoints []string
	prefix    string
	ttl       time.Duration

	// Discovered peers
	peers   map[string]*internal.NodeInfo
	peersMu *sync.RWMutex

	// Lifecycle
	started bool
	ctx     context.Context
	cancel  context.CancelFunc
}

// EtcdDiscoveryConfig contains etcd discovery configuration
type EtcdDiscoveryConfig struct {
	NodeID    string
	Endpoints []string
	Prefix    string
	TTL       time.Duration
}

// NewEtcdDiscovery creates a new etcd-based discovery service
func NewEtcdDiscovery(config EtcdDiscoveryConfig, logger forge.Logger) (*EtcdDiscovery, error) {
	// Set defaults
	if config.Prefix == "" {
		config.Prefix = "/consensus/nodes"
	}
	if config.TTL == 0 {
		config.TTL = 10 * time.Second
	}
	if len(config.Endpoints) == 0 {
		config.Endpoints = []string{"localhost:2379"}
	}

	ed := &EtcdDiscovery{
		nodeID:    config.NodeID,
		logger:    logger,
		endpoints: config.Endpoints,
		prefix:    config.Prefix,
		ttl:       config.TTL,
		peers:     make(map[string]*internal.NodeInfo),
		peersMu:   &sync.RWMutex{},
	}

	return ed, nil
}

// Start starts the discovery service
func (ed *EtcdDiscovery) Start(ctx context.Context) error {
	if ed.started {
		return nil
	}

	ed.ctx, ed.cancel = context.WithCancel(ctx)
	ed.started = true

	// Register this node
	if err := ed.register(); err != nil {
		return fmt.Errorf("failed to register node: %w", err)
	}

	// Start watching for peers
	go ed.watchPeers()

	// Start keepalive
	go ed.keepalive()

	ed.logger.Info("etcd discovery started",
		forge.F("node_id", ed.nodeID),
		forge.F("endpoints", ed.endpoints),
		forge.F("prefix", ed.prefix),
	)

	return nil
}

// Stop stops the discovery service
func (ed *EtcdDiscovery) Stop(ctx context.Context) error {
	if !ed.started {
		return nil
	}

	ed.started = false

	// Deregister node
	if err := ed.deregister(); err != nil {
		ed.logger.Warn("failed to deregister node",
			forge.F("error", err),
		)
	}

	if ed.cancel != nil {
		ed.cancel()
	}

	ed.logger.Info("etcd discovery stopped")
	return nil
}

// register registers this node in etcd
func (ed *EtcdDiscovery) register() error {
	// In a real implementation, this would:
	// 1. Connect to etcd
	// 2. Put node info with TTL
	// 3. Return any errors

	ed.logger.Debug("registering node in etcd",
		forge.F("key", ed.nodeKey()),
	)

	// Simulated registration
	// In production: use etcd client to put key with lease
	return nil
}

// deregister removes this node from etcd
func (ed *EtcdDiscovery) deregister() error {
	// In a real implementation, this would:
	// 1. Delete the node key from etcd
	// 2. Revoke the lease

	ed.logger.Debug("deregistering node from etcd",
		forge.F("key", ed.nodeKey()),
	)

	// Simulated deregistration
	return nil
}

// keepalive maintains the node registration
func (ed *EtcdDiscovery) keepalive() {
	ticker := time.NewTicker(ed.ttl / 2)
	defer ticker.Stop()

	for {
		select {
		case <-ed.ctx.Done():
			return
		case <-ticker.C:
			if err := ed.register(); err != nil {
				ed.logger.Warn("failed to refresh registration",
					forge.F("error", err),
				)
			}
		}
	}
}

// watchPeers watches for peer changes
func (ed *EtcdDiscovery) watchPeers() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ed.ctx.Done():
			return
		case <-ticker.C:
			if err := ed.refreshPeers(); err != nil {
				ed.logger.Warn("failed to refresh peers",
					forge.F("error", err),
				)
			}
		}
	}
}

// refreshPeers refreshes the peer list from etcd
func (ed *EtcdDiscovery) refreshPeers() error {
	// In a real implementation, this would:
	// 1. List all keys under the prefix
	// 2. Parse node info from values
	// 3. Update the peers map

	// Simulated peer discovery
	// In production: use etcd client to get all keys with prefix
	return nil
}

// GetPeers returns the list of discovered peers
func (ed *EtcdDiscovery) GetPeers() ([]*internal.NodeInfo, error) {
	ed.peersMu.RLock()
	defer ed.peersMu.RUnlock()

	peers := make([]*internal.NodeInfo, 0, len(ed.peers))
	for _, peer := range ed.peers {
		if peer.ID != ed.nodeID {
			peers = append(peers, peer)
		}
	}

	return peers, nil
}

// Watch watches for peer changes
func (ed *EtcdDiscovery) Watch(ctx context.Context) (<-chan internal.DiscoveryEvent, error) {
	eventChan := make(chan internal.DiscoveryEvent, 10)

	go func() {
		defer close(eventChan)

		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		lastPeers := make(map[string]*internal.NodeInfo)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ed.ctx.Done():
				return
			case <-ticker.C:
				ed.peersMu.RLock()
				currentPeers := make(map[string]*internal.NodeInfo)
				for id, peer := range ed.peers {
					currentPeers[id] = peer
				}
				ed.peersMu.RUnlock()

				// Detect changes
				for id, peer := range currentPeers {
					if _, exists := lastPeers[id]; !exists {
						// New peer
						eventChan <- internal.DiscoveryEvent{
							Type: internal.DiscoveryEventTypeJoin,
							Node: peer,
						}
					}
				}

				for id, peer := range lastPeers {
					if _, exists := currentPeers[id]; !exists {
						// Peer left
						eventChan <- internal.DiscoveryEvent{
							Type: internal.DiscoveryEventTypeLeave,
							Node: peer,
						}
					}
				}

				lastPeers = currentPeers
			}
		}
	}()

	return eventChan, nil
}

// nodeKey returns the etcd key for this node
func (ed *EtcdDiscovery) nodeKey() string {
	return fmt.Sprintf("%s/%s", ed.prefix, ed.nodeID)
}

// Health checks the health of the discovery service
func (ed *EtcdDiscovery) Health() error {
	if !ed.started {
		return fmt.Errorf("etcd discovery not started")
	}

	// In a real implementation, this would:
	// 1. Check etcd connection
	// 2. Verify node registration
	// 3. Return health status

	return nil
}

// GetNodeInfo returns information about a specific node
func (ed *EtcdDiscovery) GetNodeInfo(nodeID string) (*internal.NodeInfo, error) {
	ed.peersMu.RLock()
	defer ed.peersMu.RUnlock()

	node, exists := ed.peers[nodeID]
	if !exists {
		return nil, fmt.Errorf("node not found: %s", nodeID)
	}

	return node, nil
}

// UpdateNodeInfo updates this node's information
func (ed *EtcdDiscovery) UpdateNodeInfo(info *internal.NodeInfo) error {
	if info.ID != ed.nodeID {
		return fmt.Errorf("cannot update info for different node")
	}

	// In a real implementation, this would:
	// 1. Update the node info in etcd
	// 2. Refresh the lease

	ed.logger.Debug("updating node info",
		forge.F("node_id", ed.nodeID),
	)

	return nil
}
