// Package theme provides a dashboard-specific wrapper around the forgeui theme system.
// It adds dark mode cookie persistence, custom CSS injection, and theme configuration
// tailored to the Forge dashboard shell.
package theme

import (
	g "maragu.dev/gomponents"
	"maragu.dev/gomponents/html"

	"github.com/xraph/forgeui/theme"
)

// ThemeConfig holds dashboard-specific theme configuration.
type ThemeConfig struct {
	Mode      string // "light", "dark", "auto"
	CustomCSS string // user-provided custom CSS to inject
}

// DefaultThemeConfig returns the default theme config.
func DefaultThemeConfig() ThemeConfig {
	return ThemeConfig{
		Mode: "auto",
	}
}

// Manager handles theme rendering for the dashboard shell.
// It wraps the forgeui theme package and adds dashboard-specific
// functionality such as custom CSS injection and body class selection.
type Manager struct {
	config ThemeConfig
}

// NewManager creates a new theme manager with the given configuration.
func NewManager(config ThemeConfig) *Manager {
	return &Manager{config: config}
}

// HeadNodes returns all theme-related nodes to include in <head>.
// This includes: meta tags, CSS variable definitions, Tailwind config,
// dark mode flash prevention script, and optional custom CSS.
func (m *Manager) HeadNodes() []g.Node {
	light := theme.DefaultLight()
	dark := theme.DefaultDark()

	nodes := []g.Node{
		theme.HeadContent(light, dark),
		theme.StyleTag(light, dark),
		theme.TailwindConfigScript(),
		theme.DarkModeScript(),
	}

	if css := m.CustomCSSNode(); css != nil {
		nodes = append(nodes, css)
	}

	return nodes
}

// CustomCSSNode returns a <style> tag with custom CSS, or nil if no custom CSS is configured.
func (m *Manager) CustomCSSNode() g.Node {
	if m.config.CustomCSS == "" {
		return nil
	}
	return html.StyleEl(g.Raw(m.config.CustomCSS))
}

// ThemeBodyClass returns CSS classes for the <body> tag based on the theme mode.
//   - "light" mode: returns "" (no dark class needed)
//   - "dark" mode: returns "dark" (forces dark theme)
//   - "auto" mode: returns "" (Alpine.js / DarkModeScript handles the toggle at runtime)
func (m *Manager) ThemeBodyClass() string {
	if m.config.Mode == "dark" {
		return "dark"
	}
	return ""
}
