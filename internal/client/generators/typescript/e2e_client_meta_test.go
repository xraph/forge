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
		// orderList additionally declares x-forge-stale-time, so this fixture
		// also proves staleTime reaches ops.ts end to end: through the same
		// extension parsing every other x-forge-* field here goes through,
		// not a fixture built to carry Endpoint.StaleTime directly.
		var ext map[string]any
		if spec.Endpoints[i].ID == "orderList" {
			ext = map[string]any{"x-forge-stale-time": 30000}
		}

		client.ResolveEndpointCacheMeta(spec, &spec.Endpoints[i], ext)
	}

	cfg := baseConfig()
	cfg.Hooks = true

	out, err := (&Generator{}).Generate(context.Background(), spec, cfg)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	_, ok := out.Files["src/ops.ts"]
	opsFile := ClientManifestText(out.Files)
	if !ok {
		t.Fatal("src/ops.ts was not generated")
	}

	for _, want := range []string{
		`entity: 'Order'`,
		`provides: ['Order:{id}', 'Order[]']`,
		`invalidates: ['Order[]']`,
		`'Order': { idField: 'id' }`,
		`staleTime: 30000,`,
	} {
		if !strings.Contains(opsFile, want) {
			t.Fatalf("ops.ts missing %q\n\n%s", want, opsFile)
		}
	}

	hooks := ClientHooksText(out.Files)
	if !strings.Contains(hooks, "export const useOrderList = /*#__PURE__*/ query<Order>(op_orderList);") {
		t.Fatalf("hooks.ts missing the list hook\n\n%s", hooks)
	}

	if _, present := out.Files["src/query.ts"]; present {
		t.Fatal("src/query.ts is still being generated; the TanStack layer was not retired")
	}
}
