package shared

import (
	"encoding/json"
	"fmt"
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
	hasJSON    bool
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

		fields = append(fields, extractFields(t, path, file)...)
	}

	return fields
}

// extractFields walks every top-level struct type declared in file and
// returns one astField per exported, named field. path is used only to
// prefix error/fatal messages so they point at the right source location.
//
// Unlike an earlier version of this function, a field with no json tag at
// all is still returned (with hasJSON: false) rather than skipped. Skipping
// it was the guard's blind spot: OAuthFlows.ClientCredentials and
// .AuthorizationCode had no struct tags whatsoever, so they never became an
// astField and therefore never became a TestJSONYAMLTagParity subtest —
// putting a deliberately wrong yaml tag on one of them didn't turn the guard
// red, because the field was invisible to it. Callers (runTagParityCheck)
// now decide what a tagless field means for a given struct.
func extractFields(t *testing.T, path string, file *ast.File) []astField {
	t.Helper()

	var fields []astField

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
						hasJSON:    hasJSON,
						yamlTag:    yamlTag,
						hasYAML:    hasYAML,
					})
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

// goOnlyStructs lists structs in openapi.go/asyncapi.go that are exclusively
// constructed and consumed in Go code and are never unmarshalled from (or
// marshalled to) a spec file on disk. AsyncAPIConfig is populated only via
// forge.WithAsyncAPI(...) in application code and consumed only by the
// AsyncAPI generator — there is no YAML/JSON unmarshal call site for it
// anywhere in the codebase — so its fields are exempt from the "every
// exported field must declare an explicit json tag" rule enforced by
// runTagParityCheck below.
//
// This is a small, explicit, reviewed allowlist rather than an automatic
// "skip a struct if every one of its fields is tagless" heuristic. That
// heuristic looks appealing at first — an all-tagless struct does look like
// a Go-only config type — but it is unsafe here: OAuthFlows, the very
// struct whose four untagged fields motivated this guard extension, also
// has every field tagless. A heuristic keyed on "all fields tagless" would
// have exempted exactly the struct this guard needs to catch. Adding an
// entry to this map is a deliberate decision made after confirming (as was
// done for AsyncAPIConfig — see the review notes on this change) that the
// struct is genuinely never serialized; don't add one just to silence a
// failure.
var goOnlyStructs = map[string]bool{
	"AsyncAPIConfig": true,
}

// tFailer is the minimal subset of *testing.T that runTagParityCheck needs.
// Extracting it lets the check run either as a normal subtest (via
// *testing.T) or against recordingT in the guard's own regression tests
// (TestTaglessFieldInSpecStructFailsTagParityGuard,
// TestTaglessFieldInGoOnlyStructIsExempt), which assert on the check's
// pass/fail outcome directly instead of shelling out to a nested `go test`.
type tFailer interface {
	Helper()
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
}

// runTagParityCheck is the per-field body of TestJSONYAMLTagParity. It
// enforces two rules, in order:
//
//  1. Every exported field on a struct not listed in goOnlyStructs must
//     declare an explicit json tag. Before this rule existed, a field with
//     no tags at all was invisible to the guard: extractFields's predecessor
//     skipped it before ever constructing an astField, so it never became a
//     subtest and no assertion — right or wrong — could run against it. That
//     is exactly how OAuthFlows.ClientCredentials and .AuthorizationCode
//     went unnoticed (arriving nil from every YAML-sourced spec) while a
//     deliberately-wrong yaml tag planted on the same field during review
//     left the old guard green. These are wire-format structs; every field
//     should declare its own name rather than lean on encoding/json's
//     lenient case-insensitive unmarshal fallback, which gopkg.in/yaml.v3
//     does not share.
//
//  2. A field the json tag hides (json:"-") must be hidden from YAML too,
//     via yaml:"-". Leaning on the no-tag fallback here does not merely risk
//     a wrong key, it invents one: AsyncAPISpec.Extensions carried json:"-"
//     and no yaml tag, so every emitted YAML document grew a bogus top-level
//     "extensions" key that the same document in JSON correctly omitted.
//
//  3. For every field that does have a json tag (excluding json:"-"), an
//     explicit yaml tag must be present whose key matches the json tag's key
//     exactly (including the value before the first comma, so "$ref" is
//     compared as "$ref" not "ref"), and whose option list (in practice,
//     "omitempty") matches too.
//
//     The explicitness requirement is load-bearing on its own, and used to be
//     the guard's blind spot: a field with no yaml tag was let through
//     whenever strings.ToLower(FieldName) happened to equal the json key, and
//     the option comparison below was then skipped entirely because there was
//     no yaml tag to compare against. yaml.v3 does not read json tags, so
//     those fields silently lost their omitempty and every zero value was
//     emitted -- `type: ""`, `maximum: 0`, `contact: null` -- output a strict
//     OpenAPI validator rejects, and in which `maximum: 0` is a constraint
//     the author never wrote. Requiring the tag rather than accepting a
//     coincidence is what keeps the option check reachable for every field.
func runTagParityCheck(t tFailer, f astField) {
	t.Helper()

	if goOnlyStructs[f.structName] {
		return
	}

	if !f.hasJSON {
		t.Fatalf("field %s.%s has no json tag; spec structs in openapi.go/asyncapi.go must declare an explicit wire-format name via `json:\"...\"` (or, if %s is genuinely never serialized to/from a spec file, add it to goOnlyStructs in yaml_tags_test.go)",
			f.structName, f.fieldName, f.structName)

		return
	}

	jsonParts := strings.Split(f.jsonTag, ",")
	jsonName := jsonParts[0]

	if jsonName == "-" {
		if !f.hasYAML || strings.Split(f.yamlTag, ",")[0] != "-" {
			t.Errorf("field %s.%s: json tag is %q but the yaml tag does not hide the field (yaml tag present: %t, value %q); yaml.v3 ignores json tags and will emit this field under %q — add `yaml:\"-\"`",
				f.structName, f.fieldName, f.jsonTag, f.hasYAML, f.yamlTag, strings.ToLower(f.fieldName))
		}

		return
	}

	if jsonName == "" {
		return
	}

	if !f.hasYAML {
		t.Errorf("field %s.%s: no yaml tag (json tag %q); yaml.v3 reads neither the json key nor its omitempty, so the field is emitted under %q with its zero value in every YAML document — add `yaml:%q`",
			f.structName, f.fieldName, f.jsonTag, strings.ToLower(f.fieldName), f.jsonTag)

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

	jsonOpts := sortedOptions(jsonParts[1:])
	yamlOpts := sortedOptions(strings.Split(f.yamlTag, ",")[1:])

	if !reflect.DeepEqual(jsonOpts, yamlOpts) {
		t.Errorf("field %s.%s: yaml tag options %v do not match json tag options %v (json %q, yaml %q)",
			f.structName, f.fieldName, yamlOpts, jsonOpts, f.jsonTag, f.yamlTag)
	}
}

// TestJSONYAMLTagParity is a source-level guard, driven by parseStructFields
// and runTagParityCheck above: every exported field on every struct declared
// in openapi.go and asyncapi.go must declare an explicit json tag (unless
// its struct is in goOnlyStructs), and wherever a json tag is present a
// matching yaml tag must be present too. See runTagParityCheck's doc comment
// for the full rule and its rationale.
//
// This test is the permanent guard against the next field — or the next
// whole struct — someone adds to either file without a matching yaml tag,
// or without any tag at all.
func TestJSONYAMLTagParity(t *testing.T) {
	fields := parseStructFields(t, "openapi.go", "asyncapi.go")
	if len(fields) == 0 {
		t.Fatal("parseStructFields returned no fields; the AST walk is broken")
	}

	for _, f := range fields {
		f := f

		t.Run(f.structName+"."+f.fieldName, func(t *testing.T) {
			runTagParityCheck(t, f)
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

// TestOAuthFlowsClientCredentialsAndAuthorizationCodeFromYAML asserts that
// OAuthFlows.ClientCredentials and OAuthFlows.AuthorizationCode populate from
// YAML's camelCase "clientCredentials"/"authorizationCode" keys. Before the
// fix, OAuthFlows had no struct tags at all on any of its four fields, so
// yaml.v3's no-tag fallback looked them up as "clientcredentials" and
// "authorizationcode" (strings.ToLower of the Go field name) — keys that
// never appear in a real OpenAPI document, so these two flows silently
// decoded as nil for every YAML-sourced spec. (Implicit and Password
// happened to decode correctly by coincidence: their Go names are already a
// single lowercase word, so the no-tag fallback's lowercased key matches the
// real "implicit"/"password" keys.)
func TestOAuthFlowsClientCredentialsAndAuthorizationCodeFromYAML(t *testing.T) {
	const doc = `
implicit:
  authorizationUrl: https://example.com/authorize
password:
  tokenUrl: https://example.com/token
clientCredentials:
  tokenUrl: https://example.com/client-credentials-token
authorizationCode:
  authorizationUrl: https://example.com/authorize
  tokenUrl: https://example.com/authorization-code-token
`
	f := unmarshalYAML[OAuthFlows](t, doc)

	if f.ClientCredentials == nil {
		t.Fatal("OAuthFlows.ClientCredentials = nil, want a populated *OAuthFlow (yaml key \"clientCredentials\")")
	}

	if f.ClientCredentials.TokenURL != "https://example.com/client-credentials-token" {
		t.Errorf("OAuthFlows.ClientCredentials.TokenURL = %q, want %q", f.ClientCredentials.TokenURL, "https://example.com/client-credentials-token")
	}

	if f.AuthorizationCode == nil {
		t.Fatal("OAuthFlows.AuthorizationCode = nil, want a populated *OAuthFlow (yaml key \"authorizationCode\")")
	}

	if f.AuthorizationCode.TokenURL != "https://example.com/authorization-code-token" {
		t.Errorf("OAuthFlows.AuthorizationCode.TokenURL = %q, want %q", f.AuthorizationCode.TokenURL, "https://example.com/authorization-code-token")
	}
}

// TestOAuthFlowsJSONMarshalUsesCamelCaseKeys asserts that marshalling
// OAuthFlows to JSON emits the OpenAPI-correct camelCase keys
// ("clientCredentials", "authorizationCode") rather than Go's default
// capitalized field names ("ClientCredentials", "AuthorizationCode"). Before
// the fix, OAuthFlows had no json tags, so encoding/json fell back to the
// literal Go field name on marshal — decode was accidentally lenient
// (case-insensitive field matching) but encode was not, so any OAuthFlows
// round-tripped through this codebase's own marshaller would emit spec-
// incompatible keys.
func TestOAuthFlowsJSONMarshalUsesCamelCaseKeys(t *testing.T) {
	flows := OAuthFlows{
		Implicit:          &OAuthFlow{AuthorizationURL: "https://example.com/authorize"},
		Password:          &OAuthFlow{TokenURL: "https://example.com/token"},
		ClientCredentials: &OAuthFlow{TokenURL: "https://example.com/client-credentials-token"},
		AuthorizationCode: &OAuthFlow{TokenURL: "https://example.com/authorization-code-token"},
	}

	b, err := json.Marshal(flows)
	if err != nil {
		t.Fatalf("json.Marshal(OAuthFlows) failed: %v", err)
	}

	s := string(b)

	for _, want := range []string{`"implicit"`, `"password"`, `"clientCredentials"`, `"authorizationCode"`} {
		if !strings.Contains(s, want) {
			t.Errorf("marshalled OAuthFlows missing expected camelCase key %s: %s", want, s)
		}
	}

	for _, unwanted := range []string{`"Implicit"`, `"Password"`, `"ClientCredentials"`, `"AuthorizationCode"`} {
		if strings.Contains(s, unwanted) {
			t.Errorf("marshalled OAuthFlows still emits capitalized Go field name %s: %s", unwanted, s)
		}
	}
}

// parseStructFieldsFromSource is like parseStructFields but parses supplied
// source text directly instead of reading openapi.go/asyncapi.go off disk.
// This lets the guard's own field-discovery logic be exercised against
// synthetic structs, independent of whatever real structs currently exist in
// the shared package — used by TestTaglessFieldIsSurfacedByParser below to
// prove the parser's blind spot (skipping fields with no json tag entirely)
// without needing to temporarily vandalize openapi.go/asyncapi.go for a
// permanent test.
func parseStructFieldsFromSource(t *testing.T, filename, src string) []astField {
	t.Helper()

	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, filename, src, 0)
	if err != nil {
		t.Fatalf("parse synthetic source %s: %v", filename, err)
	}

	return extractFields(t, filename, file)
}

// TestTaglessFieldIsSurfacedByParser proves parseStructFields no longer has
// a blind spot for exported fields with no json tag at all. Before the fix,
// parseStructFields did `jsonTag, hasJSON := tag.Lookup("json"); if !hasJSON
// { continue }`, which meant a field like OAuthFlows.ClientCredentials (no
// tag of any kind) never became an astField and therefore never became a
// subtest — TestJSONYAMLTagParity was structurally incapable of catching it,
// no matter how wrong the field was. This test parses a synthetic struct
// with one tagless field and asserts that field is present in the returned
// []astField with hasJSON == false, so the tag-parity test (and any
// allowlist logic layered on top of it) actually gets a chance to see it and
// fail.
func TestTaglessFieldIsSurfacedByParser(t *testing.T) {
	const src = `package shared

type SyntheticSpecStruct struct {
	Foo string
	Bar string ` + "`json:\"bar\"`" + `
}
`
	fields := parseStructFieldsFromSource(t, "synthetic.go", src)

	var found bool

	for _, f := range fields {
		if f.structName == "SyntheticSpecStruct" && f.fieldName == "Foo" {
			found = true

			if f.hasJSON {
				t.Errorf("SyntheticSpecStruct.Foo: hasJSON = true, want false (field has no json tag in the synthetic source)")
			}
		}
	}

	if !found {
		t.Fatal("SyntheticSpecStruct.Foo was not returned by parseStructFieldsFromSource; the parser is still silently skipping fields with no json tag — this is the exact blind spot that let OAuthFlows.ClientCredentials/.AuthorizationCode hide from the guard")
	}
}

// TestTaglessFieldInSpecStructFailsTagParityGuard proves the extended guard
// rule end-to-end: an exported field with no json tag on a struct that is
// NOT in the go-only allowlist must produce a t.Errorf/t.Fatalf from
// runTagParityCheck (the extracted per-field check used by
// TestJSONYAMLTagParity). This exercises the actual assertion function
// rather than re-deriving its logic, so a future edit to the rule can't
// silently diverge between the real guard and this regression test.
func TestTaglessFieldInSpecStructFailsTagParityGuard(t *testing.T) {
	f := astField{
		structName: "SyntheticSpecStruct",
		fieldName:  "Foo",
		hasJSON:    false,
	}

	rt := &recordingT{}
	runTagParityCheck(rt, f)

	if !rt.failed {
		t.Fatal("runTagParityCheck did not fail for a tagless exported field on a non-allowlisted struct; the guard's blind spot is not closed")
	}
}

// TestTaglessFieldInGoOnlyStructIsExempt proves the false-positive side of
// the same rule: a tagless field on a struct named in goOnlyStructs (e.g.
// AsyncAPIConfig, which is only ever constructed in Go code via
// forge.WithAsyncAPI(...) and never unmarshalled from a spec file) must NOT
// fail, so the stricter rule doesn't turn every one of AsyncAPIConfig's
// intentionally-untagged fields into a spurious failure.
func TestTaglessFieldInGoOnlyStructIsExempt(t *testing.T) {
	f := astField{
		structName: "AsyncAPIConfig",
		fieldName:  "Title",
		hasJSON:    false,
	}

	rt := &recordingT{}
	runTagParityCheck(rt, f)

	if rt.failed {
		t.Fatalf("runTagParityCheck failed for AsyncAPIConfig.Title, want no failure: %v", rt.errors)
	}
}

// TestMissingYAMLTagFailsTagParityGuard proves the guard no longer accepts a
// field that has a json tag but no yaml tag, even when yaml.v3's no-tag
// fallback happens to produce the same key. That coincidence is what let 126
// of openapi.go's fields and 144 of asyncapi.go's carry json omitempty with
// no yaml omitempty: the key matched, so the field passed, and the option
// comparison never ran because there was no yaml tag to compare. The emitted
// YAML was four times the size of the JSON and full of `type: ""` and
// `maximum: 0` as a result.
func TestMissingYAMLTagFailsTagParityGuard(t *testing.T) {
	f := astField{
		structName: "SyntheticSpecStruct",
		fieldName:  "Maximum",
		jsonTag:    "maximum,omitempty",
		hasJSON:    true,
	}

	rt := &recordingT{}
	runTagParityCheck(rt, f)

	if !rt.failed {
		t.Fatal("runTagParityCheck accepted a field with json:\"maximum,omitempty\" and no yaml tag; yaml.v3 would emit `maximum: 0` for every zero value")
	}
}

// TestJSONHiddenFieldRequiresYAMLHiddenTag proves the json:"-" half of the
// same rule. AsyncAPISpec.Extensions carried json:"-" and no yaml tag, so
// yaml.v3 emitted a top-level "extensions" key — present in no OpenAPI or
// AsyncAPI document and absent from this codebase's own JSON output.
func TestJSONHiddenFieldRequiresYAMLHiddenTag(t *testing.T) {
	hidden := astField{
		structName: "SyntheticSpecStruct",
		fieldName:  "Extensions",
		jsonTag:    "-",
		hasJSON:    true,
	}

	rt := &recordingT{}
	runTagParityCheck(rt, hidden)

	if !rt.failed {
		t.Fatal("runTagParityCheck accepted json:\"-\" with no yaml tag; yaml.v3 would emit the field as \"extensions\"")
	}

	hidden.yamlTag, hidden.hasYAML = "-", true

	rt = &recordingT{}
	runTagParityCheck(rt, hidden)

	if rt.failed {
		t.Fatalf("runTagParityCheck failed for json:\"-\" yaml:\"-\", want no failure: %v", rt.errors)
	}
}

// recordingT is a minimal stand-in for *testing.T that records
// Errorf/Fatalf calls instead of acting on them, so runTagParityCheck's
// pass/fail outcome can be asserted on directly without spawning a nested
// `go test -run` subprocess.
type recordingT struct {
	failed bool
	errors []string
}

func (r *recordingT) Helper() {}

func (r *recordingT) Errorf(format string, args ...any) {
	r.failed = true
	r.errors = append(r.errors, fmt.Sprintf(format, args...))
}

func (r *recordingT) Fatalf(format string, args ...any) {
	r.failed = true
	r.errors = append(r.errors, fmt.Sprintf(format, args...))
}
