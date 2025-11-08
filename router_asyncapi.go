package forge

import "github.com/xraph/forge/internal/router"

// WithAsyncAPI enables AsyncAPI 3.0.0 spec generation.
func WithAsyncAPI(config AsyncAPIConfig) RouterOption {
	return router.WithAsyncAPI(config)
}

// WithWebSocketMessages defines send/receive message schemas for WebSocket endpoints
// sendSchema: messages that the client sends to the server (action: send)
// receiveSchema: messages that the server sends to the client (action: receive).
func WithWebSocketMessages(sendSchema, receiveSchema any) RouteOption {
	return router.WithWebSocketMessages(sendSchema, receiveSchema)
}

// WithSSEMessages defines message schemas for SSE endpoints
// messageSchemas: map of event names to their schemas
// SSE is receive-only (server -> client), so action is always "receive".
func WithSSEMessages(messageSchemas map[string]any) RouteOption {
	return router.WithSSEMessages(messageSchemas)
}

// WithSSEMessage defines a single message schema for SSE endpoints
// eventName: the SSE event name (e.g., "message", "update", "notification")
// schema: the message schema
func WithSSEMessage(eventName string, schema any) RouteOption {
	return router.WithSSEMessage(eventName, schema)
}

// WithAsyncAPIOperationID sets a custom operation ID for AsyncAPI.
func WithAsyncAPIOperationID(id string) RouteOption {
	return router.WithAsyncAPIOperationID(id)
}

// WithAsyncAPITags adds tags to the AsyncAPI operation.
func WithAsyncAPITags(tags ...string) RouteOption {
	return router.WithAsyncAPITags(tags...)
}

// WithAsyncAPIChannelName sets a custom channel name (overrides path-based naming).
func WithAsyncAPIChannelName(name string) RouteOption {
	return router.WithAsyncAPIChannelName(name)
}

// WithAsyncAPIBinding adds protocol-specific bindings
// bindingType: "ws" or "http" or "server" or "channel" or "operation" or "message"
// binding: the binding object (e.g., WebSocketChannelBinding, HTTPServerBinding)
func WithAsyncAPIBinding(bindingType string, binding any) RouteOption {
	return router.WithAsyncAPIBinding(bindingType, binding)
}

// WithMessageExamples adds message examples for send/receive
// direction: "send" or "receive"
// examples: map of example name to example value
func WithMessageExamples(direction string, examples map[string]any) RouteOption {
	return router.WithMessageExamples(direction, examples)
}

// WithMessageExample adds a single message example.
func WithMessageExample(direction, name string, example any) RouteOption {
	return router.WithMessageExample(direction, name, example)
}

// WithAsyncAPIChannelDescription sets the channel description.
func WithAsyncAPIChannelDescription(description string) RouteOption {
	return router.WithAsyncAPIChannelDescription(description)
}

// WithAsyncAPIChannelSummary sets the channel summary.
func WithAsyncAPIChannelSummary(summary string) RouteOption {
	return router.WithAsyncAPIChannelSummary(summary)
}

// WithCorrelationID adds correlation ID configuration for request-reply patterns
// location: runtime expression like "$message.header#/correlationId"
// description: description of the correlation ID
func WithCorrelationID(location, description string) RouteOption {
	return router.WithCorrelationID(location, description)
}

// WithMessageHeaders defines headers schema for messages
// headersSchema: Go type with header:"name" tags.
func WithMessageHeaders(headersSchema any) RouteOption {
	return router.WithMessageHeaders(headersSchema)
}

// WithMessageContentType sets the content type for messages
// Default is "application/json".
func WithMessageContentType(contentType string) RouteOption {
	return router.WithMessageContentType(contentType)
}

// WithAsyncAPIExternalDocs adds external documentation link.
func WithAsyncAPIExternalDocs(url, description string) RouteOption {
	return router.WithAsyncAPIExternalDocs(url, description)
}

// WithServerProtocol specifies which servers this operation should be available on
// serverNames: list of server names from AsyncAPIConfig.Servers.
func WithServerProtocol(serverNames ...string) RouteOption {
	return router.WithServerProtocol(serverNames...)
}

// WithAsyncAPISecurity adds security requirements to the operation.
func WithAsyncAPISecurity(requirements map[string][]string) RouteOption {
	return router.WithAsyncAPISecurity(requirements)
}
