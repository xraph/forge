package shared

import (
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// sharedStructsUnderTest lists every exported struct type declared in
// openapi.go and asyncapi.go. TestJSONYAMLTagParity walks each one via
// reflection; add new structs here as they are introduced so the guard
// keeps covering the whole package.
var sharedStructsUnderTest = []any{
	// openapi.go
	OpenAPIConfig{},
	OpenAPIServer{},
	ServerVariable{},
	SecurityScheme{},
	OAuthFlows{},
	OAuthFlow{},
	OpenAPITag{},
	ExternalDocs{},
	Contact{},
	License{},
	OpenAPISpec{},
	Info{},
	PathItem{},
	Operation{},
	Parameter{},
	RequestBody{},
	Response{},
	MediaType{},
	Schema{},
	Discriminator{},
	Example{},
	Header{},
	Link{},
	Encoding{},
	Components{},

	// asyncapi.go
	AsyncAPIConfig{},
	AsyncAPISpec{},
	AsyncAPIInfo{},
	AsyncAPIServer{},
	AsyncAPIServerBindings{},
	WebSocketServerBinding{},
	HTTPServerBinding{},
	AsyncAPIChannel{},
	AsyncAPIChannelBindings{},
	WebSocketChannelBinding{},
	HTTPChannelBinding{},
	AsyncAPIServerReference{},
	AsyncAPIParameter{},
	AsyncAPIOperation{},
	AsyncAPIChannelReference{},
	AsyncAPIMessageReference{},
	AsyncAPIOperationBindings{},
	WebSocketOperationBinding{},
	HTTPOperationBinding{},
	AsyncAPIOperationTrait{},
	AsyncAPIOperationReply{},
	AsyncAPIOperationReplyAddress{},
	AsyncAPIMessage{},
	AsyncAPICorrelationID{},
	AsyncAPIMessageBindings{},
	WebSocketMessageBinding{},
	HTTPMessageBinding{},
	AsyncAPIMessageExample{},
	AsyncAPIMessageTrait{},
	AsyncAPIComponents{},
	AsyncAPISecurityScheme{},
	AsyncAPIOAuthFlows{},
	AsyncAPITag{},
}

// yamlDecodeKey reproduces gopkg.in/yaml.v3's own field-name resolution: the
// name portion of an explicit `yaml` tag if present, otherwise the field name
// lowercased. This mirrors yaml.v3's fieldInfo logic in yaml.go so the test
// asserts against the library's real behaviour, not an assumption about it.
func yamlDecodeKey(field reflect.StructField) (key string, skip bool) {
	tag, ok := field.Tag.Lookup("yaml")
	if !ok {
		return strings.ToLower(field.Name), false
	}

	parts := strings.Split(tag, ",")
	if parts[0] == "-" {
		return "", true
	}

	if parts[0] == "" {
		return strings.ToLower(field.Name), false
	}

	return parts[0], false
}

// TestJSONYAMLTagParity is a reflection-based guard: for every exported field
// with a `json` tag (excluding json:"-") on every struct in
// sharedStructsUnderTest, a `yaml` tag must be present whose key matches the
// json tag's key exactly (including the value before the first comma, so
// "$ref" is compared as "$ref" not "ref"). Without an explicit yaml tag,
// gopkg.in/yaml.v3 falls back to strings.ToLower(FieldName) when decoding
// YAML, which silently diverges from the json key for anything but an
// already-lowercase single-word name or a name containing punctuation like
// "$ref". This test is the permanent guard against the next field someone
// adds to either struct without a matching yaml tag.
func TestJSONYAMLTagParity(t *testing.T) {
	for _, sample := range sharedStructsUnderTest {
		typ := reflect.TypeOf(sample)

		t.Run(typ.Name(), func(t *testing.T) {
			for i := 0; i < typ.NumField(); i++ {
				field := typ.Field(i)
				if !field.IsExported() {
					continue
				}

				jsonTag, hasJSON := field.Tag.Lookup("json")
				if !hasJSON {
					continue
				}

				jsonName := strings.Split(jsonTag, ",")[0]
				if jsonName == "-" || jsonName == "" {
					continue
				}

				yamlKey, skip := yamlDecodeKey(field)
				if skip {
					t.Errorf("field %s.%s: yaml tag is \"-\" but json tag is %q; field is unreachable from YAML", typ.Name(), field.Name, jsonTag)
					continue
				}

				if yamlKey != jsonName {
					t.Errorf("field %s.%s: yaml-decoded key %q does not match json key %q (json tag %q) — add `yaml:%q` to this field",
						typ.Name(), field.Name, yamlKey, jsonName, jsonTag, jsonTag)
				}
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
