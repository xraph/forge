package serialization

import (
	"context"
	"fmt"
	"reflect"
	"time"
)

// Serializer defines the interface for cache value serialization
type Serializer interface {
	// Name returns the serializer name
	Name() string

	// Serialize converts a value to bytes
	Serialize(ctx context.Context, value interface{}) ([]byte, error)

	// Deserialize converts bytes to a value
	Deserialize(ctx context.Context, data []byte, target interface{}) error

	// SupportsType checks if the serializer supports the given type
	SupportsType(valueType reflect.Type) bool

	// ContentType returns the MIME content type
	ContentType() string

	// Configure configures the serializer with options
	Configure(options map[string]interface{}) error
}

// SerializerFactory creates serializer instances
type SerializerFactory interface {
	Create(name string, options map[string]interface{}) (Serializer, error)
	List() []string
	Register(name string, factory func(map[string]interface{}) (Serializer, error))
}

// SerializationManager manages multiple serializers
type SerializationManager interface {
	// GetSerializer returns a serializer by name
	GetSerializer(name string) (Serializer, error)

	// GetDefaultSerializer returns the default serializer
	GetDefaultSerializer() Serializer

	// SetDefaultSerializer sets the default serializer
	SetDefaultSerializer(name string) error

	// RegisterSerializer registers a new serializer
	RegisterSerializer(serializer Serializer) error

	// DetectSerializer detects the best serializer for a value type
	DetectSerializer(valueType reflect.Type) Serializer

	// Serialize using the best available serializer
	Serialize(ctx context.Context, value interface{}) ([]byte, string, error)

	// Deserialize using the specified serializer
	Deserialize(ctx context.Context, data []byte, serializerName string, target interface{}) error
}

// SerializationEntry represents a serialized cache entry with metadata
type SerializationEntry struct {
	Data           []byte                 `json:"data"`
	SerializerName string                 `json:"serializer_name"`
	ContentType    string                 `json:"content_type"`
	Compressed     bool                   `json:"compressed"`
	Encrypted      bool                   `json:"encrypted"`
	Metadata       map[string]interface{} `json:"metadata"`
	SerializedAt   time.Time              `json:"serialized_at"`
	Size           int64                  `json:"size"`
	Checksum       string                 `json:"checksum,omitempty"`
}

// SerializationStats provides serialization statistics
type SerializationStats struct {
	SerializerName    string        `json:"serializer_name"`
	SerializeCount    int64         `json:"serialize_count"`
	DeserializeCount  int64         `json:"deserialize_count"`
	SerializeErrors   int64         `json:"serialize_errors"`
	DeserializeErrors int64         `json:"deserialize_errors"`
	TotalSize         int64         `json:"total_size"`
	AverageSize       float64       `json:"average_size"`
	SerializeTime     time.Duration `json:"serialize_time"`
	DeserializeTime   time.Duration `json:"deserialize_time"`
	LastUsed          time.Time     `json:"last_used"`
}

// CompressionAlgorithm defines compression algorithms
type CompressionAlgorithm string

const (
	CompressionNone   CompressionAlgorithm = "none"
	CompressionGzip   CompressionAlgorithm = "gzip"
	CompressionLZ4    CompressionAlgorithm = "lz4"
	CompressionSnappy CompressionAlgorithm = "snappy"
	CompressionZstd   CompressionAlgorithm = "zstd"
)

// EncryptionAlgorithm defines encryption algorithms
type EncryptionAlgorithm string

const (
	EncryptionNone      EncryptionAlgorithm = "none"
	EncryptionAES256GCM EncryptionAlgorithm = "aes-256-gcm"
	EncryptionChaCha20  EncryptionAlgorithm = "chacha20-poly1305"
)

// SerializationOptions contains options for serialization
type SerializationOptions struct {
	Compression CompressionAlgorithm   `json:"compression"`
	Encryption  EncryptionAlgorithm    `json:"encryption"`
	Metadata    map[string]interface{} `json:"metadata"`
	IncludeType bool                   `json:"include_type"`
	Validate    bool                   `json:"validate"`
}

// SerializationError represents serialization errors
type SerializationError struct {
	Operation  string
	Serializer string
	Message    string
	Cause      error
	ValueType  reflect.Type
	DataSize   int64
}

func (e *SerializationError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("serialization %s error (%s): %s: %v", e.Operation, e.Serializer, e.Message, e.Cause)
	}
	return fmt.Sprintf("serialization %s error (%s): %s", e.Operation, e.Serializer, e.Message)
}

func (e *SerializationError) Unwrap() error {
	return e.Cause
}

// Common serialization errors
var (
	ErrSerializerNotFound  = &SerializationError{Message: "serializer not found"}
	ErrUnsupportedType     = &SerializationError{Message: "unsupported value type"}
	ErrInvalidData         = &SerializationError{Message: "invalid serialized data"}
	ErrCompressionFailed   = &SerializationError{Message: "compression failed"}
	ErrDecompressionFailed = &SerializationError{Message: "decompression failed"}
	ErrEncryptionFailed    = &SerializationError{Message: "encryption failed"}
	ErrDecryptionFailed    = &SerializationError{Message: "decryption failed"}
)

// NewSerializationError creates a new serialization error
func NewSerializationError(operation, serializer, message string, cause error) *SerializationError {
	return &SerializationError{
		Operation:  operation,
		Serializer: serializer,
		Message:    message,
		Cause:      cause,
	}
}

// Helper functions for type detection
func IsPrimitiveType(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64,
		reflect.String:
		return true
	}
	return false
}

func IsStructType(t reflect.Type) bool {
	return t.Kind() == reflect.Struct
}

func IsSliceType(t reflect.Type) bool {
	return t.Kind() == reflect.Slice
}

func IsMapType(t reflect.Type) bool {
	return t.Kind() == reflect.Map
}

func IsPointerType(t reflect.Type) bool {
	return t.Kind() == reflect.Ptr
}

func GetUnderlyingType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t
}

// SerializerRegistry provides a global registry for serializers
type SerializerRegistry struct {
	serializers map[string]Serializer
	factories   map[string]func(map[string]interface{}) (Serializer, error)
	default_    string
}

func NewSerializerRegistry() *SerializerRegistry {
	return &SerializerRegistry{
		serializers: make(map[string]Serializer),
		factories:   make(map[string]func(map[string]interface{}) (Serializer, error)),
		default_:    "json",
	}
}

func (r *SerializerRegistry) Register(name string, factory func(map[string]interface{}) (Serializer, error)) {
	r.factories[name] = factory
}

func (r *SerializerRegistry) Create(name string, options map[string]interface{}) (Serializer, error) {
	factory, exists := r.factories[name]
	if !exists {
		return nil, NewSerializationError("create", name, "serializer factory not found", nil)
	}

	return factory(options)
}

func (r *SerializerRegistry) List() []string {
	names := make([]string, 0, len(r.factories))
	for name := range r.factories {
		names = append(names, name)
	}
	return names
}

// Global registry instance
var DefaultRegistry = NewSerializerRegistry()

// Default manager implementation
type defaultSerializationManager struct {
	serializers map[string]Serializer
	default_    string
	stats       map[string]*SerializationStats
}

func NewSerializationManager() SerializationManager {
	return &defaultSerializationManager{
		serializers: make(map[string]Serializer),
		default_:    "json",
		stats:       make(map[string]*SerializationStats),
	}
}

func (m *defaultSerializationManager) GetSerializer(name string) (Serializer, error) {
	serializer, exists := m.serializers[name]
	if !exists {
		return nil, NewSerializationError("get", name, "serializer not registered", nil)
	}
	return serializer, nil
}

func (m *defaultSerializationManager) GetDefaultSerializer() Serializer {
	if serializer, exists := m.serializers[m.default_]; exists {
		return serializer
	}
	return nil
}

func (m *defaultSerializationManager) SetDefaultSerializer(name string) error {
	if _, exists := m.serializers[name]; !exists {
		return NewSerializationError("set_default", name, "serializer not registered", nil)
	}
	m.default_ = name
	return nil
}

func (m *defaultSerializationManager) RegisterSerializer(serializer Serializer) error {
	name := serializer.Name()
	m.serializers[name] = serializer
	m.stats[name] = &SerializationStats{
		SerializerName: name,
	}
	return nil
}

func (m *defaultSerializationManager) DetectSerializer(valueType reflect.Type) Serializer {
	// Try to find the best serializer for the type
	for _, serializer := range m.serializers {
		if serializer.SupportsType(valueType) {
			return serializer
		}
	}
	return m.GetDefaultSerializer()
}

func (m *defaultSerializationManager) Serialize(ctx context.Context, value interface{}) ([]byte, string, error) {
	valueType := reflect.TypeOf(value)
	serializer := m.DetectSerializer(valueType)
	if serializer == nil {
		return nil, "", NewSerializationError("serialize", "auto", "no suitable serializer found", nil)
	}

	data, err := serializer.Serialize(ctx, value)
	if err != nil {
		return nil, "", err
	}

	return data, serializer.Name(), nil
}

func (m *defaultSerializationManager) Deserialize(ctx context.Context, data []byte, serializerName string, target interface{}) error {
	serializer, err := m.GetSerializer(serializerName)
	if err != nil {
		return err
	}

	return serializer.Deserialize(ctx, data, target)
}
