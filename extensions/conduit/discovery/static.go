// Package discovery adapts instance registries to Conduit service clients.
package discovery

import (
	"context"
	"slices"
	"strings"
	"sync"

	"github.com/xraph/forge/extensions/conduit/core"
)

// Static is an explicitly configured registry for development and fixed deployments.
type Static struct {
	mu        sync.RWMutex
	instances map[core.Identity]core.Instance
}

// NewStatic builds a registry without network discovery.
func NewStatic(instances ...core.Instance) *Static {
	r := &Static{instances: make(map[core.Identity]core.Instance)}
	for _, instance := range instances {
		instance.Endpoints = slices.Clone(instance.Endpoints)
		r.instances[instance.Identity] = instance
	}

	return r
}

// Register replaces the record for this exact instance, leaving its peers intact.
func (r *Static) Register(_ context.Context, instance core.Instance) error {
	if err := instance.Validate(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	instance.Endpoints = slices.Clone(instance.Endpoints)
	r.instances[instance.Identity] = instance

	return nil
}

// Deregister removes only the stopping instance.
func (r *Static) Deregister(_ context.Context, identity core.Identity) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.instances, identity)

	return nil
}

// Resolve returns ready replicas for one service in one namespace.
func (r *Static) Resolve(ctx context.Context, namespace, service string) ([]core.Instance, error) {
	instances, err := r.List(ctx, namespace)
	if err != nil {
		return nil, err
	}

	result := make([]core.Instance, 0)

	for _, instance := range instances {
		if instance.Ready && instance.Identity.ServiceID == service {
			result = append(result, instance)
		}
	}

	return result, nil
}

// List returns a namespace snapshot including unhealthy and draining members.
func (r *Static) List(_ context.Context, namespace string) ([]core.Instance, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]core.Instance, 0)

	for _, instance := range r.instances {
		if instance.Identity.Namespace == namespace {
			instance.Endpoints = slices.Clone(instance.Endpoints)
			result = append(result, instance)
		}
	}

	slices.SortFunc(result, func(a, b core.Instance) int {
		return strings.Compare(a.Identity.ServiceID+"/"+a.Identity.InstanceID, b.Identity.ServiceID+"/"+b.Identity.InstanceID)
	})

	return result, nil
}
