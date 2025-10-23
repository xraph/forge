package typescript

import (
	"fmt"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// WebSocketGenerator generates TypeScript WebSocket client code
type WebSocketGenerator struct{}

// NewWebSocketGenerator creates a new WebSocket generator
func NewWebSocketGenerator() *WebSocketGenerator {
	return &WebSocketGenerator{}
}

// Generate generates the WebSocket clients
func (w *WebSocketGenerator) Generate(spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	buf.WriteString("import WebSocket from 'ws';\n")
	buf.WriteString("import { EventEmitter } from 'events';\n")
	buf.WriteString("import * as types from './types';\n\n")

	// ConnectionState enum
	buf.WriteString("export enum ConnectionState {\n")
	buf.WriteString("  DISCONNECTED = 'disconnected',\n")
	buf.WriteString("  CONNECTING = 'connecting',\n")
	buf.WriteString("  CONNECTED = 'connected',\n")
	buf.WriteString("  RECONNECTING = 'reconnecting',\n")
	buf.WriteString("  CLOSED = 'closed',\n")
	buf.WriteString("  ERROR = 'error',\n")
	buf.WriteString("}\n\n")

	// Generate client for each WebSocket endpoint
	for _, ws := range spec.WebSockets {
		clientCode := w.generateWebSocketClient(ws, spec, config)
		buf.WriteString(clientCode)
		buf.WriteString("\n")
	}

	return buf.String()
}

// generateWebSocketClient generates a WebSocket client for an endpoint
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
	buf.WriteString(" */\n")
	buf.WriteString(fmt.Sprintf("export class %s extends EventEmitter {\n", className))
	buf.WriteString("  private ws: WebSocket | null = null;\n")
	buf.WriteString("  private baseURL: string;\n")
	buf.WriteString("  private auth?: types.AuthConfig;\n")
	buf.WriteString("  private state: ConnectionState = ConnectionState.DISCONNECTED;\n")
	buf.WriteString("  private closed: boolean = false;\n")

	if config.Features.Reconnection {
		buf.WriteString("  private reconnectAttempts: number = 0;\n")
		buf.WriteString("  private maxReconnectAttempts: number = 10;\n")
		buf.WriteString("  private reconnectDelay: number = 1000;\n")
		buf.WriteString("  private maxReconnectDelay: number = 30000;\n")
	}

	if config.Features.Heartbeat {
		buf.WriteString("  private heartbeatInterval: NodeJS.Timeout | null = null;\n")
		buf.WriteString("  private heartbeatIntervalMs: number = 30000;\n")
	}

	buf.WriteString("\n")

	// Constructor
	buf.WriteString("  constructor(baseURL: string, auth?: types.AuthConfig) {\n")
	buf.WriteString("    super();\n")
	buf.WriteString("    this.baseURL = baseURL;\n")
	buf.WriteString("    this.auth = auth;\n")
	buf.WriteString("  }\n\n")

	// Connect method
	buf.WriteString("  async connect(): Promise<void> {\n")
	buf.WriteString("    return new Promise((resolve, reject) => {\n")
	buf.WriteString("      this.setState(ConnectionState.CONNECTING);\n\n")

	buf.WriteString(fmt.Sprintf("      const wsURL = this.baseURL.replace(/^http/, 'ws') + '%s';\n", ws.Path))
	buf.WriteString("      const headers: Record<string, string> = {};\n\n")

	buf.WriteString("      if (this.auth?.bearerToken) {\n")
	buf.WriteString("        headers['Authorization'] = `Bearer ${this.auth.bearerToken}`;\n")
	buf.WriteString("      }\n")
	buf.WriteString("      if (this.auth?.apiKey) {\n")
	buf.WriteString("        headers['X-API-Key'] = this.auth.apiKey;\n")
	buf.WriteString("      }\n\n")

	buf.WriteString("      this.ws = new WebSocket(wsURL, { headers });\n\n")

	buf.WriteString("      this.ws.on('open', () => {\n")
	buf.WriteString("        this.setState(ConnectionState.CONNECTED);\n")
	if config.Features.Reconnection {
		buf.WriteString("        this.reconnectAttempts = 0;\n")
	}
	if config.Features.Heartbeat {
		buf.WriteString("        this.startHeartbeat();\n")
	}
	buf.WriteString("        resolve();\n")
	buf.WriteString("      });\n\n")

	buf.WriteString("      this.ws.on('message', (data: WebSocket.Data) => {\n")
	buf.WriteString("        try {\n")
	buf.WriteString(fmt.Sprintf("          const message: %s = JSON.parse(data.toString());\n", receiveType))
	buf.WriteString("          this.emit('message', message);\n")
	buf.WriteString("        } catch (error) {\n")
	buf.WriteString("          this.emit('error', error);\n")
	buf.WriteString("        }\n")
	buf.WriteString("      });\n\n")

	buf.WriteString("      this.ws.on('error', (error) => {\n")
	buf.WriteString("        this.setState(ConnectionState.ERROR);\n")
	buf.WriteString("        this.emit('error', error);\n")
	buf.WriteString("        reject(error);\n")
	buf.WriteString("      });\n\n")

	buf.WriteString("      this.ws.on('close', () => {\n")
	buf.WriteString("        this.setState(ConnectionState.DISCONNECTED);\n")
	if config.Features.Heartbeat {
		buf.WriteString("        this.stopHeartbeat();\n")
	}
	if config.Features.Reconnection {
		buf.WriteString("        if (!this.closed) {\n")
		buf.WriteString("          this.reconnect();\n")
		buf.WriteString("        }\n")
	}
	buf.WriteString("      });\n")

	buf.WriteString("    });\n")
	buf.WriteString("  }\n\n")

	// Send method
	buf.WriteString(fmt.Sprintf("  send(message: %s): void {\n", sendType))
	buf.WriteString("    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {\n")
	buf.WriteString("      throw new Error('WebSocket is not connected');\n")
	buf.WriteString("    }\n")
	buf.WriteString("    this.ws.send(JSON.stringify(message));\n")
	buf.WriteString("  }\n\n")

	// OnMessage convenience method
	buf.WriteString(fmt.Sprintf("  onMessage(handler: (message: %s) => void): void {\n", receiveType))
	buf.WriteString("    this.on('message', handler);\n")
	buf.WriteString("  }\n\n")

	// OnError convenience method
	buf.WriteString("  onError(handler: (error: Error) => void): void {\n")
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
		buf.WriteString("  private async reconnect(): Promise<void> {\n")
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
		buf.WriteString("    await new Promise(resolve => setTimeout(resolve, delay));\n\n")
		buf.WriteString("    try {\n")
		buf.WriteString("      await this.connect();\n")
		buf.WriteString("    } catch (error) {\n")
		buf.WriteString("      // Will retry in the close handler\n")
		buf.WriteString("    }\n")
		buf.WriteString("  }\n\n")
	}

	// Heartbeat logic
	if config.Features.Heartbeat {
		buf.WriteString("  private startHeartbeat(): void {\n")
		buf.WriteString("    this.heartbeatInterval = setInterval(() => {\n")
		buf.WriteString("      if (this.ws && this.ws.readyState === WebSocket.OPEN) {\n")
		buf.WriteString("        this.ws.ping();\n")
		buf.WriteString("      }\n")
		buf.WriteString("    }, this.heartbeatIntervalMs);\n")
		buf.WriteString("  }\n\n")

		buf.WriteString("  private stopHeartbeat(): void {\n")
		buf.WriteString("    if (this.heartbeatInterval) {\n")
		buf.WriteString("      clearInterval(this.heartbeatInterval);\n")
		buf.WriteString("      this.heartbeatInterval = null;\n")
		buf.WriteString("    }\n")
		buf.WriteString("  }\n\n")
	}

	// Close method
	buf.WriteString("  close(): void {\n")
	buf.WriteString("    this.closed = true;\n")
	if config.Features.Heartbeat {
		buf.WriteString("    this.stopHeartbeat();\n")
	}
	buf.WriteString("    if (this.ws) {\n")
	buf.WriteString("      this.ws.close();\n")
	buf.WriteString("      this.ws = null;\n")
	buf.WriteString("    }\n")
	buf.WriteString("    this.setState(ConnectionState.CLOSED);\n")
	buf.WriteString("  }\n")

	buf.WriteString("}\n")

	return buf.String()
}

// generateClassName generates a class name for a WebSocket endpoint
func (w *WebSocketGenerator) generateClassName(ws client.WebSocketEndpoint) string {
	if ws.ID != "" {
		return w.toPascalCase(ws.ID) + "WSClient"
	}
	return "WebSocketClient"
}

// getSchemaTypeName gets the type name for a schema
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

// toPascalCase converts a string to PascalCase
func (w *WebSocketGenerator) toPascalCase(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '_' || r == '-' || r == ' '
	})

	var result string
	for _, part := range parts {
		if len(part) > 0 {
			result += strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
		}
	}

	return result
}
