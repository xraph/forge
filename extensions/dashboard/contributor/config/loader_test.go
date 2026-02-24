package config

import (
	"path/filepath"
	"testing"
)

func TestParseConfig_Valid(t *testing.T) {
	yaml := `
name: auth
display_name: Authentication
version: 1.0.0
type: astro
build:
  mode: static
nav:
  - label: Overview
    path: /
    icon: shield
    group: Security
  - label: Sessions
    path: /sessions
    icon: clock
    group: Security
    priority: 1
widgets:
  - id: active-sessions
    title: Active Sessions
    size: sm
    refresh_sec: 30
settings:
  - id: auth-config
    title: Authentication Settings
    description: Configure providers and MFA
bridge_functions:
  - name: auth.getOverviewStats
    description: Fetch overview stats
searchable: true
`

	cfg, err := ParseConfig([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Name != "auth" {
		t.Errorf("Name = %q, want %q", cfg.Name, "auth")
	}

	if cfg.DisplayName != "Authentication" {
		t.Errorf("DisplayName = %q, want %q", cfg.DisplayName, "Authentication")
	}

	if cfg.Type != "astro" {
		t.Errorf("Type = %q, want %q", cfg.Type, "astro")
	}

	if cfg.Build.Mode != "static" {
		t.Errorf("Build.Mode = %q, want %q", cfg.Build.Mode, "static")
	}

	if len(cfg.Nav) != 2 {
		t.Errorf("Nav count = %d, want 2", len(cfg.Nav))
	}

	if len(cfg.Widgets) != 1 {
		t.Errorf("Widgets count = %d, want 1", len(cfg.Widgets))
	}

	if len(cfg.Settings) != 1 {
		t.Errorf("Settings count = %d, want 1", len(cfg.Settings))
	}

	if len(cfg.BridgeFunctions) != 1 {
		t.Errorf("BridgeFunctions count = %d, want 1", len(cfg.BridgeFunctions))
	}

	if !cfg.Searchable {
		t.Error("Searchable should be true")
	}
}

func TestParseConfig_Defaults(t *testing.T) {
	yaml := `
name: auth
display_name: Authentication
version: 1.0.0
type: astro
build:
  mode: static
`

	cfg, err := ParseConfig([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Build.UIDir != "ui" {
		t.Errorf("Build.UIDir = %q, want %q", cfg.Build.UIDir, "ui")
	}

	if cfg.Build.DistDir != "dist" {
		t.Errorf("Build.DistDir = %q, want %q", cfg.Build.DistDir, "dist")
	}

	if cfg.Build.EmbedPath != "ui/dist" {
		t.Errorf("Build.EmbedPath = %q, want %q", cfg.Build.EmbedPath, "ui/dist")
	}

	if cfg.Build.PagesDir != "pages" {
		t.Errorf("Build.PagesDir = %q, want %q", cfg.Build.PagesDir, "pages")
	}
}

func TestParseConfig_NextjsDefaults(t *testing.T) {
	tests := []struct {
		name     string
		mode     string
		wantDist string
		wantSSR  string
	}{
		{"static", "static", "out", ""},
		{"ssr", "ssr", ".next", "ui/.next/standalone/server.js"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			yaml := `
name: analytics
display_name: Analytics
version: 1.0.0
type: nextjs
build:
  mode: ` + tt.mode

			cfg, err := ParseConfig([]byte(yaml))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if cfg.Build.DistDir != tt.wantDist {
				t.Errorf("Build.DistDir = %q, want %q", cfg.Build.DistDir, tt.wantDist)
			}

			if cfg.Build.SSREntry != tt.wantSSR {
				t.Errorf("Build.SSREntry = %q, want %q", cfg.Build.SSREntry, tt.wantSSR)
			}
		})
	}
}

func TestParseConfig_ValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{"missing name", `display_name: Test
version: 1.0.0
type: astro
build:
  mode: static`},
		{"hyphenated name", `name: my-ext
display_name: Test
version: 1.0.0
type: astro
build:
  mode: static`},
		{"missing display_name", `name: test
version: 1.0.0
type: astro
build:
  mode: static`},
		{"missing version", `name: test
display_name: Test
type: astro
build:
  mode: static`},
		{"invalid type", `name: test
display_name: Test
version: 1.0.0
type: svelte
build:
  mode: static`},
		{"invalid mode", `name: test
display_name: Test
version: 1.0.0
type: astro
build:
  mode: hybrid`},
		{"nav missing label", `name: test
display_name: Test
version: 1.0.0
type: astro
build:
  mode: static
nav:
  - path: /`},
		{"widget missing id", `name: test
display_name: Test
version: 1.0.0
type: astro
build:
  mode: static
widgets:
  - title: Widget`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseConfig([]byte(tt.yaml))
			if err == nil {
				t.Error("expected validation error, got nil")
			}
		})
	}
}

func TestContributorConfig_Paths(t *testing.T) {
	cfg := &ContributorConfig{
		Build: BuildConfig{
			UIDir:   "ui",
			DistDir: "dist",
		},
	}

	extRoot := filepath.FromSlash("/workspace/extensions/auth")

	uiPath := cfg.UIPath(extRoot)
	wantUI := filepath.Join(extRoot, "ui")
	if uiPath != wantUI {
		t.Errorf("UIPath = %q, want %q", uiPath, wantUI)
	}

	distPath := cfg.DistPath(extRoot)
	wantDist := filepath.Join(extRoot, "ui", "dist")
	if distPath != wantDist {
		t.Errorf("DistPath = %q, want %q", distPath, wantDist)
	}
}
