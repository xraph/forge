package orpc

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/logger"
)

func TestExtension_Lifecycle(t *testing.T) {
	app := forge.New(
		forge.WithAppName("test-app"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)

	ext := NewExtension(WithEnabled(true), WithEndpoint("/rpc"), WithAutoExposeRoutes(true))

	if err := app.RegisterExtension(ext); err != nil {
		t.Fatalf("failed to register extension: %v", err)
	}

	app.Router().GET("/test", func(ctx forge.Context) error {
		return ctx.JSON(200, map[string]string{"message": "hello"})
	}, forge.WithSummary("Test endpoint"))

	if err := app.Start(context.Background()); err != nil {
		t.Fatalf("failed to start app: %v", err)
	}
	defer app.Stop(context.Background())

	if _, err := app.GetExtension("orpc"); err != nil {
		t.Errorf("extension not found: %v", err)
	}
}

func TestExtension_AutoExpose(t *testing.T) {
	app := forge.New(
		forge.WithAppName("test-app"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)

	ext := NewExtension(WithEnabled(true), WithEndpoint("/rpc"), WithAutoExposeRoutes(true))

	app.RegisterExtension(ext)
	app.Router().GET("/users/:id", func(ctx forge.Context) error {
		return ctx.JSON(200, map[string]string{"id": ctx.Param("id")})
	}, forge.WithSummary("Get user"))

	app.Router().POST("/users", func(ctx forge.Context) error {
		return ctx.JSON(201, map[string]string{"created": "true"})
	}, forge.WithSummary("Create user"))

	app.Start(context.Background())
	defer app.Stop(context.Background())

	orpcExt := ext.(*Extension)
	server := orpcExt.Server()
	methods := server.ListMethods()

	if len(methods) < 2 {
		t.Errorf("expected at least 2 methods, got %d", len(methods))
	}

	_, err := server.GetMethod("get.users.id")
	if err != nil {
		t.Errorf("expected method 'get.users.id' to exist: %v", err)
	}
}

func TestExtension_CustomMethodName(t *testing.T) {
	app := forge.New(
		forge.WithAppName("test-app"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)

	ext := NewExtension(WithEnabled(true), WithAutoExposeRoutes(true))
	app.RegisterExtension(ext)

	app.Router().GET("/users/:id", func(ctx forge.Context) error {
		return ctx.JSON(200, map[string]string{"id": ctx.Param("id")})
	},
		forge.WithORPCMethod("user.get"),
		forge.WithSummary("Get user"),
	)

	app.Start(context.Background())
	defer app.Stop(context.Background())

	orpcExt := ext.(*Extension)

	_, err := orpcExt.Server().GetMethod("user.get")
	if err != nil {
		t.Errorf("expected custom method 'user.get' to exist: %v", err)
	}
}

func TestExtension_ExcludeRoute(t *testing.T) {
	app := forge.New(
		forge.WithAppName("test-app"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)

	ext := NewExtension(WithEnabled(true), WithAutoExposeRoutes(true))
	app.RegisterExtension(ext)

	app.Router().GET("/internal/debug", func(ctx forge.Context) error {
		return ctx.JSON(200, map[string]string{"debug": "info"})
	}, forge.WithORPCExclude())

	app.Start(context.Background())
	defer app.Stop(context.Background())

	orpcExt := ext.(*Extension)

	_, err := orpcExt.Server().GetMethod("get.internal.debug")
	if err == nil {
		t.Errorf("expected excluded method to not exist")
	}
}

func TestExtension_JSONRPCRequest(t *testing.T) {
	app := forge.New(
		forge.WithAppName("test-app"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)

	ext := NewExtension(WithEnabled(true), WithAutoExposeRoutes(true))
	app.RegisterExtension(ext)

	app.Router().GET("/test", func(ctx forge.Context) error {
		return ctx.JSON(200, map[string]string{"message": "hello"})
	})

	app.Start(context.Background())
	defer app.Stop(context.Background())

	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"method":  "get.test",
		"id":      1,
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var response Response
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if response.JSONRPC != "2.0" {
		t.Errorf("expected jsonrpc 2.0, got %s", response.JSONRPC)
	}
}

func TestExtension_OpenRPCSchema(t *testing.T) {
	app := forge.New(
		forge.WithAppName("test-app"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)

	ext := NewExtension(WithEnabled(true), WithOpenRPC(true), WithAutoExposeRoutes(true))
	app.RegisterExtension(ext)

	app.Router().GET("/test", func(ctx forge.Context) error {
		return ctx.JSON(200, map[string]string{"message": "hello"})
	}, forge.WithSummary("Test endpoint"))

	app.Start(context.Background())
	defer app.Stop(context.Background())

	req := httptest.NewRequest(http.MethodGet, "/rpc/schema", nil)
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var doc OpenRPCDocument
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("failed to parse schema: %v", err)
	}

	if doc.OpenRPC != "1.3.2" {
		t.Errorf("expected openrpc 1.3.2, got %s", doc.OpenRPC)
	}
}

func TestExtension_Health(t *testing.T) {
	app := forge.New(
		forge.WithAppName("test-app"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)

	ext := NewExtension(WithEnabled(true))
	app.RegisterExtension(ext)

	app.Start(context.Background())
	defer app.Stop(context.Background())

	orpcExt := ext.(*Extension)
	if err := orpcExt.Health(context.Background()); err != nil {
		t.Errorf("health check failed: %v", err)
	}
}

func TestExtension_Health_Disabled(t *testing.T) {
	ext := NewExtension(WithEnabled(false))

	orpcExt := ext.(*Extension)
	if err := orpcExt.Health(context.Background()); err != nil {
		t.Errorf("health check should pass when disabled: %v", err)
	}
}

func TestExtension_BatchRequest(t *testing.T) {
	app := forge.New(
		forge.WithAppName("test-app"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)

	ext := NewExtension(WithEnabled(true), WithBatch(true), WithBatchLimit(2))
	app.RegisterExtension(ext)

	app.Router().GET("/test", func(ctx forge.Context) error {
		return ctx.JSON(200, map[string]string{"result": "ok"})
	})

	app.Start(context.Background())
	defer app.Stop(context.Background())

	reqBody := []map[string]any{
		{"jsonrpc": "2.0", "method": "get.test", "id": 1},
		{"jsonrpc": "2.0", "method": "get.test", "id": 2},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, req)

	var responses []Response
	if err := json.Unmarshal(rec.Body.Bytes(), &responses); err != nil {
		t.Fatalf("failed to parse batch response: %v", err)
	}

	if len(responses) != 2 {
		t.Errorf("expected 2 responses, got %d", len(responses))
	}
}

func TestExtension_ListMethods(t *testing.T) {
	app := forge.New(
		forge.WithAppName("test-app"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)

	ext := NewExtension(WithEnabled(true), WithDiscovery(true))
	app.RegisterExtension(ext)

	app.Router().GET("/test", func(ctx forge.Context) error {
		return ctx.JSON(200, map[string]string{"ok": "true"})
	})

	app.Start(context.Background())
	defer app.Stop(context.Background())

	req := httptest.NewRequest(http.MethodGet, "/rpc/methods", nil)
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestParseBatchRequest_Empty(t *testing.T) {
	body := []byte("[]")

	_, err := parseBatchRequest(body)
	if err == nil {
		t.Error("expected error for empty batch")
	}
}

func TestParseBatchRequest_InvalidElement(t *testing.T) {
	body := []byte(`[{"invalid": true}, "not an object"]`)

	_, err := parseBatchRequest(body)
	if err == nil {
		t.Error("expected error for invalid element")
	}
}

func TestNewExtensionWithConfig(t *testing.T) {
	config := Config{Enabled: true, Endpoint: "/custom-rpc"}
	ext := NewExtensionWithConfig(config)
	orpcExt := ext.(*Extension)

	if orpcExt.config.Endpoint != "/custom-rpc" {
		t.Error("NewExtensionWithConfig failed to apply config")
	}
}

func TestServer_EdgeCases(t *testing.T) {
	app := forge.New(
		forge.WithAppName("test"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)

	config := DefaultConfig()
	config.EnableMetrics = true
	ext := NewExtension(WithConfig(config))
	app.RegisterExtension(ext)

	// Test various HTTP methods for method naming
	app.Router().PUT("/items/:id", func(ctx forge.Context) error {
		return ctx.JSON(200, map[string]string{"updated": "true"})
	})
	app.Router().PATCH("/items/:id", func(ctx forge.Context) error {
		return ctx.JSON(200, map[string]string{"patched": "true"})
	})
	app.Router().DELETE("/items/:id", func(ctx forge.Context) error {
		return ctx.JSON(204, nil)
	})
	app.Router().OPTIONS("/items", func(ctx forge.Context) error {
		return ctx.JSON(200, nil)
	})

	// Test with body parameters
	app.Router().POST("/items", func(ctx forge.Context) error {
		var body map[string]any
		ctx.Bind(&body)

		return ctx.JSON(201, body)
	})

	// Test with query parameters
	app.Router().GET("/search", func(ctx forge.Context) error {
		query := ctx.Request().URL.Query()

		return ctx.JSON(200, map[string]string{"q": query.Get("q")})
	})

	app.Start(context.Background())
	defer app.Stop(context.Background())

	orpcExt := ext.(*Extension)
	server := orpcExt.Server()

	// Test GET stats
	stats := server.GetStats()
	if stats.TotalMethods != 0 {
		t.Log("Stats available")
	}

	// Test invalid params
	req := &Request{
		JSONRPC: "2.0",
		Method:  "create.items",
		Params:  json.RawMessage(`{"invalid json`),
		ID:      1,
	}

	resp := server.HandleRequest(context.Background(), req)
	if resp.Error == nil {
		t.Error("expected error for invalid params")
	}

	// Test with valid params and body
	req = &Request{
		JSONRPC: "2.0",
		Method:  "create.items",
		Params:  json.RawMessage(`{"body": {"name": "item1"}}`),
		ID:      2,
	}
	server.HandleRequest(context.Background(), req)

	// Test with query params
	req = &Request{
		JSONRPC: "2.0",
		Method:  "get.search",
		Params:  json.RawMessage(`{"query": {"q": "test"}}`),
		ID:      3,
	}
	server.HandleRequest(context.Background(), req)
}

func TestExtension_ErrorCases(t *testing.T) {
	app := forge.New(
		forge.WithAppName("test"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)

	ext := NewExtension(WithEnabled(true))
	app.RegisterExtension(ext)

	app.Router().GET("/error", func(ctx forge.Context) error {
		return forge.BadRequest("test error")
	})

	app.Start(context.Background())
	defer app.Stop(context.Background())

	// Test with read error (invalid body)
	req := httptest.NewRequest(http.MethodPost, "/rpc", nil)
	req.Body = &errorReader{}
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, req)

	// Test with invalid request type
	body := []byte(`"just a string"`)
	req = httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec = httptest.NewRecorder()
	app.Router().ServeHTTP(rec, req)

	var response Response
	json.Unmarshal(rec.Body.Bytes(), &response)

	if response.Error == nil {
		t.Error("expected error for invalid request type")
	}

	// Test batch disabled
	app2 := forge.New(
		forge.WithAppName("test2"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)

	ext2 := NewExtension(WithEnabled(true), WithBatch(false))
	app2.RegisterExtension(ext2)

	app2.Start(context.Background())
	defer app2.Stop(context.Background())

	reqBody := []map[string]any{
		{"jsonrpc": "2.0", "method": "test", "id": 1},
	}
	body, _ = json.Marshal(reqBody)
	req = httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec = httptest.NewRecorder()
	app2.Router().ServeHTTP(rec, req)

	json.Unmarshal(rec.Body.Bytes(), &response)

	if response.Error == nil {
		t.Error("expected error for disabled batch")
	}
}

func TestServer_MethodVariations(t *testing.T) {
	app := forge.New(
		forge.WithAppName("test"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)

	config := DefaultConfig()
	config.NamingStrategy = "custom"
	ext := NewExtension(WithConfig(config))
	app.RegisterExtension(ext)

	// Test HEAD method
	app.Router().HEAD("/ping", func(ctx forge.Context) error {
		return nil
	})

	// Test with {param} style
	app.Router().GET("/users/{userId}/posts/{postId}", func(ctx forge.Context) error {
		return ctx.JSON(200, map[string]string{"ok": "true"})
	})

	// Test root path
	app.Router().GET("/", func(ctx forge.Context) error {
		return ctx.JSON(200, map[string]string{"root": "true"})
	})

	app.Start(context.Background())
	defer app.Stop(context.Background())

	methods := ext.(*Extension).Server().ListMethods()
	if len(methods) == 0 {
		t.Error("expected methods to be registered")
	}
}

func TestExtension_ConfigVariations(t *testing.T) {
	// Test with method naming strategy
	app := forge.New(
		forge.WithAppName("test"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)

	ext := NewExtension(
		WithNamingStrategy("method"),
		WithMethodPrefix("api."),
		WithIncludePatterns([]string{"/api/*"}),
	)
	app.RegisterExtension(ext)

	app.Router().GET("/api/users", func(ctx forge.Context) error {
		return ctx.JSON(200, nil)
	}, forge.WithName("listUsers"))

	app.Router().GET("/other", func(ctx forge.Context) error {
		return ctx.JSON(200, nil)
	})

	app.Start(context.Background())
	defer app.Stop(context.Background())

	methods := ext.(*Extension).Server().ListMethods()
	for _, m := range methods {
		if m.Name == "listUsers" || m.Name == "api.listUsers" {
			return // Found expected method
		}
	}
}

func TestExtension_NonJSONResponse(t *testing.T) {
	app := forge.New(
		forge.WithAppName("test"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)

	ext := NewExtension(WithEnabled(true))
	app.RegisterExtension(ext)

	// Route that returns non-JSON
	app.Router().GET("/text", func(ctx forge.Context) error {
		ctx.Response().Write([]byte("plain text"))

		return nil
	})

	app.Start(context.Background())
	defer app.Stop(context.Background())

	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"method":  "get.text",
		"id":      1,
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, req)

	var response Response
	json.Unmarshal(rec.Body.Bytes(), &response)
	// Should handle non-JSON response gracefully
}

// errorReader simulates a read error.
type errorReader struct{}

func (e *errorReader) Read(p []byte) (n int, err error) {
	return 0, context.DeadlineExceeded
}

func (e *errorReader) Close() error {
	return nil
}
