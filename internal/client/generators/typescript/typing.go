package typescript

import (
	"fmt"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// TypingGenerator generates TypeScript typing indicator client code.
type TypingGenerator struct{}

// NewTypingGenerator creates a new typing generator.
func NewTypingGenerator() *TypingGenerator {
	return &TypingGenerator{}
}

// Generate generates the TypingClient TypeScript code.
func (t *TypingGenerator) Generate(spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	buf.WriteString(t.generateImports(config))
	buf.WriteString("\n")
	buf.WriteString(t.generateTypes(spec, config))
	buf.WriteString("\n")
	buf.WriteString(t.generateTypingClient(spec, config))

	return buf.String()
}

// generateImports generates import statements for the typing client.
func (t *TypingGenerator) generateImports(_ client.GeneratorConfig) string {
	var buf strings.Builder

	buf.WriteString(t.generatePolyfillSetup())
	buf.WriteString("\n")
	buf.WriteString("// Typing indicator client for real-time typing status\n\n")
	buf.WriteString("import { ConnectionState, AuthConfig } from './types';\n\n")

	return buf.String()
}

// generatePolyfillSetup generates the browser/Node.js compatibility layer.
func (t *TypingGenerator) generatePolyfillSetup() string {
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

// generateTypes generates typing-specific types.
func (t *TypingGenerator) generateTypes(_ *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	// TypingUser
	buf.WriteString("/**\n * Represents a user who is currently typing\n */\n")
	buf.WriteString("export interface TypingUser {\n")
	buf.WriteString("  /** User ID */\n")
	buf.WriteString("  userId: string;\n")
	buf.WriteString("  /** Room ID where typing */\n")
	buf.WriteString("  roomId: string;\n")
	buf.WriteString("  /** When typing started */\n")
	buf.WriteString("  startedAt: string;\n")
	buf.WriteString("}\n\n")

	// TypingHandler
	buf.WriteString("/**\n * Handler for typing events\n */\n")
	buf.WriteString("export type TypingHandler = (user: TypingUser) => void;\n\n")

	// TypingClientConfig
	buf.WriteString("/**\n * Configuration for the typing client\n */\n")
	buf.WriteString("export interface TypingClientConfig {\n")
	buf.WriteString("  /** Base URL for the WebSocket connection */\n")
	buf.WriteString("  baseURL: string;\n")
	buf.WriteString("  /** Authentication configuration */\n")
	buf.WriteString("  auth?: AuthConfig;\n")

	buf.WriteString("  /** Connection timeout in ms (default: 30000) */\n")
	buf.WriteString("  connectionTimeout?: number;\n")

	if config.Features.Reconnection {
		buf.WriteString("  /** Maximum reconnection attempts (default: 10) */\n")
		buf.WriteString("  maxReconnectAttempts?: number;\n")
		buf.WriteString("  /** Initial reconnection delay in ms (default: 1000) */\n")
		buf.WriteString("  reconnectDelay?: number;\n")
		buf.WriteString("  /** Maximum reconnection delay in ms (default: 30000) */\n")
		buf.WriteString("  maxReconnectDelay?: number;\n")
	}

	buf.WriteString("  /** Auto-stop timeout in ms (default: 3000) */\n")
	buf.WriteString("  timeoutMs?: number;\n")
	buf.WriteString("  /** Debounce interval in ms (default: 300) */\n")
	buf.WriteString("  debounceMs?: number;\n")
	buf.WriteString("}\n\n")

	return buf.String()
}

// generateTypingClient generates the main TypingClient class.
func (t *TypingGenerator) generateTypingClient(spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	// Class documentation
	buf.WriteString("/**\n")
	buf.WriteString(" * TypingClient manages real-time typing indicators.\n")
	buf.WriteString(" * \n")
	buf.WriteString(" * Features:\n")
	buf.WriteString(" * - Start/stop typing indicators\n")
	buf.WriteString(" * - Auto-stop after timeout\n")
	buf.WriteString(" * - Debounced updates to reduce noise\n")
	buf.WriteString(" * - Track who is typing in each room\n")
	buf.WriteString(" * \n")
	buf.WriteString(" * @example\n")
	buf.WriteString(" * ```typescript\n")
	buf.WriteString(" * const typing = new TypingClient({ baseURL: 'ws://localhost:8080' });\n")
	buf.WriteString(" * await typing.connect();\n")
	buf.WriteString(" * \n")
	buf.WriteString(" * // Start typing when user types\n")
	buf.WriteString(" * inputElement.addEventListener('input', () => {\n")
	buf.WriteString(" *   typing.startTyping('room-1');\n")
	buf.WriteString(" * });\n")
	buf.WriteString(" * \n")
	buf.WriteString(" * // Listen for others typing\n")
	buf.WriteString(" * typing.onTypingStart('room-1', (user) => {\n")
	buf.WriteString(" *   console.log(`${user.userId} is typing...`);\n")
	buf.WriteString(" * });\n")
	buf.WriteString(" * ```\n")
	buf.WriteString(" */\n")

	// Class declaration
	buf.WriteString("export class TypingClient extends EventEmitter {\n")

	// Private fields
	buf.WriteString("  private ws: WebSocket | null = null;\n")
	buf.WriteString("  private config: Required<Pick<TypingClientConfig, 'baseURL'>> & TypingClientConfig;\n")
	buf.WriteString("  private state: ConnectionState = ConnectionState.DISCONNECTED;\n")
	buf.WriteString("  private closed: boolean = false;\n")
	buf.WriteString("  private connectionTimeoutId: ReturnType<typeof setTimeout> | null = null;\n")
	buf.WriteString("  private typingUsers: Map<string, Map<string, TypingUser>> = new Map(); // roomId -> (userId -> TypingUser)\n")
	buf.WriteString("  private typingTimers: Map<string, ReturnType<typeof setTimeout>> = new Map(); // roomId -> timer\n")
	buf.WriteString("  private debounceTimers: Map<string, ReturnType<typeof setTimeout>> = new Map(); // roomId -> timer\n")
	buf.WriteString("  private startHandlers: Map<string, Set<TypingHandler>> = new Map();\n")
	buf.WriteString("  private stopHandlers: Map<string, Set<TypingHandler>> = new Map();\n")

	if config.Features.Reconnection {
		buf.WriteString("  private reconnectAttempts: number = 0;\n")
		buf.WriteString("  private reconnectTimeoutId: ReturnType<typeof setTimeout> | null = null;\n")
	}

	buf.WriteString("\n")

	// Get timeout and debounce values
	timeoutMs := config.Streaming.TypingConfig.TimeoutMs
	if timeoutMs == 0 {
		timeoutMs = 3000
	}
	debounceMs := config.Streaming.TypingConfig.DebounceMs
	if debounceMs == 0 {
		debounceMs = 300
	}

	// Constructor
	buf.WriteString("  constructor(config: TypingClientConfig) {\n")
	buf.WriteString("    super();\n")
	buf.WriteString("    this.config = {\n")
	buf.WriteString("      connectionTimeout: 30000,\n")
	buf.WriteString(fmt.Sprintf("      timeoutMs: %d,\n", timeoutMs))
	buf.WriteString(fmt.Sprintf("      debounceMs: %d,\n", debounceMs))

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
	wsPath := "/typing"
	if spec.Streaming != nil && spec.Streaming.Typing != nil && spec.Streaming.Typing.Path != "" {
		wsPath = spec.Streaming.Typing.Path
	}

	buf.WriteString(fmt.Sprintf("      let wsURL = this.config.baseURL.replace(/^http/, 'ws') + '%s';\n\n", wsPath))

	buf.WriteString("      // Add auth to URL for browser compatibility\n")
	buf.WriteString("      if (this.config.auth?.bearerToken) {\n")
	buf.WriteString("        const separator = wsURL.includes('?') ? '&' : '?';\n")
	buf.WriteString("        wsURL += `${separator}token=${encodeURIComponent(this.config.auth.bearerToken)}`;\n")
	buf.WriteString("      }\n\n")

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

	buf.WriteString("  private clearConnectionTimeout(): void {\n")
	buf.WriteString("    if (this.connectionTimeoutId) {\n")
	buf.WriteString("      clearTimeout(this.connectionTimeoutId);\n")
	buf.WriteString("      this.connectionTimeoutId = null;\n")
	buf.WriteString("    }\n")
	buf.WriteString("  }\n\n")

	// disconnect method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Disconnect from the WebSocket server.\n")
	buf.WriteString("   */\n")
	buf.WriteString("  disconnect(): void {\n")
	buf.WriteString("    this.closed = true;\n")
	buf.WriteString("    this.clearConnectionTimeout();\n")

	if config.Features.Reconnection {
		buf.WriteString("    this.cancelReconnect();\n")
	}

	buf.WriteString("    // Clear all timers\n")
	buf.WriteString("    for (const timer of this.typingTimers.values()) {\n")
	buf.WriteString("      clearTimeout(timer);\n")
	buf.WriteString("    }\n")
	buf.WriteString("    for (const timer of this.debounceTimers.values()) {\n")
	buf.WriteString("      clearTimeout(timer);\n")
	buf.WriteString("    }\n")
	buf.WriteString("    this.typingTimers.clear();\n")
	buf.WriteString("    this.debounceTimers.clear();\n\n")

	buf.WriteString("    if (this.ws) {\n")
	buf.WriteString("      this.ws.close();\n")
	buf.WriteString("      this.ws = null;\n")
	buf.WriteString("    }\n")
	buf.WriteString("    this.typingUsers.clear();\n")
	buf.WriteString("    this.setState(ConnectionState.CLOSED);\n")
	buf.WriteString("  }\n\n")

	// startTyping method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Indicate that you started typing in a room.\n")
	buf.WriteString("   * This method is debounced - rapid calls will be consolidated.\n")
	buf.WriteString("   * Typing will auto-stop after the configured timeout.\n")
	buf.WriteString("   * @param roomId - The ID of the room where you are typing\n")
	buf.WriteString("   */\n")
	buf.WriteString("  startTyping(roomId: string): void {\n")
	buf.WriteString("    const OPEN = 1; // WebSocket.OPEN\n")
	buf.WriteString("    if (!this.ws || this.ws.readyState !== OPEN) {\n")
	buf.WriteString("      return; // Silently ignore if not connected\n")
	buf.WriteString("    }\n")
	buf.WriteString("\n")
	buf.WriteString("    const debounceMs = this.config.debounceMs || 300;\n")
	buf.WriteString("    const timeoutMs = this.config.timeoutMs || 3000;\n")
	buf.WriteString("\n")
	buf.WriteString("    // Clear existing debounce timer\n")
	buf.WriteString("    const existingDebounce = this.debounceTimers.get(roomId);\n")
	buf.WriteString("    if (existingDebounce) {\n")
	buf.WriteString("      clearTimeout(existingDebounce);\n")
	buf.WriteString("    }\n")
	buf.WriteString("\n")
	buf.WriteString("    // Debounce the typing indicator\n")
	buf.WriteString("    const debounceTimer = setTimeout(() => {\n")
	buf.WriteString("      this.ws?.send(JSON.stringify({\n")
	buf.WriteString("        type: 'typing',\n")
	buf.WriteString("        room_id: roomId,\n")
	buf.WriteString("        data: true,\n")
	buf.WriteString("        timestamp: new Date().toISOString(),\n")
	buf.WriteString("      }));\n")
	buf.WriteString("      this.debounceTimers.delete(roomId);\n")
	buf.WriteString("    }, debounceMs);\n")
	buf.WriteString("\n")
	buf.WriteString("    this.debounceTimers.set(roomId, debounceTimer);\n")
	buf.WriteString("\n")
	buf.WriteString("    // Clear existing auto-stop timer\n")
	buf.WriteString("    const existingTimer = this.typingTimers.get(roomId);\n")
	buf.WriteString("    if (existingTimer) {\n")
	buf.WriteString("      clearTimeout(existingTimer);\n")
	buf.WriteString("    }\n")
	buf.WriteString("\n")
	buf.WriteString("    // Set auto-stop timer\n")
	buf.WriteString("    const autoStopTimer = setTimeout(() => {\n")
	buf.WriteString("      this.stopTyping(roomId);\n")
	buf.WriteString("    }, timeoutMs);\n")
	buf.WriteString("\n")
	buf.WriteString("    this.typingTimers.set(roomId, autoStopTimer);\n")
	buf.WriteString("  }\n\n")

	// stopTyping method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Indicate that you stopped typing in a room.\n")
	buf.WriteString("   * @param roomId - The ID of the room where you stopped typing\n")
	buf.WriteString("   */\n")
	buf.WriteString("  stopTyping(roomId: string): void {\n")
	buf.WriteString("    // Clear timers\n")
	buf.WriteString("    const debounceTimer = this.debounceTimers.get(roomId);\n")
	buf.WriteString("    if (debounceTimer) {\n")
	buf.WriteString("      clearTimeout(debounceTimer);\n")
	buf.WriteString("      this.debounceTimers.delete(roomId);\n")
	buf.WriteString("    }\n")
	buf.WriteString("\n")
	buf.WriteString("    const autoStopTimer = this.typingTimers.get(roomId);\n")
	buf.WriteString("    if (autoStopTimer) {\n")
	buf.WriteString("      clearTimeout(autoStopTimer);\n")
	buf.WriteString("      this.typingTimers.delete(roomId);\n")
	buf.WriteString("    }\n")
	buf.WriteString("\n")
	buf.WriteString("    const OPEN = 1; // WebSocket.OPEN\n")
	buf.WriteString("    if (!this.ws || this.ws.readyState !== OPEN) {\n")
	buf.WriteString("      return;\n")
	buf.WriteString("    }\n\n")

	buf.WriteString("    this.ws.send(JSON.stringify({\n")
	buf.WriteString("      type: 'typing',\n")
	buf.WriteString("      room_id: roomId,\n")
	buf.WriteString("      data: false,\n")
	buf.WriteString("      timestamp: new Date().toISOString(),\n")
	buf.WriteString("    }));\n")
	buf.WriteString("  }\n\n")

	// getTypingUsers method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Get all users currently typing in a room.\n")
	buf.WriteString("   * @param roomId - The ID of the room\n")
	buf.WriteString("   * @returns Array of user IDs currently typing\n")
	buf.WriteString("   */\n")
	buf.WriteString("  getTypingUsers(roomId: string): string[] {\n")
	buf.WriteString("    const roomTyping = this.typingUsers.get(roomId);\n")
	buf.WriteString("    if (!roomTyping) {\n")
	buf.WriteString("      return [];\n")
	buf.WriteString("    }\n")
	buf.WriteString("    return Array.from(roomTyping.keys());\n")
	buf.WriteString("  }\n\n")

	// onTypingStart method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Register a handler for when a user starts typing in a room.\n")
	buf.WriteString("   * @param roomId - The ID of the room to listen for\n")
	buf.WriteString("   * @param handler - Function to call when a user starts typing\n")
	buf.WriteString("   */\n")
	buf.WriteString("  onTypingStart(roomId: string, handler: TypingHandler): void {\n")
	buf.WriteString("    if (!this.startHandlers.has(roomId)) {\n")
	buf.WriteString("      this.startHandlers.set(roomId, new Set());\n")
	buf.WriteString("    }\n")
	buf.WriteString("    this.startHandlers.get(roomId)!.add(handler);\n")
	buf.WriteString("  }\n\n")

	// offTypingStart method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Remove a typing start handler.\n")
	buf.WriteString("   * @param roomId - The ID of the room\n")
	buf.WriteString("   * @param handler - The handler to remove\n")
	buf.WriteString("   */\n")
	buf.WriteString("  offTypingStart(roomId: string, handler: TypingHandler): void {\n")
	buf.WriteString("    this.startHandlers.get(roomId)?.delete(handler);\n")
	buf.WriteString("  }\n\n")

	// onTypingStop method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Register a handler for when a user stops typing in a room.\n")
	buf.WriteString("   * @param roomId - The ID of the room to listen for\n")
	buf.WriteString("   * @param handler - Function to call when a user stops typing\n")
	buf.WriteString("   */\n")
	buf.WriteString("  onTypingStop(roomId: string, handler: TypingHandler): void {\n")
	buf.WriteString("    if (!this.stopHandlers.has(roomId)) {\n")
	buf.WriteString("      this.stopHandlers.set(roomId, new Set());\n")
	buf.WriteString("    }\n")
	buf.WriteString("    this.stopHandlers.get(roomId)!.add(handler);\n")
	buf.WriteString("  }\n\n")

	// offTypingStop method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Remove a typing stop handler.\n")
	buf.WriteString("   * @param roomId - The ID of the room\n")
	buf.WriteString("   * @param handler - The handler to remove\n")
	buf.WriteString("   */\n")
	buf.WriteString("  offTypingStop(roomId: string, handler: TypingHandler): void {\n")
	buf.WriteString("    this.stopHandlers.get(roomId)?.delete(handler);\n")
	buf.WriteString("  }\n\n")

	// getState method
	if config.Features.StateManagement {
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
	}

	// Private methods
	buf.WriteString("  // Private methods\n\n")

	// handleMessage
	buf.WriteString("  private handleMessage(message: any): void {\n")
	buf.WriteString("    if (message.type === 'typing') {\n")
	buf.WriteString("      this.handleTypingEvent(message);\n")
	buf.WriteString("    } else if (message.type === 'error') {\n")
	buf.WriteString("      this.emit('error', new Error(message.message || message.error));\n")
	buf.WriteString("    }\n")
	buf.WriteString("  }\n\n")

	// handleTypingEvent
	buf.WriteString("  private handleTypingEvent(message: any): void {\n")
	buf.WriteString("    const roomId = message.room_id;\n")
	buf.WriteString("    const userId = message.user_id;\n")
	buf.WriteString("    const isTyping = message.data === true;\n")
	buf.WriteString("\n")
	buf.WriteString("    if (!this.typingUsers.has(roomId)) {\n")
	buf.WriteString("      this.typingUsers.set(roomId, new Map());\n")
	buf.WriteString("    }\n")
	buf.WriteString("\n")
	buf.WriteString("    const roomTyping = this.typingUsers.get(roomId)!;\n")
	buf.WriteString("\n")
	buf.WriteString("    const typingUser: TypingUser = {\n")
	buf.WriteString("      userId,\n")
	buf.WriteString("      roomId,\n")
	buf.WriteString("      startedAt: message.timestamp || new Date().toISOString(),\n")
	buf.WriteString("    };\n")
	buf.WriteString("\n")
	buf.WriteString("    if (isTyping) {\n")
	buf.WriteString("      roomTyping.set(userId, typingUser);\n")
	buf.WriteString("      const handlers = this.startHandlers.get(roomId);\n")
	buf.WriteString("      if (handlers) {\n")
	buf.WriteString("        handlers.forEach(handler => handler(typingUser));\n")
	buf.WriteString("      }\n")
	buf.WriteString("      this.emit('typingStart', typingUser);\n")
	buf.WriteString("    } else {\n")
	buf.WriteString("      roomTyping.delete(userId);\n")
	buf.WriteString("      const handlers = this.stopHandlers.get(roomId);\n")
	buf.WriteString("      if (handlers) {\n")
	buf.WriteString("        handlers.forEach(handler => handler(typingUser));\n")
	buf.WriteString("      }\n")
	buf.WriteString("      this.emit('typingStop', typingUser);\n")
	buf.WriteString("    }\n")
	buf.WriteString("  }\n\n")

	// setState
	if config.Features.StateManagement {
		buf.WriteString("  private setState(state: ConnectionState): void {\n")
		buf.WriteString("    this.state = state;\n")
		buf.WriteString("    this.emit('stateChange', state);\n")
		buf.WriteString("  }\n\n")
	}

	// Reconnection logic
	if config.Features.Reconnection {
		buf.WriteString("  private scheduleReconnect(): void {\n")
		buf.WriteString("    const maxAttempts = this.config.maxReconnectAttempts || 10;\n")
		buf.WriteString("    if (this.reconnectAttempts >= maxAttempts) {\n")
		buf.WriteString("      this.setState(ConnectionState.CLOSED);\n")
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
