package storage

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// PathValidator validates storage paths for security.
type PathValidator struct {
	allowedPatterns   []*regexp.Regexp
	forbiddenPatterns []*regexp.Regexp
	maxKeyLength      int
}

// NewPathValidator creates a new path validator with default rules.
func NewPathValidator() *PathValidator {
	return &PathValidator{
		allowedPatterns: []*regexp.Regexp{
			// Allow alphanumeric, hyphens, underscores, slashes, and dots
			regexp.MustCompile(`^[a-zA-Z0-9/_.-]+$`),
		},
		forbiddenPatterns: []*regexp.Regexp{
			// Prevent path traversal
			regexp.MustCompile(`\.\.`),
			// Prevent absolute paths
			regexp.MustCompile(`^/`),
			// Prevent multiple consecutive slashes
			regexp.MustCompile(`//+`),
			// Prevent Windows drive letters
			regexp.MustCompile(`^[a-zA-Z]:`),
			// Prevent null bytes
			regexp.MustCompile(`\x00`),
		},
		maxKeyLength: 1024,
	}
}

// ValidateKey validates a storage key for security issues.
func (pv *PathValidator) ValidateKey(key string) error {
	// Check empty key
	if key == "" {
		return fmt.Errorf("%w: empty key", ErrInvalidKey)
	}

	// Check length
	if len(key) > pv.maxKeyLength {
		return fmt.Errorf("%w: key exceeds maximum length of %d", ErrInvalidKey, pv.maxKeyLength)
	}

	// Check forbidden patterns
	for _, pattern := range pv.forbiddenPatterns {
		if pattern.MatchString(key) {
			return fmt.Errorf("%w: key matches forbidden pattern %s", ErrInvalidPath, pattern.String())
		}
	}

	// Check allowed patterns
	matched := false

	for _, pattern := range pv.allowedPatterns {
		if pattern.MatchString(key) {
			matched = true

			break
		}
	}

	if !matched {
		return fmt.Errorf("%w: key contains invalid characters", ErrInvalidKey)
	}

	// Additional checks
	// Allow system/internal keys starting with dot (e.g., .health_check)
	// but still prevent dangerous patterns like ../
	if strings.HasPrefix(key, ".") && !strings.HasPrefix(key, ".health") {
		return fmt.Errorf("%w: key cannot start with dot", ErrInvalidKey)
	}

	if strings.HasSuffix(key, "/") {
		return fmt.Errorf("%w: key cannot end with slash", ErrInvalidKey)
	}

	// Clean path and ensure it hasn't changed (no . or .. components)
	cleaned := filepath.Clean(key)
	if cleaned != key && cleaned != filepath.ToSlash(key) {
		return fmt.Errorf("%w: key normalization changed path", ErrInvalidPath)
	}

	return nil
}

// ValidateContentType validates content type.
func (pv *PathValidator) ValidateContentType(contentType string) error {
	if contentType == "" {
		return nil
	}

	// Basic validation - should match pattern type/subtype
	if !regexp.MustCompile(`^[a-zA-Z0-9][\w.-]*/[a-zA-Z0-9][\w.-]*(?:\+[a-zA-Z0-9][\w.-]*)?(?:;.*)?$`).MatchString(contentType) {
		return fmt.Errorf("%w: %s", ErrInvalidContentType, contentType)
	}

	return nil
}

// SanitizeKey sanitizes a key to make it safe.
func (pv *PathValidator) SanitizeKey(key string) string {
	// Convert to forward slashes
	key = filepath.ToSlash(key)

	// Remove leading/trailing slashes and spaces
	key = strings.Trim(key, "/ \t\n\r")

	// Replace multiple slashes with single slash
	re := regexp.MustCompile(`/+`)
	key = re.ReplaceAllString(key, "/")

	// Remove any .. components
	parts := strings.Split(key, "/")

	var cleaned []string

	for _, part := range parts {
		if part != "" && part != "." && part != ".." {
			cleaned = append(cleaned, part)
		}
	}

	return strings.Join(cleaned, "/")
}

// IsValidSize checks if a file size is within limits.
func IsValidSize(size int64, maxSize int64) bool {
	return size > 0 && size <= maxSize
}

// ValidateMetadata validates metadata keys and values.
func ValidateMetadata(metadata map[string]string) error {
	const (
		maxMetadataKeys = 100
		maxKeyLength    = 256
		maxValueLength  = 4096
	)

	if len(metadata) > maxMetadataKeys {
		return fmt.Errorf("metadata exceeds maximum of %d keys", maxMetadataKeys)
	}

	for key, value := range metadata {
		if len(key) > maxKeyLength {
			return fmt.Errorf("metadata key '%s' exceeds maximum length of %d", key, maxKeyLength)
		}

		if len(value) > maxValueLength {
			return fmt.Errorf("metadata value for key '%s' exceeds maximum length of %d", key, maxValueLength)
		}

		// Key should only contain alphanumeric, hyphen, underscore
		if !regexp.MustCompile(`^[a-zA-Z0-9_-]+$`).MatchString(key) {
			return fmt.Errorf("metadata key '%s' contains invalid characters", key)
		}
	}

	return nil
}
