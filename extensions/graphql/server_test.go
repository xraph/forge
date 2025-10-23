package graphql

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xraph/forge"
)

func TestNewGraphQLServer(t *testing.T) {
	container := forge.NewContainer()
	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := DefaultConfig()

	server, err := NewGraphQLServer(config, logger, metrics, container)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	if server == nil {
		t.Fatal("server is nil")
	}
}

func TestGraphQLServer_HTTPHandler(t *testing.T) {
	container := forge.NewContainer()
	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := DefaultConfig()

	server, err := NewGraphQLServer(config, logger, metrics, container)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	handler := server.HTTPHandler()
	if handler == nil {
		t.Fatal("HTTPHandler returned nil")
	}
}

func TestGraphQLServer_PlaygroundHandler(t *testing.T) {
	container := forge.NewContainer()
	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := DefaultConfig()

	server, err := NewGraphQLServer(config, logger, metrics, container)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	handler := server.PlaygroundHandler()
	if handler == nil {
		t.Fatal("PlaygroundHandler returned nil")
	}

	// Test playground renders HTML
	req := httptest.NewRequest("GET", "/playground", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if !strings.Contains(contentType, "text/html") {
		t.Errorf("expected Content-Type to contain text/html, got %s", contentType)
	}
}

func TestGraphQLServer_Query(t *testing.T) {
	container := forge.NewContainer()
	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := DefaultConfig()

	server, err := NewGraphQLServer(config, logger, metrics, container)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	handler := server.HTTPHandler()

	// Test hello query
	query := `{"query":"{ hello(name: \"World\") }"}`
	req := httptest.NewRequest("POST", "/graphql", strings.NewReader(query))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "Hello, World!") {
		t.Errorf("expected response to contain 'Hello, World!', got: %s", body)
	}
}

func TestGraphQLServer_GenerateSchema(t *testing.T) {
	container := forge.NewContainer()
	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := DefaultConfig()

	server, err := NewGraphQLServer(config, logger, metrics, container)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	schema, err := server.GenerateSchema()
	if err != nil {
		t.Fatalf("failed to generate schema: %v", err)
	}

	if schema == "" {
		t.Fatal("generated schema is empty")
	}

	// Check schema contains expected types
	if !strings.Contains(schema, "Query") {
		t.Error("schema should contain Query type")
	}
}

func TestGraphQLServer_GetSchema(t *testing.T) {
	container := forge.NewContainer()
	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := DefaultConfig()

	server, err := NewGraphQLServer(config, logger, metrics, container)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	schema := server.GetSchema()
	if schema == nil {
		t.Fatal("GetSchema returned nil")
	}

	if len(schema.Queries) == 0 {
		t.Error("schema should have queries")
	}
}

func TestGraphQLServer_Ping(t *testing.T) {
	container := forge.NewContainer()
	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := DefaultConfig()

	server, err := NewGraphQLServer(config, logger, metrics, container)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	ctx := context.Background()
	if err := server.Ping(ctx); err != nil {
		t.Errorf("Ping failed: %v", err)
	}
}

func TestGraphQLServer_RegisterType(t *testing.T) {
	container := forge.NewContainer()
	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := DefaultConfig()

	server, err := NewGraphQLServer(config, logger, metrics, container)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// RegisterType is a no-op for gqlgen-based implementation
	err = server.RegisterType("TestType", struct{}{})
	if err != nil {
		t.Errorf("RegisterType failed: %v", err)
	}
}

func TestGraphQLServer_EnableIntrospection(t *testing.T) {
	container := forge.NewContainer()
	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := DefaultConfig()

	server, err := NewGraphQLServer(config, logger, metrics, container)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// EnableIntrospection updates config
	server.EnableIntrospection(false)
	server.EnableIntrospection(true)
}
