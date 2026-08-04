// internal/router/asyncapi_client_meta_test.go
package router

import (
	"testing"

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
