package discovery

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/xraph/forge"
	"github.com/xraph/forge/farp"
	"github.com/xraph/forge/farp/providers/asyncapi"
	"github.com/xraph/forge/farp/providers/openapi"
	"github.com/xraph/forge/farp/registry/memory"
	"github.com/xraph/forge/internal/errors"
)

// SchemaPublisher handles FARP schema registration.
type SchemaPublisher struct {
	config   FARPConfig
	registry farp.SchemaRegistry
	app      forge.App
	logger   forge.Logger
}

// NewSchemaPublisher creates a new schema publisher.
func NewSchemaPublisher(config FARPConfig, app forge.App) *SchemaPublisher {
	// For now, use memory registry - in production, this should use the same backend as discovery
	registry := memory.NewRegistry()

	return &SchemaPublisher{
		config:   config,
		registry: registry,
		app:      app,
		logger:   app.Logger(),
	}
}

// Publish generates and publishes schemas for the service.
func (p *SchemaPublisher) Publish(ctx context.Context, instanceID string) error {
	if !p.config.Enabled {
		return nil
	}

	p.logger.Info("publishing FARP schemas", forge.F("instance_id", instanceID))

	// Create schema manifest
	manifest := farp.NewManifest(
		p.app.Name(),
		p.app.Version(),
		instanceID,
	)

	// Add capabilities
	for _, cap := range p.config.Capabilities {
		manifest.AddCapability(cap)
	}

	// Set endpoints with defaults
	// Health endpoint defaults to Forge's built-in health endpoint
	healthEndpoint := p.config.Endpoints.Health
	if healthEndpoint == "" {
		healthEndpoint = "/_/health" // Use Forge's default health endpoint
	}

	manifest.Endpoints = farp.SchemaEndpoints{
		Health:         healthEndpoint,
		Metrics:        p.config.Endpoints.Metrics,
		OpenAPI:        p.config.Endpoints.OpenAPI,
		AsyncAPI:       p.config.Endpoints.AsyncAPI,
		GRPCReflection: p.config.Endpoints.GRPCReflection,
		GraphQL:        p.config.Endpoints.GraphQL,
	}

	// Auto-detect schemas if none configured and auto-register is enabled
	schemas := p.config.Schemas
	if len(schemas) == 0 && p.config.AutoRegister {
		p.logger.Info("auto-detecting schemas (no schemas configured)")
		schemas = p.autoDetectSchemas()
	}

	// Process configured or auto-detected schemas
	for _, schemaConfig := range schemas {
		descriptor, err := p.createSchemaDescriptor(ctx, schemaConfig)
		if err != nil {
			p.logger.Warn("failed to create schema descriptor",
				forge.F("type", schemaConfig.Type),
				forge.F("error", err),
			)

			continue
		}

		manifest.AddSchema(*descriptor)

		// Publish schema to registry if using push strategy
		if p.config.Strategy == "push" || p.config.Strategy == "hybrid" {
			if descriptor.Location.Type == farp.LocationTypeRegistry {
				if err := p.publishSchemaContent(ctx, descriptor); err != nil {
					p.logger.Warn("failed to publish schema content",
						forge.F("type", schemaConfig.Type),
						forge.F("error", err),
					)
				}
			}
		}
	}

	// Update manifest checksum
	if err := manifest.UpdateChecksum(); err != nil {
		return fmt.Errorf("failed to update manifest checksum: %w", err)
	}

	// Register manifest with registry
	if err := p.registry.RegisterManifest(ctx, manifest); err != nil {
		return fmt.Errorf("failed to register manifest: %w", err)
	}

	checksumDisplay := manifest.Checksum
	if len(checksumDisplay) > 16 {
		checksumDisplay = checksumDisplay[:16] + "..."
	}

	p.logger.Info("FARP schemas published successfully",
		forge.F("instance_id", instanceID),
		forge.F("schemas", len(manifest.Schemas)),
		forge.F("checksum", checksumDisplay),
	)

	return nil
}

// createSchemaDescriptor creates a schema descriptor from configuration.
func (p *SchemaPublisher) createSchemaDescriptor(ctx context.Context, config FARPSchemaConfig) (*farp.SchemaDescriptor, error) {
	// Generate or fetch schema based on type
	var (
		schema interface{}
		err    error
	)

	switch config.Type {
	case "openapi":
		schema, err = p.generateOpenAPISchema(ctx)
	case "asyncapi":
		schema, err = p.generateAsyncAPISchema(ctx)
	case "grpc":
		// TODO: Implement gRPC schema extraction
		return nil, errors.New("grpc schema generation not yet implemented")
	case "graphql":
		// TODO: Implement GraphQL schema extraction
		return nil, errors.New("graphql schema generation not yet implemented")
	default:
		return nil, fmt.Errorf("unsupported schema type: %s", config.Type)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to generate %s schema: %w", config.Type, err)
	}

	// Calculate hash
	hash, err := farp.CalculateSchemaChecksum(schema)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate schema hash: %w", err)
	}

	// Calculate size
	data, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal schema: %w", err)
	}

	// Build location
	location := farp.SchemaLocation{
		Type: farp.LocationType(config.Location.Type),
	}

	switch location.Type {
	case farp.LocationTypeHTTP:
		location.URL = config.Location.URL
		location.Headers = config.Location.Headers
	case farp.LocationTypeRegistry:
		location.RegistryPath = config.Location.RegistryPath
	case farp.LocationTypeInline:
		// Schema will be embedded in descriptor
	}

	descriptor := &farp.SchemaDescriptor{
		Type:        farp.SchemaType(config.Type),
		SpecVersion: config.SpecVersion,
		Location:    location,
		ContentType: config.ContentType,
		Hash:        hash,
		Size:        int64(len(data)),
	}

	// Add inline schema if location type is inline
	if location.Type == farp.LocationTypeInline {
		descriptor.InlineSchema = schema
	}

	return descriptor, nil
}

// generateOpenAPISchema generates OpenAPI schema from the app.
func (p *SchemaPublisher) generateOpenAPISchema(ctx context.Context) (interface{}, error) {
	p.logger.Debug("generating OpenAPI schema from Forge router")

	// Get router from app
	router := p.app.Router()
	if router == nil {
		p.logger.Warn("no router available, returning minimal schema")

		return p.generateMinimalOpenAPISchema(), nil
	}

	// Use Forge-integrated OpenAPI provider
	provider := openapi.NewForgeProvider("3.1.0", "/openapi.json")

	// Generate schema from router
	schema, err := provider.GenerateFromRouter(router)
	if err != nil {
		p.logger.Warn("failed to generate OpenAPI schema from router, using minimal schema",
			forge.F("error", err),
		)

		return p.generateMinimalOpenAPISchema(), nil
	}

	// Validate schema
	if err := provider.Validate(schema); err != nil {
		p.logger.Warn("generated OpenAPI schema failed validation, using minimal schema",
			forge.F("error", err),
		)

		return p.generateMinimalOpenAPISchema(), nil
	}

	p.logger.Debug("OpenAPI schema generated successfully")

	return schema, nil
}

// generateMinimalOpenAPISchema returns a minimal valid OpenAPI schema.
func (p *SchemaPublisher) generateMinimalOpenAPISchema() map[string]interface{} {
	return map[string]interface{}{
		"openapi": "3.1.0",
		"info": map[string]interface{}{
			"title":   p.app.Name(),
			"version": p.app.Version(),
		},
		"paths": map[string]interface{}{},
	}
}

// generateAsyncAPISchema generates AsyncAPI schema from the app.
func (p *SchemaPublisher) generateAsyncAPISchema(ctx context.Context) (interface{}, error) {
	p.logger.Debug("generating AsyncAPI schema from Forge router")

	// Get router from app
	router := p.app.Router()
	if router == nil {
		p.logger.Warn("no router available, returning minimal schema")

		return p.generateMinimalAsyncAPISchema(), nil
	}

	// Use Forge-integrated AsyncAPI provider
	provider := asyncapi.NewForgeProvider("3.0.0", "/asyncapi.json")

	// Generate schema from router
	schema, err := provider.GenerateFromRouter(router)
	if err != nil {
		p.logger.Warn("failed to generate AsyncAPI schema from router, using minimal schema",
			forge.F("error", err),
		)

		return p.generateMinimalAsyncAPISchema(), nil
	}

	// Validate schema
	if err := provider.Validate(schema); err != nil {
		p.logger.Warn("generated AsyncAPI schema failed validation, using minimal schema",
			forge.F("error", err),
		)

		return p.generateMinimalAsyncAPISchema(), nil
	}

	p.logger.Debug("AsyncAPI schema generated successfully")

	return schema, nil
}

// generateMinimalAsyncAPISchema returns a minimal valid AsyncAPI schema.
func (p *SchemaPublisher) generateMinimalAsyncAPISchema() map[string]interface{} {
	return map[string]interface{}{
		"asyncapi": "3.0.0",
		"info": map[string]interface{}{
			"title":   p.app.Name(),
			"version": p.app.Version(),
		},
		"channels":   map[string]interface{}{},
		"operations": map[string]interface{}{},
	}
}

// publishSchemaContent publishes schema content to the registry.
func (p *SchemaPublisher) publishSchemaContent(ctx context.Context, descriptor *farp.SchemaDescriptor) error {
	if descriptor.Location.Type != farp.LocationTypeRegistry {
		return nil
	}

	// Generate schema
	var (
		schema interface{}
		err    error
	)

	switch descriptor.Type {
	case farp.SchemaTypeOpenAPI:
		schema, err = p.generateOpenAPISchema(ctx)
	case farp.SchemaTypeAsyncAPI:
		schema, err = p.generateAsyncAPISchema(ctx)
	default:
		return fmt.Errorf("unsupported schema type: %s", descriptor.Type)
	}

	if err != nil {
		return err
	}

	// Publish to registry
	return p.registry.PublishSchema(ctx, descriptor.Location.RegistryPath, schema)
}

// GetManifest retrieves the manifest for an instance.
func (p *SchemaPublisher) GetManifest(ctx context.Context, instanceID string) (*farp.SchemaManifest, error) {
	return p.registry.GetManifest(ctx, instanceID)
}

// GetMetadataForDiscovery returns metadata that should be added to service discovery
// This allows mDNS/Bonjour and other backends to advertise FARP endpoints in TXT records.
func (p *SchemaPublisher) GetMetadataForDiscovery(baseURL string) map[string]string {
	if !p.config.Enabled {
		return nil
	}

	metadata := make(map[string]string)

	// Add FARP manifest endpoint
	if baseURL != "" {
		metadata["farp.manifest"] = baseURL + "/_farp/manifest"
	}

	// Add schema endpoints if configured
	if p.config.Endpoints.OpenAPI != "" {
		if baseURL != "" {
			metadata["farp.openapi"] = baseURL + p.config.Endpoints.OpenAPI
		}

		metadata["farp.openapi.path"] = p.config.Endpoints.OpenAPI
	}

	if p.config.Endpoints.AsyncAPI != "" {
		if baseURL != "" {
			metadata["farp.asyncapi"] = baseURL + p.config.Endpoints.AsyncAPI
		}

		metadata["farp.asyncapi.path"] = p.config.Endpoints.AsyncAPI
	}

	if p.config.Endpoints.GraphQL != "" {
		if baseURL != "" {
			metadata["farp.graphql"] = baseURL + p.config.Endpoints.GraphQL
		}

		metadata["farp.graphql.path"] = p.config.Endpoints.GraphQL
	}

	if p.config.Endpoints.GRPCReflection {
		metadata["farp.grpc.reflection"] = "true"
	}

	// Add capabilities
	if len(p.config.Capabilities) > 0 {
		metadata["farp.capabilities"] = fmt.Sprintf("%v", p.config.Capabilities)
	}

	// Add strategy
	if p.config.Strategy != "" {
		metadata["farp.strategy"] = p.config.Strategy
	}

	// Mark as FARP-enabled
	metadata["farp.enabled"] = "true"

	return metadata
}

// autoDetectSchemas detects available schema types based on router capabilities.
func (p *SchemaPublisher) autoDetectSchemas() []FARPSchemaConfig {
	var schemas []FARPSchemaConfig

	router := p.app.Router()
	if router == nil {
		p.logger.Debug("no router available for schema auto-detection")

		return schemas
	}

	// Try to detect OpenAPI support
	if p.supportsOpenAPI(router) {
		endpoint := p.config.Endpoints.OpenAPI
		if endpoint == "" {
			endpoint = "/openapi.json"
		}

		schemas = append(schemas, FARPSchemaConfig{
			Type:        "openapi",
			SpecVersion: "3.1.0",
			Location: FARPLocationConfig{
				Type: "inline", // Use inline by default for auto-detected schemas
			},
			ContentType: "application/json",
		})

		p.logger.Debug("auto-detected OpenAPI schema support")
	}

	// Try to detect AsyncAPI support
	if p.supportsAsyncAPI(router) {
		endpoint := p.config.Endpoints.AsyncAPI
		if endpoint == "" {
			endpoint = "/asyncapi.json"
		}

		schemas = append(schemas, FARPSchemaConfig{
			Type:        "asyncapi",
			SpecVersion: "3.0.0",
			Location: FARPLocationConfig{
				Type: "inline", // Use inline by default for auto-detected schemas
			},
			ContentType: "application/json",
		})

		p.logger.Debug("auto-detected AsyncAPI schema support")
	}

	if len(schemas) > 0 {
		p.logger.Info("auto-detected schemas", forge.F("count", len(schemas)))
	} else {
		p.logger.Warn("no schemas auto-detected - router may not have routes configured yet")
	}

	return schemas
}

// supportsOpenAPI checks if the router supports OpenAPI schema generation.
func (p *SchemaPublisher) supportsOpenAPI(router interface{}) bool {
	// Check if router has OpenAPISpec method
	type hasOpenAPISpec interface {
		OpenAPISpec() interface{}
	}

	if specProvider, ok := router.(hasOpenAPISpec); ok {
		spec := specProvider.OpenAPISpec()

		return spec != nil
	}

	return false
}

// supportsAsyncAPI checks if the router supports AsyncAPI schema generation.
func (p *SchemaPublisher) supportsAsyncAPI(router interface{}) bool {
	// Check if router has AsyncAPISpec method
	type hasAsyncAPISpec interface {
		AsyncAPISpec() interface{}
	}

	if specProvider, ok := router.(hasAsyncAPISpec); ok {
		spec := specProvider.AsyncAPISpec()

		return spec != nil
	}

	return false
}

// Close closes the schema publisher.
func (p *SchemaPublisher) Close() error {
	if p.registry != nil {
		return p.registry.Close()
	}

	return nil
}
