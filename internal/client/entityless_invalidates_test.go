package client

import (
	"reflect"
	"testing"
)

// A write whose response carries no entity can still have effects on entities
// the client has cached. WithInvalidates declares exactly those edges, and
// nothing in its contract says the operation's own response has to be one.
// These pin the cases that used to drop the declaration without a word.

func TestEntitylessDeleteKeepsDeclaredInvalidates(t *testing.T) {
	spec := &APISpec{}

	// The tap shape: a DELETE answering 204 with no body at all.
	ep := &Endpoint{
		Method: "DELETE", Path: "/rules/{id}",
		Responses: map[int]*Response{204: {Description: "deleted"}},
	}

	resolveEndpointCacheMeta(spec, ep, map[string]any{
		"x-forge-invalidates": []any{"Rule[]"},
	})

	if ep.Entity != nil {
		t.Fatalf("Entity = %+v, want nil: a 204 carries nothing to normalize", ep.Entity)
	}

	want := TagSet{Invalidates: []string{"Rule[]"}}
	if !reflect.DeepEqual(ep.CacheTags, want) {
		t.Fatalf("CacheTags = %+v, want %+v", ep.CacheTags, want)
	}
}

func TestEntitylessPostKeepsDeclaredInvalidates(t *testing.T) {
	// A response that is a document but not a record: no identity field, so
	// inference rightly finds no entity in it.
	spec := &APISpec{Schemas: map[string]*Schema{
		"RedirectTo": {Type: "object", Properties: map[string]*Schema{
			"redirect_to": {Type: "string"},
		}},
	}}
	ep := &Endpoint{
		Method: "POST", Path: "/api/v1/oauth/requests/{id}/approve",
		Responses: map[int]*Response{200: {Content: map[string]*MediaType{
			"application/json": {Schema: &Schema{Ref: "#/components/schemas/RedirectTo"}},
		}}},
	}

	resolveEndpointCacheMeta(spec, ep, map[string]any{
		"x-forge-invalidates": []any{"Grant[]"},
	})

	if ep.Entity != nil {
		t.Fatalf("Entity = %+v, want nil", ep.Entity)
	}

	want := TagSet{Invalidates: []string{"Grant[]"}}
	if !reflect.DeepEqual(ep.CacheTags, want) {
		t.Fatalf("CacheTags = %+v, want %+v", ep.CacheTags, want)
	}
}

func TestNoEntityKeepsDeclaredInvalidatesButNotDerivedTags(t *testing.T) {
	// The response IS an Order, and WithoutEntity takes it out of the cache.
	// That removes everything derived from the response (provides, the
	// Order[] invalidation, the root type) and nothing the author declared
	// about what the write does elsewhere.
	spec, ep := extensionEndpoint()
	ep.Method = "POST"

	resolveEndpointCacheMeta(spec, ep, map[string]any{
		"x-forge-no-entity":   true,
		"x-forge-invalidates": []any{"Inventory[]"},
	})

	if ep.Entity != nil || ep.RootType != "" {
		t.Fatalf("Entity = %+v, RootType = %q, want both empty", ep.Entity, ep.RootType)
	}

	want := TagSet{Invalidates: []string{"Inventory[]"}}
	if !reflect.DeepEqual(ep.CacheTags, want) {
		t.Fatalf("CacheTags = %+v, want %+v", ep.CacheTags, want)
	}
}

func TestMalformedDeclaredTagWarnsAndIsDropped(t *testing.T) {
	spec := &APISpec{}
	ep := &Endpoint{
		Method: "DELETE", Path: "/rules/{id}",
		Responses: map[int]*Response{204: {Description: "deleted"}},
	}

	// Each of these is a tag the runtime can never match: an empty key, a
	// brace that never closes (substituted as literal text), and a placeholder
	// that names nothing. `Order[]:{req.archived}` is unusual and legal.
	resolveEndpointCacheMeta(spec, ep, map[string]any{
		"x-forge-invalidates": []any{
			"", "Customer:{req.customerId", "Order:{}", "Rule[]", "Order[]:{req.archived}",
		},
	})

	want := TagSet{Invalidates: []string{"Order[]:{req.archived}", "Rule[]"}}
	if !reflect.DeepEqual(ep.CacheTags, want) {
		t.Fatalf("CacheTags = %+v, want %+v", ep.CacheTags, want)
	}

	for _, needle := range []string{`""`, `"Customer:{req.customerId"`, `"Order:{}"`} {
		if !warningMentioning(spec, needle) {
			t.Errorf("Warnings = %v, want one naming %s", spec.Warnings, needle)
		}
	}

	if warningMentioning(spec, "Order[]:{req.archived}") || warningMentioning(spec, `"Rule[]"`) {
		t.Errorf("Warnings = %v, want none for a well-formed tag", spec.Warnings)
	}
}
