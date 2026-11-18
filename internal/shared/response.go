package shared

import "strings"

// SensitiveMode specifies how sensitive fields should be cleaned.
type SensitiveMode int

const (
	// SensitiveModeZero sets sensitive fields to their zero value.
	SensitiveModeZero SensitiveMode = iota
	// SensitiveModeRedact replaces sensitive fields with "[REDACTED]".
	SensitiveModeRedact
	// SensitiveModeMask replaces sensitive fields with a custom mask.
	SensitiveModeMask
)

// SensitiveFieldConfig holds configuration for a sensitive field.
type SensitiveFieldConfig struct {
	Mode SensitiveMode
	Mask string // Custom mask for SensitiveModeMask
}

// ParseSensitiveTag parses the sensitive tag value and returns the configuration.
// Supported formats:
//   - sensitive:"true"       -> zero value
//   - sensitive:"redact"     -> "[REDACTED]"
//   - sensitive:"mask:***"   -> custom mask "***"
func ParseSensitiveTag(tagValue string) *SensitiveFieldConfig {
	if tagValue == "" {
		return nil
	}

	tagValue = strings.TrimSpace(tagValue)

	switch {
	case tagValue == "true" || tagValue == "1":
		return &SensitiveFieldConfig{Mode: SensitiveModeZero}
	case tagValue == "redact":
		return &SensitiveFieldConfig{Mode: SensitiveModeRedact}
	case strings.HasPrefix(tagValue, "mask:"):
		mask := strings.TrimPrefix(tagValue, "mask:")

		return &SensitiveFieldConfig{Mode: SensitiveModeMask, Mask: mask}
	default:
		// Default to zero mode for any truthy value
		return &SensitiveFieldConfig{Mode: SensitiveModeZero}
	}
}
