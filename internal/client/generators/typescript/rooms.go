package typescript

import (
	"fmt"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// RoomsGenerator generates TypeScript room client code.
type RoomsGenerator struct{}

// NewRoomsGenerator creates a new rooms generator.
func NewRoomsGenerator() *RoomsGenerator {
	return &RoomsGenerator{}
}

// Generate generates the RoomClient TypeScript code.
func (r *RoomsGenerator) Generate(spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	buf.WriteString(r.generatePolyfillSetup())
	buf.WriteString("\n")
	buf.WriteString(r.generateImports(spec, config))
	buf.WriteString("\n")
	buf.WriteString(r.generateTypes(spec, config))
	buf.WriteString("\n")
	buf.WriteString(r.generateRoomClient(spec, config))

	return buf.String()
}

// generatePolyfillSetup generates the browser/Node.js compatibility layer.
func (r *RoomsGenerator) generatePolyfillSetup() string {
	var buf strings.Builder

	buf.WriteString("// Browser/Node.js compatibility layer\n")
	buf.WriteString("const isBrowser = typeof window !== 'undefined' && typeof window.WebSocket !== 'undefined';\n\n")

	buf.WriteString("// Lazy-loaded WebSocket implementation\n")
	buf.WriteString("let _WebSocketImpl: typeof WebSocket | null = null;\n\n")

	buf.WriteString("function getWebSocket(): typeof WebSocket {\n")
	buf.WriteString("  if (_WebSocketImpl) return _WebSocketImpl;\n")
	buf.WriteString("  \n")
	buf.WriteString("  if (isBrowser) {\n")
	buf.WriteString("    _WebSocketImpl = window.WebSocket;\n")
	buf.WriteString("  } else {\n")
	buf.WriteString("    try {\n")
	buf.WriteString("      // eslint-disable-next-line @typescript-eslint/no-var-requires\n")
	buf.WriteString("      _WebSocketImpl = require('ws');\n")
	buf.WriteString("    } catch {\n")
	buf.WriteString("      throw new Error('WebSocket implementation not found. Install \"ws\" package for Node.js.');\n")
	buf.WriteString("    }\n")
	buf.WriteString("  }\n")
	buf.WriteString("  return _WebSocketImpl!;\n")
	buf.WriteString("}\n\n")

	// Simple EventEmitter for browser
	buf.WriteString("// Simple EventEmitter for cross-platform support\n")
	buf.WriteString("class EventEmitter {\n")
	buf.WriteString("  private listeners: Map<string, Set<Function>> = new Map();\n\n")
	buf.WriteString("  on(event: string, handler: Function): void {\n")
	buf.WriteString("    if (!this.listeners.has(event)) {\n")
	buf.WriteString("      this.listeners.set(event, new Set());\n")
	buf.WriteString("    }\n")
	buf.WriteString("    this.listeners.get(event)!.add(handler);\n")
	buf.WriteString("  }\n\n")
	buf.WriteString("  off(event: string, handler: Function): void {\n")
	buf.WriteString("    this.listeners.get(event)?.delete(handler);\n")
	buf.WriteString("  }\n\n")
	buf.WriteString("  emit(event: string, ...args: any[]): void {\n")
	buf.WriteString("    const handlers = this.listeners.get(event);\n")
	buf.WriteString("    if (handlers) {\n")
	buf.WriteString("      handlers.forEach(handler => {\n")
	buf.WriteString("        try {\n")
	buf.WriteString("          handler(...args);\n")
	buf.WriteString("        } catch (error) {\n")
	buf.WriteString("          console.error('Event handler error:', error);\n")
	buf.WriteString("        }\n")
	buf.WriteString("      });\n")
	buf.WriteString("    }\n")
	buf.WriteString("  }\n\n")
	buf.WriteString("  removeAllListeners(event?: string): void {\n")
	buf.WriteString("    if (event) {\n")
	buf.WriteString("      this.listeners.delete(event);\n")
	buf.WriteString("    } else {\n")
	buf.WriteString("      this.listeners.clear();\n")
	buf.WriteString("    }\n")
	buf.WriteString("  }\n")
	buf.WriteString("}\n")

	return buf.String()
}

// generateImports generates import statements for the room client.
func (r *RoomsGenerator) generateImports(_ *client.APISpec, _ client.GeneratorConfig) string {
	var buf strings.Builder

	buf.WriteString("// Room client for managing chat rooms\n\n")
	buf.WriteString("import { ConnectionState, AuthConfig, Message, Member, Room, RoomOptions, HistoryQuery } from './types';\n\n")

	return buf.String()
}

// generateTypes generates room-specific types.
func (r *RoomsGenerator) generateTypes(_ *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	// QueuedMessage type
	buf.WriteString("/** Message queued for sending when offline */\n")
	buf.WriteString("interface QueuedRoomMessage {\n")
	buf.WriteString("  roomId: string;\n")
	buf.WriteString("  data: any;\n")
	buf.WriteString("  timestamp: number;\n")
	buf.WriteString("  resolve: () => void;\n")
	buf.WriteString("  reject: (error: Error) => void;\n")
	buf.WriteString("}\n\n")

	// JoinOptions
	buf.WriteString("/**\n * Options for joining a room\n */\n")
	buf.WriteString("export interface JoinOptions {\n")
	buf.WriteString("  /** Custom metadata to attach to the member */\n")
	buf.WriteString("  metadata?: Record<string, any>;\n")
	buf.WriteString("  /** Role to assign (if authorized) */\n")
	buf.WriteString("  role?: string;\n")
	buf.WriteString("}\n\n")

	// MessageHandler
	buf.WriteString("/**\n * Handler for room messages\n */\n")
	buf.WriteString("export type MessageHandler = (message: Message) => void;\n\n")

	// MemberHandler
	buf.WriteString("/**\n * Handler for member events\n */\n")
	buf.WriteString("export type MemberHandler = (member: Member) => void;\n\n")

	// RoomClientConfig
	buf.WriteString("/**\n * Configuration for the room client\n */\n")
	buf.WriteString("export interface RoomClientConfig {\n")
	buf.WriteString("  /** Base URL for the WebSocket connection */\n")
	buf.WriteString("  baseURL: string;\n")
	buf.WriteString("  /** Authentication configuration */\n")
	buf.WriteString("  auth?: AuthConfig;\n")
	buf.WriteString("  /** Connection timeout in ms (default: 30000) */\n")
	buf.WriteString("  connectionTimeout?: number;\n")
	buf.WriteString("  /** Request timeout in ms (default: 10000) */\n")
	buf.WriteString("  requestTimeout?: number;\n")

	if config.Features.Reconnection {
		buf.WriteString("  /** Maximum reconnection attempts (default: 10) */\n")
		buf.WriteString("  maxReconnectAttempts?: number;\n")
		buf.WriteString("  /** Initial reconnection delay in ms (default: 1000) */\n")
		buf.WriteString("  reconnectDelay?: number;\n")
		buf.WriteString("  /** Maximum reconnection delay in ms (default: 30000) */\n")
		buf.WriteString("  maxReconnectDelay?: number;\n")
	}

	buf.WriteString("  /** Enable offline message queue (default: true) */\n")
	buf.WriteString("  enableOfflineQueue?: boolean;\n")
	buf.WriteString("  /** Maximum messages in offline queue (default: 1000) */\n")
	buf.WriteString("  maxQueueSize?: number;\n")
	buf.WriteString("  /** Message TTL in queue in ms (default: 300000 - 5 min) */\n")
	buf.WriteString("  queueMessageTTL?: number;\n")
	buf.WriteString("  /** Maximum rooms per user (default: 50) */\n")
	buf.WriteString("  maxRoomsPerUser?: number;\n")
	buf.WriteString("}\n\n")

	// RoomState
	buf.WriteString("/**\n * State of a joined room\n */\n")
	buf.WriteString("export interface RoomState {\n")
	buf.WriteString("  /** Room ID */\n")
	buf.WriteString("  id: string;\n")
	buf.WriteString("  /** Room name */\n")
	buf.WriteString("  name?: string;\n")
	buf.WriteString("  /** Members in the room */\n")
	buf.WriteString("  members: Member[];\n")
	buf.WriteString("  /** Whether currently connected to this room */\n")
	buf.WriteString("  connected: boolean;\n")
	buf.WriteString("}\n\n")

	return buf.String()
}

// generateRoomClient generates the main RoomClient class.
func (r *RoomsGenerator) generateRoomClient(spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	// Class documentation
	buf.WriteString("/**\n")
	buf.WriteString(" * RoomClient manages real-time room-based communication.\n")
	buf.WriteString(" * \n")
	buf.WriteString(" * Features:\n")
	buf.WriteString(" * - Cross-platform (Browser & Node.js)\n")
	buf.WriteString(" * - Connection timeouts\n")
	buf.WriteString(" * - Offline message queue\n")
	buf.WriteString(" * - Join/leave rooms\n")
	buf.WriteString(" * - Send/receive messages in rooms\n")
	buf.WriteString(" * - Broadcast to all room members\n")

	if config.Streaming.EnableHistory {
		buf.WriteString(" * - Retrieve message history\n")
	}

	if config.Features.StateManagement {
		buf.WriteString(" * - Track member join/leave events\n")
	}

	if config.Features.Reconnection {
		buf.WriteString(" * - Automatic reconnection with exponential backoff\n")
	}

	buf.WriteString(" * \n")
	buf.WriteString(" * @example\n")
	buf.WriteString(" * ```typescript\n")
	buf.WriteString(" * const rooms = new RoomClient({ baseURL: 'ws://localhost:8080' });\n")
	buf.WriteString(" * await rooms.connect();\n")
	buf.WriteString(" * await rooms.join('my-room');\n")
	buf.WriteString(" * rooms.onMessage('my-room', (msg) => console.log(msg));\n")
	buf.WriteString(" * await rooms.send('my-room', { text: 'Hello!' });\n")
	buf.WriteString(" * ```\n")
	buf.WriteString(" */\n")

	// Class declaration
	buf.WriteString("export class RoomClient extends EventEmitter {\n")

	// Private fields
	buf.WriteString("  private ws: WebSocket | null = null;\n")
	buf.WriteString("  private config: Required<Pick<RoomClientConfig, 'baseURL'>> & RoomClientConfig;\n")
	buf.WriteString("  private state: ConnectionState = ConnectionState.DISCONNECTED;\n")
	buf.WriteString("  private closed: boolean = false;\n")
	buf.WriteString("  private connectionTimeoutId: ReturnType<typeof setTimeout> | null = null;\n")
	buf.WriteString("  private joinedRooms: Map<string, RoomState> = new Map();\n")
	buf.WriteString("  private messageHandlers: Map<string, Set<MessageHandler>> = new Map();\n")
	buf.WriteString("  private memberJoinHandlers: Map<string, Set<MemberHandler>> = new Map();\n")
	buf.WriteString("  private memberLeaveHandlers: Map<string, Set<MemberHandler>> = new Map();\n")
	buf.WriteString("  private messageQueue: QueuedRoomMessage[] = [];\n")
	buf.WriteString("  private pendingRequests: Map<string, { resolve: Function; reject: Function; timeout: ReturnType<typeof setTimeout> }> = new Map();\n")
	buf.WriteString("  private requestIdCounter: number = 0;\n")

	if config.Features.Reconnection {
		buf.WriteString("  private reconnectAttempts: number = 0;\n")
		buf.WriteString("  private reconnectTimeoutId: ReturnType<typeof setTimeout> | null = null;\n")
	}

	buf.WriteString("\n")

	// Constructor
	buf.WriteString("  constructor(config: RoomClientConfig) {\n")
	buf.WriteString("    super();\n")
	buf.WriteString("    this.config = {\n")
	buf.WriteString("      connectionTimeout: 30000,\n")
	buf.WriteString("      requestTimeout: 10000,\n")
	buf.WriteString("      maxRoomsPerUser: 50,\n")
	buf.WriteString("      enableOfflineQueue: true,\n")
	buf.WriteString("      maxQueueSize: 1000,\n")
	buf.WriteString("      queueMessageTTL: 300000,\n")

	if config.Features.Reconnection {
		buf.WriteString("      maxReconnectAttempts: 10,\n")
		buf.WriteString("      reconnectDelay: 1000,\n")
		buf.WriteString("      maxReconnectDelay: 30000,\n")
	}

	buf.WriteString("      ...config,\n")
	buf.WriteString("    };\n")
	buf.WriteString("  }\n\n")

	// connect method with timeout
	buf.WriteString("  /**\n")
	buf.WriteString("   * Connect to the WebSocket server.\n")
	buf.WriteString("   * @returns Promise that resolves when connected\n")
	buf.WriteString("   * @throws Error if connection fails or times out\n")
	buf.WriteString("   */\n")
	buf.WriteString("  async connect(): Promise<void> {\n")
	buf.WriteString("    if (this.state === ConnectionState.CONNECTED) {\n")
	buf.WriteString("      return;\n")
	buf.WriteString("    }\n\n")

	buf.WriteString("    return new Promise((resolve, reject) => {\n")
	buf.WriteString("      this.setState(ConnectionState.CONNECTING);\n")
	buf.WriteString("      this.closed = false;\n\n")

	// Determine WebSocket path from streaming spec
	wsPath := "/ws"
	if spec.Streaming != nil && spec.Streaming.Rooms != nil && spec.Streaming.Rooms.Path != "" {
		wsPath = spec.Streaming.Rooms.Path
	}

	buf.WriteString(fmt.Sprintf("      let wsURL = this.config.baseURL.replace(/^http/, 'ws') + '%s';\n\n", wsPath))

	// Add auth to URL
	buf.WriteString("      // Add auth to URL for browser compatibility\n")
	buf.WriteString("      if (this.config.auth?.bearerToken) {\n")
	buf.WriteString("        const separator = wsURL.includes('?') ? '&' : '?';\n")
	buf.WriteString("        wsURL += `${separator}token=${encodeURIComponent(this.config.auth.bearerToken)}`;\n")
	buf.WriteString("      }\n\n")

	// Setup connection timeout
	buf.WriteString("      // Setup connection timeout\n")
	buf.WriteString("      this.connectionTimeoutId = setTimeout(() => {\n")
	buf.WriteString("        this.connectionTimeoutId = null;\n")
	buf.WriteString("        if (this.ws) {\n")
	buf.WriteString("          this.ws.close();\n")
	buf.WriteString("          this.ws = null;\n")
	buf.WriteString("        }\n")
	buf.WriteString("        this.setState(ConnectionState.ERROR);\n")
	buf.WriteString("        reject(new Error('Connection timeout'));\n")
	buf.WriteString("      }, this.config.connectionTimeout);\n\n")

	buf.WriteString("      try {\n")
	buf.WriteString("        const WS = getWebSocket();\n\n")

	buf.WriteString("        if (isBrowser) {\n")
	buf.WriteString("          this.ws = new WS(wsURL);\n")
	buf.WriteString("        } else {\n")
	buf.WriteString("          // Node.js: can pass headers\n")
	buf.WriteString("          const headers: Record<string, string> = {};\n")
	buf.WriteString("          if (this.config.auth?.bearerToken) {\n")
	buf.WriteString("            headers['Authorization'] = `Bearer ${this.config.auth.bearerToken}`;\n")
	buf.WriteString("          }\n")
	buf.WriteString("          if (this.config.auth?.apiKey) {\n")
	buf.WriteString("            headers['X-API-Key'] = this.config.auth.apiKey;\n")
	buf.WriteString("          }\n")
	buf.WriteString("          this.ws = new (WS as any)(wsURL, { headers });\n")
	buf.WriteString("        }\n\n")

	buf.WriteString("        this.ws.onopen = () => {\n")
	buf.WriteString("          this.clearConnectionTimeout();\n")
	buf.WriteString("          this.setState(ConnectionState.CONNECTED);\n")
	if config.Features.Reconnection {
		buf.WriteString("          this.reconnectAttempts = 0;\n")
	}
	buf.WriteString("          this.flushQueue();\n")
	buf.WriteString("          resolve();\n")
	buf.WriteString("        };\n\n")

	buf.WriteString("        this.ws.onmessage = (event: MessageEvent) => {\n")
	buf.WriteString("          try {\n")
	buf.WriteString("            const data = typeof event.data === 'string' ? event.data : event.data.toString();\n")
	buf.WriteString("            const message = JSON.parse(data);\n")
	buf.WriteString("            this.handleMessage(message);\n")
	buf.WriteString("          } catch (error) {\n")
	buf.WriteString("            this.emit('error', error);\n")
	buf.WriteString("          }\n")
	buf.WriteString("        };\n\n")

	buf.WriteString("        this.ws.onerror = (event: Event) => {\n")
	buf.WriteString("          this.clearConnectionTimeout();\n")
	buf.WriteString("          this.setState(ConnectionState.ERROR);\n")
	buf.WriteString("          const error = new Error('WebSocket error');\n")
	buf.WriteString("          this.emit('error', error);\n")
	buf.WriteString("          reject(error);\n")
	buf.WriteString("        };\n\n")

	buf.WriteString("        this.ws.onclose = (event: CloseEvent) => {\n")
	buf.WriteString("          this.clearConnectionTimeout();\n")
	buf.WriteString("          this.rejectAllPendingRequests(new Error('Connection closed'));\n\n")

	buf.WriteString("          if (this.closed) {\n")
	buf.WriteString("            this.setState(ConnectionState.CLOSED);\n")
	buf.WriteString("          } else {\n")
	buf.WriteString("            this.setState(ConnectionState.DISCONNECTED);\n")
	if config.Features.Reconnection {
		buf.WriteString("            this.scheduleReconnect();\n")
	}
	buf.WriteString("          }\n")
	buf.WriteString("          this.emit('close', event);\n")
	buf.WriteString("        };\n")

	buf.WriteString("      } catch (error) {\n")
	buf.WriteString("        this.clearConnectionTimeout();\n")
	buf.WriteString("        this.setState(ConnectionState.ERROR);\n")
	buf.WriteString("        reject(error);\n")
	buf.WriteString("      }\n")
	buf.WriteString("    });\n")
	buf.WriteString("  }\n\n")

	// disconnect method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Disconnect from the WebSocket server.\n")
	buf.WriteString("   * @param rejectQueuedMessages - If true, reject all queued messages (default: false)\n")
	buf.WriteString("   */\n")
	buf.WriteString("  disconnect(rejectQueuedMessages: boolean = false): void {\n")
	buf.WriteString("    this.closed = true;\n")
	buf.WriteString("    this.clearConnectionTimeout();\n")
	if config.Features.Reconnection {
		buf.WriteString("    this.cancelReconnect();\n")
	}
	buf.WriteString("\n")

	buf.WriteString("    if (rejectQueuedMessages) {\n")
	buf.WriteString("      this.rejectAllQueuedMessages(new Error('Connection closed'));\n")
	buf.WriteString("    }\n\n")

	buf.WriteString("    this.rejectAllPendingRequests(new Error('Connection closed'));\n\n")

	buf.WriteString("    if (this.ws) {\n")
	buf.WriteString("      this.ws.close();\n")
	buf.WriteString("      this.ws = null;\n")
	buf.WriteString("    }\n")
	buf.WriteString("    this.joinedRooms.clear();\n")
	buf.WriteString("    this.setState(ConnectionState.CLOSED);\n")
	buf.WriteString("  }\n\n")

	// Private helper methods
	buf.WriteString("  private clearConnectionTimeout(): void {\n")
	buf.WriteString("    if (this.connectionTimeoutId) {\n")
	buf.WriteString("      clearTimeout(this.connectionTimeoutId);\n")
	buf.WriteString("      this.connectionTimeoutId = null;\n")
	buf.WriteString("    }\n")
	buf.WriteString("  }\n\n")

	buf.WriteString("  private generateRequestId(): string {\n")
	buf.WriteString("    return `req_${++this.requestIdCounter}_${Date.now()}`;\n")
	buf.WriteString("  }\n\n")

	// sendRequest with timeout
	buf.WriteString("  private sendRequest(type: string, payload: any): Promise<any> {\n")
	buf.WriteString("    return new Promise((resolve, reject) => {\n")
	buf.WriteString("      const OPEN = 1; // WebSocket.OPEN\n")
	buf.WriteString("      if (!this.ws || this.ws.readyState !== OPEN) {\n")
	buf.WriteString("        reject(new Error('WebSocket is not connected'));\n")
	buf.WriteString("        return;\n")
	buf.WriteString("      }\n\n")

	buf.WriteString("      const requestId = this.generateRequestId();\n")
	buf.WriteString("      const timeout = setTimeout(() => {\n")
	buf.WriteString("        this.pendingRequests.delete(requestId);\n")
	buf.WriteString("        reject(new Error(`Request timeout: ${type}`));\n")
	buf.WriteString("      }, this.config.requestTimeout || 10000);\n\n")

	buf.WriteString("      this.pendingRequests.set(requestId, { resolve, reject, timeout });\n\n")

	buf.WriteString("      this.ws.send(JSON.stringify({\n")
	buf.WriteString("        type,\n")
	buf.WriteString("        request_id: requestId,\n")
	buf.WriteString("        ...payload,\n")
	buf.WriteString("      }));\n")
	buf.WriteString("    });\n")
	buf.WriteString("  }\n\n")

	buf.WriteString("  private rejectAllPendingRequests(error: Error): void {\n")
	buf.WriteString("    this.pendingRequests.forEach(({ reject, timeout }) => {\n")
	buf.WriteString("      clearTimeout(timeout);\n")
	buf.WriteString("      reject(error);\n")
	buf.WriteString("    });\n")
	buf.WriteString("    this.pendingRequests.clear();\n")
	buf.WriteString("  }\n\n")

	// join method with timeout
	buf.WriteString("  /**\n")
	buf.WriteString("   * Join a room.\n")
	buf.WriteString("   * @param roomId - The ID of the room to join\n")
	buf.WriteString("   * @param options - Optional join options\n")
	buf.WriteString("   * @returns Promise that resolves when joined\n")
	buf.WriteString("   * @throws Error if not connected, room limit reached, or join fails\n")
	buf.WriteString("   */\n")
	buf.WriteString("  async join(roomId: string, options?: JoinOptions): Promise<void> {\n")
	buf.WriteString("    if (this.joinedRooms.size >= (this.config.maxRoomsPerUser || 50)) {\n")
	buf.WriteString("      throw new Error('Maximum rooms per user limit reached');\n")
	buf.WriteString("    }\n\n")

	buf.WriteString("    const response = await this.sendRequest('join', {\n")
	buf.WriteString("      room_id: roomId,\n")
	buf.WriteString("      ...options,\n")
	buf.WriteString("    });\n\n")

	buf.WriteString("    if (response.error) {\n")
	buf.WriteString("      throw new Error(response.error);\n")
	buf.WriteString("    }\n\n")

	buf.WriteString("    this.joinedRooms.set(roomId, {\n")
	buf.WriteString("      id: roomId,\n")
	buf.WriteString("      name: response.room_name,\n")
	buf.WriteString("      members: response.members || [],\n")
	buf.WriteString("      connected: true,\n")
	buf.WriteString("    });\n")
	buf.WriteString("  }\n\n")

	// leave method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Leave a room.\n")
	buf.WriteString("   * @param roomId - The ID of the room to leave\n")
	buf.WriteString("   * @returns Promise that resolves when left\n")
	buf.WriteString("   */\n")
	buf.WriteString("  async leave(roomId: string): Promise<void> {\n")
	buf.WriteString("    const OPEN = 1; // WebSocket.OPEN\n")
	buf.WriteString("    if (this.ws && this.ws.readyState === OPEN) {\n")
	buf.WriteString("      this.ws.send(JSON.stringify({\n")
	buf.WriteString("        type: 'leave',\n")
	buf.WriteString("        room_id: roomId,\n")
	buf.WriteString("      }));\n")
	buf.WriteString("    }\n\n")

	buf.WriteString("    this.joinedRooms.delete(roomId);\n")
	buf.WriteString("    this.messageHandlers.delete(roomId);\n")
	buf.WriteString("    this.memberJoinHandlers.delete(roomId);\n")
	buf.WriteString("    this.memberLeaveHandlers.delete(roomId);\n")
	buf.WriteString("  }\n\n")

	// send method with offline queue
	buf.WriteString("  /**\n")
	buf.WriteString("   * Send a message to a room. If offline, queues the message for later.\n")
	buf.WriteString("   * @param roomId - The ID of the room\n")
	buf.WriteString("   * @param data - The message data to send\n")
	buf.WriteString("   * @returns Promise that resolves when sent (or queued)\n")
	buf.WriteString("   */\n")
	buf.WriteString("  async send(roomId: string, data: any): Promise<void> {\n")
	buf.WriteString("    if (!this.joinedRooms.has(roomId)) {\n")
	buf.WriteString("      throw new Error('Not joined to room: ' + roomId);\n")
	buf.WriteString("    }\n\n")

	buf.WriteString("    const OPEN = 1; // WebSocket.OPEN\n")
	buf.WriteString("    if (this.ws && this.ws.readyState === OPEN) {\n")
	buf.WriteString("      this.ws.send(JSON.stringify({\n")
	buf.WriteString("        type: 'message',\n")
	buf.WriteString("        room_id: roomId,\n")
	buf.WriteString("        data,\n")
	buf.WriteString("        timestamp: new Date().toISOString(),\n")
	buf.WriteString("      }));\n")
	buf.WriteString("      return;\n")
	buf.WriteString("    }\n\n")

	buf.WriteString("    // Queue message for later if offline queue is enabled\n")
	buf.WriteString("    if (!this.config.enableOfflineQueue) {\n")
	buf.WriteString("      throw new Error('WebSocket is not connected');\n")
	buf.WriteString("    }\n\n")

	buf.WriteString("    return new Promise((resolve, reject) => {\n")
	buf.WriteString("      if (this.messageQueue.length >= (this.config.maxQueueSize || 1000)) {\n")
	buf.WriteString("        reject(new Error('Message queue full'));\n")
	buf.WriteString("        return;\n")
	buf.WriteString("      }\n\n")

	buf.WriteString("      this.messageQueue.push({\n")
	buf.WriteString("        roomId,\n")
	buf.WriteString("        data,\n")
	buf.WriteString("        timestamp: Date.now(),\n")
	buf.WriteString("        resolve,\n")
	buf.WriteString("        reject,\n")
	buf.WriteString("      });\n")
	buf.WriteString("    });\n")
	buf.WriteString("  }\n\n")

	// sendSync method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Send a message immediately. Throws if not connected.\n")
	buf.WriteString("   * @param roomId - The ID of the room\n")
	buf.WriteString("   * @param data - The message data to send\n")
	buf.WriteString("   */\n")
	buf.WriteString("  sendSync(roomId: string, data: any): void {\n")
	buf.WriteString("    const OPEN = 1; // WebSocket.OPEN\n")
	buf.WriteString("    if (!this.ws || this.ws.readyState !== OPEN) {\n")
	buf.WriteString("      throw new Error('WebSocket is not connected');\n")
	buf.WriteString("    }\n\n")

	buf.WriteString("    if (!this.joinedRooms.has(roomId)) {\n")
	buf.WriteString("      throw new Error('Not joined to room: ' + roomId);\n")
	buf.WriteString("    }\n\n")

	buf.WriteString("    this.ws.send(JSON.stringify({\n")
	buf.WriteString("      type: 'message',\n")
	buf.WriteString("      room_id: roomId,\n")
	buf.WriteString("      data,\n")
	buf.WriteString("      timestamp: new Date().toISOString(),\n")
	buf.WriteString("    }));\n")
	buf.WriteString("  }\n\n")

	// Queue management methods
	buf.WriteString("  /**\n")
	buf.WriteString("   * Get the number of messages in the offline queue.\n")
	buf.WriteString("   */\n")
	buf.WriteString("  getQueueSize(): number {\n")
	buf.WriteString("    return this.messageQueue.length;\n")
	buf.WriteString("  }\n\n")

	buf.WriteString("  /**\n")
	buf.WriteString("   * Clear all messages from the offline queue.\n")
	buf.WriteString("   * @param rejectPending - If true, reject pending promises (default: false)\n")
	buf.WriteString("   */\n")
	buf.WriteString("  clearQueue(rejectPending: boolean = false): void {\n")
	buf.WriteString("    if (rejectPending) {\n")
	buf.WriteString("      const error = new Error('Queue cleared');\n")
	buf.WriteString("      this.messageQueue.forEach(msg => msg.reject(error));\n")
	buf.WriteString("    }\n")
	buf.WriteString("    this.messageQueue = [];\n")
	buf.WriteString("  }\n\n")

	buf.WriteString("  private flushQueue(): void {\n")
	buf.WriteString("    const OPEN = 1; // WebSocket.OPEN\n")
	buf.WriteString("    if (!this.ws || this.ws.readyState !== OPEN) return;\n\n")

	buf.WriteString("    const now = Date.now();\n")
	buf.WriteString("    const ttl = this.config.queueMessageTTL || 300000;\n\n")

	buf.WriteString("    while (this.messageQueue.length > 0) {\n")
	buf.WriteString("      const msg = this.messageQueue[0];\n")
	buf.WriteString("      \n")
	buf.WriteString("      // Check if message expired\n")
	buf.WriteString("      if (now - msg.timestamp > ttl) {\n")
	buf.WriteString("        this.messageQueue.shift();\n")
	buf.WriteString("        msg.reject(new Error('Message expired in queue'));\n")
	buf.WriteString("        continue;\n")
	buf.WriteString("      }\n\n")

	buf.WriteString("      // Skip if no longer joined to room\n")
	buf.WriteString("      if (!this.joinedRooms.has(msg.roomId)) {\n")
	buf.WriteString("        this.messageQueue.shift();\n")
	buf.WriteString("        msg.reject(new Error('No longer joined to room'));\n")
	buf.WriteString("        continue;\n")
	buf.WriteString("      }\n\n")

	buf.WriteString("      try {\n")
	buf.WriteString("        this.ws.send(JSON.stringify({\n")
	buf.WriteString("          type: 'message',\n")
	buf.WriteString("          room_id: msg.roomId,\n")
	buf.WriteString("          data: msg.data,\n")
	buf.WriteString("          timestamp: new Date().toISOString(),\n")
	buf.WriteString("        }));\n")
	buf.WriteString("        this.messageQueue.shift();\n")
	buf.WriteString("        msg.resolve();\n")
	buf.WriteString("      } catch (error) {\n")
	buf.WriteString("        // Stop flushing on error, will retry on next connection\n")
	buf.WriteString("        break;\n")
	buf.WriteString("      }\n")
	buf.WriteString("    }\n")
	buf.WriteString("  }\n\n")

	buf.WriteString("  private rejectAllQueuedMessages(error: Error): void {\n")
	buf.WriteString("    this.messageQueue.forEach(msg => msg.reject(error));\n")
	buf.WriteString("    this.messageQueue = [];\n")
	buf.WriteString("  }\n\n")

	// broadcast method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Broadcast a message to all members in a room.\n")
	buf.WriteString("   * @param roomId - The ID of the room\n")
	buf.WriteString("   * @param data - The message data to broadcast\n")
	buf.WriteString("   * @returns Promise that resolves when broadcasted\n")
	buf.WriteString("   */\n")
	buf.WriteString("  async broadcast(roomId: string, data: any): Promise<void> {\n")
	buf.WriteString("    return this.send(roomId, data);\n")
	buf.WriteString("  }\n\n")

	// getHistory method (if enabled)
	if config.Streaming.EnableHistory {
		buf.WriteString("  /**\n")
		buf.WriteString("   * Get message history for a room.\n")
		buf.WriteString("   * @param roomId - The ID of the room\n")
		buf.WriteString("   * @param query - Optional query parameters\n")
		buf.WriteString("   * @returns Promise resolving to message history\n")
		buf.WriteString("   */\n")
		buf.WriteString("  async getHistory(roomId: string, query?: HistoryQuery): Promise<Message[]> {\n")
		buf.WriteString("    const response = await this.sendRequest('history', {\n")
		buf.WriteString("      room_id: roomId,\n")
		buf.WriteString("      ...query,\n")
		buf.WriteString("    });\n\n")

		buf.WriteString("    if (response.error) {\n")
		buf.WriteString("      throw new Error(response.error);\n")
		buf.WriteString("    }\n\n")

		buf.WriteString("    return response.messages || [];\n")
		buf.WriteString("  }\n\n")
	}

	// getMembers method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Get current members in a room.\n")
	buf.WriteString("   * @param roomId - The ID of the room\n")
	buf.WriteString("   * @returns Array of members in the room\n")
	buf.WriteString("   */\n")
	buf.WriteString("  getMembers(roomId: string): Member[] {\n")
	buf.WriteString("    const room = this.joinedRooms.get(roomId);\n")
	buf.WriteString("    return room?.members || [];\n")
	buf.WriteString("  }\n\n")

	// getJoinedRooms method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Get all rooms the client has joined.\n")
	buf.WriteString("   * @returns Array of joined room IDs\n")
	buf.WriteString("   */\n")
	buf.WriteString("  getJoinedRooms(): string[] {\n")
	buf.WriteString("    return Array.from(this.joinedRooms.keys());\n")
	buf.WriteString("  }\n\n")

	// getRoomState method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Get the state of a joined room.\n")
	buf.WriteString("   * @param roomId - The ID of the room\n")
	buf.WriteString("   * @returns Room state or undefined if not joined\n")
	buf.WriteString("   */\n")
	buf.WriteString("  getRoomState(roomId: string): RoomState | undefined {\n")
	buf.WriteString("    return this.joinedRooms.get(roomId);\n")
	buf.WriteString("  }\n\n")

	// onMessage method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Register a handler for messages in a room.\n")
	buf.WriteString("   * @param roomId - The ID of the room\n")
	buf.WriteString("   * @param handler - Function to call when a message is received\n")
	buf.WriteString("   */\n")
	buf.WriteString("  onMessage(roomId: string, handler: MessageHandler): void {\n")
	buf.WriteString("    if (!this.messageHandlers.has(roomId)) {\n")
	buf.WriteString("      this.messageHandlers.set(roomId, new Set());\n")
	buf.WriteString("    }\n")
	buf.WriteString("    this.messageHandlers.get(roomId)!.add(handler);\n")
	buf.WriteString("  }\n\n")

	// offMessage method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Remove a message handler for a room.\n")
	buf.WriteString("   * @param roomId - The ID of the room\n")
	buf.WriteString("   * @param handler - The handler to remove\n")
	buf.WriteString("   */\n")
	buf.WriteString("  offMessage(roomId: string, handler: MessageHandler): void {\n")
	buf.WriteString("    this.messageHandlers.get(roomId)?.delete(handler);\n")
	buf.WriteString("  }\n\n")

	// onMemberJoin method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Register a handler for member join events in a room.\n")
	buf.WriteString("   * @param roomId - The ID of the room\n")
	buf.WriteString("   * @param handler - Function to call when a member joins\n")
	buf.WriteString("   */\n")
	buf.WriteString("  onMemberJoin(roomId: string, handler: MemberHandler): void {\n")
	buf.WriteString("    if (!this.memberJoinHandlers.has(roomId)) {\n")
	buf.WriteString("      this.memberJoinHandlers.set(roomId, new Set());\n")
	buf.WriteString("    }\n")
	buf.WriteString("    this.memberJoinHandlers.get(roomId)!.add(handler);\n")
	buf.WriteString("  }\n\n")

	// onMemberLeave method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Register a handler for member leave events in a room.\n")
	buf.WriteString("   * @param roomId - The ID of the room\n")
	buf.WriteString("   * @param handler - Function to call when a member leaves\n")
	buf.WriteString("   */\n")
	buf.WriteString("  onMemberLeave(roomId: string, handler: MemberHandler): void {\n")
	buf.WriteString("    if (!this.memberLeaveHandlers.has(roomId)) {\n")
	buf.WriteString("      this.memberLeaveHandlers.set(roomId, new Set());\n")
	buf.WriteString("    }\n")
	buf.WriteString("    this.memberLeaveHandlers.get(roomId)!.add(handler);\n")
	buf.WriteString("  }\n\n")

	// getState and onStateChange methods
	buf.WriteString("  /**\n")
	buf.WriteString("   * Get the current connection state.\n")
	buf.WriteString("   * @returns Current connection state\n")
	buf.WriteString("   */\n")
	buf.WriteString("  getState(): ConnectionState {\n")
	buf.WriteString("    return this.state;\n")
	buf.WriteString("  }\n\n")

	buf.WriteString("  /**\n")
	buf.WriteString("   * Register a handler for connection state changes.\n")
	buf.WriteString("   * @param handler - Function to call when state changes\n")
	buf.WriteString("   */\n")
	buf.WriteString("  onStateChange(handler: (state: ConnectionState) => void): void {\n")
	buf.WriteString("    this.on('stateChange', handler);\n")
	buf.WriteString("  }\n\n")

	// isConnected helper
	buf.WriteString("  /**\n")
	buf.WriteString("   * Check if the WebSocket is currently connected.\n")
	buf.WriteString("   */\n")
	buf.WriteString("  isConnected(): boolean {\n")
	buf.WriteString("    return this.state === ConnectionState.CONNECTED;\n")
	buf.WriteString("  }\n\n")

	// Private methods
	buf.WriteString("  // Private methods\n\n")

	// handleMessage
	buf.WriteString("  private handleMessage(message: any): void {\n")
	buf.WriteString("    // Handle response to pending request\n")
	buf.WriteString("    if (message.request_id && this.pendingRequests.has(message.request_id)) {\n")
	buf.WriteString("      const { resolve, timeout } = this.pendingRequests.get(message.request_id)!;\n")
	buf.WriteString("      clearTimeout(timeout);\n")
	buf.WriteString("      this.pendingRequests.delete(message.request_id);\n")
	buf.WriteString("      resolve(message);\n")
	buf.WriteString("      return;\n")
	buf.WriteString("    }\n\n")

	buf.WriteString("    switch (message.type) {\n")
	buf.WriteString("      case 'message':\n")
	buf.WriteString("        this.handleRoomMessage(message);\n")
	buf.WriteString("        break;\n")
	buf.WriteString("      case 'member_join':\n")
	buf.WriteString("        this.handleMemberJoin(message);\n")
	buf.WriteString("        break;\n")
	buf.WriteString("      case 'member_leave':\n")
	buf.WriteString("        this.handleMemberLeave(message);\n")
	buf.WriteString("        break;\n")
	buf.WriteString("      case 'error':\n")
	buf.WriteString("        this.emit('error', new Error(message.message || message.error));\n")
	buf.WriteString("        break;\n")
	buf.WriteString("    }\n")
	buf.WriteString("  }\n\n")

	// handleRoomMessage
	buf.WriteString("  private handleRoomMessage(message: any): void {\n")
	buf.WriteString("    const roomId = message.room_id;\n")
	buf.WriteString("    const handlers = this.messageHandlers.get(roomId);\n")
	buf.WriteString("    if (handlers) {\n")
	buf.WriteString("      handlers.forEach(handler => {\n")
	buf.WriteString("        try {\n")
	buf.WriteString("          handler(message);\n")
	buf.WriteString("        } catch (error) {\n")
	buf.WriteString("          console.error('Message handler error:', error);\n")
	buf.WriteString("        }\n")
	buf.WriteString("      });\n")
	buf.WriteString("    }\n")
	buf.WriteString("    this.emit('message', message);\n")
	buf.WriteString("  }\n\n")

	// handleMemberJoin
	buf.WriteString("  private handleMemberJoin(message: any): void {\n")
	buf.WriteString("    const roomId = message.room_id;\n")
	buf.WriteString("    const member = message.member;\n\n")

	buf.WriteString("    // Update room state\n")
	buf.WriteString("    const room = this.joinedRooms.get(roomId);\n")
	buf.WriteString("    if (room && member) {\n")
	buf.WriteString("      room.members.push(member);\n")
	buf.WriteString("    }\n\n")

	buf.WriteString("    // Notify handlers\n")
	buf.WriteString("    const handlers = this.memberJoinHandlers.get(roomId);\n")
	buf.WriteString("    if (handlers && member) {\n")
	buf.WriteString("      handlers.forEach(handler => {\n")
	buf.WriteString("        try {\n")
	buf.WriteString("          handler(member);\n")
	buf.WriteString("        } catch (error) {\n")
	buf.WriteString("          console.error('Member join handler error:', error);\n")
	buf.WriteString("        }\n")
	buf.WriteString("      });\n")
	buf.WriteString("    }\n")
	buf.WriteString("    this.emit('memberJoin', { roomId, member });\n")
	buf.WriteString("  }\n\n")

	// handleMemberLeave
	buf.WriteString("  private handleMemberLeave(message: any): void {\n")
	buf.WriteString("    const roomId = message.room_id;\n")
	buf.WriteString("    const member = message.member;\n\n")

	buf.WriteString("    // Update room state\n")
	buf.WriteString("    const room = this.joinedRooms.get(roomId);\n")
	buf.WriteString("    if (room && member) {\n")
	buf.WriteString("      room.members = room.members.filter(m => m.user_id !== member.user_id);\n")
	buf.WriteString("    }\n\n")

	buf.WriteString("    // Notify handlers\n")
	buf.WriteString("    const handlers = this.memberLeaveHandlers.get(roomId);\n")
	buf.WriteString("    if (handlers && member) {\n")
	buf.WriteString("      handlers.forEach(handler => {\n")
	buf.WriteString("        try {\n")
	buf.WriteString("          handler(member);\n")
	buf.WriteString("        } catch (error) {\n")
	buf.WriteString("          console.error('Member leave handler error:', error);\n")
	buf.WriteString("        }\n")
	buf.WriteString("      });\n")
	buf.WriteString("    }\n")
	buf.WriteString("    this.emit('memberLeave', { roomId, member });\n")
	buf.WriteString("  }\n\n")

	// setState
	buf.WriteString("  private setState(state: ConnectionState): void {\n")
	buf.WriteString("    if (this.state !== state) {\n")
	buf.WriteString("      this.state = state;\n")
	buf.WriteString("      this.emit('stateChange', state);\n")
	buf.WriteString("    }\n")
	buf.WriteString("  }\n\n")

	// Reconnection logic
	if config.Features.Reconnection {
		buf.WriteString("  private scheduleReconnect(): void {\n")
		buf.WriteString("    const maxAttempts = this.config.maxReconnectAttempts || 10;\n")
		buf.WriteString("    if (this.reconnectAttempts >= maxAttempts) {\n")
		buf.WriteString("      this.setState(ConnectionState.CLOSED);\n")
		buf.WriteString("      this.rejectAllQueuedMessages(new Error('Max reconnection attempts reached'));\n")
		buf.WriteString("      return;\n")
		buf.WriteString("    }\n\n")

		buf.WriteString("    this.setState(ConnectionState.RECONNECTING);\n")
		buf.WriteString("    this.reconnectAttempts++;\n\n")

		buf.WriteString("    const delay = Math.min(\n")
		buf.WriteString("      (this.config.reconnectDelay || 1000) * Math.pow(2, this.reconnectAttempts - 1),\n")
		buf.WriteString("      this.config.maxReconnectDelay || 30000\n")
		buf.WriteString("    );\n\n")

		buf.WriteString("    this.reconnectTimeoutId = setTimeout(async () => {\n")
		buf.WriteString("      this.reconnectTimeoutId = null;\n")
		buf.WriteString("      try {\n")
		buf.WriteString("        await this.connect();\n")
		buf.WriteString("        // Rejoin rooms after reconnection\n")
		buf.WriteString("        for (const [roomId, roomState] of this.joinedRooms) {\n")
		buf.WriteString("          try {\n")
		buf.WriteString("            await this.join(roomId);\n")
		buf.WriteString("          } catch (error) {\n")
		buf.WriteString("            console.error(`Failed to rejoin room ${roomId}:`, error);\n")
		buf.WriteString("            roomState.connected = false;\n")
		buf.WriteString("          }\n")
		buf.WriteString("        }\n")
		buf.WriteString("      } catch (error) {\n")
		buf.WriteString("        // Will schedule another reconnect in onclose handler\n")
		buf.WriteString("      }\n")
		buf.WriteString("    }, delay);\n")
		buf.WriteString("  }\n\n")

		buf.WriteString("  private cancelReconnect(): void {\n")
		buf.WriteString("    if (this.reconnectTimeoutId) {\n")
		buf.WriteString("      clearTimeout(this.reconnectTimeoutId);\n")
		buf.WriteString("      this.reconnectTimeoutId = null;\n")
		buf.WriteString("    }\n")
		buf.WriteString("  }\n")
	}

	buf.WriteString("}\n")

	return buf.String()
}
