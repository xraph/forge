package shared

import (
	"bytes"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/fatih/color"
)

func TestPrintStartupBannerRendersConsoleCard(t *testing.T) {
	output := captureStartupBanner(t, BannerConfig{
		AppName:     "payments-api",
		Version:     "1.4.0",
		Environment: "development",
		HTTPAddress: ":8080",
		StartTime:   time.Now().Add(-184 * time.Millisecond),
		OpenAPISpec: "/openapi.json",
		OpenAPIUI:   "/swagger",
		AsyncAPIUI:  "/asyncapi",
		HealthPath:  "/_/health",
		MetricsPath: "/_/metrics",
		PprofPath:   "/_/debug/pprof",
	})

	for _, expected := range []string{
		"╭", "◆ FORGE", "v1.4.0", "payments-api · development", "├", "┤",
		"http://localhost:8080", "API", "Swagger", "/swagger", "OpenAPI", "/openapi.json",
		"AsyncAPI", "/asyncapi", "SYSTEM", "Health", "/_/health", "Metrics", "/_/metrics",
		"Profiling", "/_/debug/pprof", "Ctrl+C to stop", "╰", "╯",
	} {
		if !strings.Contains(output, expected) {
			t.Errorf("banner output missing %q\nGot:\n%s", expected, output)
		}
	}

	if !regexp.MustCompile(`● Ready in \d+ms`).MatchString(output) {
		t.Fatalf("banner output missing startup duration\nGot:\n%s", output)
	}

	if strings.Contains(output, "███████╗") {
		t.Fatalf("banner still contains the legacy ASCII wordmark\nGot:\n%s", output)
	}

	for line := range strings.SplitSeq(output, "\n") {
		if (strings.Contains(line, "◆ FORGE") || strings.Contains(line, "● Ready")) && !strings.HasSuffix(line, "  │") {
			t.Errorf("prominent row has no right-side breathing room: %q", line)
		}
	}
}

func TestPrintStartupBannerOmitsEmptySections(t *testing.T) {
	output := captureStartupBanner(t, BannerConfig{
		AppName:     "worker",
		Version:     "2.0.0",
		Environment: "production",
		HTTPAddress: "127.0.0.1:9000",
		StartTime:   time.Now(),
	})

	for _, unexpected := range []string{"API", "SYSTEM", "Swagger", "Health", "Metrics", "Profiling"} {
		if strings.Contains(output, unexpected) {
			t.Errorf("minimal banner unexpectedly contains %q\nGot:\n%s", unexpected, output)
		}
	}

	if !strings.Contains(output, "http://127.0.0.1:9000") {
		t.Fatalf("banner did not render a usable server URL\nGot:\n%s", output)
	}
}

func TestPrintStartupBannerKeepsColoredRowsAligned(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")

	output := captureColoredStartupBanner(t, BannerConfig{
		AppName:     "Portal",
		Version:     "0.1.0",
		Environment: "development",
		HTTPAddress: ":7901",
		StartTime:   time.Now().Add(-18 * time.Millisecond),
		OpenAPISpec: "/openapi.json",
		OpenAPIUI:   "/swagger",
		AsyncAPIUI:  "/asyncapi",
		HealthPath:  "/_/health",
		MetricsPath: "/_/metrics",
	})

	ansi := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	if !ansi.MatchString(output) {
		t.Fatal("color-enabled banner did not contain ANSI styling")
	}

	wantWidth := 0

	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		visible := ansi.ReplaceAllString(line, "")
		if wantWidth == 0 {
			wantWidth = len([]rune(visible))
		}

		if width := len([]rune(visible)); width != wantWidth {
			t.Errorf("colored banner row width = %d, want %d: %q", width, wantWidth, visible)
		}
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
			color.NoColor = true

			defer func() { color.NoColor = false }()

			if result := getEnvColor(tt.environment)(tt.expected); !strings.Contains(result, tt.expected) {
				t.Errorf("getEnvColor(%q) did not preserve text, got %q", tt.environment, result)
			}
		})
	}
}

func captureStartupBanner(t *testing.T, cfg BannerConfig) string {
	return captureStartupBannerWithColor(t, cfg, true)
}

func captureColoredStartupBanner(t *testing.T, cfg BannerConfig) string {
	return captureStartupBannerWithColor(t, cfg, false)
}

func captureStartupBannerWithColor(t *testing.T, cfg BannerConfig, noColor bool) string {
	t.Helper()

	color.NoColor = noColor

	defer func() { color.NoColor = false }()

	old := os.Stdout

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	os.Stdout = w

	defer func() { os.Stdout = old }()

	PrintStartupBanner(cfg)

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}

	return buf.String()
}
