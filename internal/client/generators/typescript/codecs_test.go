package typescript

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/forge/internal/client"
)

// nestedSpec returns baseSpec() plus a schema exercising every composite
// codec kind: a $ref property, an array of $refs, and a record.
func nestedSpec() *client.APISpec {
	spec := baseSpec()
	spec.Schemas["Nested"] = &client.Schema{
		Type: "object",
		Properties: map[string]*client.Schema{
			"user":  {Ref: "#/components/schemas/User"},
			"items": {Type: "array", Items: &client.Schema{Ref: "#/components/schemas/User"}},
			"tags":  {Type: "object", AdditionalProperties: &client.Schema{Type: "string"}},
		},
	}

	return spec
}

func TestCodecTableShape(t *testing.T) {
	code, _ := NewCodecGenerator().Generate(nestedSpec(), baseConfig())

	assert.Contains(t, code, "export const CODECS:")
	assert.Contains(t, code, `"User":`)
	assert.Contains(t, code, `"kind": "object"`)
	assert.Contains(t, code, `"kind": "array"`)
	assert.Contains(t, code, `"kind": "record"`)
	assert.Contains(t, code, "export function decode")
	assert.Contains(t, code, "export function encode")
}

// TestCodecTableSyntheticIDsAreDerivedFromPath pins the naming rule for
// inline (non-$ref) nested schemas. A counter would also be deterministic
// within one run but would shift every downstream id when an unrelated
// property is added; deriving from the parent id and property name keeps
// each id stable for as long as that property exists.
func TestCodecTableSyntheticIDsAreDerivedFromPath(t *testing.T) {
	code, _ := NewCodecGenerator().Generate(nestedSpec(), baseConfig())

	assert.Contains(t, code, `"Nested.items":`, "an inline array property needs its own entry, keyed by parent and property name")
	assert.Contains(t, code, `"Nested.tags":`, "an inline record property needs its own entry")

	// The $ref property must reuse the named codec, not mint a synthetic one.
	assert.NotContains(t, code, `"Nested.user":`)
	assert.Contains(t, code, `"codec": "User"`)
}

// TestCodecTableUnionRequiresDiscriminator covers the table shape for both
// union forms: WITH a discriminator, the entry carries the discriminator
// map; WITHOUT one, the entry still emits `members` (for structural
// matching at runtime -- see TestCodecRuntimeUndiscriminatedUnion) but omits
// `discriminator` entirely, and generation emits a warning naming the
// schema so the ambiguity is visible.
func TestCodecTableUnionRequiresDiscriminator(t *testing.T) {
	withDisc := baseSpec()
	withDisc.Schemas["Pet"] = &client.Schema{
		OneOf: []*client.Schema{
			{Ref: "#/components/schemas/Cat"},
			{Ref: "#/components/schemas/Dog"},
		},
		Discriminator: &client.Discriminator{
			PropertyName: "kind",
			Mapping: map[string]string{
				"cat": "#/components/schemas/Cat",
				"dog": "#/components/schemas/Dog",
			},
		},
	}
	withDisc.Schemas["Cat"] = &client.Schema{Type: "object", Properties: map[string]*client.Schema{"meows": {Type: "boolean"}}}
	withDisc.Schemas["Dog"] = &client.Schema{Type: "object", Properties: map[string]*client.Schema{"barks": {Type: "boolean"}}}

	code, warnings := NewCodecGenerator().Generate(withDisc, baseConfig())
	assert.Contains(t, code, `"kind": "union"`)
	assert.Contains(t, code, `"wire": "kind"`)
	assert.Empty(t, warnings, "a discriminated union must not warn")

	// Same union, no discriminator.
	noDisc := baseSpec()
	noDisc.Schemas["Pet"] = &client.Schema{
		OneOf: []*client.Schema{
			{Ref: "#/components/schemas/Cat"},
			{Ref: "#/components/schemas/Dog"},
		},
	}
	noDisc.Schemas["Cat"] = withDisc.Schemas["Cat"]
	noDisc.Schemas["Dog"] = withDisc.Schemas["Dog"]

	code, warnings = NewCodecGenerator().Generate(noDisc, baseConfig())

	// members present, discriminator absent -- exact string match on the
	// emitted entry pins the field ORDER too (Kind then Members, nothing
	// between them), which is only true when Discriminator marshals to
	// nothing (omitempty on a nil pointer).
	assert.Contains(t, code, `"Pet": {"kind": "union", "members": ["Cat", "Dog"]}`)

	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], `"Pet"`, "the warning must name the schema")
	assert.Contains(t, warnings[0], "discriminator")
}

// TestCodecTableHandlesSelfReference is the non-termination guard. A schema
// that references itself (a tree node, a linked list) must not recurse
// forever while the table is built.
func TestCodecTableHandlesSelfReference(t *testing.T) {
	spec := baseSpec()
	spec.Schemas["Node"] = &client.Schema{
		Type: "object",
		Properties: map[string]*client.Schema{
			"value":    {Type: "string"},
			"children": {Type: "array", Items: &client.Schema{Ref: "#/components/schemas/Node"}},
		},
	}

	done := make(chan string, 1)
	go func() {
		code, _ := NewCodecGenerator().Generate(spec, baseConfig())
		done <- code
	}()

	select {
	case code := <-done:
		assert.Contains(t, code, `"Node":`)
		// The cycle resolves through the synthetic array entry's `items`,
		// which points back at the named codec rather than expanding it
		// again — that pointer is exactly what makes the walk terminate.
		assert.Contains(t, code, `"Node.children"`)
		assert.Contains(t, code, `"items": "Node"`, "the self-reference must resolve to the named codec, not re-expand it")
	case <-time.After(10 * time.Second):
		t.Fatal("codec table generation did not terminate on a self-referential schema")
	}
}

func TestCodecTableIsDeterministic(t *testing.T) {
	spec := nestedSpec()
	first, firstWarnings := NewCodecGenerator().Generate(spec, baseConfig())

	for i := range 12 {
		got, gotWarnings := NewCodecGenerator().Generate(spec, baseConfig())
		if got != first {
			t.Fatalf("run %d code differs", i)
		}

		if !slices.Equal(gotWarnings, firstWarnings) {
			t.Fatalf("run %d warnings differ: %v vs %v", i, gotWarnings, firstWarnings)
		}
	}
}

// TestCodecTableWarningOrderIsDeterministic pins determinism for the case
// TestCodecTableIsDeterministic's spec doesn't exercise: MULTIPLE
// undiscriminated unions in one spec. Warnings are collected across a
// recursive, sorted-key walk, not a single flat map, so this is the test
// that would catch an accidental map-iteration dependency in how they're
// gathered or ordered.
func TestCodecTableWarningOrderIsDeterministic(t *testing.T) {
	spec := baseSpec()
	spec.Schemas["Zebra"] = &client.Schema{
		OneOf: []*client.Schema{{Ref: "#/components/schemas/User"}},
	}
	spec.Schemas["Alpha"] = &client.Schema{
		OneOf: []*client.Schema{{Ref: "#/components/schemas/User"}},
	}
	spec.Schemas["Mid"] = &client.Schema{
		OneOf: []*client.Schema{{Ref: "#/components/schemas/User"}},
	}

	_, first := NewCodecGenerator().Generate(spec, baseConfig())
	require.Len(t, first, 3, "one warning per undiscriminated union")

	for i := range 12 {
		_, got := NewCodecGenerator().Generate(spec, baseConfig())
		if !slices.Equal(got, first) {
			t.Fatalf("run %d warning order differs: %v vs %v", i, got, first)
		}
	}
}

func TestCodecsEmittedAndExported(t *testing.T) {
	out, err := NewGenerator().Generate(context.Background(), baseSpec(), baseConfig())
	require.NoError(t, err)

	assert.Contains(t, out.Files, "src/codecs.ts")
	assert.Contains(t, out.Files["src/index.ts"], "export * from './codecs';")
}

// TestCodecWarningsSurfaceOnGeneratedClient pins the chosen warnings
// mechanism end to end: CodecGenerator.Generate returns warnings on its
// existing return path (a second value, alongside the emitted code) rather
// than a logger or a package-level global, and the top-level
// Generator.Generate forwards them onto GeneratedClient.Warnings so a
// caller (e.g. the CLI) can act on them without reaching into the
// typescript package's internals.
func TestCodecWarningsSurfaceOnGeneratedClient(t *testing.T) {
	spec := baseSpec()
	spec.Schemas["Pet"] = &client.Schema{
		OneOf: []*client.Schema{
			{Ref: "#/components/schemas/User"},
		},
	}

	out, err := NewGenerator().Generate(context.Background(), spec, baseConfig())
	require.NoError(t, err)

	require.Len(t, out.Warnings, 1)
	assert.Contains(t, out.Warnings[0], `"Pet"`)
}

// TestCodecRuntimeRulesArePresent pins the three non-negotiable runtime
// behaviours in the emitted walk. The behavioural proof is the execution
// test; this catches an accidental removal without a Node round trip.
func TestCodecRuntimeRulesArePresent(t *testing.T) {
	code, _ := NewCodecGenerator().Generate(baseSpec(), baseConfig())

	assert.Contains(t, code, "out[key] = val;", "unknown keys must pass through verbatim")
	assert.Contains(t, code, "Keys are data here", "a record must rename values but never keys")
	assert.True(t, strings.Contains(code, "if (typeof tag !== 'string')"),
		"a union whose discriminator value is missing or non-string must fall back to passthrough")
	assert.Contains(t, code, "hasOwnProperty",
		"an undiscriminated union must test each member's required wire fields structurally")
}

// TestCodecRuntimeBehaviour is the execution proof for the three
// non-negotiable rules. String assertions can show the branches exist; only
// running them shows they do the right thing. The table is asserted through
// encode/decode round trips rather than by inspecting CODECS, so the test
// stays valid once a later phase makes `ts` differ from the wire name.
func TestCodecRuntimeBehaviour(t *testing.T) {
	spec := nestedSpec()
	spec.Schemas["Pet"] = &client.Schema{
		OneOf: []*client.Schema{
			{Ref: "#/components/schemas/Cat"},
			{Ref: "#/components/schemas/Dog"},
		},
		Discriminator: &client.Discriminator{
			PropertyName: "kind",
			Mapping: map[string]string{
				"cat": "#/components/schemas/Cat",
				"dog": "#/components/schemas/Dog",
			},
		},
	}
	spec.Schemas["Cat"] = &client.Schema{Type: "object", Properties: map[string]*client.Schema{
		"kind": {Type: "string"}, "meows": {Type: "boolean"},
	}}
	spec.Schemas["Dog"] = &client.Schema{Type: "object", Properties: map[string]*client.Schema{
		"kind": {Type: "string"}, "barks": {Type: "boolean"},
	}}
	spec.Schemas["Untagged"] = &client.Schema{
		OneOf: []*client.Schema{
			{Ref: "#/components/schemas/Cat"},
			{Ref: "#/components/schemas/Dog"},
		},
	}

	dir := t.TempDir()
	code, _ := NewCodecGenerator().Generate(spec, baseConfig())
	writeTree(t, dir, map[string]string{
		"src/codecs.ts": code,
	})

	driver := `
import { decode, encode } from './codecs';

const results: Record<string, any> = {};

// Rule 1: unknown keys pass through verbatim — a server that adds a field
// must not have it silently dropped by an older client.
results.unknownKeys = decode({ id: 'u1', surprise: { nested: 1 } }, 'User');

// Rule 2: a record renames its VALUES but never its KEYS. The keys of
// Nested.tags are user-chosen ids, not schema-defined field names.
results.recordKeys = decode({ tags: { 'Weird-Key': 'v', 'another key': 'w' } }, 'Nested');

// Rule 3: a union WITHOUT a discriminator falls back to passthrough rather
// than guessing a member by structural shape.
results.untagged = decode({ kind: 'cat', meows: true, extra: 1 }, 'Untagged');

// A union WITH a discriminator resolves to the tagged member.
results.tagged = decode({ kind: 'cat', meows: true, extra: 1 }, 'Pet');

// A discriminator value with no mapping entry passes through untouched.
results.unknownTag = decode({ kind: 'fish', swims: true }, 'Pet');

// Nested walks: array of $refs, and a $ref property.
results.nested = decode({ user: { id: 'u' }, items: [{ id: 'a' }, { id: 'b' }] }, 'Nested');

// encode is the inverse of decode. With ts === wire today this is identity,
// but the round trip is what a later phase's rename has to preserve.
const original = { user: { id: 'u' }, items: [{ id: 'a' }], tags: { k: 'v' } };
results.roundTrip = encode(decode(original, 'Nested'), 'Nested');

// A null or undefined value is returned as-is, never walked.
results.nullish = { a: decode(null, 'User'), b: decode(undefined, 'User') };

// An unknown codec id leaves the value alone rather than throwing.
results.unknownCodec = decode({ a: 1 }, 'NoSuchCodec');

console.log(JSON.stringify(results));
`
	writeTree(t, dir, map[string]string{"src/__driver_codecs.ts": driver})

	stdout := runNodeDriver(t, dir, "src/__driver_codecs.ts")

	var got struct {
		UnknownKeys  map[string]any `json:"unknownKeys"`
		RecordKeys   map[string]any `json:"recordKeys"`
		Untagged     map[string]any `json:"untagged"`
		Tagged       map[string]any `json:"tagged"`
		UnknownTag   map[string]any `json:"unknownTag"`
		Nested       map[string]any `json:"nested"`
		RoundTrip    map[string]any `json:"roundTrip"`
		Nullish      map[string]any `json:"nullish"`
		UnknownCodec map[string]any `json:"unknownCodec"`
	}
	decodeLastLine(t, stdout, &got)

	assert.Equal(t, map[string]any{"nested": float64(1)}, got.UnknownKeys["surprise"],
		"an unknown key must survive decode with its name and value intact; driver stdout:\n%s", stdout)

	tags, _ := got.RecordKeys["tags"].(map[string]any)
	assert.Contains(t, tags, "Weird-Key", "record keys are data and must never be renamed")
	assert.Contains(t, tags, "another key", "record keys are data and must never be renamed")

	assert.Equal(t, map[string]any{"kind": "cat", "meows": true, "extra": float64(1)}, got.Untagged,
		"a union with no discriminator must pass through untouched rather than guess a member")

	assert.Equal(t, "cat", got.Tagged["kind"])
	assert.Equal(t, float64(1), got.Tagged["extra"], "unknown keys survive inside a resolved union member too")

	assert.Equal(t, map[string]any{"kind": "fish", "swims": true}, got.UnknownTag,
		"a discriminator value absent from the mapping must pass through, not throw")

	items, _ := got.Nested["items"].([]any)
	assert.Len(t, items, 2, "an array of $refs must be walked element by element")

	assert.Equal(t, map[string]any{
		"user":  map[string]any{"id": "u"},
		"items": []any{map[string]any{"id": "a"}},
		"tags":  map[string]any{"k": "v"},
	}, got.RoundTrip, "encode(decode(x)) must round-trip; driver stdout:\n%s", stdout)

	assert.Nil(t, got.Nullish["a"], "null must pass through untouched")
	assert.Nil(t, got.Nullish["b"], "undefined must pass through untouched")

	assert.Equal(t, map[string]any{"a": float64(1)}, got.UnknownCodec,
		"an unknown codec id must leave the value alone rather than throw")
}

// structuralUnionSpec builds Shape and MaybeLabeled, which
// TestCodecRuntimeUndiscriminatedUnionStructuralMatch exercises:
//
//   - Shape: OneOf[Square, Circle], no discriminator, disjoint required
//     fields ("side" vs "radius") -- covers matching B-not-A, matching
//     neither, and matching both (first declared wins).
//   - MaybeLabeled: OneOf[Labeled ($ref), inline{required: special}] -- Gap
//     1, an inline (non-$ref) union member.
//
// Every schema that needs to prove "its fields were actually walked" carries
// an array property with $ref items: the array codec case always rebuilds
// the array via `.map`, so a fresh reference on decode is only possible if
// the walk actually reached that subtree. An untouched (still-identical)
// reference means the value passed through without being recognised at all
// -- exactly the failure mode both "no match" and Gap 1 produce.
func structuralUnionSpec() *client.APISpec {
	spec := baseSpec()

	spec.Schemas["Square"] = &client.Schema{
		Type:     "object",
		Required: []string{"side"},
		Properties: map[string]*client.Schema{
			"side": {Type: "number"},
			"tags": {Type: "array", Items: &client.Schema{Ref: "#/components/schemas/User"}},
		},
	}
	spec.Schemas["Circle"] = &client.Schema{
		Type:       "object",
		Required:   []string{"radius"},
		Properties: map[string]*client.Schema{"radius": {Type: "number"}},
	}
	spec.Schemas["Shape"] = &client.Schema{
		OneOf: []*client.Schema{
			{Ref: "#/components/schemas/Square"},
			{Ref: "#/components/schemas/Circle"},
		},
	}

	spec.Schemas["Labeled"] = &client.Schema{
		Type:       "object",
		Required:   []string{"label"},
		Properties: map[string]*client.Schema{"label": {Type: "string"}},
	}
	spec.Schemas["MaybeLabeled"] = &client.Schema{
		OneOf: []*client.Schema{
			{Ref: "#/components/schemas/Labeled"},
			{
				Type:     "object",
				Required: []string{"special"},
				Properties: map[string]*client.Schema{
					"special": {Type: "boolean"},
					"list":    {Type: "array", Items: &client.Schema{Ref: "#/components/schemas/User"}},
				},
			},
		},
	}

	return spec
}

// TestCodecRuntimeUndiscriminatedUnionStructuralMatch is the execution proof
// for structural matching (including Gap 1, the inline union member) --
// string assertions on the emitted table can show `members` is populated,
// but only running the walk shows it picks the right member, falls back
// correctly, and reaches the subtree Gap 1 previously skipped.
//
// The positive control -- a DISCRIMINATED union still resolving by tag, not
// falling into structural matching -- is covered by the existing, unchanged
// TestCodecRuntimeBehaviour ("tagged"/"Pet"); it is not repeated here.
func TestCodecRuntimeUndiscriminatedUnionStructuralMatch(t *testing.T) {
	spec := structuralUnionSpec()

	dir := t.TempDir()
	code, warnings := NewCodecGenerator().Generate(spec, baseConfig())
	writeTree(t, dir, map[string]string{"src/codecs.ts": code})

	// Two undiscriminated unions in this spec: Shape and MaybeLabeled.
	require.Len(t, warnings, 2)

	driver := `
import { decode } from './codecs';

const results: Record<string, any> = {};

// Matches Circle's required field ("radius") but not Square's ("side").
const onlyCircle = { radius: 3 };
const decodedCircle = decode(onlyCircle, 'Shape');
results.onlyCircleIsCopy = decodedCircle !== onlyCircle;
results.onlyCircleValue = decodedCircle;

// Matches neither Square nor Circle: falls back to passthrough verbatim,
// proven by reference identity -- a genuinely matched member always
// constructs a new object via the 'object' codec case.
const neither = { weight: 9 };
const decodedNeither = decode(neither, 'Shape');
results.neitherIsIdentity = decodedNeither === neither;
results.neitherValue = decodedNeither;

// Matches BOTH members. Square is declared first and must win. Proven by
// 'tags' (declared only on Square, with its own array codec) coming back as
// a NEW array -- if Circle had won instead, 'tags' would be an unrecognised
// key on Circle and would pass through by the same reference.
const both = { side: 1, radius: 2, tags: [{ id: 'x' }] };
const decodedBoth = decode(both, 'Shape');
results.bothPicksFirstDeclared = decodedBoth.tags !== both.tags;

// Gap 1: the inline (non-$ref) union member. This payload satisfies ONLY
// the inline member's required field ("special"), not the $ref member's
// ("label"). Before the fix the inline member had no codec id at all, so
// it could never be selected regardless of the payload -- this would fall
// through to passthrough (identity) instead of being walked.
const inlinePayload = { special: true, list: [{ id: 'y' }] };
const decodedInline = decode(inlinePayload, 'MaybeLabeled');
results.inlineMemberWalked = decodedInline.list !== inlinePayload.list;
results.inlineMemberIsCopy = decodedInline !== inlinePayload;

console.log(JSON.stringify(results));
`
	writeTree(t, dir, map[string]string{"src/__driver_structural_union.ts": driver})

	stdout := runNodeDriver(t, dir, "src/__driver_structural_union.ts")

	var got struct {
		OnlyCircleIsCopy       bool           `json:"onlyCircleIsCopy"`
		OnlyCircleValue        map[string]any `json:"onlyCircleValue"`
		NeitherIsIdentity      bool           `json:"neitherIsIdentity"`
		NeitherValue           map[string]any `json:"neitherValue"`
		BothPicksFirstDeclared bool           `json:"bothPicksFirstDeclared"`
		InlineMemberWalked     bool           `json:"inlineMemberWalked"`
		InlineMemberIsCopy     bool           `json:"inlineMemberIsCopy"`
	}
	decodeLastLine(t, stdout, &got)

	assert.True(t, got.OnlyCircleIsCopy,
		"a payload matching only Circle's required fields must be walked (decoded as a member), not passed through; driver stdout:\n%s", stdout)
	assert.Equal(t, map[string]any{"radius": float64(3)}, got.OnlyCircleValue)

	assert.True(t, got.NeitherIsIdentity,
		"a payload matching no member must pass through verbatim, never a best-effort guess; driver stdout:\n%s", stdout)
	assert.Equal(t, map[string]any{"weight": float64(9)}, got.NeitherValue)

	assert.True(t, got.BothPicksFirstDeclared,
		"when a payload matches every member, the first declared in the union must win, deterministically; driver stdout:\n%s", stdout)

	assert.True(t, got.InlineMemberWalked,
		"Gap 1: an inline (non-$ref) union member must get a codec id and actually be walked; driver stdout:\n%s", stdout)
	assert.True(t, got.InlineMemberIsCopy)
}

// TestCodecRuntimeAllOfPropertyIsWalked is the execution proof for Gap 2: a
// property whose schema is a pure allOf (no schema.Properties of its own)
// must get a codec id and actually be walked, not silently skipped.
//
// Container.composed carries an array property ($ref items) so a successful
// walk is provable by reference identity: the array codec case always
// rebuilds the array via `.map`, so a fresh reference after decode is only
// possible if the walk actually reached that subtree.
func TestCodecRuntimeAllOfPropertyIsWalked(t *testing.T) {
	spec := baseSpec()
	spec.Schemas["Base"] = &client.Schema{
		Type:       "object",
		Properties: map[string]*client.Schema{"baseField": {Type: "string"}},
	}
	spec.Schemas["Container"] = &client.Schema{
		Type: "object",
		Properties: map[string]*client.Schema{
			"composed": {
				AllOf: []*client.Schema{
					{Ref: "#/components/schemas/Base"},
					{
						Type: "object",
						Properties: map[string]*client.Schema{
							"list": {Type: "array", Items: &client.Schema{Ref: "#/components/schemas/User"}},
						},
					},
				},
			},
		},
	}

	dir := t.TempDir()
	code, warnings := NewCodecGenerator().Generate(spec, baseConfig())
	writeTree(t, dir, map[string]string{"src/codecs.ts": code})

	assert.Empty(t, warnings, "a pure allOf composition is not ambiguous the way an undiscriminated union is; it must not warn")

	driver := `
import { decode } from './codecs';

const results: Record<string, any> = {};

// 'composed' has no schema.Properties of its own -- only AllOf -- so before
// the fix codecIDFor returned "" for it and it was never walked at all.
const containerPayload = { composed: { baseField: 'x', list: [{ id: 'z' }] } };
const decodedContainer = decode(containerPayload, 'Container');
results.allOfPropertyWalked = decodedContainer.composed.list !== containerPayload.composed.list;
results.baseFieldSurvived = decodedContainer.composed.baseField;

console.log(JSON.stringify(results));
`
	writeTree(t, dir, map[string]string{"src/__driver_allof.ts": driver})

	stdout := runNodeDriver(t, dir, "src/__driver_allof.ts")

	var got struct {
		AllOfPropertyWalked bool   `json:"allOfPropertyWalked"`
		BaseFieldSurvived   string `json:"baseFieldSurvived"`
	}
	decodeLastLine(t, stdout, &got)

	assert.True(t, got.AllOfPropertyWalked,
		"Gap 2: a property whose schema is a pure allOf must get a codec id and actually be walked; driver stdout:\n%s", stdout)
	assert.Equal(t, "x", got.BaseFieldSurvived,
		"a field contributed by the $ref member of the allOf must still be present after decode")
}
