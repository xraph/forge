package farp

import "context"

// SchemaRegistry manages schema manifests and schemas
// Implementations store data in various backends (Consul, etcd, Kubernetes, Redis, etc.)
type SchemaRegistry interface {
	// Manifest operations

	// RegisterManifest registers a new schema manifest
	RegisterManifest(ctx context.Context, manifest *SchemaManifest) error

	// GetManifest retrieves a schema manifest by instance ID
	GetManifest(ctx context.Context, instanceID string) (*SchemaManifest, error)

	// UpdateManifest updates an existing schema manifest
	UpdateManifest(ctx context.Context, manifest *SchemaManifest) error

	// DeleteManifest removes a schema manifest
	DeleteManifest(ctx context.Context, instanceID string) error

	// ListManifests lists all manifests for a service name
	// Returns empty slice if serviceName is empty (list all services)
	ListManifests(ctx context.Context, serviceName string) ([]*SchemaManifest, error)

	// Schema operations

	// PublishSchema stores a schema in the registry
	// path is the registry path (e.g., "/schemas/user-service/v1/openapi")
	// schema is the schema content (typically map[string]interface{} or struct)
	PublishSchema(ctx context.Context, path string, schema any) error

	// FetchSchema retrieves a schema from the registry
	// Returns the schema as interface{} (must be type-asserted by caller)
	FetchSchema(ctx context.Context, path string) (any, error)

	// DeleteSchema removes a schema from the registry
	DeleteSchema(ctx context.Context, path string) error

	// Watch operations

	// WatchManifests watches for manifest changes for a service
	// onChange is called when a manifest is added, updated, or removed
	// Returns an error if watch setup fails
	// The watch continues until context is cancelled
	WatchManifests(ctx context.Context, serviceName string, onChange ManifestChangeHandler) error

	// WatchSchemas watches for schema changes at a specific path
	// onChange is called when the schema at path changes
	WatchSchemas(ctx context.Context, path string, onChange SchemaChangeHandler) error

	// Lifecycle

	// Close closes the registry and cleans up resources
	Close() error

	// Health checks if the registry backend is healthy
	Health(ctx context.Context) error
}

// ManifestChangeHandler is called when a manifest changes.
type ManifestChangeHandler func(event *ManifestEvent)

// SchemaChangeHandler is called when a schema changes.
type SchemaChangeHandler func(event *SchemaEvent)

// ManifestEvent represents a manifest change event.
type ManifestEvent struct {
	// Type of event (added, updated, removed)
	Type EventType

	// The manifest that changed
	Manifest *SchemaManifest

	// Timestamp of the event (Unix timestamp)
	Timestamp int64
}

// SchemaEvent represents a schema change event.
type SchemaEvent struct {
	// Type of event (added, updated, removed)
	Type EventType

	// Path where the schema is stored
	Path string

	// The schema content (nil for removed events)
	Schema any

	// Timestamp of the event (Unix timestamp)
	Timestamp int64
}

// EventType represents the type of change event.
type EventType string

const (
	// EventTypeAdded indicates a resource was added.
	EventTypeAdded EventType = "added"

	// EventTypeUpdated indicates a resource was updated.
	EventTypeUpdated EventType = "updated"

	// EventTypeRemoved indicates a resource was removed.
	EventTypeRemoved EventType = "removed"
)

// String returns the string representation of the event type.
func (et EventType) String() string {
	return string(et)
}

// RegistryConfig holds configuration for a schema registry.
type RegistryConfig struct {
	// Backend type (consul, etcd, kubernetes, redis, memory)
	Backend string

	// Namespace/prefix for keys (optional)
	Namespace string

	// Backend-specific configuration (varies by implementation)
	BackendConfig map[string]any

	// Max schema size in bytes (default: 1MB)
	MaxSchemaSize int64

	// Enable compression for schemas > threshold
	CompressionThreshold int64

	// TTL for schemas (0 = no expiry)
	TTL int64
}

// DefaultRegistryConfig returns default registry configuration.
func DefaultRegistryConfig() RegistryConfig {
	return RegistryConfig{
		Backend:              "memory",
		Namespace:            "farp",
		BackendConfig:        make(map[string]any),
		MaxSchemaSize:        1024 * 1024, // 1MB
		CompressionThreshold: 100 * 1024,  // 100KB
		TTL:                  0,           // No expiry
	}
}

// SchemaCache provides caching for fetched schemas.
type SchemaCache interface {
	// Get retrieves a cached schema by hash
	Get(hash string) (any, bool)

	// Set stores a schema in cache with its hash
	Set(hash string, schema any) error

	// Delete removes a schema from cache
	Delete(hash string) error

	// Clear removes all cached schemas
	Clear() error

	// Size returns the number of cached schemas
	Size() int
}

// FetchOptions provides options for fetching schemas.
type FetchOptions struct {
	// UseCache indicates whether to use cache
	UseCache bool

	// ValidateChecksum verifies schema hash after fetch
	ValidateChecksum bool

	// ExpectedHash is the expected SHA256 hash (for validation)
	ExpectedHash string

	// Timeout for fetch operation (0 = no timeout)
	Timeout int64
}

// PublishOptions provides options for publishing schemas.
type PublishOptions struct {
	// Compress indicates whether to compress the schema
	Compress bool

	// TTL is time-to-live in seconds (0 = no expiry)
	TTL int64

	// OverwriteExisting allows overwriting existing schemas
	OverwriteExisting bool
}
