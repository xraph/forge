package router

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"
)

// An embedded field's *name* is its type name, so reflect.StructField.IsExported()
// reports false for a field embedded from a lowercase-named type — even though
// encoding/json promotes and marshals that type's exported fields. These types
// pin the generated schema to what the wire format actually carries.

type unexportedItemBase struct {
	ItemID string `json:"item_id"`

	secret string //nolint:unused // present to prove genuinely unexported fields stay skipped
}

type ExportedAuditBase struct {
	Revision int `json:"revision"`
}

// midLevelBase is unexported *and* embeds another struct, exercising the
// recursive descent in flattenEmbeddedStruct rather than just the top-level loop.
type midLevelBase struct {
	ExportedAuditBase

	Mid string `json:"mid"`
}

type unexportedScalar int

// OrderWithUnexportedBase is the minimal reproduction from the bug report.
type OrderWithUnexportedBase struct {
	unexportedItemBase

	Name string `json:"name"`
}

// OrderWithUnexportedPtrBase embeds a *pointer* to the unexported-named type,
// which encoding/json also promotes.
type OrderWithUnexportedPtrBase struct {
	*unexportedItemBase

	Name string `json:"name"`
}

// OrderWithNestedUnexportedBase forces two levels of promotion through an
// unexported-named intermediary.
type OrderWithNestedUnexportedBase struct {
	midLevelBase

	Name string `json:"name"`
}

// OrderWithUnexportedScalar embeds an unexported-named NON-struct. encoding/json
// ignores these outright, and so must the generator.
type OrderWithUnexportedScalar struct {
	unexportedScalar

	Name string `json:"name"`
}

// OrderWithTaggedUnexportedBase gives the embedded unexported-named struct an
// explicit JSON name, which encoding/json nests rather than promotes.
type OrderWithTaggedUnexportedBase struct {
	unexportedItemBase `json:"base"`

	Name string `json:"name"`
}

// marshalKeys returns the top-level JSON object keys encoding/json actually
// produces for v, sorted. This is the ground truth the schema must match.
func marshalKeys(t *testing.T, v any) []string {
	t.Helper()

	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal(%T) error: %v", v, err)
	}

	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("json.Unmarshal(%s) error: %v", raw, err)
	}

	return slices.Sorted(maps.Keys(decoded))
}

// assertSchemaMatchesWire generates a schema for v and asserts its top-level
// properties are exactly the keys encoding/json emits for v. It returns the
// schema alongside the components map, so callers can resolve any $ref the
// generator registered.
func assertSchemaMatchesWire(t *testing.T, v any) (*Schema, map[string]*Schema) {
	t.Helper()

	components := make(map[string]*Schema)
	gen := newSchemaGenerator(components, nil)

	schema, err := gen.GenerateSchema(v)
	if err != nil {
		t.Fatalf("GenerateSchema(%T) error: %v", v, err)
	}

	want := marshalKeys(t, v)
	got := slices.Sorted(maps.Keys(schema.Properties))

	if !slices.Equal(got, want) {
		t.Errorf("schema properties for %T = %v, want %v (the keys encoding/json emits)", v, got, want)
	}

	return schema, components
}

// resolveRef follows a local component reference, failing the test if the
// schema is not a reference or the target is not registered.
func resolveRef(t *testing.T, schema *Schema, components map[string]*Schema) *Schema {
	t.Helper()

	name, ok := strings.CutPrefix(schema.Ref, "#/components/schemas/")
	if !ok {
		t.Fatalf("schema is not a local component reference: Ref=%q", schema.Ref)
	}

	target, ok := components[name]
	if !ok {
		t.Fatalf("component %q not registered; have %v", name, slices.Sorted(maps.Keys(components)))
	}

	return target
}

func TestEmbeddedUnexportedNamedStructIsPromoted(t *testing.T) {
	schema, _ := assertSchemaMatchesWire(t, OrderWithUnexportedBase{
		unexportedItemBase: unexportedItemBase{ItemID: "abc"},
		Name:               "n",
	})

	itemID, ok := schema.Properties["item_id"]
	if !ok {
		t.Fatal("item_id was not promoted into the schema")
	}

	if itemID.Type != "string" {
		t.Errorf("item_id type = %q, want string", itemID.Type)
	}

	if !slices.Contains(schema.Required, "item_id") {
		t.Errorf("required = %v, want it to contain item_id", schema.Required)
	}
}

func TestEmbeddedUnexportedNamedPointerStructIsPromoted(t *testing.T) {
	_, _ = assertSchemaMatchesWire(t, OrderWithUnexportedPtrBase{
		unexportedItemBase: &unexportedItemBase{ItemID: "abc"},
		Name:               "n",
	})
}

func TestEmbeddedUnexportedNamedStructPromotesNestedEmbeds(t *testing.T) {
	_, _ = assertSchemaMatchesWire(t, OrderWithNestedUnexportedBase{
		midLevelBase: midLevelBase{
			ExportedAuditBase: ExportedAuditBase{Revision: 3},
			Mid:               "m",
		},
		Name: "n",
	})
}

func TestEmbeddedUnexportedNamedScalarIsSkipped(t *testing.T) {
	_, _ = assertSchemaMatchesWire(t, OrderWithUnexportedScalar{unexportedScalar: 7, Name: "n"})
}

func TestEmbeddedUnexportedNamedStructWithJSONNameIsNested(t *testing.T) {
	schema, components := assertSchemaMatchesWire(t, OrderWithTaggedUnexportedBase{
		unexportedItemBase: unexportedItemBase{ItemID: "abc"},
		Name:               "n",
	})

	base, ok := schema.Properties["base"]
	if !ok {
		t.Fatal("base property missing")
	}

	// A named struct type is registered as a component and referenced, so the
	// nested object lives in components rather than inline.
	resolved := resolveRef(t, base, components)

	if _, ok := resolved.Properties["item_id"]; !ok {
		t.Errorf("base.item_id missing; resolved = %v", slices.Sorted(maps.Keys(resolved.Properties)))
	}
}
