// v2/cmd/forge/config/loader.go
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LoadForgeConfig loads the nearest project configuration. Invalid or ambiguous files stop the search.
func LoadForgeConfig() (*ForgeConfig, string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return nil, "", err
	}
	for {
		cfg, err := LoadForgeConfigFrom(dir)
		if err == nil {
			return cfg, cfg.ConfigPath, nil
		}

		if !errors.Is(err, os.ErrNotExist) {
			return nil, "", err
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, "", fmt.Errorf("no .forge.yml or .forge.yaml found: %w", os.ErrNotExist)
		}

		dir = parent
	}
}

// LoadForgeConfigFrom loads root only and rejects ambiguous filenames.
func LoadForgeConfigFrom(root string) (*ForgeConfig, error) {
	var found string

	for _, name := range []string{".forge.yml", ".forge.yaml"} {
		path := filepath.Join(root, name)

		_, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}

		if err != nil {
			return nil, err
		}

		if found != "" {
			return nil, fmt.Errorf("both .forge.yml and .forge.yaml exist in %s", root)
		}

		found = path
	}

	if found == "" {
		return nil, fmt.Errorf("no project configuration in %s: %w", root, os.ErrNotExist)
	}

	cfg, err := tryLoadConfig(found)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", found, err)
	}

	cfg.RootDir = root
	cfg.ConfigPath = found

	return cfg, nil
}

// tryLoadConfig attempts to load config from a specific path.
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

	// Decode the project envelope separately. The deploy v2 engine reads its own typed contract.
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	if len(document.Content) > 0 {
		mapping := document.Content[0]
		for i := 0; i+1 < len(mapping.Content); i += 2 {
			if mapping.Content[i].Value != "deploy" {
				continue
			}

			node := mapping.Content[i+1]

			var version struct {
				Version int `yaml:"version"`
			}
			if err := node.Decode(&version); err != nil {
				return nil, fmt.Errorf("invalid deploy section: %w", err)
			}

			if version.Version >= 2 {
				mapping.Content = append(mapping.Content[:i], mapping.Content[i+2:]...)
			}

			break
		}
	}
	config := DefaultConfig()
	if err := document.Decode(config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return config, nil
}

// SaveForgeConfig saves the configuration to a file.
// Uses omitempty tags to produce clean, minimal YAML output.
func SaveForgeConfig(config *ForgeConfig, path string) error {
	// Marshal to YAML with proper formatting
	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Existing v2 deployment settings belong to the deploy engine. Preserve
	// their YAML nodes and unknown project keys when another command saves.
	existing, readErr := os.ReadFile(path)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}

	if readErr == nil {
		var old, next yaml.Node

		if err := yaml.Unmarshal(existing, &old); err != nil {
			return fmt.Errorf("refusing to overwrite invalid configuration: %w", err)
		}

		if len(old.Content) > 0 && old.Content[0].Kind == yaml.MappingNode {
			mapping := old.Content[0]
			v2 := false

			for i := 0; i+1 < len(mapping.Content); i += 2 {
				if mapping.Content[i].Value == "deploy" {
					var version struct {
						Version int `yaml:"version"`
					}
					if err := mapping.Content[i+1].Decode(&version); err != nil {
						return err
					}

					v2 = version.Version >= 2
				}
			}

			if v2 {
				if err := yaml.Unmarshal(data, &next); err != nil {
					return err
				}

				for i := 0; i+1 < len(next.Content[0].Content); i += 2 {
					key, value := next.Content[0].Content[i], next.Content[0].Content[i+1]

					if key.Value == "deploy" {
						continue
					}

					found := false

					for j := 0; j+1 < len(mapping.Content); j += 2 {
						if mapping.Content[j].Value == key.Value {
							mapping.Content[j+1] = value
							found = true

							break
						}
					}

					if !found {
						mapping.Content = append(mapping.Content, key, value)
					}
				}

				data, err = yaml.Marshal(&old)
				if err != nil {
					return err
				}

				return os.WriteFile(path, data, 0600)
			}
		}
	}

	// Add header comment
	header := `# Forge Configuration
# This file uses smart defaults - only specify what you need to override.
# See https://forge.dev/docs/configuration for full documentation.

`
	finalData := []byte(header + string(data))

	// Write to file
	if err := os.WriteFile(path, finalData, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// CreateForgeConfig creates a new .forge.yaml file with default or provided config.
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

// ValidateConfig validates the configuration.
func ValidateConfig(config *ForgeConfig) error {
	if config.Project.Name == "" {
		return errors.New("project.name is required")
	}

	if config.Project.Layout != "" &&
		config.Project.Layout != "single-module" &&
		config.Project.Layout != "multi-module" {
		return errors.New("project.layout must be 'single-module' or 'multi-module'")
	}

	if config.IsSingleModule() && config.Project.Module == "" {
		return errors.New("project.module is required for single-module layout")
	}

	if config.IsMultiModule() && !config.Project.Workspace.Enabled {
		return errors.New("project.workspace.enabled must be true for multi-module layout")
	}

	return nil
}
