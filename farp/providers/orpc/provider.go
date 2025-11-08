package orpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/xraph/forge/farp"
)

// Provider generates oRPC (OpenAPI-based RPC) schemas from applications
// oRPC is similar to OpenAPI but optimized for RPC-style calls.
type Provider struct {
	specVersion string
	endpoint    string
}

// NewProvider creates a new oRPC schema provider
// specVersion should be the oRPC specification version (e.g., "1.0.0").
func NewProvider(specVersion string, endpoint string) *Provider {
	if specVersion == "" {
		specVersion = "1.0.0"
	}

	if endpoint == "" {
		endpoint = "/orpc.json"
	}

	return &Provider{
		specVersion: specVersion,
		endpoint:    endpoint,
	}
}

// Type returns the schema type.
func (p *Provider) Type() farp.SchemaType {
	return farp.SchemaTypeORPC
}

// SpecVersion returns the oRPC specification version.
func (p *Provider) SpecVersion() string {
	return p.specVersion
}

// ContentType returns the content type.
func (p *Provider) ContentType() string {
	return "application/json"
}

// Endpoint returns the HTTP endpoint where the schema is served.
func (p *Provider) Endpoint() string {
	return p.endpoint
}

// Generate generates an oRPC schema from the application.
func (p *Provider) Generate(ctx context.Context, app farp.Application) (any, error) {
	// oRPC schemas are similar to OpenAPI but with RPC-specific conventions:
	// 1. Methods are typically POST endpoints
	// 2. Request/response are structured as RPC calls
	// 3. Support for batch operations
	// 4. Built-in error handling conventions
	//
	// For now, generate a minimal oRPC schema
	// This should integrate with Forge's router to extract RPC procedures
	routes := app.Routes()
	if routes == nil {
		return nil, errors.New("application does not provide routes")
	}

	// Build oRPC spec (similar to OpenAPI but RPC-focused)
	spec := map[string]any{
		"orpc": p.specVersion,
		"info": map[string]any{
			"title":       app.Name(),
			"version":     app.Version(),
			"description": "oRPC API for " + app.Name(),
		},
		"procedures": map[string]any{
			// Example procedure structure:
			// "getProcedureName": {
			//   "summary": "Description",
			//   "input": {...schema...},
			//   "output": {...schema...},
			//   "errors": [...error codes...],
			// }
			"health": map[string]any{
				"summary": "Health check procedure",
				"input": map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
				"output": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"status": map[string]any{
							"type": "string",
						},
						"timestamp": map[string]any{
							"type": "string",
						},
					},
					"required": []string{"status", "timestamp"},
				},
			},
			"version": map[string]any{
				"summary": "Get service version",
				"input": map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
				"output": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"version": map[string]any{
							"type": "string",
						},
					},
					"required": []string{"version"},
				},
			},
		},
		"components": map[string]any{
			"schemas": map[string]any{
				"Error": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"code": map[string]any{
							"type": "integer",
						},
						"message": map[string]any{
							"type": "string",
						},
						"details": map[string]any{
							"type": "object",
						},
					},
					"required": []string{"code", "message"},
				},
			},
		},
		"transport": map[string]any{
			"protocol": "http",
			"endpoint": "/rpc",
			"encoding": "json",
		},
	}

	// TODO: Process routes and generate procedures
	// This requires integration with Forge's router to identify RPC handlers

	return spec, nil
}

// Validate validates an oRPC schema.
func (p *Provider) Validate(schema any) error {
	// Basic validation - check for required fields
	schemaMap, ok := schema.(map[string]any)
	if !ok {
		return fmt.Errorf("%w: schema must be a map", farp.ErrInvalidSchema)
	}

	// Check orpc version
	if _, ok := schemaMap["orpc"]; !ok {
		return fmt.Errorf("%w: missing 'orpc' field", farp.ErrInvalidSchema)
	}

	// Check info
	if _, ok := schemaMap["info"]; !ok {
		return fmt.Errorf("%w: missing 'info' field", farp.ErrInvalidSchema)
	}

	// Check procedures
	if _, ok := schemaMap["procedures"]; !ok {
		return fmt.Errorf("%w: missing 'procedures' field", farp.ErrInvalidSchema)
	}

	// Validate procedures structure
	procedures, ok := schemaMap["procedures"].(map[string]any)
	if !ok {
		return fmt.Errorf("%w: 'procedures' must be an object", farp.ErrInvalidSchema)
	}

	// Each procedure should have input/output
	for procName, proc := range procedures {
		procMap, ok := proc.(map[string]any)
		if !ok {
			return fmt.Errorf("%w: procedure '%s' must be an object", farp.ErrInvalidSchema, procName)
		}

		if _, ok := procMap["input"]; !ok {
			return fmt.Errorf("%w: procedure '%s' missing 'input' field", farp.ErrInvalidSchema, procName)
		}

		if _, ok := procMap["output"]; !ok {
			return fmt.Errorf("%w: procedure '%s' missing 'output' field", farp.ErrInvalidSchema, procName)
		}
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

// SetEndpoint sets the HTTP endpoint for the oRPC schema.
func (p *Provider) SetEndpoint(endpoint string) {
	p.endpoint = endpoint
}
