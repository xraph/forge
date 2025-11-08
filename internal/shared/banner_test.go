package shared

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/fatih/color"
)

func TestPrintStartupBanner(t *testing.T) {
	// Disable colors for consistent output in tests
	color.NoColor = true

	defer func() { color.NoColor = false }()

	tests := []struct {
		name     string
		config   BannerConfig
		contains []string
	}{
		{
			name: "minimal_config",
			config: BannerConfig{
				AppName:     "test-app",
				Version:     "1.0.0",
				Environment: "development",
				HTTPAddress: ":8080",
				StartTime:   time.Now(),
			},
			contains: []string{
				"███████╗", // Part of ASCII art logo
				"Application: test-app",
				"Version: 1.0.0",
				"Environment: development",
				"Server: :8080",
				"Press Ctrl+C to shutdown gracefully",
			},
		},
		{
			name: "with_openapi",
			config: BannerConfig{
				AppName:     "api-app",
				Version:     "2.0.0",
				Environment: "production",
				HTTPAddress: ":8443",
				StartTime:   time.Now(),
				OpenAPISpec: "/openapi.json",
				OpenAPIUI:   "/swagger",
			},
			contains: []string{
				"api-app",
				"production",
				"API Documentation:",
				"OpenAPI Spec:  /openapi.json",
				"Swagger UI:    /swagger",
			},
		},
		{
			name: "with_observability",
			config: BannerConfig{
				AppName:     "obs-app",
				Version:     "3.0.0",
				Environment: "staging",
				HTTPAddress: ":9090",
				StartTime:   time.Now(),
				HealthPath:  "/_/health",
				MetricsPath: "/_/metrics",
			},
			contains: []string{
				"obs-app",
				"staging",
				"Observability:",
				"Health:        /_/health",
				"Metrics:       /_/metrics",
			},
		},
		{
			name: "complete_config",
			config: BannerConfig{
				AppName:     "full-app",
				Version:     "4.0.0",
				Environment: "development",
				HTTPAddress: ":3000",
				StartTime:   time.Now(),
				OpenAPISpec: "/api/openapi.json",
				OpenAPIUI:   "/api/docs",
				AsyncAPIUI:  "/api/async-docs",
				HealthPath:  "/health",
				MetricsPath: "/metrics",
			},
			contains: []string{
				"full-app",
				"API Documentation:",
				"OpenAPI Spec:  /api/openapi.json",
				"Swagger UI:    /api/docs",
				"AsyncAPI UI:   /api/async-docs",
				"Observability:",
				"Health:        /health",
				"Metrics:       /metrics",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture stdout
			old := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			// Print banner
			PrintStartupBanner(tt.config)

			// Restore stdout
			w.Close()

			os.Stdout = old

			// Read captured output
			var buf bytes.Buffer
			io.Copy(&buf, r)
			output := buf.String()

			// Verify all expected strings are present
			for _, expected := range tt.contains {
				if !strings.Contains(output, expected) {
					t.Errorf("Banner output missing expected string: %q\nGot:\n%s", expected, output)
				}
			}

			// Verify banner structure
			if !strings.Contains(output, "━━━") {
				t.Error("Banner should contain border lines")
			}
		})
	}
}

func TestBannerEnvironmentColors(t *testing.T) {
	tests := []struct {
		environment string
		expected    string
	}{
		{"production", "production"},
		{"staging", "staging"},
		{"development", "development"},
		{"test", "test"},
	}

	for _, tt := range tests {
		t.Run(tt.environment, func(t *testing.T) {
			colorFunc := getEnvColor(tt.environment)
			result := colorFunc(tt.expected)

			// The function should return the input (colors disabled in tests)
			if !strings.Contains(result, tt.expected) {
				t.Errorf("getEnvColor(%q) did not preserve text, got %q", tt.environment, result)
			}
		})
	}
}

func TestBannerFormatEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{"OpenAPI", "/openapi.json", "OpenAPI:"},
		{"Health", "/_/health", "Health:"},
		{"Metrics", "/_/metrics", "Metrics:"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Disable colors for consistent testing
			color.NoColor = true

			defer func() { color.NoColor = false }()

			result := formatEndpoint(tt.name, tt.path, color.New(color.FgGreen).SprintFunc())

			if !strings.Contains(result, tt.expected) {
				t.Errorf("formatEndpoint did not contain expected name, got %q", result)
			}

			if !strings.Contains(result, tt.path) {
				t.Errorf("formatEndpoint did not contain expected path, got %q", result)
			}
		})
	}
}
