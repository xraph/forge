package memory

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/xraph/forge/extensions/conduit/core"
)

type lease struct {
	instance core.Instance
	expires  time.Time
}

func instanceKey(identity core.Identity) string {
	return identity.Namespace + "\x00" + identity.ServiceID + "\x00" + identity.InstanceID
}

// Register records or renews one instance's lease.
func (b *Broker) Register(ctx context.Context, instance core.Instance) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := instance.Validate(); err != nil {
		return err
	}

	instance.Endpoints = slices.Clone(instance.Endpoints)

	b.mu.Lock()
	defer b.mu.Unlock()

	b.instances[instanceKey(instance.Identity)] = lease{instance: instance, expires: time.Now().Add(b.leaseTTL)}

	return nil
}

// Deregister removes one instance and leaves other replicas listed.
func (b *Broker) Deregister(ctx context.Context, identity core.Identity) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	delete(b.instances, instanceKey(identity))

	return nil
}

// Resolve returns ready members of one service inside a namespace.
func (b *Broker) Resolve(ctx context.Context, namespace, service string) ([]core.Instance, error) {
	instances, err := b.List(ctx, namespace)
	if err != nil {
		return nil, err
	}

	return slices.DeleteFunc(instances, func(i core.Instance) bool { return !i.Ready || i.Identity.ServiceID != service }), nil
}

// List returns every leased instance inside a namespace.
func (b *Broker) List(ctx context.Context, namespace string) ([]core.Instance, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	instances := make([]core.Instance, 0)

	for key, l := range b.instances {
		if !l.expires.After(now) {
			delete(b.instances, key)

			continue
		}

		if l.instance.Identity.Namespace == namespace {
			copied := l.instance
			copied.Endpoints = slices.Clone(copied.Endpoints)
			instances = append(instances, copied)
		}
	}

	slices.SortFunc(instances, func(a, b core.Instance) int {
		return strings.Compare(a.Identity.ServiceID+"/"+a.Identity.InstanceID, b.Identity.ServiceID+"/"+b.Identity.InstanceID)
	})

	return instances, nil
}

var _ core.Registry = (*Broker)(nil)
var _ core.InstanceLister = (*Broker)(nil)
