package discovery

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge"

	"github.com/xraph/forge/extensions/dashboard/contributor"
)

// DiscoveryService is the interface the dashboard needs from a discovery service.
// This decouples the dashboard from the discovery extension's separate Go module,
// allowing the integration to work with any discovery provider that satisfies this contract.
// The forge extensions/discovery.Service type satisfies this interface.
type DiscoveryService interface {
	// ListServices returns the names of all registered services.
	ListServices(ctx context.Context) ([]string, error)

	// DiscoverWithTags discovers service instances matching name + tags.
	DiscoverWithTags(ctx context.Context, serviceName string, tags []string) ([]*ServiceInstance, error)
}

// ServiceInstance mirrors the minimal fields the dashboard needs from a discovered service.
// This matches discovery/backends.ServiceInstance without importing the external module.
type ServiceInstance struct {
	ID       string
	Name     string
	Address  string
	Port     int
	Tags     []string
	Metadata map[string]string
	Status   string // "passing", "warning", "critical", "unknown"
}

// IsHealthy returns true if the service status is "passing".
func (si *ServiceInstance) IsHealthy() bool {
	return si.Status == "passing"
}

// URL returns the full URL for the service instance.
func (si *ServiceInstance) URL(scheme string) string {
	if scheme == "" {
		scheme = "http"
	}

	return fmt.Sprintf("%s://%s:%d", scheme, si.Address, si.Port)
}

// GetMetadata retrieves metadata by key.
func (si *ServiceInstance) GetMetadata(key string) (string, bool) {
	val, ok := si.Metadata[key]

	return val, ok
}

// Integration watches a discovery service for services tagged as
// dashboard contributors and automatically registers/unregisters them.
type Integration struct {
	mu sync.Mutex

	discovery    DiscoveryService
	registry     *contributor.ContributorRegistry
	tag          string
	pollInterval time.Duration
	proxyTimeout time.Duration
	logger       forge.Logger

	// localServiceIDs holds service IDs that belong to the host process. Any
	// instance whose ID matches is filtered out before registration so the
	// dashboard never tries to fetch a contributor manifest from itself
	// (memory-backed discovery surfaces the host's own service registration
	// alongside any peers it learns about).
	localServiceIDs map[string]struct{}

	// tracked keeps track of remote contributors we've registered
	tracked map[string]string // service ID → contributor name

	stopOnce sync.Once
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// NewIntegration creates a new discovery integration.
func NewIntegration(
	discovery DiscoveryService,
	registry *contributor.ContributorRegistry,
	tag string,
	pollInterval time.Duration,
	proxyTimeout time.Duration,
	logger forge.Logger,
) *Integration {
	return &Integration{
		discovery:    discovery,
		registry:     registry,
		tag:          tag,
		pollInterval: pollInterval,
		proxyTimeout: proxyTimeout,
		logger:       logger,
		tracked:      make(map[string]string),
		stopCh:       make(chan struct{}),
	}
}

// LocalServiceIDProvider is the optional interface a discovery service
// implements when it can report the host process's own service ID. Used so
// memory-backed discovery doesn't make the dashboard try to fetch a
// contributor manifest from itself.
type LocalServiceIDProvider interface {
	LocalServiceID() string
}

// Start begins polling the discovery service for dashboard contributors.
func (i *Integration) Start(ctx context.Context) {
	// Auto-filter the host's own service ID when the discovery service can
	// report it. Memory-backed discovery surfaces the local registration in
	// every poll; without this guard the dashboard would attempt to fetch a
	// contributor manifest from itself.
	if provider, ok := i.discovery.(LocalServiceIDProvider); ok {
		if id := provider.LocalServiceID(); id != "" {
			i.IgnoreLocalService(id)

			i.logger.Debug("discovery integration: filtering local service ID",
				forge.F("local_service_id", id),
			)
		}
	}

	i.wg.Add(1)

	go i.pollLoop(ctx)

	i.logger.Info("discovery integration started",
		forge.F("tag", i.tag),
		forge.F("poll_interval", i.pollInterval.String()),
	)
}

// Stop stops the discovery integration. Safe to call multiple times — the
// dashboard extension's Stop() can fire more than once (forge calls Stop on
// the extension and the DI container Stop walks the same instance again).
func (i *Integration) Stop() {
	i.stopOnce.Do(func() {
		close(i.stopCh)
		i.wg.Wait()

		i.logger.Info("discovery integration stopped")
	})
}

// IgnoreLocalService instructs the integration to skip a service ID when
// scanning discovery. This is used to filter the host's own self-registration
// out of dashboard contributor candidates. Must be called before Start.
func (i *Integration) IgnoreLocalService(serviceID string) {
	if serviceID == "" {
		return
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	if i.localServiceIDs == nil {
		i.localServiceIDs = make(map[string]struct{})
	}

	i.localServiceIDs[serviceID] = struct{}{}
}

// pollLoop periodically checks discovery for services with the dashboard tag.
func (i *Integration) pollLoop(ctx context.Context) {
	defer i.wg.Done()

	// Initial fetch
	i.reconcile(ctx)

	ticker := time.NewTicker(i.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-i.stopCh:
			return
		case <-ticker.C:
			i.reconcile(ctx)
		}
	}
}

// reconcile checks discovery for current services and adds/removes contributors.
func (i *Integration) reconcile(ctx context.Context) {
	// List all services
	serviceNames, err := i.discovery.ListServices(ctx)
	if err != nil {
		i.logger.Warn("discovery integration: failed to list services",
			forge.F("error", err.Error()),
		)

		return
	}

	// Find instances with the dashboard contributor tag
	foundIDs := make(map[string]bool)

	for _, name := range serviceNames {
		instances, err := i.discovery.DiscoverWithTags(ctx, name, []string{i.tag})
		if err != nil {
			continue
		}

		for _, inst := range instances {
			if !inst.IsHealthy() {
				continue
			}

			i.mu.Lock()
			_, isLocal := i.localServiceIDs[inst.ID]
			i.mu.Unlock()

			if isLocal {
				continue
			}

			foundIDs[inst.ID] = true

			i.mu.Lock()
			contribName, tracked := i.tracked[inst.ID]
			i.mu.Unlock()

			if tracked {
				// Re-fetch the manifest so plugin/nav changes on the remote
				// surface in the host dashboard without service churn.
				i.refreshRemoteService(ctx, inst, contribName)

				continue
			}

			// New service — fetch manifest and register
			i.registerRemoteService(ctx, inst)
		}
	}

	// Remove any tracked services that are no longer present
	i.mu.Lock()

	for serviceID, contribName := range i.tracked {
		if !foundIDs[serviceID] {
			if err := i.registry.Unregister(contribName); err == nil {
				i.logger.Info("discovery: unregistered departed contributor",
					forge.F("service_id", serviceID),
					forge.F("contributor", contribName),
				)
			}

			delete(i.tracked, serviceID)
		}
	}

	i.mu.Unlock()
}

// registerRemoteService fetches a manifest from a discovered service and registers it.
func (i *Integration) registerRemoteService(ctx context.Context, inst *ServiceInstance) {
	baseURL := inst.URL("http")

	// Get API key from metadata if present
	apiKey, _ := inst.GetMetadata("forge-api-key")

	// Fetch manifest
	manifest, err := contributor.FetchManifest(ctx, baseURL, i.proxyTimeout, apiKey)
	if err != nil {
		i.logger.Warn("discovery: failed to fetch manifest from service",
			forge.F("service_id", inst.ID),
			forge.F("url", baseURL),
			forge.F("error", err.Error()),
		)

		return
	}

	// Create and register remote contributor
	opts := []contributor.RemoteContributorOption{}
	if apiKey != "" {
		opts = append(opts, contributor.WithAPIKey(apiKey))
	}

	rc := contributor.NewRemoteContributor(baseURL, manifest, opts...)
	if err := i.registry.RegisterRemote(rc); err != nil {
		i.logger.Warn("discovery: failed to register remote contributor",
			forge.F("service_id", inst.ID),
			forge.F("contributor", manifest.Name),
			forge.F("error", err.Error()),
		)

		return
	}

	i.mu.Lock()
	i.tracked[inst.ID] = manifest.Name
	i.mu.Unlock()

	i.logger.Info("discovery: registered remote contributor",
		forge.F("service_id", inst.ID),
		forge.F("contributor", manifest.Name),
		forge.F("url", baseURL),
	)
}

// refreshRemoteService re-fetches a tracked service's manifest and re-registers
// it when the manifest has changed (e.g. the remote loaded a new plugin). Errors
// during refresh are logged at debug level — the next poll will retry.
func (i *Integration) refreshRemoteService(ctx context.Context, inst *ServiceInstance, contribName string) {
	baseURL := inst.URL("http")
	apiKey, _ := inst.GetMetadata("forge-api-key")

	manifest, err := contributor.FetchManifest(ctx, baseURL, i.proxyTimeout, apiKey)
	if err != nil {
		i.logger.Debug("discovery: manifest refresh failed",
			forge.F("service_id", inst.ID),
			forge.F("contributor", contribName),
			forge.F("error", err.Error()),
		)

		return
	}

	current, ok := i.registry.GetManifest(contribName)
	if ok && contributor.ManifestsEqual(current, manifest) {
		return
	}

	if err := i.registry.Unregister(contribName); err != nil {
		i.logger.Debug("discovery: manifest refresh unregister failed",
			forge.F("contributor", contribName),
			forge.F("error", err.Error()),
		)

		return
	}

	opts := []contributor.RemoteContributorOption{}
	if apiKey != "" {
		opts = append(opts, contributor.WithAPIKey(apiKey))
	}

	rc := contributor.NewRemoteContributor(baseURL, manifest, opts...)
	if err := i.registry.RegisterRemote(rc); err != nil {
		i.logger.Warn("discovery: manifest refresh re-register failed",
			forge.F("contributor", contribName),
			forge.F("error", err.Error()),
		)

		return
	}

	i.logger.Info("discovery: refreshed remote contributor manifest",
		forge.F("service_id", inst.ID),
		forge.F("contributor", contribName),
	)
}


// TrackedCount returns the number of tracked remote contributors.
func (i *Integration) TrackedCount() int {
	i.mu.Lock()
	defer i.mu.Unlock()

	return len(i.tracked)
}
