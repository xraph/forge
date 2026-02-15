package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/logger"
)

func newMockLogger() forge.Logger {
	return logger.NewTestLogger()
}

func TestNewOpenAPIAggregator(t *testing.T) {
	config := DefaultOpenAPIConfig()
	logger := newMockLogger()
	rm := NewRouteManager()

	aggr := NewOpenAPIAggregator(config, logger, rm, nil)

	if aggr == nil {
		t.Fatal("expected aggregator, got nil")
	}
}

func TestOpenAPIAggregator_FetchServiceSpec_Success(t *testing.T) {
	// Create mock OpenAPI server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		spec := map[string]any{
			"openapi": "3.1.0",
			"info": map[string]any{
				"title":   "Test API",
				"version": "1.0.0",
			},
			"paths": map[string]any{
				"/users": map[string]any{
					"get": map[string]any{
						"summary": "List users",
					},
				},
			},
		}
		json.NewEncoder(w).Encode(spec)
	}))
	defer server.Close()

	config := DefaultOpenAPIConfig()
	config.FetchTimeout = 5 * time.Second
	logger := newMockLogger()
	rm := NewRouteManager()

	aggr := NewOpenAPIAggregator(config, logger, rm, nil)

	svc := discoveredOpenAPIService{
		Name:    "test-service",
		Version: "1.0.0",
		SpecURL: server.URL,
	}

	spec := aggr.fetchServiceSpec(context.Background(), svc)

	if spec == nil {
		t.Fatal("expected spec, got nil")
	}

	if !spec.Healthy {
		t.Error("expected healthy spec")
	}

	if spec.Error != "" {
		t.Errorf("unexpected error: %s", spec.Error)
	}

	if spec.PathCount != 1 {
		t.Errorf("expected 1 path, got %d", spec.PathCount)
	}
}

func TestOpenAPIAggregator_FetchServiceSpec_404(t *testing.T) {
	// Create mock server that returns 404
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	config := DefaultOpenAPIConfig()
	config.FetchTimeout = 5 * time.Second
	logger := newMockLogger()
	rm := NewRouteManager()

	aggr := NewOpenAPIAggregator(config, logger, rm, nil)

	svc := discoveredOpenAPIService{
		Name:    "test-service",
		Version: "1.0.0",
		SpecURL: server.URL,
	}

	spec := aggr.fetchServiceSpec(context.Background(), svc)

	if spec == nil {
		t.Fatal("expected spec result, got nil")
	}

	if spec.Healthy {
		t.Error("expected unhealthy spec for 404")
	}

	if spec.Error == "" {
		t.Error("expected error message")
	}
}

func TestOpenAPIAggregator_FetchServiceSpec_InvalidJSON(t *testing.T) {
	// Create mock server that returns invalid JSON
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("invalid json"))
	}))
	defer server.Close()

	config := DefaultOpenAPIConfig()
	config.FetchTimeout = 5 * time.Second
	logger := newMockLogger()
	rm := NewRouteManager()

	aggr := NewOpenAPIAggregator(config, logger, rm, nil)

	svc := discoveredOpenAPIService{
		Name:    "test-service",
		Version: "1.0.0",
		SpecURL: server.URL,
	}

	spec := aggr.fetchServiceSpec(context.Background(), svc)

	if spec == nil {
		t.Fatal("expected spec result, got nil")
	}

	if spec.Healthy {
		t.Error("expected unhealthy spec for invalid JSON")
	}

	if spec.Error == "" {
		t.Error("expected error message for invalid JSON")
	}
}

func TestOpenAPIAggregator_BuildMergedSpec(t *testing.T) {
	config := DefaultOpenAPIConfig()
	config.Title = "Test Gateway"
	config.Version = "1.0.0"
	config.MergeStrategy = "prefix"
	logger := newMockLogger()
	rm := NewRouteManager()

	aggr := NewOpenAPIAggregator(config, logger, rm, nil)

	specs := map[string]*ServiceOpenAPISpec{
		"service-a": {
			ServiceName: "service-a",
			Version:     "1.0.0",
			Healthy:     true,
			Spec: map[string]any{
				"openapi": "3.1.0",
				"paths": map[string]any{
					"/users": map[string]any{
						"get": map[string]any{
							"summary": "List users",
						},
					},
				},
			},
		},
		"service-b": {
			ServiceName: "service-b",
			Version:     "2.0.0",
			Healthy:     true,
			Spec: map[string]any{
				"openapi": "3.1.0",
				"paths": map[string]any{
					"/products": map[string]any{
						"get": map[string]any{
							"summary": "List products",
						},
					},
				},
			},
		},
	}

	merged := aggr.buildMergedSpec(specs)

	if merged == nil {
		t.Fatal("expected merged spec, got nil")
	}

	// Check top-level fields
	if merged["openapi"] != "3.1.0" {
		t.Error("expected OpenAPI version 3.1.0")
	}

	info, ok := merged["info"].(map[string]any)
	if !ok {
		t.Fatal("expected info object")
	}

	if info["title"] != "Test Gateway" {
		t.Errorf("expected title 'Test Gateway', got %v", info["title"])
	}

	// Check paths are prefixed
	paths, ok := merged["paths"].(map[string]any)
	if !ok {
		t.Fatal("expected paths object")
	}

	// With prefix strategy, paths should be prefixed with service name
	// Default prefix is /{serviceName}
	if _, ok := paths["/service-a/users"]; !ok {
		t.Error("expected /service-a/users path")
	}

	if _, ok := paths["/service-b/products"]; !ok {
		t.Error("expected /service-b/products path")
	}

	// Check tags
	tags, ok := merged["tags"].([]any)
	if !ok {
		t.Fatal("expected tags array")
	}

	if len(tags) < 2 {
		t.Errorf("expected at least 2 tags (one per service), got %d", len(tags))
	}
}

func TestOpenAPIAggregator_MergedSpecJSON(t *testing.T) {
	config := DefaultOpenAPIConfig()
	config.Title = "Test Gateway"
	logger := newMockLogger()
	rm := NewRouteManager()

	aggr := NewOpenAPIAggregator(config, logger, rm, nil)

	// Initially should be nil
	specJSON := aggr.MergedSpec()
	if specJSON != nil {
		t.Error("expected nil spec before refresh")
	}
}

func TestOpenAPIAggregator_ServiceSpec(t *testing.T) {
	config := DefaultOpenAPIConfig()
	logger := newMockLogger()
	rm := NewRouteManager()

	aggr := NewOpenAPIAggregator(config, logger, rm, nil)

	// Initially should return nil
	spec := aggr.ServiceSpec("non-existent")
	if spec != nil {
		t.Error("expected nil for non-existent service")
	}
}

func TestOpenAPIAggregator_HandleMergedSpec_NotReady(t *testing.T) {
	config := DefaultOpenAPIConfig()
	logger := newMockLogger()
	rm := NewRouteManager()

	aggr := NewOpenAPIAggregator(config, logger, rm, nil)

	// Mock forge.Context
	// Note: This is a simplified test - in real usage, forge.Context would be properly initialized
	// For now, we'll just test that MergedSpec returns nil
	specJSON := aggr.MergedSpec()
	if specJSON != nil {
		t.Error("expected nil spec before any refresh")
	}
}

func TestCountPaths(t *testing.T) {
	tests := []struct {
		name     string
		spec     map[string]any
		expected int
	}{
		{
			name: "empty paths",
			spec: map[string]any{
				"paths": map[string]any{},
			},
			expected: 0,
		},
		{
			name: "multiple paths",
			spec: map[string]any{
				"paths": map[string]any{
					"/users":    map[string]any{},
					"/products": map[string]any{},
					"/orders":   map[string]any{},
				},
			},
			expected: 3,
		},
		{
			name:     "no paths key",
			spec:     map[string]any{},
			expected: 0,
		},
		{
			name: "paths is not a map",
			spec: map[string]any{
				"paths": "invalid",
			},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count := countPaths(tt.spec)
			if count != tt.expected {
				t.Errorf("expected %d paths, got %d", tt.expected, count)
			}
		})
	}
}

func TestTagOperations(t *testing.T) {
	pathItem := map[string]any{
		"get": map[string]any{
			"summary": "Get item",
			"tags":    []any{"existing"},
		},
		"post": map[string]any{
			"summary": "Create item",
		},
	}

	result := tagOperations(pathItem, "service-a")

	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatal("expected map result")
	}

	// Check GET operation
	getOp, ok := resultMap["get"].(map[string]any)
	if !ok {
		t.Fatal("expected GET operation")
	}

	getTags, ok := getOp["tags"].([]any)
	if !ok {
		t.Fatal("expected tags array in GET")
	}

	hasServiceTag := false
	for _, tag := range getTags {
		if tag == "service-a" {
			hasServiceTag = true
			break
		}
	}

	if !hasServiceTag {
		t.Error("expected service-a tag in GET operation")
	}

	// Check POST operation
	postOp, ok := resultMap["post"].(map[string]any)
	if !ok {
		t.Fatal("expected POST operation")
	}

	postTags, ok := postOp["tags"].([]any)
	if !ok {
		t.Fatal("expected tags array in POST")
	}

	hasServiceTagPost := false
	for _, tag := range postTags {
		if tag == "service-a" {
			hasServiceTagPost = true
			break
		}
	}

	if !hasServiceTagPost {
		t.Error("expected service-a tag in POST operation")
	}
}

func TestRewriteRefs(t *testing.T) {
	obj := map[string]any{
		"schema": map[string]any{
			"$ref": "#/components/schemas/User",
		},
		"items": map[string]any{
			"$ref": "#/components/schemas/Product",
		},
	}

	rewriteRefs(obj, "service-a")

	schema, ok := obj["schema"].(map[string]any)
	if !ok {
		t.Fatal("expected schema map")
	}

	ref, ok := schema["$ref"].(string)
	if !ok {
		t.Fatal("expected $ref string")
	}

	if ref != "#/components/schemas/service-a.User" {
		t.Errorf("expected #/components/schemas/service-a.User, got %s", ref)
	}

	items, ok := obj["items"].(map[string]any)
	if !ok {
		t.Fatal("expected items map")
	}

	ref2, ok := items["$ref"].(string)
	if !ok {
		t.Fatal("expected $ref string in items")
	}

	if ref2 != "#/components/schemas/service-a.Product" {
		t.Errorf("expected #/components/schemas/service-a.Product, got %s", ref2)
	}
}

func TestOpenAPIConfig_Defaults(t *testing.T) {
	config := DefaultOpenAPIConfig()

	if config.Path == "" {
		t.Error("expected default path")
	}

	if config.UIPath == "" {
		t.Error("expected default UI path")
	}

	if config.RefreshInterval == 0 {
		t.Error("expected default refresh interval")
	}

	if config.FetchTimeout == 0 {
		t.Error("expected default fetch timeout")
	}

	if config.MergeStrategy == "" {
		t.Error("expected default merge strategy")
	}
}

func TestServiceOpenAPISpec(t *testing.T) {
	spec := &ServiceOpenAPISpec{
		ServiceName: "test-service",
		Version:     "1.0.0",
		SpecURL:     "http://localhost:8080/openapi.json",
		Healthy:     true,
		PathCount:   5,
		FetchedAt:   time.Now(),
	}

	if spec.ServiceName != "test-service" {
		t.Errorf("expected service name test-service, got %s", spec.ServiceName)
	}

	if !spec.Healthy {
		t.Error("expected healthy spec")
	}

	if spec.PathCount != 5 {
		t.Errorf("expected 5 paths, got %d", spec.PathCount)
	}
}
