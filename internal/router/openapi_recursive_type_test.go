package router

// Self-referential types: a struct that can reach itself through its own
// embedded fields.
//
// Go forbids a struct from embedding itself by value, but not from embedding a
// POINTER to itself, and not from embedding a chain of types that closes back
// on the first. Every embedded-field walk in this package flattens rather than
// recurses through a $ref, so each one of them followed such a chain forever
// and took the process down with `fatal error: stack overflow` -- not a panic,
// so no recover() and no middleware could contain it. Registering one route
// with such a request or response type was enough.
//
// The tests below cover each walk independently, because they are separate
// implementations that merely look alike.

import (
	"reflect"
	"testing"
)

// RecursiveNode embeds a pointer to itself. This is the minimal shape that
// closes the cycle.
//
// encoding/json marshals a value of this type as {"name": ...}: its field
// promotion dedupes on the type, so the embedded *RecursiveNode contributes
// nothing beyond what the outer struct already promoted. The schema generators
// must agree with that, since the schema describes the JSON.
type RecursiveNode struct {
	*RecursiveNode

	Name string `json:"name"`
}

// RecursiveRequest is RecursiveNode with a query tag, which is what routes it
// through the unified request extractor rather than the legacy body path
// (hasUnifiedTags is the gate; see unifiedRequestSchema).
type RecursiveRequest struct {
	*RecursiveRequest

	Cursor string `optional:"true" query:"cursor"`
	Name   string `json:"name"`
}

// recursivePairA and recursivePairB close the cycle through each other rather
// than directly, so a guard that only compares against the immediately
// enclosing type does not count as a fix.
type recursivePairA struct {
	*recursivePairB

	AName string `json:"a_name"`
}

type recursivePairB struct {
	*recursivePairA

	BName string `json:"b_name"`
}

// RecursiveHeaders is the AsyncAPI header-walk shape: the cycle is closed by an
// embedded pointer and the payload carries a header tag.
type RecursiveHeaders struct {
	*RecursiveHeaders

	TraceID string `header:"X-Trace-ID"`
	Body    string `json:"body"`
}

func TestGenerateSchemaTerminatesOnSelfEmbeddedType(t *testing.T) {
	g := newSchemaGenerator(map[string]*Schema{}, nil)

	schema, err := g.GenerateSchema(&RecursiveNode{})
	if err != nil {
		t.Fatalf("GenerateSchema: %v", err)
	}

	// The shape encoding/json produces: the embedded self contributes no
	// properties, because everything it could promote is already promoted.
	if _, ok := schema.Properties["name"]; !ok {
		t.Fatalf("expected property %q, got %v", "name", propertyNames(schema))
	}

	if len(schema.Properties) != 1 {
		t.Fatalf("expected exactly the promoted property set, got %v", propertyNames(schema))
	}
}

func TestGenerateSchemaTerminatesOnMutuallyEmbeddedTypes(t *testing.T) {
	g := newSchemaGenerator(map[string]*Schema{}, nil)

	schema, err := g.GenerateSchema(&recursivePairA{})
	if err != nil {
		t.Fatalf("GenerateSchema: %v", err)
	}

	// A reaches B, which reaches A again. B's own field is promoted once; the
	// second arrival at A adds nothing.
	for _, want := range []string{"a_name", "b_name"} {
		if _, ok := schema.Properties[want]; !ok {
			t.Fatalf("expected property %q, got %v", want, propertyNames(schema))
		}
	}
}

func TestExtractUnifiedRequestComponentsTerminatesOnSelfEmbeddedType(t *testing.T) {
	g := newSchemaGenerator(map[string]*Schema{}, nil)

	components, err := extractUnifiedRequestComponents(g, &RecursiveRequest{})
	if err != nil {
		t.Fatalf("extractUnifiedRequestComponents: %v", err)
	}

	if len(components.QueryParams) != 1 || components.QueryParams[0].Name != "cursor" {
		t.Fatalf("expected exactly one query parameter %q, got %+v", "cursor", components.QueryParams)
	}

	if !components.HasBody || components.BodySchema == nil {
		t.Fatal("expected a body schema from the json-tagged field")
	}

	if _, ok := components.BodySchema.Properties["name"]; !ok {
		t.Fatalf("expected body property %q, got %v", "name", propertyNames(components.BodySchema))
	}
}

func TestAsyncAPIHeadersSchemaTerminatesOnSelfEmbeddedType(t *testing.T) {
	g := newAsyncAPISchemaGenerator(map[string]*Schema{}, nil)

	headers := g.GenerateHeadersSchema(&RecursiveHeaders{})
	if headers == nil {
		t.Fatal("expected a headers schema")
	}

	if len(headers.Properties) != 1 {
		t.Fatalf("expected exactly one header, got %v", propertyNames(headers))
	}

	if _, ok := headers.Properties["X-Trace-ID"]; !ok {
		t.Fatalf("expected header %q, got %v", "X-Trace-ID", propertyNames(headers))
	}
}

func TestAsyncAPISplitMessageComponentsTerminatesOnSelfEmbeddedType(t *testing.T) {
	g := newAsyncAPISchemaGenerator(map[string]*Schema{}, nil)

	headers, payload := g.SplitMessageComponents(&RecursiveHeaders{})
	if headers == nil || payload == nil {
		t.Fatalf("expected both headers and payload, got headers=%v payload=%v", headers, payload)
	}

	if _, ok := payload.Properties["body"]; !ok {
		t.Fatalf("expected payload property %q, got %v", "body", propertyNames(payload))
	}
}

// TestSchemaMatchesEncodingJSONPromotion pins the fix to the standard library
// rather than to a number we chose. encoding/json's own field walk dedupes on
// reflect.Type for exactly this reason, and the schema describes the JSON that
// walk produces, so the two must agree about which properties exist.
func TestSchemaMatchesEncodingJSONPromotion(t *testing.T) {
	cases := []struct {
		name  string
		value any
	}{
		{"self-embedded", &RecursiveNode{}},
		{"mutually embedded", &recursivePairA{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := newSchemaGenerator(map[string]*Schema{}, nil)

			schema, err := g.GenerateSchema(tc.value)
			if err != nil {
				t.Fatalf("GenerateSchema: %v", err)
			}

			want := jsonPromotedFieldNames(reflect.TypeOf(tc.value).Elem())
			for _, name := range want {
				if _, ok := schema.Properties[name]; !ok {
					t.Errorf("encoding/json promotes %q but the schema omits it (schema has %v)", name, propertyNames(schema))
				}
			}

			if len(schema.Properties) != len(want) {
				t.Errorf("schema properties %v do not match encoding/json's promoted set %v", propertyNames(schema), want)
			}
		})
	}
}

// jsonPromotedFieldNames reports the JSON property names encoding/json would
// emit for typ, computed the same way encoding/json computes them: a
// breadth-first walk of embedded structs that refuses to visit a type twice.
func jsonPromotedFieldNames(typ reflect.Type) []string {
	var (
		names   []string
		seen    = map[reflect.Type]bool{}
		current = []reflect.Type{typ}
	)

	for len(current) > 0 {
		next := []reflect.Type{}

		for _, t := range current {
			if seen[t] {
				continue
			}

			seen[t] = true

			for i := range t.NumField() {
				field := t.Field(i)

				if field.Anonymous {
					ft := field.Type
					if ft.Kind() == reflect.Ptr {
						ft = ft.Elem()
					}

					if ft.Kind() == reflect.Struct && field.Tag.Get("json") == "" {
						next = append(next, ft)

						continue
					}
				}

				name, _ := parseJSONTag(field.Tag.Get("json"))
				if name == "" {
					name = field.Name
				}

				if name != "-" && field.IsExported() {
					names = append(names, name)
				}
			}
		}

		current = next
	}

	return names
}

func propertyNames(schema *Schema) []string {
	if schema == nil {
		return nil
	}

	names := make([]string, 0, len(schema.Properties))
	for name := range schema.Properties {
		names = append(names, name)
	}

	return names
}
