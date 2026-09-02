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

	// Warnings lists generation-time warnings that do not abort generation
	// but are worth surfacing to whoever runs it -- e.g. an undiscriminated
	// union resolved via structural matching rather than a tag, which is
	// ambiguity a caller may want to know about. Ordered deterministically
	// by whichever generator produced them.
	Warnings []string

	// ExclusiveDirs names directories whose entire contents this generator
	// determines, as paths relative to the output directory and without a
	// trailing separator. A file found in one of them that this run did not
	// produce is stale, and the writer deletes it.
	//
	// Needed because the emitters that write one file per operation cannot
	// otherwise withdraw one. A table in a single file loses a row when the
	// file is rewritten; a directory of files loses nothing, so an operation
	// removed from the specification would leave a module behind that still
	// compiles, still exports a working hook, and still points at an endpoint
	// the server no longer serves.
	//
	// Deliberately a declaration rather than a rule inferred from Files. Every
	// directory the generator writes into is NOT owned by it -- the output
	// root holds README.md, which is written separately, and in client-only
	// mode it may hold whatever else the consuming repository keeps beside the
	// generated client. Deleting is not a thing to infer.
	ExclusiveDirs []string
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

	// FeatureRooms enables room-based streaming.
	FeatureRooms    = "rooms"
	FeaturePresence = "presence"
	FeatureTyping   = "typing"
	FeatureChannels = "channels"
	FeatureHistory  = "history"
)
