package orpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge"
)

// ORPC represents the oRPC server interface
type ORPC interface {
	// Method management
	RegisterMethod(method *Method) error
	GetMethod(name string) (*Method, error)
	ListMethods() []Method

	// Route introspection (auto-expose)
	GenerateMethodFromRoute(route forge.RouteInfo) (*Method, error)

	// Execution
	HandleRequest(ctx context.Context, req *Request) *Response
	HandleBatch(ctx context.Context, requests []*Request) []*Response

	// OpenRPC schema
	OpenRPCDocument() *OpenRPCDocument

	// Interceptors
	Use(interceptor Interceptor)

	// Stats
	GetStats() ServerStats

	// Internal
	SetRouter(router forge.Router)
}

// Interceptor wraps method execution
type Interceptor func(ctx context.Context, req *Request, next MethodHandler) (interface{}, error)

// server implements ORPC interface
type server struct {
	config  Config
	logger  forge.Logger
	metrics forge.Metrics
	router  forge.Router // Reference to main router for executing routes

	methods      map[string]*Method
	methodsLock  sync.RWMutex
	interceptors []Interceptor

	// Schema cache
	schemaCache     map[string]*ParamsSchema
	schemaCacheLock sync.RWMutex

	// Stats
	stats     ServerStats
	statsLock sync.RWMutex
}

// NewORPCServer creates a new oRPC server
func NewORPCServer(config Config, logger forge.Logger, metrics forge.Metrics) ORPC {
	return &server{
		config:       config,
		logger:       logger,
		metrics:      metrics,
		methods:      make(map[string]*Method),
		interceptors: make([]Interceptor, 0),
		schemaCache:  make(map[string]*ParamsSchema),
	}
}

// SetRouter sets the router reference for executing routes
func (s *server) SetRouter(router forge.Router) {
	s.router = router
}

// RegisterMethod registers a new JSON-RPC method
func (s *server) RegisterMethod(method *Method) error {
	if method.Name == "" {
		return ErrInvalidMethodName
	}

	s.methodsLock.Lock()
	defer s.methodsLock.Unlock()

	if _, exists := s.methods[method.Name]; exists {
		s.logger.Warn("orpc: method already registered, overwriting",
			forge.F("method", method.Name),
		)
	}

	s.methods[method.Name] = method
	s.logger.Debug("orpc: method registered",
		forge.F("method", method.Name),
		forge.F("description", method.Description),
	)

	if s.metrics != nil && s.config.EnableMetrics {
		s.metrics.Gauge("orpc_methods_total").Set(float64(len(s.methods)))
	}

	return nil
}

// GetMethod retrieves a method by name
func (s *server) GetMethod(name string) (*Method, error) {
	s.methodsLock.RLock()
	defer s.methodsLock.RUnlock()

	method, exists := s.methods[name]
	if !exists {
		return nil, ErrMethodNotFoundError
	}

	return method, nil
}

// ListMethods returns all registered methods
func (s *server) ListMethods() []Method {
	s.methodsLock.RLock()
	defer s.methodsLock.RUnlock()

	methods := make([]Method, 0, len(s.methods))
	for _, method := range s.methods {
		methods = append(methods, *method)
	}

	return methods
}

// GenerateMethodFromRoute creates a JSON-RPC method from a Forge route
func (s *server) GenerateMethodFromRoute(route forge.RouteInfo) (*Method, error) {
	// Check if route explicitly excludes oRPC
	if exclude, ok := route.Metadata["orpc.exclude"].(bool); ok && exclude {
		return nil, fmt.Errorf("route explicitly excluded from oRPC")
	}

	// Get custom method name or generate from route
	methodName := s.generateMethodName(route)

	// Apply prefix if configured
	if s.config.MethodPrefix != "" {
		methodName = s.config.MethodPrefix + methodName
	}

	// Generate params schema
	paramsSchema := s.generateParamsSchema(route)

	// Generate result schema
	resultSchema := s.generateResultSchema(route)

	// Create handler that executes the route
	handler := s.createRouteHandler(route)

	method := &Method{
		Name:        methodName,
		Description: route.Summary,
		Params:      paramsSchema,
		Result:      resultSchema,
		Handler:     handler,
		RouteInfo:   route,
		Tags:        route.Tags,
		Deprecated:  false,
		Metadata:    route.Metadata,
	}

	return method, nil
}

// generateMethodName generates a JSON-RPC method name from a route
func (s *server) generateMethodName(route forge.RouteInfo) string {
	// Check for custom method name in metadata
	if customName, ok := route.Metadata["orpc.method"].(string); ok {
		return customName
	}

	// Use naming strategy
	switch s.config.NamingStrategy {
	case "method":
		// Use route.Name if available
		if route.Name != "" {
			return route.Name
		}
		fallthrough
	case "path":
		fallthrough
	default:
		// Generate from HTTP method + path
		return s.pathToMethodName(route.Method, route.Path)
	}
}

// pathToMethodName converts HTTP method and path to RPC method name
func (s *server) pathToMethodName(httpMethod, path string) string {
	// Convert: GET /users/:id -> get.users.id
	// Or: POST /api/v1/posts -> create.api.v1.posts

	path = strings.TrimPrefix(path, "/")
	path = strings.ReplaceAll(path, "/", ".")
	path = strings.ReplaceAll(path, ":", "")
	path = strings.ReplaceAll(path, "{", "")
	path = strings.ReplaceAll(path, "}", "")
	path = strings.ReplaceAll(path, "-", "_")

	// Method prefix
	var prefix string
	switch httpMethod {
	case "POST":
		prefix = "create"
	case "GET":
		prefix = "get"
	case "PUT":
		prefix = "update"
	case "PATCH":
		prefix = "patch"
	case "DELETE":
		prefix = "delete"
	default:
		prefix = strings.ToLower(httpMethod)
	}

	if path == "" {
		return prefix
	}

	return prefix + "." + path
}

// generateParamsSchema generates parameter schema from route
func (s *server) generateParamsSchema(route forge.RouteInfo) *ParamsSchema {
	// 1. Check for explicit oRPC params (backward compat - highest priority)
	if customSchema, ok := route.Metadata["orpc.params"]; ok {
		if schema, ok := customSchema.(*ParamsSchema); ok {
			return schema
		}
	}

	// 2. Use unified request schema from OpenAPI (NEW!)
	if unifiedSchema, ok := route.Metadata["openapi.requestSchema.unified"]; ok {
		return s.convertUnifiedSchemaToORPCParams(unifiedSchema, route)
	}

	// 3. Check for OpenAPI request type (legacy support)
	if requestType, ok := route.Metadata["openapi.requestType"]; ok {
		return s.generateParamsFromReflection(requestType, route)
	}

	// 4. Fallback: Auto-generate basic schema
	return s.autoGenerateParamsSchema(route)
}

// generateParamsFromReflection generates params schema from a reflected type
func (s *server) generateParamsFromReflection(requestType interface{}, route forge.RouteInfo) *ParamsSchema {
	rt := reflect.TypeOf(requestType)
	if rt == nil {
		return s.autoGenerateParamsSchema(route)
	}

	if rt.Kind() == reflect.Ptr {
		rt = rt.Elem()
	}

	if rt.Kind() != reflect.Struct {
		return s.autoGenerateParamsSchema(route)
	}

	schema := &ParamsSchema{
		Type:       "object",
		Properties: make(map[string]*PropertySchema),
		Required:   []string{},
	}

	// Extract path parameters first
	pathParams := extractPathParams(route.Path)
	for _, param := range pathParams {
		schema.Properties[param] = &PropertySchema{
			Type:        "string",
			Description: fmt.Sprintf("Path parameter: %s", param),
		}
		schema.Required = append(schema.Required, param)
	}

	// Parse struct fields
	bodyProps := make(map[string]*PropertySchema)
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if !field.IsExported() {
			continue
		}

		jsonName := field.Name
		if jsonTag := field.Tag.Get("json"); jsonTag != "" {
			name, _ := parseJSONTag(jsonTag)
			if name != "" && name != "-" {
				jsonName = name
			}
		}

		bodyProps[jsonName] = &PropertySchema{
			Type:        getJSONTypeFromReflectType(field.Type),
			Description: field.Tag.Get("description"),
		}
	}

	if len(bodyProps) > 0 {
		schema.Properties["body"] = &PropertySchema{
			Type:        "object",
			Description: "Request body",
			Properties:  bodyProps,
		}
	}

	return schema
}

// autoGenerateParamsSchema is the fallback auto-generation logic
func (s *server) autoGenerateParamsSchema(route forge.RouteInfo) *ParamsSchema {
	schema := &ParamsSchema{
		Type:       "object",
		Properties: make(map[string]*PropertySchema),
		Required:   []string{},
	}

	// Extract path parameters
	pathParams := extractPathParams(route.Path)
	for _, param := range pathParams {
		schema.Properties[param] = &PropertySchema{
			Type:        "string",
			Description: fmt.Sprintf("Path parameter: %s", param),
		}
		schema.Required = append(schema.Required, param)
	}

	// Add body for POST/PUT/PATCH
	if route.Method == "POST" || route.Method == "PUT" || route.Method == "PATCH" {
		schema.Properties["body"] = &PropertySchema{
			Type:        "object",
			Description: "Request body",
		}
	}

	// Add query parameters
	schema.Properties["query"] = &PropertySchema{
		Type:        "object",
		Description: "Query parameters (optional)",
	}

	return schema
}

// generateResultSchema generates result schema from route
func (s *server) generateResultSchema(route forge.RouteInfo) *ResultSchema {
	// 1. Check for explicit oRPC result (backward compat - highest priority)
	if customSchema, ok := route.Metadata["orpc.result"]; ok {
		if schema, ok := customSchema.(*ResultSchema); ok {
			return schema
		}
	}

	// 2. Use OpenAPI response schemas with smart selection (NEW!)
	if responseSchemas, ok := route.Metadata["openapi.responseSchemas"].(map[int]*ResponseSchemaDef); ok {
		_, responseDef := s.selectPrimaryResponseSchema(route, responseSchemas)
		if responseDef != nil {
			return convertOpenAPIResponseToORPC(responseDef)
		}
	}

	// 3. Fallback: Basic schema
	return &ResultSchema{
		Type:        "object",
		Description: "Response from " + route.Path,
	}
}

// extractPathParams extracts parameter names from path
func extractPathParams(path string) []string {
	var params []string
	parts := strings.Split(path, "/")
	for _, part := range parts {
		// Handle :param style
		if strings.HasPrefix(part, ":") {
			params = append(params, strings.TrimPrefix(part, ":"))
		}
		// Handle {param} style
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			param := strings.TrimSuffix(strings.TrimPrefix(part, "{"), "}")
			params = append(params, param)
		}
	}
	return params
}

// createRouteHandler creates a handler that executes the underlying route
func (s *server) createRouteHandler(route forge.RouteInfo) MethodHandler {
	return func(ctx interface{}, params interface{}) (interface{}, error) {
		reqCtx, ok := ctx.(context.Context)
		if !ok {
			return nil, fmt.Errorf("invalid context type")
		}

		// Convert JSON-RPC params to HTTP request
		req, err := s.buildHTTPRequest(reqCtx, route, params)
		if err != nil {
			return nil, err
		}

		// Execute the route via the router
		result, err := s.executeRoute(req, route)
		if err != nil {
			return nil, err
		}

		return result, nil
	}
}

// buildHTTPRequest builds an HTTP request from JSON-RPC params
func (s *server) buildHTTPRequest(ctx context.Context, route forge.RouteInfo, params interface{}) (*http.Request, error) {
	// Parse params
	paramsMap, ok := params.(map[string]interface{})
	if !ok {
		// Try to marshal and unmarshal
		paramsJSON, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("invalid params: %w", err)
		}
		if err := json.Unmarshal(paramsJSON, &paramsMap); err != nil {
			return nil, fmt.Errorf("invalid params format: %w", err)
		}
	}

	// Build the request path with parameters
	path := route.Path
	pathParams := extractPathParams(route.Path)
	for _, param := range pathParams {
		if val, ok := paramsMap[param]; ok {
			placeholder := ":" + param
			if !strings.Contains(path, placeholder) {
				placeholder = "{" + param + "}"
			}
			path = strings.ReplaceAll(path, placeholder, fmt.Sprintf("%v", val))
		}
	}

	// Build query string
	query := ""
	if queryArgs, ok := paramsMap["query"].(map[string]interface{}); ok {
		var queryParts []string
		for k, v := range queryArgs {
			queryParts = append(queryParts, fmt.Sprintf("%s=%v", k, v))
		}
		if len(queryParts) > 0 {
			query = "?" + strings.Join(queryParts, "&")
		}
	}

	// Extract body
	var bodyData []byte
	if body, ok := paramsMap["body"]; ok {
		var err error
		bodyData, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
	}

	s.logger.Debug("orpc: executing route",
		forge.F("method", route.Method),
		forge.F("path", path+query),
	)

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, route.Method, path+query, bytes.NewReader(bodyData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if len(bodyData) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}

	return req, nil
}

// executeRoute executes the route via the router
func (s *server) executeRoute(req *http.Request, route forge.RouteInfo) (interface{}, error) {
	if s.router == nil {
		return nil, fmt.Errorf("router not set")
	}

	// Create a response recorder
	rec := httptest.NewRecorder()

	// Execute via router
	s.router.ServeHTTP(rec, req)

	// Parse response
	result := rec.Result()
	defer result.Body.Close()

	// Check for errors
	if result.StatusCode >= 400 {
		return nil, fmt.Errorf("route returned status %d", result.StatusCode)
	}

	// Parse JSON response
	var responseData interface{}
	if err := json.NewDecoder(result.Body).Decode(&responseData); err != nil {
		// If not JSON, return raw body
		return map[string]interface{}{
			"status": result.StatusCode,
			"body":   rec.Body.String(),
		}, nil
	}

	return responseData, nil
}

// HandleRequest handles a single JSON-RPC request
func (s *server) HandleRequest(ctx context.Context, req *Request) *Response {
	start := time.Now()

	// Update stats
	s.incrementStat("requests")
	defer func() {
		s.updateLatency(time.Since(start))
	}()

	// Validate request
	if req.JSONRPC != "2.0" {
		return NewErrorResponse(req.ID, ErrInvalidRequest, "Invalid JSON-RPC version")
	}

	if req.Method == "" {
		return NewErrorResponse(req.ID, ErrInvalidRequest, "Method name is required")
	}

	// Get method
	method, err := s.GetMethod(req.Method)
	if err != nil {
		s.incrementStat("errors")
		if s.metrics != nil && s.config.EnableMetrics {
			s.metrics.Counter("orpc_requests_total", "status", "not_found").Inc()
		}
		return NewErrorResponse(req.ID, ErrMethodNotFound, fmt.Sprintf("Method '%s' not found", req.Method))
	}

	// Parse params
	var params interface{}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			s.incrementStat("errors")
			return NewErrorResponse(req.ID, ErrInvalidParams, "Invalid parameters")
		}
	}

	// Execute method with interceptors
	result, err := s.executeWithInterceptors(ctx, req, method, params)
	if err != nil {
		s.incrementStat("errors")
		if s.metrics != nil && s.config.EnableMetrics {
			s.metrics.Counter("orpc_requests_total", "status", "error", "method", req.Method).Inc()
		}
		return NewErrorResponseWithData(req.ID, ErrInternalError, err.Error(), nil)
	}

	// Success
	if s.metrics != nil && s.config.EnableMetrics {
		s.metrics.Counter("orpc_requests_total", "status", "success", "method", req.Method).Inc()
	}

	return NewSuccessResponse(req.ID, result)
}

// HandleBatch handles a batch of JSON-RPC requests
func (s *server) HandleBatch(ctx context.Context, requests []*Request) []*Response {
	if !s.config.EnableBatch {
		return []*Response{NewErrorResponse(nil, ErrServerError, "Batch requests are disabled")}
	}

	if len(requests) > s.config.BatchLimit {
		return []*Response{NewErrorResponse(nil, ErrServerError, fmt.Sprintf("Batch size exceeds limit of %d", s.config.BatchLimit))}
	}

	s.incrementStat("batch_requests")

	responses := make([]*Response, len(requests))
	for i, req := range requests {
		responses[i] = s.HandleRequest(ctx, req)
	}

	return responses
}

// executeWithInterceptors executes a method with all registered interceptors
func (s *server) executeWithInterceptors(ctx context.Context, req *Request, method *Method, params interface{}) (interface{}, error) {
	// Build interceptor chain
	handler := method.Handler

	// Wrap with interceptors in reverse order
	for i := len(s.interceptors) - 1; i >= 0; i-- {
		interceptor := s.interceptors[i]
		currentHandler := handler
		handler = func(ctx interface{}, params interface{}) (interface{}, error) {
			return interceptor(ctx.(context.Context), req, currentHandler)
		}
	}

	// Execute
	return handler(ctx, params)
}

// Use adds an interceptor to the chain
func (s *server) Use(interceptor Interceptor) {
	s.interceptors = append(s.interceptors, interceptor)
}

// OpenRPCDocument generates the OpenRPC schema document
func (s *server) OpenRPCDocument() *OpenRPCDocument {
	s.methodsLock.RLock()
	defer s.methodsLock.RUnlock()

	methods := make([]*OpenRPCMethod, 0, len(s.methods))
	for _, method := range s.methods {
		openrpcMethod := &OpenRPCMethod{
			Name:        method.Name,
			Summary:     method.Description,
			Description: method.Description,
			Deprecated:  method.Deprecated,
		}

		// Add tags
		if len(method.Tags) > 0 {
			openrpcMethod.Tags = make([]*OpenRPCTag, len(method.Tags))
			for i, tag := range method.Tags {
				openrpcMethod.Tags[i] = &OpenRPCTag{Name: tag}
			}
		}

		// Add params
		if method.Params != nil {
			openrpcMethod.Params = []*OpenRPCParam{
				{
					Name:        "params",
					Description: method.Params.Description,
					Required:    len(method.Params.Required) > 0,
					Schema:      s.schemaToMap(method.Params),
				},
			}
		}

		// Add result
		if method.Result != nil {
			openrpcMethod.Result = &OpenRPCResult{
				Name:        "result",
				Description: method.Result.Description,
				Schema:      s.schemaToMap(method.Result),
			}
		}

		methods = append(methods, openrpcMethod)
	}

	return &OpenRPCDocument{
		OpenRPC: "1.3.2",
		Info: &OpenRPCInfo{
			Title:       s.config.ServerName,
			Version:     s.config.ServerVersion,
			Description: "JSON-RPC 2.0 API",
		},
		Methods: methods,
		Servers: []*OpenRPCServer{
			{
				URL: s.config.Endpoint,
			},
		},
	}
}

// schemaToMap converts a schema to a map for JSON serialization
func (s *server) schemaToMap(schema interface{}) map[string]interface{} {
	data, _ := json.Marshal(schema)
	var result map[string]interface{}
	json.Unmarshal(data, &result)
	return result
}

// GetStats returns server statistics
func (s *server) GetStats() ServerStats {
	s.statsLock.RLock()
	defer s.statsLock.RUnlock()
	return s.stats
}

// incrementStat increments a stat counter
func (s *server) incrementStat(name string) {
	s.statsLock.Lock()
	defer s.statsLock.Unlock()

	switch name {
	case "requests":
		s.stats.TotalRequests++
	case "errors":
		s.stats.TotalErrors++
	case "batch_requests":
		s.stats.TotalBatchReqs++
	}
}

// updateLatency updates the average latency
func (s *server) updateLatency(duration time.Duration) {
	s.statsLock.Lock()
	defer s.statsLock.Unlock()

	latency := duration.Seconds()
	if s.stats.TotalRequests == 1 {
		s.stats.AverageLatency = latency
	} else {
		// Running average
		s.stats.AverageLatency = (s.stats.AverageLatency*float64(s.stats.TotalRequests-1) + latency) / float64(s.stats.TotalRequests)
	}
}
