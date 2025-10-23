package discovery

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// DNSDiscovery implements DNS-based service discovery
type DNSDiscovery struct {
	domain          string
	port            int
	refreshInterval time.Duration
	resolver        *net.Resolver

	nodes   map[string]internal.NodeInfo
	nodesMu sync.RWMutex
	logger  forge.Logger

	// Lifecycle
	started bool
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	mu      sync.RWMutex

	// Change notification
	changeListeners []chan internal.NodeChangeEvent
	listenersMu     sync.RWMutex
}

// DNSDiscoveryConfig contains DNS discovery configuration
type DNSDiscoveryConfig struct {
	Domain          string
	Port            int
	RefreshInterval time.Duration
	DNSServers      []string
}

// NewDNSDiscovery creates a new DNS-based discovery service
func NewDNSDiscovery(config DNSDiscoveryConfig, logger forge.Logger) *DNSDiscovery {
	if config.RefreshInterval == 0 {
		config.RefreshInterval = 30 * time.Second
	}

	if config.Port == 0 {
		config.Port = 7000
	}

	resolver := &net.Resolver{
		PreferGo: true,
	}

	if len(config.DNSServers) > 0 {
		// Custom DNS servers
		resolver.Dial = func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{}
			return d.DialContext(ctx, network, config.DNSServers[0])
		}
	}

	return &DNSDiscovery{
		domain:          config.Domain,
		port:            config.Port,
		refreshInterval: config.RefreshInterval,
		resolver:        resolver,
		nodes:           make(map[string]internal.NodeInfo),
		logger:          logger,
		changeListeners: make([]chan internal.NodeChangeEvent, 0),
	}
}

// Start starts the DNS discovery service
func (dd *DNSDiscovery) Start(ctx context.Context) error {
	dd.mu.Lock()
	defer dd.mu.Unlock()

	if dd.started {
		return internal.ErrAlreadyStarted
	}

	dd.ctx, dd.cancel = context.WithCancel(ctx)
	dd.started = true

	// Initial discovery
	if err := dd.refresh(); err != nil {
		dd.logger.Warn("initial DNS discovery failed",
			forge.F("error", err),
		)
	}

	// Start refresh loop
	dd.wg.Add(1)
	go dd.runRefreshLoop()

	dd.logger.Info("DNS discovery started",
		forge.F("domain", dd.domain),
		forge.F("port", dd.port),
		forge.F("refresh_interval", dd.refreshInterval),
	)

	return nil
}

// Stop stops the DNS discovery service
func (dd *DNSDiscovery) Stop(ctx context.Context) error {
	dd.mu.Lock()
	if !dd.started {
		dd.mu.Unlock()
		return internal.ErrNotStarted
	}
	dd.mu.Unlock()

	if dd.cancel != nil {
		dd.cancel()
	}

	// Wait for goroutines with timeout
	done := make(chan struct{})
	go func() {
		dd.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		dd.logger.Info("DNS discovery stopped")
	case <-ctx.Done():
		dd.logger.Warn("DNS discovery stop timed out")
	}

	// Close all change listeners
	dd.listenersMu.Lock()
	for _, ch := range dd.changeListeners {
		close(ch)
	}
	dd.changeListeners = nil
	dd.listenersMu.Unlock()

	return nil
}

// Register registers this node with the discovery service (no-op for DNS)
func (dd *DNSDiscovery) Register(ctx context.Context, node internal.NodeInfo) error {
	// DNS discovery is read-only, registration happens externally
	dd.logger.Info("register called (no-op for DNS discovery)")
	return nil
}

// Unregister unregisters this node from the discovery service (no-op for DNS)
func (dd *DNSDiscovery) Unregister(ctx context.Context) error {
	// DNS discovery is read-only, deregistration happens externally
	dd.logger.Info("unregister called (no-op for DNS discovery)")
	return nil
}

// GetNodes returns all discovered nodes
func (dd *DNSDiscovery) GetNodes(ctx context.Context) ([]internal.NodeInfo, error) {
	dd.nodesMu.RLock()
	defer dd.nodesMu.RUnlock()

	nodes := make([]internal.NodeInfo, 0, len(dd.nodes))
	for _, node := range dd.nodes {
		nodes = append(nodes, node)
	}

	return nodes, nil
}

// Watch watches for node changes
func (dd *DNSDiscovery) Watch(ctx context.Context) (<-chan internal.NodeChangeEvent, error) {
	ch := make(chan internal.NodeChangeEvent, 10)

	dd.listenersMu.Lock()
	dd.changeListeners = append(dd.changeListeners, ch)
	dd.listenersMu.Unlock()

	// Send initial nodes as "added" events
	dd.nodesMu.RLock()
	initialNodes := make([]internal.NodeInfo, 0, len(dd.nodes))
	for _, node := range dd.nodes {
		initialNodes = append(initialNodes, node)
	}
	dd.nodesMu.RUnlock()

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

// runRefreshLoop runs the refresh loop
func (dd *DNSDiscovery) runRefreshLoop() {
	defer dd.wg.Done()

	ticker := time.NewTicker(dd.refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-dd.ctx.Done():
			return

		case <-ticker.C:
			if err := dd.refresh(); err != nil {
				dd.logger.Warn("DNS refresh failed",
					forge.F("error", err),
				)
			}
		}
	}
}

// refresh performs DNS lookup and updates nodes
func (dd *DNSDiscovery) refresh() error {
	ctx, cancel := context.WithTimeout(dd.ctx, 10*time.Second)
	defer cancel()

	// Perform DNS lookup
	addrs, err := dd.resolver.LookupHost(ctx, dd.domain)
	if err != nil {
		return fmt.Errorf("DNS lookup failed: %w", err)
	}

	// Convert addresses to NodeInfo
	newNodes := make(map[string]internal.NodeInfo)
	for i, addr := range addrs {
		// Generate node ID from address
		nodeID := fmt.Sprintf("dns-%s-%d", addr, dd.port)

		node := internal.NodeInfo{
			ID:      nodeID,
			Address: addr,
			Port:    dd.port,
			Role:    internal.RoleFollower,
			Status:  internal.StatusActive,
		}

		newNodes[nodeID] = node

		dd.logger.Debug("discovered node via DNS",
			forge.F("node_id", nodeID),
			forge.F("address", addr),
			forge.F("index", i),
		)
	}

	// Determine changes
	dd.nodesMu.Lock()
	oldNodes := dd.nodes
	dd.nodes = newNodes
	dd.nodesMu.Unlock()

	// Notify listeners of changes
	dd.detectAndNotifyChanges(oldNodes, newNodes)

	dd.logger.Info("DNS discovery refreshed",
		forge.F("nodes", len(newNodes)),
	)

	return nil
}

// detectAndNotifyChanges detects and notifies listeners of node changes
func (dd *DNSDiscovery) detectAndNotifyChanges(oldNodes, newNodes map[string]internal.NodeInfo) {
	// Detect removed nodes
	for nodeID, node := range oldNodes {
		if _, exists := newNodes[nodeID]; !exists {
			dd.notifyListeners(internal.NodeChangeEvent{
				Type: internal.NodeChangeTypeRemoved,
				Node: node,
			})
		}
	}

	// Detect added nodes
	for nodeID, node := range newNodes {
		if _, exists := oldNodes[nodeID]; !exists {
			dd.notifyListeners(internal.NodeChangeEvent{
				Type: internal.NodeChangeTypeAdded,
				Node: node,
			})
		}
	}
}

// notifyListeners notifies all listeners of a node change
func (dd *DNSDiscovery) notifyListeners(event internal.NodeChangeEvent) {
	dd.listenersMu.RLock()
	listeners := dd.changeListeners
	dd.listenersMu.RUnlock()

	for _, ch := range listeners {
		select {
		case ch <- event:
		default:
			// Channel full, skip
		}
	}
}
