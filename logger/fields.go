package logger

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// ZapField wraps a zap.Field and implements the Field interface
type ZapField struct {
	zapField zap.Field
}

// Key returns the field's key
func (f ZapField) Key() string {
	return f.zapField.Key
}

// Value returns the field's value
func (f ZapField) Value() interface{} {
	return f.zapField.Interface
}

// ZapField returns the underlying zap.Field
func (f ZapField) ZapField() zap.Field {
	return f.zapField
}

// CustomField represents a field with custom key-value pairs
type CustomField struct {
	key   string
	value interface{}
}

// Key returns the field's key
func (f CustomField) Key() string {
	return f.key
}

// Value returns the field's value
func (f CustomField) Value() interface{} {
	return f.value
}

// ZapField converts to zap.Field
func (f CustomField) ZapField() zap.Field {
	return zap.Any(f.key, f.value)
}

// LazyField represents a field that evaluates its value lazily
type LazyField struct {
	key       string
	valueFunc func() interface{}
}

// Key returns the field's key
func (f LazyField) Key() string {
	return f.key
}

// Value returns the field's value (evaluated lazily)
func (f LazyField) Value() interface{} {
	if f.valueFunc != nil {
		return f.valueFunc()
	}
	return nil
}

// ZapField converts to zap.Field
func (f LazyField) ZapField() zap.Field {
	return zap.Any(f.key, f.Value())
}

// Enhanced field constructors that return wrapped fields
var (
	// Basic type constructors
	String = func(key, val string) Field {
		return ZapField{zap.String(key, val)}
	}

	Int = func(key string, val int) Field {
		return ZapField{zap.Int(key, val)}
	}

	Int8 = func(key string, val int8) Field {
		return ZapField{zap.Int8(key, val)}
	}

	Int16 = func(key string, val int16) Field {
		return ZapField{zap.Int16(key, val)}
	}

	Int32 = func(key string, val int32) Field {
		return ZapField{zap.Int32(key, val)}
	}

	Int64 = func(key string, val int64) Field {
		return ZapField{zap.Int64(key, val)}
	}

	Uint = func(key string, val uint) Field {
		return ZapField{zap.Uint(key, val)}
	}

	Uint8 = func(key string, val uint8) Field {
		return ZapField{zap.Uint8(key, val)}
	}

	Uint16 = func(key string, val uint16) Field {
		return ZapField{zap.Uint16(key, val)}
	}

	Uint32 = func(key string, val uint32) Field {
		return ZapField{zap.Uint32(key, val)}
	}

	Uint64 = func(key string, val uint64) Field {
		return ZapField{zap.Uint64(key, val)}
	}

	Float32 = func(key string, val float32) Field {
		return ZapField{zap.Float32(key, val)}
	}

	Float64 = func(key string, val float64) Field {
		return ZapField{zap.Float64(key, val)}
	}

	Bool = func(key string, val bool) Field {
		return ZapField{zap.Bool(key, val)}
	}

	// Time and duration constructors
	Time = func(key string, val time.Time) Field {
		return ZapField{zap.Time(key, val)}
	}

	Duration = func(key string, val time.Duration) Field {
		return ZapField{zap.Duration(key, val)}
	}

	// Error constructor
	Error = func(err error) Field {
		return ZapField{zap.Error(err)}
	}

	// Advanced constructors
	Stringer = func(key string, val fmt.Stringer) Field {
		return ZapField{zap.Stringer(key, val)}
	}

	Any = func(key string, val interface{}) Field {
		return ZapField{zap.Any(key, val)}
	}

	Namespace = func(key string) Field {
		return ZapField{zap.Namespace(key)}
	}

	Binary = func(key string, val []byte) Field {
		return ZapField{zap.Binary(key, val)}
	}

	ByteString = func(key string, val []byte) Field {
		return ZapField{zap.ByteString(key, val)}
	}

	Reflect = func(key string, val interface{}) Field {
		return ZapField{zap.Reflect(key, val)}
	}

	Complex64 = func(key string, val complex64) Field {
		return ZapField{zap.Complex64(key, val)}
	}

	Complex128 = func(key string, val complex128) Field {
		return ZapField{zap.Complex128(key, val)}
	}

	Object = func(key string, val zapcore.ObjectMarshaler) Field {
		return ZapField{zap.Object(key, val)}
	}

	Array = func(key string, val zapcore.ArrayMarshaler) Field {
		return ZapField{zap.Array(key, val)}
	}

	Stack = func(key string) Field {
		return ZapField{zap.Stack(key)}
	}

	Strings = func(key string, val []string) Field {
		return ZapField{zap.Strings(key, val)}
	}
)

// Additional utility field constructors
var (
	// HTTP-related fields
	HTTPMethod = func(method string) Field {
		return String("http.method", method)
	}

	HTTPStatus = func(status int) Field {
		return Int("http.status", status)
	}

	HTTPPath = func(path string) Field {
		return String("http.path", path)
	}

	HTTPURL = func(url *url.URL) Field {
		if url == nil {
			return String("http.url", "")
		}
		return String("http.url", url.String())
	}

	HTTPUserAgent = func(userAgent string) Field {
		return String("http.user_agent", userAgent)
	}

	// Database-related fields
	DatabaseQuery = func(query string) Field {
		return String("db.query", query)
	}

	DatabaseTable = func(table string) Field {
		return String("db.table", table)
	}

	DatabaseRows = func(rows int64) Field {
		return Int64("db.rows", rows)
	}

	// Service-related fields
	ServiceName = func(name string) Field {
		return String("service.name", name)
	}

	ServiceVersion = func(version string) Field {
		return String("service.version", version)
	}

	ServiceEnvironment = func(env string) Field {
		return String("service.environment", env)
	}

	// Performance-related fields
	LatencyMs = func(latency time.Duration) Field {
		return Float64("latency.ms", float64(latency.Nanoseconds())/1e6)
	}

	MemoryUsage = func(bytes int64) Field {
		return Int64("memory.usage", bytes)
	}

	// Custom field constructors
	Custom = func(key string, value interface{}) Field {
		return CustomField{key: key, value: value}
	}

	Lazy = func(key string, valueFunc func() interface{}) Field {
		return LazyField{key: key, valueFunc: valueFunc}
	}

	// Conditional field - only adds field if condition is true
	Conditional = func(condition bool, key string, value interface{}) Field {
		if condition {
			return Custom(key, value)
		}
		return nil
	}

	// Nullable field - only adds field if value is not nil
	Nullable = func(key string, value interface{}) Field {
		if value != nil {
			return Custom(key, value)
		}
		return nil
	}
)

// Context-aware field constructors
var (
	// Request context fields
	RequestID = func(ctx context.Context) Field {
		if id := RequestIDFromContext(ctx); id != "" {
			return String("request_id", id)
		}
		return nil
	}

	TraceID = func(ctx context.Context) Field {
		if id := TraceIDFromContext(ctx); id != "" {
			return String("trace_id", id)
		}
		return nil
	}

	UserID = func(ctx context.Context) Field {
		if id := UserIDFromContext(ctx); id != "" {
			return String("user_id", id)
		}
		return nil
	}

	// Combined context fields
	ContextFields = func(ctx context.Context) []Field {
		var fields []Field
		if id := RequestIDFromContext(ctx); id != "" {
			fields = append(fields, String("request_id", id))
		}
		if id := TraceIDFromContext(ctx); id != "" {
			fields = append(fields, String("trace_id", id))
		}
		if id := UserIDFromContext(ctx); id != "" {
			fields = append(fields, String("user_id", id))
		}
		return fields
	}
)

// Enhanced field conversion functions

// FieldsToZap converts Field interfaces to zap.Field efficiently
func FieldsToZap(fields []Field) []zap.Field {
	zapFields := make([]zap.Field, 0, len(fields))
	for _, field := range fields {
		if field != nil {
			zapFields = append(zapFields, field.ZapField())
		}
	}
	return zapFields
}

// MergeFields merges multiple field slices into one
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

// WrapZapField wraps a zap.Field to implement the Field interface
func WrapZapField(zapField zap.Field) Field {
	return ZapField{zapField}
}

// WrapZapFields wraps multiple zap.Fields
func WrapZapFields(zapFields []zap.Field) []Field {
	fields := make([]Field, len(zapFields))
	for i, zf := range zapFields {
		fields[i] = WrapZapField(zf)
	}
	return fields
}

// FieldGroup represents a group of related fields
type FieldGroup struct {
	fields []Field
}

// NewFieldGroup creates a new field group
func NewFieldGroup(fields ...Field) *FieldGroup {
	return &FieldGroup{fields: fields}
}

// Add adds fields to the group
func (fg *FieldGroup) Add(fields ...Field) *FieldGroup {
	fg.fields = append(fg.fields, fields...)
	return fg
}

// Fields returns all fields in the group
func (fg *FieldGroup) Fields() []Field {
	return fg.fields
}

// ZapFields converts all fields to zap.Fields
func (fg *FieldGroup) ZapFields() []zap.Field {
	return FieldsToZap(fg.fields)
}

// Predefined field groups
var (
	// HTTPRequestGroup creates a group of HTTP request fields
	HTTPRequestGroup = func(method, path, userAgent string, status int) *FieldGroup {
		return NewFieldGroup(
			HTTPMethod(method),
			HTTPPath(path),
			HTTPUserAgent(userAgent),
			HTTPStatus(status),
		)
	}

	// DatabaseQueryGroup creates a group of database query fields
	DatabaseQueryGroup = func(query, table string, rows int64, duration time.Duration) *FieldGroup {
		return NewFieldGroup(
			DatabaseQuery(query),
			DatabaseTable(table),
			DatabaseRows(rows),
			Duration("query_duration", duration),
		)
	}

	// ServiceInfoGroup creates a group of service information fields
	ServiceInfoGroup = func(name, version, environment string) *FieldGroup {
		return NewFieldGroup(
			ServiceName(name),
			ServiceVersion(version),
			ServiceEnvironment(environment),
		)
	}
)

// Field validation and sanitization

// ValidateField validates a field and returns an error if invalid
func ValidateField(field Field) error {
	if field == nil {
		return fmt.Errorf("field cannot be nil")
	}
	if field.Key() == "" {
		return fmt.Errorf("field key cannot be empty")
	}
	return nil
}

// SanitizeFields removes nil and invalid fields
func SanitizeFields(fields []Field) []Field {
	sanitized := make([]Field, 0, len(fields))
	for _, field := range fields {
		if ValidateField(field) == nil {
			sanitized = append(sanitized, field)
		}
	}
	return sanitized
}

// FieldMap creates a map representation of fields for debugging
func FieldMap(fields []Field) map[string]interface{} {
	fieldMap := make(map[string]interface{}, len(fields))
	for _, field := range fields {
		if field != nil {
			fieldMap[field.Key()] = field.Value()
		}
	}
	return fieldMap
}
