package farp

import "errors"

// Common errors.
var (
	// ErrManifestNotFound is returned when a schema manifest is not found.
	ErrManifestNotFound = errors.New("schema manifest not found")

	// ErrSchemaNotFound is returned when a schema is not found.
	ErrSchemaNotFound = errors.New("schema not found")

	// ErrInvalidManifest is returned when a manifest has invalid format.
	ErrInvalidManifest = errors.New("invalid manifest format")

	// ErrInvalidSchema is returned when a schema has invalid format.
	ErrInvalidSchema = errors.New("invalid schema format")

	// ErrSchemaToLarge is returned when a schema exceeds size limits.
	ErrSchemaToLarge = errors.New("schema exceeds size limit")

	// ErrChecksumMismatch is returned when a schema checksum doesn't match.
	ErrChecksumMismatch = errors.New("schema checksum mismatch")

	// ErrUnsupportedType is returned when a schema type is not supported.
	ErrUnsupportedType = errors.New("unsupported schema type")

	// ErrBackendUnavailable is returned when the backend is unavailable.
	ErrBackendUnavailable = errors.New("backend unavailable")

	// ErrIncompatibleVersion is returned when protocol versions are incompatible.
	ErrIncompatibleVersion = errors.New("incompatible protocol version")

	// ErrInvalidLocation is returned when a schema location is invalid.
	ErrInvalidLocation = errors.New("invalid schema location")

	// ErrProviderNotFound is returned when a schema provider is not found.
	ErrProviderNotFound = errors.New("schema provider not found")

	// ErrRegistryNotConfigured is returned when no registry is configured.
	ErrRegistryNotConfigured = errors.New("schema registry not configured")

	// ErrSchemaFetchFailed is returned when schema fetch fails.
	ErrSchemaFetchFailed = errors.New("failed to fetch schema")

	// ErrValidationFailed is returned when schema validation fails.
	ErrValidationFailed = errors.New("schema validation failed")
)

// ManifestError represents a manifest-specific error.
type ManifestError struct {
	ServiceName string
	InstanceID  string
	Err         error
}

func (e *ManifestError) Error() string {
	return "manifest error for service=" + e.ServiceName +
		" instance=" + e.InstanceID + ": " + e.Err.Error()
}

func (e *ManifestError) Unwrap() error {
	return e.Err
}

// SchemaError represents a schema-specific error.
type SchemaError struct {
	Type SchemaType
	Path string
	Err  error
}

func (e *SchemaError) Error() string {
	return "schema error type=" + string(e.Type) +
		" path=" + e.Path + ": " + e.Err.Error()
}

func (e *SchemaError) Unwrap() error {
	return e.Err
}

// ValidationError represents a validation error.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return "validation error: field=" + e.Field + " message=" + e.Message
}
