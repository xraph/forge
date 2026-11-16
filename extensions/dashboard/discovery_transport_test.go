package dashboard

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/forge"

	"github.com/xraph/forge/extensions/dashboard/contract"
	dashboarddiscovery "github.com/xraph/forge/extensions/dashboard/discovery"
)

type staticDiscovery struct {
	instances []*dashboarddiscovery.ServiceInstance
	localID   string
	asked     int
}

func (s *staticDiscovery) ListServices(context.Context) ([]string, error) {
	return []string{"svc"}, nil
}

func (s *staticDiscovery) DiscoverWithTags(context.Context, string, []string) ([]*dashboarddiscovery.ServiceInstance, error) {
	s.asked++

	return s.instances, nil
}

func (s *staticDiscovery) LocalServiceID() string { return s.localID }

func instanceAt(id, addr string) *dashboarddiscovery.ServiceInstance {
	return &dashboarddiscovery.ServiceInstance{
		ID: id, Name: "svc", Address: addr, Port: 65535,
		Tags:     []string{dashboarddiscovery.DefaultTag},
		Metadata: map[string]string{dashboarddiscovery.APIKeyMetadata: "s3cret"},
		Status:   "passing",
	}
}

func httpsInstanceAt(id, addr string) *dashboarddiscovery.ServiceInstance {
	inst := instanceAt(id, addr)
	inst.Metadata[dashboarddiscovery.SchemeMetadata] = "https"

	return inst
}

func ids(instances []*dashboarddiscovery.ServiceInstance) []string {
	out := make([]string, 0, len(instances))
	for _, inst := range instances {
		out = append(out, inst.ID)
	}

	return out
}

// Regression: discovery fetched every instance's manifest over plain http,
// with its API key, before the dashboard's transport policy ran at
// registration. The policy now hides such instances from the integration,
// so no request is ever made to them.
func TestTransportPolicyDiscovery_FiltersBeforeAnyFetch(t *testing.T) {
	src := &staticDiscovery{
		instances: []*dashboarddiscovery.ServiceInstance{
			instanceAt("remote", "10.0.0.5"),
			instanceAt("named", "svc.internal"),
			instanceAt("loopback", "127.0.0.1"),
			httpsInstanceAt("secure", "svc.internal"),
		},
		localID: "self",
	}

	strict := &transportPolicyDiscovery{DiscoveryService: src, logger: forge.NewNoopLogger()}

	got, err := strict.DiscoverWithTags(context.Background(), "svc", nil)
	if err != nil {
		t.Fatal(err)
	}

	// An instance on another host that advertises https is reached over
	// it, so the strict policy keeps it.
	if ids := ids(got); len(ids) != 2 || ids[0] != "loopback" || ids[1] != "secure" {
		t.Errorf("strict policy passed %v, want loopback and secure", ids)
	}

	if strict.LocalServiceID() != "self" {
		t.Error("wrapper hid the host's own service ID")
	}

	lax := &transportPolicyDiscovery{DiscoveryService: src, allowInsecure: true}

	got, err = lax.DiscoverWithTags(context.Background(), "svc", nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 4 {
		t.Errorf("AllowInsecureRemotes passed %v, want all four", ids(got))
	}

	// The source slice is not modified by filtering.
	if len(src.instances) != 4 || src.instances[0].ID != "remote" {
		t.Errorf("source instances changed: %v", ids(src.instances))
	}
}

// End to end through the integration: a non-loopback http instance is never
// registered and never contacted.
func TestTransportPolicyDiscovery_IntegrationNeverRegistersInsecureRemote(t *testing.T) {
	src := &staticDiscovery{instances: []*dashboarddiscovery.ServiceInstance{instanceAt("remote", "10.0.0.5")}}

	registered := 0
	integ := dashboarddiscovery.NewIntegration(&transportPolicyDiscovery{DiscoveryService: src}, dashboarddiscovery.Config{
		Register: func(*contract.ContractManifest, contract.RemoteEndpoint) error {
			registered++

			return nil
		},
		Unregister: func(string) {},
	})

	// 10.0.0.5:65535 would make a real fetch hang for the client timeout; a
	// quick return is part of the evidence that no fetch happened.
	start := time.Now()

	integ.Reconcile(context.Background())

	if src.asked == 0 {
		t.Fatal("integration never consulted discovery")
	}

	if registered != 0 || len(integ.Tracked()) != 0 {
		t.Errorf("insecure remote registered: %d, tracked %v", registered, integ.Tracked())
	}

	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("reconcile took %v; it looks like a fetch was attempted", d)
	}
}
