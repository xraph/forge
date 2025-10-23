package serialization

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"time"
)

// JSONSerializer implements JSON serialization
type JSONSerializer struct {
	name                  string
	contentType           string
	prettyPrint           bool
	escapeHTML            bool
	useNumber             bool
	disallowUnknownFields bool
	supportedTypes        map[reflect.Kind]bool
}

// NewJSONSerializer creates a new JSON serializer
func NewJSONSerializer() *JSONSerializer {
	return &JSONSerializer{
		name:                  "json",
		contentType:           "application/json",
		prettyPrint:           false,
		escapeHTML:            true,
		useNumber:             false,
		disallowUnknownFields: false,
		supportedTypes: map[reflect.Kind]bool{
			reflect.Bool:      true,
			reflect.Int:       true,
			reflect.Int8:      true,
			reflect.Int16:     true,
			reflect.Int32:     true,
			reflect.Int64:     true,
			reflect.Uint:      true,
			reflect.Uint8:     true,
			reflect.Uint16:    true,
			reflect.Uint32:    true,
			reflect.Uint64:    true,
			reflect.Float32:   true,
			reflect.Float64:   true,
			reflect.String:    true,
			reflect.Array:     true,
			reflect.Slice:     true,
			reflect.Map:       true,
			reflect.Struct:    true,
			reflect.Ptr:       true,
			reflect.Interface: true,
		},
	}
}

// Name returns the serializer name
func (j *JSONSerializer) Name() string {
	return j.name
}

// ContentType returns the MIME content type
func (j *JSONSerializer) ContentType() string {
	return j.contentType
}

// Configure configures the JSON serializer with options
func (j *JSONSerializer) Configure(options map[string]interface{}) error {
	if prettyPrint, ok := options["pretty_print"].(bool); ok {
		j.prettyPrint = prettyPrint
	}

	if escapeHTML, ok := options["escape_html"].(bool); ok {
		j.escapeHTML = escapeHTML
	}

	if useNumber, ok := options["use_number"].(bool); ok {
		j.useNumber = useNumber
	}

	if disallowUnknown, ok := options["disallow_unknown_fields"].(bool); ok {
		j.disallowUnknownFields = disallowUnknown
	}

	return nil
}

// SupportsType checks if the serializer supports the given type
func (j *JSONSerializer) SupportsType(valueType reflect.Type) bool {
	// Handle pointer types
	for valueType.Kind() == reflect.Ptr {
		valueType = valueType.Elem()
	}

	// Check basic types
	if supported, exists := j.supportedTypes[valueType.Kind()]; exists && supported {
		return true
	}

	// Special cases
	switch valueType {
	case reflect.TypeOf(time.Time{}):
		return true
	case reflect.TypeOf(json.RawMessage{}):
		return true
	}

	// Check for json.Marshaler interface
	marshalerType := reflect.TypeOf((*json.Marshaler)(nil)).Elem()
	if valueType.Implements(marshalerType) {
		return true
	}

	// Check for json.Unmarshaler interface
	unmarshalerType := reflect.TypeOf((*json.Unmarshaler)(nil)).Elem()
	if reflect.PtrTo(valueType).Implements(unmarshalerType) {
		return true
	}

	return false
}

// Serialize converts a value to JSON bytes
func (j *JSONSerializer) Serialize(ctx context.Context, value interface{}) ([]byte, error) {
	if value == nil {
		return []byte("null"), nil
	}

	// Check if the type is supported
	valueType := reflect.TypeOf(value)
	if !j.SupportsType(valueType) {
		return nil, NewSerializationError("serialize", j.name, "unsupported type", nil).WithType(valueType)
	}

	// Create encoder with appropriate settings
	var data []byte
	var err error

	if j.prettyPrint {
		data, err = json.MarshalIndent(value, "", "  ")
	} else {
		data, err = json.Marshal(value)
	}

	if err != nil {
		return nil, NewSerializationError("serialize", j.name, "marshal failed", err).WithType(valueType)
	}

	return data, nil
}

// Deserialize converts JSON bytes to a value
func (j *JSONSerializer) Deserialize(ctx context.Context, data []byte, target interface{}) error {
	if target == nil {
		return NewSerializationError("deserialize", j.name, "target cannot be nil", nil)
	}

	// Check if target is a pointer
	targetValue := reflect.ValueOf(target)
	if targetValue.Kind() != reflect.Ptr {
		return NewSerializationError("deserialize", j.name, "target must be a pointer", nil).WithType(targetValue.Type())
	}

	// Create decoder with appropriate settings
	decoder := json.NewDecoder(strings.NewReader(string(data)))

	if j.useNumber {
		decoder.UseNumber()
	}

	if j.disallowUnknownFields {
		decoder.DisallowUnknownFields()
	}

	err := decoder.Decode(target)
	if err != nil {
		return NewSerializationError("deserialize", j.name, "unmarshal failed", err).WithType(targetValue.Type())
	}

	return nil
}

// JSONArraySerializer handles JSON arrays specifically
type JSONArraySerializer struct {
	*JSONSerializer
}

// NewJSONArraySerializer creates a serializer optimized for JSON arrays
func NewJSONArraySerializer() *JSONArraySerializer {
	return &JSONArraySerializer{
		JSONSerializer: NewJSONSerializer(),
	}
}

// Configure extends the base JSON serializer configuration
func (j *JSONArraySerializer) Configure(options map[string]interface{}) error {
	// Call parent configure first
	if err := j.JSONSerializer.Configure(options); err != nil {
		return err
	}

	// Array-specific configuration can be added here
	return nil
}

// JSONStreamSerializer handles streaming JSON serialization
type JSONStreamSerializer struct {
	*JSONSerializer
	bufferSize int
}

// NewJSONStreamSerializer creates a streaming JSON serializer
func NewJSONStreamSerializer() *JSONStreamSerializer {
	return &JSONStreamSerializer{
		JSONSerializer: NewJSONSerializer(),
		bufferSize:     4096,
	}
}

// Configure extends the base JSON serializer configuration for streaming
func (j *JSONStreamSerializer) Configure(options map[string]interface{}) error {
	// Call parent configure first
	if err := j.JSONSerializer.Configure(options); err != nil {
		return err
	}

	if bufferSize, ok := options["buffer_size"].(int); ok && bufferSize > 0 {
		j.bufferSize = bufferSize
	}

	return nil
}

// SerializeStream serializes a slice of values as a JSON stream
func (j *JSONStreamSerializer) SerializeStream(ctx context.Context, values []interface{}) ([]byte, error) {
	if len(values) == 0 {
		return []byte("[]"), nil
	}

	// For now, use standard array serialization
	// In a real implementation, you might want to implement true streaming
	return j.Serialize(ctx, values)
}

// JSONNDSerializer handles newline-delimited JSON
type JSONNDSerializer struct {
	*JSONSerializer
}

// NewJSONNDSerializer creates a newline-delimited JSON serializer
func NewJSONNDSerializer() *JSONNDSerializer {
	return &JSONNDSerializer{
		JSONSerializer: NewJSONSerializer(),
	}
}

// Name returns the serializer name
func (j *JSONNDSerializer) Name() string {
	return "jsonnd"
}

// ContentType returns the MIME content type for NDJSON
func (j *JSONNDSerializer) ContentType() string {
	return "application/x-ndjson"
}

// SerializeND serializes multiple values as newline-delimited JSON
func (j *JSONNDSerializer) SerializeND(ctx context.Context, values []interface{}) ([]byte, error) {
	if len(values) == 0 {
		return []byte{}, nil
	}

	var result []byte
	for i, value := range values {
		data, err := j.JSONSerializer.Serialize(ctx, value)
		if err != nil {
			return nil, err
		}

		result = append(result, data...)
		if i < len(values)-1 {
			result = append(result, '\n')
		}
	}

	return result, nil
}

// Extension methods for SerializationError
func (e *SerializationError) WithType(valueType reflect.Type) *SerializationError {
	e.ValueType = valueType
	return e
}

func (e *SerializationError) WithDataSize(size int64) *SerializationError {
	e.DataSize = size
	return e
}

// Factory functions for registration
func NewJSONSerializerFactory() func(map[string]interface{}) (Serializer, error) {
	return func(options map[string]interface{}) (Serializer, error) {
		serializer := NewJSONSerializer()
		if err := serializer.Configure(options); err != nil {
			return nil, err
		}
		return serializer, nil
	}
}

func NewJSONArraySerializerFactory() func(map[string]interface{}) (Serializer, error) {
	return func(options map[string]interface{}) (Serializer, error) {
		serializer := NewJSONArraySerializer()
		if err := serializer.Configure(options); err != nil {
			return nil, err
		}
		return serializer, nil
	}
}

func NewJSONStreamSerializerFactory() func(map[string]interface{}) (Serializer, error) {
	return func(options map[string]interface{}) (Serializer, error) {
		serializer := NewJSONStreamSerializer()
		if err := serializer.Configure(options); err != nil {
			return nil, err
		}
		return serializer, nil
	}
}

func NewJSONNDSerializerFactory() func(map[string]interface{}) (Serializer, error) {
	return func(options map[string]interface{}) (Serializer, error) {
		serializer := NewJSONNDSerializer()
		if err := serializer.Configure(options); err != nil {
			return nil, err
		}
		return serializer, nil
	}
}

// Register JSON serializers with the default registry
func init() {
	DefaultRegistry.Register("json", NewJSONSerializerFactory())
	DefaultRegistry.Register("json-array", NewJSONArraySerializerFactory())
	DefaultRegistry.Register("json-stream", NewJSONStreamSerializerFactory())
	DefaultRegistry.Register("jsonnd", NewJSONNDSerializerFactory())
}
