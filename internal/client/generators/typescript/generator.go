package typescript

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/internal/client"
	"github.com/xraph/forge/internal/client/generators"
)

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
			restCode := restGen.Generate(spec, config)
			genClient.Files["src/rest.ts"] = restCode
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

		for name, version := range deps {
			if !first {
				depsJSONSb.WriteString(",\n")
			}

			depsJSONSb.WriteString(fmt.Sprintf("    \"%s\": \"%s\"", name, version))

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
	for name, schema := range spec.Schemas {
		typeCode := g.schemaToTypeScript(name, schema, spec)
		buf.WriteString(typeCode)
		buf.WriteString("\n")
	}

	// Auth config interface
	if config.IncludeAuth && client.NeedsAuthConfig(spec) {
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

// schemaToTypeScript converts a schema to TypeScript.
func (g *Generator) schemaToTypeScript(name string, schema *client.Schema, spec *client.APISpec) string {
	if schema == nil {
		return ""
	}

	var buf strings.Builder

	switch schema.Type {
	case "object":
		buf.WriteString(fmt.Sprintf("export interface %s {\n", name))

		for propName, prop := range schema.Properties {
			required := contains(schema.Required, propName)

			optional := ""
			if !required {
				optional = "?"
			}

			tsType := g.schemaToTSType(prop, spec)
			buf.WriteString(fmt.Sprintf("  %s%s: %s;\n", propName, optional, tsType))
		}

		buf.WriteString("}\n")

	case "array":
		if schema.Items != nil {
			itemType := g.schemaToTSType(schema.Items, spec)
			buf.WriteString(fmt.Sprintf("export type %s = %s[];\n", name, itemType))
		}

	default:
		tsType := g.schemaToTSType(schema, spec)
		buf.WriteString(fmt.Sprintf("export type %s = %s;\n", name, tsType))
	}

	return buf.String()
}

// schemaToTSType converts a schema to a TypeScript type string.
func (g *Generator) schemaToTSType(schema *client.Schema, spec *client.APISpec) string {
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
		for _, s := range schema.OneOf {
			types = append(types, g.schemaToTSType(s, spec))
		}

		result := strings.Join(types, " | ")
		if schema.Nullable {
			result += " | null"
		}

		return result
	}

	if len(schema.AnyOf) > 0 {
		var types []string
		for _, s := range schema.AnyOf {
			types = append(types, g.schemaToTSType(s, spec))
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
			types = append(types, g.schemaToTSType(s, spec))
		}

		result := strings.Join(types, " & ")
		if schema.Nullable {
			result = "(" + result + ")"
			result += " | null"
		}

		return result
	}

	switch schema.Type {
	case "string":
		if len(schema.Enum) > 0 {
			var values []string
			for _, v := range schema.Enum {
				values = append(values, fmt.Sprintf("'%v'", v))
			}

			result := strings.Join(values, " | ")
			if schema.Nullable {
				result += " | null"
			}

			return result
		}

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
			itemType := g.schemaToTSType(schema.Items, spec)
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
		if schema.Nullable {
			return "Record<string, any> | null"
		}

		return "Record<string, any>"
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
	buf.WriteString("import { ClientConfig, AuthConfig } from './types';\n")
	buf.WriteString("import { createError } from './errors';\n\n")

	buf.WriteString(fmt.Sprintf("export class %s {\n", config.APIName))
	buf.WriteString("  protected httpClient: HTTPClient;\n")
	buf.WriteString("  private auth?: AuthConfig;\n\n")

	buf.WriteString("  constructor(config: ClientConfig) {\n")
	buf.WriteString("    this.auth = config.auth;\n")
	buf.WriteString("    this.httpClient = new HTTPClient(\n")
	buf.WriteString("      config.baseURL,\n")
	buf.WriteString("      config.timeout || 30000\n")
	buf.WriteString("    );\n\n")

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
