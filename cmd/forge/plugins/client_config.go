// v2/cmd/forge/plugins/client_config.go
package plugins

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/xraph/forge/errors"
	"gopkg.in/yaml.v3"
)

// ClientConfig represents the .forge-client.yml configuration file.
type ClientConfig struct {
	// Source configuration
	Source SourceConfig `yaml:"source"`

	// Default generation settings
	Defaults GenerationDefaults `yaml:"defaults"`

	// Streaming extension configuration
	Streaming StreamingExtConfig `yaml:"streaming,omitempty"`

	// Multiple client configurations
	Clients []ClientGenConfig `yaml:"clients,omitempty"`
}

// StreamingExtConfig defines streaming extension features configuration.
type StreamingExtConfig struct {
	// Enable room management client
	Rooms bool `yaml:"rooms"`

	// Enable presence tracking client
	Presence bool `yaml:"presence"`

	// Enable typing indicator client
	Typing bool `yaml:"typing"`

	// Enable pub/sub channel client
	Channels bool `yaml:"channels"`

	// Enable message history support
	History bool `yaml:"history"`
}

// SourceConfig defines where to get the API specification.
type SourceConfig struct {
	// Type: "file", "url", "auto"
	Type string `yaml:"type"`

	// Path to spec file (when type=file)
	Path string `yaml:"path,omitempty"`

	// URL to fetch spec (when type=url)
	URL string `yaml:"url,omitempty"`

	// Auto-discovery paths (when type=auto)
	AutoDiscoverPaths []string `yaml:"auto_discover_paths,omitempty"`
}

// GenerationDefaults defines default settings for client generation.
type GenerationDefaults struct {
	Language string `yaml:"language"`
	Output   string `yaml:"output"`
	Package  string `yaml:"package"`
	BaseURL  string `yaml:"base_url,omitempty"`
	Module   string `yaml:"module,omitempty"`

	// Feature flags
	Auth      bool `yaml:"auth"`
	Streaming bool `yaml:"streaming"`

	// Streaming features
	Reconnection    bool `yaml:"reconnection"`
	Heartbeat       bool `yaml:"heartbeat"`
	StateManagement bool `yaml:"state_management"`

	// Enhanced features
	UseFetch        bool `yaml:"use_fetch"`
	DualPackage     bool `yaml:"dual_package"`
	GenerateTests   bool `yaml:"generate_tests"`
	GenerateLinting bool `yaml:"generate_linting"`
	GenerateCI      bool `yaml:"generate_ci"`
	ErrorTaxonomy   bool `yaml:"error_taxonomy"`
	Interceptors    bool `yaml:"interceptors"`
	Pagination      bool `yaml:"pagination"`

	// Output control
	ClientOnly bool `yaml:"client_only"` // Generate only client source files
}

// ClientGenConfig defines configuration for generating a specific client.
type ClientGenConfig struct {
	Name     string `yaml:"name"`
	Language string `yaml:"language"`
	Output   string `yaml:"output"`
	Package  string `yaml:"package,omitempty"`
	BaseURL  string `yaml:"base_url,omitempty"`
	Module   string `yaml:"module,omitempty"`

	// Override feature flags
	Auth      *bool `yaml:"auth,omitempty"`
	Streaming *bool `yaml:"streaming,omitempty"`
}

// LoadClientConfig loads .forge-client.yml from the current directory or parent.
func LoadClientConfig(startDir string) (*ClientConfig, error) {
	// Try current directory first
	configPath := filepath.Join(startDir, ".forge-client.yml")
	if _, err := os.Stat(configPath); err == nil {
		return loadClientConfigFile(configPath)
	}

	// Try .forge-client.yaml
	configPath = filepath.Join(startDir, ".forge-client.yaml")
	if _, err := os.Stat(configPath); err == nil {
		return loadClientConfigFile(configPath)
	}

	// Try parent directories
	dir := startDir
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached root
			break
		}

		dir = parent

		configPath = filepath.Join(dir, ".forge-client.yml")
		if _, err := os.Stat(configPath); err == nil {
			return loadClientConfigFile(configPath)
		}

		configPath = filepath.Join(dir, ".forge-client.yaml")
		if _, err := os.Stat(configPath); err == nil {
			return loadClientConfigFile(configPath)
		}
	}

	return nil, errors.New(".forge-client.yml not found")
}

// loadClientConfigFile loads and parses a client config file.
func loadClientConfigFile(path string) (*ClientConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	var config ClientConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}

	return &config, nil
}

// DefaultClientConfig returns a default client configuration.
func DefaultClientConfig() *ClientConfig {
	return &ClientConfig{
		Source: SourceConfig{
			Type: "auto",
			AutoDiscoverPaths: []string{
				"./openapi.json",
				"./openapi.yaml",
				"./api/openapi.json",
				"./api/openapi.yaml",
				"./docs/openapi.json",
				"./docs/openapi.yaml",
			},
		},
		Defaults: GenerationDefaults{
			Language:        "go",
			Output:          "./client",
			Package:         "client",
			Auth:            true,
			Streaming:       true,
			Reconnection:    true,
			Heartbeat:       true,
			StateManagement: true,
			UseFetch:        true,
			DualPackage:     true,
			GenerateTests:   true,
			GenerateLinting: true,
			GenerateCI:      true,
			ErrorTaxonomy:   true,
			Interceptors:    true,
			Pagination:      true,
		},
	}
}

// SaveClientConfig saves a client configuration to a file.
func SaveClientConfig(config *ClientConfig, path string) error {
	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}

	return nil
}

// fetchSpecFromURL fetches an OpenAPI/AsyncAPI spec from a URL.
func fetchSpecFromURL(url string, timeout time.Duration) ([]byte, error) {
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Accept", "application/json, application/yaml, application/x-yaml, text/yaml")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch spec: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch spec failed with status: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	return data, nil
}

// autoDiscoverSpec tries to find a spec file in common locations.
func autoDiscoverSpec(rootDir string, paths []string) (string, error) {
	if len(paths) == 0 {
		paths = DefaultClientConfig().Source.AutoDiscoverPaths
	}

	for _, path := range paths {
		// Make path absolute if relative
		absPath := path
		if !filepath.IsAbs(path) {
			absPath = filepath.Join(rootDir, path)
		}

		if _, err := os.Stat(absPath); err == nil {
			return absPath, nil
		}
	}

	return "", errors.New("no spec file found in auto-discover paths")
}
