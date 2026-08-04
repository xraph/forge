package client

import (
	"testing"

	"github.com/xraph/forge/internal/shared"
)

func orderSchema() *Schema {
	return &Schema{Type: "object", Properties: map[string]*Schema{
		"id":    {Type: "string"},
		"total": {Type: "integer"},
	}}
}

func TestResolveEntityInfersFromResponse(t *testing.T) {
	spec := &APISpec{Schemas: map[string]*Schema{"Order": orderSchema()}}
	ep := &Endpoint{
		Method: "GET", Path: "/orders/{id}",
		Responses: map[int]*Response{200: {Content: map[string]*MediaType{
			"application/json": {Schema: &Schema{Ref: "#/components/schemas/Order"}},
		}}},
	}

	resolveEndpointCacheMeta(spec, ep, nil)

	if ep.Entity == nil || ep.Entity.Type != "Order" {
		t.Fatalf("Entity = %+v, want Order", ep.Entity)
	}

	if len(ep.CacheTags.Provides) != 1 || ep.CacheTags.Provides[0] != "Order:{id}" {
		t.Fatalf("Provides = %v, want [Order:{id}]", ep.CacheTags.Provides)
	}
}

func TestResolveEntityDetectsListResponses(t *testing.T) {
	spec := &APISpec{Schemas: map[string]*Schema{"Order": orderSchema()}}
	ep := &Endpoint{
		Method: "GET", Path: "/orders",
		Responses: map[int]*Response{200: {Content: map[string]*MediaType{
			"application/json": {Schema: &Schema{
				Type:  "array",
				Items: &Schema{Ref: "#/components/schemas/Order"},
			}},
		}}},
	}

	resolveEndpointCacheMeta(spec, ep, nil)

	if len(ep.CacheTags.Provides) != 2 {
		t.Fatalf("Provides = %v, want item and collection", ep.CacheTags.Provides)
	}
}

func TestResolveEntityHonoursNoEntity(t *testing.T) {
	spec := &APISpec{Schemas: map[string]*Schema{"Order": orderSchema()}}
	ep := &Endpoint{
		Method: "GET", Path: "/orders/{id}/snapshot",
		Responses: map[int]*Response{200: {Content: map[string]*MediaType{
			"application/json": {Schema: &Schema{Ref: "#/components/schemas/Order"}},
		}}},
	}

	resolveEndpointCacheMeta(spec, ep, map[string]any{"x-forge-no-entity": true})

	if ep.Entity != nil {
		t.Fatalf("Entity = %+v, want nil", ep.Entity)
	}

	if ep.CacheTags.Provides != nil || ep.CacheTags.Invalidates != nil {
		t.Fatalf("CacheTags = %+v, want zero", ep.CacheTags)
	}
}

func TestResolveEntityAppliesOverrides(t *testing.T) {
	spec := &APISpec{Schemas: map[string]*Schema{"Order": orderSchema()}}
	ep := &Endpoint{
		Method: "POST", Path: "/orders",
		Responses: map[int]*Response{201: {Content: map[string]*MediaType{
			"application/json": {Schema: &Schema{Ref: "#/components/schemas/Order"}},
		}}},
	}

	resolveEndpointCacheMeta(spec, ep, map[string]any{
		"x-forge-invalidates":     []any{"Inventory[]"},
		"x-forge-no-invalidation": []any{"Order[]"},
	})

	want := []string{"Inventory[]"}
	if len(ep.CacheTags.Invalidates) != 1 || ep.CacheTags.Invalidates[0] != want[0] {
		t.Fatalf("Invalidates = %v, want %v", ep.CacheTags.Invalidates, want)
	}
}

func TestExplicitEntityBeatsInference(t *testing.T) {
	spec := &APISpec{Schemas: map[string]*Schema{"Order": orderSchema()}}
	ep := &Endpoint{
		Method: "GET", Path: "/orders/{id}",
		Responses: map[int]*Response{200: {Content: map[string]*MediaType{
			"application/json": {Schema: &Schema{Ref: "#/components/schemas/Order"}},
		}}},
	}

	resolveEndpointCacheMeta(spec, ep, map[string]any{
		"x-forge-entity": map[string]any{"type": "PurchaseOrder", "idField": "order_number"},
	})

	if ep.Entity.Type != "PurchaseOrder" || ep.Entity.IDField != "order_number" {
		t.Fatalf("Entity = %+v, want PurchaseOrder/order_number", ep.Entity)
	}
}

// --- Wiring site verification -----------------------------------------
//
// The four tests below each exercise one of the four call sites named in the
// task brief directly (rather than the standalone resolution functions
// above), to prove the resolution is actually wired into IR construction and
// not merely reachable in isolation.

// TestOperationToEndpointWiresCacheMeta verifies wiring site 1:
// operationToEndpoint calls resolveEndpointCacheMeta. If that call were
// removed, ep.Entity would be nil despite the response schema carrying an id.
func TestOperationToEndpointWiresCacheMeta(t *testing.T) {
	i := &Introspector{}
	spec := &APISpec{Schemas: map[string]*Schema{"Order": {
		Type: "object",
		Properties: map[string]*Schema{
			"id": {Type: "string"},
		},
	}}}

	op := &shared.Operation{
		Responses: map[string]*shared.Response{
			"200": {
				Content: map[string]*shared.MediaType{
					"application/json": {Schema: &shared.Schema{Ref: "#/components/schemas/Order"}},
				},
			},
		},
	}

	ep := i.operationToEndpoint(spec, "GET", "/orders/{id}", op)

	if ep.Entity == nil || ep.Entity.Type != "Order" {
		t.Fatalf("operationToEndpoint did not wire entity resolution: Entity = %+v", ep.Entity)
	}

	if len(ep.CacheTags.Provides) != 1 || ep.CacheTags.Provides[0] != "Order:{id}" {
		t.Fatalf("operationToEndpoint did not wire cache tags: CacheTags = %+v", ep.CacheTags)
	}

	if spec.Entities["Order"] == nil {
		t.Fatalf("operationToEndpoint did not register spec.Entities")
	}
}

// TestChannelToWebSocketCopiesStreamBindings verifies wiring site 2:
// channelToWebSocket copies x-forge-stream into WebSocketEndpoint.StreamBindings.
func TestChannelToWebSocketCopiesStreamBindings(t *testing.T) {
	i := &Introspector{}
	channel := &shared.AsyncAPIChannel{
		Address: "/ws/orders",
		Extensions: map[string]any{
			"x-forge-stream": []map[string]any{
				{
					"message":     "order.created",
					"entityType":  "Order",
					"intent":      "upsert",
					"invalidates": []string{"Order[]"},
				},
			},
		},
	}
	operation := &shared.AsyncAPIOperation{Action: "receive"}

	ws := i.channelToWebSocket("orders", channel, operation)

	if len(ws.StreamBindings) != 1 {
		t.Fatalf("channelToWebSocket did not copy StreamBindings: %+v", ws.StreamBindings)
	}

	got := ws.StreamBindings[0]
	if got.Message != "order.created" || got.EntityType != "Order" || got.Intent != StreamUpsert {
		t.Fatalf("StreamBindings[0] = %+v, want message/entityType/intent populated", got)
	}

	if len(got.Invalidates) != 1 || got.Invalidates[0] != "Order[]" {
		t.Fatalf("StreamBindings[0].Invalidates = %v, want [Order[]]", got.Invalidates)
	}
}

// TestChannelToSSECopiesStreamBindings verifies wiring site 3:
// channelToSSE copies x-forge-stream into SSEEndpoint.StreamBindings.
func TestChannelToSSECopiesStreamBindings(t *testing.T) {
	i := &Introspector{}
	channel := &shared.AsyncAPIChannel{
		Address: "/sse/orders",
		Extensions: map[string]any{
			// The []any shape, as it would arrive after a JSON round-trip.
			"x-forge-stream": []any{
				map[string]any{
					"message":     "order.updated",
					"entityType":  "Order",
					"intent":      "patch",
					"invalidates": []any{"Order[]"},
				},
			},
		},
	}
	operation := &shared.AsyncAPIOperation{Action: "send"}

	sse := i.channelToSSE("orders-sse", channel, operation)

	if len(sse.StreamBindings) != 1 {
		t.Fatalf("channelToSSE did not copy StreamBindings: %+v", sse.StreamBindings)
	}

	got := sse.StreamBindings[0]
	if got.Message != "order.updated" || got.Intent != StreamPatch {
		t.Fatalf("StreamBindings[0] = %+v, want message/intent populated", got)
	}

	if len(got.Invalidates) != 1 || got.Invalidates[0] != "Order[]" {
		t.Fatalf("StreamBindings[0].Invalidates = %v, want [Order[]] (from []any shape)", got.Invalidates)
	}
}

// TestConvertSchemaCopiesExtensions verifies wiring site 4: convertSchema
// copies Extensions from the shared schema through to the IR schema. Without
// this, x-forge-id never reaches InferEntity and every entity relying on the
// forge:"id" tag silently stops being recognized.
func TestConvertSchemaCopiesExtensions(t *testing.T) {
	i := &Introspector{}
	sharedSchema := &shared.Schema{
		Type:       "string",
		Extensions: map[string]any{"x-forge-id": true},
	}

	schema := i.convertSchema(sharedSchema)

	if v, ok := schema.Extensions["x-forge-id"].(bool); !ok || !v {
		t.Fatalf("convertSchema did not copy Extensions: %+v", schema.Extensions)
	}
}

// TestResolveEndpointCacheMetaExportedWrapper proves the exported wrapper
// (needed by Task 12's cross-package end-to-end test) behaves identically to
// the unexported function it wraps.
func TestResolveEndpointCacheMetaExportedWrapper(t *testing.T) {
	spec := &APISpec{Schemas: map[string]*Schema{"Order": orderSchema()}}
	ep := &Endpoint{
		Method: "GET", Path: "/orders/{id}",
		Responses: map[int]*Response{200: {Content: map[string]*MediaType{
			"application/json": {Schema: &Schema{Ref: "#/components/schemas/Order"}},
		}}},
	}

	ResolveEndpointCacheMeta(spec, ep, nil)

	if ep.Entity == nil || ep.Entity.Type != "Order" {
		t.Fatalf("ResolveEndpointCacheMeta: Entity = %+v, want Order", ep.Entity)
	}
}

func TestStringSliceAcceptsBothShapes(t *testing.T) {
	if got := stringSlice([]string{"a", "b"}); len(got) != 2 || got[0] != "a" {
		t.Fatalf("stringSlice([]string) = %v", got)
	}

	if got := stringSlice([]any{"a", "b"}); len(got) != 2 || got[1] != "b" {
		t.Fatalf("stringSlice([]any) = %v", got)
	}

	if got := stringSlice(nil); got != nil {
		t.Fatalf("stringSlice(nil) = %v, want nil", got)
	}
}

func TestSchemaNameRejectsInlineSchemas(t *testing.T) {
	if got := schemaName(&Schema{Type: "object"}); got != "" {
		t.Fatalf("schemaName(inline) = %q, want empty", got)
	}

	if got := schemaName(&Schema{Ref: "#/components/schemas/Order"}); got != "Order" {
		t.Fatalf("schemaName(ref) = %q, want Order", got)
	}
}

func TestResolveEntityPicksLowestStatusCodeDeterministically(t *testing.T) {
	spec := &APISpec{Schemas: map[string]*Schema{
		"Order":    orderSchema(),
		"Snapshot": {Type: "object", Properties: map[string]*Schema{"total": {Type: "integer"}}},
	}}

	ep := &Endpoint{
		Method: "GET", Path: "/orders/{id}",
		Responses: map[int]*Response{
			// 200 carries the entity-shaped schema; a higher 2xx code carries a
			// non-entity projection. The lowest code must win regardless of map
			// iteration order.
			206: {Content: map[string]*MediaType{
				"application/json": {Schema: &Schema{Ref: "#/components/schemas/Snapshot"}},
			}},
			200: {Content: map[string]*MediaType{
				"application/json": {Schema: &Schema{Ref: "#/components/schemas/Order"}},
			}},
		},
	}

	resolveEndpointCacheMeta(spec, ep, nil)

	if ep.Entity == nil || ep.Entity.Type != "Order" {
		t.Fatalf("Entity = %+v, want Order (from status 200, not 206)", ep.Entity)
	}
}
