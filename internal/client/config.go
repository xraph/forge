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

	// Streaming contains streaming-specific configuration
	Streaming StreamingConfig

	// Module is the Go module path (for Go only)
	Module string

	// Version is the version of the generated client
	Version string

	// Enhanced features
	UseFetch        bool // Use fetch instead of axios (TypeScript)
	DualPackage     bool // Generate ESM + CJS (TypeScript)
	GenerateTests   bool // Generate test setup
	GenerateLinting bool // Generate linting setup
	GenerateCI      bool // Generate CI config
	ErrorTaxonomy   bool // Generate typed error classes
	Interceptors    bool // Generate interceptor support
	Pagination      bool // Generate pagination helpers

	// Output control
	ClientOnly bool // Generate only client source files (no package.json, tsconfig, etc.)
}

// StreamingConfig configures streaming client generation features.
type StreamingConfig struct {
	// EnableRooms generates room management client (join/leave/broadcast)
	EnableRooms bool

	// EnableChannels generates pub/sub channel client
	EnableChannels bool

	// EnablePresence generates presence tracking client
	EnablePresence bool

	// EnableTyping generates typing indicator client
	EnableTyping bool

	// EnableHistory generates message history support
	EnableHistory bool

	// RoomConfig contains room-specific configuration
	RoomConfig RoomClientConfig

	// PresenceConfig contains presence-specific configuration
	PresenceConfig PresenceClientConfig

	// TypingConfig contains typing indicator configuration
	TypingConfig TypingClientConfig

	// ChannelConfig contains channel-specific configuration
	ChannelConfig ChannelClientConfig

	// GenerateUnifiedClient generates a unified StreamingClient that composes all features
	GenerateUnifiedClient bool

	// GenerateModularClients generates separate clients for each feature
	GenerateModularClients bool
}

// RoomClientConfig configures room client generation.
type RoomClientConfig struct {
	// MaxRoomsPerUser is the default max rooms a user can join (for docs/validation)
	MaxRoomsPerUser int

	// IncludeMemberEvents generates handlers for member join/leave events
	IncludeMemberEvents bool

	// IncludeRoomMetadata generates room metadata support
	IncludeRoomMetadata bool
}

// PresenceClientConfig configures presence client generation.
type PresenceClientConfig struct {
	// Statuses are the available presence statuses
	Statuses []string

	// HeartbeatIntervalMs is the default heartbeat interval
	HeartbeatIntervalMs int

	// IdleTimeoutMs is the default idle timeout before auto-away
	IdleTimeoutMs int

	// IncludeCustomStatus enables custom status message support
	IncludeCustomStatus bool
}

// TypingClientConfig configures typing indicator client generation.
type TypingClientConfig struct {
	// TimeoutMs is the auto-stop timeout in milliseconds
	TimeoutMs int

	// DebounceMs is the debounce interval for typing events
	DebounceMs int
}

// ChannelClientConfig configures channel client generation.
type ChannelClientConfig struct {
	// MaxChannelsPerUser is the default max channels a user can subscribe to
	MaxChannelsPerUser int

	// SupportPatterns enables wildcard/pattern subscriptions
	SupportPatterns bool
}

// DefaultStreamingConfig returns sensible defaults for streaming configuration.
func DefaultStreamingConfig() StreamingConfig {
	return StreamingConfig{
		EnableRooms:            true,
		EnableChannels:         true,
		EnablePresence:         true,
		EnableTyping:           true,
		EnableHistory:          true,
		GenerateUnifiedClient:  true,
		GenerateModularClients: true,
		RoomConfig: RoomClientConfig{
			MaxRoomsPerUser:     50,
			IncludeMemberEvents: true,
			IncludeRoomMetadata: true,
		},
		PresenceConfig: PresenceClientConfig{
			Statuses:            []string{"online", "away", "busy", "offline"},
			HeartbeatIntervalMs: 30000,
			IdleTimeoutMs:       300000, // 5 minutes
			IncludeCustomStatus: true,
		},
		TypingConfig: TypingClientConfig{
			TimeoutMs:  3000,
			DebounceMs: 300,
		},
		ChannelConfig: ChannelClientConfig{
			MaxChannelsPerUser: 100,
			SupportPatterns:    false,
		},
	}
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
		Streaming: DefaultStreamingConfig(),
		// Enhanced features - enabled by default
		UseFetch:        true,
		DualPackage:     true,
		GenerateTests:   true,
		GenerateLinting: true,
		GenerateCI:      true,
		ErrorTaxonomy:   true,
		Interceptors:    true,
		Pagination:      true,
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

// WithStreamingConfig sets the streaming configuration.
func WithStreamingConfig(streaming StreamingConfig) GeneratorOption {
	return func(c *GeneratorConfig) {
		c.Streaming = streaming
	}
}

// WithRooms enables/disables room client generation.
func WithRooms(enabled bool) GeneratorOption {
	return func(c *GeneratorConfig) {
		c.Streaming.EnableRooms = enabled
	}
}

// WithChannels enables/disables channel client generation.
func WithChannels(enabled bool) GeneratorOption {
	return func(c *GeneratorConfig) {
		c.Streaming.EnableChannels = enabled
	}
}

// WithPresence enables/disables presence client generation.
func WithPresence(enabled bool) GeneratorOption {
	return func(c *GeneratorConfig) {
		c.Streaming.EnablePresence = enabled
	}
}

// WithTyping enables/disables typing indicator client generation.
func WithTyping(enabled bool) GeneratorOption {
	return func(c *GeneratorConfig) {
		c.Streaming.EnableTyping = enabled
	}
}

// WithHistory enables/disables message history support.
func WithHistory(enabled bool) GeneratorOption {
	return func(c *GeneratorConfig) {
		c.Streaming.EnableHistory = enabled
	}
}

// WithAllStreamingFeatures enables all streaming features.
func WithAllStreamingFeatures() GeneratorOption {
	return func(c *GeneratorConfig) {
		c.IncludeStreaming = true
		c.Streaming.EnableRooms = true
		c.Streaming.EnableChannels = true
		c.Streaming.EnablePresence = true
		c.Streaming.EnableTyping = true
		c.Streaming.EnableHistory = true
		c.Streaming.GenerateUnifiedClient = true
		c.Streaming.GenerateModularClients = true
	}
}

// WithUnifiedClient enables/disables unified streaming client generation.
func WithUnifiedClient(enabled bool) GeneratorOption {
	return func(c *GeneratorConfig) {
		c.Streaming.GenerateUnifiedClient = enabled
	}
}

// WithModularClients enables/disables modular streaming client generation.
func WithModularClients(enabled bool) GeneratorOption {
	return func(c *GeneratorConfig) {
		c.Streaming.GenerateModularClients = enabled
	}
}

// WithClientOnly enables generating only client source files without package config.
func WithClientOnly(enabled bool) GeneratorOption {
	return func(c *GeneratorConfig) {
		c.ClientOnly = enabled
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

// HasAnyStreamingFeature returns true if any streaming feature is enabled.
func (c *GeneratorConfig) HasAnyStreamingFeature() bool {
	return c.IncludeStreaming && (c.Streaming.EnableRooms ||
		c.Streaming.EnableChannels ||
		c.Streaming.EnablePresence ||
		c.Streaming.EnableTyping)
}

// ShouldGenerateRoomClient returns true if room client should be generated.
func (c *GeneratorConfig) ShouldGenerateRoomClient() bool {
	return c.IncludeStreaming && c.Streaming.EnableRooms && c.Streaming.GenerateModularClients
}

// ShouldGeneratePresenceClient returns true if presence client should be generated.
func (c *GeneratorConfig) ShouldGeneratePresenceClient() bool {
	return c.IncludeStreaming && c.Streaming.EnablePresence && c.Streaming.GenerateModularClients
}

// ShouldGenerateTypingClient returns true if typing client should be generated.
func (c *GeneratorConfig) ShouldGenerateTypingClient() bool {
	return c.IncludeStreaming && c.Streaming.EnableTyping && c.Streaming.GenerateModularClients
}

// ShouldGenerateChannelClient returns true if channel client should be generated.
func (c *GeneratorConfig) ShouldGenerateChannelClient() bool {
	return c.IncludeStreaming && c.Streaming.EnableChannels && c.Streaming.GenerateModularClients
}

// ShouldGenerateUnifiedStreamingClient returns true if unified streaming client should be generated.
func (c *GeneratorConfig) ShouldGenerateUnifiedStreamingClient() bool {
	return c.IncludeStreaming && c.Streaming.GenerateUnifiedClient && c.HasAnyStreamingFeature()
}
