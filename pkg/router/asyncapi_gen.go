package router

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
	"gopkg.in/yaml.v3"
)

// AsyncAPIGenerator handles AsyncAPI 3.0 specification generation for streaming endpoints
type AsyncAPIGenerator struct {
	router  *ForgeRouter
	spec    *common.AsyncAPISpec
	mu      sync.RWMutex
	logger  common.Logger
	metrics common.Metrics
}

// NewAsyncAPIGenerator creates a new AsyncAPI generator
func NewAsyncAPIGenerator(router *ForgeRouter) *AsyncAPIGenerator {
	generator := &AsyncAPIGenerator{
		router:  router,
		logger:  router.logger,
		metrics: router.metrics,
	}

	generator.initializeSpec()
	return generator
}

// initializeSpec initializes the AsyncAPI specification
func (g *AsyncAPIGenerator) initializeSpec() {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.spec = &common.AsyncAPISpec{
		AsyncAPI: "3.0.0",
		Info: common.AsyncAPIInfo{
			Title:       "Streaming API",
			Version:     "1.0.0",
			Description: "Real-time streaming API using WebSockets and Server-Sent Events",
		},
		Servers:    make(map[string]common.AsyncAPIServer),
		Channels:   make(map[string]common.AsyncAPIChannel),
		Operations: make(map[string]common.AsyncAPIOperation),
		Components: common.AsyncAPIComponents{
			Schemas:         make(map[string]common.AsyncAPISchema),
			Messages:        make(map[string]common.AsyncAPIMessage),
			SecuritySchemes: make(map[string]common.AsyncAPISecurityScheme),
			Parameters:      make(map[string]common.AsyncAPIParameter),
			CorrelationIds:  make(map[string]common.AsyncAPICorrelationId),
			Replies:         make(map[string]common.AsyncAPIReply),
			ReplyAddresses:  make(map[string]common.AsyncAPIReplyAddress),
			ExternalDocs:    make(map[string]common.AsyncAPIExternalDocs),
			Tags:            make(map[string]common.AsyncAPITag),
		},
		DefaultContentType: "application/json",
	}

	// Add default server
	g.spec.Servers["development"] = common.AsyncAPIServer{
		Host:        "localhost:8080",
		Protocol:    "http",
		Description: "Development server",
	}

	// Add default security schemes
	g.addDefaultSecuritySchemes()
}

// AddWebSocketOperation adds a WebSocket operation to the AsyncAPI specification
func (g *AsyncAPIGenerator) AddWebSocketOperation(path string, info *common.WSHandlerInfo) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.logger != nil {
		g.logger.Debug("Adding AsyncAPI WebSocket operation",
			logger.String("path", path),
		)
	}

	// Create channel for WebSocket
	channelName := g.sanitizeChannelName(path)

	// Create message references map for the channel
	channelMessages := make(map[string]interface{})

	// Process message types from handler info
	if len(info.MessageTypes) > 0 {
		for msgName, msgType := range info.MessageTypes {
			// Create the actual message in components
			messageName := fmt.Sprintf("%s_%s", channelName, msgName)
			message := g.createAsyncAPIMessage(msgName, msgType, "WebSocket message")
			g.spec.Components.Messages[messageName] = message

			// Add reference in channel
			channelMessages[msgName] = map[string]string{
				"$ref": fmt.Sprintf("#/components/messages/%s", messageName),
			}
		}
	}

	// Add default message if none specified
	if len(channelMessages) == 0 {
		defaultMessageName := fmt.Sprintf("%s_default", channelName)
		defaultMessage := g.createDefaultWebSocketMessage()
		g.spec.Components.Messages[defaultMessageName] = defaultMessage

		channelMessages["default"] = map[string]string{
			"$ref": fmt.Sprintf("#/components/messages/%s", defaultMessageName),
		}
	}

	// Create the channel
	channel := common.AsyncAPIChannel{
		Address:     path,
		Title:       fmt.Sprintf("WebSocket channel for %s", path),
		Summary:     info.Summary,
		Description: info.Description,
		Messages:    channelMessages, // Use interface{} to support $ref structure
		Tags:        g.convertToAsyncAPITags(info.Tags),
	}

	g.spec.Channels[channelName] = channel

	// Create operations for send and receive
	g.createWebSocketOperations(channelName, path, info, channelMessages)

	if g.logger != nil {
		g.logger.Info("AsyncAPI WebSocket operation added",
			logger.String("path", path),
			logger.String("channel", channelName),
		)
	}

	if g.metrics != nil {
		g.metrics.Counter("forge.asyncapi.websocket_operations_added").Inc()
	}

	return nil
}

// AddSSEOperation adds a Server-Sent Events operation to the AsyncAPI specification
func (g *AsyncAPIGenerator) AddSSEOperation(path string, info *common.SSEHandlerInfo) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.logger != nil {
		g.logger.Debug("Adding AsyncAPI SSE operation",
			logger.String("path", path),
		)
	}

	// Create channel for SSE
	channelName := g.sanitizeChannelName(path)

	// Create message references map for the channel
	channelMessages := make(map[string]interface{})

	// Process event types from handler info
	if len(info.EventTypes) > 0 {
		for eventName, eventType := range info.EventTypes {
			// Create the actual message in components
			messageName := fmt.Sprintf("%s_%s", channelName, eventName)
			message := g.createAsyncAPIMessage(eventName, eventType, "Server-Sent Event")
			g.spec.Components.Messages[messageName] = message

			// Add reference in channel
			channelMessages[eventName] = map[string]string{
				"$ref": fmt.Sprintf("#/components/messages/%s", messageName),
			}
		}
	}

	// Add default event if none specified
	if len(channelMessages) == 0 {
		defaultMessageName := fmt.Sprintf("%s_default", channelName)
		defaultMessage := g.createDefaultSSEMessage()
		g.spec.Components.Messages[defaultMessageName] = defaultMessage

		channelMessages["default"] = map[string]string{
			"$ref": fmt.Sprintf("#/components/messages/%s", defaultMessageName),
		}
	}

	// Create the channel
	channel := common.AsyncAPIChannel{
		Address:     path,
		Title:       fmt.Sprintf("SSE channel for %s", path),
		Summary:     info.Summary,
		Description: info.Description,
		Messages:    channelMessages, // Use interface{} to support $ref structure
		Tags:        g.convertToAsyncAPITags(info.Tags),
	}

	g.spec.Channels[channelName] = channel

	// Create operation for SSE (server sends events to client)
	g.createSSEOperation(channelName, path, info, channelMessages)

	if g.logger != nil {
		g.logger.Info("AsyncAPI SSE operation added",
			logger.String("path", path),
			logger.String("channel", channelName),
		)
	}

	if g.metrics != nil {
		g.metrics.Counter("forge.asyncapi.sse_operations_added").Inc()
	}

	return nil
}

// createWebSocketOperations creates send and receive operations for WebSocket
func (g *AsyncAPIGenerator) createWebSocketOperations(channelName, path string, info *common.WSHandlerInfo, channelMessages map[string]interface{}) {
	// Send operation (client to server)
	sendOpName := fmt.Sprintf("%s_send", channelName)

	// Create messages array with references
	var sendMessages []interface{}
	for _, msgRef := range channelMessages {
		sendMessages = append(sendMessages, msgRef)
	}

	sendOp := common.AsyncAPIOperation{
		Action: "send",
		Channel: common.AsyncAPIChannelRef{
			Ref: fmt.Sprintf("#/channels/%s", channelName),
		},
		Title:       fmt.Sprintf("Send message to %s", path),
		Summary:     fmt.Sprintf("Send messages to WebSocket endpoint %s", path),
		Description: fmt.Sprintf("Allows clients to send messages to the WebSocket endpoint at %s", path),
		Tags:        g.convertToAsyncAPITags(info.Tags),
		Messages:    sendMessages,
	}

	if info.Authentication {
		sendOp.Security = []common.AsyncAPISecurityRequirement{
			{"bearerAuth": []string{}},
		}
	}

	g.spec.Operations[sendOpName] = sendOp

	// Receive operation (server to client)
	receiveOpName := fmt.Sprintf("%s_receive", channelName)

	var receiveMessages []interface{}
	for _, msgRef := range channelMessages {
		receiveMessages = append(receiveMessages, msgRef)
	}

	receiveOp := common.AsyncAPIOperation{
		Action: "receive",
		Channel: common.AsyncAPIChannelRef{
			Ref: fmt.Sprintf("#/channels/%s", channelName),
		},
		Title:       fmt.Sprintf("Receive message from %s", path),
		Summary:     fmt.Sprintf("Receive messages from WebSocket endpoint %s", path),
		Description: fmt.Sprintf("Allows clients to receive messages from the WebSocket endpoint at %s", path),
		Tags:        g.convertToAsyncAPITags(info.Tags),
		Messages:    receiveMessages,
	}

	if info.Authentication {
		receiveOp.Security = []common.AsyncAPISecurityRequirement{
			{"bearerAuth": []string{}},
		}
	}

	g.spec.Operations[receiveOpName] = receiveOp
}

// createSSEOperation creates a receive operation for SSE
func (g *AsyncAPIGenerator) createSSEOperation(channelName, path string, info *common.SSEHandlerInfo, channelMessages map[string]interface{}) {
	// Receive operation (server to client) - SSE is unidirectional
	receiveOpName := fmt.Sprintf("%s_receive", channelName)

	// Create messages array with references
	var receiveMessages []interface{}
	for _, msgRef := range channelMessages {
		receiveMessages = append(receiveMessages, msgRef)
	}

	receiveOp := common.AsyncAPIOperation{
		Action: "receive",
		Channel: common.AsyncAPIChannelRef{
			Ref: fmt.Sprintf("#/channels/%s", channelName),
		},
		Title:       fmt.Sprintf("Receive events from %s", path),
		Summary:     fmt.Sprintf("Receive events from SSE endpoint %s", path),
		Description: fmt.Sprintf("Allows clients to receive server-sent events from the endpoint at %s", path),
		Tags:        g.convertToAsyncAPITags(info.Tags),
		Messages:    receiveMessages,
	}

	if info.Authentication {
		receiveOp.Security = []common.AsyncAPISecurityRequirement{
			{"bearerAuth": []string{}},
		}
	}

	g.spec.Operations[receiveOpName] = receiveOp
}

// createAsyncAPIMessage creates an AsyncAPI message from a Go type
func (g *AsyncAPIGenerator) createAsyncAPIMessage(name string, msgType reflect.Type, description string) common.AsyncAPIMessage {
	schema := g.convertTypeToAsyncAPISchema(msgType)

	message := common.AsyncAPIMessage{
		Name:        name,
		Title:       strings.Title(name),
		Summary:     fmt.Sprintf("%s message", name),
		Description: description,
		Payload:     schema,
		ContentType: "application/json",
		// Remove correlationId entirely since we're not using it
		// If you need correlation IDs, set a proper location like:
		// CorrelationId: common.AsyncAPICorrelationId{
		//     Location: "$message.payload#/id",
		//     Description: "Correlation ID found in the message payload id field",
		// },
	}

	// Add message schema to components
	schemaName := fmt.Sprintf("%sSchema", strings.Title(name))
	g.spec.Components.Schemas[schemaName] = schema

	return message
}

// createDefaultWebSocketMessage creates a default WebSocket message
func (g *AsyncAPIGenerator) createDefaultWebSocketMessage() common.AsyncAPIMessage {
	schema := common.AsyncAPISchema{
		Type: "object",
		Properties: map[string]common.AsyncAPISchema{
			"type": {
				Type:        "string",
				Description: "Message type identifier",
				Examples:    []interface{}{"chat", "notification", "status"},
			},
			"data": {
				Type:        "object",
				Description: "Message payload data",
			},
			"id": {
				Type:        "string",
				Description: "Unique message identifier",
				Format:      "uuid",
			},
			"timestamp": {
				Type:        "string",
				Description: "Message timestamp",
				Format:      "date-time",
			},
		},
		Required: []string{"type", "data"},
	}

	return common.AsyncAPIMessage{
		Name:        "default",
		Title:       "Default WebSocket Message",
		Summary:     "Default WebSocket message format",
		Description: "Standard WebSocket message structure with type, data, and metadata",
		Payload:     schema,
		ContentType: "application/json",
		// Removed correlationId to avoid validation errors
	}
}

// createDefaultSSEMessage creates a default SSE message
func (g *AsyncAPIGenerator) createDefaultSSEMessage() common.AsyncAPIMessage {
	schema := common.AsyncAPISchema{
		Type: "object",
		Properties: map[string]common.AsyncAPISchema{
			"type": {
				Type:        "string",
				Description: "Event type identifier",
				Examples:    []interface{}{"update", "notification", "heartbeat"},
			},
			"data": {
				Type:        "object",
				Description: "Event payload data",
			},
			"id": {
				Type:        "string",
				Description: "Unique event identifier",
			},
			"retry": {
				Type:        "integer",
				Description: "Retry interval in milliseconds",
				Minimum:     ptrFloat64(0),
			},
		},
		Required: []string{"type", "data"},
	}

	return common.AsyncAPIMessage{
		Name:        "default",
		Title:       "Default SSE Event",
		Summary:     "Default Server-Sent Event format",
		Description: "Standard SSE event structure with type, data, and metadata",
		Payload:     schema,
		ContentType: "text/plain",
		// Removed correlationId to avoid validation errors
	}
}

// convertTypeToAsyncAPISchema converts a Go type to AsyncAPI schema
func (g *AsyncAPIGenerator) convertTypeToAsyncAPISchema(t reflect.Type) common.AsyncAPISchema {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	switch t.Kind() {
	case reflect.String:
		return common.AsyncAPISchema{Type: "string"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return common.AsyncAPISchema{Type: "integer"}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return common.AsyncAPISchema{Type: "integer", Minimum: ptrFloat64(0)}
	case reflect.Float32, reflect.Float64:
		return common.AsyncAPISchema{Type: "number"}
	case reflect.Bool:
		return common.AsyncAPISchema{Type: "boolean"}
	case reflect.Slice, reflect.Array:
		return common.AsyncAPISchema{
			Type:  "array",
			Items: g.convertTypeToAsyncAPISchemaInterface(t.Elem()),
		}
	case reflect.Map:
		return common.AsyncAPISchema{
			Type:                 "object",
			AdditionalProperties: g.convertTypeToAsyncAPISchemaInterface(t.Elem()),
		}
	case reflect.Struct:
		return g.generateAsyncAPIStructSchema(t)
	case reflect.Interface:
		return common.AsyncAPISchema{Type: "object"}
	default:
		return common.AsyncAPISchema{Type: "string"}
	}
}

// convertTypeToAsyncAPISchemaInterface converts type to interface{} for AsyncAPI items
func (g *AsyncAPIGenerator) convertTypeToAsyncAPISchemaInterface(t reflect.Type) interface{} {
	schema := g.convertTypeToAsyncAPISchema(t)
	return schema
}

// generateAsyncAPIStructSchema generates AsyncAPI schema for struct types
func (g *AsyncAPIGenerator) generateAsyncAPIStructSchema(t reflect.Type) common.AsyncAPISchema {
	schema := common.AsyncAPISchema{
		Type:       "object",
		Properties: make(map[string]common.AsyncAPISchema),
		Required:   []string{},
	}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		if !field.IsExported() {
			continue
		}

		fieldName := g.getJSONFieldName(field)
		if fieldName == "-" {
			continue
		}

		fieldSchema := g.convertTypeToAsyncAPISchema(field.Type)

		// Add field description from tags
		if desc := field.Tag.Get("description"); desc != "" {
			fieldSchema.Description = desc
		}

		// Add example from tags
		if example := field.Tag.Get("example"); example != "" {
			fieldSchema.Examples = []interface{}{example}
		}

		schema.Properties[fieldName] = fieldSchema

		// Check if field is required
		if field.Tag.Get("required") == "true" ||
			strings.Contains(field.Tag.Get("validate"), "required") {
			schema.Required = append(schema.Required, fieldName)
		}
	}

	return schema
}

// getJSONFieldName gets the JSON field name from struct field
func (g *AsyncAPIGenerator) getJSONFieldName(field reflect.StructField) string {
	if jsonTag := field.Tag.Get("json"); jsonTag != "" {
		parts := strings.Split(jsonTag, ",")
		if parts[0] != "" {
			return parts[0]
		}
	}
	return field.Name
}

// convertToAsyncAPITags converts string tags to AsyncAPI tags
func (g *AsyncAPIGenerator) convertToAsyncAPITags(tags []string) []common.AsyncAPITag {
	asyncTags := make([]common.AsyncAPITag, len(tags))
	for i, tag := range tags {
		asyncTags[i] = common.AsyncAPITag{
			Name:        tag,
			Description: fmt.Sprintf("Tag for %s operations", tag),
		}
	}
	return asyncTags
}

// sanitizeChannelName creates a valid channel name from a path
func (g *AsyncAPIGenerator) sanitizeChannelName(path string) string {
	// Remove leading slash and replace special characters
	name := strings.TrimPrefix(path, "/")
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, ":", "")
	name = strings.ReplaceAll(name, "-", "_")

	if name == "" {
		name = "root"
	}

	return name
}

// addDefaultSecuritySchemes adds default security schemes for AsyncAPI
func (g *AsyncAPIGenerator) addDefaultSecuritySchemes() {
	g.spec.Components.SecuritySchemes["bearerAuth"] = common.AsyncAPISecurityScheme{
		Type:        "http",
		Scheme:      "bearer",
		Description: "JWT Bearer token authentication",
	}

	g.spec.Components.SecuritySchemes["apiKey"] = common.AsyncAPISecurityScheme{
		Type:        "apiKey",
		In:          "header",
		Name:        "X-API-Key",
		Description: "API key authentication",
	}
}

// GetSpec returns the AsyncAPI specification
func (g *AsyncAPIGenerator) GetSpec() *common.AsyncAPISpec {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.spec
}

// GetSpecYAML returns the AsyncAPI specification as YAML
func (g *AsyncAPIGenerator) GetSpecYAML() ([]byte, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	// First convert to JSON to ensure proper serialization
	jsonData, err := json.Marshal(g.spec)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal spec to JSON: %w", err)
	}

	// Parse JSON into generic interface for YAML conversion
	var intermediate interface{}
	if err := json.Unmarshal(jsonData, &intermediate); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON for YAML conversion: %w", err)
	}

	// Convert to YAML
	yamlData, err := yaml.Marshal(intermediate)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal to YAML: %w", err)
	}

	return yamlData, nil
}

// GetSpecJSON returns the AsyncAPI specification as JSON
func (g *AsyncAPIGenerator) GetSpecJSON() ([]byte, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return json.MarshalIndent(g.spec, "", "  ")
}

// UpdateSpec allows updating the AsyncAPI specification
func (g *AsyncAPIGenerator) UpdateSpec(updater func(*common.AsyncAPISpec)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	updater(g.spec)
}

// SetInfo updates the AsyncAPI info section
func (g *AsyncAPIGenerator) SetInfo(title, version, description string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.spec.Info.Title = title
	g.spec.Info.Version = version
	g.spec.Info.Description = description
}

// AddServer adds a server to the AsyncAPI specification
func (g *AsyncAPIGenerator) AddServer(name string, server common.AsyncAPIServer) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.spec.Servers[name] = server
}

// createAsyncAPIMessageWithCorrelation creates an AsyncAPI message with correlation ID support
func (g *AsyncAPIGenerator) createAsyncAPIMessageWithCorrelation(name string, msgType reflect.Type, description string, correlationField string) common.AsyncAPIMessage {
	schema := g.convertTypeToAsyncAPISchema(msgType)

	message := common.AsyncAPIMessage{
		Name:        name,
		Title:       strings.Title(name),
		Summary:     fmt.Sprintf("%s message", name),
		Description: description,
		Payload:     schema,
		ContentType: "application/json",
	}

	// Only add correlationId if correlationField is provided and valid
	if correlationField != "" {
		// Validate that the correlation field exists in the message schema
		if g.hasCorrelationField(schema, correlationField) {
			message.CorrelationId = common.AsyncAPICorrelationId{
				Location:    fmt.Sprintf("$message.payload#/%s", correlationField),
				Description: fmt.Sprintf("Correlation ID found in the message payload %s field", correlationField),
			}
		}
	}

	// Add message schema to components
	schemaName := fmt.Sprintf("%sSchema", strings.Title(name))
	g.spec.Components.Schemas[schemaName] = schema

	return message
}

// hasCorrelationField checks if a field exists in the schema for correlation
func (g *AsyncAPIGenerator) hasCorrelationField(schema common.AsyncAPISchema, fieldName string) bool {
	if schema.Properties == nil {
		return false
	}

	_, exists := schema.Properties[fieldName]
	return exists
}
