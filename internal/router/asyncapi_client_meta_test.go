// internal/router/asyncapi_client_meta_test.go
package router

import (
	"reflect"
	"testing"

	"github.com/xraph/forge/internal/router/testtypes/billing"
	"github.com/xraph/forge/internal/router/testtypes/shipping"
	"github.com/xraph/forge/internal/shared"
)

// findChannelByAddress returns the channel whose Address matches path, or nil.
func findChannelByAddress(spec *AsyncAPISpec, path string) *shared.AsyncAPIChannel {
	for _, channel := range spec.Channels {
		if channel.Address == path {
			return channel
		}
	}

	return nil
}

// TestWebSocketChannelCarriesForgeStreamExtension proves processWebSocketRoute
// wires applyForgeStreamExtension. If the call in that function were removed,
// this test would fail: channel.Extensions would be nil and the assertion on
// x-forge-stream would fail the type assertion.
func TestWebSocketChannelCarriesForgeStreamExtension(t *testing.T) {
	router := NewRouter()

	err := router.WebSocket("/ws/orders", func(ctx Context, conn Connection) error {
		return nil
	},
		WithWebSocketMessages(ChatMessage{}, ChatEvent{}),
		WithName("orders"),
		WithStreamBinding(Emits[testOrder]("order.created")),
	)
	if err != nil {
		t.Fatalf("Failed to register WebSocket route: %v", err)
	}

	generator := newAsyncAPIGenerator(shared.AsyncAPIConfig{Title: "T", Version: "1.0.0"}, router)

	spec, err := generator.Generate()
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	channel := findChannelByAddress(spec, "/ws/orders")
	if channel == nil {
		t.Fatal("orders channel not found")
	}

	stream, ok := channel.Extensions["x-forge-stream"].([]map[string]any)
	if !ok || len(stream) != 1 {
		t.Fatalf("x-forge-stream missing or wrong shape: %#v", channel.Extensions)
	}

	entry := stream[0]
	if entry["message"] != "order.created" {
		t.Errorf("message = %v, want order.created", entry["message"])
	}

	if entry["entityType"] != "testOrder" {
		t.Errorf("entityType = %v, want testOrder", entry["entityType"])
	}

	if entry["intent"] != string(StreamUpsert) {
		t.Errorf("intent = %v, want %q", entry["intent"], StreamUpsert)
	}

	inv, _ := entry["invalidates"].([]string)
	if len(inv) != 1 || inv[0] != "testOrder[]" {
		t.Errorf("invalidates = %v, want [testOrder[]]", inv)
	}
}

// TestSSEChannelCarriesForgeStreamExtension proves processSSERoute wires
// applyForgeStreamExtension independently of the WebSocket path. If the call
// in that function were removed, this test would fail the same way the
// WebSocket test above would: x-forge-stream would be absent.
func TestSSEChannelCarriesForgeStreamExtension(t *testing.T) {
	router := NewRouter()

	err := router.EventStream("/sse/orders", func(ctx Context, stream Stream) error {
		return nil
	},
		WithSSEMessage("order", NotificationEvent{}),
		WithName("orders-sse"),
		WithStreamBinding(Emits[testOrder]("order.updated")),
	)
	if err != nil {
		t.Fatalf("Failed to register SSE route: %v", err)
	}

	generator := newAsyncAPIGenerator(shared.AsyncAPIConfig{Title: "T", Version: "1.0.0"}, router)

	spec, err := generator.Generate()
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	channel := findChannelByAddress(spec, "/sse/orders")
	if channel == nil {
		t.Fatal("orders channel not found")
	}

	stream, ok := channel.Extensions["x-forge-stream"].([]map[string]any)
	if !ok || len(stream) != 1 {
		t.Fatalf("x-forge-stream missing or wrong shape: %#v", channel.Extensions)
	}

	entry := stream[0]
	if entry["message"] != "order.updated" {
		t.Errorf("message = %v, want order.updated", entry["message"])
	}

	if entry["intent"] != string(StreamPatch) {
		t.Errorf("intent = %v, want %q", entry["intent"], StreamPatch)
	}

	// order.updated is a patch: Emits' default invalidation only fires for
	// non-patch intents, so this must be empty, not nil-vs-empty ambiguous.
	inv, _ := entry["invalidates"].([]string)
	if len(inv) != 0 {
		t.Errorf("invalidates = %v, want empty", inv)
	}
}

// TestWebSocketChannelWithoutStreamBindingsGetsNoExtension pins the negative
// case: a streaming route that declares no forge.client.streamBindings gets
// no x-forge-stream key at all, and Extensions is left nil rather than an
// empty map — matching applyForgeExtensions' behaviour on the OpenAPI side.
func TestWebSocketChannelWithoutStreamBindingsGetsNoExtension(t *testing.T) {
	router := NewRouter()

	err := router.WebSocket("/ws/plain", func(ctx Context, conn Connection) error {
		return nil
	},
		WithWebSocketMessages(ChatMessage{}, ChatEvent{}),
		WithName("plain"),
	)
	if err != nil {
		t.Fatalf("Failed to register WebSocket route: %v", err)
	}

	generator := newAsyncAPIGenerator(shared.AsyncAPIConfig{Title: "T", Version: "1.0.0"}, router)

	spec, err := generator.Generate()
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	channel := findChannelByAddress(spec, "/ws/plain")
	if channel == nil {
		t.Fatal("plain channel not found")
	}

	if channel.Extensions != nil {
		t.Fatalf("Extensions = %#v, want nil for a route with no stream bindings", channel.Extensions)
	}
}

// A binding carries the qualified Go type it was declared with, so the client
// generator can find the component when finalization gave it a name other
// than the bare one. Two channels emit two types that share the bare name
// Invoice; each binding names its own.
func TestStreamBindingCarriesTheQualifiedGoType(t *testing.T) {
	router := NewRouter()

	if err := router.WebSocket("/ws/billing", func(ctx Context, conn Connection) error { return nil },
		WithWebSocketMessages(billing.Invoice{}, billing.Invoice{}),
		WithStreamBinding(Emits[billing.Invoice]("invoice.created")),
	); err != nil {
		t.Fatal(err)
	}

	if err := router.WebSocket("/ws/shipping", func(ctx Context, conn Connection) error { return nil },
		WithWebSocketMessages(shipping.Invoice{}, shipping.Invoice{}),
		WithStreamBinding(Emits[shipping.Invoice]("invoice.created")),
	); err != nil {
		t.Fatal(err)
	}

	spec, err := newAsyncAPIGenerator(shared.AsyncAPIConfig{Title: "T", Version: "1.0.0"}, router).Generate()
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	for path, want := range map[string]reflect.Type{
		"/ws/billing":  reflect.TypeFor[billing.Invoice](),
		"/ws/shipping": reflect.TypeFor[shipping.Invoice](),
	} {
		channel := findChannelByAddress(spec, path)
		if channel == nil {
			t.Fatalf("%s: channel not found", path)
		}

		stream, _ := channel.Extensions["x-forge-stream"].([]map[string]any)
		if len(stream) != 1 {
			t.Fatalf("%s: x-forge-stream = %#v, want one entry", path, channel.Extensions)
		}

		if stream[0]["entityType"] != "Invoice" {
			t.Errorf("%s: entityType = %v, want the bare name Invoice", path, stream[0]["entityType"])
		}

		if stream[0]["type"] != getQualifiedTypeName(want) {
			t.Errorf("%s: type = %v, want %q", path, stream[0]["type"], getQualifiedTypeName(want))
		}
	}
}

// A WebTransport route reaches the AsyncAPI document as a channel marked with
// its protocol, its messages named for how each travels, its operations split
// by direction, and its stream bindings alongside -- which is what lets the
// client generator build a WebTransport client that takes part in the cache.
func TestWebTransportChannelIsDescribedWithItsBindings(t *testing.T) {
	router := NewRouter()

	var handler WebTransportHandler

	if err := router.WebTransport("/wt/orders", handler,
		WithWebTransportMessages(WebTransportMessages{
			Datagram:    testOrder{},
			BidiSend:    ChatMessage{},
			BidiReceive: testOrder{},
		}),
		WithName("orders-wt"),
		WithStreamBinding(Emits[testOrder]("order.created")),
	); err != nil {
		t.Fatalf("register: %v", err)
	}

	spec, err := newAsyncAPIGenerator(shared.AsyncAPIConfig{Title: "T", Version: "1.0.0"}, router).Generate()
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	channel := findChannelByAddress(spec, "/wt/orders")
	if channel == nil {
		t.Fatal("webtransport channel not found")
	}

	if channel.Extensions["x-forge-protocol"] != "webtransport" {
		t.Errorf("x-forge-protocol = %v, want webtransport", channel.Extensions["x-forge-protocol"])
	}

	for _, name := range []string{"datagram", "bidiSend", "bidiReceive"} {
		if channel.Messages[name] == nil {
			t.Errorf("message %q missing; have %v", name, channel.Messages)
		}
	}

	if channel.Messages["uniSend"] != nil {
		t.Errorf("an undeclared kind was written: %v", channel.Messages["uniSend"])
	}

	stream, _ := channel.Extensions["x-forge-stream"].([]map[string]any)
	if len(stream) != 1 || stream[0]["entityType"] != "testOrder" {
		t.Errorf("x-forge-stream = %#v, want one testOrder binding", channel.Extensions["x-forge-stream"])
	}

	var send, receive int

	for _, op := range spec.Operations {
		if op.Channel == nil || op.Channel.Ref != "#/channels/"+"wt_orders" && op.Channel.Ref != "#/channels/"+pathToChannelID("/wt/orders") {
			continue
		}

		switch op.Action {
		case "send":
			send = len(op.Messages)
		case "receive":
			receive = len(op.Messages)
		}
	}

	// datagram + bidiSend one way, datagram + bidiReceive the other.
	if send != 2 || receive != 2 {
		t.Errorf("send/receive message counts = %d/%d, want 2/2; operations: %v", send, receive, spec.Operations)
	}
}
