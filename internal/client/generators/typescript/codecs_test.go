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
	withDisc.Schemas["Cat"] = &client.Schema{Type: "object", Required: []string{"meows"}, Properties: map[string]*client.Schema{"meows": {Type: "boolean"}}}
	withDisc.Schemas["Dog"] = &client.Schema{Type: "object", Required: []string{"barks"}, Properties: map[string]*client.Schema{"barks": {Type: "boolean"}}}

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
// than guessing a member by structural shape. Cat/Dog declare no required
// fields, so both are evidence-free and neither can ever be structurally
// matched -- this must be TRUE passthrough (the identical reference), not a
// decode that merely happens to look the same because ts === wire today.
const untaggedInput = { kind: 'cat', meows: true, extra: 1 };
const untaggedResult = decode(untaggedInput, 'Untagged');
results.untagged = untaggedResult;
results.untaggedIsIdentity = untaggedResult === untaggedInput;

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
		UnknownKeys        map[string]any `json:"unknownKeys"`
		RecordKeys         map[string]any `json:"recordKeys"`
		Untagged           map[string]any `json:"untagged"`
		UntaggedIsIdentity bool           `json:"untaggedIsIdentity"`
		Tagged             map[string]any `json:"tagged"`
		UnknownTag         map[string]any `json:"unknownTag"`
		Nested             map[string]any `json:"nested"`
		RoundTrip          map[string]any `json:"roundTrip"`
		Nullish            map[string]any `json:"nullish"`
		UnknownCodec       map[string]any `json:"unknownCodec"`
	}
	decodeLastLine(t, stdout, &got)

	assert.Equal(t, map[string]any{"nested": float64(1)}, got.UnknownKeys["surprise"],
		"an unknown key must survive decode with its name and value intact; driver stdout:\n%s", stdout)

	tags, _ := got.RecordKeys["tags"].(map[string]any)
	assert.Contains(t, tags, "Weird-Key", "record keys are data and must never be renamed")
	assert.Contains(t, tags, "another key", "record keys are data and must never be renamed")

	assert.Equal(t, map[string]any{"kind": "cat", "meows": true, "extra": float64(1)}, got.Untagged,
		"a union with no discriminator must pass through untouched rather than guess a member")
	assert.True(t, got.UntaggedIsIdentity,
		"a union whose members are all evidence-free (no required fields) must be TRUE passthrough (identical reference), not a decode that coincidentally looks the same; driver stdout:\n%s", stdout)

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

// ========== Fix round 1: allOf empty-composition, conflicts, evidence-free union members ==========

// allOfChainSpec builds a three-level $ref allOf inheritance chain
// (Outer -> Mid -> Leaf, an ordinary OpenAPI pattern per the review), the
// same shape reached through an INLINE (non-$ref) intermediate composition
// (OuterInline), and a member whose $ref cannot be resolved at all
// (Dangling). Mid and the inline intermediate in OuterInline each have NO
// Properties of their own -- only AllOf -- so Outer/OuterInline cannot get
// their fields without recursively flattening through them down to Leaf.
func allOfChainSpec() *client.APISpec {
	spec := baseSpec()

	spec.Schemas["Leaf"] = &client.Schema{
		Type: "object",
		Properties: map[string]*client.Schema{
			"name": {Type: "string"},
			"tags": {Type: "array", Items: &client.Schema{Ref: "#/components/schemas/User"}},
		},
	}
	spec.Schemas["Mid"] = &client.Schema{AllOf: []*client.Schema{{Ref: "#/components/schemas/Leaf"}}}
	spec.Schemas["Outer"] = &client.Schema{AllOf: []*client.Schema{{Ref: "#/components/schemas/Mid"}}}
	spec.Schemas["OuterInline"] = &client.Schema{
		AllOf: []*client.Schema{
			{AllOf: []*client.Schema{{Ref: "#/components/schemas/Leaf"}}},
		},
	}
	spec.Schemas["Dangling"] = &client.Schema{
		AllOf: []*client.Schema{{Ref: "#/components/schemas/Ghost"}},
	}

	return spec
}

// TestCodecTableAllOfEmptyCompositionDegradesToPassthrough is the CRITICAL 1
// regression guard: allOfEntry must never emit {"kind":"object"} with no
// `fields` key. Before the fix, Outer, OuterInline, and Dangling all
// produced exactly that -- tsc rejects it (the emitted Codec type declares
// `fields` required for kind:'object') and decode() throws
// (Object.entries(undefined)).
func TestCodecTableAllOfEmptyCompositionDegradesToPassthrough(t *testing.T) {
	spec := allOfChainSpec()
	code, _ := NewCodecGenerator().Generate(spec, baseConfig())

	// Mid resolves one $ref hop (Leaf) and already worked before this fix --
	// pinned here as the control case the broken ones are compared against.
	assert.Contains(t, code, `"Mid": {"kind": "object", "fields": {"name": {"ts": "name"}, "tags"`)

	// Outer is a SECOND level ($ref Mid, which has no Properties of its
	// own): before the fix this was `{"kind": "object"}` with no fields key
	// at all. It must now flatten all the way down to Leaf's fields.
	assert.NotContains(t, code, `"Outer": {"kind": "object"}`, "an allOf entry must never have an empty fields map")
	assert.Contains(t, code, `"Outer": {"kind": "object", "fields": {"name": {"ts": "name"}, "tags"`)

	// OuterInline reaches the identical empty-composition shape through an
	// INLINE (non-$ref) intermediate rather than a $ref hop.
	assert.NotContains(t, code, `"OuterInline": {"kind": "object"}`)
	assert.Contains(t, code, `"OuterInline": {"kind": "object", "fields": {"name": {"ts": "name"}, "tags"`)

	// Dangling's only member resolves to nothing at all (spec.Schemas["Ghost"]
	// doesn't exist) -- this MUST degrade to passthrough, never a lying
	// empty object.
	assert.Contains(t, code, `"Dangling": {"kind": "passthrough"}`)
	assert.NotContains(t, code, `"Dangling": {"kind": "object"}`)
}

// TestCodecRuntimeAllOfChainDoesNotThrowAndIsWalked is the execution proof
// for CRITICAL 1 (no throw) and MINOR 5 (nested allOf actually flattens,
// not just "known scope boundary"). String assertions on the table can show
// the right JSON shape; only running it proves decode() doesn't throw on a
// dangling member and that the resolved chains are actually walked, not
// just correctly *shaped*.
func TestCodecRuntimeAllOfChainDoesNotThrowAndIsWalked(t *testing.T) {
	spec := allOfChainSpec()

	dir := t.TempDir()
	code, _ := NewCodecGenerator().Generate(spec, baseConfig())
	writeTree(t, dir, map[string]string{"src/codecs.ts": code})

	driver := `
import { decode } from './codecs';

const results: Record<string, any> = {};

// Outer flattens through Mid down to Leaf -- 'tags' must actually be
// walked (a new array reference), proving the chain resolves all the way
// down rather than stopping at Mid, which has no fields of its own.
const outerPayload = { name: 'x', tags: [{ id: 'a' }] };
const decodedOuter = decode(outerPayload, 'Outer');
results.outerWalked = decodedOuter.tags !== outerPayload.tags;
results.outerName = decodedOuter.name;

const outerInlinePayload = { name: 'y', tags: [{ id: 'b' }] };
const decodedOuterInline = decode(outerInlinePayload, 'OuterInline');
results.outerInlineWalked = decodedOuterInline.tags !== outerInlinePayload.tags;

// A dangling $ref member must not throw -- it must pass through verbatim,
// exactly like any other passthrough entry.
let danglingThrew = false;
let decodedDangling;
try {
  decodedDangling = decode({ anything: 1 }, 'Dangling');
} catch (e) {
  danglingThrew = true;
}
results.danglingThrew = danglingThrew;
results.decodedDangling = decodedDangling;

console.log(JSON.stringify(results));
`
	writeTree(t, dir, map[string]string{"src/__driver_allof_chain.ts": driver})

	stdout := runNodeDriver(t, dir, "src/__driver_allof_chain.ts")

	var got struct {
		OuterWalked       bool           `json:"outerWalked"`
		OuterName         string         `json:"outerName"`
		OuterInlineWalked bool           `json:"outerInlineWalked"`
		DanglingThrew     bool           `json:"danglingThrew"`
		DecodedDangling   map[string]any `json:"decodedDangling"`
	}
	decodeLastLine(t, stdout, &got)

	assert.True(t, got.OuterWalked,
		"a three-level allOf $ref chain must flatten all the way down to the leaf's fields; driver stdout:\n%s", stdout)
	assert.Equal(t, "x", got.OuterName)

	assert.True(t, got.OuterInlineWalked,
		"an inline (non-$ref) nested allOf composition must flatten the same way; driver stdout:\n%s", stdout)

	assert.False(t, got.DanglingThrew,
		"a dangling $ref allOf member must degrade to passthrough, never throw; driver stdout:\n%s", stdout)
	assert.Equal(t, map[string]any{"anything": float64(1)}, got.DecodedDangling)
}

// allOfConflictSpec builds Dup = allOf[MemberA, MemberB], where both members
// require a "payload" field but point it at DIFFERENT nested schemas
// (PayloadA vs PayloadB). PayloadA/PayloadB each carry their own
// array-of-$ref field (listA/listB) so that which one actually drove the
// decode is provable by reference identity, not just by inspecting the
// table.
func allOfConflictSpec() *client.APISpec {
	spec := baseSpec()

	spec.Schemas["PayloadA"] = &client.Schema{
		Type: "object",
		Properties: map[string]*client.Schema{
			"x":     {Type: "string"},
			"listA": {Type: "array", Items: &client.Schema{Ref: "#/components/schemas/User"}},
		},
	}
	spec.Schemas["PayloadB"] = &client.Schema{
		Type: "object",
		Properties: map[string]*client.Schema{
			"y":     {Type: "string"},
			"listB": {Type: "array", Items: &client.Schema{Ref: "#/components/schemas/User"}},
		},
	}
	spec.Schemas["MemberA"] = &client.Schema{
		Type: "object", Required: []string{"payload"},
		Properties: map[string]*client.Schema{"payload": {Ref: "#/components/schemas/PayloadA"}},
	}
	spec.Schemas["MemberB"] = &client.Schema{
		Type: "object", Required: []string{"payload"},
		Properties: map[string]*client.Schema{"payload": {Ref: "#/components/schemas/PayloadB"}},
	}
	spec.Schemas["Dup"] = &client.Schema{
		AllOf: []*client.Schema{
			{Ref: "#/components/schemas/MemberA"},
			{Ref: "#/components/schemas/MemberB"},
		},
	}

	return spec
}

// TestCodecTableAllOfConflictingMembersWarnAndLastWins is CRITICAL 2 /
// IMPORTANT 4's regression guard: two allOf members declaring the same wire
// field with different nested shapes must (a) warn, naming the schema and
// field, and (b) resolve deterministically to the LAST declared member's
// shape, not silently drop one with no record of the conflict.
func TestCodecTableAllOfConflictingMembersWarnAndLastWins(t *testing.T) {
	spec := allOfConflictSpec()
	code, warnings := NewCodecGenerator().Generate(spec, baseConfig())

	// Last member (MemberB / PayloadB) wins the field entry.
	assert.Contains(t, code, `"Dup": {"kind": "object", "fields": {"payload": {"ts": "payload", "codec": "PayloadB"}}, "required": ["payload"]}`)

	var conflictWarning string

	for _, w := range warnings {
		if strings.Contains(w, `"Dup"`) {
			conflictWarning = w
		}
	}

	require.NotEmpty(t, conflictWarning, "a cross-member field conflict must be named in a warning, not silently dropped")
	assert.Contains(t, conflictWarning, "payload")
	assert.Contains(t, conflictWarning, "different shapes")
}

// TestCodecTableAllOfConflictIsDeterministic pins that the last-wins
// resolution (and the warning it produces) is stable across repeated runs,
// not merely stable-by-luck once.
func TestCodecTableAllOfConflictIsDeterministic(t *testing.T) {
	spec := allOfConflictSpec()
	firstCode, firstWarnings := NewCodecGenerator().Generate(spec, baseConfig())

	for i := range 20 {
		gotCode, gotWarnings := NewCodecGenerator().Generate(spec, baseConfig())
		if gotCode != firstCode {
			t.Fatalf("run %d code differs", i)
		}

		if !slices.Equal(gotWarnings, firstWarnings) {
			t.Fatalf("run %d warnings differ: %v vs %v", i, gotWarnings, firstWarnings)
		}
	}
}

// TestCodecRuntimeAllOfConflictLastMemberDrivesTheWalk is the execution
// proof that the LAST declared member's codec is what actually runs, not
// just what the table claims: PayloadA's "listA" and PayloadB's "listB" are
// each declared with their own array codec, so if PayloadB (last) truly
// drives the walk, only "listB" gets a new array reference -- "listA"
// becomes an unrecognised key relative to PayloadB and passes through by
// the same reference, exactly as if PayloadA's declaration for "payload"
// had never existed.
func TestCodecRuntimeAllOfConflictLastMemberDrivesTheWalk(t *testing.T) {
	spec := allOfConflictSpec()

	dir := t.TempDir()
	code, _ := NewCodecGenerator().Generate(spec, baseConfig())
	writeTree(t, dir, map[string]string{"src/codecs.ts": code})

	driver := `
import { decode } from './codecs';

const results: Record<string, any> = {};

const payload = { payload: { x: 'a', y: 'b', listA: [{ id: '1' }], listB: [{ id: '2' }] } };
const decoded = decode(payload, 'Dup');

results.listBWalked = decoded.payload.listB !== payload.payload.listB;
results.listAUntouched = decoded.payload.listA === payload.payload.listA;
results.value = decoded;

console.log(JSON.stringify(results));
`
	writeTree(t, dir, map[string]string{"src/__driver_allof_conflict.ts": driver})

	stdout := runNodeDriver(t, dir, "src/__driver_allof_conflict.ts")

	var got struct {
		ListBWalked    bool           `json:"listBWalked"`
		ListAUntouched bool           `json:"listAUntouched"`
		Value          map[string]any `json:"value"`
	}
	decodeLastLine(t, stdout, &got)

	assert.True(t, got.ListBWalked,
		"the last declared member (PayloadB) must actually drive the walk, proven by 'listB' getting a fresh array reference; driver stdout:\n%s", stdout)
	assert.True(t, got.ListAUntouched,
		"PayloadA's codec must NOT run once PayloadB has won the field -- 'listA' must stay the original reference, an unrecognised key relative to PayloadB; driver stdout:\n%s", stdout)
}

// evidenceFreeUnionSpec builds three scenarios for IMPORTANT 3:
//
//   - UndeterminedPet: oneOf[CatNR, DogNR], no discriminator, NEITHER member
//     declares any required field -- mirrors the review's own measured
//     regression exactly (catIdentity/dogIdentity/alienIdentity all false).
//   - Mixed: oneOf[ArrLeaf, RealMember] -- ArrLeaf is not an 'object' kind
//     at all (it's an array), declared FIRST; RealMember is a real object
//     with a required field, declared second.
//   - Alpha: oneOf[Zebra], where Zebra sorts AFTER Alpha alphabetically --
//     the eager-registration regression guard: the top-level
//     sortedKeys(spec.Schemas) walk reaches "Alpha" before "Zebra", so
//     unionEntry must force Zebra's entry to exist before checking whether
//     it offers evidence, not rely on Zebra having been built already.
func evidenceFreeUnionSpec() *client.APISpec {
	spec := baseSpec()

	spec.Schemas["CatNR"] = &client.Schema{Type: "object", Properties: map[string]*client.Schema{
		"kind": {Type: "string"}, "meows": {Type: "boolean"},
	}}
	spec.Schemas["DogNR"] = &client.Schema{Type: "object", Properties: map[string]*client.Schema{
		"kind": {Type: "string"}, "barks": {Type: "boolean"},
	}}
	spec.Schemas["UndeterminedPet"] = &client.Schema{
		OneOf: []*client.Schema{
			{Ref: "#/components/schemas/CatNR"},
			{Ref: "#/components/schemas/DogNR"},
		},
	}

	spec.Schemas["ArrLeaf"] = &client.Schema{Type: "array", Items: &client.Schema{Ref: "#/components/schemas/User"}}
	spec.Schemas["RealMember"] = &client.Schema{
		Type: "object", Required: []string{"tag"},
		Properties: map[string]*client.Schema{
			"tag":  {Type: "string"},
			"list": {Type: "array", Items: &client.Schema{Ref: "#/components/schemas/User"}},
		},
	}
	spec.Schemas["Mixed"] = &client.Schema{
		OneOf: []*client.Schema{
			{Ref: "#/components/schemas/ArrLeaf"},
			{Ref: "#/components/schemas/RealMember"},
		},
	}

	spec.Schemas["Zebra"] = &client.Schema{Type: "object", Properties: map[string]*client.Schema{
		"z": {Type: "string"},
	}}
	spec.Schemas["Alpha"] = &client.Schema{
		OneOf: []*client.Schema{{Ref: "#/components/schemas/Zebra"}},
	}

	return spec
}

// TestCodecTableWarnsOnEvidenceFreeUnionMember is IMPORTANT 3's
// generation-time half: a member that can never be structurally matched
// (no required fields, or not an object at all) must be named in a
// warning -- a union whose first (or only) member is evidence-free is
// degenerate, and the person regenerating the client needs to know, not
// discover it via silent non-matching in production.
func TestCodecTableWarnsOnEvidenceFreeUnionMember(t *testing.T) {
	spec := evidenceFreeUnionSpec()
	_, warnings := NewCodecGenerator().Generate(spec, baseConfig())

	var catWarning, dogWarning, arrWarning, alphaWarning string

	for _, w := range warnings {
		switch {
		case strings.Contains(w, `"UndeterminedPet"`) && strings.Contains(w, `"CatNR"`):
			catWarning = w
		case strings.Contains(w, `"UndeterminedPet"`) && strings.Contains(w, `"DogNR"`):
			dogWarning = w
		case strings.Contains(w, `"Mixed"`) && strings.Contains(w, `"ArrLeaf"`):
			arrWarning = w
		case strings.Contains(w, `"Alpha"`) && strings.Contains(w, `"Zebra"`):
			alphaWarning = w
		}
	}

	require.NotEmpty(t, catWarning, "CatNR offers no required fields and must be named as evidence-free")
	require.NotEmpty(t, dogWarning, "DogNR offers no required fields and must be named as evidence-free")
	require.NotEmpty(t, arrWarning, "ArrLeaf is not an object kind at all and must be named as evidence-free")
	require.NotEmpty(t, alphaWarning,
		"Zebra (sorting AFTER Alpha alphabetically) must still be correctly detected as evidence-free -- "+
			"proves unionEntry force-registers a $ref member before checking it, rather than relying on processing order")

	for _, w := range warnings {
		assert.NotContains(t, w, `"RealMember"`, "a member that DOES declare required fields must not be reported as evidence-free")
	}
}

// TestCodecRuntimeUnionSkipsEvidenceFreeMembers is IMPORTANT 3's execution
// proof. Before the fix, codec.required defaulted to [] for an evidence-free
// member, and [].every(...) is vacuously true -- the FIRST declared member
// (even one that can't structurally represent the value at all, like an
// array) would "match" every payload, exactly the "best-effort guess" the
// feature exists to rule out.
func TestCodecRuntimeUnionSkipsEvidenceFreeMembers(t *testing.T) {
	spec := evidenceFreeUnionSpec()

	dir := t.TempDir()
	code, _ := NewCodecGenerator().Generate(spec, baseConfig())
	writeTree(t, dir, map[string]string{"src/codecs.ts": code})

	driver := `
import { decode } from './codecs';

const results: Record<string, any> = {};

// Neither CatNR nor DogNR declares required fields -- BOTH are
// evidence-free, so NO payload can ever be structurally matched to either
// one. This must be TRUE passthrough (identity) for every shape below,
// mirroring exactly what an undiscriminated client actually receives.
const dogShaped = { kind: 'dog', barks: true };
results.dogIdentity = decode(dogShaped, 'UndeterminedPet') === dogShaped;

const catShaped = { kind: 'cat', meows: true };
results.catIdentity = decode(catShaped, 'UndeterminedPet') === catShaped;

const alienShaped = { totally: 'unrelated' };
results.alienIdentity = decode(alienShaped, 'UndeterminedPet') === alienShaped;

// Mixed: ArrLeaf (array kind, evidence-free, declared FIRST) must be
// skipped outright, letting RealMember (object, required: tag, declared
// second) resolve the payload -- proven by 'list' (declared only on
// RealMember, with its own array codec) coming back as a NEW array
// reference.
const mixedPayload = { tag: 'x', list: [{ id: 'a' }] };
const decodedMixed = decode(mixedPayload, 'Mixed');
results.mixedWalkedViaRealMember = decodedMixed.list !== mixedPayload.list;
results.mixedIsCopy = decodedMixed !== mixedPayload;

console.log(JSON.stringify(results));
`
	writeTree(t, dir, map[string]string{"src/__driver_evidence_free.ts": driver})

	stdout := runNodeDriver(t, dir, "src/__driver_evidence_free.ts")

	var got struct {
		DogIdentity              bool `json:"dogIdentity"`
		CatIdentity              bool `json:"catIdentity"`
		AlienIdentity            bool `json:"alienIdentity"`
		MixedWalkedViaRealMember bool `json:"mixedWalkedViaRealMember"`
		MixedIsCopy              bool `json:"mixedIsCopy"`
	}
	decodeLastLine(t, stdout, &got)

	assert.True(t, got.DogIdentity,
		"a dog-shaped payload must pass through verbatim when every union member is evidence-free; driver stdout:\n%s", stdout)
	assert.True(t, got.CatIdentity,
		"a cat-shaped payload must pass through verbatim when every union member is evidence-free; driver stdout:\n%s", stdout)
	assert.True(t, got.AlienIdentity,
		"an unrelated payload must pass through verbatim when every union member is evidence-free; driver stdout:\n%s", stdout)

	assert.True(t, got.MixedWalkedViaRealMember,
		"a non-object (array) member must be skipped outright, letting the real object member resolve the payload; driver stdout:\n%s", stdout)
	assert.True(t, got.MixedIsCopy)
}

// ========== Fix round 2: allOf containing a oneOf/anyOf member ==========

// allOfWithPolymorphicMemberSpec builds AllOfWithOneOf = allOf[$ref OwnBase,
// $ref UnionMember], where UnionMember is itself a union (oneOf) with no
// Properties of its own. flattenAllOfLayers has nothing to merge in from
// UnionMember -- there is no single fixed set of properties a union
// member has, by definition -- but OwnBase DOES contribute real fields, so
// the entry is non-empty and Critical 1's empty-fields safety net never
// engages. Before this fix, UnionMember's contribution silently vanished
// from the table with no warning, even though schemaToTSType renders its
// members into the intersection type -- the same lying-type failure as
// Critical 1, just non-empty and therefore invisible.
func allOfWithPolymorphicMemberSpec() *client.APISpec {
	spec := baseSpec()

	spec.Schemas["Alt1"] = &client.Schema{Type: "object", Properties: map[string]*client.Schema{"a": {Type: "string"}}}
	spec.Schemas["Alt2"] = &client.Schema{Type: "object", Properties: map[string]*client.Schema{"b": {Type: "string"}}}
	spec.Schemas["UnionMember"] = &client.Schema{
		OneOf: []*client.Schema{
			{Ref: "#/components/schemas/Alt1"},
			{Ref: "#/components/schemas/Alt2"},
		},
	}
	spec.Schemas["OwnBase"] = &client.Schema{
		Type: "object", Required: []string{"fromBase"},
		Properties: map[string]*client.Schema{
			"fromBase": {Type: "string"},
			"list":     {Type: "array", Items: &client.Schema{Ref: "#/components/schemas/User"}},
		},
	}
	spec.Schemas["AllOfWithOneOf"] = &client.Schema{
		AllOf: []*client.Schema{
			{Ref: "#/components/schemas/OwnBase"},
			{Ref: "#/components/schemas/UnionMember"},
		},
	}

	return spec
}

// TestCodecTableAllOfWithPolymorphicMemberWarnsWithoutEmptyingFields is the
// table-shape half: OwnBase's fields ARE present (this is NOT Critical 1's
// empty-fields case -- the entry is genuinely non-empty), and a warning
// names the schema and the union member that couldn't be flattened in.
func TestCodecTableAllOfWithPolymorphicMemberWarnsWithoutEmptyingFields(t *testing.T) {
	spec := allOfWithPolymorphicMemberSpec()
	code, warnings := NewCodecGenerator().Generate(spec, baseConfig())

	assert.Contains(t, code, `"AllOfWithOneOf": {"kind": "object", "fields": {"fromBase"`,
		"OwnBase's fields must still be present -- this is a warn-and-degrade case, not an empty-fields one")

	var polyWarning string
	for _, w := range warnings {
		if strings.Contains(w, `"AllOfWithOneOf"`) && strings.Contains(w, `"UnionMember"`) {
			polyWarning = w
		}
	}

	require.NotEmpty(t, polyWarning, "an allOf member that is itself a union (oneOf/anyOf) must be named in a warning")
}

// TestCodecRuntimeAllOfWithPolymorphicMemberDegradesFieldsSafely is the
// execution proof: OwnBase's own field is genuinely walked (proven by
// reference identity on its array field), while the union member's
// contribution -- which the codec table cannot represent -- passes through
// UNRENAMED via the "unknown key" path rather than being corrupted or
// dropped. Safe (no data loss), but a lying type until renaming respects
// the warning; that tradeoff is exactly what the warning documents.
func TestCodecRuntimeAllOfWithPolymorphicMemberDegradesFieldsSafely(t *testing.T) {
	spec := allOfWithPolymorphicMemberSpec()

	dir := t.TempDir()
	code, _ := NewCodecGenerator().Generate(spec, baseConfig())
	writeTree(t, dir, map[string]string{"src/codecs.ts": code})

	driver := `
import { decode } from './codecs';

const results: Record<string, any> = {};

// 'a' is a field ONLY UnionMember's alternatives could ever declare -- it
// cannot be in the merged fields map at all, so it must survive as an
// unrecognised (unrenamed, but NOT dropped) key.
const payload = { fromBase: 'x', list: [{ id: 'z' }], a: 'union-field-value' };
const decoded = decode(payload, 'AllOfWithOneOf');

results.listWalked = decoded.list !== payload.list;
results.decoded = decoded;

console.log(JSON.stringify(results));
`
	writeTree(t, dir, map[string]string{"src/__driver_allof_polymorphic.ts": driver})

	stdout := runNodeDriver(t, dir, "src/__driver_allof_polymorphic.ts")

	var got struct {
		ListWalked bool           `json:"listWalked"`
		Decoded    map[string]any `json:"decoded"`
	}
	decodeLastLine(t, stdout, &got)

	assert.True(t, got.ListWalked,
		"OwnBase's own field must still be genuinely walked; driver stdout:\n%s", stdout)
	assert.Equal(t, map[string]any{"fromBase": "x", "list": []any{map[string]any{"id": "z"}}, "a": "union-field-value"}, got.Decoded,
		"the union member's field ('a') must survive UNRENAMED (safe passthrough via the unknown-key path), not be dropped or corrupted; driver stdout:\n%s", stdout)
}

// ========== Fix round 2: known-limitation pin -- inline allOf conflicts resolve FIRST-wins ==========

// inlineAllOfConflictSpec builds InlineDup = allOf[inline{payload: {p1,
// list1}}, inline{payload: {p2, list2}}] -- two INLINE (non-$ref)
// sub-schemas conflicting at the SAME field name ("payload"). Unlike the
// $ref-vs-$ref case (TestCodecRuntimeAllOfConflictLastMemberDrivesTheWalk,
// which correctly resolves last-declared-wins and warns), codecIDFor
// synthesizes an inline property's id purely from "<id>.<prop>" --
// identical regardless of which layer produced it -- so t.add's
// already-registered guard makes the FIRST layer win, silently, with no
// conflict warning (allOfEntry's doc comment documents this precisely; see
// its "Known residual limitation" paragraph). This test pins the documented
// behavior so it cannot drift without the documentation being updated
// alongside it.
func inlineAllOfConflictSpec() *client.APISpec {
	spec := baseSpec()

	spec.Schemas["InlineDup"] = &client.Schema{
		AllOf: []*client.Schema{
			{
				Type: "object", Required: []string{"payload"},
				Properties: map[string]*client.Schema{
					"payload": {
						Type: "object",
						Properties: map[string]*client.Schema{
							"p1":    {Type: "string"},
							"list1": {Type: "array", Items: &client.Schema{Ref: "#/components/schemas/User"}},
						},
					},
				},
			},
			{
				Type: "object", Required: []string{"payload"},
				Properties: map[string]*client.Schema{
					"payload": {
						Type: "object",
						Properties: map[string]*client.Schema{
							"p2":    {Type: "string"},
							"list2": {Type: "array", Items: &client.Schema{Ref: "#/components/schemas/User"}},
						},
					},
				},
			},
		},
	}

	return spec
}

// TestCodecTableAllOfInlineConflictResolvesFirstDeclaredWinsNoWarning pins
// the table-shape half of the known limitation: no conflict warning fires
// (both layers compute the SAME path-derived synthetic id, so the conflict
// check never sees them differ), and the winning entry reflects the FIRST
// declared layer.
func TestCodecTableAllOfInlineConflictResolvesFirstDeclaredWinsNoWarning(t *testing.T) {
	spec := inlineAllOfConflictSpec()
	code, warnings := NewCodecGenerator().Generate(spec, baseConfig())

	for _, w := range warnings {
		assert.NotContains(t, w, `"InlineDup"`,
			"this is the documented residual limitation: two inline sub-schemas at the same field name are NOT detected as a conflict")
	}

	assert.Contains(t, code, `"InlineDup.payload": {"kind": "object", "fields": {"list1"`,
		"the FIRST declared layer's inline structure must be what's registered")
	assert.NotContains(t, code, `"list2"`, "the second layer's inline structure must never be registered at all")
}

// TestCodecRuntimeAllOfInlineConflictResolvesFirstDeclaredWins is the
// execution proof: the FIRST layer's structure drives the walk (list1 gets
// a fresh array reference), while the SECOND layer's structure was never
// registered at all, so list2 passes through as an unrecognised key
// (unchanged reference) -- the opposite direction from the $ref-vs-$ref
// case, which is exactly why the doc comment now says "last-declared-wins
// WHEN the two layers resolve to different effective codecs" rather than
// unconditionally.
func TestCodecRuntimeAllOfInlineConflictResolvesFirstDeclaredWins(t *testing.T) {
	spec := inlineAllOfConflictSpec()

	dir := t.TempDir()
	code, _ := NewCodecGenerator().Generate(spec, baseConfig())
	writeTree(t, dir, map[string]string{"src/codecs.ts": code})

	driver := `
import { decode } from './codecs';

const results: Record<string, any> = {};

const payload = { payload: { p1: 'a', p2: 'b', list1: [{ id: '1' }], list2: [{ id: '2' }] } };
const decoded = decode(payload, 'InlineDup');

results.list1Walked = decoded.payload.list1 !== payload.payload.list1;
results.list2Walked = decoded.payload.list2 !== payload.payload.list2;

console.log(JSON.stringify(results));
`
	writeTree(t, dir, map[string]string{"src/__driver_inline_allof_conflict.ts": driver})

	stdout := runNodeDriver(t, dir, "src/__driver_inline_allof_conflict.ts")

	var got struct {
		List1Walked bool `json:"list1Walked"`
		List2Walked bool `json:"list2Walked"`
	}
	decodeLastLine(t, stdout, &got)

	assert.True(t, got.List1Walked,
		"the FIRST declared inline layer must drive the walk; driver stdout:\n%s", stdout)
	assert.False(t, got.List2Walked,
		"the SECOND declared inline layer's structure must never have been registered, so its field passes through untouched; driver stdout:\n%s", stdout)
}

// ========== Fix round 2: cosmetic -- union $ref cycle warning wording ==========

// TestCodecTableUnionCycleEvidenceFreeWarningNamesCycleNotPassthrough covers
// UA: oneOf[$ref UB], UB: oneOf[$ref UA] -- a genuine reference cycle.
// add()'s cycle guard (reserve the id before recursing) means that, from
// deep inside building UB, checking UA's entry sees the RESERVED
// placeholder {Kind: "passthrough"} reserved by the call frame further up
// the stack that is still building UA -- indistinguishable from a
// genuinely-resolved passthrough entry unless something tracks "still
// being built". Before this fix, the evidence-free warning would report
// that placeholder's kind literally, misleadingly implying UA had already
// resolved to passthrough when it is actually mid-construction as a union.
func TestCodecTableUnionCycleEvidenceFreeWarningNamesCycleNotPassthrough(t *testing.T) {
	spec := baseSpec()
	spec.Schemas["UA"] = &client.Schema{OneOf: []*client.Schema{{Ref: "#/components/schemas/UB"}}}
	spec.Schemas["UB"] = &client.Schema{OneOf: []*client.Schema{{Ref: "#/components/schemas/UA"}}}

	done := make(chan []string, 1)
	go func() {
		_, warnings := NewCodecGenerator().Generate(spec, baseConfig())
		done <- warnings
	}()

	var warnings []string
	select {
	case warnings = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("generation did not terminate on a union $ref cycle")
	}

	var cyclicWarning string
	for _, w := range warnings {
		if strings.Contains(w, "cyclic reference back to a schema still being built") {
			cyclicWarning = w
		}

		assert.NotContains(t, w, `member "UA" offers no required wire fields to match on (kind "passthrough")`,
			"UA is never genuinely passthrough -- a member still mid-construction (a cycle) must not be mislabeled as if it had already resolved to passthrough")
		assert.NotContains(t, w, `member "UB" offers no required wire fields to match on (kind "passthrough")`)
	}

	require.NotEmpty(t, cyclicWarning, "the cyclic member's warning must name the cycle, not misreport the placeholder kind")
}
