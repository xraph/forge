// Package config defines the schema for forge.contributor.yaml configuration
// files that declare dashboard contributor metadata, navigation, widgets,
// settings, and build configuration.
package config

// ContributorConfig is the top-level schema for forge.contributor.yaml.
type ContributorConfig struct {
	// Name is the unique identifier for this contributor (Go package name, no hyphens).
	Name string `json:"name" yaml:"name"`

	// DisplayName is the human-readable display name shown in the dashboard.
	DisplayName string `json:"display_name" yaml:"display_name"`

	// Icon is the icon name used in the dashboard sidebar.
	Icon string `json:"icon,omitempty" yaml:"icon,omitempty"`

	// Version is the semantic version of this contributor.
	Version string `json:"version" yaml:"version"`

	// Type is the UI framework used: "astro", "nextjs", or "custom".
	Type string `json:"type" yaml:"type"`

	// Build contains build-related configuration.
	Build BuildConfig `json:"build" yaml:"build"`

	// Nav defines sidebar navigation entries.
	Nav []NavItemConfig `json:"nav,omitempty" yaml:"nav,omitempty"`

	// Widgets defines widget descriptors for the dashboard overview.
	Widgets []WidgetConfig `json:"widgets,omitempty" yaml:"widgets,omitempty"`

	// Settings defines settings panel descriptors.
	Settings []SettingConfig `json:"settings,omitempty" yaml:"settings,omitempty"`

	// BridgeFunctions declares Go↔JS bridge functions this contributor uses.
	BridgeFunctions []BridgeFuncConfig `json:"bridge_functions,omitempty" yaml:"bridge_functions,omitempty"`

	// Searchable enables search support for this contributor's pages.
	Searchable bool `json:"searchable,omitempty" yaml:"searchable,omitempty"`
}

// BuildConfig contains framework-specific build configuration.
type BuildConfig struct {
	// Mode is the build mode: "static" (EmbeddedContributor) or "ssr" (SSRContributor).
	Mode string `json:"mode" yaml:"mode"`

	// UIDir is the path to the UI source project relative to the extension root.
	// Default: "ui"
	UIDir string `json:"ui_dir,omitempty" yaml:"ui_dir,omitempty"`

	// DistDir is the build output directory relative to the UI project.
	// Defaults: Astro static="dist", Next.js static="out", Next.js SSR=".next"
	DistDir string `json:"dist_dir,omitempty" yaml:"dist_dir,omitempty"`

	// EmbedPath is the go:embed path relative to the Go package root.
	// Used in generated code for the //go:embed directive.
	// Default: "{UIDir}/{DistDir}"
	EmbedPath string `json:"embed_path,omitempty" yaml:"embed_path,omitempty"`

	// SSREntry is the Node.js entry point for SSR mode.
	// Default: Astro="dist/server/entry.mjs", Next.js=".next/standalone/server.js"
	SSREntry string `json:"ssr_entry,omitempty" yaml:"ssr_entry,omitempty"`

	// BuildCmd overrides the framework's default build command.
	// Default: Astro="npx astro build", Next.js="npx next build"
	BuildCmd string `json:"build_cmd,omitempty" yaml:"build_cmd,omitempty"`

	// DevCmd overrides the framework's default dev command.
	// Default: Astro="npx astro dev", Next.js="npx next dev"
	DevCmd string `json:"dev_cmd,omitempty" yaml:"dev_cmd,omitempty"`

	// PagesDir is the subdirectory within the dist that contains page HTML files.
	// Default: "pages"
	PagesDir string `json:"pages_dir,omitempty" yaml:"pages_dir,omitempty"`

	// WidgetsDir is the subdirectory within the dist that contains widget fragments.
	// Default: "widgets"
	WidgetsDir string `json:"widgets_dir,omitempty" yaml:"widgets_dir,omitempty"`

	// SettingsDir is the subdirectory within the dist that contains settings fragments.
	// Default: "settings"
	SettingsDir string `json:"settings_dir,omitempty" yaml:"settings_dir,omitempty"`

	// AssetsDir is the subdirectory within the dist that contains static assets.
	// Default: "assets"
	AssetsDir string `json:"assets_dir,omitempty" yaml:"assets_dir,omitempty"`
}

// NavItemConfig defines a navigation entry in forge.contributor.yaml.
type NavItemConfig struct {
	Label    string `json:"label"              yaml:"label"`
	Path     string `json:"path"               yaml:"path"`
	Icon     string `json:"icon,omitempty"     yaml:"icon,omitempty"`
	Group    string `json:"group,omitempty"    yaml:"group,omitempty"`
	Priority int    `json:"priority,omitempty" yaml:"priority,omitempty"`
	Access   string `json:"access,omitempty"   yaml:"access,omitempty"`
}

// WidgetConfig defines a widget descriptor in forge.contributor.yaml.
type WidgetConfig struct {
	ID          string `json:"id"                    yaml:"id"`
	Title       string `json:"title"                 yaml:"title"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Size        string `json:"size"                  yaml:"size"`
	RefreshSec  int    `json:"refresh_sec,omitempty" yaml:"refresh_sec,omitempty"`
	Group       string `json:"group,omitempty"       yaml:"group,omitempty"`
	Priority    int    `json:"priority,omitempty"    yaml:"priority,omitempty"`
}

// SettingConfig defines a settings panel descriptor in forge.contributor.yaml.
type SettingConfig struct {
	ID          string `json:"id"                    yaml:"id"`
	Title       string `json:"title"                 yaml:"title"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Group       string `json:"group,omitempty"       yaml:"group,omitempty"`
	Icon        string `json:"icon,omitempty"        yaml:"icon,omitempty"`
	Priority    int    `json:"priority,omitempty"    yaml:"priority,omitempty"`
}

// BridgeFuncConfig declares a bridge function used by this contributor.
type BridgeFuncConfig struct {
	Name        string `json:"name"                  yaml:"name"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// ValidFrameworkTypes is the set of supported framework types.
var ValidFrameworkTypes = map[string]bool{
	"templ":  true,
	"astro":  true,
	"nextjs": true,
	"custom": true,
}

// ValidBuildModes is the set of supported build modes.
var ValidBuildModes = map[string]bool{
	"static": true,
	"ssr":    true,
	"local":  true,
}
