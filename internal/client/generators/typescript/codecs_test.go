package typescript

import (
	"context"
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
	code := NewCodecGenerator().Generate(nestedSpec(), baseConfig())

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
	code := NewCodecGenerator().Generate(nestedSpec(), baseConfig())

	assert.Contains(t, code, `"Nested.items":`, "an inline array property needs its own entry, keyed by parent and property name")
	assert.Contains(t, code, `"Nested.tags":`, "an inline record property needs its own entry")

	// The $ref property must reuse the named codec, not mint a synthetic one.
	assert.NotContains(t, code, `"Nested.user":`)
	assert.Contains(t, code, `"codec": "User"`)
}

// TestCodecTableUnionRequiresDiscriminator covers the runtime rule that a
// union with no discriminator degrades to passthrough. Guessing a member by
// structural shape could rename fields based on a wrong match, which is
// worse than leaving the payload alone.
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

	code := NewCodecGenerator().Generate(withDisc, baseConfig())
	assert.Contains(t, code, `"kind": "union"`)
	assert.Contains(t, code, `"wire": "kind"`)

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

	code = NewCodecGenerator().Generate(noDisc, baseConfig())
	assert.NotContains(t, code, `"kind": "union"`)
	assert.Contains(t, code, `"Pet": {"kind": "passthrough"}`)
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
	go func() { done <- NewCodecGenerator().Generate(spec, baseConfig()) }()

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
	first := NewCodecGenerator().Generate(spec, baseConfig())

	for i := range 12 {
		if got := NewCodecGenerator().Generate(spec, baseConfig()); got != first {
			t.Fatalf("run %d differs", i)
		}
	}
}

func TestCodecsEmittedAndExported(t *testing.T) {
	out, err := NewGenerator().Generate(context.Background(), baseSpec(), baseConfig())
	require.NoError(t, err)

	assert.Contains(t, out.Files, "src/codecs.ts")
	assert.Contains(t, out.Files["src/index.ts"], "export * from './codecs';")
}

// TestCodecRuntimeRulesArePresent pins the three non-negotiable runtime
// behaviours in the emitted walk. The behavioural proof is the execution
// test; this catches an accidental removal without a Node round trip.
func TestCodecRuntimeRulesArePresent(t *testing.T) {
	code := NewCodecGenerator().Generate(baseSpec(), baseConfig())

	assert.Contains(t, code, "out[key] = val;", "unknown keys must pass through verbatim")
	assert.Contains(t, code, "Keys are data here", "a record must rename values but never keys")
	assert.True(t, strings.Contains(code, "if (typeof tag !== 'string')"),
		"a union whose discriminator value is missing or non-string must fall back to passthrough")
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
	writeTree(t, dir, map[string]string{
		"src/codecs.ts": NewCodecGenerator().Generate(spec, baseConfig()),
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
