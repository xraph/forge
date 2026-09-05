package logger

// Integer conversions are used for type casting in structured logging.

import (
	"time"

	"github.com/xraph/go-utils/log"
)

// Field constructors that return wrapped fields.
var (
	// String creates a string field.
	String = log.String
	// Int creates an int field.
	Int = log.Int
	// Int8 creates an int8 field.
	Int8 = log.Int8
	// Int16 creates an int16 field.
	Int16 = log.Int16
	// Int32 creates an int32 field.
	Int32 = log.Int32
	// Int64 creates an int64 field.
	Int64 = log.Int64
	// Uint creates a uint field.
	Uint = log.Uint
	// Uint8 creates a uint8 field.
	Uint8 = log.Uint8
	// Uint16 creates a uint16 field.
	Uint16 = log.Uint16
	// Uint32 creates a uint32 field.
	Uint32 = log.Uint32
	// Uint64 creates a uint64 field.
	Uint64 = log.Uint64
	// Float32 creates a float32 field.
	Float32 = log.Float32
	// Float64 creates a float64 field.
	Float64 = log.Float64
	// Bool creates a bool field.
	Bool = log.Bool

	// Time creates a time field.
	Time = log.Time
	// Duration creates a duration field.
	Duration = log.Duration

	// Error creates an error field.
	Error = log.Error

	// Stringer creates a field from a Stringer.
	Stringer = log.Stringer

	Any       = log.Any
	Namespace = log.Namespace

	Binary = log.Binary

	ByteString = log.ByteString

	Reflect = log.Reflect

	Complex64 = log.Complex64

	Complex128 = log.Complex128

	Stack = log.Stack

	Strings = log.Strings
)

// Utility field constructors.
var (
	// HTTPMethod creates an HTTP method field.
	HTTPMethod = log.HTTPMethod

	// HTTPStatus creates an HTTP status field.
	HTTPStatus = log.HTTPStatus

	// HTTPPath creates an HTTP path field.
	HTTPPath = log.HTTPPath

	// HTTPURL creates an HTTP URL field.
	HTTPURL = log.HTTPURL

	// HTTPUserAgent creates an HTTP user agent field.
	HTTPUserAgent = log.HTTPUserAgent

	// DatabaseQuery creates a database query field.
	DatabaseQuery = log.DatabaseQuery

	// DatabaseTable creates a database table field.
	DatabaseTable = log.DatabaseTable

	// DatabaseRows creates a database rows affected field.
	DatabaseRows = log.DatabaseRows

	// ServiceName creates a service name field.
	ServiceName = log.ServiceName

	// ServiceVersion creates a service version field.
	ServiceVersion = log.ServiceVersion

	// ServiceEnvironment creates a service environment field.
	ServiceEnvironment = log.ServiceEnvironment

	// LatencyMs creates a latency milliseconds field.
	LatencyMs = log.LatencyMs

	MemoryUsage = log.MemoryUsage

	// Custom field constructors.
	Custom = log.Custom

	Lazy = log.Lazy

	// Conditional field - only adds field if condition is true.
	Conditional = log.Conditional

	// Nullable field - only adds field if value is not nil.
	Nullable = log.Nullable
)

// Context-aware field constructors.
var (
	// RequestID creates a request ID field.
	RequestID = log.RequestID

	// TraceID creates a trace ID field.
	TraceID = log.TraceID

	// UserID creates a user ID field.
	UserID = log.UserID

	// ContextFields creates fields from context.
	ContextFields = log.ContextFields
)

// FieldGroup represents a group of related fields.
type FieldGroup = log.FieldGroup

// Predefined field groups.
var (
	// HTTPRequestGroup creates a group of HTTP request fields.
	HTTPRequestGroup = func(method, path, userAgent string, status int) *FieldGroup {
		return log.HTTPRequestGroup(
			method,
			path,
			userAgent,
			status,
		)
	}

	// DatabaseQueryGroup creates a group of database query fields.
	DatabaseQueryGroup = func(query, table string, rows int64, duration time.Duration) *FieldGroup {
		return log.DatabaseQueryGroup(
			query,
			table,
			rows,
			duration,
		)
	}

	// ServiceInfoGroup creates a group of service information fields.
	ServiceInfoGroup = func(name, version, environment string) *FieldGroup {
		return log.ServiceInfoGroup(
			name,
			version,
			environment,
		)
	}
)
