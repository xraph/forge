package shared

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// astField describes one exported, named struct field discovered by parsing
// the shared package's own source, together with its raw json/yaml struct
// tags (unparsed, exactly as written).
type astField struct {
	structName string
	fieldName  string
	jsonTag    string
	yamlTag    string
	hasYAML    bool
}

// exprString renders a field type expression back to source-like text for
// error messages (used for embedded-field diagnostics), without pulling in
// go/printer for one call site.
func exprString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return "*" + exprString(e.X)
	case *ast.SelectorExpr:
		return exprString(e.X) + "." + e.Sel.Name
	default:
		return "<unknown>"
	}
}

// parseStructFields walks every top-level struct type declared in the given
// source files (resolved relative to this test file's own directory, so it
// works regardless of the test runner's working directory) and returns one
// astField per exported, named field.
//
// This is deliberately a source-level (go/parser) walk rather than a
// hand-maintained list of struct literals fed through reflection. A
// hardcoded list has to be updated by hand every time a new struct is added
// to openapi.go or asyncapi.go, and nothing enforces that: a forgotten entry
// means TestJSONYAMLTagParity passes silently on a brand-new struct with
// mismatched json/yaml tags — the same silent-data-loss bug class this test
// exists to catch, just one level up (missing struct instead of missing
// field). Parsing the files directly means every struct is covered
// automatically, with no list to maintain or forget.
func parseStructFields(t *testing.T, filenames ...string) []astField {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed to resolve this test file's own path")
	}

	dir := filepath.Dir(thisFile)

	var fields []astField

	fset := token.NewFileSet()

	for _, name := range filenames {
		path := filepath.Join(dir, name)

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}

		for _, decl := range file.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok || genDecl.Tok != token.TYPE {
				continue
			}

			for _, spec := range genDecl.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}

				structType, ok := typeSpec.Type.(*ast.StructType)
				if !ok {
					continue
				}

				for _, field := range structType.Fields.List {
					if len(field.Names) == 0 {
						// Embedded (anonymous) field: `SomeType` with no
						// identifier of its own — e.g. `io.Reader` embedded
						// directly. None of the structs in openapi.go or
						// asyncapi.go currently embed anything this way, so
						// this case is deliberately left unhandled rather
						// than guessed at: fail loudly so a future embed
						// forces a real decision (what json/yaml key would
						// yaml.v3 and encoding/json even use for it) instead
						// of being silently skipped by the guard.
						t.Fatalf("%s: struct %s has an embedded field (%s) with no explicit name; parseStructFields does not handle embedding — add explicit support before relying on the guard for this struct",
							path, typeSpec.Name.Name, exprString(field.Type))
					}

					var tagStr string

					if field.Tag != nil {
						unquoted, err := strconv.Unquote(field.Tag.Value)
						if err != nil {
							t.Fatalf("%s: struct %s: unquote tag %s: %v", path, typeSpec.Name.Name, field.Tag.Value, err)
						}

						tagStr = unquoted
					}

					tag := reflect.StructTag(tagStr)

					jsonTag, hasJSON := tag.Lookup("json")
					if !hasJSON {
						continue
					}

					yamlTag, hasYAML := tag.Lookup("yaml")

					// A single field declaration may name multiple fields
					// sharing one type and tag, e.g. `A, B string
					// \`json:"..."\``. Rare in this codebase but legal Go;
					// each name gets its own astField since each is an
					// independent struct field as far as json/yaml are
					// concerned.
					for _, ident := range field.Names {
						if !ident.IsExported() {
							continue
						}

						fields = append(fields, astField{
							structName: typeSpec.Name.Name,
							fieldName:  ident.Name,
							jsonTag:    jsonTag,
							yamlTag:    yamlTag,
							hasYAML:    hasYAML,
						})
					}
				}
			}
		}
	}

	return fields
}

// yamlDecodeKey reproduces gopkg.in/yaml.v3's own field-name resolution: the
// name portion of an explicit `yaml` tag if present, otherwise the field name
// lowercased. This mirrors yaml.v3's fieldInfo logic in yaml.go so the test
// asserts against the library's real behaviour, not an assumption about it.
func yamlDecodeKey(fieldName, yamlTag string, hasYAML bool) (key string, skip bool) {
	if !hasYAML {
		return strings.ToLower(fieldName), false
	}

	parts := strings.Split(yamlTag, ",")
	if parts[0] == "-" {
		return "", true
	}

	if parts[0] == "" {
		return strings.ToLower(fieldName), false
	}

	return parts[0], false
}

// sortedOptions returns a sorted copy of opts, so option-list comparisons
// (e.g. ["omitempty"] vs ["omitempty"]) don't depend on declaration order.
func sortedOptions(opts []string) []string {
	out := append([]string(nil), opts...)
	sort.Strings(out)

	return out
}

// TestJSONYAMLTagParity is a source-level guard, driven by parseStructFields
// above: for every exported field with a `json` tag (excluding json:"-") on
// every struct declared in openapi.go and asyncapi.go, a `yaml` tag must be
// present whose key matches the json tag's key exactly (including the value
// before the first comma, so "$ref" is compared as "$ref" not "ref").
// Without an explicit yaml tag, gopkg.in/yaml.v3 falls back to
// strings.ToLower(FieldName) when decoding YAML, which silently diverges
// from the json key for anything but an already-lowercase single-word name
// or a name containing punctuation like "$ref".
//
// Where a yaml tag is present, its option list (everything after the first
// comma — in practice, "omitempty") must also match the json tag's option
// list, so a field that drops "omitempty" on one side but not the other
// (still a real behavioural divergence between JSON and YAML output) gets
// caught too.
//
// This test is the permanent guard against the next field — or the next
// whole struct — someone adds to either file without a matching yaml tag.
func TestJSONYAMLTagParity(t *testing.T) {
	fields := parseStructFields(t, "openapi.go", "asyncapi.go")
	if len(fields) == 0 {
		t.Fatal("parseStructFields returned no fields; the AST walk is broken")
	}

	for _, f := range fields {
		f := f

		t.Run(f.structName+"."+f.fieldName, func(t *testing.T) {
			jsonParts := strings.Split(f.jsonTag, ",")
			jsonName := jsonParts[0]

			if jsonName == "-" || jsonName == "" {
				return
			}

			yamlKey, skip := yamlDecodeKey(f.fieldName, f.yamlTag, f.hasYAML)
			if skip {
				t.Errorf("field %s.%s: yaml tag is \"-\" but json tag is %q; field is unreachable from YAML", f.structName, f.fieldName, f.jsonTag)

				return
			}

			if yamlKey != jsonName {
				t.Errorf("field %s.%s: yaml-decoded key %q does not match json key %q (json tag %q) — add `yaml:%q` to this field",
					f.structName, f.fieldName, yamlKey, jsonName, f.jsonTag, f.jsonTag)
			}

			if !f.hasYAML {
				return
			}

			jsonOpts := sortedOptions(jsonParts[1:])
			yamlOpts := sortedOptions(strings.Split(f.yamlTag, ",")[1:])

			if !reflect.DeepEqual(jsonOpts, yamlOpts) {
				t.Errorf("field %s.%s: yaml tag options %v do not match json tag options %v (json %q, yaml %q)",
					f.structName, f.fieldName, yamlOpts, jsonOpts, f.jsonTag, f.yamlTag)
			}
		})
	}
}

// unmarshalYAML is a small helper shared by the behavioural tests below: it
// unmarshals doc into a fresh *T and fails the test on error.
func unmarshalYAML[T any](t *testing.T, doc string) *T {
	t.Helper()

	var v T
	if err := yaml.Unmarshal([]byte(doc), &v); err != nil {
		t.Fatalf("yaml.Unmarshal failed: %v", err)
	}

	return &v
}

// TestSchemaRefFromYAML asserts that Schema.Ref populates from a YAML
// document's "$ref" key. Before the fix, Schema had no yaml tag for Ref at
// all (json:"$ref,omitempty"), so yaml.v3's no-tag fallback looked up the
// field under the key "ref" (strings.ToLower("Ref")) — which never matches
// "$ref" in a real document. Every $ref in every YAML OpenAPI spec was
// silently dropped.
func TestSchemaRefFromYAML(t *testing.T) {
	const doc = `
$ref: "#/components/schemas/Widget"
`
	s := unmarshalYAML[Schema](t, doc)

	if s.Ref != "#/components/schemas/Widget" {
		t.Errorf("Schema.Ref = %q, want %q", s.Ref, "#/components/schemas/Widget")
	}
}

// TestSchemaCompositionKeywordsFromYAML asserts that Schema.OneOf, AnyOf, and
// AllOf populate from YAML. Before the fix these had no yaml tags, so
// yaml.v3's fallback looked them up as "oneof", "anyof", "allof" — none of
// which appear in a real spec (the camelCase JSON Schema keywords are
// "oneOf", "anyOf", "allOf") — silently losing all polymorphism information.
func TestSchemaCompositionKeywordsFromYAML(t *testing.T) {
	const doc = `
oneOf:
  - type: string
anyOf:
  - type: integer
allOf:
  - type: boolean
`
	s := unmarshalYAML[Schema](t, doc)

	if len(s.OneOf) != 1 || s.OneOf[0].Type != "string" {
		t.Errorf("Schema.OneOf = %#v, want one element with Type \"string\"", s.OneOf)
	}

	if len(s.AnyOf) != 1 || s.AnyOf[0].Type != "integer" {
		t.Errorf("Schema.AnyOf = %#v, want one element with Type \"integer\"", s.AnyOf)
	}

	if len(s.AllOf) != 1 || s.AllOf[0].Type != "boolean" {
		t.Errorf("Schema.AllOf = %#v, want one element with Type \"boolean\"", s.AllOf)
	}
}

// TestSchemaReadOnlyWriteOnlyFromYAML asserts that Schema.ReadOnly and
// Schema.WriteOnly populate from YAML's camelCase "readOnly"/"writeOnly"
// keys, not the no-tag fallback's "readonly"/"writeonly".
func TestSchemaReadOnlyWriteOnlyFromYAML(t *testing.T) {
	const doc = `
readOnly: true
writeOnly: true
`
	s := unmarshalYAML[Schema](t, doc)

	if !s.ReadOnly {
		t.Error("Schema.ReadOnly = false, want true")
	}

	if !s.WriteOnly {
		t.Error("Schema.WriteOnly = false, want true")
	}
}

// TestSchemaAdditionalPropertiesStaysWorkingFromYAML is a regression guard:
// AdditionalProperties already carried an explicit yaml tag before this
// change (a prior, narrower fix). It must keep working.
func TestSchemaAdditionalPropertiesStaysWorkingFromYAML(t *testing.T) {
	const doc = `
additionalProperties:
  type: string
`
	s := unmarshalYAML[Schema](t, doc)

	m, ok := s.AdditionalProperties.(map[string]any)
	if !ok {
		t.Fatalf("Schema.AdditionalProperties = %#v (%T), want map[string]any", s.AdditionalProperties, s.AdditionalProperties)
	}

	if m["type"] != "string" {
		t.Errorf("Schema.AdditionalProperties[\"type\"] = %#v, want \"string\"", m["type"])
	}
}

// TestOpenAPISpecExternalDocsFromYAML asserts that OpenAPISpec.ExternalDocs
// populates from YAML's "externalDocs" key rather than the no-tag fallback's
// "externaldocs".
func TestOpenAPISpecExternalDocsFromYAML(t *testing.T) {
	const doc = `
openapi: 3.1.0
info:
  title: Test
  version: 1.0.0
paths: {}
externalDocs:
  description: More info
  url: https://example.com/docs
`
	s := unmarshalYAML[OpenAPISpec](t, doc)

	if s.ExternalDocs == nil {
		t.Fatal("OpenAPISpec.ExternalDocs = nil, want a populated *ExternalDocs")
	}

	if s.ExternalDocs.URL != "https://example.com/docs" {
		t.Errorf("OpenAPISpec.ExternalDocs.URL = %q, want %q", s.ExternalDocs.URL, "https://example.com/docs")
	}
}

// TestInfoTermsOfServiceFromYAML asserts that Info.TermsOfService populates
// from YAML's "termsOfService" key rather than the no-tag fallback's
// "termsofservice".
func TestInfoTermsOfServiceFromYAML(t *testing.T) {
	const doc = `
title: Test
version: 1.0.0
termsOfService: https://example.com/terms
`
	info := unmarshalYAML[Info](t, doc)

	if info.TermsOfService != "https://example.com/terms" {
		t.Errorf("Info.TermsOfService = %q, want %q", info.TermsOfService, "https://example.com/terms")
	}
}

// TestDiscriminatorPropertyNameFromYAML asserts that
// Discriminator.PropertyName populates from YAML's "propertyName" key rather
// than the no-tag fallback's "propertyname".
func TestDiscriminatorPropertyNameFromYAML(t *testing.T) {
	const doc = `
propertyName: petType
`
	d := unmarshalYAML[Discriminator](t, doc)

	if d.PropertyName != "petType" {
		t.Errorf("Discriminator.PropertyName = %q, want %q", d.PropertyName, "petType")
	}
}

// TestAsyncAPIMessageIDFromYAML asserts that AsyncAPIMessage.MessageID
// populates from YAML's "messageId" key rather than the no-tag fallback's
// "messageid".
func TestAsyncAPIMessageIDFromYAML(t *testing.T) {
	const doc = `
messageId: userSignedUp
`
	m := unmarshalYAML[AsyncAPIMessage](t, doc)

	if m.MessageID != "userSignedUp" {
		t.Errorf("AsyncAPIMessage.MessageID = %q, want %q", m.MessageID, "userSignedUp")
	}
}

// TestAsyncAPIComponentsSecuritySchemesFromYAML asserts that
// AsyncAPIComponents.SecuritySchemes populates from YAML's "securitySchemes"
// key rather than the no-tag fallback's "securityschemes".
func TestAsyncAPIComponentsSecuritySchemesFromYAML(t *testing.T) {
	const doc = `
securitySchemes:
  apiKey:
    type: httpApiKey
    name: X-API-Key
    in: header
`
	c := unmarshalYAML[AsyncAPIComponents](t, doc)

	scheme, ok := c.SecuritySchemes["apiKey"]
	if !ok {
		t.Fatal("AsyncAPIComponents.SecuritySchemes[\"apiKey\"] not found")
	}

	if scheme.Type != "httpApiKey" {
		t.Errorf("SecuritySchemes[\"apiKey\"].Type = %q, want \"httpApiKey\"", scheme.Type)
	}
}

// TestAsyncAPIInfoExternalDocsFromYAML asserts that
// AsyncAPIInfo.ExternalDocs populates from YAML — one of the nine parent
// structs where ExternalDocs was broken.
func TestAsyncAPIInfoExternalDocsFromYAML(t *testing.T) {
	const doc = `
title: Test
version: 1.0.0
externalDocs:
  url: https://example.com/docs
`
	info := unmarshalYAML[AsyncAPIInfo](t, doc)

	if info.ExternalDocs == nil {
		t.Fatal("AsyncAPIInfo.ExternalDocs = nil, want a populated *ExternalDocs")
	}

	if info.ExternalDocs.URL != "https://example.com/docs" {
		t.Errorf("AsyncAPIInfo.ExternalDocs.URL = %q, want %q", info.ExternalDocs.URL, "https://example.com/docs")
	}
}

// TestAsyncAPIServerProtocolVersionFromYAML asserts that
// AsyncAPIServer.ProtocolVersion populates from YAML's "protocolVersion" key
// rather than the no-tag fallback's "protocolversion".
func TestAsyncAPIServerProtocolVersionFromYAML(t *testing.T) {
	const doc = `
protocol: wss
protocolVersion: "1.0"
`
	s := unmarshalYAML[AsyncAPIServer](t, doc)

	if s.ProtocolVersion != "1.0" {
		t.Errorf("AsyncAPIServer.ProtocolVersion = %q, want %q", s.ProtocolVersion, "1.0")
	}
}
