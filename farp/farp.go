// Package farp provides Forge-specific integrations for the FARP protocol.
//
// This package wraps and extends github.com/xraph/farp with Forge-specific
// functionality, particularly for schema generation from Forge routers.
//
// DEPRECATED: This internal package is being phased out in favor of the
// external github.com/xraph/farp package. New code should import the
// external package directly. This package is maintained only for
// Forge-specific integrations (providers/openapi and providers/asyncapi).
//
// # Migration Guide
//
// Old import:
//
//	import "github.com/xraph/forge/farp"
//
// New import:
//
//	import "github.com/xraph/farp"
//
// The providers remain in this package for Forge-specific integration:
//
//	import "github.com/xraph/forge/farp/providers/openapi"
//	import "github.com/xraph/forge/farp/providers/asyncapi"
//
// For full FARP documentation, see: https://github.com/xraph/farp
package farp

// Re-export core FARP types from the external package for backward compatibility
import "github.com/xraph/farp"

// Type re-exports
type (
	SchemaType       = farp.SchemaType
	LocationType     = farp.LocationType
	SchemaManifest   = farp.SchemaManifest
	SchemaDescriptor = farp.SchemaDescriptor
	SchemaLocation   = farp.SchemaLocation
	SchemaEndpoints  = farp.SchemaEndpoints
	Capability       = farp.Capability
	SchemaProvider   = farp.SchemaProvider
	Application      = farp.Application
	SchemaRegistry   = farp.SchemaRegistry
	EventType        = farp.EventType
	ManifestEvent    = farp.ManifestEvent
	SchemaEvent      = farp.SchemaEvent
	RegistryConfig   = farp.RegistryConfig
	ValidationError  = farp.ValidationError
)

// Constant re-exports
const (
	SchemaTypeOpenAPI  = farp.SchemaTypeOpenAPI
	SchemaTypeAsyncAPI = farp.SchemaTypeAsyncAPI
	SchemaTypeGRPC     = farp.SchemaTypeGRPC
	SchemaTypeGraphQL  = farp.SchemaTypeGraphQL
	SchemaTypeORPC     = farp.SchemaTypeORPC
	SchemaTypeThrift   = farp.SchemaTypeThrift
	SchemaTypeAvro     = farp.SchemaTypeAvro
	SchemaTypeCustom   = farp.SchemaTypeCustom

	LocationTypeHTTP     = farp.LocationTypeHTTP
	LocationTypeRegistry = farp.LocationTypeRegistry
	LocationTypeInline   = farp.LocationTypeInline

	CapabilityREST      = farp.CapabilityREST
	CapabilityGRPC      = farp.CapabilityGRPC
	CapabilityWebSocket = farp.CapabilityWebSocket
	CapabilitySSE       = farp.CapabilitySSE
	CapabilityGraphQL   = farp.CapabilityGraphQL
	CapabilityMQTT      = farp.CapabilityMQTT
	CapabilityAMQP      = farp.CapabilityAMQP

	EventTypeAdded   = farp.EventTypeAdded
	EventTypeUpdated = farp.EventTypeUpdated
	EventTypeRemoved = farp.EventTypeRemoved

	ProtocolVersion = farp.ProtocolVersion
)

// Error re-exports
var (
	ErrInvalidManifest     = farp.ErrInvalidManifest
	ErrInvalidSchema       = farp.ErrInvalidSchema
	ErrInvalidLocation     = farp.ErrInvalidLocation
	ErrUnsupportedType     = farp.ErrUnsupportedType
	ErrChecksumMismatch    = farp.ErrChecksumMismatch
	ErrIncompatibleVersion = farp.ErrIncompatibleVersion
	ErrManifestNotFound    = farp.ErrManifestNotFound
	ErrSchemaNotFound      = farp.ErrSchemaNotFound
)

// Function re-exports
var (
	NewManifest               = farp.NewManifest
	CalculateManifestChecksum = farp.CalculateManifestChecksum
	CalculateSchemaChecksum   = farp.CalculateSchemaChecksum
	FromJSON                  = farp.FromJSON
	ValidateSchemaDescriptor  = farp.ValidateSchemaDescriptor
	DiffManifests             = farp.DiffManifests
	IsCompatible              = farp.IsCompatible
	DefaultRegistryConfig     = farp.DefaultRegistryConfig
)
