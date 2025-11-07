package discovery

import (
	"context"
	"testing"

	"github.com/xraph/forge"
)

// TestRealWorldUsageWithAppConfigExtensions tests the exact scenario from user's code
// where extensions are passed via AppConfig.Extensions without WithAppConfig
func TestRealWorldUsageWithAppConfigExtensions(t *testing.T) {
	// This mimics the user's actual usage pattern
	app := forge.New(forge.WithConfig(forge.AppConfig{
		Name:        "kineta",
		Version:     "0.1.0",
		HTTPAddress: ":4400",
		Extensions: []forge.Extension{
			NewExtension(
				WithConfig(Config{
					Enabled: true,
					Backend: "mdns",
					// NO Service config - should auto-detect!
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
				}),
			),
		},
	}))

	// Start the app (which starts extensions)
	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatalf("failed to start app: %v", err)
	}
	defer app.Stop(context.Background())

	// Get the extension to verify service instance
	ext, err := app.GetExtension("discovery")
	if err != nil {
		t.Fatalf("failed to get extension: %v", err)
	}

	discExt := ext.(*Extension)

	// Verify service instance was created with correct values
	discExt.mu.RLock()
	instance := discExt.serviceInstance
	discExt.mu.RUnlock()

	if instance == nil {
		t.Fatal("service instance should be created")
	}

	// Validate auto-detected values
	if instance.Name != "kineta" {
		t.Errorf("service name = %s, want kineta (from app.Name)", instance.Name)
	}

	if instance.Version != "0.1.0" {
		t.Errorf("service version = %s, want 0.1.0 (from app.Version)", instance.Version)
	}

	if instance.Port != 4400 {
		t.Errorf("service port = %d, want 4400 (from HTTPAddress :4400)", instance.Port)
	}

	if instance.Address != "localhost" {
		t.Errorf("service address = %s, want localhost (from :4400)", instance.Address)
	}

	// Verify FARP metadata
	if instance.Metadata == nil {
		t.Fatal("metadata should not be nil")
	}

	if instance.Metadata["farp.enabled"] != "true" {
		t.Error("farp.enabled should be 'true'")
	}

	expectedManifest := "http://localhost:4400/_farp/manifest"
	if instance.Metadata["farp.manifest"] != expectedManifest {
		t.Errorf("farp.manifest = %s, want %s", instance.Metadata["farp.manifest"], expectedManifest)
	}

	t.Log("✅ Real-world usage test passed - Service config is truly optional!")
}

// TestMinimalAppConfig tests the absolute minimum configuration
func TestMinimalAppConfig(t *testing.T) {
	app := forge.New(forge.WithConfig(forge.AppConfig{
		Name:        "minimal-service",
		HTTPAddress: ":3000",
		Extensions: []forge.Extension{
			NewExtension(
				WithConfig(Config{
					Enabled: true,
					Backend: "memory",
					// Minimal FARP config
					FARP: FARPConfig{
						Enabled: true,
					},
				}),
			),
		},
	}))

	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatalf("failed to start app: %v", err)
	}
	defer app.Stop(context.Background())

	ext, err := app.GetExtension("discovery")
	if err != nil {
		t.Fatalf("failed to get extension: %v", err)
	}

	discExt := ext.(*Extension)
	discExt.mu.RLock()
	instance := discExt.serviceInstance
	discExt.mu.RUnlock()

	if instance == nil {
		t.Fatal("service instance should be created")
	}

	// Check defaults and auto-detection
	if instance.Name != "minimal-service" {
		t.Errorf("name = %s, want minimal-service", instance.Name)
	}

	if instance.Port != 3000 {
		t.Errorf("port = %d, want 3000", instance.Port)
	}

	if instance.Address != "localhost" {
		t.Errorf("address = %s, want localhost", instance.Address)
	}

	t.Log("✅ Minimal config test passed!")
}

// TestComplexHTTPAddress tests various HTTPAddress formats
func TestComplexHTTPAddress(t *testing.T) {
	tests := []struct {
		name        string
		httpAddress string
		wantAddr    string
		wantPort    int
	}{
		{
			name:        "port only",
			httpAddress: ":8080",
			wantAddr:    "localhost",
			wantPort:    8080,
		},
		{
			name:        "address and port",
			httpAddress: "192.168.1.100:9000",
			wantAddr:    "192.168.1.100",
			wantPort:    9000,
		},
		{
			name:        "zero address",
			httpAddress: "0.0.0.0:5000",
			wantAddr:    "localhost",
			wantPort:    5000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := forge.New(forge.WithConfig(forge.AppConfig{
				Name:        "test-service",
				HTTPAddress: tt.httpAddress,
				Extensions: []forge.Extension{
					NewExtension(
						WithConfig(Config{
							Enabled: true,
							Backend: "memory",
							FARP: FARPConfig{
								Enabled: true,
							},
						}),
					),
				},
			}))

			ctx := context.Background()
			if err := app.Start(ctx); err != nil {
				t.Fatalf("failed to start app: %v", err)
			}
			defer app.Stop(context.Background())

			ext, err := app.GetExtension("discovery")
			if err != nil {
				t.Fatalf("failed to get extension: %v", err)
			}

			discExt := ext.(*Extension)
			discExt.mu.RLock()
			instance := discExt.serviceInstance
			discExt.mu.RUnlock()

			if instance.Address != tt.wantAddr {
				t.Errorf("address = %s, want %s", instance.Address, tt.wantAddr)
			}

			if instance.Port != tt.wantPort {
				t.Errorf("port = %d, want %d", instance.Port, tt.wantPort)
			}
		})
	}
}
