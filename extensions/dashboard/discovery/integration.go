// Package discovery registers remote dashboard contributors found through a
// service discovery backend. A service that serves its contract intents with
// contract/server, and registers itself in discovery under the dashboard tag,
// shows up in the dashboard with no code on the dashboard's side.
package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/xraph/forge"

	"github.com/xraph/forge/extensions/dashboard/contract"
	contractremote "github.com/xraph/forge/extensions/dashboard/contract/remote"
)

// DefaultTag is the discovery tag a service registers under to be picked up
// as a dashboard contributor.
const DefaultTag = "forge-dashboard-contributor"

// APIKeyMetadata is the instance metadata key whose value, when present, is
// sent as a bearer token on every request the dashboard makes to that
// service.
const APIKeyMetadata = "forge-api-key" //nolint:gosec // G101: a metadata key name, not a credential

// DiscoveryService is the interface the dashboard needs from a discovery service.
// This decouples the dashboard from the discovery extension's separate Go module,
// allowing the integration to work with any discovery provider that satisfies this contract.
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

// LocalServiceIDProvider is the optional interface a discovery service
// implements when it can report the host process's own service ID, so the
// dashboard never tries to register itself as a remote contributor.
type LocalServiceIDProvider interface {
	LocalServiceID() string
}

// Config wires the integration to the dashboard. Register and Unregister are
// required; the dashboard extension supplies its own remote registration so
// validation, forwarding and status bookkeeping stay in one place.
type Config struct {
	// Tag is the discovery tag to look for. Empty means DefaultTag.
	Tag string
	// PollInterval is how often discovery is reconciled. Zero means a minute.
	PollInterval time.Duration
	// Register adds a remote contributor from its fetched manifest.
	Register func(m *contract.ContractManifest, endpoint contract.RemoteEndpoint) error
	// Unregister removes a remote contributor by name.
	Unregister func(name string)
	// Logger receives registration events. Nil means a no-op logger.
	Logger forge.Logger
}

// tracked is one remote contributor the integration registered.
type tracked struct {
	name     string
	manifest []byte // the manifest as last registered, to spot changes
}

// Integration polls a discovery service for instances carrying the dashboard
// tag and keeps the contract registry in step: new instances are registered,
// changed manifests are re-registered, and departed instances are removed.
//
// Several healthy instances of one service all serve the same contributor, so
// only one of them is registered at a time. When that one departs, another
// takes over in the same poll.
type Integration struct {
	discovery DiscoveryService
	cfg       Config

	// passMu serializes passes, so a forced Reconcile cannot race the
	// ticker's and register one instance twice.
	passMu sync.Mutex

	mu      sync.Mutex
	local   map[string]struct{}
	tracked map[string]tracked // instance ID -> contributor it registered

	stopOnce sync.Once
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// NewIntegration creates a discovery integration. Call Start to begin polling.
func NewIntegration(d DiscoveryService, cfg Config) *Integration {
	if cfg.Tag == "" {
		cfg.Tag = DefaultTag
	}

	if cfg.PollInterval <= 0 {
		cfg.PollInterval = time.Minute
	}

	if cfg.Logger == nil {
		cfg.Logger = forge.NewNoopLogger()
	}

	return &Integration{
		discovery: d,
		cfg:       cfg,
		local:     make(map[string]struct{}),
		tracked:   make(map[string]tracked),
		stopCh:    make(chan struct{}),
	}
}

// IgnoreLocalService skips a service ID when scanning discovery. Start calls
// it for the host's own ID when the discovery service can report one.
func (i *Integration) IgnoreLocalService(serviceID string) {
	if serviceID == "" {
		return
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	i.local[serviceID] = struct{}{}
}

// Start reconciles once, then keeps polling until ctx is done or Stop is called.
func (i *Integration) Start(ctx context.Context) {
	if p, ok := i.discovery.(LocalServiceIDProvider); ok {
		i.IgnoreLocalService(p.LocalServiceID())
	}

	i.wg.Go(func() {
		i.Reconcile(ctx)

		ticker := time.NewTicker(i.cfg.PollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-i.stopCh:
				return
			case <-ticker.C:
				i.Reconcile(ctx)
			}
		}
	})

	i.cfg.Logger.Info("dashboard discovery started",
		forge.F("tag", i.cfg.Tag),
		forge.F("poll_interval", i.cfg.PollInterval.String()),
	)
}

// Stop ends polling. Safe to call more than once.
func (i *Integration) Stop() {
	i.stopOnce.Do(func() {
		close(i.stopCh)
		i.wg.Wait()
	})
}

// Reconcile runs one pass: it drops contributors whose instance has gone,
// refreshes the ones still present, and registers new ones. Start runs it on
// every tick; it is exported so a caller can force a pass.
func (i *Integration) Reconcile(ctx context.Context) {
	i.passMu.Lock()
	defer i.passMu.Unlock()

	live, ok := i.liveInstances(ctx)
	if !ok {
		return
	}

	// Departures first, so a replica of a departed instance can claim its
	// contributor name in this same pass.
	i.mu.Lock()
	for id, t := range i.tracked {
		if _, still := live[id]; !still {
			delete(i.tracked, id)
			i.cfg.Unregister(t.name)
			i.cfg.Logger.Info("dashboard discovery: removed departed contributor",
				forge.F("service_id", id),
				forge.F("contributor", t.name),
			)
		}
	}
	i.mu.Unlock()

	ids := make([]string, 0, len(live))
	for id := range live {
		ids = append(ids, id)
	}

	sort.Strings(ids)

	for _, id := range ids {
		i.sync(ctx, live[id])
	}
}

// liveInstances returns every healthy, non-local instance carrying the tag,
// keyed by ID. ok is false when discovery itself could not be listed, in
// which case nothing should be removed on the strength of an empty answer.
func (i *Integration) liveInstances(ctx context.Context) (map[string]*ServiceInstance, bool) {
	names, err := i.discovery.ListServices(ctx)
	if err != nil {
		i.cfg.Logger.Warn("dashboard discovery: failed to list services",
			forge.F("error", err.Error()),
		)

		return nil, false
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	live := make(map[string]*ServiceInstance)

	for _, name := range names {
		instances, err := i.discovery.DiscoverWithTags(ctx, name, []string{i.cfg.Tag})
		if err != nil {
			continue
		}

		for _, inst := range instances {
			if inst == nil || !inst.IsHealthy() {
				continue
			}

			if _, isLocal := i.local[inst.ID]; isLocal {
				continue
			}

			live[inst.ID] = inst
		}
	}

	return live, true
}

// sync fetches one instance's manifest and registers it, or re-registers it
// if it changed since the last pass.
func (i *Integration) sync(ctx context.Context, inst *ServiceInstance) {
	baseURL := inst.URL("http")
	apiKey, _ := inst.GetMetadata(APIKeyMetadata)

	m, err := contractremote.FetchManifest(ctx, baseURL, apiKey, nil)
	if err != nil {
		i.cfg.Logger.Warn("dashboard discovery: failed to fetch contract manifest",
			forge.F("service_id", inst.ID),
			forge.F("url", baseURL),
			forge.F("error", err.Error()),
		)

		return
	}

	raw, err := json.Marshal(m)
	if err != nil {
		return
	}

	name := m.Contributor.Name
	endpoint := contract.RemoteEndpoint{BaseURL: baseURL, APIKey: apiKey}

	i.mu.Lock()
	defer i.mu.Unlock()

	if prev, ok := i.tracked[inst.ID]; ok {
		if prev.name == name && string(prev.manifest) == string(raw) {
			return
		}

		// The manifest changed, so swap it in. The old name is dropped
		// first because the registry refuses a name it already holds.
		i.cfg.Unregister(prev.name)
		delete(i.tracked, inst.ID)
	}

	for otherID, t := range i.tracked {
		if t.name == name {
			i.cfg.Logger.Debug("dashboard discovery: contributor already served by another instance",
				forge.F("service_id", inst.ID),
				forge.F("serving_instance", otherID),
				forge.F("contributor", name),
			)

			return
		}
	}

	if err := i.cfg.Register(m, endpoint); err != nil {
		i.cfg.Logger.Warn("dashboard discovery: failed to register contributor",
			forge.F("service_id", inst.ID),
			forge.F("contributor", name),
			forge.F("error", err.Error()),
		)

		return
	}

	i.tracked[inst.ID] = tracked{name: name, manifest: raw}
	i.cfg.Logger.Info("dashboard discovery: registered remote contributor",
		forge.F("service_id", inst.ID),
		forge.F("contributor", name),
		forge.F("url", baseURL),
	)
}

// Tracked returns the contributor names currently registered through
// discovery, sorted.
func (i *Integration) Tracked() []string {
	i.mu.Lock()
	defer i.mu.Unlock()

	out := make([]string, 0, len(i.tracked))
	for _, t := range i.tracked {
		out = append(out, t.name)
	}

	sort.Strings(out)

	return out
}
