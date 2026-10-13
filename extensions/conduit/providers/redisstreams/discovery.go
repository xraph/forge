package redisstreams

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"github.com/xraph/forge/extensions/conduit/core"
)

type leaseRecord struct {
	Instance   core.Instance `json:"instance"`
	Owner      string        `json:"owner"`
	Generation int64         `json:"generation"`
}

// discoveryKeys share one hash tag per namespace, so the scripts below stay on one slot.
func discoveryKeys(namespace string) (prefix, set string) {
	tag := "fc:{" + hash("discovery", namespace) + "}"

	return tag + ":instance:", tag + ":instances"
}

// registerScript writes the lease unless a newer process owns it. Generations are Unix
// milliseconds, which cjson reads exactly.
const registerScript = `local current=redis.call('GET',KEYS[1])
local lease=current and cjson.decode(current)
if lease and lease.owner~=ARGV[2] and tonumber(lease.generation)>tonumber(ARGV[3]) then return 0 end
redis.call('SET',KEYS[1],ARGV[1],'PX',ARGV[4]); redis.call('SADD',KEYS[2],ARGV[5]); return 1`

const deregisterScript = `local current=redis.call('GET',KEYS[1]); if not current then return 0 end
if cjson.decode(current).owner~=ARGV[1] then return 0 end
redis.call('DEL',KEYS[1]); redis.call('SREM',KEYS[2],ARGV[2]); return 1`

// listScript reads every member and drops index entries whose lease expired. The keys it
// builds share KEYS[1]'s hash tag.
const listScript = `local out={}
for _,member in ipairs(redis.call('SMEMBERS',KEYS[1])) do
 local value=redis.call('GET',ARGV[1]..member)
 if value then table.insert(out,value) else redis.call('SREM',KEYS[1],member) end -- expired leases leave the index
end
return out`

// Register writes or renews this process's lease for one instance.
func (p *Provider) Register(ctx context.Context, instance core.Instance) error {
	if err := instance.Validate(); err != nil {
		return err
	}

	c, err := p.client()
	if err != nil {
		return err
	}

	data, err := json.Marshal(leaseRecord{Instance: instance, Owner: p.owner, Generation: p.generation})
	if err != nil {
		return err
	}

	prefix, set := discoveryKeys(instance.Identity.Namespace)
	member := hash(instance.Identity.ServiceID, instance.Identity.InstanceID)

	return c.Eval(ctx, registerScript, []string{prefix + member, set}, string(data), p.owner, p.generation, p.options.LeaseTTL.Milliseconds(), member).Err()
}

// Deregister removes the instance only if this process owns its lease.
func (p *Provider) Deregister(ctx context.Context, identity core.Identity) error {
	c, err := p.client()
	if err != nil {
		return err
	}

	prefix, set := discoveryKeys(identity.Namespace)
	member := hash(identity.ServiceID, identity.InstanceID)

	return c.Eval(ctx, deregisterScript, []string{prefix + member, set}, p.owner, member).Err()
}

// Resolve returns ready members of one service inside a namespace.
func (p *Provider) Resolve(ctx context.Context, namespace, service string) ([]core.Instance, error) {
	instances, err := p.List(ctx, namespace)
	if err != nil {
		return nil, err
	}

	return slices.DeleteFunc(instances, func(i core.Instance) bool { return !i.Ready || i.Identity.ServiceID != service }), nil
}

// List returns every leased instance inside a namespace.
func (p *Provider) List(ctx context.Context, namespace string) ([]core.Instance, error) {
	c, err := p.client()
	if err != nil {
		return nil, err
	}

	prefix, set := discoveryKeys(namespace)

	values, err := c.Eval(ctx, listScript, []string{set}, prefix).StringSlice()
	if err != nil {
		return nil, err
	}

	instances := make([]core.Instance, 0, len(values))

	for _, value := range values {
		var record leaseRecord
		if err := json.Unmarshal([]byte(value), &record); err != nil {
			return nil, err
		}

		if record.Instance.Identity.Namespace == namespace {
			instances = append(instances, record.Instance)
		}
	}

	slices.SortFunc(instances, func(a, b core.Instance) int {
		return strings.Compare(a.Identity.ServiceID+"/"+a.Identity.InstanceID, b.Identity.ServiceID+"/"+b.Identity.InstanceID)
	})

	return instances, nil
}

var _ core.Registry = (*Provider)(nil)
var _ core.InstanceLister = (*Provider)(nil)
