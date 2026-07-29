package typescript

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/forge/internal/client"
)

// TestFixRound2DiscriminatorTagNameDisagreementEncodesCorrectly is the
// runtime proof for fix-round-2 finding (a): the encode-side discriminator
// resolution took the FIRST member declaring the wire tag property and used
// THAT member's ts name as a single global candidate for every member --
// correct only when every member happens to render the tag under the same
// name. A schema-scoped FieldOverrides entry naming just one member's
// rendering differently ("Dog.pet_kind" -> "kindOfDog") breaks this: Dog's
// actual payload uses "kindOfDog", not "petKind" (Cat's name, which won the
// scan because Cat is declared first), so `src["petKind"]` was always
// undefined and the WHOLE union passed through unrenamed -- silently,
// regardless of declaration order.
//
// Measured BEFORE this round's fix (review's own reproduction, quoted
// verbatim):
//
//	c.pets.create({kindOfDog:'dog', barkVolume:11})  ->  wire {"kindOfDog":"dog","barkVolume":11}
func TestFixRound2DiscriminatorTagNameDisagreementEncodesCorrectly(t *testing.T) {
	spec := discriminatedPetSnakeCaseSpec()

	cfg := baseConfig()
	cfg.FieldOverrides = map[string]string{"Dog.pet_kind": "kindOfDog"}

	out, err := NewGenerator().Generate(context.Background(), spec, cfg)
	require.NoError(t, err)

	types := out.Files["src/types.ts"]
	require.Contains(t, types, "kindOfDog",
		"sanity check: the FieldOverrides entry must actually change Dog's rendered TS name, or this test isn't exercising the disagreement at all")

	dir := t.TempDir()
	writeTree(t, dir, out.Files)

	driver := `
import { RESTClient } from './rest';

async function main() {
  const client: any = new RESTClient({ baseURL: 'http://example.invalid' });

  let captured: any;
  (globalThis as any).fetch = async (_url: string, init: any) => {
    captured = init;
    return new Response(null, { status: 204 });
  };

  await client.pets.create({ kindOfDog: 'dog', barkVolume: 11 });

  console.log(JSON.stringify({ sentBody: JSON.parse(captured.body) }));
}

main().catch((err) => { console.error(err); process.exit(1); });
`
	writeTree(t, dir, map[string]string{"src/__driver_fixround2_disagreement.ts": driver})

	stdout := runNodeDriver(t, dir, "src/__driver_fixround2_disagreement.ts")

	var result struct {
		SentBody map[string]any `json:"sentBody"`
	}
	decodeLastLine(t, stdout, &result)

	assert.Equal(t, "dog", result.SentBody["pet_kind"],
		"Dog's OWN rendered tag name (kindOfDog) must be tried as a candidate, not just Cat's (petKind, which won the old first-declared scan); driver stdout:\n%s", stdout)
	assert.Equal(t, float64(11), result.SentBody["bark_volume"],
		"once the correct member (Dog) is resolved, its other fields must be renamed too, not just the tag; driver stdout:\n%s", stdout)

	_, hadUnrenamedTag := result.SentBody["kindOfDog"]
	assert.False(t, hadUnrenamedTag, "the TS-cased key must not survive encoding onto the wire")
	_, hadUnrenamedField := result.SentBody["barkVolume"]
	assert.False(t, hadUnrenamedField, "the TS-cased key must not survive encoding onto the wire")
}

// TestFixRound2WarnsOnDiscriminatorTagNameDisagreement is the generation-time
// proof half of finding (a): members disagreeing on the discriminator
// property's rendered TS name is a real spec smell -- encode() must now try
// every distinct name, but which one resolves is runtime-data-dependent, and
// that ambiguity should be visible to whoever generated the client, not
// silent.
func TestFixRound2WarnsOnDiscriminatorTagNameDisagreement(t *testing.T) {
	spec := discriminatedPetSnakeCaseSpec()

	cfg := baseConfig()
	cfg.FieldOverrides = map[string]string{"Dog.pet_kind": "kindOfDog"}

	_, warnings := NewCodecGenerator().Generate(spec, cfg)

	found := false
	for _, w := range warnings {
		if strings.Contains(w, "Pet") && strings.Contains(w, "pet_kind") {
			found = true
		}
	}
	assert.True(t, found, "expected a warning naming schema %q and discriminator property %q; got: %v", "Pet", "pet_kind", warnings)
}

// noDiscriminatorPropertySpec returns baseSpec() plus an oneOf union where
// the discriminator names a property NEITHER member actually declares:
// APet = oneOf[ACat, ADog], discriminator "pet_kind", but neither ACat nor
// ADog has a "pet_kind" property at all. This is finding (b): a lenient
// (non-strictly-conforming) spec where the discriminator names a property no
// member carries -- strict OpenAPI requires every member to declare and
// require the discriminator property, but nothing in this codebase enforces
// that today.
func noDiscriminatorPropertySpec() *client.APISpec {
	spec := baseSpec()

	spec.Schemas["ACat"] = &client.Schema{
		Type:       "object",
		Properties: map[string]*client.Schema{"meows": {Type: "boolean"}},
	}
	spec.Schemas["ADog"] = &client.Schema{
		Type:       "object",
		Properties: map[string]*client.Schema{"bark_volume": {Type: "integer"}},
	}
	spec.Schemas["APet"] = &client.Schema{
		OneOf: []*client.Schema{
			{Ref: "#/components/schemas/ACat"},
			{Ref: "#/components/schemas/ADog"},
		},
		Discriminator: &client.Discriminator{
			PropertyName: "pet_kind",
			Mapping: map[string]string{
				"cat": "#/components/schemas/ACat",
				"dog": "#/components/schemas/ADog",
			},
		},
	}

	spec.Endpoints = append(spec.Endpoints, client.Endpoint{
		Method: "POST", Path: "/apets", OperationID: "apets.create",
		RequestBody: &client.RequestBody{Required: true, Content: map[string]*client.MediaType{
			"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/APet"}}}},
		Responses: map[int]*client.Response{201: {Content: map[string]*client.MediaType{
			"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/APet"}}}}},
	})

	return spec
}

// TestFixRound2NoMemberDeclaresDiscriminatorPropertyWarnsAndStillDecodes is
// the runtime + generation-time proof for finding (b): when NO member
// declares the discriminator property at all, encode() has no candidate key
// to try besides the bare wire name -- which, correctly, will almost never
// be present on a TS-shaped payload (the property isn't even part of any
// member's declared TypeScript type) -- so encoding this union safely stays
// a passthrough (NOT a silent corruption: nothing is mis-renamed, nothing is
// invented). What changes in this round is that generation now WARNS about
// this spec smell, and decode (which reads the wire name directly against a
// wire-shaped payload) is confirmed to still work fine -- exactly the
// asymmetry the original Critical finding was about.
func TestFixRound2NoMemberDeclaresDiscriminatorPropertyWarnsAndStillDecodes(t *testing.T) {
	spec := noDiscriminatorPropertySpec()

	out, err := NewGenerator().Generate(context.Background(), spec, baseConfig())
	require.NoError(t, err)

	foundWarning := false
	for _, w := range out.Warnings {
		if strings.Contains(w, "APet") && strings.Contains(w, "pet_kind") {
			foundWarning = true
		}
	}
	assert.True(t, foundWarning,
		"expected a generation-time warning naming schema %q and discriminator property %q (declared by no member); got: %v",
		"APet", "pet_kind", out.Warnings)

	dir := t.TempDir()
	writeTree(t, dir, out.Files)

	driver := `
import { RESTClient } from './rest';

async function main() {
  const client: any = new RESTClient({ baseURL: 'http://example.invalid' });
  const results: Record<string, any> = {};

  // Encode: no candidate key exists on the TS-shaped payload, so this must
  // stay a safe passthrough -- not corrupted, just unresolved.
  let captured: any;
  (globalThis as any).fetch = async (_url: string, init: any) => {
    captured = init;
    return new Response(null, { status: 204 });
  };
  await client.apets.create({ barkVolume: 11 });
  results.sentBody = JSON.parse(captured.body);

  // Decode: reading the wire name directly against a wire-shaped payload
  // must still resolve the member correctly.
  (globalThis as any).fetch = async () => new Response(
    JSON.stringify({ pet_kind: 'dog', bark_volume: 9 }),
    { status: 201, headers: { 'content-type': 'application/json' } },
  );
  results.decoded = await client.apets.create({ barkVolume: 11 });

  console.log(JSON.stringify(results));
}

main().catch((err) => { console.error(err); process.exit(1); });
`
	writeTree(t, dir, map[string]string{"src/__driver_fixround2_no_tag_property.ts": driver})

	stdout := runNodeDriver(t, dir, "src/__driver_fixround2_no_tag_property.ts")

	var result struct {
		SentBody map[string]any `json:"sentBody"`
		Decoded  map[string]any `json:"decoded"`
	}
	decodeLastLine(t, stdout, &result)

	assert.Equal(t, float64(11), result.SentBody["barkVolume"],
		"safe, documented passthrough: with no candidate key at all, the union cannot be resolved, so it must stay untouched rather than guess; driver stdout:\n%s", stdout)
	_, hadWireKey := result.SentBody["bark_volume"]
	assert.False(t, hadWireKey, "an unresolved union must not partially rename -- it is all-or-nothing passthrough")

	assert.Equal(t, float64(9), result.Decoded["barkVolume"],
		"decode direction must still work correctly -- it reads the wire name directly against a wire-shaped payload; driver stdout:\n%s", stdout)
}
