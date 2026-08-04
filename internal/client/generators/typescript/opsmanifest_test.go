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
	out := NewOpsManifestGenerator().Generate(manifestSpec(), client.GeneratorConfig{})

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
	out := NewOpsManifestGenerator().Generate(manifestSpec(), client.GeneratorConfig{})

	if !strings.Contains(out, `Order: { idField: 'id' }`) {
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

func TestOpsManifestEscapesHostileValues(t *testing.T) {
	spec := &client.APISpec{Endpoints: []client.Endpoint{{
		ID: "x", Method: "GET", Path: `/orders'; evil()//`,
	}}}

	out := NewOpsManifestGenerator().Generate(spec, client.GeneratorConfig{})

	if strings.Contains(out, `'/orders'; evil()//'`) {
		t.Fatalf("unescaped quote broke out of the string literal\n\n%s", out)
	}
}
