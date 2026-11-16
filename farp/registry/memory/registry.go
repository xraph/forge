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
	watchers  map[string][]*watcher
	closed    bool
}

// NewRegistry creates a new in-memory registry.
func NewRegistry() *Registry {
	return &Registry{
		manifests: make(map[string]*farp.SchemaManifest),
		schemas:   make(map[string]any),
		watchers:  make(map[string][]*watcher),
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
//
// Events are delivered on a goroutine owned by the watch. Delivery never
// blocks the registry and never loses the latest state of an instance: when
// the callback falls behind, pending events for the same instance are
// coalesced so only the most recent one is delivered. The watch ends when ctx
// is cancelled or the registry is closed.
func (r *Registry) WatchManifests(ctx context.Context, serviceName string, onChange farp.ManifestChangeHandler) error {
	if onChange == nil {
		return errors.New("memory registry: onChange handler is required")
	}

	r.mu.Lock()

	if r.closed {
		r.mu.Unlock()

		return farp.ErrBackendUnavailable
	}

	w := newWatcher()
	r.watchers[serviceName] = append(r.watchers[serviceName], w)
	r.mu.Unlock()

	go func() {
		for {
			select {
			case <-ctx.Done():
				r.removeWatcher(serviceName, w)

				return
			case <-w.done:
				return
			case <-w.signal:
				for _, event := range w.drain() {
					onChange(event)
				}
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

	// Stop all watch goroutines
	for _, watchers := range r.watchers {
		for _, w := range watchers {
			w.stop()
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
	for _, w := range r.watchers[serviceName] {
		w.enqueue(event)
	}

	// Notify global watchers (empty service name)
	if serviceName != "" {
		for _, w := range r.watchers[""] {
			w.enqueue(event)
		}
	}
}

// removeWatcher removes a watcher and stops its goroutine.
func (r *Registry) removeWatcher(serviceName string, w *watcher) {
	r.mu.Lock()
	defer r.mu.Unlock()

	watchers := r.watchers[serviceName]
	for i, candidate := range watchers {
		if candidate == w {
			r.watchers[serviceName] = append(watchers[:i], watchers[i+1:]...)

			break
		}
	}

	w.stop()
}

// watcher buffers events for one WatchManifests subscription.
//
// The queue holds at most one pending event per instance ID, so its size is
// bounded by the number of instances while still preserving each instance's
// latest state. A slow consumer therefore sees coalesced updates instead of
// silently missing a removal or registration.
type watcher struct {
	mu      sync.Mutex
	order   []string
	pending map[string]*farp.ManifestEvent
	signal  chan struct{}
	done    chan struct{}
	once    sync.Once
}

func newWatcher() *watcher {
	return &watcher{
		pending: make(map[string]*farp.ManifestEvent),
		signal:  make(chan struct{}, 1),
		done:    make(chan struct{}),
	}
}

func (w *watcher) enqueue(event *farp.ManifestEvent) {
	if event == nil || event.Manifest == nil {
		return
	}

	id := event.Manifest.InstanceID

	w.mu.Lock()
	if _, ok := w.pending[id]; !ok {
		w.order = append(w.order, id)
	}

	w.pending[id] = event
	w.mu.Unlock()

	select {
	case w.signal <- struct{}{}:
	default:
		// A wake-up is already pending; the drain will pick this event up.
	}
}

func (w *watcher) drain() []*farp.ManifestEvent {
	w.mu.Lock()
	defer w.mu.Unlock()

	events := make([]*farp.ManifestEvent, 0, len(w.order))
	for _, id := range w.order {
		events = append(events, w.pending[id])
	}

	w.order = nil
	w.pending = make(map[string]*farp.ManifestEvent)

	return events
}

func (w *watcher) stop() {
	w.once.Do(func() { close(w.done) })
}

// Clear removes all manifests and schemas (useful for testing).
func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.manifests = make(map[string]*farp.SchemaManifest)
	r.schemas = make(map[string]any)
}
