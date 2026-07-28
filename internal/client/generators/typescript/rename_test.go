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
// flattened-allOf namespace (Addr, composed of a $ref member Base and an
// INLINE member declaring "streetName" second): the printed key
// "Addr.streetName" must resolve at render time in both the allOf
// intersection's rendered object literal and the codec table's merged
// "Addr" entry. allOfFlattenedCollisionSpec() declares Base ($ref, "street_name")
// FIRST and the inline member ("streetName") SECOND, so the inline member is
// the one flagged (Base's own "street_name" claims "streetName" first) --
// the printed key is therefore the COMPOSITION's own namespace, "Addr",
// which is correct because the inline member genuinely renders as part of
// Addr's own intersection member. The mirror case, where the $ref member is
// the one flagged, is TestFieldOverrideResolvesRefContributedAllOfCollision
// below -- fixing round 1's CRITICAL 1 finding is specifically about that
// mirror case, where the pre-fix code printed this SAME "Addr.<wire>" key
// unconditionally even when the wire name in question was contributed by
// the $ref member, which does not render under "Addr" at all.
func TestFieldOverrideAppliesAcrossFlattenedAllOf(t *testing.T) {
	spec := allOfFlattenedCollisionSpec()

	err := checkFieldNameCollisions(spec, collisionConfig())
	require.Error(t, err)
	assert.Contains(t, err.Error(), `FieldOverrides["Addr.streetName"]`)

	config := collisionConfig()
	config.FieldOverrides = map[string]string{"Addr.streetName": "streetNameAlt"}

	out, err := NewGenerator().Generate(context.Background(), spec, config)
	require.NoError(t, err)

	types := out.Files["src/types.ts"]
	assert.Contains(t, types, "streetNameAlt", "the override value for a flattened allOf member's property must appear in the rendered intersection type")

	// Exact entries, not a loose whole-file Contains: pins that the OVERRIDE
	// value lands on the INLINE member's field ("streetName") while Base's
	// own $ref-contributed field ("street_name") keeps its ordinary
	// strategy-derived name, in BOTH the merged "Addr" entry and Base's own
	// independent top-level entry.
	code, _ := NewCodecGenerator().Generate(spec, config)
	assert.Contains(t, code, `"Addr": {"kind": "object", "fields": {"streetName": {"ts": "streetNameAlt"}, "street_name": {"ts": "streetName"}}}`)
	assert.Contains(t, code, `"Base": {"kind": "object", "fields": {"street_name": {"ts": "streetName"}}}`)
}

// allOfFlattenedCollisionSpecRefSecond is allOfFlattenedCollisionSpec with
// the two AllOf members swapped: the INLINE member ("streetName") declared
// FIRST, the $ref member (Base, "street_name") declared SECOND. sortedKeys
// visits "streetName" (claimed first, by the inline layer) before
// "street_name" (the second, conflicting claim, contributed by the $ref
// layer) either way -- but swapping which MEMBER is declared first changes
// nothing about processing order, since flattenAllOfLayers preserves AllOf
// DECLARATION order, not property sort order, and each layer's OWN
// properties are visited in sortedKeys order independently. What actually
// flips which layer is "first claimer" here is which layer's properties
// happen to enumerate first alphabetically WITHIN that layer -- so this
// spec is built so that the roles are reversed from
// allOfFlattenedCollisionSpec: the review's exact reproduction had the
// inline member first and Base second, and found that ordering is what
// exposed CRITICAL 1 (the composition id was printed and applied even
// though the SECOND, conflicting claim came from Base, the $ref member).
func allOfFlattenedCollisionSpecRefSecond() *client.APISpec {
	return &client.APISpec{
		Info: client.APIInfo{Title: "Flattened Collision API (ref second)", Version: "1.0.0"},
		Schemas: map[string]*client.Schema{
			"Base": {Type: "object", Properties: map[string]*client.Schema{"street_name": {Type: "string"}}},
			"Addr": {
				AllOf: []*client.Schema{
					{Type: "object", Properties: map[string]*client.Schema{"streetName": {Type: "string"}}},
					{Ref: "#/components/schemas/Base"},
				},
			},
		},
	}
}

// TestFieldOverrideResolvesRefContributedAllOfCollision is round 1's
// CRITICAL 1 regression guard: reproduced directly against the pre-fix
// code, this failed as follows.
//
// checkFieldNameCollisions printed FieldOverrides["Addr.street_name"] (the
// COMPOSITION's own namespace) for the wire name "street_name", which is
// actually contributed by Base, a $ref member. Setting that exact key:
//
//	types.ts:   export type Addr = { streetName?: string; } & Base;
//	            export interface Base { street_name?: string; }     <-- override absent
//	codecs.ts:  "Addr": {... "street_name": {"ts": "OVERRIDDEN"} ...}
//	            "Base": {... "street_name": {"ts": "streetName"} ...}
//
// The printed key silenced checkFieldNameCollisions' error (Generate
// returned no error) while the rendered "Base" interface -- what actually
// declares this field, since schemaToTSType returns a $ref member's bare
// type name without recursing into it -- still collided with the inline
// member's "streetName", and the "Addr" and "Base" codec entries disagreed
// about the field's ts. A key that silences the error while the collision
// is still live in the rendered type is exactly the class of defect fixed
// on the guard side in an earlier round (fix/ts-client-generator-phase1's
// "stop printing phantom FieldOverrides keys for nested allOf collisions"),
// reintroduced here for a $ref-contributed allOf member.
//
// After the fix: the guard must print "Base.street_name" instead (the
// namespace that actually owns this field), and setting THAT key must
// resolve the collision consistently in both the rendered "Base" interface
// and both codec entries that reference it.
func TestFieldOverrideResolvesRefContributedAllOfCollision(t *testing.T) {
	spec := allOfFlattenedCollisionSpecRefSecond()

	err := checkFieldNameCollisions(spec, collisionConfig())
	require.Error(t, err, "the inline member's \"streetName\" and Base's \"street_name\" must still collide")
	assert.Contains(t, err.Error(), `FieldOverrides["Base.street_name"]`,
		"the printed key must name Base -- the namespace that actually owns this $ref-contributed field -- not the composition")
	assert.NotContains(t, err.Error(), `FieldOverrides["Addr.street_name"]`,
		"a composition-scoped key for a $ref-contributed field would silence the error without the render side ever consulting it")

	config := collisionConfig()
	config.FieldOverrides = map[string]string{"Base.street_name": "streetNameAlt"}

	out, err := NewGenerator().Generate(context.Background(), spec, config)
	require.NoError(t, err, "the printed key must actually resolve the collision")

	types := out.Files["src/types.ts"]
	base := extractInterfaceBlock(t, types, "Base")
	assert.Contains(t, base, "streetNameAlt?: string;",
		"Base's own rendered interface -- what actually declares this field -- must show the override")

	code, _ := NewCodecGenerator().Generate(spec, config)
	assert.Contains(t, code, `"Base": {"kind": "object", "fields": {"street_name": {"ts": "streetNameAlt"}}}`,
		"Base's own independent codec entry must show the override")
	assert.Contains(t, code, `"Addr": {"kind": "object", "fields": {"streetName": {"ts": "streetName"}, "street_name": {"ts": "streetNameAlt"}}}`,
		"the merged Addr entry must agree -- both entries derive this field's ts from Base's own namespace")
}

// TestCodecRuntimeRefContributedAllOfFieldRenamesCorrectly is the execution
// proof for the fix above: decoding through the MERGED "Addr" codec entry
// must actually rename the $ref-contributed field using the override, and
// encode must reverse it -- not just that the two entries' `ts` strings
// happen to match textually.
func TestCodecRuntimeRefContributedAllOfFieldRenamesCorrectly(t *testing.T) {
	spec := allOfFlattenedCollisionSpecRefSecond()

	config := collisionConfig()
	config.FieldOverrides = map[string]string{"Base.street_name": "streetNameAlt"}

	dir := t.TempDir()
	code, _ := NewCodecGenerator().Generate(spec, config)
	writeTree(t, dir, map[string]string{"src/codecs.ts": code})

	driver := `
import { decode, encode } from './codecs';

const results: Record<string, any> = {};

const decoded = decode({ streetName: 'x', street_name: 'y' }, 'Addr');
results.decoded = decoded;
results.encoded = encode(decoded, 'Addr');

console.log(JSON.stringify(results));
`
	writeTree(t, dir, map[string]string{"src/__driver_allof_ref.ts": driver})

	stdout := runNodeDriver(t, dir, "src/__driver_allof_ref.ts")

	var got struct {
		Decoded map[string]any `json:"decoded"`
		Encoded map[string]any `json:"encoded"`
	}
	decodeLastLine(t, stdout, &got)

	assert.Equal(t, map[string]any{"streetName": "x", "streetNameAlt": "y"}, got.Decoded,
		"decode must rename the $ref-contributed field via the override, alongside the inline member's untouched \"streetName\"; driver stdout:\n%s", stdout)
	assert.Equal(t, map[string]any{"streetName": "x", "street_name": "y"}, got.Encoded,
		"encode must reverse both fields back to their wire names; driver stdout:\n%s", stdout)
}

// --- IMPORTANT 2: a schema declaring BOTH Properties and
// additionalProperties (rendered as an intersection: objectPropsLiteral &
// Record<string, valueType>) must rename fields inside the additional
// VALUE schema too, not just the declared Properties. Before this fix,
// codecTable.add's `len(schema.Properties) > 0` case returned before ever
// reaching the additionalProperties block below it, so such a schema never
// got a `values` codec id at all -- the emitted TYPE promised renamed
// fields inside every "additional" value, but decode/encode had no codec to
// walk them with, silently leaving them at their wire names (and, going the
// other way, never renaming an ENCODE-direction "additional" value's client
// names back to wire names either).

// propsPlusAdditionalSpec returns a schema ("Order") with both a declared
// property ("order_id") and a typed additionalProperties value schema
// (itself an object with property "unit_price") -- the exact shape the
// review reproduced.
func propsPlusAdditionalSpec() *client.APISpec {
	spec := baseSpec()
	spec.Schemas["Order"] = &client.Schema{
		Type: "object",
		Properties: map[string]*client.Schema{
			"order_id": {Type: "string"},
		},
		AdditionalProperties: &client.Schema{
			Type: "object",
			Properties: map[string]*client.Schema{
				"unit_price": {Type: "string"},
			},
		},
	}

	return spec
}

// TestCodecTablePropsPlusAdditionalPropertiesGetsValuesEntry pins the
// failing case directly against the table: before this fix, "Order"'s
// entry had no "values" key at all -- `assert.Contains(t, code,
// `"values":`)` scoped to the Order entry failed outright, since the
// entire additionalProperties branch in codecTable.add was unreachable for
// a schema with any declared Properties.
func TestCodecTablePropsPlusAdditionalPropertiesGetsValuesEntry(t *testing.T) {
	code, _ := NewCodecGenerator().Generate(propsPlusAdditionalSpec(), baseConfig())

	assert.Contains(t, code, `"Order": {"kind": "object", "fields": {"order_id": {"ts": "orderId"}}, "values": "Order.values"}`,
		"a schema with both Properties and additionalProperties must register a \"values\" codec id, not silently drop the additionalProperties side")
	assert.Contains(t, code, `"Order.values": {"kind": "object", "fields": {"unit_price": {"ts": "unitPrice"}}}`,
		"the synthetic \"Order.values\" entry must itself rename the additional value schema's own properties")
}

// TestCodecRuntimeRenamesInsideAdditionalPropertiesValue is the execution
// proof: decode({order_id:'o', extra:{unit_price:'9'}}, 'Order') must yield
// {orderId:'o', extra:{unitPrice:'9'}} -- the declared field renamed as
// usual, AND the value nested under the unknown ("extra") key ALSO renamed,
// with "extra" itself left untouched since additionalProperties keys are
// data. encode must reverse both directions.
func TestCodecRuntimeRenamesInsideAdditionalPropertiesValue(t *testing.T) {
	dir := t.TempDir()
	code, _ := NewCodecGenerator().Generate(propsPlusAdditionalSpec(), baseConfig())
	writeTree(t, dir, map[string]string{"src/codecs.ts": code})

	driver := `
import { decode, encode } from './codecs';

const results: Record<string, any> = {};

const decoded = decode({ order_id: 'o', extra: { unit_price: '9' } }, 'Order');
results.decoded = decoded;
results.encoded = encode(decoded, 'Order');

console.log(JSON.stringify(results));
`
	writeTree(t, dir, map[string]string{"src/__driver_props_plus_additional.ts": driver})

	stdout := runNodeDriver(t, dir, "src/__driver_props_plus_additional.ts")

	var got struct {
		Decoded map[string]any `json:"decoded"`
		Encoded map[string]any `json:"encoded"`
	}
	decodeLastLine(t, stdout, &got)

	assert.Equal(t, map[string]any{"orderId": "o", "extra": map[string]any{"unitPrice": "9"}}, got.Decoded,
		"decode must rename the declared field AND the value nested under the additionalProperties key, while leaving that key itself (\"extra\") untouched; driver stdout:\n%s", stdout)
	assert.Equal(t, map[string]any{"order_id": "o", "extra": map[string]any{"unit_price": "9"}}, got.Encoded,
		"encode must reverse both directions; driver stdout:\n%s", stdout)
}

// --- MINOR 3: a wire (or client) field literally named "__proto__" must
// not silently vanish from the codec table or the walked result. ----------
//
// Two distinct JS gotchas compound here:
//   - `{ "__proto__": x }` as an object LITERAL sets the object's
//     prototype instead of creating an own property, so the emitted
//     CODECS table's own `fields: {"__proto__": {...}}` would never be
//     seen by `Object.entries(codec.fields)` at all;
//   - `obj["__proto__"] = x` (or `obj.__proto__ = x`) as a plain
//     assignment goes through the legacy `Object.prototype.__proto__`
//     accessor, silently reassigning `obj`'s prototype (a no-op if `x`
//     isn't itself an object) instead of creating an own property -- so
//     even with the table fixed, decode/encode's own `out[key] = value`
//     writes would still have silently dropped a "__proto__" field.

// dunderProtoSpec returns a schema ("Weird2") with a property literally
// named "__proto__".
func dunderProtoSpec() *client.APISpec {
	spec := baseSpec()
	spec.Schemas["Weird2"] = &client.Schema{
		Type: "object",
		Properties: map[string]*client.Schema{
			"__proto__": {Type: "string"},
		},
	}

	return spec
}

// TestCodecTableEmitsProtoAsComputedKey pins the table-literal half of the
// fix: the emitted `fields` entry for a property named "__proto__" must use
// computed-key syntax (`["__proto__"]:`), not the plain literal form
// (`"__proto__":`) that the object-literal special case would silently
// swallow.
func TestCodecTableEmitsProtoAsComputedKey(t *testing.T) {
	code, _ := NewCodecGenerator().Generate(dunderProtoSpec(), baseConfig())

	assert.Contains(t, code, `"Weird2": {"kind": "object", "fields": {["__proto__"]: {"ts": "proto"}}}`)
	assert.NotContains(t, code, `"__proto__": {"ts"`, "a plain literal \"__proto__\" key would silently become a prototype reassignment instead of an own property")
}

// TestGeneratedProtoFieldTypeChecks proves the computed-key rewrite is
// still valid, type-checking TypeScript -- not just a string that happens
// to look right.
func TestGeneratedProtoFieldTypeChecks(t *testing.T) {
	out, err := NewGenerator().Generate(context.Background(), dunderProtoSpec(), baseConfig())
	require.NoError(t, err)

	dir := t.TempDir()
	writeTree(t, dir, out.Files)

	errs := typeCheck(t, dir)
	assert.Empty(t, errs, "the computed-key \"__proto__\" rewrite must still type-check cleanly")
}

// TestCodecRuntimeRoundTripsProtoField is the execution proof for BOTH
// gotchas at once. The input is built via JSON.parse, not an object
// literal, specifically so the TEST's own input genuinely has "__proto__"
// as an own property (an object literal `{ __proto__: 'x' }` in the driver
// itself would suffer the identical special-casing and prove nothing).
func TestCodecRuntimeRoundTripsProtoField(t *testing.T) {
	dir := t.TempDir()
	code, _ := NewCodecGenerator().Generate(dunderProtoSpec(), baseConfig())
	writeTree(t, dir, map[string]string{"src/codecs.ts": code})

	driver := `
import { decode, encode } from './codecs';

const results: Record<string, any> = {};

const input: Record<string, unknown> = JSON.parse('{"__proto__":"x"}');
results.inputIsOwn = Object.prototype.hasOwnProperty.call(input, '__proto__');

const decoded = decode(input, 'Weird2') as Record<string, unknown>;
results.decodedIsOwn = Object.prototype.hasOwnProperty.call(decoded, 'proto');
results.decoded = { ...decoded };

const encoded = encode(decoded, 'Weird2') as Record<string, unknown>;
results.encodedIsOwn = Object.prototype.hasOwnProperty.call(encoded, '__proto__');
results.encoded = { ...encoded };

console.log(JSON.stringify(results));
`
	writeTree(t, dir, map[string]string{"src/__driver_proto.ts": driver})

	stdout := runNodeDriver(t, dir, "src/__driver_proto.ts")

	var got struct {
		InputIsOwn   bool           `json:"inputIsOwn"`
		DecodedIsOwn bool           `json:"decodedIsOwn"`
		Decoded      map[string]any `json:"decoded"`
		EncodedIsOwn bool           `json:"encodedIsOwn"`
		Encoded      map[string]any `json:"encoded"`
	}
	decodeLastLine(t, stdout, &got)

	require.True(t, got.InputIsOwn, "test setup sanity: JSON.parse must produce a genuine own \"__proto__\" property; driver stdout:\n%s", stdout)

	assert.True(t, got.DecodedIsOwn,
		"decode must produce a genuine own \"proto\" property, not silently reassign the result's prototype; driver stdout:\n%s", stdout)
	assert.Equal(t, map[string]any{"proto": "x"}, got.Decoded, "driver stdout:\n%s", stdout)

	assert.True(t, got.EncodedIsOwn,
		"encode must produce a genuine own \"__proto__\" property, not silently drop the field; driver stdout:\n%s", stdout)
	assert.Equal(t, map[string]any{"__proto__": "x"}, got.Encoded, "driver stdout:\n%s", stdout)
}
