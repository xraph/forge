package client

import (
	"context"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/shared"
)

// chainSpec is `Order -> Shipment -> Carrier`, where Shipment is not an entity
// and Carrier is. It also carries `Order -> Crate -> Crate`, a non-entity that
// only reaches itself.
func chainSpec() *APISpec {
	return &APISpec{
		Schemas: map[string]*Schema{
			"Order": {Type: "object", Properties: map[string]*Schema{
				"id":       {Type: "string"},
				"shipment": {Ref: "#/components/schemas/Shipment"},
				"crate":    {Ref: "#/components/schemas/Crate"},
			}},
			"Shipment": {Type: "object", Properties: map[string]*Schema{
				"carrier":  {Ref: "#/components/schemas/Carrier"},
				"weightKg": {Type: "number"},
			}},
			"Carrier": {Type: "object", Properties: map[string]*Schema{"id": {Type: "string"}}},
			"Crate": {Type: "object", Properties: map[string]*Schema{
				"inner": {Ref: "#/components/schemas/Crate"},
				"label": {Type: "string"},
			}},
		},
		Entities: map[string]*EntityRef{
			"Order":   {Type: "Order", IDField: "id"},
			"Carrier": {Type: "Carrier", IDField: "id"},
		},
	}
}

// The chain the old pass could not express: an entity reached THROUGH a type
// that is not one.
func TestResolveEntityFieldsWalksThroughNonEntityHops(t *testing.T) {
	spec := chainSpec()

	resolveEntityFields(spec)

	assertFields(t, spec.Entities["Order"].Fields, map[string]string{"shipment": "Shipment"})

	shipment := spec.RoutingTypes["Shipment"]
	if shipment == nil {
		t.Fatalf("RoutingTypes = %v, want a Shipment row", spec.RoutingTypes)
	}

	if shipment.IDField != "" {
		t.Fatalf("Shipment.IDField = %q, want empty: a routing row is never stored", shipment.IDField)
	}

	assertFields(t, shipment.Fields, map[string]string{"carrier": "Carrier"})
}

// The other half of that rule. `Crate` is a non-entity whose only edge is to
// itself, so no entity is reachable through it: no row, and no edge from Order
// pointing at one. A rule that kept every named type it could reach would
// emit both.
func TestResolveEntityFieldsDropsHopsThatReachNoEntity(t *testing.T) {
	spec := chainSpec()

	resolveEntityFields(spec)

	if _, ok := spec.Entities["Order"].Fields["crate"]; ok {
		t.Fatalf("Order.Fields kept an edge to a type reaching no entity: %v", spec.Entities["Order"].Fields)
	}

	if _, ok := spec.RoutingTypes["Crate"]; ok {
		t.Fatalf("RoutingTypes gave a row to a type reaching no entity: %v", spec.RoutingTypes)
	}
}

// Reachability now follows refs, so the pass can revisit a name and the visited
// sets are what make it stop. This is the input that hangs without them: a
// mutual cycle between two non-entities, one of which also reaches an entity,
// plus a self-edge -- all shapes an ordinary bidirectional association
// produces.
//
// It asserts termination by completing. A test that hangs reports as a timeout
// rather than a failure, which is the honest signal here: there is no partial
// wrong answer to compare against.
func TestResolveEntityFieldsTerminatesOnCycles(t *testing.T) {
	spec := &APISpec{
		Schemas: map[string]*Schema{
			"Order": {Type: "object", Properties: map[string]*Schema{
				"id":   {Type: "string"},
				"wrap": {Ref: "#/components/schemas/WrapA"},
				"self": {Ref: "#/components/schemas/Order"},
			}},
			// WrapA <-> WrapB, and WrapB reaches an entity.
			"WrapA": {Type: "object", Properties: map[string]*Schema{
				"b": {Ref: "#/components/schemas/WrapB"},
			}},
			"WrapB": {Type: "object", Properties: map[string]*Schema{
				"a":     {Ref: "#/components/schemas/WrapA"},
				"order": {Ref: "#/components/schemas/Order"},
			}},
			// LoopA <-> LoopB, reaching nothing.
			"LoopA": {Type: "object", Properties: map[string]*Schema{
				"b": {Ref: "#/components/schemas/LoopB"},
			}},
			"LoopB": {Type: "object", Properties: map[string]*Schema{
				"a": {Ref: "#/components/schemas/LoopA"},
			}},
		},
		Entities: map[string]*EntityRef{"Order": {Type: "Order", IDField: "id"}},
	}

	resolveEntityFields(spec)

	assertFields(t, spec.Entities["Order"].Fields, map[string]string{
		"wrap": "WrapA",
		"self": "Order",
	})

	assertFields(t, spec.RoutingTypes["WrapA"].Fields, map[string]string{"b": "WrapB"})
	assertFields(t, spec.RoutingTypes["WrapB"].Fields, map[string]string{
		"a":     "WrapA",
		"order": "Order",
	})

	// A cycle that reaches no entity is still not worth a row. Getting this
	// wrong is how a forward memoized walk fails: it has to answer "is A
	// useful" while A is on its own stack.
	for _, name := range []string{"LoopA", "LoopB"} {
		if _, ok := spec.RoutingTypes[name]; ok {
			t.Fatalf("RoutingTypes gave %s a row; it reaches no entity: %v", name, spec.RoutingTypes)
		}
	}
}

// Running the pass twice must produce the pass's own output, not a merge with
// it. TestResolveEntityFieldsIsIdempotent covers the unchanged-input case; this
// one covers the harder direction, where the second run has to REMOVE a routing
// row rather than rewrite one. spec.RoutingTypes is built by this pass alone,
// so an accumulating map would be a defect visible only on a second call.
func TestResolveEntityFieldsDropsStaleRoutingRows(t *testing.T) {
	spec := chainSpec()

	resolveEntityFields(spec)

	first := spec.RoutingTypes["Shipment"].Fields

	spec.Schemas["Shipment"].Properties = map[string]*Schema{"weightKg": {Type: "number"}}
	resolveEntityFields(spec)

	if _, ok := spec.RoutingTypes["Shipment"]; ok {
		t.Fatalf("Shipment kept a stale routing row after its edges went away: %v (was %v)",
			spec.RoutingTypes, first)
	}

	if got := spec.Entities["Order"].Fields; len(got) != 0 {
		t.Fatalf("Order.Fields = %v, want empty once the hop routes nothing", got)
	}
}

// envelopeOpenAPI is one enveloped list operation, expressed as the router's
// in-memory OpenAPI document.
func envelopeOpenAPI() *shared.OpenAPISpec {
	return &shared.OpenAPISpec{
		OpenAPI: "3.0.3",
		Info:    shared.Info{Title: "Orders", Version: "1.0.0"},
		Paths: map[string]*shared.PathItem{
			"/orders": {Get: &shared.Operation{
				OperationID: "orders.list",
				Responses: map[string]*shared.Response{
					"200": {Content: map[string]*shared.MediaType{
						"application/json": {Schema: &shared.Schema{
							Ref: "#/components/schemas/PageOrder",
						}},
					}},
				},
			}},
		},
		Components: &shared.Components{Schemas: map[string]*shared.Schema{
			"PageOrder": {
				Type:       "object",
				Extensions: map[string]any{envelopeExtension: true},
				Properties: map[string]*shared.Schema{
					"items": {Type: "array", Items: &shared.Schema{
						Ref: "#/components/schemas/Order",
					}},
					"total": {Type: "integer"},
				},
			},
			"Order": {Type: "object", Properties: map[string]*shared.Schema{
				"id": {Type: "string"},
			}},
		}},
	}
}

// The live-router path resolves an envelope exactly as the file path does.
//
// Both builders reach this through the one resolveEndpointCacheMeta, which is
// the point: a live-versus-file divergence in this package's cache metadata has
// been a recurring defect, and the way it gets written is two implementations
// of one rule. Stubbing out either builder's call to that function fails this
// test or its file-side twin in
// internal/client/generators/typescript/e2e_envelope_test.go, and not both.
func TestIntrospectResolvesEnvelopeCacheMeta(t *testing.T) {
	spec, err := NewIntrospector(specOnlyRouter{openAPI: envelopeOpenAPI()}).
		Introspect(context.Background())
	if err != nil {
		t.Fatalf("Introspect: %v", err)
	}

	ep := spec.Endpoints[0]

	if ep.Entity == nil || ep.Entity.Type != "Order" || ep.Entity.IDField != "id" {
		t.Fatalf("Entity = %+v, want Order/id", ep.Entity)
	}

	if ep.RootType != "PageOrder" {
		t.Fatalf("RootType = %q, want PageOrder -- the type the document IS, not the one it carries",
			ep.RootType)
	}

	if got := strings.Join(ep.CacheTags.Provides, ","); got != "Order:{id},Order[]" {
		t.Fatalf("provides = %q, want the item and collection tags a bare array would provide", got)
	}

	assertFields(t, spec.RoutingTypes["PageOrder"].Fields, map[string]string{"items": "Order"})
}

// An opt-out takes the whole response out, root type included. Leaving RootType
// behind would keep the runtime descending into the response and normalizing
// what it found -- the merge into canonical records the opt-out exists to
// prevent, one level down from where it was declared.
func TestResolveEndpointCacheMetaOptOutClearsRootType(t *testing.T) {
	spec := &APISpec{
		Schemas: map[string]*Schema{
			"Order": {Type: "object", Properties: map[string]*Schema{"id": {Type: "string"}}},
		},
	}

	ep := &Endpoint{
		Method: "GET",
		Path:   "/orders/{id}",
		Responses: map[int]*Response{200: {Content: map[string]*MediaType{
			"application/json": {Schema: &Schema{Ref: "#/components/schemas/Order"}},
		}}},
	}

	resolveEndpointCacheMeta(spec, ep, map[string]any{"x-forge-no-entity": true})

	if ep.RootType != "" || ep.Entity != nil {
		t.Fatalf("opt-out left RootType=%q Entity=%+v, want both empty", ep.RootType, ep.Entity)
	}
}

// A declared envelope beats the id-name heuristic on the wrapper itself.
//
// Same precedence InferEntity applies between x-forge-id and that heuristic,
// for the same reason: if a guess can overrule a declaration, reaching for the
// declaration does not work. A wrapper that happens to carry an `id` -- a
// request id, a page token -- is the case this decides.
func TestEnvelopeDeclarationBeatsInferenceOnTheWrapper(t *testing.T) {
	spec := &APISpec{
		Schemas: map[string]*Schema{
			"PageOrder": {
				Type:       "object",
				Extensions: map[string]any{envelopeExtension: true},
				Properties: map[string]*Schema{
					"id":    {Type: "string"},
					"items": {Type: "array", Items: &Schema{Ref: "#/components/schemas/Order"}},
				},
			},
			"Order": {Type: "object", Properties: map[string]*Schema{"id": {Type: "string"}}},
		},
	}

	ep := &Endpoint{
		Method: "GET",
		Path:   "/orders",
		Responses: map[int]*Response{200: {Content: map[string]*MediaType{
			"application/json": {Schema: &Schema{Ref: "#/components/schemas/PageOrder"}},
		}}},
	}

	resolveEndpointCacheMeta(spec, ep, nil)

	if ep.Entity == nil || ep.Entity.Type != "Order" {
		t.Fatalf("Entity = %+v, want Order: the declaration must beat the wrapper's own id", ep.Entity)
	}
}

// `x-forge-envelope: false` is a deliberate "not an envelope", which matters
// when a schema inherits the key from a shared base.
func TestEnvelopeMarkerFalseIsNotAnEnvelope(t *testing.T) {
	spec := &APISpec{
		Schemas: map[string]*Schema{
			"PageOrder": {
				Type:       "object",
				Extensions: map[string]any{envelopeExtension: false},
				Properties: map[string]*Schema{
					"items": {Type: "array", Items: &Schema{Ref: "#/components/schemas/Order"}},
				},
			},
			"Order": {Type: "object", Properties: map[string]*Schema{"id": {Type: "string"}}},
		},
	}

	ep := &Endpoint{
		Method: "GET",
		Path:   "/orders",
		Responses: map[int]*Response{200: {Content: map[string]*MediaType{
			"application/json": {Schema: &Schema{Ref: "#/components/schemas/PageOrder"}},
		}}},
	}

	resolveEndpointCacheMeta(spec, ep, nil)

	if ep.Entity != nil {
		t.Fatalf("Entity = %+v, want none", ep.Entity)
	}

	if len(spec.Warnings) != 0 {
		t.Fatalf("Warnings = %v, want none: declining is not a mistake", spec.Warnings)
	}
}

// A declaration naming a property the schema does not have is a mistake worth
// reporting: as written it silently produces no cache contract, which looks
// exactly like a cache that is simply not very effective.
func TestEnvelopeNamedPropertyMissingWarns(t *testing.T) {
	spec := &APISpec{
		Schemas: map[string]*Schema{
			"PageOrder": {
				Type:       "object",
				Extensions: map[string]any{envelopeExtension: "records"},
				Properties: map[string]*Schema{
					"items": {Type: "array", Items: &Schema{Ref: "#/components/schemas/Order"}},
					"total": {Type: "integer"},
				},
			},
			"Order": {Type: "object", Properties: map[string]*Schema{"id": {Type: "string"}}},
		},
	}

	ep := &Endpoint{
		Method: "GET",
		Path:   "/orders",
		Responses: map[int]*Response{200: {Content: map[string]*MediaType{
			"application/json": {Schema: &Schema{Ref: "#/components/schemas/PageOrder"}},
		}}},
	}

	resolveEndpointCacheMeta(spec, ep, nil)

	if ep.Entity != nil {
		t.Fatalf("Entity = %+v, want none", ep.Entity)
	}

	joined := strings.Join(spec.Warnings, "\n")
	for _, want := range []string{`names property "records"`, "has: items, total"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("warning %q not raised; got: %s", want, joined)
		}
	}
}
