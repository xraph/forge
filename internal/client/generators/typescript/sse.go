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

	// Use conditional imports for browser/Node compatibility
	buf.WriteString("// EventSource polyfill for Node.js\n")
	buf.WriteString("let EventSourceImpl: any;\n")
	buf.WriteString("let EventEmitterImpl: any;\n\n")

	buf.WriteString("if (typeof window !== 'undefined' && window.EventSource) {\n")
	buf.WriteString("  // Browser environment\n")
	buf.WriteString("  EventSourceImpl = window.EventSource;\n")
	buf.WriteString("  // Use a simple event emitter for browser\n")
	buf.WriteString("  EventEmitterImpl = class {\n")
	buf.WriteString("    private listeners: Map<string, Function[]> = new Map();\n")
	buf.WriteString("    on(event: string, handler: Function) {\n")
	buf.WriteString("      if (!this.listeners.has(event)) this.listeners.set(event, []);\n")
	buf.WriteString("      this.listeners.get(event)!.push(handler);\n")
	buf.WriteString("    }\n")
	buf.WriteString("    emit(event: string, ...args: any[]) {\n")
	buf.WriteString("      const handlers = this.listeners.get(event) || [];\n")
	buf.WriteString("      handlers.forEach(h => h(...args));\n")
	buf.WriteString("    }\n")
	buf.WriteString("  };\n")
	buf.WriteString("} else {\n")
	buf.WriteString("  // Node.js environment\n")
	buf.WriteString("  const esModule = await import('eventsource');\n")
	buf.WriteString("  EventSourceImpl = esModule.default || esModule;\n")
	buf.WriteString("  const eventsModule = await import('events');\n")
	buf.WriteString("  EventEmitterImpl = eventsModule.EventEmitter;\n")
	buf.WriteString("}\n\n")

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

// generateSSEClient generates an SSE client for an endpoint.
func (s *SSEGenerator) generateSSEClient(sse client.SSEEndpoint, spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	className := s.generateClassName(sse)

	// Class definition
	buf.WriteString(fmt.Sprintf("/**\n * %s\n", className))

	if sse.Description != "" {
		buf.WriteString(fmt.Sprintf(" * %s\n", sse.Description))
	}

	buf.WriteString(" */\n")
	buf.WriteString(fmt.Sprintf("export class %s extends EventEmitterImpl {\n", className))
	buf.WriteString("  private eventSource: any | null = null;\n")
	buf.WriteString("  private baseURL: string;\n")
	buf.WriteString("  private auth?: types.AuthConfig;\n")
	buf.WriteString("  private state: ConnectionState = ConnectionState.DISCONNECTED;\n")
	buf.WriteString("  private closed: boolean = false;\n")

	if config.Features.Reconnection {
		buf.WriteString("  private reconnectAttempts: number = 0;\n")
		buf.WriteString("  private maxReconnectAttempts: number = 10;\n")
		buf.WriteString("  private reconnectDelay: number = 1000;\n")
		buf.WriteString("  private maxReconnectDelay: number = 30000;\n")
		buf.WriteString("  private lastEventId: string = '';\n")
	}

	buf.WriteString("\n")

	// Constructor
	buf.WriteString("  constructor(baseURL: string, auth?: types.AuthConfig) {\n")
	buf.WriteString("    super();\n")
	buf.WriteString("    this.baseURL = baseURL;\n")
	buf.WriteString("    this.auth = auth;\n")
	buf.WriteString("  }\n\n")

	// Connect method
	buf.WriteString("  connect(): void {\n")
	buf.WriteString("    this.setState(ConnectionState.CONNECTING);\n\n")

	buf.WriteString(fmt.Sprintf("    const url = this.baseURL + '%s';\n", sse.Path))
	buf.WriteString("    const headers: Record<string, string> = {\n")
	buf.WriteString("      'Accept': 'text/event-stream',\n")
	buf.WriteString("      'Cache-Control': 'no-cache',\n")
	buf.WriteString("    };\n\n")

	buf.WriteString("    if (this.auth?.bearerToken) {\n")
	buf.WriteString("      headers['Authorization'] = `Bearer ${this.auth.bearerToken}`;\n")
	buf.WriteString("    }\n")
	buf.WriteString("    if (this.auth?.apiKey) {\n")
	buf.WriteString("      headers['X-API-Key'] = this.auth.apiKey;\n")
	buf.WriteString("    }\n")

	if config.Features.Reconnection {
		buf.WriteString("    if (this.lastEventId) {\n")
		buf.WriteString("      headers['Last-Event-ID'] = this.lastEventId;\n")
		buf.WriteString("    }\n\n")
	}

	buf.WriteString("    this.eventSource = new EventSourceImpl(url, { headers });\n\n")

	buf.WriteString("    this.eventSource.onopen = () => {\n")
	buf.WriteString("      this.setState(ConnectionState.CONNECTED);\n")

	if config.Features.Reconnection {
		buf.WriteString("      this.reconnectAttempts = 0;\n")
	}

	buf.WriteString("    };\n\n")

	buf.WriteString("    this.eventSource.onerror = (error) => {\n")
	buf.WriteString("      this.setState(ConnectionState.ERROR);\n")
	buf.WriteString("      this.emit('error', error);\n")

	if config.Features.Reconnection {
		buf.WriteString("      const CLOSED = typeof EventSourceImpl.CLOSED !== 'undefined' ? EventSourceImpl.CLOSED : 2;\n")
		buf.WriteString("      if (this.eventSource && this.eventSource.readyState === CLOSED) {\n")
		buf.WriteString("        if (!this.closed) {\n")
		buf.WriteString("          this.reconnect();\n")
		buf.WriteString("        }\n")
		buf.WriteString("      }\n")
	}

	buf.WriteString("    };\n\n")

	// Register event listeners for each event type
	for eventName, schema := range sse.EventSchemas {
		typeName := s.getSchemaTypeName(schema, spec)

		buf.WriteString(fmt.Sprintf("    this.eventSource.addEventListener('%s', (event: any) => {\n", eventName))
		buf.WriteString("      try {\n")

		if config.Features.Reconnection {
			buf.WriteString("        if (event.lastEventId) {\n")
			buf.WriteString("          this.lastEventId = event.lastEventId;\n")
			buf.WriteString("        }\n")
		}

		buf.WriteString(fmt.Sprintf("        const data: %s = JSON.parse(event.data);\n", typeName))
		buf.WriteString(fmt.Sprintf("        this.emit('%s', data);\n", eventName))
		buf.WriteString("      } catch (error) {\n")
		buf.WriteString("        this.emit('error', error);\n")
		buf.WriteString("      }\n")
		buf.WriteString("    });\n\n")
	}

	buf.WriteString("  }\n\n")

	// Generate on<EventName> methods for each event type
	for eventName, schema := range sse.EventSchemas {
		typeName := s.getSchemaTypeName(schema, spec)
		methodName := s.toPascalCase(eventName)

		buf.WriteString(fmt.Sprintf("  /**\n   * Register a handler for %s events\n   */\n", eventName))
		buf.WriteString(fmt.Sprintf("  on%s(handler: (data: %s) => void): void {\n", methodName, typeName))
		buf.WriteString(fmt.Sprintf("    this.on('%s', handler);\n", eventName))
		buf.WriteString("  }\n\n")
	}

	// OnError convenience method
	buf.WriteString("  onError(handler: (error: any) => void): void {\n")
	buf.WriteString("    this.on('error', handler);\n")
	buf.WriteString("  }\n\n")

	// OnStateChange method
	if config.Features.StateManagement {
		buf.WriteString("  onStateChange(handler: (state: ConnectionState) => void): void {\n")
		buf.WriteString("    this.on('stateChange', handler);\n")
		buf.WriteString("  }\n\n")

		buf.WriteString("  private setState(state: ConnectionState): void {\n")
		buf.WriteString("    this.state = state;\n")
		buf.WriteString("    this.emit('stateChange', state);\n")
		buf.WriteString("  }\n\n")

		buf.WriteString("  getState(): ConnectionState {\n")
		buf.WriteString("    return this.state;\n")
		buf.WriteString("  }\n\n")
	}

	// Reconnection logic
	if config.Features.Reconnection {
		buf.WriteString("  private reconnect(): void {\n")
		buf.WriteString("    if (this.reconnectAttempts >= this.maxReconnectAttempts) {\n")
		buf.WriteString("      this.setState(ConnectionState.CLOSED);\n")
		buf.WriteString("      return;\n")
		buf.WriteString("    }\n\n")
		buf.WriteString("    this.setState(ConnectionState.RECONNECTING);\n")
		buf.WriteString("    this.reconnectAttempts++;\n\n")
		buf.WriteString("    const delay = Math.min(\n")
		buf.WriteString("      this.reconnectDelay * Math.pow(2, this.reconnectAttempts - 1),\n")
		buf.WriteString("      this.maxReconnectDelay\n")
		buf.WriteString("    );\n\n")
		buf.WriteString("    setTimeout(() => {\n")
		buf.WriteString("      if (!this.closed) {\n")
		buf.WriteString("        this.connect();\n")
		buf.WriteString("      }\n")
		buf.WriteString("    }, delay);\n")
		buf.WriteString("  }\n\n")
	}

	// Close method
	buf.WriteString("  close(): void {\n")
	buf.WriteString("    this.closed = true;\n")
	buf.WriteString("    if (this.eventSource) {\n")
	buf.WriteString("      this.eventSource.close();\n")
	buf.WriteString("      this.eventSource = null;\n")
	buf.WriteString("    }\n")
	buf.WriteString("    this.setState(ConnectionState.CLOSED);\n")
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

	var (
		result      string
		resultSb237 strings.Builder
	)

	for _, part := range parts {
		if len(part) > 0 {
			resultSb237.WriteString(strings.ToUpper(part[:1]) + strings.ToLower(part[1:]))
		}
	}

	result += resultSb237.String()

	return result
}
