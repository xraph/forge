package common

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/logger"
)

// ForgeContext provides enhanced context functionality for the Forge framework
type ForgeContext struct {
	context.Context
	container      Container
	logger         Logger
	metrics        Metrics
	config         ConfigManager
	request        *http.Request
	responseWriter http.ResponseWriter
	values         map[string]interface{}
	mu             sync.RWMutex
}

// NewForgeContext creates a new ForgeContext
func NewForgeContext(ctx context.Context, container Container, logger Logger, metrics Metrics, config ConfigManager) Context {
	return &ForgeContext{
		Context:   ctx,
		container: container,
		logger:    logger,
		metrics:   metrics,
		config:    config,
		values:    make(map[string]interface{}),
	}
}

// =============================================================================
// REQUEST/RESPONSE MANAGEMENT
// =============================================================================

// Request returns the HTTP request
func (c *ForgeContext) Request() *http.Request {
	return c.request
}

// ResponseWriter returns the HTTP response writer
func (c *ForgeContext) ResponseWriter() http.ResponseWriter {
	return c.responseWriter
}

// WithRequest returns a new context with the HTTP request
func (c *ForgeContext) WithRequest(req *http.Request) Context {
	newCtx := c.copy()
	newCtx.request = req
	return newCtx
}

// WithResponseWriter returns a new context with the HTTP response writer
func (c *ForgeContext) WithResponseWriter(w http.ResponseWriter) Context {
	newCtx := c.copy()
	newCtx.responseWriter = w
	return newCtx
}

// =============================================================================
// CONTEXT MANIPULATION
// =============================================================================

// WithValue returns a new context with the key-value pair
func (c *ForgeContext) WithValue(key string, value interface{}) Context {
	newCtx := c.copy()
	newCtx.values[key] = value
	return newCtx
}

// WithTimeout returns a new context with timeout
func (c *ForgeContext) WithTimeout(timeout time.Duration) (Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(c.Context, timeout)
	newCtx := c.copy()
	newCtx.Context = ctx
	return newCtx, cancel
}

// WithCancel returns a new context with cancellation
func (c *ForgeContext) WithCancel() (Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(c.Context)
	newCtx := c.copy()
	newCtx.Context = ctx
	return newCtx, cancel
}

// Copy creates a copy of the context
func (c *ForgeContext) Copy() Context {
	return c.copy()
}

// copy creates a deep copy of the context
func (c *ForgeContext) copy() *ForgeContext {
	c.mu.RLock()
	defer c.mu.RUnlock()

	newValues := make(map[string]interface{}, len(c.values))
	for k, v := range c.values {
		newValues[k] = v
	}

	return &ForgeContext{
		Context:        c.Context,
		container:      c.container,
		logger:         c.logger,
		metrics:        c.metrics,
		config:         c.config,
		request:        c.request,
		responseWriter: c.responseWriter,
		values:         newValues,
	}
}

// =============================================================================
// VALUE ACCESS
// =============================================================================

// Get returns a value from the context
func (c *ForgeContext) Get(key string) interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.values[key]
}

// GetString returns a string value from the context
func (c *ForgeContext) GetString(key string) string {
	if value := c.Get(key); value != nil {
		if str, ok := value.(string); ok {
			return str
		}
	}
	return ""
}

// GetInt returns an integer value from the context
func (c *ForgeContext) GetInt(key string) int {
	if value := c.Get(key); value != nil {
		if i, ok := value.(int); ok {
			return i
		}
	}
	return 0
}

// GetBool returns a boolean value from the context
func (c *ForgeContext) GetBool(key string) bool {
	if value := c.Get(key); value != nil {
		if b, ok := value.(bool); ok {
			return b
		}
	}
	return false
}

// Set sets a value in the context
func (c *ForgeContext) Set(key string, value interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values[key] = value
}

// Has checks if a key exists in the context
func (c *ForgeContext) Has(key string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, exists := c.values[key]
	return exists
}

// Delete removes a key from the context
func (c *ForgeContext) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.values, key)
}

// =============================================================================
// FRAMEWORK ACCESS
// =============================================================================

// Container returns the DI container
func (c *ForgeContext) Container() Container {
	return c.container
}

// Logger returns the logger with context
func (c *ForgeContext) Logger() Logger {
	return c.logger.WithContext(c.Context)
}

// Metrics returns the metrics collector
func (c *ForgeContext) Metrics() Metrics {
	return c.metrics
}

// Config returns the configuration manager
func (c *ForgeContext) Config() ConfigManager {
	return c.config
}

// =============================================================================
// SERVICE RESOLUTION
// =============================================================================

// Resolve resolves a service from the DI container
func (c *ForgeContext) Resolve(serviceType interface{}) (interface{}, error) {
	return c.container.Resolve(serviceType)
}

// ResolveNamed resolves a named service from the DI container
func (c *ForgeContext) ResolveNamed(name string) (interface{}, error) {
	return c.container.ResolveNamed(name)
}

// MustResolve resolves a service from the DI container or panics
func (c *ForgeContext) MustResolve(serviceType interface{}) interface{} {
	service, err := c.container.Resolve(serviceType)
	if err != nil {
		panic(fmt.Sprintf("failed to resolve service: %v", err))
	}
	return service
}

// MustResolveNamed resolves a named service from the DI container or panics
func (c *ForgeContext) MustResolveNamed(name string) interface{} {
	service, err := c.container.ResolveNamed(name)
	if err != nil {
		panic(fmt.Sprintf("failed to resolve named service '%s': %v", name, err))
	}
	return service
}

// =============================================================================
// LOGGING
// =============================================================================

// Debug logs a debug message
func (c *ForgeContext) Debug(msg string, fields ...LogField) {
	c.logger.Debug(msg, c.enrichFields(fields)...)
}

// Info logs an info message
func (c *ForgeContext) Info(msg string, fields ...LogField) {
	c.logger.Info(msg, c.enrichFields(fields)...)
}

// Warn logs a warning message
func (c *ForgeContext) Warn(msg string, fields ...LogField) {
	c.logger.Warn(msg, c.enrichFields(fields)...)
}

// Error logs an error message
func (c *ForgeContext) Error(msg string, fields ...LogField) {
	c.logger.Error(msg, c.enrichFields(fields)...)
}

// enrichFields adds context information to log fields
func (c *ForgeContext) enrichFields(fields []LogField) []LogField {
	enriched := make([]LogField, 0, len(fields)+4)

	// Add context fields
	if requestID := c.RequestID(); requestID != "" {
		enriched = append(enriched, logger.String("request_id", requestID))
	}
	if traceID := c.TraceID(); traceID != "" {
		enriched = append(enriched, logger.String("trace_id", traceID))
	}
	if userID := c.UserID(); userID != "" {
		enriched = append(enriched, logger.String("user_id", userID))
	}
	if c.request != nil {
		enriched = append(enriched, logger.String("method", c.request.Method))
		enriched = append(enriched, logger.String("path", c.request.URL.Path))
	}

	// Add original fields
	enriched = append(enriched, fields...)

	return enriched
}

// =============================================================================
// METRICS
// =============================================================================

// Counter returns a counter metric
func (c *ForgeContext) Counter(name string, tags ...string) Counter {
	return c.metrics.Counter(name, tags...)
}

// Gauge returns a gauge metric
func (c *ForgeContext) Gauge(name string, tags ...string) Gauge {
	return c.metrics.Gauge(name, tags...)
}

// Histogram returns a histogram metric
func (c *ForgeContext) Histogram(name string, tags ...string) Histogram {
	return c.metrics.Histogram(name, tags...)
}

// Timer returns a timer metric
func (c *ForgeContext) Timer(name string, tags ...string) Timer {
	return c.metrics.Timer(name, tags...)
}

// =============================================================================
// CONFIGURATION
// =============================================================================

// GetConfig gets a configuration value
func (c *ForgeContext) GetConfig(key string) interface{} {
	return c.config.Get(key)
}

// GetConfigString gets a string configuration value
func (c *ForgeContext) GetConfigString(key string) string {
	return c.config.GetString(key)
}

// GetConfigInt gets an integer configuration value
func (c *ForgeContext) GetConfigInt(key string) int {
	return c.config.GetInt(key)
}

// GetConfigBool gets a boolean configuration value
func (c *ForgeContext) GetConfigBool(key string) bool {
	return c.config.GetBool(key)
}

// GetConfigDuration gets a duration configuration value
func (c *ForgeContext) GetConfigDuration(key string) time.Duration {
	return c.config.GetDuration(key)
}

// BindConfig binds configuration to a struct
func (c *ForgeContext) BindConfig(key string, target interface{}) error {
	return c.config.Bind(key, target)
}

// =============================================================================
// REQUEST METADATA
// =============================================================================

// UserID returns the user ID from the context
func (c *ForgeContext) UserID() string {
	return c.GetString("user_id")
}

// SetUserID sets the user ID in the context
func (c *ForgeContext) SetUserID(userID string) {
	c.Set("user_id", userID)
}

// RequestID returns the request ID from the context
func (c *ForgeContext) RequestID() string {
	return c.GetString("request_id")
}

// SetRequestID sets the request ID in the context
func (c *ForgeContext) SetRequestID(requestID string) {
	c.Set("request_id", requestID)
}

// TraceID returns the trace ID from the context
func (c *ForgeContext) TraceID() string {
	return c.GetString("trace_id")
}

// SetTraceID sets the trace ID in the context
func (c *ForgeContext) SetTraceID(traceID string) {
	c.Set("trace_id", traceID)
}

// =============================================================================
// AUTHENTICATION
// =============================================================================

// IsAuthenticated checks if the user is authenticated
func (c *ForgeContext) IsAuthenticated() bool {
	return c.UserID() != ""
}

// IsAnonymous checks if the user is anonymous
func (c *ForgeContext) IsAnonymous() bool {
	return c.UserID() == ""
}

// =============================================================================
// REQUEST BINDING
// =============================================================================

// BindJSON binds JSON request body to target
func (c *ForgeContext) BindJSON(target interface{}) error {
	if c.request == nil || c.request.Body == nil {
		return fmt.Errorf("no request body available")
	}

	decoder := json.NewDecoder(c.request.Body)
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("failed to decode JSON: %w", err)
	}

	return c.validateStruct(target)
}

// BindXML binds XML request body to target
func (c *ForgeContext) BindXML(target interface{}) error {
	if c.request == nil || c.request.Body == nil {
		return fmt.Errorf("no request body available")
	}

	decoder := xml.NewDecoder(c.request.Body)
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("failed to decode XML: %w", err)
	}

	return c.validateStruct(target)
}

// BindForm binds form data to target
func (c *ForgeContext) BindForm(target interface{}) error {
	if c.request == nil {
		return fmt.Errorf("no request available")
	}

	if err := c.request.ParseForm(); err != nil {
		return fmt.Errorf("failed to parse form: %w", err)
	}

	return c.bindFormValues(target, c.request.Form)
}

// BindQuery binds query parameters to target
func (c *ForgeContext) BindQuery(target interface{}) error {
	if c.request == nil {
		return fmt.Errorf("no request available")
	}

	return c.bindFormValues(target, c.request.URL.Query())
}

// BindPath binds path parameters to target
func (c *ForgeContext) BindPath(target interface{}) error {
	if c.request == nil {
		return fmt.Errorf("no request available")
	}

	// Try to get path parameters from context first (set by router)
	pathParams := c.Get("path_params")
	var params map[string]string
	var ok bool

	if pathParams != nil {
		params, ok = pathParams.(map[string]string)
		if !ok {
			return fmt.Errorf("invalid path parameters format")
		}
	} else {
		// Fallback: try to extract from request context (Steel router might store them there)
		params = c.extractPathParametersFromRequest()
		if len(params) == 0 {
			// Last resort: try to get from route pattern stored in context
			params = c.extractPathParametersFromRoute()
		}
	}

	if len(params) == 0 {
		// Enhanced debugging info
		routePattern := c.GetString("route_pattern")
		requestPath := c.request.URL.Path

		if c.logger != nil {
			c.logger.Debug("No path parameters found during binding",
				logger.String("route_pattern", routePattern),
				logger.String("request_path", requestPath),
				logger.String("method", c.request.Method),
			)
		}

		return fmt.Errorf("no path parameters available (route: %s, path: %s)", routePattern, requestPath)
	}

	return c.bindPathValues(target, params)
}

// BindHeaders binds HTTP headers to target struct fields
//
// Example usage:
//
//	type RequestHeaders struct {
//	    Authorization string   `header:"Authorization"`
//	    ContentType   string   `header:"Content-Type"`
//	    UserAgent     string   `header:"User-Agent"`
//	    CustomHeaders []string `header:"X-Custom-Header"`
//	}
//
//	var headers RequestHeaders
//	err := ctx.BindHeaders(&headers)
func (c *ForgeContext) BindHeaders(target interface{}) error {
	if c.request == nil {
		return fmt.Errorf("no request available")
	}

	return c.bindHeaderValues(target, c.request.Header)
}

// bindHeaderValues binds HTTP header values to a struct
func (c *ForgeContext) bindHeaderValues(target interface{}, headers http.Header) error {
	targetValue := reflect.ValueOf(target)
	if targetValue.Kind() != reflect.Ptr || targetValue.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("target must be a pointer to struct")
	}

	targetValue = targetValue.Elem()
	targetType := targetValue.Type()

	return c.bindStructFields(targetValue, targetType, func(fieldName string) (string, bool) {
		headerValues := headers.Values(fieldName)
		if len(headerValues) == 0 {
			return "", false
		}
		return headerValues[0], true
	}, "header")
}

// bindStructFields is a generic method to bind struct fields from various sources
func (c *ForgeContext) bindStructFields(
	targetValue reflect.Value,
	targetType reflect.Type,
	valueGetter func(fieldName string) (string, bool),
	tags ...string,
) error {
	for i := 0; i < targetValue.NumField(); i++ {
		field := targetValue.Field(i)
		fieldType := targetType.Field(i)

		if !field.CanSet() {
			continue
		}

		// Handle embedded/anonymous fields
		if fieldType.Anonymous {
			if err := c.bindEmbeddedFields(field, fieldType, valueGetter, tags...); err != nil {
				return fmt.Errorf("failed to bind embedded field '%s': %w", fieldType.Name, err)
			}
			continue
		}

		// Get field name from tags
		fieldName := c.getFieldName(fieldType, tags...)
		if fieldName == "" || fieldName == "-" {
			continue
		}

		// Get value from source
		value, exists := valueGetter(fieldName)
		if !exists {
			// Check if field is required
			if c.isFieldRequired(fieldType) {
				return fmt.Errorf("required field '%s' is missing", fieldName)
			}
			continue
		}

		// Set field value using the type converter system
		if err := c.setFieldValue(field, value, fieldType); err != nil {
			return fmt.Errorf("failed to set field '%s': %w", fieldName, err)
		}
	}

	return nil
}

// bindEmbeddedFields handles embedded/anonymous struct fields
func (c *ForgeContext) bindEmbeddedFields(
	field reflect.Value,
	fieldType reflect.StructField,
	valueGetter func(fieldName string) (string, bool),
	tags ...string,
) error {
	// Handle pointer to embedded struct
	if field.Kind() == reflect.Ptr {
		if field.IsNil() {
			field.Set(reflect.New(field.Type().Elem()))
		}
		field = field.Elem()
	}

	// Only process struct types
	if field.Kind() != reflect.Struct {
		return nil
	}

	embeddedType := field.Type()
	return c.bindStructFields(field, embeddedType, valueGetter, tags...)
}

// bindFormValues binds URL values to a struct
func (c *ForgeContext) bindFormValues(target interface{}, values url.Values) error {
	targetValue := reflect.ValueOf(target)
	if targetValue.Kind() != reflect.Ptr || targetValue.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("target must be a pointer to struct")
	}

	targetValue = targetValue.Elem()
	targetType := targetValue.Type()

	return c.bindStructFields(targetValue, targetType, func(fieldName string) (string, bool) {
		formValues, exists := values[fieldName]
		if !exists || len(formValues) == 0 {
			return "", false
		}
		return formValues[0], true
	}, "form", "query")
}

// bindPathValues binds path parameters to a struct
func (c *ForgeContext) bindPathValues(target interface{}, params map[string]string) error {
	targetValue := reflect.ValueOf(target)
	if targetValue.Kind() != reflect.Ptr || targetValue.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("target must be a pointer to struct")
	}

	targetValue = targetValue.Elem()
	targetType := targetValue.Type()

	return c.bindStructFields(targetValue, targetType, func(fieldName string) (string, bool) {
		paramValue, exists := params[fieldName]
		return paramValue, exists
	}, "path")
}

// getFieldName gets the field name from struct tags
func (c *ForgeContext) getFieldName(field reflect.StructField, tags ...string) string {
	for _, tag := range tags {
		if tagValue := field.Tag.Get(tag); tagValue != "" {
			// Handle tag format like `json:"name,omitempty"`
			parts := strings.Split(tagValue, ",")
			if len(parts) > 0 && parts[0] != "" {
				return parts[0]
			}
		}
	}

	// Fallback: if no explicit tag found, try to infer from JSON tag for query/form binding
	if len(tags) > 0 && (tags[0] == "query" || tags[0] == "form") {
		if jsonTag := field.Tag.Get("json"); jsonTag != "" && jsonTag != "-" {
			parts := strings.Split(jsonTag, ",")
			if len(parts) > 0 && parts[0] != "" {
				return parts[0]
			}
		}
	}

	return ""
}

// isFieldRequired checks if a field is required
func (c *ForgeContext) isFieldRequired(field reflect.StructField) bool {
	validate := field.Tag.Get("validate")
	return strings.Contains(validate, "required")
}

// setFieldValue sets a field value from string
func (c *ForgeContext) setFieldValue(field reflect.Value, value string, fieldType reflect.StructField) error {
	return ConvertFieldValue(field, value, fieldType.Type)
}

// validateStruct validates a struct using validation tags
func (c *ForgeContext) validateStruct(target interface{}) error {
	// This is a simplified validation - in practice, you'd use a proper validation library
	// like go-playground/validator
	return nil
}

// =============================================================================
// RESPONSE HELPERS
// =============================================================================

// JSON sends a JSON response
func (c *ForgeContext) JSON(status int, data interface{}) error {
	if c.responseWriter == nil {
		return fmt.Errorf("no response writer available")
	}

	c.responseWriter.Header().Set("Content-Type", "application/json")
	c.responseWriter.WriteHeader(status)

	encoder := json.NewEncoder(c.responseWriter)
	return encoder.Encode(data)
}

// XML sends an XML response
func (c *ForgeContext) XML(status int, data interface{}) error {
	if c.responseWriter == nil {
		return fmt.Errorf("no response writer available")
	}

	c.responseWriter.Header().Set("Content-Type", "application/xml")
	c.responseWriter.WriteHeader(status)

	encoder := xml.NewEncoder(c.responseWriter)
	return encoder.Encode(data)
}

// String sends a plain text response
func (c *ForgeContext) String(status int, message string) error {
	if c.responseWriter == nil {
		return fmt.Errorf("no response writer available")
	}

	c.responseWriter.Header().Set("Content-Type", "text/plain")
	c.responseWriter.WriteHeader(status)
	_, err := c.responseWriter.Write([]byte(message))
	return err
}

// Status sets the response status and returns the context for chaining
func (c *ForgeContext) Status(status int) Context {
	if c.responseWriter != nil {
		c.responseWriter.WriteHeader(status)
	}
	return c
}

// Header sets a response header and returns the context for chaining
func (c *ForgeContext) Header(key, value string) Context {
	if c.responseWriter != nil {
		c.responseWriter.Header().Set(key, value)
	}
	return c
}

// =============================================================================
// MULTIPART FORM HANDLING
// =============================================================================

// ParseMultipartForm parses multipart form data
func (c *ForgeContext) ParseMultipartForm(maxMemory int64) (*multipart.Form, error) {
	if c.request == nil {
		return nil, fmt.Errorf("no request available")
	}

	if err := c.request.ParseMultipartForm(maxMemory); err != nil {
		return nil, fmt.Errorf("failed to parse multipart form: %w", err)
	}

	return c.request.MultipartForm, nil
}

// FormFile retrieves a file from multipart form
func (c *ForgeContext) FormFile(key string) (multipart.File, *multipart.FileHeader, error) {
	if c.request == nil {
		return nil, nil, fmt.Errorf("no request available")
	}

	return c.request.FormFile(key)
}

// FormValue retrieves a form value
func (c *ForgeContext) FormValue(key string) string {
	if c.request == nil {
		return ""
	}
	return c.request.FormValue(key)
}

// QueryParam retrieves a query parameter
func (c *ForgeContext) QueryParam(key string) string {
	if c.request == nil {
		return ""
	}
	return c.request.URL.Query().Get(key)
}

// PathParam retrieves a path parameter
func (c *ForgeContext) PathParam(key string) string {
	// Try getting from stored path_params first
	pathParams := c.Get("path_params")
	if pathParams != nil {
		if params, ok := pathParams.(map[string]string); ok {
			if value, exists := params[key]; exists {
				return value
			}
		}
	}

	// Fallback: try extracting from request
	params := c.extractPathParametersFromRequest()
	if len(params) == 0 {
		params = c.extractPathParametersFromRoute()
	}

	return params[key]
}

// extractPathParametersFromRequest tries to extract path parameters from the request context
func (c *ForgeContext) extractPathParametersFromRequest() map[string]string {
	if c.request == nil {
		return make(map[string]string)
	}

	ctx := c.request.Context()

	// Try common keys that routers use to store path parameters
	commonKeys := []string{
		"pathParams", "params", "chi-context", "gorilla.mux.Vars",
		"httprouter.Params", "steel-params", "route-params",
		"path-parameters", "url-params", "mux-vars",
	}

	for _, key := range commonKeys {
		if value := ctx.Value(key); value != nil {
			if paramMap, ok := value.(map[string]string); ok {
				if len(paramMap) > 0 {
					if c.logger != nil {
						c.logger.Debug("Found path parameters in request context",
							logger.String("context_key", key),
							logger.Int("param_count", len(paramMap)),
						)
					}
					return paramMap
				}
			}

			// Try to handle other common parameter formats
			if paramSlice, ok := value.([]string); ok && len(paramSlice) > 0 {
				// Convert slice to map (some routers store as alternating key-value pairs)
				paramMap := make(map[string]string)
				for i := 0; i < len(paramSlice)-1; i += 2 {
					paramMap[paramSlice[i]] = paramSlice[i+1]
				}
				if len(paramMap) > 0 {
					return paramMap
				}
			}
		}
	}

	return make(map[string]string)
}

// SetPathParams Add a helper method to set path parameters (for use by the router)
func (c *ForgeContext) SetPathParams(params map[string]string) Context {
	newCtx := c.copy()
	newCtx.Set("path_params", params)
	return newCtx
}

// SetRoutePattern Add a helper method to set route pattern (for use by the router)
func (c *ForgeContext) SetRoutePattern(pattern string) Context {
	newCtx := c.copy()

	// Store both the original pattern and a normalized version
	newCtx.Set("route_pattern", pattern)
	newCtx.Set("original_route_pattern", pattern)

	// Also store a Steel-normalized version for compatibility
	steelPattern := c.convertToSteelFormat(pattern)
	if steelPattern != pattern {
		newCtx.Set("steel_route_pattern", steelPattern)
	}

	return newCtx
}

// convertToSteelFormat converts OpenAPI format to Steel format for compatibility
func (c *ForgeContext) convertToSteelFormat(pattern string) string {
	parts := strings.Split(pattern, "/")

	for i, part := range parts {
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			paramName := part[1 : len(part)-1]
			parts[i] = ":" + paramName
		}
	}

	return strings.Join(parts, "/")
}

// extractPathParametersFromRoute extracts path parameters by comparing route pattern with request path
func (c *ForgeContext) extractPathParametersFromRoute() map[string]string {
	if c.request == nil {
		return make(map[string]string)
	}

	// Get the route pattern from context (should be set by the router)
	routePattern := c.GetString("route_pattern")
	if routePattern == "" {
		// Try alternative context keys that might contain the route pattern
		alternativeKeys := []string{"route_path", "pattern", "route", "handler_pattern"}
		for _, key := range alternativeKeys {
			if pattern := c.GetString(key); pattern != "" {
				routePattern = pattern
				break
			}
		}
	}

	if routePattern == "" {
		if c.logger != nil {
			c.logger.Debug("No route pattern found in context for path parameter extraction")
		}
		return make(map[string]string)
	}

	params := c.extractPathParametersFromURL(routePattern, c.request.URL.Path)

	if c.logger != nil {
		c.logger.Debug("Path parameter extraction result",
			logger.String("route_pattern", routePattern),
			logger.String("request_path", c.request.URL.Path),
			logger.Int("params_found", len(params)),
		)

		for key, value := range params {
			c.logger.Debug("Extracted parameter",
				logger.String("key", key),
				logger.String("value", value),
			)
		}
	}

	return params
}

// extractPathParametersFromURL manually extracts path parameters from URL and route pattern
func (c *ForgeContext) extractPathParametersFromURL(routePattern, requestPath string) map[string]string {
	routeParts := strings.Split(strings.Trim(routePattern, "/"), "/")
	pathParts := strings.Split(strings.Trim(requestPath, "/"), "/")

	params := make(map[string]string)

	// Handle empty paths
	if routePattern == "/" && requestPath == "/" {
		return params
	}

	if len(routeParts) != len(pathParts) {
		if c.logger != nil {
			c.logger.Debug("Path segment count mismatch",
				logger.Int("route_segments", len(routeParts)),
				logger.Int("path_segments", len(pathParts)),
				logger.String("route_pattern", routePattern),
				logger.String("request_path", requestPath),
			)
		}
		return params // Path doesn't match pattern
	}

	for i, routePart := range routeParts {
		var paramName string
		var isParam bool

		// Handle Steel router format: :paramName
		if strings.HasPrefix(routePart, ":") {
			paramName = routePart[1:] // Remove the ':' prefix
			isParam = true
		}
		// Handle OpenAPI format: {paramName}
		if strings.HasPrefix(routePart, "{") && strings.HasSuffix(routePart, "}") {
			paramName = routePart[1 : len(routePart)-1] // Remove the '{' and '}'
			isParam = true
		}

		if isParam && i < len(pathParts) {
			params[paramName] = pathParts[i]
			if c.logger != nil {
				c.logger.Debug("Extracted path parameter",
					logger.String("param_name", paramName),
					logger.String("param_value", pathParts[i]),
					logger.String("format", getParamFormat(routePart)),
				)
			}
		}
	}

	return params
}

// Helper function to identify parameter format for logging
func getParamFormat(routePart string) string {
	if strings.HasPrefix(routePart, ":") {
		return "steel" // :param
	}
	if strings.HasPrefix(routePart, "{") && strings.HasSuffix(routePart, "}") {
		return "openapi" // {param}
	}
	return "literal"
}

// =============================================================================
// UTILITY METHODS
// =============================================================================

// ClientIP returns the client IP address
func (c *ForgeContext) ClientIP() string {
	if c.request == nil {
		return ""
	}

	// Check X-Forwarded-For header
	if xff := c.request.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	// Check X-Real-IP header
	if xri := c.request.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}

	// Fallback to RemoteAddr
	ip := c.request.RemoteAddr
	if colon := strings.LastIndex(ip, ":"); colon != -1 {
		ip = ip[:colon]
	}

	return ip
}

// UserAgent returns the User-Agent header
func (c *ForgeContext) UserAgent() string {
	if c.request == nil {
		return ""
	}
	return c.request.UserAgent()
}

// Referer returns the Referer header
func (c *ForgeContext) Referer() string {
	if c.request == nil {
		return ""
	}
	return c.request.Referer()
}

// ContentType returns the Content-Type header
func (c *ForgeContext) ContentType() string {
	if c.request == nil {
		return ""
	}
	return c.request.Header.Get("Content-Type")
}

// Method returns the HTTP method
func (c *ForgeContext) Method() string {
	if c.request == nil {
		return ""
	}
	return c.request.Method
}

// Path returns the request path
func (c *ForgeContext) Path() string {
	if c.request == nil {
		return ""
	}
	return c.request.URL.Path
}

// Query returns the raw query string
func (c *ForgeContext) Query() string {
	if c.request == nil {
		return ""
	}
	return c.request.URL.RawQuery
}

// ForgeContextKey is the context key for storing ForgeContext
type contextKey string

const ForgeContextKey contextKey = "forge_context"

// GetForgeContext extracts ForgeContext from standard context
func GetForgeContext(ctx context.Context) Context {
	if forgeCtx := ctx.Value(ForgeContextKey); forgeCtx != nil {
		if fc, ok := forgeCtx.(Context); ok {
			return fc
		}
	}
	return nil
}

// MustGetForgeContext extracts ForgeContext or panics
func MustGetForgeContext(ctx context.Context) Context {
	forgeCtx := GetForgeContext(ctx)
	if forgeCtx == nil {
		panic("ForgeContext not found in request context")
	}
	return forgeCtx
}

// WithForgeContext stores ForgeContext in standard context
func WithForgeContext(ctx context.Context, forgeCtx Context) context.Context {
	return context.WithValue(ctx, ForgeContextKey, forgeCtx)
}
