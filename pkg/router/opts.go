package router

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/xraph/forge/pkg/common"
)

// =============================================================================
// HANDLER OPTIONS
// =============================================================================

// WithDependencies sets the dependencies for a handler
func WithDependencies(deps ...string) common.HandlerOption {
	return func(info *common.RouteHandlerInfo) {
		info.Dependencies = deps
	}
}

// WithOpenAPITags sets OpenAPI tags for the handler
func WithOpenAPITags(tags ...string) common.HandlerOption {
	return func(info *common.RouteHandlerInfo) {
		if info.Tags == nil {
			info.Tags = make(map[string]string)
		}
		info.Tags["openapi_tags"] = strings.Join(tags, ",")
		info.Tags["group"] = strings.Join(tags, ",")
	}
}

// WithOpenAPIGroup sets OpenAPI group for the handler (alias for WithOpenAPITags)
func WithOpenAPIGroup(group string) common.HandlerOption {
	return WithOpenAPITags(group)
}

// WithTags sets custom tags for the handler
func WithTags(tags map[string]string) common.HandlerOption {
	return func(info *common.RouteHandlerInfo) {
		if info.Tags == nil {
			info.Tags = make(map[string]string)
		}
		for k, v := range tags {
			info.Tags[k] = v
		}
	}
}

// WithTag sets a single tag for the handler
func WithTag(key, value string) common.HandlerOption {
	return func(info *common.RouteHandlerInfo) {
		if info.Tags == nil {
			info.Tags = make(map[string]string)
		}
		info.Tags[key] = value
	}
}

// WithConfig sets configuration for the handler
func WithConfig(config interface{}) common.HandlerOption {
	return func(info *common.RouteHandlerInfo) {
		info.Config = config
	}
}

// WithMiddleware sets middleware for the handler
func WithMiddleware(middleware ...any) common.HandlerOption {
	return func(info *common.RouteHandlerInfo) {
		info.Middleware = middleware
	}
}

// WithOpinionated marks the handler as opinionated for automatic schema generation
func WithOpinionated(opinionated bool) common.HandlerOption {
	return func(info *common.RouteHandlerInfo) {
		info.Opinionated = opinionated
	}
}

// WithSummary sets the OpenAPI summary for the handler
func WithSummary(summary string) common.HandlerOption {
	return func(info *common.RouteHandlerInfo) {
		if info.Tags == nil {
			info.Tags = make(map[string]string)
		}
		info.Tags["summary"] = summary
	}
}

// WithDescription sets the OpenAPI description for the handler
func WithDescription(description string) common.HandlerOption {
	return func(info *common.RouteHandlerInfo) {
		if info.Tags == nil {
			info.Tags = make(map[string]string)
		}
		info.Tags["description"] = description
	}
}

// WithOperation sets the operation type for the handler
func WithOperation(operation string) common.HandlerOption {
	return func(info *common.RouteHandlerInfo) {
		if info.Tags == nil {
			info.Tags = make(map[string]string)
		}
		info.Tags["operation"] = operation
	}
}

// WithComplexity sets the complexity level for the handler
func WithComplexity(complexity string) common.HandlerOption {
	return func(info *common.RouteHandlerInfo) {
		if info.Tags == nil {
			info.Tags = make(map[string]string)
		}
		info.Tags["complexity"] = complexity
	}
}

// WithAuthLevel sets the authentication level required for the handler
func WithAuthLevel(authLevel string) common.HandlerOption {
	return func(info *common.RouteHandlerInfo) {
		if info.Tags == nil {
			info.Tags = make(map[string]string)
		}
		info.Tags["auth_level"] = authLevel
	}
}

// WithRateLimit sets rate limiting for the handler
func WithRateLimit(limit string) common.HandlerOption {
	return func(info *common.RouteHandlerInfo) {
		if info.Tags == nil {
			info.Tags = make(map[string]string)
		}
		info.Tags["rate_limit"] = limit
	}
}

// WithCache sets caching configuration for the handler
func WithCache(cacheConfig string) common.HandlerOption {
	return func(info *common.RouteHandlerInfo) {
		if info.Tags == nil {
			info.Tags = make(map[string]string)
		}
		info.Tags["cache"] = cacheConfig
	}
}

// WithTimeout sets timeout configuration for the handler
func WithTimeout(timeout string) common.HandlerOption {
	return func(info *common.RouteHandlerInfo) {
		if info.Tags == nil {
			info.Tags = make(map[string]string)
		}
		info.Tags["timeout"] = timeout
	}
}

// WithGroupPrefix creates a handler option that sets a prefix for OpenAPI tags
func WithGroupPrefix(prefix string) common.HandlerOption {
	return func(info *common.RouteHandlerInfo) {
		if info.Tags == nil {
			info.Tags = make(map[string]string)
		}
		if existing := info.Tags["group"]; existing != "" {
			info.Tags["group"] = prefix + "." + existing
		} else {
			info.Tags["group"] = prefix
		}
	}
}

// WithGroupTags creates a handler option that adds group-specific tags
func WithGroupTags(tags ...string) common.HandlerOption {
	return func(info *common.RouteHandlerInfo) {
		if info.Tags == nil {
			info.Tags = make(map[string]string)
		}

		existingTags := info.Tags["openapi_tags"]
		if existingTags != "" {
			info.Tags["openapi_tags"] = existingTags + "," + strings.Join(tags, ",")
		} else {
			info.Tags["openapi_tags"] = strings.Join(tags, ",")
		}

		info.Tags["group"] = strings.Join(tags, ",")
	}
}

// WithGroupMiddleware creates a handler option that adds group middleware
func WithGroupMiddleware(middleware ...any) common.HandlerOption {
	return func(info *common.RouteHandlerInfo) {
		info.Middleware = append(info.Middleware, middleware...)
	}
}

// =============================================================================
// PREDEFINED HANDLER CONFIGURATIONS
// =============================================================================

// RESTfulHandler creates options for RESTful API handlers
func RESTfulHandler(resource string, operation string) []common.HandlerOption {
	return []common.HandlerOption{
		WithOpenAPITags(resource),
		WithOperation(operation),
		WithTag("pattern", "restful"),
		WithTag("resource", resource),
	}
}

// CreateHandler creates options for resource creation handlers
func CreateHandler(resource string) []common.HandlerOption {
	return append(RESTfulHandler(resource, "create"),
		WithSummary("Create a new "+resource),
		WithDescription("Creates a new "+resource+" with the provided data"),
		WithAuthLevel("authenticated"),
	)
}

// ReadHandler creates options for resource reading handlers
func ReadHandler(resource string) []common.HandlerOption {
	return append(RESTfulHandler(resource, "read"),
		WithSummary("Get "+resource+" details"),
		WithDescription("Retrieves detailed information about a specific "+resource),
		WithCache("5m"),
	)
}

// UpdateHandler creates options for resource update handlers
func UpdateHandler(resource string) []common.HandlerOption {
	return append(RESTfulHandler(resource, "update"),
		WithSummary("Update "+resource),
		WithDescription("Updates an existing "+resource+" with the provided data"),
		WithAuthLevel("authenticated"),
	)
}

// DeleteHandler creates options for resource deletion handlers
func DeleteHandler(resource string) []common.HandlerOption {
	return append(RESTfulHandler(resource, "delete"),
		WithSummary("Delete "+resource),
		WithDescription("Permanently removes a "+resource+" from the system"),
		WithAuthLevel("authenticated"),
	)
}

// ListHandler creates options for resource listing handlers
func ListHandler(resource string) []common.HandlerOption {
	return append(RESTfulHandler(resource, "list"),
		WithSummary("List "+resource+"s"),
		WithDescription("Retrieves a paginated list of "+resource+"s with optional filtering"),
		WithCache("2m"),
		WithComplexity("medium"),
	)
}

// SearchHandler creates options for search handlers
func SearchHandler(resource string) []common.HandlerOption {
	return []common.HandlerOption{
		WithOpenAPITags(resource, "search"),
		WithOperation("search"),
		WithSummary("Search " + resource + "s"),
		WithDescription("Performs full-text search across " + resource + "s with advanced filtering"),
		WithComplexity("high"),
		WithCache("1m"),
	}
}

// UploadHandler creates options for file upload handlers
func UploadHandler(resource string) []common.HandlerOption {
	return []common.HandlerOption{
		WithOpenAPITags(resource, "upload"),
		WithOperation("upload"),
		WithSummary("Upload file for " + resource),
		WithDescription("Handles file upload and processing for " + resource),
		WithAuthLevel("authenticated"),
		WithComplexity("high"),
		WithTimeout("30s"),
	}
}

// HealthCheckHandler creates options for health check handlers
func HealthCheckHandler() []common.HandlerOption {
	return []common.HandlerOption{
		WithOpenAPITags("health"),
		WithOperation("check"),
		WithSummary("Health check endpoint"),
		WithDescription("Returns the health status of the service and its dependencies"),
		WithCache("10s"),
		WithComplexity("low"),
	}
}

// MetricsHandler creates options for metrics handlers
func MetricsHandler() []common.HandlerOption {
	return []common.HandlerOption{
		WithOpenAPITags("metrics"),
		WithOperation("metrics"),
		WithSummary("Application metrics"),
		WithDescription("Returns application performance and usage metrics"),
		WithAuthLevel("admin"),
		WithComplexity("low"),
	}
}

// AdminHandler creates options for admin-only handlers
func AdminHandler(operation string) []common.HandlerOption {
	return []common.HandlerOption{
		WithOpenAPITags("admin"),
		WithOperation(operation),
		WithAuthLevel("admin"),
		WithComplexity("high"),
		WithTimeout("60s"),
	}
}

// PublicHandler creates options for public handlers (no auth required)
func PublicHandler(group string, operation string) []common.HandlerOption {
	return []common.HandlerOption{
		WithOpenAPITags(group),
		WithOperation(operation),
		WithAuthLevel("none"),
		WithComplexity("low"),
		WithCache("5m"),
	}
}

// =============================================================================
// MIDDLEWARE BUILDERS
// =============================================================================

// AuthenticationMiddleware creates authentication middleware
func AuthenticationMiddleware(authType string) common.MiddlewareDefinition {
	return common.MiddlewareDefinition{
		Name:     "authentication-" + authType,
		Priority: 10,
		Handler: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Authentication logic would go here
				// For now, just pass through
				next.ServeHTTP(w, r)
			})
		},
		Dependencies: []string{},
		Config:       map[string]string{"type": authType},
	}
}

// AuthorizationMiddleware creates authorization middleware
func AuthorizationMiddleware(requiredRole string) common.MiddlewareDefinition {
	return common.MiddlewareDefinition{
		Name:     "authorization-" + requiredRole,
		Priority: 20,
		Handler: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Authorization logic would go here
				// For now, just pass through
				next.ServeHTTP(w, r)
			})
		},
		Dependencies: []string{},
		Config:       map[string]string{"required_role": requiredRole},
	}
}

// CacheMiddleware creates caching middleware
func CacheMiddleware(duration string) common.MiddlewareDefinition {
	return common.MiddlewareDefinition{
		Name:     "cache-" + duration,
		Priority: 50,
		Handler: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Cache logic would go here
				w.Header().Set("Cache-Control", "public, max-age="+duration)
				next.ServeHTTP(w, r)
			})
		},
		Dependencies: []string{},
		Config:       map[string]string{"duration": duration},
	}
}

// RateLimitMiddleware creates rate limiting middleware
func RateLimitMiddleware(limit string) common.MiddlewareDefinition {
	return common.MiddlewareDefinition{
		Name:     "rate-limit-" + limit,
		Priority: 5,
		Handler: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Rate limiting logic would go here
				// For now, just pass through
				next.ServeHTTP(w, r)
			})
		},
		Dependencies: []string{},
		Config:       map[string]string{"limit": limit},
	}
}

// ValidationMiddleware creates request validation middleware
func ValidationMiddleware() common.MiddlewareDefinition {
	return common.MiddlewareDefinition{
		Name:     "validation",
		Priority: 30,
		Handler: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Validation logic would go here
				// For now, just pass through
				next.ServeHTTP(w, r)
			})
		},
		Dependencies: []string{},
		Config:       map[string]string{},
	}
}

// =============================================================================
// ROUTE BUILDERS
// =============================================================================

// RESTfulRoutes creates a complete set of RESTful routes for a resource
type RESTfulRoutes struct {
	Resource    string
	BasePath    string
	IDParam     string
	Controllers map[string]interface{}
}

// BuildRoutes builds all RESTful routes for the resource
func (r *RESTfulRoutes) BuildRoutes(router common.Router) error {
	if r.IDParam == "" {
		r.IDParam = "id"
	}

	basePath := r.BasePath
	if basePath == "" {
		basePath = "/" + strings.ToLower(r.Resource) + "s"
	}

	itemPath := basePath + "/:" + r.IDParam

	// List route: GET /resources
	if handler, exists := r.Controllers["list"]; exists {
		if err := router.GET(basePath, handler, ListHandler(r.Resource)...); err != nil {
			return err
		}
	}

	// Create route: POST /resources
	if handler, exists := r.Controllers["create"]; exists {
		if err := router.POST(basePath, handler, CreateHandler(r.Resource)...); err != nil {
			return err
		}
	}

	// Read route: GET /resources/:id
	if handler, exists := r.Controllers["read"]; exists {
		if err := router.GET(itemPath, handler, ReadHandler(r.Resource)...); err != nil {
			return err
		}
	}

	// Update route: PUT /resources/:id
	if handler, exists := r.Controllers["update"]; exists {
		if err := router.PUT(itemPath, handler, UpdateHandler(r.Resource)...); err != nil {
			return err
		}
	}

	// Partial update route: PATCH /resources/:id
	if handler, exists := r.Controllers["patch"]; exists {
		if err := router.PATCH(itemPath, handler, UpdateHandler(r.Resource)...); err != nil {
			return err
		}
	}

	// Delete route: DELETE /resources/:id
	if handler, exists := r.Controllers["delete"]; exists {
		if err := router.DELETE(itemPath, handler, DeleteHandler(r.Resource)...); err != nil {
			return err
		}
	}

	// Search route: GET /resources/search
	if handler, exists := r.Controllers["search"]; exists {
		if err := router.GET(basePath+"/search", handler, SearchHandler(r.Resource)...); err != nil {
			return err
		}
	}

	// Upload route: POST /resources/:id/upload
	if handler, exists := r.Controllers["upload"]; exists {
		if err := router.POST(itemPath+"/upload", handler, UploadHandler(r.Resource)...); err != nil {
			return err
		}
	}

	return nil
}

// =============================================================================
// CONTROLLER BUILDER
// =============================================================================

// ControllerBuilder helps build controllers with common patterns
type ControllerBuilder struct {
	prefix       string
	name         string
	dependencies []string
	middleware   []any
	routes       []routeConfig
}

// routeConfig stores route configuration for the builder
type routeConfig struct {
	method  string
	pattern string
	handler interface{}
	options []common.HandlerOption
}

// NewControllerBuilder creates a new controller builder
func NewControllerBuilder(name string) *ControllerBuilder {
	return &ControllerBuilder{
		name:         name,
		prefix:       "/",
		dependencies: make([]string, 0),
		middleware:   make([]any, 0),
		routes:       make([]routeConfig, 0),
	}
}

// WithPrefix sets the controller prefix
func (cb *ControllerBuilder) WithPrefix(prefix string) *ControllerBuilder {
	cb.prefix = prefix
	return cb
}

// WithDependency adds a dependency to the controller
func (cb *ControllerBuilder) WithDependency(dependency string) *ControllerBuilder {
	cb.dependencies = append(cb.dependencies, dependency)
	return cb
}

// WithRoute adds a route to the controller
func (cb *ControllerBuilder) WithRoute(method, pattern string, handler interface{}, options ...common.HandlerOption) *ControllerBuilder {
	cb.routes = append(cb.routes, routeConfig{
		method:  method,
		pattern: pattern,
		handler: handler,
		options: options,
	})
	return cb
}

// WithMiddleware adds middleware to the controller
func (cb *ControllerBuilder) WithMiddleware(middleware common.Middleware) *ControllerBuilder {
	cb.middleware = append(cb.middleware, middleware)
	return cb
}

// Build creates the controller
func (cb *ControllerBuilder) Build() common.Controller {
	return &builtController{
		prefix:       cb.prefix,
		name:         cb.name,
		dependencies: cb.dependencies,
		middleware:   cb.middleware,
		routes:       cb.routes,
	}
}

// builtController implements common.Controller with ConfigureRoutes
type builtController struct {
	name         string
	prefix       string
	dependencies []string
	middleware   []any
	routes       []routeConfig
}

func (c *builtController) Name() string {
	return c.name
}

func (c *builtController) Prefix() string {
	return c.prefix
}

func (c *builtController) Dependencies() []string {
	return c.dependencies
}

func (c *builtController) Middleware() []any {
	return c.middleware
}

func (c *builtController) Initialize(container common.Container) error {
	// Default initialization - override if needed
	return nil
}

// ConfigureRoutes implements the flexible route configuration
func (c *builtController) ConfigureRoutes(router common.Router) error {
	for _, route := range c.routes {
		var err error
		switch strings.ToUpper(route.method) {
		case "GET":
			err = router.GET(route.pattern, route.handler, route.options...)
		case "POST":
			err = router.POST(route.pattern, route.handler, route.options...)
		case "PUT":
			err = router.PUT(route.pattern, route.handler, route.options...)
		case "DELETE":
			err = router.DELETE(route.pattern, route.handler, route.options...)
		case "PATCH":
			err = router.PATCH(route.pattern, route.handler, route.options...)
		case "OPTIONS":
			err = router.OPTIONS(route.pattern, route.handler, route.options...)
		case "HEAD":
			err = router.HEAD(route.pattern, route.handler, route.options...)
		default:
			return common.ErrInvalidConfig("method", fmt.Errorf("unsupported HTTP method: %s", route.method))
		}

		if err != nil {
			return fmt.Errorf("failed to register route %s %s: %w", route.method, route.pattern, err)
		}
	}
	return nil
}

// =============================================================================
// ENHANCED RESPONSE BODY OPTIONS
// =============================================================================

// WithResponseContentTypes sets the supported content types for the response
func WithResponseContentTypes(contentTypes ...string) common.HandlerOption {
	return func(info *common.RouteHandlerInfo) {
		if info.Tags == nil {
			info.Tags = make(map[string]string)
		}
		info.Tags["response_content_types"] = strings.Join(contentTypes, ",")
	}
}

// WithJSONResponse indicates the handler returns JSON responses
func WithJSONResponse() common.HandlerOption {
	return WithResponseContentTypes("application/json")
}

// WithXMLResponse indicates the handler returns XML responses
func WithXMLResponse() common.HandlerOption {
	return WithResponseContentTypes("application/xml", "text/xml")
}

// WithHTMLResponse indicates the handler returns HTML responses
func WithHTMLResponse() common.HandlerOption {
	return WithResponseContentTypes("text/html")
}

// WithTextResponse indicates the handler returns plain text responses
func WithTextResponse() common.HandlerOption {
	return WithResponseContentTypes("text/plain")
}

// WithBinaryResponse indicates the handler returns binary responses
func WithBinaryResponse() common.HandlerOption {
	return WithResponseContentTypes("application/octet-stream")
}

// WithMultiFormatResponse indicates the handler supports multiple response formats
func WithMultiFormatResponse() common.HandlerOption {
	return WithResponseContentTypes(
		"application/json",
		"application/xml",
		"text/xml",
		"text/html",
		"text/plain",
	)
}

// WithCustomContentType sets a custom content type for responses
func WithCustomContentType(contentType string) common.HandlerOption {
	return func(info *common.RouteHandlerInfo) {
		if info.Tags == nil {
			info.Tags = make(map[string]string)
		}

		existing := info.Tags["response_content_types"]
		if existing != "" {
			info.Tags["response_content_types"] = existing + "," + contentType
		} else {
			info.Tags["response_content_types"] = contentType
		}
	}
}

// WithResponseExample sets an example response for documentation
func WithResponseExample(example string) common.HandlerOption {
	return func(info *common.RouteHandlerInfo) {
		if info.Tags == nil {
			info.Tags = make(map[string]string)
		}
		info.Tags["response_example"] = example
	}
}

// WithResponseExamples sets multiple examples for different content types
func WithResponseExamples(examples map[string]string) common.HandlerOption {
	return func(info *common.RouteHandlerInfo) {
		if info.Tags == nil {
			info.Tags = make(map[string]string)
		}
		for contentType, example := range examples {
			key := "example-" + strings.ReplaceAll(contentType, "/", "-")
			info.Tags[key] = example
		}
	}
}

// WithStatusCodes sets the possible HTTP status codes for the response
func WithStatusCodes(codes ...int) common.HandlerOption {
	return func(info *common.RouteHandlerInfo) {
		if info.Tags == nil {
			info.Tags = make(map[string]string)
		}
		var strCodes []string
		for _, code := range codes {
			strCodes = append(strCodes, fmt.Sprintf("%d", code))
		}
		info.Tags["status_codes"] = strings.Join(strCodes, ",")
	}
}

// WithResponseHeaders sets custom response headers for documentation
func WithResponseHeaders(headers map[string]string) common.HandlerOption {
	return func(info *common.RouteHandlerInfo) {
		if info.Tags == nil {
			info.Tags = make(map[string]string)
		}
		for header, description := range headers {
			info.Tags["response_header_"+header] = description
		}
	}
}

// WithConditionalResponses indicates the handler may return different response types
func WithConditionalResponses() common.HandlerOption {
	return func(info *common.RouteHandlerInfo) {
		if info.Tags == nil {
			info.Tags = make(map[string]string)
		}
		info.Tags["conditional_responses"] = "true"
	}
}

// WithErrorResponses sets the possible error response codes and descriptions
func WithErrorResponses(errorMap map[int]string) common.HandlerOption {
	return func(info *common.RouteHandlerInfo) {
		if info.Tags == nil {
			info.Tags = make(map[string]string)
		}
		for code, description := range errorMap {
			info.Tags[fmt.Sprintf("error_%d", code)] = description
		}
	}
}

// =============================================================================
// PREDEFINED RESPONSE CONFIGURATIONS
// =============================================================================

// JSONAPIResponse creates options for JSON API responses
func JSONAPIResponse() []common.HandlerOption {
	return []common.HandlerOption{
		WithJSONResponse(),
		WithResponseHeaders(map[string]string{
			"Content-Type":    "application/json",
			"X-Response-Time": "Response processing time in milliseconds",
		}),
		WithStatusCodes(200, 400, 404, 500),
	}
}

// FileDownloadResponse creates options for file download responses
func FileDownloadResponse(contentType string) []common.HandlerOption {
	return []common.HandlerOption{
		WithCustomContentType(contentType),
		WithBinaryResponse(),
		WithResponseHeaders(map[string]string{
			"Content-Disposition": "File download attachment",
			"Content-Length":      "File size in bytes",
			"Content-Type":        contentType + " content type",
		}),
		WithStatusCodes(200, 404, 403, 500),
	}
}

// HTMLPageResponse creates options for HTML page responses
func HTMLPageResponse() []common.HandlerOption {
	return []common.HandlerOption{
		WithHTMLResponse(),
		WithResponseHeaders(map[string]string{
			"Content-Type":  "text/html; charset=utf-8",
			"Cache-Control": "HTML page cache control",
		}),
		WithStatusCodes(200, 404, 500),
	}
}

// APIWithMultipleFormats creates options for APIs supporting multiple formats
func APIWithMultipleFormats() []common.HandlerOption {
	return []common.HandlerOption{
		WithMultiFormatResponse(),
		WithConditionalResponses(),
		WithResponseHeaders(map[string]string{
			"Content-Type": "Response content type based on Accept header",
			"Vary":         "Accept",
		}),
		WithStatusCodes(200, 400, 404, 406, 500),
	}
}

// StreamingResponse creates options for streaming responses
func StreamingResponse(contentType string) []common.HandlerOption {
	return []common.HandlerOption{
		WithCustomContentType(contentType),
		WithResponseHeaders(map[string]string{
			"Transfer-Encoding": "chunked",
			"Content-Type":      contentType,
			"Cache-Control":     "no-cache",
		}),
		WithStatusCodes(200, 404, 500),
	}
}

// CSVExportResponse creates options for CSV export responses
func CSVExportResponse() []common.HandlerOption {
	return []common.HandlerOption{
		WithCustomContentType("text/csv"),
		WithResponseHeaders(map[string]string{
			"Content-Type":        "text/csv; charset=utf-8",
			"Content-Disposition": "attachment; filename=export.csv",
		}),
		WithStatusCodes(200, 400, 404, 500),
	}
}

// PDFResponse creates options for PDF responses
func PDFResponse() []common.HandlerOption {
	return []common.HandlerOption{
		WithCustomContentType("application/pdf"),
		WithBinaryResponse(),
		WithResponseHeaders(map[string]string{
			"Content-Type":        "application/pdf",
			"Content-Disposition": "inline; filename=document.pdf",
		}),
		WithStatusCodes(200, 404, 500),
	}
}

// XMLAPIResponse creates options for XML API responses
func XMLAPIResponse() []common.HandlerOption {
	return []common.HandlerOption{
		WithXMLResponse(),
		WithResponseHeaders(map[string]string{
			"Content-Type": "application/xml; charset=utf-8",
		}),
		WithStatusCodes(200, 400, 404, 500),
	}
}

// WebSocketUpgradeResponse creates options for WebSocket upgrade responses
func WebSocketUpgradeResponse() []common.HandlerOption {
	return []common.HandlerOption{
		WithCustomContentType("application/json"), // Initial response before upgrade
		WithResponseHeaders(map[string]string{
			"Upgrade":              "websocket",
			"Connection":           "Upgrade",
			"Sec-WebSocket-Accept": "WebSocket accept key",
		}),
		WithStatusCodes(101, 400, 404, 426),
	}
}

// ImageResponse creates options for image responses
func ImageResponse(imageType string) []common.HandlerOption {
	contentType := "image/" + imageType
	return []common.HandlerOption{
		WithCustomContentType(contentType),
		WithBinaryResponse(),
		WithResponseHeaders(map[string]string{
			"Content-Type":  contentType,
			"Cache-Control": "public, max-age=3600",
		}),
		WithStatusCodes(200, 404, 500),
	}
}

// RedirectResponse creates options for redirect responses
func RedirectResponse() []common.HandlerOption {
	return []common.HandlerOption{
		WithHTMLResponse(), // Fallback content
		WithResponseHeaders(map[string]string{
			"Location": "Redirect target URL",
		}),
		WithStatusCodes(301, 302, 307, 308),
	}
}

// =============================================================================
// SPECIALIZED HANDLER BUILDERS WITH RESPONSE SUPPORT
// =============================================================================

// RESTfulHandlerWithResponse creates RESTful handler options with response configuration
func RESTfulHandlerWithResponse(resource string, operation string, responseType string) []common.HandlerOption {
	options := RESTfulHandler(resource, operation)

	switch responseType {
	case "json":
		options = append(options, JSONAPIResponse()...)
	case "xml":
		options = append(options, XMLAPIResponse()...)
	case "html":
		options = append(options, HTMLPageResponse()...)
	case "multi":
		options = append(options, APIWithMultipleFormats()...)
	case "binary":
		options = append(options, WithBinaryResponse())
	}

	return options
}

// CreateHandlerWithResponse creates options for resource creation with specific response type
func CreateHandlerWithResponse(resource string, responseType string) []common.HandlerOption {
	options := CreateHandler(resource)
	options = append(options, WithStatusCodes(201, 400, 409, 500))

	switch responseType {
	case "json":
		options = append(options, WithJSONResponse())
	case "xml":
		options = append(options, WithXMLResponse())
	case "multi":
		options = append(options, WithMultiFormatResponse())
	}

	return options
}

// FileUploadHandlerWithResponse creates options for file upload with response configuration
func FileUploadHandlerWithResponse(resource string) []common.HandlerOption {
	return []common.HandlerOption{
		WithOpenAPITags(resource, "upload"),
		WithOperation("upload"),
		WithSummary("Upload file for " + resource),
		WithDescription("Handles multipart file upload with progress tracking and validation"),
		WithAuthLevel("authenticated"),
		WithComplexity("high"),
		WithTimeout("60s"),
		WithJSONResponse(),
		WithStatusCodes(201, 400, 413, 415, 500),
		WithResponseHeaders(map[string]string{
			"Content-Type": "application/json",
			"X-Upload-ID":  "Unique upload identifier",
		}),
	}
}

// =============================================================================
// RESPONSE VALIDATION HELPERS
// =============================================================================

// ValidateResponseContentType validates that a content type is supported
func ValidateResponseContentType(contentType string) bool {
	supportedTypes := []string{
		"application/json",
		"application/xml",
		"text/xml",
		"text/html",
		"text/plain",
		"application/octet-stream",
		"text/csv",
		"application/pdf",
		"image/",
		"video/",
		"audio/",
	}

	for _, supported := range supportedTypes {
		if strings.HasPrefix(contentType, supported) {
			return true
		}
	}

	return false
}

// GetResponseContentTypes extracts content types from handler info tags
func GetResponseContentTypes(info *common.RouteHandlerInfo) []string {
	if info.Tags == nil {
		return []string{"application/json"} // Default
	}

	if contentTypes, exists := info.Tags["response_content_types"]; exists {
		return strings.Split(contentTypes, ",")
	}

	return []string{"application/json"}
}

// HasMultipleResponseFormats checks if handler supports multiple response formats
func HasMultipleResponseFormats(info *common.RouteHandlerInfo) bool {
	contentTypes := GetResponseContentTypes(info)
	return len(contentTypes) > 1
}

// =============================================================================
// MIDDLEWARE FOR RESPONSE HANDLING
// =============================================================================

// ResponseFormatMiddleware creates middleware for handling multiple response formats
func ResponseFormatMiddleware() common.MiddlewareDefinition {
	return common.MiddlewareDefinition{
		Name:     "response-format",
		Priority: 40,
		Handler: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Analyze Accept header and set appropriate response format
				acceptHeader := r.Header.Get("Accept")

				// Set context value for response format preference
				ctx := r.Context()
				ctx = context.WithValue(ctx, "preferred_response_format", acceptHeader)
				r = r.WithContext(ctx)

				next.ServeHTTP(w, r)
			})
		},
		Dependencies: []string{},
	}
}

// ResponseCompressionMiddleware creates middleware for response compression
func ResponseCompressionMiddleware() common.MiddlewareDefinition {
	return common.MiddlewareDefinition{
		Name:     "response-compression",
		Priority: 60,
		Handler: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Check Accept-Encoding header and apply compression
				encoding := r.Header.Get("Accept-Encoding")

				if strings.Contains(encoding, "gzip") {
					w.Header().Set("Content-Encoding", "gzip")
				}

				next.ServeHTTP(w, r)
			})
		},
		Dependencies: []string{},
	}
}
