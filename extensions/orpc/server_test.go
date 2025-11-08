package orpc

import (
	"context"
	"testing"

	"github.com/xraph/forge"
)

func TestServer_RegisterMethod(t *testing.T) {
	server := NewORPCServer(DefaultConfig(), forge.NewNoopLogger(), forge.NewNoOpMetrics())

	method := &Method{
		Name:        "test.method",
		Description: "Test method",
		Handler: func(ctx any, params any) (any, error) {
			return "result", nil
		},
	}

	// Register method
	if err := server.RegisterMethod(method); err != nil {
		t.Fatalf("failed to register method: %v", err)
	}

	// Get method
	retrieved, err := server.GetMethod("test.method")
	if err != nil {
		t.Fatalf("failed to get method: %v", err)
	}

	if retrieved.Name != "test.method" {
		t.Errorf("expected method name 'test.method', got %s", retrieved.Name)
	}

	// Overwrite method (should log warning)
	if err := server.RegisterMethod(method); err != nil {
		t.Fatalf("failed to overwrite method: %v", err)
	}
}

func TestServer_RegisterMethod_EmptyName(t *testing.T) {
	server := NewORPCServer(DefaultConfig(), forge.NewNoopLogger(), forge.NewNoOpMetrics())

	method := &Method{
		Name:    "",
		Handler: func(ctx any, params any) (any, error) { return nil, nil },
	}

	if err := server.RegisterMethod(method); !errors.Is(err, ErrInvalidMethodName) {
		t.Errorf("expected ErrInvalidMethodName, got %v", err)
	}
}

func TestServer_GetMethod_NotFound(t *testing.T) {
	server := NewORPCServer(DefaultConfig(), forge.NewNoopLogger(), forge.NewNoOpMetrics())

	_, err := server.GetMethod("nonexistent")
	if !errors.Is(err, ErrMethodNotFoundError) {
		t.Errorf("expected ErrMethodNotFoundError, got %v", err)
	}
}

func TestServer_ListMethods(t *testing.T) {
	server := NewORPCServer(DefaultConfig(), forge.NewNoopLogger(), forge.NewNoOpMetrics())

	method1 := &Method{
		Name:    "method1",
		Handler: func(ctx any, params any) (any, error) { return nil, nil },
	}
	method2 := &Method{
		Name:    "method2",
		Handler: func(ctx any, params any) (any, error) { return nil, nil },
	}

	server.RegisterMethod(method1)
	server.RegisterMethod(method2)

	methods := server.ListMethods()
	if len(methods) != 2 {
		t.Errorf("expected 2 methods, got %d", len(methods))
	}
}

func TestServer_HandleBatch(t *testing.T) {
	config := DefaultConfig()
	config.EnableBatch = true
	config.BatchLimit = 2
	server := NewORPCServer(config, forge.NewNoopLogger(), forge.NewNoOpMetrics())

	method := &Method{
		Name: "test",
		Handler: func(ctx any, params any) (any, error) {
			return "result", nil
		},
	}
	server.RegisterMethod(method)

	// Test batch within limit
	requests := []*Request{
		{JSONRPC: "2.0", Method: "test", ID: 1},
		{JSONRPC: "2.0", Method: "test", ID: 2},
	}

	responses := server.HandleBatch(context.Background(), requests)
	if len(responses) != 2 {
		t.Errorf("expected 2 responses, got %d", len(responses))
	}

	// Test batch exceeding limit
	requests = []*Request{
		{JSONRPC: "2.0", Method: "test", ID: 1},
		{JSONRPC: "2.0", Method: "test", ID: 2},
		{JSONRPC: "2.0", Method: "test", ID: 3},
	}

	responses = server.HandleBatch(context.Background(), requests)
	if len(responses) != 1 || responses[0].Error == nil {
		t.Error("expected batch limit error")
	}
}

func TestServer_HandleBatch_Disabled(t *testing.T) {
	config := DefaultConfig()
	config.EnableBatch = false
	server := NewORPCServer(config, forge.NewNoopLogger(), forge.NewNoOpMetrics())

	requests := []*Request{
		{JSONRPC: "2.0", Method: "test", ID: 1},
	}

	responses := server.HandleBatch(context.Background(), requests)
	if len(responses) != 1 || responses[0].Error == nil {
		t.Error("expected batch disabled error")
	}
}

func TestServer_HandleRequest_VersionCheck(t *testing.T) {
	server := NewORPCServer(DefaultConfig(), forge.NewNoopLogger(), forge.NewNoOpMetrics())

	req := &Request{
		JSONRPC: "1.0",
		Method:  "test",
		ID:      1,
	}

	resp := server.HandleRequest(context.Background(), req)
	if resp.Error == nil || resp.Error.Code != ErrInvalidRequest {
		t.Error("expected invalid request error for wrong version")
	}
}

func TestServer_HandleRequest_EmptyMethod(t *testing.T) {
	server := NewORPCServer(DefaultConfig(), forge.NewNoopLogger(), forge.NewNoOpMetrics())

	req := &Request{
		JSONRPC: "2.0",
		Method:  "",
		ID:      1,
	}

	resp := server.HandleRequest(context.Background(), req)
	if resp.Error == nil || resp.Error.Code != ErrInvalidRequest {
		t.Error("expected invalid request error for empty method")
	}
}

func TestServer_HandleRequest_MethodNotFound(t *testing.T) {
	server := NewORPCServer(DefaultConfig(), forge.NewNoopLogger(), forge.NewNoOpMetrics())

	req := &Request{
		JSONRPC: "2.0",
		Method:  "nonexistent",
		ID:      1,
	}

	resp := server.HandleRequest(context.Background(), req)
	if resp.Error == nil || resp.Error.Code != ErrMethodNotFound {
		t.Error("expected method not found error")
	}
}

func TestServer_Use(t *testing.T) {
	server := NewORPCServer(DefaultConfig(), forge.NewNoopLogger(), forge.NewNoOpMetrics())

	called := false
	interceptor := func(ctx context.Context, req *Request, next MethodHandler) (any, error) {
		called = true

		return next(ctx, nil)
	}

	server.Use(interceptor)

	method := &Method{
		Name: "test",
		Handler: func(ctx any, params any) (any, error) {
			return "result", nil
		},
	}
	server.RegisterMethod(method)

	req := &Request{
		JSONRPC: "2.0",
		Method:  "test",
		ID:      1,
	}

	server.HandleRequest(context.Background(), req)

	if !called {
		t.Error("interceptor was not called")
	}
}

func TestServer_GetStats(t *testing.T) {
	server := NewORPCServer(DefaultConfig(), forge.NewNoopLogger(), forge.NewNoOpMetrics())

	stats := server.GetStats()
	if stats.TotalMethods != 0 {
		t.Error("expected 0 methods initially")
	}
}

func TestServer_NamingStrategies(t *testing.T) {
	tests := []struct {
		name     string
		strategy string
		route    forge.RouteInfo
		want     string
	}{
		{
			name:     "path strategy",
			strategy: "path",
			route: forge.RouteInfo{
				Method: "GET",
				Path:   "/users/:id",
			},
			want: "get.users.id",
		},
		{
			name:     "method strategy with name",
			strategy: "method",
			route: forge.RouteInfo{
				Name:   "get-user",
				Method: "GET",
				Path:   "/users/:id",
			},
			want: "get-user",
		},
		{
			name:     "method strategy fallback",
			strategy: "method",
			route: forge.RouteInfo{
				Name:   "",
				Method: "POST",
				Path:   "/users",
			},
			want: "create.users",
		},
		{
			name:     "custom via metadata",
			strategy: "path",
			route: forge.RouteInfo{
				Method: "GET",
				Path:   "/users/:id",
				Metadata: map[string]any{
					"orpc.method": "user.get",
				},
			},
			want: "user.get",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultConfig()
			config.NamingStrategy = tt.strategy
			server := NewORPCServer(config, forge.NewNoopLogger(), forge.NewNoOpMetrics()).(*server)

			got := server.generateMethodName(tt.route)
			if got != tt.want {
				t.Errorf("generateMethodName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestServer_GenerateParamsSchema_WithCustom(t *testing.T) {
	server := NewORPCServer(DefaultConfig(), forge.NewNoopLogger(), forge.NewNoOpMetrics()).(*server)

	customSchema := &ParamsSchema{
		Type: "object",
		Properties: map[string]*PropertySchema{
			"custom": {Type: "string"},
		},
	}

	route := forge.RouteInfo{
		Method: "GET",
		Path:   "/test",
		Metadata: map[string]any{
			"orpc.params": customSchema,
		},
	}

	schema := server.generateParamsSchema(route)
	if schema.Type != "object" || len(schema.Properties) != 1 {
		t.Error("expected custom schema to be used")
	}
}

func TestServer_GenerateResultSchema_WithCustom(t *testing.T) {
	server := NewORPCServer(DefaultConfig(), forge.NewNoopLogger(), forge.NewNoOpMetrics()).(*server)

	customSchema := &ResultSchema{
		Type:        "object",
		Description: "Custom result",
	}

	route := forge.RouteInfo{
		Method: "GET",
		Path:   "/test",
		Metadata: map[string]any{
			"orpc.result": customSchema,
		},
	}

	schema := server.generateResultSchema(route)
	if schema.Description != "Custom result" {
		t.Error("expected custom schema to be used")
	}
}

func TestExtractPathParams(t *testing.T) {
	tests := []struct {
		name string
		path string
		want []string
	}{
		{
			name: "colon style",
			path: "/users/:id/posts/:postId",
			want: []string{"id", "postId"},
		},
		{
			name: "brace style",
			path: "/users/{id}/posts/{postId}",
			want: []string{"id", "postId"},
		},
		{
			name: "no params",
			path: "/users",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractPathParams(tt.path)
			if len(got) != len(tt.want) {
				t.Errorf("extractPathParams() = %v, want %v", got, tt.want)
			}

			for i, v := range got {
				if v != tt.want[i] {
					t.Errorf("param[%d] = %v, want %v", i, v, tt.want[i])
				}
			}
		})
	}
}

func TestPathToMethodName(t *testing.T) {
	server := NewORPCServer(DefaultConfig(), forge.NewNoopLogger(), forge.NewNoOpMetrics()).(*server)

	tests := []struct {
		method string
		path   string
		want   string
	}{
		{"GET", "/users", "get.users"},
		{"POST", "/users", "create.users"},
		{"PUT", "/users/:id", "update.users.id"},
		{"PATCH", "/users/:id", "patch.users.id"},
		{"DELETE", "/users/:id", "delete.users.id"},
		{"GET", "/api/v1/posts", "get.api.v1.posts"},
		{"GET", "/users-admin", "get.users_admin"},
		{"GET", "/", "get"},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			got := server.pathToMethodName(tt.method, tt.path)
			if got != tt.want {
				t.Errorf("pathToMethodName() = %v, want %v", got, tt.want)
			}
		})
	}
}
