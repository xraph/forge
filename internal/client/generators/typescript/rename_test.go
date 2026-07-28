package typescript

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/forge/internal/client"
)

// --- Task 5: schema properties emit client-side names, and the codec
// table's `ts` actually differs from the wire name. --------------------
//
// Before this task, objectPropsLiteral and schemaToTypeScript rendered
// schema.Properties' WIRE names verbatim, and codecTable.add set every
// field's `ts` to the wire name too -- so encode/decode were identity and
// the emitted TypeScript never matched the configured FieldNaming strategy
// at all. tsFieldName (fieldname.go) and the collision guard already existed
// and were exercised directly by unit tests, but nothing actually rendered
// through them, which is exactly what these tests close.

// TestObjectPropsLiteralAppliesFieldNaming pins the failing case from the
// task brief: baseSpec()'s "User" schema declares wire property "user_id",
// and under baseConfig()'s default camel strategy the rendered TypeScript
// must show the derived name "userId", not the wire name.
//
// Before this fix, this assertion failed: types.ts contained
// "user_id?: string;" verbatim (objectPropsLiteral and schemaToTypeScript's
// interface case both called tsPropertyKey(propName) directly on the wire
// name, with no call to tsFieldName at all).
func TestObjectPropsLiteralAppliesFieldNaming(t *testing.T) {
	out, err := NewGenerator().Generate(context.Background(), baseSpec(), baseConfig())
	require.NoError(t, err)

	types := out.Files["src/types.ts"]

	userInterface := extractInterfaceBlock(t, types, "User")

	if !strings.Contains(userInterface, "userId?: string;") {
		t.Errorf("expected wire \"user_id\" to render as client field \"userId\" under camel naming, got:\n%s", userInterface)
	}

	// Scoped to the "User" interface specifically: baseSpec()'s streaming
	// boilerplate (Message, Member, ...) declares its own hardcoded
	// "user_id" fields unrelated to schema.Properties rename, so a
	// whole-file search would false-negative on those instead of proving
	// anything about THIS schema's rename.
	if strings.Contains(userInterface, "user_id") {
		t.Errorf("wire name \"user_id\" must not appear verbatim in the User interface, got:\n%s", userInterface)
	}
}

// extractInterfaceBlock returns the `export interface <name> { ... }` block
// from types (up to and including the first closing brace on its own
// line), or fails the test if it cannot be found.
func extractInterfaceBlock(t *testing.T, types, name string) string {
	t.Helper()

	marker := "export interface " + name + " {"

	start := strings.Index(types, marker)
	if start == -1 {
		t.Fatalf("interface %q not found in generated types.ts:\n%s", name, types)
	}

	end := strings.Index(types[start:], "\n}\n")
	if end == -1 {
		t.Fatalf("could not find closing brace for interface %q", name)
	}

	return types[start : start+end]
}

// TestCodecTableTSNameIsDerived pins the second half of the failing case:
// the emitted CODECS entry for "User" must record fields.user_id.ts ==
// "userId", not "user_id". Before this fix, codecTable.add hardcoded
// `TS: prop` (the wire name) for every field, making the codec table's
// encode/decode identity regardless of the configured strategy.
func TestCodecTableTSNameIsDerived(t *testing.T) {
	code, _ := NewCodecGenerator().Generate(baseSpec(), baseConfig())

	assert.Contains(t, code, `"user_id": {"ts": "userId"}`,
		"the codec table must record the DERIVED client name as ts, keyed by the wire name")
}

// TestCodecRuntimeRenamesUserID is the execution proof for the round trip
// the brief specifies directly: decode({user_id:'x'}, 'User') must yield
// {userId:'x'}, and encode must reverse it. A string-only assertion on the
// table (TestCodecTableTSNameIsDerived, above) can show the `ts` mapping
// exists; only actually running the generated walk shows decode/encode use
// it correctly in both directions.
func TestCodecRuntimeRenamesUserID(t *testing.T) {
	dir := t.TempDir()
	code, _ := NewCodecGenerator().Generate(baseSpec(), baseConfig())
	writeTree(t, dir, map[string]string{"src/codecs.ts": code})

	driver := `
import { decode, encode } from './codecs';

const results: Record<string, any> = {};

const decoded = decode({ user_id: 'x' }, 'User');
results.decoded = decoded;

results.encoded = encode(decoded, 'User');

console.log(JSON.stringify(results));
`
	writeTree(t, dir, map[string]string{"src/__driver_rename.ts": driver})

	stdout := runNodeDriver(t, dir, "src/__driver_rename.ts")

	var got struct {
		Decoded map[string]any `json:"decoded"`
		Encoded map[string]any `json:"encoded"`
	}
	decodeLastLine(t, stdout, &got)

	assert.Equal(t, map[string]any{"userId": "x"}, got.Decoded,
		"decode({user_id:'x'}, 'User') must yield {userId:'x'}; driver stdout:\n%s", stdout)
	assert.Equal(t, map[string]any{"user_id": "x"}, got.Encoded,
		"encode must reverse decode back to the wire name; driver stdout:\n%s", stdout)
}

// TestNamingPreserveLeavesFieldsAndCodecUnchanged asserts that under
// NamingPreserve, both the rendered TypeScript AND the codec table keep the
// wire name verbatim -- the rename this task adds must be strategy-gated,
// not unconditional.
func TestNamingPreserveLeavesFieldsAndCodecUnchanged(t *testing.T) {
	config := baseConfig()
	config.FieldNaming = client.NamingPreserve

	out, err := NewGenerator().Generate(context.Background(), baseSpec(), config)
	require.NoError(t, err)

	types := out.Files["src/types.ts"]
	if !strings.Contains(types, "user_id?: string;") {
		t.Errorf("expected wire name \"user_id\" preserved verbatim under NamingPreserve, got:\n%s", types)
	}

	code, _ := NewCodecGenerator().Generate(baseSpec(), config)
	assert.Contains(t, code, `"user_id": {"ts": "user_id"}`,
		"NamingPreserve must keep ts == wire in the codec table too")
}

// --- Phase 2 survival: required optionality, JSDoc, @deprecated, and
// quoted non-identifier keys must all still work once the property key
// itself is a derived name rather than the wire name verbatim. -----------

// TestRenamedPropertySurvivesPhase2Rendering is the single combined
// regression guard the brief asks for explicitly: one schema exercising
// every Phase 2 behavior at once, all on properties that are ALSO renamed
// by this task, so a regression in any of the four could not hide behind a
// property that happens not to need renaming.
func TestRenamedPropertySurvivesPhase2Rendering(t *testing.T) {
	spec := baseSpec()
	spec.Schemas["Mixed"] = &client.Schema{
		Type:     "object",
		Required: []string{"user_id"},
		Properties: map[string]*client.Schema{
			// Required (by its WIRE name) AND renamed: must render with no
			// "?" under its DERIVED name.
			"user_id": {Type: "string"},
			// Renamed, with a description and @deprecated: JSDoc must still
			// be emitted, keyed off the property's schema (unaffected by
			// the rename), immediately above the renamed key.
			"old_flag": {Type: "boolean", Description: "Old flag.", Deprecated: true},
			// Renamed to a name that is STILL not a valid TS identifier
			// (leading digit survives camel derivation): tsPropertyKey must
			// still quote it.
			"3d_tiles": {Type: "string"},
		},
	}

	out, err := NewGenerator().Generate(context.Background(), spec, baseConfig())
	require.NoError(t, err)

	types := out.Files["src/types.ts"]
	mixed := extractInterfaceBlock(t, types, "Mixed")

	// Required property, renamed: no "?".
	assert.Contains(t, mixed, "userId: string;", "a required property must render without \"?\" under its derived name")
	assert.NotContains(t, mixed, "userId?: string;")

	// JSDoc + @deprecated survive on the renamed key. Deprecated forces the
	// multi-line JSDoc form (propertyJSDoc only uses the single-line "/** ... */"
	// shorthand when Deprecated is false), so this matches that form rather
	// than TestPropertyJSDocIsEmitted's non-deprecated "kept" case.
	assert.Contains(t, mixed, "   * Old flag.\n   * @deprecated\n   */")
	assert.Contains(t, mixed, "oldFlag?: boolean;", "the deprecated property must still render under its derived camelCase name")

	// A derived name that remains a non-identifier is still quoted.
	assert.Contains(t, mixed, `"3dTiles"?: string;`, "camel-derivation of \"3d_tiles\" (\"3dTiles\") still starts with a digit and must still be quoted")

	dir := t.TempDir()
	writeTree(t, dir, out.Files)
	errs := typeCheck(t, dir)
	assert.Empty(t, errs, "the combined Phase 2 + rename case must still type-check cleanly")
}

// --- The override-actually-applies proof ---------------------------------
//
// Task 3's review left this half-proven: it verified a FieldOverrides entry
// silences the collision ERROR, but could not verify it takes effect at
// RENDER time, because nothing rendered through tsFieldName yet. These
// tests close that gap directly: they regenerate with the override set and
// assert the OVERRIDE VALUE -- not just the absence of an error -- appears
// in both the rendered types.ts and the codec table's `ts`.

// TestFieldOverrideAppliesAtTopLevel is the simplest case: a top-level
// schema's own property, overridden.
func TestFieldOverrideAppliesAtTopLevel(t *testing.T) {
	config := collisionConfig()
	config.FieldOverrides = map[string]string{"User.user_id": "userIdentifier"}

	out, err := NewGenerator().Generate(context.Background(), collisionSpec(), config)
	require.NoError(t, err)

	types := out.Files["src/types.ts"]
	assert.Contains(t, types, "userIdentifier", "the override value must appear in rendered TypeScript, not just silence the collision error")

	code, _ := NewCodecGenerator().Generate(collisionSpec(), config)
	assert.Contains(t, code, `"user_id": {"ts": "userIdentifier"}`, "the override value must be the codec table's ts, not the strategy-derived name")
}

// TestFieldOverrideAppliesInsideNestedInlineObject proves the override key
// printed for a nested inline-object collision (Order.shipping.street_name)
// resolves at RENDER time, not just at the collision-guard level: the
// override value must appear in the nested object's rendered literal AND in
// the codec table's "Order.shipping" entry.
func TestFieldOverrideAppliesInsideNestedInlineObject(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{Title: "Nested Collision API", Version: "1.0.0"},
		Schemas: map[string]*client.Schema{
			"Order": {
				Type: "object",
				Properties: map[string]*client.Schema{
					"shipping": {
						Type: "object",
						Properties: map[string]*client.Schema{
							"street_name": {Type: "string"},
							"streetName":  {Type: "string"},
						},
					},
				},
			},
		},
	}

	config := collisionConfig()
	config.FieldOverrides = map[string]string{"Order.shipping.street_name": "streetNameAlt"}

	out, err := NewGenerator().Generate(context.Background(), spec, config)
	require.NoError(t, err)

	types := out.Files["src/types.ts"]
	assert.Contains(t, types, "streetNameAlt", "the override value for a nested inline object's property must appear in the rendered object literal")

	code, _ := NewCodecGenerator().Generate(spec, config)
	assert.Contains(t, code, `"Order.shipping": {"kind": "object", "fields": {"streetName": {"ts": "streetName"}, "street_name": {"ts": "streetNameAlt"}}}`,
		"the codec table's \"Order.shipping\" namespace must record the override value as ts")
}

// TestFieldOverrideAppliesAcrossFlattenedAllOf proves the same for the
// flattened-allOf namespace (Addr, composed of a $ref member and an inline
// member): the printed key "Addr.streetName" must resolve at render time in
// both the allOf intersection's rendered object literal and the codec
// table's merged "Addr" entry.
func TestFieldOverrideAppliesAcrossFlattenedAllOf(t *testing.T) {
	spec := allOfFlattenedCollisionSpec()

	config := collisionConfig()
	config.FieldOverrides = map[string]string{"Addr.streetName": "streetNameAlt"}

	out, err := NewGenerator().Generate(context.Background(), spec, config)
	require.NoError(t, err)

	types := out.Files["src/types.ts"]
	assert.Contains(t, types, "streetNameAlt", "the override value for a flattened allOf member's property must appear in the rendered intersection type")

	code, _ := NewCodecGenerator().Generate(spec, config)
	assert.Contains(t, code, `"streetNameAlt"`, "the codec table's merged \"Addr\" entry must record the override value as ts")
	assert.Contains(t, code, `"Addr":`)
}
