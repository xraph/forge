package avro

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/xraph/forge/farp"
)

// Provider generates Apache Avro schemas from applications
type Provider struct {
	specVersion string
	endpoint    string
	schemaFiles []string // Optional: paths to .avsc files
}

// NewProvider creates a new Avro schema provider
// specVersion should be the Avro specification version (e.g., "1.11.1")
func NewProvider(specVersion string, schemaFiles []string) *Provider {
	if specVersion == "" {
		specVersion = "1.11.1"
	}

	return &Provider{
		specVersion: specVersion,
		endpoint:    "/avro.json", // Avro schemas are typically JSON
		schemaFiles: schemaFiles,
	}
}

// Type returns the schema type
func (p *Provider) Type() farp.SchemaType {
	return farp.SchemaTypeAvro
}

// SpecVersion returns the Avro specification version
func (p *Provider) SpecVersion() string {
	return p.specVersion
}

// ContentType returns the content type
func (p *Provider) ContentType() string {
	return "application/json" // Avro schemas are JSON
}

// Endpoint returns the HTTP endpoint where the schema is served
func (p *Provider) Endpoint() string {
	return p.endpoint
}

// Generate generates an Avro schema from the application
func (p *Provider) Generate(ctx context.Context, app farp.Application) (interface{}, error) {
	// For Avro, we have several options:
	// 1. Parse .avsc schema files
	// 2. Generate schemas from application data structures
	// 3. Use Avro schema registry
	//
	// Avro schemas define data serialization format
	// They are commonly used with Kafka, Hadoop, and other big data systems

	if len(p.schemaFiles) > 0 {
		return p.generateFromSchemaFiles(ctx, app)
	}

	return p.generateFromApplication(ctx, app)
}

// generateFromSchemaFiles generates schema by parsing .avsc files
func (p *Provider) generateFromSchemaFiles(ctx context.Context, app farp.Application) (interface{}, error) {
	// This would parse Avro schema files (.avsc)
	// For now, return a minimal structure
	schema := map[string]interface{}{
		"avro_version": p.specVersion,
		"protocol":     app.Name() + "Protocol",
		"namespace":    "com." + app.Name(),
		"types": []interface{}{
			map[string]interface{}{
				"type": "record",
				"name": "HealthStatus",
				"fields": []interface{}{
					map[string]interface{}{"name": "status", "type": "string"},
					map[string]interface{}{"name": "timestamp", "type": "long"},
				},
			},
		},
		"messages": map[string]interface{}{
			"getVersion": map[string]interface{}{
				"request":  []interface{}{},
				"response": "string",
			},
		},
		"schema_files": p.schemaFiles,
	}

	return schema, nil
}

// generateFromApplication generates schema from application structure
func (p *Provider) generateFromApplication(ctx context.Context, app farp.Application) (interface{}, error) {
	// This would analyze the application's data structures
	// and generate Avro schema definitions
	//
	// Avro Protocol format includes types and messages
	schema := map[string]interface{}{
		"avro_version": p.specVersion,
		"protocol":     app.Name() + "Protocol",
		"namespace":    "com." + app.Name(),
		"doc":          "Avro protocol for " + app.Name(),
		"types": []interface{}{
			map[string]interface{}{
				"type": "record",
				"name": "HealthStatus",
				"doc":  "Health status response",
				"fields": []interface{}{
					map[string]interface{}{
						"name": "status",
						"type": "string",
						"doc":  "Current health status",
					},
					map[string]interface{}{
						"name": "timestamp",
						"type": "long",
						"doc":  "Unix timestamp in milliseconds",
					},
				},
			},
			map[string]interface{}{
				"type": "record",
				"name": "VersionInfo",
				"doc":  "Version information",
				"fields": []interface{}{
					map[string]interface{}{
						"name": "version",
						"type": "string",
					},
					map[string]interface{}{
						"name":    "buildTime",
						"type":    []interface{}{"null", "long"},
						"default": nil,
					},
				},
			},
		},
		"messages": map[string]interface{}{
			"health": map[string]interface{}{
				"doc":      "Get health status",
				"request":  []interface{}{},
				"response": "HealthStatus",
			},
			"getVersion": map[string]interface{}{
				"doc":      "Get version information",
				"request":  []interface{}{},
				"response": "VersionInfo",
			},
		},
	}

	return schema, nil
}

// Validate validates an Avro schema
func (p *Provider) Validate(schema interface{}) error {
	// Basic validation - check for required fields
	schemaMap, ok := schema.(map[string]interface{})
	if !ok {
		return fmt.Errorf("%w: schema must be a map", farp.ErrInvalidSchema)
	}

	// Check for protocol name (Avro Protocol format)
	if _, ok := schemaMap["protocol"]; !ok {
		return fmt.Errorf("%w: missing 'protocol' field", farp.ErrInvalidSchema)
	}

	// Check for namespace
	if _, ok := schemaMap["namespace"]; !ok {
		return fmt.Errorf("%w: missing 'namespace' field", farp.ErrInvalidSchema)
	}

	// Check for types
	types, ok := schemaMap["types"]
	if !ok {
		return fmt.Errorf("%w: missing 'types' field", farp.ErrInvalidSchema)
	}

	// Ensure types is an array
	if _, ok := types.([]interface{}); !ok {
		return fmt.Errorf("%w: 'types' must be an array", farp.ErrInvalidSchema)
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

// SetSchemaFiles sets the .avsc schema files to parse
func (p *Provider) SetSchemaFiles(files []string) {
	p.schemaFiles = files
}

// GetSchemaFiles returns the configured schema files
func (p *Provider) GetSchemaFiles() []string {
	return p.schemaFiles
}

// SetEndpoint sets the HTTP endpoint for the Avro schema
func (p *Provider) SetEndpoint(endpoint string) {
	p.endpoint = endpoint
}
