// v2/cmd/forge/config/app_config.go
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// AppConfig represents app-level .forge.yaml configuration
// This is different from ForgeConfig (project-level) and is used
// to configure individual applications within a Forge project.
type AppConfig struct {
	App   AppSection     `yaml:"app"`
	Dev   AppDevConfig   `yaml:"dev,omitempty"`
	Build AppBuildConfig `yaml:"build,omitempty"`

	// Internal fields
	AppDir     string `yaml:"-"` // Directory containing the app's .forge.yaml
	ConfigPath string `yaml:"-"` // Full path to app's .forge.yaml
}

// AppSection defines app metadata.
type AppSection struct {
	Name    string `yaml:"name"`
	Type    string `yaml:"type"` // "web", "cli", "worker", etc.
	Version string `yaml:"version,omitempty"`
}

// AppDevConfig defines app-specific dev configuration.
type AppDevConfig struct {
	Port    int    `yaml:"port,omitempty"`
	Host    string `yaml:"host,omitempty"`
	EnvFile string `yaml:"env_file,omitempty"`
}

// AppBuildConfig defines app-specific build configuration.
type AppBuildConfig struct {
	Output     string   `yaml:"output"`
	Dockerfile string   `yaml:"dockerfile,omitempty"`
	Tags       []string `yaml:"tags,omitempty"`
	LDFlags    string   `yaml:"ldflags,omitempty"`
}

// LoadAppConfig loads an app-level .forge.yaml from the specified directory.
// Returns the config and any error encountered.
func LoadAppConfig(appDir string) (*AppConfig, error) {
	// Try .forge.yaml
	configPath := filepath.Join(appDir, ".forge.yaml")
	if config, err := tryLoadAppConfig(configPath); err == nil {
		config.AppDir = appDir
		config.ConfigPath = configPath
		return config, nil
	}

	// Try .forge.yml
	configPath = filepath.Join(appDir, ".forge.yml")
	if config, err := tryLoadAppConfig(configPath); err == nil {
		config.AppDir = appDir
		config.ConfigPath = configPath
		return config, nil
	}

	return nil, fmt.Errorf("no .forge.yaml or .forge.yml found in %s", appDir)
}

// tryLoadAppConfig attempts to load app config from a specific path.
func tryLoadAppConfig(path string) (*AppConfig, error) {
	// Check if file exists
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}

	// Read file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read app config file: %w", err)
	}

	// Parse YAML
	config := &AppConfig{}
	if err := yaml.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("failed to parse app config file: %w", err)
	}

	return config, nil
}

// GetPort returns the dev port with fallback to 0 (not set).
func (d *AppDevConfig) GetPort() int {
	return d.Port
}

// GetHost returns the dev host with fallback to localhost.
func (d *AppDevConfig) GetHost() string {
	if d.Host != "" {
		return d.Host
	}
	return "localhost"
}
