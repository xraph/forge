package graphql

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/xraph/forge/farp"
)

// Provider generates GraphQL schemas (SDL - Schema Definition Language) from applications.
type Provider struct {
	specVersion string
	endpoint    string
	useSDL      bool // If true, generate SDL; if false, use introspection query result
}

// NewProvider creates a new GraphQL schema provider
// specVersion should be "2021" (June 2021 Edition) or "2018" (October 2018 Edition)
// endpoint is typically "/graphql" for introspection.
func NewProvider(specVersion string, endpoint string) *Provider {
	if specVersion == "" {
		specVersion = "2021"
	}

	if endpoint == "" {
		endpoint = "/graphql"
	}

	return &Provider{
		specVersion: specVersion,
		endpoint:    endpoint,
		useSDL:      true, // Default to SDL format
	}
}

// Type returns the schema type.
func (p *Provider) Type() farp.SchemaType {
	return farp.SchemaTypeGraphQL
}

// SpecVersion returns the GraphQL specification version.
func (p *Provider) SpecVersion() string {
	return p.specVersion
}

// ContentType returns the content type.
func (p *Provider) ContentType() string {
	if p.useSDL {
		return "application/graphql" // SDL text format
	}

	return "application/json" // Introspection query result
}

// Endpoint returns the HTTP endpoint where GraphQL introspection is available.
func (p *Provider) Endpoint() string {
	return p.endpoint
}

// Generate generates a GraphQL schema from the application.
func (p *Provider) Generate(ctx context.Context, app farp.Application) (any, error) {
	// For GraphQL, we have two options:
	// 1. Generate SDL (Schema Definition Language) directly
	// 2. Use introspection query to extract schema
	//
	// The introspection query is:
	// query IntrospectionQuery {
	//   __schema {
	//     types { name kind fields { name type { name kind } } }
	//     queryType { name }
	//     mutationType { name }
	//     subscriptionType { name }
	//   }
	// }
	if p.useSDL {
		return p.generateSDL(ctx, app)
	}

	return p.generateIntrospectionResult(ctx, app)
}

// generateSDL generates GraphQL Schema Definition Language.
func (p *Provider) generateSDL(ctx context.Context, app farp.Application) (any, error) {
	// This would integrate with Forge's GraphQL support
	// or parse existing schema files
	//
	// For now, return a minimal SDL as a map
	schema := map[string]any{
		"sdl": fmt.Sprintf(`
# GraphQL Schema for %s v%s

schema {
  query: Query
  mutation: Mutation
}

type Query {
  # Health check
  health: HealthStatus!
  
  # Version information
  version: String!
}

type Mutation {
  # Placeholder for mutations
  _empty: String
}

type HealthStatus {
  status: String!
  timestamp: String!
}
`, app.Name(), app.Version()),
		"spec_version": p.specVersion,
		"format":       "SDL",
	}

	return schema, nil
}

// generateIntrospectionResult generates GraphQL introspection query result.
func (p *Provider) generateIntrospectionResult(ctx context.Context, app farp.Application) (any, error) {
	// This would execute the introspection query against a running GraphQL server
	// and return the result
	//
	// For now, return a minimal introspection result structure
	schema := map[string]any{
		"data": map[string]any{
			"__schema": map[string]any{
				"queryType": map[string]any{
					"name": "Query",
				},
				"mutationType": map[string]any{
					"name": "Mutation",
				},
				"subscriptionType": nil,
				"types": []any{
					map[string]any{
						"kind": "OBJECT",
						"name": "Query",
						"fields": []any{
							map[string]any{
								"name": "health",
								"type": map[string]any{
									"kind": "NON_NULL",
									"name": nil,
									"ofType": map[string]any{
										"kind": "OBJECT",
										"name": "HealthStatus",
									},
								},
							},
							map[string]any{
								"name": "version",
								"type": map[string]any{
									"kind": "NON_NULL",
									"name": nil,
									"ofType": map[string]any{
										"kind": "SCALAR",
										"name": "String",
									},
								},
							},
						},
					},
					map[string]any{
						"kind": "OBJECT",
						"name": "HealthStatus",
						"fields": []any{
							map[string]any{
								"name": "status",
								"type": map[string]any{
									"kind": "NON_NULL",
									"name": nil,
									"ofType": map[string]any{
										"kind": "SCALAR",
										"name": "String",
									},
								},
							},
							map[string]any{
								"name": "timestamp",
								"type": map[string]any{
									"kind": "NON_NULL",
									"name": nil,
									"ofType": map[string]any{
										"kind": "SCALAR",
										"name": "String",
									},
								},
							},
						},
					},
				},
			},
		},
		"spec_version": p.specVersion,
		"format":       "introspection",
	}

	return schema, nil
}

// Validate validates a GraphQL schema.
func (p *Provider) Validate(schema any) error {
	schemaMap, ok := schema.(map[string]any)
	if !ok {
		return fmt.Errorf("%w: schema must be a map", farp.ErrInvalidSchema)
	}

	// Check for either SDL or introspection format
	format, _ := schemaMap["format"].(string)

	switch format {
	case "SDL":
		// Check for sdl field
		if _, ok := schemaMap["sdl"]; !ok {
			return fmt.Errorf("%w: missing 'sdl' field for SDL format", farp.ErrInvalidSchema)
		}

	case "introspection":
		// Check for data.__schema
		data, ok := schemaMap["data"].(map[string]any)
		if !ok {
			return fmt.Errorf("%w: missing 'data' field for introspection format", farp.ErrInvalidSchema)
		}

		if _, ok := data["__schema"]; !ok {
			return fmt.Errorf("%w: missing '__schema' field in introspection data", farp.ErrInvalidSchema)
		}

	default:
		return fmt.Errorf("%w: unknown GraphQL schema format: %s", farp.ErrInvalidSchema, format)
	}

	return nil
}

// Hash calculates SHA256 hash of the schema.
func (p *Provider) Hash(schema any) (string, error) {
	return farp.CalculateSchemaChecksum(schema)
}

// Serialize converts schema to JSON bytes.
func (p *Provider) Serialize(schema any) ([]byte, error) {
	return json.Marshal(schema)
}

// GenerateDescriptor generates a complete SchemaDescriptor for this schema.
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

// UseSDL configures the provider to generate SDL format.
func (p *Provider) UseSDL() {
	p.useSDL = true
}

// UseIntrospection configures the provider to generate introspection query result.
func (p *Provider) UseIntrospection() {
	p.useSDL = false
}

// SetEndpoint sets the GraphQL endpoint for introspection.
func (p *Provider) SetEndpoint(endpoint string) {
	p.endpoint = endpoint
}
