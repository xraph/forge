// internal/router/router_opts_client_test.go
package router

import (
	"testing"
	"time"
)

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
	cfg := applyOpts(
		WithoutInvalidation("Order[]"),
		WithoutInvalidation("Inventory[]"),
	)

	tags, _ := cfg.Metadata["forge.client.noInvalidation"].([]string)
	if len(tags) != 2 {
		t.Fatalf("noInvalidation = %v, want two entries", tags)
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

func TestWithStaleTime(t *testing.T) {
	tests := []struct {
		name string
		in   time.Duration
		want int64
	}{
		{"seconds", 30 * time.Second, 30000},
		{"milliseconds", 250 * time.Millisecond, 250},
		{"sub-millisecond truncates to zero and is dropped", 500 * time.Microsecond, 0},
		{"negative is dropped", -time.Second, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := applyOpts(WithStaleTime(tt.in))

			got, ok := cfg.Metadata["forge.client.staleTime"].(int64)

			if tt.want == 0 {
				if ok {
					t.Fatalf("a value that cannot mean anything must not be recorded, got %v", got)
				}
				return
			}

			if !ok {
				t.Fatalf("metadata missing staleTime, got %#v", cfg.Metadata)
			}

			if got != tt.want {
				t.Fatalf("staleTime = %d, want %d", got, tt.want)
			}
		})
	}
}
