package typescript

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xraph/forge/internal/client"
)

func TestRESTGenerator_NestedStructure(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{
			Title:   "Test API",
			Version: "1.0.0",
		},
		Endpoints: []client.Endpoint{
			{
				Method:      "GET",
				Path:        "/api/capabilities/defaults",
				OperationID: "capabilities.defaults.get",
				Summary:     "Get default capabilities",
				Responses:   map[int]*client.Response{},
			},
			{
				Method:      "GET",
				Path:        "/api/connectors/categories/simple",
				OperationID: "connectors.categories.simple",
				Summary:     "List connector categories",
				Responses:   map[int]*client.Response{},
			},
		},
		Schemas: make(map[string]*client.Schema),
	}

	config := client.DefaultConfig()
	gen := NewRESTGenerator()
	code, _ := gen.Generate(spec, config)

	// Verify nested structure
	assert.Contains(t, code, "public readonly capabilities = {")
	assert.Contains(t, code, "defaults: {")
	assert.Contains(t, code, "get: async (")
	assert.Contains(t, code, "public readonly connectors = {")
	assert.Contains(t, code, "categories: {")
	assert.Contains(t, code, "simple: async (")

	// Verify JSDoc comments are present
	assert.Contains(t, code, "Get default capabilities")
	assert.Contains(t, code, "List connector categories")
}

func TestRESTGenerator_MixedMethodsAndProperties(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{
			Title:   "Test API",
			Version: "1.0.0",
		},
		Endpoints: []client.Endpoint{
			{
				Method:      "GET",
				Path:        "/api/users",
				OperationID: "users.list",
				Summary:     "List all users",
				Responses:   map[int]*client.Response{},
			},
			{
				Method:      "GET",
				Path:        "/api/users/active",
				OperationID: "users.active.list",
				Summary:     "List active users",
				Responses:   map[int]*client.Response{},
			},
		},
		Schemas: make(map[string]*client.Schema),
	}

	config := client.DefaultConfig()
	gen := NewRESTGenerator()
	code, _ := gen.Generate(spec, config)

	// Should support both users.list() and users.active.list()
	assert.Contains(t, code, "public readonly users = {")
	assert.Contains(t, code, "list: async (")
	assert.Contains(t, code, "active: {")

	// Verify both methods are present
	assert.Contains(t, code, "List all users")
	assert.Contains(t, code, "List active users")
}

func TestRESTGenerator_SingleLevelOperationID(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{
			Title:   "Test API",
			Version: "1.0.0",
		},
		Endpoints: []client.Endpoint{
			{
				Method:      "GET",
				Path:        "/api/status",
				OperationID: "getStatus",
				Summary:     "Get API status",
				Responses:   map[int]*client.Response{},
			},
		},
		Schemas: make(map[string]*client.Schema),
	}

	config := client.DefaultConfig()
	gen := NewRESTGenerator()
	code, _ := gen.Generate(spec, config)

	// Single level operation ID should be a root-level property
	assert.Contains(t, code, "public readonly getStatus = ")
	assert.Contains(t, code, "public readonly getStatus = async (")
	assert.Contains(t, code, "Get API status")
}

func TestRESTGenerator_EmptyOperationID(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{
			Title:   "Test API",
			Version: "1.0.0",
		},
		Endpoints: []client.Endpoint{
			{
				Method:      "GET",
				Path:        "/api/users/profile",
				OperationID: "", // Empty - should auto-generate
				Summary:     "Get user profile",
				Responses:   map[int]*client.Response{},
			},
		},
		Schemas: make(map[string]*client.Schema),
	}

	config := client.DefaultConfig()
	gen := NewRESTGenerator()
	code, _ := gen.Generate(spec, config)

	// Should generate from path: get.api.users.profile
	assert.Contains(t, code, "public readonly get = {")
	assert.Contains(t, code, "api: {")
	assert.Contains(t, code, "users: {")
	assert.Contains(t, code, "profile: async (")
}

func TestRESTGenerator_WithParameters(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{
			Title:   "Test API",
			Version: "1.0.0",
		},
		Endpoints: []client.Endpoint{
			{
				Method:      "GET",
				Path:        "/api/workspaces/{workspaceId}/connections/{connectionId}/billing/users/{externalId}",
				OperationID: "connections.billing.users.usage",
				Summary:     "Get user usage",
				PathParams: []client.Parameter{
					{Name: "workspaceId", Schema: &client.Schema{Type: "string"}, Required: true},
					{Name: "connectionId", Schema: &client.Schema{Type: "string"}, Required: true},
					{Name: "externalId", Schema: &client.Schema{Type: "string"}, Required: true},
				},
				QueryParams: []client.Parameter{
					{Name: "startDate", Schema: &client.Schema{Type: "string"}, Required: false},
					{Name: "endDate", Schema: &client.Schema{Type: "string"}, Required: false},
				},
				Responses: map[int]*client.Response{},
			},
		},
		Schemas: make(map[string]*client.Schema),
	}

	config := client.DefaultConfig()
	gen := NewRESTGenerator()
	code, _ := gen.Generate(spec, config)

	// Verify nested structure
	assert.Contains(t, code, "public readonly connections = {")
	assert.Contains(t, code, "billing: {")
	assert.Contains(t, code, "users: {")
	assert.Contains(t, code, "usage: async (")

	// Verify parameters are included
	assert.Contains(t, code, "workspaceId: string")
	assert.Contains(t, code, "connectionId: string")
	assert.Contains(t, code, "externalId: string")
	assert.Contains(t, code, "startDate?: string | undefined")
	assert.Contains(t, code, "endDate?: string | undefined")

	// Verify path template with URL encoding
	assert.Contains(t, code, "/api/workspaces/${encodeURIComponent(String(workspaceId))}/connections/${encodeURIComponent(String(connectionId))}/billing/users/${encodeURIComponent(String(externalId))}")

	// Verify query params handling
	assert.Contains(t, code, "queryParams: Record<string, any> = {}")
	assert.Contains(t, code, "if (startDate !== undefined)")
	assert.Contains(t, code, "if (endDate !== undefined)")
}

func TestRESTGenerator_WithRequestBody(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{
			Title:   "Test API",
			Version: "1.0.0",
		},
		Endpoints: []client.Endpoint{
			{
				Method:      "POST",
				Path:        "/api/users",
				OperationID: "users.create",
				Summary:     "Create a user",
				RequestBody: &client.RequestBody{
					Required: true,
					Content: map[string]*client.MediaType{
						"application/json": {
							Schema: &client.Schema{
								Type: "object",
								Properties: map[string]*client.Schema{
									"name":  {Type: "string"},
									"email": {Type: "string"},
								},
							},
						},
					},
				},
				Responses: map[int]*client.Response{
					201: {
						Description: "Created",
					},
				},
			},
		},
		Schemas: make(map[string]*client.Schema),
	}

	config := client.DefaultConfig()
	gen := NewRESTGenerator()
	code, _ := gen.Generate(spec, config)

	// Verify nested structure
	assert.Contains(t, code, "public readonly users = {")
	assert.Contains(t, code, "create: async (")

	// Verify request body parameter
	assert.Contains(t, code, "body: Record<string, any>")
	assert.Contains(t, code, "method: 'POST'")
	assert.Contains(t, code, "body,")
}

func TestRESTGenerator_DeprecatedEndpoint(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{
			Title:   "Test API",
			Version: "1.0.0",
		},
		Endpoints: []client.Endpoint{
			{
				Method:      "GET",
				Path:        "/api/legacy/endpoint",
				OperationID: "legacy.getOldData",
				Summary:     "Old endpoint",
				Deprecated:  true,
				Responses:   map[int]*client.Response{},
			},
		},
		Schemas: make(map[string]*client.Schema),
	}

	config := client.DefaultConfig()
	gen := NewRESTGenerator()
	code, _ := gen.Generate(spec, config)

	// Verify deprecated annotation
	assert.Contains(t, code, "@deprecated")
}

func TestRESTGenerator_DeterministicOutput(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{
			Title:   "Test API",
			Version: "1.0.0",
		},
		Endpoints: []client.Endpoint{
			{Method: "GET", Path: "/z", OperationID: "z.method", Responses: map[int]*client.Response{}},
			{Method: "GET", Path: "/a", OperationID: "a.method", Responses: map[int]*client.Response{}},
			{Method: "GET", Path: "/m", OperationID: "m.method", Responses: map[int]*client.Response{}},
		},
		Schemas: make(map[string]*client.Schema),
	}

	config := client.DefaultConfig()
	gen := NewRESTGenerator()

	// Generate twice
	code1, _ := gen.Generate(spec, config)
	code2, _ := gen.Generate(spec, config)

	// Output should be identical (sorted alphabetically)
	assert.Equal(t, code1, code2)

	// Check that methods appear in alphabetical order
	aIndex := strings.Index(code1, "public readonly a = {")
	mIndex := strings.Index(code1, "public readonly m = {")
	zIndex := strings.Index(code1, "public readonly z = {")

	require.NotEqual(t, -1, aIndex)
	require.NotEqual(t, -1, mIndex)
	require.NotEqual(t, -1, zIndex)

	assert.Less(t, aIndex, mIndex, "Expected 'a' to come before 'm'")
	assert.Less(t, mIndex, zIndex, "Expected 'm' to come before 'z'")
}

func TestRESTGenerator_DeepNesting(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{
			Title:   "Test API",
			Version: "1.0.0",
		},
		Endpoints: []client.Endpoint{
			{
				Method:      "GET",
				Path:        "/api/level1/level2/level3/level4/endpoint",
				OperationID: "level1.level2.level3.level4.getData",
				Summary:     "Deeply nested endpoint",
				Responses:   map[int]*client.Response{},
			},
		},
		Schemas: make(map[string]*client.Schema),
	}

	config := client.DefaultConfig()
	gen := NewRESTGenerator()
	code, _ := gen.Generate(spec, config)

	// Verify deep nesting structure
	assert.Contains(t, code, "public readonly level1 = {")
	assert.Contains(t, code, "level2: {")
	assert.Contains(t, code, "level3: {")
	assert.Contains(t, code, "level4: {")
	assert.Contains(t, code, "getData: async (")
}

func TestRESTGenerator_ConflictingOperationIDs(t *testing.T) {
	// Test case where one operation ID is a prefix of another
	// e.g., "users" vs "users.active.list"
	spec := &client.APISpec{
		Info: client.APIInfo{
			Title:   "Test API",
			Version: "1.0.0",
		},
		Endpoints: []client.Endpoint{
			{
				Method:      "GET",
				Path:        "/api/users",
				OperationID: "users", // Single part
				Summary:     "Get all users",
				Responses:   map[int]*client.Response{},
			},
			{
				Method:      "GET",
				Path:        "/api/users/active",
				OperationID: "users.active.list", // Nested under same prefix
				Summary:     "List active users",
				Responses:   map[int]*client.Response{},
			},
		},
		Schemas: make(map[string]*client.Schema),
	}

	config := client.DefaultConfig()
	gen := NewRESTGenerator()
	code, _ := gen.Generate(spec, config)

	// Both should be accessible
	// "users" becomes "users.users" to avoid conflict
	assert.Contains(t, code, "public readonly users = {")
	assert.Contains(t, code, "users: async (") // The original "users" method
	assert.Contains(t, code, "active: {")
	assert.Contains(t, code, "list: async (")
}

func TestRESTGenerator_ReturnTypes(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{
			Title:   "Test API",
			Version: "1.0.0",
		},
		Endpoints: []client.Endpoint{
			{
				Method:      "GET",
				Path:        "/api/data",
				OperationID: "data.get",
				Responses: map[int]*client.Response{
					200: {
						Description: "Success",
						Content: map[string]*client.MediaType{
							"application/json": {
								Schema: &client.Schema{
									Ref: "#/components/schemas/DataResponse",
								},
							},
						},
					},
				},
			},
			{
				Method:      "DELETE",
				Path:        "/api/data/{id}",
				OperationID: "data.delete",
				PathParams: []client.Parameter{
					{Name: "id", Schema: &client.Schema{Type: "string"}, Required: true},
				},
				Responses: map[int]*client.Response{
					204: {
						Description: "No Content",
					},
				},
			},
		},
		Schemas: map[string]*client.Schema{
			"DataResponse": {
				Type: "object",
				Properties: map[string]*client.Schema{
					"data": {Type: "string"},
				},
			},
		},
	}

	config := client.DefaultConfig()
	gen := NewRESTGenerator()
	code, _ := gen.Generate(spec, config)

	// Verify return types. Referenced schemas are qualified with the `types`
	// namespace the generated file imports (`import * as types from './types'`).
	assert.Contains(t, code, "Promise<types.DataResponse>")
	assert.Contains(t, code, "Promise<void>")
	assert.Contains(t, code, "return this.request<types.DataResponse>(config)")
	assert.Contains(t, code, "await this.request(config)")
}

// TestReturnTypeCoversAll2xxAndNonJSON asserts that generateReturnType looks
// at every 2xx response (not just 200/201) and every content type (not just
// application/json): a 202 with a JSON body, a union across multiple success
// codes with different bodies, and non-JSON bodies (octet-stream -> Blob,
// text/plain -> string) must all produce a real type instead of degrading to
// `any`. It also proves the generated fetch.ts response parser agrees with a
// Blob-typed declaration by actually calling response.blob() at runtime,
// since a declared type tsc cannot check against the runtime parser would be
// a silent lie.
func TestReturnTypeCoversAll2xxAndNonJSON(t *testing.T) {
	mk := func(responses map[int]*client.Response) string {
		code, _ := NewRESTGenerator().Generate(&client.APISpec{
			Info: client.APIInfo{Title: "T", Version: "1"},
			Endpoints: []client.Endpoint{{
				Method: "GET", Path: "/x", OperationID: "x.get", Responses: responses,
			}},
			Schemas: map[string]*client.Schema{"A": {Type: "object"}, "B": {Type: "object"}},
		}, client.DefaultConfig())

		return code
	}

	// 202 with a JSON body must not degrade to any.
	code := mk(map[int]*client.Response{202: {Content: map[string]*client.MediaType{
		"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/A"}}}}})
	assert.Contains(t, code, "Promise<types.A>")

	// Two success codes with different bodies produce a union.
	code = mk(map[int]*client.Response{
		200: {Content: map[string]*client.MediaType{
			"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/A"}}}},
		201: {Content: map[string]*client.MediaType{
			"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/B"}}}},
	})
	assert.Contains(t, code, "Promise<types.A | types.B>")

	// A non-JSON body is a Blob, not any.
	code = mk(map[int]*client.Response{200: {Content: map[string]*client.MediaType{
		"application/octet-stream": {Schema: &client.Schema{Type: "string", Format: "binary"}}}}})
	assert.Contains(t, code, "Promise<Blob>")

	// text/plain is a string.
	code = mk(map[int]*client.Response{200: {Content: map[string]*client.MediaType{
		"text/plain": {Schema: &client.Schema{Type: "string"}}}}})
	assert.Contains(t, code, "Promise<string>")
}

// TestFetchTsAgreesWithBlobReturnType is the runtime half of the above: a
// declared Promise<Blob> return type is only honest if the generated
// fetch.ts actually calls response.blob() for a non-JSON, non-text body.
// Without this, tsc would happily accept the declared Blob type while the
// runtime handed back a string from response.text(), a mismatch tsc cannot
// catch because it never executes the generated code.
func TestFetchTsAgreesWithBlobReturnType(t *testing.T) {
	spec := baseSpec()
	spec.Endpoints = append(spec.Endpoints, client.Endpoint{
		Method: "GET", Path: "/download", OperationID: "download.get",
		Responses: map[int]*client.Response{200: {Content: map[string]*client.MediaType{
			"application/octet-stream": {Schema: &client.Schema{Type: "string", Format: "binary"}}}}},
	})

	out, err := NewGenerator().Generate(context.Background(), spec, baseConfig())
	require.NoError(t, err)

	rest := out.Files["src/rest.ts"]
	assert.Contains(t, rest, "Promise<Blob>")

	fetchTS := out.Files["src/fetch.ts"]
	assert.Contains(t, fetchTS, ".blob()", "fetch.ts must call response.blob() so a declared Blob return type is not a lie tsc cannot catch")

	dir := t.TempDir()
	writeTree(t, dir, out.Files)

	errs := typeCheck(t, dir)
	assert.Empty(t, errs, "a client with a Blob-returning endpoint must still type-check cleanly")
}

// TestEmptyBodyResponseResolvesToUndefined is the runtime proof for the
// critical fix-round-1 defect: an endpoint with a 200 (real body) and a 202
// (no content) declares Promise<types.User | void> (per generateReturnType's
// union logic), but before this fix the generated fetch.ts's executeRequest
// never actually produced `undefined` for a genuinely empty response body —
// a 204 hit the `{} as T` special case (an always-truthy empty object, not
// `void`/undefined), and anything else (including a bare 202 with no
// Content-Type, exactly what review reproduced) fell through content-type
// branching all the way to `response.blob()` (an always-truthy Blob). Either
// way, `if (result) { result.id }` — code the declared union type explicitly
// invites — would silently misbehave: the guard always passes, and `.id` is
// undefined at runtime despite compiling cleanly.
//
// tsc cannot catch this class of bug because it never executes anything, so
// this test actually bundles the generated client with esbuild and runs it
// under Node against a mocked global fetch, exactly as review did: once
// against a real 200 JSON body (the already-covered path, checked for
// regression) and once against a 202 with a null body and no headers at all
// (the exact shape from review's reproduction).
func TestEmptyBodyResponseResolvesToUndefined(t *testing.T) {
	spec := baseSpec()
	spec.Endpoints = append(spec.Endpoints, client.Endpoint{
		Method: "GET", Path: "/mixed", OperationID: "mixed.get",
		Responses: map[int]*client.Response{
			200: {Content: map[string]*client.MediaType{
				"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/User"}}}},
			202: {}, // no content
		},
	})

	out, err := NewGenerator().Generate(context.Background(), spec, baseConfig())
	require.NoError(t, err)

	rest := out.Files["src/rest.ts"]
	require.Contains(t, rest, "Promise<types.User | void>",
		"sanity check: the declared union must include void, or this test is not exercising the reported defect")

	dir := t.TempDir()
	writeTree(t, dir, out.Files)

	driver := `
import { RESTClient } from './rest';

async function main() {
  const client: any = new RESTClient({ baseURL: 'http://example.invalid' });

  // 200 with a real JSON body: the already-covered path, checked here only
  // to prove the fix didn't break it while making empty bodies honest.
  (globalThis as any).fetch = async () => new Response(JSON.stringify({ id: 'abc' }), {
    status: 200,
    headers: { 'content-type': 'application/json' },
  });
  const withBody = await client.mixed.get();

  // 202 with a null body and NO headers at all — no Content-Type, no
  // Content-Length. This is the exact shape review reproduced: an empty ack
  // with nothing to say about what it is.
  (globalThis as any).fetch = async () => new Response(null, { status: 202 });
  const empty = await client.mixed.get();

  let emptyDescription: string;
  if (empty === undefined) {
    emptyDescription = 'undefined';
  } else if (typeof Blob !== 'undefined' && empty instanceof Blob) {
    emptyDescription = 'Blob';
  } else {
    emptyDescription = 'other:' + JSON.stringify(empty);
  }

  console.log(JSON.stringify({
    withBodyId: withBody ? withBody.id : null,
    emptyDescription,
  }));
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
`
	writeTree(t, dir, map[string]string{"src/__driver.ts": driver})

	stdout := runNodeDriver(t, dir, "src/__driver.ts")

	var result struct {
		WithBodyID       string `json:"withBodyId"`
		EmptyDescription string `json:"emptyDescription"`
	}

	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(lastLine(stdout))), &result), "driver stdout:\n%s", stdout)

	assert.Equal(t, "abc", result.WithBodyID, "the ordinary 200-with-body path must still resolve to the real value")
	assert.Equal(t, "undefined", result.EmptyDescription,
		"an empty-bodied 2xx response must resolve to undefined, not {} (old 204 special case) or a Blob (old fallthrough) — either is an always-truthy value the declared void member did not promise")
}

// lastLine returns the final non-empty line of s, so a driver script's
// diagnostic console.log/console.error noise (if any slips through despite
// the driver only emitting one JSON line) doesn't break json.Unmarshal.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return lines[i]
		}
	}

	return ""
}

// TestEmptyBodyConversionIsGatedBySpec is the runtime proof for the
// fix-round-2 defect: round 1 made executeRequest convert ANY empty body to
// `undefined` unconditionally (`blob.size === 0`), regardless of whether the
// endpoint's declared return type had a `void` member at all. That silently
// corrupted legitimate empty payloads for endpoints that never declared a
// no-content 2xx: an empty `text/plain` body (a valid empty string) and a
// zero-byte binary body (a valid empty file) both became `undefined`, and an
// empty JSON body — which should throw a parse error — silently "succeeded"
// with `undefined` instead.
//
// The fix threads the spec's knowledge through a new `RequestConfig.
// allowEmptyBody` flag, set by generateMethodBody exactly when
// generateReturnType found a `void` member in the endpoint's union, and
// executeRequest only treats a zero-byte body as `undefined` when that flag
// is set. This test proves both directions, and the two status-only cases,
// by executing the actual generated client under Node with a mocked global
// fetch — the only way to see the runtime value, since tsc never executes
// anything:
//
//  1. WITH a no-content 2xx (users.get: 200 body + 202 none) -> an empty 202
//     still resolves to undefined (round-1 behavior preserved).
//  2. WITHOUT one, Promise<string> (texts.get, text/plain only) -> an empty
//     body resolves to "", not undefined.
//  3. WITHOUT one, Promise<Blob> (downloads.get, octet-stream only) -> a
//     zero-byte body resolves to a real zero-size Blob, not undefined.
//  4. WITHOUT one, Promise<types.User> (users.create, JSON only) -> an empty
//     body throws (JSON.parse on an empty string throws SyntaxError), not undefined.
//  5. Status-based 204/205 -> undefined unconditionally, flag or not.
func TestEmptyBodyConversionIsGatedBySpec(t *testing.T) {
	spec := baseSpec()
	spec.Endpoints = append(spec.Endpoints,
		client.Endpoint{
			Method: "GET", Path: "/empty204", OperationID: "empty204.get",
			Responses: map[int]*client.Response{204: {}},
		},
		client.Endpoint{
			Method: "GET", Path: "/empty205", OperationID: "empty205.get",
			Responses: map[int]*client.Response{205: {}},
		},
	)

	out, err := NewGenerator().Generate(context.Background(), spec, baseConfig())
	require.NoError(t, err)

	rest := out.Files["src/rest.ts"]
	// Sanity checks: confirm the declared types this test's conclusions rest
	// on are actually what's generated, before trusting the runtime results.
	require.Contains(t, rest, "Promise<types.User | void>", "users.get must declare the mixed union for case 1 to be meaningful")
	require.Contains(t, rest, "Promise<string>", "texts.get must declare a bare string type (no void) for case 2 to be meaningful")
	require.Contains(t, rest, "Promise<Blob>", "downloads.get must declare a bare Blob type (no void) for case 3 to be meaningful")

	fetchTS := out.Files["src/fetch.ts"]
	require.Contains(t, fetchTS, "allowEmptyBody", "fetch.ts must reference the allowEmptyBody gate for this test to be meaningful")

	dir := t.TempDir()
	writeTree(t, dir, out.Files)

	driver := `
import { RESTClient } from './rest';

function setFetch(status: number, body: string | null, headers: Record<string, string>) {
  (globalThis as any).fetch = async () => new Response(body, { status, headers });
}

function describe(v: unknown): string {
  if (v === undefined) return 'undefined';
  if (typeof Blob !== 'undefined' && v instanceof Blob) return 'Blob(size=' + (v as Blob).size + ')';
  return JSON.stringify(v);
}

async function main() {
  const client: any = new RESTClient({ baseURL: 'http://example.invalid' });
  const results: Record<string, string> = {};

  // 1. WITH a no-content 2xx: an empty 202 must still resolve to undefined.
  setFetch(202, null, {});
  results.withVoidEmpty = describe(await client.users.get('u1'));

  // 2. WITHOUT one, Promise<string>: an empty text/plain body is legitimate
  //    data, not void.
  setFetch(200, '', { 'content-type': 'text/plain' });
  results.textEmpty = describe(await client.texts.get());

  // 3. WITHOUT one, Promise<Blob>: a zero-byte binary body is a legitimate
  //    (empty) file, not void.
  setFetch(200, '', { 'content-type': 'application/octet-stream' });
  results.binaryEmpty = describe(await client.downloads.get());

  // 4. WITHOUT one, Promise<types.User>: an empty JSON body is a genuine
  //    parse error, not a legitimate value.
  setFetch(200, '', { 'content-type': 'application/json' });
  try {
    await client.users.create({ id: 'x' });
    results.jsonEmpty = 'did-not-throw';
  } catch (err) {
    results.jsonEmpty = 'threw:' + (err instanceof Error ? err.constructor.name : typeof err);
  }

  // 5. Status-based no-body responses: undefined regardless of the flag.
  setFetch(204, null, {});
  results.status204 = describe(await client.empty204.get());
  setFetch(205, null, {});
  results.status205 = describe(await client.empty205.get());

  console.log(JSON.stringify(results));
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
`
	writeTree(t, dir, map[string]string{"src/__driver2.ts": driver})

	stdout := runNodeDriver(t, dir, "src/__driver2.ts")

	var results map[string]string

	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(lastLine(stdout))), &results), "driver stdout:\n%s", stdout)

	assert.Equal(t, "undefined", results["withVoidEmpty"],
		"case 1: an endpoint that declares a no-content 2xx must still resolve an empty body to undefined")
	assert.Equal(t, `""`, results["textEmpty"],
		"case 2: an endpoint with no no-content 2xx must resolve an empty text/plain body to the empty string, not undefined")
	assert.Equal(t, "Blob(size=0)", results["binaryEmpty"],
		"case 3: an endpoint with no no-content 2xx must resolve a zero-byte binary body to a zero-byte Blob, not undefined")
	assert.Contains(t, results["jsonEmpty"], "threw:",
		"case 4: an endpoint with no no-content 2xx must let an empty JSON body throw, not silently resolve to undefined")
	assert.Equal(t, "undefined", results["status204"], "case 5a: 204 must resolve to undefined regardless of allowEmptyBody")
	assert.Equal(t, "undefined", results["status205"], "case 5b: 205 must resolve to undefined regardless of allowEmptyBody")
}

func TestEndpointTreeKeepsBothOrders(t *testing.T) {
	// Reuses the same two operation IDs as TestRESTGenerator_ConflictingOperationIDs
	// ("users" and "users.active.list") but exercises both insertion orders.
	//
	// "users" first, then "users.active.list" (leaf-then-branch) is the order
	// already covered by TestRESTGenerator_ConflictingOperationIDs and is the
	// direction insertIntoTree already handled: the leaf gets converted into a
	// branch and the original method is re-inserted inside it.
	//
	// "users.active.list" first, then "users" (branch-then-leaf) is the
	// direction that was broken: the leaf-insertion case at len(parts) == 1
	// unconditionally overwrote node.Children["users"], discarding the
	// "active.list" branch built by the first endpoint entirely.
	//
	// Since spec.Endpoints ordering is not guaranteed (map iteration during
	// spec construction, declaration order, etc.), the tree - and therefore
	// the generated code - must not depend on which order the endpoints were
	// inserted in. This test asserts that directly by comparing the two
	// outputs for byte-for-byte equality, rather than just checking that both
	// pieces of the tree happen to be present.
	mk := func(opID string) client.Endpoint {
		return client.Endpoint{
			Method: "GET", Path: "/x", OperationID: opID,
			Responses: map[int]*client.Response{204: {Description: "ok"}},
		}
	}

	gen := NewRESTGenerator()
	config := client.DefaultConfig()

	branchThenLeaf, _ := gen.Generate(&client.APISpec{
		Info:      client.APIInfo{Title: "T", Version: "1"},
		Endpoints: []client.Endpoint{mk("users.active.list"), mk("users")},
	}, config)

	leafThenBranch, _ := gen.Generate(&client.APISpec{
		Info:      client.APIInfo{Title: "T", Version: "1"},
		Endpoints: []client.Endpoint{mk("users"), mk("users.active.list")},
	}, config)

	for name, code := range map[string]string{"branch-then-leaf": branchThenLeaf, "leaf-then-branch": leafThenBranch} {
		assert.Contains(t, code, "active: {", "%s: nested namespace must survive", name)
		assert.Contains(t, code, "list: async (", "%s: sibling method under the namespace must survive", name)
		assert.Contains(t, code, "users: async (", "%s: the single-segment 'users' method must survive", name)
	}

	assert.Equal(t, leafThenBranch, branchThenLeaf, "tree shape (and generated code) must not depend on insertion order")
}

func TestPathParamsAreURLEncoded(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{Title: "T", Version: "1"},
		Endpoints: []client.Endpoint{{
			Method:      "GET",
			Path:        "/files/{path}",
			OperationID: "files.get",
			PathParams:  []client.Parameter{{Name: "path", Schema: &client.Schema{Type: "string"}, Required: true}},
			Responses:   map[int]*client.Response{204: {Description: "ok"}},
		}},
	}

	code, _ := NewRESTGenerator().Generate(spec, client.DefaultConfig())

	assert.Contains(t, code, "${encodeURIComponent(String(path))}")
}

// TestNonJSONRequestBodies is task 8's failing-test-first proof for the
// defect hasBodyParam had: it accepted only "application/json", so a
// multipart, binary, or plain-text request body generated a method with NO
// body parameter at all — the request was silently sent empty, with no
// compile error and no runtime error, because the shorthand `body,` in
// generateMethodBody's config object was never emitted either (see
// generateMethodBody's `if r.hasBodyParam(endpoint)` guard).
//
// Mirrors responseBodyType's content-type precedence (application/json
// first, then text/*, then anything else) so a request body picks the same
// bucket a response would for the same declared content types. The
// TypeScript parameter type per bucket: application/json -> the schema type,
// multipart/form-data -> FormData (the DOM type a caller builds an upload
// with), text/* -> string, anything else (e.g. application/octet-stream) ->
// Blob (the DOM type for an opaque byte payload).
func TestNonJSONRequestBodies(t *testing.T) {
	mk := func(contentType string, schema *client.Schema) string {
		code, _ := NewRESTGenerator().Generate(&client.APISpec{
			Info: client.APIInfo{Title: "T", Version: "1"},
			Endpoints: []client.Endpoint{{
				Method: "POST", Path: "/up", OperationID: "up.post",
				RequestBody: &client.RequestBody{Required: true, Content: map[string]*client.MediaType{
					contentType: {Schema: schema}}},
				Responses: map[int]*client.Response{204: {Description: "ok"}},
			}},
			Schemas: map[string]*client.Schema{},
		}, client.DefaultConfig())

		return code
	}

	code := mk("multipart/form-data", &client.Schema{Type: "object"})
	assert.Contains(t, code, "body: FormData", "multipart/form-data body must be typed as FormData")
	assert.Contains(t, code, "body,", "the body parameter must actually be forwarded into the request config")

	code = mk("application/octet-stream", &client.Schema{Type: "string", Format: "binary"})
	assert.Contains(t, code, "body: Blob", "an unrecognised binary content type must fall back to Blob")

	code = mk("text/plain", &client.Schema{Type: "string"})
	assert.Contains(t, code, "body: string", "text/* must be typed as string")
}

// TestRequestBodyContentTypePrecedenceMatchesResponse asserts that when an
// endpoint declares multiple request-body content types (unusual, but the IR
// allows it — RequestBody.Content is a map), the SAME endpoint always picks
// exactly one, using the same precedence responseBodyType already
// established for responses: application/json wins over everything, then any
// text/* media type, then the remainder. This keeps the two "which content
// type wins" decisions in the generator consistent instead of diverging.
func TestRequestBodyContentTypePrecedenceMatchesResponse(t *testing.T) {
	mk := func(content map[string]*client.MediaType) string {
		code, _ := NewRESTGenerator().Generate(&client.APISpec{
			Info: client.APIInfo{Title: "T", Version: "1"},
			Endpoints: []client.Endpoint{{
				Method: "POST", Path: "/up", OperationID: "up.post",
				RequestBody: &client.RequestBody{Required: true, Content: content},
				Responses:   map[int]*client.Response{204: {Description: "ok"}},
			}},
			Schemas: map[string]*client.Schema{"User": {Type: "object"}},
		}, client.DefaultConfig())

		return code
	}

	// application/json beats both text/plain and multipart/form-data when all
	// three are declared on the same request body.
	code := mk(map[string]*client.MediaType{
		"application/json":    {Schema: &client.Schema{Ref: "#/components/schemas/User"}},
		"text/plain":          {Schema: &client.Schema{Type: "string"}},
		"multipart/form-data": {Schema: &client.Schema{Type: "object"}},
	})
	assert.Contains(t, code, "body: types.User")
	assert.NotContains(t, code, "body: FormData")
	assert.NotContains(t, code, "body?: string")

	// Without JSON, text/* beats the remaining generic bucket.
	code = mk(map[string]*client.MediaType{
		"text/plain":               {Schema: &client.Schema{Type: "string"}},
		"application/octet-stream": {Schema: &client.Schema{Type: "string", Format: "binary"}},
	})
	assert.Contains(t, code, "body: string")

	// A SCHEMALESS application/json entry must not win, in the first branch
	// or in the sorted fallback. "application/json" sorts before both
	// "multipart/form-data" and "application/octet-stream", so a naive
	// `return keys[0]` fallback re-selects it and maps it to `any` via
	// getSchemaTypeName(nil) — silently erasing a usable sibling body.
	// `content: {application/json: {}}` is legal OpenAPI.
	code = mk(map[string]*client.MediaType{
		"application/json":    {},
		"multipart/form-data": {Schema: &client.Schema{Type: "object"}},
	})
	assert.Contains(t, code, "body: FormData")
	assert.NotContains(t, code, "body: any")

	code = mk(map[string]*client.MediaType{
		"application/json":         {},
		"application/octet-stream": {Schema: &client.Schema{Type: "string", Format: "binary"}},
	})
	assert.Contains(t, code, "body: Blob")
	assert.NotContains(t, code, "body: any")

	// Schemaless JSON as the ONLY content type still yields a body param —
	// there is nothing better to fall back to.
	code = mk(map[string]*client.MediaType{"application/json": {}})
	assert.Contains(t, code, "body: any")

	// application/x-www-form-urlencoded maps to URLSearchParams, the native
	// BodyInit for it, not to the generic Blob bucket.
	code = mk(map[string]*client.MediaType{
		"application/x-www-form-urlencoded": {Schema: &client.Schema{Type: "object"}},
	})
	assert.Contains(t, code, "body: URLSearchParams")
}

// TestNoRequestBodyStillGeneratesNoBodyParam is the negative control for
// hasBodyParam's generalisation: an endpoint with no RequestBody at all must
// still generate no `body` parameter and no `body,` forwarding line, exactly
// as before this task.
func TestNoRequestBodyStillGeneratesNoBodyParam(t *testing.T) {
	code, _ := NewRESTGenerator().Generate(&client.APISpec{
		Info: client.APIInfo{Title: "T", Version: "1"},
		Endpoints: []client.Endpoint{{
			Method: "GET", Path: "/noop", OperationID: "noop.get",
			Responses: map[int]*client.Response{204: {Description: "ok"}},
		}},
	}, client.DefaultConfig())

	assert.NotContains(t, code, "body:")
	assert.NotContains(t, code, "body,")
	assert.NotContains(t, code, "body?")
}

// requestConfigBlock extracts one generated method's `const config:
// RequestConfig = { ... };` literal, isolated via the method's distinctive
// `let __path = ...;` line (every method builds `url: __path,` identically,
// so that alone can't disambiguate one method from another — the path
// template is what's unique per endpoint). Brace-counting rather than a fixed
// indentation offset keeps this correct regardless of how deeply the
// endpoint's operation ID nests it in the generated tree.
func requestConfigBlock(t *testing.T, code, pathMarker string) string {
	t.Helper()

	idx := strings.Index(code, pathMarker)
	require.NotEqual(t, -1, idx, "marker %q not found in generated code:\n%s", pathMarker, code)

	tail := code[idx:]
	start := strings.Index(tail, "const config: RequestConfig = {")
	require.NotEqual(t, -1, start, "no RequestConfig literal found after %q", pathMarker)

	tail = tail[start:]
	braceStart := strings.Index(tail, "{")
	require.NotEqual(t, -1, braceStart)

	depth := 0
	for i := braceStart; i < len(tail); i++ {
		switch tail[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return tail[:i+1]
			}
		}
	}

	t.Fatalf("unbalanced braces scanning RequestConfig literal after %q", pathMarker)

	return ""
}

// TestRestPopulatesCodecRefsFromStaticSchemaIDs is the string-level proof that
// rest.ts populates RequestConfig.bodyCodec/responseCodec from statically
// known schema ids at each call site — only when the endpoint's JSON body or
// response resolves to a NAMED component schema (a direct $ref), since that is
// the only shape the codec table (codecs.go) ever registers an entry for.
func TestRestPopulatesCodecRefsFromStaticSchemaIDs(t *testing.T) {
	spec := baseSpec()
	code, _ := NewRESTGenerator().Generate(spec, baseConfig())

	// users.create (POST /users): required $ref-User request body, 201 $ref
	// User response — both fields populated from the same schema id.
	usersCreate := requestConfigBlock(t, code, "let __path = `/users`;\n")
	assert.Contains(t, usersCreate, `bodyCodec: CODECS["User"]`,
		"users.create's request body is $ref User; bodyCodec must reference it")
	assert.Contains(t, usersCreate, `responseCodec: CODECS["User"]`,
		"users.create's 201 response is $ref User; responseCodec must reference it")

	// users.get (GET /users/{id}): no request body at all, and a 2xx set of
	// {200: $ref User, 202: no content} — the 202 contributes nothing, so the
	// single named ref among the JSON responses still wins.
	usersGet := requestConfigBlock(t, code, "let __path = `/users/${encodeURIComponent(String(id))}`;\n")
	assert.NotContains(t, usersGet, "bodyCodec", "users.get has no request body")
	assert.Contains(t, usersGet, `responseCodec: CODECS["User"]`,
		"users.get's 200 response is $ref User; responseCodec must reference it")

	// texts.get (GET /text): text/plain response only — not JSON, so neither
	// codec field is meaningful.
	textsGet := requestConfigBlock(t, code, "let __path = `/text`;\n")
	assert.NotContains(t, textsGet, "Codec", "texts.get's response is text/plain, not JSON")

	// downloads.get (GET /download): application/octet-stream response only —
	// not JSON either.
	downloadsGet := requestConfigBlock(t, code, "let __path = `/download`;\n")
	assert.NotContains(t, downloadsGet, "Codec", "downloads.get's response is application/octet-stream, not JSON")

	// uploads.create (POST /uploads): multipart/form-data request body (not
	// JSON, so no bodyCodec) but a real $ref-User JSON 201 response (so
	// responseCodec must still apply).
	uploadsCreate := requestConfigBlock(t, code, "let __path = `/uploads`;\n")
	assert.NotContains(t, uploadsCreate, "bodyCodec", "uploads.create's request body is multipart/form-data, not JSON")
	assert.Contains(t, uploadsCreate, `responseCodec: CODECS["User"]`, "uploads.create's 201 response is still $ref User")

	// raw.create (POST /raw): application/octet-stream request body (not
	// JSON) and a 204 (no content) response — neither field applies.
	rawCreate := requestConfigBlock(t, code, "let __path = `/raw`;\n")
	assert.NotContains(t, rawCreate, "bodyCodec", "raw.create's request body is application/octet-stream, not JSON")
	assert.NotContains(t, rawCreate, "responseCodec", "raw.create's response is 204 (no content)")
}

// TestRestResponseCodecRefOmittedWhenAmbiguousAcrossStatuses asserts the
// conservative choice for an endpoint whose 2xx set resolves to two DIFFERENT
// named JSON schemas (e.g. 200 -> User, 201 -> Team): there is no single
// codec id correct for every status this call could resolve to, so
// responseCodec is left unset entirely rather than guessing one of them (which
// would silently mis-rename the other status's shape).
//
// Fix-round-1 review (IMPORTANT 2): leaving responseCodec unset here is only
// honest if it's ALSO surfaced — the method still declares
// Promise<types.User | types.Team>, a camelCase-typed value that will never
// actually be renamed, and silence would just move the lie from "wrong
// rename" to "quietly no rename at all". RESTGenerator.Generate now returns
// (string, []string) — mirroring CodecGenerator's own warnings channel — and
// must name the endpoint and the reason here.
func TestRestResponseCodecRefOmittedWhenAmbiguousAcrossStatuses(t *testing.T) {
	spec := baseSpec()
	spec.Schemas["Team"] = &client.Schema{Type: "object", Properties: map[string]*client.Schema{"name": {Type: "string"}}}
	spec.Endpoints = append(spec.Endpoints, client.Endpoint{
		Method: "POST", Path: "/ambiguous", OperationID: "ambiguous.create",
		Responses: map[int]*client.Response{
			200: {Content: map[string]*client.MediaType{"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/User"}}}},
			201: {Content: map[string]*client.MediaType{"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/Team"}}}},
		},
	})

	code, warnings := NewRESTGenerator().Generate(spec, baseConfig())
	block := requestConfigBlock(t, code, "let __path = `/ambiguous`;\n")
	assert.NotContains(t, block, "responseCodec",
		"two different named schemas across the 2xx set have no single codec id correct for every status this call could resolve")

	require.Len(t, warnings, 1, "the skip must be surfaced as a warning, not silent: %v", warnings)
	assert.Contains(t, warnings[0], "ambiguous.create", "the warning must name the endpoint it applies to")
}

// TestRestResponseCodecRefOmittedForInlineJSONSchema asserts that an inline
// (non-$ref) JSON response schema never gets a responseCodec: the codec table
// (codecs.go) only registers entries reachable by walking spec.Schemas, keyed
// by named component schema — an inline schema at an endpoint boundary has no
// such entry, so referencing it would hand decode() a dangling id.
//
// Fix-round-1 review (IMPORTANT 2): same reasoning as the ambiguous-status
// case above — the skip must be a visible warning, not silence, since
// generateReturnType still declares a camelCase-typed return value that will
// never be decoded.
func TestRestResponseCodecRefOmittedForInlineJSONSchema(t *testing.T) {
	spec := baseSpec()
	spec.Endpoints = append(spec.Endpoints, client.Endpoint{
		Method: "GET", Path: "/inline", OperationID: "inline.get",
		Responses: map[int]*client.Response{
			200: {Content: map[string]*client.MediaType{"application/json": {Schema: &client.Schema{
				Type: "object", Properties: map[string]*client.Schema{"x": {Type: "string"}},
			}}}},
		},
	})

	code, warnings := NewRESTGenerator().Generate(spec, baseConfig())
	block := requestConfigBlock(t, code, "let __path = `/inline`;\n")
	assert.NotContains(t, block, "responseCodec",
		"an inline JSON response schema has no codec-table entry; referencing it would be a dangling id")

	require.Len(t, warnings, 1, "the skip must be surfaced as a warning, not silent: %v", warnings)
	assert.Contains(t, warnings[0], "inline.get", "the warning must name the endpoint it applies to")
}

// TestRestCodecRefsAndWarningsSuppressedUnderPreserve is Task 7's REST-side
// negative control: under NamingPreserve (no FieldOverrides), rest.go must
// neither emit bodyCodec/responseCodec refs (codecsNeeded(config) == false)
// nor surface the unresolvable-codec-ref warning that fires for this exact
// same inline-schema shape under camel (see
// TestRestResponseCodecRefOmittedForInlineJSONSchema, just above) --
// that warning is about renaming machinery that isn't running at all under
// preserve, so surfacing it would just be confusing a caller who opted out
// of renaming entirely.
func TestRestCodecRefsAndWarningsSuppressedUnderPreserve(t *testing.T) {
	spec := baseSpec()
	spec.Endpoints = append(spec.Endpoints, client.Endpoint{
		Method: "GET", Path: "/inline", OperationID: "inline.get",
		Responses: map[int]*client.Response{
			200: {Content: map[string]*client.MediaType{"application/json": {Schema: &client.Schema{
				Type: "object", Properties: map[string]*client.Schema{"x": {Type: "string"}},
			}}}},
		},
	})

	code, warnings := NewRESTGenerator().Generate(spec, preserveConfig())

	assert.NotContains(t, code, "bodyCodec", "no codec table exists under preserve; rest.ts must not reference one")
	assert.NotContains(t, code, "responseCodec", "no codec table exists under preserve; rest.ts must not reference one")
	assert.Empty(t, warnings, "an unresolvable codec ref is meaningless under preserve: nothing is being renamed")
}

// TestRESTClientRoundTripsBodyAndResponseCodecsAtRuntime is the full,
// generated-client execution proof: a nested object, an array of objects, and
// a record all round-trip correctly through both encode (request) and decode
// (response) at every nesting level, and an unknown key at both the top level
// and inside a nested object survives untouched in both directions. This
// drives the REAL generated RESTClient (rest.ts + fetch.ts + codecs.ts +
// client.ts + types.ts all wired together), not a hand-built RequestConfig —
// the thing task 6 is actually about is rest.ts populating bodyCodec/
// responseCodec correctly AND fetch.ts's executeRequest applying them, and
// only the full generated tree exercises both halves together.
func TestRESTClientRoundTripsBodyAndResponseCodecsAtRuntime(t *testing.T) {
	spec := nestedSpec()
	spec.Endpoints = append(spec.Endpoints, client.Endpoint{
		Method: "POST", Path: "/nested", OperationID: "nested.create",
		RequestBody: &client.RequestBody{Required: true, Content: map[string]*client.MediaType{
			"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/Nested"}}}},
		Responses: map[int]*client.Response{201: {Content: map[string]*client.MediaType{
			"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/Nested"}}}}},
	})

	out, err := NewGenerator().Generate(context.Background(), spec, baseConfig())
	require.NoError(t, err)

	rest := out.Files["src/rest.ts"]
	require.Contains(t, rest, `bodyCodec: CODECS["Nested"]`,
		"sanity check: nested.create's request body is $ref Nested, or this test is not exercising the codec path at all")
	require.Contains(t, rest, `responseCodec: CODECS["Nested"]`,
		"sanity check: nested.create's response is $ref Nested, or this test is not exercising the codec path at all")

	dir := t.TempDir()
	writeTree(t, dir, out.Files)

	driver := `
import { RESTClient } from './rest';

async function main() {
  const client: any = new RESTClient({ baseURL: 'http://example.invalid' });

  let captured: any;
  (globalThis as any).fetch = async (_url: string, init: any) => {
    captured = init;
    const wireResponse = {
      user: { id: 'u2', user_id: 'w2', created_at: '2020-01-02', extra_user_field: 'keep-me' },
      items: [{ id: 'u3', user_id: 'w3', created_at: '2020-01-03' }],
      tags: { alpha: 'one', beta: 'two' },
      extra_top_level: 'also-keep',
    };
    return new Response(JSON.stringify(wireResponse), {
      status: 201,
      headers: { 'content-type': 'application/json' },
    });
  };

  const result = await client.nested.create({
    user: { id: 'u1', userId: 'w1', createdAt: '2020-01-01' },
    items: [{ id: 'u1b', userId: 'w1b', createdAt: '2020-01-01b' }],
    tags: { alpha: 'one', beta: 'two' },
  });

  console.log(JSON.stringify({ sentBody: JSON.parse(captured.body), result }));
}

main().catch((err) => { console.error(err); process.exit(1); });
`
	writeTree(t, dir, map[string]string{"src/__driver_codec_roundtrip.ts": driver})

	stdout := runNodeDriver(t, dir, "src/__driver_codec_roundtrip.ts")

	var result struct {
		SentBody map[string]any `json:"sentBody"`
		Result   map[string]any `json:"result"`
	}
	decodeLastLine(t, stdout, &result)

	// --- encode direction: camelCase -> wire, at every nesting level.
	sentUser, ok := result.SentBody["user"].(map[string]any)
	require.True(t, ok, "driver stdout:\n%s", stdout)
	assert.Equal(t, "w1", sentUser["user_id"], "nested object: userId must encode to user_id on the wire")

	sentItems, ok := result.SentBody["items"].([]any)
	require.True(t, ok && len(sentItems) == 1, "driver stdout:\n%s", stdout)
	sentItem0, ok := sentItems[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "w1b", sentItem0["user_id"], "array of objects: each item's userId must encode to user_id")

	sentTags, ok := result.SentBody["tags"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "one", sentTags["alpha"], "record: keys are data and must stay untouched")
	assert.Equal(t, "two", sentTags["beta"], "record: keys are data and must stay untouched")

	// --- decode direction: wire -> camelCase, at every nesting level, plus
	// unknown-key survival at the top level and inside a nested object.
	resultUser, ok := result.Result["user"].(map[string]any)
	require.True(t, ok, "driver stdout:\n%s", stdout)
	assert.Equal(t, "w2", resultUser["userId"], "nested object: user_id must decode to userId")
	assert.Equal(t, "keep-me", resultUser["extra_user_field"],
		"an unknown key inside a nested object must pass through verbatim, not be renamed or dropped")

	resultItems, ok := result.Result["items"].([]any)
	require.True(t, ok && len(resultItems) == 1, "driver stdout:\n%s", stdout)
	resultItem0, ok := resultItems[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "w3", resultItem0["userId"], "array of objects: each item's user_id must decode to userId")

	resultTags, ok := result.Result["tags"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "one", resultTags["alpha"])
	assert.Equal(t, "two", resultTags["beta"])

	assert.Equal(t, "also-keep", result.Result["extra_top_level"], "a top-level unknown key must pass through verbatim")
}
