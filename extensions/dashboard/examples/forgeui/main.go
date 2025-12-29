package main

import (
	"context"
	"log"
	"net/http"
	"time"

	g "maragu.dev/gomponents"
	"maragu.dev/gomponents/html"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/dashboard"
	"github.com/xraph/forgeui"
	"github.com/xraph/forgeui/router"
	"github.com/xraph/forgeui/theme"
)

func main() {
	// Create Forge app for backend services
	forgeApp := forge.NewApp(forge.AppConfig{
		Name:        "dashboard-example",
		Version:     "1.0.0",
		Environment: "development",
		HTTPAddress: ":8080",
	})

	// Register some example services for demonstration
	// Note: You'll need to implement your own service registration logic
	// forgeApp.RegisterService("api-server", &ExampleService{name: "API Server"})
	// forgeApp.RegisterService("database", &ExampleService{name: "Database"})
	// forgeApp.RegisterService("cache", &ExampleService{name: "Cache"})

	// Create ForgeUI app for web interface
	uiApp := forgeui.New(
		forgeui.WithDebug(true),
		forgeui.WithThemeName("default"),
	)

	// Configure dashboard
	dashboardConfig := dashboard.Config{
		BasePath:        "/dashboard",
		Title:           "Example Application Dashboard",
		Theme:           "auto",
		EnableRealtime:  true,
		EnableExport:    true,
		RefreshInterval: 10 * time.Second,
		HistoryDuration: 30 * time.Minute,
		MaxDataPoints:   500,
	}

	// Create dashboard integration for ForgeUI
	// Note: This is a special integration for ForgeUI router
	// For standard Forge apps, just use: forgeApp.RegisterExtension(dashboard.NewExtension(...))
	dashboardIntegration := dashboard.NewForgeUIIntegration(
		dashboardConfig,
		forgeApp.HealthManager(),
		forgeApp.Metrics(),
		forgeApp.Logger(),
		forgeApp.Container(),
	)

	// Register dashboard routes with ForgeUI router
	dashboardIntegration.RegisterRoutes(uiApp.Router())

	// Add a simple home page
	uiApp.Router().Get("/", func(ctx *router.PageContext) (g.Node, error) {
		return html.HTML(
			html.Lang("en"),
			html.Head(
				theme.HeadContent(theme.DefaultLight(), theme.DefaultDark()),
				html.TitleEl(g.Text("Dashboard Example")),
				html.Script(html.Src("https://cdn.tailwindcss.com")), // For demo, use CDN
				theme.TailwindConfigScript(),
				theme.StyleTag(theme.DefaultLight(), theme.DefaultDark()),
			),
			html.Body(
				html.Class("min-h-screen bg-background text-foreground"),
				g.Attr("x-data", "{darkMode: false}"),
				g.Attr("x-bind:class", "darkMode ? 'dark' : ''"),

				html.Div(
					html.Class("container mx-auto px-4 py-16"),
					html.Div(
						html.Class("max-w-2xl mx-auto text-center space-y-8"),

						// Header
						html.H1(
							html.Class("text-5xl font-bold tracking-tight"),
							g.Text("Dashboard Example"),
						),

						html.P(
							html.Class("text-xl text-muted-foreground"),
							g.Text("A modern, real-time dashboard built with ForgeUI and Forge framework"),
						),

						// Navigation Cards
						html.Div(
							html.Class("grid gap-6 mt-12 md:grid-cols-2"),

							// Dashboard Card
							html.A(
								html.Href("/dashboard"),
								html.Class("group block p-6 bg-card border rounded-lg hover:shadow-lg transition-all duration-200 hover:-translate-y-1"),
								html.Div(
									html.Class("space-y-2"),
									html.H3(
										html.Class("text-2xl font-semibold group-hover:text-primary transition-colors"),
										g.Text("📊 Dashboard"),
									),
									html.P(
										html.Class("text-muted-foreground"),
										g.Text("View real-time metrics, health checks, and system status"),
									),
								),
							),

							// Export Card
							html.Div(
								html.Class("p-6 bg-card border rounded-lg space-y-4"),
								html.H3(
									html.Class("text-xl font-semibold"),
									g.Text("📤 Export Data"),
								),
								html.Div(
									html.Class("flex flex-col gap-2"),
									html.A(
										html.Href("/dashboard/export/json"),
										html.Class("text-sm text-primary hover:underline"),
										g.Text("→ JSON Format"),
									),
									html.A(
										html.Href("/dashboard/export/csv"),
										html.Class("text-sm text-primary hover:underline"),
										g.Text("→ CSV Format"),
									),
									html.A(
										html.Href("/dashboard/export/prometheus"),
										html.Class("text-sm text-primary hover:underline"),
										g.Text("→ Prometheus Format"),
									),
								),
							),
						),

						// Theme Toggle
						html.Div(
							html.Class("mt-8 pt-8 border-t"),
							html.Button(
								html.Class("px-4 py-2 bg-primary text-primary-foreground rounded-md hover:bg-primary/90 transition-colors"),
								g.Attr("@click", "darkMode = !darkMode"),
								g.Attr("x-text", "darkMode ? '☀️ Light Mode' : '🌙 Dark Mode'"),
							),
						),
					),
				),

				// Alpine.js scripts
				html.Script(html.Src("https://unpkg.com/alpinejs@3.x.x/dist/cdn.min.js"), g.Attr("defer", "")),
				theme.DarkModeScript(),
			),
		), nil
	})

	// Start context
	ctx := context.Background()

	// Start dashboard background services
	if err := dashboardIntegration.Start(ctx); err != nil {
		log.Fatal("Failed to start dashboard:", err)
	}
	defer dashboardIntegration.Stop(ctx)

	// Start Forge app in background (optional - for health checks and metrics)
	go func() {
		if err := forgeApp.Start(ctx); err != nil {
			log.Fatal("Failed to start Forge app:", err)
		}
	}()

	// Start simulating metrics
	// go simulateMetrics(forgeApp) // Commented out: API differs from example

	// Start ForgeUI server
	log.Println("🚀 Server starting...")
	log.Println("📊 Dashboard: http://localhost:8080/dashboard")
	log.Println("🏠 Home: http://localhost:8080/")
	log.Println("📤 Export JSON: http://localhost:8080/dashboard/export/json")
	log.Println("📤 Export CSV: http://localhost:8080/dashboard/export/csv")
	log.Println("📤 Export Prometheus: http://localhost:8080/dashboard/export/prometheus")

	if err := http.ListenAndServe(":8080", uiApp.Router()); err != nil {
		log.Fatal("Server error:", err)
	}
}

// ExampleService is a simple service for demonstration
type ExampleService struct {
	name string
}

func (s *ExampleService) Name() string {
	return s.name
}

func (s *ExampleService) Health(ctx context.Context) error {
	// Simulate health check
	return nil
}

func (s *ExampleService) Start(ctx context.Context) error {
	log.Printf("Starting %s...", s.name)
	return nil
}

func (s *ExampleService) Stop(ctx context.Context) error {
	log.Printf("Stopping %s...", s.name)
	return nil
}

// simulateMetrics generates example metrics for the dashboard
// Commented out: The Metrics API used here doesn't match the actual forge.Metrics interface
// Implement this based on your actual metrics API
/*
func simulateMetrics(app forge.App) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	counter := 0
	for range ticker.C {
		counter++

		// Record some example metrics
		// app.Metrics().RecordCounter("requests_total", float64(counter))
		// app.Metrics().RecordGauge("active_connections", float64(counter%100))
		// app.Metrics().RecordHistogram("request_duration_ms", float64(counter%1000))

		// Simulate varying load
		if counter%10 == 0 {
			// app.Metrics().RecordCounter("errors_total", 1)
		}
	}
}
*/
