package golang

import (
	"fmt"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// SSEGenerator generates SSE (Server-Sent Events) client code
type SSEGenerator struct {
	typesGen *TypesGenerator
}

// NewSSEGenerator creates a new SSE generator
func NewSSEGenerator() *SSEGenerator {
	return &SSEGenerator{
		typesGen: NewTypesGenerator(),
	}
}

// Generate generates the sse.go file
func (s *SSEGenerator) Generate(spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	buf.WriteString(fmt.Sprintf("package %s\n\n", config.PackageName))

	// Imports
	buf.WriteString("import (\n")
	buf.WriteString("\t\"bufio\"\n")
	buf.WriteString("\t\"context\"\n")
	buf.WriteString("\t\"encoding/json\"\n")
	buf.WriteString("\t\"fmt\"\n")
	buf.WriteString("\t\"io\"\n")
	buf.WriteString("\t\"net/http\"\n")
	buf.WriteString("\t\"strings\"\n")
	buf.WriteString("\t\"sync\"\n")
	buf.WriteString("\t\"time\"\n")
	buf.WriteString(")\n\n")

	// Generate SSE Event type
	buf.WriteString(s.generateSSEEventType())

	// Generate SSE clients for each endpoint
	for _, sse := range spec.SSEs {
		clientCode := s.generateSSEClient(sse, spec, config)
		buf.WriteString(clientCode)
		buf.WriteString("\n")
	}

	return buf.String()
}

// generateSSEEventType generates the SSE event type
func (s *SSEGenerator) generateSSEEventType() string {
	var buf strings.Builder

	buf.WriteString("// SSEEvent represents a Server-Sent Event\n")
	buf.WriteString("type SSEEvent struct {\n")
	buf.WriteString("\tEvent string\n")
	buf.WriteString("\tData  string\n")
	buf.WriteString("\tID    string\n")
	buf.WriteString("\tRetry int\n")
	buf.WriteString("}\n\n")

	return buf.String()
}

// generateSSEClient generates an SSE client for an endpoint
func (s *SSEGenerator) generateSSEClient(sse client.SSEEndpoint, spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	clientName := client.GenerateSSEClientName(sse)

	// Client struct
	buf.WriteString(fmt.Sprintf("// %s is an SSE client for %s\n", clientName, sse.Path))
	if sse.Description != "" {
		buf.WriteString(fmt.Sprintf("// %s\n", sse.Description))
	}
	buf.WriteString(fmt.Sprintf("type %s struct {\n", clientName))
	buf.WriteString("\tclient *Client\n")
	buf.WriteString("\tresp   *http.Response\n")
	buf.WriteString("\tmu     sync.RWMutex\n\n")

	if config.Features.StateManagement {
		buf.WriteString("\tstate       ConnectionState\n")
		buf.WriteString("\tstateChange func(ConnectionState)\n\n")
	}

	if config.Features.Reconnection {
		buf.WriteString("\treconnectConfig reconnectConfig\n")
		buf.WriteString("\tattempts        int\n")
		buf.WriteString("\tlastEventID     string\n\n")
	}

	buf.WriteString("\thandlers  map[string][]func(interface{})\n")
	buf.WriteString("\tcloseChan chan struct{}\n")
	buf.WriteString("\tclosed    bool\n")
	buf.WriteString("}\n\n")

	// Constructor
	buf.WriteString(fmt.Sprintf("// New%s creates a new SSE client\n", clientName))
	buf.WriteString(fmt.Sprintf("func (c *Client) New%s() *%s {\n", clientName, clientName))
	buf.WriteString(fmt.Sprintf("\treturn &%s{\n", clientName))
	buf.WriteString("\t\tclient:    c,\n")
	buf.WriteString("\t\thandlers:  make(map[string][]func(interface{})),\n")
	buf.WriteString("\t\tcloseChan: make(chan struct{}),\n")

	if config.Features.StateManagement {
		buf.WriteString("\t\tstate:     ConnectionStateDisconnected,\n")
	}

	if config.Features.Reconnection {
		buf.WriteString("\t\treconnectConfig: defaultReconnectConfig(),\n")
	}

	buf.WriteString("\t}\n")
	buf.WriteString("}\n\n")

	// Connect method
	buf.WriteString(s.generateSSEConnectMethod(clientName, sse, config))

	// OnEvent methods for each event type
	for eventName := range sse.EventSchemas {
		buf.WriteString(s.generateOnEventMethod(clientName, eventName, sse, spec))
	}

	// Close method
	buf.WriteString(s.generateSSECloseMethod(clientName, config))

	// State change callback
	if config.Features.StateManagement {
		buf.WriteString(s.generateSSEOnStateChangeMethod(clientName))
	}

	return buf.String()
}

// generateSSEConnectMethod generates the Connect method for SSE
func (s *SSEGenerator) generateSSEConnectMethod(clientName string, sse client.SSEEndpoint, config client.GeneratorConfig) string {
	var buf strings.Builder

	buf.WriteString(fmt.Sprintf("// Connect connects to the SSE endpoint and starts receiving events\n"))
	buf.WriteString(fmt.Sprintf("func (sse *%s) Connect(ctx context.Context) error {\n", clientName))
	buf.WriteString("\tsse.mu.Lock()\n")
	buf.WriteString("\tdefer sse.mu.Unlock()\n\n")

	if config.Features.StateManagement {
		buf.WriteString("\tsse.setState(ConnectionStateConnecting)\n\n")
	}

	buf.WriteString(fmt.Sprintf("\turl := sse.client.buildURL(\"%s\")\n\n", sse.Path))

	buf.WriteString("\treq, err := http.NewRequestWithContext(ctx, \"GET\", url, nil)\n")
	buf.WriteString("\tif err != nil {\n")
	if config.Features.StateManagement {
		buf.WriteString("\t\tsse.setState(ConnectionStateError)\n")
	}
	buf.WriteString("\t\treturn fmt.Errorf(\"create request: %w\", err)\n")
	buf.WriteString("\t}\n\n")

	buf.WriteString("\treq.Header.Set(\"Accept\", \"text/event-stream\")\n")
	buf.WriteString("\treq.Header.Set(\"Cache-Control\", \"no-cache\")\n")
	buf.WriteString("\treq.Header.Set(\"Connection\", \"keep-alive\")\n\n")

	if config.Features.Reconnection {
		buf.WriteString("\tif sse.lastEventID != \"\" {\n")
		buf.WriteString("\t\treq.Header.Set(\"Last-Event-ID\", sse.lastEventID)\n")
		buf.WriteString("\t}\n\n")
	}

	buf.WriteString("\tsse.client.addAuth(req)\n\n")

	buf.WriteString("\tresp, err := sse.client.httpClient.Do(req)\n")
	buf.WriteString("\tif err != nil {\n")
	if config.Features.StateManagement {
		buf.WriteString("\t\tsse.setState(ConnectionStateError)\n")
	}
	buf.WriteString("\t\treturn fmt.Errorf(\"do request: %w\", err)\n")
	buf.WriteString("\t}\n\n")

	buf.WriteString("\tif resp.StatusCode != http.StatusOK {\n")
	buf.WriteString("\t\tresp.Body.Close()\n")
	if config.Features.StateManagement {
		buf.WriteString("\t\tsse.setState(ConnectionStateError)\n")
	}
	buf.WriteString("\t\treturn fmt.Errorf(\"unexpected status: %d\", resp.StatusCode)\n")
	buf.WriteString("\t}\n\n")

	buf.WriteString("\tsse.resp = resp\n")
	if config.Features.StateManagement {
		buf.WriteString("\tsse.setState(ConnectionStateConnected)\n")
	}
	if config.Features.Reconnection {
		buf.WriteString("\tsse.attempts = 0\n")
	}

	buf.WriteString("\n\t// Start reading events\n")
	buf.WriteString("\tgo sse.readEvents()\n\n")

	buf.WriteString("\treturn nil\n")
	buf.WriteString("}\n\n")

	// Generate readEvents method
	buf.WriteString(fmt.Sprintf("func (sse *%s) readEvents() {\n", clientName))
	buf.WriteString("\tdefer func() {\n")
	buf.WriteString("\t\tif sse.resp != nil {\n")
	buf.WriteString("\t\t\tsse.resp.Body.Close()\n")
	buf.WriteString("\t\t}\n")
	if config.Features.Reconnection {
		buf.WriteString("\t\tif !sse.closed {\n")
		buf.WriteString("\t\t\tsse.reconnect()\n")
		buf.WriteString("\t\t}\n")
	}
	buf.WriteString("\t}()\n\n")

	buf.WriteString("\treader := bufio.NewReader(sse.resp.Body)\n")
	buf.WriteString("\tvar event SSEEvent\n\n")

	buf.WriteString("\tfor {\n")
	buf.WriteString("\t\tselect {\n")
	buf.WriteString("\t\tcase <-sse.closeChan:\n")
	buf.WriteString("\t\t\treturn\n")
	buf.WriteString("\t\tdefault:\n")
	buf.WriteString("\t\t\tline, err := reader.ReadString('\\n')\n")
	buf.WriteString("\t\t\tif err != nil {\n")
	buf.WriteString("\t\t\t\tif err != io.EOF {\n")
	if config.Features.StateManagement {
		buf.WriteString("\t\t\t\t\tsse.setState(ConnectionStateError)\n")
	}
	buf.WriteString("\t\t\t\t}\n")
	buf.WriteString("\t\t\t\treturn\n")
	buf.WriteString("\t\t\t}\n\n")

	buf.WriteString("\t\t\tline = strings.TrimSuffix(line, \"\\n\")\n")
	buf.WriteString("\t\t\tline = strings.TrimSuffix(line, \"\\r\")\n\n")

	buf.WriteString("\t\t\tif line == \"\" {\n")
	buf.WriteString("\t\t\t\t// Empty line marks end of event\n")
	buf.WriteString("\t\t\t\tif event.Event != \"\" || event.Data != \"\" {\n")
	buf.WriteString("\t\t\t\t\tsse.dispatchEvent(event)\n")

	if config.Features.Reconnection {
		buf.WriteString("\t\t\t\t\tif event.ID != \"\" {\n")
		buf.WriteString("\t\t\t\t\t\tsse.lastEventID = event.ID\n")
		buf.WriteString("\t\t\t\t\t}\n")
	}

	buf.WriteString("\t\t\t\t\tevent = SSEEvent{}\n")
	buf.WriteString("\t\t\t\t}\n")
	buf.WriteString("\t\t\t\tcontinue\n")
	buf.WriteString("\t\t\t}\n\n")

	buf.WriteString("\t\t\tif strings.HasPrefix(line, \":\") {\n")
	buf.WriteString("\t\t\t\t// Comment line\n")
	buf.WriteString("\t\t\t\tcontinue\n")
	buf.WriteString("\t\t\t}\n\n")

	buf.WriteString("\t\t\tparts := strings.SplitN(line, \":\", 2)\n")
	buf.WriteString("\t\t\tif len(parts) != 2 {\n")
	buf.WriteString("\t\t\t\tcontinue\n")
	buf.WriteString("\t\t\t}\n\n")

	buf.WriteString("\t\t\tfield := parts[0]\n")
	buf.WriteString("\t\t\tvalue := strings.TrimPrefix(parts[1], \" \")\n\n")

	buf.WriteString("\t\t\tswitch field {\n")
	buf.WriteString("\t\t\tcase \"event\":\n")
	buf.WriteString("\t\t\t\tevent.Event = value\n")
	buf.WriteString("\t\t\tcase \"data\":\n")
	buf.WriteString("\t\t\t\tif event.Data != \"\" {\n")
	buf.WriteString("\t\t\t\t\tevent.Data += \"\\n\"\n")
	buf.WriteString("\t\t\t\t}\n")
	buf.WriteString("\t\t\t\tevent.Data += value\n")
	buf.WriteString("\t\t\tcase \"id\":\n")
	buf.WriteString("\t\t\t\tevent.ID = value\n")
	buf.WriteString("\t\t\tcase \"retry\":\n")
	buf.WriteString("\t\t\t\tfmt.Sscanf(value, \"%d\", &event.Retry)\n")
	buf.WriteString("\t\t\t}\n")
	buf.WriteString("\t\t}\n")
	buf.WriteString("\t}\n")
	buf.WriteString("}\n\n")

	// Generate dispatchEvent method
	buf.WriteString(fmt.Sprintf("func (sse *%s) dispatchEvent(event SSEEvent) {\n", clientName))
	buf.WriteString("\tsse.mu.RLock()\n")
	buf.WriteString("\thandlers, ok := sse.handlers[event.Event]\n")
	buf.WriteString("\tsse.mu.RUnlock()\n\n")

	buf.WriteString("\tif !ok {\n")
	buf.WriteString("\t\treturn\n")
	buf.WriteString("\t}\n\n")

	buf.WriteString("\t// Try to parse as JSON\n")
	buf.WriteString("\tvar data interface{}\n")
	buf.WriteString("\tif err := json.Unmarshal([]byte(event.Data), &data); err != nil {\n")
	buf.WriteString("\t\t// Use raw data if not JSON\n")
	buf.WriteString("\t\tdata = event.Data\n")
	buf.WriteString("\t}\n\n")

	buf.WriteString("\tfor _, handler := range handlers {\n")
	buf.WriteString("\t\tgo handler(data)\n")
	buf.WriteString("\t}\n")
	buf.WriteString("}\n\n")

	// Generate reconnect method if enabled
	if config.Features.Reconnection {
		buf.WriteString(fmt.Sprintf("func (sse *%s) reconnect() {\n", clientName))
		buf.WriteString("\tsse.mu.Lock()\n")
		buf.WriteString("\tdefer sse.mu.Unlock()\n\n")
		if config.Features.StateManagement {
			buf.WriteString("\tsse.setState(ConnectionStateReconnecting)\n\n")
		}
		buf.WriteString("\tif sse.attempts >= sse.reconnectConfig.maxAttempts {\n")
		if config.Features.StateManagement {
			buf.WriteString("\t\tsse.setState(ConnectionStateClosed)\n")
		}
		buf.WriteString("\t\treturn\n")
		buf.WriteString("\t}\n\n")
		buf.WriteString("\tdelay := calculateBackoff(sse.attempts, sse.reconnectConfig)\n")
		buf.WriteString("\tsse.attempts++\n")
		buf.WriteString("\ttime.Sleep(delay)\n\n")
		buf.WriteString("\tsse.Connect(context.Background())\n")
		buf.WriteString("}\n\n")
	}

	return buf.String()
}

// generateOnEventMethod generates the OnEvent method for a specific event type
func (s *SSEGenerator) generateOnEventMethod(clientName, eventName string, sse client.SSEEndpoint, spec *client.APISpec) string {
	var buf strings.Builder

	methodName := s.typesGen.toGoFieldName(eventName)
	schema := sse.EventSchemas[eventName]
	typeName := s.getSchemaTypeName(schema, spec)

	buf.WriteString(fmt.Sprintf("// On%s registers a handler for %s events\n", methodName, eventName))
	buf.WriteString(fmt.Sprintf("func (sse *%s) On%s(handler func(%s)) {\n", clientName, methodName, typeName))
	buf.WriteString("\tsse.mu.Lock()\n")
	buf.WriteString("\tdefer sse.mu.Unlock()\n\n")

	buf.WriteString(fmt.Sprintf("\tsse.handlers[\"%s\"] = append(sse.handlers[\"%s\"], func(data interface{}) {\n", eventName, eventName))
	buf.WriteString(fmt.Sprintf("\t\tif typed, ok := data.(%s); ok {\n", typeName))
	buf.WriteString("\t\t\thandler(typed)\n")
	buf.WriteString("\t\t}\n")
	buf.WriteString("\t})\n")
	buf.WriteString("}\n\n")

	return buf.String()
}

// generateSSECloseMethod generates the Close method for SSE
func (s *SSEGenerator) generateSSECloseMethod(clientName string, config client.GeneratorConfig) string {
	var buf strings.Builder

	buf.WriteString(fmt.Sprintf("// Close closes the SSE connection\n"))
	buf.WriteString(fmt.Sprintf("func (sse *%s) Close() error {\n", clientName))
	buf.WriteString("\tsse.mu.Lock()\n")
	buf.WriteString("\tdefer sse.mu.Unlock()\n\n")

	buf.WriteString("\tif sse.closed {\n")
	buf.WriteString("\t\treturn nil\n")
	buf.WriteString("\t}\n\n")

	buf.WriteString("\tsse.closed = true\n")
	buf.WriteString("\tclose(sse.closeChan)\n\n")

	if config.Features.StateManagement {
		buf.WriteString("\tsse.setState(ConnectionStateClosed)\n\n")
	}

	buf.WriteString("\tif sse.resp != nil {\n")
	buf.WriteString("\t\treturn sse.resp.Body.Close()\n")
	buf.WriteString("\t}\n\n")

	buf.WriteString("\treturn nil\n")
	buf.WriteString("}\n\n")

	return buf.String()
}

// generateSSEOnStateChangeMethod generates the OnStateChange method
func (s *SSEGenerator) generateSSEOnStateChangeMethod(clientName string) string {
	var buf strings.Builder

	buf.WriteString(fmt.Sprintf("// OnStateChange registers a callback for state changes\n"))
	buf.WriteString(fmt.Sprintf("func (sse *%s) OnStateChange(handler func(ConnectionState)) {\n", clientName))
	buf.WriteString("\tsse.mu.Lock()\n")
	buf.WriteString("\tdefer sse.mu.Unlock()\n")
	buf.WriteString("\tsse.stateChange = handler\n")
	buf.WriteString("}\n\n")

	buf.WriteString(fmt.Sprintf("func (sse *%s) setState(state ConnectionState) {\n", clientName))
	buf.WriteString("\tsse.state = state\n")
	buf.WriteString("\tif sse.stateChange != nil {\n")
	buf.WriteString("\t\tgo sse.stateChange(state)\n")
	buf.WriteString("\t}\n")
	buf.WriteString("}\n\n")

	return buf.String()
}

// getSchemaTypeName gets the type name for a schema
func (s *SSEGenerator) getSchemaTypeName(schema *client.Schema, spec *client.APISpec) string {
	if schema == nil {
		return "interface{}"
	}

	if schema.Ref != "" {
		return s.typesGen.extractRefName(schema.Ref)
	}

	return "interface{}"
}
