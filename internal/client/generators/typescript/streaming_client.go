package typescript

import (
	"strings"

	"github.com/xraph/forge/internal/client"
)

// StreamingClientGenerator generates the unified TypeScript streaming client.
type StreamingClientGenerator struct{}

// NewStreamingClientGenerator creates a new streaming client generator.
func NewStreamingClientGenerator() *StreamingClientGenerator {
	return &StreamingClientGenerator{}
}

// Generate generates the unified StreamingClient TypeScript code.
func (s *StreamingClientGenerator) Generate(spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	buf.WriteString(s.generateImports(spec, config))
	buf.WriteString("\n")
	buf.WriteString(s.generateTypes(spec, config))
	buf.WriteString("\n")
	buf.WriteString(s.generateStreamingClient(spec, config))

	return buf.String()
}

// generateImports generates import statements for the streaming client.
func (s *StreamingClientGenerator) generateImports(spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	buf.WriteString("// Unified streaming client composing all streaming features\n\n")
	buf.WriteString("import { ConnectionState, AuthConfig } from './types';\n")

	if config.Features.StateManagement {
		buf.WriteString("import { EventEmitter } from './events';\n")
	}

	// Import modular clients based on enabled features
	if config.ShouldGenerateRoomClient() && (spec.HasRooms() || config.Streaming.EnableRooms) {
		buf.WriteString("import { RoomClient, RoomClientConfig, MessageHandler, MemberHandler, JoinOptions, RoomState } from './rooms';\n")
	}

	if config.ShouldGeneratePresenceClient() && (spec.HasPresence() || config.Streaming.EnablePresence) {
		buf.WriteString("import { PresenceClient, PresenceClientConfig, PresenceStatus, PresenceHandler } from './presence';\n")
	}

	if config.ShouldGenerateTypingClient() && (spec.HasTyping() || config.Streaming.EnableTyping) {
		buf.WriteString("import { TypingClient, TypingClientConfig, TypingHandler, TypingUser } from './typing';\n")
	}

	if config.ShouldGenerateChannelClient() && (spec.HasChannels() || config.Streaming.EnableChannels) {
		buf.WriteString("import { ChannelClient, ChannelClientConfig, ChannelMessageHandler, SubscribeOptions, ChannelMessage } from './channels';\n")
	}

	buf.WriteString("\n")

	return buf.String()
}

// generateTypes generates streaming-specific types.
func (s *StreamingClientGenerator) generateTypes(spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	// StreamingClientConfig
	buf.WriteString("/**\n * Configuration for the unified streaming client\n */\n")
	buf.WriteString("export interface StreamingClientConfig {\n")
	buf.WriteString("  /** Base URL for WebSocket connections */\n")
	buf.WriteString("  baseURL: string;\n")
	buf.WriteString("  /** Authentication configuration */\n")
	buf.WriteString("  auth?: AuthConfig;\n")

	if config.Features.Reconnection {
		buf.WriteString("  /** Maximum reconnection attempts */\n")
		buf.WriteString("  maxReconnectAttempts?: number;\n")
		buf.WriteString("  /** Initial reconnection delay in ms */\n")
		buf.WriteString("  reconnectDelay?: number;\n")
		buf.WriteString("  /** Maximum reconnection delay in ms */\n")
		buf.WriteString("  maxReconnectDelay?: number;\n")
	}

	// Feature-specific configs
	if config.Streaming.EnableRooms {
		buf.WriteString("  /** Room client configuration overrides */\n")
		buf.WriteString("  rooms?: Partial<RoomClientConfig>;\n")
	}

	if config.Streaming.EnablePresence {
		buf.WriteString("  /** Presence client configuration overrides */\n")
		buf.WriteString("  presence?: Partial<PresenceClientConfig>;\n")
	}

	if config.Streaming.EnableTyping {
		buf.WriteString("  /** Typing client configuration overrides */\n")
		buf.WriteString("  typing?: Partial<TypingClientConfig>;\n")
	}

	if config.Streaming.EnableChannels {
		buf.WriteString("  /** Channel client configuration overrides */\n")
		buf.WriteString("  channels?: Partial<ChannelClientConfig>;\n")
	}

	buf.WriteString("}\n\n")

	// ConnectOptions
	buf.WriteString("/**\n * Options for connecting to streaming services\n */\n")
	buf.WriteString("export interface ConnectOptions {\n")
	buf.WriteString("  /** Which clients to connect (default: all enabled) */\n")
	buf.WriteString("  clients?: {\n")

	if config.Streaming.EnableRooms {
		buf.WriteString("    rooms?: boolean;\n")
	}
	if config.Streaming.EnablePresence {
		buf.WriteString("    presence?: boolean;\n")
	}
	if config.Streaming.EnableTyping {
		buf.WriteString("    typing?: boolean;\n")
	}
	if config.Streaming.EnableChannels {
		buf.WriteString("    channels?: boolean;\n")
	}

	buf.WriteString("  };\n")
	buf.WriteString("}\n\n")

	return buf.String()
}

// generateStreamingClient generates the main StreamingClient class.
func (s *StreamingClientGenerator) generateStreamingClient(spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	hasRooms := config.Streaming.EnableRooms
	hasPresence := config.Streaming.EnablePresence
	hasTyping := config.Streaming.EnableTyping
	hasChannels := config.Streaming.EnableChannels

	// Class documentation
	buf.WriteString("/**\n")
	buf.WriteString(" * StreamingClient provides a unified interface for all streaming features.\n")
	buf.WriteString(" * \n")
	buf.WriteString(" * This client composes the following modular clients:\n")

	if hasRooms {
		buf.WriteString(" * - **RoomClient**: Room-based messaging and member management\n")
	}
	if hasPresence {
		buf.WriteString(" * - **PresenceClient**: User presence tracking and status updates\n")
	}
	if hasTyping {
		buf.WriteString(" * - **TypingClient**: Real-time typing indicators\n")
	}
	if hasChannels {
		buf.WriteString(" * - **ChannelClient**: Pub/sub channel messaging\n")
	}

	buf.WriteString(" * \n")
	buf.WriteString(" * @example\n")
	buf.WriteString(" * ```typescript\n")
	buf.WriteString(" * const streaming = new StreamingClient({\n")
	buf.WriteString(" *   baseURL: 'ws://localhost:8080',\n")
	buf.WriteString(" *   auth: { bearerToken: 'my-token' },\n")
	buf.WriteString(" * });\n")
	buf.WriteString(" * \n")
	buf.WriteString(" * await streaming.connect();\n")
	buf.WriteString(" * \n")

	if hasRooms {
		buf.WriteString(" * // Use rooms\n")
		buf.WriteString(" * await streaming.rooms.join('my-room');\n")
		buf.WriteString(" * streaming.rooms.onMessage('my-room', (msg) => console.log(msg));\n")
		buf.WriteString(" * \n")
	}

	if hasPresence {
		buf.WriteString(" * // Use presence\n")
		buf.WriteString(" * await streaming.presence.setStatus(PresenceStatus.ONLINE);\n")
		buf.WriteString(" * \n")
	}

	if hasTyping {
		buf.WriteString(" * // Use typing indicators\n")
		buf.WriteString(" * streaming.typing.startTyping('my-room');\n")
		buf.WriteString(" * \n")
	}

	if hasChannels {
		buf.WriteString(" * // Use channels\n")
		buf.WriteString(" * streaming.channels.subscribe('notifications');\n")
	}

	buf.WriteString(" * ```\n")
	buf.WriteString(" */\n")

	// Class declaration
	buf.WriteString("export class StreamingClient extends EventEmitter {\n")

	// Public readonly client instances
	if hasRooms {
		buf.WriteString("  /**\n")
		buf.WriteString("   * Room client for managing chat rooms.\n")
		buf.WriteString("   * Use this to join/leave rooms, send messages, and track members.\n")
		buf.WriteString("   */\n")
		buf.WriteString("  public readonly rooms: RoomClient;\n\n")
	}

	if hasPresence {
		buf.WriteString("  /**\n")
		buf.WriteString("   * Presence client for tracking user status.\n")
		buf.WriteString("   * Use this to set your status and subscribe to others' presence.\n")
		buf.WriteString("   */\n")
		buf.WriteString("  public readonly presence: PresenceClient;\n\n")
	}

	if hasTyping {
		buf.WriteString("  /**\n")
		buf.WriteString("   * Typing client for typing indicators.\n")
		buf.WriteString("   * Use this to show when you're typing and see who else is typing.\n")
		buf.WriteString("   */\n")
		buf.WriteString("  public readonly typing: TypingClient;\n\n")
	}

	if hasChannels {
		buf.WriteString("  /**\n")
		buf.WriteString("   * Channel client for pub/sub messaging.\n")
		buf.WriteString("   * Use this to subscribe to channels and publish messages.\n")
		buf.WriteString("   */\n")
		buf.WriteString("  public readonly channels: ChannelClient;\n\n")
	}

	// Private fields
	buf.WriteString("  private config: StreamingClientConfig;\n")
	buf.WriteString("  private connectionState: ConnectionState = ConnectionState.DISCONNECTED;\n")
	buf.WriteString("\n")

	// Constructor
	buf.WriteString("  constructor(config: StreamingClientConfig) {\n")
	buf.WriteString("    super();\n")
	buf.WriteString("    this.config = config;\n\n")

	// Initialize modular clients
	if hasRooms {
		buf.WriteString("    // Initialize room client\n")
		buf.WriteString("    this.rooms = new RoomClient({\n")
		buf.WriteString("      baseURL: config.baseURL,\n")
		buf.WriteString("      auth: config.auth,\n")

		if config.Features.Reconnection {
			buf.WriteString("      maxReconnectAttempts: config.maxReconnectAttempts,\n")
			buf.WriteString("      reconnectDelay: config.reconnectDelay,\n")
			buf.WriteString("      maxReconnectDelay: config.maxReconnectDelay,\n")
		}

		buf.WriteString("      ...config.rooms,\n")
		buf.WriteString("    });\n\n")
	}

	if hasPresence {
		buf.WriteString("    // Initialize presence client\n")
		buf.WriteString("    this.presence = new PresenceClient({\n")
		buf.WriteString("      baseURL: config.baseURL,\n")
		buf.WriteString("      auth: config.auth,\n")

		if config.Features.Reconnection {
			buf.WriteString("      maxReconnectAttempts: config.maxReconnectAttempts,\n")
			buf.WriteString("      reconnectDelay: config.reconnectDelay,\n")
			buf.WriteString("      maxReconnectDelay: config.maxReconnectDelay,\n")
		}

		buf.WriteString("      ...config.presence,\n")
		buf.WriteString("    });\n\n")
	}

	if hasTyping {
		buf.WriteString("    // Initialize typing client\n")
		buf.WriteString("    this.typing = new TypingClient({\n")
		buf.WriteString("      baseURL: config.baseURL,\n")
		buf.WriteString("      auth: config.auth,\n")

		if config.Features.Reconnection {
			buf.WriteString("      maxReconnectAttempts: config.maxReconnectAttempts,\n")
			buf.WriteString("      reconnectDelay: config.reconnectDelay,\n")
			buf.WriteString("      maxReconnectDelay: config.maxReconnectDelay,\n")
		}

		buf.WriteString("      ...config.typing,\n")
		buf.WriteString("    });\n\n")
	}

	if hasChannels {
		buf.WriteString("    // Initialize channel client\n")
		buf.WriteString("    this.channels = new ChannelClient({\n")
		buf.WriteString("      baseURL: config.baseURL,\n")
		buf.WriteString("      auth: config.auth,\n")

		if config.Features.Reconnection {
			buf.WriteString("      maxReconnectAttempts: config.maxReconnectAttempts,\n")
			buf.WriteString("      reconnectDelay: config.reconnectDelay,\n")
			buf.WriteString("      maxReconnectDelay: config.maxReconnectDelay,\n")
		}

		buf.WriteString("      ...config.channels,\n")
		buf.WriteString("    });\n\n")
	}

	buf.WriteString("    // Forward state change events\n")
	buf.WriteString("    this.setupEventForwarding();\n")
	buf.WriteString("  }\n\n")

	// connect method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Connect all streaming clients.\n")
	buf.WriteString("   * @param options - Optional connection options\n")
	buf.WriteString("   * @returns Promise that resolves when all specified clients are connected\n")
	buf.WriteString("   */\n")
	buf.WriteString("  async connect(options?: ConnectOptions): Promise<void> {\n")
	buf.WriteString("    this.connectionState = ConnectionState.CONNECTING;\n")
	buf.WriteString("    this.emit('stateChange', this.connectionState);\n\n")

	buf.WriteString("    const connectClients = options?.clients || {\n")

	if hasRooms {
		buf.WriteString("      rooms: true,\n")
	}
	if hasPresence {
		buf.WriteString("      presence: true,\n")
	}
	if hasTyping {
		buf.WriteString("      typing: true,\n")
	}
	if hasChannels {
		buf.WriteString("      channels: true,\n")
	}

	buf.WriteString("    };\n\n")

	buf.WriteString("    const connectPromises: Promise<void>[] = [];\n\n")

	if hasRooms {
		buf.WriteString("    if (connectClients.rooms) {\n")
		buf.WriteString("      connectPromises.push(this.rooms.connect());\n")
		buf.WriteString("    }\n\n")
	}

	if hasPresence {
		buf.WriteString("    if (connectClients.presence) {\n")
		buf.WriteString("      connectPromises.push(this.presence.connect());\n")
		buf.WriteString("    }\n\n")
	}

	if hasTyping {
		buf.WriteString("    if (connectClients.typing) {\n")
		buf.WriteString("      connectPromises.push(this.typing.connect());\n")
		buf.WriteString("    }\n\n")
	}

	if hasChannels {
		buf.WriteString("    if (connectClients.channels) {\n")
		buf.WriteString("      connectPromises.push(this.channels.connect());\n")
		buf.WriteString("    }\n\n")
	}

	buf.WriteString("    try {\n")
	buf.WriteString("      await Promise.all(connectPromises);\n")
	buf.WriteString("      this.connectionState = ConnectionState.CONNECTED;\n")
	buf.WriteString("      this.emit('stateChange', this.connectionState);\n")
	buf.WriteString("    } catch (error) {\n")
	buf.WriteString("      this.connectionState = ConnectionState.ERROR;\n")
	buf.WriteString("      this.emit('stateChange', this.connectionState);\n")
	buf.WriteString("      throw error;\n")
	buf.WriteString("    }\n")
	buf.WriteString("  }\n\n")

	// disconnect method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Disconnect all streaming clients.\n")
	buf.WriteString("   */\n")
	buf.WriteString("  disconnect(): void {\n")

	if hasRooms {
		buf.WriteString("    this.rooms.disconnect();\n")
	}
	if hasPresence {
		buf.WriteString("    this.presence.disconnect();\n")
	}
	if hasTyping {
		buf.WriteString("    this.typing.disconnect();\n")
	}
	if hasChannels {
		buf.WriteString("    this.channels.disconnect();\n")
	}

	buf.WriteString("\n")
	buf.WriteString("    this.connectionState = ConnectionState.CLOSED;\n")
	buf.WriteString("    this.emit('stateChange', this.connectionState);\n")
	buf.WriteString("  }\n\n")

	// getState method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Get the overall connection state.\n")
	buf.WriteString("   * @returns Current connection state\n")
	buf.WriteString("   */\n")
	buf.WriteString("  getState(): ConnectionState {\n")
	buf.WriteString("    return this.connectionState;\n")
	buf.WriteString("  }\n\n")

	// onConnectionStateChange method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Register a handler for connection state changes.\n")
	buf.WriteString("   * @param handler - Function to call when state changes\n")
	buf.WriteString("   */\n")
	buf.WriteString("  onConnectionStateChange(handler: (state: ConnectionState) => void): void {\n")
	buf.WriteString("    this.on('stateChange', handler);\n")
	buf.WriteString("  }\n\n")

	// onError method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Register a handler for errors from any client.\n")
	buf.WriteString("   * @param handler - Function to call when an error occurs\n")
	buf.WriteString("   */\n")
	buf.WriteString("  onError(handler: (error: Error) => void): void {\n")
	buf.WriteString("    this.on('error', handler);\n")
	buf.WriteString("  }\n\n")

	// getClientStates method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Get the connection state of each client.\n")
	buf.WriteString("   * @returns Object with state for each client\n")
	buf.WriteString("   */\n")
	buf.WriteString("  getClientStates(): {\n")

	if hasRooms {
		buf.WriteString("    rooms: ConnectionState;\n")
	}
	if hasPresence {
		buf.WriteString("    presence: ConnectionState;\n")
	}
	if hasTyping {
		buf.WriteString("    typing: ConnectionState;\n")
	}
	if hasChannels {
		buf.WriteString("    channels: ConnectionState;\n")
	}

	buf.WriteString("  } {\n")
	buf.WriteString("    return {\n")

	if hasRooms {
		buf.WriteString("      rooms: this.rooms.getState(),\n")
	}
	if hasPresence {
		buf.WriteString("      presence: this.presence.getState(),\n")
	}
	if hasTyping {
		buf.WriteString("      typing: this.typing.getState(),\n")
	}
	if hasChannels {
		buf.WriteString("      channels: this.channels.getState(),\n")
	}

	buf.WriteString("    };\n")
	buf.WriteString("  }\n\n")

	// Private methods
	buf.WriteString("  // Private methods\n\n")

	// setupEventForwarding
	buf.WriteString("  private setupEventForwarding(): void {\n")

	if hasRooms {
		buf.WriteString("    // Forward room events\n")
		buf.WriteString("    this.rooms.on('stateChange', (state) => {\n")
		buf.WriteString("      this.emit('rooms:stateChange', state);\n")
		buf.WriteString("      this.updateOverallState();\n")
		buf.WriteString("    });\n")
		buf.WriteString("    this.rooms.on('error', (error) => this.emit('error', error));\n\n")
	}

	if hasPresence {
		buf.WriteString("    // Forward presence events\n")
		buf.WriteString("    this.presence.on('stateChange', (state) => {\n")
		buf.WriteString("      this.emit('presence:stateChange', state);\n")
		buf.WriteString("      this.updateOverallState();\n")
		buf.WriteString("    });\n")
		buf.WriteString("    this.presence.on('error', (error) => this.emit('error', error));\n\n")
	}

	if hasTyping {
		buf.WriteString("    // Forward typing events\n")
		buf.WriteString("    this.typing.on('stateChange', (state) => {\n")
		buf.WriteString("      this.emit('typing:stateChange', state);\n")
		buf.WriteString("      this.updateOverallState();\n")
		buf.WriteString("    });\n")
		buf.WriteString("    this.typing.on('error', (error) => this.emit('error', error));\n\n")
	}

	if hasChannels {
		buf.WriteString("    // Forward channel events\n")
		buf.WriteString("    this.channels.on('stateChange', (state) => {\n")
		buf.WriteString("      this.emit('channels:stateChange', state);\n")
		buf.WriteString("      this.updateOverallState();\n")
		buf.WriteString("    });\n")
		buf.WriteString("    this.channels.on('error', (error) => this.emit('error', error));\n")
	}

	buf.WriteString("  }\n\n")

	// updateOverallState
	buf.WriteString("  private updateOverallState(): void {\n")
	buf.WriteString("    const states = this.getClientStates();\n")
	buf.WriteString("    const stateValues = Object.values(states);\n\n")

	buf.WriteString("    // Determine overall state based on individual client states\n")
	buf.WriteString("    if (stateValues.every(s => s === ConnectionState.CONNECTED)) {\n")
	buf.WriteString("      this.connectionState = ConnectionState.CONNECTED;\n")
	buf.WriteString("    } else if (stateValues.some(s => s === ConnectionState.CONNECTING)) {\n")
	buf.WriteString("      this.connectionState = ConnectionState.CONNECTING;\n")
	buf.WriteString("    } else if (stateValues.some(s => s === ConnectionState.RECONNECTING)) {\n")
	buf.WriteString("      this.connectionState = ConnectionState.RECONNECTING;\n")
	buf.WriteString("    } else if (stateValues.some(s => s === ConnectionState.ERROR)) {\n")
	buf.WriteString("      this.connectionState = ConnectionState.ERROR;\n")
	buf.WriteString("    } else if (stateValues.every(s => s === ConnectionState.CLOSED)) {\n")
	buf.WriteString("      this.connectionState = ConnectionState.CLOSED;\n")
	buf.WriteString("    } else {\n")
	buf.WriteString("      this.connectionState = ConnectionState.DISCONNECTED;\n")
	buf.WriteString("    }\n\n")
	buf.WriteString("    this.emit('stateChange', this.connectionState);\n")
	buf.WriteString("  }\n")

	buf.WriteString("}\n\n")

	// Export type aliases for convenience
	buf.WriteString("// Re-export types for convenience\n")

	if hasRooms {
		buf.WriteString("export type { RoomClientConfig, MessageHandler, MemberHandler, JoinOptions, RoomState };\n")
	}
	if hasPresence {
		buf.WriteString("export { PresenceStatus };\n")
		buf.WriteString("export type { PresenceClientConfig, PresenceHandler };\n")
	}
	if hasTyping {
		buf.WriteString("export type { TypingClientConfig, TypingHandler, TypingUser };\n")
	}
	if hasChannels {
		buf.WriteString("export type { ChannelClientConfig, ChannelMessageHandler, SubscribeOptions, ChannelMessage };\n")
	}

	return buf.String()
}

