package typescript

import (
	"context"
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
	code := gen.Generate(spec, config)

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
	code := gen.Generate(spec, config)

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
	code := gen.Generate(spec, config)

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
	code := gen.Generate(spec, config)

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
	code := gen.Generate(spec, config)

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
	code := gen.Generate(spec, config)

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
	code := gen.Generate(spec, config)

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
	code1 := gen.Generate(spec, config)
	code2 := gen.Generate(spec, config)

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
	code := gen.Generate(spec, config)

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
	code := gen.Generate(spec, config)

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
	code := gen.Generate(spec, config)

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
		return NewRESTGenerator().Generate(&client.APISpec{
			Info: client.APIInfo{Title: "T", Version: "1"},
			Endpoints: []client.Endpoint{{
				Method: "GET", Path: "/x", OperationID: "x.get", Responses: responses,
			}},
			Schemas: map[string]*client.Schema{"A": {Type: "object"}, "B": {Type: "object"}},
		}, client.DefaultConfig())
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

	branchThenLeaf := gen.Generate(&client.APISpec{
		Info:      client.APIInfo{Title: "T", Version: "1"},
		Endpoints: []client.Endpoint{mk("users.active.list"), mk("users")},
	}, config)

	leafThenBranch := gen.Generate(&client.APISpec{
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
			PathParams: []client.Parameter{{Name: "path", Schema: &client.Schema{Type: "string"}, Required: true}},
			Responses:  map[int]*client.Response{204: {Description: "ok"}},
		}},
	}

	code := NewRESTGenerator().Generate(spec, client.DefaultConfig())

	assert.Contains(t, code, "${encodeURIComponent(String(path))}")
}
