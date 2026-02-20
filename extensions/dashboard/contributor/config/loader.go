package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ConfigFileName is the standard name for the contributor config file.
const ConfigFileName = "forge.contributor.yaml"

// LoadConfig reads and validates a forge.contributor.yaml file.
func LoadConfig(path string) (*ContributorConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read contributor config: %w", err)
	}

	return ParseConfig(data)
}

// ParseConfig parses and validates contributor config from raw YAML bytes.
func ParseConfig(data []byte) (*ContributorConfig, error) {
	var cfg ContributorConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse contributor config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	cfg.ApplyDefaults()

	return &cfg, nil
}

// FindConfig searches for forge.contributor.yaml starting from dir and
// walking up to maxDepth parent directories. Returns the absolute path
// to the config file or an error if not found.
func FindConfig(dir string) (string, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}

	path := filepath.Join(absDir, ConfigFileName)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

	// Also check in common subdirectories
	for _, sub := range []string{"ui", "contributor"} {
		path = filepath.Join(absDir, sub, ConfigFileName)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("forge.contributor.yaml not found in %s", absDir)
}

// Validate checks the contributor config for required fields and valid values.
func (c *ContributorConfig) Validate() error {
	var errs []string

	if c.Name == "" {
		errs = append(errs, "name is required")
	} else if strings.Contains(c.Name, "-") {
		errs = append(errs, "name must not contain hyphens (use underscores)")
	}

	if c.DisplayName == "" {
		errs = append(errs, "display_name is required")
	}

	if c.Version == "" {
		errs = append(errs, "version is required")
	}

	if c.Type == "" {
		errs = append(errs, "type is required")
	} else if !ValidFrameworkTypes[c.Type] {
		errs = append(errs, fmt.Sprintf("type %q is not valid (use: astro, nextjs, custom)", c.Type))
	}

	if c.Build.Mode == "" {
		errs = append(errs, "build.mode is required")
	} else if !ValidBuildModes[c.Build.Mode] {
		errs = append(errs, fmt.Sprintf("build.mode %q is not valid (use: static, ssr)", c.Build.Mode))
	}

	// Validate nav items
	for i, nav := range c.Nav {
		if nav.Label == "" {
			errs = append(errs, fmt.Sprintf("nav[%d].label is required", i))
		}

		if nav.Path == "" {
			errs = append(errs, fmt.Sprintf("nav[%d].path is required", i))
		}
	}

	// Validate widgets
	for i, w := range c.Widgets {
		if w.ID == "" {
			errs = append(errs, fmt.Sprintf("widgets[%d].id is required", i))
		}

		if w.Title == "" {
			errs = append(errs, fmt.Sprintf("widgets[%d].title is required", i))
		}
	}

	// Validate settings
	for i, s := range c.Settings {
		if s.ID == "" {
			errs = append(errs, fmt.Sprintf("settings[%d].id is required", i))
		}

		if s.Title == "" {
			errs = append(errs, fmt.Sprintf("settings[%d].title is required", i))
		}
	}

	// Validate bridge functions
	for i, bf := range c.BridgeFunctions {
		if bf.Name == "" {
			errs = append(errs, fmt.Sprintf("bridge_functions[%d].name is required", i))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("contributor config validation: %s", strings.Join(errs, "; "))
	}

	return nil
}

// ApplyDefaults fills in default values for optional fields based on the
// framework type and build mode.
func (c *ContributorConfig) ApplyDefaults() {
	if c.Build.UIDir == "" {
		c.Build.UIDir = "ui"
	}

	if c.Build.DistDir == "" {
		switch c.Type {
		case "astro":
			c.Build.DistDir = "dist"
		case "nextjs":
			if c.Build.Mode == "static" {
				c.Build.DistDir = "out"
			} else {
				c.Build.DistDir = ".next"
			}
		default:
			c.Build.DistDir = "dist"
		}
	}

	if c.Build.EmbedPath == "" && c.Build.Mode == "static" {
		c.Build.EmbedPath = filepath.Join(c.Build.UIDir, c.Build.DistDir)
	}

	if c.Build.SSREntry == "" && c.Build.Mode == "ssr" {
		switch c.Type {
		case "astro":
			c.Build.SSREntry = filepath.Join(c.Build.UIDir, "dist/server/entry.mjs")
		case "nextjs":
			c.Build.SSREntry = filepath.Join(c.Build.UIDir, ".next/standalone/server.js")
		}
	}

	if c.Build.PagesDir == "" {
		c.Build.PagesDir = "pages"
	}

	if c.Build.WidgetsDir == "" {
		c.Build.WidgetsDir = "widgets"
	}

	if c.Build.SettingsDir == "" {
		c.Build.SettingsDir = "settings"
	}

	if c.Build.AssetsDir == "" {
		c.Build.AssetsDir = "assets"
	}
}

// UIPath returns the absolute path to the UI source directory given the
// extension root directory.
func (c *ContributorConfig) UIPath(extRoot string) string {
	return filepath.Join(extRoot, c.Build.UIDir)
}

// DistPath returns the absolute path to the build output directory given
// the extension root directory.
func (c *ContributorConfig) DistPath(extRoot string) string {
	return filepath.Join(extRoot, c.Build.UIDir, c.Build.DistDir)
}
