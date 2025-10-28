package main

import (
	"fmt"
	"time"

	"github.com/xraph/forge"
)

// This example demonstrates the startup banner with different configurations
func main() {
	fmt.Println("========== Example 1: Minimal Configuration ==========")
	app1 := forge.NewApp(forge.AppConfig{
		Name:        "minimal-app",
		Version:     "1.0.0",
		Environment: "development",
		HTTPAddress: ":8080",
	})

	// Simulate startup banner display
	time.Sleep(100 * time.Millisecond)

	fmt.Println("\n========== Example 2: Production with OpenAPI ==========")
	app2 := forge.NewApp(forge.AppConfig{
		Name:        "production-api",
		Version:     "2.5.3",
		Environment: "production",
		HTTPAddress: ":443",
		RouterOptions: []forge.RouterOption{
			forge.WithOpenAPI(forge.OpenAPIConfig{
				Title:       "Production API",
				Description: "Production REST API with OpenAPI documentation",
				Version:     "2.5.3",
				UIEnabled:   true,
				SpecEnabled: true,
			}),
		},
	})

	time.Sleep(100 * time.Millisecond)

	fmt.Println("\n========== Example 3: Staging with Full Observability ==========")
	app3 := forge.NewApp(forge.AppConfig{
		Name:        "staging-service",
		Version:     "3.0.0-beta",
		Environment: "staging",
		HTTPAddress: ":9090",
		MetricsConfig: forge.MetricsConfig{
			Enabled: true,
		},
		HealthConfig: forge.HealthConfig{
			Enabled: true,
		},
	})

	// Show that we created the apps
	_ = app1.Run()
	_ = app2.Run()
	_ = app3.Run()

	fmt.Println("\n✓ All banner examples displayed successfully!")
	fmt.Println("\nNote: Each app shows:")
	fmt.Println("  • Forge ASCII logo in cyan")
	fmt.Println("  • Application metadata (name, version, environment)")
	fmt.Println("  • Server address and start time")
	fmt.Println("  • API documentation paths (if OpenAPI/AsyncAPI enabled)")
	fmt.Println("  • Observability endpoints (if health/metrics enabled)")
	fmt.Println("  • Environment-specific colors:")
	fmt.Println("    - Production: RED (high attention)")
	fmt.Println("    - Staging:    YELLOW (caution)")
	fmt.Println("    - Development: CYAN (calm)")
}
