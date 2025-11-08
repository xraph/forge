package discovery

import (
	"context"
	"testing"

	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/logger"
)

// TestAutoConfigFromApp validates that Service config is optional and reads from app config.
func TestAutoConfigFromApp(t *testing.T) {
	// Create app with name, version, and HTTPAddress
	app := forge.New(forge.WithConfig(forge.AppConfig{
		Name:        "kineta",
		Version:     "0.1.0",
		HTTPAddress: ":4400",
	}), forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithAppMetrics(forge.NewNoOpMetrics()))

	// Configure discovery WITHOUT Service config - should use app config
	appConfig := forge.AppConfig{
		Name:        "kineta",
		Version:     "0.1.0",
		HTTPAddress: ":4400",
	}

	config := Config{
		Enabled: true,
		Backend: "memory",
		// Service: NOT PROVIDED - should auto-detect from app
		FARP: FARPConfig{
			Enabled:      true,
			AutoRegister: true,
			Capabilities: []string{"rest", "grpc", "graphql"},
			Endpoints: FARPEndpointsConfig{
				OpenAPI:        "/openapi.json",
				GraphQL:        "/graphql",
				GRPCReflection: true,
			},
		},
	}

	ext := NewExtension(WithConfig(config), WithAppConfig(appConfig))
	discExt := ext.(*Extension)

	// Register extension
	if err := discExt.Register(app); err != nil {
		t.Fatalf("failed to register extension: %v", err)
	}

	// Start extension
	ctx := context.Background()
	if err := discExt.Start(ctx); err != nil {
		t.Fatalf("failed to start extension: %v", err)
	}
	defer discExt.Stop(context.Background())

	// Verify service instance was created from app config
	discExt.mu.RLock()
	instance := discExt.serviceInstance
	discExt.mu.RUnlock()

	if instance == nil {
		t.Fatal("service instance should be created even without Service config")
	}

	// Validate values came from app config
	if instance.Name != "kineta" {
		t.Errorf("service name = %s, want kineta (from app.Name)", instance.Name)
	}

	if instance.Version != "0.1.0" {
		t.Errorf("service version = %s, want 0.1.0 (from app.Version)", instance.Version)
	}

	if instance.Port != 4400 {
		t.Errorf("service port = %d, want 4400 (from app.HTTPAddress :4400)", instance.Port)
	}

	if instance.Address != "localhost" {
		t.Errorf("service address = %s, want localhost (default)", instance.Address)
	}

	// Verify FARP metadata was injected
	if instance.Metadata == nil {
		t.Fatal("service instance metadata is nil")
	}

	if instance.Metadata["farp.enabled"] != "true" {
		t.Error("farp.enabled should be 'true' in metadata")
	}

	// Verify manifest URL includes correct address and port
	expectedManifest := "http://localhost:4400/_farp/manifest"
	if instance.Metadata["farp.manifest"] != expectedManifest {
		t.Errorf("farp.manifest = %s, want %s", instance.Metadata["farp.manifest"], expectedManifest)
	}
}

// TestAutoConfigWithPartialServiceConfig validates mixing Service config with app config.
func TestAutoConfigWithPartialServiceConfig(t *testing.T) {
	app := forge.New(forge.WithConfig(forge.AppConfig{
		Name:        "my-app",
		Version:     "1.0.0",
		HTTPAddress: "0.0.0.0:8080",
	}), forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithAppMetrics(forge.NewNoOpMetrics()))

	// Provide partial Service config - only override address
	appConfig := forge.AppConfig{
		Name:        "my-app",
		Version:     "1.0.0",
		HTTPAddress: "0.0.0.0:8080",
	}

	config := Config{
		Enabled: true,
		Backend: "memory",
		Service: ServiceConfig{
			Address: "192.168.1.100", // Override address
			// Name, Version, Port should come from app config
		},
		FARP: FARPConfig{
			Enabled:      true,
			AutoRegister: true,
		},
	}

	ext := NewExtension(WithConfig(config), WithAppConfig(appConfig))
	discExt := ext.(*Extension)

	if err := discExt.Register(app); err != nil {
		t.Fatalf("failed to register extension: %v", err)
	}

	ctx := context.Background()
	if err := discExt.Start(ctx); err != nil {
		t.Fatalf("failed to start extension: %v", err)
	}
	defer discExt.Stop(context.Background())

	discExt.mu.RLock()
	instance := discExt.serviceInstance
	discExt.mu.RUnlock()

	// Name and version should come from app
	if instance.Name != "my-app" {
		t.Errorf("service name = %s, want my-app (from app)", instance.Name)
	}

	if instance.Version != "1.0.0" {
		t.Errorf("service version = %s, want 1.0.0 (from app)", instance.Version)
	}

	// Port should come from HTTPAddress
	if instance.Port != 8080 {
		t.Errorf("service port = %d, want 8080 (from HTTPAddress)", instance.Port)
	}

	// Address should use the override
	if instance.Address != "192.168.1.100" {
		t.Errorf("service address = %s, want 192.168.1.100 (override)", instance.Address)
	}

	// Verify manifest URL uses overridden address
	expectedManifest := "http://192.168.1.100:8080/_farp/manifest"
	if instance.Metadata["farp.manifest"] != expectedManifest {
		t.Errorf("farp.manifest = %s, want %s", instance.Metadata["farp.manifest"], expectedManifest)
	}
}

// TestHTTPAddressParsingVariations validates different HTTPAddress formats.
func TestHTTPAddressParsingVariations(t *testing.T) {
	tests := []struct {
		name            string
		httpAddress     string
		expectedAddress string
		expectedPort    int
	}{
		{
			name:            ":4400",
			httpAddress:     ":4400",
			expectedAddress: "localhost",
			expectedPort:    4400,
		},
		{
			name:            "localhost:8080",
			httpAddress:     "localhost:8080",
			expectedAddress: "localhost",
			expectedPort:    8080,
		},
		{
			name:            "0.0.0.0:3000",
			httpAddress:     "0.0.0.0:3000",
			expectedAddress: "localhost", // 0.0.0.0 converted to localhost
			expectedPort:    3000,
		},
		{
			name:            "192.168.1.100:9000",
			httpAddress:     "192.168.1.100:9000",
			expectedAddress: "192.168.1.100",
			expectedPort:    9000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			appConfig := forge.AppConfig{
				Name:        "test-app",
				Version:     "1.0.0",
				HTTPAddress: tt.httpAddress,
			}

			app := forge.New(forge.WithConfig(appConfig), forge.WithAppLogger(logger.NewNoopLogger()),
				forge.WithAppMetrics(forge.NewNoOpMetrics()))

			config := Config{
				Enabled: true,
				Backend: "memory",
				FARP: FARPConfig{
					Enabled: true,
				},
			}

			ext := NewExtension(WithConfig(config), WithAppConfig(appConfig))
			discExt := ext.(*Extension)

			if err := discExt.Register(app); err != nil {
				t.Fatalf("failed to register extension: %v", err)
			}

			ctx := context.Background()
			if err := discExt.Start(ctx); err != nil {
				t.Fatalf("failed to start extension: %v", err)
			}
			defer discExt.Stop(context.Background())

			discExt.mu.RLock()
			instance := discExt.serviceInstance
			discExt.mu.RUnlock()

			if instance.Address != tt.expectedAddress {
				t.Errorf("address = %s, want %s", instance.Address, tt.expectedAddress)
			}

			if instance.Port != tt.expectedPort {
				t.Errorf("port = %d, want %d", instance.Port, tt.expectedPort)
			}
		})
	}
}
