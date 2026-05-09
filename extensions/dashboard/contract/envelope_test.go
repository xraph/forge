// envelope_test.go
package contract

import (
	"encoding/json"
	"testing"
)

func TestRequest_RoundTrip_Command(t *testing.T) {
	req := Request{
		Envelope:       "v1",
		Kind:           KindCommand,
		Contributor:    "users",
		Intent:         "user.disable",
		IntentVersion:  2,
		Payload:        json.RawMessage(`{"id":"u_42"}`),
		Params:         map[string]any{"tenant": "acme"},
		Context:        RequestContext{Route: "/admin/users", CorrelationID: "req_x"},
		CSRF:           "csrf_token",
		IdempotencyKey: "ik_1",
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Request
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Kind != KindCommand || got.IdempotencyKey != "ik_1" {
		t.Errorf("round trip lost data: %+v", got)
	}
}

func TestKind_Constants(t *testing.T) {
	for _, k := range []Kind{KindGraph, KindQuery, KindCommand, KindSubscribe} {
		if k == "" {
			t.Errorf("kind constant empty")
		}
	}
}
