package forge

import "testing"

type reexportOrder struct {
	ID string `json:"id"`
}

func TestClientMetaReExports(t *testing.T) {
	// Compiling is most of the assertion: these are the names users type.
	_ = WithEntity(EntityDef{Type: "Order", IDField: "ID"})
	_ = WithoutEntity()
	_ = WithInvalidates("Inventory[]")
	_ = WithoutInvalidation("Order[]")
	_ = WithStreamBinding(Emits[reexportOrder]("order.created"))

	if StreamUpsert != "upsert" {
		t.Fatalf("StreamUpsert = %q, want upsert", StreamUpsert)
	}

	b := Emits[reexportOrder]("order.deleted").Build()
	if b.Intent != StreamEvict {
		t.Fatalf("Intent = %q, want evict", b.Intent)
	}
}
