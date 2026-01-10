package typescript

import (
	"fmt"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// WebSocketGenerator generates TypeScript WebSocket client code.
type WebSocketGenerator struct{}

// NewWebSocketGenerator creates a new WebSocket generator.
func NewWebSocketGenerator() *WebSocketGenerator {
	return &WebSocketGenerator{}
}

// Generate generates the WebSocket clients.
func (w *WebSocketGenerator) Generate(spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	// Generate environment detection and polyfill setup
	buf.WriteString(w.generatePolyfillSetup())
	buf.WriteString("\n")

	// Generate base types
	buf.WriteString(w.generateBaseTypes(config))
	buf.WriteString("\n")

	buf.WriteString("import * as types from './types';\n")
	buf.WriteString("import { ConnectionState } from './types';\n\n")

	// Generate client for each WebSocket endpoint
	for _, ws := range spec.WebSockets {
		clientCode := w.generateWebSocketClient(ws, spec, config)
		buf.WriteString(clientCode)
		buf.WriteString("\n")
	}

	return buf.String()
}

// generatePolyfillSetup generates the browser/Node.js compatibility layer.
func (w *WebSocketGenerator) generatePolyfillSetup() string {
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
	buf.WriteString("    // Node.js - use dynamic require for compatibility\n")
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

// generateBaseTypes generates base types for WebSocket clients.
func (w *WebSocketGenerator) generateBaseTypes(config client.GeneratorConfig) string {
	var buf strings.Builder

	// QueuedMessage type
	buf.WriteString("\n/** Message queued for sending when offline */\n")
	buf.WriteString("interface QueuedMessage {\n")
	buf.WriteString("  data: any;\n")
	buf.WriteString("  timestamp: number;\n")
	buf.WriteString("  resolve: () => void;\n")
	buf.WriteString("  reject: (error: Error) => void;\n")
	buf.WriteString("}\n\n")

	// WebSocketClientConfig type
	buf.WriteString("/** Configuration for WebSocket client */\n")
	buf.WriteString("export interface WebSocketClientConfig {\n")
	buf.WriteString("  /** Base URL for WebSocket connection */\n")
	buf.WriteString("  baseURL: string;\n")
	buf.WriteString("  /** Authentication configuration */\n")
	buf.WriteString("  auth?: types.AuthConfig;\n")
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

	if config.Features.Heartbeat {
		buf.WriteString("  /** Heartbeat interval in ms (default: 30000) */\n")
		buf.WriteString("  heartbeatInterval?: number;\n")
	}

	buf.WriteString("}\n")

	return buf.String()
}

// generateWebSocketClient generates a WebSocket client for an endpoint.
func (w *WebSocketGenerator) generateWebSocketClient(ws client.WebSocketEndpoint, spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	className := w.generateClassName(ws)
	sendType := w.getSchemaTypeName(ws.SendSchema, spec)
	receiveType := w.getSchemaTypeName(ws.ReceiveSchema, spec)

	// Class definition
	buf.WriteString(fmt.Sprintf("/**\n * %s\n", className))

	if ws.Description != "" {
		buf.WriteString(fmt.Sprintf(" * %s\n", ws.Description))
	}

	buf.WriteString(" * \n")
	buf.WriteString(" * Features:\n")
	buf.WriteString(" * - Cross-platform (Browser & Node.js)\n")
	buf.WriteString(" * - Connection timeouts\n")
	buf.WriteString(" * - Offline message queue\n")

	if config.Features.Reconnection {
		buf.WriteString(" * - Automatic reconnection with exponential backoff\n")
	}

	if config.Features.Heartbeat {
		buf.WriteString(" * - Heartbeat/ping support\n")
	}

	buf.WriteString(" */\n")

	buf.WriteString(fmt.Sprintf("export class %s extends EventEmitter {\n", className))

	// Private fields
	buf.WriteString("  private ws: WebSocket | null = null;\n")
	buf.WriteString("  private config: Required<Pick<WebSocketClientConfig, 'baseURL'>> & WebSocketClientConfig;\n")
	buf.WriteString("  private state: ConnectionState = ConnectionState.DISCONNECTED;\n")
	buf.WriteString("  private closed: boolean = false;\n")
	buf.WriteString("  private connectionTimeoutId: ReturnType<typeof setTimeout> | null = null;\n")
	buf.WriteString("  private messageQueue: QueuedMessage[] = [];\n")

	if config.Features.Reconnection {
		buf.WriteString("  private reconnectAttempts: number = 0;\n")
		buf.WriteString("  private reconnectTimeoutId: ReturnType<typeof setTimeout> | null = null;\n")
	}

	if config.Features.Heartbeat {
		buf.WriteString("  private heartbeatIntervalId: ReturnType<typeof setInterval> | null = null;\n")
	}

	buf.WriteString("\n")

	// Constructor
	buf.WriteString("  constructor(config: WebSocketClientConfig) {\n")
	buf.WriteString("    super();\n")
	buf.WriteString("    this.config = {\n")
	buf.WriteString("      connectionTimeout: 30000,\n")
	buf.WriteString("      requestTimeout: 10000,\n")

	if config.Features.Reconnection {
		buf.WriteString("      maxReconnectAttempts: 10,\n")
		buf.WriteString("      reconnectDelay: 1000,\n")
		buf.WriteString("      maxReconnectDelay: 30000,\n")
	}

	buf.WriteString("      enableOfflineQueue: true,\n")
	buf.WriteString("      maxQueueSize: 1000,\n")
	buf.WriteString("      queueMessageTTL: 300000,\n")

	if config.Features.Heartbeat {
		buf.WriteString("      heartbeatInterval: 30000,\n")
	}

	buf.WriteString("      ...config,\n")
	buf.WriteString("    };\n")
	buf.WriteString("  }\n\n")

	// Connect method with timeout
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

	buf.WriteString(fmt.Sprintf("      const wsURL = this.config.baseURL.replace(/^http/, 'ws') + '%s';\n\n", ws.Path))

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
	buf.WriteString("        const WS = getWebSocket();\n")
	buf.WriteString("        \n")
	buf.WriteString("        // Build URL with auth if needed\n")
	buf.WriteString("        let url = wsURL;\n")
	buf.WriteString("        if (this.config.auth?.bearerToken) {\n")
	buf.WriteString("          const separator = url.includes('?') ? '&' : '?';\n")
	buf.WriteString("          url += `${separator}token=${encodeURIComponent(this.config.auth.bearerToken)}`;\n")
	buf.WriteString("        }\n\n")

	// Node.js WebSocket supports headers, browser doesn't
	buf.WriteString("        if (isBrowser) {\n")
	buf.WriteString("          this.ws = new WS(url);\n")
	buf.WriteString("        } else {\n")
	buf.WriteString("          // Node.js: can pass headers\n")
	buf.WriteString("          const headers: Record<string, string> = {};\n")
	buf.WriteString("          if (this.config.auth?.bearerToken) {\n")
	buf.WriteString("            headers['Authorization'] = `Bearer ${this.config.auth.bearerToken}`;\n")
	buf.WriteString("          }\n")
	buf.WriteString("          if (this.config.auth?.apiKey) {\n")
	buf.WriteString("            headers['X-API-Key'] = this.config.auth.apiKey;\n")
	buf.WriteString("          }\n")
	buf.WriteString("          this.ws = new (WS as any)(url, { headers });\n")
	buf.WriteString("        }\n\n")

	// Use standard event handlers that work in both browser and Node.js
	buf.WriteString("        // Standard WebSocket event handlers (works in browser and Node.js)\n")
	buf.WriteString("        this.ws.onopen = () => {\n")
	buf.WriteString("          this.clearConnectionTimeout();\n")
	buf.WriteString("          this.setState(ConnectionState.CONNECTED);\n")

	if config.Features.Reconnection {
		buf.WriteString("          this.reconnectAttempts = 0;\n")
	}

	if config.Features.Heartbeat {
		buf.WriteString("          this.startHeartbeat();\n")
	}

	buf.WriteString("          this.flushQueue();\n")
	buf.WriteString("          resolve();\n")
	buf.WriteString("        };\n\n")

	buf.WriteString("        this.ws.onmessage = (event: MessageEvent) => {\n")
	buf.WriteString("          try {\n")
	buf.WriteString("            const data = typeof event.data === 'string' ? event.data : event.data.toString();\n")
	buf.WriteString(fmt.Sprintf("            const message: %s = JSON.parse(data);\n", receiveType))
	buf.WriteString("            this.emit('message', message);\n")
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

	if config.Features.Heartbeat {
		buf.WriteString("          this.stopHeartbeat();\n")
	}

	buf.WriteString("          \n")
	buf.WriteString("          if (this.closed) {\n")
	buf.WriteString("            this.setState(ConnectionState.CLOSED);\n")
	buf.WriteString("          } else {\n")
	buf.WriteString("            this.setState(ConnectionState.DISCONNECTED);\n")

	if config.Features.Reconnection {
		buf.WriteString("            this.scheduleReconnect();\n")
	}

	buf.WriteString("          }\n")
	buf.WriteString("          \n")
	buf.WriteString("          this.emit('close', event);\n")
	buf.WriteString("        };\n")

	buf.WriteString("      } catch (error) {\n")
	buf.WriteString("        this.clearConnectionTimeout();\n")
	buf.WriteString("        this.setState(ConnectionState.ERROR);\n")
	buf.WriteString("        reject(error);\n")
	buf.WriteString("      }\n")
	buf.WriteString("    });\n")
	buf.WriteString("  }\n\n")

	// Send method with offline queue
	buf.WriteString("  /**\n")
	buf.WriteString("   * Send a message. If offline, queues the message for later.\n")
	buf.WriteString("   * @param message - The message to send\n")
	buf.WriteString("   * @returns Promise that resolves when sent (or queued)\n")
	buf.WriteString("   */\n")
	buf.WriteString(fmt.Sprintf("  async send(message: %s): Promise<void> {\n", sendType))
	buf.WriteString("    const OPEN = 1; // WebSocket.OPEN\n")
	buf.WriteString("    \n")
	buf.WriteString("    if (this.ws && this.ws.readyState === OPEN) {\n")
	buf.WriteString("      this.ws.send(JSON.stringify(message));\n")
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
	buf.WriteString("        data: message,\n")
	buf.WriteString("        timestamp: Date.now(),\n")
	buf.WriteString("        resolve,\n")
	buf.WriteString("        reject,\n")
	buf.WriteString("      });\n")
	buf.WriteString("    });\n")
	buf.WriteString("  }\n\n")

	// Synchronous send without queuing
	buf.WriteString("  /**\n")
	buf.WriteString("   * Send a message immediately. Throws if not connected.\n")
	buf.WriteString("   * @param message - The message to send\n")
	buf.WriteString("   */\n")
	buf.WriteString(fmt.Sprintf("  sendSync(message: %s): void {\n", sendType))
	buf.WriteString("    const OPEN = 1; // WebSocket.OPEN\n")
	buf.WriteString("    if (!this.ws || this.ws.readyState !== OPEN) {\n")
	buf.WriteString("      throw new Error('WebSocket is not connected');\n")
	buf.WriteString("    }\n")
	buf.WriteString("    this.ws.send(JSON.stringify(message));\n")
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

	// Flush queue
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

	buf.WriteString("      try {\n")
	buf.WriteString("        this.ws.send(JSON.stringify(msg.data));\n")
	buf.WriteString("        this.messageQueue.shift();\n")
	buf.WriteString("        msg.resolve();\n")
	buf.WriteString("      } catch (error) {\n")
	buf.WriteString("        // Stop flushing on error, will retry on next connection\n")
	buf.WriteString("        break;\n")
	buf.WriteString("      }\n")
	buf.WriteString("    }\n")
	buf.WriteString("  }\n\n")

	// OnMessage convenience method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Register a handler for incoming messages.\n")
	buf.WriteString("   * @param handler - Function to call when a message is received\n")
	buf.WriteString("   */\n")
	buf.WriteString(fmt.Sprintf("  onMessage(handler: (message: %s) => void): void {\n", receiveType))
	buf.WriteString("    this.on('message', handler);\n")
	buf.WriteString("  }\n\n")

	// OffMessage method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Remove a message handler.\n")
	buf.WriteString("   * @param handler - The handler to remove\n")
	buf.WriteString("   */\n")
	buf.WriteString(fmt.Sprintf("  offMessage(handler: (message: %s) => void): void {\n", receiveType))
	buf.WriteString("    this.off('message', handler);\n")
	buf.WriteString("  }\n\n")

	// OnError convenience method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Register a handler for errors.\n")
	buf.WriteString("   * @param handler - Function to call when an error occurs\n")
	buf.WriteString("   */\n")
	buf.WriteString("  onError(handler: (error: Error) => void): void {\n")
	buf.WriteString("    this.on('error', handler);\n")
	buf.WriteString("  }\n\n")

	// OnClose method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Register a handler for connection close.\n")
	buf.WriteString("   * @param handler - Function to call when connection closes\n")
	buf.WriteString("   */\n")
	buf.WriteString("  onClose(handler: (event: CloseEvent) => void): void {\n")
	buf.WriteString("    this.on('close', handler);\n")
	buf.WriteString("  }\n\n")

	// State management
	if config.Features.StateManagement {
		buf.WriteString("  /**\n")
		buf.WriteString("   * Register a handler for state changes.\n")
		buf.WriteString("   * @param handler - Function to call when state changes\n")
		buf.WriteString("   */\n")
		buf.WriteString("  onStateChange(handler: (state: ConnectionState) => void): void {\n")
		buf.WriteString("    this.on('stateChange', handler);\n")
		buf.WriteString("  }\n\n")

		buf.WriteString("  /**\n")
		buf.WriteString("   * Get the current connection state.\n")
		buf.WriteString("   */\n")
		buf.WriteString("  getState(): ConnectionState {\n")
		buf.WriteString("    return this.state;\n")
		buf.WriteString("  }\n\n")
	}

	buf.WriteString("  private setState(state: ConnectionState): void {\n")
	buf.WriteString("    if (this.state !== state) {\n")
	buf.WriteString("      this.state = state;\n")
	buf.WriteString("      this.emit('stateChange', state);\n")
	buf.WriteString("    }\n")
	buf.WriteString("  }\n\n")

	buf.WriteString("  private clearConnectionTimeout(): void {\n")
	buf.WriteString("    if (this.connectionTimeoutId) {\n")
	buf.WriteString("      clearTimeout(this.connectionTimeoutId);\n")
	buf.WriteString("      this.connectionTimeoutId = null;\n")
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
		buf.WriteString("  }\n\n")
	}

	buf.WriteString("  private rejectAllQueuedMessages(error: Error): void {\n")
	buf.WriteString("    this.messageQueue.forEach(msg => msg.reject(error));\n")
	buf.WriteString("    this.messageQueue = [];\n")
	buf.WriteString("  }\n\n")

	// Heartbeat logic
	if config.Features.Heartbeat {
		buf.WriteString("  private startHeartbeat(): void {\n")
		buf.WriteString("    this.stopHeartbeat();\n")
		buf.WriteString("    this.heartbeatIntervalId = setInterval(() => {\n")
		buf.WriteString("      const OPEN = 1; // WebSocket.OPEN\n")
		buf.WriteString("      if (this.ws && this.ws.readyState === OPEN) {\n")
		buf.WriteString("        // Send ping frame (works in Node.js, browser handles ping/pong automatically)\n")
		buf.WriteString("        if (!isBrowser && typeof (this.ws as any).ping === 'function') {\n")
		buf.WriteString("          (this.ws as any).ping();\n")
		buf.WriteString("        }\n")
		buf.WriteString("      }\n")
		buf.WriteString("    }, this.config.heartbeatInterval || 30000);\n")
		buf.WriteString("  }\n\n")

		buf.WriteString("  private stopHeartbeat(): void {\n")
		buf.WriteString("    if (this.heartbeatIntervalId) {\n")
		buf.WriteString("      clearInterval(this.heartbeatIntervalId);\n")
		buf.WriteString("      this.heartbeatIntervalId = null;\n")
		buf.WriteString("    }\n")
		buf.WriteString("  }\n\n")
	}

	// Close method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Close the WebSocket connection.\n")
	buf.WriteString("   * @param rejectQueuedMessages - If true, reject all queued messages (default: false)\n")
	buf.WriteString("   */\n")
	buf.WriteString("  close(rejectQueuedMessages: boolean = false): void {\n")
	buf.WriteString("    this.closed = true;\n")
	buf.WriteString("    this.clearConnectionTimeout();\n")

	if config.Features.Reconnection {
		buf.WriteString("    this.cancelReconnect();\n")
	}

	if config.Features.Heartbeat {
		buf.WriteString("    this.stopHeartbeat();\n")
	}

	buf.WriteString("\n")
	buf.WriteString("    if (rejectQueuedMessages) {\n")
	buf.WriteString("      this.rejectAllQueuedMessages(new Error('Connection closed'));\n")
	buf.WriteString("    }\n\n")

	buf.WriteString("    if (this.ws) {\n")
	buf.WriteString("      this.ws.close();\n")
	buf.WriteString("      this.ws = null;\n")
	buf.WriteString("    }\n")
	buf.WriteString("    this.setState(ConnectionState.CLOSED);\n")
	buf.WriteString("  }\n\n")

	// isConnected helper
	buf.WriteString("  /**\n")
	buf.WriteString("   * Check if the WebSocket is currently connected.\n")
	buf.WriteString("   */\n")
	buf.WriteString("  isConnected(): boolean {\n")
	buf.WriteString("    return this.state === ConnectionState.CONNECTED;\n")
	buf.WriteString("  }\n")

	buf.WriteString("}\n")

	return buf.String()
}

// generateClassName generates a class name for a WebSocket endpoint.
func (w *WebSocketGenerator) generateClassName(ws client.WebSocketEndpoint) string {
	if ws.ID != "" {
		return w.toPascalCase(ws.ID) + "WSClient"
	}

	return "WebSocketClient"
}

// getSchemaTypeName gets the type name for a schema.
func (w *WebSocketGenerator) getSchemaTypeName(schema *client.Schema, spec *client.APISpec) string {
	if schema == nil {
		return "any"
	}

	if schema.Ref != "" {
		parts := strings.Split(schema.Ref, "/")

		return "types." + parts[len(parts)-1]
	}

	return "any"
}

// toPascalCase converts a string to PascalCase.
func (w *WebSocketGenerator) toPascalCase(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '_' || r == '-' || r == ' '
	})

	var resultSb strings.Builder

	for _, part := range parts {
		if len(part) > 0 {
			resultSb.WriteString(strings.ToUpper(part[:1]) + strings.ToLower(part[1:]))
		}
	}

	return resultSb.String()
}
