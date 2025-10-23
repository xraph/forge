package discovery

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// Registry manages node registration and discovery with centralized coordination
type Registry struct {
	nodeID string
	logger forge.Logger

	// Registered nodes
	nodes   map[string]*RegisteredNode
	nodesMu sync.RWMutex

	// Configuration
	config RegistryConfig

	// Event subscribers
	subscribers []chan internal.DiscoveryEvent
	subsMu      sync.RWMutex

	// Lifecycle
	started bool
	ctx     context.Context
	cancel  context.CancelFunc
}

// RegisteredNode represents a registered node
type RegisteredNode struct {
	Info         *internal.NodeInfo
	RegisteredAt time.Time
	LastSeen     time.Time
	Healthy      bool
	Metadata     map[string]interface{}
}

// RegistryConfig contains registry configuration
type RegistryConfig struct {
	NodeID              string
	HealthCheckInterval time.Duration
	NodeTimeout         time.Duration
	EnableAutoCleanup   bool
}

// NewRegistry creates a new registry
func NewRegistry(config RegistryConfig, logger forge.Logger) *Registry {
	// Set defaults
	if config.HealthCheckInterval == 0 {
		config.HealthCheckInterval = 10 * time.Second
	}
	if config.NodeTimeout == 0 {
		config.NodeTimeout = 30 * time.Second
	}

	return &Registry{
		nodeID:      config.NodeID,
		logger:      logger,
		nodes:       make(map[string]*RegisteredNode),
		subscribers: make([]chan internal.DiscoveryEvent, 0),
		config:      config,
	}
}

// Start starts the registry
func (r *Registry) Start(ctx context.Context) error {
	if r.started {
		return nil
	}

	r.ctx, r.cancel = context.WithCancel(ctx)
	r.started = true

	// Start health check monitor
	if r.config.EnableAutoCleanup {
		go r.monitorHealth()
	}

	r.logger.Info("registry started",
		forge.F("node_id", r.nodeID),
	)

	return nil
}

// Stop stops the registry
func (r *Registry) Stop(ctx context.Context) error {
	if !r.started {
		return nil
	}

	r.started = false

	if r.cancel != nil {
		r.cancel()
	}

	// Close all event channels
	r.subsMu.Lock()
	for _, ch := range r.subscribers {
		close(ch)
	}
	r.subscribers = nil
	r.subsMu.Unlock()

	r.logger.Info("registry stopped")
	return nil
}

// Register registers a node
func (r *Registry) Register(node *internal.NodeInfo) error {
	r.nodesMu.Lock()
	defer r.nodesMu.Unlock()

	existing, exists := r.nodes[node.ID]

	registered := &RegisteredNode{
		Info:         node,
		RegisteredAt: time.Now(),
		LastSeen:     time.Now(),
		Healthy:      true,
		Metadata:     make(map[string]interface{}),
	}

	if exists {
		// Preserve registration time
		registered.RegisteredAt = existing.RegisteredAt
	}

	r.nodes[node.ID] = registered

	// Notify subscribers
	event := internal.DiscoveryEvent{
		Type: internal.DiscoveryEventTypeJoin,
		Node: node,
	}

	if exists {
		event.Type = internal.DiscoveryEventTypeUpdate
	}

	r.notifySubscribers(event)

	r.logger.Info("node registered",
		forge.F("node_id", node.ID),
		forge.F("address", node.Address),
		forge.F("new", !exists),
	)

	return nil
}

// Deregister removes a node from the registry
func (r *Registry) Deregister(nodeID string) error {
	r.nodesMu.Lock()
	registered, exists := r.nodes[nodeID]
	if exists {
		delete(r.nodes, nodeID)
	}
	r.nodesMu.Unlock()

	if !exists {
		return fmt.Errorf("node not registered: %s", nodeID)
	}

	// Notify subscribers
	r.notifySubscribers(DiscoveryEvent{
		Type: internal.DiscoveryEventTypeLeave,
		Node: registered.Info,
	})

	r.logger.Info("node deregistered",
		forge.F("node_id", nodeID),
	)

	return nil
}

// GetPeers returns all registered peers
func (r *Registry) GetPeers() ([]*internal.NodeInfo, error) {
	r.nodesMu.RLock()
	defer r.nodesMu.RUnlock()

	peers := make([]*internal.NodeInfo, 0, len(r.nodes))
	for _, registered := range r.nodes {
		if registered.Info.ID != r.nodeID && registered.Healthy {
			peers = append(peers, registered.Info)
		}
	}

	return peers, nil
}

// GetNodeInfo returns information about a node
func (r *Registry) GetNodeInfo(nodeID string) (*internal.NodeInfo, error) {
	r.nodesMu.RLock()
	defer r.nodesMu.RUnlock()

	registered, exists := r.nodes[nodeID]
	if !exists {
		return nil, fmt.Errorf("node not found: %s", nodeID)
	}

	return registered.Info, nil
}

// UpdateNodeInfo updates node information
func (r *Registry) UpdateNodeInfo(info *internal.NodeInfo) error {
	r.nodesMu.Lock()
	registered, exists := r.nodes[info.ID]
	if !exists {
		r.nodesMu.Unlock()
		return fmt.Errorf("node not registered: %s", info.ID)
	}

	registered.Info = info
	registered.LastSeen = time.Now()
	r.nodesMu.Unlock()

	// Notify subscribers
	r.notifySubscribers(DiscoveryEvent{
		Type: internal.DiscoveryEventTypeUpdate,
		Node: info,
	})

	r.logger.Debug("node info updated",
		forge.F("node_id", info.ID),
	)

	return nil
}

// Heartbeat updates the last seen time for a node
func (r *Registry) Heartbeat(nodeID string) error {
	r.nodesMu.Lock()
	defer r.nodesMu.Unlock()

	registered, exists := r.nodes[nodeID]
	if !exists {
		return fmt.Errorf("node not registered: %s", nodeID)
	}

	registered.LastSeen = time.Now()
	registered.Healthy = true

	return nil
}

// Watch watches for node changes
func (r *Registry) Watch(ctx context.Context) (<-chan DiscoveryEvent, error) {
	eventChan := make(chan DiscoveryEvent, 10)

	r.subsMu.Lock()
	r.subscribers = append(r.subscribers, eventChan)
	r.subsMu.Unlock()

	// Send current nodes as join events
	go func() {
		r.nodesMu.RLock()
		for _, registered := range r.nodes {
			if registered.Info.ID != r.nodeID {
				select {
				case eventChan <- DiscoveryEvent{
					Type: internal.DiscoveryEventTypeJoin,
					Node: registered.Info,
				}:
				case <-ctx.Done():
					r.nodesMu.RUnlock()
					return
				}
			}
		}
		r.nodesMu.RUnlock()
	}()

	return eventChan, nil
}

// notifySubscribers notifies all subscribers of an event
func (r *Registry) notifySubscribers(event DiscoveryEvent) {
	r.subsMu.RLock()
	defer r.subsMu.RUnlock()

	for _, ch := range r.subscribers {
		select {
		case ch <- event:
		default:
			// Channel full, skip
		}
	}
}

// monitorHealth monitors node health
func (r *Registry) monitorHealth() {
	ticker := time.NewTicker(r.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			r.checkNodeHealth()
		}
	}
}

// checkNodeHealth checks health of all nodes
func (r *Registry) checkNodeHealth() {
	r.nodesMu.Lock()
	defer r.nodesMu.Unlock()

	now := time.Now()
	unhealthy := make([]string, 0)

	for nodeID, registered := range r.nodes {
		if nodeID == r.nodeID {
			continue
		}

		// Check if node is stale
		if now.Sub(registered.LastSeen) > r.config.NodeTimeout {
			if registered.Healthy {
				registered.Healthy = false
				unhealthy = append(unhealthy, nodeID)

				r.logger.Warn("node marked unhealthy",
					forge.F("node_id", nodeID),
					forge.F("last_seen", registered.LastSeen),
				)

				// Notify subscribers
				go r.notifySubscribers(DiscoveryEvent{
					Type: internal.DiscoveryEventTypeLeave,
					Node: registered.Info,
				})
			}
		}
	}

	if len(unhealthy) > 0 {
		r.logger.Info("health check completed",
			forge.F("unhealthy_nodes", len(unhealthy)),
		)
	}
}

// GetAllNodes returns all registered nodes including unhealthy
func (r *Registry) GetAllNodes() map[string]*RegisteredNode {
	r.nodesMu.RLock()
	defer r.nodesMu.RUnlock()

	result := make(map[string]*RegisteredNode)
	for id, node := range r.nodes {
		result[id] = node
	}

	return result
}

// GetHealthyNodes returns only healthy nodes
func (r *Registry) GetHealthyNodes() []*internal.NodeInfo {
	r.nodesMu.RLock()
	defer r.nodesMu.RUnlock()

	nodes := make([]*internal.NodeInfo, 0)
	for _, registered := range r.nodes {
		if registered.Healthy {
			nodes = append(nodes, registered.Info)
		}
	}

	return nodes
}

// GetUnhealthyNodes returns unhealthy nodes
func (r *Registry) GetUnhealthyNodes() []*internal.NodeInfo {
	r.nodesMu.RLock()
	defer r.nodesMu.RUnlock()

	nodes := make([]*internal.NodeInfo, 0)
	for _, registered := range r.nodes {
		if !registered.Healthy {
			nodes = append(nodes, registered.Info)
		}
	}

	return nodes
}

// SetNodeMetadata sets metadata for a node
func (r *Registry) SetNodeMetadata(nodeID string, key string, value interface{}) error {
	r.nodesMu.Lock()
	defer r.nodesMu.Unlock()

	registered, exists := r.nodes[nodeID]
	if !exists {
		return fmt.Errorf("node not registered: %s", nodeID)
	}

	if registered.Metadata == nil {
		registered.Metadata = make(map[string]interface{})
	}

	registered.Metadata[key] = value

	return nil
}

// GetNodeMetadata gets metadata for a node
func (r *Registry) GetNodeMetadata(nodeID string, key string) (interface{}, error) {
	r.nodesMu.RLock()
	defer r.nodesMu.RUnlock()

	registered, exists := r.nodes[nodeID]
	if !exists {
		return nil, fmt.Errorf("node not registered: %s", nodeID)
	}

	value, exists := registered.Metadata[key]
	if !exists {
		return nil, fmt.Errorf("metadata not found: %s", key)
	}

	return value, nil
}

// Health checks registry health
func (r *Registry) Health() error {
	if !r.started {
		return fmt.Errorf("registry not started")
	}
	return nil
}

// GetStatistics returns registry statistics
func (r *Registry) GetStatistics() RegistryStatistics {
	r.nodesMu.RLock()
	defer r.nodesMu.RUnlock()

	stats := RegistryStatistics{
		TotalNodes: len(r.nodes),
	}

	for _, registered := range r.nodes {
		if registered.Healthy {
			stats.HealthyNodes++
		} else {
			stats.UnhealthyNodes++
		}
	}

	return stats
}

// RegistryStatistics contains registry statistics
type RegistryStatistics struct {
	TotalNodes     int
	HealthyNodes   int
	UnhealthyNodes int
}
