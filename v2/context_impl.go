package forge

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
)

// ctx implements Context interface
type ctx struct {
	request       *http.Request
	response      http.ResponseWriter
	params        map[string]string
	values        map[string]any
	scope         Scope
	container     Container
	metrics       Metrics
	healthManager HealthManager
}

// newContext creates a new context
func newContext(w http.ResponseWriter, r *http.Request, container Container) *ctx {
	var scope Scope
	if container != nil {
		scope = container.BeginScope()
	}

	return &ctx{
		request:   r,
		response:  w,
		params:    make(map[string]string),
		values:    make(map[string]any),
		scope:     scope,
		container: container,
	}
}

// Request returns the HTTP request
func (c *ctx) Request() *http.Request {
	return c.request
}

// Response returns the HTTP response writer
func (c *ctx) Response() http.ResponseWriter {
	return c.response
}

// Param returns a path parameter
func (c *ctx) Param(name string) string {
	return c.params[name]
}

// Params returns all path parameters
func (c *ctx) Params() map[string]string {
	return c.params
}

// Query returns a query parameter
func (c *ctx) Query(name string) string {
	return c.request.URL.Query().Get(name)
}

// QueryDefault returns a query parameter with default value
func (c *ctx) QueryDefault(name, defaultValue string) string {
	val := c.request.URL.Query().Get(name)
	if val == "" {
		return defaultValue
	}
	return val
}

// Bind binds request body to a value (auto-detects JSON/XML)
func (c *ctx) Bind(v any) error {
	contentType := c.request.Header.Get("Content-Type")

	switch {
	case contentType == "application/json" || contentType == "":
		return c.BindJSON(v)
	case contentType == "application/xml":
		return c.BindXML(v)
	default:
		return fmt.Errorf("unsupported content type: %s", contentType)
	}
}

// BindJSON binds JSON request body
func (c *ctx) BindJSON(v any) error {
	if c.request.Body == nil {
		return fmt.Errorf("request body is nil")
	}
	defer c.request.Body.Close()

	decoder := json.NewDecoder(c.request.Body)
	if err := decoder.Decode(v); err != nil {
		return fmt.Errorf("failed to decode JSON: %w", err)
	}
	return nil
}

// BindXML binds XML request body
func (c *ctx) BindXML(v any) error {
	if c.request.Body == nil {
		return fmt.Errorf("request body is nil")
	}
	defer c.request.Body.Close()

	decoder := xml.NewDecoder(c.request.Body)
	if err := decoder.Decode(v); err != nil {
		return fmt.Errorf("failed to decode XML: %w", err)
	}
	return nil
}

// JSON sends JSON response
func (c *ctx) JSON(code int, v any) error {
	c.response.Header().Set("Content-Type", "application/json")
	c.response.WriteHeader(code)

	encoder := json.NewEncoder(c.response)
	if err := encoder.Encode(v); err != nil {
		return fmt.Errorf("failed to encode JSON: %w", err)
	}
	return nil
}

// XML sends XML response
func (c *ctx) XML(code int, v any) error {
	c.response.Header().Set("Content-Type", "application/xml")
	c.response.WriteHeader(code)

	encoder := xml.NewEncoder(c.response)
	if err := encoder.Encode(v); err != nil {
		return fmt.Errorf("failed to encode XML: %w", err)
	}
	return nil
}

// String sends string response
func (c *ctx) String(code int, s string) error {
	c.response.Header().Set("Content-Type", "text/plain")
	c.response.WriteHeader(code)

	_, err := c.response.Write([]byte(s))
	if err != nil {
		return fmt.Errorf("failed to write string: %w", err)
	}
	return nil
}

// Bytes sends byte response
func (c *ctx) Bytes(code int, data []byte) error {
	c.response.WriteHeader(code)

	_, err := c.response.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write bytes: %w", err)
	}
	return nil
}

// NoContent sends no content response
func (c *ctx) NoContent(code int) error {
	c.response.WriteHeader(code)
	return nil
}

// Redirect sends redirect response
func (c *ctx) Redirect(code int, url string) error {
	if code < 300 || code >= 400 {
		return fmt.Errorf("invalid redirect status code: %d", code)
	}
	http.Redirect(c.response, c.request, url, code)
	return nil
}

// Header returns a request header
func (c *ctx) Header(key string) string {
	return c.request.Header.Get(key)
}

// SetHeader sets a response header
func (c *ctx) SetHeader(key, value string) {
	c.response.Header().Set(key, value)
}

// Set stores a value in the context
func (c *ctx) Set(key string, value any) {
	c.values[key] = value
}

// Get retrieves a value from the context
func (c *ctx) Get(key string) any {
	return c.values[key]
}

// MustGet retrieves a value or panics if not found
func (c *ctx) MustGet(key string) any {
	val, ok := c.values[key]
	if !ok {
		panic(fmt.Sprintf("key %s does not exist", key))
	}
	return val
}

// Context returns the request context
func (c *ctx) Context() context.Context {
	return c.request.Context()
}

// WithContext replaces the request context
func (c *ctx) WithContext(ctx context.Context) {
	c.request = c.request.WithContext(ctx)
}

// Container returns the DI container
func (c *ctx) Container() Container {
	return c.container
}

// Metrics returns the metrics collector
func (c *ctx) Metrics() Metrics {
	return c.metrics
}

// HealthManager returns the health manager
func (c *ctx) HealthManager() HealthManager {
	return c.healthManager
}

// Scope returns the request scope
func (c *ctx) Scope() Scope {
	return c.scope
}

// Resolve resolves a service from the scope
func (c *ctx) Resolve(name string) (any, error) {
	if c.scope != nil {
		return c.scope.Resolve(name)
	}
	if c.container != nil {
		return c.container.Resolve(name)
	}
	return nil, fmt.Errorf("no container or scope available")
}

// Must resolves a service or panics
func (c *ctx) Must(name string) any {
	val, err := c.Resolve(name)
	if err != nil {
		panic(fmt.Sprintf("failed to resolve %s: %v", name, err))
	}
	return val
}

// setParam sets a path parameter (internal)
func (c *ctx) setParam(key, value string) {
	c.params[key] = value
}

// cleanup ends the scope (should be called after request)
func (c *ctx) cleanup() {
	if c.scope != nil {
		_ = c.scope.End()
	}
}
