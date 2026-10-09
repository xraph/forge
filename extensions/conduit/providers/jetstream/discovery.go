package jetstream

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	js "github.com/nats-io/nats.go/jetstream"
	"github.com/xraph/forge/extensions/conduit/core"
)

func (p *Provider) discoveryBucket(ctx context.Context, namespace string) (js.KeyValue, error) {
	client, err := p.client()
	if err != nil {
		return nil, err
	}

	return p.ensureBucket(ctx, client, js.KeyValueConfig{Bucket: "FC_DISC_" + hash(namespace), Storage: js.FileStorage, TTL: 45 * time.Second, Replicas: p.options.DeadLetterReplicas, History: 1})
}

// Register renews a lease for exactly one instance. Crashed members expire after 45 seconds.
func (p *Provider) Register(ctx context.Context, instance core.Instance) error {
	if err := instance.Validate(); err != nil {
		return err
	}

	bucket, err := p.discoveryBucket(ctx, instance.Identity.Namespace)
	if err != nil {
		return err
	}

	data, err := json.Marshal(instance)
	if err != nil {
		return err
	}

	_, err = bucket.Put(ctx, "i."+hash(instance.Identity.ServiceID)+"."+hash(instance.Identity.InstanceID), data)

	return err
}

// Deregister removes one instance, preserving other replicas of the service.
func (p *Provider) Deregister(ctx context.Context, identity core.Identity) error {
	bucket, err := p.discoveryBucket(ctx, identity.Namespace)
	if err != nil {
		return err
	}

	return bucket.Delete(ctx, "i."+hash(identity.ServiceID)+"."+hash(identity.InstanceID))
}

// Resolve returns ready members for a logical service inside a namespace.
func (p *Provider) Resolve(ctx context.Context, namespace, service string) ([]core.Instance, error) {
	instances, err := p.listInstances(ctx, namespace, "i."+hash(service)+".*")
	if err != nil {
		return nil, err
	}

	return slices.DeleteFunc(instances, func(instance core.Instance) bool { return !instance.Ready || instance.Identity.ServiceID != service }), nil
}

// List returns all leased instances inside the configured namespace.
func (p *Provider) List(ctx context.Context, namespace string) ([]core.Instance, error) {
	return p.listInstances(ctx, namespace, "i.>")
}

func (p *Provider) listInstances(ctx context.Context, namespace, filter string) ([]core.Instance, error) {
	bucket, err := p.discoveryBucket(ctx, namespace)
	if err != nil {
		return nil, err
	}

	lister, err := bucket.ListKeysFiltered(ctx, filter)
	if errors.Is(err, js.ErrNoKeysFound) {
		return []core.Instance{}, nil
	}

	if err != nil {
		return nil, err
	}

	defer func() { _ = lister.Stop() }()

	instances := make([]core.Instance, 0)

	for key := range lister.Keys() {
		entry, err := bucket.Get(ctx, key)
		if errors.Is(err, js.ErrKeyNotFound) {
			continue
		}

		if err != nil {
			return nil, err
		}

		var instance core.Instance
		if err := json.Unmarshal(entry.Value(), &instance); err != nil {
			return nil, err
		}

		if instance.Identity.Namespace == namespace {
			instances = append(instances, instance)
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	slices.SortFunc(instances, func(a, b core.Instance) int {
		return strings.Compare(a.Identity.ServiceID+"/"+a.Identity.InstanceID, b.Identity.ServiceID+"/"+b.Identity.InstanceID)
	})

	return instances, nil
}

var _ core.Registry = (*Provider)(nil)
var _ core.InstanceLister = (*Provider)(nil)
