package cli

import (
	"testing"
	"time"
)

func TestNewStringFlag(t *testing.T) {
	flag := NewStringFlag("name", "n", "User name", "default")

	if flag.Name() != "name" {
		t.Errorf("expected name 'name', got '%s'", flag.Name())
	}

	if flag.ShortName() != "n" {
		t.Errorf("expected short name 'n', got '%s'", flag.ShortName())
	}

	if flag.Description() != "User name" {
		t.Errorf("expected description 'User name', got '%s'", flag.Description())
	}

	if flag.DefaultValue() != "default" {
		t.Errorf("expected default value 'default', got '%v'", flag.DefaultValue())
	}

	if flag.Type() != StringFlagType {
		t.Errorf("expected type StringFlagType, got %v", flag.Type())
	}
}

func TestNewIntFlag(t *testing.T) {
	flag := NewIntFlag("port", "p", "Port number", 8080)

	if flag.Name() != "port" {
		t.Errorf("expected name 'port', got '%s'", flag.Name())
	}

	if flag.DefaultValue() != 8080 {
		t.Errorf("expected default value 8080, got '%v'", flag.DefaultValue())
	}

	if flag.Type() != IntFlagType {
		t.Errorf("expected type IntFlagType, got %v", flag.Type())
	}
}

func TestNewBoolFlag(t *testing.T) {
	flag := NewBoolFlag("verbose", "v", "Verbose output", false)

	if flag.Name() != "verbose" {
		t.Errorf("expected name 'verbose', got '%s'", flag.Name())
	}

	if flag.DefaultValue() != false {
		t.Errorf("expected default value false, got '%v'", flag.DefaultValue())
	}

	if flag.Type() != BoolFlagType {
		t.Errorf("expected type BoolFlagType, got %v", flag.Type())
	}
}

func TestNewDurationFlag(t *testing.T) {
	flag := NewDurationFlag("timeout", "t", "Timeout duration", 30*time.Second)

	if flag.Name() != "timeout" {
		t.Errorf("expected name 'timeout', got '%s'", flag.Name())
	}

	if flag.DefaultValue() != 30*time.Second {
		t.Errorf("expected default value 30s, got '%v'", flag.DefaultValue())
	}

	if flag.Type() != DurationFlagType {
		t.Errorf("expected type DurationFlagType, got %v", flag.Type())
	}
}

func TestRequiredFlag(t *testing.T) {
	flag := NewStringFlag("name", "n", "Name", "", Required())

	if !flag.Required() {
		t.Error("expected flag to be required")
	}
}

func TestValidateRange(t *testing.T) {
	flag := NewIntFlag("port", "p", "Port", 8080, ValidateRange(1, 65535))

	// Valid value
	err := flag.Validate(8080)
	if err != nil {
		t.Errorf("expected no error for valid value, got %v", err)
	}

	// Too low
	err = flag.Validate(0)
	if err == nil {
		t.Error("expected error for value too low")
	}

	// Too high
	err = flag.Validate(70000)
	if err == nil {
		t.Error("expected error for value too high")
	}
}

func TestValidateEnum(t *testing.T) {
	flag := NewStringFlag("env", "e", "Environment", "dev", ValidateEnum("dev", "staging", "prod"))

	// Valid value
	err := flag.Validate("dev")
	if err != nil {
		t.Errorf("expected no error for valid value, got %v", err)
	}

	err = flag.Validate("staging")
	if err != nil {
		t.Errorf("expected no error for valid value, got %v", err)
	}

	// Invalid value
	err = flag.Validate("invalid")
	if err == nil {
		t.Error("expected error for invalid value")
	}
}

func TestFlagValue(t *testing.T) {
	// String value
	strValue := &flagValue{rawValue: "test", isSet: true}
	if strValue.String() != "test" {
		t.Errorf("expected string 'test', got '%s'", strValue.String())
	}

	// Int value
	intValue := &flagValue{rawValue: 42, isSet: true}
	if intValue.Int() != 42 {
		t.Errorf("expected int 42, got %d", intValue.Int())
	}

	// Bool value
	boolValue := &flagValue{rawValue: true, isSet: true}
	if !boolValue.Bool() {
		t.Error("expected bool true, got false")
	}

	// IsSet
	unsetValue := &flagValue{rawValue: nil, isSet: false}
	if unsetValue.IsSet() {
		t.Error("expected IsSet to be false")
	}
}

func TestParseValue(t *testing.T) {
	// String
	val, err := parseValue("hello", StringFlagType)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if val != "hello" {
		t.Errorf("expected 'hello', got '%v'", val)
	}

	// Int
	val, err = parseValue("42", IntFlagType)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if val != 42 {
		t.Errorf("expected 42, got %v", val)
	}

	// Bool
	val, err = parseValue("true", BoolFlagType)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if val != true {
		t.Errorf("expected true, got %v", val)
	}

	// Duration
	val, err = parseValue("5s", DurationFlagType)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if val != 5*time.Second {
		t.Errorf("expected 5s, got %v", val)
	}
}
