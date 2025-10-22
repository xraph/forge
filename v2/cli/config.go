package cli

import (
	"fmt"
	"time"
)

// CLIConfig contains configuration for the CLI application
type CLIConfig struct {
	Name           string               `yaml:"name" json:"name"`
	Description    string               `yaml:"description" json:"description"`
	Version        string               `yaml:"version" json:"version"`
	ConfigPaths    []string             `yaml:"config_paths" json:"config_paths"`
	LogLevel       string               `yaml:"log_level" json:"log_level"`
	Middleware     []MiddlewareConfig   `yaml:"middleware" json:"middleware"`
	Plugins        []PluginConfig       `yaml:"plugins" json:"plugins"`
	Completion     CompletionConfig     `yaml:"completion" json:"completion"`
	Output         OutputConfig         `yaml:"output" json:"output"`
	Authentication AuthenticationConfig `yaml:"authentication" json:"authentication"`
	Timeout        time.Duration        `yaml:"timeout" json:"timeout"`
	RetryPolicy    *RetryPolicy         `yaml:"retry_policy" json:"retry_policy"`
}

// MiddlewareConfig contains middleware configuration
type MiddlewareConfig struct {
	Name     string                 `yaml:"name" json:"name"`
	Enabled  bool                   `yaml:"enabled" json:"enabled"`
	Priority int                    `yaml:"priority" json:"priority"`
	Config   map[string]interface{} `yaml:"config" json:"config"`
}

// PluginConfig contains plugin configuration
type PluginConfig struct {
	Name     string                 `yaml:"name" json:"name"`
	Enabled  bool                   `yaml:"enabled" json:"enabled"`
	Path     string                 `yaml:"path" json:"path"`
	Config   map[string]interface{} `yaml:"config" json:"config"`
	Commands []string               `yaml:"commands" json:"commands"`
}

// CompletionConfig contains shell completion configuration
type CompletionConfig struct {
	Enabled     bool     `yaml:"enabled" json:"enabled"`
	Shells      []string `yaml:"shells" json:"shells"`
	InstallPath string   `yaml:"install_path" json:"install_path"`
}

// OutputConfig contains output formatting configuration
type OutputConfig struct {
	DefaultFormat string            `yaml:"default_format" json:"default_format"`
	Formats       []string          `yaml:"formats" json:"formats"`
	Colors        bool              `yaml:"colors" json:"colors"`
	Timestamps    bool              `yaml:"timestamps" json:"timestamps"`
	Verbose       bool              `yaml:"verbose" json:"verbose"`
	Quiet         bool              `yaml:"quiet" json:"quiet"`
	Template      string            `yaml:"template" json:"template"`
	Fields        map[string]string `yaml:"fields" json:"fields"`
}

// AuthenticationConfig contains authentication configuration
type AuthenticationConfig struct {
	Enabled     bool           `yaml:"enabled" json:"enabled"`
	TokenFile   string         `yaml:"token_file" json:"token_file"`
	TokenEnvVar string         `yaml:"token_env_var" json:"token_env_var"`
	Providers   []AuthProvider `yaml:"providers" json:"providers"`
	SessionFile string         `yaml:"session_file" json:"session_file"`
	Timeout     time.Duration  `yaml:"timeout" json:"timeout"`
	Scopes      []string       `yaml:"scopes" json:"scopes"`
	Endpoints   AuthEndpoints  `yaml:"endpoints" json:"endpoints"`
}

// AuthProvider contains authentication provider configuration
type AuthProvider struct {
	Name         string            `yaml:"name" json:"name"`
	Type         string            `yaml:"type" json:"type"`
	ClientID     string            `yaml:"client_id" json:"client_id"`
	ClientSecret string            `yaml:"client_secret" json:"client_secret"`
	AuthURL      string            `yaml:"auth_url" json:"auth_url"`
	TokenURL     string            `yaml:"token_url" json:"token_url"`
	RedirectURL  string            `yaml:"redirect_url" json:"redirect_url"`
	Scopes       []string          `yaml:"scopes" json:"scopes"`
	Config       map[string]string `yaml:"config" json:"config"`
}

// AuthEndpoints contains authentication endpoint URLs
type AuthEndpoints struct {
	Login    string `yaml:"login" json:"login"`
	Logout   string `yaml:"logout" json:"logout"`
	Refresh  string `yaml:"refresh" json:"refresh"`
	Profile  string `yaml:"profile" json:"profile"`
	Validate string `yaml:"validate" json:"validate"`
}

// DefaultCLIConfig returns a default CLI configuration
func DefaultCLIConfig() *CLIConfig {
	return &CLIConfig{
		LogLevel:    "info",
		ConfigPaths: []string{"./config.yaml", "~/.forge/config.yaml", "/etc/forge/config.yaml"},
		Timeout:     30 * time.Second,
		Middleware: []MiddlewareConfig{
			{
				Name:     "logging",
				Enabled:  true,
				Priority: 10,
			},
			{
				Name:     "metrics",
				Enabled:  true,
				Priority: 20,
			},
			{
				Name:     "recovery",
				Enabled:  true,
				Priority: 1,
			},
		},
		Completion: CompletionConfig{
			Enabled: true,
			Shells:  []string{"bash", "zsh", "fish"},
		},
		Output: OutputConfig{
			DefaultFormat: "json",
			Formats:       []string{"json", "yaml", "table"},
			Colors:        true,
			Timestamps:    false,
			Verbose:       false,
			Quiet:         false,
		},
		Authentication: AuthenticationConfig{
			Enabled:     false,
			TokenEnvVar: "FORGE_TOKEN",
			SessionFile: "~/.forge/session",
			Timeout:     5 * time.Minute,
		},
		RetryPolicy: &RetryPolicy{
			MaxAttempts:  3,
			InitialDelay: 1 * time.Second,
			MaxDelay:     10 * time.Second,
			Multiplier:   2.0,
		},
	}
}

// Validate validates the CLI configuration
func (c *CLIConfig) Validate() error {
	if c.Name == "" {
		return &ConfigValidationError{
			Field:   "name",
			Message: "application name is required",
		}
	}

	// Validate log level
	validLogLevels := map[string]bool{
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
		"fatal": true,
	}

	if c.LogLevel != "" && !validLogLevels[c.LogLevel] {
		return &ConfigValidationError{
			Field:   "log_level",
			Message: "invalid log level: " + c.LogLevel,
		}
	}

	// Validate output format
	if c.Output.DefaultFormat != "" {
		validFormats := make(map[string]bool)
		for _, format := range c.Output.Formats {
			validFormats[format] = true
		}

		if !validFormats[c.Output.DefaultFormat] {
			return &ConfigValidationError{
				Field:   "output.default_format",
				Message: "default format not in supported formats",
			}
		}
	}

	// Validate retry policy
	if c.RetryPolicy != nil {
		if c.RetryPolicy.MaxAttempts < 1 {
			return &ConfigValidationError{
				Field:   "retry_policy.max_attempts",
				Message: "max attempts must be at least 1",
			}
		}

		if c.RetryPolicy.Multiplier <= 0 {
			return &ConfigValidationError{
				Field:   "retry_policy.multiplier",
				Message: "multiplier must be greater than 0",
			}
		}
	}

	// Validate middleware configurations
	for i, mw := range c.Middleware {
		if mw.Name == "" {
			return &ConfigValidationError{
				Field:   fmt.Sprintf("middleware[%d].name", i),
				Message: "middleware name is required",
			}
		}
	}

	// Validate plugin configurations
	for i, plugin := range c.Plugins {
		if plugin.Name == "" {
			return &ConfigValidationError{
				Field:   fmt.Sprintf("plugins[%d].name", i),
				Message: "plugin name is required",
			}
		}
	}

	// Validate authentication configuration
	if c.Authentication.Enabled {
		if len(c.Authentication.Providers) == 0 {
			return &ConfigValidationError{
				Field:   "authentication.providers",
				Message: "at least one auth provider is required when authentication is enabled",
			}
		}

		for i, provider := range c.Authentication.Providers {
			if provider.Name == "" {
				return &ConfigValidationError{
					Field:   fmt.Sprintf("authentication.providers[%d].name", i),
					Message: "provider name is required",
				}
			}

			if provider.Type == "" {
				return &ConfigValidationError{
					Field:   fmt.Sprintf("authentication.providers[%d].type", i),
					Message: "provider type is required",
				}
			}
		}
	}

	return nil
}

// Merge merges another configuration into this one
func (c *CLIConfig) Merge(other *CLIConfig) {
	if other.Name != "" {
		c.Name = other.Name
	}

	if other.Description != "" {
		c.Description = other.Description
	}

	if other.Version != "" {
		c.Version = other.Version
	}

	if other.LogLevel != "" {
		c.LogLevel = other.LogLevel
	}

	if other.Timeout != 0 {
		c.Timeout = other.Timeout
	}

	// Merge arrays
	if len(other.ConfigPaths) > 0 {
		c.ConfigPaths = append(c.ConfigPaths, other.ConfigPaths...)
	}

	// Merge middleware
	existingMiddleware := make(map[string]*MiddlewareConfig)
	for i := range c.Middleware {
		existingMiddleware[c.Middleware[i].Name] = &c.Middleware[i]
	}

	for _, mw := range other.Middleware {
		if existing, found := existingMiddleware[mw.Name]; found {
			// Update existing middleware
			existing.Enabled = mw.Enabled
			existing.Priority = mw.Priority
			if mw.Config != nil {
				existing.Config = mw.Config
			}
		} else {
			// Add new middleware
			c.Middleware = append(c.Middleware, mw)
		}
	}

	// Merge plugins
	existingPlugins := make(map[string]*PluginConfig)
	for i := range c.Plugins {
		existingPlugins[c.Plugins[i].Name] = &c.Plugins[i]
	}

	for _, plugin := range other.Plugins {
		if existing, found := existingPlugins[plugin.Name]; found {
			// Update existing plugin
			existing.Enabled = plugin.Enabled
			existing.Path = plugin.Path
			if plugin.Config != nil {
				existing.Config = plugin.Config
			}
			if len(plugin.Commands) > 0 {
				existing.Commands = plugin.Commands
			}
		} else {
			// Add new plugin
			c.Plugins = append(c.Plugins, plugin)
		}
	}

	// Merge other complex fields
	c.mergeCompletion(&other.Completion)
	c.mergeOutput(&other.Output)
	c.mergeAuthentication(&other.Authentication)

	if other.RetryPolicy != nil {
		c.RetryPolicy = other.RetryPolicy
	}
}

// mergeCompletion merges completion configuration
func (c *CLIConfig) mergeCompletion(other *CompletionConfig) {
	c.Completion.Enabled = other.Enabled

	if len(other.Shells) > 0 {
		c.Completion.Shells = other.Shells
	}

	if other.InstallPath != "" {
		c.Completion.InstallPath = other.InstallPath
	}
}

// mergeOutput merges output configuration
func (c *CLIConfig) mergeOutput(other *OutputConfig) {
	if other.DefaultFormat != "" {
		c.Output.DefaultFormat = other.DefaultFormat
	}

	if len(other.Formats) > 0 {
		c.Output.Formats = other.Formats
	}

	c.Output.Colors = other.Colors
	c.Output.Timestamps = other.Timestamps
	c.Output.Verbose = other.Verbose
	c.Output.Quiet = other.Quiet

	if other.Template != "" {
		c.Output.Template = other.Template
	}

	if other.Fields != nil {
		if c.Output.Fields == nil {
			c.Output.Fields = make(map[string]string)
		}
		for k, v := range other.Fields {
			c.Output.Fields[k] = v
		}
	}
}

// mergeAuthentication merges authentication configuration
func (c *CLIConfig) mergeAuthentication(other *AuthenticationConfig) {
	c.Authentication.Enabled = other.Enabled

	if other.TokenFile != "" {
		c.Authentication.TokenFile = other.TokenFile
	}

	if other.TokenEnvVar != "" {
		c.Authentication.TokenEnvVar = other.TokenEnvVar
	}

	if other.SessionFile != "" {
		c.Authentication.SessionFile = other.SessionFile
	}

	if other.Timeout != 0 {
		c.Authentication.Timeout = other.Timeout
	}

	if len(other.Scopes) > 0 {
		c.Authentication.Scopes = other.Scopes
	}

	if len(other.Providers) > 0 {
		c.Authentication.Providers = other.Providers
	}

	// Merge endpoints
	if other.Endpoints.Login != "" {
		c.Authentication.Endpoints.Login = other.Endpoints.Login
	}
	if other.Endpoints.Logout != "" {
		c.Authentication.Endpoints.Logout = other.Endpoints.Logout
	}
	if other.Endpoints.Refresh != "" {
		c.Authentication.Endpoints.Refresh = other.Endpoints.Refresh
	}
	if other.Endpoints.Profile != "" {
		c.Authentication.Endpoints.Profile = other.Endpoints.Profile
	}
	if other.Endpoints.Validate != "" {
		c.Authentication.Endpoints.Validate = other.Endpoints.Validate
	}
}

// ConfigValidationError represents a configuration validation error
type ConfigValidationError struct {
	Field   string
	Message string
}

// Error implements the error interface
func (e *ConfigValidationError) Error() string {
	return fmt.Sprintf("config validation error for field '%s': %s", e.Field, e.Message)
}
