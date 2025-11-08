package farp

import (
	"context"
	"encoding/json"
)

// SchemaProvider generates schemas from application code
// Implementations generate protocol-specific schemas (OpenAPI, AsyncAPI, gRPC, GraphQL).
type SchemaProvider interface {
	// Type returns the schema type this provider generates
	Type() SchemaType

	// Generate generates a schema from the application
	// Returns the schema as interface{} (typically map[string]interface{} or struct)
	Generate(ctx context.Context, app Application) (any, error)

	// Validate validates a generated schema for correctness
	Validate(schema any) error

	// Hash calculates the SHA256 hash of a schema
	Hash(schema any) (string, error)

	// Serialize converts schema to bytes for storage/transmission
	Serialize(schema any) ([]byte, error)

	// Endpoint returns the HTTP endpoint where the schema is served
	// Returns empty string if not served via HTTP
	Endpoint() string

	// SpecVersion returns the specification version (e.g., "3.1.0" for OpenAPI)
	SpecVersion() string

	// ContentType returns the content type for the schema
	ContentType() string
}

// Application represents an application that can have schemas generated from it
// This is an abstraction to avoid direct dependency on Forge framework.
type Application interface {
	// Name returns the application/service name
	Name() string

	// Version returns the application version
	Version() string

	// Routes returns route information for schema generation
	// The actual type depends on the framework (e.g., []forge.RouteInfo)
	Routes() any
}

// BaseSchemaProvider provides common functionality for schema providers.
type BaseSchemaProvider struct {
	schemaType   SchemaType
	specVersion  string
	contentType  string
	endpoint     string
	validateFunc func(any) error
}

// Type returns the schema type.
func (p *BaseSchemaProvider) Type() SchemaType {
	return p.schemaType
}

// SpecVersion returns the specification version.
func (p *BaseSchemaProvider) SpecVersion() string {
	return p.specVersion
}

// ContentType returns the content type.
func (p *BaseSchemaProvider) ContentType() string {
	return p.contentType
}

// Endpoint returns the HTTP endpoint.
func (p *BaseSchemaProvider) Endpoint() string {
	return p.endpoint
}

// Hash calculates SHA256 hash of the schema.
func (p *BaseSchemaProvider) Hash(schema any) (string, error) {
	return CalculateSchemaChecksum(schema)
}

// Serialize converts schema to JSON bytes.
func (p *BaseSchemaProvider) Serialize(schema any) ([]byte, error) {
	return json.Marshal(schema)
}

// Validate validates the schema using custom validation function.
func (p *BaseSchemaProvider) Validate(schema any) error {
	if p.validateFunc != nil {
		return p.validateFunc(schema)
	}

	return nil
}

// ProviderRegistry manages registered schema providers.
type ProviderRegistry struct {
	providers map[SchemaType]SchemaProvider
}

// NewProviderRegistry creates a new provider registry.
func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{
		providers: make(map[SchemaType]SchemaProvider),
	}
}

// Register registers a schema provider.
func (r *ProviderRegistry) Register(provider SchemaProvider) {
	r.providers[provider.Type()] = provider
}

// Get retrieves a provider by schema type.
func (r *ProviderRegistry) Get(schemaType SchemaType) (SchemaProvider, bool) {
	provider, ok := r.providers[schemaType]

	return provider, ok
}

// Has checks if a provider exists for a schema type.
func (r *ProviderRegistry) Has(schemaType SchemaType) bool {
	_, ok := r.providers[schemaType]

	return ok
}

// List returns all registered schema types.
func (r *ProviderRegistry) List() []SchemaType {
	types := make([]SchemaType, 0, len(r.providers))
	for schemaType := range r.providers {
		types = append(types, schemaType)
	}

	return types
}

// Global provider registry.
var globalRegistry = NewProviderRegistry()

// RegisterProvider registers a schema provider globally.
func RegisterProvider(provider SchemaProvider) {
	globalRegistry.Register(provider)
}

// GetProvider retrieves a provider from the global registry.
func GetProvider(schemaType SchemaType) (SchemaProvider, bool) {
	return globalRegistry.Get(schemaType)
}

// HasProvider checks if a provider exists in the global registry.
func HasProvider(schemaType SchemaType) bool {
	return globalRegistry.Has(schemaType)
}

// ListProviders returns all registered schema types from the global registry.
func ListProviders() []SchemaType {
	return globalRegistry.List()
}
