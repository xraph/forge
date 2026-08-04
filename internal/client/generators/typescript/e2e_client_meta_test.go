// internal/client/generators/typescript/e2e_client_meta_test.go
package typescript

import (
	"context"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
)

// orderSchema is declared here as well as in package client's tests: test
// helpers do not cross package boundaries, and duplicating four lines beats
// exporting a fixture from production code.
func orderSchema() *client.Schema {
	return &client.Schema{Type: "object", Properties: map[string]*client.Schema{
		"id":    {Type: "string"},
		"total": {Type: "integer"},
	}}
}

// TestGenerationCarriesEntityMetaEndToEnd drives a spec with no explicit
// declarations at all and asserts the zero-config promise: normalization and
// correct same-entity invalidation with nothing annotated.
func TestGenerationCarriesEntityMetaEndToEnd(t *testing.T) {
	orderRef := &client.Schema{Ref: "#/components/schemas/Order"}

	spec := &client.APISpec{
		Info:    client.APIInfo{Title: "Orders", Version: "1.0.0"},
		Schemas: map[string]*client.Schema{"Order": orderSchema()},
		Endpoints: []client.Endpoint{
			{
				ID: "orderList", Method: "GET", Path: "/orders",
				Responses: map[int]*client.Response{200: {Content: map[string]*client.MediaType{
					"application/json": {Schema: &client.Schema{Type: "array", Items: orderRef}},
				}}},
			},
			{
				ID: "orderCreate", Method: "POST", Path: "/orders",
				Responses: map[int]*client.Response{201: {Content: map[string]*client.MediaType{
					"application/json": {Schema: orderRef},
				}}},
			},
		},
	}

	for i := range spec.Endpoints {
		client.ResolveEndpointCacheMeta(spec, &spec.Endpoints[i], nil)
	}

	cfg := baseConfig()
	cfg.Hooks = true

	out, err := (&Generator{}).Generate(context.Background(), spec, cfg)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	opsFile, ok := out.Files["src/ops.ts"]
	if !ok {
		t.Fatal("src/ops.ts was not generated")
	}

	for _, want := range []string{
		`entity: 'Order'`,
		`provides: ['Order:{id}', 'Order[]']`,
		`invalidates: ['Order[]']`,
		`Order: { idField: 'id' }`,
	} {
		if !strings.Contains(opsFile, want) {
			t.Fatalf("ops.ts missing %q\n\n%s", want, opsFile)
		}
	}

	hooks := out.Files["src/hooks.ts"]
	if !strings.Contains(hooks, "export const useOrderList = query(ops.orderList);") {
		t.Fatalf("hooks.ts missing the list hook\n\n%s", hooks)
	}

	if _, present := out.Files["src/query.ts"]; present {
		t.Fatal("src/query.ts is still being generated; the TanStack layer was not retired")
	}
}
