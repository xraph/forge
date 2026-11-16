package dashboard

import (
	"context"

	"github.com/xraph/forge"

	contractremote "github.com/xraph/forge/extensions/dashboard/contract/remote"
	dashboarddiscovery "github.com/xraph/forge/extensions/dashboard/discovery"
)

// transportPolicyDiscovery hides discovered instances the dashboard may not
// talk to under its remote transport policy, so the discovery integration
// never sends them anything.
//
// The policy has to run before the integration fetches a manifest, not when
// the fetched manifest is registered: the fetch is the first request to the
// instance, and by then it has already crossed the network. The integration
// checks the same policy itself before fetching; filtering here as well means
// an instance that fails it looks departed, so a contributor registered from
// it earlier is dropped rather than kept on stale terms.
//
// An instance is judged by the address the integration will actually use
// (ServiceInstance.BaseURL): https when it advertises it, plain http
// otherwise. It passes when that is https, a loopback address, or
// AllowInsecureRemotes is set.
type transportPolicyDiscovery struct {
	dashboarddiscovery.DiscoveryService

	allowInsecure bool
	logger        forge.Logger
}

// DiscoverWithTags returns the instances the transport policy allows.
func (d *transportPolicyDiscovery) DiscoverWithTags(ctx context.Context, serviceName string, tags []string) ([]*dashboarddiscovery.ServiceInstance, error) {
	instances, err := d.DiscoveryService.DiscoverWithTags(ctx, serviceName, tags)
	if err != nil {
		return nil, err
	}

	allowed := instances[:0:0]

	for _, inst := range instances {
		if inst == nil {
			continue
		}

		target := inst.BaseURL()
		if err := contractremote.CheckTransport(target, d.allowInsecure); err != nil {
			if d.logger != nil {
				d.logger.Warn("dashboard discovery: skipping contributor reachable only over plain http; advertise https or allow insecure remotes",
					forge.F("service_id", inst.ID),
					forge.F("url", target),
				)
			}

			continue
		}

		allowed = append(allowed, inst)
	}

	return allowed, nil
}

// LocalServiceID passes the wrapped service's own ID through, so wrapping it
// does not make the integration register the host as its own remote.
func (d *transportPolicyDiscovery) LocalServiceID() string {
	if p, ok := d.DiscoveryService.(dashboarddiscovery.LocalServiceIDProvider); ok {
		return p.LocalServiceID()
	}

	return ""
}
