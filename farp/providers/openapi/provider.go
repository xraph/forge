package openapi

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/xraph/forge/farp"
)

// Provider generates OpenAPI 3.x schemas from applications
type Provider struct {
	specVersion string
	endpoint    string
}

// NewProvider creates a new OpenAPI schema provider
// specVersion should be "3.0.0", "3.0.1", or "3.1.0" (recommended)
func NewProvider(specVersion string, endpoint string) *Provider {
	if specVersion == "" {
		specVersion = "3.1.0"
	}
	if endpoint == "" {
		endpoint = "/openapi.json"
	}

	return &Provider{
		specVersion: specVersion,
		endpoint:    endpoint,
	}
}

// Type returns the schema type
func (p *Provider) Type() farp.SchemaType {
	return farp.SchemaTypeOpenAPI
}

// SpecVersion returns the OpenAPI specification version
func (p *Provider) SpecVersion() string {
	return p.specVersion
}

// ContentType returns the content type
func (p *Provider) ContentType() string {
	return "application/json"
}

// Endpoint returns the HTTP endpoint where the schema is served
func (p *Provider) Endpoint() string {
	return p.endpoint
}

// Generate generates an OpenAPI schema from the application
// app should provide Routes() method that returns route information
func (p *Provider) Generate(ctx context.Context, app farp.Application) (interface{}, error) {
	// This is a placeholder implementation
	// The actual implementation should integrate with Forge's OpenAPI generator
	// For now, we return a minimal valid OpenAPI schema

	routes := app.Routes()
	if routes == nil {
		return nil, fmt.Errorf("application does not provide routes")
	}

	// Build minimal OpenAPI spec
	spec := map[string]interface{}{
		"openapi": p.specVersion,
		"info": map[string]interface{}{
			"title":   app.Name(),
			"version": app.Version(),
		},
		"paths": map[string]interface{}{},
	}

	// TODO: Process routes and generate paths
	// This requires integration with the actual Forge router

	return spec, nil
}

// Validate validates an OpenAPI schema
func (p *Provider) Validate(schema interface{}) error {
	// Basic validation - check for required fields
	schemaMap, ok := schema.(map[string]interface{})
	if !ok {
		return fmt.Errorf("%w: schema must be a map", farp.ErrInvalidSchema)
	}

	// Check openapi version
	if _, ok := schemaMap["openapi"]; !ok {
		return fmt.Errorf("%w: missing 'openapi' field", farp.ErrInvalidSchema)
	}

	// Check info
	if _, ok := schemaMap["info"]; !ok {
		return fmt.Errorf("%w: missing 'info' field", farp.ErrInvalidSchema)
	}

	// Check paths
	if _, ok := schemaMap["paths"]; !ok {
		return fmt.Errorf("%w: missing 'paths' field", farp.ErrInvalidSchema)
	}

	return nil
}

// Hash calculates SHA256 hash of the schema
func (p *Provider) Hash(schema interface{}) (string, error) {
	return farp.CalculateSchemaChecksum(schema)
}

// Serialize converts schema to JSON bytes
func (p *Provider) Serialize(schema interface{}) ([]byte, error) {
	return json.Marshal(schema)
}

// GenerateDescriptor generates a complete SchemaDescriptor for this schema
func (p *Provider) GenerateDescriptor(ctx context.Context, app farp.Application, locationType farp.LocationType, locationConfig map[string]string) (*farp.SchemaDescriptor, error) {
	// Generate schema
	schema, err := p.Generate(ctx, app)
	if err != nil {
		return nil, fmt.Errorf("failed to generate schema: %w", err)
	}

	// Calculate hash
	hash, err := p.Hash(schema)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate hash: %w", err)
	}

	// Calculate size
	data, err := p.Serialize(schema)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize schema: %w", err)
	}

	// Build location
	location := farp.SchemaLocation{
		Type: locationType,
	}

	switch locationType {
	case farp.LocationTypeHTTP:
		url := locationConfig["url"]
		if url == "" {
			return nil, fmt.Errorf("url required for HTTP location")
		}
		location.URL = url
		if headers := locationConfig["headers"]; headers != "" {
			// Parse headers from JSON string
			var headersMap map[string]string
			if err := json.Unmarshal([]byte(headers), &headersMap); err == nil {
				location.Headers = headersMap
			}
		}

	case farp.LocationTypeRegistry:
		registryPath := locationConfig["registry_path"]
		if registryPath == "" {
			return nil, fmt.Errorf("registry_path required for registry location")
		}
		location.RegistryPath = registryPath

	case farp.LocationTypeInline:
		// Schema will be embedded
	}

	descriptor := &farp.SchemaDescriptor{
		Type:        p.Type(),
		SpecVersion: p.SpecVersion(),
		Location:    location,
		ContentType: p.ContentType(),
		Hash:        hash,
		Size:        int64(len(data)),
	}

	// Add inline schema if location type is inline
	if locationType == farp.LocationTypeInline {
		descriptor.InlineSchema = schema
	}

	return descriptor, nil
}
