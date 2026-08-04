package router

import "testing"

type taggedOrder struct {
	OrderNumber string `json:"order_number" forge:"id"`
	Total       int    `json:"total"`
}

func TestForgeIDTagBecomesExtension(t *testing.T) {
	components := make(map[string]*Schema)
	gen := newSchemaGenerator(components, nil)

	schema, err := gen.GenerateSchema(taggedOrder{})
	if err != nil {
		t.Fatalf("GenerateSchema failed: %v", err)
	}

	prop, ok := schema.Properties["order_number"]
	if !ok {
		t.Fatalf("order_number missing from properties: %#v", schema.Properties)
	}

	if v, _ := prop.Extensions["x-forge-id"].(bool); !v {
		t.Fatalf("x-forge-id = %#v, want true", prop.Extensions["x-forge-id"])
	}

	totalProp, ok := schema.Properties["total"]
	if !ok {
		t.Fatalf("total missing from properties: %#v", schema.Properties)
	}

	if _, present := totalProp.Extensions["x-forge-id"]; present {
		t.Fatal("x-forge-id must not be set on untagged fields")
	}
}

// TaggedIdentity is embedded (anonymous, no explicit json tag) so its
// fields are flattened/promoted onto the outer struct via the second
// code path in generateStructSchema (flattenEmbeddedStruct), not the
// primary field loop.
//
// The embedded type's name MUST be exported (capitalized): reflect
// derives a struct field's exported-ness from the field name, and for
// an anonymous field the field name is the type name. An unexported
// embedded type name makes reflect.StructField.IsExported() report
// false, which causes the generator to skip the field entirely before
// it ever reaches the flatten branch.
type TaggedIdentity struct {
	ItemID string `json:"item_id" forge:"id"`
}

type embeddingItem struct {
	TaggedIdentity

	Name string `json:"name"`
}

func TestForgeIDTagBecomesExtensionOnEmbeddedField(t *testing.T) {
	components := make(map[string]*Schema)
	gen := newSchemaGenerator(components, nil)

	schema, err := gen.GenerateSchema(embeddingItem{})
	if err != nil {
		t.Fatalf("GenerateSchema failed: %v", err)
	}

	prop, ok := schema.Properties["item_id"]
	if !ok {
		t.Fatalf("item_id (promoted from embedded struct) missing from properties: %#v", schema.Properties)
	}

	if v, _ := prop.Extensions["x-forge-id"].(bool); !v {
		t.Fatalf("x-forge-id = %#v, want true on promoted embedded field", prop.Extensions["x-forge-id"])
	}

	nameProp, ok := schema.Properties["name"]
	if !ok {
		t.Fatalf("name missing from properties: %#v", schema.Properties)
	}

	if _, present := nameProp.Extensions["x-forge-id"]; present {
		t.Fatal("x-forge-id must not be set on untagged fields")
	}
}
