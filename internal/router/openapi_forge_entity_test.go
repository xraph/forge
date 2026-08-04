package router

import "testing"

// forgeEntityOrder declares identity through the ForgeEntity interface rather
// than a struct tag, and is deliberately ambiguous without it: `id` and
// `tenant_id` are both identity-shaped, which is precisely the case client-side
// inference refuses to guess at. Before ForgeEntity was wired into the schema
// generator, implementing it changed nothing at all.
type forgeEntityOrder struct {
	OrderNumber string `json:"order_number"`
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

	if _, present := schema.Properties["total"].Extensions["x-forge-id"]; present {
		t.Fatal("x-forge-id must not be set on a field ForgeEntity did not name")
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
