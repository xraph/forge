// stream_test.go
package transport

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

type stubSource struct {
	events chan contract.StreamEvent
}

func (s *stubSource) Subscribe(_ context.Context, _ contract.Principal, _ string, _ contract.Intent, _ map[string]contract.ParamSource) (<-chan contract.StreamEvent, func(), error) {
	stop := func() { close(s.events) }
	return s.events, stop, nil
}

func registerSubscriptionFixture(t *testing.T, reg contract.Registry) {
	t.Helper()
	src := `
schemaVersion: 1
contributor: { name: feeds, envelope: { supports: [v1], preferred: v1 } }
intents:
  - { name: audit.tail, kind: subscription, version: 1, capability: read, mode: append }
`
	var feedsM contract.ContractManifest
	if err := contract.UnmarshalManifestForTest([]byte(src), &feedsM); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(&feedsM); err != nil {
		t.Fatal(err)
	}
}

func TestStream_ControlSubscribeAndDeliver(t *testing.T) {
	reg, wreg := setupRegistry(t)
	registerSubscriptionFixture(t, reg)

	source := &stubSource{events: make(chan contract.StreamEvent, 4)}
	broker := NewStreamBroker(reg, wreg, source)

	// Open the stream with a cancellable context so we can tear it down
	// cleanly before reading the recorder body (avoids data race on body).
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	streamReq := httptest.NewRequest(http.MethodGet, "/api/dashboard/v1/stream", nil).WithContext(ctx)
	streamW := httptest.NewRecorder()
	streamDone := make(chan struct{})
	go func() {
		broker.ServeStream(streamW, streamReq)
		close(streamDone)
	}()
	time.Sleep(20 * time.Millisecond) // allow registration to land

	ids := broker.SnapshotIDs()
	if len(ids) == 0 {
		t.Fatalf("no active streams")
	}

	// Subscribe via control
	cmd, _ := json.Marshal(ControlMessage{
		StreamID: ids[0],
		Op:       "subscribe", Contributor: "feeds", Intent: "audit.tail", SubscriptionID: "s1",
	})
	ctlReq := httptest.NewRequest(http.MethodPost, "/api/dashboard/v1/stream/control", bytes.NewReader(cmd))
	ctlW := httptest.NewRecorder()
	broker.ServeControl(ctlW, ctlReq)
	if ctlW.Code != http.StatusOK {
		t.Fatalf("control status = %d body=%s", ctlW.Code, ctlW.Body)
	}

	// Push an event
	source.events <- contract.StreamEvent{Intent: "audit.tail", Mode: contract.ModeAppend, Payload: json.RawMessage(`{"line":"hi"}`), Seq: 1}
	time.Sleep(20 * time.Millisecond)

	// Tear down the stream so the broker goroutine stops writing to streamW.
	// ServeStream's deferred cancel will run all subscription cancels, which
	// closes the source channel and exits the per-event goroutine.
	cancel()
	<-streamDone

	// Validate the SSE body contains the event
	body := streamW.Body.String()
	if !strings.Contains(body, `"intent":"audit.tail"`) || !strings.Contains(body, `"line":"hi"`) {
		t.Errorf("stream did not deliver event: %s", body)
	}
	// And the event header carries the subscription ID
	scanner := bufio.NewScanner(strings.NewReader(body))
	hasEventID := false
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "event: s1") {
			hasEventID = true
			break
		}
	}
	if !hasEventID {
		t.Error("expected event: s1 line in stream")
	}
}

func TestStream_ModeMismatch_DeliversAndLogs(t *testing.T) {
	reg, wreg := setupRegistry(t)
	registerSubscriptionFixture(t, reg)

	source := &stubSource{events: make(chan contract.StreamEvent, 4)}
	broker := NewStreamBroker(reg, wreg, source)

	// Capture log output
	var logBuf bytes.Buffer
	origOut := log.Writer()
	origFlags := log.Flags()
	log.SetOutput(&logBuf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(origOut)
		log.SetFlags(origFlags)
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	streamReq := httptest.NewRequest(http.MethodGet, "/api/dashboard/v1/stream", nil).WithContext(ctx)
	streamW := httptest.NewRecorder()
	streamDone := make(chan struct{})
	go func() {
		broker.ServeStream(streamW, streamReq)
		close(streamDone)
	}()
	time.Sleep(20 * time.Millisecond)

	ids := broker.SnapshotIDs()
	if len(ids) == 0 {
		t.Fatalf("no active streams")
	}

	cmd, _ := json.Marshal(ControlMessage{
		StreamID: ids[0],
		Op:       "subscribe", Contributor: "feeds", Intent: "audit.tail", SubscriptionID: "s2",
	})
	ctlReq := httptest.NewRequest(http.MethodPost, "/api/dashboard/v1/stream/control", bytes.NewReader(cmd))
	ctlW := httptest.NewRecorder()
	broker.ServeControl(ctlW, ctlReq)
	if ctlW.Code != http.StatusOK {
		t.Fatalf("control status = %d body=%s", ctlW.Code, ctlW.Body)
	}

	// Intent declares mode=append; emit an event with mode=replace
	source.events <- contract.StreamEvent{Intent: "audit.tail", Mode: contract.ModeReplace, Payload: json.RawMessage(`{"snapshot":true}`), Seq: 1}
	time.Sleep(20 * time.Millisecond)

	// Tear down before reading the recorder body
	cancel()
	<-streamDone

	body := streamW.Body.String()
	if !strings.Contains(body, `"snapshot":true`) {
		t.Errorf("event not delivered despite mode mismatch: %s", body)
	}
	logged := logBuf.String()
	if !strings.Contains(logged, "mode mismatch") {
		t.Errorf("expected mode-mismatch warning in log output, got: %q", logged)
	}
}
