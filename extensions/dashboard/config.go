package dashboard

import (
	"fmt"
	"time"
)

// Config contains dashboard configuration
type Config struct {
	// Server settings
	Port            int           `yaml:"port" json:"port"`
	BasePath        string        `yaml:"base_path" json:"base_path"`
	ReadTimeout     time.Duration `yaml:"read_timeout" json:"read_timeout"`
	WriteTimeout    time.Duration `yaml:"write_timeout" json:"write_timeout"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout" json:"shutdown_timeout"`

	// Features
	EnableAuth     bool     `yaml:"enable_auth" json:"enable_auth"`
	EnableRealtime bool     `yaml:"enable_realtime" json:"enable_realtime"`
	EnableExport   bool     `yaml:"enable_export" json:"enable_export"`
	ExportFormats  []string `yaml:"export_formats" json:"export_formats"`

	// Data collection
	RefreshInterval time.Duration `yaml:"refresh_interval" json:"refresh_interval"`
	HistoryDuration time.Duration `yaml:"history_duration" json:"history_duration"`
	MaxDataPoints   int           `yaml:"max_data_points" json:"max_data_points"`

	// UI settings
	Theme     string            `yaml:"theme" json:"theme"` // light, dark, auto
	Title     string            `yaml:"title" json:"title"`
	CustomCSS string            `yaml:"custom_css" json:"custom_css"`
	CustomJS  string            `yaml:"custom_js" json:"custom_js"`
	MetaTags  map[string]string `yaml:"meta_tags" json:"meta_tags"`

	// Internal
	RequireConfig bool `yaml:"-" json:"-"`
}

// DefaultConfig returns default dashboard configuration
func DefaultConfig() Config {
	return Config{
		Port:            8080,
		BasePath:        "/dashboard",
		ReadTimeout:     30 * time.Second,
		WriteTimeout:    30 * time.Second,
		ShutdownTimeout: 10 * time.Second,

		EnableAuth:     false,
		EnableRealtime: true,
		EnableExport:   true,
		ExportFormats:  []string{"json", "csv", "prometheus"},

		RefreshInterval: 30 * time.Second,
		HistoryDuration: 1 * time.Hour,
		MaxDataPoints:   1000,

		Theme:    "auto",
		Title:    "Forge Dashboard",
		MetaTags: make(map[string]string),

		RequireConfig: false,
	}
}

// Validate validates the configuration
func (c Config) Validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("invalid port: %d (must be 1-65535)", c.Port)
	}

	if c.RefreshInterval < time.Second {
		return fmt.Errorf("refresh interval too short: %v (minimum 1s)", c.RefreshInterval)
	}

	if c.MaxDataPoints < 10 {
		return fmt.Errorf("max data points too low: %d (minimum 10)", c.MaxDataPoints)
	}

	validThemes := map[string]bool{"light": true, "dark": true, "auto": true}
	if !validThemes[c.Theme] {
		return fmt.Errorf("invalid theme: %s (must be light, dark, or auto)", c.Theme)
	}

	return nil
}

// ConfigOption is a functional option for Config
type ConfigOption func(*Config)

// WithPort sets the server port
func WithPort(port int) ConfigOption {
	return func(c *Config) {
		c.Port = port
	}
}

// WithBasePath sets the base URL path
func WithBasePath(path string) ConfigOption {
	return func(c *Config) {
		c.BasePath = path
	}
}

// WithAuth enables authentication
func WithAuth(enabled bool) ConfigOption {
	return func(c *Config) {
		c.EnableAuth = enabled
	}
}

// WithRealtime enables real-time updates
func WithRealtime(enabled bool) ConfigOption {
	return func(c *Config) {
		c.EnableRealtime = enabled
	}
}

// WithRefreshInterval sets the refresh interval
func WithRefreshInterval(interval time.Duration) ConfigOption {
	return func(c *Config) {
		c.RefreshInterval = interval
	}
}

// WithTheme sets the UI theme
func WithTheme(theme string) ConfigOption {
	return func(c *Config) {
		c.Theme = theme
	}
}

// WithTitle sets the dashboard title
func WithTitle(title string) ConfigOption {
	return func(c *Config) {
		c.Title = title
	}
}

// WithConfig sets the complete config
func WithConfig(config Config) ConfigOption {
	return func(c *Config) {
		*c = config
	}
}

// WithRequireConfig requires config from ConfigManager
func WithRequireConfig(required bool) ConfigOption {
	return func(c *Config) {
		c.RequireConfig = required
	}
}

// WithExport enables/disables export functionality
func WithExport(enabled bool) ConfigOption {
	return func(c *Config) {
		c.EnableExport = enabled
	}
}

// WithHistoryDuration sets the data retention duration
func WithHistoryDuration(duration time.Duration) ConfigOption {
	return func(c *Config) {
		c.HistoryDuration = duration
	}
}

// WithMaxDataPoints sets the maximum number of data points to retain
func WithMaxDataPoints(max int) ConfigOption {
	return func(c *Config) {
		c.MaxDataPoints = max
	}
}
