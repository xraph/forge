package client

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/xraph/forge/internal/router"
	"github.com/xraph/forge/internal/shared"
)

// entityFieldsSpec builds a spec whose Order type reaches every shape the
// resolver has to decide about at once: a direct $ref to another entity, an
// array of them, a $ref to a named type that is NOT an entity, a self edge,
// and an inline (anonymous) object.
func entityFieldsSpec() *APISpec {
	return &APISpec{
		Schemas: map[string]*Schema{
			"Order": {Type: "object", Properties: map[string]*Schema{
				"id":       {Type: "string"},
				"customer": {Ref: "#/components/schemas/Customer"},
				"items": {Type: "array", Items: &Schema{
					Ref: "#/components/schemas/LineItem",
				}},
				"status": {Ref: "#/components/schemas/OrderStatus"},
				"parent": {Ref: "#/components/schemas/Order"},
				"audit":  {Type: "object", Properties: map[string]*Schema{"by": {Type: "string"}}},
				"total":  {Type: "integer"},
			}},
			"Customer":    {Type: "object", Properties: map[string]*Schema{"id": {Type: "string"}}},
			"LineItem":    {Type: "object", Properties: map[string]*Schema{"id": {Type: "string"}}},
			"OrderStatus": {Type: "string", Enum: []any{"open", "closed"}},
		},
		Entities: map[string]*EntityRef{
			"Order":    {Type: "Order", IDField: "id"},
			"Customer": {Type: "Customer", IDField: "id"},
			"LineItem": {Type: "LineItem", IDField: "id"},
		},
	}
}

func assertFields(t *testing.T, got map[string]string, want map[string]string) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("Fields = %v, want %v", got, want)
	}

	for prop, typ := range want {
		if got[prop] != typ {
			t.Fatalf("Fields[%q] = %q, want %q (whole map %v)", prop, got[prop], typ, got)
		}
	}
}

// A nested entity of a DIFFERENT type produces an edge, and an array of
// entities records the ELEMENT typename -- not an array marker, because the
// runtime carries a typename through an array unchanged.
func TestResolveEntityFieldsRecordsRefAndArrayEdges(t *testing.T) {
	spec := entityFieldsSpec()

	resolveEntityFields(spec)

	assertFields(t, spec.Entities["Order"].Fields, map[string]string{
		"customer": "Customer",
		"items":    "LineItem",
		"parent":   "Order",
	})
}

// A property whose type is a named non-entity with NOTHING BENEATH IT (here an
// enum) records no edge.
//
// The decision, and why: the runtime's only use for an edge is to look the
// target up in this same table and descend. A type from which no entity is
// reachable has nothing worth descending to, so it gets no entry, no edge
// pointing at it, and no bytes in a file CI byte-diffs.
//
// The rule is reachability, not entity-ness -- a non-entity WITH an entity
// beneath it does get both, which is what closes the Order -> Shipment ->
// Carrier chain; see TestResolveEntityFieldsWalksThroughNonEntityHops.
func TestResolveEntityFieldsSkipsNonEntityNamedTypes(t *testing.T) {
	spec := entityFieldsSpec()

	resolveEntityFields(spec)

	if typ, ok := spec.Entities["Order"].Fields["status"]; ok {
		t.Fatalf("Fields[status] = %q, want no edge to the non-entity OrderStatus", typ)
	}
}

// An inline object has no component name, so it cannot be an edge target: a
// cache key needs a stable typename and an anonymous struct has none.
func TestResolveEntityFieldsSkipsInlineObjects(t *testing.T) {
	spec := entityFieldsSpec()

	resolveEntityFields(spec)

	if typ, ok := spec.Entities["Order"].Fields["audit"]; ok {
		t.Fatalf("Fields[audit] = %q, want no edge for an inline object", typ)
	}
}

// An entity with no entity-typed property gets a nil map, not an empty one:
// the manifest emits `fields` only when it is non-empty.
func TestResolveEntityFieldsLeavesLeafEntitiesNil(t *testing.T) {
	spec := entityFieldsSpec()

	resolveEntityFields(spec)

	if got := spec.Entities["LineItem"].Fields; got != nil {
		t.Fatalf("LineItem.Fields = %v, want nil", got)
	}
}

func TestResolveEntityFieldsHandlesRefShapes(t *testing.T) {
	tests := []struct {
		name   string
		schema *Schema
		want   string
	}{
		{
			name:   "direct ref",
			schema: &Schema{Ref: "#/components/schemas/Customer"},
			want:   "Customer",
		},
		{
			name:   "nullable ref (OpenAPI 3.0 spelling)",
			schema: &Schema{Ref: "#/components/schemas/Customer", Nullable: true},
			want:   "Customer",
		},
		{
			name: "array of refs records the element type",
			schema: &Schema{Type: "array", Items: &Schema{
				Ref: "#/components/schemas/Customer",
			}},
			want: "Customer",
		},
		{
			name: "array of arrays of refs",
			schema: &Schema{Type: "array", Items: &Schema{Type: "array", Items: &Schema{
				Ref: "#/components/schemas/Customer",
			}}},
			want: "Customer",
		},
		{
			name: "oneOf-wrapped nullable ref",
			schema: &Schema{OneOf: []*Schema{
				{Ref: "#/components/schemas/Customer"},
				{Type: "null"},
			}},
			want: "Customer",
		},
		{
			name: "anyOf-wrapped nullable ref",
			schema: &Schema{AnyOf: []*Schema{
				{Type: "null"},
				{Ref: "#/components/schemas/Customer"},
			}},
			want: "Customer",
		},
		{
			name:   "allOf with a single ref member",
			schema: &Schema{AllOf: []*Schema{{Ref: "#/components/schemas/Customer"}}},
			want:   "Customer",
		},
		{
			name: "array whose items are a nullable oneOf ref",
			schema: &Schema{Type: "array", Items: &Schema{OneOf: []*Schema{
				{Ref: "#/components/schemas/Customer"},
				{Type: "null"},
			}}},
			want: "Customer",
		},
		{
			name: "oneOf naming two different types refuses",
			schema: &Schema{OneOf: []*Schema{
				{Ref: "#/components/schemas/Customer"},
				{Ref: "#/components/schemas/LineItem"},
			}},
			want: "",
		},
		{
			name: "oneOf with an unnamed member refuses",
			schema: &Schema{OneOf: []*Schema{
				{Ref: "#/components/schemas/Customer"},
				{Type: "string"},
			}},
			want: "",
		},
		{
			name:   "plain scalar",
			schema: &Schema{Type: "string"},
			want:   "",
		},
		{
			name:   "nil",
			schema: nil,
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := namedSchemaTarget(tt.schema, 0); got != tt.want {
				t.Fatalf("namedSchemaTarget = %q, want %q", got, tt.want)
			}
		})
	}
}

// A hand-built spec whose schemas point back at each other must refuse rather
// than overflow the stack. No parser produces this -- both convertSchema
// implementations build a finite tree -- but nothing in the type system stops
// an in-memory builder from doing so.
func TestNamedSchemaTargetTerminatesOnSelfReferentialInlineSchema(t *testing.T) {
	loop := &Schema{Type: "array"}
	loop.Items = loop

	if got := namedSchemaTarget(loop, 0); got != "" {
		t.Fatalf("namedSchemaTarget = %q, want \"\"", got)
	}
}

// An entity naming a type no component schema describes (a declared
// x-forge-entity, or a stream binding for an undefined type) yields no edges
// and does not panic.
func TestResolveEntityFieldsToleratesMissingSchema(t *testing.T) {
	spec := &APISpec{
		Schemas:  map[string]*Schema{},
		Entities: map[string]*EntityRef{"Ghost": {Type: "Ghost", IDField: "id"}},
	}

	resolveEntityFields(spec)

	if got := spec.Entities["Ghost"].Fields; got != nil {
		t.Fatalf("Ghost.Fields = %v, want nil", got)
	}
}

// Running the pass twice replaces rather than accumulates, so a spec that
// passes through it more than once is unchanged by the second visit.
func TestResolveEntityFieldsIsIdempotent(t *testing.T) {
	spec := entityFieldsSpec()

	resolveEntityFields(spec)
	first := spec.Entities["Order"].Fields

	resolveEntityFields(spec)

	assertFields(t, spec.Entities["Order"].Fields, first)
}

// --- Wiring: the file path --------------------------------------------

const entityFieldsSpecFile = `{
  "openapi": "3.0.3",
  "info": { "title": "Orders", "version": "1.0.0" },
  "paths": {
    "/orders/{id}": {
      "get": {
        "operationId": "orders.get",
        "responses": { "200": { "description": "ok", "content": { "application/json": {
          "schema": { "$ref": "#/components/schemas/Order" } } } } }
      }
    },
    "/customers/{id}": {
      "get": {
        "operationId": "customers.get",
        "responses": { "200": { "description": "ok", "content": { "application/json": {
          "schema": { "$ref": "#/components/schemas/Customer" } } } } }
      }
    }
  },
  "components": {
    "schemas": {
      "Order": {
        "type": "object",
        "properties": {
          "id": { "type": "string" },
          "customer": { "$ref": "#/components/schemas/Customer" }
        }
      },
      "Customer": {
        "type": "object",
        "properties": { "id": { "type": "string" } }
      }
    }
  }
}`

// SpecParser.ParseFile wires the pass. Note that `Customer` is registered by
// an operation the walk reaches AFTER `/orders/{id}` (paths are walked
// sorted, so /customers precedes /orders here -- reverse the two and the
// point still holds), which is exactly why this cannot live inside
// per-endpoint resolution.
func TestParseFileWiresEntityFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "openapi.json")

	if err := os.WriteFile(path, []byte(entityFieldsSpecFile), 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	spec, err := NewSpecParser().ParseFile(context.Background(), path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	order := spec.Entities["Order"]
	if order == nil {
		t.Fatalf("Entities = %v, want Order", spec.Entities)
	}

	assertFields(t, order.Fields, map[string]string{"customer": "Customer"})
}

// --- Wiring: the live-router path -------------------------------------

// specOnlyRouter serves a prepared OpenAPI document and nothing else.
//
// The embedded nil router.Router satisfies the rest of a large interface;
// Introspect calls only the two spec accessors (and Routes(), only when there
// is no OpenAPI document), so no nil method is ever reached.
type specOnlyRouter struct {
	router.Router

	openAPI *router.OpenAPISpec
}

func (r specOnlyRouter) OpenAPISpec() *router.OpenAPISpec { return r.openAPI }

func (r specOnlyRouter) AsyncAPISpec() *router.AsyncAPISpec { return nil }

// Introspector.Introspect wires the same pass for a live router. Live and file
// have diverged in this package before; both call resolveEntityFields, so
// this test and TestParseFileWiresEntityFields assert the same edge.
func TestIntrospectWiresEntityFields(t *testing.T) {
	jsonResponse := func(ref string) map[string]*shared.Response {
		return map[string]*shared.Response{
			"200": {Content: map[string]*shared.MediaType{
				"application/json": {Schema: &shared.Schema{Ref: ref}},
			}},
		}
	}

	openAPI := &shared.OpenAPISpec{
		OpenAPI: "3.0.3",
		Info:    shared.Info{Title: "Orders", Version: "1.0.0"},
		Paths: map[string]*shared.PathItem{
			"/orders/{id}": {Get: &shared.Operation{
				OperationID: "orders.get",
				Responses:   jsonResponse("#/components/schemas/Order"),
			}},
			"/customers/{id}": {Get: &shared.Operation{
				OperationID: "customers.get",
				Responses:   jsonResponse("#/components/schemas/Customer"),
			}},
		},
		Components: &shared.Components{Schemas: map[string]*shared.Schema{
			"Order": {Type: "object", Properties: map[string]*shared.Schema{
				"id":       {Type: "string"},
				"customer": {Ref: "#/components/schemas/Customer"},
				"items": {Type: "array", Items: &shared.Schema{
					Ref: "#/components/schemas/LineItem",
				}},
			}},
			"Customer": {Type: "object", Properties: map[string]*shared.Schema{
				"id": {Type: "string"},
			}},
			"LineItem": {Type: "object", Properties: map[string]*shared.Schema{
				"id": {Type: "string"},
			}},
		}},
	}

	spec, err := NewIntrospector(specOnlyRouter{openAPI: openAPI}).Introspect(context.Background())
	if err != nil {
		t.Fatalf("Introspect: %v", err)
	}

	order := spec.Entities["Order"]
	if order == nil {
		t.Fatalf("Entities = %v, want Order", spec.Entities)
	}

	// `items` records LineItem only if LineItem is itself an entity; no
	// endpoint returns one here, so it is absent from the table and the edge
	// is not recorded. That is the documented rule, asserted from the live
	// path too.
	assertFields(t, order.Fields, map[string]string{"customer": "Customer"})
}
