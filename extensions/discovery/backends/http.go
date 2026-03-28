package backends

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"sync"
	"time"
)

// HTTPBackend is an HTTP polling-based service discovery backend.
// It polls known service URLs at their /_farp/discovery endpoints to discover
// services. This is useful when mDNS is unreliable or when services run on
// known addresses (e.g., local development, Docker Compose, static deployments).
type HTTPBackend struct {
	config     HTTPConfig
	httpClient *http.Client

	// services stores polled service instances: service name -> instance ID -> instance
	services map[string]map[string]*ServiceInstance

	// localServices stores locally registered instances (via Register())
	localServices map[string]map[string]*ServiceInstance

	// watchers stores watch callbacks: service name -> callbacks
	watchers map[string][]func([]*ServiceInstance)

	mu     sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// discoveryResponse mirrors the /_farp/discovery JSON response format.
type discoveryResponse struct {
	ServiceID      string            `json:"service_id"`
	ServiceName    string            `json:"service_name"`
	ServiceVersion string            `json:"service_version"`
	Address        string            `json:"address"`
	Port           int               `json:"port"`
	Tags           []string          `json:"tags"`
	Metadata       map[string]string `json:"metadata"`
	FARPEnabled    bool              `json:"farp_enabled"`
	Backend        string            `json:"backend"`
}

// NewHTTPBackend creates a new HTTP polling-based discovery backend.
func NewHTTPBackend(config HTTPConfig) (*HTTPBackend, error) {
	if config.PollInterval <= 0 {
		config.PollInterval = 10 * time.Second
	}

	if config.Timeout <= 0 {
		config.Timeout = 5 * time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &HTTPBackend{
		config: config,
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
		services:      make(map[string]map[string]*ServiceInstance),
		localServices: make(map[string]map[string]*ServiceInstance),
		watchers:      make(map[string][]func([]*ServiceInstance)),
		ctx:           ctx,
		cancel:        cancel,
	}, nil
}

// Name returns the backend name.
func (b *HTTPBackend) Name() string {
	return "http"
}

// Initialize initializes the backend by performing an initial poll and starting
// the background polling goroutine.
func (b *HTTPBackend) Initialize(ctx context.Context) error {
	// Perform initial poll (don't fail if seeds are unreachable)
	b.pollAllSeeds()

	// Start background polling
	b.wg.Add(1)

	go b.pollLoop()

	return nil
}

// Register registers a local service instance.
func (b *HTTPBackend) Register(ctx context.Context, instance *ServiceInstance) error {
	if instance.ID == "" {
		return errors.New("service instance ID is required")
	}

	if instance.Name == "" {
		return errors.New("service name is required")
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.localServices[instance.Name]; !ok {
		b.localServices[instance.Name] = make(map[string]*ServiceInstance)
	}

	instanceCopy := *instance
	instanceCopy.LastHeartbeat = time.Now().Unix()
	b.localServices[instance.Name][instance.ID] = &instanceCopy

	b.notifyWatchersLocked(instance.Name)

	return nil
}

// Deregister deregisters a local service instance.
func (b *HTTPBackend) Deregister(ctx context.Context, serviceID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	var (
		found       bool
		serviceName string
	)

	for name, instances := range b.localServices {
		if _, ok := instances[serviceID]; ok {
			delete(instances, serviceID)

			found = true
			serviceName = name

			break
		}
	}

	if !found {
		return fmt.Errorf("service instance not found: %s", serviceID)
	}

	b.notifyWatchersLocked(serviceName)

	return nil
}

// Discover discovers service instances by name from both polled and local sources.
func (b *HTTPBackend) Discover(ctx context.Context, serviceName string) ([]*ServiceInstance, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	result := make([]*ServiceInstance, 0)

	// Collect from polled services
	if instances, ok := b.services[serviceName]; ok {
		for _, inst := range instances {
			copy := *inst
			result = append(result, &copy)
		}
	}

	// Collect from local services
	if instances, ok := b.localServices[serviceName]; ok {
		for _, inst := range instances {
			copy := *inst
			result = append(result, &copy)
		}
	}

	// Sort by ID for deterministic ordering
	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})

	return result, nil
}

// DiscoverWithTags discovers service instances by name and tags.
func (b *HTTPBackend) DiscoverWithTags(ctx context.Context, serviceName string, tags []string) ([]*ServiceInstance, error) {
	instances, err := b.Discover(ctx, serviceName)
	if err != nil {
		return nil, err
	}

	if len(tags) == 0 {
		return instances, nil
	}

	filtered := make([]*ServiceInstance, 0, len(instances))
	for _, inst := range instances {
		if inst.HasAllTags(tags) {
			filtered = append(filtered, inst)
		}
	}

	return filtered, nil
}

// Watch watches for changes to a service.
func (b *HTTPBackend) Watch(ctx context.Context, serviceName string, onChange func([]*ServiceInstance)) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.watchers[serviceName] = append(b.watchers[serviceName], onChange)

	// Immediately notify with current state
	current := b.getInstancesLocked(serviceName)

	go onChange(current)

	return nil
}

// ListServices lists all known service names from both polled and local sources.
func (b *HTTPBackend) ListServices(ctx context.Context) ([]string, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	names := make(map[string]bool)

	for name := range b.services {
		if len(b.services[name]) > 0 {
			names[name] = true
		}
	}

	for name := range b.localServices {
		if len(b.localServices[name]) > 0 {
			names[name] = true
		}
	}

	result := make([]string, 0, len(names))
	for name := range names {
		result = append(result, name)
	}

	sort.Strings(result)

	return result, nil
}

// Health checks backend health by verifying at least one seed is reachable.
func (b *HTTPBackend) Health(ctx context.Context) error {
	if len(b.config.Seeds) == 0 {
		return nil // No seeds configured is not an error
	}

	b.mu.RLock()
	totalInstances := 0

	for _, instances := range b.services {
		totalInstances += len(instances)
	}

	b.mu.RUnlock()

	if totalInstances > 0 {
		return nil // We have discovered services, so we're healthy
	}

	return fmt.Errorf("no services discovered from %d seed URLs", len(b.config.Seeds))
}

// Close stops the background polling and cleans up resources.
func (b *HTTPBackend) Close() error {
	b.cancel()
	b.wg.Wait()

	b.mu.Lock()
	defer b.mu.Unlock()

	b.services = make(map[string]map[string]*ServiceInstance)
	b.localServices = make(map[string]map[string]*ServiceInstance)
	b.watchers = make(map[string][]func([]*ServiceInstance))

	return nil
}

// --- Internal methods ---

// pollLoop runs the background polling goroutine.
func (b *HTTPBackend) pollLoop() {
	defer b.wg.Done()

	ticker := time.NewTicker(b.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			b.pollAllSeeds()
		case <-b.ctx.Done():
			return
		}
	}
}

// pollAllSeeds polls all configured seed URLs and updates the services cache.
func (b *HTTPBackend) pollAllSeeds() {
	newServices := make(map[string]map[string]*ServiceInstance)

	for _, seedURL := range b.config.Seeds {
		inst, err := b.pollSeed(seedURL)
		if err != nil {
			// Seed unreachable — skip without failing
			continue
		}

		if inst == nil {
			continue
		}

		if _, ok := newServices[inst.Name]; !ok {
			newServices[inst.Name] = make(map[string]*ServiceInstance)
		}

		newServices[inst.Name][inst.ID] = inst
	}

	// Determine which services changed
	b.mu.Lock()

	changedServices := b.diffServices(newServices)
	b.services = newServices

	// Notify watchers for changed services
	for _, name := range changedServices {
		b.notifyWatchersLocked(name)
	}

	b.mu.Unlock()
}

// pollSeed polls a single seed URL's /_farp/discovery endpoint.
func (b *HTTPBackend) pollSeed(seedURL string) (*ServiceInstance, error) {
	discoveryURL := seedURL + "/_farp/discovery"

	req, err := http.NewRequestWithContext(b.ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("poll %s: %w", discoveryURL, err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("poll %s: status %d", discoveryURL, resp.StatusCode)
	}

	var dr discoveryResponse
	if err := json.NewDecoder(resp.Body).Decode(&dr); err != nil {
		return nil, fmt.Errorf("decode %s: %w", discoveryURL, err)
	}

	if dr.ServiceName == "" {
		return nil, fmt.Errorf("poll %s: empty service name", discoveryURL)
	}

	// Resolve address: if service reports localhost/0.0.0.0, use the seed URL host
	address, port := resolveAddress(seedURL, dr.Address, dr.Port)

	// Ensure metadata contains the farp.manifest URL so the discovery manager
	// can fetch the full manifest from this service.
	metadata := dr.Metadata
	if metadata == nil {
		metadata = make(map[string]string)
	}

	if _, ok := metadata["farp.manifest"]; !ok {
		metadata["farp.manifest"] = seedURL + "/_farp/manifest"
	}

	if _, ok := metadata["farp.enabled"]; !ok && dr.FARPEnabled {
		metadata["farp.enabled"] = "true"
	}

	return &ServiceInstance{
		ID:            dr.ServiceID,
		Name:          dr.ServiceName,
		Version:       dr.ServiceVersion,
		Address:       address,
		Port:          port,
		Tags:          dr.Tags,
		Metadata:      metadata,
		Status:        HealthStatusPassing, // Responded to poll, so it's alive
		LastHeartbeat: time.Now().Unix(),
	}, nil
}

// resolveAddress handles the case where a service reports "localhost" or "0.0.0.0"
// as its address. In that case, we extract the host from the seed URL since the
// gateway may be reaching the service from a different context.
func resolveAddress(seedURL, responseAddr string, responsePort int) (string, int) {
	if responseAddr != "" &&
		responseAddr != "localhost" &&
		responseAddr != "127.0.0.1" &&
		responseAddr != "0.0.0.0" &&
		responseAddr != "::1" {
		return responseAddr, responsePort
	}

	// Extract host from seed URL
	u, err := url.Parse(seedURL)
	if err != nil {
		return responseAddr, responsePort
	}

	host := u.Hostname()

	if responsePort > 0 {
		return host, responsePort
	}

	// Fallback: extract port from seed URL
	portStr := u.Port()
	if portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil {
			return host, p
		}
	}

	return host, responsePort
}

// diffServices compares new services with current cached services and returns
// the names of services that changed.
func (b *HTTPBackend) diffServices(newServices map[string]map[string]*ServiceInstance) []string {
	changed := make(map[string]bool)

	// Check for new or changed services
	for name, newInstances := range newServices {
		oldInstances, exists := b.services[name]
		if !exists {
			changed[name] = true

			continue
		}

		if len(newInstances) != len(oldInstances) {
			changed[name] = true

			continue
		}

		for id := range newInstances {
			if _, ok := oldInstances[id]; !ok {
				changed[name] = true

				break
			}
		}
	}

	// Check for removed services
	for name := range b.services {
		if _, exists := newServices[name]; !exists {
			changed[name] = true
		}
	}

	result := make([]string, 0, len(changed))
	for name := range changed {
		result = append(result, name)
	}

	return result
}

// getInstancesLocked returns copies of all instances for a service (must hold at least read lock).
func (b *HTTPBackend) getInstancesLocked(serviceName string) []*ServiceInstance {
	result := make([]*ServiceInstance, 0)

	if instances, ok := b.services[serviceName]; ok {
		for _, inst := range instances {
			copy := *inst
			result = append(result, &copy)
		}
	}

	if instances, ok := b.localServices[serviceName]; ok {
		for _, inst := range instances {
			copy := *inst
			result = append(result, &copy)
		}
	}

	return result
}

// notifyWatchersLocked notifies all watchers for a service (must be called with lock held).
func (b *HTTPBackend) notifyWatchersLocked(serviceName string) {
	watchers, ok := b.watchers[serviceName]
	if !ok || len(watchers) == 0 {
		return
	}

	current := b.getInstancesLocked(serviceName)

	for _, watcher := range watchers {
		w := watcher // Capture for goroutine
		go w(current)
	}
}
