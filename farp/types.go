package farp

import "fmt"

// SchemaType represents supported schema/protocol types
type SchemaType string

const (
	// SchemaTypeOpenAPI represents OpenAPI/Swagger specifications
	SchemaTypeOpenAPI SchemaType = "openapi"

	// SchemaTypeAsyncAPI represents AsyncAPI specifications
	SchemaTypeAsyncAPI SchemaType = "asyncapi"

	// SchemaTypeGRPC represents gRPC protocol buffer definitions
	SchemaTypeGRPC SchemaType = "grpc"

	// SchemaTypeGraphQL represents GraphQL Schema Definition Language
	SchemaTypeGraphQL SchemaType = "graphql"

	// SchemaTypeORPC represents oRPC (OpenAPI-based RPC) specifications
	SchemaTypeORPC SchemaType = "orpc"

	// SchemaTypeThrift represents Apache Thrift IDL (future support)
	SchemaTypeThrift SchemaType = "thrift"

	// SchemaTypeAvro represents Apache Avro schemas (future support)
	SchemaTypeAvro SchemaType = "avro"

	// SchemaTypeCustom represents custom/proprietary schema types
	SchemaTypeCustom SchemaType = "custom"
)

// IsValid checks if the schema type is valid
func (st SchemaType) IsValid() bool {
	switch st {
	case SchemaTypeOpenAPI, SchemaTypeAsyncAPI, SchemaTypeGRPC,
		SchemaTypeGraphQL, SchemaTypeORPC, SchemaTypeThrift, SchemaTypeAvro, SchemaTypeCustom:
		return true
	default:
		return false
	}
}

// String returns the string representation of the schema type
func (st SchemaType) String() string {
	return string(st)
}

// LocationType represents how schemas can be retrieved
type LocationType string

const (
	// LocationTypeHTTP means fetch schema via HTTP GET
	LocationTypeHTTP LocationType = "http"

	// LocationTypeRegistry means fetch schema from backend KV store
	LocationTypeRegistry LocationType = "registry"

	// LocationTypeInline means schema is embedded in the manifest
	LocationTypeInline LocationType = "inline"
)

// IsValid checks if the location type is valid
func (lt LocationType) IsValid() bool {
	switch lt {
	case LocationTypeHTTP, LocationTypeRegistry, LocationTypeInline:
		return true
	default:
		return false
	}
}

// String returns the string representation of the location type
func (lt LocationType) String() string {
	return string(lt)
}

// SchemaManifest describes all API contracts for a service instance
type SchemaManifest struct {
	// Version of the FARP protocol (semver)
	Version string `json:"version"`

	// Service identity
	ServiceName    string `json:"service_name"`
	ServiceVersion string `json:"service_version"`
	InstanceID     string `json:"instance_id"`

	// Schemas exposed by this instance
	Schemas []SchemaDescriptor `json:"schemas"`

	// Capabilities/protocols supported (e.g., ["rest", "grpc", "websocket"])
	Capabilities []string `json:"capabilities"`

	// Endpoints for introspection and health
	Endpoints SchemaEndpoints `json:"endpoints"`

	// Change tracking
	UpdatedAt int64  `json:"updated_at"` // Unix timestamp
	Checksum  string `json:"checksum"`   // SHA256 of all schemas combined
}

// SchemaDescriptor describes a single API schema/contract
type SchemaDescriptor struct {
	// Type of schema (openapi, asyncapi, grpc, graphql, etc.)
	Type SchemaType `json:"type"`

	// Specification version (e.g., "3.1.0" for OpenAPI, "3.0.0" for AsyncAPI)
	SpecVersion string `json:"spec_version"`

	// How to retrieve the schema
	Location SchemaLocation `json:"location"`

	// Content type (e.g., "application/json", "application/x-protobuf")
	ContentType string `json:"content_type"`

	// Optional: Inline schema for small schemas (< 100KB recommended)
	InlineSchema interface{} `json:"inline_schema,omitempty"`

	// Integrity validation
	Hash string `json:"hash"` // SHA256 of schema content
	Size int64  `json:"size"` // Size in bytes
}

// SchemaLocation describes where and how to fetch a schema
type SchemaLocation struct {
	// Location type (http, registry, inline)
	Type LocationType `json:"type"`

	// HTTP URL (if Type == HTTP)
	// Example: "http://user-service:8080/openapi.json"
	URL string `json:"url,omitempty"`

	// Registry path in backend KV store (if Type == Registry)
	// Example: "/schemas/user-service/v1/openapi"
	RegistryPath string `json:"registry_path,omitempty"`

	// HTTP headers for authentication (if Type == HTTP)
	// Example: {"Authorization": "Bearer token"}
	Headers map[string]string `json:"headers,omitempty"`
}

// Validate checks if the schema location is valid
func (sl *SchemaLocation) Validate() error {
	if !sl.Type.IsValid() {
		return fmt.Errorf("%w: invalid location type: %s", ErrInvalidLocation, sl.Type)
	}

	switch sl.Type {
	case LocationTypeHTTP:
		if sl.URL == "" {
			return fmt.Errorf("%w: URL required for HTTP location", ErrInvalidLocation)
		}
	case LocationTypeRegistry:
		if sl.RegistryPath == "" {
			return fmt.Errorf("%w: registry path required for registry location", ErrInvalidLocation)
		}
	case LocationTypeInline:
		// No additional validation needed for inline
	}

	return nil
}

// SchemaEndpoints provides URLs for service introspection
type SchemaEndpoints struct {
	// Health check endpoint (required)
	// Example: "/health" or "/healthz"
	Health string `json:"health"`

	// Prometheus metrics endpoint (optional)
	// Example: "/metrics"
	Metrics string `json:"metrics,omitempty"`

	// OpenAPI spec endpoint (optional)
	// Example: "/openapi.json"
	OpenAPI string `json:"openapi,omitempty"`

	// AsyncAPI spec endpoint (optional)
	// Example: "/asyncapi.json"
	AsyncAPI string `json:"asyncapi,omitempty"`

	// Whether gRPC server reflection is enabled
	GRPCReflection bool `json:"grpc_reflection,omitempty"`

	// GraphQL introspection endpoint (optional)
	// Example: "/graphql"
	GraphQL string `json:"graphql,omitempty"`
}

// Capability represents a protocol capability
type Capability string

const (
	// CapabilityREST indicates REST API support
	CapabilityREST Capability = "rest"

	// CapabilityGRPC indicates gRPC support
	CapabilityGRPC Capability = "grpc"

	// CapabilityWebSocket indicates WebSocket support
	CapabilityWebSocket Capability = "websocket"

	// CapabilitySSE indicates Server-Sent Events support
	CapabilitySSE Capability = "sse"

	// CapabilityGraphQL indicates GraphQL support
	CapabilityGraphQL Capability = "graphql"

	// CapabilityMQTT indicates MQTT support
	CapabilityMQTT Capability = "mqtt"

	// CapabilityAMQP indicates AMQP support
	CapabilityAMQP Capability = "amqp"
)

// String returns the string representation of the capability
func (c Capability) String() string {
	return string(c)
}
