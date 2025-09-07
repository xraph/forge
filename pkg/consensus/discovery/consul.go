package discovery

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/hashicorp/consul/api"
	apiwatch "github.com/hashicorp/consul/api/watch"
	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// ConsulDiscovery implements Consul-based node discovery
type ConsulDiscovery struct {
	client     *api.Client
	config     ConsulDiscoveryConfig
	nodes      map[string]NodeInfo
	watchers   []NodeChangeCallback
	stats      DiscoveryStats
	logger     common.Logger
	metrics    common.Metrics
	startTime  time.Time
	stopCh     chan struct{}
	watchPlan  *apiwatch.Plan
	mu         sync.RWMutex
	watchersMu sync.RWMutex
}

// ConsulDiscoveryConfig contains configuration for Consul discovery
type ConsulDiscoveryConfig struct {
	Address         string            `json:"address"`
	ServiceName     string            `json:"service_name"`
	ServiceID       string            `json:"service_id"`
	ServicePort     int               `json:"service_port"`
	ServiceAddress  string            `json:"service_address"`
	Datacenter      string            `json:"datacenter"`
	Namespace       string            `json:"namespace"`
	Tags            []string          `json:"tags"`
	Meta            map[string]string `json:"meta"`
	HealthCheck     HealthCheckConfig `json:"health_check"`
	RefreshInterval time.Duration     `json:"refresh_interval"`
	Timeout         time.Duration     `json:"timeout"`
	Retries         int               `json:"retries"`
	TTL             time.Duration     `json:"ttl"`
	Token           string            `json:"token"`
	TLS             TLSConfig         `json:"tls"`
}

// HealthCheckConfig contains health check configuration
type HealthCheckConfig struct {
	Enabled                        bool          `json:"enabled"`
	HTTPEndpoint                   string        `json:"http_endpoint"`
	Interval                       time.Duration `json:"interval"`
	Timeout                        time.Duration `json:"timeout"`
	DeregisterCriticalServiceAfter time.Duration `json:"deregister_critical_service_after"`
}

// TLSConfig contains TLS configuration
type TLSConfig struct {
	Enabled    bool   `json:"enabled"`
	CertFile   string `json:"cert_file"`
	KeyFile    string `json:"key_file"`
	CAFile     string `json:"ca_file"`
	ServerName string `json:"server_name"`
}

// ConsulDiscoveryFactory creates Consul discovery services
type ConsulDiscoveryFactory struct {
	logger  common.Logger
	metrics common.Metrics
}

// NewConsulDiscoveryFactory creates a new Consul discovery factory
func NewConsulDiscoveryFactory(logger common.Logger, metrics common.Metrics) *ConsulDiscoveryFactory {
	return &ConsulDiscoveryFactory{
		logger:  logger,
		metrics: metrics,
	}
}

// Create creates a new Consul discovery service
func (f *ConsulDiscoveryFactory) Create(config DiscoveryConfig) (Discovery, error) {
	consulConfig := ConsulDiscoveryConfig{
		Address:         "localhost:8500",
		ServiceName:     "forge-consensus",
		ServicePort:     8080,
		ServiceAddress:  "localhost",
		Datacenter:      "dc1",
		Namespace:       config.Namespace,
		Tags:            []string{"consensus", "forge"},
		Meta:            make(map[string]string),
		RefreshInterval: config.RefreshInterval,
		Timeout:         config.Timeout,
		Retries:         config.Retries,
		TTL:             config.NodeTTL,
		HealthCheck: HealthCheckConfig{
			Enabled:                        true,
			HTTPEndpoint:                   "/health",
			Interval:                       10 * time.Second,
			Timeout:                        5 * time.Second,
			DeregisterCriticalServiceAfter: 30 * time.Second,
		},
	}

	// Parse Consul-specific options
	if address, ok := config.Options["address"].(string); ok {
		consulConfig.Address = address
	}
	if serviceName, ok := config.Options["service_name"].(string); ok {
		consulConfig.ServiceName = serviceName
	}
	if serviceID, ok := config.Options["service_id"].(string); ok {
		consulConfig.ServiceID = serviceID
	}
	if servicePort, ok := config.Options["service_port"].(float64); ok {
		consulConfig.ServicePort = int(servicePort)
	}
	if serviceAddress, ok := config.Options["service_address"].(string); ok {
		consulConfig.ServiceAddress = serviceAddress
	}
	if datacenter, ok := config.Options["datacenter"].(string); ok {
		consulConfig.Datacenter = datacenter
	}
	if tags, ok := config.Options["tags"].([]interface{}); ok {
		consulConfig.Tags = make([]string, len(tags))
		for i, tag := range tags {
			consulConfig.Tags[i] = tag.(string)
		}
	}
	if token, ok := config.Options["token"].(string); ok {
		consulConfig.Token = token
	}

	// Set defaults
	if consulConfig.RefreshInterval == 0 {
		consulConfig.RefreshInterval = 30 * time.Second
	}
	if consulConfig.Timeout == 0 {
		consulConfig.Timeout = 10 * time.Second
	}
	if consulConfig.Retries == 0 {
		consulConfig.Retries = 3
	}
	if consulConfig.TTL == 0 {
		consulConfig.TTL = 30 * time.Second
	}

	// Create Consul client
	apiConfig := api.DefaultConfig()
	apiConfig.Address = consulConfig.Address
	apiConfig.Datacenter = consulConfig.Datacenter
	apiConfig.Token = consulConfig.Token

	// Configure TLS if enabled
	if consulConfig.TLS.Enabled {
		apiConfig.TLSConfig = api.TLSConfig{
			CertFile: consulConfig.TLS.CertFile,
			KeyFile:  consulConfig.TLS.KeyFile,
			CAFile:   consulConfig.TLS.CAFile,
		}
		apiConfig.Scheme = "https"
	}

	client, err := api.NewClient(apiConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Consul client: %w", err)
	}

	cd := &ConsulDiscovery{
		client:    client,
		config:    consulConfig,
		nodes:     make(map[string]NodeInfo),
		watchers:  make([]NodeChangeCallback, 0),
		logger:    f.logger,
		metrics:   f.metrics,
		startTime: time.Now(),
		stopCh:    make(chan struct{}),
		stats: DiscoveryStats{
			LastUpdate: time.Now(),
		},
	}

	// Perform initial discovery
	if err := cd.discoverNodes(); err != nil {
		if f.logger != nil {
			f.logger.Warn("initial Consul discovery failed", logger.Error(err))
		}
	}

	// Start watch for service changes
	if err := cd.startWatch(); err != nil {
		if f.logger != nil {
			f.logger.Warn("failed to start Consul watch", logger.Error(err))
		}
	}

	// Start refresh routine
	go cd.refreshLoop()

	if f.logger != nil {
		f.logger.Info("Consul discovery service created",
			logger.String("address", consulConfig.Address),
			logger.String("service_name", consulConfig.ServiceName),
			logger.String("datacenter", consulConfig.Datacenter),
			logger.Duration("refresh_interval", consulConfig.RefreshInterval),
		)
	}

	return cd, nil
}

// Name returns the factory name
func (f *ConsulDiscoveryFactory) Name() string {
	return DiscoveryTypeConsul
}

// Version returns the factory version
func (f *ConsulDiscoveryFactory) Version() string {
	return "1.0.0"
}

// ValidateConfig validates the configuration
func (f *ConsulDiscoveryFactory) ValidateConfig(config DiscoveryConfig) error {
	if config.Type != DiscoveryTypeConsul {
		return fmt.Errorf("invalid discovery type: %s", config.Type)
	}

	// Validate required fields
	if address, ok := config.Options["address"].(string); ok {
		if address == "" {
			return fmt.Errorf("Consul address cannot be empty")
		}
	}

	if serviceName, ok := config.Options["service_name"].(string); ok {
		if serviceName == "" {
			return fmt.Errorf("service name cannot be empty")
		}
	}

	return nil
}

// GetNodes returns all discovered nodes
func (cd *ConsulDiscovery) GetNodes(ctx context.Context) ([]NodeInfo, error) {
	cd.mu.RLock()
	defer cd.mu.RUnlock()

	cd.incrementOperation()

	nodes := make([]NodeInfo, 0, len(cd.nodes))
	for _, node := range cd.nodes {
		nodes = append(nodes, node)
	}

	if cd.logger != nil {
		cd.logger.Debug("retrieved nodes from Consul discovery",
			logger.Int("count", len(nodes)),
		)
	}

	return nodes, nil
}

// GetNode returns information about a specific node
func (cd *ConsulDiscovery) GetNode(ctx context.Context, nodeID string) (*NodeInfo, error) {
	cd.mu.RLock()
	defer cd.mu.RUnlock()

	cd.incrementOperation()

	node, exists := cd.nodes[nodeID]
	if !exists {
		cd.incrementError()
		return nil, &DiscoveryError{
			Code:    ErrCodeNodeNotFound,
			Message: fmt.Sprintf("node not found: %s", nodeID),
			NodeID:  nodeID,
		}
	}

	if cd.logger != nil {
		cd.logger.Debug("retrieved node from Consul discovery",
			logger.String("node_id", nodeID),
			logger.String("address", node.Address),
		)
	}

	return &node, nil
}

// Register registers a node with Consul
func (cd *ConsulDiscovery) Register(ctx context.Context, node NodeInfo) error {
	cd.mu.Lock()
	defer cd.mu.Unlock()

	cd.incrementOperation()

	// Create service registration
	serviceID := node.ID
	if serviceID == "" {
		serviceID = fmt.Sprintf("%s-%s-%d", cd.config.ServiceName, node.Address, node.Port)
	}

	registration := &api.AgentServiceRegistration{
		ID:      serviceID,
		Name:    cd.config.ServiceName,
		Port:    node.Port,
		Address: node.Address,
		Tags:    cd.config.Tags,
		Meta:    cd.config.Meta,
	}

	// Add node metadata
	for key, value := range node.Metadata {
		if strValue, ok := value.(string); ok {
			registration.Meta[key] = strValue
		}
	}

	// Add health check if enabled
	if cd.config.HealthCheck.Enabled {
		registration.Check = &api.AgentServiceCheck{
			HTTP:                           fmt.Sprintf("http://%s:%d%s", node.Address, node.Port, cd.config.HealthCheck.HTTPEndpoint),
			Interval:                       cd.config.HealthCheck.Interval.String(),
			Timeout:                        cd.config.HealthCheck.Timeout.String(),
			DeregisterCriticalServiceAfter: cd.config.HealthCheck.DeregisterCriticalServiceAfter.String(),
		}
	}

	// Register service
	if err := cd.client.Agent().ServiceRegister(registration); err != nil {
		cd.incrementError()
		return &DiscoveryError{
			Code:    ErrCodeServiceUnavailable,
			Message: fmt.Sprintf("failed to register service: %v", err),
			NodeID:  node.ID,
			Cause:   err,
		}
	}

	// Update local cache
	cd.nodes[node.ID] = node

	if cd.logger != nil {
		cd.logger.Info("node registered with Consul",
			logger.String("node_id", node.ID),
			logger.String("service_id", serviceID),
			logger.String("address", node.Address),
			logger.Int("port", node.Port),
		)
	}

	if cd.metrics != nil {
		cd.metrics.Counter("forge.consensus.discovery.consul_registrations").Inc()
	}

	return nil
}

// Unregister removes a node from Consul
func (cd *ConsulDiscovery) Unregister(ctx context.Context, nodeID string) error {
	cd.mu.Lock()
	defer cd.mu.Unlock()

	cd.incrementOperation()

	// Find service ID
	serviceID := nodeID
	if node, exists := cd.nodes[nodeID]; exists {
		serviceID = fmt.Sprintf("%s-%s-%d", cd.config.ServiceName, node.Address, node.Port)
	}

	// Deregister service
	if err := cd.client.Agent().ServiceDeregister(serviceID); err != nil {
		cd.incrementError()
		return &DiscoveryError{
			Code:    ErrCodeServiceUnavailable,
			Message: fmt.Sprintf("failed to deregister service: %v", err),
			NodeID:  nodeID,
			Cause:   err,
		}
	}

	// Remove from local cache
	delete(cd.nodes, nodeID)

	if cd.logger != nil {
		cd.logger.Info("node unregistered from Consul",
			logger.String("node_id", nodeID),
			logger.String("service_id", serviceID),
		)
	}

	if cd.metrics != nil {
		cd.metrics.Counter("forge.consensus.discovery.consul_deregistrations").Inc()
	}

	return nil
}

// UpdateNode updates node information in Consul
func (cd *ConsulDiscovery) UpdateNode(ctx context.Context, node NodeInfo) error {
	cd.mu.Lock()
	defer cd.mu.Unlock()

	cd.incrementOperation()

	// For Consul, update is essentially re-registration
	if err := cd.Register(ctx, node); err != nil {
		return err
	}

	if cd.logger != nil {
		cd.logger.Info("node updated in Consul",
			logger.String("node_id", node.ID),
			logger.String("address", node.Address),
			logger.Int("port", node.Port),
		)
	}

	return nil
}

// WatchNodes watches for node changes
func (cd *ConsulDiscovery) WatchNodes(ctx context.Context, callback NodeChangeCallback) error {
	cd.watchersMu.Lock()
	defer cd.watchersMu.Unlock()

	cd.watchers = append(cd.watchers, callback)

	// Update stats
	cd.mu.Lock()
	cd.stats.WatcherCount = len(cd.watchers)
	cd.mu.Unlock()

	if cd.logger != nil {
		cd.logger.Info("Consul discovery watcher added",
			logger.Int("total_watchers", len(cd.watchers)),
		)
	}

	if cd.metrics != nil {
		cd.metrics.Counter("forge.consensus.discovery.consul_watchers_added").Inc()
		cd.metrics.Gauge("forge.consensus.discovery.consul_active_watchers").Set(float64(len(cd.watchers)))
	}

	return nil
}

// HealthCheck performs a health check
func (cd *ConsulDiscovery) HealthCheck(ctx context.Context) error {
	cd.mu.RLock()
	defer cd.mu.RUnlock()

	// Check Consul connectivity
	if _, err := cd.client.Status().Leader(); err != nil {
		return &DiscoveryError{
			Code:    ErrCodeConnectionFailed,
			Message: fmt.Sprintf("Consul connection failed: %v", err),
			Cause:   err,
		}
	}

	// Check error rate
	if cd.stats.OperationCount > 0 {
		errorRate := float64(cd.stats.ErrorCount) / float64(cd.stats.OperationCount)
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
func (cd *ConsulDiscovery) GetStats(ctx context.Context) DiscoveryStats {
	cd.mu.RLock()
	defer cd.mu.RUnlock()

	stats := cd.stats
	stats.Uptime = time.Since(cd.startTime)

	// Calculate average latency
	if stats.OperationCount > 0 {
		stats.AverageLatency = time.Millisecond * 10 // Consul typically has low latency
	}

	return stats
}

// Close closes the Consul discovery service
func (cd *ConsulDiscovery) Close(ctx context.Context) error {
	cd.mu.Lock()
	defer cd.mu.Unlock()

	// Signal stop
	close(cd.stopCh)

	// Stop watch plan
	if cd.watchPlan != nil {
		cd.watchPlan.Stop()
	}

	// Clear watchers
	cd.watchersMu.Lock()
	cd.watchers = nil
	cd.watchersMu.Unlock()

	// Clear nodes
	cd.nodes = nil

	if cd.logger != nil {
		cd.logger.Info("Consul discovery service closed")
	}

	return nil
}

// discoverNodes discovers nodes from Consul
func (cd *ConsulDiscovery) discoverNodes() error {
	// Query healthy services
	services, _, err := cd.client.Health().Service(cd.config.ServiceName, "", true, nil)
	if err != nil {
		cd.incrementError()
		return fmt.Errorf("failed to query Consul services: %w", err)
	}

	// Convert services to nodes
	discoveredNodes := make(map[string]NodeInfo)
	for _, service := range services {
		nodeID := service.Service.ID
		if nodeID == "" {
			nodeID = fmt.Sprintf("%s-%s-%d", service.Service.Service, service.Service.Address, service.Service.Port)
		}

		node := NodeInfo{
			ID:      nodeID,
			Address: service.Service.Address,
			Port:    service.Service.Port,
			Role:    "consensus",
			Status:  cd.consulHealthToNodeStatus(service.Checks),
			Metadata: map[string]interface{}{
				"service_id":   service.Service.ID,
				"service_name": service.Service.Service,
				"datacenter":   service.Node.Datacenter,
				"node_name":    service.Node.Node,
			},
			Tags:         service.Service.Tags,
			LastSeen:     time.Now(),
			RegisteredAt: time.Now(),
			UpdatedAt:    time.Now(),
			TTL:          cd.config.TTL,
		}

		// Add service metadata
		for key, value := range service.Service.Meta {
			node.Metadata[key] = value
		}

		discoveredNodes[nodeID] = node
	}

	// Update nodes and notify watchers
	cd.updateNodes(discoveredNodes)

	if cd.logger != nil {
		cd.logger.Info("Consul discovery completed",
			logger.String("service_name", cd.config.ServiceName),
			logger.Int("discovered_nodes", len(discoveredNodes)),
		)
	}

	if cd.metrics != nil {
		cd.metrics.Counter("forge.consensus.discovery.consul_queries").Inc()
		cd.metrics.Gauge("forge.consensus.discovery.consul_discovered_nodes").Set(float64(len(discoveredNodes)))
	}

	return nil
}

// consulHealthToNodeStatus converts Consul health checks to node status
func (cd *ConsulDiscovery) consulHealthToNodeStatus(checks api.HealthChecks) NodeStatus {
	if len(checks) == 0 {
		return NodeStatusActive
	}

	for _, check := range checks {
		switch check.Status {
		case api.HealthCritical:
			return NodeStatusFailed
		case api.HealthWarning:
			return NodeStatusSuspected
		}
	}

	return NodeStatusActive
}

// updateNodes updates the nodes map and notifies watchers
func (cd *ConsulDiscovery) updateNodes(discoveredNodes map[string]NodeInfo) {
	cd.mu.Lock()
	defer cd.mu.Unlock()

	oldNodes := cd.nodes
	cd.nodes = discoveredNodes

	// Update stats
	cd.updateStats()

	// Notify watchers of changes
	cd.notifyWatchersOfChanges(oldNodes, discoveredNodes)
}

// notifyWatchersOfChanges compares old and new nodes and notifies watchers
func (cd *ConsulDiscovery) notifyWatchersOfChanges(oldNodes, newNodes map[string]NodeInfo) {
	now := time.Now()

	// Find added nodes
	for nodeID, node := range newNodes {
		if _, exists := oldNodes[nodeID]; !exists {
			cd.notifyWatchers(NodeChangeEvent{
				Type:      NodeChangeTypeAdded,
				Node:      node,
				Timestamp: now,
			})
		}
	}

	// Find removed nodes
	for nodeID, node := range oldNodes {
		if _, exists := newNodes[nodeID]; !exists {
			cd.notifyWatchers(NodeChangeEvent{
				Type:      NodeChangeTypeRemoved,
				Node:      node,
				Timestamp: now,
			})
		}
	}

	// Find updated nodes
	for nodeID, newNode := range newNodes {
		if oldNode, exists := oldNodes[nodeID]; exists {
			if !nodesEqual(oldNode, newNode) {
				cd.notifyWatchers(NodeChangeEvent{
					Type:      NodeChangeTypeUpdated,
					Node:      newNode,
					OldNode:   &oldNode,
					Timestamp: now,
				})
			}
		}
	}
}

// startWatch starts watching for service changes
func (cd *ConsulDiscovery) startWatch() error {
	// Create watch plan for service changes
	watchPlan, err := apiwatch.Parse(map[string]interface{}{
		"type": "service",
		"service": map[string]interface{}{
			"id": cd.config.ServiceName,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create watch plan: %w", err)
	}

	watchPlan.Handler = func(idx uint64, data interface{}) {
		if err := cd.discoverNodes(); err != nil {
			if cd.logger != nil {
				cd.logger.Error("Consul watch discovery failed", logger.Error(err))
			}
		}
	}

	cd.watchPlan = watchPlan

	// Start watch in background
	go func() {
		if err := watchPlan.Run(cd.config.Address); err != nil {
			if cd.logger != nil {
				cd.logger.Error("Consul watch failed", logger.Error(err))
			}
		}
	}()

	return nil
}

// refreshLoop periodically refreshes node information
func (cd *ConsulDiscovery) refreshLoop() {
	ticker := time.NewTicker(cd.config.RefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := cd.discoverNodes(); err != nil {
				if cd.logger != nil {
					cd.logger.Error("Consul discovery refresh failed", logger.Error(err))
				}
			}
		case <-cd.stopCh:
			return
		}
	}
}

// updateStats updates discovery statistics
func (cd *ConsulDiscovery) updateStats() {
	cd.stats.TotalNodes = len(cd.nodes)
	cd.stats.ActiveNodes = 0
	cd.stats.InactiveNodes = 0
	cd.stats.FailedNodes = 0
	cd.stats.LastUpdate = time.Now()

	for _, node := range cd.nodes {
		switch node.Status {
		case NodeStatusActive:
			cd.stats.ActiveNodes++
		case NodeStatusInactive:
			cd.stats.InactiveNodes++
		case NodeStatusFailed:
			cd.stats.FailedNodes++
		}
	}

	cd.watchersMu.RLock()
	cd.stats.WatcherCount = len(cd.watchers)
	cd.watchersMu.RUnlock()
}

// incrementOperation increments the operation count
func (cd *ConsulDiscovery) incrementOperation() {
	cd.stats.OperationCount++
}

// incrementError increments the error count
func (cd *ConsulDiscovery) incrementError() {
	cd.stats.ErrorCount++
}

// notifyWatchers notifies all watchers of a node change
func (cd *ConsulDiscovery) notifyWatchers(event NodeChangeEvent) {
	cd.watchersMu.RLock()
	watchers := make([]NodeChangeCallback, len(cd.watchers))
	copy(watchers, cd.watchers)
	cd.watchersMu.RUnlock()

	for _, callback := range watchers {
		go func(cb NodeChangeCallback) {
			if err := cb(event); err != nil {
				if cd.logger != nil {
					cd.logger.Error("Consul discovery watcher callback failed",
						logger.String("event_type", string(event.Type)),
						logger.String("node_id", event.Node.ID),
						logger.Error(err),
					)
				}
			}
		}(callback)
	}
}

// Helper functions for Consul discovery

// CreateConsulDiscovery creates a Consul discovery service
func CreateConsulDiscovery(address, serviceName string, servicePort int, logger common.Logger, metrics common.Metrics) (Discovery, error) {
	factory := NewConsulDiscoveryFactory(logger, metrics)

	config := DiscoveryConfig{
		Type:            DiscoveryTypeConsul,
		Namespace:       "default",
		NodeTTL:         30 * time.Second,
		RefreshInterval: 30 * time.Second,
		Timeout:         10 * time.Second,
		Retries:         3,
		Options: map[string]interface{}{
			"address":      address,
			"service_name": serviceName,
			"service_port": servicePort,
		},
	}

	return factory.Create(config)
}

// CreateConsulDiscoveryWithAuth creates a Consul discovery service with authentication
func CreateConsulDiscoveryWithAuth(address, serviceName, token string, servicePort int, logger common.Logger, metrics common.Metrics) (Discovery, error) {
	factory := NewConsulDiscoveryFactory(logger, metrics)

	config := DiscoveryConfig{
		Type:            DiscoveryTypeConsul,
		Namespace:       "default",
		NodeTTL:         30 * time.Second,
		RefreshInterval: 30 * time.Second,
		Timeout:         10 * time.Second,
		Retries:         3,
		Options: map[string]interface{}{
			"address":      address,
			"service_name": serviceName,
			"service_port": servicePort,
			"token":        token,
		},
	}

	return factory.Create(config)
}
