// internal/router/router_opts_client_test.go
package router

import "testing"

func applyOpts(opts ...RouteOption) *RouteConfig {
	cfg := &RouteConfig{}
	for _, o := range opts {
		o.Apply(cfg)
	}

	return cfg
}

func TestWithEntityStoresDefinition(t *testing.T) {
	cfg := applyOpts(WithEntity(EntityDef{Type: "Order", IDField: "OrderNumber"}))

	def, ok := cfg.Metadata["forge.client.entity"].(EntityDef)
	if !ok {
		t.Fatalf("metadata missing entity, got %#v", cfg.Metadata)
	}

	if def.IDField != "OrderNumber" {
		t.Fatalf("IDField = %q, want OrderNumber", def.IDField)
	}
}

func TestWithoutEntityStoresFlag(t *testing.T) {
	cfg := applyOpts(WithoutEntity())

	if v, _ := cfg.Metadata["forge.client.noEntity"].(bool); !v {
		t.Fatalf("noEntity = %#v, want true", cfg.Metadata["forge.client.noEntity"])
	}
}

func TestWithInvalidatesAccumulates(t *testing.T) {
	cfg := applyOpts(
		WithInvalidates("Inventory[]"),
		WithInvalidates("Customer:{req.customerId}"),
	)

	tags, _ := cfg.Metadata["forge.client.invalidates"].([]string)
	if len(tags) != 2 {
		t.Fatalf("invalidates = %v, want two entries", tags)
	}
}

func TestWithoutInvalidationAccumulates(t *testing.T) {
	cfg := applyOpts(WithoutInvalidation("Order[]"))

	tags, _ := cfg.Metadata["forge.client.noInvalidation"].([]string)
	if len(tags) != 1 || tags[0] != "Order[]" {
		t.Fatalf("noInvalidation = %v, want [Order[]]", tags)
	}
}

func TestWithStreamBindingBuildsBindings(t *testing.T) {
	cfg := applyOpts(WithStreamBinding(
		Emits[testOrder]("order.created"),
		Emits[testOrder]("order.updated"),
	))

	bindings, _ := cfg.Metadata["forge.client.streamBindings"].([]StreamBinding)
	if len(bindings) != 2 {
		t.Fatalf("bindings = %v, want two", bindings)
	}

	if bindings[0].Intent != StreamUpsert || bindings[1].Intent != StreamPatch {
		t.Fatalf("intents = %q/%q, want upsert/patch", bindings[0].Intent, bindings[1].Intent)
	}
}
