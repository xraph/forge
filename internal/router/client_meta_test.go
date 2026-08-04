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

func TestEmiBuilderIdempotence(t *testing.T) {
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
