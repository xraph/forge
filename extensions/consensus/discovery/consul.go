package discovery

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// ConsulDiscovery implements Consul-based service discovery
type ConsulDiscovery struct {
	client      *api.Client
	serviceName string
	nodeID      string
	address     string
	port        int
	tags        []string
	logger      forge.Logger

	// Caching
	cachedPeers []internal.NodeInfo
	cacheMu     sync.RWMutex
	cacheExpiry time.Time
	cacheTTL    time.Duration

	// Lifecycle
	started       bool
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	mu            sync.RWMutex
	checkInterval time.Duration
}

// ConsulDiscoveryConfig contains Consul discovery configuration
type ConsulDiscoveryConfig struct {
	Address       string
	Datacenter    string
	Token         string
	ServiceName   string
	NodeID        string
	NodeAddress   string
	NodePort      int
	Tags          []string
	CheckInterval time.Duration
	CacheTTL      time.Duration
	HealthCheck   *api.AgentServiceCheck
}

// NewConsulDiscovery creates a new Consul service discovery
func NewConsulDiscovery(config ConsulDiscoveryConfig, logger forge.Logger) (*ConsulDiscovery, error) {
	// Create Consul client
	consulConfig := api.DefaultConfig()
	consulConfig.Address = config.Address
	consulConfig.Datacenter = config.Datacenter
	consulConfig.Token = config.Token

	client, err := api.NewClient(consulConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create consul client: %w", err)
	}

	if config.ServiceName == "" {
		config.ServiceName = "forge-consensus"
	}

	if config.CheckInterval == 0 {
		config.CheckInterval = 30 * time.Second
	}

	if config.CacheTTL == 0 {
		config.CacheTTL = 10 * time.Second
	}

	return &ConsulDiscovery{
		client:        client,
		serviceName:   config.ServiceName,
		nodeID:        config.NodeID,
		address:       config.NodeAddress,
		port:          config.NodePort,
		tags:          config.Tags,
		logger:        logger,
		cacheTTL:      config.CacheTTL,
		checkInterval: config.CheckInterval,
	}, nil
}

// Start starts the discovery service
func (cd *ConsulDiscovery) Start(ctx context.Context) error {
	cd.mu.Lock()
	defer cd.mu.Unlock()

	if cd.started {
		return internal.ErrAlreadyStarted
	}

	cd.ctx, cd.cancel = context.WithCancel(ctx)

	// Register this node as a service
	registration := &api.AgentServiceRegistration{
		ID:      cd.nodeID,
		Name:    cd.serviceName,
		Address: cd.address,
		Port:    cd.port,
		Tags:    cd.tags,
		Meta: map[string]string{
			"node_id": cd.nodeID,
		},
		Check: &api.AgentServiceCheck{
			TTL:                            "30s",
			DeregisterCriticalServiceAfter: "90s",
		},
	}

	if err := cd.client.Agent().ServiceRegister(registration); err != nil {
		return fmt.Errorf("failed to register service: %w", err)
	}

	cd.started = true

	// Start health check updater
	cd.wg.Add(1)
	go cd.healthCheckLoop()

	// Start peer discovery loop
	cd.wg.Add(1)
	go cd.discoveryLoop()

	cd.logger.Info("consul discovery started",
		forge.F("service_name", cd.serviceName),
		forge.F("node_id", cd.nodeID),
		forge.F("address", fmt.Sprintf("%s:%d", cd.address, cd.port)),
	)

	return nil
}

// Stop stops the discovery service
func (cd *ConsulDiscovery) Stop(ctx context.Context) error {
	cd.mu.Lock()
	if !cd.started {
		cd.mu.Unlock()
		return internal.ErrNotStarted
	}
	cd.mu.Unlock()

	if cd.cancel != nil {
		cd.cancel()
	}

	// Deregister service
	if err := cd.client.Agent().ServiceDeregister(cd.nodeID); err != nil {
		cd.logger.Error("failed to deregister service",
			forge.F("error", err),
		)
	}

	// Wait for goroutines
	done := make(chan struct{})
	go func() {
		cd.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		cd.logger.Info("consul discovery stopped")
	case <-ctx.Done():
		cd.logger.Warn("consul discovery stop timed out")
	}

	return nil
}

// DiscoverPeers discovers peer nodes
func (cd *ConsulDiscovery) DiscoverPeers(ctx context.Context) ([]internal.NodeInfo, error) {
	// Check cache first
	cd.cacheMu.RLock()
	if time.Now().Before(cd.cacheExpiry) && len(cd.cachedPeers) > 0 {
		peers := make([]internal.NodeInfo, len(cd.cachedPeers))
		copy(peers, cd.cachedPeers)
		cd.cacheMu.RUnlock()
		return peers, nil
	}
	cd.cacheMu.RUnlock()

	// Query Consul for healthy services
	services, _, err := cd.client.Health().Service(cd.serviceName, "", true, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to query consul: %w", err)
	}

	var peers []internal.NodeInfo
	for _, service := range services {
		// Skip ourselves
		if service.Service.ID == cd.nodeID {
			continue
		}

		nodeID := service.Service.Meta["node_id"]
		if nodeID == "" {
			nodeID = service.Service.ID
		}

		peers = append(peers, internal.NodeInfo{
			ID:      nodeID,
			Address: service.Service.Address,
			Port:    service.Service.Port,
			Role:    internal.RoleFollower,
			Status:  internal.StatusActive,
		})
	}

	// Update cache
	cd.cacheMu.Lock()
	cd.cachedPeers = peers
	cd.cacheExpiry = time.Now().Add(cd.cacheTTL)
	cd.cacheMu.Unlock()

	cd.logger.Debug("discovered peers",
		forge.F("count", len(peers)),
	)

	return peers, nil
}

// healthCheckLoop maintains the health check
func (cd *ConsulDiscovery) healthCheckLoop() {
	defer cd.wg.Done()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Update TTL check
			checkID := fmt.Sprintf("service:%s", cd.nodeID)
			if err := cd.client.Agent().UpdateTTL(checkID, "healthy", api.HealthPassing); err != nil {
				cd.logger.Error("failed to update health check",
					forge.F("error", err),
				)
			}

		case <-cd.ctx.Done():
			return
		}
	}
}

// discoveryLoop periodically discovers peers
func (cd *ConsulDiscovery) discoveryLoop() {
	defer cd.wg.Done()

	ticker := time.NewTicker(cd.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Pre-fetch peers to refresh cache
			if _, err := cd.DiscoverPeers(cd.ctx); err != nil {
				cd.logger.Error("failed to discover peers",
					forge.F("error", err),
				)
			}

		case <-cd.ctx.Done():
			return
		}
	}
}

// GetNodeInfo returns information about a specific node
func (cd *ConsulDiscovery) GetNodeInfo(nodeID string) (*internal.NodeInfo, error) {
	peers, err := cd.DiscoverPeers(cd.ctx)
	if err != nil {
		return nil, err
	}

	for _, peer := range peers {
		if peer.ID == nodeID {
			return &peer, nil
		}
	}

	return nil, internal.ErrNodeNotFound
}

// WatchPeers watches for peer changes
func (cd *ConsulDiscovery) WatchPeers(ctx context.Context) (<-chan []internal.NodeInfo, error) {
	ch := make(chan []internal.NodeInfo, 1)

	cd.wg.Add(1)
	go func() {
		defer cd.wg.Done()
		defer close(ch)

		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		var lastPeers []internal.NodeInfo

		for {
			select {
			case <-ticker.C:
				peers, err := cd.DiscoverPeers(ctx)
				if err != nil {
					cd.logger.Error("failed to watch peers",
						forge.F("error", err),
					)
					continue
				}

				// Check if peers changed
				if !peersEqual(lastPeers, peers) {
					lastPeers = peers
					select {
					case ch <- peers:
					case <-ctx.Done():
						return
					}
				}

			case <-ctx.Done():
				return
			case <-cd.ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}

// peersEqual checks if two peer lists are equal
func peersEqual(a, b []internal.NodeInfo) bool {
	if len(a) != len(b) {
		return false
	}

	aMap := make(map[string]bool)
	for _, peer := range a {
		aMap[peer.ID] = true
	}

	for _, peer := range b {
		if !aMap[peer.ID] {
			return false
		}
	}

	return true
}
