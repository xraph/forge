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
	"strconv"
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
		return fmt.Errorf("no path parameters available")
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

	for i := 0; i < targetValue.NumField(); i++ {
		field := targetValue.Field(i)
		fieldType := targetType.Field(i)

		if !field.CanSet() {
			continue
		}

		// Get field name from header tag
		fieldName := c.getFieldName(fieldType, "header")
		if fieldName == "" || fieldName == "-" {
			continue
		}

		// Get value from headers (headers are case-insensitive)
		headerValues := headers.Values(fieldName)
		if len(headerValues) == 0 {
			// Check if field is required
			if c.isFieldRequired(fieldType) {
				return fmt.Errorf("required header '%s' is missing", fieldName)
			}
			continue
		}

		// Use the first header value, or join multiple values for slice types
		var value string
		if field.Kind() == reflect.Slice && field.Type().Elem().Kind() == reflect.String {
			// For string slices, use all header values
			slice := reflect.MakeSlice(field.Type(), len(headerValues), len(headerValues))
			for j, headerValue := range headerValues {
				slice.Index(j).SetString(headerValue)
			}
			field.Set(slice)
			continue
		} else {
			// For single values, use the first header value
			value = headerValues[0]
		}

		// Set field value
		if err := c.setFieldValue(field, value, fieldType); err != nil {
			return fmt.Errorf("failed to set header field '%s': %w", fieldName, err)
		}
	}

	return c.validateStruct(target)
}

// bindFormValues binds URL values to a struct
func (c *ForgeContext) bindFormValues(target interface{}, values url.Values) error {
	targetValue := reflect.ValueOf(target)
	if targetValue.Kind() != reflect.Ptr || targetValue.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("target must be a pointer to struct")
	}

	targetValue = targetValue.Elem()
	targetType := targetValue.Type()

	for i := 0; i < targetValue.NumField(); i++ {
		field := targetValue.Field(i)
		fieldType := targetType.Field(i)

		if !field.CanSet() {
			continue
		}

		// Get field name from tags
		fieldName := c.getFieldName(fieldType, "form", "query")
		if fieldName == "" || fieldName == "-" {
			continue
		}

		// Get value from form/query
		formValues, exists := values[fieldName]
		if !exists || len(formValues) == 0 {
			// Check if field is required
			if c.isFieldRequired(fieldType) {
				return fmt.Errorf("required field '%s' is missing", fieldName)
			}
			continue
		}

		// Set field value
		if err := c.setFieldValue(field, formValues[0], fieldType); err != nil {
			return fmt.Errorf("failed to set field '%s': %w", fieldName, err)
		}
	}

	return c.validateStruct(target)
}

// bindPathValues binds path parameters to a struct
func (c *ForgeContext) bindPathValues(target interface{}, params map[string]string) error {
	targetValue := reflect.ValueOf(target)
	if targetValue.Kind() != reflect.Ptr || targetValue.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("target must be a pointer to struct")
	}

	targetValue = targetValue.Elem()
	targetType := targetValue.Type()

	for i := 0; i < targetValue.NumField(); i++ {
		field := targetValue.Field(i)
		fieldType := targetType.Field(i)

		if !field.CanSet() {
			continue
		}

		// Get field name from path tag
		fieldName := c.getFieldName(fieldType, "path")
		if fieldName == "" || fieldName == "-" {
			continue
		}

		// Get value from path params
		paramValue, exists := params[fieldName]
		if !exists {
			// Check if field is required
			if c.isFieldRequired(fieldType) {
				return fmt.Errorf("required path parameter '%s' is missing", fieldName)
			}
			continue
		}

		// Set field value
		if err := c.setFieldValue(field, paramValue, fieldType); err != nil {
			return fmt.Errorf("failed to set path parameter '%s': %w", fieldName, err)
		}
	}

	return c.validateStruct(target)
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
	return ""
}

// isFieldRequired checks if a field is required
func (c *ForgeContext) isFieldRequired(field reflect.StructField) bool {
	validate := field.Tag.Get("validate")
	return strings.Contains(validate, "required")
}

// setFieldValue sets a field value from string
func (c *ForgeContext) setFieldValue(field reflect.Value, value string, fieldType reflect.StructField) error {
	switch field.Kind() {
	case reflect.String:
		field.SetString(value)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		intVal, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid integer value: %s", value)
		}
		field.SetInt(intVal)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		uintVal, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid unsigned integer value: %s", value)
		}
		field.SetUint(uintVal)
	case reflect.Float32, reflect.Float64:
		floatVal, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("invalid float value: %s", value)
		}
		field.SetFloat(floatVal)
	case reflect.Bool:
		boolVal, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid boolean value: %s", value)
		}
		field.SetBool(boolVal)
	case reflect.Slice:
		// Handle comma-separated values
		if field.Type().Elem().Kind() == reflect.String {
			values := strings.Split(value, ",")
			slice := reflect.MakeSlice(field.Type(), len(values), len(values))
			for i, val := range values {
				slice.Index(i).SetString(strings.TrimSpace(val))
			}
			field.Set(slice)
		} else {
			return fmt.Errorf("unsupported slice type")
		}
	default:
		return fmt.Errorf("unsupported field type: %v", field.Kind())
	}

	return nil
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
	commonKeys := []string{"pathParams", "params", "chi-context", "gorilla.mux.Vars", "httprouter.Params"}

	for _, key := range commonKeys {
		if value := ctx.Value(key); value != nil {
			if paramMap, ok := value.(map[string]string); ok {
				return paramMap
			}
		}
	}

	// Try to extract from Steel router specific context
	// Steel router might use a different key or structure
	if steelParams := ctx.Value("steel-params"); steelParams != nil {
		if paramMap, ok := steelParams.(map[string]string); ok {
			return paramMap
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
	newCtx.Set("route_pattern", pattern)
	return newCtx
}

// extractPathParametersFromRoute extracts path parameters by comparing route pattern with request path
func (c *ForgeContext) extractPathParametersFromRoute() map[string]string {
	if c.request == nil {
		return make(map[string]string)
	}

	// Get the route pattern from context (should be set by the router)
	routePattern := c.GetString("route_pattern")
	if routePattern == "" {
		return make(map[string]string)
	}

	return c.extractPathParametersFromURL(routePattern, c.request.URL.Path)
}

// extractPathParametersFromURL manually extracts path parameters from URL and route pattern
func (c *ForgeContext) extractPathParametersFromURL(routePattern, requestPath string) map[string]string {
	routeParts := strings.Split(routePattern, "/")
	pathParts := strings.Split(requestPath, "/")

	params := make(map[string]string)

	if len(routeParts) != len(pathParts) {
		return params // Path doesn't match pattern
	}

	for i, routePart := range routeParts {
		if strings.HasPrefix(routePart, ":") {
			paramName := routePart[1:] // Remove the ':' prefix
			if i < len(pathParts) {
				params[paramName] = pathParts[i]
			}
		}
	}

	return params
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
