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

	buf.WriteString(w.generateHeader())
	buf.WriteString("\n")
	buf.WriteString(w.generateTypes(config))
	buf.WriteString("\n")

	buf.WriteString("import * as types from './types';\n\n")

	// Generate client for each WebTransport endpoint
	for _, wt := range spec.WebTransports {
		clientCode := w.generateWebTransportClient(wt, spec, config)
		buf.WriteString(clientCode)
		buf.WriteString("\n")
	}

	return buf.String()
}

// generateHeader generates the header with environment detection.
func (w *WebTransportGenerator) generateHeader() string {
	var buf strings.Builder

	buf.WriteString("// WebTransport client - requires browser with WebTransport support or Node.js 20+\n\n")

	// Check for WebTransport support
	buf.WriteString("// Check WebTransport support\n")
	buf.WriteString("const isWebTransportSupported = typeof WebTransport !== 'undefined';\n\n")

	// Simple EventEmitter for cross-platform support
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

// generateTypes generates WebTransport-specific types.
func (w *WebTransportGenerator) generateTypes(config client.GeneratorConfig) string {
	var buf strings.Builder

	// ConnectionState enum
	buf.WriteString("\n/** WebTransport connection state */\n")
	buf.WriteString("export enum WebTransportState {\n")
	buf.WriteString("  DISCONNECTED = 'disconnected',\n")
	buf.WriteString("  CONNECTING = 'connecting',\n")
	buf.WriteString("  CONNECTED = 'connected',\n")
	buf.WriteString("  RECONNECTING = 'reconnecting',\n")
	buf.WriteString("  CLOSED = 'closed',\n")
	buf.WriteString("  ERROR = 'error',\n")
	buf.WriteString("}\n\n")

	// Config interface
	buf.WriteString("/** Configuration for WebTransport client */\n")
	buf.WriteString("export interface WebTransportClientConfig {\n")
	buf.WriteString("  /** Base URL for WebTransport connection */\n")
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

	buf.WriteString("  /** Enable offline datagram queue (default: true) */\n")
	buf.WriteString("  enableOfflineQueue?: boolean;\n")
	buf.WriteString("  /** Maximum datagrams in offline queue (default: 100) */\n")
	buf.WriteString("  maxQueueSize?: number;\n")
	buf.WriteString("  /** Datagram TTL in queue in ms (default: 30000) */\n")
	buf.WriteString("  queueDatagramTTL?: number;\n")
	buf.WriteString("}\n\n")

	// QueuedDatagram type
	buf.WriteString("/** Datagram queued for sending when offline */\n")
	buf.WriteString("interface QueuedDatagram {\n")
	buf.WriteString("  data: Uint8Array;\n")
	buf.WriteString("  timestamp: number;\n")
	buf.WriteString("  resolve: () => void;\n")
	buf.WriteString("  reject: (error: Error) => void;\n")
	buf.WriteString("}\n")

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

	buf.WriteString(" * \n")
	buf.WriteString(" * Features:\n")
	buf.WriteString(" * - Bidirectional streams for reliable ordered data\n")
	buf.WriteString(" * - Unidirectional streams for one-way data\n")
	buf.WriteString(" * - Datagrams for unreliable low-latency data\n")
	buf.WriteString(" * - Connection timeouts\n")

	if config.Features.Reconnection {
		buf.WriteString(" * - Automatic reconnection with exponential backoff\n")
	}

	buf.WriteString(" * \n")
	buf.WriteString(" * @note Requires browser with WebTransport support or Node.js 20+\n")
	buf.WriteString(" */\n")

	// Class definition
	buf.WriteString(fmt.Sprintf("export class %s extends EventEmitter {\n", className))
	buf.WriteString("  private transport: WebTransport | null = null;\n")
	buf.WriteString("  private config: Required<Pick<WebTransportClientConfig, 'baseURL'>> & WebTransportClientConfig;\n")
	buf.WriteString("  private state: WebTransportState = WebTransportState.DISCONNECTED;\n")
	buf.WriteString("  private closed: boolean = false;\n")
	buf.WriteString("  private connectionTimeoutId: ReturnType<typeof setTimeout> | null = null;\n")
	buf.WriteString("  private datagramQueue: QueuedDatagram[] = [];\n")

	if config.Features.Reconnection {
		buf.WriteString("  private reconnectAttempts: number = 0;\n")
		buf.WriteString("  private reconnectTimeoutId: ReturnType<typeof setTimeout> | null = null;\n")
	}

	buf.WriteString("\n")

	// Constructor
	buf.WriteString("  constructor(config: WebTransportClientConfig) {\n")
	buf.WriteString("    super();\n")
	buf.WriteString("    \n")
	buf.WriteString("    if (!isWebTransportSupported) {\n")
	buf.WriteString("      throw new Error('WebTransport is not supported in this environment');\n")
	buf.WriteString("    }\n")
	buf.WriteString("    \n")
	buf.WriteString("    this.config = {\n")
	buf.WriteString("      connectionTimeout: 30000,\n")
	buf.WriteString("      requestTimeout: 10000,\n")

	if config.Features.Reconnection {
		buf.WriteString("      maxReconnectAttempts: 10,\n")
		buf.WriteString("      reconnectDelay: 1000,\n")
		buf.WriteString("      maxReconnectDelay: 30000,\n")
	}

	buf.WriteString("      enableOfflineQueue: true,\n")
	buf.WriteString("      maxQueueSize: 100,\n")
	buf.WriteString("      queueDatagramTTL: 30000,\n")
	buf.WriteString("      ...config,\n")
	buf.WriteString("    };\n")
	buf.WriteString("  }\n\n")

	// Connect method with timeout
	buf.WriteString("  /**\n")
	buf.WriteString(fmt.Sprintf("   * Connect to WebTransport endpoint %s\n", wt.Path))
	buf.WriteString("   * @returns Promise that resolves when connected\n")
	buf.WriteString("   * @throws Error if connection fails or times out\n")
	buf.WriteString("   */\n")
	buf.WriteString("  async connect(): Promise<void> {\n")
	buf.WriteString("    if (this.state === WebTransportState.CONNECTED) {\n")
	buf.WriteString("      return;\n")
	buf.WriteString("    }\n\n")

	buf.WriteString("    this.setState(WebTransportState.CONNECTING);\n")
	buf.WriteString("    this.closed = false;\n\n")

	buf.WriteString(fmt.Sprintf("    let wtURL = this.config.baseURL.replace(/^http/, 'https') + '%s';\n\n", wt.Path))

	// Add auth to URL
	buf.WriteString("    // Add auth to URL if provided\n")
	buf.WriteString("    if (this.config.auth?.bearerToken) {\n")
	buf.WriteString("      const separator = wtURL.includes('?') ? '&' : '?';\n")
	buf.WriteString("      wtURL += `${separator}token=${encodeURIComponent(this.config.auth.bearerToken)}`;\n")
	buf.WriteString("    }\n\n")

	// Create connection with timeout
	buf.WriteString("    // Create transport with timeout\n")
	buf.WriteString("    const connectPromise = new Promise<void>(async (resolve, reject) => {\n")
	buf.WriteString("      this.connectionTimeoutId = setTimeout(() => {\n")
	buf.WriteString("        this.connectionTimeoutId = null;\n")
	buf.WriteString("        if (this.transport) {\n")
	buf.WriteString("          this.transport.close();\n")
	buf.WriteString("          this.transport = null;\n")
	buf.WriteString("        }\n")
	buf.WriteString("        this.setState(WebTransportState.ERROR);\n")
	buf.WriteString("        reject(new Error('Connection timeout'));\n")
	buf.WriteString("      }, this.config.connectionTimeout);\n\n")

	buf.WriteString("      try {\n")
	buf.WriteString("        this.transport = new WebTransport(wtURL);\n")
	buf.WriteString("        await this.transport.ready;\n\n")

	buf.WriteString("        this.clearConnectionTimeout();\n")
	buf.WriteString("        this.setState(WebTransportState.CONNECTED);\n")

	if config.Features.Reconnection {
		buf.WriteString("        this.reconnectAttempts = 0;\n")
	}

	buf.WriteString("        this.flushDatagramQueue();\n\n")

	buf.WriteString("        // Start handling incoming streams\n")
	buf.WriteString("        this.handleIncomingStreams();\n\n")

	buf.WriteString("        // Handle connection closure\n")
	buf.WriteString("        this.transport.closed\n")
	buf.WriteString("          .then(() => {\n")
	buf.WriteString("            if (this.closed) {\n")
	buf.WriteString("              this.setState(WebTransportState.CLOSED);\n")
	buf.WriteString("            } else {\n")
	buf.WriteString("              this.setState(WebTransportState.DISCONNECTED);\n")

	if config.Features.Reconnection {
		buf.WriteString("              this.scheduleReconnect();\n")
	}

	buf.WriteString("            }\n")
	buf.WriteString("            this.emit('close');\n")
	buf.WriteString("          })\n")
	buf.WriteString("          .catch((error) => {\n")
	buf.WriteString("            this.setState(WebTransportState.ERROR);\n")
	buf.WriteString("            this.emit('error', error);\n")

	if config.Features.Reconnection {
		buf.WriteString("            if (!this.closed) {\n")
		buf.WriteString("              this.scheduleReconnect();\n")
		buf.WriteString("            }\n")
	}

	buf.WriteString("          });\n\n")

	buf.WriteString("        resolve();\n")
	buf.WriteString("      } catch (error) {\n")
	buf.WriteString("        this.clearConnectionTimeout();\n")
	buf.WriteString("        this.setState(WebTransportState.ERROR);\n")
	buf.WriteString("        reject(error);\n")
	buf.WriteString("      }\n")
	buf.WriteString("    });\n\n")

	buf.WriteString("    return connectPromise;\n")
	buf.WriteString("  }\n\n")

	buf.WriteString("  private clearConnectionTimeout(): void {\n")
	buf.WriteString("    if (this.connectionTimeoutId) {\n")
	buf.WriteString("      clearTimeout(this.connectionTimeoutId);\n")
	buf.WriteString("      this.connectionTimeoutId = null;\n")
	buf.WriteString("    }\n")
	buf.WriteString("  }\n\n")

	// Bidirectional stream methods
	if wt.BiStreamSchema != nil {
		buf.WriteString(w.generateBiStreamMethods(wt.BiStreamSchema, spec, config))
	}

	// Unidirectional stream methods
	if wt.UniStreamSchema != nil {
		buf.WriteString(w.generateUniStreamMethods(wt.UniStreamSchema, spec, config))
	}

	// Datagram methods with queue
	if wt.DatagramSchema != nil {
		buf.WriteString(w.generateDatagramMethods(wt.DatagramSchema, spec, config))
	}

	// Queue management
	buf.WriteString(w.generateQueueMethods())

	// Handle incoming streams
	buf.WriteString(w.generateIncomingStreamHandler())

	// State management
	buf.WriteString(w.generateStateManagement())

	// Error handling
	buf.WriteString(w.generateErrorHandling())

	// Reconnection
	if config.Features.Reconnection {
		buf.WriteString(w.generateReconnection())
	}

	// Close method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Close the WebTransport connection.\n")
	buf.WriteString("   * @param rejectQueuedDatagrams - If true, reject all queued datagrams (default: false)\n")
	buf.WriteString("   */\n")
	buf.WriteString("  close(rejectQueuedDatagrams: boolean = false): void {\n")
	buf.WriteString("    this.closed = true;\n")
	buf.WriteString("    this.clearConnectionTimeout();\n")

	if config.Features.Reconnection {
		buf.WriteString("    this.cancelReconnect();\n")
	}

	buf.WriteString("\n")
	buf.WriteString("    if (rejectQueuedDatagrams) {\n")
	buf.WriteString("      this.rejectAllQueuedDatagrams(new Error('Connection closed'));\n")
	buf.WriteString("    }\n\n")

	buf.WriteString("    if (this.transport) {\n")
	buf.WriteString("      this.transport.close();\n")
	buf.WriteString("      this.transport = null;\n")
	buf.WriteString("    }\n")
	buf.WriteString("    this.setState(WebTransportState.CLOSED);\n")
	buf.WriteString("  }\n\n")

	// Get state method
	buf.WriteString("  /**\n")
	buf.WriteString("   * Get the current connection state.\n")
	buf.WriteString("   */\n")
	buf.WriteString("  getState(): WebTransportState {\n")
	buf.WriteString("    return this.state;\n")
	buf.WriteString("  }\n\n")

	// isConnected helper
	buf.WriteString("  /**\n")
	buf.WriteString("   * Check if the WebTransport is currently connected.\n")
	buf.WriteString("   */\n")
	buf.WriteString("  isConnected(): boolean {\n")
	buf.WriteString("    return this.state === WebTransportState.CONNECTED;\n")
	buf.WriteString("  }\n\n")

	// setState helper
	buf.WriteString("  private setState(state: WebTransportState): void {\n")
	buf.WriteString("    if (this.state !== state) {\n")
	buf.WriteString("      this.state = state;\n")
	buf.WriteString("      this.emit('stateChange', state);\n")
	buf.WriteString("    }\n")
	buf.WriteString("  }\n")

	buf.WriteString("}\n")

	return buf.String()
}

// generateBiStreamMethods generates bidirectional stream methods.
func (w *WebTransportGenerator) generateBiStreamMethods(schema *client.StreamSchema, spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	sendType := w.getSchemaTypeName(schema.SendSchema, spec)
	receiveType := w.getSchemaTypeName(schema.ReceiveSchema, spec)

	buf.WriteString("  /**\n")
	buf.WriteString("   * Open a new bidirectional stream.\n")
	buf.WriteString("   * @returns Promise resolving to a BiDiStream instance\n")
	buf.WriteString("   * @throws Error if not connected or operation times out\n")
	buf.WriteString("   */\n")
	buf.WriteString("  async openBidiStream(): Promise<BiDiStream> {\n")
	buf.WriteString("    if (!this.transport || this.state !== WebTransportState.CONNECTED) {\n")
	buf.WriteString("      throw new Error('Not connected');\n")
	buf.WriteString("    }\n\n")

	buf.WriteString("    const timeout = this.config.requestTimeout || 10000;\n")
	buf.WriteString("    const stream = await Promise.race([\n")
	buf.WriteString("      this.transport.createBidirectionalStream(),\n")
	buf.WriteString("      new Promise<never>((_, reject) => \n")
	buf.WriteString("        setTimeout(() => reject(new Error('Stream creation timeout')), timeout)\n")
	buf.WriteString("      ),\n")
	buf.WriteString("    ]);\n")
	buf.WriteString("    return new BiDiStream(stream);\n")
	buf.WriteString("  }\n\n")

	// BiDiStream class
	buf.WriteString(fmt.Sprintf(`/**
 * Bidirectional stream wrapper for typed send/receive operations.
 */
class BiDiStream {
  private stream: WebTransportBidirectionalStream;
  private writer: WritableStreamDefaultWriter | null = null;
  private reader: ReadableStreamDefaultReader | null = null;

  constructor(stream: WebTransportBidirectionalStream) {
    this.stream = stream;
  }

  /**
   * Send a message over the stream.
   * @param msg - The message to send
   */
  async send(msg: %s): Promise<void> {
    if (!this.writer) {
      this.writer = this.stream.writable.getWriter();
    }
    const encoder = new TextEncoder();
    const data = encoder.encode(JSON.stringify(msg));
    await this.writer.write(data);
  }

  /**
   * Receive a message from the stream.
   * @returns Promise resolving to the received message
   */
  async receive(): Promise<%s> {
    if (!this.reader) {
      this.reader = this.stream.readable.getReader();
    }
    const decoder = new TextDecoder();
    let result = '';

    while (true) {
      const { done, value } = await this.reader.read();
      if (done) break;
      result += decoder.decode(value, { stream: true });
    }

    return JSON.parse(result);
  }

  /**
   * Receive messages as an async iterator.
   */
  async *receiveIterator(): AsyncGenerator<%s> {
    if (!this.reader) {
      this.reader = this.stream.readable.getReader();
    }
    const decoder = new TextDecoder();
    let buffer = '';

    while (true) {
      const { done, value } = await this.reader.read();
      if (done) break;
      
      buffer += decoder.decode(value, { stream: true });
      
      // Try to parse complete JSON objects
      const lines = buffer.split('\n');
      buffer = lines.pop() || '';
      
      for (const line of lines) {
        if (line.trim()) {
          yield JSON.parse(line);
        }
      }
    }
  }

  /**
   * Close the stream.
   */
  async close(): Promise<void> {
    if (this.writer) {
      await this.writer.close();
      this.writer = null;
    }
    if (this.reader) {
      await this.reader.cancel();
      this.reader = null;
    }
  }
}

`, sendType, receiveType, receiveType))

	return buf.String()
}

// generateUniStreamMethods generates unidirectional stream methods.
func (w *WebTransportGenerator) generateUniStreamMethods(schema *client.StreamSchema, spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	sendType := w.getSchemaTypeName(schema.SendSchema, spec)

	buf.WriteString("  /**\n")
	buf.WriteString("   * Open a new unidirectional stream for sending.\n")
	buf.WriteString("   * @returns Promise resolving to a UniStream instance\n")
	buf.WriteString("   * @throws Error if not connected or operation times out\n")
	buf.WriteString("   */\n")
	buf.WriteString("  async openUniStream(): Promise<UniStream> {\n")
	buf.WriteString("    if (!this.transport || this.state !== WebTransportState.CONNECTED) {\n")
	buf.WriteString("      throw new Error('Not connected');\n")
	buf.WriteString("    }\n\n")

	buf.WriteString("    const timeout = this.config.requestTimeout || 10000;\n")
	buf.WriteString("    const stream = await Promise.race([\n")
	buf.WriteString("      this.transport.createUnidirectionalStream(),\n")
	buf.WriteString("      new Promise<never>((_, reject) => \n")
	buf.WriteString("        setTimeout(() => reject(new Error('Stream creation timeout')), timeout)\n")
	buf.WriteString("      ),\n")
	buf.WriteString("    ]);\n")
	buf.WriteString("    return new UniStream(stream);\n")
	buf.WriteString("  }\n\n")

	// UniStream class
	buf.WriteString(fmt.Sprintf(`/**
 * Unidirectional stream wrapper for typed send operations.
 */
class UniStream {
  private stream: WritableStream;
  private writer: WritableStreamDefaultWriter | null = null;

  constructor(stream: WritableStream) {
    this.stream = stream;
  }

  /**
   * Send a message over the stream.
   * @param msg - The message to send
   */
  async send(msg: %s): Promise<void> {
    if (!this.writer) {
      this.writer = this.stream.getWriter();
    }
    const encoder = new TextEncoder();
    const data = encoder.encode(JSON.stringify(msg));
    await this.writer.write(data);
  }

  /**
   * Close the stream.
   */
  async close(): Promise<void> {
    if (this.writer) {
      await this.writer.close();
      this.writer = null;
    }
  }
}

`, sendType))

	return buf.String()
}

// generateDatagramMethods generates datagram methods with offline queue.
func (w *WebTransportGenerator) generateDatagramMethods(schema *client.Schema, spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	typeName := w.getSchemaTypeName(schema, spec)

	buf.WriteString("  /**\n")
	buf.WriteString(fmt.Sprintf("   * Send a %s as an unreliable datagram.\n", typeName))
	buf.WriteString("   * If offline and queue enabled, queues for later.\n")
	buf.WriteString("   * @param msg - The message to send\n")
	buf.WriteString("   * @returns Promise that resolves when sent (or queued)\n")
	buf.WriteString("   */\n")
	buf.WriteString(fmt.Sprintf("  async sendDatagram(msg: %s): Promise<void> {\n", typeName))
	buf.WriteString("    const encoder = new TextEncoder();\n")
	buf.WriteString("    const data = encoder.encode(JSON.stringify(msg));\n\n")

	buf.WriteString("    if (this.transport && this.state === WebTransportState.CONNECTED) {\n")
	buf.WriteString("      const writer = this.transport.datagrams.writable.getWriter();\n")
	buf.WriteString("      try {\n")
	buf.WriteString("        await writer.write(data);\n")
	buf.WriteString("      } finally {\n")
	buf.WriteString("        writer.releaseLock();\n")
	buf.WriteString("      }\n")
	buf.WriteString("      return;\n")
	buf.WriteString("    }\n\n")

	buf.WriteString("    // Queue datagram for later if enabled\n")
	buf.WriteString("    if (!this.config.enableOfflineQueue) {\n")
	buf.WriteString("      throw new Error('Not connected');\n")
	buf.WriteString("    }\n\n")

	buf.WriteString("    return new Promise((resolve, reject) => {\n")
	buf.WriteString("      if (this.datagramQueue.length >= (this.config.maxQueueSize || 100)) {\n")
	buf.WriteString("        reject(new Error('Datagram queue full'));\n")
	buf.WriteString("        return;\n")
	buf.WriteString("      }\n\n")

	buf.WriteString("      this.datagramQueue.push({\n")
	buf.WriteString("        data,\n")
	buf.WriteString("        timestamp: Date.now(),\n")
	buf.WriteString("        resolve,\n")
	buf.WriteString("        reject,\n")
	buf.WriteString("      });\n")
	buf.WriteString("    });\n")
	buf.WriteString("  }\n\n")

	buf.WriteString("  /**\n")
	buf.WriteString(fmt.Sprintf("   * Send a %s immediately. Throws if not connected.\n", typeName))
	buf.WriteString("   * @param msg - The message to send\n")
	buf.WriteString("   */\n")
	buf.WriteString(fmt.Sprintf("  async sendDatagramSync(msg: %s): Promise<void> {\n", typeName))
	buf.WriteString("    if (!this.transport || this.state !== WebTransportState.CONNECTED) {\n")
	buf.WriteString("      throw new Error('Not connected');\n")
	buf.WriteString("    }\n\n")

	buf.WriteString("    const encoder = new TextEncoder();\n")
	buf.WriteString("    const data = encoder.encode(JSON.stringify(msg));\n")
	buf.WriteString("    const writer = this.transport.datagrams.writable.getWriter();\n")
	buf.WriteString("    try {\n")
	buf.WriteString("      await writer.write(data);\n")
	buf.WriteString("    } finally {\n")
	buf.WriteString("      writer.releaseLock();\n")
	buf.WriteString("    }\n")
	buf.WriteString("  }\n\n")

	buf.WriteString("  /**\n")
	buf.WriteString(fmt.Sprintf("   * Receive a %s datagram.\n", typeName))
	buf.WriteString("   * @returns Promise resolving to the received datagram\n")
	buf.WriteString("   */\n")
	buf.WriteString(fmt.Sprintf("  async receiveDatagram(): Promise<%s> {\n", typeName))
	buf.WriteString("    if (!this.transport || this.state !== WebTransportState.CONNECTED) {\n")
	buf.WriteString("      throw new Error('Not connected');\n")
	buf.WriteString("    }\n\n")

	buf.WriteString("    const reader = this.transport.datagrams.readable.getReader();\n")
	buf.WriteString("    try {\n")
	buf.WriteString("      const { value } = await reader.read();\n")
	buf.WriteString("      const decoder = new TextDecoder();\n")
	buf.WriteString("      const text = decoder.decode(value);\n")
	buf.WriteString("      return JSON.parse(text);\n")
	buf.WriteString("    } finally {\n")
	buf.WriteString("      reader.releaseLock();\n")
	buf.WriteString("    }\n")
	buf.WriteString("  }\n\n")

	buf.WriteString("  /**\n")
	buf.WriteString("   * Receive datagrams as an async iterator.\n")
	buf.WriteString("   */\n")
	buf.WriteString(fmt.Sprintf("  async *receiveDatagrams(): AsyncGenerator<%s> {\n", typeName))
	buf.WriteString("    if (!this.transport) {\n")
	buf.WriteString("      throw new Error('Not connected');\n")
	buf.WriteString("    }\n\n")

	buf.WriteString("    const reader = this.transport.datagrams.readable.getReader();\n")
	buf.WriteString("    const decoder = new TextDecoder();\n\n")

	buf.WriteString("    try {\n")
	buf.WriteString("      while (true) {\n")
	buf.WriteString("        const { done, value } = await reader.read();\n")
	buf.WriteString("        if (done) break;\n")
	buf.WriteString("        const text = decoder.decode(value);\n")
	buf.WriteString("        yield JSON.parse(text);\n")
	buf.WriteString("      }\n")
	buf.WriteString("    } finally {\n")
	buf.WriteString("      reader.releaseLock();\n")
	buf.WriteString("    }\n")
	buf.WriteString("  }\n\n")

	return buf.String()
}

// generateQueueMethods generates queue management methods.
func (w *WebTransportGenerator) generateQueueMethods() string {
	return `  /**
   * Get the number of datagrams in the offline queue.
   */
  getQueueSize(): number {
    return this.datagramQueue.length;
  }

  /**
   * Clear all datagrams from the offline queue.
   * @param rejectPending - If true, reject pending promises (default: false)
   */
  clearQueue(rejectPending: boolean = false): void {
    if (rejectPending) {
      const error = new Error('Queue cleared');
      this.datagramQueue.forEach(dg => dg.reject(error));
    }
    this.datagramQueue = [];
  }

  private flushDatagramQueue(): void {
    if (!this.transport || this.state !== WebTransportState.CONNECTED) return;

    const now = Date.now();
    const ttl = this.config.queueDatagramTTL || 30000;

    const processNext = async () => {
      while (this.datagramQueue.length > 0) {
        const dg = this.datagramQueue[0];
        
        // Check if datagram expired
        if (now - dg.timestamp > ttl) {
          this.datagramQueue.shift();
          dg.reject(new Error('Datagram expired in queue'));
          continue;
        }

        try {
          const writer = this.transport!.datagrams.writable.getWriter();
          await writer.write(dg.data);
          writer.releaseLock();
          this.datagramQueue.shift();
          dg.resolve();
        } catch (error) {
          // Stop flushing on error, will retry on next connection
          break;
        }
      }
    };

    processNext().catch(() => {});
  }

  private rejectAllQueuedDatagrams(error: Error): void {
    this.datagramQueue.forEach(dg => dg.reject(error));
    this.datagramQueue = [];
  }

`
}

// generateIncomingStreamHandler generates handler for incoming streams.
func (w *WebTransportGenerator) generateIncomingStreamHandler() string {
	return `  private async handleIncomingStreams(): Promise<void> {
    if (!this.transport) return;

    // Handle incoming bidirectional streams
    this.handleIncomingBidiStreams();
    
    // Handle incoming unidirectional streams
    this.handleIncomingUniStreams();
  }

  private async handleIncomingBidiStreams(): Promise<void> {
    if (!this.transport) return;

    const reader = this.transport.incomingBidirectionalStreams.getReader();
    
    try {
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        
        // Emit incoming stream for application handling
        this.emit('incomingBidiStream', new BiDiStream(value));
      }
    } catch (error) {
      if (!this.closed) {
        this.emit('error', error);
      }
    } finally {
      reader.releaseLock();
    }
  }

  private async handleIncomingUniStreams(): Promise<void> {
    if (!this.transport) return;

    const reader = this.transport.incomingUnidirectionalStreams.getReader();
    
    try {
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        
        // Process incoming unidirectional stream
        this.processIncomingUniStream(value);
      }
    } catch (error) {
      if (!this.closed) {
        this.emit('error', error);
      }
    } finally {
      reader.releaseLock();
    }
  }

  private async processIncomingUniStream(stream: ReadableStream): Promise<void> {
    const reader = stream.getReader();
    const decoder = new TextDecoder();
    let data = '';

    try {
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        data += decoder.decode(value, { stream: true });
      }

      if (data) {
        this.emit('incomingUniStream', JSON.parse(data));
      }
    } catch (error) {
      this.emit('error', error);
    } finally {
      reader.releaseLock();
    }
  }

`
}

// generateStateManagement generates state management methods.
func (w *WebTransportGenerator) generateStateManagement() string {
	return `  /**
   * Register a handler for state changes.
   * @param handler - Function to call when state changes
   */
  onStateChange(handler: (state: WebTransportState) => void): void {
    this.on('stateChange', handler);
  }

  /**
   * Register a handler for incoming bidirectional streams.
   * @param handler - Function to call when a bidi stream is received
   */
  onIncomingBidiStream(handler: (stream: BiDiStream) => void): void {
    this.on('incomingBidiStream', handler);
  }

  /**
   * Register a handler for incoming unidirectional stream data.
   * @param handler - Function to call when uni stream data is received
   */
  onIncomingUniStream(handler: (data: any) => void): void {
    this.on('incomingUniStream', handler);
  }

  /**
   * Register a handler for connection close.
   * @param handler - Function to call when connection closes
   */
  onClose(handler: () => void): void {
    this.on('close', handler);
  }

`
}

// generateErrorHandling generates error handling methods.
func (w *WebTransportGenerator) generateErrorHandling() string {
	return `  /**
   * Register an error handler.
   * @param handler - Function to call when an error occurs
   */
  onError(handler: (error: Error) => void): void {
    this.on('error', handler);
  }

`
}

// generateReconnection generates reconnection logic.
func (w *WebTransportGenerator) generateReconnection() string {
	return `  private scheduleReconnect(): void {
    const maxAttempts = this.config.maxReconnectAttempts || 10;
    if (this.reconnectAttempts >= maxAttempts) {
      this.setState(WebTransportState.CLOSED);
      this.rejectAllQueuedDatagrams(new Error('Max reconnection attempts reached'));
      return;
    }

    this.setState(WebTransportState.RECONNECTING);
    this.reconnectAttempts++;

    const delay = Math.min(
      (this.config.reconnectDelay || 1000) * Math.pow(2, this.reconnectAttempts - 1),
      this.config.maxReconnectDelay || 30000
    );

    this.reconnectTimeoutId = setTimeout(async () => {
      this.reconnectTimeoutId = null;
      try {
        await this.connect();
      } catch (error) {
        // Will schedule another reconnect in closed handler
      }
    }, delay);
  }

  private cancelReconnect(): void {
    if (this.reconnectTimeoutId) {
      clearTimeout(this.reconnectTimeoutId);
      this.reconnectTimeoutId = null;
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

	var resultSb strings.Builder

	for _, part := range parts {
		if len(part) > 0 {
			resultSb.WriteString(strings.ToUpper(part[:1]) + strings.ToLower(part[1:]))
		}
	}

	return resultSb.String()
}
