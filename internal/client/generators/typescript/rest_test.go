package typescript

import (
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
	assert.Contains(t, code, "workspaceid: string")
	assert.Contains(t, code, "connectionid: string")
	assert.Contains(t, code, "externalid: string")
	assert.Contains(t, code, "startdate?: string | undefined")
	assert.Contains(t, code, "enddate?: string | undefined")

	// Verify path template
	assert.Contains(t, code, "/api/workspaces/${workspaceid}/connections/${connectionid}/billing/users/${externalid}")

	// Verify query params handling
	assert.Contains(t, code, "queryParams: Record<string, any> = {}")
	assert.Contains(t, code, "if (startdate !== undefined)")
	assert.Contains(t, code, "if (enddate !== undefined)")
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

	// Verify return types
	assert.Contains(t, code, "Promise<DataResponse>")
	assert.Contains(t, code, "Promise<void>")
	assert.Contains(t, code, "return this.request<DataResponse>(config)")
	assert.Contains(t, code, "await this.request(config)")
}

