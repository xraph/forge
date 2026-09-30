package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

// fakeDiscovery is a deterministic DiscoveryService. When localID is set it
// also reports the host's own service ID.
type fakeDiscovery struct {
	mu        sync.Mutex
	services  []string
	instances map[string][]*ServiceInstance
	localID   string
	listErr   error
}

func newFakeDiscovery() *fakeDiscovery {
	return &fakeDiscovery{instances: map[string][]*ServiceInstance{}}
}

func (f *fakeDiscovery) LocalServiceID() string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.localID
}

func (f *fakeDiscovery) set(service string, instances ...*ServiceInstance) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if !slices.Contains(f.services, service) {
		f.services = append(f.services, service)
	}

	f.instances[service] = instances
}

func (f *fakeDiscovery) ListServices(context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.listErr != nil {
		return nil, f.listErr
	}

	return slices.Clone(f.services), nil
}

func (f *fakeDiscovery) DiscoverWithTags(_ context.Context, service string, tags []string) ([]*ServiceInstance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var out []*ServiceInstance

	for _, inst := range f.instances[service] {
		if hasAll(inst.Tags, tags) {
			out = append(out, inst)
		}
	}

	return out, nil
}

func hasAll(have, want []string) bool {
	for _, w := range want {
		if !slices.Contains(have, w) {
			return false
		}
	}

	return true
}

// upstream serves a contract manifest for one contributor. The manifest can
// be swapped at runtime, and the last Authorization header is recorded.
type upstream struct {
	srv      *httptest.Server
	manifest atomic.Value // []byte
	lastAuth atomic.Value // string
}

func newUpstream(t *testing.T, contributor string, intents ...string) *upstream {
	t.Helper()

	u := &upstream{}
	u.setManifest(t, contributor, intents...)
	u.lastAuth.Store("")

	u.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_forge/contract/manifest" {
			http.NotFound(w, r)

			return
		}

		u.lastAuth.Store(r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(u.manifest.Load().([]byte))
	}))
	t.Cleanup(u.srv.Close)

	return u
}

func (u *upstream) setManifest(t *testing.T, contributor string, intents ...string) {
	t.Helper()

	m := contract.ContractManifest{
		SchemaVersion: 1,
		Contributor: contract.Contributor{
			Name:     contributor,
			Envelope: contract.EnvelopeSupport{Supports: []string{"v1"}, Preferred: "v1"},
		},
	}
	for _, name := range intents {
		m.Intents = append(m.Intents, contract.Intent{
			Name: name, Kind: contract.IntentKindQuery, Version: 1, Capability: contract.CapRead,
		})
	}

	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}

	u.manifest.Store(raw)
}

// instance describes this upstream as a healthy, tagged discovery instance.
func (u *upstream) instance(t *testing.T, id string) *ServiceInstance {
	t.Helper()

	parsed, err := url.Parse(u.srv.URL)
	if err != nil {
		t.Fatal(err)
	}

	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}

	return &ServiceInstance{
		ID: id, Name: "svc", Address: parsed.Hostname(), Port: port,
		Tags: []string{DefaultTag}, Status: "passing",
	}
}

// harness is an integration wired to a real contract registry.
type harness struct {
	reg   contract.Registry
	disc  *fakeDiscovery
	integ *Integration
}

func newHarness() *harness {
	h := &harness{reg: contract.NewRegistry(), disc: newFakeDiscovery()}
	h.integ = NewIntegration(h.disc, Config{
		Register:   h.reg.RegisterRemote,
		Unregister: h.reg.Unregister,
	})

	return h
}

func (h *harness) remote(t *testing.T, name string) contract.RemoteEndpoint {
	t.Helper()

	ep, ok := h.reg.Remote(name)
	if !ok {
		t.Fatalf("contributor %q is not registered as a remote; registered: %v", name, h.integ.Tracked())
	}

	return ep
}

func TestReconcile_RegistersTaggedHealthyInstance(t *testing.T) {
	h := newHarness()
	up := newUpstream(t, "reports", "reports.list")
	h.disc.set("reports-svc", up.instance(t, "reports-1"))

	h.integ.Reconcile(context.Background())

	if ep := h.remote(t, "reports"); ep.BaseURL != up.srv.URL {
		t.Errorf("endpoint = %q, want %q", ep.BaseURL, up.srv.URL)
	}

	if _, ok := h.reg.Intent("reports", "reports.list", 1); !ok {
		t.Error("reports.list was not registered from the fetched manifest")
	}
}

func TestReconcile_SkipsUnhealthyUntaggedAndLocal(t *testing.T) {
	h := newHarness()
	h.disc.localID = "self-1"

	sick := newUpstream(t, "sick", "sick.list")
	sickInst := sick.instance(t, "sick-1")
	sickInst.Status = "critical"

	plain := newUpstream(t, "plain", "plain.list")
	plainInst := plain.instance(t, "plain-1")
	plainInst.Tags = nil

	self := newUpstream(t, "self", "self.list")

	h.disc.set("sick-svc", sickInst)
	h.disc.set("plain-svc", plainInst)
	h.disc.set("self-svc", self.instance(t, "self-1"))

	h.integ.Start(context.Background())
	h.integ.Stop()

	if got := h.integ.Tracked(); len(got) != 0 {
		t.Errorf("tracked = %v, want none", got)
	}
}

func TestReconcile_RemovesDepartedInstance(t *testing.T) {
	h := newHarness()
	up := newUpstream(t, "reports", "reports.list")
	h.disc.set("reports-svc", up.instance(t, "reports-1"))
	h.integ.Reconcile(context.Background())
	h.remote(t, "reports")

	h.disc.set("reports-svc")
	h.integ.Reconcile(context.Background())

	if _, ok := h.reg.Contributor("reports"); ok {
		t.Error("reports is still registered after its only instance left discovery")
	}
}

func TestReconcile_ListFailureRemovesNothing(t *testing.T) {
	h := newHarness()
	up := newUpstream(t, "reports", "reports.list")
	h.disc.set("reports-svc", up.instance(t, "reports-1"))
	h.integ.Reconcile(context.Background())

	h.disc.listErr = errors.New("consul is down")
	h.integ.Reconcile(context.Background())

	h.remote(t, "reports")
}

func TestReconcile_PicksUpChangedManifest(t *testing.T) {
	h := newHarness()
	up := newUpstream(t, "reports", "reports.list")
	h.disc.set("reports-svc", up.instance(t, "reports-1"))
	h.integ.Reconcile(context.Background())

	up.setManifest(t, "reports", "reports.list", "reports.export")
	h.integ.Reconcile(context.Background())

	if _, ok := h.reg.Intent("reports", "reports.export", 1); !ok {
		t.Error("an intent added to the remote manifest never reached the registry")
	}
}

func TestReconcile_ReplicaTakesOverInSamePass(t *testing.T) {
	h := newHarness()
	a := newUpstream(t, "reports", "reports.list")
	b := newUpstream(t, "reports", "reports.list")
	h.disc.set("reports-svc", a.instance(t, "reports-a"), b.instance(t, "reports-b"))

	h.integ.Reconcile(context.Background())

	if got := h.integ.Tracked(); len(got) != 1 {
		t.Fatalf("tracked = %v, want one registration for two replicas", got)
	}

	if ep := h.remote(t, "reports"); ep.BaseURL != a.srv.URL {
		t.Fatalf("serving instance = %q, want the first by ID (%q)", ep.BaseURL, a.srv.URL)
	}

	// The serving replica leaves. The other must take over in this pass,
	// not leave the contributor missing until the next one.
	h.disc.set("reports-svc", b.instance(t, "reports-b"))
	h.integ.Reconcile(context.Background())

	if ep := h.remote(t, "reports"); ep.BaseURL != b.srv.URL {
		t.Errorf("serving instance = %q after failover, want %q", ep.BaseURL, b.srv.URL)
	}
}

func TestReconcile_SendsAPIKeyFromMetadata(t *testing.T) {
	h := newHarness()
	up := newUpstream(t, "reports", "reports.list")
	inst := up.instance(t, "reports-1")
	inst.Metadata = map[string]string{APIKeyMetadata: "s3cret"}
	h.disc.set("reports-svc", inst)

	h.integ.Reconcile(context.Background())

	if got := up.lastAuth.Load().(string); got != "Bearer s3cret" {
		t.Errorf("manifest fetch Authorization = %q, want the instance's key", got)
	}

	if ep := h.remote(t, "reports"); ep.APIKey != "s3cret" {
		t.Errorf("endpoint APIKey = %q, want it carried to forwarded envelopes", ep.APIKey)
	}
}
