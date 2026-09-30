package dashboard

import (
	"errors"
	"fmt"
	"time"
)

// MemoryProfile controls memory usage tuning for the dashboard.
// Use "low" for 512MB systems, "medium" for 1-2GB, "high" for 4GB+.
type MemoryProfile string

const (
	// MemoryProfileLow tunes for 512MB systems with minimal history and trace retention.
	MemoryProfileLow MemoryProfile = "low"
	// MemoryProfileMedium is the default, suitable for 1-2GB systems.
	MemoryProfileMedium MemoryProfile = "medium"
	// MemoryProfileHigh allows maximum history and trace retention for 4GB+ systems.
	MemoryProfileHigh MemoryProfile = "high"
)

// Config contains dashboard extension configuration.
type Config struct {
	// BasePath prefixes every route the extension mounts.
	BasePath string `json:"base_path" yaml:"base_path"`

	// Data collection
	RefreshInterval time.Duration `json:"refresh_interval" yaml:"refresh_interval"`
	HistoryDuration time.Duration `json:"history_duration" yaml:"history_duration"`
	MaxDataPoints   int           `json:"max_data_points"  yaml:"max_data_points"`

	// Tracing
	TraceMaxCount  int           `json:"trace_max_count"  yaml:"trace_max_count"`
	TraceRetention time.Duration `json:"trace_retention"  yaml:"trace_retention"`
	// TraceMaxSpansPerTrace caps how many spans one trace retains. A single
	// long-lived trace, such as a websocket, would otherwise grow unbounded.
	TraceMaxSpansPerTrace int `json:"trace_max_spans_per_trace" yaml:"trace_max_spans_per_trace"`
	// TraceIdleTTL is how long after the last dashboard request spans keep being
	// retained. A negative duration disables the gate, retaining always (the
	// pre-gate behaviour). Zero is not a reliable way to disable it: under a
	// ConfigManager, config merging (see extension_config.go) only overrides a
	// field when the source value is non-zero, so an explicit 0 here is
	// silently skipped and the default survives.
	TraceIdleTTL time.Duration `json:"trace_idle_ttl" yaml:"trace_idle_ttl"`
	// TraceCaptureRequestBody retains a bounded, redacted JSON request body in
	// the in-memory trace store. It is opt-in because arbitrary fields may hold
	// private data even after credential-shaped keys are removed.
	TraceCaptureRequestBody bool `json:"trace_capture_request_body" yaml:"trace_capture_request_body"`

	// Security
	EnableCSRF bool `json:"enable_csrf" yaml:"enable_csrf"`
	// EnableContractSecurity gates CSRF validation, idempotency dedup, and
	// distributed tracing on the contract envelope endpoint. Default true;
	// set to false during a rollout window where clients have not yet
	// adopted CSRF tokens or the idempotency-key contract.
	EnableContractSecurity bool `json:"enable_contract_security" yaml:"enable_contract_security"`
	// ContractMaxBodyBytes caps the contract envelope a client may POST.
	// Zero or less means transport.DefaultMaxBodyBytes (1 MiB).
	ContractMaxBodyBytes int64 `json:"contract_max_body_bytes" yaml:"contract_max_body_bytes"`

	// Authentication
	EnableAuth bool `json:"enable_auth" yaml:"enable_auth"`
	// LoginPath is where a signed-out user is sent, relative to BasePath. The
	// dashboard serves no login page itself: the principal endpoint reports
	// this path in its 401 so the client knows where to redirect.
	LoginPath string `json:"login_path" yaml:"login_path"`
	// RequiredRoles, when non-empty, restricts dashboard access to users
	// carrying at least one matching role. The principal endpoint returns
	// 403 PERMISSION_DENIED for users who don't qualify. Rendering that as
	// an "access denied" screen is the client's job.
	RequiredRoles []string `json:"required_roles" yaml:"required_roles"`

	// Export
	EnableExport  bool     `json:"enable_export"  yaml:"enable_export"`
	ExportFormats []string `json:"export_formats" yaml:"export_formats"`

	// Internal
	RequireConfig bool `json:"-" yaml:"-"`
}

// DefaultConfig returns the default dashboard configuration.
func DefaultConfig() Config {
	return Config{
		BasePath: "/dashboard",

		EnableExport: true,

		RefreshInterval: 30 * time.Second,
		HistoryDuration: 30 * time.Minute,
		MaxDataPoints:   120,

		TraceMaxCount:         200,
		TraceRetention:        30 * time.Minute,
		TraceMaxSpansPerTrace: 200,
		TraceIdleTTL:          5 * time.Minute,

		EnableCSRF:             true,
		EnableContractSecurity: true,

		EnableAuth: false,
		LoginPath:  "/login",

		ExportFormats: []string{"json", "csv", "prometheus"},

		RequireConfig: false,
	}
}

// Validate validates the configuration.
func (c Config) Validate() error {
	if c.BasePath == "" {
		return errors.New("dashboard: base_path cannot be empty")
	}

	if c.RefreshInterval < time.Second {
		return fmt.Errorf("dashboard: refresh_interval too short: %v (minimum 1s)", c.RefreshInterval)
	}

	if c.MaxDataPoints < 10 {
		return fmt.Errorf("dashboard: max_data_points too low: %d (minimum 10)", c.MaxDataPoints)
	}

	return nil
}

// ConfigOption is a functional option for Config.
type ConfigOption func(*Config)

// WithBasePath sets the base URL path for the dashboard.
func WithBasePath(path string) ConfigOption {
	return func(c *Config) { c.BasePath = path }
}

// WithExport enables or disables export functionality.
func WithExport(enabled bool) ConfigOption {
	return func(c *Config) { c.EnableExport = enabled }
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

// WithTraceMaxCount sets the maximum number of traces kept in memory.
func WithTraceMaxCount(count int) ConfigOption {
	return func(c *Config) { c.TraceMaxCount = count }
}

// WithTraceRetention sets the retention duration for traces.
func WithTraceRetention(duration time.Duration) ConfigOption {
	return func(c *Config) { c.TraceRetention = duration }
}

// WithTraceMaxSpansPerTrace sets how many spans a single trace retains.
func WithTraceMaxSpansPerTrace(n int) ConfigOption {
	return func(c *Config) { c.TraceMaxSpansPerTrace = n }
}

// WithTraceIdleTTL sets how long after the last dashboard request traces keep
// being collected. Pass a negative duration to collect always, disabling the
// gate. Passing zero is not reliable for this: under a ConfigManager, config
// merging only overrides a field when the source value is non-zero, so an
// explicit zero here can be silently skipped in favor of the existing value.
func WithTraceIdleTTL(duration time.Duration) ConfigOption {
	return func(c *Config) { c.TraceIdleTTL = duration }
}

// WithTraceCaptureRequestBody enables bounded JSON request-body inspection.
func WithTraceCaptureRequestBody(enabled bool) ConfigOption {
	return func(c *Config) { c.TraceCaptureRequestBody = enabled }
}

// WithCSRF enables or disables CSRF token protection.
func WithCSRF(enabled bool) ConfigOption {
	return func(c *Config) { c.EnableCSRF = enabled }
}

// WithContractSecurity enables or disables the contract envelope's
// security stack (CSRF validation, idempotency dedup, request tracing).
// Defaults to true; switching off should be reserved for rollout windows
// where clients have not yet adopted CSRF tokens or idempotency keys.
func WithContractSecurity(enabled bool) ConfigOption {
	return func(c *Config) { c.EnableContractSecurity = enabled }
}

// WithContractMaxBodyBytes caps the contract envelope a client may POST.
// Larger bodies are refused with 413 before they are decoded. Zero or less
// keeps the 1 MiB default.
func WithContractMaxBodyBytes(n int64) ConfigOption {
	return func(c *Config) { c.ContractMaxBodyBytes = n }
}

// WithExportFormats sets the supported export formats.
func WithExportFormats(formats []string) ConfigOption {
	return func(c *Config) { c.ExportFormats = formats }
}

// WithEnableAuth enables or disables authentication support.
func WithEnableAuth(enabled bool) ConfigOption {
	return func(c *Config) { c.EnableAuth = enabled }
}

// WithRequiredRoles restricts dashboard access to users carrying at least
// one of the given roles. Pass nil/empty to allow all authenticated users.
// Auth extensions (e.g. authsome) call this via Extension.SetRequiredRoles
// when their config declares a role gate; deployments can also configure
// it directly via this option.
func WithRequiredRoles(roles []string) ConfigOption {
	return func(c *Config) { c.RequiredRoles = append([]string(nil), roles...) }
}

// WithLoginPath sets the path, relative to BasePath, that the principal
// endpoint reports for signed-out users (e.g. "/auth/login").
func WithLoginPath(path string) ConfigOption {
	return func(c *Config) { c.LoginPath = path }
}

// WithMemoryProfile auto-tunes data collection and retention settings based
// on the available system memory. Individual settings applied after this
// option will override the profile defaults.
func WithMemoryProfile(profile MemoryProfile) ConfigOption {
	return func(c *Config) {
		switch profile {
		case MemoryProfileLow:
			c.MaxDataPoints = 60
			c.HistoryDuration = 15 * time.Minute
			c.TraceMaxCount = 50
			c.TraceRetention = 10 * time.Minute
			c.TraceMaxSpansPerTrace = 50
			c.TraceIdleTTL = 1 * time.Minute
		case MemoryProfileHigh:
			c.MaxDataPoints = 500
			c.HistoryDuration = 1 * time.Hour
			c.TraceMaxCount = 1000
			c.TraceRetention = 1 * time.Hour
			c.TraceMaxSpansPerTrace = 500
			c.TraceIdleTTL = 15 * time.Minute
		default: // medium — same as DefaultConfig
			c.MaxDataPoints = 120
			c.HistoryDuration = 30 * time.Minute
			c.TraceMaxCount = 200
			c.TraceRetention = 30 * time.Minute
			c.TraceMaxSpansPerTrace = 200
			c.TraceIdleTTL = 5 * time.Minute
		}
	}
}

// WithConfig sets the complete config.
func WithConfig(config Config) ConfigOption {
	return func(c *Config) { *c = config }
}

// WithRequireConfig requires config from ConfigManager.
func WithRequireConfig(required bool) ConfigOption {
	return func(c *Config) { c.RequireConfig = required }
}
