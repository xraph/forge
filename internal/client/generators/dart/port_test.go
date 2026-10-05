package dart

import (
	"os"
	"testing"

	"github.com/xraph/forge/internal/client"
	"github.com/xraph/forge/internal/client/generators/dart/portgen"
)

// The ported files must stay in sync with the TypeScript originals by
// construction. This regenerates every port from the current TypeScript
// sources and fails when a committed copy differs, so a change to a
// TypeScript helper surfaces here instead of as drift the parity test might
// not reach. A missing declaration or edit anchor makes Generate fail too.
func TestPortedFilesMatchTypeScript(t *testing.T) {
	want, err := portgen.Generate("../typescript")
	if err != nil {
		t.Fatalf("portgen.Generate: %v", err)
	}

	for name, text := range want {
		got, err := os.ReadFile(name)
		if err != nil {
			t.Errorf("%s: %v (run go generate ./internal/client/generators/dart)", name, err)

			continue
		}

		if string(got) != text {
			t.Errorf("%s differs from what typescript/ produces now: run go generate ./internal/client/generators/dart and commit the result", name)
		}
	}
}

// The ported helpers must answer as their TypeScript originals do. The
// parity test compares whole tables; these pin the pieces it is built from.
func TestPortedOperationKeys(t *testing.T) {
	keys := operationKeys([]client.Endpoint{
		{Method: "GET", Path: "/orders/{id}", OperationID: "orders.get"},
		{Method: "GET", Path: "/orders/{id}", OperationID: "orders.get"},
		{Method: "POST", Path: "/sign-up"},
		{ID: "explicit", Method: "GET", Path: "/x"},
	})

	want := []string{"orders.get", "orders.get2", "post.signup", "explicit"}
	for i := range want {
		if keys[i] != want[i] {
			t.Errorf("keys[%d] = %q, want %q", i, keys[i], want[i])
		}
	}
}

func TestPortedFieldNamingDefaultsToCamelForDart(t *testing.T) {
	cfg := baseConfig()

	if got := clientFieldName("Order", "order_number", cfg); got != "orderNumber" {
		t.Errorf("clientFieldName = %q, want orderNumber", got)
	}

	cfg.FieldOverrides = map[string]string{"Order.order_number": "number"}
	if got := clientFieldName("Order", "order_number", cfg); got != "number" {
		t.Errorf("a schema-scoped override must win, got %q", got)
	}

	cfg.FieldOverrides = nil
	cfg.FieldNaming = client.NamingPreserve

	if got := clientFieldName("Order", "order_number", cfg); got != "order_number" {
		t.Errorf("preserve must keep the wire name, got %q", got)
	}
}

func TestPortedCodecTableNamesClientFields(t *testing.T) {
	table := buildCodecTable(ordersSpec(), baseConfig())

	entry := table.entries["Order"]
	if entry.Kind != "object" || entry.Fields["order_number"].Client != "orderNumber" {
		t.Errorf("Order entry = %+v", entry)
	}

	if table.entries["[]Order"].Items != "Order" {
		t.Errorf("the list body codec must be registered: %+v", table.entries["[]Order"])
	}
}

// The array codec a request body looks up is the one the table registered,
// as in the TypeScript original: a schemaless application/json entry beside
// a +json array body must not take the registration.
func TestPortedRequestBodyArrayCodecIsRegisteredUnderItsLookupKey(t *testing.T) {
	spec := &client.APISpec{
		Schemas: map[string]*client.Schema{
			"Order": {Type: "object", Properties: map[string]*client.Schema{"order_id": {Type: "string"}}},
		},
		Endpoints: []client.Endpoint{{
			Method: "POST", Path: "/orders/bulk", OperationID: "orders.bulk",
			RequestBody: &client.RequestBody{Required: true, Content: map[string]*client.MediaType{
				"application/json":         {},
				"application/vnd.api+json": {Schema: &client.Schema{Type: "array", Items: ref("Order")}},
			}},
			Responses: map[int]*client.Response{204: {Description: "ok"}},
		}},
	}

	id, _ := requestBodyCodecRef(&spec.Endpoints[0])
	if id == "" {
		t.Fatal("the +json array body resolves to no codec")
	}

	if _, ok := buildCodecTable(spec, baseConfig()).entries[id]; !ok {
		t.Errorf("requestBodyCodecRef looks up %q, which the codec table never registered", id)
	}
}
