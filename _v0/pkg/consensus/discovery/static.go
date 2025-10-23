package discovery

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
)

// StaticDiscovery implements static node discovery
type StaticDiscovery struct {
	nodes      map[string]NodeInfo
	watchers   []NodeChangeCallback
	config     StaticDiscoveryConfig
	stats      DiscoveryStats
	logger     common.Logger
	metrics    common.Metrics
	startTime  time.Time
	mu         sync.RWMutex
	watchersMu sync.RWMutex
}

// StaticDiscoveryConfig contains configuration for static discovery
type StaticDiscoveryConfig struct {
	Nodes           []NodeInfo    `json:"nodes"`
	Namespace       string        `json:"namespace"`
	NodeTTL         time.Duration `json:"node_ttl"`
	RefreshInterval time.Duration `json:"refresh_interval"`
	EnableWatch     bool          `json:"enable_watch"`
}

// StaticDiscoveryFactory creates static discovery services
type StaticDiscoveryFactory struct {
	logger  common.Logger
	metrics common.Metrics
}

// NewStaticDiscoveryFactory creates a new static discovery factory
func NewStaticDiscoveryFactory(logger common.Logger, metrics common.Metrics) *StaticDiscoveryFactory {
	return &StaticDiscoveryFactory{
		logger:  logger,
		metrics: metrics,
	}
}

// Create creates a new static discovery service
func (f *StaticDiscoveryFactory) Create(config DiscoveryConfig) (Discovery, error) {
	staticConfig := StaticDiscoveryConfig{
		Namespace:       config.Namespace,
		NodeTTL:         config.NodeTTL,
		RefreshInterval: config.RefreshInterval,
		EnableWatch:     true,
	}

	// Parse nodes from config
	if nodesConfig, ok := config.Options["nodes"]; ok {
		if nodesList, ok := nodesConfig.([]interface{}); ok {
			for _, nodeConfig := range nodesList {
				if nodeMap, ok := nodeConfig.(map[string]interface{}); ok {
					node, err := parseNodeFromConfig(nodeMap)
					if err != nil {
						return nil, fmt.Errorf("failed to parse node configuration: %w", err)
					}
					staticConfig.Nodes = append(staticConfig.Nodes, node)
				}
			}
		}
	}

	// Set defaults
	if staticConfig.NodeTTL == 0 {
		staticConfig.NodeTTL = 5 * time.Minute
	}
	if staticConfig.RefreshInterval == 0 {
		staticConfig.RefreshInterval = 30 * time.Second
	}

	sd := &StaticDiscovery{
		nodes:     make(map[string]NodeInfo),
		watchers:  make([]NodeChangeCallback, 0),
		config:    staticConfig,
		logger:    f.logger,
		metrics:   f.metrics,
		startTime: time.Now(),
		stats: DiscoveryStats{
			TotalNodes:     0,
			ActiveNodes:    0,
			InactiveNodes:  0,
			FailedNodes:    0,
			LastUpdate:     time.Now(),
			WatcherCount:   0,
			OperationCount: 0,
			ErrorCount:     0,
		},
	}

	// Initialize with configured nodes
	for _, node := range staticConfig.Nodes {
		sd.nodes[node.ID] = node
	}

	// Update stats
	sd.updateStats()

	// Start refresh routine if enabled
	if staticConfig.RefreshInterval > 0 {
		go sd.refreshLoop()
	}

	if f.logger != nil {
		f.logger.Info("static discovery service created",
			logger.String("namespace", staticConfig.Namespace),
			logger.Int("nodes", len(staticConfig.Nodes)),
			logger.Duration("node_ttl", staticConfig.NodeTTL),
			logger.Duration("refresh_interval", staticConfig.RefreshInterval),
		)
	}

	return sd, nil
}

// Name returns the factory name
func (f *StaticDiscoveryFactory) Name() string {
	return DiscoveryTypeStatic
}

// Version returns the factory version
func (f *StaticDiscoveryFactory) Version() string {
	return "1.0.0"
}

// ValidateConfig validates the configuration
func (f *StaticDiscoveryFactory) ValidateConfig(config DiscoveryConfig) error {
	if config.Type != DiscoveryTypeStatic {
		return fmt.Errorf("invalid discovery type: %s", config.Type)
	}

	// Validate nodes configuration
	if nodesConfig, ok := config.Options["nodes"]; ok {
		if nodesList, ok := nodesConfig.([]interface{}); ok {
			for i, nodeConfig := range nodesList {
				if nodeMap, ok := nodeConfig.(map[string]interface{}); ok {
					if _, err := parseNodeFromConfig(nodeMap); err != nil {
						return fmt.Errorf("invalid node configuration at index %d: %w", i, err)
					}
				} else {
					return fmt.Errorf("invalid node configuration at index %d: expected map", i)
				}
			}
		} else {
			return fmt.Errorf("invalid nodes configuration: expected array")
		}
	}

	return nil
}

// GetNodes returns all nodes
func (sd *StaticDiscovery) GetNodes(ctx context.Context) ([]NodeInfo, error) {
	sd.mu.RLock()
	defer sd.mu.RUnlock()

	sd.incrementOperation()

	nodes := make([]NodeInfo, 0, len(sd.nodes))
	for _, node := range sd.nodes {
		nodes = append(nodes, node)
	}

	if sd.logger != nil {
		sd.logger.Debug("retrieved nodes",
			logger.Int("count", len(nodes)),
		)
	}

	return nodes, nil
}

// GetNode returns information about a specific node
func (sd *StaticDiscovery) GetNode(ctx context.Context, nodeID string) (*NodeInfo, error) {
	sd.mu.RLock()
	defer sd.mu.RUnlock()

	sd.incrementOperation()

	node, exists := sd.nodes[nodeID]
	if !exists {
		sd.incrementError()
		return nil, &DiscoveryError{
			Code:    ErrCodeNodeNotFound,
			Message: fmt.Sprintf("node not found: %s", nodeID),
			NodeID:  nodeID,
		}
	}

	if sd.logger != nil {
		sd.logger.Debug("retrieved node",
			logger.String("node_id", nodeID),
			logger.String("address", node.Address),
			logger.String("status", string(node.Status)),
		)
	}

	return &node, nil
}

// Register registers a node
func (sd *StaticDiscovery) Register(ctx context.Context, node NodeInfo) error {
	sd.mu.Lock()
	defer sd.mu.Unlock()

	sd.incrementOperation()

	// Check if node already exists
	if _, exists := sd.nodes[node.ID]; exists {
		sd.incrementError()
		return &DiscoveryError{
			Code:    ErrCodeNodeExists,
			Message: fmt.Sprintf("node already exists: %s", node.ID),
			NodeID:  node.ID,
		}
	}

	// Set registration time
	node.RegisteredAt = time.Now()
	node.UpdatedAt = time.Now()
	node.LastSeen = time.Now()

	// Set default TTL if not specified
	if node.TTL == 0 {
		node.TTL = sd.config.NodeTTL
	}

	// Add node
	sd.nodes[node.ID] = node

	// Update stats
	sd.updateStats()

	// Notify watchers
	if sd.config.EnableWatch {
		sd.notifyWatchers(NodeChangeEvent{
			Type:      NodeChangeTypeAdded,
			Node:      node,
			Timestamp: time.Now(),
		})
	}

	if sd.logger != nil {
		sd.logger.Info("node registered",
			logger.String("node_id", node.ID),
			logger.String("address", node.Address),
			logger.String("status", string(node.Status)),
		)
	}

	if sd.metrics != nil {
		sd.metrics.Counter("forge.consensus.discovery.nodes_registered").Inc()
	}

	return nil
}

// Unregister removes a node
func (sd *StaticDiscovery) Unregister(ctx context.Context, nodeID string) error {
	sd.mu.Lock()
	defer sd.mu.Unlock()

	sd.incrementOperation()

	// Check if node exists
	node, exists := sd.nodes[nodeID]
	if !exists {
		sd.incrementError()
		return &DiscoveryError{
			Code:    ErrCodeNodeNotFound,
			Message: fmt.Sprintf("node not found: %s", nodeID),
			NodeID:  nodeID,
		}
	}

	// Remove node
	delete(sd.nodes, nodeID)

	// Update stats
	sd.updateStats()

	// Notify watchers
	if sd.config.EnableWatch {
		sd.notifyWatchers(NodeChangeEvent{
			Type:      NodeChangeTypeRemoved,
			Node:      node,
			Timestamp: time.Now(),
		})
	}

	if sd.logger != nil {
		sd.logger.Info("node unregistered",
			logger.String("node_id", nodeID),
		)
	}

	if sd.metrics != nil {
		sd.metrics.Counter("forge.consensus.discovery.nodes_unregistered").Inc()
	}

	return nil
}

// UpdateNode updates node information
func (sd *StaticDiscovery) UpdateNode(ctx context.Context, node NodeInfo) error {
	sd.mu.Lock()
	defer sd.mu.Unlock()

	sd.incrementOperation()

	// Check if node exists
	oldNode, exists := sd.nodes[node.ID]
	if !exists {
		sd.incrementError()
		return &DiscoveryError{
			Code:    ErrCodeNodeNotFound,
			Message: fmt.Sprintf("node not found: %s", node.ID),
			NodeID:  node.ID,
		}
	}

	// Update node
	node.RegisteredAt = oldNode.RegisteredAt
	node.UpdatedAt = time.Now()
	node.LastSeen = time.Now()

	// Set default TTL if not specified
	if node.TTL == 0 {
		node.TTL = sd.config.NodeTTL
	}

	sd.nodes[node.ID] = node

	// Update stats
	sd.updateStats()

	// Notify watchers
	if sd.config.EnableWatch {
		sd.notifyWatchers(NodeChangeEvent{
			Type:      NodeChangeTypeUpdated,
			Node:      node,
			OldNode:   &oldNode,
			Timestamp: time.Now(),
		})
	}

	if sd.logger != nil {
		sd.logger.Info("node updated",
			logger.String("node_id", node.ID),
			logger.String("address", node.Address),
			logger.String("status", string(node.Status)),
		)
	}

	if sd.metrics != nil {
		sd.metrics.Counter("forge.consensus.discovery.nodes_updated").Inc()
	}

	return nil
}

// WatchNodes watches for node changes
func (sd *StaticDiscovery) WatchNodes(ctx context.Context, callback NodeChangeCallback) error {
	if !sd.config.EnableWatch {
		return &DiscoveryError{
			Code:    ErrCodeServiceUnavailable,
			Message: "node watching is not enabled",
		}
	}

	sd.watchersMu.Lock()
	defer sd.watchersMu.Unlock()

	sd.watchers = append(sd.watchers, callback)

	// Update stats
	sd.mu.Lock()
	sd.stats.WatcherCount = len(sd.watchers)
	sd.mu.Unlock()

	if sd.logger != nil {
		sd.logger.Info("watcher added",
			logger.Int("total_watchers", len(sd.watchers)),
		)
	}

	if sd.metrics != nil {
		sd.metrics.Counter("forge.consensus.discovery.watchers_added").Inc()
		sd.metrics.Gauge("forge.consensus.discovery.active_watchers").Set(float64(len(sd.watchers)))
	}

	return nil
}

// HealthCheck performs a health check
func (sd *StaticDiscovery) HealthCheck(ctx context.Context) error {
	sd.mu.RLock()
	defer sd.mu.RUnlock()

	// Check if we have any nodes
	if len(sd.nodes) == 0 {
		return &DiscoveryError{
			Code:    ErrCodeServiceUnavailable,
			Message: "no nodes available",
		}
	}

	// Check error rate
	if sd.stats.OperationCount > 0 {
		errorRate := float64(sd.stats.ErrorCount) / float64(sd.stats.OperationCount)
		if errorRate > 0.1 { // 10% error rate threshold
			return &DiscoveryError{
				Code:    ErrCodeServiceUnavailable,
				Message: fmt.Sprintf("high error rate: %.2f%%", errorRate*100),
			}
		}
	}

	return nil
}

// GetStats returns discovery statistics
func (sd *StaticDiscovery) GetStats(ctx context.Context) DiscoveryStats {
	sd.mu.RLock()
	defer sd.mu.RUnlock()

	stats := sd.stats
	stats.Uptime = time.Since(sd.startTime)

	// Calculate average latency (static discovery has minimal latency)
	if stats.OperationCount > 0 {
		stats.AverageLatency = time.Microsecond * 100 // Very low latency for static discovery
	}

	return stats
}

// Close closes the discovery service
func (sd *StaticDiscovery) Close(ctx context.Context) error {
	sd.mu.Lock()
	defer sd.mu.Unlock()

	// Clear watchers
	sd.watchersMu.Lock()
	sd.watchers = nil
	sd.watchersMu.Unlock()

	// Clear nodes
	sd.nodes = nil

	if sd.logger != nil {
		sd.logger.Info("static discovery service closed")
	}

	return nil
}

// refreshLoop periodically refreshes node information
func (sd *StaticDiscovery) refreshLoop() {
	ticker := time.NewTicker(sd.config.RefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			sd.refreshNodes()
		}
	}
}

// refreshNodes refreshes node information
func (sd *StaticDiscovery) refreshNodes() {
	sd.mu.Lock()
	defer sd.mu.Unlock()

	now := time.Now()
	var expiredNodes []string

	// Check for expired nodes
	for nodeID, node := range sd.nodes {
		if node.TTL > 0 && now.Sub(node.LastSeen) > node.TTL {
			expiredNodes = append(expiredNodes, nodeID)
		}
	}

	// Remove expired nodes
	for _, nodeID := range expiredNodes {
		node := sd.nodes[nodeID]
		delete(sd.nodes, nodeID)

		// Update node status to failed
		node.Status = NodeStatusFailed
		node.UpdatedAt = now

		// Notify watchers
		if sd.config.EnableWatch {
			sd.notifyWatchers(NodeChangeEvent{
				Type:      NodeChangeTypeRemoved,
				Node:      node,
				Timestamp: now,
			})
		}

		if sd.logger != nil {
			sd.logger.Warn("node expired",
				logger.String("node_id", nodeID),
				logger.Duration("ttl", node.TTL),
				logger.Duration("last_seen_ago", now.Sub(node.LastSeen)),
			)
		}
	}

	// Update stats
	sd.updateStats()
}

// updateStats updates discovery statistics
func (sd *StaticDiscovery) updateStats() {
	sd.stats.TotalNodes = len(sd.nodes)
	sd.stats.ActiveNodes = 0
	sd.stats.InactiveNodes = 0
	sd.stats.FailedNodes = 0
	sd.stats.LastUpdate = time.Now()

	for _, node := range sd.nodes {
		switch node.Status {
		case NodeStatusActive:
			sd.stats.ActiveNodes++
		case NodeStatusInactive:
			sd.stats.InactiveNodes++
		case NodeStatusFailed:
			sd.stats.FailedNodes++
		}
	}

	sd.watchersMu.RLock()
	sd.stats.WatcherCount = len(sd.watchers)
	sd.watchersMu.RUnlock()
}

// incrementOperation increments the operation count
func (sd *StaticDiscovery) incrementOperation() {
	sd.stats.OperationCount++
}

// incrementError increments the error count
func (sd *StaticDiscovery) incrementError() {
	sd.stats.ErrorCount++
}

// notifyWatchers notifies all watchers of a node change
func (sd *StaticDiscovery) notifyWatchers(event NodeChangeEvent) {
	sd.watchersMu.RLock()
	watchers := make([]NodeChangeCallback, len(sd.watchers))
	copy(watchers, sd.watchers)
	sd.watchersMu.RUnlock()

	for _, callback := range watchers {
		go func(cb NodeChangeCallback) {
			if err := cb(event); err != nil {
				if sd.logger != nil {
					sd.logger.Error("watcher callback failed",
						logger.String("event_type", string(event.Type)),
						logger.String("node_id", event.Node.ID),
						logger.Error(err),
					)
				}
			}
		}(callback)
	}
}

// parseNodeFromConfig parses node information from configuration
func parseNodeFromConfig(config map[string]interface{}) (NodeInfo, error) {
	var node NodeInfo

	// Required fields
	if id, ok := config["id"].(string); ok {
		node.ID = id
	} else {
		return node, fmt.Errorf("node id is required")
	}

	if address, ok := config["address"].(string); ok {
		node.Address = address
	} else {
		return node, fmt.Errorf("node address is required")
	}

	// Optional fields
	if port, ok := config["port"].(float64); ok {
		node.Port = int(port)
	}

	if role, ok := config["role"].(string); ok {
		node.Role = role
	}

	if status, ok := config["status"].(string); ok {
		node.Status = NodeStatus(status)
	} else {
		node.Status = NodeStatusActive
	}

	// Metadata
	if metadata, ok := config["metadata"].(map[string]interface{}); ok {
		node.Metadata = metadata
	} else {
		node.Metadata = make(map[string]interface{})
	}

	// Tags
	if tags, ok := config["tags"].([]interface{}); ok {
		node.Tags = make([]string, len(tags))
		for i, tag := range tags {
			if tagStr, ok := tag.(string); ok {
				node.Tags[i] = tagStr
			}
		}
	}

	// TTL
	if ttl, ok := config["ttl"].(string); ok {
		if duration, err := time.ParseDuration(ttl); err == nil {
			node.TTL = duration
		}
	}

	// Set timestamps
	now := time.Now()
	node.RegisteredAt = now
	node.UpdatedAt = now
	node.LastSeen = now

	return node, nil
}

// Helper functions for common operations

// CreateStaticDiscovery creates a static discovery service with nodes
func CreateStaticDiscovery(nodes []NodeInfo, logger common.Logger, metrics common.Metrics) (Discovery, error) {
	factory := NewStaticDiscoveryFactory(logger, metrics)

	config := DiscoveryConfig{
		Type:            DiscoveryTypeStatic,
		Namespace:       "default",
		NodeTTL:         5 * time.Minute,
		RefreshInterval: 30 * time.Second,
		Options: map[string]interface{}{
			"nodes": convertNodesToConfig(nodes),
		},
	}

	return factory.Create(config)
}

// convertNodesToConfig converts NodeInfo slice to config format
func convertNodesToConfig(nodes []NodeInfo) []interface{} {
	configs := make([]interface{}, len(nodes))
	for i, node := range nodes {
		configs[i] = map[string]interface{}{
			"id":       node.ID,
			"address":  node.Address,
			"port":     node.Port,
			"role":     node.Role,
			"status":   string(node.Status),
			"metadata": node.Metadata,
			"tags":     node.Tags,
			"ttl":      node.TTL.String(),
		}
	}
	return configs
}

// NewStaticDiscoveryFromNodes creates a static discovery service from a list of nodes
func NewStaticDiscoveryFromNodes(nodes []NodeInfo, logger common.Logger, metrics common.Metrics) *StaticDiscovery {
	sd := &StaticDiscovery{
		nodes:    make(map[string]NodeInfo),
		watchers: make([]NodeChangeCallback, 0),
		config: StaticDiscoveryConfig{
			Namespace:       "default",
			NodeTTL:         5 * time.Minute,
			RefreshInterval: 30 * time.Second,
			EnableWatch:     true,
		},
		logger:    logger,
		metrics:   metrics,
		startTime: time.Now(),
		stats: DiscoveryStats{
			LastUpdate: time.Now(),
		},
	}

	// Add nodes
	for _, node := range nodes {
		sd.nodes[node.ID] = node
	}

	// Update stats
	sd.updateStats()

	return sd
}
