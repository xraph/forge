package client

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/router"
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

func TestStaleTimeIsReadFromTheExtension(t *testing.T) {
	spec := &APISpec{Schemas: map[string]*Schema{"Order": orderSchema()}}
	ep := &Endpoint{
		Method: "GET", Path: "/orders",
		Responses: map[int]*Response{200: {Content: map[string]*MediaType{
			"application/json": {Schema: &Schema{Type: "array", Items: &Schema{Ref: "#/components/schemas/Order"}}},
		}}},
	}

	resolveEndpointCacheMeta(spec, ep, map[string]any{"x-forge-stale-time": float64(30000)})

	if ep.StaleTime != 30000 {
		t.Fatalf("StaleTime = %d, want 30000", ep.StaleTime)
	}
}

func TestStaleTimeIsIgnoredOnAWrite(t *testing.T) {
	spec := &APISpec{Schemas: map[string]*Schema{"Order": orderSchema()}}
	ep := &Endpoint{
		Method: "POST", Path: "/orders",
		Responses: map[int]*Response{201: {Content: map[string]*MediaType{
			"application/json": {Schema: &Schema{Ref: "#/components/schemas/Order"}},
		}}},
	}

	resolveEndpointCacheMeta(spec, ep, map[string]any{"x-forge-stale-time": float64(30000)})

	if ep.StaleTime != 0 {
		t.Fatalf("StaleTime = %d, want 0 (a write has no cached result to keep fresh)", ep.StaleTime)
	}
}

// TestStaleTimeOnWriteWarnsOnlyWhenDeclared pins the loud-drop behaviour
// alongside TestStaleTimeIsIgnoredOnAWrite above: a write that declares
// x-forge-stale-time must not just silently end up with a zero StaleTime, it
// must produce a warning naming the method and path so the declaring user has
// something to act on. A write that never declared one must stay silent, or
// every ordinary POST in a large document would produce a warning.
func TestStaleTimeOnWriteWarnsOnlyWhenDeclared(t *testing.T) {
	schemas := map[string]*Schema{"Order": orderSchema()}

	declared := &APISpec{Schemas: schemas}
	ep := &Endpoint{
		Method: "POST", Path: "/orders",
		Responses: map[int]*Response{201: {Content: map[string]*MediaType{
			"application/json": {Schema: &Schema{Ref: "#/components/schemas/Order"}},
		}}},
	}

	resolveEndpointCacheMeta(declared, ep, map[string]any{"x-forge-stale-time": float64(30000)})

	var found bool

	for _, w := range declared.Warnings {
		if strings.Contains(w, "POST") && strings.Contains(w, "/orders") && strings.Contains(w, "x-forge-stale-time") {
			found = true
		}
	}

	if !found {
		t.Fatalf("Warnings = %v, want one naming POST and /orders about the dropped x-forge-stale-time",
			declared.Warnings)
	}

	undeclared := &APISpec{Schemas: schemas}
	epNoDecl := &Endpoint{
		Method: "POST", Path: "/orders",
		Responses: map[int]*Response{201: {Content: map[string]*MediaType{
			"application/json": {Schema: &Schema{Ref: "#/components/schemas/Order"}},
		}}},
	}

	resolveEndpointCacheMeta(undeclared, epNoDecl, nil)

	if len(undeclared.Warnings) != 0 {
		t.Fatalf("Warnings = %v, want none for a write that never declared x-forge-stale-time",
			undeclared.Warnings)
	}
}

// TestStaleTimeWarnsOnUnusableValue pins the loud-drop behaviour for a GET or
// HEAD that declares x-forge-stale-time with a value that is not a usable
// positive number: a non-numeric string, or a negative number. Both are
// present but unusable, so both must warn, naming the method and path, the
// same way a write's declaration does. A valid positive value must produce no
// warning at all.
func TestStaleTimeWarnsOnUnusableValue(t *testing.T) {
	schemas := map[string]*Schema{"Order": orderSchema()}

	cases := []struct {
		name string
		ext  map[string]any
	}{
		{name: "non-numeric", ext: map[string]any{"x-forge-stale-time": "30s"}},
		{name: "negative", ext: map[string]any{"x-forge-stale-time": float64(-1000)}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := &APISpec{Schemas: schemas}
			ep := &Endpoint{
				Method: "GET", Path: "/orders",
				Responses: map[int]*Response{200: {Content: map[string]*MediaType{
					"application/json": {Schema: &Schema{Ref: "#/components/schemas/Order"}},
				}}},
			}

			resolveEndpointCacheMeta(spec, ep, tc.ext)

			if ep.StaleTime != 0 {
				t.Fatalf("StaleTime = %d, want 0 for an unusable declared value", ep.StaleTime)
			}

			var found bool

			for _, w := range spec.Warnings {
				if strings.Contains(w, "GET") && strings.Contains(w, "/orders") && strings.Contains(w, "x-forge-stale-time") {
					found = true
				}
			}

			if !found {
				t.Fatalf("Warnings = %v, want one naming GET and /orders about the unusable x-forge-stale-time",
					spec.Warnings)
			}
		})
	}

	valid := &APISpec{Schemas: schemas}
	epValid := &Endpoint{
		Method: "GET", Path: "/orders",
		Responses: map[int]*Response{200: {Content: map[string]*MediaType{
			"application/json": {Schema: &Schema{Ref: "#/components/schemas/Order"}},
		}}},
	}

	resolveEndpointCacheMeta(valid, epValid, map[string]any{"x-forge-stale-time": float64(30000)})

	if len(valid.Warnings) != 0 {
		t.Fatalf("Warnings = %v, want none for a valid positive x-forge-stale-time", valid.Warnings)
	}
}

func TestStaleTimeAcceptsInt64AndInt(t *testing.T) {
	spec := &APISpec{Schemas: map[string]*Schema{"Order": orderSchema()}}

	for _, v := range []any{int64(15000), int(15000)} {
		ep := &Endpoint{
			Method: "HEAD", Path: "/orders",
			Responses: map[int]*Response{200: {Content: map[string]*MediaType{
				"application/json": {Schema: &Schema{Ref: "#/components/schemas/Order"}},
			}}},
		}

		resolveEndpointCacheMeta(spec, ep, map[string]any{"x-forge-stale-time": v})

		if ep.StaleTime != 15000 {
			t.Fatalf("StaleTime = %d for %T, want 15000", ep.StaleTime, v)
		}
	}
}

func TestStaleTimeIsAbsentWhenUndeclared(t *testing.T) {
	spec := &APISpec{Schemas: map[string]*Schema{"Order": orderSchema()}}
	ep := &Endpoint{
		Method: "GET", Path: "/orders/{id}",
		Responses: map[int]*Response{200: {Content: map[string]*MediaType{
			"application/json": {Schema: &Schema{Ref: "#/components/schemas/Order"}},
		}}},
	}

	resolveEndpointCacheMeta(spec, ep, nil)

	if ep.StaleTime != 0 {
		t.Fatalf("StaleTime = %d, want 0 when undeclared", ep.StaleTime)
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

// TestOperationToEndpointHandlesDefaultResponse proves the "default" status
// key still routes to Endpoint.DefaultError and is never treated as a 2xx
// success response that could carry an entity.
func TestOperationToEndpointHandlesDefaultResponse(t *testing.T) {
	i := &Introspector{}
	spec := &APISpec{Schemas: map[string]*Schema{}}

	op := &shared.Operation{
		Responses: map[string]*shared.Response{
			"default": {
				Content: map[string]*shared.MediaType{
					"application/json": {Schema: &shared.Schema{Type: "object"}},
				},
			},
		},
	}

	ep := i.operationToEndpoint(spec, "GET", "/orders/{id}", op)

	if ep.DefaultError == nil {
		t.Fatalf("operationToEndpoint did not populate DefaultError for the \"default\" key")
	}

	if len(ep.Responses) != 0 {
		t.Fatalf("operationToEndpoint filed \"default\" under Responses: %+v", ep.Responses)
	}

	if ep.Entity != nil {
		t.Fatalf("Entity = %+v, want nil (no 2xx response to infer from)", ep.Entity)
	}
}

// TestOperationToEndpointSkipsUnparseableStatusKey proves a status key that
// parseStatusCode rejects (non-numeric, not "default", not a valid "NXX"
// wildcard) is dropped rather than silently filed under DefaultError or
// Responses. Filing it as DefaultError would let a typo'd status key become
// the endpoint's error shape; filing it under Responses would risk a bogus
// entry the generators don't expect.
func TestOperationToEndpointSkipsUnparseableStatusKey(t *testing.T) {
	i := &Introspector{}
	spec := &APISpec{Schemas: map[string]*Schema{"Order": orderSchema()}}

	op := &shared.Operation{
		Responses: map[string]*shared.Response{
			"not-a-status": {
				Content: map[string]*shared.MediaType{
					"application/json": {Schema: &shared.Schema{Ref: "#/components/schemas/Order"}},
				},
			},
		},
	}

	ep := i.operationToEndpoint(spec, "GET", "/orders/{id}", op)

	if len(ep.Responses) != 0 {
		t.Fatalf("operationToEndpoint kept an unparseable status key in Responses: %+v", ep.Responses)
	}

	if ep.DefaultError != nil {
		t.Fatalf("operationToEndpoint filed an unparseable status key under DefaultError: %+v", ep.DefaultError)
	}

	if ep.Entity != nil {
		t.Fatalf("Entity = %+v, want nil (no valid 2xx response to infer from)", ep.Entity)
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
	spec := &APISpec{Schemas: map[string]*Schema{"Order": orderSchema()}}

	ws := i.channelToWebSocket(spec, "orders", channel, operation)

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
	spec := &APISpec{Schemas: map[string]*Schema{"Order": orderSchema()}}

	sse := i.channelToSSE(spec, "orders-sse", channel, operation)

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

// TestIntrospectorSortsSecuritySchemes mirrors
// TestParserSortsSecuritySchemes in spec_parser_test.go, but drives the
// live-router path instead of a parsed file: extractFromOpenAPI ranges the
// same openAPI.Components.SecuritySchemes map, so it needs the identical
// sort. Without it, a client generated by introspecting a running router
// (rather than parsing an OpenAPI document) gets a nondeterministic
// AuthConfig field order on every regeneration.
func TestIntrospectorSortsSecuritySchemes(t *testing.T) {
	openAPI := &shared.OpenAPISpec{
		OpenAPI: "3.0.0",
		Info:    shared.Info{Title: "t", Version: "1"},
		Components: &shared.Components{SecuritySchemes: map[string]shared.SecurityScheme{
			"zeta":  {Type: "http", Scheme: "bearer"},
			"alpha": {Type: "apiKey", In: "header", Name: "X-A"},
			"mid":   {Type: "apiKey", In: "query", Name: "q"},
		}},
	}

	// A map range is unordered, so one Introspect proves nothing. Repeat it,
	// same as the parser test does.
	for i := 0; i < 20; i++ {
		spec, err := NewIntrospector(specOnlyRouter{openAPI: openAPI}).Introspect(context.Background())
		if err != nil {
			t.Fatalf("Introspect: %v", err)
		}

		var keys []string
		for _, s := range spec.Security {
			keys = append(keys, s.Key)
		}

		want := []string{"alpha", "mid", "zeta"}
		if !slices.Equal(keys, want) {
			t.Fatalf("run %d: keys = %v, want %v", i, keys, want)
		}
	}
}

// TestIntrospectorKeepsCookieParameters mirrors
// TestParserKeepsCookieParameters in spec_parser_test.go, but drives the
// live-router path: operationToEndpoint has its own path/query/header/cookie
// switch, separate from convertOperation's in spec_parser.go, and needs the
// identical cookie case or a router-introspected client silently loses any
// cookie parameter a parsed-file client would keep.
func TestIntrospectorKeepsCookieParameters(t *testing.T) {
	openAPI := &shared.OpenAPISpec{
		OpenAPI: "3.0.0",
		Info:    shared.Info{Title: "t", Version: "1"},
		Paths: map[string]*shared.PathItem{
			"/me": {Get: &shared.Operation{
				OperationID: "me",
				Parameters: []shared.Parameter{
					{Name: "sid", In: "cookie", Required: true, Schema: &shared.Schema{Type: "string"}},
				},
				Responses: map[string]*shared.Response{"200": {Description: "ok"}},
			}},
		},
	}

	spec, err := NewIntrospector(specOnlyRouter{openAPI: openAPI}).Introspect(context.Background())
	if err != nil {
		t.Fatalf("Introspect: %v", err)
	}

	if len(spec.Endpoints) != 1 {
		t.Fatalf("endpoints = %d, want 1", len(spec.Endpoints))
	}

	got := spec.Endpoints[0].CookieParams
	if len(got) != 1 || got[0].Name != "sid" {
		t.Fatalf("CookieParams = %+v, want one named sid", got)
	}
}

// TestIntrospectorWarnsOnAnUnknownParameterLocation mirrors
// TestParserWarnsOnAnUnknownParameterLocation: the live-router path must
// report an unrecognized parameter location the same way the parsed-file
// path does, not drop it silently.
func TestIntrospectorWarnsOnAnUnknownParameterLocation(t *testing.T) {
	openAPI := &shared.OpenAPISpec{
		OpenAPI: "3.0.0",
		Info:    shared.Info{Title: "t", Version: "1"},
		Paths: map[string]*shared.PathItem{
			"/me": {Get: &shared.Operation{
				OperationID: "me",
				Parameters: []shared.Parameter{
					{Name: "weird", In: "telepathy", Schema: &shared.Schema{Type: "string"}},
				},
				Responses: map[string]*shared.Response{"200": {Description: "ok"}},
			}},
		},
	}

	spec, err := NewIntrospector(specOnlyRouter{openAPI: openAPI}).Introspect(context.Background())
	if err != nil {
		t.Fatalf("Introspect: %v", err)
	}

	var found bool
	for _, w := range spec.Warnings {
		if strings.Contains(w, "telepathy") && strings.Contains(w, "weird") {
			found = true
		}
	}

	if !found {
		t.Fatalf("warnings = %v, want one naming the parameter and its location", spec.Warnings)
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

// A declaration the reader cannot use must say so.
//
// Every one of these used to drop in silence. A document declared something,
// the generator ignored it, the client came out as though nothing had been
// declared at all, and the only way to find out was to notice the cache
// behaving wrongly weeks later. `x-forge-stale-time` was fixed first; these
// are the rest of the same shape.

/** A read endpoint returning one Order, for extension tests. */
func extensionEndpoint() (*APISpec, *Endpoint) {
	spec := &APISpec{Schemas: map[string]*Schema{"Order": orderSchema()}}
	ep := &Endpoint{
		Method: "GET", Path: "/orders/{id}",
		Responses: map[int]*Response{200: {Content: map[string]*MediaType{
			"application/json": {Schema: &Schema{Ref: "#/components/schemas/Order"}},
		}}},
	}

	return spec, ep
}

func warningMentioning(spec *APISpec, needle string) bool {
	for _, w := range spec.Warnings {
		if strings.Contains(w, needle) {
			return true
		}
	}

	return false
}

func TestNoEntityWarnsWhenItIsNotABool(t *testing.T) {
	spec, ep := extensionEndpoint()

	// A hand-written YAML `x-forge-no-entity: "true"` is a string. The bool
	// assertion misses it, and the endpoint silently KEEPS the entity the
	// author was trying to remove, which is the opposite of what they asked.
	resolveEndpointCacheMeta(spec, ep, map[string]any{"x-forge-no-entity": "true"})

	if !warningMentioning(spec, "x-forge-no-entity") {
		t.Fatalf("Warnings = %v, want one naming x-forge-no-entity", spec.Warnings)
	}
}

func TestNoEntityIsSilentWhenValidOrAbsent(t *testing.T) {
	spec, ep := extensionEndpoint()

	resolveEndpointCacheMeta(spec, ep, map[string]any{"x-forge-no-entity": true})

	if len(spec.Warnings) != 0 {
		t.Fatalf("Warnings = %v, want none for a valid declaration", spec.Warnings)
	}

	other, ep2 := extensionEndpoint()

	resolveEndpointCacheMeta(other, ep2, nil)

	if len(other.Warnings) != 0 {
		t.Fatalf("Warnings = %v, want none when nothing is declared", other.Warnings)
	}
}

func TestEntityWarnsWhenTheDeclarationIsIncomplete(t *testing.T) {
	spec, ep := extensionEndpoint()

	// `idField` misspelled. The map assertion succeeds, the guard on both
	// fields fails, and resolution falls through to INFERENCE, which produces
	// a plausible answer that is not the one declared.
	resolveEndpointCacheMeta(spec, ep, map[string]any{
		"x-forge-entity": map[string]any{"type": "Order", "id_field": "id"},
	})

	if !warningMentioning(spec, "x-forge-entity") {
		t.Fatalf("Warnings = %v, want one naming x-forge-entity", spec.Warnings)
	}
}

func TestEntityWarnsWhenItIsNotAMap(t *testing.T) {
	spec, ep := extensionEndpoint()

	resolveEndpointCacheMeta(spec, ep, map[string]any{"x-forge-entity": "Order"})

	if !warningMentioning(spec, "x-forge-entity") {
		t.Fatalf("Warnings = %v, want one naming x-forge-entity", spec.Warnings)
	}
}

func TestInvalidatesWarnsWhenItIsNotAList(t *testing.T) {
	spec, ep := extensionEndpoint()
	ep.Method = "POST"

	// A bare string where a list belongs. `stringSlice` returns nil from its
	// default case, so the endpoint declares no cross-entity invalidation at
	// all and every query the author meant to refresh goes stale forever.
	resolveEndpointCacheMeta(spec, ep, map[string]any{"x-forge-invalidates": "Inventory[]"})

	if !warningMentioning(spec, "x-forge-invalidates") {
		t.Fatalf("Warnings = %v, want one naming x-forge-invalidates", spec.Warnings)
	}
}

func TestInvalidatesWarnsWhenAnItemIsNotAString(t *testing.T) {
	spec, ep := extensionEndpoint()
	ep.Method = "POST"

	// The list survives and the bad element vanishes from it, so the endpoint
	// invalidates one tag where two were declared.
	resolveEndpointCacheMeta(spec, ep, map[string]any{
		"x-forge-invalidates": []any{"Inventory[]", 7},
	})

	if !warningMentioning(spec, "x-forge-invalidates") {
		t.Fatalf("Warnings = %v, want one naming x-forge-invalidates", spec.Warnings)
	}
}

func TestInvalidatesIsSilentForAValidList(t *testing.T) {
	spec, ep := extensionEndpoint()
	ep.Method = "POST"

	resolveEndpointCacheMeta(spec, ep, map[string]any{
		"x-forge-invalidates": []any{"Inventory[]"},
	})

	if len(spec.Warnings) != 0 {
		t.Fatalf("Warnings = %v, want none for a valid list", spec.Warnings)
	}
}

// routeTableRouter has no OpenAPI document, so Introspect falls back to the raw
// route table. The embedded nil router.Router satisfies the rest of the
// interface; only the three methods Introspect calls are implemented.
type routeTableRouter struct {
	router.Router

	routes []router.RouteInfo
}

func (r routeTableRouter) OpenAPISpec() *router.OpenAPISpec   { return nil }
func (r routeTableRouter) AsyncAPISpec() *router.AsyncAPISpec { return nil }
func (r routeTableRouter) Routes() []router.RouteInfo         { return r.routes }

// A server without OpenAPI still declares its cache contract on its routes,
// and the fallback path used to copy that metadata onto the endpoint and
// resolve none of it: entity nil, tags empty, a generated client that never
// invalidates. The route path has to read the same declarations the
// document path does.
func TestIntrospectResolvesCacheMetaFromRawRoutes(t *testing.T) {
	r := routeTableRouter{routes: []router.RouteInfo{{
		Method: "POST", Path: "/orders",
		Metadata: map[string]any{
			"forge.client.entity":      router.EntityDef{Type: "Order", IDField: "id"},
			"forge.client.invalidates": []string{"Inventory[]"},
		},
	}, {
		Method: "GET", Path: "/orders",
		Metadata: map[string]any{
			"forge.client.entity":    router.EntityDef{Type: "Order", IDField: "id"},
			"forge.client.staleTime": int64(30000),
		},
	}}}

	spec, err := NewIntrospector(r).Introspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(spec.Endpoints) != 2 {
		t.Fatalf("endpoints = %d, want 2", len(spec.Endpoints))
	}

	create := spec.Endpoints[0]
	if create.Entity == nil || create.Entity.Type != "Order" {
		t.Fatalf("POST entity = %+v, want Order", create.Entity)
	}

	if !slices.Equal(create.CacheTags.Invalidates, []string{"Inventory[]", "Order[]"}) {
		t.Fatalf("POST invalidates = %v, want [Inventory[] Order[]]", create.CacheTags.Invalidates)
	}

	list := spec.Endpoints[1]
	if list.StaleTime != 30000 {
		t.Fatalf("GET staleTime = %d, want 30000", list.StaleTime)
	}

	if _, ok := spec.Entities["Order"]; !ok {
		t.Fatalf("entities = %v, want Order registered", spec.Entities)
	}
}

// A declared type that names no component leaves the response with no
// entities-table row to start normalizing from. The tags stay live and the
// cache stores nothing under them, so the only symptom is a warning here.
func TestDeclaredEntityTypeNamingNoComponentWarns(t *testing.T) {
	spec := &APISpec{Schemas: map[string]*Schema{"OrderResponse": orderSchema()}}
	ep := &Endpoint{
		Method: "GET", Path: "/orders/{id}",
		Responses: map[int]*Response{200: {Content: map[string]*MediaType{
			"application/json": {Schema: &Schema{Ref: "#/components/schemas/OrderResponse"}},
		}}},
	}

	resolveEndpointCacheMeta(spec, ep, map[string]any{
		"x-forge-entity": map[string]any{"type": "Order", "idField": "id"},
	})

	if ep.Entity == nil || ep.Entity.Type != "Order" {
		t.Fatalf("Entity = %+v, want the declaration honoured", ep.Entity)
	}

	if len(spec.Warnings) != 1 || !strings.Contains(spec.Warnings[0], `type "Order"`) ||
		!strings.Contains(spec.Warnings[0], `component "OrderResponse"`) {
		t.Fatalf("warnings = %v, want one naming both the declared type and the component", spec.Warnings)
	}
}

// Naming the response's own component, or another component that exists, is
// the ordinary use and says nothing.
func TestDeclaredEntityTypeNamingAComponentIsSilent(t *testing.T) {
	for _, typ := range []string{"Order", "OrderSummary"} {
		spec := &APISpec{Schemas: map[string]*Schema{
			"Order":        orderSchema(),
			"OrderSummary": orderSchema(),
		}}
		ep := &Endpoint{
			Method: "GET", Path: "/orders/{id}",
			Responses: map[int]*Response{200: {Content: map[string]*MediaType{
				"application/json": {Schema: &Schema{Ref: "#/components/schemas/Order"}},
			}}},
		}

		resolveEndpointCacheMeta(spec, ep, map[string]any{
			"x-forge-entity": map[string]any{"type": typ, "idField": "id"},
		})

		if len(spec.Warnings) != 0 {
			t.Fatalf("type %q: warnings = %v, want none", typ, spec.Warnings)
		}
	}
}

// asyncOnlyRouter serves a prepared AsyncAPI document and nothing else.
type asyncOnlyRouter struct {
	router.Router

	async *router.AsyncAPISpec
}

func (r asyncOnlyRouter) OpenAPISpec() *router.OpenAPISpec   { return nil }
func (r asyncOnlyRouter) AsyncAPISpec() *router.AsyncAPISpec { return r.async }
func (r asyncOnlyRouter) Routes() []router.RouteInfo         { return nil }

// The live-router path reads a WebTransport channel exactly as the file path
// does; both go through buildWebTransportEndpoint.
func TestIntrospectReadsAWebTransportChannel(t *testing.T) {
	order := &shared.Schema{Ref: "#/components/schemas/Order"}
	doc := &shared.AsyncAPISpec{
		AsyncAPI: "3.0.0",
		Info:     shared.AsyncAPIInfo{Title: "Orders", Version: "1.0.0"},
		Channels: map[string]*shared.AsyncAPIChannel{
			"wt_orders": {
				Address: "/wt/orders",
				Messages: map[string]*shared.AsyncAPIMessage{
					"datagram":    {Payload: order},
					"bidiSend":    {Payload: order},
					"bidiReceive": {Payload: order},
				},
				Extensions: map[string]any{
					"x-forge-protocol": "webtransport",
					"x-forge-stream": []map[string]any{{
						"message": "order.created", "entityType": "Order", "intent": "upsert",
						"invalidates": []string{"Order[]"},
					}},
				},
			},
		},
		Operations: map[string]*shared.AsyncAPIOperation{
			"ordersSend":    {Action: "send", Channel: &shared.AsyncAPIChannelReference{Ref: "#/channels/wt_orders"}},
			"ordersReceive": {Action: "receive", Channel: &shared.AsyncAPIChannelReference{Ref: "#/channels/wt_orders"}},
		},
		Components: &shared.AsyncAPIComponents{Schemas: map[string]*shared.Schema{
			"Order": {Type: "object", Properties: map[string]*shared.Schema{"id": {Type: "string"}}},
		}},
	}

	spec, err := NewIntrospector(asyncOnlyRouter{async: doc}).Introspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	assertWebTransportEndpoint(t, spec)
}

// A socket channel carries a send and a receive operation. The file parser
// merges the two into one endpoint; the live-router path has to as well, or
// websocket.ts gets two clients for one path.
func TestIntrospectConvertsAWebSocketChannelOnce(t *testing.T) {
	order := &shared.Schema{Ref: "#/components/schemas/Order"}
	doc := &shared.AsyncAPISpec{
		AsyncAPI: "3.0.0",
		Info:     shared.AsyncAPIInfo{Title: "Orders", Version: "1.0.0"},
		Servers: map[string]*shared.AsyncAPIServer{
			"ws": {Host: "localhost", Protocol: "ws"},
		},
		Channels: map[string]*shared.AsyncAPIChannel{
			"ws_orders": {
				Address: "/ws/orders",
				Servers: []shared.AsyncAPIServerReference{{Ref: "#/servers/ws"}},
				Messages: map[string]*shared.AsyncAPIMessage{
					"send":    {Payload: order},
					"receive": {Payload: order},
				},
			},
		},
		Operations: map[string]*shared.AsyncAPIOperation{
			"ordersSend":    {Action: "send", Channel: &shared.AsyncAPIChannelReference{Ref: "#/channels/ws_orders"}},
			"ordersReceive": {Action: "receive", Channel: &shared.AsyncAPIChannelReference{Ref: "#/channels/ws_orders"}},
		},
		Components: &shared.AsyncAPIComponents{Schemas: map[string]*shared.Schema{
			"Order": {Type: "object", Properties: map[string]*shared.Schema{"id": {Type: "string"}}},
		}},
	}

	spec, err := NewIntrospector(asyncOnlyRouter{async: doc}).Introspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(spec.WebSockets) != 1 {
		t.Fatalf("WebSockets = %d, want one endpoint for one channel", len(spec.WebSockets))
	}

	ws := spec.WebSockets[0]
	if ws.SendSchema == nil || ws.ReceiveSchema == nil {
		t.Fatalf("endpoint = %+v, want both directions typed from the two operations", ws)
	}
}

// A multiplexed channel names several messages in one direction. The
// endpoint records each by name, per direction, so the generator can type the
// direction as what it carries rather than as whichever message sorts last.
func TestIntrospectRecordsEachMessagePerDirection(t *testing.T) {
	ref := func(name string) *shared.Schema { return &shared.Schema{Ref: "#/components/schemas/" + name} }
	doc := &shared.AsyncAPISpec{
		AsyncAPI: "3.0.0",
		Info:     shared.AsyncAPIInfo{Title: "Chat", Version: "1.0.0"},
		Servers:  map[string]*shared.AsyncAPIServer{"ws": {Host: "localhost", Protocol: "ws"}},
		Channels: map[string]*shared.AsyncAPIChannel{
			"ws_chat": {
				Address: "/ws/chat",
				Servers: []shared.AsyncAPIServerReference{{Ref: "#/servers/ws"}},
				Messages: map[string]*shared.AsyncAPIMessage{
					"say":   {Payload: ref("Say")},
					"typed": {Payload: ref("Typing")},
					"said":  {Payload: ref("Said")},
				},
			},
		},
		Operations: map[string]*shared.AsyncAPIOperation{
			"chatSend": {Action: "send", Channel: &shared.AsyncAPIChannelReference{Ref: "#/channels/ws_chat"},
				Messages: []shared.AsyncAPIMessageReference{
					{Ref: "#/channels/ws_chat/messages/say"}, {Ref: "#/channels/ws_chat/messages/typed"},
				}},
			"chatReceive": {Action: "receive", Channel: &shared.AsyncAPIChannelReference{Ref: "#/channels/ws_chat"},
				Messages: []shared.AsyncAPIMessageReference{{Ref: "#/channels/ws_chat/messages/said"}}},
		},
		Components: &shared.AsyncAPIComponents{Schemas: map[string]*shared.Schema{
			"Say":    {Type: "object", Properties: map[string]*shared.Schema{"text": {Type: "string"}}},
			"Typing": {Type: "object", Properties: map[string]*shared.Schema{"on": {Type: "boolean"}}},
			"Said":   {Type: "object", Properties: map[string]*shared.Schema{"id": {Type: "string"}}},
		}},
	}

	spec, err := NewIntrospector(asyncOnlyRouter{async: doc}).Introspect(context.Background())
	if err != nil {
		t.Fatal(err)
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

// The URL path reads the same document through the introspector, which used
// to append one endpoint per operation: neither half carried both schemas, so
// a duplex channel reached through a URL source never became a duplex binding
// at all. Both readers must fold the operations the same way.
func TestIntrospectorDuplexChannelNamesEachDirectionFromItsOperation(t *testing.T) {
	raw, err := json.Marshal(duplexAsyncAPIDocument())
	if err != nil {
		t.Fatal(err)
	}

	var asyncAPI shared.AsyncAPISpec
	if err := json.Unmarshal(raw, &asyncAPI); err != nil {
		t.Fatal(err)
	}

	spec := &APISpec{Schemas: map[string]*Schema{}}
	if err := (&Introspector{}).extractFromAsyncAPI(spec, &asyncAPI); err != nil {
		t.Fatalf("extractFromAsyncAPI: %v", err)
	}

	assertDuplexDirections(t, spec)
}

// The same hardening through the URL reader. Both readers share the fold, but
// only this one passes its own spec and operation id into it, and a warning
// that names the wrong operation (or lands on no spec at all) is worth
// nothing. So the component-ref resolution and the warning are checked here
// as well as through the file parser.
func TestIntrospectorFoldResolvesComponentRefsAndWarnsWhenItCannot(t *testing.T) {
	parse := func(t *testing.T, doc map[string]any) *APISpec {
		t.Helper()

		raw, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}

		var asyncAPI shared.AsyncAPISpec
		if err := json.Unmarshal(raw, &asyncAPI); err != nil {
			t.Fatal(err)
		}

		spec := &APISpec{Schemas: map[string]*Schema{}}
		if err := (&Introspector{}).extractFromAsyncAPI(spec, &asyncAPI); err != nil {
			t.Fatalf("extractFromAsyncAPI: %v", err)
		}

		return spec
	}

	pointOperationsAt := func(doc map[string]any, ref func(action string) string) map[string]any {
		operations, _ := doc["operations"].(map[string]any)
		for id, action := range map[string]string{"query.live.wsReceive": "receive", "query.live.wsSend": "send"} {
			operation, _ := operations[id].(map[string]any)
			operation["messages"] = []any{map[string]any{"$ref": ref(action)}}
		}

		return doc
	}

	resolvable := parse(t, pointOperationsAt(duplexAsyncAPIDocument(), func(action string) string {
		return "#/components/messages/" + action
	}))
	assertDuplexDirections(t, resolvable)

	if len(resolvable.Warnings) != 0 {
		t.Errorf("Warnings = %v, want none for refs this fold could resolve", resolvable.Warnings)
	}

	unresolvable := parse(t, pointOperationsAt(duplexAsyncAPIDocument(), func(string) string {
		return "#/components/messages/nothingNamedThis"
	}))

	var named bool

	for _, warning := range unresolvable.Warnings {
		if strings.Contains(warning, "query.live.wsSend") && strings.Contains(warning, "/api/v1/query/live/ws") {
			named = true
		}
	}

	if !named {
		t.Errorf("Warnings = %v, want one naming the operation and the channel", unresolvable.Warnings)
	}
}
