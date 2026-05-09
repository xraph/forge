// envelope_test.go
package contract

import (
	"bytes"
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

func TestErrorResponse_RoundTrip(t *testing.T) {
	er := ErrorResponse{
		OK:       false,
		Envelope: "v1",
		Error: &Error{
			Code:          CodePermissionDenied,
			Message:       "denied",
			CorrelationID: "c1",
			Redactions:    []string{"users[*].email"},
		},
	}
	b, err := json.Marshal(er)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !bytes.Contains(b, []byte(`"code":"PERMISSION_DENIED"`)) {
		t.Errorf("marshaled form missing code: %s", b)
	}
	var got ErrorResponse
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Error.Code != CodePermissionDenied {
		t.Errorf("round trip lost code")
	}
}

func TestStreamEvent_RoundTrip_AllModes(t *testing.T) {
	for _, mode := range []SubscriptionMode{ModeReplace, ModeAppend, ModeSnapshotDelta} {
		ev := StreamEvent{Intent: "audit.tail", Mode: mode, Payload: json.RawMessage(`{"a":1}`), Seq: 42}
		b, _ := json.Marshal(ev)
		var got StreamEvent
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("mode %s: %v", mode, err)
		}
		if got.Mode != mode || got.Seq != 42 {
			t.Errorf("mode %s round trip lost data: %+v", mode, got)
		}
	}
}
