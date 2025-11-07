package discovery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xraph/forge"
	"github.com/xraph/forge/farp"
)

// TestFARPManifestEndpoint validates the /_farp/manifest endpoint works correctly
func TestFARPManifestEndpoint(t *testing.T) {
	// Create app with router
	app := forge.New(forge.WithConfig(forge.AppConfig{
		Name:    "test-service",
		Version: "1.0.0",
	}))

	// Configure discovery with FARP
	config := Config{
		Enabled: true,
		Backend: "memory",
		Service: ServiceConfig{
			Name:    "test-service",
			Version: "1.0.0",
			Address: "127.0.0.1",
			Port:    8080,
		},
		FARP: FARPConfig{
			Enabled:      true,
			AutoRegister: true,
			Capabilities: []string{"rest", "grpc"},
			Endpoints: FARPEndpointsConfig{
				Health:  "/health",
				OpenAPI: "/openapi.json",
			},
		},
	}

	ext := NewExtension(WithConfig(config))
	discExt := ext.(*Extension)

	// Register and start extension
	if err := discExt.Register(app); err != nil {
		t.Fatalf("failed to register extension: %v", err)
	}

	ctx := context.Background()
	if err := discExt.Start(ctx); err != nil {
		t.Fatalf("failed to start extension: %v", err)
	}
	defer discExt.Stop(context.Background())

	// Create test HTTP request to manifest endpoint
	req := httptest.NewRequest("GET", "/_farp/manifest", nil)
	rec := httptest.NewRecorder()

	// Get router and serve request
	router := app.Router()
	if router == nil {
		t.Fatal("router is nil")
	}

	// Serve the request through the router
	router.ServeHTTP(rec, req)

	// Check response
	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
		t.Logf("Response body: %s", rec.Body.String())
	}

	// Parse response
	var manifest farp.SchemaManifest
	if err := json.Unmarshal(rec.Body.Bytes(), &manifest); err != nil {
		t.Fatalf("failed to parse manifest: %v", err)
	}

	// Validate manifest
	if manifest.ServiceName != "test-service" {
		t.Errorf("service_name = %s, want test-service", manifest.ServiceName)
	}

	if manifest.ServiceVersion != "1.0.0" {
		t.Errorf("service_version = %s, want 1.0.0", manifest.ServiceVersion)
	}

	if manifest.InstanceID == "" {
		t.Error("instance_id is empty")
	}

	if len(manifest.Capabilities) != 2 {
		t.Errorf("len(capabilities) = %d, want 2", len(manifest.Capabilities))
	}

	if manifest.Endpoints.Health != "/health" {
		t.Errorf("health endpoint = %s, want /health", manifest.Endpoints.Health)
	}

	if manifest.Endpoints.OpenAPI != "/openapi.json" {
		t.Errorf("openapi endpoint = %s, want /openapi.json", manifest.Endpoints.OpenAPI)
	}
}

// TestFARPDiscoveryEndpoint validates the /_farp/discovery endpoint works correctly
func TestFARPDiscoveryEndpoint(t *testing.T) {
	// Create app with router
	app := forge.New(forge.WithConfig(forge.AppConfig{
		Name:    "test-service",
		Version: "1.0.0",
	}))

	// Configure discovery with FARP
	config := Config{
		Enabled: true,
		Backend: "memory",
		Service: ServiceConfig{
			Name:    "test-service",
			Version: "1.0.0",
			Address: "192.168.1.100",
			Port:    8080,
			Tags:    []string{"api", "v1"},
		},
		FARP: FARPConfig{
			Enabled:      true,
			AutoRegister: true,
			Capabilities: []string{"rest"},
		},
	}

	ext := NewExtension(WithConfig(config))
	discExt := ext.(*Extension)

	// Register and start extension
	if err := discExt.Register(app); err != nil {
		t.Fatalf("failed to register extension: %v", err)
	}

	ctx := context.Background()
	if err := discExt.Start(ctx); err != nil {
		t.Fatalf("failed to start extension: %v", err)
	}
	defer discExt.Stop(context.Background())

	// Create test HTTP request to discovery endpoint
	req := httptest.NewRequest("GET", "/_farp/discovery", nil)
	rec := httptest.NewRecorder()

	// Get router and serve request
	router := app.Router()
	if router == nil {
		t.Fatal("router is nil")
	}

	// Serve the request
	router.ServeHTTP(rec, req)

	// Check response
	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
		t.Logf("Response body: %s", rec.Body.String())
	}

	// Parse response
	var response map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	// Validate response
	if response["service_name"] != "test-service" {
		t.Errorf("service_name = %v, want test-service", response["service_name"])
	}

	if response["service_version"] != "1.0.0" {
		t.Errorf("service_version = %v, want 1.0.0", response["service_version"])
	}

	if response["address"] != "192.168.1.100" {
		t.Errorf("address = %v, want 192.168.1.100", response["address"])
	}

	if response["port"] != float64(8080) {
		t.Errorf("port = %v, want 8080", response["port"])
	}

	if response["farp_enabled"] != true {
		t.Error("farp_enabled should be true")
	}

	if response["backend"] != "memory" {
		t.Errorf("backend = %v, want memory", response["backend"])
	}

	// Check metadata
	metadata, ok := response["metadata"].(map[string]interface{})
	if !ok {
		t.Fatal("metadata is not a map")
	}

	if metadata["farp.enabled"] != "true" {
		t.Error("farp.enabled should be 'true' in metadata")
	}
}

// TestFARPEndpointsWithoutService validates endpoints return error when service not registered
func TestFARPEndpointsWithoutService(t *testing.T) {
	// Create app with router
	app := forge.New(forge.WithConfig(forge.AppConfig{
		Name:    "test-service",
		Version: "1.0.0",
	}))

	// Configure discovery with FARP but WITHOUT service registration
	config := Config{
		Enabled: true,
		Backend: "memory",
		Service: ServiceConfig{
			Name: "", // Empty name = no service registration
		},
		FARP: FARPConfig{
			Enabled:      true,
			AutoRegister: false,
		},
	}

	ext := NewExtension(WithConfig(config))
	discExt := ext.(*Extension)

	// Register extension (but don't start - no service registration)
	if err := discExt.Register(app); err != nil {
		t.Fatalf("failed to register extension: %v", err)
	}

	// Test manifest endpoint
	req := httptest.NewRequest("GET", "/_farp/manifest", nil)
	rec := httptest.NewRecorder()

	router := app.Router()
	if router == nil {
		t.Fatal("router is nil")
	}

	router.ServeHTTP(rec, req)

	// Should return 503 Service Unavailable
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", rec.Code)
	}

	// Parse error response
	var errResponse map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &errResponse); err != nil {
		t.Fatalf("failed to parse error response: %v", err)
	}

	if errResponse["error"] != "service not registered" {
		t.Errorf("error message = %s, want 'service not registered'", errResponse["error"])
	}
}
