package typescript

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/xraph/forge/internal/client"
)

func jsonMediaEndpoint(responseType, bodyType string) *client.Endpoint {
	schemaRef := &client.Schema{Ref: "#/components/schemas/Order"}

	ep := &client.Endpoint{
		Method: "POST", Path: "/orders", OperationID: "orders.create",
		Responses: map[int]*client.Response{200: {Content: map[string]*client.MediaType{responseType: {Schema: schemaRef}}}},
	}

	if bodyType != "" {
		ep.RequestBody = &client.RequestBody{Required: true, Content: map[string]*client.MediaType{bodyType: {Schema: schemaRef}}}
	}

	return ep
}

func TestIsJSONMediaType(t *testing.T) {
	for contentType, want := range map[string]bool{
		"application/json":                  true,
		"Application/JSON; charset=utf-8":   true,
		" text/json ":                       true,
		"application/problem+json":          true,
		"application/vnd.api+json; foo=bar": true,
		"application/jsonl":                 false,
		"application/x-ndjson":              false,
		"text/plain":                        false,
		"application/json-patch":            false,
		"image/png":                         false,
		"":                                  false,
		"application/xml; note=\"+json\"":   false,
	} {
		assert.Equal(t, want, isJSONMediaType(contentType), contentType)
	}
}

// A +json response is JSON on the wire, so it gets the codec an
// application/json response with the same schema gets.
func TestJSONSuffixResponseGetsItsCodec(t *testing.T) {
	for _, contentType := range []string{"application/json", "application/problem+json", "application/vnd.api+json"} {
		id, warning := responseCodecRef(jsonMediaEndpoint(contentType, ""))

		assert.Equal(t, "Order", id, contentType)
		assert.Empty(t, warning, contentType)
	}

	id, _ := responseCodecRef(jsonMediaEndpoint("image/png", ""))
	assert.Empty(t, id, "a non-JSON response never gets a codec")
}

func TestJSONSuffixRequestBodyIsPlannedAsJSONWithItsCodec(t *testing.T) {
	ep := jsonMediaEndpoint("application/json", "application/vnd.api+json")

	assert.Equal(t, "application/vnd.api+json", requestBodyContentType(ep))

	id, warning := requestBodyCodecRef(ep)
	assert.Equal(t, "Order", id)
	assert.Empty(t, warning)
}

func TestJSONEntryOutranksTextAndOtherBodies(t *testing.T) {
	schema := &client.Schema{Ref: "#/components/schemas/Order"}
	ep := &client.Endpoint{RequestBody: &client.RequestBody{Content: map[string]*client.MediaType{
		"text/plain":               {Schema: &client.Schema{Type: "string"}},
		"multipart/form-data":      {Schema: &client.Schema{Type: "object"}},
		"application/vnd.api+json": {Schema: schema},
		"application/json":         {},
	}}}

	// A schemaless application/json is skipped, as it always was, so the
	// +json entry that has a schema wins.
	assert.Equal(t, "application/vnd.api+json", requestBodyContentType(ep))

	ep.RequestBody.Content["application/json"] = &client.MediaType{Schema: schema}
	assert.Equal(t, "application/json", requestBodyContentType(ep))
}

func TestJSONSuffixArrayResponseRegistersItsCodec(t *testing.T) {
	spec := &client.APISpec{
		Info:    client.APIInfo{Title: "T", Version: "1"},
		Schemas: map[string]*client.Schema{"Order": {Type: "object", Properties: map[string]*client.Schema{"order_no": {Type: "string"}}}},
		Endpoints: []client.Endpoint{{
			Method: "GET", Path: "/orders", OperationID: "orders.list",
			Responses: map[int]*client.Response{200: {Content: map[string]*client.MediaType{
				"application/vnd.api+json": {Schema: &client.Schema{Type: "array", Items: &client.Schema{Ref: "#/components/schemas/Order"}}},
			}}},
		}},
	}

	cfg := client.DefaultConfig()
	cfg.Language = "typescript"
	table := buildCodecTable(spec, cfg)

	id, _ := responseCodecRef(&spec.Endpoints[0])
	assert.Equal(t, arrayRefCodecID("Order"), id)
	assert.Contains(t, table.entries, id, "the array codec the response names must be in the table")
}

// The array codec a request body looks up must be the one the table
// registered. A schemaless application/json entry beside a +json body with an
// array-of-$ref schema used to register under one key (the schemaless entry,
// so nothing) and be looked up under the other.
func TestRequestBodyArrayCodecIsRegisteredUnderItsLookupKey(t *testing.T) {
	spec := &client.APISpec{
		Schemas: map[string]*client.Schema{
			"Order": {Type: "object", Properties: map[string]*client.Schema{"order_id": {Type: "string"}}},
		},
		Endpoints: []client.Endpoint{{
			Method: "POST", Path: "/orders/bulk", OperationID: "orders.bulk",
			RequestBody: &client.RequestBody{Required: true, Content: map[string]*client.MediaType{
				"application/json":         {},
				"application/vnd.api+json": {Schema: &client.Schema{Type: "array", Items: &client.Schema{Ref: "#/components/schemas/Order"}}},
			}},
			Responses: map[int]*client.Response{204: {Description: "ok"}},
		}},
	}

	id, _ := requestBodyCodecRef(&spec.Endpoints[0])
	if !assert.NotEmpty(t, id, "the +json array body resolves to a codec") {
		return
	}

	table := buildCodecTable(spec, client.GeneratorConfig{Language: "typescript", FieldNaming: client.NamingCamel})

	_, registered := table.entries[id]
	assert.True(t, registered, "requestBodyCodecRef looks up %q, which the codec table never registered", id)
}

func TestIsTextMediaType(t *testing.T) {
	for contentType, want := range map[string]bool{
		"text/plain; charset=utf-8":         true,
		"Text/CSV":                          true,
		"application/atom+xml":              true,
		"application/openapi+yaml":          true,
		"application/xml":                   true,
		"application/x-ndjson":              true,
		"application/jsonl":                 true,
		"application/x-www-form-urlencoded": true,
		"application/octet-stream":          false,
		"application/problem+json":          false,
		"image/svg":                         false,
		"":                                  false,
	} {
		assert.Equal(t, want, isTextMediaType(contentType), contentType)
	}
}

// The declared return type has to be what fetch.ts resolves with, so a
// response is typed by the same rule the runtime decodes it with.
func TestResponseBodyTypeFollowsTheRuntimeRule(t *testing.T) {
	r := &RESTGenerator{}
	spec := &client.APISpec{}
	order := &client.Schema{Ref: "#/components/schemas/Order"}

	for contentType, want := range map[string]string{
		"application/json":                        "types.Order",
		"application/problem+json":                "types.Order",
		"application/vnd.api+json; charset=utf-8": "types.Order",
		"text/json":                               "types.Order",
		"application/xml":                         "string",
		"application/x-ndjson":                    "string",
		"application/jsonl":                       "string",
		"image/png":                               "Blob",
	} {
		resp := &client.Response{Content: map[string]*client.MediaType{contentType: {Schema: order}}}
		assert.Equal(t, want, r.responseBodyType(resp, spec), contentType)
	}

	schemaless := &client.Response{Content: map[string]*client.MediaType{"application/problem+json": {}}}
	assert.Equal(t, "any", r.responseBodyType(schemaless, spec),
		"a schemaless JSON response is still parsed as JSON, so it cannot be typed Blob")
}

// TestFetchDecodesByTheSharedContentTypeRule runs the generated fetch.ts
// against a stub fetch. A problem+json success body must be parsed and run
// through its codec, and a problem+json error body must reach HTTPError's
// details; the substring test this replaced handed both back as a Blob.
func TestFetchDecodesByTheSharedContentTypeRule(t *testing.T) {
	dir := writeFetchOnly(t)

	driver := `
import { HTTPClient, HTTPError } from './fetch';
import { CODECS } from './codecs';

function respond(body: string, contentType: string, status = 200) {
  (globalThis as any).fetch = async () => new Response(body, { status, headers: { 'content-type': contentType } });
}

function shape(value: any) {
  return typeof value === 'string' ? 'string' : value instanceof Blob ? 'blob' : value;
}

async function main() {
  const client = new HTTPClient('http://example.invalid', 5000);
  const results: Record<string, any> = {};

  respond('{"user_id":"x"}', 'Application/Problem+JSON; charset=utf-8');
  results.problemJSON = await client.request<any>({ method: 'GET', url: '/x', responseCodec: CODECS['User'] });

  respond('{"user_id":"y"}', 'application/vnd.api+json');
  results.vndJSON = await client.request<any>({ method: 'GET', url: '/x' });

  respond('{"a":1}', 'text/json');
  results.textJSON = await client.request<any>({ method: 'GET', url: '/x' });

  respond('{"a":1}\n{"a":2}\n', 'application/jsonl');
  results.jsonl = shape(await client.request<any>({ method: 'GET', url: '/x' }));

  respond('<a/>', 'application/atom+xml');
  results.atom = shape(await client.request<any>({ method: 'GET', url: '/x' }));

  respond('{"a":1}', 'application/octet-stream');
  results.octet = shape(await client.request<any>({ method: 'GET', url: '/x' }));

  respond('{"title":"Invalid","code":"invalid_order","detail":"no lines"}', 'application/problem+json', 422);
  try {
    await client.request<any>({ method: 'POST', url: '/x' });
    results.problemError = 'resolved';
  } catch (err) {
    const e = err as HTTPError;
    results.problemError = { status: e.statusCode, code: e.code, details: e.details };
  }

  console.log(JSON.stringify(results));
}

main().catch((err) => { console.error(err); process.exit(1); });
`
	writeTree(t, dir, map[string]string{"src/__driver_media_rule.ts": driver})

	stdout := runNodeDriver(t, dir, "src/__driver_media_rule.ts")

	var result struct {
		ProblemJSON  map[string]any `json:"problemJSON"`
		VndJSON      map[string]any `json:"vndJSON"`
		TextJSON     map[string]any `json:"textJSON"`
		JSONL        string         `json:"jsonl"`
		Atom         string         `json:"atom"`
		Octet        string         `json:"octet"`
		ProblemError struct {
			Status  int            `json:"status"`
			Code    string         `json:"code"`
			Details map[string]any `json:"details"`
		} `json:"problemError"`
	}
	decodeLastLine(t, stdout, &result)

	assert.Equal(t, map[string]any{"userId": "x"}, result.ProblemJSON,
		"a problem+json body must be parsed and decoded through its codec; driver stdout:\n%s", stdout)
	assert.Equal(t, map[string]any{"user_id": "y"}, result.VndJSON, "a +json body without a codec is parsed as is")
	assert.Equal(t, map[string]any{"a": float64(1)}, result.TextJSON, "text/json is JSON, not text")
	assert.Equal(t, "string", result.JSONL, "application/jsonl only contains the substring application/json; it is text")
	assert.Equal(t, "string", result.Atom, "a +xml body is text")
	assert.Equal(t, "blob", result.Octet, "an opaque type stays bytes")
	assert.Equal(t, 422, result.ProblemError.Status)
	assert.Equal(t, "invalid_order", result.ProblemError.Code, "a problem+json error body must be parsed into HTTPError")
	assert.Equal(t, "no lines", result.ProblemError.Details["detail"])
}
