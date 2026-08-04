// internal/router/client_meta_test.go
package router

import "testing"

type testOrder struct {
	ID string `json:"id"`
}

func TestEmitsInfersEntityTypeAndIntent(t *testing.T) {
	tests := []struct {
		message string
		want    StreamIntent
	}{
		{"order.created", StreamUpsert},
		{"order.updated", StreamPatch},
		{"order.changed", StreamPatch},
		{"order.deleted", StreamEvict},
		{"order.removed", StreamEvict},
		{"order.fulfilled", StreamPatch}, // unrecognised suffix falls back to patch
	}

	for _, tt := range tests {
		t.Run(tt.message, func(t *testing.T) {
			b := Emits[testOrder](tt.message).Build()

			if b.EntityType != "testOrder" {
				t.Fatalf("EntityType = %q, want testOrder", b.EntityType)
			}

			if b.Intent != tt.want {
				t.Fatalf("Intent = %q, want %q", b.Intent, tt.want)
			}
		})
	}
}

func TestEmitsCreatedInvalidatesCollection(t *testing.T) {
	b := Emits[testOrder]("order.created").Build()

	if len(b.Invalidates) != 1 || b.Invalidates[0] != "testOrder[]" {
		t.Fatalf("Invalidates = %v, want [testOrder[]]", b.Invalidates)
	}
}

func TestEmitsUpdatedInvalidatesNothing(t *testing.T) {
	b := Emits[testOrder]("order.updated").Build()

	if len(b.Invalidates) != 0 {
		t.Fatalf("Invalidates = %v, want empty: a patch needs no refetch", b.Invalidates)
	}
}

func TestEmitsExplicitOverrides(t *testing.T) {
	b := Emits[testOrder]("order.fulfilled").
		As(StreamPatch).
		Invalidates("Shipment[]").
		Build()

	if b.Intent != StreamPatch {
		t.Fatalf("Intent = %q, want patch", b.Intent)
	}

	if len(b.Invalidates) != 1 || b.Invalidates[0] != "Shipment[]" {
		t.Fatalf("Invalidates = %v, want [Shipment[]]", b.Invalidates)
	}
}

func TestEmitsCreatedWithExplicitEmptyInvalidates(t *testing.T) {
	b := Emits[testOrder]("order.created").Invalidates().Build()

	if len(b.Invalidates) != 0 {
		t.Fatalf("Invalidates = %v, want empty: explicit Invalidates() suppresses default", b.Invalidates)
	}
}

func TestEmitsBuilderIdempotence(t *testing.T) {
	builder := Emits[testOrder]("order.created")

	first := builder.Build()
	second := builder.Build()

	if first.Intent != second.Intent {
		t.Fatalf("Build() not idempotent: first Intent = %q, second = %q", first.Intent, second.Intent)
	}

	if len(first.Invalidates) != len(second.Invalidates) {
		t.Fatalf("Build() not idempotent: first Invalidates = %v, second = %v", first.Invalidates, second.Invalidates)
	}

	for i, v := range first.Invalidates {
		if second.Invalidates[i] != v {
			t.Fatalf("Build() not idempotent: Invalidates differ at index %d", i)
		}
	}
}

// TestEmitsUnnamedTypeArgumentProducesEmptyEntityType documents the input
// side of a known gap: EntityType is derived via
// reflect.TypeOf((*T)(nil)).Elem().Name(), which returns "" for a type
// argument with no name of its own -- an anonymous struct, or a slice, map,
// or pointer type. Emits itself cannot report this: it runs here, in the
// router package, with no *client.APISpec and therefore nowhere to record a
// warning. Detection instead happens at generation time, in
// registerStreamBindingEntities (internal/client/introspector.go), which is
// where spec.Warnings is reachable and where the two related "will not
// normalize" cases are already reported. This test only pins down that the
// empty string reaches that point unchanged; the reporting itself is
// covered by TestRegisterStreamBindingEntitiesWarnsOnUnnamedEntityType in
// the client package.
func TestEmitsUnnamedTypeArgumentProducesEmptyEntityType(t *testing.T) {
	type anonymous = struct {
		ID string `json:"id"`
	}

	b := Emits[anonymous]("thing.updated").Build()

	if b.EntityType != "" {
		t.Fatalf("EntityType = %q, want empty for an unnamed type argument", b.EntityType)
	}

	b2 := Emits[[]testOrder]("things.updated").Build()

	if b2.EntityType != "" {
		t.Fatalf("EntityType = %q, want empty for a slice type argument", b2.EntityType)
	}
}
