package router

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/router/testtypes/billing"
	"github.com/xraph/forge/internal/router/testtypes/shipping"
	"github.com/xraph/forge/internal/shared"
)

func TestOperationCarriesForgeExtensions(t *testing.T) {
	route := RouteInfo{
		Method: "POST",
		Path:   "/orders",
		Metadata: map[string]any{
			"forge.client.entity":         EntityDef{Type: "Order", IDField: "OrderNumber"},
			"forge.client.invalidates":    []string{"Inventory[]"},
			"forge.client.noInvalidation": []string{"Order[]"},
		},
	}

	op := &Operation{}
	applyForgeExtensions(op, route.Metadata)

	ent, ok := op.Extensions["x-forge-entity"].(map[string]any)
	if !ok {
		t.Fatalf("x-forge-entity missing: %#v", op.Extensions)
	}

	if ent["idField"] != "OrderNumber" {
		t.Fatalf("idField = %v, want OrderNumber", ent["idField"])
	}

	inv, _ := op.Extensions["x-forge-invalidates"].([]string)
	if len(inv) != 1 || inv[0] != "Inventory[]" {
		t.Fatalf("x-forge-invalidates = %v, want [Inventory[]]", inv)
	}

	sup, _ := op.Extensions["x-forge-no-invalidation"].([]string)
	if len(sup) != 1 || sup[0] != "Order[]" {
		t.Fatalf("x-forge-no-invalidation = %v, want [Order[]]", sup)
	}
}

func TestOperationWithoutForgeMetadataGetsNoExtensions(t *testing.T) {
	op := &Operation{}
	applyForgeExtensions(op, map[string]any{"unrelated": true})

	for key := range op.Extensions {
		if len(key) > 8 && key[:8] == "x-forge-" {
			t.Fatalf("unexpected extension %q on a route that declared nothing", key)
		}
	}
}

func TestNoEntityFlagIsEmitted(t *testing.T) {
	op := &Operation{}
	applyForgeExtensions(op, map[string]any{"forge.client.noEntity": true})

	if v, _ := op.Extensions["x-forge-no-entity"].(bool); !v {
		t.Fatalf("x-forge-no-entity = %#v, want true", op.Extensions["x-forge-no-entity"])
	}
}

func TestStaleTimeExtensionIsEmitted(t *testing.T) {
	op := &Operation{}
	applyForgeExtensions(op, map[string]any{"forge.client.staleTime": int64(30000)})

	if got, _ := op.Extensions["x-forge-stale-time"].(int64); got != 30000 {
		t.Fatalf("x-forge-stale-time = %#v, want 30000", op.Extensions["x-forge-stale-time"])
	}
}

func TestStaleTimeExtensionIsAbsentWhenUndeclared(t *testing.T) {
	op := &Operation{}
	applyForgeExtensions(op, map[string]any{})

	if _, ok := op.Extensions["x-forge-stale-time"]; ok {
		t.Fatalf("x-forge-stale-time = %#v, want absent when undeclared", op.Extensions["x-forge-stale-time"])
	}
}

// TestEmptyInvalidatesSliceEmitsNoKey pins the guard that an empty (but
// non-nil) []string for invalidates/noInvalidation produces no key at all,
// rather than an empty JSON array. An empty array in the document would later
// have to be distinguished from absence by every consumer.
func TestEmptyInvalidatesSliceEmitsNoKey(t *testing.T) {
	op := &Operation{}
	applyForgeExtensions(op, map[string]any{
		"forge.client.invalidates":    []string{},
		"forge.client.noInvalidation": []string{},
	})

	if op.Extensions != nil {
		t.Fatalf("Extensions = %#v, want nil: an empty invalidates/noInvalidation slice must emit nothing", op.Extensions)
	}

	if _, ok := op.Extensions["x-forge-invalidates"]; ok {
		t.Fatalf("x-forge-invalidates present for an empty slice, want absent")
	}

	if _, ok := op.Extensions["x-forge-no-invalidation"]; ok {
		t.Fatalf("x-forge-no-invalidation present for an empty slice, want absent")
	}
}

// A component that finalization named something other than its type's bare
// name says what it was generated from, so a stream binding declared with the
// bare name can still be matched to it. An uncontested component is not
// marked: its bare name already answers, and an import path on every schema
// would be paid by all for the sake of the few that moved.
func TestRenamedComponentCarriesItsGoType(t *testing.T) {
	r := NewRouter(WithOpenAPI(OpenAPIConfig{Title: "T", Version: "1.0.0"}))

	if err := r.GET("/billing/invoice", func(ctx shared.Context, req *collisionEmptyRequest) (*billing.Invoice, error) {
		return &billing.Invoice{}, nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := r.GET("/shipping/invoice", func(ctx shared.Context, req *collisionEmptyRequest) (*shipping.Invoice, error) {
		return &shipping.Invoice{}, nil
	}); err != nil {
		t.Fatal(err)
	}

	spec := r.OpenAPISpec()
	if spec == nil {
		t.Fatal("no spec")
	}

	// Through JSON, so the assertion is about what a consumer of the document
	// sees: the extension has to be hoisted onto the schema, not nested.
	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}

	var doc struct {
		Components struct {
			Schemas map[string]map[string]any `json:"schemas"`
		} `json:"components"`
	}

	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}

	marked := map[string]string{}

	for name, schema := range doc.Components.Schemas {
		if typ, ok := schema["x-forge-type"].(string); ok {
			marked[name] = typ
		}
	}

	if _, contested := doc.Components.Schemas["Invoice"]; contested {
		t.Fatalf("a contested bare name reached the document: %v", doc.Components.Schemas)
	}

	// Both Invoice types moved, and both say where they came from. Their
	// nested Note types collide too and are marked the same way.
	want := map[string]bool{
		getQualifiedTypeName(billingType()):  false,
		getQualifiedTypeName(shippingType()): false,
	}

	for name, typ := range marked {
		if _, ok := want[typ]; ok {
			want[typ] = true
		}

		// A marked component is one that moved: its name is never the bare
		// type name the qualified string ends in.
		if bare := typ[strings.LastIndex(typ, ".")+1:]; name == bare {
			t.Errorf("component %q kept its bare name yet is marked with %q", name, typ)
		}
	}

	for typ, seen := range want {
		if !seen {
			t.Errorf("no component is marked with %q; marked = %v", typ, marked)
		}
	}

	for name := range doc.Components.Schemas {
		if _, ok := marked[name]; ok {
			continue
		}

		if _, ok := doc.Components.Schemas[name]["x-forge-type"]; ok {
			t.Errorf("component %q is unexpectedly marked", name)
		}
	}
}

func billingType() reflect.Type  { return reflect.TypeFor[billing.Invoice]() }
func shippingType() reflect.Type { return reflect.TypeFor[shipping.Invoice]() }
