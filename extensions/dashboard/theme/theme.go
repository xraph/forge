// Package theme provides a dashboard-specific wrapper around the forgeui theme system.
// It stores theme configuration (mode, custom CSS) for the Forge dashboard shell.
package theme

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

// Manager handles theme configuration for the dashboard shell.
type Manager struct {
	config ThemeConfig
}

// NewManager creates a new theme manager with the given configuration.
func NewManager(config ThemeConfig) *Manager {
	return &Manager{config: config}
}

// Config returns the current theme configuration.
func (m *Manager) Config() ThemeConfig {
	return m.config
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
