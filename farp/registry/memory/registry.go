package memory

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/farp"
)

// Registry is an in-memory implementation of SchemaRegistry
// Useful for testing and development.
type Registry struct {
	mu        sync.RWMutex
	manifests map[string]*farp.SchemaManifest // key: instanceID
	schemas   map[string]any                  // key: registry path
	watchers  map[string][]chan *farp.ManifestEvent
	closed    bool
}

// NewRegistry creates a new in-memory registry.
func NewRegistry() *Registry {
	return &Registry{
		manifests: make(map[string]*farp.SchemaManifest),
		schemas:   make(map[string]any),
		watchers:  make(map[string][]chan *farp.ManifestEvent),
	}
}

// RegisterManifest registers a new schema manifest.
func (r *Registry) RegisterManifest(ctx context.Context, manifest *farp.SchemaManifest) error {
	if err := manifest.Validate(); err != nil {
		return fmt.Errorf("invalid manifest: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return farp.ErrBackendUnavailable
	}

	r.manifests[manifest.InstanceID] = manifest.Clone()

	// Notify watchers
	r.notifyWatchers(manifest.ServiceName, &farp.ManifestEvent{
		Type:      farp.EventTypeAdded,
		Manifest:  manifest,
		Timestamp: time.Now().Unix(),
	})

	return nil
}

// GetManifest retrieves a schema manifest by instance ID.
func (r *Registry) GetManifest(ctx context.Context, instanceID string) (*farp.SchemaManifest, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	manifest, ok := r.manifests[instanceID]
	if !ok {
		return nil, farp.ErrManifestNotFound
	}

	return manifest.Clone(), nil
}

// UpdateManifest updates an existing schema manifest.
func (r *Registry) UpdateManifest(ctx context.Context, manifest *farp.SchemaManifest) error {
	if err := manifest.Validate(); err != nil {
		return fmt.Errorf("invalid manifest: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return farp.ErrBackendUnavailable
	}

	if _, ok := r.manifests[manifest.InstanceID]; !ok {
		return farp.ErrManifestNotFound
	}

	r.manifests[manifest.InstanceID] = manifest.Clone()

	// Notify watchers
	r.notifyWatchers(manifest.ServiceName, &farp.ManifestEvent{
		Type:      farp.EventTypeUpdated,
		Manifest:  manifest,
		Timestamp: time.Now().Unix(),
	})

	return nil
}

// DeleteManifest removes a schema manifest.
func (r *Registry) DeleteManifest(ctx context.Context, instanceID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return farp.ErrBackendUnavailable
	}

	manifest, ok := r.manifests[instanceID]
	if !ok {
		return farp.ErrManifestNotFound
	}

	delete(r.manifests, instanceID)

	// Notify watchers
	r.notifyWatchers(manifest.ServiceName, &farp.ManifestEvent{
		Type:      farp.EventTypeRemoved,
		Manifest:  manifest,
		Timestamp: time.Now().Unix(),
	})

	return nil
}

// ListManifests lists all manifests for a service name.
func (r *Registry) ListManifests(ctx context.Context, serviceName string) ([]*farp.SchemaManifest, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var manifests []*farp.SchemaManifest

	for _, manifest := range r.manifests {
		if serviceName == "" || manifest.ServiceName == serviceName {
			manifests = append(manifests, manifest.Clone())
		}
	}

	return manifests, nil
}

// PublishSchema stores a schema in the registry.
func (r *Registry) PublishSchema(ctx context.Context, path string, schema any) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return farp.ErrBackendUnavailable
	}

	r.schemas[path] = schema

	return nil
}

// FetchSchema retrieves a schema from the registry.
func (r *Registry) FetchSchema(ctx context.Context, path string) (any, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	schema, ok := r.schemas[path]
	if !ok {
		return nil, farp.ErrSchemaNotFound
	}

	return schema, nil
}

// DeleteSchema removes a schema from the registry.
func (r *Registry) DeleteSchema(ctx context.Context, path string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return farp.ErrBackendUnavailable
	}

	delete(r.schemas, path)

	return nil
}

// WatchManifests watches for manifest changes for a service.
func (r *Registry) WatchManifests(ctx context.Context, serviceName string, onChange farp.ManifestChangeHandler) error {
	r.mu.Lock()

	if r.closed {
		r.mu.Unlock()

		return farp.ErrBackendUnavailable
	}

	eventChan := make(chan *farp.ManifestEvent, 10)
	r.watchers[serviceName] = append(r.watchers[serviceName], eventChan)
	r.mu.Unlock()

	// Watch for events until context is cancelled
	go func() {
		for {
			select {
			case <-ctx.Done():
				r.removeWatcher(serviceName, eventChan)

				return
			case event := <-eventChan:
				onChange(event)
			}
		}
	}()

	return nil
}

// WatchSchemas watches for schema changes at a specific path.
func (r *Registry) WatchSchemas(ctx context.Context, path string, onChange farp.SchemaChangeHandler) error {
	// For memory registry, schema watches are not implemented
	// This would require tracking individual schema paths
	return errors.New("schema watching not supported in memory registry")
}

// Close closes the registry and cleans up resources.
func (r *Registry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil
	}

	r.closed = true

	// Close all watcher channels
	for _, watchers := range r.watchers {
		for _, ch := range watchers {
			close(ch)
		}
	}

	r.watchers = nil

	return nil
}

// Health checks if the registry is healthy.
func (r *Registry) Health(ctx context.Context) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.closed {
		return farp.ErrBackendUnavailable
	}

	return nil
}

// notifyWatchers notifies all watchers for a service (must be called with lock held).
func (r *Registry) notifyWatchers(serviceName string, event *farp.ManifestEvent) {
	// Notify specific service watchers
	if watchers, ok := r.watchers[serviceName]; ok {
		for _, ch := range watchers {
			select {
			case ch <- event:
			default:
				// Channel full, skip
			}
		}
	}

	// Notify global watchers (empty service name)
	if watchers, ok := r.watchers[""]; ok {
		for _, ch := range watchers {
			select {
			case ch <- event:
			default:
				// Channel full, skip
			}
		}
	}
}

// removeWatcher removes a watcher channel.
func (r *Registry) removeWatcher(serviceName string, eventChan chan *farp.ManifestEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()

	watchers := r.watchers[serviceName]
	for i, ch := range watchers {
		if ch == eventChan {
			r.watchers[serviceName] = append(watchers[:i], watchers[i+1:]...)

			close(eventChan)

			break
		}
	}
}

// Clear removes all manifests and schemas (useful for testing).
func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.manifests = make(map[string]*farp.SchemaManifest)
	r.schemas = make(map[string]any)
}
