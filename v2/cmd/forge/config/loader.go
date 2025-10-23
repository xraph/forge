// v2/cmd/forge/config/loader.go
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LoadForgeConfig searches for .forge.yaml up the directory tree and loads it
// Returns the config, the path where it was found, and any error
func LoadForgeConfig() (*ForgeConfig, string, error) {
	// Start from current directory
	dir, err := os.Getwd()
	if err != nil {
		return nil, "", fmt.Errorf("failed to get current directory: %w", err)
	}

	// Search up the directory tree
	for {
		// Try .forge.yaml
		configPath := filepath.Join(dir, ".forge.yaml")
		if config, err := tryLoadConfig(configPath); err == nil {
			config.RootDir = dir
			config.ConfigPath = configPath
			return config, configPath, nil
		}

		// Try .forge.yml
		configPath = filepath.Join(dir, ".forge.yml")
		if config, err := tryLoadConfig(configPath); err == nil {
			config.RootDir = dir
			config.ConfigPath = configPath
			return config, configPath, nil
		}

		// Move up one directory
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached root without finding config
			return nil, "", fmt.Errorf("no .forge.yaml or .forge.yml found in current directory or any parent")
		}
		dir = parent
	}
}

// tryLoadConfig attempts to load config from a specific path
func tryLoadConfig(path string) (*ForgeConfig, error) {
	// Check if file exists
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}

	// Read file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Parse YAML
	config := DefaultConfig()
	if err := yaml.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return config, nil
}

// SaveForgeConfig saves the configuration to a file
func SaveForgeConfig(config *ForgeConfig, path string) error {
	// Marshal to YAML
	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write to file
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// CreateForgeConfig creates a new .forge.yaml file with default or provided config
func CreateForgeConfig(path string, config *ForgeConfig) error {
	// Use default if config is nil
	if config == nil {
		config = DefaultConfig()
	}

	// Check if file already exists
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("config file already exists: %s", path)
	}

	return SaveForgeConfig(config, path)
}

// ValidateConfig validates the configuration
func ValidateConfig(config *ForgeConfig) error {
	if config.Project.Name == "" {
		return fmt.Errorf("project.name is required")
	}

	if config.Project.Layout != "" &&
		config.Project.Layout != "single-module" &&
		config.Project.Layout != "multi-module" {
		return fmt.Errorf("project.layout must be 'single-module' or 'multi-module'")
	}

	if config.IsSingleModule() && config.Project.Module == "" {
		return fmt.Errorf("project.module is required for single-module layout")
	}

	if config.IsMultiModule() && !config.Project.Workspace.Enabled {
		return fmt.Errorf("project.workspace.enabled must be true for multi-module layout")
	}

	return nil
}
