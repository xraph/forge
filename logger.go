package forge

import "github.com/xraph/forge/internal/logger"

// Re-export logger interfaces for 100% v1 compatibility
type (
	Logger        = logger.Logger
	SugarLogger   = logger.SugarLogger
	Field         = logger.Field
	LogLevel      = logger.LogLevel
	LoggingConfig = logger.LoggingConfig
)

// Re-export logger constants
const (
	LevelInfo  = logger.LevelInfo
	LevelWarn  = logger.LevelWarn
	LevelError = logger.LevelError
	LevelFatal = logger.LevelFatal
	LevelDebug = logger.LevelDebug
)

// Re-export logger constructors
var (
	NewLogger            = logger.NewLogger
	NewDevelopmentLogger = logger.NewDevelopmentLogger
	NewProductionLogger  = logger.NewProductionLogger
	NewNoopLogger        = logger.NewNoopLogger
	GetGlobalLogger      = logger.GetGlobalLogger
	SetGlobalLogger      = logger.SetGlobalLogger
)

// Re-export field constructors
var (
	// Basic types
	String  = logger.String
	Int     = logger.Int
	Int8    = logger.Int8
	Int16   = logger.Int16
	Int32   = logger.Int32
	Int64   = logger.Int64
	Uint    = logger.Uint
	Uint8   = logger.Uint8
	Uint16  = logger.Uint16
	Uint32  = logger.Uint32
	Uint64  = logger.Uint64
	Float32 = logger.Float32
	Float64 = logger.Float64
	Bool    = logger.Bool

	// Time and duration
	Time     = logger.Time
	Duration = logger.Duration

	// Error
	Error = logger.Error

	// Advanced
	Stringer = logger.Stringer
	Any      = logger.Any
	Stack    = logger.Stack
	Strings  = logger.Strings

	// HTTP fields
	HTTPMethod    = logger.HTTPMethod
	HTTPStatus    = logger.HTTPStatus
	HTTPPath      = logger.HTTPPath
	HTTPURL       = logger.HTTPURL
	HTTPUserAgent = logger.HTTPUserAgent

	// Database fields
	DatabaseQuery = logger.DatabaseQuery
	DatabaseTable = logger.DatabaseTable
	DatabaseRows  = logger.DatabaseRows

	// Service fields
	ServiceName        = logger.ServiceName
	ServiceVersion     = logger.ServiceVersion
	ServiceEnvironment = logger.ServiceEnvironment

	// Context fields
	RequestID     = logger.RequestID
	TraceID       = logger.TraceID
	UserID        = logger.UserID
	ContextFields = logger.ContextFields

	// Custom
	Custom = logger.Custom
	Lazy   = logger.Lazy
)

// Re-export context helpers (note: WithLogger conflicts with context.go, use logger.WithLogger directly)
var (
	LoggerFromContext    = logger.LoggerFromContext
	WithRequestID        = logger.WithRequestID
	RequestIDFromContext = logger.RequestIDFromContext
	WithTraceID          = logger.WithTraceID
	TraceIDFromContext   = logger.TraceIDFromContext
	WithUserID           = logger.WithUserID
	UserIDFromContext    = logger.UserIDFromContext
)

// Re-export utility functions
var (
	Track              = logger.Track
	TrackWithLogger    = logger.TrackWithLogger
	TrackWithFields    = logger.TrackWithFields
	LogPanic           = logger.LogPanic
	LogPanicWithFields = logger.LogPanicWithFields
)

// Re-export field groups
var (
	HTTPRequestGroup   = logger.HTTPRequestGroup
	DatabaseQueryGroup = logger.DatabaseQueryGroup
	ServiceInfoGroup   = logger.ServiceInfoGroup
)

// F creates a new field (alias for Any for backwards compatibility with Phase 7)
func F(key string, value interface{}) Field {
	return logger.Any(key, value)
}
