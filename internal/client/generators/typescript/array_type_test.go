package typescript

import (
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
)

func TestArrayTypeGroupsComposedItems(t *testing.T) {
	ref := &client.Schema{Ref: "#/components/schemas/Item"}
	null := &client.Schema{Type: "object", Nullable: true, Enum: []any{nil}}
	number := &client.Schema{Type: "number", Nullable: true}
	spec := &client.APISpec{Schemas: map[string]*client.Schema{"Item": {Type: "object"}}}
	g := &Generator{}

	for _, tc := range []struct {
		name     string
		item     *client.Schema
		nullable bool
		want     string
	}{
		{"number", number, false, "(number | null)[]"},
		{"nullable array", number, true, "(number | null)[] | null"},
		{"reference", &client.Schema{OneOf: []*client.Schema{ref, null}}, false, "(Item | null)[]"},
		{"union", &client.Schema{AnyOf: []*client.Schema{{Type: "number"}, {Type: "string"}}}, false, "(number | string)[]"},
		{"intersection", &client.Schema{AllOf: []*client.Schema{ref, ref}}, false, "(Item & Item)[]"},
		{"nested", &client.Schema{Type: "array", Items: number}, false, "((number | null)[])[]"},
		{"plain", &client.Schema{Type: "number"}, false, "number[]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := g.schemaToTSType(&client.Schema{Type: "array", Items: tc.item, Nullable: tc.nullable}, spec, "Row.values", client.GeneratorConfig{})
			if got != tc.want {
				t.Fatalf("array type %q; want %q", got, tc.want)
			}
		})
	}

	got := NewRESTGenerator().schemaToTSType(&client.Schema{Type: "array", Items: &client.Schema{Type: "string", Enum: []any{"up", "down"}}}, spec)
	if got != "(\"up\" | \"down\")[]" {
		t.Fatal("inline REST enum array lost grouping", got)
	}
}

func TestNullableArrayTypeAcceptsMissingValues(t *testing.T) {
	g := &Generator{}
	typ := g.schemaToTSType(&client.Schema{Type: "array", Items: &client.Schema{Type: "number", Nullable: true}}, &client.APISpec{}, "Row.values", client.GeneratorConfig{})
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"tsconfig.json": probeTSConfig,
		"src/array.ts": strings.ReplaceAll(`type Values = ARRAY_TYPE;
const values: Values = [1, null, 0];
const value: number | null = values[1];
// @ts-expect-error A scalar is not an array.
const scalar: Values = 1;
// @ts-expect-error Item nullability does not make the array nullable.
const absent: Values = null;
export { values, value, scalar, absent };
`, "ARRAY_TYPE", typ),
	})

	if errs := typeCheck(t, dir); len(errs) != 0 {
		t.Fatal(errs)
	}
}
