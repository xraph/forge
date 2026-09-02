package router

import "testing"

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
