package gateway

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/xraph/forge"
	farpgw "github.com/xraph/forge/farp/gateway"
)

// DiscoveryService is the interface that the gateway requires from a discovery provider.
// This decouples the gateway from the concrete discovery.Service type.
type DiscoveryService interface {
	// ListServices lists all registered service names.
	ListServices(ctx context.Context) ([]string, error)

	// DiscoverHealthy returns healthy instances for a service.
	DiscoverHealthy(ctx context.Context, serviceName string) ([]*ServiceInstanceInfo, error)
}

// ServiceInstanceInfo represents a discovered service instance.
// This mirrors the fields we need from discovery.ServiceInstance.
type ServiceInstanceInfo struct {
	ID       string
	Name     string
	Version  string
	Address  string
	Port     int
	Tags     []string
	Metadata map[string]string
	Healthy  bool
}

// URL returns the full URL for the service instance.
func (si *ServiceInstanceInfo) URL(scheme string) string {
	if scheme == "" {
		scheme = "http"
	}

	return fmt.Sprintf("%s://%s:%d", scheme, si.Address, si.Port)
}

// IsHealthy returns whether the instance is healthy.
func (si *ServiceInstanceInfo) IsHealthy() bool {
	return si.Healthy
}

// ServiceDiscovery integrates FARP and service discovery for automatic route generation.
type ServiceDiscovery struct {
	config     DiscoveryConfig
	logger     forge.Logger
	rm         *RouteManager
	service    DiscoveryService
	farpClient *farpgw.Client

	mu             sync.RWMutex
	discoveredSvcs map[string]*DiscoveredService
	stopCh         chan struct{}
	wg             sync.WaitGroup
}

// NewServiceDiscovery creates a new service discovery integration.
func NewServiceDiscovery(
	config DiscoveryConfig,
	logger forge.Logger,
	rm *RouteManager,
	discService DiscoveryService,
) *ServiceDiscovery {
	return &ServiceDiscovery{
		config:         config,
		logger:         logger,
		rm:             rm,
		service:        discService,
		discoveredSvcs: make(map[string]*DiscoveredService),
		stopCh:         make(chan struct{}),
	}
}

// Start begins service discovery.
func (sd *ServiceDiscovery) Start(ctx context.Context) error {
	if !sd.config.Enabled || sd.service == nil {
		sd.logger.Info("service discovery disabled or no discovery service available")

		return nil
	}

	sd.logger.Info("starting gateway service discovery",
		forge.F("poll_interval", sd.config.PollInterval),
		forge.F("watch_mode", sd.config.WatchMode),
	)

	// Initial discovery
	if err := sd.refresh(ctx); err != nil {
		sd.logger.Warn("initial service discovery failed", forge.F("error", err))
	}

	// Start polling or watching
	sd.wg.Add(1)

	go sd.loop(ctx)

	return nil
}

// Stop stops service discovery.
func (sd *ServiceDiscovery) Stop() {
	close(sd.stopCh)
	sd.wg.Wait()
}

// Refresh forces a re-scan of services.
func (sd *ServiceDiscovery) Refresh(ctx context.Context) error {
	return sd.refresh(ctx)
}

// DiscoveredServices returns all discovered services.
func (sd *ServiceDiscovery) DiscoveredServices() []*DiscoveredService {
	sd.mu.RLock()
	defer sd.mu.RUnlock()

	services := make([]*DiscoveredService, 0, len(sd.discoveredSvcs))

	for _, svc := range sd.discoveredSvcs {
		services = append(services, svc)
	}

	return services
}

func (sd *ServiceDiscovery) loop(ctx context.Context) {
	defer sd.wg.Done()

	ticker := time.NewTicker(sd.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-sd.stopCh:
			return
		case <-ticker.C:
			if err := sd.refresh(ctx); err != nil {
				sd.logger.Warn("service discovery refresh failed", forge.F("error", err))
			}
		}
	}
}

func (sd *ServiceDiscovery) refresh(ctx context.Context) error {
	if sd.service == nil {
		return nil
	}

	// List all services
	serviceNames, err := sd.service.ListServices(ctx)
	if err != nil {
		return fmt.Errorf("failed to list services: %w", err)
	}

	currentServices := make(map[string]bool)

	for _, name := range serviceNames {
		if !sd.matchesFilter(name) {
			continue
		}

		instances, discErr := sd.service.DiscoverHealthy(ctx, name)
		if discErr != nil {
			sd.logger.Warn("failed to discover service",
				forge.F("service", name),
				forge.F("error", discErr),
			)

			continue
		}

		if len(instances) == 0 {
			continue
		}

		currentServices[name] = true

		sd.processService(ctx, name, instances)
	}

	// Remove routes for services that are no longer present
	sd.mu.Lock()

	for name := range sd.discoveredSvcs {
		if !currentServices[name] {
			sd.logger.Info("service deregistered, removing routes",
				forge.F("service", name),
			)

			sd.rm.RemoveByServiceName(name)
			delete(sd.discoveredSvcs, name)
		}
	}

	sd.mu.Unlock()

	return nil
}

func (sd *ServiceDiscovery) processService(ctx context.Context, name string, instances []*ServiceInstanceInfo) {
	if len(instances) == 0 {
		return
	}

	// Build targets from instances
	targets := make([]*Target, 0, len(instances))

	for _, inst := range instances {
		targets = append(targets, &Target{
			ID:       inst.ID,
			URL:      inst.URL("http"),
			Weight:   1,
			Healthy:  inst.IsHealthy(),
			Tags:     inst.Tags,
			Metadata: inst.Metadata,
		})
	}

	// Check if service has FARP manifest
	var protocols []string
	var schemaTypes []string
	capabilities := make([]string, 0)
	var routes []*Route

	firstInstance := instances[0]

	if farpEnabled, ok := firstInstance.Metadata["farp.enabled"]; ok && farpEnabled == "true" {
		// Try to get routes from FARP manifest
		farpRoutes := sd.routesFromFARP(ctx, name, firstInstance, targets)
		if len(farpRoutes) > 0 {
			routes = farpRoutes

			for _, r := range routes {
				protocols = append(protocols, string(r.Protocol))

				switch r.Protocol {
				case ProtocolWebSocket, ProtocolSSE:
					schemaTypes = append(schemaTypes, "asyncapi")
				case ProtocolGRPC:
					schemaTypes = append(schemaTypes, "grpc")
				default:
					schemaTypes = append(schemaTypes, "openapi")
				}
			}
		}
	}

	// Fallback: create a catch-all route for the service
	if len(routes) == 0 {
		prefix := sd.buildPrefix(name)

		route := &Route{
			ID:          fmt.Sprintf("discovery-%s", name),
			Path:        prefix + "/*",
			Targets:     targets,
			StripPrefix: sd.config.StripPrefix,
			Protocol:    ProtocolHTTP,
			Source:      SourceDiscovery,
			ServiceName: name,
			Priority:    10,
			Enabled:     true,
		}

		routes = []*Route{route}
		protocols = []string{"http"}
	}

	// Add or update routes
	for _, route := range routes {
		existing, ok := sd.rm.GetRoute(route.ID)
		if ok {
			// Update existing route with new targets
			existing.Targets = targets
			existing.UpdatedAt = time.Now()

			if err := sd.rm.UpdateRoute(existing); err != nil {
				sd.logger.Warn("failed to update discovered route",
					forge.F("route_id", route.ID),
					forge.F("error", err),
				)
			}
		} else {
			// Add new route
			if err := sd.rm.AddRoute(route); err != nil {
				sd.logger.Warn("failed to add discovered route",
					forge.F("route_id", route.ID),
					forge.F("error", err),
				)
			}
		}
	}

	// Update discovered service info
	sd.mu.Lock()
	sd.discoveredSvcs[name] = &DiscoveredService{
		Name:         name,
		Version:      firstInstance.Version,
		Address:      firstInstance.Address,
		Port:         firstInstance.Port,
		Protocols:    unique(protocols),
		SchemaTypes:  unique(schemaTypes),
		Capabilities: capabilities,
		Healthy:      firstInstance.IsHealthy(),
		Metadata:     firstInstance.Metadata,
		RouteCount:   len(routes),
		DiscoveredAt: time.Now(),
	}
	sd.mu.Unlock()
}

func (sd *ServiceDiscovery) routesFromFARP(_ context.Context, serviceName string, instance *ServiceInstanceInfo, targets []*Target) []*Route {
	// Try to get the manifest URL from metadata
	manifestURL, ok := instance.Metadata["farp.manifest"]
	if !ok {
		return nil
	}

	_ = manifestURL // Used for HTTP fetch in production

	// Parse FARP manifest to extract routes
	// For now, use the OpenAPI/AsyncAPI endpoints from metadata
	var routes []*Route
	prefix := sd.buildPrefix(serviceName)

	// Check for OpenAPI endpoint
	if openapiEndpoint, ok := instance.Metadata["farp.openapi"]; ok && openapiEndpoint != "" {
		// Create HTTP proxy route
		route := &Route{
			ID:          fmt.Sprintf("farp-%s-http", serviceName),
			Path:        prefix + "/*",
			Targets:     targets,
			StripPrefix: sd.config.StripPrefix,
			Protocol:    ProtocolHTTP,
			Source:      SourceFARP,
			ServiceName: serviceName,
			Priority:    20,
			Enabled:     true,
		}

		routes = append(routes, route)
	}

	// Check for AsyncAPI (WebSocket/SSE)
	if asyncapiEndpoint, ok := instance.Metadata["farp.asyncapi"]; ok && asyncapiEndpoint != "" {
		// Create WebSocket proxy route for async channels
		route := &Route{
			ID:          fmt.Sprintf("farp-%s-ws", serviceName),
			Path:        prefix + "/ws/*",
			Targets:     targets,
			StripPrefix: sd.config.StripPrefix,
			Protocol:    ProtocolWebSocket,
			Source:      SourceFARP,
			ServiceName: serviceName,
			Priority:    20,
			Enabled:     true,
		}

		routes = append(routes, route)
	}

	// Check for GraphQL
	if gqlEndpoint, ok := instance.Metadata["farp.graphql"]; ok && gqlEndpoint != "" {
		route := &Route{
			ID:          fmt.Sprintf("farp-%s-graphql", serviceName),
			Path:        prefix + "/graphql",
			Methods:     []string{"GET", "POST"},
			Targets:     targets,
			StripPrefix: sd.config.StripPrefix,
			Protocol:    ProtocolGraphQL,
			Source:      SourceFARP,
			ServiceName: serviceName,
			Priority:    20,
			Enabled:     true,
		}

		routes = append(routes, route)
	}

	return routes
}

// ConvertFARPRoutes converts FARP gateway client ServiceRoutes into gateway Routes.
func ConvertFARPRoutes(serviceName string, farpRoutes []farpgw.ServiceRoute, prefix string, stripPrefix bool) []*Route {
	var routes []*Route

	for _, fr := range farpRoutes {
		protocol := ProtocolHTTP

		// Detect protocol from metadata
		if schemaType, ok := fr.Metadata["schema_type"]; ok {
			switch schemaType {
			case "asyncapi":
				protocol = ProtocolWebSocket
			case "graphql":
				protocol = ProtocolGraphQL
			}
		}

		// Detect WebSocket from methods
		for _, m := range fr.Methods {
			if strings.EqualFold(m, "WEBSOCKET") {
				protocol = ProtocolWebSocket

				break
			}
		}

		methods := fr.Methods
		if protocol == ProtocolWebSocket {
			methods = nil // WebSocket doesn't filter by method
		}

		path := prefix + fr.Path

		route := &Route{
			ID:          uuid.New().String(),
			Path:        path,
			Methods:     methods,
			StripPrefix: stripPrefix,
			Protocol:    protocol,
			Source:      SourceFARP,
			ServiceName: serviceName,
			Priority:    20,
			Enabled:     true,
			Targets: []*Target{
				{
					ID:      uuid.New().String(),
					URL:     fr.TargetURL,
					Weight:  1,
					Healthy: true,
				},
			},
			Metadata: fr.Metadata,
		}

		routes = append(routes, route)
	}

	return routes
}

func (sd *ServiceDiscovery) buildPrefix(serviceName string) string {
	if !sd.config.AutoPrefix {
		return ""
	}

	tmpl := sd.config.PrefixTemplate
	if tmpl == "" {
		tmpl = "/{{.ServiceName}}"
	}

	return strings.ReplaceAll(tmpl, "{{.ServiceName}}", serviceName)
}

func (sd *ServiceDiscovery) matchesFilter(serviceName string) bool {
	if len(sd.config.ServiceFilters) == 0 {
		return true
	}

	for _, filter := range sd.config.ServiceFilters {
		// Check include names
		if len(filter.IncludeNames) > 0 {
			found := false

			for _, name := range filter.IncludeNames {
				if matchWildcard(serviceName, name) {
					found = true

					break
				}
			}

			if !found {
				return false
			}
		}

		// Check exclude names
		for _, name := range filter.ExcludeNames {
			if matchWildcard(serviceName, name) {
				return false
			}
		}
	}

	return true
}

func matchWildcard(s, pattern string) bool {
	if pattern == "*" {
		return true
	}

	if strings.Contains(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")

		return strings.HasPrefix(s, prefix)
	}

	return s == pattern
}

func unique(ss []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(ss))

	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}

	return result
}
