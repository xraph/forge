package asyncapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/xraph/farp"
)

// AsyncAPISpecProvider is an interface for types that can provide an AsyncAPI spec.
type AsyncAPISpecProvider interface {
	AsyncAPISpec() any
}

// ForgeProvider is a Forge-specific AsyncAPI provider that generates schemas
// from Forge's built-in AsyncAPI generator.
type ForgeProvider struct {
	*Provider
}

// NewForgeProvider creates a new Forge-integrated AsyncAPI provider.
func NewForgeProvider(specVersion string, endpoint string) *ForgeProvider {
	return &ForgeProvider{
		Provider: NewProvider(specVersion, endpoint),
	}
}

// Generate generates AsyncAPI schema from Forge application.
func (p *ForgeProvider) Generate(ctx context.Context, app farp.Application) (any, error) {
	// Try to get AsyncAPI spec provider interface
	if provider, ok := app.(AsyncAPISpecProvider); ok {
		spec := provider.AsyncAPISpec()
		if spec == nil {
			return nil, errors.New("AsyncAPI spec not available (ensure AsyncAPI is enabled in router)")
		}

		return spec, nil
	}

	// Fall back to base provider (placeholder schema)
	return p.Provider.Generate(ctx, app)
}

// GenerateFromRouter generates AsyncAPI schema directly from any type that provides AsyncAPI specs
// This is a convenience method for direct router access.
func (p *ForgeProvider) GenerateFromRouter(provider any) (any, error) {
	if provider == nil {
		return nil, errors.New("provider is nil")
	}

	// Use type assertion to get the spec
	// This works with any type that has an AsyncAPISpec() method
	type hasAsyncAPISpec interface {
		AsyncAPISpec() any
	}

	if specProvider, ok := provider.(hasAsyncAPISpec); ok {
		spec := specProvider.AsyncAPISpec()
		if spec == nil {
			return nil, errors.New("AsyncAPI spec not available")
		}

		return spec, nil
	}

	return nil, errors.New("provider does not implement AsyncAPISpec() method")
}

// Validate validates an AsyncAPI schema generated from Forge.
func (p *ForgeProvider) Validate(schema any) error {
	// Validate that it has the minimum required structure
	if schema == nil {
		return fmt.Errorf("%w: schema is nil", farp.ErrInvalidSchema)
	}

	// Try to marshal to JSON to ensure it's serializable
	data, err := json.Marshal(schema)
	if err != nil {
		return fmt.Errorf("%w: schema is not JSON-serializable: %w", farp.ErrInvalidSchema, err)
	}

	// Try to unmarshal to map to check structure
	var schemaMap map[string]any
	if err := json.Unmarshal(data, &schemaMap); err != nil {
		return fmt.Errorf("%w: schema is not a valid JSON object: %w", farp.ErrInvalidSchema, err)
	}

	// Check for required AsyncAPI fields
	if _, ok := schemaMap["asyncapi"]; !ok {
		return fmt.Errorf("%w: missing 'asyncapi' field", farp.ErrInvalidSchema)
	}

	if _, ok := schemaMap["info"]; !ok {
		return fmt.Errorf("%w: missing 'info' field", farp.ErrInvalidSchema)
	}

	// AsyncAPI 3.x requires channels and operations
	asyncAPIVersion, _ := schemaMap["asyncapi"].(string)
	if asyncAPIVersion >= "3.0.0" {
		if _, ok := schemaMap["channels"]; !ok {
			return fmt.Errorf("%w: missing 'channels' field (required for AsyncAPI 3.x)", farp.ErrInvalidSchema)
		}
	}

	return nil
}

// CreateForgeDescriptor creates a schema descriptor from a Forge router
// This is a helper method to simplify descriptor creation.
func CreateForgeDescriptor(router any, locationType farp.LocationType, locationConfig map[string]string) (*farp.SchemaDescriptor, error) {
	provider := NewForgeProvider("3.0.0", "/asyncapi.json")

	// Generate schema from router
	schema, err := provider.GenerateFromRouter(router)
	if err != nil {
		return nil, fmt.Errorf("failed to generate schema: %w", err)
	}

	// Validate schema
	if err := provider.Validate(schema); err != nil {
		return nil, fmt.Errorf("schema validation failed: %w", err)
	}

	// Calculate hash
	hash, err := provider.Hash(schema)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate hash: %w", err)
	}

	// Calculate size
	data, err := provider.Serialize(schema)
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
			return nil, errors.New("url required for HTTP location")
		}

		location.URL = url

		if headers := locationConfig["headers"]; headers != "" {
			var headersMap map[string]string
			if err := json.Unmarshal([]byte(headers), &headersMap); err == nil {
				location.Headers = headersMap
			}
		}

	case farp.LocationTypeRegistry:
		registryPath := locationConfig["registry_path"]
		if registryPath == "" {
			return nil, errors.New("registry_path required for registry location")
		}

		location.RegistryPath = registryPath

	case farp.LocationTypeInline:
		// Schema will be embedded
	}

	descriptor := &farp.SchemaDescriptor{
		Type:        provider.Type(),
		SpecVersion: provider.SpecVersion(),
		Location:    location,
		ContentType: provider.ContentType(),
		Hash:        hash,
		Size:        int64(len(data)),
	}

	// Add inline schema if location type is inline
	if locationType == farp.LocationTypeInline {
		descriptor.InlineSchema = schema
	}

	return descriptor, nil
}
