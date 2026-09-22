package client

import (
	"context"
	"slices"
	"strings"
	"testing"
)

// TestSpecParserRegistersEntityFromStreamBindingOnly is GAP 2's core case: a
// spec whose ONLY reference to an entity is a stream binding -- no HTTP
// endpoint anywhere returns it -- must still produce an `entities` row for it,
// because the browser runtime needs that row to know which JSON property
// identifies a record arriving over the channel. Without it, a streams[]
// entry naming this entity is inert.
//
// Driven through SpecParser.ParseFile (the real entry point) rather than a
// hand-built IR fixture: a hand-built fixture is exactly what let a critical
// defect in this area survive 14 reviews on the previous branch.
func TestSpecParserRegistersEntityFromStreamBindingOnly(t *testing.T) {
	path := writeSpec(t, map[string]any{
		"asyncapi": "3.0.0",
		"info":     map[string]any{"title": "Orders Stream", "version": "1.0.0"},
		"components": map[string]any{
			"schemas": map[string]any{"Order": orderComponent()},
		},
		"channels": map[string]any{
			"orders": map[string]any{
				"address": "/orders",
				"messages": map[string]any{
					"orderUpdated": map[string]any{
						"payload": map[string]any{"$ref": "#/components/schemas/Order"},
					},
				},
				"x-forge-stream": []any{
					map[string]any{
						"message":    "orderUpdated",
						"entityType": "Order",
						"intent":     "upsert",
					},
				},
			},
		},
		"operations": map[string]any{
			"receiveOrderUpdate": map[string]any{
				"action":  "receive",
				"channel": map[string]any{"$ref": "#/channels/orders"},
			},
		},
	})

	spec, err := NewSpecParser().ParseFile(context.Background(), path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	entity := spec.Entities["Order"]
	if entity == nil {
		t.Fatalf("spec.Entities missing Order: %+v — a stream binding is the only reference to this "+
			"entity, and it must still be registered", spec.Entities)
	}

	if entity.IDField != "id" {
		t.Fatalf("Entities[\"Order\"].IDField = %q, want \"id\"", entity.IDField)
	}

	if len(spec.Warnings) != 0 {
		t.Fatalf("Warnings = %v, want none — the entity resolved cleanly", spec.Warnings)
	}
}

// TestSpecParserWarnsOnUnresolvableStreamBindingEntity is GAP 2's degrade-loud
// case: a stream binding names an entity type that has no matching schema
// component at all. Generation must not fail, no `entities` row may be
// invented, and a warning naming the channel and the entity type must appear
// -- silent degradation is exactly the failure mode this mechanism exists to
// prevent.
func TestSpecParserWarnsOnUnresolvableStreamBindingEntity(t *testing.T) {
	path := writeSpec(t, map[string]any{
		"asyncapi": "3.0.0",
		"info":     map[string]any{"title": "Ghost Stream", "version": "1.0.0"},
		"channels": map[string]any{
			"ghosts": map[string]any{
				"address": "/ghosts",
				"messages": map[string]any{
					"ghostSeen": map[string]any{
						"payload": map[string]any{
							"type":       "object",
							"properties": map[string]any{"id": map[string]any{"type": "string"}},
						},
					},
				},
				"x-forge-stream": []any{
					map[string]any{
						"message":    "ghostSeen",
						"entityType": "Ghost",
						"intent":     "upsert",
					},
				},
			},
		},
		"operations": map[string]any{
			"receiveGhostSeen": map[string]any{
				"action":  "receive",
				"channel": map[string]any{"$ref": "#/channels/ghosts"},
			},
		},
	})

	spec, err := NewSpecParser().ParseFile(context.Background(), path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if _, ok := spec.Entities["Ghost"]; ok {
		t.Fatalf("Entities[\"Ghost\"] = %+v, want no entry — Ghost has no schema component to infer from",
			spec.Entities["Ghost"])
	}

	joined := strings.Join(spec.Warnings, "\n")
	if !strings.Contains(joined, "Ghost") || !strings.Contains(joined, "/ghosts") {
		t.Fatalf("Warnings = %v, want one naming channel /ghosts and entity type Ghost", spec.Warnings)
	}
}

// TestSpecParserStreamBindingDoesNotOverwriteHTTPEntity is GAP 2's precedence
// case: an entity already resolved from an HTTP endpoint's response schema is
// authoritative and must not be replaced by a stream binding naming the same
// type.
//
// A single spec file is either OpenAPI or AsyncAPI (SpecParser.detectSpecType
// picks one from the top-level version key), so an HTTP endpoint and a stream
// channel for the same entity cannot appear in one file the way they would in
// one running application's combined spec. This test gets its spec.Entities
// and spec.Schemas state from ParseFile against a real OpenAPI file -- the
// realistic, HTTP-only half -- and then calls registerStreamBindingEntities
// directly with that same *APISpec: that call is exactly what all four wiring
// sites do with a channel's resolved bindings, so this exercises the real
// overwrite guard, not a reimplementation of it.
//
// The Order schema here is deliberately ambiguous for plain inference: it
// carries both a plain "id" property and an explicitly x-forge-id-marked
// "order_number" property, so InferEntity (run with no other input) always
// resolves the MARKED field, "order_number". The HTTP endpoint overrides that
// with an explicit x-forge-entity declaration naming "id" instead -- a
// legitimate, authoritative override. If stream-binding registration ran
// InferEntity again and overwrote spec.Entities["Order"], this test would
// observe IDField flip from "id" back to "order_number".
func TestSpecParserStreamBindingDoesNotOverwriteHTTPEntity(t *testing.T) {
	orderSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":           map[string]any{"type": "string"},
			"order_number": map[string]any{"type": "string", "x-forge-id": true},
		},
	}

	path := writeSpec(t, map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]any{"title": "Orders", "version": "1.0.0"},
		"components": map[string]any{
			"schemas": map[string]any{"Order": orderSchema},
		},
		"paths": map[string]any{
			"/orders/{id}": map[string]any{
				"get": map[string]any{
					"operationId": "orderGet",
					"x-forge-entity": map[string]any{
						"type":    "Order",
						"idField": "id",
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "ok",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/Order"},
								},
							},
						},
					},
				},
			},
		},
	})

	spec, err := NewSpecParser().ParseFile(context.Background(), path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if spec.Entities["Order"] == nil || spec.Entities["Order"].IDField != "id" {
		t.Fatalf("Entities[\"Order\"] = %+v, want IDField \"id\" from the HTTP endpoint's explicit override",
			spec.Entities["Order"])
	}

	// Now simulate a stream binding on a channel that also emits Order, using
	// the exact function every wiring site calls with a channel's resolved
	// bindings.
	registerStreamBindingEntities(spec, "/ws/orders", []StreamBinding{
		{Message: "orderUpdated", EntityType: "Order", Intent: StreamUpsert},
	})

	if spec.Entities["Order"].IDField != "id" {
		t.Fatalf("Entities[\"Order\"].IDField = %q after a stream binding for the same type, want it to"+
			" stay \"id\" -- the HTTP-resolved entity must not be overwritten by stream-binding inference"+
			" (which would have produced %q)", spec.Entities["Order"].IDField, "order_number")
	}
}

// TestRegisterStreamBindingEntitiesWarnsOnUnnamedEntityType covers the third
// failure mode registerStreamBindingEntities guards against: a StreamBinding
// whose EntityType is "". router.Emits[T] derives EntityType via
// reflect.TypeOf((*T)(nil)).Elem().Name(), which returns "" for an unnamed
// type argument (an anonymous struct, or a slice, map, or pointer type) --
// there is no schema lookup that can catch this the way the "no matching
// schema component" and "identity could not be inferred" cases are caught,
// because there is no name to look up. Without this check the binding would
// silently fall through with no entity registered and no explanation, which
// is exactly the silent-degradation failure mode this mechanism exists to
// prevent.
func TestRegisterStreamBindingEntitiesWarnsOnUnnamedEntityType(t *testing.T) {
	spec := &APISpec{}

	registerStreamBindingEntities(spec, "/live/updates", []StreamBinding{
		{Message: "tick", EntityType: "", Intent: StreamPatch},
	})

	if len(spec.Entities) != 0 {
		t.Fatalf("Entities = %+v, want none -- an unnamed entity type has nothing to register",
			spec.Entities)
	}

	joined := strings.Join(spec.Warnings, "\n")
	if !strings.Contains(joined, "/live/updates") || !strings.Contains(joined, "tick") {
		t.Fatalf("Warnings = %v, want one naming channel /live/updates and message tick", spec.Warnings)
	}
}

// Two Go types named Invoice were qualified apart when the document was
// generated, and the component carries the qualified type it came from. The
// binding was declared with the bare name and carries the same qualified type,
// so it is matched to the component the entity table will hold, and its
// derived collection tag follows.
func TestStreamBindingResolvesARenamedComponentByGoType(t *testing.T) {
	spec := &APISpec{Schemas: map[string]*Schema{
		"BillingInvoice": {Type: "object",
			Properties: map[string]*Schema{"id": {Type: "string"}},
			Extensions: map[string]any{"x-forge-type": "github.com/acme/billing.Invoice"},
		},
		"ShippingInvoice": {Type: "object",
			Properties: map[string]*Schema{"id": {Type: "string"}},
			Extensions: map[string]any{"x-forge-type": "github.com/acme/shipping.Invoice"},
		},
	}}

	bindings := []StreamBinding{
		{Message: "invoice.created", EntityType: "Invoice", Intent: StreamUpsert,
			Invalidates: []string{"Invoice[]", "Ledger[]"}, Type: "github.com/acme/shipping.Invoice"},
	}

	registerStreamBindingEntities(spec, "/ws/shipping", bindings)

	if bindings[0].EntityType != "ShippingInvoice" {
		t.Fatalf("EntityType = %q, want ShippingInvoice", bindings[0].EntityType)
	}

	if !slices.Equal(bindings[0].Invalidates, []string{"ShippingInvoice[]", "Ledger[]"}) {
		t.Fatalf("Invalidates = %v, want the derived tag renamed and the declared one kept", bindings[0].Invalidates)
	}

	if _, ok := spec.Entities["ShippingInvoice"]; !ok {
		t.Fatalf("entities = %v, want ShippingInvoice registered", spec.Entities)
	}

	if len(spec.Warnings) != 0 {
		t.Fatalf("warnings = %v, want none", spec.Warnings)
	}
}

// A binding whose qualified type marks no component keeps its bare name and
// reports the miss exactly as before.
func TestStreamBindingWithUnmatchedGoTypeKeepsTheBareName(t *testing.T) {
	spec := &APISpec{Schemas: map[string]*Schema{
		"BillingInvoice": {Type: "object",
			Properties: map[string]*Schema{"id": {Type: "string"}},
			Extensions: map[string]any{"x-forge-type": "github.com/acme/billing.Invoice"},
		},
	}}

	bindings := []StreamBinding{
		{Message: "invoice.created", EntityType: "Invoice", Intent: StreamUpsert,
			Invalidates: []string{"Invoice[]"}, Type: "github.com/acme/shipping.Invoice"},
	}

	registerStreamBindingEntities(spec, "/ws/shipping", bindings)

	if bindings[0].EntityType != "Invoice" {
		t.Fatalf("EntityType = %q, want Invoice", bindings[0].EntityType)
	}

	if len(spec.Warnings) != 1 || !strings.Contains(spec.Warnings[0], "no matching schema component") {
		t.Fatalf("warnings = %v, want the unmatched binding reported", spec.Warnings)
	}
}

// The extension's `type` field survives a JSON round trip into the binding.
func TestStreamBindingsReadTheQualifiedType(t *testing.T) {
	bindings := streamBindings(map[string]any{"x-forge-stream": []any{map[string]any{
		"message":     "invoice.created",
		"entityType":  "Invoice",
		"intent":      "upsert",
		"invalidates": []any{"Invoice[]"},
		"type":        "github.com/acme/billing.Invoice",
	}}})

	if len(bindings) != 1 || bindings[0].Type != "github.com/acme/billing.Invoice" {
		t.Fatalf("bindings = %+v, want the qualified type carried", bindings)
	}
}

// wtChannelDocument is an AsyncAPI document with one WebTransport channel, as
// the router's generator writes it: marked with its protocol, messages named
// for how they travel, one operation per direction, and a stream binding.
func wtChannelDocument() map[string]any {
	order := map[string]any{"$ref": "#/components/schemas/Order"}

	return map[string]any{
		"asyncapi": "3.0.0",
		"info":     map[string]any{"title": "Orders", "version": "1.0.0"},
		"components": map[string]any{
			"schemas": map[string]any{"Order": orderComponent()},
		},
		"channels": map[string]any{
			"wt_orders": map[string]any{
				"address":          "/wt/orders",
				"x-forge-protocol": "webtransport",
				"messages": map[string]any{
					"datagram":    map[string]any{"payload": order},
					"bidiSend":    map[string]any{"payload": order},
					"bidiReceive": map[string]any{"payload": order},
				},
				"x-forge-stream": []any{map[string]any{
					"message": "order.created", "entityType": "Order", "intent": "upsert",
					"invalidates": []any{"Order[]"},
				}},
			},
		},
		"operations": map[string]any{
			"ordersSend": map[string]any{
				"action": "send", "channel": map[string]any{"$ref": "#/channels/wt_orders"},
			},
			"ordersReceive": map[string]any{
				"action": "receive", "channel": map[string]any{"$ref": "#/channels/wt_orders"},
			},
		},
	}
}

// A marked channel is a WebTransport endpoint, converted once for its two
// operations, typed per stream kind, and carrying its bindings, which is what
// lets a datagram take part in the cache the way a socket frame does.
func TestSpecParserReadsAWebTransportChannel(t *testing.T) {
	spec, err := NewSpecParser().ParseFile(context.Background(), writeSpec(t, wtChannelDocument()))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	assertWebTransportEndpoint(t, spec)
}

func assertWebTransportEndpoint(t *testing.T, spec *APISpec) {
	t.Helper()

	if len(spec.WebSockets) != 0 || len(spec.SSEs) != 0 {
		t.Fatalf("marked channel was read as a socket or SSE stream: ws=%d sse=%d", len(spec.WebSockets), len(spec.SSEs))
	}

	if len(spec.WebTransports) != 1 {
		t.Fatalf("WebTransports = %d, want the one channel converted once", len(spec.WebTransports))
	}

	wt := spec.WebTransports[0]

	if wt.Path != "/wt/orders" || wt.DatagramSchema == nil || wt.BiStreamSchema == nil ||
		wt.BiStreamSchema.SendSchema == nil || wt.BiStreamSchema.ReceiveSchema == nil {
		t.Fatalf("endpoint = %+v, want datagram and both bidi halves typed", wt)
	}

	if wt.UniStreamSchema != nil {
		t.Fatalf("UniStreamSchema = %+v, want nil for an undeclared kind", wt.UniStreamSchema)
	}

	if len(wt.StreamBindings) != 1 || wt.StreamBindings[0].EntityType != "Order" {
		t.Fatalf("StreamBindings = %+v, want the Order binding", wt.StreamBindings)
	}

	if _, ok := spec.Entities["Order"]; !ok {
		t.Fatalf("entities = %v, want Order registered from the binding", spec.Entities)
	}
}

// The file path records the same per-direction messages the live path does.
func TestSpecParserRecordsEachMessagePerDirection(t *testing.T) {
	ref := func(name string) map[string]any { return map[string]any{"$ref": "#/components/schemas/" + name} }
	path := writeSpec(t, map[string]any{
		"asyncapi": "3.0.0",
		"info":     map[string]any{"title": "Chat", "version": "1.0.0"},
		"servers":  map[string]any{"ws": map[string]any{"host": "localhost", "protocol": "ws"}},
		"components": map[string]any{"schemas": map[string]any{
			"Say":    map[string]any{"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}}},
			"Typing": map[string]any{"type": "object", "properties": map[string]any{"on": map[string]any{"type": "boolean"}}},
			"Said":   map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}}},
		}},
		"channels": map[string]any{
			"ws_chat": map[string]any{
				"address": "/ws/chat",
				"servers": []any{map[string]any{"$ref": "#/servers/ws"}},
				"messages": map[string]any{
					"say":   map[string]any{"payload": ref("Say")},
					"typed": map[string]any{"payload": ref("Typing")},
					"said":  map[string]any{"payload": ref("Said")},
				},
			},
		},
		"operations": map[string]any{
			"chatSend": map[string]any{
				"action": "send", "channel": map[string]any{"$ref": "#/channels/ws_chat"},
				"messages": []any{
					map[string]any{"$ref": "#/channels/ws_chat/messages/say"},
					map[string]any{"$ref": "#/channels/ws_chat/messages/typed"},
				},
			},
			"chatReceive": map[string]any{
				"action": "receive", "channel": map[string]any{"$ref": "#/channels/ws_chat"},
				"messages": []any{map[string]any{"$ref": "#/channels/ws_chat/messages/said"}},
			},
		},
	})

	spec, err := NewSpecParser().ParseFile(context.Background(), path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if len(spec.WebSockets) != 1 {
		t.Fatalf("WebSockets = %d, want 1", len(spec.WebSockets))
	}

	ws := spec.WebSockets[0]

	if got := sortedStringKeys(ws.SendMessages); !slices.Equal(got, []string{"say", "typed"}) {
		t.Errorf("SendMessages = %v, want [say typed]", got)
	}

	if got := sortedStringKeys(ws.ReceiveMessages); !slices.Equal(got, []string{"said"}) {
		t.Errorf("ReceiveMessages = %v, want [said]", got)
	}
}
