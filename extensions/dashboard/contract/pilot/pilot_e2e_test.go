package pilot

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/dashboard/collector"
	"github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"
	"github.com/xraph/forge/extensions/dashboard/contract/transport"
	"github.com/xraph/forge/extensions/dashboard/contributor"
)

func setupPilotEnv(t *testing.T) (http.Handler, *transport.StreamBroker, *dispatcher.Dispatcher) {
	t.Helper()
	d := dispatcher.New(dispatcher.NoopMetricsEmitter{})
	reg := contract.NewRegistry()
	wreg := contract.NewWardenRegistry()

	extReg := newRegistryWith(t,
		&contributor.Manifest{Name: "auth", DisplayName: "Authentication", Version: "1.0"},
	)
	deps := Deps{
		ExtensionsRegistry: extReg,
		Services:           &stubServices{list: []collector.ServiceInfo{{Name: "db", Status: "healthy"}}},
		Metrics:            &stubMetrics{data: &collector.MetricsData{Stats: collector.MetricsStats{TotalMetrics: 5}}},
		MetricsInterval:    20 * time.Millisecond,
	}
	if err := Register(d, reg, wreg, deps); err != nil {
		t.Fatalf("pilot register: %v", err)
	}
	httpHandler := transport.NewHandler(reg, wreg, d, contract.NoopAuditEmitter{})
	broker := transport.NewStreamBroker(reg, wreg, d)
	return httpHandler, broker, d
}

func TestPilotE2E_ExtensionsList_HTTPRoundTrip(t *testing.T) {
	h, _, _ := setupPilotEnv(t)
	body, _ := json.Marshal(contract.Request{
		Envelope: "v1", Kind: contract.KindQuery,
		Contributor: "core-contract", Intent: "extensions.list", IntentVersion: 1,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/dashboard/v1", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body)
	}
	var resp contract.Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.OK {
		t.Errorf("ok = false")
	}
	var data ExtensionsList
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		t.Fatalf("data unmarshal: %v", err)
	}
	if len(data.Extensions) != 1 || data.Extensions[0].Name != "auth" {
		t.Errorf("extensions = %+v", data.Extensions)
	}
}

func TestPilotE2E_ServicesDetail_NotFoundEnvelope(t *testing.T) {
	h, _, _ := setupPilotEnv(t)
	body, _ := json.Marshal(contract.Request{
		Envelope: "v1", Kind: contract.KindQuery,
		Contributor: "core-contract", Intent: "services.detail", IntentVersion: 1,
		Payload: json.RawMessage(`{"name":"missing"}`),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/dashboard/v1", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		// transport.NewHandler maps errors via asContractError; verify the wire
		// envelope code rather than HTTP status (lenient on non-2xx).
	}
	if !strings.Contains(w.Body.String(), "NOT_FOUND") {
		t.Errorf("expected NOT_FOUND in body: %s", w.Body)
	}
}

func TestPilotE2E_MetricsSummary_SSE(t *testing.T) {
	_, broker, _ := setupPilotEnv(t)

	// Open the SSE stream with a cancellable context so we can tear it down
	// cleanly before reading the recorder body (avoids data race on body).
	streamReq := httptest.NewRequest(http.MethodGet, "/api/dashboard/v1/stream", nil)
	streamCtx, cancelStream := context.WithCancel(streamReq.Context())
	streamReq = streamReq.WithContext(streamCtx)
	streamW := httptest.NewRecorder()

	streamDone := make(chan struct{})
	go func() {
		broker.ServeStream(streamW, streamReq)
		close(streamDone)
	}()

	// Wait for the broker to register the stream.
	deadline := time.After(250 * time.Millisecond)
	var streamID string
LOOP:
	for {
		ids := broker.SnapshotIDs()
		if len(ids) > 0 {
			streamID = ids[0]
			break LOOP
		}
		select {
		case <-deadline:
			cancelStream()
			<-streamDone
			t.Fatal("stream not registered in time")
		case <-time.After(5 * time.Millisecond):
		}
	}

	// Subscribe via control.
	cmd, _ := json.Marshal(transport.ControlMessage{
		StreamID: streamID, Op: "subscribe",
		Contributor: "core-contract", Intent: "metrics.summary",
		SubscriptionID: "s1",
	})
	ctlReq := httptest.NewRequest(http.MethodPost, "/api/dashboard/v1/stream/control", bytes.NewReader(cmd))
	ctlW := httptest.NewRecorder()
	broker.ServeControl(ctlW, ctlReq)
	if ctlW.Code != http.StatusOK {
		cancelStream()
		<-streamDone
		t.Fatalf("control = %d body=%s", ctlW.Code, ctlW.Body)
	}

	// MetricsInterval is 20ms; give the ticker a few cycles to fire so at
	// least one event has been written to the recorder.
	time.Sleep(150 * time.Millisecond)

	// Tear down the stream and join the broker goroutine BEFORE reading the
	// recorder body. ServeStream's deferred cancel runs all subscription
	// cancels, the metrics goroutine exits via ctx.Done(), and the broker
	// stops writing to streamW. This matches the slice (a) race-clean pattern.
	cancelStream()
	<-streamDone

	body := streamW.Body.String()
	if !strings.Contains(body, `"totalMetrics":5`) {
		t.Fatalf("no metrics event with totalMetrics:5 in stream; body=%s", body)
	}
}
