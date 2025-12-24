package generators

import (
	"context"
)

// APISpec is a forward declaration to avoid import cycle
// The actual type is defined in the client package.
type APISpec any

// GeneratorConfig is a forward declaration to avoid import cycle
// The actual type is defined in the client package.
type GeneratorConfig any

// LanguageGenerator defines the interface for language-specific client generators.
type LanguageGenerator interface {
	// Name returns the generator name (e.g., "go", "typescript")
	Name() string

	// SupportedFeatures returns a list of features this generator supports
	SupportedFeatures() []string

	// Generate produces client code from the API specification
	Generate(ctx context.Context, spec APISpec, config GeneratorConfig) (*GeneratedClient, error)

	// Validate checks if the spec can be generated for this language
	Validate(spec APISpec) error
}

// GeneratedClient represents the generated client code.
type GeneratedClient struct {
	// Files maps filename to file contents
	Files map[string]string

	// Instructions provides setup/usage instructions for the client
	Instructions string

	// Dependencies lists required dependencies
	Dependencies []Dependency

	// Language is the target language
	Language string

	// Version is the generated client version
	Version string
}

// Dependency represents a required dependency.
type Dependency struct {
	Name    string
	Version string
	Type    string // "direct", "dev", "peer"
}

// Feature constants for common features.
const (
	FeatureREST              = "rest"
	FeatureWebSocket         = "websocket"
	FeatureSSE               = "sse"
	FeatureWebTransport      = "webtransport"
	FeatureAuth              = "auth"
	FeatureReconnection      = "reconnection"
	FeatureHeartbeat         = "heartbeat"
	FeatureStateManagement   = "state-management"
	FeatureTypedErrors       = "typed-errors"
	FeatureRequestRetry      = "request-retry"
	FeatureTimeout           = "timeout"
	FeatureMiddleware        = "middleware"
	FeatureLogging           = "logging"
	FeaturePolymorphicTypes  = "polymorphic-types"
	FeatureFileUpload        = "file-upload"
	FeatureStreamingResponse = "streaming-response"

	// Streaming extension features
	FeatureRooms    = "rooms"
	FeaturePresence = "presence"
	FeatureTyping   = "typing"
	FeatureChannels = "channels"
	FeatureHistory  = "history"
)
