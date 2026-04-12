package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xraph/farp"
	farpdiscovery "github.com/xraph/farp/discovery"
	"github.com/xraph/forge"
)

// backendToDiscovery adapts a forge Backend to FARP's ServiceDiscovery interface.
// This allows the FARP ServiceNode to use forge's existing discovery backends.
type backendToDiscovery struct {
	backend Backend
}

func newBackendToDiscovery(backend Backend) farpdiscovery.ServiceDiscovery {
	return &backendToDiscovery{backend: backend}
}

func (a *backendToDiscovery) Discover(ctx context.Context, serviceName string) ([]farpdiscovery.ServiceInstance, error) {
	instances, err := a.backend.Discover(ctx, serviceName)
	if err != nil {
		return nil, err
	}

	result := make([]farpdiscovery.ServiceInstance, len(instances))
	for i, inst := range instances {
		result[i] = forgeInstanceToFARP(inst)
	}

	return result, nil
}

func (a *backendToDiscovery) Watch(ctx context.Context, serviceName string, handler farpdiscovery.DiscoveryEventHandler) error {
	return a.backend.Watch(ctx, serviceName, func(instances []*ServiceInstance) {
		// Emit update events for all instances
		for _, inst := range instances {
			handler(farpdiscovery.DiscoveryEvent{
				Type:     farp.EventTypeUpdated,
				Instance: forgeInstanceToFARP(inst),
			})
		}
	})
}

func (a *backendToDiscovery) Register(ctx context.Context, instance farpdiscovery.ServiceInstance) error {
	forgeInst := farpInstanceToForge(instance)
	return a.backend.Register(ctx, forgeInst)
}

func (a *backendToDiscovery) Deregister(ctx context.Context, instanceID string) error {
	return a.backend.Deregister(ctx, instanceID)
}

func (a *backendToDiscovery) ReportHealth(ctx context.Context, instanceID string, status farp.InstanceStatus) error {
	// Forge backends don't have ReportHealth — re-register with updated status
	// This is a best-effort approach; the health loop in ServiceNode calls this periodically.
	return nil
}

func (a *backendToDiscovery) Close() error {
	return a.backend.Close()
}

func (a *backendToDiscovery) Health(ctx context.Context) error {
	return a.backend.Health(ctx)
}

// forgeInstanceToFARP converts a forge ServiceInstance to FARP's ServiceInstance.
func forgeInstanceToFARP(inst *ServiceInstance) farpdiscovery.ServiceInstance {
	status := farp.InstanceStatusHealthy
	switch inst.Status {
	case HealthStatusWarning:
		status = farp.InstanceStatusDegraded
	case HealthStatusCritical:
		status = farp.InstanceStatusUnhealthy
	case HealthStatusUnknown:
		status = farp.InstanceStatusStarting
	}

	return farpdiscovery.ServiceInstance{
		ID:          inst.ID,
		ServiceName: inst.Name,
		Version:     inst.Version,
		Address:     inst.Address,
		Port:        inst.Port,
		Status:      status,
		Tags:        inst.Tags,
		Metadata:    inst.Metadata,
	}
}

// farpInstanceToForge converts a FARP ServiceInstance to forge's ServiceInstance.
func farpInstanceToForge(inst farpdiscovery.ServiceInstance) *ServiceInstance {
	status := HealthStatusPassing
	switch inst.Status {
	case farp.InstanceStatusDegraded:
		status = HealthStatusWarning
	case farp.InstanceStatusUnhealthy:
		status = HealthStatusCritical
	case farp.InstanceStatusStarting, farp.InstanceStatusStopping, farp.InstanceStatusDraining:
		status = HealthStatusUnknown
	}

	return &ServiceInstance{
		ID:       inst.ID,
		Name:     inst.ServiceName,
		Version:  inst.Version,
		Address:  inst.Address,
		Port:     inst.Port,
		Status:   status,
		Tags:     inst.Tags,
		Metadata: inst.Metadata,
	}
}

// forgeApp adapts a forge.App to satisfy farp.Application and the
// OpenAPISpecProvider / AsyncAPISpecProvider interfaces that the
// forge FARP providers expect.
type forgeApp struct {
	name    string
	version string
	router  any
}

func newForgeApp(name, version string, router any) *forgeApp {
	return &forgeApp{name: name, version: version, router: router}
}

func (a *forgeApp) Name() string    { return a.name }
func (a *forgeApp) Version() string { return a.version }
func (a *forgeApp) Routes() any     { return a.router }

// OpenAPISpec delegates to the router if it supports OpenAPI spec generation.
func (a *forgeApp) OpenAPISpec() any {
	type hasOpenAPISpec interface {
		OpenAPISpec() *forge.OpenAPISpec
	}

	if provider, ok := a.router.(hasOpenAPISpec); ok {
		return provider.OpenAPISpec()
	}

	return nil
}

// AsyncAPISpec delegates to the router if it supports AsyncAPI spec generation.
func (a *forgeApp) AsyncAPISpec() any {
	type hasAsyncAPISpec interface {
		AsyncAPISpec() *forge.AsyncAPISpec
	}

	if provider, ok := a.router.(hasAsyncAPISpec); ok {
		return provider.AsyncAPISpec()
	}

	return nil
}

// buildServiceNodeProviders builds FARP schema providers from the forge app and config.
func buildServiceNodeProviders(app *forgeApp, config FARPConfig) []farp.SchemaProvider {
	var providers []farp.SchemaProvider

	// Build combined path rules: internal exclusions + user rules
	pathRules := farp.BuildPathRules(config.ShouldExcludeInternalPaths(), config.PathRules)

	// Check for explicit schema configs
	for _, sc := range config.Schemas {
		switch sc.Type {
		case "openapi":
			providers = append(providers, newForgeOpenAPIProvider(app, pathRules))
		case "asyncapi":
			providers = append(providers, newForgeAsyncAPIProvider(app, pathRules))
		}
	}

	// Auto-detect if no schemas configured
	if len(providers) == 0 && config.AutoRegister {
		if app.OpenAPISpec() != nil {
			providers = append(providers, newForgeOpenAPIProvider(app, pathRules))
		}

		if app.AsyncAPISpec() != nil {
			providers = append(providers, newForgeAsyncAPIProvider(app, pathRules))
		}
	}

	return providers
}

// forgeOpenAPIProvider wraps the forge OpenAPI generation as a FARP SchemaProvider.
type forgeOpenAPIProvider struct {
	app       *forgeApp
	pathRules []farp.PathRule
}

func newForgeOpenAPIProvider(app *forgeApp, pathRules []farp.PathRule) *forgeOpenAPIProvider {
	return &forgeOpenAPIProvider{app: app, pathRules: pathRules}
}

func (p *forgeOpenAPIProvider) Type() farp.SchemaType     { return farp.SchemaTypeOpenAPI }
func (p *forgeOpenAPIProvider) SpecVersion() string       { return "3.1.0" }
func (p *forgeOpenAPIProvider) ContentType() string       { return "application/json" }
func (p *forgeOpenAPIProvider) Endpoint() string          { return "/openapi.json" }
func (p *forgeOpenAPIProvider) Validate(schema any) error { return nil }

func (p *forgeOpenAPIProvider) Generate(_ context.Context, _ farp.Application) (any, error) {
	spec := p.app.OpenAPISpec()
	if spec == nil {
		return nil, fmt.Errorf("OpenAPI spec not available")
	}

	if len(p.pathRules) == 0 {
		return spec, nil
	}

	// Marshal to map so we can filter paths before FARP stores the schema.
	data, err := json.Marshal(spec)
	if err != nil {
		return spec, nil // fall back to unfiltered
	}

	var specMap map[string]any
	if err := json.Unmarshal(data, &specMap); err != nil {
		return spec, nil
	}

	if paths, ok := specMap["paths"].(map[string]any); ok {
		for path := range paths {
			if !farp.ShouldIncludePath(path, p.pathRules) {
				delete(paths, path)
			}
		}
	}

	return specMap, nil
}

func (p *forgeOpenAPIProvider) Hash(schema any) (string, error) {
	return farp.CalculateSchemaChecksum(schema)
}

func (p *forgeOpenAPIProvider) Serialize(schema any) ([]byte, error) {
	return json.Marshal(schema)
}

// forgeAsyncAPIProvider wraps the forge AsyncAPI generation as a FARP SchemaProvider.
type forgeAsyncAPIProvider struct {
	app       *forgeApp
	pathRules []farp.PathRule
}

func newForgeAsyncAPIProvider(app *forgeApp, pathRules []farp.PathRule) *forgeAsyncAPIProvider {
	return &forgeAsyncAPIProvider{app: app, pathRules: pathRules}
}

func (p *forgeAsyncAPIProvider) Type() farp.SchemaType     { return farp.SchemaTypeAsyncAPI }
func (p *forgeAsyncAPIProvider) SpecVersion() string       { return "3.0.0" }
func (p *forgeAsyncAPIProvider) ContentType() string       { return "application/json" }
func (p *forgeAsyncAPIProvider) Endpoint() string          { return "/asyncapi.json" }
func (p *forgeAsyncAPIProvider) Validate(schema any) error { return nil }

func (p *forgeAsyncAPIProvider) Generate(_ context.Context, _ farp.Application) (any, error) {
	spec := p.app.AsyncAPISpec()
	if spec == nil {
		return nil, fmt.Errorf("AsyncAPI spec not available")
	}

	if len(p.pathRules) == 0 {
		return spec, nil
	}

	// Filter channels based on path rules
	data, err := json.Marshal(spec)
	if err != nil {
		return spec, nil
	}

	var specMap map[string]any
	if err := json.Unmarshal(data, &specMap); err != nil {
		return spec, nil
	}

	if channels, ok := specMap["channels"].(map[string]any); ok {
		for ch := range channels {
			if !farp.ShouldIncludePath(ch, p.pathRules) {
				delete(channels, ch)
			}
		}
	}

	return specMap, nil
}

func (p *forgeAsyncAPIProvider) Hash(schema any) (string, error) {
	return farp.CalculateSchemaChecksum(schema)
}

func (p *forgeAsyncAPIProvider) Serialize(schema any) ([]byte, error) {
	return json.Marshal(schema)
}

// forgeRoutesToRouteDescriptors converts forge RouteInfo entries into FARP RouteDescriptors.
// Paths are filtered using the provided path rules.
func forgeRoutesToRouteDescriptors(routes []forge.RouteInfo, pathRules []farp.PathRule) []farp.RouteDescriptor {
	if len(routes) == 0 {
		return nil
	}

	var descriptors []farp.RouteDescriptor
	for _, route := range routes {
		// Apply path rules
		if len(pathRules) > 0 && !farp.ShouldIncludePath(route.Path, pathRules) {
			continue
		}

		// Determine protocol from metadata or tags
		protocol := "rest"
		if p, ok := route.Metadata["protocol"].(string); ok && p != "" {
			protocol = p
		} else {
			for _, tag := range route.Tags {
				switch strings.ToLower(tag) {
				case "grpc", "graphql", "websocket", "sse":
					protocol = strings.ToLower(tag)
				}
			}
		}

		// OperationID: prefer explicit, fall back to route name
		operationID := route.OperationID
		if operationID == "" {
			operationID = route.Name
		}

		// Timeout
		var timeout string
		if route.Timeout > 0 {
			timeout = route.Timeout.String()
		}

		// Build metadata with summary, description, tags
		var metadata map[string]any
		if route.Summary != "" || route.Description != "" || len(route.Tags) > 0 || len(route.Metadata) > 0 {
			metadata = make(map[string]any)
			for k, v := range route.Metadata {
				if k == "protocol" || k == "public" {
					continue // already handled as dedicated fields
				}
				metadata[k] = v
			}
			if route.Summary != "" {
				metadata["summary"] = route.Summary
			}
			if route.Description != "" {
				metadata["description"] = route.Description
			}
			if len(route.Tags) > 0 {
				metadata["tags"] = route.Tags
			}
			if len(metadata) == 0 {
				metadata = nil
			}
		}

		// Public flag
		public, _ := route.Metadata["public"].(bool)

		descriptors = append(descriptors, farp.RouteDescriptor{
			Path:        route.Path,
			Methods:     []string{route.Method},
			Protocol:    protocol,
			OperationID: operationID,
			Timeout:     timeout,
			Deprecated:  route.Deprecated,
			Metadata:    metadata,
			Public:      public,
		})
	}

	if len(descriptors) == 0 {
		return nil
	}
	return descriptors
}
