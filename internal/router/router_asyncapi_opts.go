package router

// AsyncAPI route options for WebSocket and SSE routes

// WithWebSocketMessages defines send/receive message schemas for WebSocket endpoints
// sendSchema: messages that the client sends to the server (action: send)
// receiveSchema: messages that the server sends to the client (action: receive).
func WithWebSocketMessages(sendSchema, receiveSchema any) RouteOption {
	return &wsMessagesOpt{sendSchema, receiveSchema}
}

type wsMessagesOpt struct {
	send    any
	receive any
}

func (o *wsMessagesOpt) Apply(cfg *RouteConfig) {
	if cfg.Metadata == nil {
		cfg.Metadata = make(map[string]any)
	}

	cfg.Metadata["asyncapi.ws.send"] = o.send
	cfg.Metadata["asyncapi.ws.receive"] = o.receive
}

// WithSSEMessages defines message schemas for SSE endpoints
// messageSchemas: map of event names to their schemas
// SSE is receive-only (server -> client), so action is always "receive".
func WithSSEMessages(messageSchemas map[string]any) RouteOption {
	return &sseMessagesOpt{messageSchemas}
}

type sseMessagesOpt struct {
	messages map[string]any
}

func (o *sseMessagesOpt) Apply(cfg *RouteConfig) {
	if cfg.Metadata == nil {
		cfg.Metadata = make(map[string]any)
	}

	cfg.Metadata["asyncapi.sse.messages"] = o.messages
}

// WithSSEMessage defines a single message schema for SSE endpoints
// eventName: the SSE event name (e.g., "message", "update", "notification")
// schema: the message schema
func WithSSEMessage(eventName string, schema any) RouteOption {
	return &sseMessageOpt{eventName, schema}
}

type sseMessageOpt struct {
	eventName string
	schema    any
}

func (o *sseMessageOpt) Apply(cfg *RouteConfig) {
	if cfg.Metadata == nil {
		cfg.Metadata = make(map[string]any)
	}

	// Get or create the messages map
	var messages map[string]any
	if existing, ok := cfg.Metadata["asyncapi.sse.messages"].(map[string]any); ok {
		messages = existing
	} else {
		messages = make(map[string]any)
		cfg.Metadata["asyncapi.sse.messages"] = messages
	}

	messages[o.eventName] = o.schema
}

// WithAsyncAPIOperationID sets a custom operation ID for AsyncAPI.
func WithAsyncAPIOperationID(id string) RouteOption {
	return WithMetadata("asyncapi.operationId", id)
}

// WithAsyncAPITags adds tags to the AsyncAPI operation.
func WithAsyncAPITags(tags ...string) RouteOption {
	return &asyncAPITagsOpt{tags}
}

type asyncAPITagsOpt struct {
	tags []string
}

func (o *asyncAPITagsOpt) Apply(cfg *RouteConfig) {
	if cfg.Metadata == nil {
		cfg.Metadata = make(map[string]any)
	}

	// Merge with existing AsyncAPI tags
	var existingTags []string
	if existing, ok := cfg.Metadata["asyncapi.tags"].([]string); ok {
		existingTags = existing
	}

	allTags := append(existingTags, o.tags...)
	cfg.Metadata["asyncapi.tags"] = allTags
}

// WithAsyncAPIChannelName sets a custom channel name (overrides path-based naming).
func WithAsyncAPIChannelName(name string) RouteOption {
	return WithMetadata("asyncapi.channelName", name)
}

// WithAsyncAPIBinding adds protocol-specific bindings
// bindingType: "ws" or "http" or "server" or "channel" or "operation" or "message"
// binding: the binding object (e.g., WebSocketChannelBinding, HTTPServerBinding)
func WithAsyncAPIBinding(bindingType string, binding any) RouteOption {
	return &asyncAPIBindingOpt{bindingType, binding}
}

type asyncAPIBindingOpt struct {
	bindingType string
	binding     any
}

func (o *asyncAPIBindingOpt) Apply(cfg *RouteConfig) {
	if cfg.Metadata == nil {
		cfg.Metadata = make(map[string]any)
	}

	// Store bindings in a map
	var bindings map[string]any
	if existing, ok := cfg.Metadata["asyncapi.bindings"].(map[string]any); ok {
		bindings = existing
	} else {
		bindings = make(map[string]any)
		cfg.Metadata["asyncapi.bindings"] = bindings
	}

	bindings[o.bindingType] = o.binding
}

// WithMessageExamples adds message examples for send/receive
// direction: "send" or "receive"
// examples: map of example name to example value
func WithMessageExamples(direction string, examples map[string]any) RouteOption {
	return &messageExamplesOpt{direction, examples}
}

type messageExamplesOpt struct {
	direction string
	examples  map[string]any
}

func (o *messageExamplesOpt) Apply(cfg *RouteConfig) {
	if cfg.Metadata == nil {
		cfg.Metadata = make(map[string]any)
	}

	key := "asyncapi.examples." + o.direction
	cfg.Metadata[key] = o.examples
}

// WithMessageExample adds a single message example.
func WithMessageExample(direction, name string, example any) RouteOption {
	return &messageExampleOpt{direction, name, example}
}

type messageExampleOpt struct {
	direction string
	name      string
	example   any
}

func (o *messageExampleOpt) Apply(cfg *RouteConfig) {
	if cfg.Metadata == nil {
		cfg.Metadata = make(map[string]any)
	}

	key := "asyncapi.examples." + o.direction

	// Get or create the examples map
	var examples map[string]any
	if existing, ok := cfg.Metadata[key].(map[string]any); ok {
		examples = existing
	} else {
		examples = make(map[string]any)
		cfg.Metadata[key] = examples
	}

	examples[o.name] = o.example
}

// WithAsyncAPIChannelDescription sets the channel description.
func WithAsyncAPIChannelDescription(description string) RouteOption {
	return WithMetadata("asyncapi.channel.description", description)
}

// WithAsyncAPIChannelSummary sets the channel summary.
func WithAsyncAPIChannelSummary(summary string) RouteOption {
	return WithMetadata("asyncapi.channel.summary", summary)
}

// WithCorrelationID adds correlation ID configuration for request-reply patterns
// location: runtime expression like "$message.header#/correlationId"
// description: description of the correlation ID
func WithCorrelationID(location, description string) RouteOption {
	return &correlationIDOpt{location, description}
}

type correlationIDOpt struct {
	location    string
	description string
}

func (o *correlationIDOpt) Apply(cfg *RouteConfig) {
	if cfg.Metadata == nil {
		cfg.Metadata = make(map[string]any)
	}

	cfg.Metadata["asyncapi.correlationId"] = map[string]string{
		"location":    o.location,
		"description": o.description,
	}
}

// WithMessageHeaders defines headers schema for messages
// headersSchema: Go type with header:"name" tags.
func WithMessageHeaders(headersSchema any) RouteOption {
	return WithMetadata("asyncapi.message.headers", headersSchema)
}

// WithMessageContentType sets the content type for messages
// Default is "application/json".
func WithMessageContentType(contentType string) RouteOption {
	return WithMetadata("asyncapi.message.contentType", contentType)
}

// WithAsyncAPIExternalDocs adds external documentation link.
func WithAsyncAPIExternalDocs(url, description string) RouteOption {
	return &asyncAPIExternalDocsOpt{url, description}
}

type asyncAPIExternalDocsOpt struct {
	url         string
	description string
}

func (o *asyncAPIExternalDocsOpt) Apply(cfg *RouteConfig) {
	if cfg.Metadata == nil {
		cfg.Metadata = make(map[string]any)
	}

	cfg.Metadata["asyncapi.externalDocs"] = map[string]string{
		"url":         o.url,
		"description": o.description,
	}
}

// WithServerProtocol specifies which servers this operation should be available on
// serverNames: list of server names from AsyncAPIConfig.Servers.
func WithServerProtocol(serverNames ...string) RouteOption {
	return WithMetadata("asyncapi.servers", serverNames)
}

// WithAsyncAPISecurity adds security requirements to the operation.
func WithAsyncAPISecurity(requirements map[string][]string) RouteOption {
	return WithMetadata("asyncapi.security", requirements)
}
