package middleware

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/pflag"
	"github.com/xraph/forge/v0/pkg/cli"
	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
)

// ConfigMiddleware handles configuration loading and validation
type ConfigMiddleware struct {
	*cli.BaseMiddleware
	configManager common.ConfigManager
	config        ConfigMiddlewareConfig
}

// ConfigMiddlewareConfig contains config middleware configuration
type ConfigMiddlewareConfig struct {
	ConfigPaths    []string          `yaml:"config_paths" json:"config_paths"`
	EnvPrefix      string            `yaml:"env_prefix" json:"env_prefix"`
	RequireConfig  bool              `yaml:"require_config" json:"require_config"`
	WatchConfig    bool              `yaml:"watch_config" json:"watch_config"`
	DefaultValues  map[string]string `yaml:"default_values" json:"default_values"`
	ValidateOnLoad bool              `yaml:"validate_on_load" json:"validate_on_load"`
	ExpandVars     bool              `yaml:"expand_vars" json:"expand_vars"`
	MergeStrategy  string            `yaml:"merge_strategy" json:"merge_strategy"` // "override", "merge", "append"
	SecretSources  []string          `yaml:"secret_sources" json:"secret_sources"`
}

// NewConfigMiddleware creates a new config middleware
func NewConfigMiddleware(configManager common.ConfigManager) cli.CLIMiddleware {
	return NewConfigMiddlewareWithConfig(configManager, DefaultConfigMiddlewareConfig())
}

// NewConfigMiddlewareWithConfig creates config middleware with custom config
func NewConfigMiddlewareWithConfig(configManager common.ConfigManager, config ConfigMiddlewareConfig) cli.CLIMiddleware {
	return &ConfigMiddleware{
		BaseMiddleware: cli.NewBaseMiddleware("config", 5),
		configManager:  configManager,
		config:         config,
	}
}

// Execute executes the config middleware
func (cm *ConfigMiddleware) Execute(ctx cli.CLIContext, next func() error) error {
	// Load configuration
	if err := cm.loadConfiguration(ctx); err != nil {
		if cm.config.RequireConfig {
			return fmt.Errorf("failed to load configuration: %w", err)
		}

		// Log warning but continue
		if ctx.Logger() != nil {
			ctx.Logger().Warn("configuration loading failed, using defaults",
				logger.Error(err),
			)
		}
	}

	// Set up configuration watching if enabled
	if cm.config.WatchConfig {
		cm.setupConfigWatching(ctx)
	}

	// Make config available in context
	ctx.Set("config_loaded", true)
	ctx.Set("config_manager", cm.configManager)

	return next()
}

// loadConfiguration loads configuration from various sources
func (cm *ConfigMiddleware) loadConfiguration(ctx cli.CLIContext) error {
	// Load default values first
	cm.loadDefaultValues()

	// Load from configuration files
	if err := cm.loadFromFiles(); err != nil {
		return err
	}

	// Load from environment variables
	cm.loadFromEnvironment()

	// Load from command line flags
	cm.loadFromFlags(ctx)

	// Load secrets if configured
	if err := cm.loadSecrets(); err != nil {
		return err
	}

	// Expand variables if enabled
	if cm.config.ExpandVars {
		if err := cm.expandVariables(); err != nil {
			return err
		}
	}

	// Validate configuration if enabled
	if cm.config.ValidateOnLoad {
		if err := cm.validateConfiguration(); err != nil {
			return err
		}
	}

	return nil
}

// loadDefaultValues loads default configuration values
func (cm *ConfigMiddleware) loadDefaultValues() {
	for key, value := range cm.config.DefaultValues {
		cm.configManager.Set(key, value)
	}
}

// loadFromFiles loads configuration from files
func (cm *ConfigMiddleware) loadFromFiles() error {
	var loadedAny bool
	var lastError error

	for _, configPath := range cm.config.ConfigPaths {
		// Expand user home directory
		if strings.HasPrefix(configPath, "~/") {
			home, err := os.UserHomeDir()
			if err != nil {
				continue
			}
			configPath = filepath.Join(home, configPath[2:])
		}

		// Check if file exists
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			continue
		}

		// Load configuration file
		if err := cm.loadConfigFile(configPath); err != nil {
			lastError = err
			continue
		}

		loadedAny = true
		break // Use the first successfully loaded config file
	}

	if !loadedAny && cm.config.RequireConfig {
		if lastError != nil {
			return lastError
		}
		return fmt.Errorf("no configuration file found in paths: %v", cm.config.ConfigPaths)
	}

	return nil
}

// loadConfigFile loads a specific configuration file
func (cm *ConfigMiddleware) loadConfigFile(path string) error {
	// This is a simplified implementation
	// In a real implementation, you'd use a proper config library like Viper
	return nil
}

// loadFromEnvironment loads configuration from environment variables
func (cm *ConfigMiddleware) loadFromEnvironment() {
	if cm.config.EnvPrefix == "" {
		return
	}

	envPrefix := strings.ToUpper(cm.config.EnvPrefix) + "_"

	for _, env := range os.Environ() {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key, value := parts[0], parts[1]
		if !strings.HasPrefix(key, envPrefix) {
			continue
		}

		// Convert environment variable name to config key
		configKey := strings.ToLower(strings.TrimPrefix(key, envPrefix))
		configKey = strings.ReplaceAll(configKey, "_", ".")

		cm.configManager.Set(configKey, value)
	}
}

// loadFromFlags loads configuration from command line flags
func (cm *ConfigMiddleware) loadFromFlags(ctx cli.CLIContext) {
	// Iterate through flags and set configuration values
	ctx.Command().Flags().VisitAll(func(flag *pflag.Flag) {
		if flag.Changed {
			configKey := strings.ReplaceAll(flag.Name, "-", ".")
			cm.configManager.Set(configKey, flag.Value.String())
		}
	})
}

// loadSecrets loads secrets from configured sources
func (cm *ConfigMiddleware) loadSecrets() error {
	for _, secretSource := range cm.config.SecretSources {
		if err := cm.loadSecretsFromSource(secretSource); err != nil {
			return fmt.Errorf("failed to load secrets from %s: %w", secretSource, err)
		}
	}
	return nil
}

// loadSecretsFromSource loads secrets from a specific source
func (cm *ConfigMiddleware) loadSecretsFromSource(source string) error {
	// This would integrate with secret management systems
	// like AWS Secrets Manager, Azure Key Vault, HashiCorp Vault, etc.
	switch source {
	case "env":
		// Load secrets from environment variables with special prefix
		return cm.loadSecretsFromEnv()
	case "file":
		// Load secrets from secure file
		return cm.loadSecretsFromFile()
	default:
		return fmt.Errorf("unsupported secret source: %s", source)
	}
}

// loadSecretsFromEnv loads secrets from environment variables
func (cm *ConfigMiddleware) loadSecretsFromEnv() error {
	secretPrefix := "SECRET_"

	for _, env := range os.Environ() {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key, value := parts[0], parts[1]
		if !strings.HasPrefix(key, secretPrefix) {
			continue
		}

		// Convert to config key
		configKey := strings.ToLower(strings.TrimPrefix(key, secretPrefix))
		configKey = strings.ReplaceAll(configKey, "_", ".")

		cm.configManager.Set(configKey, value)
	}

	return nil
}

// loadSecretsFromFile loads secrets from a secure file
func (cm *ConfigMiddleware) loadSecretsFromFile() error {
	// This would load from a secure file format
	// Implementation would depend on the chosen format (encrypted JSON, etc.)
	return nil
}

// expandVariables expands variables in configuration values
func (cm *ConfigMiddleware) expandVariables() error {
	// This would expand variables like ${VAR_NAME} in config values
	// Implementation would iterate through all config values and expand them
	return nil
}

// validateConfiguration validates the loaded configuration
func (cm *ConfigMiddleware) validateConfiguration() error {
	// This would validate the configuration against a schema or rules
	// Implementation would depend on the validation requirements
	return nil
}

// setupConfigWatching sets up configuration file watching
func (cm *ConfigMiddleware) setupConfigWatching(ctx cli.CLIContext) {
	// This would set up file system watchers to reload config on changes
	// Implementation would use libraries like fsnotify
	if ctx.Logger() != nil {
		ctx.Logger().Debug("config watching enabled")
	}
}

// DefaultConfigMiddlewareConfig returns default config middleware configuration
func DefaultConfigMiddlewareConfig() ConfigMiddlewareConfig {
	return ConfigMiddlewareConfig{
		ConfigPaths: []string{
			"./config.yaml",
			"./config.yml",
			"./config.json",
			"~/.config/app/config.yaml",
			"/etc/app/config.yaml",
		},
		EnvPrefix:      "APP",
		RequireConfig:  false,
		WatchConfig:    false,
		DefaultValues:  make(map[string]string),
		ValidateOnLoad: false,
		ExpandVars:     true,
		MergeStrategy:  "override",
		SecretSources:  []string{},
	}
}

// StrictConfigMiddlewareConfig returns configuration that requires config files
func StrictConfigMiddlewareConfig() ConfigMiddlewareConfig {
	config := DefaultConfigMiddlewareConfig()
	config.RequireConfig = true
	config.ValidateOnLoad = true
	return config
}

// DynamicConfigMiddlewareConfig returns configuration with watching enabled
func DynamicConfigMiddlewareConfig() ConfigMiddlewareConfig {
	config := DefaultConfigMiddlewareConfig()
	config.WatchConfig = true
	return config
}
