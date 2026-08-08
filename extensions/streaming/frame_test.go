package streaming_test

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/xraph/forge/extensions/streaming"
)

// The Go half of the wire-contract proof.
//
// packages/client-core/__tests__/streaming.test.ts is the other half: it drives
// the same envelope through the real StreamBinder and asserts what the bytes
// do. This half asserts the bytes. Both are required, and for the reason the
// defect they pin demonstrates -- each side had a suite, each suite tested that
// side against its own idea of the envelope, and a frame neither would accept
// from the other passed both.

// TestEventMessageWireShape pins the field names and values the client decodes.
//
// Asserted through the marshalled JSON rather than the struct, because the
// struct tags are the contract and a rename that kept the field names would be
// invisible to an assertion on the fields.
func TestEventMessageWireShape(t *testing.T) {
	msg := streaming.NewEventMessage("order.created", map[string]any{"id": 9})
	msg.ID = "msg-1"
	msg.ChannelID = "orders"
	msg.UserID = "u-1"

	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// The two names, and which is which. Reversing them is the defect.
	if got := wire["event"]; got != "order.created" {
		t.Errorf("event = %v, want the domain name order.created", got)
	}

	if got := wire["type"]; got != streaming.MessageTypeMessage {
		t.Errorf("type = %v, want the transport kind %q", got, streaming.MessageTypeMessage)
	}

	// The payload field. The client reads `data`; `payload` is the other shape
	// in circulation and this envelope must not be spelling it.
	data, ok := wire["data"].(map[string]any)
	if !ok {
		t.Fatalf("data = %v, want the payload object", wire["data"])
	}

	if data["id"] != float64(9) {
		t.Errorf("data.id = %v, want 9", data["id"])
	}

	if _, exists := wire["payload"]; exists {
		t.Error("envelope carries a payload field; the client reads data")
	}

	// The channel, in the spelling the decoder looks for. Note that this is a
	// logical subscription id and not the endpoint path a manifest binding is
	// keyed on, which is why the client does not surface it as the frame's
	// channel unless an application supplies a mapping.
	if got := wire["channel_id"]; got != "orders" {
		t.Errorf("channel_id = %v, want orders", got)
	}
}

// TestTransportKindsMirrorTheConstants fails when a kind is added to this
// package and not to TRANSPORT_KINDS in packages/client-core/src/streaming.ts.
//
// The failure is the point. An unmirrored kind reaches the client as a frame
// name no binding claims and is reported as an unknown message on every channel
// that emits it -- a quiet, permanent, per-frame warning for something working
// exactly as designed.
func TestTransportKindsMirrorTheConstants(t *testing.T) {
	// Every MessageType* constant this package declares, written out so that
	// adding one without touching TransportKinds fails here.
	declared := []string{
		streaming.MessageTypeMessage,
		streaming.MessageTypePresence,
		streaming.MessageTypeTyping,
		streaming.MessageTypeSystem,
		streaming.MessageTypeJoin,
		streaming.MessageTypeLeave,
		streaming.MessageTypeError,
	}

	kinds := streaming.TransportKinds()

	if !slices.Equal(slices.Sorted(slices.Values(kinds)), slices.Sorted(slices.Values(declared))) {
		t.Fatalf("TransportKinds() = %v, want %v", kinds, declared)
	}

	// The literal set the TypeScript decoder holds. Copied from
	// packages/client-core/src/streaming.ts; if this fails, that file is the
	// other edit the change needs.
	mirrored := []string{"message", "presence", "typing", "system", "join", "leave", "error"}

	if !slices.Equal(slices.Sorted(slices.Values(kinds)), slices.Sorted(slices.Values(mirrored))) {
		t.Errorf(
			"TransportKinds() = %v, but packages/client-core/src/streaming.ts holds %v",
			kinds, mirrored,
		)
	}
}

func TestIsTransportKind(t *testing.T) {
	if !streaming.IsTransportKind(streaming.MessageTypePresence) {
		t.Error("presence is a reserved transport kind")
	}

	// The case a producer needs the check for: a domain name that happens to
	// collide with a reserved kind cannot be bound on the client.
	if streaming.IsTransportKind("order.created") {
		t.Error("order.created is a domain event, not a transport kind")
	}
}

// TestNewEventMessageLeavesIdentityAlone pins the deliberate omission. A helper
// that invented an ID would be inventing deduplication and history semantics
// that belong to the producer.
func TestNewEventMessageLeavesIdentityAlone(t *testing.T) {
	msg := streaming.NewEventMessage("order.created", nil)

	if msg.ID != "" || msg.UserID != "" || msg.RoomID != "" || msg.ChannelID != "" {
		t.Errorf("constructor filled a routing or identity field: %+v", msg)
	}

	if msg.Timestamp.IsZero() {
		t.Error("timestamp is zero; a frame without one marshals as a wrong answer")
	}
}
