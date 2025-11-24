package router

import (
	"strconv"
)

// CallbackConfig defines a callback (webhook) for an operation.
type CallbackConfig struct {
	Name       string
	Expression string // Callback URL expression (e.g., "{$request.body#/callbackUrl}")
	Operations map[string]*CallbackOperation
}

// CallbackOperation defines an operation that will be called back.
type CallbackOperation struct {
	Summary     string
	Description string
	RequestBody *RequestBody
	Responses   map[string]*Response
}

// WithCallback adds a callback definition to a route.
func WithCallback(config CallbackConfig) RouteOption {
	return &callbackOpt{config: config}
}

type callbackOpt struct {
	config CallbackConfig
}

func (o *callbackOpt) Apply(config *RouteConfig) {
	if config.Metadata == nil {
		config.Metadata = make(map[string]any)
	}

	callbacks, ok := config.Metadata["openapi.callbacks"].(map[string]CallbackConfig)
	if !ok {
		callbacks = make(map[string]CallbackConfig)
		config.Metadata["openapi.callbacks"] = callbacks
	}

	callbacks[o.config.Name] = o.config
}

// WithWebhook adds a webhook definition to the OpenAPI spec
// Webhooks are documented at the API level, not per-route.
func WithWebhook(name string, operation *CallbackOperation) RouteOption {
	return &webhookOpt{
		name:      name,
		operation: operation,
	}
}

type webhookOpt struct {
	name      string
	operation *CallbackOperation
}

func (o *webhookOpt) Apply(config *RouteConfig) {
	if config.Metadata == nil {
		config.Metadata = make(map[string]any)
	}

	webhooks, ok := config.Metadata["openapi.webhooks"].(map[string]*CallbackOperation)
	if !ok {
		webhooks = make(map[string]*CallbackOperation)
		config.Metadata["openapi.webhooks"] = webhooks
	}

	webhooks[o.name] = o.operation
}

// Helper functions to build callback operations

// NewCallbackOperation creates a new callback operation.
func NewCallbackOperation(summary, description string) *CallbackOperation {
	return &CallbackOperation{
		Summary:     summary,
		Description: description,
		Responses:   make(map[string]*Response),
	}
}

// WithCallbackRequestBody sets the request body for a callback operation.
func (co *CallbackOperation) WithCallbackRequestBody(description string, schema any) *CallbackOperation {
	gen := newSchemaGenerator(nil, nil) // Callbacks use inline schemas

	var schemaObj *Schema

	if s, ok := schema.(*Schema); ok {
		schemaObj = s
	} else {
		var err error
		schemaObj, err = gen.GenerateSchema(schema)
		if err != nil {
			// Return operation with nil schema on error
			return co
		}
	}

	co.RequestBody = &RequestBody{
		Description: description,
		Required:    true,
		Content: map[string]*MediaType{
			"application/json": {
				Schema: schemaObj,
			},
		},
	}

	return co
}

// WithCallbackResponse adds a response to a callback operation.
func (co *CallbackOperation) WithCallbackResponse(statusCode int, description string) *CallbackOperation {
	co.Responses[strconv.Itoa(statusCode)] = &Response{
		Description: description,
	}

	return co
}

// processCallbacks adds callbacks to an operation.
func processCallbacks(operation *Operation, metadata map[string]any) {
	if metadata == nil {
		return
	}

	callbacks, ok := metadata["openapi.callbacks"].(map[string]CallbackConfig)
	if !ok || len(callbacks) == 0 {
		return
	}

	// Convert callbacks to OpenAPI format
	if operation.Callbacks == nil {
		operation.Callbacks = make(map[string]map[string]*PathItem)
	}

	for callbackName, callbackConfig := range callbacks {
		callbackPaths := make(map[string]*PathItem)

		for method, callbackOp := range callbackConfig.Operations {
			pathItem := &PathItem{}

			op := &Operation{
				Summary:     callbackOp.Summary,
				Description: callbackOp.Description,
				RequestBody: callbackOp.RequestBody,
				Responses:   callbackOp.Responses,
			}

			// Set operation based on HTTP method
			switch method {
			case "POST":
				pathItem.Post = op
			case "GET":
				pathItem.Get = op
			case "PUT":
				pathItem.Put = op
			case "DELETE":
				pathItem.Delete = op
			case "PATCH":
				pathItem.Patch = op
			}

			callbackPaths[callbackConfig.Expression] = pathItem
		}

		operation.Callbacks[callbackName] = callbackPaths
	}
}

// processWebhooks adds webhooks to the OpenAPI spec.
func processWebhooks(spec *OpenAPISpec, routes []RouteInfo) {
	webhooksMap := make(map[string]*PathItem)

	// Collect all webhooks from routes
	for _, route := range routes {
		if route.Metadata == nil {
			continue
		}

		webhooks, ok := route.Metadata["openapi.webhooks"].(map[string]*CallbackOperation)
		if !ok {
			continue
		}

		for name, webhookOp := range webhooks {
			pathItem := &PathItem{}

			op := &Operation{
				Summary:     webhookOp.Summary,
				Description: webhookOp.Description,
				RequestBody: webhookOp.RequestBody,
				Responses:   webhookOp.Responses,
			}

			// Webhooks are typically POST operations
			pathItem.Post = op
			webhooksMap[name] = pathItem
		}
	}

	if len(webhooksMap) > 0 {
		spec.Webhooks = webhooksMap
	}
}

// Callback schema helpers

// NewEventCallbackConfig creates a callback config for event notifications.
func NewEventCallbackConfig(callbackURLExpression string, eventSchema any) CallbackConfig {
	return CallbackConfig{
		Name:       "onEvent",
		Expression: callbackURLExpression,
		Operations: map[string]*CallbackOperation{
			"POST": NewCallbackOperation(
				"Event notification",
				"Notifies about an event",
			).WithCallbackRequestBody(
				"Event payload",
				eventSchema,
			).WithCallbackResponse(200, "Event received"),
		},
	}
}

// NewStatusCallbackConfig creates a callback config for status updates.
func NewStatusCallbackConfig(callbackURLExpression string, statusSchema any) CallbackConfig {
	return CallbackConfig{
		Name:       "onStatusChange",
		Expression: callbackURLExpression,
		Operations: map[string]*CallbackOperation{
			"POST": NewCallbackOperation(
				"Status change notification",
				"Notifies about a status change",
			).WithCallbackRequestBody(
				"Status update payload",
				statusSchema,
			).WithCallbackResponse(200, "Status update received"),
		},
	}
}

// NewCompletionCallbackConfig creates a callback config for async operation completion.
func NewCompletionCallbackConfig(callbackURLExpression string, resultSchema any) CallbackConfig {
	return CallbackConfig{
		Name:       "onCompletion",
		Expression: callbackURLExpression,
		Operations: map[string]*CallbackOperation{
			"POST": NewCallbackOperation(
				"Operation completion notification",
				"Notifies when an async operation completes",
			).WithCallbackRequestBody(
				"Completion result",
				resultSchema,
			).WithCallbackResponse(200, "Completion acknowledged"),
		},
	}
}
