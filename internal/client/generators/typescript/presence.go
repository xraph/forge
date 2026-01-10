package typescript

import (
	"fmt"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// PresenceGenerator generates TypeScript presence client code.
type PresenceGenerator struct{}

// NewPresenceGenerator creates a new presence generator.
func NewPresenceGenerator() *PresenceGenerator {
	return &PresenceGenerator{}
}

// Generate generates the PresenceClient TypeScript code.
func (p *PresenceGenerator) Generate(spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	buf.WriteString(p.generateImports(config))
	buf.WriteString("\n")
	buf.WriteString(p.generateTypes(spec, config))
	buf.WriteString("\n")
	buf.WriteString(p.generatePresenceClient(spec, config))

	return buf.String()
}

// generateImports generates import statements for the presence client.
func (p *PresenceGenerator) generateImports(_ client.GeneratorConfig) string {
	var buf strings.Builder

	buf.WriteString(p.generatePolyfillSetup())
	buf.WriteString("\n")
	buf.WriteString("// Presence client for tracking user online status\n\n")
	buf.WriteString("import { ConnectionState, AuthConfig, UserPresence } from './types';\n\n")

	return buf.String()
}

// generatePolyfillSetup generates the browser/Node.js compatibility layer.
func (p *PresenceGenerator) generatePolyfillSetup() string {
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

// generateTypes generates presence-specific types.
func (p *PresenceGenerator) generateTypes(spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	// Get statuses from spec or config
	statuses := config.Streaming.PresenceConfig.Statuses
	if spec.Streaming != nil && spec.Streaming.Presence != nil && len(spec.Streaming.Presence.Statuses) > 0 {
		statuses = spec.Streaming.Presence.Statuses
	}

	if len(statuses) == 0 {
		statuses = []string{"online", "away", "busy", "offline"}
	}

	// PresenceStatus enum
	buf.WriteString("/**\n * Available presence statuses\n */\n")
	buf.WriteString("export enum PresenceStatus {\n")

	for _, status := range statuses {
		buf.WriteString(fmt.Sprintf("  %s = '%s',\n", strings.ToUpper(status), status))
	}

	buf.WriteString("}\n\n")

	// PresenceHandler
	buf.WriteString("/**\n * Handler for presence updates\n */\n")
	buf.WriteString("export type PresenceHandler = (presence: UserPresence) => void;\n\n")

	// PresenceClientConfig
	buf.WriteString("/**\n * Configuration for the presence client\n */\n")
	buf.WriteString("export interface PresenceClientConfig {\n")
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

	buf.WriteString("  /** Heartbeat interval in ms (to maintain online status) */\n")
	buf.WriteString("  heartbeatIntervalMs?: number;\n")
	buf.WriteString("  /** Idle timeout in ms before auto-away */\n")
	buf.WriteString("  idleTimeoutMs?: number;\n")
	buf.WriteString("}\n\n")

	// PresenceUpdate
	buf.WriteString("/**\n * Presence update payload\n */\n")
	buf.WriteString("export interface PresenceUpdate {\n")
	buf.WriteString("  /** User ID */\n")
	buf.WriteString("  userId: string;\n")
	buf.WriteString("  /** New status */\n")
	buf.WriteString("  status: PresenceStatus;\n")
	buf.WriteString("  /** Custom status message */\n")
	buf.WriteString("  customMessage?: string;\n")
	buf.WriteString("  /** Timestamp of the update */\n")
	buf.WriteString("  timestamp: string;\n")
	buf.WriteString("}\n\n")

	return buf.String()
}

// generatePresenceClient generates the main PresenceClient class.
func (p *PresenceGenerator) generatePresenceClient(spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	// Class documentation
	buf.WriteString("/**\n")
	buf.WriteString(" * PresenceClient manages real-time user presence tracking.\n")
	buf.WriteString(" * \n")
	buf.WriteString(" * Features:\n")
	buf.WriteString(" * - Set your own presence status\n")
	buf.WriteString(" * - Subscribe to other users' presence updates\n")
	buf.WriteString(" * - Get list of online users\n")
	buf.WriteString(" * - Automatic heartbeat to maintain online status\n")
	buf.WriteString(" * \n")
	buf.WriteString(" * @example\n")
	buf.WriteString(" * ```typescript\n")
	buf.WriteString(" * const presence = new PresenceClient({ baseURL: 'ws://localhost:8080' });\n")
	buf.WriteString(" * await presence.connect();\n")
	buf.WriteString(" * await presence.setStatus(PresenceStatus.ONLINE, 'Working on a project');\n")
	buf.WriteString(" * presence.subscribe(['user-1', 'user-2']);\n")
	buf.WriteString(" * presence.onPresenceChange((update) => console.log(update));\n")
	buf.WriteString(" * ```\n")
	buf.WriteString(" */\n")

	// Class declaration
	buf.WriteString("export class PresenceClient extends EventEmitter {\n")

	// Private fields
	buf.WriteString("  private ws: WebSocket | null = null;\n")
	buf.WriteString("  private config: Required<Pick<PresenceClientConfig, 'baseURL'>> & PresenceClientConfig;\n")
	buf.WriteString("  private state: ConnectionState = ConnectionState.DISCONNECTED;\n")
	buf.WriteString("  private closed: boolean = false;\n")
	buf.WriteString("  private connectionTimeoutId: ReturnType<typeof setTimeout> | null = null;\n")
	buf.WriteString("  private currentStatus: PresenceStatus = PresenceStatus.OFFLINE;\n")
	buf.WriteString("  private currentCustomMessage: string = '';\n")
	buf.WriteString("  private subscribedUsers: Set<string> = new Set();\n")
	buf.WriteString("  private presenceCache: Map<string, UserPresence> = new Map();\n")
	buf.WriteString("  private heartbeatTimer: ReturnType<typeof setInterval> | null = null;\n")
	buf.WriteString("  private idleTimer: ReturnType<typeof setTimeout> | null = null;\n")

	if config.Features.Reconnection {
		buf.WriteString("  private reconnectAttempts: number = 0;\n")
		buf.WriteString("  private reconnectTimeoutId: ReturnType<typeof setTimeout> | null = null;\n")
	}

	buf.WriteString("\n")

	// Constructor
	buf.WriteString("  constructor(config: PresenceClientConfig) {\n")
	buf.WriteString("    super();\n")
	buf.WriteString("    this.config = {\n")
	buf.WriteString("      connectionTimeout: 30000,\n")
	buf.WriteString(fmt.Sprintf("      heartbeatIntervalMs: %d,\n", config.Streaming.PresenceConfig.HeartbeatIntervalMs))
	buf.WriteString(fmt.Sprintf("      idleTimeoutMs: %d,\n", config.Streaming.PresenceConfig.IdleTimeoutMs))

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
	wsPath := "/presence"
	if spec.Streaming != nil && spec.Streaming.Presence != nil && spec.Streaming.Presence.Path != "" {
		wsPath = spec.Streaming.Presence.Path
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

	buf.WriteString("          this.startHeartbeat();\n")
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
	buf.WriteString("          this.stopHeartbeat();\n")
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

	buf.WriteString("    this.stopHeartbeat();\n")
	buf.WriteString("    if (this.ws) {\n")
	buf.WriteString("      this.ws.close();\n")
	buf.WriteString("      this.ws = null;\n")
	buf.WriteString("    }\n")
	buf.WriteString("    this.subscribedUsers.clear();\n")
	buf.WriteString("    this.presenceCache.clear();\n")
	buf.WriteString("    this.setState(ConnectionState.CLOSED);\n")
	buf.WriteString("  }\n\n")

	// setStatus method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Set your presence status.\n")
	buf.WriteString("   * @param status - The new presence status\n")
	buf.WriteString("   * @param customMessage - Optional custom status message\n")
	buf.WriteString("   * @returns Promise that resolves when status is set\n")
	buf.WriteString("   */\n")
	buf.WriteString("  async setStatus(status: PresenceStatus, customMessage?: string): Promise<void> {\n")
	buf.WriteString("    const OPEN = 1; // WebSocket.OPEN\n")
	buf.WriteString("    if (!this.ws || this.ws.readyState !== OPEN) {\n")
	buf.WriteString("      throw new Error('WebSocket is not connected');\n")
	buf.WriteString("    }\n")
	buf.WriteString("\n")
	buf.WriteString("    this.currentStatus = status;\n")
	buf.WriteString("    this.currentCustomMessage = customMessage || '';\n")
	buf.WriteString("\n")
	buf.WriteString("    this.ws.send(JSON.stringify({\n")
	buf.WriteString("      type: 'presence',\n")
	buf.WriteString("      status,\n")
	buf.WriteString("      custom_status: customMessage,\n")
	buf.WriteString("      timestamp: new Date().toISOString(),\n")
	buf.WriteString("    }));\n")
	buf.WriteString("  }\n\n")

	// getStatus method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Get the presence status of a user.\n")
	buf.WriteString("   * @param userId - The user ID to get status for\n")
	buf.WriteString("   * @returns The user's presence or undefined if not cached\n")
	buf.WriteString("   */\n")
	buf.WriteString("  getStatus(userId: string): UserPresence | undefined {\n")
	buf.WriteString("    return this.presenceCache.get(userId);\n")
	buf.WriteString("  }\n\n")

	// getCurrentStatus method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Get your current presence status.\n")
	buf.WriteString("   * @returns Your current presence status\n")
	buf.WriteString("   */\n")
	buf.WriteString("  getCurrentStatus(): { status: PresenceStatus; customMessage: string } {\n")
	buf.WriteString("    return {\n")
	buf.WriteString("      status: this.currentStatus,\n")
	buf.WriteString("      customMessage: this.currentCustomMessage,\n")
	buf.WriteString("    };\n")
	buf.WriteString("  }\n\n")

	// subscribe method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Subscribe to presence updates for specific users.\n")
	buf.WriteString("   * @param userIds - Array of user IDs to subscribe to\n")
	buf.WriteString("   */\n")
	buf.WriteString("  subscribe(userIds: string[]): void {\n")
	buf.WriteString("    const OPEN = 1; // WebSocket.OPEN\n")
	buf.WriteString("    if (!this.ws || this.ws.readyState !== OPEN) {\n")
	buf.WriteString("      throw new Error('WebSocket is not connected');\n")
	buf.WriteString("    }\n")
	buf.WriteString("\n")
	buf.WriteString("    for (const userId of userIds) {\n")
	buf.WriteString("      this.subscribedUsers.add(userId);\n")
	buf.WriteString("    }\n")
	buf.WriteString("\n")
	buf.WriteString("    this.ws.send(JSON.stringify({\n")
	buf.WriteString("      type: 'subscribe_presence',\n")
	buf.WriteString("      user_ids: userIds,\n")
	buf.WriteString("    }));\n")
	buf.WriteString("  }\n\n")

	// unsubscribe method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Unsubscribe from presence updates for specific users.\n")
	buf.WriteString("   * @param userIds - Array of user IDs to unsubscribe from\n")
	buf.WriteString("   */\n")
	buf.WriteString("  unsubscribe(userIds: string[]): void {\n")
	buf.WriteString("    const OPEN = 1; // WebSocket.OPEN\n")
	buf.WriteString("    if (!this.ws || this.ws.readyState !== OPEN) {\n")
	buf.WriteString("      throw new Error('WebSocket is not connected');\n")
	buf.WriteString("    }\n")
	buf.WriteString("\n")
	buf.WriteString("    for (const userId of userIds) {\n")
	buf.WriteString("      this.subscribedUsers.delete(userId);\n")
	buf.WriteString("      this.presenceCache.delete(userId);\n")
	buf.WriteString("    }\n")
	buf.WriteString("\n")
	buf.WriteString("    this.ws.send(JSON.stringify({\n")
	buf.WriteString("      type: 'unsubscribe_presence',\n")
	buf.WriteString("      user_ids: userIds,\n")
	buf.WriteString("    }));\n")
	buf.WriteString("  }\n\n")

	// getOnlineUsers method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Get all online users (from subscribed users).\n")
	buf.WriteString("   * @param roomId - Optional room ID to filter by\n")
	buf.WriteString("   * @returns Array of online user presences\n")
	buf.WriteString("   */\n")
	buf.WriteString("  getOnlineUsers(roomId?: string): UserPresence[] {\n")
	buf.WriteString("    const onlineUsers: UserPresence[] = [];\n")
	buf.WriteString("    for (const presence of this.presenceCache.values()) {\n")
	buf.WriteString("      if (presence.status !== 'offline') {\n")
	buf.WriteString("        if (!roomId || presence.roomId === roomId) {\n")
	buf.WriteString("          onlineUsers.push(presence);\n")
	buf.WriteString("        }\n")
	buf.WriteString("      }\n")
	buf.WriteString("    }\n")
	buf.WriteString("    return onlineUsers;\n")
	buf.WriteString("  }\n\n")

	// onPresenceChange method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Register a handler for presence changes.\n")
	buf.WriteString("   * @param handler - Function to call when presence changes\n")
	buf.WriteString("   */\n")
	buf.WriteString("  onPresenceChange(handler: PresenceHandler): void {\n")
	buf.WriteString("    this.on('presenceChange', handler);\n")
	buf.WriteString("  }\n\n")

	// offPresenceChange method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Remove a presence change handler.\n")
	buf.WriteString("   * @param handler - The handler to remove\n")
	buf.WriteString("   */\n")
	buf.WriteString("  offPresenceChange(handler: PresenceHandler): void {\n")
	buf.WriteString("    this.off('presenceChange', handler);\n")
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
	buf.WriteString("    switch (message.type) {\n")
	buf.WriteString("      case 'presence':\n")
	buf.WriteString("        this.handlePresenceUpdate(message);\n")
	buf.WriteString("        break;\n")
	buf.WriteString("      case 'presence_sync':\n")
	buf.WriteString("        this.handlePresenceSync(message);\n")
	buf.WriteString("        break;\n")
	buf.WriteString("      case 'error':\n")
	buf.WriteString("        this.emit('error', new Error(message.message || message.error));\n")
	buf.WriteString("        break;\n")
	buf.WriteString("    }\n")
	buf.WriteString("  }\n\n")

	// handlePresenceUpdate
	buf.WriteString("  private handlePresenceUpdate(message: any): void {\n")
	buf.WriteString("    const presence: UserPresence = {\n")
	buf.WriteString("      userId: message.user_id,\n")
	buf.WriteString("      status: message.status,\n")
	buf.WriteString("      customMessage: message.custom_status,\n")
	buf.WriteString("      lastSeen: message.timestamp,\n")
	buf.WriteString("      roomId: message.room_id,\n")
	buf.WriteString("    };\n")
	buf.WriteString("\n")
	buf.WriteString("    this.presenceCache.set(message.user_id, presence);\n")
	buf.WriteString("    this.emit('presenceChange', presence);\n")
	buf.WriteString("  }\n\n")

	// handlePresenceSync
	buf.WriteString("  private handlePresenceSync(message: any): void {\n")
	buf.WriteString("    if (message.presences && Array.isArray(message.presences)) {\n")
	buf.WriteString("      for (const p of message.presences) {\n")
	buf.WriteString("        const presence: UserPresence = {\n")
	buf.WriteString("          userId: p.user_id,\n")
	buf.WriteString("          status: p.status,\n")
	buf.WriteString("          customMessage: p.custom_status,\n")
	buf.WriteString("          lastSeen: p.timestamp,\n")
	buf.WriteString("          roomId: p.room_id,\n")
	buf.WriteString("        };\n")
	buf.WriteString("        this.presenceCache.set(p.user_id, presence);\n")
	buf.WriteString("      }\n")
	buf.WriteString("      this.emit('presenceSync', message.presences);\n")
	buf.WriteString("    }\n")
	buf.WriteString("  }\n\n")

	// startHeartbeat
	buf.WriteString("  private startHeartbeat(): void {\n")
	buf.WriteString("    this.stopHeartbeat();\n")
	buf.WriteString("    const interval = this.config.heartbeatIntervalMs || 30000;\n")
	buf.WriteString("    this.heartbeatTimer = setInterval(() => {\n")
	buf.WriteString("      const OPEN = 1; // WebSocket.OPEN\n")
	buf.WriteString("      if (this.ws && this.ws.readyState === OPEN) {\n")
	buf.WriteString("        this.ws.send(JSON.stringify({\n")
	buf.WriteString("          type: 'heartbeat',\n")
	buf.WriteString("          timestamp: new Date().toISOString(),\n")
	buf.WriteString("        }));\n")
	buf.WriteString("      }\n")
	buf.WriteString("    }, interval);\n")
	buf.WriteString("  }\n\n")

	// stopHeartbeat
	buf.WriteString("  private stopHeartbeat(): void {\n")
	buf.WriteString("    if (this.heartbeatTimer) {\n")
	buf.WriteString("      clearInterval(this.heartbeatTimer);\n")
	buf.WriteString("      this.heartbeatTimer = null;\n")
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
		buf.WriteString("        // Restore status after reconnection\n")
		buf.WriteString("        await this.setStatus(this.currentStatus, this.currentCustomMessage);\n")
		buf.WriteString("        // Resubscribe to users\n")
		buf.WriteString("        if (this.subscribedUsers.size > 0) {\n")
		buf.WriteString("          this.subscribe(Array.from(this.subscribedUsers));\n")
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
