package features

import "time"

// Config holds feature flags extension configuration
type Config struct {
	// Enabled determines if feature flags extension is enabled
	Enabled bool `yaml:"enabled" json:"enabled"`

	// Provider specifies the backend provider
	// Options: "local", "launchdarkly", "unleash", "flagsmith", "posthog"
	Provider string `yaml:"provider" json:"provider"`

	// RefreshInterval is how often to refresh flags from remote provider
	RefreshInterval time.Duration `yaml:"refresh_interval" json:"refresh_interval"`

	// EnableCache enables local caching of flag values
	EnableCache bool `yaml:"enable_cache" json:"enable_cache"`

	// CacheTTL is the cache TTL for flag values
	CacheTTL time.Duration `yaml:"cache_ttl" json:"cache_ttl"`

	// Local provider settings
	Local LocalProviderConfig `yaml:"local" json:"local"`

	// LaunchDarkly provider settings
	LaunchDarkly LaunchDarklyConfig `yaml:"launchdarkly" json:"launchdarkly"`

	// Unleash provider settings
	Unleash UnleashConfig `yaml:"unleash" json:"unleash"`

	// Flagsmith provider settings
	Flagsmith FlagsmithConfig `yaml:"flagsmith" json:"flagsmith"`

	// PostHog provider settings
	PostHog PostHogConfig `yaml:"posthog" json:"posthog"`

	// DefaultFlags are default flag values (used as fallback)
	DefaultFlags map[string]interface{} `yaml:"default_flags" json:"default_flags"`
}

// LocalProviderConfig for local in-memory provider
type LocalProviderConfig struct {
	// Flags are the static flag definitions
	Flags map[string]FlagConfig `yaml:"flags" json:"flags"`
}

// FlagConfig defines a single feature flag
type FlagConfig struct {
	// Key is the unique flag identifier
	Key string `yaml:"key" json:"key"`

	// Name is the human-readable name
	Name string `yaml:"name" json:"name"`

	// Description describes what this flag controls
	Description string `yaml:"description" json:"description"`

	// Type is the flag value type: "boolean", "string", "number", "json"
	Type string `yaml:"type" json:"type"`

	// Enabled is the default enabled state for boolean flags
	Enabled bool `yaml:"enabled" json:"enabled"`

	// Value is the default value for non-boolean flags
	Value interface{} `yaml:"value" json:"value"`

	// Targeting defines user/group targeting rules
	Targeting []TargetingRule `yaml:"targeting" json:"targeting"`

	// Rollout defines percentage-based rollout
	Rollout *RolloutConfig `yaml:"rollout" json:"rollout"`
}

// TargetingRule defines targeting rules for a flag
type TargetingRule struct {
	// Attribute is the user attribute to match (e.g., "user_id", "email", "group")
	Attribute string `yaml:"attribute" json:"attribute"`

	// Operator is the comparison operator: "equals", "contains", "in", "not_in"
	Operator string `yaml:"operator" json:"operator"`

	// Values are the values to match against
	Values []string `yaml:"values" json:"values"`

	// Value is the flag value for matching users
	Value interface{} `yaml:"value" json:"value"`
}

// RolloutConfig defines percentage-based rollout
type RolloutConfig struct {
	// Percentage of users to enable (0-100)
	Percentage int `yaml:"percentage" json:"percentage"`

	// Attribute to use for consistent hashing (default: "user_id")
	Attribute string `yaml:"attribute" json:"attribute"`
}

// LaunchDarklyConfig for LaunchDarkly provider
type LaunchDarklyConfig struct {
	// SDKKey is the LaunchDarkly SDK key
	SDKKey string `yaml:"sdk_key" json:"sdk_key"`

	// Timeout for API requests
	Timeout time.Duration `yaml:"timeout" json:"timeout"`
}

// UnleashConfig for Unleash provider
type UnleashConfig struct {
	// URL is the Unleash API URL
	URL string `yaml:"url" json:"url"`

	// APIToken is the Unleash API token
	APIToken string `yaml:"api_token" json:"api_token"`

	// AppName is the application name
	AppName string `yaml:"app_name" json:"app_name"`

	// Environment is the environment name
	Environment string `yaml:"environment" json:"environment"`

	// InstanceID is a unique instance identifier
	InstanceID string `yaml:"instance_id" json:"instance_id"`
}

// FlagsmithConfig for Flagsmith provider
type FlagsmithConfig struct {
	// APIURL is the Flagsmith API URL
	APIURL string `yaml:"api_url" json:"api_url"`

	// EnvironmentKey is the Flagsmith environment key
	EnvironmentKey string `yaml:"environment_key" json:"environment_key"`
}

// PostHogConfig for PostHog provider
type PostHogConfig struct {
	// APIKey is the PostHog project API key
	APIKey string `yaml:"api_key" json:"api_key"`

	// Host is the PostHog host (default: https://app.posthog.com)
	Host string `yaml:"host" json:"host"`

	// PersonalAPIKey for admin operations (optional)
	PersonalAPIKey string `yaml:"personal_api_key" json:"personal_api_key"`

	// EnableLocalEvaluation enables local flag evaluation (faster)
	EnableLocalEvaluation bool `yaml:"enable_local_evaluation" json:"enable_local_evaluation"`

	// PollingInterval for flag refresh (default: 30s)
	PollingInterval time.Duration `yaml:"polling_interval" json:"polling_interval"`
}

// DefaultConfig returns the default feature flags configuration
func DefaultConfig() Config {
	return Config{
		Enabled:         true,
		Provider:        "local",
		RefreshInterval: 30 * time.Second,
		EnableCache:     true,
		CacheTTL:        5 * time.Minute,
		DefaultFlags:    make(map[string]interface{}),
		Local: LocalProviderConfig{
			Flags: make(map[string]FlagConfig),
		},
	}
}

// ConfigOption configures the feature flags extension
type ConfigOption func(*Config)

// WithEnabled sets whether feature flags is enabled
func WithEnabled(enabled bool) ConfigOption {
	return func(c *Config) {
		c.Enabled = enabled
	}
}

// WithProvider sets the feature flags provider
func WithProvider(provider string) ConfigOption {
	return func(c *Config) {
		c.Provider = provider
	}
}

// WithRefreshInterval sets the refresh interval
func WithRefreshInterval(interval time.Duration) ConfigOption {
	return func(c *Config) {
		c.RefreshInterval = interval
	}
}

// WithCache enables caching with TTL
func WithCache(enabled bool, ttl time.Duration) ConfigOption {
	return func(c *Config) {
		c.EnableCache = enabled
		c.CacheTTL = ttl
	}
}

// WithLocalFlags sets local provider flags
func WithLocalFlags(flags map[string]FlagConfig) ConfigOption {
	return func(c *Config) {
		c.Provider = "local"
		c.Local.Flags = flags
	}
}

// WithLaunchDarkly configures LaunchDarkly provider
func WithLaunchDarkly(sdkKey string) ConfigOption {
	return func(c *Config) {
		c.Provider = "launchdarkly"
		c.LaunchDarkly.SDKKey = sdkKey
	}
}

// WithUnleash configures Unleash provider
func WithUnleash(url, apiToken, appName string) ConfigOption {
	return func(c *Config) {
		c.Provider = "unleash"
		c.Unleash.URL = url
		c.Unleash.APIToken = apiToken
		c.Unleash.AppName = appName
	}
}

// WithFlagsmith configures Flagsmith provider
func WithFlagsmith(apiURL, environmentKey string) ConfigOption {
	return func(c *Config) {
		c.Provider = "flagsmith"
		c.Flagsmith.APIURL = apiURL
		c.Flagsmith.EnvironmentKey = environmentKey
	}
}

// WithPostHog configures PostHog provider
func WithPostHog(apiKey, host string) ConfigOption {
	return func(c *Config) {
		c.Provider = "posthog"
		c.PostHog.APIKey = apiKey
		c.PostHog.Host = host
	}
}

// WithDefaultFlags sets default flag values
func WithDefaultFlags(flags map[string]interface{}) ConfigOption {
	return func(c *Config) {
		c.DefaultFlags = flags
	}
}

// WithConfig sets the complete config
func WithConfig(config Config) ConfigOption {
	return func(c *Config) {
		*c = config
	}
}
