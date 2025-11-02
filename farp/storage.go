package farp

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// StorageBackend provides low-level key-value storage operations
// This abstracts the underlying storage mechanism (Consul KV, etcd, Redis, etc.)
type StorageBackend interface {
	// Put stores a value at the given key
	Put(ctx context.Context, key string, value []byte) error

	// Get retrieves a value by key
	// Returns ErrSchemaNotFound if key doesn't exist
	Get(ctx context.Context, key string) ([]byte, error)

	// Delete removes a key
	Delete(ctx context.Context, key string) error

	// List lists all keys with the given prefix
	List(ctx context.Context, prefix string) ([]string, error)

	// Watch watches for changes to keys with the given prefix
	// Returns a channel that receives change events
	Watch(ctx context.Context, prefix string) (<-chan StorageEvent, error)

	// Close closes the backend connection
	Close() error
}

// StorageEvent represents a storage change event
type StorageEvent struct {
	// Type of event
	Type EventType

	// Key that changed
	Key string

	// Value (nil for delete events)
	Value []byte
}

// StorageHelper provides utility functions for storage operations
type StorageHelper struct {
	backend           StorageBackend
	compressionThreshold int64
	maxSize           int64
}

// NewStorageHelper creates a new storage helper
func NewStorageHelper(backend StorageBackend, compressionThreshold, maxSize int64) *StorageHelper {
	return &StorageHelper{
		backend:           backend,
		compressionThreshold: compressionThreshold,
		maxSize:           maxSize,
	}
}

// PutJSON stores a JSON-serializable value
func (h *StorageHelper) PutJSON(ctx context.Context, key string, value interface{}) error {
	// Serialize to JSON
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	// Check size limit
	if h.maxSize > 0 && int64(len(data)) > h.maxSize {
		return fmt.Errorf("%w: %d bytes (max %d)", ErrSchemaToLarge, len(data), h.maxSize)
	}

	// Compress if above threshold
	if h.compressionThreshold > 0 && int64(len(data)) > h.compressionThreshold {
		compressed, err := compressData(data)
		if err != nil {
			return fmt.Errorf("failed to compress data: %w", err)
		}
		data = compressed
		key = key + ".gz"
	}

	return h.backend.Put(ctx, key, data)
}

// GetJSON retrieves and deserializes a JSON value
func (h *StorageHelper) GetJSON(ctx context.Context, key string, target interface{}) error {
	// Try compressed version first
	data, err := h.backend.Get(ctx, key+".gz")
	if err == nil {
		// Decompress
		decompressed, err := decompressData(data)
		if err != nil {
			return fmt.Errorf("failed to decompress data: %w", err)
		}
		data = decompressed
	} else {
		// Try uncompressed version
		data, err = h.backend.Get(ctx, key)
		if err != nil {
			return err
		}
	}

	// Deserialize JSON
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	return nil
}

// compressData compresses data using gzip
func compressData(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)

	if _, err := writer.Write(data); err != nil {
		writer.Close()
		return nil, err
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// decompressData decompresses gzip data
func decompressData(data []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	return io.ReadAll(reader)
}

// ManifestStorage provides high-level operations for manifest storage
type ManifestStorage struct {
	helper    *StorageHelper
	namespace string
}

// NewManifestStorage creates a new manifest storage
func NewManifestStorage(backend StorageBackend, namespace string, compressionThreshold, maxSize int64) *ManifestStorage {
	return &ManifestStorage{
		helper:    NewStorageHelper(backend, compressionThreshold, maxSize),
		namespace: namespace,
	}
}

// manifestKey generates a storage key for a manifest
func (s *ManifestStorage) manifestKey(serviceName, instanceID string) string {
	return fmt.Sprintf("%s/services/%s/instances/%s/manifest", s.namespace, serviceName, instanceID)
}

// schemaKey generates a storage key for a schema
func (s *ManifestStorage) schemaKey(path string) string {
	return fmt.Sprintf("%s%s", s.namespace, path)
}

// Put stores a manifest
func (s *ManifestStorage) Put(ctx context.Context, manifest *SchemaManifest) error {
	key := s.manifestKey(manifest.ServiceName, manifest.InstanceID)
	return s.helper.PutJSON(ctx, key, manifest)
}

// Get retrieves a manifest
func (s *ManifestStorage) Get(ctx context.Context, serviceName, instanceID string) (*SchemaManifest, error) {
	key := s.manifestKey(serviceName, instanceID)
	var manifest SchemaManifest
	if err := s.helper.GetJSON(ctx, key, &manifest); err != nil {
		if err == ErrSchemaNotFound {
			return nil, ErrManifestNotFound
		}
		return nil, err
	}
	return &manifest, nil
}

// Delete removes a manifest
func (s *ManifestStorage) Delete(ctx context.Context, serviceName, instanceID string) error {
	key := s.manifestKey(serviceName, instanceID)
	return s.helper.backend.Delete(ctx, key)
}

// List lists all manifests for a service
func (s *ManifestStorage) List(ctx context.Context, serviceName string) ([]*SchemaManifest, error) {
	prefix := fmt.Sprintf("%s/services/%s/instances/", s.namespace, serviceName)
	keys, err := s.helper.backend.List(ctx, prefix)
	if err != nil {
		return nil, err
	}

	manifests := make([]*SchemaManifest, 0, len(keys))
	for _, key := range keys {
		var manifest SchemaManifest
		if err := s.helper.GetJSON(ctx, key, &manifest); err != nil {
			// Skip invalid manifests
			continue
		}
		manifests = append(manifests, &manifest)
	}

	return manifests, nil
}

// PutSchema stores a schema
func (s *ManifestStorage) PutSchema(ctx context.Context, path string, schema interface{}) error {
	key := s.schemaKey(path)
	return s.helper.PutJSON(ctx, key, schema)
}

// GetSchema retrieves a schema
func (s *ManifestStorage) GetSchema(ctx context.Context, path string) (interface{}, error) {
	key := s.schemaKey(path)
	var schema interface{}
	if err := s.helper.GetJSON(ctx, key, &schema); err != nil {
		return nil, err
	}
	return schema, nil
}

// DeleteSchema removes a schema
func (s *ManifestStorage) DeleteSchema(ctx context.Context, path string) error {
	key := s.schemaKey(path)
	return s.helper.backend.Delete(ctx, key)
}

