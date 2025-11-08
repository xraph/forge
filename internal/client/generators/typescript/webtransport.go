package typescript

import (
	"fmt"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// WebTransportGenerator generates TypeScript WebTransport client code.
type WebTransportGenerator struct{}

// NewWebTransportGenerator creates a new WebTransport generator.
func NewWebTransportGenerator() *WebTransportGenerator {
	return &WebTransportGenerator{}
}

// Generate generates the WebTransport clients.
func (w *WebTransportGenerator) Generate(spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	buf.WriteString("import { EventEmitter } from 'events';\n")
	buf.WriteString("import * as types from './types';\n\n")

	// ConnectionState enum
	buf.WriteString("export enum WebTransportState {\n")
	buf.WriteString("  DISCONNECTED = 'disconnected',\n")
	buf.WriteString("  CONNECTING = 'connecting',\n")
	buf.WriteString("  CONNECTED = 'connected',\n")
	buf.WriteString("  RECONNECTING = 'reconnecting',\n")
	buf.WriteString("  CLOSED = 'closed',\n")
	buf.WriteString("  ERROR = 'error',\n")
	buf.WriteString("}\n\n")

	// Generate client for each WebTransport endpoint
	for _, wt := range spec.WebTransports {
		clientCode := w.generateWebTransportClient(wt, spec, config)
		buf.WriteString(clientCode)
		buf.WriteString("\n")
	}

	return buf.String()
}

// generateWebTransportClient generates a WebTransport client for an endpoint.
func (w *WebTransportGenerator) generateWebTransportClient(wt client.WebTransportEndpoint, spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	className := w.generateClassName(wt)

	// Class documentation
	buf.WriteString(fmt.Sprintf("/**\n * %s\n", className))

	if wt.Description != "" {
		buf.WriteString(fmt.Sprintf(" * %s\n", wt.Description))
	}

	buf.WriteString(" */\n")

	// Class definition
	buf.WriteString(fmt.Sprintf("export class %s extends EventEmitter {\n", className))
	buf.WriteString("  private transport: WebTransport | null = null;\n")
	buf.WriteString("  private baseURL: string;\n")
	buf.WriteString("  private auth?: types.AuthConfig;\n")
	buf.WriteString("  private state: WebTransportState = WebTransportState.DISCONNECTED;\n")
	buf.WriteString("  private closed: boolean = false;\n\n")

	if config.Features.Reconnection {
		buf.WriteString("  // Reconnection\n")
		buf.WriteString("  private reconnectAttempts: number = 0;\n")
		buf.WriteString("  private maxReconnectAttempts: number = 10;\n")
		buf.WriteString("  private reconnectDelay: number = 1000;\n")
		buf.WriteString("  private maxReconnectDelay: number = 30000;\n\n")
	}

	buf.WriteString("  constructor(baseURL: string, auth?: types.AuthConfig) {\n")
	buf.WriteString("    super();\n")
	buf.WriteString("    this.baseURL = baseURL;\n")
	buf.WriteString("    this.auth = auth;\n")
	buf.WriteString("  }\n\n")

	// Connect method
	buf.WriteString("  /**\n")
	buf.WriteString(fmt.Sprintf("   * Connect to WebTransport endpoint %s\n", wt.Path))
	buf.WriteString("   */\n")
	buf.WriteString("  async connect(): Promise<void> {\n")
	buf.WriteString("    this.setState(WebTransportState.CONNECTING);\n\n")

	buf.WriteString(fmt.Sprintf("    const wtURL = this.baseURL.replace(/^http/, 'https') + '%s';\n\n", wt.Path))

	buf.WriteString("    try {\n")
	buf.WriteString("      this.transport = new WebTransport(wtURL);\n")
	buf.WriteString("      await this.transport.ready;\n\n")

	buf.WriteString("      this.setState(WebTransportState.CONNECTED);\n")

	if config.Features.Reconnection {
		buf.WriteString("      this.reconnectAttempts = 0;\n\n")
	}

	buf.WriteString("      // Start handling incoming streams\n")
	buf.WriteString("      this.handleIncomingStreams();\n\n")

	buf.WriteString("      // Handle connection closure\n")
	buf.WriteString("      this.transport.closed\n")
	buf.WriteString("        .then(() => {\n")
	buf.WriteString("          this.setState(WebTransportState.DISCONNECTED);\n")

	if config.Features.Reconnection {
		buf.WriteString("          if (!this.closed) {\n")
		buf.WriteString("            this.reconnect();\n")
		buf.WriteString("          }\n")
	}

	buf.WriteString("        })\n")
	buf.WriteString("        .catch((error) => {\n")
	buf.WriteString("          this.setState(WebTransportState.ERROR);\n")
	buf.WriteString("          this.emit('error', error);\n")

	if config.Features.Reconnection {
		buf.WriteString("          if (!this.closed) {\n")
		buf.WriteString("            this.reconnect();\n")
		buf.WriteString("          }\n")
	}

	buf.WriteString("        });\n")
	buf.WriteString("    } catch (error) {\n")
	buf.WriteString("      this.setState(WebTransportState.ERROR);\n")
	buf.WriteString("      throw error;\n")
	buf.WriteString("    }\n")
	buf.WriteString("  }\n\n")

	// Bidirectional stream methods
	if wt.BiStreamSchema != nil {
		buf.WriteString(w.generateBiStreamMethods(wt.BiStreamSchema, spec))
	}

	// Unidirectional stream methods
	if wt.UniStreamSchema != nil {
		buf.WriteString(w.generateUniStreamMethods(wt.UniStreamSchema, spec))
	}

	// Datagram methods
	if wt.DatagramSchema != nil {
		buf.WriteString(w.generateDatagramMethods(wt.DatagramSchema, spec))
	}

	// Handle incoming streams
	buf.WriteString(w.generateIncomingStreamHandler())

	// State management
	if config.Features.StateManagement {
		buf.WriteString(w.generateStateManagement())
	}

	// Error handling
	buf.WriteString(w.generateErrorHandling())

	// Reconnection
	if config.Features.Reconnection {
		buf.WriteString(w.generateReconnection())
	}

	// Close method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Close the WebTransport connection\n")
	buf.WriteString("   */\n")
	buf.WriteString("  close(): void {\n")
	buf.WriteString("    this.closed = true;\n")
	buf.WriteString("    if (this.transport) {\n")
	buf.WriteString("      this.transport.close();\n")
	buf.WriteString("      this.transport = null;\n")
	buf.WriteString("    }\n")
	buf.WriteString("    this.setState(WebTransportState.CLOSED);\n")
	buf.WriteString("  }\n\n")

	// Get state method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Get the current connection state\n")
	buf.WriteString("   */\n")
	buf.WriteString("  getState(): WebTransportState {\n")
	buf.WriteString("    return this.state;\n")
	buf.WriteString("  }\n\n")

	// setState helper
	buf.WriteString("  private setState(state: WebTransportState): void {\n")
	buf.WriteString("    this.state = state;\n")
	buf.WriteString("    this.emit('stateChange', state);\n")
	buf.WriteString("  }\n")

	buf.WriteString("}\n")

	return buf.String()
}

// generateBiStreamMethods generates bidirectional stream methods.
func (w *WebTransportGenerator) generateBiStreamMethods(schema *client.StreamSchema, spec *client.APISpec) string {
	var buf strings.Builder

	sendType := w.getSchemaTypeName(schema.SendSchema, spec)
	receiveType := w.getSchemaTypeName(schema.ReceiveSchema, spec)

	buf.WriteString("  /**\n")
	buf.WriteString("   * Open a new bidirectional stream\n")
	buf.WriteString("   */\n")
	buf.WriteString("  async openBidiStream(): Promise<BiDiStream> {\n")
	buf.WriteString("    if (!this.transport) {\n")
	buf.WriteString("      throw new Error('Not connected');\n")
	buf.WriteString("    }\n\n")

	buf.WriteString("    const stream = await this.transport.createBidirectionalStream();\n")
	buf.WriteString("    return new BiDiStream(stream);\n")
	buf.WriteString("  }\n\n")

	// BiDiStream class
	buf.WriteString("/**\n")
	buf.WriteString(" * Bidirectional stream wrapper\n")
	buf.WriteString(" */\n")
	buf.WriteString("class BiDiStream {\n")
	buf.WriteString("  private stream: WebTransportBidirectionalStream;\n\n")

	buf.WriteString("  constructor(stream: WebTransportBidirectionalStream) {\n")
	buf.WriteString("    this.stream = stream;\n")
	buf.WriteString("  }\n\n")

	buf.WriteString(fmt.Sprintf("  async send(msg: %s): Promise<void> {\n", sendType))
	buf.WriteString("    const writer = this.stream.writable.getWriter();\n")
	buf.WriteString("    const encoder = new TextEncoder();\n")
	buf.WriteString("    const data = encoder.encode(JSON.stringify(msg));\n")
	buf.WriteString("    await writer.write(data);\n")
	buf.WriteString("    writer.releaseLock();\n")
	buf.WriteString("  }\n\n")

	buf.WriteString(fmt.Sprintf("  async receive(): Promise<%s> {\n", receiveType))
	buf.WriteString("    const reader = this.stream.readable.getReader();\n")
	buf.WriteString("    const decoder = new TextDecoder();\n")
	buf.WriteString("    let result = '';\n\n")

	buf.WriteString("    while (true) {\n")
	buf.WriteString("      const { done, value } = await reader.read();\n")
	buf.WriteString("      if (done) break;\n")
	buf.WriteString("      result += decoder.decode(value, { stream: true });\n")
	buf.WriteString("    }\n\n")

	buf.WriteString("    reader.releaseLock();\n")
	buf.WriteString("    return JSON.parse(result);\n")
	buf.WriteString("  }\n\n")

	buf.WriteString("  close(): void {\n")
	buf.WriteString("    // Streams are automatically closed when done\n")
	buf.WriteString("  }\n")
	buf.WriteString("}\n\n")

	return buf.String()
}

// generateUniStreamMethods generates unidirectional stream methods.
func (w *WebTransportGenerator) generateUniStreamMethods(schema *client.StreamSchema, spec *client.APISpec) string {
	var buf strings.Builder

	sendType := w.getSchemaTypeName(schema.SendSchema, spec)

	buf.WriteString("  /**\n")
	buf.WriteString("   * Open a new unidirectional stream for sending\n")
	buf.WriteString("   */\n")
	buf.WriteString("  async openUniStream(): Promise<UniStream> {\n")
	buf.WriteString("    if (!this.transport) {\n")
	buf.WriteString("      throw new Error('Not connected');\n")
	buf.WriteString("    }\n\n")

	buf.WriteString("    const stream = await this.transport.createUnidirectionalStream();\n")
	buf.WriteString("    return new UniStream(stream);\n")
	buf.WriteString("  }\n\n")

	// UniStream class
	buf.WriteString("/**\n")
	buf.WriteString(" * Unidirectional stream wrapper\n")
	buf.WriteString(" */\n")
	buf.WriteString("class UniStream {\n")
	buf.WriteString("  private stream: WritableStream;\n\n")

	buf.WriteString("  constructor(stream: WritableStream) {\n")
	buf.WriteString("    this.stream = stream;\n")
	buf.WriteString("  }\n\n")

	buf.WriteString(fmt.Sprintf("  async send(msg: %s): Promise<void> {\n", sendType))
	buf.WriteString("    const writer = this.stream.getWriter();\n")
	buf.WriteString("    const encoder = new TextEncoder();\n")
	buf.WriteString("    const data = encoder.encode(JSON.stringify(msg));\n")
	buf.WriteString("    await writer.write(data);\n")
	buf.WriteString("    writer.releaseLock();\n")
	buf.WriteString("  }\n\n")

	buf.WriteString("  async close(): Promise<void> {\n")
	buf.WriteString("    const writer = this.stream.getWriter();\n")
	buf.WriteString("    await writer.close();\n")
	buf.WriteString("  }\n")
	buf.WriteString("}\n\n")

	return buf.String()
}

// generateDatagramMethods generates datagram methods.
func (w *WebTransportGenerator) generateDatagramMethods(schema *client.Schema, spec *client.APISpec) string {
	var buf strings.Builder

	typeName := w.getSchemaTypeName(schema, spec)

	buf.WriteString("  /**\n")
	buf.WriteString(fmt.Sprintf("   * Send a %s as an unreliable datagram\n", typeName))
	buf.WriteString("   */\n")
	buf.WriteString(fmt.Sprintf("  async sendDatagram(msg: %s): Promise<void> {\n", typeName))
	buf.WriteString("    if (!this.transport) {\n")
	buf.WriteString("      throw new Error('Not connected');\n")
	buf.WriteString("    }\n\n")

	buf.WriteString("    const writer = this.transport.datagrams.writable.getWriter();\n")
	buf.WriteString("    const encoder = new TextEncoder();\n")
	buf.WriteString("    const data = encoder.encode(JSON.stringify(msg));\n")
	buf.WriteString("    await writer.write(data);\n")
	buf.WriteString("    writer.releaseLock();\n")
	buf.WriteString("  }\n\n")

	buf.WriteString("  /**\n")
	buf.WriteString(fmt.Sprintf("   * Receive a %s datagram\n", typeName))
	buf.WriteString("   */\n")
	buf.WriteString(fmt.Sprintf("  async receiveDatagram(): Promise<%s> {\n", typeName))
	buf.WriteString("    if (!this.transport) {\n")
	buf.WriteString("      throw new Error('Not connected');\n")
	buf.WriteString("    }\n\n")

	buf.WriteString("    const reader = this.transport.datagrams.readable.getReader();\n")
	buf.WriteString("    const { value } = await reader.read();\n")
	buf.WriteString("    reader.releaseLock();\n\n")

	buf.WriteString("    const decoder = new TextDecoder();\n")
	buf.WriteString("    const text = decoder.decode(value);\n")
	buf.WriteString("    return JSON.parse(text);\n")
	buf.WriteString("  }\n\n")

	return buf.String()
}

// generateIncomingStreamHandler generates handler for incoming streams.
func (w *WebTransportGenerator) generateIncomingStreamHandler() string {
	return `  private async handleIncomingStreams(): Promise<void> {
    if (!this.transport) return;

    const reader = this.transport.incomingBidirectionalStreams.getReader();
    
    while (true) {
      try {
        const { done, value } = await reader.read();
        if (done) break;
        
        // Handle incoming stream
        this.handleStream(value);
      } catch (error) {
        this.emit('error', error);
        break;
      }
    }
  }

  private async handleStream(stream: WebTransportBidirectionalStream): Promise<void> {
    try {
      const reader = stream.readable.getReader();
      const decoder = new TextDecoder();
      let data = '';

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        data += decoder.decode(value, { stream: true });
      }

      reader.releaseLock();
      
      // TODO: Process stream data based on your application logic
      this.emit('stream', JSON.parse(data));
    } catch (error) {
      this.emit('error', error);
    }
  }

`
}

// generateStateManagement generates state management methods.
func (w *WebTransportGenerator) generateStateManagement() string {
	return `  /**
   * Register a handler for state changes
   */
  onStateChange(handler: (state: WebTransportState) => void): void {
    this.on('stateChange', handler);
  }

`
}

// generateErrorHandling generates error handling methods.
func (w *WebTransportGenerator) generateErrorHandling() string {
	return `  /**
   * Register an error handler
   */
  onError(handler: (error: Error) => void): void {
    this.on('error', handler);
  }

`
}

// generateReconnection generates reconnection logic.
func (w *WebTransportGenerator) generateReconnection() string {
	return `  private async reconnect(): Promise<void> {
    if (this.reconnectAttempts >= this.maxReconnectAttempts) {
      this.setState(WebTransportState.CLOSED);
      return;
    }

    this.setState(WebTransportState.RECONNECTING);
    this.reconnectAttempts++;

    const delay = Math.min(
      this.reconnectDelay * Math.pow(2, this.reconnectAttempts - 1),
      this.maxReconnectDelay
    );

    await new Promise(resolve => setTimeout(resolve, delay));

    try {
      await this.connect();
    } catch (error) {
      // Will retry via connection closure handler
    }
  }

`
}

// generateClassName generates a class name for a WebTransport endpoint.
func (w *WebTransportGenerator) generateClassName(wt client.WebTransportEndpoint) string {
	if wt.ID != "" {
		return w.toPascalCase(wt.ID) + "WTClient"
	}

	return "WebTransportClient"
}

// getSchemaTypeName gets the type name for a schema.
func (w *WebTransportGenerator) getSchemaTypeName(schema *client.Schema, spec *client.APISpec) string {
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
func (w *WebTransportGenerator) toPascalCase(str string) string {
	parts := strings.FieldsFunc(str, func(r rune) bool {
		return r == '_' || r == '-' || r == ' '
	})

	var result string
	var resultSb468 strings.Builder

	for _, part := range parts {
		if len(part) > 0 {
			resultSb468.WriteString(strings.ToUpper(part[:1]) + strings.ToLower(part[1:]))
		}
	}
	result += resultSb468.String()

	return result
}
