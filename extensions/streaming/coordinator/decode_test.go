package coordinator

import (
	"encoding/json"
	"testing"

	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

func TestDecodeMessage_BroadcastPayloadIsAMessage(t *testing.T) {
	wire, err := json.Marshal(CoordinatorMessage{
		Type:    MessageTypeBroadcast,
		NodeID:  "n1",
		RoomID:  "lobby",
		Payload: &streaming.Message{ID: "m1", Type: streaming.MessageTypeMessage, RoomID: "lobby", Sequence: 5, Data: "hi"},
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := DecodeMessage(wire)
	if err != nil {
		t.Fatal(err)
	}

	if got.Type != MessageTypeBroadcast || got.NodeID != "n1" || got.RoomID != "lobby" {
		t.Fatalf("envelope = %+v", got)
	}

	msg, ok := got.Payload.(*streaming.Message)
	if !ok {
		t.Fatalf("payload is %T, want *streaming.Message", got.Payload)
	}

	if msg.ID != "m1" || msg.Sequence != 5 || msg.Data != "hi" {
		t.Fatalf("payload = %+v", msg)
	}
}

// Payloads of other message types keep the generic decode they always had.
func TestDecodeMessage_OtherPayloadsStayGeneric(t *testing.T) {
	got, err := DecodeMessage([]byte(`{"type":"node.register","nodeId":"n2","payload":{"address":"10.0.0.2"}}`))
	if err != nil {
		t.Fatal(err)
	}

	meta, ok := got.Payload.(map[string]any)
	if !ok || meta["address"] != "10.0.0.2" {
		t.Fatalf("payload = %#v, want a map", got.Payload)
	}

	got, err = DecodeMessage([]byte(`{"type":"broadcast","payload":null}`))
	if err != nil {
		t.Fatal(err)
	}

	if got.Payload != nil {
		t.Fatalf("null payload decoded to %#v", got.Payload)
	}
}
