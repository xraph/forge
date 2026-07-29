package typescript

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/internal/client"
	"github.com/xraph/forge/internal/client/generators"
)

// tsImportLine builds an `import { ... } from './types';` line from names,
// omitting "AuthConfig" when auth is disabled while preserving every other
// listed name and their order. Shared by the streaming generators (rooms,
// presence, typing, channels, streaming_client), which each import a
// different, sometimes long, set of names from './types'.
func tsImportLine(config client.GeneratorConfig, names ...string) string {
	kept := make([]string, 0, len(names))

	for _, name := range names {
		if name == "AuthConfig" && !config.IncludeAuth {
			continue
		}

		kept = append(kept, name)
	}

	return fmt.Sprintf("import { %s } from './types';", strings.Join(kept, ", "))
}

// sortedKeys returns the keys of m in ascending order. Generated output must be
// byte-identical across runs, and Go randomizes map iteration.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	return keys
}

// formatTSType maps an OpenAPI format to a TypeScript type, returning "" when
// the format carries no type information and the base type should be used.
//
// Two judgement calls are encoded here:
//   - "date-time" is deliberately NOT mapped to "Date": JSON.parse produces a
//     plain string, and nothing in the generated runtime revives it into a
//     Date, so emitting Date would be a type that lies about the runtime
//     value. It falls through to the ordinary string handling.
//   - "int64"/"uint64" become "string" rather than "number": values beyond
//     Number.MAX_SAFE_INTEGER (2^53-1) silently lose precision as a JS
//     number. Carrying them as decimal strings matches what most other
//     OpenAPI-to-TypeScript generators do for 64-bit integers.
func formatTSType(schema *client.Schema) string {
	if schema == nil {
		return ""
	}

	switch schema.Format {
	case "binary":
		return "Blob"
	case "int64", "uint64":
		return "string"
	}

	return ""
}

// enumTSType renders schema.Enum as a TypeScript literal union (e.g.
// `"active" | "off"` or `1 | 2 | 3`), or "" when the schema is not an enum.
// Shared by both schemaToTSType implementations (generator.go and rest.go) so
// the escaping logic — and the bug fix below — lives in exactly one place.
//
// String values are escaped via json.Marshal, the same mechanism
// tsPropertyKey uses to escape object keys. Interpolating with
// fmt.Sprintf("'%v'", v) — the code this replaces — breaks the entire
// generated file the moment a value contains a quote (e.g. "it's" produces
// the unterminated literal 'it's'); json.Marshal handles quotes, backslashes,
// control characters, and non-ASCII correctly without hand-rolled escaping.
// This also means string literals are double-quoted (`"active"`) rather than
// single-quoted — both are valid TypeScript, and this keeps the escaping
// consistent with tsPropertyKey rather than introducing a second scheme.
//
// Non-string scalars (bool, nil, numbers) are rendered with their natural
// literal form. A nil entry (JSON null is a legal enum member) renders as the
// TS literal `null`. An enum mixing types (e.g. a string, a number, and a
// bool in the same list) renders each value as a literal of its own type,
// producing a heterogeneous union — TypeScript literal unions support mixing
// literal kinds natively, and OpenAPI/JSON Schema does not forbid
// heterogeneous enum arrays.
func enumTSType(schema *client.Schema) string {
	if schema == nil || len(schema.Enum) == 0 {
		return ""
	}

	parts := make([]string, 0, len(schema.Enum))

	for _, v := range schema.Enum {
		switch tv := v.(type) {
		case string:
			b, _ := json.Marshal(tv)
			parts = append(parts, string(b))
		case bool:
			parts = append(parts, fmt.Sprintf("%t", tv))
		case nil:
			parts = append(parts, "null")
		default:
			parts = append(parts, fmt.Sprintf("%v", tv))
		}
	}

	return strings.Join(parts, " | ")
}

// reservedStreamingTypeNames returns the six interface names
// generateStreamingTypes may emit verbatim: Message, Member, Room,
// RoomOptions, HistoryQuery, UserPresence. Not all six are emitted for every
// config — see checkSchemaNameCollisions, which reflects the actual
// per-name emission conditions rather than treating this list as a blanket
// reservation. Exposed so later tasks and tests share one list instead of
// duplicating the string literals.
func reservedStreamingTypeNames() []string {
	return []string{"Message", "Member", "Room", "RoomOptions", "HistoryQuery", "UserPresence"}
}

// checkSchemaNameCollisions reports schema names that collide with a
// streaming interface name that generateStreamingTypes will actually emit
// for this config. Message, Member, Room, and RoomOptions are emitted
// whenever Streaming.EnableRooms is set; HistoryQuery is additionally gated
// on Streaming.EnableHistory (nested under EnableRooms in
// generateStreamingTypes); UserPresence is gated independently on
// Streaming.EnablePresence. A name that is only reserved under a condition
// that's off for this config is not a collision — for example a schema
// named "Message" with streaming disabled entirely, or "HistoryQuery" with
// rooms on but history off, must generate successfully.
func checkSchemaNameCollisions(spec *client.APISpec, config client.GeneratorConfig) error {
	if !config.HasAnyStreamingFeature() {
		return nil
	}

	reserved := make(map[string]bool, 6)

	if config.Streaming.EnableRooms {
		reserved["Message"] = true
		reserved["Member"] = true
		reserved["Room"] = true
		reserved["RoomOptions"] = true

		if config.Streaming.EnableHistory {
			reserved["HistoryQuery"] = true
		}
	}

	if config.Streaming.EnablePresence {
		reserved["UserPresence"] = true
	}

	for _, name := range sortedKeys(spec.Schemas) {
		if reserved[name] {
			return fmt.Errorf(
				"schema %q collides with a generated streaming type; rename the schema or disable streaming features",
				name)
		}
	}

	return nil
}

// Generator generates TypeScript clients.
type Generator struct{}

// NewGenerator creates a new TypeScript generator.
func NewGenerator() generators.LanguageGenerator {
	return &Generator{}
}

// Name returns the generator name.
func (g *Generator) Name() string {
	return "typescript"
}

// SupportedFeatures returns supported features.
func (g *Generator) SupportedFeatures() []string {
	return []string{
		generators.FeatureREST,
		generators.FeatureWebSocket,
		generators.FeatureSSE,
		generators.FeatureWebTransport,
		generators.FeatureAuth,
		generators.FeatureReconnection,
		generators.FeatureHeartbeat,
		generators.FeatureStateManagement,
		generators.FeatureTypedErrors,
		generators.FeatureRooms,
		generators.FeaturePresence,
		generators.FeatureTyping,
		generators.FeatureChannels,
	}
}

// Validate validates the spec for TypeScript generation.
func (g *Generator) Validate(specIface generators.APISpec) error {
	spec, ok := specIface.(*client.APISpec)
	if !ok || spec == nil {
		return errors.New("spec is nil or invalid type")
	}

	if spec.Info.Title == "" {
		return errors.New("API title is required")
	}

	return nil
}

// Generate generates the TypeScript client.
func (g *Generator) Generate(ctx context.Context, specIface generators.APISpec, configIface generators.GeneratorConfig) (*generators.GeneratedClient, error) {
	spec, ok := specIface.(*client.APISpec)
	if !ok || spec == nil {
		return nil, errors.New("spec is nil or invalid type")
	}

	config, ok := configIface.(client.GeneratorConfig)
	if !ok {
		return nil, errors.New("config is invalid type")
	}

	if err := checkSchemaNameCollisions(spec, config); err != nil {
		return nil, err
	}

	if err := checkFieldNameCollisions(spec, config); err != nil {
		return nil, err
	}

	genClient := &generators.GeneratedClient{
		Files:        make(map[string]string),
		Language:     "typescript",
		Version:      config.Version,
		Dependencies: g.getDependencies(config),
	}

	// Generate package configuration files (unless client-only mode)
	if !config.ClientOnly {
		// Generate package.json
		packageJSON := g.generatePackageJSON(spec, config)
		genClient.Files["package.json"] = packageJSON

		// Generate tsconfig.json
		tsconfigJSON := g.generateTSConfig()
		genClient.Files["tsconfig.json"] = tsconfigJSON
	}

	// Determine if we're in AsyncAPI-only mode (streaming only, no REST endpoints)
	isAsyncAPIOnly := config.HasAnyStreamingFeature() && len(spec.Endpoints) == 0

	// Generate REST client files (unless AsyncAPI-only mode)
	if !isAsyncAPIOnly {
		// Generate fetch client
		fetchGen := NewFetchClientGenerator()
		fetchCode := fetchGen.GenerateBaseClient(spec, config)
		genClient.Files["src/fetch.ts"] = fetchCode

		// Generate error classes
		errorGen := NewErrorGenerator()
		errorCode := errorGen.Generate(spec, config)
		genClient.Files["src/errors.ts"] = errorCode

		// Generate main client
		clientCode := g.generateClient(spec, config)
		genClient.Files["src/client.ts"] = clientCode

		// Generate REST methods
		if len(spec.Endpoints) > 0 {
			restGen := NewRESTGenerator()
			restCode, restWarnings := restGen.Generate(spec, config)
			genClient.Files["src/rest.ts"] = restCode
			genClient.Warnings = append(genClient.Warnings, restWarnings...)
		}

		// Generate pagination helpers if enabled
		if config.Pagination && len(spec.Endpoints) > 0 {
			paginationGen := NewPaginationGenerator()
			paginationCode := paginationGen.GeneratePaginationHelpers(spec, config)
			genClient.Files["src/pagination.ts"] = paginationCode
		}
	}

	// Generate types (always needed)
	typesCode := g.generateTypes(spec, config)
	genClient.Files["src/types.ts"] = typesCode

	// Generate the codec table. Always emitted for now: the config field
	// that will let a caller opt out (preserving wire casing, making the
	// table pointless) does not exist yet.
	codecCode, codecWarnings := NewCodecGenerator().Generate(spec, config)
	genClient.Files["src/codecs.ts"] = codecCode
	genClient.Warnings = append(genClient.Warnings, codecWarnings...)

	// Generate WebSocket clients
	if len(spec.WebSockets) > 0 && config.IncludeStreaming {
		wsGen := NewWebSocketGenerator()
		wsCode := wsGen.Generate(spec, config)
		genClient.Files["src/websocket.ts"] = wsCode
	}

	// Generate SSE clients
	if len(spec.SSEs) > 0 && config.IncludeStreaming {
		sseGen := NewSSEGenerator()
		sseCode := sseGen.Generate(spec, config)
		genClient.Files["src/sse.ts"] = sseCode
	}

	// Generate WebTransport clients
	if len(spec.WebTransports) > 0 && config.IncludeStreaming {
		wtGen := NewWebTransportGenerator()
		wtCode := wtGen.Generate(spec, config)
		genClient.Files["src/webtransport.ts"] = wtCode
	}

	// Generate event emitter utility for streaming clients
	if config.HasAnyStreamingFeature() || (config.IncludeStreaming && (len(spec.WebSockets) > 0 || len(spec.SSEs) > 0)) {
		eventsCode := g.generateEventEmitter()
		genClient.Files["src/events.ts"] = eventsCode
	}

	// Generate modular streaming clients
	if config.Streaming.GenerateModularClients {
		// Generate RoomClient
		if config.ShouldGenerateRoomClient() {
			roomsGen := NewRoomsGenerator()
			roomsCode := roomsGen.Generate(spec, config)
			genClient.Files["src/rooms.ts"] = roomsCode
		}

		// Generate PresenceClient
		if config.ShouldGeneratePresenceClient() {
			presenceGen := NewPresenceGenerator()
			presenceCode := presenceGen.Generate(spec, config)
			genClient.Files["src/presence.ts"] = presenceCode
		}

		// Generate TypingClient
		if config.ShouldGenerateTypingClient() {
			typingGen := NewTypingGenerator()
			typingCode := typingGen.Generate(spec, config)
			genClient.Files["src/typing.ts"] = typingCode
		}

		// Generate ChannelClient
		if config.ShouldGenerateChannelClient() {
			channelsGen := NewChannelsGenerator()
			channelsCode := channelsGen.Generate(spec, config)
			genClient.Files["src/channels.ts"] = channelsCode
		}
	}

	// Generate unified StreamingClient
	if config.ShouldGenerateUnifiedStreamingClient() {
		streamingGen := NewStreamingClientGenerator()
		streamingCode := streamingGen.Generate(spec, config)
		genClient.Files["src/streaming.ts"] = streamingCode
	}

	// Generate index (barrel export)
	indexCode := g.generateIndex(spec, config)
	genClient.Files["src/index.ts"] = indexCode

	// Generate project configuration files (unless client-only mode)
	if !config.ClientOnly {
		// Generate testing setup if enabled
		if config.GenerateTests {
			testGen := NewTestingGenerator()
			genClient.Files["jest.config.js"] = testGen.GenerateJestConfig(spec, config)
			genClient.Files["tests/client.test.ts"] = testGen.GenerateExampleTest(spec, config)
			genClient.Files["tests/utils.ts"] = testGen.GenerateTestUtils(spec, config)
		}

		// Generate linting setup if enabled
		if config.GenerateLinting {
			lintGen := NewLintingGenerator()
			genClient.Files[".eslintrc.js"] = lintGen.GenerateESLintConfig(spec, config)
			genClient.Files[".prettierrc"] = lintGen.GeneratePrettierConfig(spec, config)
			genClient.Files[".prettierignore"] = lintGen.GeneratePrettierIgnore(spec, config)
			genClient.Files[".eslintignore"] = lintGen.GenerateESLintIgnore(spec, config)
		}

		// Generate CI setup if enabled
		if config.GenerateCI {
			ciGen := NewCIGenerator()
			genClient.Files[".github/workflows/ci.yml"] = ciGen.GenerateGitHubActions(spec, config)
			genClient.Files[".gitignore"] = ciGen.GenerateGitIgnore(spec, config)
		}

		// Generate .npmignore
		npmIgnoreGen := NewNPMIgnoreGenerator()
		genClient.Files[".npmignore"] = npmIgnoreGen.Generate(spec, config)
	}

	// Generate instructions
	genClient.Instructions = g.generateInstructions(spec, config)

	// If client-only mode, remove 'src/' prefix from all file paths
	if config.ClientOnly {
		newFiles := make(map[string]string)

		for path, content := range genClient.Files {
			// Remove 'src/' prefix if present
			newPath := strings.TrimPrefix(path, "src/")
			newFiles[newPath] = content
		}

		genClient.Files = newFiles
	}

	return genClient, nil
}

// generatePackageJSON generates package.json.
func (g *Generator) generatePackageJSON(spec *client.APISpec, config client.GeneratorConfig) string {
	packageName := config.PackageName
	if packageName == "" {
		packageName = strings.ToLower(strings.ReplaceAll(spec.Info.Title, " ", "-"))
	}

	deps := make(map[string]string)

	// Only add streaming deps if needed (Node.js polyfills)
	if config.IncludeStreaming {
		deps["ws"] = "^8.16.0"
		deps["eventsource"] = "^2.0.2"
	}

	depsJSON := "{\n"

	if len(deps) > 0 {
		first := true

		var depsJSONSb strings.Builder

		for _, name := range sortedKeys(deps) {
			if !first {
				depsJSONSb.WriteString(",\n")
			}

			depsJSONSb.WriteString(fmt.Sprintf("    \"%s\": \"%s\"", name, deps[name]))

			first = false
		}

		depsJSON += depsJSONSb.String() + "\n  }"
	} else {
		depsJSON = "{}"
	}

	// Modern dual package structure
	return fmt.Sprintf(`{
  "name": "%s",
  "version": "%s",
  "description": "%s",
  "type": "module",
  "main": "./dist/index.cjs",
  "module": "./dist/index.mjs",
  "types": "./dist/index.d.ts",
  "exports": {
    ".": {
      "import": "./dist/index.mjs",
      "require": "./dist/index.cjs",
      "types": "./dist/index.d.ts"
    }
  },
  "scripts": {
    "build": "tsup src/index.ts --format cjs,esm --dts --clean",
    "prepublish": "npm run build",
    "test": "jest",
    "lint": "eslint src --ext .ts",
    "format": "prettier --write \"src/**/*.ts\""
  },
  "dependencies": %s,
  "devDependencies": {
    "@types/node": "^20.0.0",
    "@types/ws": "^8.5.0",
    "typescript": "^5.3.0",
    "tsup": "^8.0.0",
    "eslint": "^8.55.0",
    "@typescript-eslint/eslint-plugin": "^6.15.0",
    "@typescript-eslint/parser": "^6.15.0",
    "prettier": "^3.1.1",
    "jest": "^29.7.0",
    "@types/jest": "^29.5.11"
  },
  "files": [
    "dist"
  ],
  "engines": {
    "node": ">=18.0.0"
  }
}
`, packageName, config.Version, spec.Info.Description, depsJSON)
}

// generateTSConfig generates tsconfig.json.
func (g *Generator) generateTSConfig() string {
	return `{
  "compilerOptions": {
    "target": "ES2020",
    "module": "ESNext",
    "lib": ["ES2020", "DOM"],
    "declaration": true,
    "declarationMap": true,
    "outDir": "./dist",
    "rootDir": "./src",
    "strict": true,
    "esModuleInterop": true,
    "skipLibCheck": true,
    "forceConsistentCasingInFileNames": true,
    "moduleResolution": "bundler",
    "resolveJsonModule": true,
    "isolatedModules": true,
    "noEmit": true
  },
  "include": ["src/**/*"],
  "exclude": ["node_modules", "dist", "**/*.test.ts"]
}
`
}

// generateTypes generates types.ts.
func (g *Generator) generateTypes(spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	buf.WriteString("// Generated types\n\n")

	// Add ConnectionState enum for streaming
	if config.IncludeStreaming {
		buf.WriteString("export enum ConnectionState {\n")
		buf.WriteString("  DISCONNECTED = 'disconnected',\n")
		buf.WriteString("  CONNECTING = 'connecting',\n")
		buf.WriteString("  CONNECTED = 'connected',\n")
		buf.WriteString("  RECONNECTING = 'reconnecting',\n")
		buf.WriteString("  CLOSED = 'closed',\n")
		buf.WriteString("  ERROR = 'error',\n")
		buf.WriteString("}\n\n")
	}

	// Generate types from schemas
	for _, name := range sortedKeys(spec.Schemas) {
		typeCode := g.schemaToTypeScript(name, spec.Schemas[name], spec, config)
		buf.WriteString(typeCode)
		buf.WriteString("\n")
	}

	// Auth config interface. Emitted whenever auth is enabled, because
	// ClientConfig.auth and the client.ts import are both gated on IncludeAuth
	// alone — a narrower condition here leaves those references unresolved.
	if config.IncludeAuth {
		buf.WriteString("export interface AuthConfig {\n")
		buf.WriteString("  bearerToken?: string;\n")
		buf.WriteString("  apiKey?: string;\n")
		buf.WriteString("  customHeaders?: Record<string, string>;\n")
		buf.WriteString("}\n\n")
	}

	// Client config interface
	buf.WriteString("export interface ClientConfig {\n")
	buf.WriteString("  baseURL: string;\n")

	if config.IncludeAuth {
		buf.WriteString("  auth?: AuthConfig;\n")
	}

	buf.WriteString("  timeout?: number;\n")
	buf.WriteString("}\n\n")

	// Generate streaming types if streaming features are enabled
	if config.HasAnyStreamingFeature() {
		buf.WriteString(g.generateStreamingTypes(spec, config))
	}

	return buf.String()
}

// generateStreamingTypes generates streaming-related type definitions.
func (g *Generator) generateStreamingTypes(spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	buf.WriteString("// Streaming Types\n\n")

	// Message type (common for rooms)
	if config.Streaming.EnableRooms {
		buf.WriteString("/**\n * Represents a message in a room\n */\n")
		buf.WriteString("export interface Message {\n")
		buf.WriteString("  /** Message ID */\n")
		buf.WriteString("  id?: string;\n")
		buf.WriteString("  /** Room ID */\n")
		buf.WriteString("  room_id: string;\n")
		buf.WriteString("  /** Sender user ID */\n")
		buf.WriteString("  user_id?: string;\n")
		buf.WriteString("  /** Message type */\n")
		buf.WriteString("  type: string;\n")
		buf.WriteString("  /** Message data/content */\n")
		buf.WriteString("  data: any;\n")
		buf.WriteString("  /** Timestamp */\n")
		buf.WriteString("  timestamp: string;\n")
		buf.WriteString("  /** Optional metadata */\n")
		buf.WriteString("  metadata?: Record<string, any>;\n")
		buf.WriteString("}\n\n")

		// Member type
		buf.WriteString("/**\n * Represents a member in a room\n */\n")
		buf.WriteString("export interface Member {\n")
		buf.WriteString("  /** User ID */\n")
		buf.WriteString("  user_id: string;\n")
		buf.WriteString("  /** Display name */\n")
		buf.WriteString("  display_name?: string;\n")
		buf.WriteString("  /** Avatar URL */\n")
		buf.WriteString("  avatar_url?: string;\n")
		buf.WriteString("  /** Member role in the room */\n")
		buf.WriteString("  role?: string;\n")
		buf.WriteString("  /** When the member joined */\n")
		buf.WriteString("  joined_at?: string;\n")
		buf.WriteString("  /** Custom metadata */\n")
		buf.WriteString("  metadata?: Record<string, any>;\n")
		buf.WriteString("}\n\n")

		// Room type
		buf.WriteString("/**\n * Represents a room\n */\n")
		buf.WriteString("export interface Room {\n")
		buf.WriteString("  /** Room ID */\n")
		buf.WriteString("  id: string;\n")
		buf.WriteString("  /** Room name */\n")
		buf.WriteString("  name?: string;\n")
		buf.WriteString("  /** Room description */\n")
		buf.WriteString("  description?: string;\n")
		buf.WriteString("  /** Room type */\n")
		buf.WriteString("  type?: string;\n")
		buf.WriteString("  /** Creator user ID */\n")
		buf.WriteString("  created_by?: string;\n")
		buf.WriteString("  /** When the room was created */\n")
		buf.WriteString("  created_at?: string;\n")
		buf.WriteString("  /** Custom metadata */\n")
		buf.WriteString("  metadata?: Record<string, any>;\n")
		buf.WriteString("}\n\n")

		// RoomOptions type
		buf.WriteString("/**\n * Options for room creation/configuration\n */\n")
		buf.WriteString("export interface RoomOptions {\n")
		buf.WriteString("  /** Room name */\n")
		buf.WriteString("  name?: string;\n")
		buf.WriteString("  /** Room type */\n")
		buf.WriteString("  type?: string;\n")
		buf.WriteString("  /** Maximum members allowed */\n")
		buf.WriteString("  max_members?: number;\n")
		buf.WriteString("  /** Whether room is private */\n")
		buf.WriteString("  is_private?: boolean;\n")
		buf.WriteString("  /** Custom metadata */\n")
		buf.WriteString("  metadata?: Record<string, any>;\n")
		buf.WriteString("}\n\n")

		// HistoryQuery type
		if config.Streaming.EnableHistory {
			buf.WriteString("/**\n * Query parameters for message history\n */\n")
			buf.WriteString("export interface HistoryQuery {\n")
			buf.WriteString("  /** Maximum number of messages to return */\n")
			buf.WriteString("  limit?: number;\n")
			buf.WriteString("  /** Return messages before this timestamp */\n")
			buf.WriteString("  before?: string;\n")
			buf.WriteString("  /** Return messages after this timestamp */\n")
			buf.WriteString("  after?: string;\n")
			buf.WriteString("  /** Return messages before this message ID */\n")
			buf.WriteString("  before_id?: string;\n")
			buf.WriteString("  /** Return messages after this message ID */\n")
			buf.WriteString("  after_id?: string;\n")
			buf.WriteString("}\n\n")
		}
	}

	// UserPresence type (for presence)
	if config.Streaming.EnablePresence {
		buf.WriteString("/**\n * Represents a user's presence status\n */\n")
		buf.WriteString("export interface UserPresence {\n")
		buf.WriteString("  /** User ID */\n")
		buf.WriteString("  userId: string;\n")
		buf.WriteString("  /** Current status */\n")
		buf.WriteString("  status: string;\n")
		buf.WriteString("  /** Custom status message */\n")
		buf.WriteString("  customMessage?: string;\n")
		buf.WriteString("  /** Last seen timestamp */\n")
		buf.WriteString("  lastSeen?: string;\n")
		buf.WriteString("  /** Current room ID (if in a room) */\n")
		buf.WriteString("  roomId?: string;\n")
		buf.WriteString("  /** Custom metadata */\n")
		buf.WriteString("  metadata?: Record<string, any>;\n")
		buf.WriteString("}\n\n")
	}

	return buf.String()
}

// generateEventEmitter generates a simple EventEmitter implementation.
func (g *Generator) generateEventEmitter() string {
	return `// Simple EventEmitter implementation for streaming clients

export type EventHandler = (...args: any[]) => void;

/**
 * Simple EventEmitter for managing event subscriptions.
 */
export class EventEmitter {
  private events: Map<string, Set<EventHandler>> = new Map();

  /**
   * Register an event handler.
   * @param event - Event name
   * @param handler - Handler function
   */
  on(event: string, handler: EventHandler): void {
    if (!this.events.has(event)) {
      this.events.set(event, new Set());
    }
    this.events.get(event)!.add(handler);
  }

  /**
   * Register a one-time event handler.
   * @param event - Event name
   * @param handler - Handler function
   */
  once(event: string, handler: EventHandler): void {
    const onceHandler = (...args: any[]) => {
      this.off(event, onceHandler);
      handler(...args);
    };
    this.on(event, onceHandler);
  }

  /**
   * Remove an event handler.
   * @param event - Event name
   * @param handler - Handler function to remove
   */
  off(event: string, handler: EventHandler): void {
    this.events.get(event)?.delete(handler);
  }

  /**
   * Remove all handlers for an event or all events.
   * @param event - Optional event name; if omitted, clears all events
   */
  removeAllListeners(event?: string): void {
    if (event) {
      this.events.delete(event);
    } else {
      this.events.clear();
    }
  }

  /**
   * Emit an event with arguments.
   * @param event - Event name
   * @param args - Arguments to pass to handlers
   */
  protected emit(event: string, ...args: any[]): void {
    const handlers = this.events.get(event);
    if (handlers) {
      handlers.forEach(handler => {
        try {
          handler(...args);
        } catch (error) {
          console.error('Event handler error:', error);
        }
      });
    }
  }

  /**
   * Get the number of listeners for an event.
   * @param event - Event name
   * @returns Number of listeners
   */
  listenerCount(event: string): number {
    return this.events.get(event)?.size || 0;
  }

  /**
   * Get all registered event names.
   * @returns Array of event names
   */
  eventNames(): string[] {
    return Array.from(this.events.keys());
  }
}
`
}

// additionalPropsSchema interprets Schema.AdditionalProperties, which the IR
// types as `any` because JSON Schema allows either a bool or a schema.
// Returns (valueSchema, allowed). A nil valueSchema with allowed=true means
// "any value". A nil valueSchema with allowed=false means additional
// properties are absent or explicitly disallowed — the ordinary closed
// interface case.
//
// The IR field is populated by copying shared.Schema.AdditionalProperties
// (also `any`) straight through in both spec_parser.go and introspector.go.
// shared.Schema has no custom UnmarshalJSON for that field, so when a spec is
// parsed from a JSON/YAML document (spec_parser.go), a schema-valued
// `additionalProperties` decodes via encoding/json's generic `any` handling
// to map[string]any, not *client.Schema — only a bool or a genuine
// *client.Schema constructed in Go (e.g. by the introspector, or by tests
// building the IR directly) take the other two branches. That map[string]any
// case is real and reachable in this codebase, but is deliberately not
// normalised into a *client.Schema here: doing so is a separate piece of
// work (re-running schema conversion on a raw map) outside this fix's scope.
func additionalPropsSchema(v any) (*client.Schema, bool) {
	switch t := v.(type) {
	case nil:
		return nil, false
	case bool:
		return nil, t
	case *client.Schema:
		return t, true
	}

	return nil, false
}

// objectPropsLiteral renders schema.Properties as a TypeScript object type
// literal body ("{ ... }"), including per-property JSDoc. Shared by the
// ordinary `export interface` case and the additionalProperties intersection
// case, both of which need the identical property rendering — only the
// wrapper differs (interface vs. `{...} & Record<string, V>`).
//
// nsID is the object namespace's own id, in the exact scheme fieldname.go's
// checkSchemaFieldCollisions and codecs.go's codecIDFor already use: a
// top-level schema's own name ("User"), or a synthetic dotted path for an
// inline nested object ("Order.shipping", "Order.line_items.items",
// "Order.extras."+additionalPropertiesSegment, "Order.payment.oneOf0",
// ...). tsFieldName's
// schema-scoped override lookup is keyed by nsID + "." + wireName, so all
// three consumers of that namespace id -- the collision guard, the codec
// table, and this renderer -- must agree on it; otherwise a FieldOverrides
// entry that silences a collision error would not apply here, silently
// losing data instead of renaming it.
func (g *Generator) objectPropsLiteral(schema *client.Schema, spec *client.APISpec, nsID string, config client.GeneratorConfig) string {
	var buf strings.Builder

	buf.WriteString("{\n")

	for _, propName := range sortedKeys(schema.Properties) {
		prop := schema.Properties[propName]
		required := contains(schema.Required, propName)

		optional := ""
		if !required {
			optional = "?"
		}

		buf.WriteString(propertyJSDoc(prop, "  "))

		clientName := tsFieldName(nsID, propName, config)
		tsType := g.schemaToTSType(prop, spec, nsID+"."+propName, config)
		buf.WriteString(fmt.Sprintf("  %s%s: %s;\n", tsPropertyKey(clientName), optional, tsType))
	}

	buf.WriteString("}")

	return buf.String()
}

// schemaToTypeScript converts a schema to TypeScript. name doubles as the
// schema's own namespace id (see objectPropsLiteral's doc comment) for every
// property tsFieldName resolves at this level.
func (g *Generator) schemaToTypeScript(name string, schema *client.Schema, spec *client.APISpec, config client.GeneratorConfig) string {
	if schema == nil {
		return ""
	}

	var buf strings.Builder

	// Schema-level description, rendered as a JSDoc block above the export.
	// Schemas don't carry Deprecated as a standalone concept the way
	// properties do here, but propertyJSDoc already handles "description
	// only" cleanly, so it's reused as-is.
	buf.WriteString(propertyJSDoc(schema, ""))

	// Polymorphic schemas (oneOf/anyOf/allOf) are handled before the
	// switch on schema.Type below, for two reasons:
	//
	//  1. A schema that is *purely* polymorphic (no sibling "type") has an
	//     empty Schema.Type, so it would fall to the switch's default
	//     branch anyway — which already delegates to schemaToTSType, so
	//     this first case is a no-op for that shape today. It is kept
	//     explicit rather than relying on the default branch because (2)
	//     below is a real, reachable divergence.
	//  2. A schema that declares "type: object" *alongside* oneOf/anyOf/
	//     allOf — a pattern real OpenAPI documents use for composition —
	//     would otherwise hit the "object" case, which reads
	//     schema.Properties (empty for a purely compositional schema) and
	//     silently emits an empty `export interface Pet {}`, discarding
	//     the polymorphism entirely. Checking OneOf/AnyOf/AllOf first
	//     avoids that regardless of what schema.Type says.
	//
	// schemaToTSType already joins OneOf/AnyOf with " | " and AllOf with
	// " & " (and applies Nullable), so this delegates rather than
	// reimplementing union logic here.
	if len(schema.OneOf) > 0 || len(schema.AnyOf) > 0 || len(schema.AllOf) > 0 {
		buf.WriteString(fmt.Sprintf("export type %s = %s;\n", name, g.schemaToTSType(schema, spec, name, config)))
		return buf.String()
	}

	switch schema.Type {
	case "object":
		valueSchema, allowed := additionalPropsSchema(schema.AdditionalProperties)
		hasProps := len(schema.Properties) > 0

		switch {
		case allowed && !hasProps:
			// No declared properties, open-ended map: a plain Record. A nil
			// valueSchema means additionalProperties was `true` ("any" value).
			valueType := "any"
			if valueSchema != nil {
				valueType = g.schemaToTSType(valueSchema, spec, name+"."+additionalPropertiesSegment, config)
			}

			buf.WriteString(fmt.Sprintf("export type %s = Record<string, %s>;\n", name, valueType))

		case allowed && hasProps:
			// Declared properties AND an open-ended map: an interface with
			// both `id: string` and `[key: string]: number` is rejected by
			// TypeScript (TS2411) because an index signature must be
			// compatible with every declared property. An intersection type
			// sidesteps that entirely, so this is a `type` alias rather than
			// an `interface` for schemas that take this branch — declaration
			// merging and `implements X` are no longer available to
			// consumers of this generated type. That is the correct
			// trade-off (the alternative doesn't type-check), but it is a
			// real, conscious API shape change for schemas with both
			// declared properties and a typed additionalProperties.
			valueType := "any"
			if valueSchema != nil {
				valueType = g.schemaToTSType(valueSchema, spec, name+"."+additionalPropertiesSegment, config)
			}

			buf.WriteString(fmt.Sprintf("export type %s = %s & Record<string, %s>;\n", name, g.objectPropsLiteral(schema, spec, name, config), valueType))

		default:
			buf.WriteString(fmt.Sprintf("export interface %s {\n", name))

			for _, propName := range sortedKeys(schema.Properties) {
				prop := schema.Properties[propName]
				required := contains(schema.Required, propName)

				optional := ""
				if !required {
					optional = "?"
				}

				buf.WriteString(propertyJSDoc(prop, "  "))

				clientName := tsFieldName(name, propName, config)
				tsType := g.schemaToTSType(prop, spec, name+"."+propName, config)
				buf.WriteString(fmt.Sprintf("  %s%s: %s;\n", tsPropertyKey(clientName), optional, tsType))
			}

			buf.WriteString("}\n")
		}

	case "array":
		if schema.Items != nil {
			itemType := g.schemaToTSType(schema.Items, spec, name+".items", config)
			buf.WriteString(fmt.Sprintf("export type %s = %s[];\n", name, itemType))
		}

	default:
		tsType := g.schemaToTSType(schema, spec, name, config)
		buf.WriteString(fmt.Sprintf("export type %s = %s;\n", name, tsType))
	}

	return buf.String()
}

// escapeJSDocTerminator replaces "*/" with "*\/" so a description containing
// a literal comment terminator cannot close the JSDoc block early. Same class
// of defect Phase 1 fixed in tsPropertyKey for property names.
func escapeJSDocTerminator(s string) string {
	return strings.ReplaceAll(s, "*/", "*\\/")
}

// propertyJSDoc renders a schema's description and deprecation as a JSDoc
// block, or the empty string when there is nothing to say. An empty comment
// is worse than no comment, so both fields absent yields no output.
//
// Blank lines within a multi-line description are preserved as bare " *"
// continuation lines rather than dropped: a blank line in prose is a
// paragraph break, and silently joining paragraphs together would lose that
// structure. This matches how hand-written and tool-generated JSDoc/TSDoc
// represent paragraph breaks.
func propertyJSDoc(schema *client.Schema, indent string) string {
	if schema == nil || (schema.Description == "" && !schema.Deprecated) {
		return ""
	}

	description := escapeJSDocTerminator(schema.Description)

	// Single-line form when there is only a description and it has no newline.
	if description != "" && !schema.Deprecated && !strings.Contains(description, "\n") {
		return fmt.Sprintf("%s/** %s */\n", indent, description)
	}

	var buf strings.Builder

	fmt.Fprintf(&buf, "%s/**\n", indent)

	// Only split-and-render when there is an actual description: an empty
	// Description with Deprecated set must not produce a stray blank " *"
	// line before "@deprecated" — that blank line would carry no meaning,
	// unlike a genuine blank line between two paragraphs of prose.
	if description != "" {
		for _, line := range strings.Split(description, "\n") {
			if line == "" {
				fmt.Fprintf(&buf, "%s *\n", indent)
				continue
			}

			fmt.Fprintf(&buf, "%s * %s\n", indent, line)
		}
	}

	if schema.Deprecated {
		fmt.Fprintf(&buf, "%s * @deprecated\n", indent)
	}

	fmt.Fprintf(&buf, "%s */\n", indent)

	return buf.String()
}

// schemaToTSType converts a schema to a TypeScript type string.
//
// nsID is schema's OWN namespace id -- what tsFieldName is called with as
// the "schema name" for any property schema declares directly (see
// objectPropsLiteral's doc comment for the full scheme). Recursion into a
// child schema derives the child's own id from nsID exactly the way
// codecIDFor (codecs.go) and checkSchemaFieldCollisions (fieldname.go) do,
// so all three consumers of a namespace id agree on what it is:
//   - array items: nsID + ".items"
//   - a oneOf/anyOf member with no $ref of its own: nsID + ".oneOf"/".anyOf" + index
//   - an allOf member: nsID unchanged -- allOf's members are flattened into
//     ONE namespace (the composition's own id), never a per-member one; see
//     checkSchemaFieldCollisions' doc comment for why a per-member id here
//     would print a FieldOverrides key the codec table never builds an entry
//     under.
//
// A $ref member needs no derived id: schemaToTSType returns its type name
// directly without recursing into its properties (those render under the
// ref target's own top-level namespace, elsewhere), so nsID is simply unused
// for that branch.
func (g *Generator) schemaToTSType(schema *client.Schema, spec *client.APISpec, nsID string, config client.GeneratorConfig) string {
	if schema == nil {
		return "any"
	}

	if schema.Ref != "" {
		parts := strings.Split(schema.Ref, "/")
		typeName := parts[len(parts)-1]

		// Add null union if nullable
		if schema.Nullable {
			return typeName + " | null"
		}

		return typeName
	}

	// Handle polymorphic types
	if len(schema.OneOf) > 0 {
		var types []string
		for i, s := range schema.OneOf {
			types = append(types, g.schemaToTSType(s, spec, fmt.Sprintf("%s.oneOf%d", nsID, i), config))
		}

		result := strings.Join(types, " | ")
		if schema.Nullable {
			result += " | null"
		}

		return result
	}

	if len(schema.AnyOf) > 0 {
		var types []string
		for i, s := range schema.AnyOf {
			types = append(types, g.schemaToTSType(s, spec, fmt.Sprintf("%s.anyOf%d", nsID, i), config))
		}

		result := strings.Join(types, " | ")
		if schema.Nullable {
			result += " | null"
		}

		return result
	}

	if len(schema.AllOf) > 0 {
		var types []string
		for _, s := range schema.AllOf {
			// nsID unchanged for every member -- see this function's doc
			// comment.
			types = append(types, g.schemaToTSType(s, spec, nsID, config))
		}

		// A schema can declare its own direct Properties ALONGSIDE AllOf --
		// legal but unusual OpenAPI, and allOf's own doc comment already
		// notes allOfEntry/flattenAllOfLayers (codecs.go) treat that case
		// as a real, contributing layer: schema's own Properties are
		// flattened in as the LAST layer of the composition, and its
		// fields appear in the emitted codec table under this schema's own
		// id. Before this fix, THIS RENDERER silently dropped them: the
		// AllOf branch returned only the joined member types, so
		// `export type Addr = Base;` when Addr ALSO declared its own
		// "own_field" -- decode/encode would still rename and emit
		// "own_field" (the codec table has no trouble with it), producing
		// a value with a property the declared type claims doesn't exist.
		// Appending the schema's own object literal as one more
		// intersection member, last (matching flattenAllOfLayers' own
		// layer order), keeps the rendered type honest about what the
		// codec table -- and therefore decode -- actually produces.
		if len(schema.Properties) > 0 {
			types = append(types, g.objectPropsLiteral(schema, spec, nsID, config))
		}

		result := strings.Join(types, " & ")
		if schema.Nullable {
			result = "(" + result + ")"
			result += " | null"
		}

		return result
	}

	// Enum wins over format: an enum lists the exact permitted literal
	// values, which is strictly more specific type information than a format
	// hint about how to interpret the base type. The two co-occurring is
	// unusual (e.g. an int64-format integer enum, or a binary-format string
	// enum — the latter doesn't meaningfully happen in practice), but when
	// both are present the literal union is more useful to callers than the
	// generic format-driven type, so it is checked first.
	if et := enumTSType(schema); et != "" {
		if schema.Nullable {
			return et + " | null"
		}

		return et
	}

	if ft := formatTSType(schema); ft != "" {
		if schema.Nullable {
			return ft + " | null"
		}

		return ft
	}

	switch schema.Type {
	case "string":
		if schema.Nullable {
			return "string | null"
		}

		return "string"
	case "integer", "number":
		if schema.Nullable {
			return "number | null"
		}

		return "number"
	case "boolean":
		if schema.Nullable {
			return "boolean | null"
		}

		return "boolean"
	case "array":
		if schema.Items != nil {
			itemType := g.schemaToTSType(schema.Items, spec, nsID+".items", config)
			if schema.Nullable {
				return itemType + "[] | null"
			}

			return itemType + "[]"
		}

		if schema.Nullable {
			return "any[] | null"
		}

		return "any[]"
	case "object":
		valueSchema, allowed := additionalPropsSchema(schema.AdditionalProperties)

		var result string

		switch {
		case allowed && len(schema.Properties) > 0:
			valueType := "any"
			if valueSchema != nil {
				valueType = g.schemaToTSType(valueSchema, spec, nsID+"."+additionalPropertiesSegment, config)
			}

			result = g.objectPropsLiteral(schema, spec, nsID, config) + " & Record<string, " + valueType + ">"

		case allowed:
			valueType := "any"
			if valueSchema != nil {
				valueType = g.schemaToTSType(valueSchema, spec, nsID+"."+additionalPropertiesSegment, config)
			}

			result = "Record<string, " + valueType + ">"

		case len(schema.Properties) > 0:
			// additionalProperties absent/false but the schema still declares
			// real properties: an inline object (most commonly a oneOf/anyOf
			// member with no $ref of its own) renders as an object type
			// literal via the same helper the named-schema "object" case
			// uses, rather than collapsing to Record<string, any> and losing
			// every declared field.
			result = g.objectPropsLiteral(schema, spec, nsID, config)

		default:
			result = "Record<string, any>"
		}

		if schema.Nullable {
			return "(" + result + ") | null"
		}

		return result
	case "null":
		return "null"
	}

	if schema.Nullable {
		return "any | null"
	}

	return "any"
}

// generateClient generates client.ts.
func (g *Generator) generateClient(spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	buf.WriteString("import { HTTPClient, RequestConfig } from './fetch';\n")

	if config.IncludeAuth {
		buf.WriteString("import { ClientConfig, AuthConfig } from './types';\n")
	} else {
		buf.WriteString("import { ClientConfig } from './types';\n")
	}

	buf.WriteString("import { createError } from './errors';\n\n")

	buf.WriteString(fmt.Sprintf("export class %s {\n", config.APIName))
	buf.WriteString("  protected httpClient: HTTPClient;\n")

	if config.IncludeAuth {
		buf.WriteString("  private auth?: AuthConfig;\n\n")
	} else {
		buf.WriteString("\n")
	}

	buf.WriteString("  constructor(config: ClientConfig) {\n")

	if config.IncludeAuth {
		buf.WriteString("    this.auth = config.auth;\n")
	}

	buf.WriteString("    this.httpClient = new HTTPClient(\n")
	buf.WriteString("      config.baseURL,\n")
	buf.WriteString("      config.timeout || 30000\n")
	buf.WriteString("    );\n\n")

	if config.IncludeAuth {
		buf.WriteString("    // Setup auth headers\n")
		buf.WriteString("    if (this.auth?.bearerToken) {\n")
		buf.WriteString("      this.httpClient.setDefaultHeader('Authorization', `Bearer ${this.auth.bearerToken}`);\n")
		buf.WriteString("    }\n")
		buf.WriteString("    if (this.auth?.apiKey) {\n")
		buf.WriteString("      this.httpClient.setDefaultHeader('X-API-Key', this.auth.apiKey);\n")
		buf.WriteString("    }\n")
		buf.WriteString("    if (this.auth?.customHeaders) {\n")
		buf.WriteString("      for (const [key, value] of Object.entries(this.auth.customHeaders)) {\n")
		buf.WriteString("        this.httpClient.setDefaultHeader(key, value);\n")
		buf.WriteString("      }\n")
		buf.WriteString("    }\n")
	}

	buf.WriteString("  }\n\n")

	buf.WriteString("  protected async request<T>(config: RequestConfig): Promise<T> {\n")
	buf.WriteString("    try {\n")
	buf.WriteString("      return await this.httpClient.request<T>(config);\n")
	buf.WriteString("    } catch (error: any) {\n")
	buf.WriteString("      // Transform errors into typed error classes\n")
	buf.WriteString("      if (error.statusCode) {\n")
	buf.WriteString("        throw createError(error.statusCode, error.message, error.code, error.details);\n")
	buf.WriteString("      }\n")
	buf.WriteString("      throw error;\n")
	buf.WriteString("    }\n")
	buf.WriteString("  }\n")
	buf.WriteString("}\n")

	return buf.String()
}

// generateIndex generates index.ts.
func (g *Generator) generateIndex(spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	// Determine if we're in AsyncAPI-only mode
	isAsyncAPIOnly := config.HasAnyStreamingFeature() && len(spec.Endpoints) == 0

	// Export base modules (skip REST-related in AsyncAPI-only mode)
	if !isAsyncAPIOnly {
		buf.WriteString("export * from './fetch';\n")
		buf.WriteString("export * from './errors';\n")
	}

	buf.WriteString("export * from './types';\n")
	buf.WriteString("export * from './codecs';\n")

	if !isAsyncAPIOnly {
		buf.WriteString("export * from './client';\n\n")

		// Export generated clients
		if len(spec.Endpoints) > 0 {
			buf.WriteString("export * from './rest';\n")
		}

		// Export pagination helpers
		if config.Pagination && len(spec.Endpoints) > 0 {
			buf.WriteString("export * from './pagination';\n")
		}
	} else {
		buf.WriteString("\n")
	}

	// Export events utility
	if config.HasAnyStreamingFeature() || (config.IncludeStreaming && (len(spec.WebSockets) > 0 || len(spec.SSEs) > 0)) {
		buf.WriteString("export * from './events';\n")
	}

	if len(spec.WebSockets) > 0 && config.IncludeStreaming && !isAsyncAPIOnly {
		buf.WriteString("export * from './websocket';\n")
	}

	if len(spec.SSEs) > 0 && config.IncludeStreaming && !isAsyncAPIOnly {
		buf.WriteString("export * from './sse';\n")
	}

	if len(spec.WebTransports) > 0 && config.IncludeStreaming && !isAsyncAPIOnly {
		buf.WriteString("export * from './webtransport';\n")
	}

	// Export modular streaming clients
	if config.Streaming.GenerateModularClients {
		buf.WriteString("\n// Streaming clients\n")

		if config.ShouldGenerateRoomClient() {
			buf.WriteString("export * from './rooms';\n")
		}

		if config.ShouldGeneratePresenceClient() {
			buf.WriteString("export * from './presence';\n")
		}

		if config.ShouldGenerateTypingClient() {
			buf.WriteString("export * from './typing';\n")
		}

		if config.ShouldGenerateChannelClient() {
			buf.WriteString("export * from './channels';\n")
		}
	}

	// Export unified streaming client
	if config.ShouldGenerateUnifiedStreamingClient() {
		buf.WriteString("export * from './streaming';\n")
	}

	return buf.String()
}

// getDependencies returns the list of dependencies.
func (g *Generator) getDependencies(config client.GeneratorConfig) []generators.Dependency {
	deps := []generators.Dependency{
		{Name: "typescript", Version: "^5.3.0", Type: "dev"},
		{Name: "tsup", Version: "^8.0.0", Type: "dev"},
	}

	// Add Node.js polyfills for streaming when needed
	if config.IncludeStreaming {
		deps = append(deps,
			generators.Dependency{Name: "ws", Version: "^8.16.0", Type: "direct"},
			generators.Dependency{Name: "eventsource", Version: "^2.0.2", Type: "direct"},
		)
	}

	return deps
}

// generateInstructions generates setup instructions.
func (g *Generator) generateInstructions(spec *client.APISpec, config client.GeneratorConfig) string {
	outputMgr := client.NewOutputManager()
	authGen := client.NewAuthCodeGenerator()

	authDocs := ""

	if config.IncludeAuth {
		schemes := authGen.DetectAuthSchemes(spec)
		authDocs = authGen.GenerateAuthDocumentation(schemes)
	}

	return outputMgr.GenerateREADME(config, spec, authDocs)
}

// Helper function.
func contains(slice []string, item string) bool {
	return slices.Contains(slice, item)
}
