package discovery

import (
	"context"
	"sync"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// StaticDiscovery implements static service discovery with a fixed peer list
type StaticDiscovery struct {
	nodes   map[string]internal.NodeInfo
	nodesMu sync.RWMutex
	logger  forge.Logger

	// Lifecycle
	started bool
	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.RWMutex

	// Change notification
	changeListeners []chan internal.NodeChangeEvent
	listenersMu     sync.RWMutex
}

// StaticDiscoveryConfig contains static discovery configuration
type StaticDiscoveryConfig struct {
	Nodes []internal.NodeInfo
}

// NewStaticDiscovery creates a new static discovery service
func NewStaticDiscovery(config StaticDiscoveryConfig, logger forge.Logger) *StaticDiscovery {
	sd := &StaticDiscovery{
		nodes:           make(map[string]internal.NodeInfo),
		logger:          logger,
		changeListeners: make([]chan internal.NodeChangeEvent, 0),
	}

	// Initialize with provided nodes
	for _, node := range config.Nodes {
		sd.nodes[node.ID] = node
	}

	return sd
}

// Start starts the discovery service
func (sd *StaticDiscovery) Start(ctx context.Context) error {
	sd.mu.Lock()
	defer sd.mu.Unlock()

	if sd.started {
		return internal.ErrAlreadyStarted
	}

	sd.ctx, sd.cancel = context.WithCancel(ctx)
	sd.started = true

	sd.logger.Info("static discovery started",
		forge.F("nodes", len(sd.nodes)),
	)

	return nil
}

// Stop stops the discovery service
func (sd *StaticDiscovery) Stop(ctx context.Context) error {
	sd.mu.Lock()
	if !sd.started {
		sd.mu.Unlock()
		return internal.ErrNotStarted
	}
	sd.mu.Unlock()

	if sd.cancel != nil {
		sd.cancel()
	}

	// Close all change listeners
	sd.listenersMu.Lock()
	for _, ch := range sd.changeListeners {
		close(ch)
	}
	sd.changeListeners = nil
	sd.listenersMu.Unlock()

	sd.logger.Info("static discovery stopped")
	return nil
}

// Register registers this node with the discovery service
func (sd *StaticDiscovery) Register(ctx context.Context, node internal.NodeInfo) error {
	sd.nodesMu.Lock()
	defer sd.nodesMu.Unlock()

	sd.nodes[node.ID] = node

	sd.logger.Info("node registered",
		forge.F("node_id", node.ID),
		forge.F("address", node.Address),
		forge.F("port", node.Port),
	)

	// Notify listeners
	sd.notifyListeners(internal.NodeChangeEvent{
		Type: internal.NodeChangeTypeAdded,
		Node: node,
	})

	return nil
}

// Unregister unregisters this node from the discovery service
func (sd *StaticDiscovery) Unregister(ctx context.Context) error {
	// Static discovery doesn't remove nodes
	sd.logger.Info("unregister called (no-op for static discovery)")
	return nil
}

// GetNodes returns all discovered nodes
func (sd *StaticDiscovery) GetNodes(ctx context.Context) ([]internal.NodeInfo, error) {
	sd.nodesMu.RLock()
	defer sd.nodesMu.RUnlock()

	nodes := make([]internal.NodeInfo, 0, len(sd.nodes))
	for _, node := range sd.nodes {
		nodes = append(nodes, node)
	}

	return nodes, nil
}

// Watch watches for node changes
func (sd *StaticDiscovery) Watch(ctx context.Context) (<-chan internal.NodeChangeEvent, error) {
	ch := make(chan internal.NodeChangeEvent, 10)

	sd.listenersMu.Lock()
	sd.changeListeners = append(sd.changeListeners, ch)
	sd.listenersMu.Unlock()

	// Send initial nodes as "added" events
	sd.nodesMu.RLock()
	initialNodes := make([]internal.NodeInfo, 0, len(sd.nodes))
	for _, node := range sd.nodes {
		initialNodes = append(initialNodes, node)
	}
	sd.nodesMu.RUnlock()

	go func() {
		for _, node := range initialNodes {
			select {
			case ch <- internal.NodeChangeEvent{
				Type: internal.NodeChangeTypeAdded,
				Node: node,
			}:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}

// AddNode dynamically adds a node (for testing/dynamic updates)
func (sd *StaticDiscovery) AddNode(node internal.NodeInfo) error {
	sd.nodesMu.Lock()
	defer sd.nodesMu.Unlock()

	sd.nodes[node.ID] = node

	sd.logger.Info("node added",
		forge.F("node_id", node.ID),
	)

	// Notify listeners
	sd.notifyListeners(internal.NodeChangeEvent{
		Type: internal.NodeChangeTypeAdded,
		Node: node,
	})

	return nil
}

// RemoveNode dynamically removes a node (for testing/dynamic updates)
func (sd *StaticDiscovery) RemoveNode(nodeID string) error {
	sd.nodesMu.Lock()
	node, exists := sd.nodes[nodeID]
	if exists {
		delete(sd.nodes, nodeID)
	}
	sd.nodesMu.Unlock()

	if !exists {
		return internal.ErrNodeNotFound
	}

	sd.logger.Info("node removed",
		forge.F("node_id", nodeID),
	)

	// Notify listeners
	sd.notifyListeners(internal.NodeChangeEvent{
		Type: internal.NodeChangeTypeRemoved,
		Node: node,
	})

	return nil
}

// notifyListeners notifies all listeners of a node change
func (sd *StaticDiscovery) notifyListeners(event internal.NodeChangeEvent) {
	sd.listenersMu.RLock()
	listeners := sd.changeListeners
	sd.listenersMu.RUnlock()

	for _, ch := range listeners {
		select {
		case ch <- event:
		default:
			// Channel full, skip
		}
	}
}
