package client

import (
	"strings"
	"testing"
)

// inferSpec builds a spec whose only wrapper is `Wrapper`, with the properties
// given, plus an `Order` entity and a `Customer` entity to point at.
func inferSpec(wrapper map[string]*Schema, ext map[string]any) *APISpec {
	return &APISpec{
		Schemas: map[string]*Schema{
			"Wrapper":  {Type: "object", Properties: wrapper, Extensions: ext},
			"Order":    {Type: "object", Properties: map[string]*Schema{"id": {Type: "string"}}},
			"Customer": {Type: "object", Properties: map[string]*Schema{"id": {Type: "string"}}},
			// Two identity-shaped fields: InferEntity refuses, so nothing that
			// points at this may be tagged.
			"Row": {Type: "object", Properties: map[string]*Schema{
				"id": {Type: "string"}, "uuid": {Type: "string",
					Extensions: map[string]any{"x-forge-id": true}},
				"other": {Type: "string", Extensions: map[string]any{"x-forge-id": true}},
			}},
		},
	}
}

func inferEP(method string) *Endpoint {
	return &Endpoint{
		Method: method,
		Path:   "/things",
		Responses: map[int]*Response{200: {Content: map[string]*MediaType{
			"application/json": {Schema: &Schema{Ref: "#/components/schemas/Wrapper"}},
		}}},
	}
}

func arrayOf(name string) *Schema {
	return &Schema{Type: "array", Items: &Schema{Ref: "#/components/schemas/" + name}}
}

// The headline case. A list read returning an undeclared wrapper around
// `[]Order` gets the contract a bare `[]Order` would, so a mutation declaring
// `invalidates: ['Order[]']` reaches it.
func TestInferredCollectionEnvelopeProvidesCollectionTag(t *testing.T) {
	spec := inferSpec(map[string]*Schema{
		"orders": arrayOf("Order"),
		"total":  {Type: "integer"},
	}, nil)

	ep := inferEP("GET")
	resolveEndpointCacheMeta(spec, ep, nil)

	if ep.Entity == nil || ep.Entity.Type != "Order" || ep.Entity.IDField != "id" {
		t.Fatalf("Entity = %+v, want Order/id", ep.Entity)
	}

	if ep.RootType != "Wrapper" {
		t.Fatalf("RootType = %q, want Wrapper: the entity is what the document carries,"+
			" the root type is what it IS", ep.RootType)
	}

	if got := strings.Join(ep.CacheTags.Provides, ","); got != "Order:{id},Order[]" {
		t.Fatalf("provides = %q, want exactly what a bare []Order provides", got)
	}
}

// An entity-typed property that is NOT an array does not make the response a
// collection read, and must not make it an item read either. A login callback
// carrying the signed-in user is the real shape this decides: tagging it
// `Order:{id}` would wire every write of that type to an operation that is not
// a read of it.
func TestInferredEnvelopeIgnoresNonArrayEntityProperty(t *testing.T) {
	spec := inferSpec(map[string]*Schema{
		"customer":  {Ref: "#/components/schemas/Customer"},
		"expiresAt": {Type: "string"},
	}, nil)

	ep := inferEP("GET")
	resolveEndpointCacheMeta(spec, ep, nil)

	if ep.Entity != nil {
		t.Fatalf("Entity = %+v, want nil: a wrapper carrying one record is not a collection read",
			ep.Entity)
	}
}

// Two array-of-entity properties are ambiguous. Which collection the response
// IS cannot be read off the shape, and picking one asserts a membership claim
// nobody wrote. Same refusal soleEntityProperty makes on the declared path.
func TestInferredEnvelopeRefusesTwoCollections(t *testing.T) {
	spec := inferSpec(map[string]*Schema{
		"orders":    arrayOf("Order"),
		"customers": arrayOf("Customer"),
	}, nil)

	ep := inferEP("GET")
	resolveEndpointCacheMeta(spec, ep, nil)

	if ep.Entity != nil {
		t.Fatalf("Entity = %+v, want nil on two candidate collections", ep.Entity)
	}
}

// The element type has to be an entity in its own right. Inference is widened
// to reach THROUGH a collection; it is not widened to guess identity, so a
// type whose identity is ambiguous stays untagged exactly as it does today.
func TestInferredEnvelopeRefusesUnidentifiableElement(t *testing.T) {
	spec := inferSpec(map[string]*Schema{"rows": arrayOf("Row")}, nil)

	ep := inferEP("GET")
	resolveEndpointCacheMeta(spec, ep, nil)

	if ep.Entity != nil {
		t.Fatalf("Entity = %+v, want nil: Row has two declared identity fields", ep.Entity)
	}
}

// `x-forge-envelope: false` is a deliberate "this is not an envelope". If
// inference overruled it the opt-out would not work, which is the same reason
// a declaration beats the id-name heuristic.
func TestInferredEnvelopeRespectsExplicitFalse(t *testing.T) {
	spec := inferSpec(
		map[string]*Schema{"orders": arrayOf("Order")},
		map[string]any{envelopeExtension: false},
	)

	ep := inferEP("GET")
	resolveEndpointCacheMeta(spec, ep, nil)

	if ep.Entity != nil {
		t.Fatalf("Entity = %+v, want nil: the schema declared it is not an envelope", ep.Entity)
	}
}

// A declared envelope that FAILED to resolve already warned. Falling through to
// inference would answer a question the developer asked explicitly and got a
// diagnostic for, so the shape rule stays out of it.
func TestInferredEnvelopeDoesNotRescueFailedDeclaration(t *testing.T) {
	spec := inferSpec(
		map[string]*Schema{"orders": arrayOf("Order")},
		map[string]any{envelopeExtension: "nosuchprop"},
	)

	ep := inferEP("GET")
	resolveEndpointCacheMeta(spec, ep, nil)

	if ep.Entity != nil {
		t.Fatalf("Entity = %+v, want nil: the declaration named a missing property and warned",
			ep.Entity)
	}

	if len(spec.Warnings) == 0 {
		t.Fatal("want the declared-path warning to survive")
	}
}

// Inference produces READ tags only. On a POST the same shape would derive
// `invalidates: ['Order[]']`, turning a search that happens to return
// `{results: []Order}` into a write that evicts every order list. A human can
// still declare it; shape alone may not.
func TestInferredEnvelopeIsReadOnly(t *testing.T) {
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		spec := inferSpec(map[string]*Schema{"results": arrayOf("Order")}, nil)

		ep := inferEP(method)
		resolveEndpointCacheMeta(spec, ep, nil)

		if ep.Entity != nil {
			t.Fatalf("%s: Entity = %+v, want nil: inference must not derive an invalidation",
				method, ep.Entity)
		}
	}
}

// HEAD reads the same document GET does.
func TestInferredEnvelopeAppliesToHEAD(t *testing.T) {
	spec := inferSpec(map[string]*Schema{"orders": arrayOf("Order")}, nil)

	ep := inferEP("HEAD")
	resolveEndpointCacheMeta(spec, ep, nil)

	if ep.Entity == nil || ep.Entity.Type != "Order" {
		t.Fatalf("Entity = %+v, want Order", ep.Entity)
	}
}

// A wrapper that is itself an entity stays an item read. Inference through a
// collection is the LAST tier, so it never demotes a record to the list it
// happens to embed.
func TestWrapperThatIsItselfAnEntityWins(t *testing.T) {
	spec := inferSpec(map[string]*Schema{
		"id":    {Type: "string"},
		"lines": arrayOf("Order"),
	}, nil)

	ep := inferEP("GET")
	resolveEndpointCacheMeta(spec, ep, nil)

	if ep.Entity == nil || ep.Entity.Type != "Wrapper" {
		t.Fatalf("Entity = %+v, want Wrapper: the response IS a record", ep.Entity)
	}

	if got := strings.Join(ep.CacheTags.Provides, ","); got != "Wrapper:{id}" {
		t.Fatalf("provides = %q, want the item tag only", got)
	}
}

// The route-level opt-out still removes the false edge inference can add. This
// is the escape for the report-over-a-collection shape: `{topOrders: []Order}`
// is indistinguishable from a page, so the endpoint that is not a view of the
// collection says so.
func TestInferredEnvelopeYieldsToRouteOptOut(t *testing.T) {
	spec := inferSpec(map[string]*Schema{"topOrders": arrayOf("Order")}, nil)

	ep := inferEP("GET")
	resolveEndpointCacheMeta(spec, ep, map[string]any{"x-forge-no-entity": true})

	if ep.Entity != nil || ep.RootType != "" {
		t.Fatalf("opt-out left Entity=%+v RootType=%q", ep.Entity, ep.RootType)
	}
}
