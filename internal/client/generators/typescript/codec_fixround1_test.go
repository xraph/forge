package typescript

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/forge/internal/client"
)

// discriminatedPetSnakeCaseSpec returns baseSpec() plus a discriminated union (Pet =
// oneOf[Cat, Dog], discriminator "pet_kind") and a POST /pets endpoint whose
// body and response are both $ref Pet. This is the review's own reproduction
// scenario for the fix-round-1 CRITICAL finding: Cat/Dog's "pet_kind" and
// Dog's "bark_volume" both have underscores, so a camelCase client renders
// them as "petKind"/"barkVolume" -- exactly what's needed to prove a rename
// actually happened, not merely that the same string passed through.
func discriminatedPetSnakeCaseSpec() *client.APISpec {
	spec := baseSpec()

	spec.Schemas["Cat"] = &client.Schema{
		Type: "object", Required: []string{"pet_kind"},
		Properties: map[string]*client.Schema{
			"pet_kind": {Type: "string", Enum: []any{"cat"}},
			"meows":    {Type: "boolean"},
		},
	}
	spec.Schemas["Dog"] = &client.Schema{
		Type: "object", Required: []string{"pet_kind", "bark_volume"},
		Properties: map[string]*client.Schema{
			"pet_kind":    {Type: "string", Enum: []any{"dog"}},
			"bark_volume": {Type: "integer"},
		},
	}
	spec.Schemas["Pet"] = &client.Schema{
		OneOf: []*client.Schema{
			{Ref: "#/components/schemas/Cat"},
			{Ref: "#/components/schemas/Dog"},
		},
		Discriminator: &client.Discriminator{
			PropertyName: "pet_kind",
			Mapping: map[string]string{
				"cat": "#/components/schemas/Cat",
				"dog": "#/components/schemas/Dog",
			},
		},
	}

	spec.Endpoints = append(spec.Endpoints, client.Endpoint{
		Method: "POST", Path: "/pets", OperationID: "pets.create",
		RequestBody: &client.RequestBody{Required: true, Content: map[string]*client.MediaType{
			"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/Pet"}}}},
		Responses: map[int]*client.Response{201: {Content: map[string]*client.MediaType{
			"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/Pet"}}}}},
	})

	return spec
}

// TestCriticalDiscriminatedUnionBodyEncodesCorrectly is the runtime proof for
// fix-round-1 review's CRITICAL finding: codecRuntime's union case resolved
// the discriminator tag (and, for an undiscriminated union, `required`) by
// WIRE name in both directions. Decoding a wire-shaped payload was already
// correct (the tag IS the wire name there); encoding a TS-shaped payload
// looked up a key ("pet_kind") that plainly does not exist on
// { petKind: 'dog', barkVolume: 11 } -- so `tag` was always undefined,
// codecRuntime fell through to its passthrough branch, and the ENTIRE union
// body shipped unrenamed, silently, with zero generation-time warning.
//
// Measured BEFORE the fix (review's own reproduction, quoted verbatim):
//
//	call:  c.pets.create({ petKind: 'dog', barkVolume: 11 })
//	wire:  {"petKind":"dog","barkVolume":11}     <-- UNRENAMED
//	resp:  {"pet_kind":"dog","bark_volume":9} -> {petKind:'dog', barkVolume:9}   (decode fine)
func TestCriticalDiscriminatedUnionBodyEncodesCorrectly(t *testing.T) {
	spec := discriminatedPetSnakeCaseSpec()

	out, err := NewGenerator().Generate(context.Background(), spec, baseConfig())
	require.NoError(t, err)

	rest := out.Files["src/rest.ts"]
	require.Contains(t, rest, `bodyCodec: "Pet"`,
		"sanity check: pets.create's request body is $ref Pet, or this test is not exercising the union codec path at all")
	require.Contains(t, rest, `responseCodec: "Pet"`)

	dir := t.TempDir()
	writeTree(t, dir, out.Files)

	driver := `
import { RESTClient } from './rest';

async function main() {
  const client: any = new RESTClient({ baseURL: 'http://example.invalid' });

  let captured: any;
  (globalThis as any).fetch = async (_url: string, init: any) => {
    captured = init;
    return new Response(JSON.stringify({ pet_kind: 'dog', bark_volume: 9 }), {
      status: 201,
      headers: { 'content-type': 'application/json' },
    });
  };

  const result = await client.pets.create({ petKind: 'dog', barkVolume: 11 });

  console.log(JSON.stringify({ sentBody: JSON.parse(captured.body), result }));
}

main().catch((err) => { console.error(err); process.exit(1); });
`
	writeTree(t, dir, map[string]string{"src/__driver_critical_discriminated_union.ts": driver})

	stdout := runNodeDriver(t, dir, "src/__driver_critical_discriminated_union.ts")

	var result struct {
		SentBody map[string]any `json:"sentBody"`
		Result   map[string]any `json:"result"`
	}
	decodeLastLine(t, stdout, &result)

	assert.Equal(t, "dog", result.SentBody["pet_kind"],
		"discriminator tag must encode from petKind to pet_kind on the wire, not ship as the unrenamed TS key; driver stdout:\n%s", stdout)
	_, hadUnrenamedTag := result.SentBody["petKind"]
	assert.False(t, hadUnrenamedTag, "the TS-cased key must not survive encoding onto the wire")

	assert.Equal(t, float64(11), result.SentBody["bark_volume"],
		"the discriminated member's OWN required field must also be renamed on encode, not just the tag; driver stdout:\n%s", stdout)

	assert.Equal(t, "dog", result.Result["petKind"],
		"decode direction (already correct pre-fix) must still work: pet_kind -> petKind; driver stdout:\n%s", stdout)
	assert.Equal(t, float64(9), result.Result["barkVolume"], "driver stdout:\n%s", stdout)
}

// undiscriminatedThingSpec returns baseSpec() plus an UNDISCRIMINATED union
// (Thing = oneOf[Widget, Gadget], no discriminator) whose two members'
// required fields ("widget_id"/"gadget_id") both have underscores, so a
// camelCase client renders them as "widgetId"/"gadgetId" -- proving the
// structural-match half of the same CRITICAL bug: `required` lists wire
// names, and testing them directly against a TS-shaped `src` during encode
// always failed to match ANY member, falling through to the same silent
// passthrough.
func undiscriminatedThingSpec() *client.APISpec {
	spec := baseSpec()

	spec.Schemas["Widget"] = &client.Schema{
		Type: "object", Required: []string{"widget_id"},
		Properties: map[string]*client.Schema{"widget_id": {Type: "string"}},
	}
	spec.Schemas["Gadget"] = &client.Schema{
		Type: "object", Required: []string{"gadget_id"},
		Properties: map[string]*client.Schema{"gadget_id": {Type: "string"}},
	}
	spec.Schemas["Thing"] = &client.Schema{
		OneOf: []*client.Schema{
			{Ref: "#/components/schemas/Widget"},
			{Ref: "#/components/schemas/Gadget"},
		},
	}

	spec.Endpoints = append(spec.Endpoints, client.Endpoint{
		Method: "POST", Path: "/things", OperationID: "things.create",
		RequestBody: &client.RequestBody{Required: true, Content: map[string]*client.MediaType{
			"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/Thing"}}}},
		Responses: map[int]*client.Response{201: {Content: map[string]*client.MediaType{
			"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/Thing"}}}}},
	})

	return spec
}

// TestCriticalUndiscriminatedUnionBodyEncodesCorrectly is the structural-match
// half of the CRITICAL finding: an undiscriminated union's `required` wire
// names, tested directly against a TS-shaped encode() `src`, never matched
// any member, so the whole body passed through unrenamed on encode even
// though decode (wire-shaped `src`, wire-named `required`) already worked.
func TestCriticalUndiscriminatedUnionBodyEncodesCorrectly(t *testing.T) {
	spec := undiscriminatedThingSpec()

	out, err := NewGenerator().Generate(context.Background(), spec, baseConfig())
	require.NoError(t, err)

	rest := out.Files["src/rest.ts"]
	require.Contains(t, rest, `bodyCodec: "Thing"`,
		"sanity check: things.create's request body is $ref Thing, or this test is not exercising the union codec path at all")

	dir := t.TempDir()
	writeTree(t, dir, out.Files)

	driver := `
import { RESTClient } from './rest';

async function main() {
  const client: any = new RESTClient({ baseURL: 'http://example.invalid' });

  let captured: any;
  (globalThis as any).fetch = async (_url: string, init: any) => {
    captured = init;
    return new Response(JSON.stringify({ gadget_id: 'g1' }), {
      status: 201,
      headers: { 'content-type': 'application/json' },
    });
  };

  const result = await client.things.create({ widgetId: 'w1' });

  console.log(JSON.stringify({ sentBody: JSON.parse(captured.body), result }));
}

main().catch((err) => { console.error(err); process.exit(1); });
`
	writeTree(t, dir, map[string]string{"src/__driver_critical_undiscriminated_union.ts": driver})

	stdout := runNodeDriver(t, dir, "src/__driver_critical_undiscriminated_union.ts")

	var result struct {
		SentBody map[string]any `json:"sentBody"`
		Result   map[string]any `json:"result"`
	}
	decodeLastLine(t, stdout, &result)

	assert.Equal(t, "w1", result.SentBody["widget_id"],
		"an undiscriminated union member's required field (widgetId) must be matched via its TS name and encoded to widget_id on the wire; driver stdout:\n%s", stdout)
	_, hadUnrenamedKey := result.SentBody["widgetId"]
	assert.False(t, hadUnrenamedKey, "the TS-cased key must not survive encoding onto the wire")

	assert.Equal(t, "g1", result.Result["gadgetId"], "decode direction must still work: gadget_id -> gadgetId; driver stdout:\n%s", stdout)
}

// TestImportant1ArrayOfRefBodyAndResponseAreCodecd is the runtime proof for
// fix-round-1 review's IMPORTANT 1 finding: an endpoint whose JSON body or
// response is a bare array wrapping a direct $ref (`{type: array, items:
// $ref User}` -- the single most common OpenAPI "list of X" shape) got no
// codec on either side. requestBodyCodecRef/responseCodecRef both bottomed
// out at a direct refName(schema.Ref) check, which an array wrapper always
// fails, so the declared `types.User[]`/`body: types.User[]` camelCase type
// shipped over a wire-cased runtime payload for every list endpoint.
//
// Measured BEFORE the fix (review's own reproduction, quoted verbatim):
//
//	list.get(): Promise<types.User[]>       -> [{"id":"i","user_id":"srv","created_at":"ts"}]   UNDECODED
//	arraybody.create(body: types.User[])    -> [{"id":"i","userId":"cli",...}]                  UNENCODED
func TestImportant1ArrayOfRefBodyAndResponseAreCodecd(t *testing.T) {
	spec := baseSpec()
	spec.Endpoints = append(spec.Endpoints,
		client.Endpoint{
			Method: "GET", Path: "/list", OperationID: "list.get",
			Responses: map[int]*client.Response{200: {Content: map[string]*client.MediaType{
				"application/json": {Schema: &client.Schema{Type: "array", Items: &client.Schema{Ref: "#/components/schemas/User"}}}}}},
		},
		client.Endpoint{
			Method: "POST", Path: "/arraybody", OperationID: "arraybody.create",
			RequestBody: &client.RequestBody{Required: true, Content: map[string]*client.MediaType{
				"application/json": {Schema: &client.Schema{Type: "array", Items: &client.Schema{Ref: "#/components/schemas/User"}}}}},
			Responses: map[int]*client.Response{201: {Description: "ok"}},
		},
	)

	out, err := NewGenerator().Generate(context.Background(), spec, baseConfig())
	require.NoError(t, err)

	rest := out.Files["src/rest.ts"]
	require.Contains(t, rest, `responseCodec: "[]User"`,
		"sanity check: list.get's response is an array of $ref User, or this test is not exercising the array-of-ref codec path")
	require.Contains(t, rest, `bodyCodec: "[]User"`,
		"sanity check: arraybody.create's request body is an array of $ref User, or this test is not exercising the array-of-ref codec path")

	dir := t.TempDir()
	writeTree(t, dir, out.Files)

	driver := `
import { RESTClient } from './rest';

async function main() {
  const client: any = new RESTClient({ baseURL: 'http://example.invalid' });
  const results: Record<string, any> = {};

  // list.get(): a wire-cased array response must decode to camelCase.
  (globalThis as any).fetch = async () => new Response(
    JSON.stringify([{ id: 'i', user_id: 'srv', created_at: 'ts' }]),
    { status: 200, headers: { 'content-type': 'application/json' } },
  );
  results.listGet = await client.list.get();

  // arraybody.create(): a camelCase array body must encode to wire-cased.
  let captured: any;
  (globalThis as any).fetch = async (_url: string, init: any) => {
    captured = init;
    return new Response(null, { status: 201 });
  };
  await client.arraybody.create([{ id: 'i', userId: 'cli', createdAt: 'ts' }]);
  results.sentBody = JSON.parse(captured.body);

  console.log(JSON.stringify(results));
}

main().catch((err) => { console.error(err); process.exit(1); });
`
	writeTree(t, dir, map[string]string{"src/__driver_important1_array_of_ref.ts": driver})

	stdout := runNodeDriver(t, dir, "src/__driver_important1_array_of_ref.ts")

	var result struct {
		ListGet  []map[string]any `json:"listGet"`
		SentBody []map[string]any `json:"sentBody"`
	}
	decodeLastLine(t, stdout, &result)

	require.Len(t, result.ListGet, 1, "driver stdout:\n%s", stdout)
	assert.Equal(t, "srv", result.ListGet[0]["userId"],
		"each item of an array-of-$ref response must be decoded (user_id -> userId), not left wire-cased; driver stdout:\n%s", stdout)
	_, hadWireKey := result.ListGet[0]["user_id"]
	assert.False(t, hadWireKey)

	require.Len(t, result.SentBody, 1, "driver stdout:\n%s", stdout)
	assert.Equal(t, "cli", result.SentBody[0]["user_id"],
		"each item of an array-of-$ref request body must be encoded (userId -> user_id), not shipped camelCase; driver stdout:\n%s", stdout)
	_, hadTSKey := result.SentBody[0]["userId"]
	assert.False(t, hadTSKey)
}

// TestMinorRequestConfigDocWarnsInterceptorsMustSpread pins fix-round-1
// review's MINOR finding: the generated RequestConfig doc comment must warn
// interceptor authors that a request interceptor which RETURNS A REPLACEMENT
// object (rather than spreading the incoming config) silently drops fields
// it didn't name -- bodyCodec/responseCodec included. No behavior change was
// requested here (encode-after-interceptors is correct and stays), only
// documentation.
func TestMinorRequestConfigDocWarnsInterceptorsMustSpread(t *testing.T) {
	code := NewFetchClientGenerator().GenerateBaseClient(baseSpec(), baseConfig())

	idx := strings.Index(code, "export interface RequestConfig")
	require.NotEqual(t, -1, idx)

	preceding := code[:idx]
	assert.Contains(t, preceding, "spread",
		"the doc comment immediately above RequestConfig must tell interceptor authors to spread the incoming config, not replace it")
	assert.Contains(t, preceding, "bodyCodec",
		"the warning must name bodyCodec (and, by the same sentence, responseCodec) as fields a replacing interceptor silently drops")
}

// TestMinorReplacingInterceptorDropsBodyCodecButSpreadingPreservesIt is the
// runtime characterization (not a fix -- review confirmed
// encode-after-interceptors is correct and must stay) of the same MINOR
// finding: a request interceptor is free to return either a spread copy or a
// from-scratch replacement object, per RequestConfig | Promise<RequestConfig>.
// A replacement that only lists the fields it cares about silently drops
// bodyCodec, so a JSON body that would otherwise be renamed ships wire-cased
// and unrenamed instead -- with no error anywhere. Spreading preserves it.
//
// Measured (review's own reproduction, quoted verbatim):
//
//	spread  ({...cfg, headers:...})  -> {"user_id":"x"}   OK
//	observe (return cfg)             -> {"user_id":"x"}   OK
//	replace ({method, url, body})     -> {"userId":"x"}    bodyCodec lost
func TestMinorReplacingInterceptorDropsBodyCodecButSpreadingPreservesIt(t *testing.T) {
	dir := writeFetchOnly(t)

	driver := `
import { HTTPClient } from './fetch';

async function send(interceptor: (config: any) => any) {
  const client: any = new HTTPClient('http://example.invalid', 5000);
  client.addRequestInterceptor({ onRequest: interceptor });

  let captured: any;
  (globalThis as any).fetch = async (_url: string, init: any) => {
    captured = init;
    return new Response(null, { status: 204 });
  };

  await client.request({
    method: 'POST',
    url: '/users',
    body: { userId: 'x' },
    bodyCodec: 'User',
    allowEmptyBody: true,
  });

  return captured.body;
}

async function main() {
  const results: Record<string, string> = {};

  results.spread = await send((config: any) => ({ ...config, headers: { ...config.headers } }));
  results.observe = await send((config: any) => config);
  results.replace = await send((config: any) => ({ method: config.method, url: config.url, body: config.body }));

  console.log(JSON.stringify(results));
}

main().catch((err) => { console.error(err); process.exit(1); });
`
	writeTree(t, dir, map[string]string{"src/__driver_minor_interceptor_replace.ts": driver})

	stdout := runNodeDriver(t, dir, "src/__driver_minor_interceptor_replace.ts")

	var result struct {
		Spread  string `json:"spread"`
		Observe string `json:"observe"`
		Replace string `json:"replace"`
	}
	decodeLastLine(t, stdout, &result)

	assert.Equal(t, `{"user_id":"x"}`, result.Spread,
		"an interceptor that spreads the incoming config must preserve bodyCodec; driver stdout:\n%s", stdout)
	assert.Equal(t, `{"user_id":"x"}`, result.Observe,
		"an interceptor that returns config unchanged must preserve bodyCodec; driver stdout:\n%s", stdout)
	assert.Equal(t, `{"userId":"x"}`, result.Replace,
		"documented, pre-existing hazard: an interceptor that builds a from-scratch replacement object silently drops bodyCodec -- this is what the RequestConfig doc comment now warns about, not a defect this task fixes; driver stdout:\n%s", stdout)
}
