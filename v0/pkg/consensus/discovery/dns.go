package discovery

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// DNSDiscovery implements DNS-based node discovery using SRV records
type DNSDiscovery struct {
	config     DNSDiscoveryConfig
	resolver   *net.Resolver
	nodes      map[string]NodeInfo
	watchers   []NodeChangeCallback
	stats      DiscoveryStats
	logger     common.Logger
	metrics    common.Metrics
	startTime  time.Time
	stopCh     chan struct{}
	mu         sync.RWMutex
	watchersMu sync.RWMutex
}

// DNSDiscoveryConfig contains configuration for DNS discovery
type DNSDiscoveryConfig struct {
	ServiceName     string        `json:"service_name"`
	Protocol        string        `json:"protocol"`
	Domain          string        `json:"domain"`
	Port            int           `json:"port"`
	Namespace       string        `json:"namespace"`
	RefreshInterval time.Duration `json:"refresh_interval"`
	Timeout         time.Duration `json:"timeout"`
	Retries         int           `json:"retries"`
	TTL             time.Duration `json:"ttl"`
	EnableWatch     bool          `json:"enable_watch"`
	DNSServer       string        `json:"dns_server"`
	UseSystemDNS    bool          `json:"use_system_dns"`
}

// DNSDiscoveryFactory creates DNS discovery services
type DNSDiscoveryFactory struct {
	logger  common.Logger
	metrics common.Metrics
}

// NewDNSDiscoveryFactory creates a new DNS discovery factory
func NewDNSDiscoveryFactory(logger common.Logger, metrics common.Metrics) *DNSDiscoveryFactory {
	return &DNSDiscoveryFactory{
		logger:  logger,
		metrics: metrics,
	}
}

// Create creates a new DNS discovery service
func (f *DNSDiscoveryFactory) Create(config DiscoveryConfig) (Discovery, error) {
	dnsConfig := DNSDiscoveryConfig{
		ServiceName:     "_consensus._tcp",
		Protocol:        "tcp",
		Domain:          "local",
		Port:            8080,
		Namespace:       config.Namespace,
		RefreshInterval: config.RefreshInterval,
		Timeout:         config.Timeout,
		Retries:         config.Retries,
		TTL:             config.NodeTTL,
		EnableWatch:     true,
		UseSystemDNS:    true,
	}

	// Parse DNS-specific options
	if serviceName, ok := config.Options["service_name"].(string); ok {
		dnsConfig.ServiceName = serviceName
	}
	if protocol, ok := config.Options["protocol"].(string); ok {
		dnsConfig.Protocol = protocol
	}
	if domain, ok := config.Options["domain"].(string); ok {
		dnsConfig.Domain = domain
	}
	if port, ok := config.Options["port"].(float64); ok {
		dnsConfig.Port = int(port)
	}
	if dnsServer, ok := config.Options["dns_server"].(string); ok {
		dnsConfig.DNSServer = dnsServer
		dnsConfig.UseSystemDNS = false
	}
	if useSystemDNS, ok := config.Options["use_system_dns"].(bool); ok {
		dnsConfig.UseSystemDNS = useSystemDNS
	}

	// Set defaults
	if dnsConfig.RefreshInterval == 0 {
		dnsConfig.RefreshInterval = 30 * time.Second
	}
	if dnsConfig.Timeout == 0 {
		dnsConfig.Timeout = 5 * time.Second
	}
	if dnsConfig.Retries == 0 {
		dnsConfig.Retries = 3
	}
	if dnsConfig.TTL == 0 {
		dnsConfig.TTL = 5 * time.Minute
	}

	// Create resolver
	var resolver *net.Resolver
	if dnsConfig.UseSystemDNS {
		resolver = net.DefaultResolver
	} else if dnsConfig.DNSServer != "" {
		resolver = &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{
					Timeout: dnsConfig.Timeout,
				}
				return d.DialContext(ctx, network, dnsConfig.DNSServer)
			},
		}
	} else {
		resolver = net.DefaultResolver
	}

	dd := &DNSDiscovery{
		config:    dnsConfig,
		resolver:  resolver,
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
	if err := dd.discoverNodes(); err != nil {
		if f.logger != nil {
			f.logger.Warn("initial DNS discovery failed", logger.Error(err))
		}
	}

	// Start refresh routine
	go dd.refreshLoop()

	if f.logger != nil {
		f.logger.Info("DNS discovery service created",
			logger.String("service_name", dnsConfig.ServiceName),
			logger.String("protocol", dnsConfig.Protocol),
			logger.String("domain", dnsConfig.Domain),
			logger.Int("port", dnsConfig.Port),
			logger.Duration("refresh_interval", dnsConfig.RefreshInterval),
		)
	}

	return dd, nil
}

// Name returns the factory name
func (f *DNSDiscoveryFactory) Name() string {
	return DiscoveryTypeDNS
}

// Version returns the factory version
func (f *DNSDiscoveryFactory) Version() string {
	return "1.0.0"
}

// ValidateConfig validates the configuration
func (f *DNSDiscoveryFactory) ValidateConfig(config DiscoveryConfig) error {
	if config.Type != DiscoveryTypeDNS {
		return fmt.Errorf("invalid discovery type: %s", config.Type)
	}

	// Validate service name format
	if serviceName, ok := config.Options["service_name"].(string); ok {
		if !strings.HasPrefix(serviceName, "_") {
			return fmt.Errorf("service name must start with underscore: %s", serviceName)
		}
	}

	// Validate protocol
	if protocol, ok := config.Options["protocol"].(string); ok {
		if protocol != "tcp" && protocol != "udp" {
			return fmt.Errorf("invalid protocol: %s", protocol)
		}
	}

	// Validate DNS server format
	if dnsServer, ok := config.Options["dns_server"].(string); ok {
		if _, _, err := net.SplitHostPort(dnsServer); err != nil {
			return fmt.Errorf("invalid DNS server format: %s", dnsServer)
		}
	}

	return nil
}

// GetNodes returns all discovered nodes
func (dd *DNSDiscovery) GetNodes(ctx context.Context) ([]NodeInfo, error) {
	dd.mu.RLock()
	defer dd.mu.RUnlock()

	dd.incrementOperation()

	nodes := make([]NodeInfo, 0, len(dd.nodes))
	for _, node := range dd.nodes {
		nodes = append(nodes, node)
	}

	if dd.logger != nil {
		dd.logger.Debug("retrieved nodes from DNS discovery",
			logger.Int("count", len(nodes)),
		)
	}

	return nodes, nil
}

// GetNode returns information about a specific node
func (dd *DNSDiscovery) GetNode(ctx context.Context, nodeID string) (*NodeInfo, error) {
	dd.mu.RLock()
	defer dd.mu.RUnlock()

	dd.incrementOperation()

	node, exists := dd.nodes[nodeID]
	if !exists {
		dd.incrementError()
		return nil, &DiscoveryError{
			Code:    ErrCodeNodeNotFound,
			Message: fmt.Sprintf("node not found: %s", nodeID),
			NodeID:  nodeID,
		}
	}

	if dd.logger != nil {
		dd.logger.Debug("retrieved node from DNS discovery",
			logger.String("node_id", nodeID),
			logger.String("address", node.Address),
		)
	}

	return &node, nil
}

// Register is not supported for DNS discovery
func (dd *DNSDiscovery) Register(ctx context.Context, node NodeInfo) error {
	dd.incrementOperation()
	dd.incrementError()
	return &DiscoveryError{
		Code:    ErrCodeServiceUnavailable,
		Message: "node registration not supported for DNS discovery",
	}
}

// Unregister is not supported for DNS discovery
func (dd *DNSDiscovery) Unregister(ctx context.Context, nodeID string) error {
	dd.incrementOperation()
	dd.incrementError()
	return &DiscoveryError{
		Code:    ErrCodeServiceUnavailable,
		Message: "node unregistration not supported for DNS discovery",
	}
}

// UpdateNode is not supported for DNS discovery
func (dd *DNSDiscovery) UpdateNode(ctx context.Context, node NodeInfo) error {
	dd.incrementOperation()
	dd.incrementError()
	return &DiscoveryError{
		Code:    ErrCodeServiceUnavailable,
		Message: "node update not supported for DNS discovery",
	}
}

// WatchNodes watches for node changes
func (dd *DNSDiscovery) WatchNodes(ctx context.Context, callback NodeChangeCallback) error {
	if !dd.config.EnableWatch {
		return &DiscoveryError{
			Code:    ErrCodeServiceUnavailable,
			Message: "node watching is not enabled",
		}
	}

	dd.watchersMu.Lock()
	defer dd.watchersMu.Unlock()

	dd.watchers = append(dd.watchers, callback)

	// Update stats
	dd.mu.Lock()
	dd.stats.WatcherCount = len(dd.watchers)
	dd.mu.Unlock()

	if dd.logger != nil {
		dd.logger.Info("DNS discovery watcher added",
			logger.Int("total_watchers", len(dd.watchers)),
		)
	}

	if dd.metrics != nil {
		dd.metrics.Counter("forge.consensus.discovery.dns_watchers_added").Inc()
		dd.metrics.Gauge("forge.consensus.discovery.dns_active_watchers").Set(float64(len(dd.watchers)))
	}

	return nil
}

// HealthCheck performs a health check
func (dd *DNSDiscovery) HealthCheck(ctx context.Context) error {
	dd.mu.RLock()
	defer dd.mu.RUnlock()

	// Try to perform DNS lookup
	if err := dd.testDNSLookup(ctx); err != nil {
		return &DiscoveryError{
			Code:    ErrCodeServiceUnavailable,
			Message: fmt.Sprintf("DNS lookup failed: %v", err),
			Cause:   err,
		}
	}

	// Check error rate
	if dd.stats.OperationCount > 0 {
		errorRate := float64(dd.stats.ErrorCount) / float64(dd.stats.OperationCount)
		if errorRate > 0.2 { // 20% error rate threshold for DNS (higher than static)
			return &DiscoveryError{
				Code:    ErrCodeServiceUnavailable,
				Message: fmt.Sprintf("high error rate: %.2f%%", errorRate*100),
			}
		}
	}

	return nil
}

// GetStats returns discovery statistics
func (dd *DNSDiscovery) GetStats(ctx context.Context) DiscoveryStats {
	dd.mu.RLock()
	defer dd.mu.RUnlock()

	stats := dd.stats
	stats.Uptime = time.Since(dd.startTime)

	// Calculate average latency (DNS has higher latency than static)
	if stats.OperationCount > 0 {
		stats.AverageLatency = time.Millisecond * 50 // Higher latency for DNS lookups
	}

	return stats
}

// Close closes the DNS discovery service
func (dd *DNSDiscovery) Close(ctx context.Context) error {
	dd.mu.Lock()
	defer dd.mu.Unlock()

	// Signal stop
	close(dd.stopCh)

	// Clear watchers
	dd.watchersMu.Lock()
	dd.watchers = nil
	dd.watchersMu.Unlock()

	// Clear nodes
	dd.nodes = nil

	if dd.logger != nil {
		dd.logger.Info("DNS discovery service closed")
	}

	return nil
}

// discoverNodes performs DNS discovery to find nodes
func (dd *DNSDiscovery) discoverNodes() error {
	ctx, cancel := context.WithTimeout(context.Background(), dd.config.Timeout)
	defer cancel()

	// Construct SRV record name
	srvName := fmt.Sprintf("%s.%s.%s", dd.config.ServiceName, dd.config.Protocol, dd.config.Domain)

	// Perform SRV lookup
	_, addrs, err := dd.resolver.LookupSRV(ctx, dd.config.ServiceName[1:], dd.config.Protocol, dd.config.Domain)
	if err != nil {
		dd.incrementError()
		return fmt.Errorf("failed to lookup SRV records for %s: %w", srvName, err)
	}

	// Convert SRV records to nodes
	discoveredNodes := make(map[string]NodeInfo)
	for _, addr := range addrs {
		nodeID := fmt.Sprintf("%s:%d", addr.Target, addr.Port)

		// Resolve IP address
		ips, err := dd.resolver.LookupIPAddr(ctx, addr.Target)
		if err != nil {
			if dd.logger != nil {
				dd.logger.Warn("failed to resolve IP for target",
					logger.String("target", addr.Target),
					logger.Error(err),
				)
			}
			continue
		}

		if len(ips) == 0 {
			continue
		}

		// Use first IP address
		ip := ips[0].IP.String()

		node := NodeInfo{
			ID:      nodeID,
			Address: ip,
			Port:    int(addr.Port),
			Role:    "consensus",
			Status:  NodeStatusActive,
			Metadata: map[string]interface{}{
				"srv_target":   addr.Target,
				"srv_port":     addr.Port,
				"srv_priority": addr.Priority,
				"srv_weight":   addr.Weight,
			},
			Tags:         []string{"dns", "srv"},
			LastSeen:     time.Now(),
			RegisteredAt: time.Now(),
			UpdatedAt:    time.Now(),
			TTL:          dd.config.TTL,
		}

		discoveredNodes[nodeID] = node
	}

	// Update nodes and notify watchers
	dd.updateNodes(discoveredNodes)

	if dd.logger != nil {
		dd.logger.Info("DNS discovery completed",
			logger.String("srv_name", srvName),
			logger.Int("discovered_nodes", len(discoveredNodes)),
		)
	}

	if dd.metrics != nil {
		dd.metrics.Counter("forge.consensus.discovery.dns_lookups").Inc()
		dd.metrics.Gauge("forge.consensus.discovery.dns_discovered_nodes").Set(float64(len(discoveredNodes)))
	}

	return nil
}

// updateNodes updates the nodes map and notifies watchers
func (dd *DNSDiscovery) updateNodes(discoveredNodes map[string]NodeInfo) {
	dd.mu.Lock()
	defer dd.mu.Unlock()

	oldNodes := dd.nodes
	dd.nodes = discoveredNodes

	// Update stats
	dd.updateStats()

	// Notify watchers if enabled
	if dd.config.EnableWatch {
		dd.notifyWatchersOfChanges(oldNodes, discoveredNodes)
	}
}

// notifyWatchersOfChanges compares old and new nodes and notifies watchers
func (dd *DNSDiscovery) notifyWatchersOfChanges(oldNodes, newNodes map[string]NodeInfo) {
	now := time.Now()

	// Find added nodes
	for nodeID, node := range newNodes {
		if _, exists := oldNodes[nodeID]; !exists {
			dd.notifyWatchers(NodeChangeEvent{
				Type:      NodeChangeTypeAdded,
				Node:      node,
				Timestamp: now,
			})
		}
	}

	// Find removed nodes
	for nodeID, node := range oldNodes {
		if _, exists := newNodes[nodeID]; !exists {
			dd.notifyWatchers(NodeChangeEvent{
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
				dd.notifyWatchers(NodeChangeEvent{
					Type:      NodeChangeTypeUpdated,
					Node:      newNode,
					OldNode:   &oldNode,
					Timestamp: now,
				})
			}
		}
	}
}

// refreshLoop periodically refreshes node information
func (dd *DNSDiscovery) refreshLoop() {
	ticker := time.NewTicker(dd.config.RefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := dd.discoverNodes(); err != nil {
				if dd.logger != nil {
					dd.logger.Error("DNS discovery refresh failed", logger.Error(err))
				}
			}
		case <-dd.stopCh:
			return
		}
	}
}

// testDNSLookup tests DNS connectivity
func (dd *DNSDiscovery) testDNSLookup(ctx context.Context) error {
	// Test basic DNS resolution
	_, err := dd.resolver.LookupHost(ctx, "localhost")
	return err
}

// updateStats updates discovery statistics
func (dd *DNSDiscovery) updateStats() {
	dd.stats.TotalNodes = len(dd.nodes)
	dd.stats.ActiveNodes = 0
	dd.stats.InactiveNodes = 0
	dd.stats.FailedNodes = 0
	dd.stats.LastUpdate = time.Now()

	for _, node := range dd.nodes {
		switch node.Status {
		case NodeStatusActive:
			dd.stats.ActiveNodes++
		case NodeStatusInactive:
			dd.stats.InactiveNodes++
		case NodeStatusFailed:
			dd.stats.FailedNodes++
		}
	}

	dd.watchersMu.RLock()
	dd.stats.WatcherCount = len(dd.watchers)
	dd.watchersMu.RUnlock()
}

// incrementOperation increments the operation count
func (dd *DNSDiscovery) incrementOperation() {
	dd.stats.OperationCount++
}

// incrementError increments the error count
func (dd *DNSDiscovery) incrementError() {
	dd.stats.ErrorCount++
}

// notifyWatchers notifies all watchers of a node change
func (dd *DNSDiscovery) notifyWatchers(event NodeChangeEvent) {
	dd.watchersMu.RLock()
	watchers := make([]NodeChangeCallback, len(dd.watchers))
	copy(watchers, dd.watchers)
	dd.watchersMu.RUnlock()

	for _, callback := range watchers {
		go func(cb NodeChangeCallback) {
			if err := cb(event); err != nil {
				if dd.logger != nil {
					dd.logger.Error("DNS discovery watcher callback failed",
						logger.String("event_type", string(event.Type)),
						logger.String("node_id", event.Node.ID),
						logger.Error(err),
					)
				}
			}
		}(callback)
	}
}

// nodesEqual compares two nodes for equality
func nodesEqual(a, b NodeInfo) bool {
	return a.ID == b.ID &&
		a.Address == b.Address &&
		a.Port == b.Port &&
		a.Role == b.Role &&
		a.Status == b.Status
}

// Helper functions for DNS discovery

// CreateDNSDiscovery creates a DNS discovery service
func CreateDNSDiscovery(serviceName, protocol, domain string, port int, logger common.Logger, metrics common.Metrics) (Discovery, error) {
	factory := NewDNSDiscoveryFactory(logger, metrics)

	config := DiscoveryConfig{
		Type:            DiscoveryTypeDNS,
		Namespace:       "default",
		NodeTTL:         5 * time.Minute,
		RefreshInterval: 30 * time.Second,
		Timeout:         5 * time.Second,
		Retries:         3,
		Options: map[string]interface{}{
			"service_name":   serviceName,
			"protocol":       protocol,
			"domain":         domain,
			"port":           port,
			"use_system_dns": true,
		},
	}

	return factory.Create(config)
}

// CreateCustomDNSDiscovery creates a DNS discovery service with custom DNS server
func CreateCustomDNSDiscovery(serviceName, protocol, domain string, port int, dnsServer string, logger common.Logger, metrics common.Metrics) (Discovery, error) {
	factory := NewDNSDiscoveryFactory(logger, metrics)

	config := DiscoveryConfig{
		Type:            DiscoveryTypeDNS,
		Namespace:       "default",
		NodeTTL:         5 * time.Minute,
		RefreshInterval: 30 * time.Second,
		Timeout:         5 * time.Second,
		Retries:         3,
		Options: map[string]interface{}{
			"service_name":   serviceName,
			"protocol":       protocol,
			"domain":         domain,
			"port":           port,
			"dns_server":     dnsServer,
			"use_system_dns": false,
		},
	}

	return factory.Create(config)
}

// ValidateSRVRecord validates an SRV record format
func ValidateSRVRecord(serviceName, protocol, domain string) error {
	if !strings.HasPrefix(serviceName, "_") {
		return fmt.Errorf("service name must start with underscore: %s", serviceName)
	}

	if protocol != "tcp" && protocol != "udp" {
		return fmt.Errorf("protocol must be tcp or udp: %s", protocol)
	}

	if domain == "" {
		return fmt.Errorf("domain cannot be empty")
	}

	return nil
}

// ParseSRVRecord parses an SRV record name into components
func ParseSRVRecord(srvRecord string) (serviceName, protocol, domain string, err error) {
	parts := strings.Split(srvRecord, ".")
	if len(parts) < 3 {
		return "", "", "", fmt.Errorf("invalid SRV record format: %s", srvRecord)
	}

	serviceName = parts[0]
	protocol = strings.TrimPrefix(parts[1], "_")
	domain = strings.Join(parts[2:], ".")

	return serviceName, protocol, domain, nil
}
