package shared

import (
	"context"
	"net/http"
)

// Context wraps http.Request with convenience methods
type Context interface {

	// Request access
	Request() *http.Request
	Response() http.ResponseWriter

	// Path parameters
	Param(name string) string
	Params() map[string]string

	// Query parameters
	Query(name string) string
	QueryDefault(name, defaultValue string) string

	// Request body
	Bind(v any) error
	BindJSON(v any) error
	BindXML(v any) error

	// Response helpers
	JSON(code int, v any) error
	XML(code int, v any) error
	String(code int, s string) error
	Bytes(code int, data []byte) error
	NoContent(code int) error
	Redirect(code int, url string) error

	// Headers
	Header(key string) string
	SetHeader(key, value string)

	// Context values
	Set(key string, value any)
	Get(key string) any
	MustGet(key string) any

	// Request context
	Context() context.Context
	WithContext(ctx context.Context)

	// DI integration
	Container() Container
	Scope() Scope
	Resolve(name string) (any, error)
	Must(name string) any
}
