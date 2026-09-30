package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"
	"github.com/xraph/forge/extensions/dashboard/contract/loader"
	"github.com/xraph/forge/extensions/dashboard/contract/server"
	dashboarddiscovery "github.com/xraph/forge/extensions/dashboard/discovery"
)

// remoteReports stands up a separate service the way a remote contributor
// really runs: its own registry and dispatcher behind contract/server, with a
// reports.list handler that answers from that process.
func remoteReports(t *testing.T) *httptest.Server {
	t.Helper()

	reg := contract.NewRegistry()
	wreg := contract.NewWardenRegistry()
	src := `
schemaVersion: 1
contributor: { name: reports, envelope: { supports: [v1], preferred: v1 } }
intents:
  - { name: reports.list, kind: query, version: 1, capability: read }
`

	m, err := loader.Load(strings.NewReader(src), "reports.yaml")
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if err := loader.Validate(m, wreg); err != nil {
		t.Fatalf("validate: %v", err)
	}

	if err := reg.Register(m); err != nil {
		t.Fatalf("register: %v", err)
	}

	d := dispatcher.New(dispatcher.NoopMetricsEmitter{})

	type reports struct {
		Items []string `json:"items"`
	}

	if err := dispatcher.RegisterQuery(d, "reports", "reports.list", 1,
		func(context.Context, struct{}, contract.Principal) (reports, error) {
			return reports{Items: []string{"q3-revenue"}}, nil
		},
	); err != nil {
		t.Fatalf("register handler: %v", err)
	}

	srv := httptest.NewServer(server.New(reg, wreg, d, contract.NoopAuditEmitter{}))
	t.Cleanup(srv.Close)

	return srv
}

// dispatchReportsList posts a reports.list envelope to the dashboard's own
// contract endpoint, exactly as a browser would, and returns the response.
func dispatchReportsList(t *testing.T, e *Extension) (int, contract.Response, string) {
	t.Helper()

	body, _ := json.Marshal(contract.Request{
		Envelope: "v1", Kind: contract.KindQuery,
		Contributor: "reports", Intent: "reports.list", IntentVersion: 1,
	})
	req := httptest.NewRequest(http.MethodPost, "/dashboard/api/dashboard/v1", bytes.NewReader(body))
	w := httptest.NewRecorder()
	e.handleContractPOST()(w, req)

	var resp contract.Response
	_ = json.Unmarshal(w.Body.Bytes(), &resp)

	return w.Code, resp, w.Body.String()
}

func assertServedByRemote(t *testing.T, e *Extension) {
	t.Helper()

	code, resp, raw := dispatchReportsList(t, e)
	if code != http.StatusOK || !resp.OK {
		t.Fatalf("status = %d body = %s; want the remote to answer", code, raw)
	}

	if !strings.Contains(string(resp.Data), "q3-revenue") {
		t.Errorf("data = %s; want the remote handler's answer", resp.Data)
	}
}

func TestRemoteContractContributor_ExplicitRegistrationServesDispatch(t *testing.T) {
	upstream := remoteReports(t)
	e := newDiscoveryTestExt(t, nil)

	if err := e.RegisterRemoteContractContributor(context.Background(), upstream.URL, ""); err != nil {
		t.Fatalf("RegisterRemoteContractContributor: %v", err)
	}

	assertServedByRemote(t, e)

	if _, ok := capabilityStatus(t, e)["reports"]; !ok {
		t.Error("the remote contributor is missing from /capabilities")
	}
}

// discoveryStub is a one-service DiscoveryService whose instances a test can
// replace between passes.
type discoveryStub struct {
	mu        sync.Mutex
	instances []*dashboarddiscovery.ServiceInstance
}

func (d *discoveryStub) set(instances ...*dashboarddiscovery.ServiceInstance) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.instances = instances
}

func (d *discoveryStub) ListServices(context.Context) ([]string, error) {
	return []string{"reports-svc"}, nil
}

func (d *discoveryStub) DiscoverWithTags(context.Context, string, []string) ([]*dashboarddiscovery.ServiceInstance, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.instances, nil
}

func instanceFor(t *testing.T, srv *httptest.Server) *dashboarddiscovery.ServiceInstance {
	t.Helper()

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}

	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}

	return &dashboarddiscovery.ServiceInstance{
		ID: "reports-1", Name: "reports-svc", Address: u.Hostname(), Port: port,
		Tags: []string{dashboarddiscovery.DefaultTag}, Status: "passing",
	}
}

// TestRemoteContractContributor_DiscoveredServiceServesDispatch drives the
// whole discovery path through the dashboard's own entry points: a service
// appears in discovery, the dashboard answers envelopes for it by forwarding
// to that service, and once the service leaves, the dashboard stops.
func TestRemoteContractContributor_DiscoveredServiceServesDispatch(t *testing.T) {
	upstream := remoteReports(t)
	e := newDiscoveryTestExt(t, nil)
	e.config.EnableDiscovery = true

	disc := &discoveryStub{}
	disc.set(instanceFor(t, upstream))
	e.SetDiscoveryService(disc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	e.startDiscovery(ctx)
	defer e.discoveryInteg.Stop()

	// Start reconciles synchronously before its first tick, but in a
	// goroutine, so force a pass rather than race it.
	e.discoveryInteg.Reconcile(ctx)

	assertServedByRemote(t, e)

	disc.set()
	e.discoveryInteg.Reconcile(ctx)

	code, resp, raw := dispatchReportsList(t, e)
	if code == http.StatusOK && resp.OK {
		t.Fatalf("the dashboard still served reports after the service left discovery: %s", raw)
	}

	if !strings.Contains(raw, string(contract.CodeNotFound)) {
		t.Errorf("body = %s; want %s once the contributor is gone", raw, contract.CodeNotFound)
	}
}
