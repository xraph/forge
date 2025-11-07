package forge

import (
	"time"

	"github.com/xraph/forge/internal/errors"
	"github.com/xraph/forge/internal/router"
	"github.com/xraph/forge/internal/shared"
)

// Re-export HTTP error types and constructors for backward compatibility
type HTTPError = errors.HTTPError

var (
	NewHTTPError  = errors.NewHTTPError
	BadRequest    = errors.BadRequest
	Unauthorized  = errors.Unauthorized
	Forbidden     = errors.Forbidden
	NotFound      = errors.NotFound
	InternalError = errors.InternalError
)

// Router provides HTTP routing with multiple backend support
type Router = router.Router

// RouteOption configures a route
type RouteOption = router.RouteOption

// GroupOption configures a route group
type GroupOption = router.GroupOption

// Middleware wraps HTTP handlers
type Middleware = router.Middleware

// RouteConfig holds route configuration
type RouteConfig = router.RouteConfig

// GroupConfig holds route group configuration
type GroupConfig = router.GroupConfig

// RouteInfo provides route information for inspection
type RouteInfo = router.RouteInfo

// RouteExtension represents a route-level extension (e.g., OpenAPI, custom validation)
// Note: This is different from app-level Extension which manages app components
type RouteExtension = router.RouteExtension

// NewRouter creates a new router with options
func NewRouter(opts ...RouterOption) Router {
	return router.NewRouter(opts...)
}

// GetRouter resolves the router from the container
// Returns the router instance and an error if resolution fails
func GetRouter(c Container) (Router, error) {
	return router.GetRouter(c)
}

// RouterOption configures the router
type RouterOption = router.RouterOption

// RouterAdapter wraps a routing backend
type RouterAdapter = router.RouterAdapter

// ErrorHandler handles errors from handlers
type ErrorHandler = shared.ErrorHandler

// NewDefaultErrorHandler creates a default error handler
func NewDefaultErrorHandler(l Logger) ErrorHandler {
	return shared.NewDefaultErrorHandler(l)
}

// Route option constructors
func WithName(name string) RouteOption {
	return router.WithName(name)
}

func WithSummary(summary string) RouteOption {
	return router.WithSummary(summary)
}

func WithDescription(desc string) RouteOption {
	return router.WithDescription(desc)
}

func WithTags(tags ...string) RouteOption {
	return router.WithTags(tags...)
}

func WithMiddleware(mw ...Middleware) RouteOption {
	return router.WithMiddleware(mw...)
}

func WithTimeout(d time.Duration) RouteOption {
	return router.WithTimeout(d)
}

func WithMetadata(key string, value any) RouteOption {
	return router.WithMetadata(key, value)
}

func WithExtension(name string, ext Extension) RouteOption {
	return router.WithExtension(name, ext)
}

func WithOperationID(id string) RouteOption {
	return router.WithOperationID(id)
}

func WithDeprecated() RouteOption {
	return router.WithDeprecated()
}

// OpenAPI Schema Options

// WithRequestSchema sets the unified request schema for OpenAPI generation.
// This is the recommended approach that automatically classifies struct fields based on tags:
//   - path:"paramName" - Path parameter
//   - query:"paramName" - Query parameter
//   - header:"HeaderName" - Header parameter
//   - body:"" or json:"fieldName" - Request body field
//
// Example:
//
//	type CreateUserRequest struct {
//	    TenantID string `path:"tenantId" description:"Tenant ID" format:"uuid"`
//	    DryRun   bool   `query:"dryRun" description:"Preview mode"`
//	    APIKey   string `header:"X-API-Key" description:"API Key"`
//	    Name     string `json:"name" body:"" description:"User name" minLength:"1"`
//	    Email    string `json:"email" body:"" description:"Email" format:"email"`
//	}
//
// If the struct has no path/query/header tags, it's treated as body-only for backward compatibility.
func WithRequestSchema(schemaOrType interface{}) RouteOption {
	return router.WithRequestSchema(schemaOrType)
}

// WithRequestBodySchema sets only the request body schema for OpenAPI generation.
// Use this for explicit body-only schemas when you need separate schemas for different parts.
func WithRequestBodySchema(schemaOrType interface{}) RouteOption {
	return router.WithRequestBodySchema(schemaOrType)
}

// WithResponseSchema sets a response schema for OpenAPI generation
func WithResponseSchema(statusCode int, description string, schemaOrType interface{}) RouteOption {
	return router.WithResponseSchema(statusCode, description, schemaOrType)
}

// WithQuerySchema sets the query parameters schema for OpenAPI generation
func WithQuerySchema(schemaType interface{}) RouteOption {
	return router.WithQuerySchema(schemaType)
}

// WithHeaderSchema sets the header parameters schema for OpenAPI generation
func WithHeaderSchema(schemaType interface{}) RouteOption {
	return router.WithHeaderSchema(schemaType)
}

// WithRequestContentTypes specifies the content types for request body
func WithRequestContentTypes(types ...string) RouteOption {
	return router.WithRequestContentTypes(types...)
}

// WithResponseContentTypes specifies the content types for response body
func WithResponseContentTypes(types ...string) RouteOption {
	return router.WithResponseContentTypes(types...)
}

// OpenAPI Advanced Features

// WithDiscriminator adds discriminator support for polymorphic schemas
func WithDiscriminator(config DiscriminatorConfig) RouteOption {
	return router.WithDiscriminator(config)
}

// WithRequestExample adds an example for the request body
func WithRequestExample(name string, example interface{}) RouteOption {
	return router.WithRequestExample(name, example)
}

// WithResponseExample adds an example for a specific response status code
func WithResponseExample(statusCode int, name string, example interface{}) RouteOption {
	return router.WithResponseExample(statusCode, name, example)
}

// WithSchemaRef adds a schema reference to components
func WithSchemaRef(name string, schema interface{}) RouteOption {
	return router.WithSchemaRef(name, schema)
}

// OpenAPI Response Helpers

// WithPaginatedResponse creates a route option for paginated list responses
func WithPaginatedResponse(itemType interface{}, statusCode int) RouteOption {
	return router.WithPaginatedResponse(itemType, statusCode)
}

// WithErrorResponses adds standard HTTP error responses to a route
func WithErrorResponses() RouteOption {
	return router.WithErrorResponses()
}

// WithStandardRESTResponses adds standard REST CRUD responses for a resource
func WithStandardRESTResponses(resourceType interface{}) RouteOption {
	return router.WithStandardRESTResponses(resourceType)
}

// WithFileUploadResponse creates a response for file upload success
func WithFileUploadResponse(statusCode int) RouteOption {
	return router.WithFileUploadResponse(statusCode)
}

// WithNoContentResponse creates a 204 No Content response
func WithNoContentResponse() RouteOption {
	return router.WithNoContentResponse()
}

// WithCreatedResponse creates a 201 Created response
func WithCreatedResponse(resourceType interface{}) RouteOption {
	return router.WithCreatedResponse(resourceType)
}

// WithAcceptedResponse creates a 202 Accepted response for async operations
func WithAcceptedResponse() RouteOption {
	return router.WithAcceptedResponse()
}

// WithListResponse creates a simple list response (array of items)
func WithListResponse(itemType interface{}, statusCode int) RouteOption {
	return router.WithListResponse(itemType, statusCode)
}

// WithBatchResponse creates a response for batch operations
func WithBatchResponse(itemType interface{}, statusCode int) RouteOption {
	return router.WithBatchResponse(itemType, statusCode)
}

// WithValidationErrorResponse adds a 422 Unprocessable Entity response for validation errors
func WithValidationErrorResponse() RouteOption {
	return router.WithValidationErrorResponse()
}

// Validation Options

// WithValidation adds validation middleware to a route
func WithValidation(enabled bool) RouteOption {
	return router.WithValidation(enabled)
}

// WithStrictValidation enables strict validation (validates both request and response)
func WithStrictValidation() RouteOption {
	return router.WithStrictValidation()
}

// Callback and Webhook Options

// WithCallback adds a callback definition to a route
func WithCallback(config CallbackConfig) RouteOption {
	return router.WithCallback(config)
}

// WithWebhook adds a webhook definition to the OpenAPI spec
func WithWebhook(name string, operation *CallbackOperation) RouteOption {
	return router.WithWebhook(name, operation)
}

// OpenAPI Type Exports

// DiscriminatorConfig defines discriminator for polymorphic types
type DiscriminatorConfig = router.DiscriminatorConfig

// CallbackConfig defines a callback (webhook) for an operation
type CallbackConfig = router.CallbackConfig

// CallbackOperation defines an operation that will be called back
type CallbackOperation = router.CallbackOperation

// ValidationError represents a single field validation error
type ValidationError = router.ValidationError

// ValidationErrors is a collection of validation errors
type ValidationErrors = router.ValidationErrors

// ResponseSchemaDef defines a response schema
type ResponseSchemaDef = router.ResponseSchemaDef

// Callback and Webhook Helpers

// NewCallbackOperation creates a new callback operation
func NewCallbackOperation(summary, description string) *CallbackOperation {
	return router.NewCallbackOperation(summary, description)
}

// NewEventCallbackConfig creates a callback config for event notifications
func NewEventCallbackConfig(callbackURLExpression string, eventSchema interface{}) CallbackConfig {
	return router.NewEventCallbackConfig(callbackURLExpression, eventSchema)
}

// NewStatusCallbackConfig creates a callback config for status updates
func NewStatusCallbackConfig(callbackURLExpression string, statusSchema interface{}) CallbackConfig {
	return router.NewStatusCallbackConfig(callbackURLExpression, statusSchema)
}

// NewCompletionCallbackConfig creates a callback config for async operation completion
func NewCompletionCallbackConfig(callbackURLExpression string, resultSchema interface{}) CallbackConfig {
	return router.NewCompletionCallbackConfig(callbackURLExpression, resultSchema)
}

// Validation Helpers

// NewValidationErrors creates a new ValidationErrors instance
func NewValidationErrors() *ValidationErrors {
	return router.NewValidationErrors()
}

// Group option constructors
func WithGroupMiddleware(mw ...Middleware) GroupOption {
	return router.WithGroupMiddleware(mw...)
}

func WithGroupTags(tags ...string) GroupOption {
	return router.WithGroupTags(tags...)
}

func WithGroupMetadata(key string, value any) GroupOption {
	return router.WithGroupMetadata(key, value)
}

// Router option constructors
func WithAdapter(adapter RouterAdapter) RouterOption {
	return router.WithAdapter(adapter)
}

func WithContainer(container Container) RouterOption {
	return router.WithContainer(container)
}

func WithLogger(logger Logger) RouterOption {
	return router.WithLogger(logger)
}

func WithErrorHandler(handler ErrorHandler) RouterOption {
	return router.WithErrorHandler(handler)
}

func WithRecovery() RouterOption {
	return router.WithRecovery()
}

// oRPC-specific route options for JSON-RPC method metadata

// WithORPCMethod sets a custom JSON-RPC method name for this route.
// By default, method names are generated from the HTTP method and path.
//
// Example:
//
//	router.GET("/users/:id", getUserHandler,
//	    forge.WithORPCMethod("user.get"),
//	)
func WithORPCMethod(methodName string) RouteOption {
	return WithMetadata("orpc.method", methodName)
}

// WithORPCParams sets the params schema for OpenRPC schema generation.
// The schema should be a struct or map describing the expected parameters.
//
// Example:
//
//	type UserGetParams struct {
//	    ID string `json:"id"`
//	}
//	router.GET("/users/:id", getUserHandler,
//	    forge.WithORPCParams(&orpc.ParamsSchema{
//	        Type: "object",
//	        Properties: map[string]*orpc.PropertySchema{
//	            "id": {Type: "string", Description: "User ID"},
//	        },
//	        Required: []string{"id"},
//	    }),
//	)
func WithORPCParams(schema any) RouteOption {
	return WithMetadata("orpc.params", schema)
}

// WithORPCResult sets the result schema for OpenRPC schema generation.
// The schema should be a struct or map describing the expected result.
//
// Example:
//
//	router.GET("/users/:id", getUserHandler,
//	    forge.WithORPCResult(&orpc.ResultSchema{
//	        Type: "object",
//	        Description: "User details",
//	    }),
//	)
func WithORPCResult(schema any) RouteOption {
	return WithMetadata("orpc.result", schema)
}

// WithORPCExclude excludes this route from oRPC auto-exposure.
// Use this to prevent specific routes from being exposed as JSON-RPC methods.
//
// Example:
//
//	router.GET("/internal/debug", debugHandler,
//	    forge.WithORPCExclude(),
//	)
func WithORPCExclude() RouteOption {
	return WithMetadata("orpc.exclude", true)
}

// WithORPCTags adds custom tags for OpenRPC schema organization.
// These tags are used in addition to the route's regular tags.
//
// Example:
//
//	router.GET("/users/:id", getUserHandler,
//	    forge.WithORPCTags("users", "read"),
//	)
func WithORPCTags(tags ...string) RouteOption {
	return WithMetadata("orpc.tags", tags)
}

// WithORPCPrimaryResponse sets which response status code should be used as the primary
// oRPC result schema when multiple success responses (200, 201, etc.) are defined.
// This is useful when you have both 200 and 201 responses and want to explicitly choose one.
//
// By default, oRPC uses method-aware selection:
//   - POST: Prefers 201, then 200
//   - GET: Prefers 200
//   - PUT/PATCH/DELETE: Prefers 200
//
// Example:
//
//	router.POST("/users", createUserHandler,
//	    forge.WithResponseSchema(200, "Updated user", UserResponse{}),
//	    forge.WithResponseSchema(201, "Created user", UserResponse{}),
//	    forge.WithORPCPrimaryResponse(200), // Explicitly use 200
//	)
func WithORPCPrimaryResponse(statusCode int) RouteOption {
	return WithMetadata("orpc.primaryResponse", statusCode)
}

// Authentication route options

// WithAuth adds authentication to a route using one or more providers.
// Multiple providers create an OR condition - any one succeeding allows access.
//
// Example:
//
//	router.GET("/protected", handler,
//	    forge.WithAuth("api-key", "jwt"),
//	)
func WithAuth(providerNames ...string) RouteOption {
	return router.WithAuth(providerNames...)
}

// WithRequiredAuth adds authentication with required scopes/permissions.
// The specified provider must succeed AND the auth context must have all required scopes.
//
// Example:
//
//	router.POST("/admin/users", handler,
//	    forge.WithRequiredAuth("jwt", "write:users", "admin"),
//	)
func WithRequiredAuth(providerName string, scopes ...string) RouteOption {
	return router.WithRequiredAuth(providerName, scopes...)
}

// WithAuthAnd requires ALL specified providers to succeed (AND condition).
// This is useful for multi-factor authentication or combining auth methods.
//
// Example:
//
//	router.GET("/high-security", handler,
//	    forge.WithAuthAnd("api-key", "mfa"),
//	)
func WithAuthAnd(providerNames ...string) RouteOption {
	return router.WithAuthAnd(providerNames...)
}

// Authentication group options

// WithGroupAuth adds authentication to all routes in a group.
// Multiple providers create an OR condition.
//
// Example:
//
//	api := router.Group("/api", forge.WithGroupAuth("jwt"))
func WithGroupAuth(providerNames ...string) GroupOption {
	return router.WithGroupAuth(providerNames...)
}

// WithGroupAuthAnd requires all providers to succeed for all routes in the group.
func WithGroupAuthAnd(providerNames ...string) GroupOption {
	return router.WithGroupAuthAnd(providerNames...)
}

// WithGroupRequiredScopes sets required scopes for all routes in a group.
func WithGroupRequiredScopes(scopes ...string) GroupOption {
	return router.WithGroupRequiredScopes(scopes...)
}
