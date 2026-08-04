package client

import (
	"context"
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
