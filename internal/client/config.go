package client

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// GeneratorConfig configures client generation.
type GeneratorConfig struct {
	// Language specifies the target language (go, typescript, rust)
	Language string

	// OutputDir is the directory where generated files will be written
	OutputDir string

	// PackageName is the name of the generated package/module
	PackageName string

	// APIName is the name of the main client struct/class
	APIName string

	// BaseURL is the default base URL for the API
	BaseURL string

	// IncludeAuth determines if auth configuration should be generated
	IncludeAuth bool

	// IncludeStreaming determines if WebSocket/SSE clients should be generated
	IncludeStreaming bool

	// Features contains feature flags for generation
	Features Features

	// Module is the Go module path (for Go only)
	Module string

	// Version is the version of the generated client
	Version string
}

// Features contains feature flags for client generation.
type Features struct {
	// Reconnection enables automatic reconnection for streaming endpoints
	Reconnection bool

	// Heartbeat enables heartbeat/ping for maintaining connections
	Heartbeat bool

	// StateManagement enables connection state tracking
	StateManagement bool

	// TypedErrors generates typed error responses
	TypedErrors bool

	// RequestRetry enables automatic request retry with exponential backoff
	RequestRetry bool

	// Timeout enables request timeout configuration
	Timeout bool

	// Middleware enables request/response middleware/interceptors
	Middleware bool

	// Logging enables built-in logging support
	Logging bool
}

// DefaultConfig returns a default generator configuration.
func DefaultConfig() GeneratorConfig {
	return GeneratorConfig{
		Language:         "go",
		OutputDir:        "./client",
		PackageName:      "client",
		APIName:          "Client",
		IncludeAuth:      true,
		IncludeStreaming: true,
		Version:          "1.0.0",
		Features: Features{
			Reconnection:    true,
			Heartbeat:       true,
			StateManagement: true,
			TypedErrors:     true,
			RequestRetry:    true,
			Timeout:         true,
			Middleware:      false,
			Logging:         false,
		},
	}
}

// Validate validates the configuration.
func (c *GeneratorConfig) Validate() error {
	if c.Language == "" {
		return errors.New("language is required")
	}

	// Normalize language name
	c.Language = strings.ToLower(c.Language)

	// Validate supported languages
	supportedLanguages := []string{"go", "typescript", "ts"}
	if !contains(supportedLanguages, c.Language) {
		return fmt.Errorf("unsupported language: %s (supported: go, typescript)", c.Language)
	}

	// Normalize typescript alias
	if c.Language == "ts" {
		c.Language = "typescript"
	}

	if c.OutputDir == "" {
		return errors.New("output directory is required")
	}

	if c.PackageName == "" {
		return errors.New("package name is required")
	}

	if c.APIName == "" {
		c.APIName = "Client"
	}

	// Validate package name format
	if err := c.validatePackageName(); err != nil {
		return err
	}

	return nil
}

// validatePackageName validates the package name format.
func (c *GeneratorConfig) validatePackageName() error {
	switch c.Language {
	case "go":
		// Go package names should be lowercase, no spaces, no special chars except underscore
		if !isValidGoPackageName(c.PackageName) {
			return fmt.Errorf("invalid Go package name: %s (must be lowercase alphanumeric with underscores)", c.PackageName)
		}
	case "typescript":
		// TypeScript package names can include @org/package format
		if !isValidTypeScriptPackageName(c.PackageName) {
			return fmt.Errorf("invalid TypeScript package name: %s", c.PackageName)
		}
	}

	return nil
}

// isValidGoPackageName checks if a string is a valid Go package name.
func isValidGoPackageName(name string) bool {
	if name == "" {
		return false
	}

	for i, c := range name {
		if i == 0 && (c >= '0' && c <= '9') {
			return false // Cannot start with digit
		}

		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_') {
			return false
		}
	}

	return true
}

// isValidTypeScriptPackageName checks if a string is a valid TypeScript package name.
func isValidTypeScriptPackageName(name string) bool {
	if name == "" {
		return false
	}
	// Allow @org/package format
	if strings.HasPrefix(name, "@") {
		parts := strings.Split(name, "/")
		if len(parts) != 2 {
			return false
		}
		// Validate both parts
		return isValidNPMName(parts[0][1:]) && isValidNPMName(parts[1])
	}

	return isValidNPMName(name)
}

// isValidNPMName checks basic NPM name validity.
func isValidNPMName(name string) bool {
	if name == "" {
		return false
	}

	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return false
		}
	}

	return true
}

// contains checks if a slice contains a string.
func contains(slice []string, item string) bool {
	return slices.Contains(slice, item)
}

// GeneratorOptions provides functional options for generator config.
type GeneratorOption func(*GeneratorConfig)

// WithLanguage sets the target language.
func WithLanguage(lang string) GeneratorOption {
	return func(c *GeneratorConfig) {
		c.Language = lang
	}
}

// WithOutputDir sets the output directory.
func WithOutputDir(dir string) GeneratorOption {
	return func(c *GeneratorConfig) {
		c.OutputDir = dir
	}
}

// WithPackageName sets the package name.
func WithPackageName(name string) GeneratorOption {
	return func(c *GeneratorConfig) {
		c.PackageName = name
	}
}

// WithAPIName sets the API client name.
func WithAPIName(name string) GeneratorOption {
	return func(c *GeneratorConfig) {
		c.APIName = name
	}
}

// WithBaseURL sets the base URL.
func WithBaseURL(url string) GeneratorOption {
	return func(c *GeneratorConfig) {
		c.BaseURL = url
	}
}

// WithAuth enables/disables auth generation.
func WithAuth(enabled bool) GeneratorOption {
	return func(c *GeneratorConfig) {
		c.IncludeAuth = enabled
	}
}

// WithStreaming enables/disables streaming generation.
func WithStreaming(enabled bool) GeneratorOption {
	return func(c *GeneratorConfig) {
		c.IncludeStreaming = enabled
	}
}

// WithFeatures sets the features.
func WithFeatures(features Features) GeneratorOption {
	return func(c *GeneratorConfig) {
		c.Features = features
	}
}

// WithModule sets the Go module path.
func WithModule(module string) GeneratorOption {
	return func(c *GeneratorConfig) {
		c.Module = module
	}
}

// WithVersion sets the client version.
func WithVersion(version string) GeneratorOption {
	return func(c *GeneratorConfig) {
		c.Version = version
	}
}

// NewConfig creates a new generator config with options.
func NewConfig(opts ...GeneratorOption) GeneratorConfig {
	config := DefaultConfig()
	for _, opt := range opts {
		opt(&config)
	}

	return config
}
