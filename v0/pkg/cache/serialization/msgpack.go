package serialization

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/vmihailenco/msgpack/v5"
	// "github.com/zclconf/go-cty/cty/msgpack"
)

// MsgPackSerializer implements MessagePack serialization
type MsgPackSerializer struct {
	name             string
	contentType      string
	useCompactInts   bool
	useCompactFloats bool
	sortMapKeys      bool
	useJSONTag       bool
	supportedTypes   map[reflect.Kind]bool
}

// NewMsgPackSerializer creates a new MessagePack serializer
func NewMsgPackSerializer() *MsgPackSerializer {
	return &MsgPackSerializer{
		name:             "msgpack",
		contentType:      "application/msgpack",
		useCompactInts:   true,
		useCompactFloats: true,
		sortMapKeys:      false,
		useJSONTag:       false,
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
func (m *MsgPackSerializer) Name() string {
	return m.name
}

// ContentType returns the MIME content type
func (m *MsgPackSerializer) ContentType() string {
	return m.contentType
}

// Configure configures the MessagePack serializer with options
func (m *MsgPackSerializer) Configure(options map[string]interface{}) error {
	if useCompactInts, ok := options["use_compact_ints"].(bool); ok {
		m.useCompactInts = useCompactInts
	}

	if useCompactFloats, ok := options["use_compact_floats"].(bool); ok {
		m.useCompactFloats = useCompactFloats
	}

	if sortMapKeys, ok := options["sort_map_keys"].(bool); ok {
		m.sortMapKeys = sortMapKeys
	}

	if useJSONTag, ok := options["use_json_tag"].(bool); ok {
		m.useJSONTag = useJSONTag
	}

	return nil
}

// SupportsType checks if the serializer supports the given type
func (m *MsgPackSerializer) SupportsType(valueType reflect.Type) bool {
	// Handle pointer types
	for valueType.Kind() == reflect.Ptr {
		valueType = valueType.Elem()
	}

	// Check basic types
	if supported, exists := m.supportedTypes[valueType.Kind()]; exists && supported {
		// Special cases that MessagePack doesn't support
		switch valueType.Kind() {
		case reflect.Chan, reflect.Func, reflect.UnsafePointer:
			return false
		case reflect.Complex64, reflect.Complex128:
			return false // MessagePack doesn't support complex numbers natively
		}
		return true
	}

	// Special types
	switch valueType {
	case reflect.TypeOf(time.Time{}):
		return true
	case reflect.TypeOf(time.Duration(0)):
		return true
	}

	// Check for msgpack.Marshaler interface
	marshalerType := reflect.TypeOf((*msgpack.Marshaler)(nil)).Elem()
	if valueType.Implements(marshalerType) {
		return true
	}

	// Check for msgpack.Unmarshaler interface
	unmarshalerType := reflect.TypeOf((*msgpack.Unmarshaler)(nil)).Elem()
	if reflect.PtrTo(valueType).Implements(unmarshalerType) {
		return true
	}

	return false
}

// Serialize converts a value to MessagePack bytes
func (m *MsgPackSerializer) Serialize(ctx context.Context, value interface{}) ([]byte, error) {
	if value == nil {
		return msgpack.Marshal(nil)
	}

	// Check if the type is supported
	valueType := reflect.TypeOf(value)
	if !m.SupportsType(valueType) {
		return nil, NewSerializationError("serialize", m.name, "unsupported type", nil).WithType(valueType)
	}

	data, err := msgpack.Marshal(value)
	if err != nil {
		return nil, NewSerializationError("serialize", m.name, "marshal failed", err).WithType(valueType)
	}

	return data, nil
}

// Deserialize converts MessagePack bytes to a value
func (m *MsgPackSerializer) Deserialize(ctx context.Context, data []byte, target interface{}) error {
	if target == nil {
		return NewSerializationError("deserialize", m.name, "target cannot be nil", nil)
	}

	// Check if target is a pointer
	targetValue := reflect.ValueOf(target)
	if targetValue.Kind() != reflect.Ptr {
		return NewSerializationError("deserialize", m.name, "target must be a pointer", nil).WithType(targetValue.Type())
	}

	err := msgpack.Unmarshal(data, target)
	if err != nil {
		return NewSerializationError("deserialize", m.name, "unmarshal failed", err).WithType(targetValue.Type())
	}

	return nil
}

// MsgPackCustomSerializer extends MsgPackSerializer with custom encoding
type MsgPackCustomSerializer struct {
	*MsgPackSerializer
	encoders map[reflect.Type]func(interface{}) (interface{}, error)
	decoders map[reflect.Type]func(interface{}) (interface{}, error)
}

// NewMsgPackCustomSerializer creates a MessagePack serializer with custom encoding
func NewMsgPackCustomSerializer() *MsgPackCustomSerializer {
	return &MsgPackCustomSerializer{
		MsgPackSerializer: NewMsgPackSerializer(),
		encoders:          make(map[reflect.Type]func(interface{}) (interface{}, error)),
		decoders:          make(map[reflect.Type]func(interface{}) (interface{}, error)),
	}
}

// Name returns the serializer name
func (m *MsgPackCustomSerializer) Name() string {
	return "msgpack-custom"
}

// RegisterEncoder registers a custom encoder for a specific type
func (m *MsgPackCustomSerializer) RegisterEncoder(valueType reflect.Type, encoder func(interface{}) (interface{}, error)) {
	m.encoders[valueType] = encoder
}

// RegisterDecoder registers a custom decoder for a specific type
func (m *MsgPackCustomSerializer) RegisterDecoder(valueType reflect.Type, decoder func(interface{}) (interface{}, error)) {
	m.decoders[valueType] = decoder
}

// Serialize with custom encoding support
func (m *MsgPackCustomSerializer) Serialize(ctx context.Context, value interface{}) ([]byte, error) {
	if value == nil {
		return msgpack.Marshal(nil)
	}

	valueType := reflect.TypeOf(value)

	// Check for custom encoder
	if encoder, exists := m.encoders[valueType]; exists {
		encodedValue, err := encoder(value)
		if err != nil {
			return nil, NewSerializationError("serialize", m.name, "custom encoding failed", err).WithType(valueType)
		}
		value = encodedValue
	}

	return m.MsgPackSerializer.Serialize(ctx, value)
}

// Deserialize with custom decoding support
func (m *MsgPackCustomSerializer) Deserialize(ctx context.Context, data []byte, target interface{}) error {
	if target == nil {
		return NewSerializationError("deserialize", m.name, "target cannot be nil", nil)
	}

	targetValue := reflect.ValueOf(target)
	if targetValue.Kind() != reflect.Ptr {
		return NewSerializationError("deserialize", m.name, "target must be a pointer", nil)
	}

	targetType := targetValue.Elem().Type()

	// Check for custom decoder
	if decoder, exists := m.decoders[targetType]; exists {
		// First unmarshal into an interface{}
		var intermediate interface{}
		if err := msgpack.Unmarshal(data, &intermediate); err != nil {
			return NewSerializationError("deserialize", m.name, "intermediate unmarshal failed", err).WithType(targetType)
		}

		// Apply custom decoder
		decodedValue, err := decoder(intermediate)
		if err != nil {
			return NewSerializationError("deserialize", m.name, "custom decoding failed", err).WithType(targetType)
		}

		// Set the decoded value
		targetValue.Elem().Set(reflect.ValueOf(decodedValue))
		return nil
	}

	return m.MsgPackSerializer.Deserialize(ctx, data, target)
}

// MsgPackStreamSerializer handles streaming MessagePack serialization
type MsgPackStreamSerializer struct {
	*MsgPackSerializer
	bufferSize int
}

// NewMsgPackStreamSerializer creates a streaming MessagePack serializer
func NewMsgPackStreamSerializer() *MsgPackStreamSerializer {
	return &MsgPackStreamSerializer{
		MsgPackSerializer: NewMsgPackSerializer(),
		bufferSize:        4096,
	}
}

// Name returns the serializer name
func (m *MsgPackStreamSerializer) Name() string {
	return "msgpack-stream"
}

// Configure extends the base MessagePack serializer configuration for streaming
func (m *MsgPackStreamSerializer) Configure(options map[string]interface{}) error {
	// Call parent configure first
	if err := m.MsgPackSerializer.Configure(options); err != nil {
		return err
	}

	if bufferSize, ok := options["buffer_size"].(int); ok && bufferSize > 0 {
		m.bufferSize = bufferSize
	}

	return nil
}

// SerializeStream serializes a slice of values as a MessagePack array
func (m *MsgPackStreamSerializer) SerializeStream(ctx context.Context, values []interface{}) ([]byte, error) {
	if len(values) == 0 {
		return msgpack.Marshal([]interface{}{})
	}

	return m.Serialize(ctx, values)
}

// MsgPackTypedSerializer handles type information in serialization
type MsgPackTypedSerializer struct {
	*MsgPackSerializer
	includeTypeInfo bool
}

// NewMsgPackTypedSerializer creates a typed MessagePack serializer
func NewMsgPackTypedSerializer() *MsgPackTypedSerializer {
	return &MsgPackTypedSerializer{
		MsgPackSerializer: NewMsgPackSerializer(),
		includeTypeInfo:   true,
	}
}

// Name returns the serializer name
func (m *MsgPackTypedSerializer) Name() string {
	return "msgpack-typed"
}

// TypedData represents data with type information
type TypedData struct {
	Type string      `msgpack:"type"`
	Data interface{} `msgpack:"data"`
}

// Serialize with type information
func (m *MsgPackTypedSerializer) Serialize(ctx context.Context, value interface{}) ([]byte, error) {
	if value == nil {
		return msgpack.Marshal(nil)
	}

	if !m.includeTypeInfo {
		return m.MsgPackSerializer.Serialize(ctx, value)
	}

	valueType := reflect.TypeOf(value)
	typed := TypedData{
		Type: valueType.String(),
		Data: value,
	}

	return m.MsgPackSerializer.Serialize(ctx, typed)
}

// Deserialize with type validation
func (m *MsgPackTypedSerializer) Deserialize(ctx context.Context, data []byte, target interface{}) error {
	if target == nil {
		return NewSerializationError("deserialize", m.name, "target cannot be nil", nil)
	}

	if !m.includeTypeInfo {
		return m.MsgPackSerializer.Deserialize(ctx, data, target)
	}

	var typed TypedData
	if err := msgpack.Unmarshal(data, &typed); err != nil {
		// Fallback to direct deserialization
		return m.MsgPackSerializer.Deserialize(ctx, data, target)
	}

	targetValue := reflect.ValueOf(target)
	if targetValue.Kind() != reflect.Ptr {
		return NewSerializationError("deserialize", m.name, "target must be a pointer", nil)
	}

	expectedType := targetValue.Elem().Type().String()
	if typed.Type != expectedType {
		return NewSerializationError("deserialize", m.name,
			fmt.Sprintf("type mismatch: expected %s, got %s", expectedType, typed.Type), nil)
	}

	// Marshal the data again and unmarshal into target
	dataBytes, err := msgpack.Marshal(typed.Data)
	if err != nil {
		return NewSerializationError("deserialize", m.name, "re-marshal failed", err)
	}

	return m.MsgPackSerializer.Deserialize(ctx, dataBytes, target)
}

// Configure typed serializer options
func (m *MsgPackTypedSerializer) Configure(options map[string]interface{}) error {
	// Call parent configure first
	if err := m.MsgPackSerializer.Configure(options); err != nil {
		return err
	}

	if includeTypeInfo, ok := options["include_type_info"].(bool); ok {
		m.includeTypeInfo = includeTypeInfo
	}

	return nil
}

// Factory functions for registration
func NewMsgPackSerializerFactory() func(map[string]interface{}) (Serializer, error) {
	return func(options map[string]interface{}) (Serializer, error) {
		serializer := NewMsgPackSerializer()
		if err := serializer.Configure(options); err != nil {
			return nil, err
		}
		return serializer, nil
	}
}

func NewMsgPackCustomSerializerFactory() func(map[string]interface{}) (Serializer, error) {
	return func(options map[string]interface{}) (Serializer, error) {
		serializer := NewMsgPackCustomSerializer()
		if err := serializer.Configure(options); err != nil {
			return nil, err
		}
		return serializer, nil
	}
}

func NewMsgPackStreamSerializerFactory() func(map[string]interface{}) (Serializer, error) {
	return func(options map[string]interface{}) (Serializer, error) {
		serializer := NewMsgPackStreamSerializer()
		if err := serializer.Configure(options); err != nil {
			return nil, err
		}
		return serializer, nil
	}
}

func NewMsgPackTypedSerializerFactory() func(map[string]interface{}) (Serializer, error) {
	return func(options map[string]interface{}) (Serializer, error) {
		serializer := NewMsgPackTypedSerializer()
		if err := serializer.Configure(options); err != nil {
			return nil, err
		}
		return serializer, nil
	}
}

// Register MessagePack serializers with the default registry
func init() {
	DefaultRegistry.Register("msgpack", NewMsgPackSerializerFactory())
	DefaultRegistry.Register("msgpack-custom", NewMsgPackCustomSerializerFactory())
	DefaultRegistry.Register("msgpack-stream", NewMsgPackStreamSerializerFactory())
	DefaultRegistry.Register("msgpack-typed", NewMsgPackTypedSerializerFactory())
}
