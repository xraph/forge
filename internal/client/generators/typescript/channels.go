package typescript

import (
	"fmt"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// ChannelsGenerator generates TypeScript channel client code.
type ChannelsGenerator struct{}

// NewChannelsGenerator creates a new channels generator.
func NewChannelsGenerator() *ChannelsGenerator {
	return &ChannelsGenerator{}
}

// Generate generates the ChannelClient TypeScript code.
func (c *ChannelsGenerator) Generate(spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	buf.WriteString(c.generatePolyfillSetup())
	buf.WriteString("\n")
	buf.WriteString(c.generateImports(config))
	buf.WriteString("\n")
	buf.WriteString(c.generateTypes(config))
	buf.WriteString("\n")
	buf.WriteString(c.generateChannelClient(spec, config))

	return buf.String()
}

// generatePolyfillSetup generates the browser/Node.js compatibility layer.
func (c *ChannelsGenerator) generatePolyfillSetup() string {
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

	// Simple EventEmitter
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

// generateImports generates import statements for the channel client.
func (c *ChannelsGenerator) generateImports(_ client.GeneratorConfig) string {
	var buf strings.Builder

	buf.WriteString("// Channel client for pub/sub messaging\n\n")
	buf.WriteString("import { ConnectionState, AuthConfig } from './types';\n\n")

	return buf.String()
}

// generateTypes generates channel-specific types.
func (c *ChannelsGenerator) generateTypes(config client.GeneratorConfig) string {
	var buf strings.Builder

	// QueuedPublish type
	buf.WriteString("/** Message queued for publishing when offline */\n")
	buf.WriteString("interface QueuedPublish {\n")
	buf.WriteString("  channelId: string;\n")
	buf.WriteString("  data: any;\n")
	buf.WriteString("  timestamp: number;\n")
	buf.WriteString("  resolve: () => void;\n")
	buf.WriteString("  reject: (error: Error) => void;\n")
	buf.WriteString("}\n\n")

	// ChannelMessage
	buf.WriteString("/**\n * Message received from a channel\n */\n")
	buf.WriteString("export interface ChannelMessage<T = any> {\n")
	buf.WriteString("  /** Channel ID */\n")
	buf.WriteString("  channelId: string;\n")
	buf.WriteString("  /** Message data */\n")
	buf.WriteString("  data: T;\n")
	buf.WriteString("  /** Sender user ID */\n")
	buf.WriteString("  userId?: string;\n")
	buf.WriteString("  /** Timestamp */\n")
	buf.WriteString("  timestamp: string;\n")
	buf.WriteString("  /** Optional message ID */\n")
	buf.WriteString("  messageId?: string;\n")
	buf.WriteString("}\n\n")

	// ChannelMessageHandler
	buf.WriteString("/**\n * Handler for channel messages\n */\n")
	buf.WriteString("export type ChannelMessageHandler<T = any> = (message: ChannelMessage<T>) => void;\n\n")

	// SubscribeOptions
	buf.WriteString("/**\n * Options for subscribing to a channel\n */\n")
	buf.WriteString("export interface SubscribeOptions {\n")
	buf.WriteString("  /** Filter messages by specific criteria */\n")
	buf.WriteString("  filter?: Record<string, any>;\n")
	buf.WriteString("  /** Start from a specific message ID */\n")
	buf.WriteString("  fromMessageId?: string;\n")
	buf.WriteString("  /** Start from a specific timestamp */\n")
	buf.WriteString("  fromTimestamp?: string;\n")
	buf.WriteString("}\n\n")

	// ChannelClientConfig
	buf.WriteString("/**\n * Configuration for the channel client\n */\n")
	buf.WriteString("export interface ChannelClientConfig {\n")
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

	buf.WriteString("  /** Enable offline message queue (default: true) */\n")
	buf.WriteString("  enableOfflineQueue?: boolean;\n")
	buf.WriteString("  /** Maximum messages in offline queue (default: 1000) */\n")
	buf.WriteString("  maxQueueSize?: number;\n")
	buf.WriteString("  /** Message TTL in queue in ms (default: 300000 - 5 min) */\n")
	buf.WriteString("  queueMessageTTL?: number;\n")
	buf.WriteString("  /** Maximum channels per user (default: 100) */\n")
	buf.WriteString("  maxChannelsPerUser?: number;\n")
	buf.WriteString("}\n\n")

	return buf.String()
}

// generateChannelClient generates the main ChannelClient class.
func (c *ChannelsGenerator) generateChannelClient(spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	// Class documentation
	buf.WriteString("/**\n")
	buf.WriteString(" * ChannelClient manages pub/sub channel subscriptions.\n")
	buf.WriteString(" * \n")
	buf.WriteString(" * Features:\n")
	buf.WriteString(" * - Cross-platform (Browser & Node.js)\n")
	buf.WriteString(" * - Connection timeouts\n")
	buf.WriteString(" * - Offline message queue\n")
	buf.WriteString(" * - Subscribe/unsubscribe to channels\n")
	buf.WriteString(" * - Publish messages to channels\n")
	buf.WriteString(" * - Receive real-time messages from subscribed channels\n")
	buf.WriteString(" * - Filter messages with custom criteria\n")
	if config.Features.Reconnection {
		buf.WriteString(" * - Automatic reconnection with exponential backoff\n")
	}
	buf.WriteString(" * \n")
	buf.WriteString(" * @example\n")
	buf.WriteString(" * ```typescript\n")
	buf.WriteString(" * const channels = new ChannelClient({ baseURL: 'ws://localhost:8080' });\n")
	buf.WriteString(" * await channels.connect();\n")
	buf.WriteString(" * \n")
	buf.WriteString(" * channels.subscribe('notifications');\n")
	buf.WriteString(" * channels.onMessage('notifications', (msg) => {\n")
	buf.WriteString(" *   console.log('Notification:', msg.data);\n")
	buf.WriteString(" * });\n")
	buf.WriteString(" * \n")
	buf.WriteString(" * await channels.publish('updates', { type: 'status', value: 'active' });\n")
	buf.WriteString(" * ```\n")
	buf.WriteString(" */\n")

	// Class declaration
	buf.WriteString("export class ChannelClient extends EventEmitter {\n")

	// Private fields
	buf.WriteString("  private ws: WebSocket | null = null;\n")
	buf.WriteString("  private config: Required<Pick<ChannelClientConfig, 'baseURL'>> & ChannelClientConfig;\n")
	buf.WriteString("  private state: ConnectionState = ConnectionState.DISCONNECTED;\n")
	buf.WriteString("  private closed: boolean = false;\n")
	buf.WriteString("  private connectionTimeoutId: ReturnType<typeof setTimeout> | null = null;\n")
	buf.WriteString("  private subscriptions: Map<string, SubscribeOptions | undefined> = new Map();\n")
	buf.WriteString("  private messageHandlers: Map<string, Set<ChannelMessageHandler>> = new Map();\n")
	buf.WriteString("  private publishQueue: QueuedPublish[] = [];\n")

	if config.Features.Reconnection {
		buf.WriteString("  private reconnectAttempts: number = 0;\n")
		buf.WriteString("  private reconnectTimeoutId: ReturnType<typeof setTimeout> | null = null;\n")
	}

	buf.WriteString("\n")

	// Get maxChannelsPerUser from config
	maxChannels := config.Streaming.ChannelConfig.MaxChannelsPerUser
	if maxChannels == 0 {
		maxChannels = 100
	}

	// Constructor
	buf.WriteString("  constructor(config: ChannelClientConfig) {\n")
	buf.WriteString("    super();\n")
	buf.WriteString("    this.config = {\n")
	buf.WriteString("      connectionTimeout: 30000,\n")
	buf.WriteString(fmt.Sprintf("      maxChannelsPerUser: %d,\n", maxChannels))
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
	wsPath := "/channels"
	if spec.Streaming != nil && spec.Streaming.Channels != nil && spec.Streaming.Channels.Path != "" {
		wsPath = spec.Streaming.Channels.Path
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

	buf.WriteString("    if (this.ws) {\n")
	buf.WriteString("      this.ws.close();\n")
	buf.WriteString("      this.ws = null;\n")
	buf.WriteString("    }\n")
	buf.WriteString("    this.subscriptions.clear();\n")
	buf.WriteString("    this.messageHandlers.clear();\n")
	buf.WriteString("    this.setState(ConnectionState.CLOSED);\n")
	buf.WriteString("  }\n\n")

	// subscribe method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Subscribe to a channel.\n")
	buf.WriteString("   * @param channelId - The ID of the channel to subscribe to\n")
	buf.WriteString("   * @param options - Optional subscription options\n")
	buf.WriteString("   */\n")
	buf.WriteString("  subscribe(channelId: string, options?: SubscribeOptions): void {\n")
	buf.WriteString("    const OPEN = 1; // WebSocket.OPEN\n")
	buf.WriteString("    if (!this.ws || this.ws.readyState !== OPEN) {\n")
	buf.WriteString("      throw new Error('WebSocket is not connected');\n")
	buf.WriteString("    }\n\n")

	buf.WriteString("    const maxChannels = this.config.maxChannelsPerUser || 100;\n")
	buf.WriteString("    if (this.subscriptions.size >= maxChannels) {\n")
	buf.WriteString("      throw new Error('Maximum channels per user limit reached');\n")
	buf.WriteString("    }\n\n")

	buf.WriteString("    this.subscriptions.set(channelId, options);\n\n")

	buf.WriteString("    this.ws.send(JSON.stringify({\n")
	buf.WriteString("      action: 'subscribe',\n")
	buf.WriteString("      channel_id: channelId,\n")
	buf.WriteString("      ...options,\n")
	buf.WriteString("    }));\n")
	buf.WriteString("  }\n\n")

	// unsubscribe method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Unsubscribe from a channel.\n")
	buf.WriteString("   * @param channelId - The ID of the channel to unsubscribe from\n")
	buf.WriteString("   */\n")
	buf.WriteString("  unsubscribe(channelId: string): void {\n")
	buf.WriteString("    const OPEN = 1; // WebSocket.OPEN\n")
	buf.WriteString("    if (this.ws && this.ws.readyState === OPEN) {\n")
	buf.WriteString("      this.ws.send(JSON.stringify({\n")
	buf.WriteString("        action: 'unsubscribe',\n")
	buf.WriteString("        channel_id: channelId,\n")
	buf.WriteString("      }));\n")
	buf.WriteString("    }\n\n")

	buf.WriteString("    this.subscriptions.delete(channelId);\n")
	buf.WriteString("    this.messageHandlers.delete(channelId);\n")
	buf.WriteString("  }\n\n")

	// publish method with offline queue
	buf.WriteString("  /**\n")
	buf.WriteString("   * Publish a message to a channel. If offline, queues for later.\n")
	buf.WriteString("   * @param channelId - The ID of the channel to publish to\n")
	buf.WriteString("   * @param data - The data to publish\n")
	buf.WriteString("   * @returns Promise that resolves when published (or queued)\n")
	buf.WriteString("   */\n")
	buf.WriteString("  async publish<T = any>(channelId: string, data: T): Promise<void> {\n")
	buf.WriteString("    const OPEN = 1; // WebSocket.OPEN\n")
	buf.WriteString("    if (this.ws && this.ws.readyState === OPEN) {\n")
	buf.WriteString("      this.ws.send(JSON.stringify({\n")
	buf.WriteString("        action: 'publish',\n")
	buf.WriteString("        channel_id: channelId,\n")
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
	buf.WriteString("      if (this.publishQueue.length >= (this.config.maxQueueSize || 1000)) {\n")
	buf.WriteString("        reject(new Error('Message queue full'));\n")
	buf.WriteString("        return;\n")
	buf.WriteString("      }\n\n")

	buf.WriteString("      this.publishQueue.push({\n")
	buf.WriteString("        channelId,\n")
	buf.WriteString("        data,\n")
	buf.WriteString("        timestamp: Date.now(),\n")
	buf.WriteString("        resolve,\n")
	buf.WriteString("        reject,\n")
	buf.WriteString("      });\n")
	buf.WriteString("    });\n")
	buf.WriteString("  }\n\n")

	// Queue management
	buf.WriteString("  /**\n")
	buf.WriteString("   * Get the number of messages in the offline queue.\n")
	buf.WriteString("   */\n")
	buf.WriteString("  getQueueSize(): number {\n")
	buf.WriteString("    return this.publishQueue.length;\n")
	buf.WriteString("  }\n\n")

	buf.WriteString("  /**\n")
	buf.WriteString("   * Clear all messages from the offline queue.\n")
	buf.WriteString("   * @param rejectPending - If true, reject pending promises (default: false)\n")
	buf.WriteString("   */\n")
	buf.WriteString("  clearQueue(rejectPending: boolean = false): void {\n")
	buf.WriteString("    if (rejectPending) {\n")
	buf.WriteString("      const error = new Error('Queue cleared');\n")
	buf.WriteString("      this.publishQueue.forEach(msg => msg.reject(error));\n")
	buf.WriteString("    }\n")
	buf.WriteString("    this.publishQueue = [];\n")
	buf.WriteString("  }\n\n")

	buf.WriteString("  private flushQueue(): void {\n")
	buf.WriteString("    const OPEN = 1; // WebSocket.OPEN\n")
	buf.WriteString("    if (!this.ws || this.ws.readyState !== OPEN) return;\n\n")

	buf.WriteString("    const now = Date.now();\n")
	buf.WriteString("    const ttl = this.config.queueMessageTTL || 300000;\n\n")

	buf.WriteString("    while (this.publishQueue.length > 0) {\n")
	buf.WriteString("      const msg = this.publishQueue[0];\n")
	buf.WriteString("      \n")
	buf.WriteString("      if (now - msg.timestamp > ttl) {\n")
	buf.WriteString("        this.publishQueue.shift();\n")
	buf.WriteString("        msg.reject(new Error('Message expired in queue'));\n")
	buf.WriteString("        continue;\n")
	buf.WriteString("      }\n\n")

	buf.WriteString("      try {\n")
	buf.WriteString("        this.ws.send(JSON.stringify({\n")
	buf.WriteString("          action: 'publish',\n")
	buf.WriteString("          channel_id: msg.channelId,\n")
	buf.WriteString("          data: msg.data,\n")
	buf.WriteString("          timestamp: new Date().toISOString(),\n")
	buf.WriteString("        }));\n")
	buf.WriteString("        this.publishQueue.shift();\n")
	buf.WriteString("        msg.resolve();\n")
	buf.WriteString("      } catch (error) {\n")
	buf.WriteString("        break;\n")
	buf.WriteString("      }\n")
	buf.WriteString("    }\n")
	buf.WriteString("  }\n\n")

	buf.WriteString("  private rejectAllQueuedMessages(error: Error): void {\n")
	buf.WriteString("    this.publishQueue.forEach(msg => msg.reject(error));\n")
	buf.WriteString("    this.publishQueue = [];\n")
	buf.WriteString("  }\n\n")

	// getSubscribedChannels method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Get all subscribed channel IDs.\n")
	buf.WriteString("   * @returns Array of subscribed channel IDs\n")
	buf.WriteString("   */\n")
	buf.WriteString("  getSubscribedChannels(): string[] {\n")
	buf.WriteString("    return Array.from(this.subscriptions.keys());\n")
	buf.WriteString("  }\n\n")

	// isSubscribed method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Check if subscribed to a channel.\n")
	buf.WriteString("   * @param channelId - The channel ID to check\n")
	buf.WriteString("   * @returns True if subscribed\n")
	buf.WriteString("   */\n")
	buf.WriteString("  isSubscribed(channelId: string): boolean {\n")
	buf.WriteString("    return this.subscriptions.has(channelId);\n")
	buf.WriteString("  }\n\n")

	// onMessage method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Register a handler for messages on a specific channel.\n")
	buf.WriteString("   * @param channelId - The ID of the channel to listen to\n")
	buf.WriteString("   * @param handler - Function to call when a message is received\n")
	buf.WriteString("   */\n")
	buf.WriteString("  onMessage<T = any>(channelId: string, handler: ChannelMessageHandler<T>): void {\n")
	buf.WriteString("    if (!this.messageHandlers.has(channelId)) {\n")
	buf.WriteString("      this.messageHandlers.set(channelId, new Set());\n")
	buf.WriteString("    }\n")
	buf.WriteString("    this.messageHandlers.get(channelId)!.add(handler as ChannelMessageHandler);\n")
	buf.WriteString("  }\n\n")

	// offMessage method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Remove a message handler for a channel.\n")
	buf.WriteString("   * @param channelId - The ID of the channel\n")
	buf.WriteString("   * @param handler - The handler to remove\n")
	buf.WriteString("   */\n")
	buf.WriteString("  offMessage<T = any>(channelId: string, handler: ChannelMessageHandler<T>): void {\n")
	buf.WriteString("    this.messageHandlers.get(channelId)?.delete(handler as ChannelMessageHandler);\n")
	buf.WriteString("  }\n\n")

	// onAnyMessage method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Register a handler for messages on any channel.\n")
	buf.WriteString("   * @param handler - Function to call when a message is received\n")
	buf.WriteString("   */\n")
	buf.WriteString("  onAnyMessage<T = any>(handler: ChannelMessageHandler<T>): void {\n")
	buf.WriteString("    this.on('message', handler);\n")
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
	buf.WriteString("    if (message.type === 'message' || message.action === 'message') {\n")
	buf.WriteString("      this.handleChannelMessage(message);\n")
	buf.WriteString("    } else if (message.type === 'error') {\n")
	buf.WriteString("      this.emit('error', new Error(message.message || message.error));\n")
	buf.WriteString("    } else if (message.type === 'subscribed') {\n")
	buf.WriteString("      this.emit('subscribed', message.channel_id);\n")
	buf.WriteString("    } else if (message.type === 'unsubscribed') {\n")
	buf.WriteString("      this.emit('unsubscribed', message.channel_id);\n")
	buf.WriteString("    }\n")
	buf.WriteString("  }\n\n")

	// handleChannelMessage
	buf.WriteString("  private handleChannelMessage(message: any): void {\n")
	buf.WriteString("    const channelMessage: ChannelMessage = {\n")
	buf.WriteString("      channelId: message.channel_id,\n")
	buf.WriteString("      data: message.data,\n")
	buf.WriteString("      userId: message.user_id,\n")
	buf.WriteString("      timestamp: message.timestamp || new Date().toISOString(),\n")
	buf.WriteString("      messageId: message.message_id || message.id,\n")
	buf.WriteString("    };\n\n")

	buf.WriteString("    // Notify channel-specific handlers\n")
	buf.WriteString("    const handlers = this.messageHandlers.get(channelMessage.channelId);\n")
	buf.WriteString("    if (handlers) {\n")
	buf.WriteString("      handlers.forEach(handler => {\n")
	buf.WriteString("        try {\n")
	buf.WriteString("          handler(channelMessage);\n")
	buf.WriteString("        } catch (error) {\n")
	buf.WriteString("          console.error('Channel message handler error:', error);\n")
	buf.WriteString("        }\n")
	buf.WriteString("      });\n")
	buf.WriteString("    }\n\n")

	buf.WriteString("    // Notify global handlers\n")
	buf.WriteString("    this.emit('message', channelMessage);\n")
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
		buf.WriteString("        // Resubscribe to channels after reconnection\n")
		buf.WriteString("        for (const [channelId, options] of this.subscriptions.entries()) {\n")
		buf.WriteString("          try {\n")
		buf.WriteString("            this.subscribe(channelId, options);\n")
		buf.WriteString("          } catch (error) {\n")
		buf.WriteString("            console.error(`Failed to resubscribe to channel ${channelId}:`, error);\n")
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
