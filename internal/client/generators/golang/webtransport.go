package golang

import (
	"fmt"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// WebTransportGenerator generates Go WebTransport client code.
type WebTransportGenerator struct{}

// NewWebTransportGenerator creates a new WebTransport generator.
func NewWebTransportGenerator() *WebTransportGenerator {
	return &WebTransportGenerator{}
}

// Generate generates the WebTransport clients.
func (w *WebTransportGenerator) Generate(spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	// Imports
	buf.WriteString("package " + config.PackageName + "\n\n")
	buf.WriteString("import (\n")
	buf.WriteString("\t\"context\"\n")
	buf.WriteString("\t\"encoding/json\"\n")
	buf.WriteString("\t\"fmt\"\n")
	buf.WriteString("\t\"io\"\n")
	buf.WriteString("\t\"sync\"\n")
	buf.WriteString("\t\"time\"\n\n")
	buf.WriteString("\t\"github.com/quic-go/webtransport-go\"\n")
	buf.WriteString(")\n\n")

	// Connection state enum
	buf.WriteString("// WebTransportState represents the WebTransport connection state\n")
	buf.WriteString("type WebTransportState int\n\n")
	buf.WriteString("const (\n")
	buf.WriteString("\tWebTransportStateDisconnected WebTransportState = iota\n")
	buf.WriteString("\tWebTransportStateConnecting\n")
	buf.WriteString("\tWebTransportStateConnected\n")
	buf.WriteString("\tWebTransportStateReconnecting\n")
	buf.WriteString("\tWebTransportStateClosed\n")
	buf.WriteString("\tWebTransportStateError\n")
	buf.WriteString(")\n\n")

	buf.WriteString("// String returns the string representation of the state\n")
	buf.WriteString("func (s WebTransportState) String() string {\n")
	buf.WriteString("\tswitch s {\n")
	buf.WriteString("\tcase WebTransportStateDisconnected:\n")
	buf.WriteString("\t\treturn \"disconnected\"\n")
	buf.WriteString("\tcase WebTransportStateConnecting:\n")
	buf.WriteString("\t\treturn \"connecting\"\n")
	buf.WriteString("\tcase WebTransportStateConnected:\n")
	buf.WriteString("\t\treturn \"connected\"\n")
	buf.WriteString("\tcase WebTransportStateReconnecting:\n")
	buf.WriteString("\t\treturn \"reconnecting\"\n")
	buf.WriteString("\tcase WebTransportStateClosed:\n")
	buf.WriteString("\t\treturn \"closed\"\n")
	buf.WriteString("\tcase WebTransportStateError:\n")
	buf.WriteString("\t\treturn \"error\"\n")
	buf.WriteString("\tdefault:\n")
	buf.WriteString("\t\treturn \"unknown\"\n")
	buf.WriteString("\t}\n")
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

	// Client documentation
	buf.WriteString(fmt.Sprintf("// %s is a WebTransport client for %s\n", className, wt.Path))

	if wt.Description != "" {
		buf.WriteString(fmt.Sprintf("// %s\n", wt.Description))
	}

	// Client struct
	buf.WriteString(fmt.Sprintf("type %s struct {\n", className))
	buf.WriteString("\tsession *webtransport.Session\n")
	buf.WriteString("\tbaseURL string\n")
	buf.WriteString("\tauth    *AuthConfig\n")
	buf.WriteString("\tstate   WebTransportState\n")
	buf.WriteString("\tmu      sync.RWMutex\n")
	buf.WriteString("\tclosed  bool\n\n")

	if config.Features.Reconnection {
		buf.WriteString("\t// Reconnection\n")
		buf.WriteString("\treconnectAttempts    int\n")
		buf.WriteString("\tmaxReconnectAttempts int\n")
		buf.WriteString("\treconnectDelay       time.Duration\n")
		buf.WriteString("\tmaxReconnectDelay    time.Duration\n\n")
	}

	if config.Features.StateManagement {
		buf.WriteString("\t// State management\n")
		buf.WriteString("\tstateHandlers []func(WebTransportState)\n\n")
	}

	buf.WriteString("\t// Error handling\n")
	buf.WriteString("\terrorHandlers []func(error)\n")
	buf.WriteString("}\n\n")

	// Constructor
	buf.WriteString(fmt.Sprintf("// New%s creates a new WebTransport client\n", className))
	buf.WriteString(fmt.Sprintf("func New%s(baseURL string, auth *AuthConfig) *%s {\n", className, className))
	buf.WriteString(fmt.Sprintf("\treturn &%s{\n", className))
	buf.WriteString("\t\tbaseURL: baseURL,\n")
	buf.WriteString("\t\tauth:    auth,\n")
	buf.WriteString("\t\tstate:   WebTransportStateDisconnected,\n")

	if config.Features.Reconnection {
		buf.WriteString("\t\tmaxReconnectAttempts: 10,\n")
		buf.WriteString("\t\treconnectDelay:       time.Second,\n")
		buf.WriteString("\t\tmaxReconnectDelay:    30 * time.Second,\n")
	}

	buf.WriteString("\t}\n")
	buf.WriteString("}\n\n")

	// Connect method
	buf.WriteString(fmt.Sprintf("// Connect establishes a WebTransport connection to %s\n", wt.Path))
	buf.WriteString(fmt.Sprintf("func (c *%s) Connect(ctx context.Context) error {\n", className))
	buf.WriteString("\tc.mu.Lock()\n")
	buf.WriteString("\tdefer c.mu.Unlock()\n\n")

	buf.WriteString("\tc.setState(WebTransportStateConnecting)\n\n")

	buf.WriteString("\t// Build WebTransport URL\n")
	buf.WriteString(fmt.Sprintf("\twtURL := c.baseURL + \"%s\"\n\n", wt.Path))

	buf.WriteString("\t// Create dialer\n")
	buf.WriteString("\tdialer := &webtransport.Dialer{}\n\n")

	buf.WriteString("\t// Dial WebTransport\n")
	buf.WriteString("\tsession, _, err := dialer.Dial(ctx, wtURL, nil)\n")
	buf.WriteString("\tif err != nil {\n")
	buf.WriteString("\t\tc.setState(WebTransportStateError)\n")
	buf.WriteString("\t\treturn fmt.Errorf(\"failed to connect: %w\", err)\n")
	buf.WriteString("\t}\n\n")

	buf.WriteString("\tc.session = session\n")
	buf.WriteString("\tc.setState(WebTransportStateConnected)\n")

	if config.Features.Reconnection {
		buf.WriteString("\tc.reconnectAttempts = 0\n")
	}

	buf.WriteString("\n\t// Start handling incoming streams in background\n")
	buf.WriteString("\tgo c.handleIncomingStreams(ctx)\n\n")

	buf.WriteString("\treturn nil\n")
	buf.WriteString("}\n\n")

	// Bidirectional stream methods
	if wt.BiStreamSchema != nil {
		buf.WriteString(w.generateBiStreamMethods(className, wt.BiStreamSchema, spec))
	}

	// Unidirectional stream methods
	if wt.UniStreamSchema != nil {
		buf.WriteString(w.generateUniStreamMethods(className, wt.UniStreamSchema, spec))
	}

	// Datagram methods
	if wt.DatagramSchema != nil {
		buf.WriteString(w.generateDatagramMethods(className, wt.DatagramSchema, spec))
	}

	// Handle incoming streams
	buf.WriteString(w.generateIncomingStreamHandler(className, wt, spec))

	// State management
	if config.Features.StateManagement {
		buf.WriteString(w.generateStateManagement(className))
	}

	// Error handling
	buf.WriteString(w.generateErrorHandling(className))

	// Reconnection
	if config.Features.Reconnection {
		buf.WriteString(w.generateReconnection(className))
	}

	// Close method
	buf.WriteString("// Close closes the WebTransport connection\n")
	buf.WriteString(fmt.Sprintf("func (c *%s) Close() error {\n", className))
	buf.WriteString("\tc.mu.Lock()\n")
	buf.WriteString("\tdefer c.mu.Unlock()\n\n")

	buf.WriteString("\tc.closed = true\n")
	buf.WriteString("\tif c.session != nil {\n")
	buf.WriteString("\t\terr := c.session.CloseWithError(0, \"client closing\")\n")
	buf.WriteString("\t\tc.session = nil\n")
	buf.WriteString("\t\tc.setState(WebTransportStateClosed)\n")
	buf.WriteString("\t\treturn err\n")
	buf.WriteString("\t}\n")
	buf.WriteString("\treturn nil\n")
	buf.WriteString("}\n\n")

	// Get state method
	buf.WriteString("// GetState returns the current connection state\n")
	buf.WriteString(fmt.Sprintf("func (c *%s) GetState() WebTransportState {\n", className))
	buf.WriteString("\tc.mu.RLock()\n")
	buf.WriteString("\tdefer c.mu.RUnlock()\n")
	buf.WriteString("\treturn c.state\n")
	buf.WriteString("}\n\n")

	// setState helper
	buf.WriteString(fmt.Sprintf("func (c *%s) setState(state WebTransportState) {\n", className))
	buf.WriteString("\tc.state = state\n")

	if config.Features.StateManagement {
		buf.WriteString("\tfor _, handler := range c.stateHandlers {\n")
		buf.WriteString("\t\tgo handler(state)\n")
		buf.WriteString("\t}\n")
	}

	buf.WriteString("}\n\n")

	return buf.String()
}

// generateBiStreamMethods generates bidirectional stream methods.
func (w *WebTransportGenerator) generateBiStreamMethods(className string, schema *client.StreamSchema, spec *client.APISpec) string {
	var buf strings.Builder

	sendType := w.getSchemaTypeName(schema.SendSchema, spec)
	receiveType := w.getSchemaTypeName(schema.ReceiveSchema, spec)

	buf.WriteString("// OpenBidiStream opens a new bidirectional stream\n")
	buf.WriteString(fmt.Sprintf("func (c *%s) OpenBidiStream(ctx context.Context) (*BiDiStream, error) {\n", className))
	buf.WriteString("\tc.mu.RLock()\n")
	buf.WriteString("\tsession := c.session\n")
	buf.WriteString("\tc.mu.RUnlock()\n\n")

	buf.WriteString("\tif session == nil {\n")
	buf.WriteString("\t\treturn nil, fmt.Errorf(\"not connected\")\n")
	buf.WriteString("\t}\n\n")

	buf.WriteString("\tstream, err := session.OpenStreamSync(ctx)\n")
	buf.WriteString("\tif err != nil {\n")
	buf.WriteString("\t\treturn nil, fmt.Errorf(\"failed to open stream: %w\", err)\n")
	buf.WriteString("\t}\n\n")

	buf.WriteString("\treturn &BiDiStream{stream: stream}, nil\n")
	buf.WriteString("}\n\n")

	// BiDiStream wrapper
	buf.WriteString("// BiDiStream wraps a WebTransport bidirectional stream\n")
	buf.WriteString("type BiDiStream struct {\n")
	buf.WriteString("\tstream webtransport.Stream\n")
	buf.WriteString("}\n\n")

	buf.WriteString(fmt.Sprintf("// Send sends a %s message on the stream\n", sendType))
	buf.WriteString(fmt.Sprintf("func (s *BiDiStream) Send(msg %s) error {\n", sendType))
	buf.WriteString("\tdata, err := json.Marshal(msg)\n")
	buf.WriteString("\tif err != nil {\n")
	buf.WriteString("\t\treturn fmt.Errorf(\"failed to marshal message: %w\", err)\n")
	buf.WriteString("\t}\n\n")

	buf.WriteString("\t_, err = s.stream.Write(data)\n")
	buf.WriteString("\treturn err\n")
	buf.WriteString("}\n\n")

	buf.WriteString(fmt.Sprintf("// Receive receives a %s message from the stream\n", receiveType))
	buf.WriteString(fmt.Sprintf("func (s *BiDiStream) Receive() (*%s, error) {\n", receiveType))
	buf.WriteString("\tdata, err := io.ReadAll(s.stream)\n")
	buf.WriteString("\tif err != nil {\n")
	buf.WriteString("\t\treturn nil, fmt.Errorf(\"failed to read from stream: %w\", err)\n")
	buf.WriteString("\t}\n\n")

	buf.WriteString(fmt.Sprintf("\tvar msg %s\n", receiveType))
	buf.WriteString("\tif err := json.Unmarshal(data, &msg); err != nil {\n")
	buf.WriteString("\t\treturn nil, fmt.Errorf(\"failed to unmarshal message: %w\", err)\n")
	buf.WriteString("\t}\n\n")

	buf.WriteString("\treturn &msg, nil\n")
	buf.WriteString("}\n\n")

	buf.WriteString("// Close closes the stream\n")
	buf.WriteString("func (s *BiDiStream) Close() error {\n")
	buf.WriteString("\treturn s.stream.Close()\n")
	buf.WriteString("}\n\n")

	return buf.String()
}

// generateUniStreamMethods generates unidirectional stream methods.
func (w *WebTransportGenerator) generateUniStreamMethods(className string, schema *client.StreamSchema, spec *client.APISpec) string {
	var buf strings.Builder

	sendType := w.getSchemaTypeName(schema.SendSchema, spec)

	buf.WriteString("// OpenUniStream opens a new unidirectional stream for sending\n")
	buf.WriteString(fmt.Sprintf("func (c *%s) OpenUniStream(ctx context.Context) (*UniStream, error) {\n", className))
	buf.WriteString("\tc.mu.RLock()\n")
	buf.WriteString("\tsession := c.session\n")
	buf.WriteString("\tc.mu.RUnlock()\n\n")

	buf.WriteString("\tif session == nil {\n")
	buf.WriteString("\t\treturn nil, fmt.Errorf(\"not connected\")\n")
	buf.WriteString("\t}\n\n")

	buf.WriteString("\tstream, err := session.OpenUniStreamSync(ctx)\n")
	buf.WriteString("\tif err != nil {\n")
	buf.WriteString("\t\treturn nil, fmt.Errorf(\"failed to open stream: %w\", err)\n")
	buf.WriteString("\t}\n\n")

	buf.WriteString("\treturn &UniStream{stream: stream}, nil\n")
	buf.WriteString("}\n\n")

	// UniStream wrapper
	buf.WriteString("// UniStream wraps a WebTransport unidirectional stream\n")
	buf.WriteString("type UniStream struct {\n")
	buf.WriteString("\tstream webtransport.SendStream\n")
	buf.WriteString("}\n\n")

	buf.WriteString(fmt.Sprintf("// Send sends a %s message on the stream\n", sendType))
	buf.WriteString(fmt.Sprintf("func (s *UniStream) Send(msg %s) error {\n", sendType))
	buf.WriteString("\tdata, err := json.Marshal(msg)\n")
	buf.WriteString("\tif err != nil {\n")
	buf.WriteString("\t\treturn fmt.Errorf(\"failed to marshal message: %w\", err)\n")
	buf.WriteString("\t}\n\n")

	buf.WriteString("\t_, err = s.stream.Write(data)\n")
	buf.WriteString("\treturn err\n")
	buf.WriteString("}\n\n")

	buf.WriteString("// Close closes the stream\n")
	buf.WriteString("func (s *UniStream) Close() error {\n")
	buf.WriteString("\treturn s.stream.Close()\n")
	buf.WriteString("}\n\n")

	return buf.String()
}

// generateDatagramMethods generates datagram methods.
func (w *WebTransportGenerator) generateDatagramMethods(className string, schema *client.Schema, spec *client.APISpec) string {
	var buf strings.Builder

	typeName := w.getSchemaTypeName(schema, spec)

	buf.WriteString(fmt.Sprintf("// SendDatagram sends a %s as an unreliable datagram\n", typeName))
	buf.WriteString(fmt.Sprintf("func (c *%s) SendDatagram(msg %s) error {\n", className, typeName))
	buf.WriteString("\tc.mu.RLock()\n")
	buf.WriteString("\tsession := c.session\n")
	buf.WriteString("\tc.mu.RUnlock()\n\n")

	buf.WriteString("\tif session == nil {\n")
	buf.WriteString("\t\treturn fmt.Errorf(\"not connected\")\n")
	buf.WriteString("\t}\n\n")

	buf.WriteString("\tdata, err := json.Marshal(msg)\n")
	buf.WriteString("\tif err != nil {\n")
	buf.WriteString("\t\treturn fmt.Errorf(\"failed to marshal message: %w\", err)\n")
	buf.WriteString("\t}\n\n")

	buf.WriteString("\treturn session.SendDatagram(data)\n")
	buf.WriteString("}\n\n")

	buf.WriteString(fmt.Sprintf("// ReceiveDatagram receives a %s datagram\n", typeName))
	buf.WriteString(fmt.Sprintf("func (c *%s) ReceiveDatagram(ctx context.Context) (*%s, error) {\n", className, typeName))
	buf.WriteString("\tc.mu.RLock()\n")
	buf.WriteString("\tsession := c.session\n")
	buf.WriteString("\tc.mu.RUnlock()\n\n")

	buf.WriteString("\tif session == nil {\n")
	buf.WriteString("\t\treturn nil, fmt.Errorf(\"not connected\")\n")
	buf.WriteString("\t}\n\n")

	buf.WriteString("\tdata, err := session.ReceiveDatagram(ctx)\n")
	buf.WriteString("\tif err != nil {\n")
	buf.WriteString("\t\treturn nil, fmt.Errorf(\"failed to receive datagram: %w\", err)\n")
	buf.WriteString("\t}\n\n")

	buf.WriteString(fmt.Sprintf("\tvar msg %s\n", typeName))
	buf.WriteString("\tif err := json.Unmarshal(data, &msg); err != nil {\n")
	buf.WriteString("\t\treturn nil, fmt.Errorf(\"failed to unmarshal datagram: %w\", err)\n")
	buf.WriteString("\t}\n\n")

	buf.WriteString("\treturn &msg, nil\n")
	buf.WriteString("}\n\n")

	return buf.String()
}

// generateIncomingStreamHandler generates handler for incoming streams.
func (w *WebTransportGenerator) generateIncomingStreamHandler(className string, wt client.WebTransportEndpoint, spec *client.APISpec) string {
	var buf strings.Builder

	buf.WriteString(fmt.Sprintf("func (c *%s) handleIncomingStreams(ctx context.Context) {\n", className))
	buf.WriteString("\tfor {\n")
	buf.WriteString("\t\tselect {\n")
	buf.WriteString("\t\tcase <-ctx.Done():\n")
	buf.WriteString("\t\t\treturn\n")
	buf.WriteString("\t\tdefault:\n")
	buf.WriteString("\t\t\tc.mu.RLock()\n")
	buf.WriteString("\t\t\tsession := c.session\n")
	buf.WriteString("\t\t\tc.mu.RUnlock()\n\n")

	buf.WriteString("\t\t\tif session == nil {\n")
	buf.WriteString("\t\t\t\ttime.Sleep(100 * time.Millisecond)\n")
	buf.WriteString("\t\t\t\tcontinue\n")
	buf.WriteString("\t\t\t}\n\n")

	buf.WriteString("\t\t\t// Accept incoming stream\n")
	buf.WriteString("\t\t\tstream, err := session.AcceptStream(ctx)\n")
	buf.WriteString("\t\t\tif err != nil {\n")
	buf.WriteString("\t\t\t\tc.handleError(fmt.Errorf(\"failed to accept stream: %w\", err))\n")
	buf.WriteString("\t\t\t\tcontinue\n")
	buf.WriteString("\t\t\t}\n\n")

	buf.WriteString("\t\t\t// Handle stream in goroutine\n")
	buf.WriteString("\t\t\tgo c.handleStream(ctx, stream)\n")
	buf.WriteString("\t\t}\n")
	buf.WriteString("\t}\n")
	buf.WriteString("}\n\n")

	buf.WriteString(fmt.Sprintf("func (c *%s) handleStream(ctx context.Context, stream webtransport.Stream) {\n", className))
	buf.WriteString("\tdefer stream.Close()\n\n")

	buf.WriteString("\t// Read stream data\n")
	buf.WriteString("\tdata, err := io.ReadAll(stream)\n")
	buf.WriteString("\tif err != nil {\n")
	buf.WriteString("\t\tc.handleError(fmt.Errorf(\"failed to read stream: %w\", err))\n")
	buf.WriteString("\t\treturn\n")
	buf.WriteString("\t}\n\n")

	buf.WriteString("\t// TODO: Process stream data based on your application logic\n")
	buf.WriteString("\t_ = data\n")
	buf.WriteString("}\n\n")

	return buf.String()
}

// generateStateManagement generates state management methods.
func (w *WebTransportGenerator) generateStateManagement(className string) string {
	var buf strings.Builder

	buf.WriteString("// OnStateChange registers a handler for state changes\n")
	buf.WriteString(fmt.Sprintf("func (c *%s) OnStateChange(handler func(WebTransportState)) {\n", className))
	buf.WriteString("\tc.mu.Lock()\n")
	buf.WriteString("\tdefer c.mu.Unlock()\n")
	buf.WriteString("\tc.stateHandlers = append(c.stateHandlers, handler)\n")
	buf.WriteString("}\n\n")

	return buf.String()
}

// generateErrorHandling generates error handling methods.
func (w *WebTransportGenerator) generateErrorHandling(className string) string {
	var buf strings.Builder

	buf.WriteString("// OnError registers an error handler\n")
	buf.WriteString(fmt.Sprintf("func (c *%s) OnError(handler func(error)) {\n", className))
	buf.WriteString("\tc.mu.Lock()\n")
	buf.WriteString("\tdefer c.mu.Unlock()\n")
	buf.WriteString("\tc.errorHandlers = append(c.errorHandlers, handler)\n")
	buf.WriteString("}\n\n")

	buf.WriteString(fmt.Sprintf("func (c *%s) handleError(err error) {\n", className))
	buf.WriteString("\tif err == nil {\n")
	buf.WriteString("\t\treturn\n")
	buf.WriteString("\t}\n\n")

	buf.WriteString("\tc.mu.RLock()\n")
	buf.WriteString("\thandlers := make([]func(error), len(c.errorHandlers))\n")
	buf.WriteString("\tcopy(handlers, c.errorHandlers)\n")
	buf.WriteString("\tc.mu.RUnlock()\n\n")

	buf.WriteString("\tfor _, handler := range handlers {\n")
	buf.WriteString("\t\tgo handler(err)\n")
	buf.WriteString("\t}\n")
	buf.WriteString("}\n\n")

	return buf.String()
}

// generateReconnection generates reconnection logic.
func (w *WebTransportGenerator) generateReconnection(className string) string {
	var buf strings.Builder

	buf.WriteString("// Reconnect attempts to reconnect to the WebTransport server\n")
	buf.WriteString(fmt.Sprintf("func (c *%s) Reconnect(ctx context.Context) error {\n", className))
	buf.WriteString("\tc.mu.Lock()\n")
	buf.WriteString("\tif c.reconnectAttempts >= c.maxReconnectAttempts {\n")
	buf.WriteString("\t\tc.mu.Unlock()\n")
	buf.WriteString("\t\treturn fmt.Errorf(\"max reconnect attempts reached\")\n")
	buf.WriteString("\t}\n\n")

	buf.WriteString("\tc.setState(WebTransportStateReconnecting)\n")
	buf.WriteString("\tc.reconnectAttempts++\n")
	buf.WriteString("\tattempts := c.reconnectAttempts\n")
	buf.WriteString("\tc.mu.Unlock()\n\n")

	buf.WriteString("\t// Exponential backoff\n")
	buf.WriteString("\tdelay := c.reconnectDelay\n")
	buf.WriteString("\tfor i := 1; i < attempts; i++ {\n")
	buf.WriteString("\t\tdelay *= 2\n")
	buf.WriteString("\t\tif delay > c.maxReconnectDelay {\n")
	buf.WriteString("\t\t\tdelay = c.maxReconnectDelay\n")
	buf.WriteString("\t\t\tbreak\n")
	buf.WriteString("\t\t}\n")
	buf.WriteString("\t}\n\n")

	buf.WriteString("\ttime.Sleep(delay)\n\n")

	buf.WriteString("\treturn c.Connect(ctx)\n")
	buf.WriteString("}\n\n")

	return buf.String()
}

// generateClassName generates a class name for a WebTransport endpoint.
func (w *WebTransportGenerator) generateClassName(wt client.WebTransportEndpoint) string {
	if wt.ID != "" {
		return toPascalCase(wt.ID) + "WTClient"
	}

	return "WebTransportClient"
}

// getSchemaTypeName gets the type name for a schema.
func (w *WebTransportGenerator) getSchemaTypeName(schema *client.Schema, spec *client.APISpec) string {
	if schema == nil {
		return "interface{}"
	}

	if schema.Ref != "" {
		parts := strings.Split(schema.Ref, "/")

		return parts[len(parts)-1]
	}

	// Use types generator for schema conversion
	typesGen := NewTypesGenerator()

	return typesGen.schemaToGoType(schema, spec)
}

// toPascalCase converts a string to PascalCase.
func toPascalCase(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '_' || r == '-' || r == ' ' || r == '/'
	})

	var result string
	var resultSb555 strings.Builder

	for _, part := range parts {
		if len(part) > 0 {
			resultSb555.WriteString(strings.ToUpper(part[:1]) + strings.ToLower(part[1:]))
		}
	}
	result += resultSb555.String()

	return result
}
