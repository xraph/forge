package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/xraph/forge/extensions/conduit/core"
)

// Register writes or renews this process's lease. A write from an older process that
// still holds a lease on the same name is ignored, so a replacement keeps its record.
func (p *Provider) Register(ctx context.Context, instance core.Instance) error {
	if err := instance.Validate(); err != nil {
		return err
	}

	pool, err := p.db()
	if err != nil {
		return err
	}

	data, err := json.Marshal(instance)
	if err != nil {
		return err
	}

	_, err = pool.Exec(ctx, fmt.Sprintf(`INSERT INTO %s AS current (namespace, service_id, instance_id, record, owner, generation, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, clock_timestamp() + $7::bigint * interval '1 millisecond')
ON CONFLICT (namespace, service_id, instance_id) DO UPDATE
SET record = EXCLUDED.record, owner = EXCLUDED.owner, generation = EXCLUDED.generation, expires_at = EXCLUDED.expires_at
WHERE current.owner = EXCLUDED.owner OR current.generation <= EXCLUDED.generation OR current.expires_at <= clock_timestamp()`, p.t.instances),
		instance.Identity.Namespace, instance.Identity.ServiceID, instance.Identity.InstanceID, string(data), p.owner, p.generation, p.options.LeaseTTL.Milliseconds())

	return err
}

// Deregister removes the instance only if this process owns its lease.
func (p *Provider) Deregister(ctx context.Context, identity core.Identity) error {
	pool, err := p.db()
	if err != nil {
		return err
	}

	_, err = pool.Exec(ctx, fmt.Sprintf(`DELETE FROM %s WHERE namespace = $1 AND service_id = $2 AND instance_id = $3 AND owner = $4`, p.t.instances),
		identity.Namespace, identity.ServiceID, identity.InstanceID, p.owner)

	return err
}

// Resolve returns ready members of one service inside a namespace.
func (p *Provider) Resolve(ctx context.Context, namespace, service string) ([]core.Instance, error) {
	instances, err := p.instances(ctx, namespace, service)
	if err != nil {
		return nil, err
	}

	return slices.DeleteFunc(instances, func(i core.Instance) bool { return !i.Ready }), nil
}

// List returns every leased instance inside a namespace.
func (p *Provider) List(ctx context.Context, namespace string) ([]core.Instance, error) {
	return p.instances(ctx, namespace, "")
}

func (p *Provider) instances(ctx context.Context, namespace, service string) ([]core.Instance, error) {
	pool, err := p.db()
	if err != nil {
		return nil, err
	}

	rows, err := pool.Query(ctx, fmt.Sprintf(`SELECT record::text FROM %s
WHERE namespace = $1 AND ($2 = '' OR service_id = $2) AND expires_at > clock_timestamp() ORDER BY service_id, instance_id`, p.t.instances), namespace, service)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	instances := make([]core.Instance, 0)

	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}

		var instance core.Instance
		if err := json.Unmarshal(raw, &instance); err != nil {
			return nil, err
		}

		instances = append(instances, instance)
	}

	return instances, rows.Err()
}

var _ core.Registry = (*Provider)(nil)
var _ core.InstanceLister = (*Provider)(nil)
