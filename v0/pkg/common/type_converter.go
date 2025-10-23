package common

import (
	"encoding"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// TypeConverter defines an interface for converting string values to specific types
type TypeConverter interface {
	// Convert converts a string value to the target type and sets it on the field
	Convert(field reflect.Value, value string) error
	// CanConvert checks if this converter can handle the given type
	CanConvert(fieldType reflect.Type) bool
}

// TypeConverterRegistry manages type converters
type TypeConverterRegistry struct {
	converters []TypeConverter
}

// NewTypeConverterRegistry creates a new registry with default converters
func NewTypeConverterRegistry() *TypeConverterRegistry {
	registry := &TypeConverterRegistry{
		converters: make([]TypeConverter, 0),
	}

	// Register default converters in order of preference
	registry.Register(&TextUnmarshalerConverter{})
	registry.Register(&BinaryUnmarshalerConverter{})
	registry.Register(&TimeConverter{})
	registry.Register(&DurationConverter{})
	registry.Register(&XIDConverter{})
	registry.Register(&UUIDConverter{})
	registry.Register(&BasicTypeConverter{})
	registry.Register(&SliceConverter{})
	registry.Register(&PointerConverter{registry})

	return registry
}

// Register adds a new converter to the registry
func (r *TypeConverterRegistry) Register(converter TypeConverter) {
	r.converters = append(r.converters, converter)
}

// Convert finds an appropriate converter and converts the value
func (r *TypeConverterRegistry) Convert(field reflect.Value, value string, fieldType reflect.Type) error {
	for _, converter := range r.converters {
		if converter.CanConvert(fieldType) {
			return converter.Convert(field, value)
		}
	}
	return fmt.Errorf("no converter found for type %v", fieldType)
}

// =============================================================================
// BUILT-IN CONVERTERS
// =============================================================================

// TextUnmarshalerConverter handles types that implement encoding.TextUnmarshaler
type TextUnmarshalerConverter struct{}

func (c *TextUnmarshalerConverter) CanConvert(fieldType reflect.Type) bool {
	// Check if type implements encoding.TextUnmarshaler
	textUnmarshalerType := reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem()
	return fieldType.Implements(textUnmarshalerType) || reflect.PtrTo(fieldType).Implements(textUnmarshalerType)
}

func (c *TextUnmarshalerConverter) Convert(field reflect.Value, value string) error {
	// Create a new instance if field is nil pointer
	if field.Kind() == reflect.Ptr && field.IsNil() {
		field.Set(reflect.New(field.Type().Elem()))
	}

	// Get the unmarshaler interface
	var unmarshaler encoding.TextUnmarshaler
	if field.Kind() == reflect.Ptr {
		unmarshaler = field.Interface().(encoding.TextUnmarshaler)
	} else if field.CanAddr() {
		unmarshaler = field.Addr().Interface().(encoding.TextUnmarshaler)
	} else {
		return fmt.Errorf("cannot get address of field for TextUnmarshaler")
	}

	return unmarshaler.UnmarshalText([]byte(value))
}

// BinaryUnmarshalerConverter handles types that implement encoding.BinaryUnmarshaler
type BinaryUnmarshalerConverter struct{}

func (c *BinaryUnmarshalerConverter) CanConvert(fieldType reflect.Type) bool {
	binaryUnmarshalerType := reflect.TypeOf((*encoding.BinaryUnmarshaler)(nil)).Elem()
	return fieldType.Implements(binaryUnmarshalerType) || reflect.PtrTo(fieldType).Implements(binaryUnmarshalerType)
}

func (c *BinaryUnmarshalerConverter) Convert(field reflect.Value, value string) error {
	// For binary unmarshaler, we'll treat the string as hex-encoded data
	// This might need adjustment based on your specific needs
	if field.Kind() == reflect.Ptr && field.IsNil() {
		field.Set(reflect.New(field.Type().Elem()))
	}

	var unmarshaler encoding.BinaryUnmarshaler
	if field.Kind() == reflect.Ptr {
		unmarshaler = field.Interface().(encoding.BinaryUnmarshaler)
	} else if field.CanAddr() {
		unmarshaler = field.Addr().Interface().(encoding.BinaryUnmarshaler)
	} else {
		return fmt.Errorf("cannot get address of field for BinaryUnmarshaler")
	}

	return unmarshaler.UnmarshalBinary([]byte(value))
}

// TimeConverter handles time.Time types
type TimeConverter struct{}

func (c *TimeConverter) CanConvert(fieldType reflect.Type) bool {
	return fieldType == reflect.TypeOf(time.Time{}) ||
		fieldType == reflect.TypeOf(&time.Time{}) ||
		(fieldType.Kind() == reflect.Ptr && fieldType.Elem() == reflect.TypeOf(time.Time{}))
}

func (c *TimeConverter) Convert(field reflect.Value, value string) error {
	// Try multiple time formats
	timeFormats := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05Z",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}

	var parsedTime time.Time
	var err error

	for _, format := range timeFormats {
		parsedTime, err = time.Parse(format, value)
		if err == nil {
			break
		}
	}

	if err != nil {
		return fmt.Errorf("invalid time format: %s", value)
	}

	if field.Kind() == reflect.Ptr {
		if field.IsNil() {
			field.Set(reflect.New(field.Type().Elem()))
		}
		field.Elem().Set(reflect.ValueOf(parsedTime))
	} else {
		field.Set(reflect.ValueOf(parsedTime))
	}

	return nil
}

// DurationConverter handles time.Duration types
type DurationConverter struct{}

func (c *DurationConverter) CanConvert(fieldType reflect.Type) bool {
	return fieldType == reflect.TypeOf(time.Duration(0)) ||
		(fieldType.Kind() == reflect.Ptr && fieldType.Elem() == reflect.TypeOf(time.Duration(0)))
}

func (c *DurationConverter) Convert(field reflect.Value, value string) error {
	duration, err := time.ParseDuration(value)
	if err != nil {
		return fmt.Errorf("invalid duration format: %s", value)
	}

	if field.Kind() == reflect.Ptr {
		if field.IsNil() {
			field.Set(reflect.New(field.Type().Elem()))
		}
		field.Elem().Set(reflect.ValueOf(duration))
	} else {
		field.Set(reflect.ValueOf(duration))
	}

	return nil
}

// XIDConverter handles github.com/rs/xid.ID types specifically
type XIDConverter struct{}

func (c *XIDConverter) CanConvert(fieldType reflect.Type) bool {
	return fieldType.PkgPath() == "github.com/rs/xid" && fieldType.Name() == "ID"
}

func (c *XIDConverter) Convert(field reflect.Value, value string) error {
	// Since xid.ID implements encoding.TextUnmarshaler, we can create a new instance
	// and unmarshal the text. But we need to be careful about the specific API.

	// Create a new instance
	newXID := reflect.New(field.Type())

	// XID should implement TextUnmarshaler, so let's use that
	if unmarshaler, ok := newXID.Interface().(encoding.TextUnmarshaler); ok {
		if err := unmarshaler.UnmarshalText([]byte(value)); err != nil {
			return fmt.Errorf("invalid XID format: %s", value)
		}
		field.Set(newXID.Elem())
		return nil
	}

	// Fallback: try to call FromString method if available
	fromStringMethod := newXID.MethodByName("FromString")
	if fromStringMethod.IsValid() {
		results := fromStringMethod.Call([]reflect.Value{reflect.ValueOf(value)})
		if len(results) >= 2 && !results[1].IsNil() {
			return fmt.Errorf("invalid XID format: %s", value)
		}
		if len(results) >= 1 {
			field.Set(results[0])
			return nil
		}
	}

	return fmt.Errorf("cannot convert XID from string: %s", value)
}

// UUIDConverter handles common UUID types
type UUIDConverter struct{}

func (c *UUIDConverter) CanConvert(fieldType reflect.Type) bool {
	return (fieldType.PkgPath() == "github.com/google/uuid" ||
		fieldType.PkgPath() == "github.com/satori/go.uuid") &&
		fieldType.Name() == "UUID"
}

func (c *UUIDConverter) Convert(field reflect.Value, value string) error {
	// Most UUID libraries implement TextUnmarshaler, so try that first
	if field.Kind() == reflect.Ptr && field.IsNil() {
		field.Set(reflect.New(field.Type().Elem()))
	}

	var target reflect.Value
	if field.Kind() == reflect.Ptr {
		target = field
	} else if field.CanAddr() {
		target = field.Addr()
	} else {
		return fmt.Errorf("cannot get address of UUID field")
	}

	if unmarshaler, ok := target.Interface().(encoding.TextUnmarshaler); ok {
		return unmarshaler.UnmarshalText([]byte(value))
	}

	return fmt.Errorf("UUID type does not implement TextUnmarshaler")
}

// BasicTypeConverter handles basic Go types
type BasicTypeConverter struct{}

func (c *BasicTypeConverter) CanConvert(fieldType reflect.Type) bool {
	switch fieldType.Kind() {
	case reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64, reflect.Bool:
		return true
	}
	return false
}

func (c *BasicTypeConverter) Convert(field reflect.Value, value string) error {
	switch field.Kind() {
	case reflect.String:
		field.SetString(value)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		intVal, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid integer value: %s", value)
		}
		field.SetInt(intVal)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		uintVal, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid unsigned integer value: %s", value)
		}
		field.SetUint(uintVal)
	case reflect.Float32, reflect.Float64:
		floatVal, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("invalid float value: %s", value)
		}
		field.SetFloat(floatVal)
	case reflect.Bool:
		boolVal, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid boolean value: %s", value)
		}
		field.SetBool(boolVal)
	default:
		return fmt.Errorf("unsupported basic type: %v", field.Kind())
	}
	return nil
}

// SliceConverter handles slice types
type SliceConverter struct{}

func (c *SliceConverter) CanConvert(fieldType reflect.Type) bool {
	return fieldType.Kind() == reflect.Slice
}

func (c *SliceConverter) Convert(field reflect.Value, value string) error {
	if field.Type().Elem().Kind() == reflect.String {
		// Handle comma-separated string values
		values := strings.Split(value, ",")
		slice := reflect.MakeSlice(field.Type(), len(values), len(values))
		for i, val := range values {
			slice.Index(i).SetString(strings.TrimSpace(val))
		}
		field.Set(slice)
		return nil
	}

	// For other slice types, you might want to implement JSON unmarshaling
	// or other specific parsing logic
	return fmt.Errorf("unsupported slice element type: %v", field.Type().Elem().Kind())
}

// PointerConverter handles pointer types by delegating to the underlying type
type PointerConverter struct {
	registry *TypeConverterRegistry
}

func (c *PointerConverter) CanConvert(fieldType reflect.Type) bool {
	return fieldType.Kind() == reflect.Ptr
}

func (c *PointerConverter) Convert(field reflect.Value, value string) error {
	// Handle nil pointers
	if field.IsNil() {
		field.Set(reflect.New(field.Type().Elem()))
	}

	// Delegate to converter for the underlying type
	return c.registry.Convert(field.Elem(), value, field.Type().Elem())
}

// =============================================================================
// GLOBAL REGISTRY INSTANCE
// =============================================================================

var defaultTypeConverterRegistry = NewTypeConverterRegistry()

// RegisterTypeConverter registers a custom type converter globally
func RegisterTypeConverter(converter TypeConverter) {
	defaultTypeConverterRegistry.Register(converter)
}

// ConvertFieldValue converts a string value to the appropriate field type
func ConvertFieldValue(field reflect.Value, value string, fieldType reflect.Type) error {
	return defaultTypeConverterRegistry.Convert(field, value, fieldType)
}
