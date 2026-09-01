package router

import (
	"context"
	"crypto/tls"
	"net/http"
	"time"

	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/internal/shared"
	"github.com/xraph/go-utils/di"
)

// HTTPError represents an HTTP error for backward compatibility.
type HTTPError = errors.HTTPError

var (
	NewHTTPError  = errors.NewHTTPError
	BadRequest    = errors.BadRequest
	Unauthorized  = errors.Unauthorized
	Forbidden     = errors.Forbidden
	NotFound      = errors.NotFound
	InternalError = errors.InternalError
)

// Router provides HTTP routing with multiple backend support.
type Router interface {
	// HTTP Methods - register routes
	GET(path string, handler any, opts ...RouteOption) error
	POST(path string, handler any, opts ...RouteOption) error
	PUT(path string, handler any, opts ...RouteOption) error
	DELETE(path string, handler any, opts ...RouteOption) error
	PATCH(path string, handler any, opts ...RouteOption) error
	OPTIONS(path string, handler any, opts ...RouteOption) error
	HEAD(path string, handler any, opts ...RouteOption) error
	Any(path string, handler any, opts ...RouteOption) error

	// Handle mounts an http.Handler at the given path, handling all HTTP methods.
	// This behaves like http.Handle() - the handler is directly mounted and receives
	// all requests to the path regardless of HTTP method.
	// Use this for mounting other routers, file servers, or pre-existing http.Handlers.
	Handle(path string, handler http.Handler) error

	// Grouping - organize routes
	Group(prefix string, opts ...GroupOption) Router

	// Middleware - wrap handlers
	Use(middleware ...Middleware)
	UseGlobal(middleware ...Middleware)

	// Controller registration
	RegisterController(controller Controller) error

	// Lifecycle
	Start(ctx context.Context) error
	Stop(ctx context.Context) error

	// HTTP serving
	ServeHTTP(w http.ResponseWriter, r *http.Request)
	Handler() http.Handler

	// Inspection
	Routes() []RouteInfo
	RouteByName(name string) (RouteInfo, bool)
	RoutesByTag(tag string) []RouteInfo
	RoutesByMetadata(key string, value any) []RouteInfo

	// OpenAPI
	OpenAPISpec() *OpenAPISpec

	// AsyncAPI
	AsyncAPISpec() *AsyncAPISpec

	// Streaming
	WebSocket(path string, handler WebSocketHandler, opts ...RouteOption) error
	EventStream(path string, handler SSEHandler, opts ...RouteOption) error
	SSE(path string, handler Handler, opts ...RouteOption) error

	// WebTransport
	WebTransport(path string, handler WebTransportHandler, opts ...RouteOption) error
	EnableWebTransport(config WebTransportConfig) error
	StartHTTP3(addr string, tlsConfig *tls.Config) error
	StopHTTP3() error
}

// RouteOption configures a route.
type RouteOption interface {
	Apply(config *RouteConfig)
}

// GroupOption configures a route group.
type GroupOption interface {
	Apply(config *GroupConfig)
}

// Handler is a forge handler function that takes a Context and returns an error
// This is the preferred handler pattern for forge applications.
type Handler func(ctx Context) error

// Middleware wraps forge handlers (new pattern)
// This is the preferred middleware pattern for forge applications.
type Middleware func(next Handler) Handler

// PureMiddleware wraps HTTP handlers.
type PureMiddleware func(http.Handler) http.Handler

// RouteConfig holds route configuration.
type RouteConfig struct {
	Name        string
	Method      string // HTTP method override (for SSE, WebSocket, etc.)
	Summary     string
	Description string
	Tags        []string
	Middleware  []Middleware
	Timeout     time.Duration
	Metadata    map[string]any

	// Kind classifies the route by connection lifetime. Set by the streaming
	// constructors, not by application code.
	Kind       RouteKind
	Extensions map[string]Extension

	// Interceptors run before the handler (after middleware)
	// Unlike middleware, interceptors don't wrap the handler chain -
	// they simply allow/block the request or enrich the context.
	Interceptors     []Interceptor
	SkipInterceptors map[string]bool // Names of interceptors to skip

	// OpenAPI metadata
	OperationID string
	Deprecated  bool

	// SensitiveFieldCleaning enables cleaning of sensitive fields in responses.
	SensitiveFieldCleaning bool

	// MaxBodySize caps this route's request body in bytes, overriding the
	// router-wide setting. 0 inherits; negative means unlimited.
	MaxBodySize int64

	// EventLog makes an SSE route resumable. When set, events sent by the
	// handler are recorded and a reconnecting client is replayed the ones it
	// missed. Nil leaves the route behaving exactly as it did before.
	EventLog EventLog

	// EventLogChannel derives the log partition from the request, so one route
	// serving per-tenant or per-resource streams does not replay one client's
	// events to another. Required whenever EventLog is set.
	EventLogChannel func(Context) string

	// EventLogAuthoritative records that the log is fed by the application's own
	// producer rather than by this route's connections.
	//
	// It decides what an empty replay is allowed to mean. A connection-written
	// log records nothing while nobody is connected, so "no events after your
	// position" is indistinguishable from "nothing was recording" — and only a
	// producer-written log can tell the client the first of those two. See
	// WithProducerEventLog.
	EventLogAuthoritative bool
}

// GroupConfig holds route group configuration.
type GroupConfig struct {
	Middleware []Middleware
	Tags       []string
	Metadata   map[string]any

	// Interceptors inherited by all routes in the group
	Interceptors     []Interceptor
	SkipInterceptors map[string]bool // Names of interceptors to skip
}

// RouteInfo provides route information for inspection.
//
// Every RouteInfo is built by newRouteInfo; nothing else should assemble one
// from a route. Adding a field here means adding it there, once.
type RouteInfo struct {
	// Name is the route's name, except that it reports OperationID instead
	// whenever one is set. openapi_generator.go reads Name as the operation
	// id, a conflation that predates the OperationID field below. Read
	// OperationID when you want the operation id and nothing else.
	Name        string
	Method      string
	Path        string
	Pattern     string
	Handler     any
	Middleware  []Middleware
	Tags        []string
	Metadata    map[string]any
	Extensions  map[string]Extension
	Summary     string
	Description string

	// Kind classifies the route by connection lifetime. The zero value,
	// KindHTTP, is an ordinary request/response route.
	Kind RouteKind

	// Interceptors provides access to route interceptors for inspection
	Interceptors     []Interceptor
	SkipInterceptors map[string]bool

	// SensitiveFieldCleaning indicates if sensitive fields should be cleaned in responses.
	SensitiveFieldCleaning bool

	// OperationID is the OpenAPI operation identifier for this route.
	OperationID string

	// Deprecated indicates this route is deprecated.
	Deprecated bool

	// Timeout is the per-route handler timeout. Zero means use the default.
	Timeout time.Duration
}

// RouteExtension represents a route-level extension (e.g., OpenAPI, custom validation)
// Note: This is different from app-level Extension which manages app components.
type RouteExtension interface {
	Name() string
	Validate() error
}

// NewRouter creates a new router with options.
func NewRouter(opts ...RouterOption) Router {
	return newRouter(opts...)
}

// RouterOption configures the router.
type RouterOption interface {
	Apply(config *routerConfig)
}

// routerConfig holds router configuration.
type routerConfig struct {
	adapter        RouterAdapter
	container      di.Container
	logger         Logger
	errorHandler   ErrorHandler
	recovery       bool
	httpAddress    string // HTTP server address for automatic localhost server in OpenAPI
	openAPIConfig  *OpenAPIConfig
	asyncAPIConfig *AsyncAPIConfig
	metricsConfig  *shared.MetricsConfig
	healthConfig   *shared.HealthConfig

	// webSocketOrigins is the allow-list for WebSocket upgrade Origins.
	// Empty means same-origin only; see origin.go.
	webSocketOrigins []string

	// maxBodySize caps request bodies. 0 means DefaultMaxRequestBodySize;
	// negative means unlimited.
	maxBodySize int64
}

// RouterAdapter wraps a routing backend.
type RouterAdapter = shared.RouterAdapter

// ExtendedAdapter is the optional wide interface a backend may implement.
type ExtendedAdapter = shared.ExtendedAdapter

// Capabilities reports which wide behaviors a backend honors.
type Capabilities = shared.Capabilities

// MatcherConfig carries router-level matcher settings to a wide backend.
type MatcherConfig = shared.MatcherConfig

// RouteSpec is a route in the form a wide backend receives it.
type RouteSpec = shared.RouteSpec

// RouteKind classifies a route by connection lifetime.
type RouteKind = shared.RouteKind

const (
	KindHTTP         = shared.KindHTTP
	KindSSE          = shared.KindSSE
	KindWebSocket    = shared.KindWebSocket
	KindWebTransport = shared.KindWebTransport
)

// ErrorHandler handles errors from handlers.
type ErrorHandler = shared.ErrorHandler

// NewDefaultErrorHandler creates a default error handler.
func NewDefaultErrorHandler(l Logger) ErrorHandler {
	return shared.NewDefaultErrorHandler(l)
}

// WithName sets the route name.
func WithName(name string) RouteOption {
	return &nameOpt{name}
}

func WithSummary(summary string) RouteOption {
	return &summaryOpt{summary}
}

func WithDescription(desc string) RouteOption {
	return &descriptionOpt{desc}
}

func WithTags(tags ...string) RouteOption {
	return &tagsOpt{tags}
}

func WithMiddleware(mw ...Middleware) RouteOption {
	return &middlewareOpt{mw}
}

func WithTimeout(d time.Duration) RouteOption {
	return &timeoutOpt{d}
}

// WithMaxBodySize caps this route's request body in bytes, overriding the
// router-wide limit. Use it to raise the cap on upload endpoints, or lower it on
// routes that should only ever receive small payloads.
//
// Pass a negative value for no limit on this route.
func WithMaxBodySize(bytes int64) RouteOption {
	return &routeMaxBodySizeOpt{bytes}
}

func WithMetadata(key string, value any) RouteOption {
	return &metadataOpt{key, value}
}

func WithExtension(name string, ext Extension) RouteOption {
	return &extensionOpt{name, ext}
}

func WithOperationID(id string) RouteOption {
	return &operationIDOpt{id}
}

func WithDeprecated() RouteOption {
	return &deprecatedOpt{}
}

// WithSensitiveFieldCleaning enables cleaning of sensitive fields in responses.
// Fields marked with the `sensitive` tag will be processed:
//   - sensitive:"true"     -> set to zero value
//   - sensitive:"redact"   -> set to "[REDACTED]"
//   - sensitive:"mask:***" -> set to custom mask "***"
func WithSensitiveFieldCleaning() RouteOption {
	return &sensitiveCleaningOpt{}
}

// WithMethod overrides the HTTP method for a route.
// Primarily used for SSE/WebSocket endpoints that default to GET.
func WithMethod(method string) RouteOption {
	return &methodOpt{method}
}

// WithGroupMiddleware adds middleware to a route group.
func WithGroupMiddleware(mw ...Middleware) GroupOption {
	return &groupMiddlewareOpt{mw}
}

func WithGroupTags(tags ...string) GroupOption {
	return &groupTagsOpt{tags}
}

func WithGroupMetadata(key string, value any) GroupOption {
	return &groupMetadataOpt{key, value}
}

// WithAdapter sets the router adapter.
func WithAdapter(adapter RouterAdapter) RouterOption {
	return &adapterOpt{adapter}
}

func WithContainer(container di.Container) RouterOption {
	return &containerOpt{container}
}

func WithLogger(logger Logger) RouterOption {
	return &loggerOpt{logger}
}

func WithErrorHandler(handler ErrorHandler) RouterOption {
	return &errorHandlerOpt{handler}
}

func WithRecovery() RouterOption {
	return &recoveryOpt{}
}

// WithHTTPAddress sets the HTTP server address for automatic localhost server generation in OpenAPI.
// The address format can be ":8080" or "localhost:8080" or "0.0.0.0:8080".
func WithHTTPAddress(address string) RouterOption {
	return &httpAddressOpt{address}
}

// WithWebSocketOrigins sets the Origin allow-list for WebSocket upgrades.
//
// By default only same-origin upgrades are accepted, because browsers send
// cookies with WebSocket upgrades but do not apply CORS to them — an
// unrestricted upgrade endpoint lets any site open an authenticated socket as
// the visiting user. Use this to permit specific cross-origin clients.
//
// Accepted entry forms:
//
//	"https://app.example.com" exact scheme://host[:port]
//	"app.example.com"         bare host, any scheme
//	"*.example.com"           any strict subdomain of example.com
//	"*"                       any origin (disables the check — avoid in production)
//
// Requests with no Origin header are always allowed: non-browser clients do not
// send one, and browsers always do.
func WithWebSocketOrigins(origins ...string) RouterOption {
	return &webSocketOriginsOpt{origins}
}

// WithMaxRequestBodySize caps the request body for every route on this router,
// in bytes. Bodies exceeding the cap fail the read with a 413-shaped error
// instead of allocating without bound.
//
// Defaults to DefaultMaxRequestBodySize. Pass a negative value to disable the
// limit entirely (not recommended on internet-facing routes); override per route
// with WithMaxBodySize for upload endpoints.
func WithMaxRequestBodySize(bytes int64) RouterOption {
	return &maxBodySizeOpt{bytes}
}
