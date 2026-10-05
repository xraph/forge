package forge_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/client"
)

// TestWithIdempotencyRoundTripsToTheClientIR proves that what this package
// emits is what the client spec parser reads: a route option becomes an
// OpenAPI extension becomes Endpoint.Idempotent, and a route without the
// option stays false.
func TestWithIdempotencyRoundTripsToTheClientIR(t *testing.T) {
	r := forge.NewRouter(forge.WithOpenAPI(forge.OpenAPIConfig{Title: "Orders", Version: "1.0.0", SpecEnabled: true}))

	type order struct {
		ID string `json:"id"`
	}

	ok := func(ctx forge.Context) error { return ctx.JSON(http.StatusCreated, order{ID: "7"}) }

	if err := r.POST("/orders", ok, forge.WithOperationID("orderCreate"), forge.WithIdempotency()); err != nil {
		t.Fatal(err)
	}

	if err := r.POST("/orders/{id}/notes", ok, forge.WithOperationID("orderNote")); err != nil {
		t.Fatal(err)
	}

	// A group mark reaches the read too; only the write may come out true.
	g := r.Group("/v2", forge.WithGroupIdempotency())

	if err := g.PUT("/orders/{id}", ok, forge.WithOperationID("orderReplace")); err != nil {
		t.Fatal(err)
	}

	if err := g.GET("/orders/{id}", ok, forge.WithOperationID("orderGet")); err != nil {
		t.Fatal(err)
	}

	data, err := json.Marshal(r.OpenAPISpec())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	path := filepath.Join(t.TempDir(), "openapi.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	spec, err := client.NewSpecParser().ParseFile(context.Background(), path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	want := map[string]bool{"orderCreate": true, "orderNote": false, "orderReplace": true, "orderGet": false}
	seen := map[string]bool{}

	for _, ep := range spec.Endpoints {
		// The parser sets OperationID for REST endpoints and leaves ID empty.
		expected, tracked := want[ep.OperationID]
		if !tracked {
			continue
		}

		seen[ep.OperationID] = true

		if ep.Idempotent != expected {
			t.Fatalf("%s: Idempotent = %v, want %v", ep.OperationID, ep.Idempotent, expected)
		}
	}

	if len(seen) != len(want) {
		t.Fatalf("parsed endpoints %v, want all of %v", seen, want)
	}
}
