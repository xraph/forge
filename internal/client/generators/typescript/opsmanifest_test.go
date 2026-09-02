// internal/client/generators/typescript/opsmanifest_test.go
package typescript

import (
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
)

func manifestSpec() *client.APISpec {
	return &client.APISpec{
		Endpoints: []client.Endpoint{
			{
				ID: "orderList", Method: "GET", Path: "/orders",
				Entity:    &client.EntityRef{Type: "Order", IDField: "id"},
				CacheTags: client.TagSet{Provides: []string{"Order:{id}", "Order[]"}},
			},
			{
				ID: "orderCreate", Method: "POST", Path: "/orders",
				Entity:    &client.EntityRef{Type: "Order", IDField: "id"},
				CacheTags: client.TagSet{Provides: []string{"Order:{id}"}, Invalidates: []string{"Order[]"}},
			},
		},
		Entities: map[string]*client.EntityRef{
			"Order": {Type: "Order", IDField: "id"},
		},
	}
}

func TestOpsManifestContainsOperations(t *testing.T) {
	out := manifestText(manifestSpec(), client.GeneratorConfig{})

	for _, want := range []string{
		"orderList",
		"orderCreate",
		`method: 'POST'`,
		`path: '/orders'`,
		`provides: ['Order:{id}', 'Order[]']`,
		`invalidates: ['Order[]']`,
		`entity: 'Order'`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("ops.ts missing %q\n\n%s", want, out)
		}
	}
}

func TestOpsManifestContainsEntities(t *testing.T) {
	out := manifestText(manifestSpec(), client.GeneratorConfig{})

	if !strings.Contains(out, `'Order': { idField: 'id' }`) {
		t.Fatalf("ops.ts missing entity table\n\n%s", out)
	}
}

// Generated output is diffed by CI; map iteration must not reach the file.
func TestOpsManifestIsDeterministic(t *testing.T) {
	gen := NewOpsManifestGenerator()

	first := gen.Generate(manifestSpec(), client.GeneratorConfig{})
	for i := 0; i < 50; i++ {
		if got := gen.Generate(manifestSpec(), client.GeneratorConfig{}); got != first {
			t.Fatal("ops.ts differs between runs: a map is being iterated unsorted")
		}
	}
}

// fieldMapSpec is manifestSpec plus the property-to-typename edges the
// resolver fills in. `Order` reaches a Customer directly and a list of
// LineItems through an array; `LineItem` reaches nothing.
func fieldMapSpec() *client.APISpec {
	spec := manifestSpec()
	spec.Entities = map[string]*client.EntityRef{
		"Order": {Type: "Order", IDField: "id", Fields: map[string]string{
			"customer": "Customer",
			"items":    "LineItem",
			"parent":   "Order",
		}},
		"Customer": {Type: "Customer", IDField: "id"},
		"LineItem": {Type: "LineItem", IDField: "sku"},
	}

	return spec
}

func TestOpsManifestEmitsFieldMap(t *testing.T) {
	out := manifestText(fieldMapSpec(), client.GeneratorConfig{})

	want := `'Order': { idField: 'id', fields: { 'customer': 'Customer', 'items': 'LineItem', 'parent': 'Order' } }`
	if !strings.Contains(out, want) {
		t.Fatalf("ops.ts missing %q\n\n%s", want, out)
	}

	// The runtime's EntityMeta types `fields` as optional; an entity with no
	// entity-typed property must not carry an empty object.
	if !strings.Contains(out, `'LineItem': { idField: 'sku' },`) {
		t.Fatalf("ops.ts did not omit an empty field map\n\n%s", out)
	}

	if strings.Contains(out, "fields: {  }") || strings.Contains(out, "fields: {}") {
		t.Fatalf("ops.ts emitted an empty field map\n\n%s", out)
	}
}

// The declared interface has to admit the property the table now carries, or
// the `satisfies` clause fails to compile in the consuming repository.
func TestOpsManifestDeclaresFieldsOnEntityMeta(t *testing.T) {
	out := manifestText(fieldMapSpec(), client.GeneratorConfig{})

	if !strings.Contains(out, "readonly fields?: Readonly<Record<string, string>>;") {
		t.Fatalf("ops.ts EntityMeta does not declare fields\n\n%s", out)
	}
}

// EntityRef.Fields is a Go map, and this file is byte-diffed by CI: an
// unsorted walk over it reports a change on every regeneration.
func TestOpsManifestFieldMapIsDeterministic(t *testing.T) {
	gen := NewOpsManifestGenerator()

	first := gen.Generate(fieldMapSpec(), client.GeneratorConfig{})
	for i := 0; i < 50; i++ {
		if got := gen.Generate(fieldMapSpec(), client.GeneratorConfig{}); got != first {
			t.Fatal("ops.ts differs between runs: EntityRef.Fields is being iterated unsorted")
		}
	}
}

func TestOpsManifestEscapesHostileValues(t *testing.T) {
	spec := &client.APISpec{Endpoints: []client.Endpoint{{
		ID: "x", Method: "GET", Path: `/orders'; evil()//`,
	}}}

	out := manifestText(spec, client.GeneratorConfig{})

	if strings.Contains(out, `'/orders'; evil()//'`) {
		t.Fatalf("unescaped quote broke out of the string literal\n\n%s", out)
	}
}
