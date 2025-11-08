package core

// BindOptions provides flexible options for binding configuration to structs.
type BindOptions struct {
	// DefaultValue to use when the key is not found or is nil
	DefaultValue any

	// UseDefaults when true, uses struct field default values for missing keys
	UseDefaults bool

	// IgnoreCase when true, performs case-insensitive key matching
	IgnoreCase bool

	// TagName specifies which struct tag to use for field mapping (default: "yaml")
	TagName string

	// Required specifies field names that must be present in the configuration
	Required []string

	// ErrorOnMissing when true, returns error if any field cannot be bound
	ErrorOnMissing bool

	// DeepMerge when true, performs deep merging for nested structs
	DeepMerge bool
}

// DefaultBindOptions returns default bind options.
func DefaultBindOptions() BindOptions {
	return BindOptions{
		UseDefaults:    true,
		IgnoreCase:     false,
		TagName:        "yaml",
		Required:       []string{},
		ErrorOnMissing: false,
		DeepMerge:      true,
	}
}
