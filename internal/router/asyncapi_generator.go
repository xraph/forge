package router

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"sync"

	"github.com/xraph/forge/internal/shared"
)

// Type aliases for AsyncAPI types.
type (
	AsyncAPIConfig            = shared.AsyncAPIConfig
	AsyncAPISpec              = shared.AsyncAPISpec
	AsyncAPIInfo              = shared.AsyncAPIInfo
	AsyncAPIServer            = shared.AsyncAPIServer
	AsyncAPIChannel           = shared.AsyncAPIChannel
	AsyncAPIChannelBindings   = shared.AsyncAPIChannelBindings
	AsyncAPIOperation         = shared.AsyncAPIOperation
	AsyncAPIOperationBindings = shared.AsyncAPIOperationBindings
	AsyncAPIChannelReference  = shared.AsyncAPIChannelReference
	AsyncAPIMessageReference  = shared.AsyncAPIMessageReference
	AsyncAPIComponents        = shared.AsyncAPIComponents
	AsyncAPITag               = shared.AsyncAPITag
	WebSocketChannelBinding   = shared.WebSocketChannelBinding
	HTTPChannelBinding        = shared.HTTPChannelBinding
)

// asyncAPIGenerator generates AsyncAPI 3.0.0 specifications from a router.
type asyncAPIGenerator struct {
	config  AsyncAPIConfig
	router  Router
	schemas *asyncAPISchemaGenerator

	// mu, cached, cachedRev and cachedOK play the same role here as on
	// openAPIGenerator, for the same reason: AsyncAPI payloads run through the
	// same schemaGenerator and write into the same kind of shared maps, so
	// concurrent generation was the same fatal concurrent map write.
	mu        sync.Mutex
	cached    *AsyncAPISpec
	cachedRev uint64
	cachedOK  bool
}

// newAsyncAPIGenerator creates a new AsyncAPI generator.
func newAsyncAPIGenerator(config AsyncAPIConfig, router Router) *asyncAPIGenerator {
	// Set defaults
	if config.AsyncAPIVersion == "" {
		config.AsyncAPIVersion = "3.0.0"
	}

	if config.UIPath == "" {
		config.UIPath = "/asyncapi"
	}

	if config.SpecPath == "" {
		config.SpecPath = "/asyncapi.json"
	}

	if config.DefaultContentType == "" {
		config.DefaultContentType = "application/json"
	}

	// Create components map that will be shared with schema generator
	// This allows nested struct types to be registered as components
	componentsSchemas := make(map[string]*Schema)

	return &asyncAPIGenerator{
		config:  config,
		router:  router,
		schemas: newAsyncAPISchemaGenerator(componentsSchemas, nil), // Logger will be set via setLogger if available
	}
}

// Generate returns the AsyncAPI specification for the router's current routes.
//
// The returned document is shared between callers and must be treated as
// read-only; mutating it corrupts every other holder's copy.
func (g *asyncAPIGenerator) Generate() (*AsyncAPISpec, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	rev, versioned := currentRouteRevision(g.router)

	if versioned && g.cachedOK && g.cachedRev == rev {
		return g.cached, nil
	}

	spec, err := g.generate()
	if err != nil {
		return nil, err
	}

	if versioned {
		g.cached, g.cachedRev, g.cachedOK = spec, rev, true
	}

	return spec, nil
}

// generate builds the complete AsyncAPI specification. The caller holds g.mu.
func (g *asyncAPIGenerator) generate() (*AsyncAPISpec, error) {
	// AsyncAPI payloads run through the same schema generator, and so through
	// the same component naming: start a fresh document here too.
	g.schemas.schemaGen.beginSpec()

	spec := &AsyncAPISpec{
		AsyncAPI: g.config.AsyncAPIVersion,
		Info: AsyncAPIInfo{
			Title:        g.config.Title,
			Description:  g.config.Description,
			Version:      g.config.Version,
			Contact:      g.config.Contact,
			License:      g.config.License,
			ExternalDocs: g.config.ExternalDocs,
		},
		// Cloned rather than aliased, matching the OpenAPI generator: Servers is
		// the only mutable collection AsyncAPIConfig hands straight to the
		// document, and every returned spec would otherwise share that one map
		// with the config and with each other. (AsyncAPIConfig has no Tags or
		// Security fields, so there is no third case here.)
		Servers:    maps.Clone(g.config.Servers),
		Channels:   make(map[string]*AsyncAPIChannel),
		Operations: make(map[string]*AsyncAPIOperation),
		Components: &AsyncAPIComponents{
			Schemas:  g.schemas.components, // Use the shared components map for nested struct types
			Messages: make(map[string]*AsyncAPIMessage),
		},
	}

	// Process all routes
	routes := g.router.Routes()
	for _, route := range routes {
		if err := g.processRoute(spec, route); err != nil {
			return nil, err
		}
	}

	// Settle component names now that every payload type is known.
	g.schemas.schemaGen.finalizeComponentNames()

	// Detach the components map, as the OpenAPI generator does: the next
	// beginSpec clears the generator's map in place, and a document already
	// returned to a caller must not empty out underneath them.
	spec.Components.Schemas = maps.Clone(g.schemas.components)

	return spec, nil
}

// processRoute converts a route to AsyncAPI channels and operations.
func (g *asyncAPIGenerator) processRoute(spec *AsyncAPISpec, route RouteInfo) error {
	// Check if route is excluded from AsyncAPI
	if exclude, ok := route.Metadata["asyncapi.exclude"].(bool); ok && exclude {
		return nil // Skip this route
	}

	// Check if this is a streaming route (WebSocket or SSE)
	routeType := g.getRouteType(route)

	switch routeType {
	case "websocket":
		if err := g.processWebSocketRoute(spec, route); err != nil {
			return err
		}
	case "sse":
		if err := g.processSSERoute(spec, route); err != nil {
			return err
		}
	default:
		// Not a streaming route, skip
		return nil
	}

	return nil
}

// getRouteType determines if a route is WebSocket, SSE, or regular HTTP.
func (g *asyncAPIGenerator) getRouteType(route RouteInfo) string {
	// Check metadata for streaming indicators
	if _, ok := route.Metadata["asyncapi.ws.send"]; ok {
		return "websocket"
	}

	if _, ok := route.Metadata["asyncapi.ws.receive"]; ok {
		return "websocket"
	}

	if _, ok := route.Metadata["asyncapi.sse.messages"]; ok {
		return "sse"
	}

	// Check route metadata for route type marker (set by router.WebSocket() or router.EventStream())
	if routeType, ok := route.Metadata["route.type"].(string); ok {
		return routeType
	}

	return "http"
}

// processWebSocketRoute processes a WebSocket route.
func (g *asyncAPIGenerator) processWebSocketRoute(spec *AsyncAPISpec, route RouteInfo) error {
	// Generate channel ID from path
	channelID := g.getChannelID(route)

	// Create channel
	channel := &AsyncAPIChannel{
		Address:     createChannelAddress(route.Path),
		Title:       route.Summary,
		Description: route.Description,
		Parameters:  extractChannelParameters(route.Path),
		Tags:        g.getAsyncAPITags(route),
	}

	// Apply channel metadata
	if desc, ok := route.Metadata["asyncapi.channel.description"].(string); ok {
		channel.Description = desc
	}

	if summary, ok := route.Metadata["asyncapi.channel.summary"].(string); ok {
		channel.Summary = summary
	}

	// Process client-generation stream bindings (forge.client.streamBindings) into x-forge-stream
	applyForgeStreamExtension(channel, route.Metadata)

	// Add WebSocket binding
	channel.Bindings = &AsyncAPIChannelBindings{
		WS: &WebSocketChannelBinding{
			Method:         "GET",
			BindingVersion: "latest",
		},
	}

	// Process messages
	messages := make(map[string]*AsyncAPIMessage)

	// Send messages (client -> server)
	if sendSchema, ok := route.Metadata["asyncapi.ws.send"]; ok && sendSchema != nil {
		msg, err := g.schemas.GenerateMessageSchema(sendSchema, g.config.DefaultContentType)
		if err != nil {
			return err
		}

		if msg != nil {
			msg.Name = "SendMessage"
			msg.Title = "Client to Server Message"
			msg.Summary = "Messages sent from client to server"
			messages["send"] = msg

			// Add to components for reuse
			msgID := channelID + "SendMessage"
			spec.Components.Messages[msgID] = msg
		}
	}

	// Receive messages (server -> client)
	if receiveSchema, ok := route.Metadata["asyncapi.ws.receive"]; ok && receiveSchema != nil {
		msg, err := g.schemas.GenerateMessageSchema(receiveSchema, g.config.DefaultContentType)
		if err != nil {
			return err
		}

		if msg != nil {
			msg.Name = "ReceiveMessage"
			msg.Title = "Server to Client Message"
			msg.Summary = "Messages sent from server to client"
			messages["receive"] = msg

			// Add to components for reuse
			msgID := channelID + "ReceiveMessage"
			spec.Components.Messages[msgID] = msg
		}
	}

	channel.Messages = messages
	spec.Channels[channelID] = channel

	// Create operations
	operationIDPrefix := g.getOperationID(route, channelID)

	// Send operation (client sends to server)
	if _, ok := messages["send"]; ok {
		sendOp := &AsyncAPIOperation{
			Action: "send",
			Channel: &AsyncAPIChannelReference{
				Ref: "#/channels/" + channelID,
			},
			Title:       "Send message",
			Summary:     "Send messages to " + route.Path,
			Description: route.Description,
			Tags:        g.getAsyncAPITags(route),
			Messages: []AsyncAPIMessageReference{
				{Ref: "#/channels/" + channelID + "/messages/send"},
			},
		}

		spec.Operations[operationIDPrefix+"Send"] = sendOp
	}

	// Receive operation (server sends to client)
	if _, ok := messages["receive"]; ok {
		receiveOp := &AsyncAPIOperation{
			Action: "receive",
			Channel: &AsyncAPIChannelReference{
				Ref: "#/channels/" + channelID,
			},
			Title:       "Receive message",
			Summary:     "Receive messages from " + route.Path,
			Description: route.Description,
			Tags:        g.getAsyncAPITags(route),
			Messages: []AsyncAPIMessageReference{
				{Ref: "#/channels/" + channelID + "/messages/receive"},
			},
		}

		spec.Operations[operationIDPrefix+"Receive"] = receiveOp
	}

	return nil
}

// processSSERoute processes a Server-Sent Events route.
func (g *asyncAPIGenerator) processSSERoute(spec *AsyncAPISpec, route RouteInfo) error {
	// Generate channel ID from path
	channelID := g.getChannelID(route)

	// Create channel
	channel := &AsyncAPIChannel{
		Address:     createChannelAddress(route.Path),
		Title:       route.Summary,
		Description: route.Description,
		Parameters:  extractChannelParameters(route.Path),
		Tags:        g.getAsyncAPITags(route),
	}

	// Apply channel metadata
	if desc, ok := route.Metadata["asyncapi.channel.description"].(string); ok {
		channel.Description = desc
	}

	if summary, ok := route.Metadata["asyncapi.channel.summary"].(string); ok {
		channel.Summary = summary
	}

	// Process client-generation stream bindings (forge.client.streamBindings) into x-forge-stream
	applyForgeStreamExtension(channel, route.Metadata)

	// Add HTTP binding for SSE
	channel.Bindings = &AsyncAPIChannelBindings{
		HTTP: &HTTPChannelBinding{
			Method:         "GET",
			BindingVersion: "latest",
		},
	}

	// Process SSE messages
	messages := make(map[string]*AsyncAPIMessage)

	if messageSchemas, ok := route.Metadata["asyncapi.sse.messages"].(map[string]any); ok {
		for eventName, schema := range messageSchemas {
			msg, err := g.schemas.GenerateMessageSchema(schema, "text/event-stream")
			if err != nil {
				return err
			}

			if msg != nil {
				msg.Name = eventName
				msg.Title = eventName + " event"
				msg.Summary = "SSE event: " + eventName
				messages[eventName] = msg

				// Add to components
				msgID := channelID + eventName
				spec.Components.Messages[msgID] = msg
			}
		}
	}

	channel.Messages = messages
	spec.Channels[channelID] = channel

	// Create receive operation (SSE is server -> client only)
	operationID := g.getOperationID(route, channelID)

	// Build message references
	var messageRefs []AsyncAPIMessageReference
	for eventName := range messages {
		messageRefs = append(messageRefs, AsyncAPIMessageReference{
			Ref: "#/channels/" + channelID + "/messages/" + eventName,
		})
	}

	receiveOp := &AsyncAPIOperation{
		Action: "receive",
		Channel: &AsyncAPIChannelReference{
			Ref: "#/channels/" + channelID,
		},
		Title:       "Receive SSE events",
		Summary:     "Receive Server-Sent Events from " + route.Path,
		Description: route.Description,
		Tags:        g.getAsyncAPITags(route),
		Messages:    messageRefs,
	}

	spec.Operations[operationID] = receiveOp

	return nil
}

// applyForgeStreamExtension copies stream bindings onto an AsyncAPI channel as
// the x-forge-stream extension.
//
// A channel whose route declared no stream bindings gets no extension at all,
// so a spec generated by an application that never opts in is unchanged from
// today's output.
func applyForgeStreamExtension(channel *AsyncAPIChannel, metadata map[string]any) {
	if metadata == nil {
		return
	}

	bindings, ok := metadata["forge.client.streamBindings"].([]StreamBinding)
	if !ok || len(bindings) == 0 {
		return
	}

	out := make([]map[string]any, 0, len(bindings))
	for _, b := range bindings {
		out = append(out, map[string]any{
			"message":     b.Message,
			"entityType":  b.EntityType,
			"intent":      string(b.Intent),
			"invalidates": b.Invalidates,
		})
	}

	if channel.Extensions == nil {
		channel.Extensions = make(map[string]any)
	}

	channel.Extensions["x-forge-stream"] = out
}

// getChannelID gets or generates a channel ID for a route.
func (g *asyncAPIGenerator) getChannelID(route RouteInfo) string {
	// Check for custom channel name
	if name, ok := route.Metadata["asyncapi.channelName"].(string); ok {
		return name
	}

	// Generate from path
	return pathToChannelID(route.Path)
}

// getOperationID gets or generates an operation ID for a route.
func (g *asyncAPIGenerator) getOperationID(route RouteInfo, channelID string) string {
	// Check for custom operation ID
	if id, ok := route.Metadata["asyncapi.operationId"].(string); ok {
		return id
	}

	// Use route name if available
	if route.Name != "" {
		return route.Name
	}

	// Generate from channel ID
	return channelID
}

// getAsyncAPITags gets AsyncAPI tags for a route.
func (g *asyncAPIGenerator) getAsyncAPITags(route RouteInfo) []AsyncAPITag {
	var tags []AsyncAPITag

	// Check for AsyncAPI-specific tags
	if asyncTags, ok := route.Metadata["asyncapi.tags"].([]string); ok {
		for _, tag := range asyncTags {
			tags = append(tags, AsyncAPITag{Name: tag})
		}
	}

	// Fall back to route tags
	if len(tags) == 0 {
		for _, tag := range route.Tags {
			tags = append(tags, AsyncAPITag{Name: tag})
		}
	}

	return tags
}

// RegisterEndpoints registers the AsyncAPI spec and UI endpoints.
func (g *asyncAPIGenerator) RegisterEndpoints() {
	// Register spec endpoint
	if g.config.SpecEnabled {
		_ = g.router.GET(g.config.SpecPath, g.handleSpecEndpoint)
	}

	// Register UI endpoint
	if g.config.UIEnabled {
		_ = g.router.GET(g.config.UIPath, g.handleUIEndpoint)
	}
}

// handleSpecEndpoint serves the AsyncAPI JSON specification.
func (g *asyncAPIGenerator) handleSpecEndpoint(ctx Context) error {
	spec, err := g.Generate()
	if err != nil {
		return err
	}

	ctx.Response().Header().Set("Content-Type", "application/json")

	// No wildcard Access-Control-Allow-Origin here. The spec enumerates every
	// channel, message shape and internal name in the app; making it readable by
	// any origin hands that map to any page the user visits. Apps that genuinely
	// need cross-origin spec access should mount the CORS middleware with an
	// explicit allow-list instead.

	encoder := json.NewEncoder(ctx.Response())
	if g.config.PrettyJSON {
		encoder.SetIndent("", "  ")
	}

	return encoder.Encode(spec)
}

// handleUIEndpoint serves the AsyncAPI UI.
func (g *asyncAPIGenerator) handleUIEndpoint(ctx Context) error {
	html := g.generateUIHTML()

	ctx.Response().Header().Set("Content-Type", "text/html; charset=utf-8")
	ctx.Response().WriteHeader(http.StatusOK)
	_, err := ctx.Response().Write([]byte(html))

	return err
}

// generateUIHTML generates HTML for AsyncAPI Studio viewer.
func (g *asyncAPIGenerator) generateUIHTML() string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>%s - AsyncAPI Documentation</title>
    <link rel="stylesheet" href="https://unpkg.com/@asyncapi/react-component@2.6.5/styles/default.min.css">
</head>
<body>
    <div id="asyncapi"></div>
    <script src="https://unpkg.com/@asyncapi/react-component@2.6.5/browser/standalone/index.js"></script>
    <script>
        AsyncApiStandalone.render({
            schema: {
                url: '%s',
            },
            config: {
                show: {
                    sidebar: true,
                    info: true,
                    operations: true,
                    messages: true,
                    schemas: true,
                    errors: true,
                },
            },
        }, document.getElementById('asyncapi'));
    </script>
</body>
</html>`, g.config.Title, g.config.SpecPath)
}
