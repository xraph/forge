package typescript

import (
	"context"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
)

// streamOnlySpec is a client made of channels: one websocket carrying an
// envelope, a binding naming the entity inside it, and not one REST route.
//
// A gateway that mounts a realtime service alongside its REST ones publishes
// exactly this once a path filter narrows to the realtime slice, and an
// AsyncAPI document with no OpenAPI beside it is the same shape.
func streamOnlySpec() *client.APISpec {
	spec := &client.APISpec{
		Info: client.APIInfo{Title: "Shop Stream", Version: "1.0.0"},
		Schemas: map[string]*client.Schema{
			"Order": {Type: "object", Properties: map[string]*client.Schema{
				"id":    {Type: "string"},
				"total": {Type: "number"},
			}},
			"OrderEvent": {Type: "object", Properties: map[string]*client.Schema{
				"order": {Ref: "#/components/schemas/Order"},
			}},
		},
		WebSockets: []client.WebSocketEndpoint{{
			ID:            "orders",
			Path:          "/shop/ws/orders",
			ReceiveSchema: &client.Schema{Ref: "#/components/schemas/OrderEvent"},
			StreamBindings: []client.StreamBinding{{
				Message:    "orderUpdated",
				EntityType: "Order",
				Intent:     client.StreamUpsert,
			}},
		}},
		Entities: map[string]*client.EntityRef{
			"Order": {Type: "Order", IDField: "id"},
		},
	}

	client.ResolveEntityFields(spec)

	return spec
}

// TestStreamOnlyClientStillGetsItsManifest is the gate this test file exists
// for.
//
// ops.ts carries three tables and only one of them is about REST. The streams
// table is what the runtime matches an arriving message against, and the
// entities table is what tells it which property identifies the record inside
// -- see live.ts, which reads both. Gating the file on `len(spec.Endpoints) >
// 0` meant a client of channels generated a working socket and none of the
// cache metadata that makes the socket worth pointing at a cache, with nothing
// anywhere reporting the omission.
func TestStreamOnlyClientStillGetsItsManifest(t *testing.T) {
	config := baseConfig()
	config.Hooks = true
	config.IncludeStreaming = true

	out, err := NewGenerator().Generate(context.Background(), streamOnlySpec(), config)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	ops, ok := out.Files["src/ops.ts"]
	if !ok {
		t.Fatalf("no src/ops.ts; a client of channels has cache metadata too. files: %v", fileNames(out.Files))
	}

	for _, want := range []string{
		"export const streams = [",
		"channel: '/shop/ws/orders'",
		"message: 'orderUpdated'",
		"entity: 'Order'",
		"export const entities = {",
		"Order: { idField: 'id' }",
		"OrderEvent: { fields: { order: 'Order' } }",
	} {
		if !strings.Contains(ops, want) {
			t.Errorf("ops.ts is missing %q", want)
		}
	}

	// The index has to export it or the consumer cannot reach any of it.
	if index := out.Files["src/index.ts"]; !strings.Contains(index, "export * from './ops';") {
		t.Errorf("index.ts does not export ./ops:\n%s", index)
	}

	// hooks.ts is one binding per REST operation. With none, the file would be
	// two imports and nothing else, so it is correctly absent.
	if _, ok := out.Files["src/hooks.ts"]; ok {
		t.Error("hooks.ts was emitted for a spec with no operations to bind")
	}
}

// An SSE-only client is the same case through the other transport.
func TestSSEOnlyClientStillGetsItsManifest(t *testing.T) {
	spec := streamOnlySpec()
	spec.SSEs = []client.SSEEndpoint{{
		ID:           "alerts",
		Path:         "/shop/sse/alerts",
		EventSchemas: map[string]*client.Schema{"alert": {Ref: "#/components/schemas/OrderEvent"}},
		StreamBindings: []client.StreamBinding{{
			Message:    "alert",
			EntityType: "Order",
			Intent:     client.StreamUpsert,
		}},
	}}
	spec.WebSockets = nil

	config := baseConfig()
	config.Hooks = true
	config.IncludeStreaming = true

	out, err := NewGenerator().Generate(context.Background(), spec, config)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if _, ok := out.Files["src/ops.ts"]; !ok {
		t.Fatalf("no src/ops.ts for an SSE-only client. files: %v", fileNames(out.Files))
	}
}

// A spec with nothing to put in a manifest still gets no manifest. The gate
// moved, it did not go away.
func TestSpecWithNoSurfaceGetsNoManifest(t *testing.T) {
	config := baseConfig()
	config.Hooks = true

	out, err := NewGenerator().Generate(context.Background(), &client.APISpec{
		Info:    client.APIInfo{Title: "Empty", Version: "1.0.0"},
		Schemas: map[string]*client.Schema{"Order": {Type: "object"}},
	}, config)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if _, ok := out.Files["src/ops.ts"]; ok {
		t.Error("ops.ts was emitted for a spec with neither endpoints nor channels")
	}
}
