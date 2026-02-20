package dashboard

import (
	"fmt"
	"time"
)

// Config contains dashboard extension configuration.
type Config struct {
	// Server settings
	BasePath string `json:"base_path" yaml:"base_path"`
	Title    string `json:"title"     yaml:"title"`

	// Features
	EnableRealtime  bool `json:"enable_realtime"  yaml:"enable_realtime"` // SSE real-time updates
	EnableExport    bool `json:"enable_export"    yaml:"enable_export"`
	EnableSearch    bool `json:"enable_search"    yaml:"enable_search"`
	EnableSettings  bool `json:"enable_settings"  yaml:"enable_settings"`
	EnableDiscovery bool `json:"enable_discovery" yaml:"enable_discovery"` // auto-discover remote contributors
	EnableBridge    bool `json:"enable_bridge"    yaml:"enable_bridge"`    // Go↔JS bridge function system

	// Data collection
	RefreshInterval time.Duration `json:"refresh_interval" yaml:"refresh_interval"`
	HistoryDuration time.Duration `json:"history_duration" yaml:"history_duration"`
	MaxDataPoints   int           `json:"max_data_points"  yaml:"max_data_points"`

	// Proxy/Remote
	ProxyTimeout time.Duration `json:"proxy_timeout" yaml:"proxy_timeout"`
	CacheMaxSize int           `json:"cache_max_size" yaml:"cache_max_size"`
	CacheTTL     time.Duration `json:"cache_ttl"      yaml:"cache_ttl"`

	// SSE
	SSEKeepAlive time.Duration `json:"sse_keep_alive" yaml:"sse_keep_alive"`

	// Security
	EnableCSP  bool `json:"enable_csp"  yaml:"enable_csp"`
	EnableCSRF bool `json:"enable_csrf" yaml:"enable_csrf"`

	// Authentication
	EnableAuth    bool   `json:"enable_auth"     yaml:"enable_auth"`     // enable auth support
	LoginPath     string `json:"login_path"      yaml:"login_path"`     // relative auth login path (e.g. "/auth/login")
	LogoutPath    string `json:"logout_path"     yaml:"logout_path"`    // relative auth logout path (e.g. "/auth/logout")
	DefaultAccess string `json:"default_access"  yaml:"default_access"` // "public", "protected", "partial"

	// Theming
	Theme     string `json:"theme"      yaml:"theme"` // light, dark, auto
	CustomCSS string `json:"custom_css" yaml:"custom_css"`

	// Discovery
	DiscoveryTag          string        `json:"discovery_tag"           yaml:"discovery_tag"`
	DiscoveryPollInterval time.Duration `json:"discovery_poll_interval" yaml:"discovery_poll_interval"`

	// Export
	ExportFormats []string `json:"export_formats" yaml:"export_formats"`

	// Internal
	RequireConfig bool `json:"-" yaml:"-"`
}

// DefaultConfig returns the default dashboard configuration.
func DefaultConfig() Config {
	return Config{
		BasePath: "/dashboard",
		Title:    "Forge Dashboard",

		EnableRealtime:  true,
		EnableExport:    true,
		EnableSearch:    true,
		EnableSettings:  true,
		EnableDiscovery: false,
		EnableBridge:    true,

		RefreshInterval: 30 * time.Second,
		HistoryDuration: 1 * time.Hour,
		MaxDataPoints:   1000,

		ProxyTimeout: 10 * time.Second,
		CacheMaxSize: 1000,
		CacheTTL:     30 * time.Second,

		SSEKeepAlive: 15 * time.Second,

		EnableCSP:  true,
		EnableCSRF: true,

		EnableAuth:    false,
		LoginPath:     "/auth/login",
		LogoutPath:    "/auth/logout",
		DefaultAccess: "public",

		Theme: "auto",

		DiscoveryTag:          "forge-dashboard-contributor",
		DiscoveryPollInterval: 60 * time.Second,

		ExportFormats: []string{"json", "csv", "prometheus"},

		RequireConfig: false,
	}
}

// Validate validates the configuration.
func (c Config) Validate() error {
	if c.BasePath == "" {
		return fmt.Errorf("dashboard: base_path cannot be empty")
	}

	if c.RefreshInterval < time.Second {
		return fmt.Errorf("dashboard: refresh_interval too short: %v (minimum 1s)", c.RefreshInterval)
	}

	if c.MaxDataPoints < 10 {
		return fmt.Errorf("dashboard: max_data_points too low: %d (minimum 10)", c.MaxDataPoints)
	}

	validThemes := map[string]bool{"light": true, "dark": true, "auto": true}
	if !validThemes[c.Theme] {
		return fmt.Errorf("dashboard: invalid theme: %s (must be light, dark, or auto)", c.Theme)
	}

	if c.ProxyTimeout < time.Second {
		return fmt.Errorf("dashboard: proxy_timeout too short: %v (minimum 1s)", c.ProxyTimeout)
	}

	if c.CacheMaxSize < 0 {
		return fmt.Errorf("dashboard: cache_max_size cannot be negative: %d", c.CacheMaxSize)
	}

	if c.EnableAuth {
		validAccess := map[string]bool{"public": true, "protected": true, "partial": true}
		if c.DefaultAccess != "" && !validAccess[c.DefaultAccess] {
			return fmt.Errorf("dashboard: invalid default_access: %s (must be public, protected, or partial)", c.DefaultAccess)
		}
	}

	return nil
}

// ConfigOption is a functional option for Config.
type ConfigOption func(*Config)

// WithBasePath sets the base URL path for the dashboard.
func WithBasePath(path string) ConfigOption {
	return func(c *Config) { c.BasePath = path }
}

// WithTitle sets the dashboard title.
func WithTitle(title string) ConfigOption {
	return func(c *Config) { c.Title = title }
}

// WithRealtime enables or disables real-time SSE updates.
func WithRealtime(enabled bool) ConfigOption {
	return func(c *Config) { c.EnableRealtime = enabled }
}

// WithExport enables or disables export functionality.
func WithExport(enabled bool) ConfigOption {
	return func(c *Config) { c.EnableExport = enabled }
}

// WithSearch enables or disables global search.
func WithSearch(enabled bool) ConfigOption {
	return func(c *Config) { c.EnableSearch = enabled }
}

// WithSettings enables or disables aggregated settings pages.
func WithSettings(enabled bool) ConfigOption {
	return func(c *Config) { c.EnableSettings = enabled }
}

// WithDiscovery enables or disables automatic service discovery of remote contributors.
func WithDiscovery(enabled bool) ConfigOption {
	return func(c *Config) { c.EnableDiscovery = enabled }
}

// WithBridge enables or disables the Go↔JS bridge function system.
func WithBridge(enabled bool) ConfigOption {
	return func(c *Config) { c.EnableBridge = enabled }
}

// WithRefreshInterval sets the data collection refresh interval.
func WithRefreshInterval(interval time.Duration) ConfigOption {
	return func(c *Config) { c.RefreshInterval = interval }
}

// WithHistoryDuration sets the data retention duration.
func WithHistoryDuration(duration time.Duration) ConfigOption {
	return func(c *Config) { c.HistoryDuration = duration }
}

// WithMaxDataPoints sets the maximum number of data points to retain.
func WithMaxDataPoints(maxPoints int) ConfigOption {
	return func(c *Config) { c.MaxDataPoints = maxPoints }
}

// WithProxyTimeout sets the timeout for proxying requests to remote contributors.
func WithProxyTimeout(timeout time.Duration) ConfigOption {
	return func(c *Config) { c.ProxyTimeout = timeout }
}

// WithCacheMaxSize sets the maximum number of cached fragments.
func WithCacheMaxSize(size int) ConfigOption {
	return func(c *Config) { c.CacheMaxSize = size }
}

// WithCacheTTL sets the time-to-live for cached fragments.
func WithCacheTTL(ttl time.Duration) ConfigOption {
	return func(c *Config) { c.CacheTTL = ttl }
}

// WithSSEKeepAlive sets the SSE keep-alive interval.
func WithSSEKeepAlive(interval time.Duration) ConfigOption {
	return func(c *Config) { c.SSEKeepAlive = interval }
}

// WithCSP enables or disables Content-Security-Policy headers.
func WithCSP(enabled bool) ConfigOption {
	return func(c *Config) { c.EnableCSP = enabled }
}

// WithCSRF enables or disables CSRF token protection.
func WithCSRF(enabled bool) ConfigOption {
	return func(c *Config) { c.EnableCSRF = enabled }
}

// WithTheme sets the UI theme (light, dark, auto).
func WithTheme(theme string) ConfigOption {
	return func(c *Config) { c.Theme = theme }
}

// WithCustomCSS sets custom CSS to inject into the dashboard.
func WithCustomCSS(css string) ConfigOption {
	return func(c *Config) { c.CustomCSS = css }
}

// WithDiscoveryTag sets the service discovery tag to filter dashboard contributors.
func WithDiscoveryTag(tag string) ConfigOption {
	return func(c *Config) { c.DiscoveryTag = tag }
}

// WithDiscoveryPollInterval sets how often to poll for new contributors via discovery.
func WithDiscoveryPollInterval(interval time.Duration) ConfigOption {
	return func(c *Config) { c.DiscoveryPollInterval = interval }
}

// WithExportFormats sets the supported export formats.
func WithExportFormats(formats []string) ConfigOption {
	return func(c *Config) { c.ExportFormats = formats }
}

// WithEnableAuth enables or disables authentication support.
func WithEnableAuth(enabled bool) ConfigOption {
	return func(c *Config) { c.EnableAuth = enabled }
}

// WithLoginPath sets the relative login page path (e.g. "/auth/login").
func WithLoginPath(path string) ConfigOption {
	return func(c *Config) { c.LoginPath = path }
}

// WithLogoutPath sets the relative logout page path (e.g. "/auth/logout").
func WithLogoutPath(path string) ConfigOption {
	return func(c *Config) { c.LogoutPath = path }
}

// WithDefaultAccess sets the default access level for pages ("public", "protected", "partial").
func WithDefaultAccess(access string) ConfigOption {
	return func(c *Config) { c.DefaultAccess = access }
}

// WithConfig sets the complete config.
func WithConfig(config Config) ConfigOption {
	return func(c *Config) { *c = config }
}

// WithRequireConfig requires config from ConfigManager.
func WithRequireConfig(required bool) ConfigOption {
	return func(c *Config) { c.RequireConfig = required }
}
