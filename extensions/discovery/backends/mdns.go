package backends

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/grandcat/zeroconf"
)

// MDNSBackend implements service discovery using mDNS/DNS-SD
// This works natively on:
// - macOS (Bonjour)
// - Linux (Avahi)
// - Windows (DNS-SD).
type MDNSBackend struct {
	config   MDNSConfig
	services map[string]*registeredService // service ID -> registered service
	watchers map[string]*serviceWatcher    // service name -> watcher
	mu       sync.RWMutex
	ctx      context.Context
	cancel   context.CancelFunc
	logger   MDNSLogger
}

// log emits a diagnostic message via the configured logger (if any).
func (b *MDNSBackend) log(format string, args ...any) {
	if b.logger != nil {
		b.logger(format, args...)
	}
}

// registeredService tracks a registered mDNS service.
type registeredService struct {
	instance *ServiceInstance
	server   *zeroconf.Server
}

// serviceWatcher manages service watching.
type serviceWatcher struct {
	serviceName string
	entries     map[string]*zeroconf.ServiceEntry // service ID -> entry
	callbacks   []func([]*ServiceInstance)
	resolver    *zeroconf.Resolver
	cancel      context.CancelFunc
	mu          sync.RWMutex
}

// NewMDNSBackend creates a new mDNS service discovery backend.
func NewMDNSBackend(config MDNSConfig) (*MDNSBackend, error) {
	// Set defaults
	if config.Domain == "" {
		config.Domain = "local."
	}

	if config.BrowseTimeout == 0 {
		config.BrowseTimeout = 5 * time.Second
	}

	if config.WatchInterval == 0 {
		config.WatchInterval = 30 * time.Second
	}

	if config.TTL == 0 {
		config.TTL = 120 // 2 minutes
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &MDNSBackend{
		config:   config,
		services: make(map[string]*registeredService),
		watchers: make(map[string]*serviceWatcher),
		ctx:      ctx,
		cancel:   cancel,
		logger:   config.Logger,
	}, nil
}

// Name returns the backend name.
func (b *MDNSBackend) Name() string {
	return "mdns"
}

// Initialize initializes the backend.
func (b *MDNSBackend) Initialize(ctx context.Context) error {
	// Test mDNS availability by creating a test resolver
	_, err := zeroconf.NewResolver(nil)
	if err != nil {
		return fmt.Errorf("mDNS not available on this system: %w", err)
	}

	return nil
}

// Register registers a service instance via mDNS.
func (b *MDNSBackend) Register(ctx context.Context, instance *ServiceInstance) error {
	if instance.ID == "" {
		return errors.New("service instance ID is required")
	}

	if instance.Name == "" {
		return errors.New("service name is required")
	}

	if instance.Port == 0 {
		return errors.New("service port is required")
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// Check if already registered
	if _, exists := b.services[instance.ID]; exists {
		return fmt.Errorf("service instance already registered: %s", instance.ID)
	}

	// Get local addresses if not specified
	addresses := []string{}
	if instance.Address != "" {
		addresses = append(addresses, instance.Address)
	} else {
		// Get all local IP addresses
		addrs, err := net.InterfaceAddrs()
		if err != nil {
			return fmt.Errorf("failed to get interface addresses: %w", err)
		}

		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					addresses = append(addresses, ipnet.IP.String())
				} else if b.config.IPv6 && ipnet.IP.To16() != nil {
					addresses = append(addresses, ipnet.IP.String())
				}
			}
		}
	}

	if len(addresses) == 0 {
		return errors.New("no valid addresses found for service registration")
	}

	// Convert metadata to TXT records.
	// Include the service name so that gateways browsing a common ServiceType
	// can recover the real name (e.g. "Portal", "TwinOS") from TXT records.
	txt := make([]string, 0, len(instance.Metadata)+len(instance.Tags)+3)
	txt = append(txt, "name="+instance.Name)
	txt = append(txt, "version="+instance.Version)

	txt = append(txt, "id="+instance.ID)
	for k, v := range instance.Metadata {
		txt = append(txt, fmt.Sprintf("%s=%s", k, v))
	}

	if len(instance.Tags) > 0 {
		txt = append(txt, "tags="+strings.Join(instance.Tags, ","))
	}

	// Register mDNS service
	// Use configured service type or generate from service name
	serviceType := b.config.ServiceType
	if serviceType == "" {
		// Default: _<service-name>._tcp
		serviceType = fmt.Sprintf("_%s._tcp", sanitizeServiceName(instance.Name))
	}

	// Add service type to metadata for gateway discovery
	txt = append(txt, "mdns.service_type="+serviceType)

	// Get network interfaces for registration
	// If Interface is specified, use only that one, otherwise use all
	var ifaces []net.Interface

	if b.config.Interface != "" {
		iface, err := net.InterfaceByName(b.config.Interface)
		if err != nil {
			return fmt.Errorf("failed to get interface %s: %w", b.config.Interface, err)
		}

		ifaces = []net.Interface{*iface}
	}

	server, err := zeroconf.Register(
		instance.ID,     // Instance name
		serviceType,     // Service type (configurable)
		b.config.Domain, // Domain
		instance.Port,   // Port
		txt,             // TXT records
		ifaces,          // Network interfaces (nil = all, or specific interface)
	)
	if err != nil {
		return fmt.Errorf("failed to register mDNS service: %w", err)
	}

	// Store registration
	b.services[instance.ID] = &registeredService{
		instance: &ServiceInstance{
			ID:            instance.ID,
			Name:          instance.Name,
			Version:       instance.Version,
			Address:       addresses[0], // Use first address
			Port:          instance.Port,
			Tags:          instance.Tags,
			Metadata:      instance.Metadata,
			Status:        HealthStatusPassing,
			LastHeartbeat: time.Now().Unix(),
		},
		server: server,
	}

	// // Log successful registration with details
	// fmt.Printf("[mDNS] Service registered successfully:\n")
	// fmt.Printf("  - Instance ID: %s\n", instance.ID)
	// fmt.Printf("  - Service Type: %s\n", serviceType)
	// fmt.Printf("  - Domain: %s\n", b.config.Domain)
	// fmt.Printf("  - Address: %s:%d\n", addresses[0], instance.Port)
	// fmt.Printf("  - TXT Records: %d records\n", len(txt))

	// if b.config.Interface != "" {
	// 	fmt.Printf("  - Interface: %s\n", b.config.Interface)
	// } else {
	// 	fmt.Printf("  - Interface: all interfaces\n")
	// }

	// Give the mDNS server time to fully initialize and start responding
	// This ensures the service is discoverable immediately
	time.Sleep(100 * time.Millisecond)

	// fmt.Printf("[mDNS] Service is now discoverable\n")

	return nil
}

// Deregister deregisters a service instance.
func (b *MDNSBackend) Deregister(ctx context.Context, serviceID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	registered, exists := b.services[serviceID]
	if !exists {
		return fmt.Errorf("service instance not found: %s", serviceID)
	}

	// Shutdown mDNS server
	registered.server.Shutdown()

	// Remove from registry
	delete(b.services, serviceID)

	return nil
}

// Discover discovers service instances by name via mDNS.
func (b *MDNSBackend) Discover(ctx context.Context, serviceName string) ([]*ServiceInstance, error) {
	// Use configured service type or generate from service name
	serviceType := b.config.ServiceType
	if serviceType == "" {
		serviceType = fmt.Sprintf("_%s._tcp", sanitizeServiceName(serviceName))
	}

	instances, err := b.discoverByServiceType(ctx, serviceName, serviceType)
	if err != nil {
		return nil, err
	}

	// When a shared ServiceType is configured (e.g. "_twinos._tcp"), browsing
	// returns ALL services under that type. Filter to only return instances
	// whose name matches the requested serviceName.
	if b.config.ServiceType != "" {
		filtered := make([]*ServiceInstance, 0, len(instances))
		for _, inst := range instances {
			if strings.EqualFold(inst.Name, serviceName) {
				filtered = append(filtered, inst)
			}
		}
		return filtered, nil
	}

	return instances, nil
}

// discoverByServiceType discovers services by specific mDNS service type.
func (b *MDNSBackend) discoverByServiceType(ctx context.Context, serviceName, serviceType string) ([]*ServiceInstance, error) {
	b.log("[mdns] browse: type=%s domain=%s timeout=%s", serviceType, b.config.Domain, b.config.BrowseTimeout)

	// Create resolver
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create resolver: %w", err)
	}

	// Create results channel
	entries := make(chan *zeroconf.ServiceEntry, 100)
	instances := make([]*ServiceInstance, 0)
	instanceMap := make(map[string]*ServiceInstance) // Deduplicate by ID

	// Collect results in background
	var wg sync.WaitGroup

	wg.Go(func() {
		for entry := range entries {
			instance := convertEntryToInstance(serviceName, entry)
			if instance != nil {
				// Deduplicate by ID
				instanceMap[instance.ID] = instance
			}
		}
	})

	// Browse for services
	browseCtx, cancel := context.WithTimeout(ctx, b.config.BrowseTimeout)
	defer cancel()

	err = resolver.Browse(browseCtx, serviceType, b.config.Domain, entries)
	// Note: Browse will close the entries channel when done
	wg.Wait()

	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			b.log("[mdns] browse timed out for %s (found %d entries)", serviceType, len(instanceMap))
		} else {
			return nil, fmt.Errorf("failed to browse services: %w", err)
		}
	}

	b.log("[mdns] browse complete: type=%s found=%d", serviceType, len(instanceMap))

	// Convert map to slice
	for _, instance := range instanceMap {
		instances = append(instances, instance)
	}

	return instances, nil
}

// DiscoverAllTypes discovers services across all configured service types
// This is useful for gateways and service meshes that need to discover multiple service types.
func (b *MDNSBackend) DiscoverAllTypes(ctx context.Context) ([]*ServiceInstance, error) {
	serviceTypes := b.config.ServiceTypes
	if len(serviceTypes) == 0 {
		return nil, errors.New("no service types configured for discovery")
	}

	allInstances := make([]*ServiceInstance, 0)
	instanceMap := make(map[string]*ServiceInstance) // Deduplicate by ID

	for _, serviceType := range serviceTypes {
		// Extract service name from type (_octopus._tcp -> octopus)
		serviceName := extractServiceNameFromType(serviceType)

		instances, err := b.discoverByServiceType(ctx, serviceName, serviceType)
		if err != nil {
			// Log but continue with other types
			continue
		}

		// Deduplicate by ID
		for _, instance := range instances {
			instanceMap[instance.ID] = instance
		}
	}

	// Convert map to slice
	for _, instance := range instanceMap {
		allInstances = append(allInstances, instance)
	}

	return allInstances, nil
}

// DiscoverWithTags discovers service instances by name and tags.
func (b *MDNSBackend) DiscoverWithTags(ctx context.Context, serviceName string, tags []string) ([]*ServiceInstance, error) {
	instances, err := b.Discover(ctx, serviceName)
	if err != nil {
		return nil, err
	}

	if len(tags) == 0 {
		return instances, nil
	}

	// Filter by tags
	filtered := make([]*ServiceInstance, 0, len(instances))
	for _, instance := range instances {
		if instance.HasAllTags(tags) {
			filtered = append(filtered, instance)
		}
	}

	return filtered, nil
}

// Watch watches for changes to a service via mDNS.
func (b *MDNSBackend) Watch(ctx context.Context, serviceName string, onChange func([]*ServiceInstance)) error {
	b.mu.Lock()

	// Check if watcher already exists
	watcher, exists := b.watchers[serviceName]
	if !exists {
		// Create new watcher
		resolver, err := zeroconf.NewResolver(nil)
		if err != nil {
			b.mu.Unlock()

			return fmt.Errorf("failed to create resolver: %w", err)
		}

		watchCtx, cancel := context.WithCancel(b.ctx)
		watcher = &serviceWatcher{
			serviceName: serviceName,
			entries:     make(map[string]*zeroconf.ServiceEntry),
			callbacks:   []func([]*ServiceInstance){},
			resolver:    resolver,
			cancel:      cancel,
		}
		b.watchers[serviceName] = watcher

		// Start watching in background
		go b.watchService(watchCtx, watcher)
	}

	// Add callback
	watcher.callbacks = append(watcher.callbacks, onChange)

	b.mu.Unlock()

	// Get initial instances and notify
	instances, err := b.Discover(ctx, serviceName)
	if err == nil {
		go onChange(instances)
	}

	return nil
}

// watchService continuously watches for service changes.
func (b *MDNSBackend) watchService(ctx context.Context, watcher *serviceWatcher) {
	// Use configured service type or generate from service name
	serviceType := b.config.ServiceType
	if serviceType == "" {
		serviceType = fmt.Sprintf("_%s._tcp", sanitizeServiceName(watcher.serviceName))
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Re-browse for services
			entries := make(chan *zeroconf.ServiceEntry, 100)
			newEntries := make(map[string]*zeroconf.ServiceEntry)

			// Collect entries
			var wg sync.WaitGroup

			wg.Go(func() {
				for entry := range entries {
					// Use instance name as ID
					id := entry.Instance
					newEntries[id] = entry
				}
			})

			// Browse with timeout
			browseCtx, cancel := context.WithTimeout(ctx, b.config.BrowseTimeout)
			_ = watcher.resolver.Browse(browseCtx, serviceType, b.config.Domain, entries)
			// Note: Browse will close the entries channel when browseCtx expires
			wg.Wait()
			cancel()

			// Check for changes
			watcher.mu.Lock()

			changed := len(newEntries) != len(watcher.entries)
			if !changed {
				for id := range newEntries {
					if _, exists := watcher.entries[id]; !exists {
						changed = true

						break
					}
				}
			}

			// Update entries and notify if changed
			if changed {
				watcher.entries = newEntries

				instances := make([]*ServiceInstance, 0, len(newEntries))
				for _, entry := range newEntries {
					if instance := convertEntryToInstance(watcher.serviceName, entry); instance != nil {
						instances = append(instances, instance)
					}
				}

				// Notify all callbacks
				callbacks := watcher.callbacks
				watcher.mu.Unlock()

				for _, callback := range callbacks {
					go callback(instances)
				}
			} else {
				watcher.mu.Unlock()
			}
		}
	}
}

// ListServices lists all discoverable services.
// It includes locally registered services and, when ServiceTypes are configured,
// also browses the network to discover remote services.
func (b *MDNSBackend) ListServices(ctx context.Context) ([]string, error) {
	services := make(map[string]bool)

	// Include locally registered services
	b.mu.RLock()
	localCount := len(b.services)
	for _, registered := range b.services {
		services[registered.instance.Name] = true
	}
	b.mu.RUnlock()

	b.log("[mdns] ListServices: %d local services, ServiceTypes=%v", localCount, b.config.ServiceTypes)

	// Browse the network for remote services when ServiceTypes are configured
	if len(b.config.ServiceTypes) > 0 {
		if err := b.browseServiceNames(ctx, services); err != nil {
			b.log("[mdns] ListServices: browseServiceNames failed: %v", err)
		}
	} else {
		b.log("[mdns] ListServices: no ServiceTypes configured, skipping network browse")
	}

	result := make([]string, 0, len(services))
	for name := range services {
		result = append(result, name)
	}

	b.log("[mdns] ListServices: returning %d services: %v", len(result), result)

	return result, nil
}

// browseServiceNames browses the network for each configured ServiceType
// and adds discovered service names to the provided map.
func (b *MDNSBackend) browseServiceNames(ctx context.Context, services map[string]bool) error {
	var lastErr error
	networkFound := 0

	for _, serviceType := range b.config.ServiceTypes {
		serviceName := extractServiceNameFromType(serviceType)

		browseCtx, cancel := context.WithTimeout(ctx, b.config.BrowseTimeout)
		instances, err := b.discoverByServiceType(browseCtx, serviceName, serviceType)
		cancel()

		if err != nil {
			b.log("[mdns] browseServiceNames: browse failed for %s: %v", serviceType, err)
			lastErr = err
			continue
		}

		if len(instances) == 0 {
			b.log("[mdns] browseServiceNames: no instances found for %s, retrying once...", serviceType)
			// Retry once with a fresh context — helps with intermittent mDNS timing
			retryCtx, retryCancel := context.WithTimeout(ctx, b.config.BrowseTimeout)
			instances, err = b.discoverByServiceType(retryCtx, serviceName, serviceType)
			retryCancel()

			if err != nil {
				b.log("[mdns] browseServiceNames: retry failed for %s: %v", serviceType, err)
				lastErr = err
				continue
			}
		}

		b.log("[mdns] browseServiceNames: found %d instances for %s", len(instances), serviceType)
		for _, inst := range instances {
			services[inst.Name] = true
		}
		networkFound += len(instances)
	}

	if networkFound == 0 && lastErr != nil {
		return fmt.Errorf("all service type browses failed, last error: %w", lastErr)
	}

	return nil
}

// Health checks backend health.
func (b *MDNSBackend) Health(ctx context.Context) error {
	// Test mDNS by creating a resolver
	_, err := zeroconf.NewResolver(nil)
	if err != nil {
		return fmt.Errorf("mDNS not available: %w", err)
	}

	return nil
}

// Close closes the backend.
func (b *MDNSBackend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Cancel context
	b.cancel()

	// Shutdown all registered services
	for _, registered := range b.services {
		registered.server.Shutdown()
	}

	// Stop all watchers
	for _, watcher := range b.watchers {
		watcher.cancel()
	}

	// Clear state
	b.services = make(map[string]*registeredService)
	b.watchers = make(map[string]*serviceWatcher)

	return nil
}

// convertEntryToInstance converts a zeroconf entry to a service instance.
func convertEntryToInstance(serviceName string, entry *zeroconf.ServiceEntry) *ServiceInstance {
	if entry == nil {
		return nil
	}

	// Parse TXT records
	metadata := make(map[string]string)
	tags := []string{}
	version := ""
	name := serviceName  // fallback to the caller-supplied name
	id := entry.Instance // Default to instance name

	for _, txt := range entry.Text {
		parts := strings.SplitN(txt, "=", 2)
		if len(parts) == 2 {
			key, value := parts[0], parts[1]
			switch key {
			case "name":
				// Prefer the actual service name from TXT over the
				// caller-supplied name (which may be derived from the
				// mDNS service type, e.g. "twinos" instead of "TwinOS").
				name = value
			case "version":
				version = value
			case "id":
				id = value
			case "tags":
				tags = strings.Split(value, ",")
			default:
				metadata[key] = value
			}
		}
	}

	// Get first IPv4 address
	address := ""
	if len(entry.AddrIPv4) > 0 {
		address = entry.AddrIPv4[0].String()
	} else if len(entry.AddrIPv6) > 0 {
		address = entry.AddrIPv6[0].String()
	}

	if address == "" {
		return nil
	}

	return &ServiceInstance{
		ID:            id,
		Name:          name,
		Version:       version,
		Address:       address,
		Port:          entry.Port,
		Tags:          tags,
		Metadata:      metadata,
		Status:        HealthStatusPassing,
		LastHeartbeat: time.Now().Unix(),
	}
}

// sanitizeServiceName sanitizes a service name for mDNS
// service names should be lowercase and contain only alphanumeric and hyphens.
func sanitizeServiceName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "_", "-")
	name = strings.ReplaceAll(name, " ", "-")

	return name
}

// extractServiceNameFromType extracts service name from mDNS service type
// Example: "_octopus._tcp" -> "octopus", "_http._tcp.local." -> "http".
func extractServiceNameFromType(serviceType string) string {
	// Remove leading underscore
	name := strings.TrimPrefix(serviceType, "_")

	// Split by dots and take first part
	parts := strings.Split(name, ".")
	if len(parts) > 0 {
		return parts[0]
	}

	return name
}
