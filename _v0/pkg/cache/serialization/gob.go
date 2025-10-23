package serialization

import (
	"bytes"
	"context"
	"encoding/gob"
	"reflect"
	"sync"
	"time"
)

// GobSerializer implements Go's gob serialization
type GobSerializer struct {
	name            string
	contentType     string
	registerTypes   bool
	registeredTypes map[reflect.Type]bool
	mu              sync.RWMutex
	supportedTypes  map[reflect.Kind]bool
}

// NewGobSerializer creates a new Gob serializer
func NewGobSerializer() *GobSerializer {
	serializer := &GobSerializer{
		name:            "gob",
		contentType:     "application/gob",
		registerTypes:   true,
		registeredTypes: make(map[reflect.Type]bool),
		supportedTypes: map[reflect.Kind]bool{
			reflect.Bool:       true,
			reflect.Int:        true,
			reflect.Int8:       true,
			reflect.Int16:      true,
			reflect.Int32:      true,
			reflect.Int64:      true,
			reflect.Uint:       true,
			reflect.Uint8:      true,
			reflect.Uint16:     true,
			reflect.Uint32:     true,
			reflect.Uint64:     true,
			reflect.Uintptr:    true,
			reflect.Float32:    true,
			reflect.Float64:    true,
			reflect.Complex64:  true,
			reflect.Complex128: true,
			reflect.String:     true,
			reflect.Array:      true,
			reflect.Slice:      true,
			reflect.Map:        true,
			reflect.Struct:     true,
			reflect.Ptr:        true,
			reflect.Interface:  true,
		},
	}

	// Register common types by default
	serializer.registerCommonTypes()

	return serializer
}

// Name returns the serializer name
func (g *GobSerializer) Name() string {
	return g.name
}

// ContentType returns the MIME content type
func (g *GobSerializer) ContentType() string {
	return g.contentType
}

// Configure configures the Gob serializer with options
func (g *GobSerializer) Configure(options map[string]interface{}) error {
	if registerTypes, ok := options["register_types"].(bool); ok {
		g.registerTypes = registerTypes
	}

	// Register additional types if specified
	if types, ok := options["types"].([]interface{}); ok {
		for _, t := range types {
			if err := g.RegisterType(t); err != nil {
				return NewSerializationError("configure", g.name, "failed to register type", err)
			}
		}
	}

	return nil
}

// SupportsType checks if the serializer supports the given type
func (g *GobSerializer) SupportsType(valueType reflect.Type) bool {
	// Handle pointer types
	originalType := valueType
	for valueType.Kind() == reflect.Ptr {
		valueType = valueType.Elem()
	}

	// Check if the kind is supported
	if supported, exists := g.supportedTypes[valueType.Kind()]; !exists || !supported {
		return false
	}

	// Gob has special requirements
	switch valueType.Kind() {
	case reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return false
	case reflect.Interface:
		// Empty interfaces are supported, but typed interfaces need registration
		return valueType == reflect.TypeOf((*interface{})(nil)).Elem()
	case reflect.Map:
		// Check key and value types
		keyType := valueType.Key()
		valueType := valueType.Elem()
		return g.SupportsType(keyType) && g.SupportsType(valueType)
	case reflect.Slice, reflect.Array:
		// Check element type
		elemType := valueType.Elem()
		return g.SupportsType(elemType)
	case reflect.Struct:
		// Check if all fields are supported
		return g.isStructSupported(valueType)
	}

	// Check if the type implements gob.GobEncoder/GobDecoder
	encoderType := reflect.TypeOf((*gob.GobEncoder)(nil)).Elem()
	decoderType := reflect.TypeOf((*gob.GobDecoder)(nil)).Elem()

	if originalType.Implements(encoderType) && reflect.PtrTo(originalType).Implements(decoderType) {
		return true
	}

	return true
}

// isStructSupported checks if a struct type is supported
func (g *GobSerializer) isStructSupported(structType reflect.Type) bool {
	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Check if field type is supported
		if !g.SupportsType(field.Type) {
			return false
		}
	}
	return true
}

// RegisterType registers a type with gob for encoding/decoding
func (g *GobSerializer) RegisterType(value interface{}) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	valueType := reflect.TypeOf(value)
	if valueType == nil {
		return NewSerializationError("register", g.name, "cannot register nil type", nil)
	}

	// Register with gob if not already registered
	if !g.registeredTypes[valueType] {
		gob.Register(value)
		g.registeredTypes[valueType] = true
	}

	return nil
}

// registerCommonTypes registers commonly used types
func (g *GobSerializer) registerCommonTypes() {
	commonTypes := []interface{}{
		time.Time{},
		time.Duration(0),
		map[string]interface{}{},
		[]interface{}{},
		(*interface{})(nil),
	}

	for _, t := range commonTypes {
		g.RegisterType(t)
	}
}

// Serialize converts a value to gob bytes
func (g *GobSerializer) Serialize(ctx context.Context, value interface{}) ([]byte, error) {
	if value == nil {
		return g.serializeNil()
	}

	// Check if the type is supported
	valueType := reflect.TypeOf(value)
	if !g.SupportsType(valueType) {
		return nil, NewSerializationError("serialize", g.name, "unsupported type", nil).WithType(valueType)
	}

	// Auto-register type if enabled
	if g.registerTypes {
		if err := g.RegisterType(value); err != nil {
			return nil, NewSerializationError("serialize", g.name, "failed to register type", err).WithType(valueType)
		}
	}

	var buf bytes.Buffer
	encoder := gob.NewEncoder(&buf)

	err := encoder.Encode(value)
	if err != nil {
		return nil, NewSerializationError("serialize", g.name, "encode failed", err).WithType(valueType)
	}

	return buf.Bytes(), nil
}

// Deserialize converts gob bytes to a value
func (g *GobSerializer) Deserialize(ctx context.Context, data []byte, target interface{}) error {
	if target == nil {
		return NewSerializationError("deserialize", g.name, "target cannot be nil", nil)
	}

	// Check if target is a pointer
	targetValue := reflect.ValueOf(target)
	if targetValue.Kind() != reflect.Ptr {
		return NewSerializationError("deserialize", g.name, "target must be a pointer", nil).WithType(targetValue.Type())
	}

	// Handle nil data (represents nil value)
	if len(data) == 0 || (len(data) == 1 && data[0] == 0) {
		return g.deserializeNil(target)
	}

	buf := bytes.NewBuffer(data)
	decoder := gob.NewDecoder(buf)

	err := decoder.Decode(target)
	if err != nil {
		return NewSerializationError("deserialize", g.name, "decode failed", err).WithType(targetValue.Type())
	}

	return nil
}

// serializeNil handles nil value serialization
func (g *GobSerializer) serializeNil() ([]byte, error) {
	// Use a special marker for nil values
	return []byte{0}, nil
}

// deserializeNil handles nil value deserialization
func (g *GobSerializer) deserializeNil(target interface{}) error {
	targetValue := reflect.ValueOf(target)
	if targetValue.Kind() != reflect.Ptr {
		return NewSerializationError("deserialize", g.name, "target must be a pointer for nil deserialization", nil)
	}

	// Set the pointer to nil
	targetValue.Elem().Set(reflect.Zero(targetValue.Elem().Type()))
	return nil
}

// GobRegistrySerializer extends GobSerializer with type registry management
type GobRegistrySerializer struct {
	*GobSerializer
	typeRegistry map[string]reflect.Type
	nameRegistry map[reflect.Type]string
}

// NewGobRegistrySerializer creates a Gob serializer with type registry
func NewGobRegistrySerializer() *GobRegistrySerializer {
	return &GobRegistrySerializer{
		GobSerializer: NewGobSerializer(),
		typeRegistry:  make(map[string]reflect.Type),
		nameRegistry:  make(map[reflect.Type]string),
	}
}

// Name returns the serializer name
func (g *GobRegistrySerializer) Name() string {
	return "gob-registry"
}

// RegisterTypeWithName registers a type with a specific name
func (g *GobRegistrySerializer) RegisterTypeWithName(name string, value interface{}) error {
	if err := g.GobSerializer.RegisterType(value); err != nil {
		return err
	}

	valueType := reflect.TypeOf(value)
	g.typeRegistry[name] = valueType
	g.nameRegistry[valueType] = name

	return nil
}

// GetTypeByName returns a type by its registered name
func (g *GobRegistrySerializer) GetTypeByName(name string) (reflect.Type, bool) {
	t, exists := g.typeRegistry[name]
	return t, exists
}

// GetNameByType returns the registered name for a type
func (g *GobRegistrySerializer) GetNameByType(t reflect.Type) (string, bool) {
	name, exists := g.nameRegistry[t]
	return name, exists
}

// GobVersionedSerializer handles versioned serialization
type GobVersionedSerializer struct {
	*GobSerializer
	version    int
	migrations map[int]func([]byte) ([]byte, error)
}

// NewGobVersionedSerializer creates a versioned Gob serializer
func NewGobVersionedSerializer(version int) *GobVersionedSerializer {
	return &GobVersionedSerializer{
		GobSerializer: NewGobSerializer(),
		version:       version,
		migrations:    make(map[int]func([]byte) ([]byte, error)),
	}
}

// Name returns the serializer name
func (g *GobVersionedSerializer) Name() string {
	return "gob-versioned"
}

// VersionedData represents versioned serialized data
type VersionedData struct {
	Version int    `json:"version"`
	Data    []byte `json:"data"`
}

// Serialize with version information
func (g *GobVersionedSerializer) Serialize(ctx context.Context, value interface{}) ([]byte, error) {
	data, err := g.GobSerializer.Serialize(ctx, value)
	if err != nil {
		return nil, err
	}

	versioned := VersionedData{
		Version: g.version,
		Data:    data,
	}

	return g.GobSerializer.Serialize(ctx, versioned)
}

// Deserialize with version migration support
func (g *GobVersionedSerializer) Deserialize(ctx context.Context, data []byte, target interface{}) error {
	var versioned VersionedData
	if err := g.GobSerializer.Deserialize(ctx, data, &versioned); err != nil {
		// Fallback to direct deserialization for backward compatibility
		return g.GobSerializer.Deserialize(ctx, data, target)
	}

	// Apply migrations if needed
	migrationData := versioned.Data
	for version := versioned.Version; version < g.version; version++ {
		if migration, exists := g.migrations[version]; exists {
			var err error
			migrationData, err = migration(migrationData)
			if err != nil {
				return NewSerializationError("deserialize", g.name, "migration failed", err)
			}
		}
	}

	return g.GobSerializer.Deserialize(ctx, migrationData, target)
}

// AddMigration adds a migration function for a specific version
func (g *GobVersionedSerializer) AddMigration(fromVersion int, migration func([]byte) ([]byte, error)) {
	g.migrations[fromVersion] = migration
}

// Factory functions for registration
func NewGobSerializerFactory() func(map[string]interface{}) (Serializer, error) {
	return func(options map[string]interface{}) (Serializer, error) {
		serializer := NewGobSerializer()
		if err := serializer.Configure(options); err != nil {
			return nil, err
		}
		return serializer, nil
	}
}

func NewGobRegistrySerializerFactory() func(map[string]interface{}) (Serializer, error) {
	return func(options map[string]interface{}) (Serializer, error) {
		serializer := NewGobRegistrySerializer()
		if err := serializer.Configure(options); err != nil {
			return nil, err
		}
		return serializer, nil
	}
}

func NewGobVersionedSerializerFactory() func(map[string]interface{}) (Serializer, error) {
	return func(options map[string]interface{}) (Serializer, error) {
		version := 1
		if v, ok := options["version"].(int); ok {
			version = v
		}

		serializer := NewGobVersionedSerializer(version)
		if err := serializer.Configure(options); err != nil {
			return nil, err
		}
		return serializer, nil
	}
}

// Register Gob serializers with the default registry
func init() {
	DefaultRegistry.Register("gob", NewGobSerializerFactory())
	DefaultRegistry.Register("gob-registry", NewGobRegistrySerializerFactory())
	DefaultRegistry.Register("gob-versioned", NewGobVersionedSerializerFactory())
}
