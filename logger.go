package forge

import "github.com/xraph/forge/internal/logger"

// Re-export logger interfaces for 100% v1 compatibility.
type (
	Logger        = logger.Logger
	SugarLogger   = logger.SugarLogger
	Field         = logger.Field
	LogLevel      = logger.LogLevel
	LoggingConfig = logger.LoggingConfig
	LoggerConfig  = logger.LoggingConfig
)

// Re-export logger constants.
const (
	LevelInfo  = logger.LevelInfo
	LevelWarn  = logger.LevelWarn
	LevelError = logger.LevelError
	LevelFatal = logger.LevelFatal
	LevelDebug = logger.LevelDebug
)

// Re-export logger constructors.
var (
	NewLogger            = logger.NewLogger
	NewDevelopmentLogger = logger.NewDevelopmentLogger
	NewBeautifulLogger   = logger.NewBeautifulLogger
	NewProductionLogger  = logger.NewProductionLogger
	NewNoopLogger        = logger.NewNoopLogger
	GetGlobalLogger      = logger.GetGlobalLogger
	SetGlobalLogger      = logger.SetGlobalLogger
)

// Field constructors for structured logging.
var (
	// String creates a string field.
	String = logger.String
	// Int creates an int field.
	Int = logger.Int
	// Int8 creates an int8 field.
	Int8 = logger.Int8
	// Int16 creates an int16 field.
	Int16 = logger.Int16
	// Int32 creates an int32 field.
	Int32 = logger.Int32
	// Int64 creates an int64 field.
	Int64 = logger.Int64
	// Uint creates a uint field.
	Uint = logger.Uint
	// Uint8 creates a uint8 field.
	Uint8 = logger.Uint8
	// Uint16 creates a uint16 field.
	Uint16 = logger.Uint16
	// Uint32 creates a uint32 field.
	Uint32 = logger.Uint32
	// Uint64 creates a uint64 field.
	Uint64 = logger.Uint64
	// Float32 creates a float32 field.
	Float32 = logger.Float32
	// Float64 creates a float64 field.
	Float64 = logger.Float64
	// Bool creates a bool field.
	Bool = logger.Bool

	// Time creates a time field.
	Time = logger.Time
	// Duration creates a duration field.
	Duration = logger.Duration

	// Error creates an error field.
	Error = logger.Error

	// Stringer creates a field from a Stringer.
	Stringer = logger.Stringer
	// Any creates a field from any value.
	Any = logger.Any
	// Stack creates a stack trace field.
	Stack = logger.Stack
	// Strings creates a string slice field.
	Strings = logger.Strings

	// HTTPMethod creates an HTTP method field.
	HTTPMethod = logger.HTTPMethod
	// HTTPStatus creates an HTTP status field.
	HTTPStatus = logger.HTTPStatus
	// HTTPPath creates an HTTP path field.
	HTTPPath = logger.HTTPPath
	// HTTPURL creates an HTTP URL field.
	HTTPURL = logger.HTTPURL
	// HTTPUserAgent creates an HTTP user agent field.
	HTTPUserAgent = logger.HTTPUserAgent

	// DatabaseQuery creates a database query field.
	DatabaseQuery = logger.DatabaseQuery
	// DatabaseTable creates a database table field.
	DatabaseTable = logger.DatabaseTable
	// DatabaseRows creates a database rows affected field.
	DatabaseRows = logger.DatabaseRows

	// ServiceName creates a service name field.
	ServiceName = logger.ServiceName
	// ServiceVersion creates a service version field.
	ServiceVersion = logger.ServiceVersion
	// ServiceEnvironment creates a service environment field.
	ServiceEnvironment = logger.ServiceEnvironment

	// RequestID creates a request ID field.
	RequestID = logger.RequestID
	// TraceID creates a trace ID field.
	TraceID = logger.TraceID
	// UserID creates a user ID field.
	UserID = logger.UserID
	// ContextFields creates fields from context.
	ContextFields = logger.ContextFields

	// Custom creates a custom field.
	Custom = logger.Custom
	// Lazy creates a lazily evaluated field.
	Lazy = logger.Lazy
)

// Re-export context helpers (note: WithLogger conflicts with context.go, use logger.WithLogger directly).
var (
	LoggerFromContext    = logger.LoggerFromContext
	WithRequestID        = logger.WithRequestID
	RequestIDFromContext = logger.RequestIDFromContext
	WithTraceID          = logger.WithTraceID
	TraceIDFromContext   = logger.TraceIDFromContext
	WithUserID           = logger.WithUserID
	UserIDFromContext    = logger.UserIDFromContext
)

// Re-export utility functions.
var (
	Track              = logger.Track
	TrackWithLogger    = logger.TrackWithLogger
	TrackWithFields    = logger.TrackWithFields
	LogPanic           = logger.LogPanic
	LogPanicWithFields = logger.LogPanicWithFields
)

// Re-export field groups.
var (
	HTTPRequestGroup   = logger.HTTPRequestGroup
	DatabaseQueryGroup = logger.DatabaseQueryGroup
	ServiceInfoGroup   = logger.ServiceInfoGroup
)

// F creates a new field (alias for Any for backwards compatibility with Phase 7).
func F(key string, value any) Field {
	return logger.Any(key, value)
}
