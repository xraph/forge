package golang

import (
	"fmt"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// WebSocketGenerator generates WebSocket client code
type WebSocketGenerator struct {
	typesGen *TypesGenerator
}

// NewWebSocketGenerator creates a new WebSocket generator
func NewWebSocketGenerator() *WebSocketGenerator {
	return &WebSocketGenerator{
		typesGen: NewTypesGenerator(),
	}
}

// Generate generates the websocket.go file
func (w *WebSocketGenerator) Generate(spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	buf.WriteString(fmt.Sprintf("package %s\n\n", config.PackageName))

	// Imports
	buf.WriteString("import (\n")
	buf.WriteString("\t\"context\"\n")
	buf.WriteString("\t\"encoding/json\"\n")
	buf.WriteString("\t\"fmt\"\n")
	buf.WriteString("\t\"net/http\"\n")
	buf.WriteString("\t\"sync\"\n")
	buf.WriteString("\t\"time\"\n\n")
	buf.WriteString("\t\"github.com/gorilla/websocket\"\n")
	buf.WriteString(")\n\n")

	// Generate ConnectionState type
	buf.WriteString(w.generateConnectionStateType())

	// Generate base WebSocket client helper
	if config.Features.StateManagement || config.Features.Reconnection || config.Features.Heartbeat {
		buf.WriteString(w.generateWSHelpers(config))
	}

	// Generate WebSocket clients for each endpoint
	for _, ws := range spec.WebSockets {
		clientCode := w.generateWebSocketClient(ws, spec, config)
		buf.WriteString(clientCode)
		buf.WriteString("\n")
	}

	return buf.String()
}

// generateConnectionStateType generates the ConnectionState type
func (w *WebSocketGenerator) generateConnectionStateType() string {
	var buf strings.Builder

	buf.WriteString("// ConnectionState represents the state of a WebSocket connection\n")
	buf.WriteString("type ConnectionState string\n\n")
	buf.WriteString("const (\n")
	buf.WriteString("\tConnectionStateDisconnected  ConnectionState = \"disconnected\"\n")
	buf.WriteString("\tConnectionStateConnecting    ConnectionState = \"connecting\"\n")
	buf.WriteString("\tConnectionStateConnected     ConnectionState = \"connected\"\n")
	buf.WriteString("\tConnectionStateReconnecting  ConnectionState = \"reconnecting\"\n")
	buf.WriteString("\tConnectionStateClosed        ConnectionState = \"closed\"\n")
	buf.WriteString("\tConnectionStateError         ConnectionState = \"error\"\n")
	buf.WriteString(")\n\n")

	return buf.String()
}

// generateWSHelpers generates WebSocket helper functions
func (w *WebSocketGenerator) generateWSHelpers(config client.GeneratorConfig) string {
	var buf strings.Builder

	if config.Features.Reconnection {
		buf.WriteString("// reconnectConfig holds reconnection configuration\n")
		buf.WriteString("type reconnectConfig struct {\n")
		buf.WriteString("\tinitialDelay  time.Duration\n")
		buf.WriteString("\tmaxDelay      time.Duration\n")
		buf.WriteString("\tmaxAttempts   int\n")
		buf.WriteString("\tbackoffFactor float64\n")
		buf.WriteString("}\n\n")

		buf.WriteString("func defaultReconnectConfig() reconnectConfig {\n")
		buf.WriteString("\treturn reconnectConfig{\n")
		buf.WriteString("\t\tinitialDelay:  time.Second,\n")
		buf.WriteString("\t\tmaxDelay:      30 * time.Second,\n")
		buf.WriteString("\t\tmaxAttempts:   10,\n")
		buf.WriteString("\t\tbackoffFactor: 2.0,\n")
		buf.WriteString("\t}\n")
		buf.WriteString("}\n\n")

		buf.WriteString("func calculateBackoff(attempt int, config reconnectConfig) time.Duration {\n")
		buf.WriteString("\tdelay := float64(config.initialDelay)\n")
		buf.WriteString("\tfor i := 0; i < attempt; i++ {\n")
		buf.WriteString("\t\tdelay *= config.backoffFactor\n")
		buf.WriteString("\t}\n")
		buf.WriteString("\tif time.Duration(delay) > config.maxDelay {\n")
		buf.WriteString("\t\treturn config.maxDelay\n")
		buf.WriteString("\t}\n")
		buf.WriteString("\treturn time.Duration(delay)\n")
		buf.WriteString("}\n\n")
	}

	return buf.String()
}

// generateWebSocketClient generates a WebSocket client for an endpoint
func (w *WebSocketGenerator) generateWebSocketClient(ws client.WebSocketEndpoint, spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	clientName := client.GenerateWebSocketClientName(ws)

	// Client struct
	buf.WriteString(fmt.Sprintf("// %s is a WebSocket client for %s\n", clientName, ws.Path))
	if ws.Description != "" {
		buf.WriteString(fmt.Sprintf("// %s\n", ws.Description))
	}
	buf.WriteString(fmt.Sprintf("type %s struct {\n", clientName))
	buf.WriteString("\tclient *Client\n")
	buf.WriteString("\tconn   *websocket.Conn\n")
	buf.WriteString("\tmu     sync.RWMutex\n\n")

	if config.Features.StateManagement {
		buf.WriteString("\tstate       ConnectionState\n")
		buf.WriteString("\tstateChange func(ConnectionState)\n\n")
	}

	if config.Features.Reconnection {
		buf.WriteString("\treconnectConfig reconnectConfig\n")
		buf.WriteString("\tattempts        int\n\n")
	}

	if config.Features.Heartbeat {
		buf.WriteString("\theartbeatInterval time.Duration\n")
		buf.WriteString("\theartbeatTicker   *time.Ticker\n\n")
	}

	buf.WriteString("\tcloseChan chan struct{}\n")
	buf.WriteString("\tclosed    bool\n")
	buf.WriteString("}\n\n")

	// Constructor
	buf.WriteString(fmt.Sprintf("// New%s creates a new WebSocket client\n", clientName))
	buf.WriteString(fmt.Sprintf("func (c *Client) New%s() *%s {\n", clientName, clientName))
	buf.WriteString(fmt.Sprintf("\treturn &%s{\n", clientName))
	buf.WriteString("\t\tclient:    c,\n")
	buf.WriteString("\t\tcloseChan: make(chan struct{}),\n")

	if config.Features.StateManagement {
		buf.WriteString("\t\tstate:     ConnectionStateDisconnected,\n")
	}

	if config.Features.Reconnection {
		buf.WriteString("\t\treconnectConfig: defaultReconnectConfig(),\n")
	}

	if config.Features.Heartbeat {
		buf.WriteString("\t\theartbeatInterval: 30 * time.Second,\n")
	}

	buf.WriteString("\t}\n")
	buf.WriteString("}\n\n")

	// Connect method
	buf.WriteString(w.generateConnectMethod(clientName, ws, config))

	// Send method
	if ws.SendSchema != nil {
		buf.WriteString(w.generateSendMethod(clientName, ws, spec, config))
	}

	// OnMessage method
	if ws.ReceiveSchema != nil {
		buf.WriteString(w.generateOnMessageMethod(clientName, ws, spec, config))
	}

	// Close method
	buf.WriteString(w.generateCloseMethod(clientName, config))

	// State change callback
	if config.Features.StateManagement {
		buf.WriteString(w.generateOnStateChangeMethod(clientName))
	}

	return buf.String()
}

// generateConnectMethod generates the Connect method
func (w *WebSocketGenerator) generateConnectMethod(clientName string, ws client.WebSocketEndpoint, config client.GeneratorConfig) string {
	var buf strings.Builder

	buf.WriteString(fmt.Sprintf("// Connect connects to the WebSocket endpoint\n"))
	buf.WriteString(fmt.Sprintf("func (ws *%s) Connect(ctx context.Context) error {\n", clientName))
	buf.WriteString("\tws.mu.Lock()\n")
	buf.WriteString("\tdefer ws.mu.Unlock()\n\n")

	if config.Features.StateManagement {
		buf.WriteString("\tws.setState(ConnectionStateConnecting)\n\n")
	}

	buf.WriteString(fmt.Sprintf("\turl := ws.client.buildURL(\"%s\")\n", ws.Path))
	buf.WriteString("\turl = \"ws\" + url[4:] // Convert http(s) to ws(s)\n\n")

	buf.WriteString("\theader := http.Header{}\n")
	buf.WriteString("\tif ws.client.auth != nil {\n")
	buf.WriteString("\t\tif ws.client.auth.BearerToken != \"\" {\n")
	buf.WriteString("\t\t\theader.Set(\"Authorization\", \"Bearer \"+ws.client.auth.BearerToken)\n")
	buf.WriteString("\t\t}\n")
	buf.WriteString("\t}\n\n")

	buf.WriteString("\tconn, _, err := websocket.DefaultDialer.DialContext(ctx, url, header)\n")
	buf.WriteString("\tif err != nil {\n")
	if config.Features.StateManagement {
		buf.WriteString("\t\tws.setState(ConnectionStateError)\n")
	}
	buf.WriteString("\t\treturn fmt.Errorf(\"dial websocket: %w\", err)\n")
	buf.WriteString("\t}\n\n")

	buf.WriteString("\tws.conn = conn\n")
	if config.Features.StateManagement {
		buf.WriteString("\tws.setState(ConnectionStateConnected)\n")
	}
	if config.Features.Reconnection {
		buf.WriteString("\tws.attempts = 0\n")
	}

	if config.Features.Heartbeat {
		buf.WriteString("\n\t// Start heartbeat\n")
		buf.WriteString("\tws.startHeartbeat()\n")
	}

	buf.WriteString("\n\treturn nil\n")
	buf.WriteString("}\n\n")

	// Add heartbeat method if enabled
	if config.Features.Heartbeat {
		buf.WriteString(fmt.Sprintf("func (ws *%s) startHeartbeat() {\n", clientName))
		buf.WriteString("\tws.heartbeatTicker = time.NewTicker(ws.heartbeatInterval)\n")
		buf.WriteString("\tgo func() {\n")
		buf.WriteString("\t\tfor {\n")
		buf.WriteString("\t\t\tselect {\n")
		buf.WriteString("\t\t\tcase <-ws.heartbeatTicker.C:\n")
		buf.WriteString("\t\t\t\tws.mu.Lock()\n")
		buf.WriteString("\t\t\t\tif ws.conn != nil {\n")
		buf.WriteString("\t\t\t\t\tws.conn.WriteMessage(websocket.PingMessage, nil)\n")
		buf.WriteString("\t\t\t\t}\n")
		buf.WriteString("\t\t\t\tws.mu.Unlock()\n")
		buf.WriteString("\t\t\tcase <-ws.closeChan:\n")
		buf.WriteString("\t\t\t\treturn\n")
		buf.WriteString("\t\t\t}\n")
		buf.WriteString("\t\t}\n")
		buf.WriteString("\t}()\n")
		buf.WriteString("}\n\n")
	}

	return buf.String()
}

// generateSendMethod generates the Send method
func (w *WebSocketGenerator) generateSendMethod(clientName string, ws client.WebSocketEndpoint, spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	typeName := w.getSchemaTypeName(ws.SendSchema, spec)

	buf.WriteString(fmt.Sprintf("// Send sends a message to the WebSocket\n"))
	buf.WriteString(fmt.Sprintf("func (ws *%s) Send(msg %s) error {\n", clientName, typeName))
	buf.WriteString("\tws.mu.RLock()\n")
	buf.WriteString("\tdefer ws.mu.RUnlock()\n\n")

	buf.WriteString("\tif ws.conn == nil {\n")
	buf.WriteString("\t\treturn fmt.Errorf(\"not connected\")\n")
	buf.WriteString("\t}\n\n")

	buf.WriteString("\tdata, err := json.Marshal(msg)\n")
	buf.WriteString("\tif err != nil {\n")
	buf.WriteString("\t\treturn fmt.Errorf(\"marshal message: %w\", err)\n")
	buf.WriteString("\t}\n\n")

	buf.WriteString("\treturn ws.conn.WriteMessage(websocket.TextMessage, data)\n")
	buf.WriteString("}\n\n")

	return buf.String()
}

// generateOnMessageMethod generates the OnMessage method
func (w *WebSocketGenerator) generateOnMessageMethod(clientName string, ws client.WebSocketEndpoint, spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	typeName := w.getSchemaTypeName(ws.ReceiveSchema, spec)

	buf.WriteString(fmt.Sprintf("// OnMessage registers a handler for incoming messages\n"))
	buf.WriteString(fmt.Sprintf("func (ws *%s) OnMessage(handler func(%s)) {\n", clientName, typeName))
	buf.WriteString("\tgo func() {\n")
	buf.WriteString("\t\tfor {\n")
	buf.WriteString("\t\t\tselect {\n")
	buf.WriteString("\t\t\tcase <-ws.closeChan:\n")
	buf.WriteString("\t\t\t\treturn\n")
	buf.WriteString("\t\t\tdefault:\n")
	buf.WriteString("\t\t\t\tws.mu.RLock()\n")
	buf.WriteString("\t\t\t\tconn := ws.conn\n")
	buf.WriteString("\t\t\t\tws.mu.RUnlock()\n\n")

	buf.WriteString("\t\t\t\tif conn == nil {\n")
	buf.WriteString("\t\t\t\t\ttime.Sleep(100 * time.Millisecond)\n")
	buf.WriteString("\t\t\t\t\tcontinue\n")
	buf.WriteString("\t\t\t\t}\n\n")

	buf.WriteString("\t\t\t\t_, data, err := conn.ReadMessage()\n")
	buf.WriteString("\t\t\t\tif err != nil {\n")
	if config.Features.Reconnection {
		buf.WriteString("\t\t\t\t\t// Connection lost, try to reconnect\n")
		buf.WriteString("\t\t\t\t\tws.reconnect()\n")
	}
	buf.WriteString("\t\t\t\t\tcontinue\n")
	buf.WriteString("\t\t\t\t}\n\n")

	buf.WriteString(fmt.Sprintf("\t\t\t\tvar msg %s\n", typeName))
	buf.WriteString("\t\t\t\tif err := json.Unmarshal(data, &msg); err != nil {\n")
	buf.WriteString("\t\t\t\t\tcontinue\n")
	buf.WriteString("\t\t\t\t}\n\n")

	buf.WriteString("\t\t\t\thandler(msg)\n")
	buf.WriteString("\t\t\t}\n")
	buf.WriteString("\t\t}\n")
	buf.WriteString("\t}()\n")
	buf.WriteString("}\n\n")

	// Add reconnect method if enabled
	if config.Features.Reconnection {
		buf.WriteString(fmt.Sprintf("func (ws *%s) reconnect() {\n", clientName))
		buf.WriteString("\tws.mu.Lock()\n")
		buf.WriteString("\tdefer ws.mu.Unlock()\n\n")
		if config.Features.StateManagement {
			buf.WriteString("\tws.setState(ConnectionStateReconnecting)\n\n")
		}
		buf.WriteString("\tif ws.attempts >= ws.reconnectConfig.maxAttempts {\n")
		if config.Features.StateManagement {
			buf.WriteString("\t\tws.setState(ConnectionStateClosed)\n")
		}
		buf.WriteString("\t\treturn\n")
		buf.WriteString("\t}\n\n")
		buf.WriteString("\tdelay := calculateBackoff(ws.attempts, ws.reconnectConfig)\n")
		buf.WriteString("\tws.attempts++\n")
		buf.WriteString("\ttime.Sleep(delay)\n\n")
		buf.WriteString("\tws.Connect(context.Background())\n")
		buf.WriteString("}\n\n")
	}

	return buf.String()
}

// generateCloseMethod generates the Close method
func (w *WebSocketGenerator) generateCloseMethod(clientName string, config client.GeneratorConfig) string {
	var buf strings.Builder

	buf.WriteString(fmt.Sprintf("// Close closes the WebSocket connection\n"))
	buf.WriteString(fmt.Sprintf("func (ws *%s) Close() error {\n", clientName))
	buf.WriteString("\tws.mu.Lock()\n")
	buf.WriteString("\tdefer ws.mu.Unlock()\n\n")

	buf.WriteString("\tif ws.closed {\n")
	buf.WriteString("\t\treturn nil\n")
	buf.WriteString("\t}\n\n")

	buf.WriteString("\tws.closed = true\n")
	buf.WriteString("\tclose(ws.closeChan)\n\n")

	if config.Features.Heartbeat {
		buf.WriteString("\tif ws.heartbeatTicker != nil {\n")
		buf.WriteString("\t\tws.heartbeatTicker.Stop()\n")
		buf.WriteString("\t}\n\n")
	}

	if config.Features.StateManagement {
		buf.WriteString("\tws.setState(ConnectionStateClosed)\n\n")
	}

	buf.WriteString("\tif ws.conn != nil {\n")
	buf.WriteString("\t\treturn ws.conn.Close()\n")
	buf.WriteString("\t}\n\n")

	buf.WriteString("\treturn nil\n")
	buf.WriteString("}\n\n")

	return buf.String()
}

// generateOnStateChangeMethod generates the OnStateChange method
func (w *WebSocketGenerator) generateOnStateChangeMethod(clientName string) string {
	var buf strings.Builder

	buf.WriteString(fmt.Sprintf("// OnStateChange registers a callback for state changes\n"))
	buf.WriteString(fmt.Sprintf("func (ws *%s) OnStateChange(handler func(ConnectionState)) {\n", clientName))
	buf.WriteString("\tws.mu.Lock()\n")
	buf.WriteString("\tdefer ws.mu.Unlock()\n")
	buf.WriteString("\tws.stateChange = handler\n")
	buf.WriteString("}\n\n")

	buf.WriteString(fmt.Sprintf("func (ws *%s) setState(state ConnectionState) {\n", clientName))
	buf.WriteString("\tws.state = state\n")
	buf.WriteString("\tif ws.stateChange != nil {\n")
	buf.WriteString("\t\tgo ws.stateChange(state)\n")
	buf.WriteString("\t}\n")
	buf.WriteString("}\n\n")

	return buf.String()
}

// getSchemaTypeName gets the type name for a schema
func (w *WebSocketGenerator) getSchemaTypeName(schema *client.Schema, spec *client.APISpec) string {
	if schema == nil {
		return "interface{}"
	}

	if schema.Ref != "" {
		return w.typesGen.extractRefName(schema.Ref)
	}

	return "map[string]interface{}"
}
