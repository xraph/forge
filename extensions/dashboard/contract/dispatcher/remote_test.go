package dispatcher

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

// stubRemoteDispatcher captures every call so the test can assert routing
// decisions (local vs remote) without touching HTTP.
type stubRemoteDispatcher struct {
	called      int
	lastIntent  string
	respondWith json.RawMessage
	respondErr  error
}

func (s *stubRemoteDispatcher) Dispatch(_ context.Context, req contract.Request, _ contract.Principal) (json.RawMessage, contract.ResponseMeta, error) {
	s.called++
	s.lastIntent = req.Intent
	if s.respondErr != nil {
		return nil, contract.ResponseMeta{}, s.respondErr
	}
	return s.respondWith, contract.ResponseMeta{}, nil
}

func TestDispatch_PrefersLocalHandlerOverRemote(t *testing.T) {
	d := New(NoopMetricsEmitter{})
	if err := RegisterQuery(d, "x", "x.list", 1,
		func(_ context.Context, _ struct{}, _ contract.Principal) (struct{ V int }, error) {
			return struct{ V int }{V: 1}, nil
		},
	); err != nil {
		t.Fatalf("register: %v", err)
	}
	rem := &stubRemoteDispatcher{respondWith: json.RawMessage(`{"v":99}`)}
	d.SetRemoteDispatcher(rem)
	_, _, err := d.Dispatch(context.Background(), contract.Request{
		Kind: contract.KindQuery, Contributor: "x", Intent: "x.list", IntentVersion: 1,
	}, contract.Principal{})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if rem.called != 0 {
		t.Errorf("remote called %d times for a locally-handled intent", rem.called)
	}
}

func TestDispatch_FallsThroughToRemoteWhenLocalMissing(t *testing.T) {
	d := New(NoopMetricsEmitter{})
	rem := &stubRemoteDispatcher{respondWith: json.RawMessage(`{"ok":true}`)}
	d.SetRemoteDispatcher(rem)
	data, _, err := d.Dispatch(context.Background(), contract.Request{
		Kind: contract.KindQuery, Contributor: "x", Intent: "x.missing", IntentVersion: 1,
	}, contract.Principal{})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if rem.called != 1 {
		t.Errorf("remote should be called once, got %d", rem.called)
	}
	if string(data) != `{"ok":true}` {
		t.Errorf("data = %s", data)
	}
}

func TestDispatch_RemoteErrorIsSurfacedVerbatim(t *testing.T) {
	d := New(NoopMetricsEmitter{})
	d.SetRemoteDispatcher(&stubRemoteDispatcher{
		respondErr: &contract.Error{Code: contract.CodePermissionDenied, Message: "nope"},
	})
	_, _, err := d.Dispatch(context.Background(), contract.Request{
		Kind: contract.KindQuery, Contributor: "x", Intent: "x.thing", IntentVersion: 1,
	}, contract.Principal{})
	ce, ok := err.(*contract.Error)
	if !ok || ce.Code != contract.CodePermissionDenied {
		t.Errorf("expected CodePermissionDenied surfaced, got %v", err)
	}
}

func TestDispatch_NotFoundWhenNeitherLocalNorRemoteKnows(t *testing.T) {
	d := New(NoopMetricsEmitter{})
	// No SetRemoteDispatcher — expect the pre-slice-(m) behaviour.
	_, _, err := d.Dispatch(context.Background(), contract.Request{
		Kind: contract.KindQuery, Contributor: "x", Intent: "x.missing", IntentVersion: 1,
	}, contract.Principal{})
	ce, ok := err.(*contract.Error)
	if !ok || ce.Code != contract.CodeNotFound {
		t.Errorf("expected CodeNotFound, got %v", err)
	}
}
