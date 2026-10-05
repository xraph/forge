package client

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// GeneratorConfig configures client generation.
type GeneratorConfig struct {
	// Language specifies the target language (go, typescript, dart)
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

	// Hooks emits the operation manifest (src/ops.ts) and typed hook facades
	// (src/hooks.ts) delegating to @forge-go/client-core.
	//
	// A layer rather than a second client: the hooks bind directly to the
	// operations the manifest describes. Off by default — it adds a
	// dependency on @forge-go/client-core, which a consumer with no need for
	// cached hooks should not inherit.
	//
	// Read this through HooksEnabled, never directly, so the deprecated
	// ReactQuery alias is honoured too.
	Hooks bool

	// ReactQuery is the former name of Hooks, kept so existing callers still
	// compile. Go has no field aliases, so both fields exist and HooksEnabled
	// ORs them; setting either one enables the layer.
	//
	// Deprecated: use Hooks. The generated code has not been TanStack Query
	// since the hook facades replaced it (see facades.go), and this name
	// describes a library the output no longer uses.
	ReactQuery bool

	// PathFilter selects which endpoints the generated client covers.
	//
	// Honoured by GenerateFromFile, which owns the spec it parses. Callers of
	// Generate hold their own *APISpec and should apply the filter themselves
	// with spec.Apply — silently mutating an argument would be a surprise.
	PathFilter PathFilter

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

	// FieldNaming selects the client-side identifier style for schema properties.
	// The wire name always comes from the spec. The TypeScript and Dart
	// generators read this field; the Go generator ignores it. Defaults to
	// NamingCamel when Language is "typescript" or "dart", and to
	// NamingPreserve otherwise.
	FieldNaming NamingStrategy

	// FieldOverrides maps a wire name to an explicit client-side name. A key of
	// "Schema.wire_name" applies to that schema only; a bare "wire_name" applies
	// globally. A schema-scoped entry wins over a global one for the same wire
	// name. Overrides bypass FieldNaming entirely and are used verbatim.
	FieldOverrides map[string]string

	// StripPrefixes are the leading service prefixes ("Studio_", "Portal_")
	// removed from schema names, operation ids, entity typenames and cache tags
	// before generation.
	//
	// This is a TYPE-level rename and has nothing to do with FieldNaming, which
	// renames a schema's properties. The two compose: a client may strip
	// `Studio_` from its typenames and still camelCase their fields.
	//
	// Meant for a client generated from one service's slice of a merged
	// gateway document, where the prefix that disambiguated the merge is noise.
	// A SET rather than one prefix because a service re-describes types it does
	// not own: identity's document carries `Portal_WorkspaceResponse` for the
	// same record portal's own client calls `WorkspaceResponse`, and stripping
	// only identity's own prefix leaves two names for one record. See
	// StripPrefix in stripprefix.go for that and for the collision rule.
	//
	// Empty disables it, which is the default and the behaviour every existing
	// configuration keeps.
	StripPrefixes []string

	// Int64 selects how the Dart generator types an int64 schema: Int64String
	// (the default, an extension type over String that keeps every digit on
	// the web) or Int64Int (a raw int, for apps that never target the web).
	// Other generators ignore it.
	Int64 Int64Mode

	// EmitTablesJSON makes the TypeScript and Dart generators also write
	// forge-tables.json, their ops, entities, streams and capability tables
	// as canonical JSON. Not exposed on the command line: it exists for the
	// cross-generator parity test, which compares the two files.
	EmitTablesJSON bool
}

// Int64Mode selects the Dart representation of an int64 schema.
type Int64Mode string

const (
	// Int64String carries int64 values as decimal strings. The zero value
	// means the same thing.
	Int64String Int64Mode = "string"
	// Int64Int carries int64 values as Dart ints, which lose precision above
	// 2^53 on the web.
	Int64Int Int64Mode = "int"
)

// HooksEnabled reports whether the operation manifest and hook facade layer
// should be emitted, honouring the deprecated ReactQuery alias for Hooks.
//
// Every read of the gate goes through here so the two fields are reconciled in
// exactly one place; the generators never touch Hooks or ReactQuery directly.
func (c GeneratorConfig) HooksEnabled() bool {
	return c.Hooks || c.ReactQuery
}

// NamingStrategy selects a target identifier style.
type NamingStrategy string

const (
	NamingCamel    NamingStrategy = "camel"
	NamingPascal   NamingStrategy = "pascal"
	NamingSnake    NamingStrategy = "snake"
	NamingPreserve NamingStrategy = "preserve"
)

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
	supportedLanguages := []string{"go", "typescript", "ts", "dart"}
	if !contains(supportedLanguages, c.Language) {
		return fmt.Errorf("unsupported language: %s (supported: go, typescript, dart)", c.Language)
	}

	switch c.Int64 {
	case "", Int64String, Int64Int:
	default:
		return fmt.Errorf("invalid int64 mode %q (supported: string, int)", c.Int64)
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
	case "dart":
		if !isValidDartPackageName(c.PackageName) {
			return fmt.Errorf("invalid Dart package name: %s (must be lowercase_with_underscores, start with a letter or underscore, and not be a Dart reserved word)", c.PackageName)
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

		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}

	return true
}

// dartReservedPackageNames are the words pub refuses as package names, because
// a package name is also the identifier its library is imported as.
var dartReservedPackageNames = map[string]bool{
	"abstract": true, "as": true, "assert": true, "async": true, "await": true, "base": true,
	"break": true, "case": true, "catch": true, "class": true, "const": true, "continue": true,
	"covariant": true, "default": true, "deferred": true, "do": true, "dynamic": true, "else": true,
	"enum": true, "export": true, "extends": true, "extension": true, "external": true, "factory": true,
	"false": true, "final": true, "finally": true, "for": true, "function": true, "get": true,
	"hide": true, "if": true, "implements": true, "import": true, "in": true, "interface": true,
	"is": true, "late": true, "library": true, "mixin": true, "new": true, "null": true, "of": true,
	"on": true, "operator": true, "part": true, "required": true, "rethrow": true, "return": true,
	"sealed": true, "set": true, "show": true, "static": true, "super": true, "switch": true,
	"sync": true, "this": true, "throw": true, "true": true, "try": true, "type": true,
	"typedef": true, "var": true, "void": true, "when": true, "while": true, "with": true, "yield": true,
}

// isValidDartPackageName checks a pub package name: lowercase letters, digits
// and underscores, not starting with a digit, and not a reserved word.
func isValidDartPackageName(name string) bool {
	if name == "" || dartReservedPackageNames[name] {
		return false
	}

	for i, c := range name {
		switch {
		case c >= 'a' && c <= 'z', c == '_':
		case c >= '0' && c <= '9' && i > 0:
		default:
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
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' && c != '_' {
			return false
		}
	}

	return true
}

// contains checks if a slice contains a string.
func contains(slice []string, item string) bool {
	return slices.Contains(slice, item)
}

// GeneratorOption provides functional options for generator config.
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
