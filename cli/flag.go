package cli

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/xraph/forge/errors"
)

// Flag represents a command-line flag.
type Flag interface {
	Name() string
	ShortName() string
	Description() string
	DefaultValue() any
	Required() bool
	Type() FlagType
	Validate(value any) error
}

// FlagValue represents a parsed flag value.
type FlagValue interface {
	String() string
	Int() int
	Bool() bool
	StringSlice() []string
	Duration() time.Duration
	IsSet() bool
	Raw() any
}

// FlagType represents the type of a flag.
type FlagType int

const (
	StringFlagType FlagType = iota
	IntFlagType
	BoolFlagType
	StringSliceFlagType
	DurationFlagType
)

// FlagOption is a functional option for configuring flags.
type FlagOption func(*flagConfig)

// flagConfig holds flag configuration.
type flagConfig struct {
	shortName    string
	description  string
	defaultValue any
	required     bool
	validator    func(any) error
}

// flag implements the Flag interface.
type flag struct {
	name         string
	shortName    string
	description  string
	defaultValue any
	required     bool
	flagType     FlagType
	validator    func(any) error
}

func (f *flag) Name() string        { return f.name }
func (f *flag) ShortName() string   { return f.shortName }
func (f *flag) Description() string { return f.description }
func (f *flag) DefaultValue() any   { return f.defaultValue }
func (f *flag) Required() bool      { return f.required }
func (f *flag) Type() FlagType      { return f.flagType }

func (f *flag) Validate(value any) error {
	if f.validator != nil {
		return f.validator(value)
	}

	return nil
}

// flagValue implements the FlagValue interface.
type flagValue struct {
	rawValue any
	isSet    bool
}

func (fv *flagValue) String() string {
	if fv.rawValue == nil {
		return ""
	}

	return fmt.Sprintf("%v", fv.rawValue)
}

func (fv *flagValue) Int() int {
	if fv.rawValue == nil {
		return 0
	}

	switch v := fv.rawValue.(type) {
	case int:
		return v
	case string:
		i, _ := strconv.Atoi(v)

		return i
	default:
		return 0
	}
}

func (fv *flagValue) Bool() bool {
	if fv.rawValue == nil {
		return false
	}

	switch v := fv.rawValue.(type) {
	case bool:
		return v
	case string:
		return v == "true" || v == "1" || v == "yes"
	default:
		return false
	}
}

func (fv *flagValue) StringSlice() []string {
	if fv.rawValue == nil {
		return []string{}
	}

	switch v := fv.rawValue.(type) {
	case []string:
		return v
	case string:
		return strings.Split(v, ",")
	default:
		return []string{}
	}
}

func (fv *flagValue) Duration() time.Duration {
	if fv.rawValue == nil {
		return 0
	}

	switch v := fv.rawValue.(type) {
	case time.Duration:
		return v
	case string:
		d, _ := time.ParseDuration(v)

		return d
	case int64:
		return time.Duration(v)
	default:
		return 0
	}
}

func (fv *flagValue) IsSet() bool { return fv.isSet }
func (fv *flagValue) Raw() any    { return fv.rawValue }

// Flag option constructors

// WithAlias sets a short name alias for the flag.
func WithAlias(alias string) FlagOption {
	return func(c *flagConfig) {
		c.shortName = alias
	}
}

// WithDefault sets a default value for the flag.
func WithDefault(value any) FlagOption {
	return func(c *flagConfig) {
		c.defaultValue = value
	}
}

// WithDescription sets the description for the flag.
func WithDescription(desc string) FlagOption {
	return func(c *flagConfig) {
		c.description = desc
	}
}

// Required marks the flag as required.
func Required() FlagOption {
	return func(c *flagConfig) {
		c.required = true
	}
}

// WithValidator sets a custom validator for the flag.
func WithValidator(validator func(any) error) FlagOption {
	return func(c *flagConfig) {
		c.validator = validator
	}
}

// ValidateRange validates that an int flag is within a range.
func ValidateRange(minVal, maxVal int) FlagOption {
	return WithValidator(func(value any) error {
		v, ok := value.(int)
		if !ok {
			return errors.New("expected int value")
		}

		if v < minVal || v > maxVal {
			return fmt.Errorf("value must be between %d and %d", minVal, maxVal)
		}

		return nil
	})
}

// ValidateEnum validates that a string flag is one of the allowed values.
func ValidateEnum(allowed ...string) FlagOption {
	return WithValidator(func(value any) error {
		v, ok := value.(string)
		if !ok {
			return errors.New("expected string value")
		}

		if slices.Contains(allowed, v) {
			return nil
		}

		return fmt.Errorf("value must be one of: %s", strings.Join(allowed, ", "))
	})
}

// Flag constructors

// WithStringFlag creates a string flag.
func WithStringFlag(name, shortName, description, defaultValue string, opts ...FlagOption) FlagOption {
	return func(c *flagConfig) {
		// This is used as a command option, not a flag option
		// Implementation will be in command.go
	}
}

// WithIntFlag creates an int flag.
func WithIntFlag(name, shortName, description string, defaultValue int, opts ...FlagOption) FlagOption {
	return func(c *flagConfig) {
		// This is used as a command option, not a flag option
		// Implementation will be in command.go
	}
}

// WithBoolFlag creates a bool flag.
func WithBoolFlag(name, shortName, description string, defaultValue bool, opts ...FlagOption) FlagOption {
	return func(c *flagConfig) {
		// This is used as a command option, not a flag option
		// Implementation will be in command.go
	}
}

// WithStringSliceFlag creates a string slice flag.
func WithStringSliceFlag(name, shortName, description string, defaultValue []string, opts ...FlagOption) FlagOption {
	return func(c *flagConfig) {
		// This is used as a command option, not a flag option
		// Implementation will be in command.go
	}
}

// WithDurationFlag creates a duration flag.
func WithDurationFlag(name, shortName, description string, defaultValue time.Duration, opts ...FlagOption) FlagOption {
	return func(c *flagConfig) {
		// This is used as a command option, not a flag option
		// Implementation will be in command.go
	}
}

// NewFlag creates a new flag.
func NewFlag(name string, flagType FlagType, opts ...FlagOption) Flag {
	config := &flagConfig{}
	for _, opt := range opts {
		opt(config)
	}

	return &flag{
		name:         name,
		shortName:    config.shortName,
		description:  config.description,
		defaultValue: config.defaultValue,
		required:     config.required,
		flagType:     flagType,
		validator:    config.validator,
	}
}

// NewStringFlag creates a string flag.
func NewStringFlag(name, shortName, description, defaultValue string, opts ...FlagOption) Flag {
	opts = append([]FlagOption{WithAlias(shortName), WithDescription(description), WithDefault(defaultValue)}, opts...)

	return NewFlag(name, StringFlagType, opts...)
}

// NewIntFlag creates an int flag.
func NewIntFlag(name, shortName, description string, defaultValue int, opts ...FlagOption) Flag {
	opts = append([]FlagOption{WithAlias(shortName), WithDescription(description), WithDefault(defaultValue)}, opts...)

	return NewFlag(name, IntFlagType, opts...)
}

// NewBoolFlag creates a bool flag.
func NewBoolFlag(name, shortName, description string, defaultValue bool, opts ...FlagOption) Flag {
	opts = append([]FlagOption{WithAlias(shortName), WithDescription(description), WithDefault(defaultValue)}, opts...)

	return NewFlag(name, BoolFlagType, opts...)
}

// NewStringSliceFlag creates a string slice flag.
func NewStringSliceFlag(name, shortName, description string, defaultValue []string, opts ...FlagOption) Flag {
	opts = append([]FlagOption{WithAlias(shortName), WithDescription(description), WithDefault(defaultValue)}, opts...)

	return NewFlag(name, StringSliceFlagType, opts...)
}

// NewDurationFlag creates a duration flag.
func NewDurationFlag(name, shortName, description string, defaultValue time.Duration, opts ...FlagOption) Flag {
	opts = append([]FlagOption{WithAlias(shortName), WithDescription(description), WithDefault(defaultValue)}, opts...)

	return NewFlag(name, DurationFlagType, opts...)
}
