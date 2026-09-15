package client

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSpec marshals a minimal OpenAPI document carrying x-forge extensions and returns its
// path on disk.
func writeSpec(t *testing.T, doc map[string]any) string {
	t.Helper()

	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}

	path := filepath.Join(t.TempDir(), "openapi.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	return path
}

func orderComponent() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":    map[string]any{"type": "string"},
			"total": map[string]any{"type": "integer"},
		},
	}
}

func TestSpecParserResolvesEntityFromFile(t *testing.T) {
	path := writeSpec(t, map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]any{"title": "Orders", "version": "1.0.0"},
		"components": map[string]any{
			"schemas": map[string]any{"Order": orderComponent()},
		},
		"paths": map[string]any{
			"/orders": map[string]any{
				"get": map[string]any{
					"operationId": "orderList",
					"responses": map[string]any{
						"200": map[string]any{
							"description": "ok",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{
										"type":  "array",
										"items": map[string]any{"$ref": "#/components/schemas/Order"},
									},
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

	if len(spec.Endpoints) != 1 {
		t.Fatalf("endpoints = %d, want 1", len(spec.Endpoints))
	}

	ep := spec.Endpoints[0]

	if ep.Entity == nil || ep.Entity.Type != "Order" || ep.Entity.IDField != "id" {
		t.Fatalf("Entity = %+v, want Order/id — the file path did not resolve identity", ep.Entity)
	}

	if len(ep.CacheTags.Provides) != 2 {
		t.Fatalf("Provides = %v, want item and collection for a list response", ep.CacheTags.Provides)
	}

	if spec.Entities["Order"] == nil {
		t.Fatalf("spec.Entities missing Order: %+v", spec.Entities)
	}
}

// x-forge-id must survive the file round trip. This is the case that silently degrades: the
// endpoint still generates, it just stops being an entity.
func TestSpecParserCarriesForgeIDExtension(t *testing.T) {
	component := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"order_number": map[string]any{"type": "string", "x-forge-id": true},
		},
	}

	path := writeSpec(t, map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]any{"title": "Orders", "version": "1.0.0"},
		"components": map[string]any{
			"schemas": map[string]any{"Order": component},
		},
		"paths": map[string]any{
			"/orders/{id}": map[string]any{
				"get": map[string]any{
					"operationId": "orderGet",
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

	if spec.Schemas["Order"].Properties["order_number"].Extensions["x-forge-id"] != true {
		t.Fatalf("x-forge-id did not survive convertSchema: %+v",
			spec.Schemas["Order"].Properties["order_number"].Extensions)
	}

	if spec.Endpoints[0].Entity == nil || spec.Endpoints[0].Entity.IDField != "order_number" {
		t.Fatalf("Entity = %+v, want IDField order_number", spec.Endpoints[0].Entity)
	}
}

// An explicit opt-out on the file path must beat inference, same as on the live path.
func TestSpecParserHonoursNoEntityFromFile(t *testing.T) {
	path := writeSpec(t, map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]any{"title": "Orders", "version": "1.0.0"},
		"components": map[string]any{
			"schemas": map[string]any{"Order": orderComponent()},
		},
		"paths": map[string]any{
			"/orders/{id}/snapshot": map[string]any{
				"get": map[string]any{
					"operationId":       "orderSnapshot",
					"x-forge-no-entity": true,
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

	if spec.Endpoints[0].Entity != nil {
		t.Fatalf("Entity = %+v, want nil — a projection must not be normalized",
			spec.Endpoints[0].Entity)
	}
}

// Cross-entity declarations arrive as []any from JSON, not []string. stringSlice must cope.
// It also carries x-forge-stale-time on a sibling GET, since a number decoded from JSON
// arrives as float64 the same way an []any does, and numericExtension must cope with that
// the same way stringSlice copes with []any.
func TestSpecParserReadsInvalidatesFromFile(t *testing.T) {
	path := writeSpec(t, map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]any{"title": "Orders", "version": "1.0.0"},
		"components": map[string]any{
			"schemas": map[string]any{"Order": orderComponent()},
		},
		"paths": map[string]any{
			"/orders": map[string]any{
				"post": map[string]any{
					"operationId":         "orderCreate",
					"x-forge-invalidates": []any{"Inventory[]"},
					"responses": map[string]any{
						"201": map[string]any{
							"description": "created",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/Order"},
								},
							},
						},
					},
				},
			},
			"/orders/{id}": map[string]any{
				"get": map[string]any{
					"operationId":        "orderGet",
					"x-forge-stale-time": 30000,
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

	inv := spec.Endpoints[0].CacheTags.Invalidates

	var found bool

	for _, tag := range inv {
		if tag == "Inventory[]" {
			found = true
		}
	}

	if !found {
		t.Fatalf("Invalidates = %v, want it to contain Inventory[] — []any was not coerced", inv)
	}

	if spec.Endpoints[1].StaleTime != 30000 {
		t.Fatalf("StaleTime = %d, want 30000 — x-forge-stale-time was dropped by the JSON file path",
			spec.Endpoints[1].StaleTime)
	}
}

// x-forge-stream on a WebSocket channel must survive the file round trip into
// WebSocketEndpoint.StreamBindings. This exercises wiring site 3
// (convertWebSocketChannel), separately from site 4 below: the channel here
// references a server whose protocol is "wss", which is what routes it through
// the WebSocket branch of parseAsyncAPI rather than the SSE branch.
func TestSpecParserWebSocketStreamBindingsFromFile(t *testing.T) {
	path := writeSpec(t, map[string]any{
		"asyncapi": "3.0.0",
		"info":     map[string]any{"title": "Orders Stream", "version": "1.0.0"},
		"servers": map[string]any{
			"main": map[string]any{
				"host":     "ws.example.com",
				"protocol": "wss",
			},
		},
		"channels": map[string]any{
			"orders": map[string]any{
				"address": "/orders",
				"servers": []any{
					map[string]any{"$ref": "#/servers/main"},
				},
				"messages": map[string]any{
					"orderUpdated": map[string]any{
						"payload": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"id": map[string]any{"type": "string"},
							},
						},
					},
				},
				"x-forge-stream": []any{
					map[string]any{
						"message":     "orderUpdated",
						"entityType":  "Order",
						"intent":      "update",
						"invalidates": []any{"Order[]"},
					},
				},
			},
		},
		"operations": map[string]any{
			"sendOrderUpdate": map[string]any{
				"action":  "send",
				"channel": map[string]any{"$ref": "#/channels/orders"},
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

	bindings := spec.WebSockets[0].StreamBindings
	if len(bindings) != 1 {
		t.Fatalf("StreamBindings = %+v, want 1 entry — convertWebSocketChannel did not copy x-forge-stream", bindings)
	}

	b := bindings[0]
	if b.Message != "orderUpdated" || b.EntityType != "Order" || string(b.Intent) != "update" {
		t.Fatalf("StreamBindings[0] = %+v, want message/entityType/intent orderUpdated/Order/update", b)
	}

	if len(b.Invalidates) != 1 || b.Invalidates[0] != "Order[]" {
		t.Fatalf("StreamBindings[0].Invalidates = %v, want [Order[]]", b.Invalidates)
	}
}

// x-forge-stream on an SSE channel must survive the file round trip into
// SSEEndpoint.StreamBindings. This exercises wiring site 4 (convertSSEChannel)
// separately from site 3 above: this channel declares no messages and no
// server reference, so detectWebSocketChannel routes it through the SSE
// branch of parseAsyncAPI instead of the WebSocket one.
func TestSpecParserSSEStreamBindingsFromFile(t *testing.T) {
	path := writeSpec(t, map[string]any{
		"asyncapi": "3.0.0",
		"info":     map[string]any{"title": "Notifications Stream", "version": "1.0.0"},
		"channels": map[string]any{
			"notifications": map[string]any{
				"address": "/notifications",
				"x-forge-stream": []any{
					map[string]any{
						"message":     "userJoined",
						"entityType":  "User",
						"intent":      "create",
						"invalidates": []any{"User[]"},
					},
				},
			},
		},
		"operations": map[string]any{
			"receiveNotifications": map[string]any{
				"action":  "receive",
				"channel": map[string]any{"$ref": "#/channels/notifications"},
			},
		},
	})

	spec, err := NewSpecParser().ParseFile(context.Background(), path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if len(spec.SSEs) != 1 {
		t.Fatalf("SSEs = %d, want 1", len(spec.SSEs))
	}

	bindings := spec.SSEs[0].StreamBindings
	if len(bindings) != 1 {
		t.Fatalf("StreamBindings = %+v, want 1 entry — convertSSEChannel did not copy x-forge-stream", bindings)
	}

	b := bindings[0]
	if b.Message != "userJoined" || b.EntityType != "User" || string(b.Intent) != "create" {
		t.Fatalf("StreamBindings[0] = %+v, want message/entityType/intent userJoined/User/create", b)
	}

	if len(b.Invalidates) != 1 || b.Invalidates[0] != "User[]" {
		t.Fatalf("StreamBindings[0].Invalidates = %v, want [User[]]", b.Invalidates)
	}
}

// A duplex channel names each direction from the operation that speaks it.
// The document below is the shape forge's own router publishes: one message
// per direction keyed `send` and `receive`, each carrying a `name`, and one
// operation per direction referencing its message. Before this test the
// parser stamped every message with the first operation's action, so the
// send direction went unnamed, and it took the last-sorted payload for both
// schemas, so the receive schema was the send payload.
func TestSpecParserDuplexChannelNamesEachDirectionFromItsOperation(t *testing.T) {
	path := writeSpec(t, duplexAsyncAPIDocument())

	spec, err := NewSpecParser().ParseFile(context.Background(), path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	assertDuplexDirections(t, spec)
}

// duplexAsyncAPIDocument is the live-query channel as forge's router emits it:
// operation ids sort the receive operation first, which is the order that
// exposed the bug.
func duplexAsyncAPIDocument() map[string]any {
	return map[string]any{
		"asyncapi": "3.0.0",
		"info":     map[string]any{"title": "Live", "version": "1.0.0"},
		"servers": map[string]any{
			"main": map[string]any{"host": "ws.example.com", "protocol": "wss"},
		},
		"channels": map[string]any{
			"liveQueryWS": map[string]any{
				"address": "/api/v1/query/live/ws",
				"servers": []any{map[string]any{"$ref": "#/servers/main"}},
				"messages": map[string]any{
					"receive": map[string]any{
						"name": "ReceiveMessage",
						"payload": map[string]any{
							"type":       "object",
							"properties": map[string]any{"type": map[string]any{"type": "string"}},
						},
					},
					"send": map[string]any{
						"name": "SendMessage",
						"payload": map[string]any{
							"type":       "object",
							"properties": map[string]any{"action": map[string]any{"type": "string"}},
						},
					},
				},
			},
		},
		"operations": map[string]any{
			"query.live.wsReceive": map[string]any{
				"action":   "receive",
				"channel":  map[string]any{"$ref": "#/channels/liveQueryWS"},
				"messages": []any{map[string]any{"$ref": "#/channels/liveQueryWS/messages/receive"}},
			},
			"query.live.wsSend": map[string]any{
				"action":   "send",
				"channel":  map[string]any{"$ref": "#/channels/liveQueryWS"},
				"messages": []any{map[string]any{"$ref": "#/channels/liveQueryWS/messages/send"}},
			},
		},
	}
}

// assertDuplexDirections checks the one endpoint both parsers should produce
// from duplexAsyncAPIDocument: one WebSocket, the send schema carrying the
// send payload, the receive schema the receive payload, and the messages
// metadata keyed by message NAME so the generated binding can say
// `send: 'SendMessage'` rather than an empty string.
func assertDuplexDirections(t *testing.T, spec *APISpec) {
	t.Helper()

	if len(spec.WebSockets) != 1 {
		t.Fatalf("WebSockets = %d, want the two operations folded into 1", len(spec.WebSockets))
	}

	ws := spec.WebSockets[0]

	if ws.SendSchema == nil || ws.SendSchema.Properties["action"] == nil {
		t.Errorf("SendSchema = %+v, want the send message payload (has `action`)", ws.SendSchema)
	}

	if ws.ReceiveSchema == nil || ws.ReceiveSchema.Properties["type"] == nil {
		t.Errorf("ReceiveSchema = %+v, want the receive message payload (has `type`)", ws.ReceiveSchema)
	}

	names, _ := ws.Metadata["messages"].(map[string]string)
	if names["SendMessage"] != "send" || names["ReceiveMessage"] != "receive" || len(names) != 2 {
		t.Errorf("Metadata[messages] = %v, want {SendMessage: send, ReceiveMessage: receive}", names)
	}
}

// AsyncAPI 3 says an operation's `messages` must reference the channel's own
// messages, and forge's router obeys that. Other generators do not: they point
// at `#/components/messages/<key>` and leave the channel to carry the
// definition. That document used to resolve no reference at all, fall back to
// "this operation speaks the whole channel" for both operations, and stamp
// every message name with each action in turn, which is the unnamed send
// direction this fold exists to prevent. The trailing segment of the
// reference is the channel's key here, so resolve it.
func TestOperationMessagesResolveComponentRefsByTrailingSegment(t *testing.T) {
	doc := duplexAsyncAPIDocument()
	operations, _ := doc["operations"].(map[string]any)

	for id, action := range map[string]string{"query.live.wsReceive": "receive", "query.live.wsSend": "send"} {
		operation, _ := operations[id].(map[string]any)
		operation["messages"] = []any{map[string]any{"$ref": "#/components/messages/" + action}}
	}

	spec, err := NewSpecParser().ParseFile(context.Background(), writeSpec(t, doc))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	assertDuplexDirections(t, spec)
}

// When the references resolve to nothing at all the fold still has to produce
// something, and speaking the whole channel is the only answer available. It
// is also the answer that silently mislabels both directions, so it says so:
// a client generated from that document is wrong in a way nothing about it
// looks wrong, and the warning is the only place a reader finds out.
func TestOperationMessagesWarnWhenListedRefsResolveToNothing(t *testing.T) {
	doc := duplexAsyncAPIDocument()
	operations, _ := doc["operations"].(map[string]any)

	for _, id := range []string{"query.live.wsReceive", "query.live.wsSend"} {
		operation, _ := operations[id].(map[string]any)
		operation["messages"] = []any{map[string]any{"$ref": "#/components/messages/nothingNamedThis"}}
	}

	spec, err := NewSpecParser().ParseFile(context.Background(), writeSpec(t, doc))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	var found string

	for _, warning := range spec.Warnings {
		if strings.Contains(warning, "query.live.wsSend") {
			found = warning
		}
	}

	if found == "" {
		t.Fatalf("Warnings = %v, want one naming the operation whose message refs resolved to nothing", spec.Warnings)
	}

	if !strings.Contains(found, "/api/v1/query/live/ws") {
		t.Errorf("warning = %q, want it to name the channel as well as the operation", found)
	}
}

// An operation that lists no messages legitimately speaks the whole channel,
// and that is how every document written before operations carried `messages`
// reads. Two such operations both claim every message, and the fold used to
// let the later one relabel what the earlier had already claimed: whichever
// operation sorted last owned every name, so one direction ended up empty in
// the generated binding. A claim by the opposite direction stands.
func TestWholeChannelFallbackDoesNotRelabelAClaimedDirection(t *testing.T) {
	doc := duplexAsyncAPIDocument()
	operations, _ := doc["operations"].(map[string]any)

	for _, id := range []string{"query.live.wsReceive", "query.live.wsSend"} {
		operation, _ := operations[id].(map[string]any)
		delete(operation, "messages")
	}

	spec, err := NewSpecParser().ParseFile(context.Background(), writeSpec(t, doc))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if len(spec.WebSockets) != 1 {
		t.Fatalf("WebSockets = %d, want 1", len(spec.WebSockets))
	}

	names, _ := spec.WebSockets[0].Metadata["messages"].(map[string]string)

	// query.live.wsReceive sorts first and claims both names; the send
	// operation must not take them back.
	if names["ReceiveMessage"] != "receive" {
		t.Errorf("Metadata[messages] = %v, want ReceiveMessage still claimed by the receive operation", names)
	}
}
