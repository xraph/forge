package grpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/xraph/forge/farp"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// Provider generates gRPC schemas (FileDescriptorSet) from applications.
type Provider struct {
	specVersion string
	endpoint    string
	protoFiles  []string // Optional: paths to .proto files
}

// NewProvider creates a new gRPC schema provider
// specVersion should be "proto3" (recommended) or "proto2"
// endpoint is typically empty for gRPC (uses reflection).
func NewProvider(specVersion string, protoFiles []string) *Provider {
	if specVersion == "" {
		specVersion = "proto3"
	}

	return &Provider{
		specVersion: specVersion,
		endpoint:    "", // gRPC uses reflection, not HTTP endpoint
		protoFiles:  protoFiles,
	}
}

// Type returns the schema type.
func (p *Provider) Type() farp.SchemaType {
	return farp.SchemaTypeGRPC
}

// SpecVersion returns the Protocol Buffer version.
func (p *Provider) SpecVersion() string {
	return p.specVersion
}

// ContentType returns the content type
// Can be "application/x-protobuf" for binary or "application/json" for JSON representation.
func (p *Provider) ContentType() string {
	return "application/json" // JSON representation of FileDescriptorSet
}

// Endpoint returns the HTTP endpoint (empty for gRPC - uses reflection).
func (p *Provider) Endpoint() string {
	return p.endpoint
}

// Generate generates a gRPC schema (FileDescriptorSet) from the application.
func (p *Provider) Generate(ctx context.Context, app farp.Application) (any, error) {
	// For gRPC, we have two options:
	// 1. Parse .proto files if provided
	// 2. Use gRPC server reflection to extract FileDescriptorSet
	//
	// For now, we return a minimal FileDescriptorSet structure
	// In production, this should use:
	// - protoc to compile .proto files
	// - grpc reflection client to extract from running server
	// - Forge's gRPC integration to extract service definitions
	if len(p.protoFiles) > 0 {
		return p.generateFromProtoFiles(ctx, app)
	}

	return p.generateFromReflection(ctx, app)
}

// generateFromProtoFiles generates schema by parsing .proto files.
func (p *Provider) generateFromProtoFiles(ctx context.Context, app farp.Application) (any, error) {
	// This would use protoc or a Go protobuf parser
	// to read .proto files and generate FileDescriptorSet
	//
	// For now, return a minimal structure
	fds := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{
			{
				Name:    proto.String(app.Name() + ".proto"),
				Package: proto.String(app.Name()),
				Syntax:  proto.String(p.specVersion),
			},
		},
	}

	// Convert to JSON representation for storage
	return protojson.Marshal(fds)
}

// generateFromReflection generates schema using gRPC server reflection.
func (p *Provider) generateFromReflection(ctx context.Context, app farp.Application) (any, error) {
	// This would connect to the gRPC server's reflection service
	// and extract the FileDescriptorSet
	//
	// The reflection service is defined in:
	// google.golang.org/grpc/reflection
	//
	// For now, return a minimal structure
	fds := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{
			{
				Name:    proto.String(app.Name() + ".proto"),
				Package: proto.String(app.Name()),
				Syntax:  proto.String(p.specVersion),
				Service: []*descriptorpb.ServiceDescriptorProto{
					{
						Name: proto.String(app.Name() + "Service"),
					},
				},
			},
		},
	}

	// Convert to JSON for easier storage and transmission
	data, err := protojson.Marshal(fds)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal FileDescriptorSet: %w", err)
	}

	// Return as map for consistency with other providers
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal to map: %w", err)
	}

	return result, nil
}

// Validate validates a gRPC schema (FileDescriptorSet).
func (p *Provider) Validate(schema any) error {
	// Check if it's a map (JSON representation)
	schemaMap, ok := schema.(map[string]any)
	if !ok {
		return fmt.Errorf("%w: schema must be a map", farp.ErrInvalidSchema)
	}

	// Check for 'file' field (FileDescriptorSet.file)
	files, ok := schemaMap["file"]
	if !ok {
		return fmt.Errorf("%w: missing 'file' field in FileDescriptorSet", farp.ErrInvalidSchema)
	}

	// Ensure files is an array
	if _, ok := files.([]any); !ok {
		return fmt.Errorf("%w: 'file' must be an array", farp.ErrInvalidSchema)
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

// SetProtoFiles sets the .proto files to parse.
func (p *Provider) SetProtoFiles(files []string) {
	p.protoFiles = files
}

// EnableReflection configures the provider to use gRPC reflection.
func (p *Provider) EnableReflection() {
	p.protoFiles = nil // Clear proto files to use reflection
}
