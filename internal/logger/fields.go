package logger

// Integer conversions are used for type casting in structured logging.

import (
	"time"

	"github.com/xraph/go-utils/log"
	"go.uber.org/zap"
)

// ZapField wraps a zap.Field and implements the Field interface.
type ZapField = log.ZapField

// CustomField represents a field with custom key-value pairs.
type CustomField = log.CustomField

// LazyField represents a field that evaluates its value lazily.
type LazyField = log.LazyField

// Enhanced field constructors that return wrapped fields.
var (
	// Basic type constructors.
	String  = log.String
	Int     = log.Int
	Int8    = log.Int8
	Int16   = log.Int16
	Int32   = log.Int32
	Int64   = log.Int64
	Uint    = log.Uint
	Uint8   = log.Uint8
	Uint16  = log.Uint16
	Uint32  = log.Uint32
	Uint64  = log.Uint64
	Float32 = log.Float32
	Float64 = log.Float64
	Bool    = log.Bool

	// Time and duration constructors.
	Time     = log.Time
	Duration = log.Duration

	// Error constructor.
	Error = log.Error

	// Advanced constructors.
	Stringer = log.Stringer

	Any       = log.Any
	Namespace = log.Namespace

	Binary = log.Binary

	ByteString = log.ByteString

	Reflect = log.Reflect

	Complex64 = log.Complex64

	Complex128 = log.Complex128

	Object = log.Object

	Array = log.Array

	Stack = log.Stack

	Strings = log.Strings
)

// Additional utility field constructors.
var (
	// HTTP-related fields.
	HTTPMethod = log.HTTPMethod

	HTTPStatus = log.HTTPStatus

	HTTPPath = log.HTTPPath

	HTTPURL = log.HTTPURL

	HTTPUserAgent = log.HTTPUserAgent

	// Database-related fields.
	DatabaseQuery = log.DatabaseQuery

	DatabaseTable = log.DatabaseTable

	DatabaseRows = log.DatabaseRows

	// Service-related fields.
	ServiceName = log.ServiceName

	ServiceVersion = log.ServiceVersion

	ServiceEnvironment = log.ServiceEnvironment

	// Performance-related fields.
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
	// Request context fields.
	RequestID = log.RequestID

	TraceID = log.TraceID

	UserID = log.UserID

	// Combined context fields.
	ContextFields = log.ContextFields
)

// Enhanced field conversion functions

// FieldsToZap converts Field interfaces to zap.Field efficiently.
func FieldsToZap(fields []Field) []zap.Field {
	return log.FieldsToZap(fields)
}

// MergeFields merges multiple field slices into one.
func MergeFields(fieldSlices ...[]Field) []Field {
	totalLen := 0
	for _, slice := range fieldSlices {
		totalLen += len(slice)
	}

	result := make([]Field, 0, totalLen)

	for _, slice := range fieldSlices {
		for _, field := range slice {
			if field != nil {
				result = append(result, field)
			}
		}
	}

	return result
}

// WrapZapField wraps a zap.Field to implement the Field interface.
func WrapZapField(zapField zap.Field) Field {
	return log.WrapZapField(zapField)
}

// WrapZapFields wraps multiple zap.Fields.
func WrapZapFields(zapFields []zap.Field) []Field {
	return log.WrapZapFields(zapFields)
}

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

// Field validation and sanitization

// ValidateField validates a field and returns an error if invalid.
func ValidateField(field Field) error {
	return log.ValidateField(field)
}

// SanitizeFields removes nil and invalid fields.
func SanitizeFields(fields []Field) []Field {
	return log.SanitizeFields(fields)
}

// FieldMap creates a map representation of fields for debugging.
func FieldMap(fields []Field) map[string]any {
	return log.FieldMap(fields)
}
