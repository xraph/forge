package router

import "testing"

// forgeEntityOrder declares identity through the ForgeEntity interface rather
// than a struct tag, on a type that ALSO carries a property named `id` which is
// not the identity. That combination is the case the interface is documented
// for, and the one client-side inference gets wrong without an explicit marker
// to prefer: the name heuristic would key every Order by `id`.
//
// Before ForgeEntity was wired into the schema generator, implementing it
// changed nothing at all. The client-side half -- that the marker then beats the
// `id` name -- is asserted end to end in
// internal/client/entity_forge_entity_e2e_test.go; what this file asserts is
// that the marker is emitted onto the right property in the first place.
type forgeEntityOrder struct {
	OrderNumber string `json:"order_number"`
	ID          string `json:"id"`
	Total       int    `json:"total"`
}

func (forgeEntityOrder) ForgeEntity() EntityDef {
	return EntityDef{Type: "Order", IDField: "order_number"}
}

func TestForgeEntityMarksDeclaredIDProperty(t *testing.T) {
	gen := newSchemaGenerator(make(map[string]*Schema), nil)

	schema, err := gen.GenerateSchema(forgeEntityOrder{})
	if err != nil {
		t.Fatalf("GenerateSchema failed: %v", err)
	}

	prop, ok := schema.Properties["order_number"]
	if !ok {
		t.Fatalf("order_number missing from properties: %#v", schema.Properties)
	}

	if v, _ := prop.Extensions["x-forge-id"].(bool); !v {
		t.Fatalf("x-forge-id = %#v, want true (ForgeEntity was not honoured)",
			prop.Extensions["x-forge-id"])
	}

	for _, other := range []string{"total", "id"} {
		if _, present := schema.Properties[other].Extensions["x-forge-id"]; present {
			t.Fatalf("x-forge-id was set on %q, which ForgeEntity did not name", other)
		}
	}
}

// forgeEntityPointerOrder implements ForgeEntity on a pointer receiver, which
// is the shape most people write without thinking about it.
type forgeEntityPointerOrder struct {
	Ref string `json:"ref"`
}

func (*forgeEntityPointerOrder) ForgeEntity() EntityDef {
	return EntityDef{Type: "PointerOrder", IDField: "ref"}
}

func TestForgeEntityHonoursPointerReceiver(t *testing.T) {
	gen := newSchemaGenerator(make(map[string]*Schema), nil)

	schema, err := gen.GenerateSchema(forgeEntityPointerOrder{})
	if err != nil {
		t.Fatalf("GenerateSchema failed: %v", err)
	}

	if v, _ := schema.Properties["ref"].Extensions["x-forge-id"].(bool); !v {
		t.Fatal("a pointer-receiver ForgeEntity implementation was ignored")
	}
}

// forgeEntityGoFieldName declares the GO field name, the mistake the old doc
// comment invited. Nothing is marked, and generation must not panic or invent
// a property.
type forgeEntityGoFieldName struct {
	ID string `json:"id"`
}

func (forgeEntityGoFieldName) ForgeEntity() EntityDef {
	return EntityDef{Type: "Wrong", IDField: "ID"}
}

func TestForgeEntityWithUnknownPropertyMarksNothing(t *testing.T) {
	gen := newSchemaGenerator(make(map[string]*Schema), nil)

	schema, err := gen.GenerateSchema(forgeEntityGoFieldName{})
	if err != nil {
		t.Fatalf("GenerateSchema failed: %v", err)
	}

	if _, present := schema.Properties["id"].Extensions["x-forge-id"]; present {
		t.Fatal("a declaration naming a non-existent property must not mark a different one")
	}
}

// plainOrder implements nothing; the wiring must leave ordinary types alone.
type plainOrder struct {
	ID string `json:"id"`
}

func TestNonEntityTypesAreUntouched(t *testing.T) {
	gen := newSchemaGenerator(make(map[string]*Schema), nil)

	schema, err := gen.GenerateSchema(plainOrder{})
	if err != nil {
		t.Fatalf("GenerateSchema failed: %v", err)
	}

	if _, present := schema.Properties["id"].Extensions["x-forge-id"]; present {
		t.Fatal("x-forge-id was set on a type that declares nothing")
	}
}
