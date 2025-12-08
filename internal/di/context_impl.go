package di

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/xraph/forge/internal/shared"
)

type Metrics = shared.Metrics
type HealthManager = shared.HealthManager

type ContextWithClean interface {
	Cleanup()
}

// Ctx implements Context interface.
type Ctx struct {
	request       *http.Request
	response      http.ResponseWriter
	params        map[string]string
	values        map[string]any
	scope         Scope
	container     Container
	metrics       Metrics
	healthManager HealthManager
	session       shared.Session
	sessionStore  any // Will be SessionStore interface from security extension
}

// ResponseBuilder provides fluent response building.
type ResponseBuilder struct {
	ctx    *Ctx
	status int
}

// NewContext creates a new context.
func NewContext(w http.ResponseWriter, r *http.Request, container Container) Context {
	var scope Scope
	if container != nil {
		scope = container.BeginScope()
	}

	// Extract params from request context (set by router adapter)
	params := make(map[string]string)
	// Use the same key type as the router adapter
	if p := r.Context().Value("forge:params"); p != nil {
		if paramMap, ok := p.(map[string]string); ok {
			params = paramMap
		}
	}

	return &Ctx{
		request:   r,
		response:  w,
		params:    params,
		values:    make(map[string]any),
		scope:     scope,
		container: container,
	}
}

// Request returns the HTTP request.
func (c *Ctx) Request() *http.Request {
	return c.request
}

// Response returns the HTTP response writer.
func (c *Ctx) Response() http.ResponseWriter {
	return c.response
}

// Param returns a path parameter.
func (c *Ctx) Param(name string) string {
	return c.params[name]
}

// Params returns all path parameters.
func (c *Ctx) Params() map[string]string {
	return c.params
}

// ParamInt returns a path parameter as int.
func (c *Ctx) ParamInt(name string) (int, error) {
	val := c.params[name]
	if val == "" {
		return 0, fmt.Errorf("param %s not found", name)
	}

	return strconv.Atoi(val)
}

// ParamInt64 returns a path parameter as int64.
func (c *Ctx) ParamInt64(name string) (int64, error) {
	val := c.params[name]
	if val == "" {
		return 0, fmt.Errorf("param %s not found", name)
	}

	return strconv.ParseInt(val, 10, 64)
}

// ParamFloat64 returns a path parameter as float64.
func (c *Ctx) ParamFloat64(name string) (float64, error) {
	val := c.params[name]
	if val == "" {
		return 0, fmt.Errorf("param %s not found", name)
	}

	return strconv.ParseFloat(val, 64)
}

// ParamBool returns a path parameter as bool.
func (c *Ctx) ParamBool(name string) (bool, error) {
	val := c.params[name]
	if val == "" {
		return false, fmt.Errorf("param %s not found", name)
	}

	return strconv.ParseBool(val)
}

// ParamIntDefault returns a path parameter as int with default value.
func (c *Ctx) ParamIntDefault(name string, defaultValue int) int {
	val, err := c.ParamInt(name)
	if err != nil {
		return defaultValue
	}

	return val
}

// ParamInt64Default returns a path parameter as int64 with default value.
func (c *Ctx) ParamInt64Default(name string, defaultValue int64) int64 {
	val, err := c.ParamInt64(name)
	if err != nil {
		return defaultValue
	}

	return val
}

// ParamFloat64Default returns a path parameter as float64 with default value.
func (c *Ctx) ParamFloat64Default(name string, defaultValue float64) float64 {
	val, err := c.ParamFloat64(name)
	if err != nil {
		return defaultValue
	}

	return val
}

// ParamBoolDefault returns a path parameter as bool with default value.
func (c *Ctx) ParamBoolDefault(name string, defaultValue bool) bool {
	val, err := c.ParamBool(name)
	if err != nil {
		return defaultValue
	}

	return val
}

// Query returns a query parameter.
func (c *Ctx) Query(name string) string {
	return c.request.URL.Query().Get(name)
}

// QueryDefault returns a query parameter with default value.
func (c *Ctx) QueryDefault(name, defaultValue string) string {
	val := c.request.URL.Query().Get(name)
	if val == "" {
		return defaultValue
	}

	return val
}

// Bind binds request body to a value (auto-detects JSON/XML/multipart).
func (c *Ctx) Bind(v any) error {
	contentType := c.request.Header.Get("Content-Type")

	switch {
	case contentType == "application/json" || contentType == "":
		return c.BindJSON(v)
	case contentType == "application/xml" || contentType == "text/xml":
		return c.BindXML(v)
	case strings.HasPrefix(contentType, "multipart/form-data"):
		// For multipart forms, we don't auto-bind to structs
		// Users should use FormFile() and FormValue() methods directly
		return errors.New("multipart/form-data should be handled using FormFile() and FormValue() methods")
	case strings.HasPrefix(contentType, "application/x-www-form-urlencoded"):
		// Parse form values
		if err := c.request.ParseForm(); err != nil {
			return fmt.Errorf("failed to parse form: %w", err)
		}

		return nil
	default:
		return fmt.Errorf("unsupported content type: %s", contentType)
	}
}

// BindJSON binds JSON request body.
func (c *Ctx) BindJSON(v any) error {
	if c.request.Body == nil {
		return errors.New("request body is nil")
	}
	defer c.request.Body.Close()

	decoder := json.NewDecoder(c.request.Body)
	if err := decoder.Decode(v); err != nil {
		return fmt.Errorf("failed to decode JSON: %w", err)
	}

	return nil
}

// BindXML binds XML request body.
func (c *Ctx) BindXML(v any) error {
	if c.request.Body == nil {
		return errors.New("request body is nil")
	}
	defer c.request.Body.Close()

	decoder := xml.NewDecoder(c.request.Body)
	if err := decoder.Decode(v); err != nil {
		return fmt.Errorf("failed to decode XML: %w", err)
	}

	return nil
}

// FormFile retrieves a file from a multipart form.
func (c *Ctx) FormFile(name string) (multipart.File, *multipart.FileHeader, error) {
	return c.request.FormFile(name)
}

// FormFiles retrieves multiple files with the same field name from a multipart form.
func (c *Ctx) FormFiles(name string) ([]*multipart.FileHeader, error) {
	if c.request.MultipartForm == nil {
		// Try to parse the form with default max memory (32MB)
		if err := c.request.ParseMultipartForm(32 << 20); err != nil {
			return nil, fmt.Errorf("failed to parse multipart form: %w", err)
		}
	}

	if c.request.MultipartForm == nil || c.request.MultipartForm.File == nil {
		return nil, errors.New("no multipart form found")
	}

	files, ok := c.request.MultipartForm.File[name]
	if !ok || len(files) == 0 {
		return nil, fmt.Errorf("no files found for field: %s", name)
	}

	return files, nil
}

// FormValue retrieves a form value from multipart or url-encoded form.
func (c *Ctx) FormValue(name string) string {
	return c.request.FormValue(name)
}

// FormValues retrieves all values for a form field.
func (c *Ctx) FormValues(name string) []string {
	if c.request.Form == nil {
		// Try to parse form
		_ = c.request.ParseForm()
	}

	if c.request.Form == nil {
		return nil
	}

	return c.request.Form[name]
}

// ParseMultipartForm parses a multipart form with the specified max memory
// maxMemory: maximum memory in bytes to use for storing files in memory (rest goes to disk)
// Recommended values: 10MB (10<<20), 32MB (32<<20), 64MB (64<<20).
func (c *Ctx) ParseMultipartForm(maxMemory int64) error {
	if err := c.request.ParseMultipartForm(maxMemory); err != nil {
		return fmt.Errorf("failed to parse multipart form: %w", err)
	}

	return nil
}

// JSON sends JSON response.
// If v is a struct with header:"..." tags, those headers are set automatically.
// If v has a field with body:"" tag, that field's value is serialized instead of the whole struct.
// If the route has sensitive field cleaning enabled, fields with sensitive:"..." tags are processed.
func (c *Ctx) JSON(code int, v any) error {
	// Check if sensitive field cleaning is enabled for this route
	cleanSensitive := c.shouldCleanSensitiveFields()

	// Process response to handle header, body, and sensitive tags
	body := shared.ProcessResponseValueWithSensitive(v, c.SetHeader, cleanSensitive)

	c.response.Header().Set("Content-Type", "application/json")
	c.response.WriteHeader(code)

	encoder := json.NewEncoder(c.response)
	if err := encoder.Encode(body); err != nil {
		return fmt.Errorf("failed to encode JSON: %w", err)
	}

	return nil
}

// shouldCleanSensitiveFields checks if sensitive field cleaning is enabled for this route.
// It checks both the forge context values and the request context for the flag.
func (c *Ctx) shouldCleanSensitiveFields() bool {
	// Check forge context values first
	if val := c.Get("forge:sensitive_field_cleaning"); val != nil {
		if enabled, ok := val.(bool); ok && enabled {
			return true
		}
	}

	// Check request context (for cases where handler creates a new forge context)
	if val := c.request.Context().Value(shared.ContextKeyForSensitiveCleaning); val != nil {
		if enabled, ok := val.(bool); ok && enabled {
			return true
		}
	}

	return false
}

// XML sends XML response.
func (c *Ctx) XML(code int, v any) error {
	c.response.Header().Set("Content-Type", "application/xml")
	c.response.WriteHeader(code)

	encoder := xml.NewEncoder(c.response)
	if err := encoder.Encode(v); err != nil {
		return fmt.Errorf("failed to encode XML: %w", err)
	}

	return nil
}

// String sends string response.
func (c *Ctx) String(code int, s string) error {
	c.response.Header().Set("Content-Type", "text/plain")
	c.response.WriteHeader(code)

	_, err := c.response.Write([]byte(s))
	if err != nil {
		return fmt.Errorf("failed to write string: %w", err)
	}

	return nil
}

// Bytes sends byte response.
func (c *Ctx) Bytes(code int, data []byte) error {
	c.response.WriteHeader(code)

	_, err := c.response.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write bytes: %w", err)
	}

	return nil
}

// NoContent sends no content response.
func (c *Ctx) NoContent(code int) error {
	c.response.WriteHeader(code)

	return nil
}

// Redirect sends redirect response.
func (c *Ctx) Redirect(code int, url string) error {
	if code < 300 || code >= 400 {
		return fmt.Errorf("invalid redirect status code: %d", code)
	}

	http.Redirect(c.response, c.request, url, code)

	return nil
}

// WriteSSE writes a Server-Sent Event with automatic content type detection.
// For string data, sends as-is. For other types, marshals to JSON.
// Automatically flushes after writing.
func (c *Ctx) WriteSSE(event string, data any) error {
	var dataStr string

	// Auto-detect data type
	switch v := data.(type) {
	case string:
		dataStr = v
	case []byte:
		dataStr = string(v)
	default:
		// Marshal to JSON for non-string types
		jsonData, err := json.Marshal(data)
		if err != nil {
			return fmt.Errorf("failed to marshal SSE data to JSON: %w", err)
		}
		dataStr = string(jsonData)
	}

	// Format SSE event
	var builder strings.Builder
	if event != "" {
		builder.WriteString("event: ")
		builder.WriteString(event)
		builder.WriteString("\n")
	}
	builder.WriteString("data: ")
	builder.WriteString(dataStr)
	builder.WriteString("\n\n")

	// Write to response
	_, err := c.response.Write([]byte(builder.String()))
	if err != nil {
		return fmt.Errorf("failed to write SSE event: %w", err)
	}

	// Auto-flush
	return c.Flush()
}

// Flush flushes any buffered response data to the client.
// Returns an error if the response writer doesn't support flushing.
func (c *Ctx) Flush() error {
	flusher, ok := c.response.(http.Flusher)
	if !ok {
		return errors.New("response writer does not support flushing")
	}

	flusher.Flush()
	return nil
}

// Header returns a request header.
func (c *Ctx) Header(key string) string {
	return c.request.Header.Get(key)
}

// SetHeader sets a response header.
func (c *Ctx) SetHeader(key, value string) {
	c.response.Header().Set(key, value)
}

// processResponseValue handles response struct tags using shared logic.
// - Sets header:"..." fields as HTTP response headers
// - Unwraps body:"" fields to return just the body content
// - Falls back to original value if no special tags found.
func (c *Ctx) processResponseValue(v any) any {
	return shared.ProcessResponseValue(v, c.SetHeader)
}

// Set stores a value in the context.
func (c *Ctx) Set(key string, value any) {
	c.values[key] = value
}

// Get retrieves a value from the context.
func (c *Ctx) Get(key string) any {
	return c.values[key]
}

// MustGet retrieves a value or panics if not found.
func (c *Ctx) MustGet(key string) any {
	val, ok := c.values[key]
	if !ok {
		panic(fmt.Sprintf("key %s does not exist", key))
	}

	return val
}

// Context returns the request context.
func (c *Ctx) Context() context.Context {
	return c.request.Context()
}

// WithContext replaces the request context.
func (c *Ctx) WithContext(ctx context.Context) {
	c.request = c.request.WithContext(ctx)
}

// Container returns the DI container.
func (c *Ctx) Container() Container {
	return c.container
}

// Metrics returns the metrics collector.
func (c *Ctx) Metrics() Metrics {
	return c.metrics
}

// HealthManager returns the health manager.
func (c *Ctx) HealthManager() HealthManager {
	return c.healthManager
}

// Scope returns the request scope.
func (c *Ctx) Scope() Scope {
	return c.scope
}

// Resolve resolves a service from the scope.
func (c *Ctx) Resolve(name string) (any, error) {
	if c.scope != nil {
		return c.scope.Resolve(name)
	}

	if c.container != nil {
		return c.container.Resolve(name)
	}

	return nil, errors.New("no container or scope available")
}

// Must resolves a service or panics.
func (c *Ctx) Must(name string) any {
	val, err := c.Resolve(name)
	if err != nil {
		panic(fmt.Sprintf("failed to resolve %s: %v", name, err))
	}

	return val
}

// Cookie returns a cookie value.
func (c *Ctx) Cookie(name string) (string, error) {
	cookie, err := c.request.Cookie(name)
	if err != nil {
		if errors.Is(err, http.ErrNoCookie) {
			return "", fmt.Errorf("cookie %s not found", name)
		}

		return "", err
	}

	return cookie.Value, nil
}

// SetCookie sets a cookie with basic options.
func (c *Ctx) SetCookie(name, value string, maxAge int) {
	cookie := &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	}
	http.SetCookie(c.response, cookie)
}

// SetCookieWithOptions sets a cookie with full control over options.
func (c *Ctx) SetCookieWithOptions(name, value string, path, domain string, maxAge int, secure, httpOnly bool) {
	cookie := &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     path,
		Domain:   domain,
		MaxAge:   maxAge,
		HttpOnly: httpOnly,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
	http.SetCookie(c.response, cookie)
}

// DeleteCookie deletes a cookie by setting MaxAge to -1.
func (c *Ctx) DeleteCookie(name string) {
	cookie := &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
	}
	http.SetCookie(c.response, cookie)
}

// HasCookie checks if a cookie exists.
func (c *Ctx) HasCookie(name string) bool {
	_, err := c.request.Cookie(name)

	return err == nil
}

// GetAllCookies returns all cookies as a map.
func (c *Ctx) GetAllCookies() map[string]string {
	cookies := c.request.Cookies()

	result := make(map[string]string, len(cookies))
	for _, cookie := range cookies {
		result[cookie.Name] = cookie.Value
	}

	return result
}

// Session returns the current session.
func (c *Ctx) Session() (shared.Session, error) {
	if c.session != nil {
		return c.session, nil
	}

	return nil, errors.New("no session found in context")
}

// SetSession sets the current session in the context.
func (c *Ctx) SetSession(session shared.Session) {
	c.session = session
}

// SaveSession saves the current session to the session store.
func (c *Ctx) SaveSession() error {
	if c.session == nil {
		return errors.New("no session to save")
	}

	// Try to resolve session store from container
	if c.sessionStore == nil && c.container != nil {
		store, err := c.container.Resolve("security.SessionStore")
		if err != nil {
			return fmt.Errorf("session store not available: %w", err)
		}

		c.sessionStore = store
	}

	if c.sessionStore == nil {
		return errors.New("session store not configured")
	}

	// Use type assertion to call Update method
	// This requires the SessionStore interface from security extension
	type sessionStore interface {
		Update(ctx context.Context, session any, ttl time.Duration) error
	}

	if store, ok := c.sessionStore.(sessionStore); ok {
		// Calculate TTL from expiration
		ttl := time.Until(c.session.GetExpiresAt())
		if ttl < 0 {
			return errors.New("session has expired")
		}

		return store.Update(c.Context(), c.session, ttl)
	}

	return errors.New("invalid session store")
}

// DestroySession removes the current session from the store.
func (c *Ctx) DestroySession() error {
	if c.session == nil {
		return errors.New("no session to destroy")
	}

	// Try to resolve session store from container
	if c.sessionStore == nil && c.container != nil {
		store, err := c.container.Resolve("security.SessionStore")
		if err != nil {
			return fmt.Errorf("session store not available: %w", err)
		}

		c.sessionStore = store
	}

	if c.sessionStore == nil {
		return errors.New("session store not configured")
	}

	// Use type assertion to call Delete method
	type sessionStore interface {
		Delete(ctx context.Context, sessionID string) error
	}

	if store, ok := c.sessionStore.(sessionStore); ok {
		err := store.Delete(c.Context(), c.session.GetID())
		if err != nil {
			return err
		}

		c.session = nil

		return nil
	}

	return errors.New("invalid session store")
}

// GetSessionValue gets a value from the current session.
func (c *Ctx) GetSessionValue(key string) (any, bool) {
	if c.session == nil {
		return nil, false
	}

	return c.session.GetData(key)
}

// SetSessionValue sets a value in the current session.
func (c *Ctx) SetSessionValue(key string, value any) {
	if c.session != nil {
		c.session.SetData(key, value)
	}
}

// DeleteSessionValue deletes a value from the current session.
func (c *Ctx) DeleteSessionValue(key string) {
	if c.session != nil {
		c.session.DeleteData(key)
	}
}

// SessionID returns the current session ID.
func (c *Ctx) SessionID() string {
	if c.session != nil {
		return c.session.GetID()
	}

	return ""
}

// setParam sets a path parameter (internal).
func (c *Ctx) setParam(key, value string) {
	c.params[key] = value
}

// cleanup ends the scope (should be called after request).
func (c *Ctx) cleanup() {
	// Clean up multipart form if it was parsed
	if c.request.MultipartForm != nil {
		_ = c.request.MultipartForm.RemoveAll()
	}

	// End DI scope
	if c.scope != nil {
		_ = c.scope.End()
	}
}

// Cleanup ends the scope (should be called after request).
func (c *Ctx) Cleanup() {
	c.cleanup()
}

// Status sets the HTTP status code and returns a builder for chaining.
func (c *Ctx) Status(code int) shared.ResponseBuilder {
	return &ResponseBuilder{
		ctx:    c,
		status: code,
	}
}

// JSON sends a JSON response with the configured status.
func (rb *ResponseBuilder) JSON(v any) error {
	// Check if sensitive field cleaning is enabled for this route
	cleanSensitive := rb.ctx.shouldCleanSensitiveFields()

	// Process response to handle header, body, and sensitive tags
	body := shared.ProcessResponseValueWithSensitive(v, rb.ctx.SetHeader, cleanSensitive)

	rb.ctx.response.Header().Set("Content-Type", "application/json")
	rb.ctx.response.WriteHeader(rb.status)

	encoder := json.NewEncoder(rb.ctx.response)
	if err := encoder.Encode(body); err != nil {
		return fmt.Errorf("failed to encode JSON: %w", err)
	}

	return nil
}

// XML sends an XML response with the configured status.
func (rb *ResponseBuilder) XML(v any) error {
	rb.ctx.response.Header().Set("Content-Type", "application/xml")
	rb.ctx.response.WriteHeader(rb.status)

	encoder := xml.NewEncoder(rb.ctx.response)
	if err := encoder.Encode(v); err != nil {
		return fmt.Errorf("failed to encode XML: %w", err)
	}

	return nil
}

// String sends a string response with the configured status.
func (rb *ResponseBuilder) String(s string) error {
	rb.ctx.response.Header().Set("Content-Type", "text/plain")
	rb.ctx.response.WriteHeader(rb.status)

	_, err := rb.ctx.response.Write([]byte(s))
	if err != nil {
		return fmt.Errorf("failed to write string: %w", err)
	}

	return nil
}

// Bytes sends a byte response with the configured status.
func (rb *ResponseBuilder) Bytes(data []byte) error {
	rb.ctx.response.WriteHeader(rb.status)

	_, err := rb.ctx.response.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write bytes: %w", err)
	}

	return nil
}

// Header sets a response header and returns the builder for chaining.
func (rb *ResponseBuilder) Header(key, value string) shared.ResponseBuilder {
	rb.ctx.response.Header().Set(key, value)

	return rb
}

// NoContent sends a no-content response with the configured status.
func (rb *ResponseBuilder) NoContent() error {
	rb.ctx.response.WriteHeader(rb.status)

	return nil
}
