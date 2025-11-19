package shared

import (
	"context"
	"mime/multipart"
	"net/http"
	"time"
)

// Session represents a user session (mirrors security.Session).
type Session interface {
	GetID() string
	GetUserID() string
	GetData(key string) (any, bool)
	SetData(key string, value any)
	DeleteData(key string)
	IsExpired() bool
	IsValid() bool
	Touch()
	GetCreatedAt() time.Time
	GetExpiresAt() time.Time
	GetLastAccessedAt() time.Time
}

// ResponseBuilder provides fluent response building.
type ResponseBuilder interface {
	JSON(v any) error
	XML(v any) error
	String(s string) error
	Bytes(data []byte) error
	NoContent() error
	Header(key, value string) ResponseBuilder
}

// Context wraps http.Request with convenience methods.
type Context interface {

	// Request access
	Request() *http.Request
	Response() http.ResponseWriter

	// Path parameters
	Param(name string) string
	Params() map[string]string

	// Path parameters with type conversion
	ParamInt(name string) (int, error)
	ParamInt64(name string) (int64, error)
	ParamFloat64(name string) (float64, error)
	ParamBool(name string) (bool, error)

	// Path parameters with defaults
	ParamIntDefault(name string, defaultValue int) int
	ParamInt64Default(name string, defaultValue int64) int64
	ParamFloat64Default(name string, defaultValue float64) float64
	ParamBoolDefault(name string, defaultValue bool) bool

	// Query parameters
	Query(name string) string
	QueryDefault(name, defaultValue string) string

	// Request body
	Bind(v any) error
	BindJSON(v any) error
	BindXML(v any) error

	// BindRequest binds and validates request data from all sources (path, query, header, body)
	// using struct tags. Automatically validates based on validation tags.
	BindRequest(v any) error

	// Multipart form data
	FormFile(name string) (multipart.File, *multipart.FileHeader, error)
	FormFiles(name string) ([]*multipart.FileHeader, error)
	FormValue(name string) string
	FormValues(name string) []string
	ParseMultipartForm(maxMemory int64) error

	// Response helpers
	JSON(code int, v any) error
	XML(code int, v any) error
	String(code int, s string) error
	Bytes(code int, data []byte) error
	NoContent(code int) error
	Redirect(code int, url string) error

	// Fluent response builder
	Status(code int) ResponseBuilder

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

	// Cookie management
	Cookie(name string) (string, error)
	SetCookie(name, value string, maxAge int)
	SetCookieWithOptions(name, value string, path, domain string, maxAge int, secure, httpOnly bool)
	DeleteCookie(name string)
	HasCookie(name string) bool
	GetAllCookies() map[string]string

	// Session management
	Session() (Session, error)
	SetSession(session Session)
	SaveSession() error
	DestroySession() error
	GetSessionValue(key string) (any, bool)
	SetSessionValue(key string, value any)
	DeleteSessionValue(key string)
	SessionID() string
}
