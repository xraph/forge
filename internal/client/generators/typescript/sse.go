package typescript

import (
	"fmt"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// SSEGenerator generates TypeScript SSE client code.
type SSEGenerator struct{}

// NewSSEGenerator creates a new SSE generator.
func NewSSEGenerator() *SSEGenerator {
	return &SSEGenerator{}
}

// Generate generates the SSE clients.
func (s *SSEGenerator) Generate(spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	// Generate environment detection and polyfill setup
	buf.WriteString(s.generatePolyfillSetup())
	buf.WriteString("\n")

	// Generate base types
	buf.WriteString(s.generateBaseTypes(config))
	buf.WriteString("\n")

	buf.WriteString("import * as types from './types';\n")
	buf.WriteString("import { ConnectionState } from './types';\n\n")

	// Generate client for each SSE endpoint
	for _, sse := range spec.SSEs {
		clientCode := s.generateSSEClient(sse, spec, config)
		buf.WriteString(clientCode)
		buf.WriteString("\n")
	}

	return buf.String()
}

// generatePolyfillSetup generates the browser/Node.js compatibility layer.
func (s *SSEGenerator) generatePolyfillSetup() string {
	var buf strings.Builder

	buf.WriteString("// Browser/Node.js compatibility layer\n")
	buf.WriteString("const isBrowser = typeof window !== 'undefined' && typeof window.EventSource !== 'undefined';\n\n")

	buf.WriteString("// Lazy-loaded EventSource implementation\n")
	buf.WriteString("let _EventSourceImpl: typeof EventSource | null = null;\n\n")

	buf.WriteString("function getEventSource(): typeof EventSource {\n")
	buf.WriteString("  if (_EventSourceImpl) return _EventSourceImpl;\n")
	buf.WriteString("  \n")
	buf.WriteString("  if (isBrowser) {\n")
	buf.WriteString("    _EventSourceImpl = window.EventSource;\n")
	buf.WriteString("  } else {\n")
	buf.WriteString("    // Node.js - use dynamic require for compatibility\n")
	buf.WriteString("    try {\n")
	buf.WriteString("      // eslint-disable-next-line @typescript-eslint/no-var-requires\n")
	buf.WriteString("      const esModule = require('eventsource');\n")
	buf.WriteString("      _EventSourceImpl = esModule.default || esModule;\n")
	buf.WriteString("    } catch {\n")
	buf.WriteString("      throw new Error('EventSource implementation not found. Install \"eventsource\" package for Node.js.');\n")
	buf.WriteString("    }\n")
	buf.WriteString("  }\n")
	buf.WriteString("  return _EventSourceImpl!;\n")
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

// generateBaseTypes generates base types for SSE clients.
func (s *SSEGenerator) generateBaseTypes(config client.GeneratorConfig) string {
	var buf strings.Builder

	// SSEClientConfig type
	buf.WriteString("\n/** Configuration for SSE client */\n")
	buf.WriteString("export interface SSEClientConfig {\n")
	buf.WriteString("  /** Base URL for SSE connection */\n")
	buf.WriteString("  baseURL: string;\n")
	buf.WriteString("  /** Authentication configuration */\n")
	buf.WriteString("  auth?: types.AuthConfig;\n")
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

	buf.WriteString("  /** Whether to include credentials (cookies) in browser (default: false) */\n")
	buf.WriteString("  withCredentials?: boolean;\n")
	buf.WriteString("}\n")

	return buf.String()
}

// generateSSEClient generates an SSE client for an endpoint.
func (s *SSEGenerator) generateSSEClient(sse client.SSEEndpoint, spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	className := s.generateClassName(sse)

	// Class definition
	buf.WriteString(fmt.Sprintf("/**\n * %s\n", className))

	if sse.Description != "" {
		buf.WriteString(fmt.Sprintf(" * %s\n", sse.Description))
	}

	buf.WriteString(" * \n")
	buf.WriteString(" * Features:\n")
	buf.WriteString(" * - Cross-platform (Browser & Node.js)\n")
	buf.WriteString(" * - Connection timeouts\n")

	if config.Features.Reconnection {
		buf.WriteString(" * - Automatic reconnection with exponential backoff\n")
		buf.WriteString(" * - Last-Event-ID support for resumption\n")
	}

	buf.WriteString(" */\n")

	buf.WriteString(fmt.Sprintf("export class %s extends EventEmitter {\n", className))

	// Private fields
	buf.WriteString("  private eventSource: EventSource | null = null;\n")
	buf.WriteString("  private config: Required<Pick<SSEClientConfig, 'baseURL'>> & SSEClientConfig;\n")
	buf.WriteString("  private state: ConnectionState = ConnectionState.DISCONNECTED;\n")
	buf.WriteString("  private closed: boolean = false;\n")
	buf.WriteString("  private connectionTimeoutId: ReturnType<typeof setTimeout> | null = null;\n")

	if config.Features.Reconnection {
		buf.WriteString("  private reconnectAttempts: number = 0;\n")
		buf.WriteString("  private reconnectTimeoutId: ReturnType<typeof setTimeout> | null = null;\n")
		buf.WriteString("  private lastEventId: string = '';\n")
	}

	buf.WriteString("\n")

	// Constructor
	buf.WriteString("  constructor(config: SSEClientConfig) {\n")
	buf.WriteString("    super();\n")
	buf.WriteString("    this.config = {\n")
	buf.WriteString("      connectionTimeout: 30000,\n")

	if config.Features.Reconnection {
		buf.WriteString("      maxReconnectAttempts: 10,\n")
		buf.WriteString("      reconnectDelay: 1000,\n")
		buf.WriteString("      maxReconnectDelay: 30000,\n")
	}

	buf.WriteString("      withCredentials: false,\n")
	buf.WriteString("      ...config,\n")
	buf.WriteString("    };\n")
	buf.WriteString("  }\n\n")

	// Connect method with timeout
	buf.WriteString("  /**\n")
	buf.WriteString("   * Connect to the SSE stream.\n")
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

	// Build URL with query parameters for auth in browser
	buf.WriteString(fmt.Sprintf("      let url = this.config.baseURL + '%s';\n", sse.Path))
	buf.WriteString("      \n")
	buf.WriteString("      // In browser, add auth as query params since EventSource doesn't support custom headers\n")
	buf.WriteString("      if (isBrowser) {\n")
	buf.WriteString("        const params = new URLSearchParams();\n")
	buf.WriteString("        if (this.config.auth?.bearerToken) {\n")
	buf.WriteString("          params.set('token', this.config.auth.bearerToken);\n")
	buf.WriteString("        }\n")
	buf.WriteString("        if (this.config.auth?.apiKey) {\n")
	buf.WriteString("          params.set('apiKey', this.config.auth.apiKey);\n")
	buf.WriteString("        }\n")

	if config.Features.Reconnection {
		buf.WriteString("        if (this.lastEventId) {\n")
		buf.WriteString("          params.set('lastEventId', this.lastEventId);\n")
		buf.WriteString("        }\n")
	}

	buf.WriteString("        const queryString = params.toString();\n")
	buf.WriteString("        if (queryString) {\n")
	buf.WriteString("          url += (url.includes('?') ? '&' : '?') + queryString;\n")
	buf.WriteString("        }\n")
	buf.WriteString("      }\n\n")

	// Setup connection timeout
	buf.WriteString("      // Setup connection timeout\n")
	buf.WriteString("      this.connectionTimeoutId = setTimeout(() => {\n")
	buf.WriteString("        this.connectionTimeoutId = null;\n")
	buf.WriteString("        if (this.eventSource) {\n")
	buf.WriteString("          this.eventSource.close();\n")
	buf.WriteString("          this.eventSource = null;\n")
	buf.WriteString("        }\n")
	buf.WriteString("        this.setState(ConnectionState.ERROR);\n")
	buf.WriteString("        reject(new Error('Connection timeout'));\n")
	buf.WriteString("      }, this.config.connectionTimeout);\n\n")

	buf.WriteString("      try {\n")
	buf.WriteString("        const ES = getEventSource();\n\n")

	// Create EventSource with appropriate options
	buf.WriteString("        if (isBrowser) {\n")
	buf.WriteString("          this.eventSource = new ES(url, {\n")
	buf.WriteString("            withCredentials: this.config.withCredentials,\n")
	buf.WriteString("          });\n")
	buf.WriteString("        } else {\n")
	buf.WriteString("          // Node.js: can pass custom headers\n")
	buf.WriteString("          const headers: Record<string, string> = {\n")
	buf.WriteString("            'Accept': 'text/event-stream',\n")
	buf.WriteString("            'Cache-Control': 'no-cache',\n")
	buf.WriteString("          };\n")
	buf.WriteString("          if (this.config.auth?.bearerToken) {\n")
	buf.WriteString("            headers['Authorization'] = `Bearer ${this.config.auth.bearerToken}`;\n")
	buf.WriteString("          }\n")
	buf.WriteString("          if (this.config.auth?.apiKey) {\n")
	buf.WriteString("            headers['X-API-Key'] = this.config.auth.apiKey;\n")
	buf.WriteString("          }\n")

	if config.Features.Reconnection {
		buf.WriteString("          if (this.lastEventId) {\n")
		buf.WriteString("            headers['Last-Event-ID'] = this.lastEventId;\n")
		buf.WriteString("          }\n")
	}

	buf.WriteString("          this.eventSource = new (ES as any)(url, { headers });\n")
	buf.WriteString("        }\n\n")

	// Event handlers
	buf.WriteString("        this.eventSource.onopen = () => {\n")
	buf.WriteString("          this.clearConnectionTimeout();\n")
	buf.WriteString("          this.setState(ConnectionState.CONNECTED);\n")

	if config.Features.Reconnection {
		buf.WriteString("          this.reconnectAttempts = 0;\n")
	}

	buf.WriteString("          resolve();\n")
	buf.WriteString("        };\n\n")

	buf.WriteString("        this.eventSource.onerror = (event: Event) => {\n")
	buf.WriteString("          this.clearConnectionTimeout();\n")
	buf.WriteString("          this.setState(ConnectionState.ERROR);\n")
	buf.WriteString("          const error = new Error('SSE connection error');\n")
	buf.WriteString("          this.emit('error', error);\n\n")

	if config.Features.Reconnection {
		buf.WriteString("          const CLOSED = 2; // EventSource.CLOSED\n")
		buf.WriteString("          if (this.eventSource && this.eventSource.readyState === CLOSED) {\n")
		buf.WriteString("            if (!this.closed) {\n")
		buf.WriteString("              this.scheduleReconnect();\n")
		buf.WriteString("            }\n")
		buf.WriteString("          }\n")
	}

	buf.WriteString("          reject(error);\n")
	buf.WriteString("        };\n\n")

	// Register event listeners for each event type
	for eventName, schema := range sse.EventSchemas {
		typeName := s.getSchemaTypeName(schema, spec)

		buf.WriteString(fmt.Sprintf("        this.eventSource.addEventListener('%s', (event: MessageEvent) => {\n", eventName))
		buf.WriteString("          try {\n")

		if config.Features.Reconnection {
			buf.WriteString("            if (event.lastEventId) {\n")
			buf.WriteString("              this.lastEventId = event.lastEventId;\n")
			buf.WriteString("            }\n")
		}

		buf.WriteString(fmt.Sprintf("            const data: %s = JSON.parse(event.data);\n", typeName))
		buf.WriteString(fmt.Sprintf("            this.emit('%s', data);\n", eventName))
		buf.WriteString("          } catch (error) {\n")
		buf.WriteString("            this.emit('error', error);\n")
		buf.WriteString("          }\n")
		buf.WriteString("        });\n\n")
	}

	// Also listen for generic 'message' events
	buf.WriteString("        // Listen for generic messages\n")
	buf.WriteString("        this.eventSource.onmessage = (event: MessageEvent) => {\n")

	if config.Features.Reconnection {
		buf.WriteString("          if (event.lastEventId) {\n")
		buf.WriteString("            this.lastEventId = event.lastEventId;\n")
		buf.WriteString("          }\n")
	}

	buf.WriteString("          this.emit('message', event.data);\n")
	buf.WriteString("        };\n")

	buf.WriteString("      } catch (error) {\n")
	buf.WriteString("        this.clearConnectionTimeout();\n")
	buf.WriteString("        this.setState(ConnectionState.ERROR);\n")
	buf.WriteString("        reject(error);\n")
	buf.WriteString("      }\n")
	buf.WriteString("    });\n")
	buf.WriteString("  }\n\n")

	// Generate on<EventName> methods for each event type
	for eventName, schema := range sse.EventSchemas {
		typeName := s.getSchemaTypeName(schema, spec)
		methodName := s.toPascalCase(eventName)

		buf.WriteString(fmt.Sprintf("  /**\n   * Register a handler for %s events.\n", eventName))
		buf.WriteString(fmt.Sprintf("   * @param handler - Function to call when %s event is received\n", eventName))
		buf.WriteString("   */\n")
		buf.WriteString(fmt.Sprintf("  on%s(handler: (data: %s) => void): void {\n", methodName, typeName))
		buf.WriteString(fmt.Sprintf("    this.on('%s', handler);\n", eventName))
		buf.WriteString("  }\n\n")

		buf.WriteString(fmt.Sprintf("  /**\n   * Remove a handler for %s events.\n", eventName))
		buf.WriteString("   * @param handler - The handler to remove\n")
		buf.WriteString("   */\n")
		buf.WriteString(fmt.Sprintf("  off%s(handler: (data: %s) => void): void {\n", methodName, typeName))
		buf.WriteString(fmt.Sprintf("    this.off('%s', handler);\n", eventName))
		buf.WriteString("  }\n\n")
	}

	// OnMessage for generic messages
	buf.WriteString("  /**\n")
	buf.WriteString("   * Register a handler for generic messages.\n")
	buf.WriteString("   * @param handler - Function to call when a generic message is received\n")
	buf.WriteString("   */\n")
	buf.WriteString("  onMessage(handler: (data: string) => void): void {\n")
	buf.WriteString("    this.on('message', handler);\n")
	buf.WriteString("  }\n\n")

	// OnError convenience method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Register a handler for errors.\n")
	buf.WriteString("   * @param handler - Function to call when an error occurs\n")
	buf.WriteString("   */\n")
	buf.WriteString("  onError(handler: (error: Error) => void): void {\n")
	buf.WriteString("    this.on('error', handler);\n")
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
		buf.WriteString("      if (!this.closed) {\n")
		buf.WriteString("        try {\n")
		buf.WriteString("          await this.connect();\n")
		buf.WriteString("        } catch (error) {\n")
		buf.WriteString("          // Will schedule another reconnect in error handler\n")
		buf.WriteString("        }\n")
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

	// Close method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Close the SSE connection.\n")
	buf.WriteString("   */\n")
	buf.WriteString("  close(): void {\n")
	buf.WriteString("    this.closed = true;\n")
	buf.WriteString("    this.clearConnectionTimeout();\n")

	if config.Features.Reconnection {
		buf.WriteString("    this.cancelReconnect();\n")
	}

	buf.WriteString("\n")
	buf.WriteString("    if (this.eventSource) {\n")
	buf.WriteString("      this.eventSource.close();\n")
	buf.WriteString("      this.eventSource = null;\n")
	buf.WriteString("    }\n")
	buf.WriteString("    this.setState(ConnectionState.CLOSED);\n")
	buf.WriteString("  }\n\n")

	// isConnected helper
	buf.WriteString("  /**\n")
	buf.WriteString("   * Check if the SSE stream is currently connected.\n")
	buf.WriteString("   */\n")
	buf.WriteString("  isConnected(): boolean {\n")
	buf.WriteString("    return this.state === ConnectionState.CONNECTED;\n")
	buf.WriteString("  }\n")

	buf.WriteString("}\n")

	return buf.String()
}

// generateClassName generates a class name for an SSE endpoint.
func (s *SSEGenerator) generateClassName(sse client.SSEEndpoint) string {
	if sse.ID != "" {
		return s.toPascalCase(sse.ID) + "SSEClient"
	}

	return "SSEClient"
}

// getSchemaTypeName gets the type name for a schema.
func (s *SSEGenerator) getSchemaTypeName(schema *client.Schema, spec *client.APISpec) string {
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
func (s *SSEGenerator) toPascalCase(str string) string {
	parts := strings.FieldsFunc(str, func(r rune) bool {
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
