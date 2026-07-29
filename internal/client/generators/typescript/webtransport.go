package typescript

import (
	"fmt"
	"sort"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// WebTransportGenerator generates TypeScript WebTransport client code.
type WebTransportGenerator struct {
	// warnings accumulates generation-time messages that don't abort
	// generation but are worth surfacing -- one per bidirectional-stream,
	// unidirectional-stream, or datagram schema whose schema could not be
	// resolved to a codec-table id (see wtCodecRef). Reset at the start of
	// each Generate call so a reused *WebTransportGenerator never leaks a
	// prior call's warnings into the next one -- mirrors
	// WebSocketGenerator.warnings (websocket.go) and SSEGenerator.warnings
	// (sse.go) exactly.
	warnings []string
}

// NewWebTransportGenerator creates a new WebTransport generator.
func NewWebTransportGenerator() *WebTransportGenerator {
	return &WebTransportGenerator{}
}

// Generate generates the WebTransport clients. The second return value lists
// generation-time warnings -- mirroring WebSocketGenerator.Generate's and
// SSEGenerator.Generate's own (string, []string) shape -- currently one per
// stream/datagram schema whose declared TypeScript type could not be
// resolved to a codec-table id: the generated message type
// (getSchemaTypeName) still declares a camelCase TypeScript shape, but
// nothing will actually rename the payload at runtime, which must be
// visible, not silent.
func (w *WebTransportGenerator) Generate(spec *client.APISpec, config client.GeneratorConfig) (string, []string) {
	w.warnings = nil

	var buf strings.Builder

	buf.WriteString(w.generateHeader())
	buf.WriteString("\n")
	buf.WriteString(w.generateTypes(config))
	buf.WriteString("\n")

	buf.WriteString("import * as types from './types';\n")

	// codecsNeeded gates this import exactly as websocket.go's and sse.go's
	// own gates do: under NamingPreserve with no FieldOverrides,
	// generator.go never emits src/codecs.ts at all (see codecsNeeded's doc
	// comment, fieldname.go), so an unconditional import here would dangle
	// and fail tsc (TS2307 "Cannot find module './codecs'"). Stream/datagram
	// schema codec ids are only resolved below when this is true, so
	// encode()/decode() are only ever referenced when the import exists.
	needsCodecs := codecsNeeded(config)
	if needsCodecs {
		buf.WriteString("import { decode, encode } from './codecs';\n")
	}

	buf.WriteString("\n")

	// Generate client for each WebTransport endpoint
	for _, wt := range spec.WebTransports {
		clientCode := w.generateWebTransportClient(wt, spec, config, needsCodecs)
		buf.WriteString(clientCode)
		buf.WriteString("\n")
	}

	sort.Strings(w.warnings)

	return buf.String(), w.warnings
}

// wtLabel returns a short, human-identifiable name for a WebTransport
// endpoint, for use in a generation-time warning -- mirrors rest.go's
// endpointLabel, websocket.go's wsLabel, and sse.go's sseLabel.
func wtLabel(wt client.WebTransportEndpoint) string {
	if wt.ID != "" {
		return wt.ID
	}

	return wt.Path
}

// wtCodecRef returns the codec table id (see schemaCodecRef, rest.go) for one
// WebTransport bidirectional-stream, unidirectional-stream, or datagram
// schema, and a warning to append to WebTransportGenerator.warnings when one
// is needed. kind describes which schema this is (e.g. "bidirectional-stream
// send message"), used only to make the warning readable -- mirrors
// websocket.go's messageCodecRef and sse.go's sseEventCodecRef.
//
// A nil schema needs no warning: getSchemaTypeName renders it as "any",
// which makes no renamed-shape promise for encode/decode to fail to honor. A
// schema that resolves (a direct $ref, or an array of one) gets its id
// silently. Anything else -- an inline object, oneOf/anyOf, allOf -- warns:
// the message type is still declared in its camelCase TypeScript shape, but
// it will be sent/received wire-cased, unrenamed, because there is no
// codec-table entry to encode/decode it with. Silence there would reproduce
// exactly the regression this function exists to fix.
func wtCodecRef(schema *client.Schema, wt client.WebTransportEndpoint, kind string) (id string, warning string) {
	if schema == nil {
		return "", ""
	}

	if ref := schemaCodecRef(schema); ref != "" {
		return ref, ""
	}

	return "", fmt.Sprintf(
		"webtransport endpoint %q: %s schema is not a direct $ref (or an array of one) to a named component schema -- the generated message type is still declared in its camelCase TypeScript shape, but it will be sent/received wire-cased, unrenamed, because there is no codec-table entry to encode/decode it with",
		wtLabel(wt), kind)
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

	// Gated on config.IncludeAuth exactly like every other generator in this
	// package (websocket.go, sse.go, rooms.go, presence.go, typing.go,
	// channels.go, streaming_client.go, testing.go, and generator.go's own
	// ClientConfig.auth) -- generator.go's generateTypes (types.ts) only ever
	// emits `export interface AuthConfig` when config.IncludeAuth is true
	// (see its doc comment at generator.go:17-25), so an unconditional
	// `auth?: types.AuthConfig;` reference here would dangle whenever a
	// caller disables auth (e.g. the "no-auth-streaming"/"no-auth-ws-sse"
	// fixtures' own config, or any hand-built GeneratorConfig that never sets
	// IncludeAuth at all -- its zero value is false). Was unconditional
	// before this fix; caught only once a real tsc run exercised a
	// WebTransport client with auth disabled, which nothing in the corpus
	// had ever done.
	if config.IncludeAuth {
		buf.WriteString("  /** Authentication configuration */\n")
		buf.WriteString("  auth?: types.AuthConfig;\n")
	}

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
func (w *WebTransportGenerator) generateWebTransportClient(wt client.WebTransportEndpoint, spec *client.APISpec, config client.GeneratorConfig, needsCodecs bool) string {
	var buf strings.Builder

	className := w.generateClassName(wt)

	// Shared with generateBiDiStreamClass/generateUniStreamClass, which
	// derive the SAME two names for their own standalone class declarations:
	// handleIncomingBidiStreams below instantiates `${className}BiDiStream`
	// regardless of whether this endpoint declares an outgoing BiStreamSchema
	// of its own, since an incoming bidirectional stream is a connection-level
	// event, not something gated on this endpoint's own send/receive schema.
	// That is exactly why both classes are emitted unconditionally — see
	// generateIncomingStreamHandler.
	biDiStreamName := className + "BiDiStream"

	// Codec ids for every stream/datagram schema this endpoint declares,
	// resolved only when codecsNeeded(config) -- see wtCodecRef's doc
	// comment, and Generate's needsCodecs gate above around the './codecs'
	// import. Each stays "" otherwise, which makes wireEncodeExpr/
	// wireDecodeExpr below no-ops -- exactly the raw JSON.stringify/
	// JSON.parse casts that shipped before this fix.
	var biSendCodecID, biReceiveCodecID string
	if needsCodecs && wt.BiStreamSchema != nil {
		var warning string

		biSendCodecID, warning = wtCodecRef(wt.BiStreamSchema.SendSchema, wt, "bidirectional-stream send message")
		if warning != "" {
			w.warnings = append(w.warnings, warning)
		}

		biReceiveCodecID, warning = wtCodecRef(wt.BiStreamSchema.ReceiveSchema, wt, "bidirectional-stream receive message")
		if warning != "" {
			w.warnings = append(w.warnings, warning)
		}
	}

	var uniSendCodecID, uniReceiveCodecID string
	if needsCodecs && wt.UniStreamSchema != nil {
		var warning string

		uniSendCodecID, warning = wtCodecRef(wt.UniStreamSchema.SendSchema, wt, "unidirectional-stream send message")
		if warning != "" {
			w.warnings = append(w.warnings, warning)
		}

		uniReceiveCodecID, warning = wtCodecRef(wt.UniStreamSchema.ReceiveSchema, wt, "unidirectional-stream receive message")
		if warning != "" {
			w.warnings = append(w.warnings, warning)
		}
	}

	var datagramCodecID string
	if needsCodecs && wt.DatagramSchema != nil {
		var warning string

		datagramCodecID, warning = wtCodecRef(wt.DatagramSchema, wt, "datagram")
		if warning != "" {
			w.warnings = append(w.warnings, warning)
		}
	}

	// Incoming unidirectional streams (server -> client, handled by
	// handleIncomingUniStreams/processIncomingUniStream below) are typed from
	// UniStreamSchema.ReceiveSchema -- previously unused anywhere in this
	// generator despite being a live IR field (ir.go's StreamSchema), which
	// left every incoming uni-stream hardcoded as `any` and its payload a
	// raw, un-decoded JSON.parse. A nil ReceiveSchema (no endpoint declares
	// one) keeps that exact previous behavior: getSchemaTypeName renders
	// "any", and uniReceiveCodecID stays "", so wireDecodeExpr is a no-op.
	uniReceiveType := "any"

	var uniReceiveSchema *client.Schema
	if wt.UniStreamSchema != nil {
		uniReceiveSchema = wt.UniStreamSchema.ReceiveSchema
	}

	if uniReceiveSchema != nil {
		uniReceiveType = w.getSchemaTypeName(uniReceiveSchema, spec)
	}

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

	// Add auth to URL. Gated on config.IncludeAuth for the same reason the
	// WebTransportClientConfig.auth field declaration itself is: referencing
	// this.config.auth when the interface never declares an `auth` property
	// (config.IncludeAuth false) is a dangling property access, not merely
	// stylistic -- see generateTypes' own IncludeAuth gate.
	if config.IncludeAuth {
		buf.WriteString("    // Add auth to URL if provided\n")
		buf.WriteString("    if (this.config.auth?.bearerToken) {\n")
		buf.WriteString("      const separator = wtURL.includes('?') ? '&' : '?';\n")
		buf.WriteString("      wtURL += `${separator}token=${encodeURIComponent(this.config.auth.bearerToken)}`;\n")
		buf.WriteString("    }\n\n")
	}

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

	// Bidirectional and unidirectional stream wrapper classes (BiDiStream,
	// UniStream) are collected separately from the outer client class body:
	// they must be emitted as their own top-level `class` declarations, AFTER
	// this class's closing brace, not spliced in before it. A `class`
	// statement is not a legal class-body member in TypeScript/JavaScript --
	// only property and method declarations are -- so embedding one directly
	// inside `export class DataWTClient extends EventEmitter { ... }` is a
	// parse error, not merely a type error: tsc reports a cascade of
	// "Unexpected token"/"Declaration or statement expected" diagnostics from
	// the point of the nested `class` keyword onward, and esbuild's parser
	// (which runNodeDriver's bundling step depends on) rejects the same input
	// outright ("Expected \";\" but found \"BiDiStream\""), so this was never
	// actually possible to bundle or execute, only to generate as a Go
	// string. This is a pre-existing defect, independent of the codec/rename
	// fix below -- naming each class "<className>BiDiStream"/
	// "<className>UniStream" additionally disambiguates the two wrapper
	// classes per WebTransport endpoint (a spec with more than one
	// WebTransport endpoint, each declaring its own BiStreamSchema, would
	// otherwise redeclare a single top-level `class BiDiStream` twice).
	var auxClasses strings.Builder

	// The BiDiStream and UniStream wrapper classes are ALWAYS emitted, for
	// EVERY WebTransport endpoint, regardless of whether wt.BiStreamSchema/
	// wt.UniStreamSchema is nil. handleIncomingBidiStreams/
	// handleIncomingUniStreams below run unconditionally too (a connection
	// can receive a bidi/uni stream the server opened, independent of
	// whether THIS endpoint's own spec declares an outgoing schema for
	// opening one itself -- see biDiStreamName's doc comment above), so they
	// always need a class to instantiate. Before this, the class was only
	// emitted when the corresponding schema was non-nil, which left every
	// WebTransport endpoint that declares ONLY a DatagramSchema (the single
	// most idiomatic WebTransport shape -- unreliable datagrams are the
	// transport's headline feature) with a dangling reference to an
	// undeclared class: `new DataWTClientBiDiStream(value)` with no
	// `class DataWTClientBiDiStream` anywhere in the file (TS2304 "Cannot
	// find name"). generateBiDiStreamClass/generateUniStreamClass accept a
	// nil schema and render send/receive as `any` in that case -- the
	// wrapper still WORKS at runtime (JSON.parse/JSON.stringify with no
	// codec, exactly like any other unresolved schema), it just makes no
	// renamed-shape promise the type checker has to honor.
	//
	// openBidiStream()/openUniStream() -- the methods that let THIS
	// endpoint's own client CREATE a new outgoing stream -- remain gated on
	// wt.BiStreamSchema/wt.UniStreamSchema != nil: unlike the incoming-stream
	// handlers, opening a stream is this endpoint's own declared capability,
	// not connection-level infrastructure every endpoint gets for free.
	auxClasses.WriteString(w.generateBiDiStreamClass(wt.BiStreamSchema, spec, biSendCodecID, biReceiveCodecID, className))

	if wt.BiStreamSchema != nil {
		buf.WriteString(w.generateOpenBidiStreamMethod(className))
	}

	auxClasses.WriteString(w.generateUniStreamClass(wt.UniStreamSchema, spec, uniSendCodecID, className))

	if wt.UniStreamSchema != nil {
		buf.WriteString(w.generateOpenUniStreamMethod(className))
	}

	// Datagram methods with queue
	if wt.DatagramSchema != nil {
		buf.WriteString(w.generateDatagramMethods(wt.DatagramSchema, spec, config, datagramCodecID))
	}

	// Queue management
	buf.WriteString(w.generateQueueMethods())

	// Handle incoming streams
	buf.WriteString(w.generateIncomingStreamHandler(uniReceiveCodecID, biDiStreamName))

	// State management
	buf.WriteString(w.generateStateManagement(uniReceiveType, biDiStreamName))

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

	buf.WriteString("}\n\n")
	buf.WriteString(auxClasses.String())

	return buf.String()
}

// generateOpenBidiStreamMethod generates the openBidiStream() method body,
// meant to be embedded inside the outer client class. Only called when
// wt.BiStreamSchema != nil (see generateWebTransportClient) -- opening a new
// outgoing bidirectional stream is this endpoint's own declared capability,
// unlike the BiDiStream class itself (generateBiDiStreamClass), which is
// always emitted because incoming bidi streams are connection-level.
func (w *WebTransportGenerator) generateOpenBidiStreamMethod(className string) string {
	biDiStreamName := className + "BiDiStream"

	var buf strings.Builder

	buf.WriteString("  /**\n")
	buf.WriteString("   * Open a new bidirectional stream.\n")
	buf.WriteString(fmt.Sprintf("   * @returns Promise resolving to a %s instance\n", biDiStreamName))
	buf.WriteString("   * @throws Error if not connected or operation times out\n")
	buf.WriteString("   */\n")
	buf.WriteString(fmt.Sprintf("  async openBidiStream(): Promise<%s> {\n", biDiStreamName))
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
	buf.WriteString(fmt.Sprintf("    return new %s(stream);\n", biDiStreamName))
	buf.WriteString("  }\n\n")

	return buf.String()
}

// generateBiDiStreamClass generates the standalone top-level
// `class <className>BiDiStream { ... }` declaration, meant to be emitted
// AFTER the outer client class's closing brace (see generateWebTransportClient's
// auxClasses doc comment for why this can no longer be spliced inside it).
//
// Called UNCONDITIONALLY for every WebTransport endpoint, even when schema is
// nil: handleIncomingBidiStreams (generateIncomingStreamHandler) instantiates
// this class whenever the connection receives a server-initiated
// bidirectional stream, which can happen regardless of whether THIS
// endpoint's own spec declares an outgoing BiStreamSchema. A nil schema
// renders send/receive as "any" (getSchemaTypeName's own nil-schema
// behavior) and sendCodecID/receiveCodecID are "" in that case too (see
// generateWebTransportClient: they are only resolved when
// wt.BiStreamSchema != nil), so wireEncodeExpr/wireDecodeExpr degrade to the
// plain, un-decoded JSON.stringify/JSON.parse the "any" type makes no
// promise to rename anyway.
func (w *WebTransportGenerator) generateBiDiStreamClass(schema *client.StreamSchema, spec *client.APISpec, sendCodecID, receiveCodecID, className string) string {
	biDiStreamName := className + "BiDiStream"

	sendType := "any"
	receiveType := "any"

	if schema != nil {
		sendType = w.getSchemaTypeName(schema.SendSchema, spec)
		receiveType = w.getSchemaTypeName(schema.ReceiveSchema, spec)
	}

	return fmt.Sprintf(`/**
 * Bidirectional stream wrapper for typed send/receive operations.
 */
class %s {
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
    const data = encoder.encode(JSON.stringify(%s));
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

    return %s;
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
          yield %s;
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

`, biDiStreamName, sendType, wireEncodeExpr(sendCodecID, "msg"), receiveType, wireDecodeExpr(receiveCodecID, "JSON.parse(result)"), receiveType, wireDecodeExpr(receiveCodecID, "JSON.parse(line)"))
}

// generateOpenUniStreamMethod generates the openUniStream() method body,
// meant to be embedded inside the outer client class. Only called when
// wt.UniStreamSchema != nil (see generateWebTransportClient) -- mirrors
// generateOpenBidiStreamMethod's own reasoning.
func (w *WebTransportGenerator) generateOpenUniStreamMethod(className string) string {
	uniStreamName := className + "UniStream"

	var buf strings.Builder

	buf.WriteString("  /**\n")
	buf.WriteString("   * Open a new unidirectional stream for sending.\n")
	buf.WriteString(fmt.Sprintf("   * @returns Promise resolving to a %s instance\n", uniStreamName))
	buf.WriteString("   * @throws Error if not connected or operation times out\n")
	buf.WriteString("   */\n")
	buf.WriteString(fmt.Sprintf("  async openUniStream(): Promise<%s> {\n", uniStreamName))
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
	buf.WriteString(fmt.Sprintf("    return new %s(stream);\n", uniStreamName))
	buf.WriteString("  }\n\n")

	return buf.String()
}

// generateUniStreamClass generates the standalone top-level
// `class <className>UniStream { ... }` declaration, meant to be emitted
// AFTER the outer client class's closing brace.
//
// Unlike generateBiDiStreamClass, UniStream has no unconditionally-emitted
// caller today: incoming unidirectional streams are handled directly as raw
// ReadableStream bytes by processIncomingUniStream
// (generateIncomingStreamHandler), which never constructs a UniStream
// instance -- only openUniStream() does, and that stays gated on
// wt.UniStreamSchema != nil. This is still called UNCONDITIONALLY, for
// symmetry with generateBiDiStreamClass and to keep
// generateWebTransportClient's aux-classes wiring uniform between the two
// wrapper kinds, rather than because a dangling reference has been observed
// here the way it was for BiDiStream. An unreferenced, un-exported top-level
// class is inert (dead code, not a compile error -- this package's
// tsconfig.json does not set noUnusedLocals), so this costs nothing when
// wt.UniStreamSchema is nil.
func (w *WebTransportGenerator) generateUniStreamClass(schema *client.StreamSchema, spec *client.APISpec, sendCodecID, className string) string {
	uniStreamName := className + "UniStream"

	sendType := "any"
	if schema != nil {
		sendType = w.getSchemaTypeName(schema.SendSchema, spec)
	}

	return fmt.Sprintf(`/**
 * Unidirectional stream wrapper for typed send operations.
 */
class %s {
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
    const data = encoder.encode(JSON.stringify(%s));
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

`, uniStreamName, sendType, wireEncodeExpr(sendCodecID, "msg"))
}

// generateDatagramMethods generates datagram methods with offline queue.
func (w *WebTransportGenerator) generateDatagramMethods(schema *client.Schema, spec *client.APISpec, config client.GeneratorConfig, codecID string) string {
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
	buf.WriteString(fmt.Sprintf("    const data = encoder.encode(JSON.stringify(%s));\n\n", wireEncodeExpr(codecID, "msg")))

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
	buf.WriteString(fmt.Sprintf("    const data = encoder.encode(JSON.stringify(%s));\n", wireEncodeExpr(codecID, "msg")))
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
	buf.WriteString(fmt.Sprintf("      return %s;\n", wireDecodeExpr(codecID, "JSON.parse(text)")))
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
	buf.WriteString(fmt.Sprintf("        yield %s;\n", wireDecodeExpr(codecID, "JSON.parse(text)")))
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
// biDiStreamName is the endpoint-specific `class` name generateBiDiStreamClass
// declares (className + "BiDiStream") -- handleIncomingBidiStreams
// instantiates it by name, so the two must agree.
//
// Both this function AND generateBiDiStreamClass run unconditionally. They
// used to disagree: this handler always referenced the class while the class
// itself was only emitted when wt.BiStreamSchema != nil, so a datagram-only
// endpoint -- the most idiomatic WebTransport shape -- generated
// `TS2304: Cannot find name '<X>BiDiStream'` and did not compile. Emitting
// the class unconditionally (typed `any` when no schema is declared) is the
// fix, because an incoming bidirectional stream is a connection-level event
// that arrives whether or not this endpoint declares an outgoing schema.
func (w *WebTransportGenerator) generateIncomingStreamHandler(uniReceiveCodecID, biDiStreamName string) string {
	return fmt.Sprintf(`  private async handleIncomingStreams(): Promise<void> {
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
        this.emit('incomingBidiStream', new %s(value));
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
        this.emit('incomingUniStream', %s);
      }
    } catch (error) {
      this.emit('error', error);
    } finally {
      reader.releaseLock();
    }
  }

`, biDiStreamName, wireDecodeExpr(uniReceiveCodecID, "JSON.parse(data)"))
}

// generateStateManagement generates state management methods. biDiStreamName
// is the same endpoint-specific class name generateIncomingStreamHandler uses
// (see its own doc comment) -- onIncomingBidiStream's handler signature must
// reference the same declared class.
func (w *WebTransportGenerator) generateStateManagement(uniReceiveType, biDiStreamName string) string {
	return fmt.Sprintf(`  /**
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
  onIncomingBidiStream(handler: (stream: %s) => void): void {
    this.on('incomingBidiStream', handler);
  }

  /**
   * Register a handler for incoming unidirectional stream data.
   * @param handler - Function to call when uni stream data is received
   */
  onIncomingUniStream(handler: (data: %s) => void): void {
    this.on('incomingUniStream', handler);
  }

  /**
   * Register a handler for connection close.
   * @param handler - Function to call when connection closes
   */
  onClose(handler: () => void): void {
    this.on('close', handler);
  }

`, biDiStreamName, uniReceiveType)
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
	return toPascal(str)
}
