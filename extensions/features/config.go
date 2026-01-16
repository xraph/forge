package features

import "time"

// DI container keys for features extension services.
const (
	// ServiceKey is the DI key for the features service.
	ServiceKey = "features"
	// ServiceKeyLegacy is the legacy DI key for the features service.
	ServiceKeyLegacy = "features.Service"
)

// Config holds feature flags extension configuration.
type Config struct {
	// Enabled determines if feature flags extension is enabled
	Enabled bool `json:"enabled" yaml:"enabled"`

	// Provider specifies the backend provider
	// Options: "local", "launchdarkly", "unleash", "flagsmith", "posthog"
	Provider string `json:"provider" yaml:"provider"`

	// RefreshInterval is how often to refresh flags from remote provider
	RefreshInterval time.Duration `json:"refresh_interval" yaml:"refresh_interval"`

	// EnableCache enables local caching of flag values
	EnableCache bool `json:"enable_cache" yaml:"enable_cache"`

	// CacheTTL is the cache TTL for flag values
	CacheTTL time.Duration `json:"cache_ttl" yaml:"cache_ttl"`

	// Local provider settings
	Local LocalProviderConfig `json:"local" yaml:"local"`

	// LaunchDarkly provider settings
	LaunchDarkly LaunchDarklyConfig `json:"launchdarkly" yaml:"launchdarkly"`

	// Unleash provider settings
	Unleash UnleashConfig `json:"unleash" yaml:"unleash"`

	// Flagsmith provider settings
	Flagsmith FlagsmithConfig `json:"flagsmith" yaml:"flagsmith"`

	// PostHog provider settings
	PostHog PostHogConfig `json:"posthog" yaml:"posthog"`

	// DefaultFlags are default flag values (used as fallback)
	DefaultFlags map[string]any `json:"default_flags" yaml:"default_flags"`
}

// LocalProviderConfig for local in-memory provider.
type LocalProviderConfig struct {
	// Flags are the static flag definitions
	Flags map[string]FlagConfig `json:"flags" yaml:"flags"`
}

// FlagConfig defines a single feature flag.
type FlagConfig struct {
	// Key is the unique flag identifier
	Key string `json:"key" yaml:"key"`

	// Name is the human-readable name
	Name string `json:"name" yaml:"name"`

	// Description describes what this flag controls
	Description string `json:"description" yaml:"description"`

	// Type is the flag value type: "boolean", "string", "number", "json"
	Type string `json:"type" yaml:"type"`

	// Enabled is the default enabled state for boolean flags
	Enabled bool `json:"enabled" yaml:"enabled"`

	// Value is the default value for non-boolean flags
	Value any `json:"value" yaml:"value"`

	// Targeting defines user/group targeting rules
	Targeting []TargetingRule `json:"targeting" yaml:"targeting"`

	// Rollout defines percentage-based rollout
	Rollout *RolloutConfig `json:"rollout" yaml:"rollout"`
}

// TargetingRule defines targeting rules for a flag.
type TargetingRule struct {
	// Attribute is the user attribute to match (e.g., "user_id", "email", "group")
	Attribute string `json:"attribute" yaml:"attribute"`

	// Operator is the comparison operator: "equals", "contains", "in", "not_in"
	Operator string `json:"operator" yaml:"operator"`

	// Values are the values to match against
	Values []string `json:"values" yaml:"values"`

	// Value is the flag value for matching users
	Value any `json:"value" yaml:"value"`
}

// RolloutConfig defines percentage-based rollout.
type RolloutConfig struct {
	// Percentage of users to enable (0-100)
	Percentage int `json:"percentage" yaml:"percentage"`

	// Attribute to use for consistent hashing (default: "user_id")
	Attribute string `json:"attribute" yaml:"attribute"`
}

// LaunchDarklyConfig for LaunchDarkly provider.
type LaunchDarklyConfig struct {
	// SDKKey is the LaunchDarkly SDK key
	SDKKey string `json:"sdk_key" yaml:"sdk_key"`

	// Timeout for API requests
	Timeout time.Duration `json:"timeout" yaml:"timeout"`
}

// UnleashConfig for Unleash provider.
type UnleashConfig struct {
	// URL is the Unleash API URL
	URL string `json:"url" yaml:"url"`

	// APIToken is the Unleash API token
	APIToken string `json:"api_token" yaml:"api_token"`

	// AppName is the application name
	AppName string `json:"app_name" yaml:"app_name"`

	// Environment is the environment name
	Environment string `json:"environment" yaml:"environment"`

	// InstanceID is a unique instance identifier
	InstanceID string `json:"instance_id" yaml:"instance_id"`
}

// FlagsmithConfig for Flagsmith provider.
type FlagsmithConfig struct {
	// APIURL is the Flagsmith API URL
	APIURL string `json:"api_url" yaml:"api_url"`

	// EnvironmentKey is the Flagsmith environment key
	EnvironmentKey string `json:"environment_key" yaml:"environment_key"`
}

// PostHogConfig for PostHog provider.
type PostHogConfig struct {
	// APIKey is the PostHog project API key
	APIKey string `json:"api_key" yaml:"api_key"`

	// Host is the PostHog host (default: https://app.posthog.com)
	Host string `json:"host" yaml:"host"`

	// PersonalAPIKey for admin operations (optional)
	PersonalAPIKey string `json:"personal_api_key" yaml:"personal_api_key"`

	// EnableLocalEvaluation enables local flag evaluation (faster)
	EnableLocalEvaluation bool `json:"enable_local_evaluation" yaml:"enable_local_evaluation"`

	// PollingInterval for flag refresh (default: 30s)
	PollingInterval time.Duration `json:"polling_interval" yaml:"polling_interval"`
}

// DefaultConfig returns the default feature flags configuration.
func DefaultConfig() Config {
	return Config{
		Enabled:         true,
		Provider:        "local",
		RefreshInterval: 30 * time.Second,
		EnableCache:     true,
		CacheTTL:        5 * time.Minute,
		DefaultFlags:    make(map[string]any),
		Local: LocalProviderConfig{
			Flags: make(map[string]FlagConfig),
		},
	}
}

// ConfigOption configures the feature flags extension.
type ConfigOption func(*Config)

// WithEnabled sets whether feature flags is enabled.
func WithEnabled(enabled bool) ConfigOption {
	return func(c *Config) {
		c.Enabled = enabled
	}
}

// WithProvider sets the feature flags provider.
func WithProvider(provider string) ConfigOption {
	return func(c *Config) {
		c.Provider = provider
	}
}

// WithRefreshInterval sets the refresh interval.
func WithRefreshInterval(interval time.Duration) ConfigOption {
	return func(c *Config) {
		c.RefreshInterval = interval
	}
}

// WithCache enables caching with TTL.
func WithCache(enabled bool, ttl time.Duration) ConfigOption {
	return func(c *Config) {
		c.EnableCache = enabled
		c.CacheTTL = ttl
	}
}

// WithLocalFlags sets local provider flags.
func WithLocalFlags(flags map[string]FlagConfig) ConfigOption {
	return func(c *Config) {
		c.Provider = "local"
		c.Local.Flags = flags
	}
}

// WithLaunchDarkly configures LaunchDarkly provider.
func WithLaunchDarkly(sdkKey string) ConfigOption {
	return func(c *Config) {
		c.Provider = "launchdarkly"
		c.LaunchDarkly.SDKKey = sdkKey
	}
}

// WithUnleash configures Unleash provider.
func WithUnleash(url, apiToken, appName string) ConfigOption {
	return func(c *Config) {
		c.Provider = "unleash"
		c.Unleash.URL = url
		c.Unleash.APIToken = apiToken
		c.Unleash.AppName = appName
	}
}

// WithFlagsmith configures Flagsmith provider.
func WithFlagsmith(apiURL, environmentKey string) ConfigOption {
	return func(c *Config) {
		c.Provider = "flagsmith"
		c.Flagsmith.APIURL = apiURL
		c.Flagsmith.EnvironmentKey = environmentKey
	}
}

// WithPostHog configures PostHog provider.
func WithPostHog(apiKey, host string) ConfigOption {
	return func(c *Config) {
		c.Provider = "posthog"
		c.PostHog.APIKey = apiKey
		c.PostHog.Host = host
	}
}

// WithDefaultFlags sets default flag values.
func WithDefaultFlags(flags map[string]any) ConfigOption {
	return func(c *Config) {
		c.DefaultFlags = flags
	}
}

// WithConfig sets the complete config.
func WithConfig(config Config) ConfigOption {
	return func(c *Config) {
		*c = config
	}
}
