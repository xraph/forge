package pilot

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/dashboard/collector"
	"github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"
	"github.com/xraph/forge/extensions/dashboard/contract/transport"
)

// graphEnv is the smallest setup that registers the slice-(c)/(h) pilot and
// exposes the transport handler so tests can POST kind=graph envelopes.
func graphEnv(t *testing.T) http.Handler {
	t.Helper()
	d := dispatcher.New(dispatcher.NoopMetricsEmitter{})
	reg := contract.NewRegistry()
	wreg := contract.NewWardenRegistry()
	deps := Deps{
		ExtensionsRegistry: newRegistryWith(t),
		Services:           &stubServices{list: []collector.ServiceInfo{}},
		Metrics:            &stubMetrics{data: &collector.MetricsData{}},
		MetricsInterval:    50 * time.Millisecond,
	}
	if err := Register(d, reg, wreg, deps); err != nil {
		t.Fatalf("pilot register: %v", err)
	}
	return transport.NewHandler(reg, wreg, d, contract.NoopAuditEmitter{})
}

func postGraph(t *testing.T, h http.Handler, route string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(contract.Request{
		Envelope:    "v1",
		Kind:        contract.KindGraph,
		Contributor: "core-contract",
		Intent:      "page.shell",
		Payload:     json.RawMessage(`{"route":"` + route + `"}`),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/dashboard/v1", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestGraphHandler_ReturnsGraphForExactRoute(t *testing.T) {
	h := graphEnv(t)
	w := postGraph(t, h, "/health")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body)
	}
	var resp contract.Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.OK {
		t.Fatalf("ok = false body=%s", w.Body)
	}
	var node contract.GraphNode
	if err := json.Unmarshal(resp.Data, &node); err != nil {
		t.Fatalf("data unmarshal: %v", err)
	}
	if node.Route != "/health" || node.Intent != "page.shell" {
		t.Errorf("unexpected node: route=%s intent=%s", node.Route, node.Intent)
	}
	if len(resp.Meta.RouteParams) != 0 {
		t.Errorf("expected empty params for exact match, got %v", resp.Meta.RouteParams)
	}
}

func TestGraphHandler_ParamRouteExtractsID(t *testing.T) {
	h := graphEnv(t)
	w := postGraph(t, h, "/traces/abc123")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body)
	}
	var resp contract.Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.OK {
		t.Fatalf("ok = false body=%s", w.Body)
	}
	var node contract.GraphNode
	if err := json.Unmarshal(resp.Data, &node); err != nil {
		t.Fatalf("data unmarshal: %v", err)
	}
	if node.Route != "/traces/:id" {
		t.Errorf("expected /traces/:id, got %q", node.Route)
	}
	if got := resp.Meta.RouteParams["id"]; got != "abc123" {
		t.Errorf("route params id = %q, want abc123 (full = %v)", got, resp.Meta.RouteParams)
	}
}

func TestGraphHandler_NotFoundForUnknownRoute(t *testing.T) {
	h := graphEnv(t)
	w := postGraph(t, h, "/no/such/route")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", w.Code, w.Body)
	}
	var er contract.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &er); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if er.OK || er.Error == nil || er.Error.Code != contract.CodeNotFound {
		t.Errorf("expected NOT_FOUND envelope, got %+v", er)
	}
}

func TestGraphHandler_PrefersExactOverParam(t *testing.T) {
	h := graphEnv(t)
	w := postGraph(t, h, "/traces")
	var resp contract.Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var node contract.GraphNode
	_ = json.Unmarshal(resp.Data, &node)
	if node.Route != "/traces" {
		t.Errorf("expected exact /traces, got %q", node.Route)
	}
	if len(resp.Meta.RouteParams) != 0 {
		t.Errorf("exact match should have no params, got %v", resp.Meta.RouteParams)
	}
}
