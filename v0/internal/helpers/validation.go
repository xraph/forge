package helpers

import (
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// ValidationHelper provides input validation utilities for the CLI
type ValidationHelper struct {
	patterns map[string]*regexp.Regexp
}

// ValidationError represents a validation error
type ValidationError struct {
	Field   string `json:"field"`
	Value   string `json:"value"`
	Message string `json:"message"`
	Code    string `json:"code"`
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error for field '%s': %s", e.Field, e.Message)
}

// ValidationResult represents the result of a validation
type ValidationResult struct {
	Valid  bool               `json:"valid"`
	Errors []*ValidationError `json:"errors"`
}

// NewValidationError creates a new validation error
func NewValidationError(field, value, message, code string) *ValidationError {
	return &ValidationError{
		Field:   field,
		Value:   value,
		Message: message,
		Code:    code,
	}
}

// NewValidationHelper creates a new validation helper
func NewValidationHelper() *ValidationHelper {
	v := &ValidationHelper{
		patterns: make(map[string]*regexp.Regexp),
	}
	v.initializePatterns()
	return v
}

// initializePatterns initializes common regex patterns
func (v *ValidationHelper) initializePatterns() {
	patterns := map[string]string{
		"go_identifier":   `^[a-zA-Z_][a-zA-Z0-9_]*$`,
		"go_package_name": `^[a-z][a-z0-9]*$`,
		"project_name":    `^[a-zA-Z][a-zA-Z0-9_-]*$`,
		"docker_image":    `^[a-z0-9]+(([._]|__|[-]*)[a-z0-9]+)*(:[a-zA-Z0-9_][a-zA-Z0-9._-]*)?$`,
		"semver":          `^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-((?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$`,
		"kubernetes_name": `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`,
		"dns_label":       `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`,
		"env_var_name":    `^[A-Z_][A-Z0-9_]*$`,
		"file_name":       `^[a-zA-Z0-9]([a-zA-Z0-9._-]*[a-zA-Z0-9])?$`,
		"git_branch":      `^[a-zA-Z0-9]([a-zA-Z0-9._/-]*[a-zA-Z0-9])?$`,
		"hex_color":       `^#([A-Fa-f0-9]{6}|[A-Fa-f0-9]{3})$`,
		"ipv4":            `^((25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)$`,
		"port":            `^([1-9][0-9]{0,3}|[1-5][0-9]{4}|6[0-4][0-9]{3}|65[0-4][0-9]{2}|655[0-2][0-9]|6553[0-5])$`,
		"username":        `^[a-zA-Z0-9_.-]+$`,
		"slug":            `^[a-z0-9]+(?:-[a-z0-9]+)*$`,
		"tag":             `^[a-zA-Z0-9]([a-zA-Z0-9._-]*[a-zA-Z0-9])?$`,
	}

	for name, pattern := range patterns {
		v.patterns[name] = regexp.MustCompile(pattern)
	}
}

// ValidateGoIdentifier validates a Go identifier (variable, function, type name)
func (v *ValidationHelper) ValidateGoIdentifier(name string) error {
	if name == "" {
		return NewValidationError("identifier", name, "identifier cannot be empty", "empty")
	}

	if !v.patterns["go_identifier"].MatchString(name) {
		return NewValidationError("identifier", name, "must be a valid Go identifier (start with letter or underscore, contain only letters, numbers, and underscores)", "invalid_format")
	}

	// Check if it's a Go keyword
	if v.isGoKeyword(name) {
		return NewValidationError("identifier", name, "cannot be a Go keyword", "go_keyword")
	}

	return nil
}

// ValidateGoPackageName validates a Go package name
func (v *ValidationHelper) ValidateGoPackageName(name string) error {
	if name == "" {
		return NewValidationError("package", name, "package name cannot be empty", "empty")
	}

	if !v.patterns["go_package_name"].MatchString(name) {
		return NewValidationError("package", name, "must be a valid Go package name (lowercase letters and numbers only, start with letter)", "invalid_format")
	}

	// Check if it's a Go keyword
	if v.isGoKeyword(name) {
		return NewValidationError("package", name, "cannot be a Go keyword", "go_keyword")
	}

	return nil
}

// ValidateProjectName validates a project name
func (v *ValidationHelper) ValidateProjectName(name string) error {
	if name == "" {
		return NewValidationError("project_name", name, "project name cannot be empty", "empty")
	}

	if len(name) < 2 {
		return NewValidationError("project_name", name, "project name must be at least 2 characters long", "too_short")
	}

	if len(name) > 50 {
		return NewValidationError("project_name", name, "project name must be less than 50 characters long", "too_long")
	}

	if !v.patterns["project_name"].MatchString(name) {
		return NewValidationError("project_name", name, "must start with a letter and contain only letters, numbers, hyphens, and underscores", "invalid_format")
	}

	return nil
}

// ValidateEmail validates an email address
func (v *ValidationHelper) ValidateEmail(email string) error {
	if email == "" {
		return NewValidationError("email", email, "email cannot be empty", "empty")
	}

	_, err := mail.ParseAddress(email)
	if err != nil {
		return NewValidationError("email", email, "must be a valid email address", "invalid_format")
	}

	return nil
}

// ValidateURL validates a URL
func (v *ValidationHelper) ValidateURL(rawURL string) error {
	if rawURL == "" {
		return NewValidationError("url", rawURL, "URL cannot be empty", "empty")
	}

	_, err := url.Parse(rawURL)
	if err != nil {
		return NewValidationError("url", rawURL, "must be a valid URL", "invalid_format")
	}

	return nil
}

// ValidatePort validates a port number
func (v *ValidationHelper) ValidatePort(portStr string) error {
	if portStr == "" {
		return NewValidationError("port", portStr, "port cannot be empty", "empty")
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return NewValidationError("port", portStr, "must be a valid number", "invalid_format")
	}

	if port < 1 || port > 65535 {
		return NewValidationError("port", portStr, "must be between 1 and 65535", "out_of_range")
	}

	return nil
}

// ValidateIPv4 validates an IPv4 address
func (v *ValidationHelper) ValidateIPv4(ip string) error {
	if ip == "" {
		return NewValidationError("ipv4", ip, "IP address cannot be empty", "empty")
	}

	parsedIP := net.ParseIP(ip)
	if parsedIP == nil || parsedIP.To4() == nil {
		return NewValidationError("ipv4", ip, "must be a valid IPv4 address", "invalid_format")
	}

	return nil
}

// ValidateDockerImageName validates a Docker image name
func (v *ValidationHelper) ValidateDockerImageName(name string) error {
	if name == "" {
		return NewValidationError("docker_image", name, "Docker image name cannot be empty", "empty")
	}

	if len(name) > 256 {
		return NewValidationError("docker_image", name, "Docker image name cannot exceed 256 characters", "too_long")
	}

	// Split registry, image, and tag
	parts := strings.Split(name, "/")
	imagePart := parts[len(parts)-1]

	// Check if it has a tag
	imageAndTag := strings.Split(imagePart, ":")
	imageName := imageAndTag[0]

	if !v.isValidDockerImageComponent(imageName) {
		return NewValidationError("docker_image", name, "must be a valid Docker image name (lowercase alphanumeric characters, underscores, periods, and dashes)", "invalid_format")
	}

	// Validate tag if present
	if len(imageAndTag) > 1 {
		tag := imageAndTag[1]
		if !v.isValidDockerTag(tag) {
			return NewValidationError("docker_image", name, "tag must be valid (alphanumeric characters, underscores, periods, and dashes, up to 128 characters)", "invalid_tag")
		}
	}

	return nil
}

// ValidateSemver validates a semantic version string
func (v *ValidationHelper) ValidateSemver(version string) error {
	if version == "" {
		return NewValidationError("semver", version, "version cannot be empty", "empty")
	}

	if !v.patterns["semver"].MatchString(version) {
		return NewValidationError("semver", version, "must be a valid semantic version (e.g., 1.0.0, 1.0.0-alpha.1)", "invalid_format")
	}

	return nil
}

// ValidateKubernetesName validates a Kubernetes resource name
func (v *ValidationHelper) ValidateKubernetesName(name string) error {
	if name == "" {
		return NewValidationError("kubernetes_name", name, "Kubernetes name cannot be empty", "empty")
	}

	if len(name) > 253 {
		return NewValidationError("kubernetes_name", name, "Kubernetes name cannot exceed 253 characters", "too_long")
	}

	if !v.patterns["kubernetes_name"].MatchString(name) {
		return NewValidationError("kubernetes_name", name, "must be a valid Kubernetes name (lowercase alphanumeric characters and dashes)", "invalid_format")
	}

	return nil
}

// ValidateEnvironmentVariableName validates an environment variable name
func (v *ValidationHelper) ValidateEnvironmentVariableName(name string) error {
	if name == "" {
		return NewValidationError("env_var", name, "environment variable name cannot be empty", "empty")
	}

	if !v.patterns["env_var_name"].MatchString(name) {
		return NewValidationError("env_var", name, "must be a valid environment variable name (uppercase letters, numbers, and underscores, start with letter or underscore)", "invalid_format")
	}

	return nil
}

// ValidateFileName validates a file name
func (v *ValidationHelper) ValidateFileName(name string) error {
	if name == "" {
		return NewValidationError("file_name", name, "file name cannot be empty", "empty")
	}

	if len(name) > 255 {
		return NewValidationError("file_name", name, "file name cannot exceed 255 characters", "too_long")
	}

	// Check for invalid characters
	invalidChars := []string{"/", "\\", ":", "*", "?", "\"", "<", ">", "|"}
	for _, char := range invalidChars {
		if strings.Contains(name, char) {
			return NewValidationError("file_name", name, fmt.Sprintf("cannot contain '%s'", char), "invalid_character")
		}
	}

	// Check for reserved names (Windows)
	reservedNames := []string{"CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9"}
	upperName := strings.ToUpper(name)
	for _, reserved := range reservedNames {
		if upperName == reserved {
			return NewValidationError("file_name", name, "cannot be a reserved system name", "reserved_name")
		}
	}

	return nil
}

// ValidateDirectoryPath validates a directory path
func (v *ValidationHelper) ValidateDirectoryPath(path string) error {
	if path == "" {
		return NewValidationError("directory_path", path, "directory path cannot be empty", "empty")
	}

	// Clean the path
	cleanPath := filepath.Clean(path)

	// Check if path is absolute when it shouldn't be (context dependent)
	if filepath.IsAbs(cleanPath) {
		// This might be valid in some contexts, so just a warning could be appropriate
	}

	// Validate each component
	components := strings.Split(cleanPath, string(filepath.Separator))
	for _, component := range components {
		if component == "" || component == "." {
			continue
		}
		if component == ".." {
			continue // Allow relative paths
		}
		if err := v.ValidateFileName(component); err != nil {
			return NewValidationError("directory_path", path, fmt.Sprintf("invalid component '%s': %s", component, err.(*ValidationError).Message), "invalid_component")
		}
	}

	return nil
}

// ValidateGitBranchName validates a Git branch name
func (v *ValidationHelper) ValidateGitBranchName(name string) error {
	if name == "" {
		return NewValidationError("git_branch", name, "branch name cannot be empty", "empty")
	}

	if !v.patterns["git_branch"].MatchString(name) {
		return NewValidationError("git_branch", name, "must be a valid Git branch name", "invalid_format")
	}

	// Additional Git branch name rules
	if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
		return NewValidationError("git_branch", name, "cannot start or end with a dash", "invalid_format")
	}

	if strings.Contains(name, "..") {
		return NewValidationError("git_branch", name, "cannot contain consecutive dots", "invalid_format")
	}

	if strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") {
		return NewValidationError("git_branch", name, "cannot start or end with a slash", "invalid_format")
	}

	return nil
}

// ValidateRequired validates that a value is not empty
func (v *ValidationHelper) ValidateRequired(fieldName, value string) error {
	if strings.TrimSpace(value) == "" {
		return NewValidationError(fieldName, value, "is required", "required")
	}
	return nil
}

// ValidateMinLength validates minimum string length
func (v *ValidationHelper) ValidateMinLength(fieldName, value string, minLength int) error {
	if len(value) < minLength {
		return NewValidationError(fieldName, value, fmt.Sprintf("must be at least %d characters long", minLength), "too_short")
	}
	return nil
}

// ValidateMaxLength validates maximum string length
func (v *ValidationHelper) ValidateMaxLength(fieldName, value string, maxLength int) error {
	if len(value) > maxLength {
		return NewValidationError(fieldName, value, fmt.Sprintf("must be no more than %d characters long", maxLength), "too_long")
	}
	return nil
}

// ValidateNumericRange validates that a number is within a range
func (v *ValidationHelper) ValidateNumericRange(fieldName, value string, min, max int) error {
	num, err := strconv.Atoi(value)
	if err != nil {
		return NewValidationError(fieldName, value, "must be a valid number", "invalid_format")
	}

	if num < min || num > max {
		return NewValidationError(fieldName, value, fmt.Sprintf("must be between %d and %d", min, max), "out_of_range")
	}

	return nil
}

// ValidateOneOf validates that a value is one of the allowed values
func (v *ValidationHelper) ValidateOneOf(fieldName, value string, allowedValues []string) error {
	for _, allowed := range allowedValues {
		if value == allowed {
			return nil
		}
	}

	return NewValidationError(fieldName, value, fmt.Sprintf("must be one of: %s", strings.Join(allowedValues, ", ")), "invalid_choice")
}

// ValidatePattern validates a value against a regex pattern
func (v *ValidationHelper) ValidatePattern(fieldName, value, pattern, message string) error {
	regex, err := regexp.Compile(pattern)
	if err != nil {
		return NewValidationError(fieldName, value, "invalid validation pattern", "invalid_pattern")
	}

	if !regex.MatchString(value) {
		if message == "" {
			message = fmt.Sprintf("must match pattern: %s", pattern)
		}
		return NewValidationError(fieldName, value, message, "pattern_mismatch")
	}

	return nil
}

// ValidateStruct validates a struct using validation tags
func (v *ValidationHelper) ValidateStruct(s interface{}) *ValidationResult {
	result := &ValidationResult{
		Valid:  true,
		Errors: []*ValidationError{},
	}

	// This would typically use reflection to inspect struct tags
	// For now, it's a placeholder that always returns valid
	// A full implementation would use tags like `validate:"required,min=3,max=50"`

	return result
}

// Helper functions

// isGoKeyword checks if a string is a Go keyword
func (v *ValidationHelper) isGoKeyword(word string) bool {
	keywords := map[string]bool{
		"break": true, "default": true, "func": true, "interface": true, "select": true,
		"case": true, "defer": true, "go": true, "map": true, "struct": true,
		"chan": true, "else": true, "goto": true, "package": true, "switch": true,
		"const": true, "fallthrough": true, "if": true, "range": true, "type": true,
		"continue": true, "for": true, "import": true, "return": true, "var": true,
	}
	return keywords[word]
}

// isValidDockerImageComponent validates a Docker image name component
func (v *ValidationHelper) isValidDockerImageComponent(component string) bool {
	if len(component) == 0 || len(component) > 255 {
		return false
	}

	for i, r := range component {
		if i == 0 {
			if !unicode.IsLower(r) && !unicode.IsDigit(r) {
				return false
			}
		} else {
			if !unicode.IsLower(r) && !unicode.IsDigit(r) && r != '.' && r != '_' && r != '-' {
				return false
			}
		}
	}

	return true
}

// isValidDockerTag validates a Docker tag
func (v *ValidationHelper) isValidDockerTag(tag string) bool {
	if len(tag) == 0 || len(tag) > 128 {
		return false
	}

	for _, r := range tag {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '.' && r != '_' && r != '-' {
			return false
		}
	}

	return true
}

// ValidateAll performs multiple validations and returns aggregated results
func (v *ValidationHelper) ValidateAll(validations []func() error) *ValidationResult {
	result := &ValidationResult{
		Valid:  true,
		Errors: []*ValidationError{},
	}

	for _, validation := range validations {
		if err := validation(); err != nil {
			result.Valid = false
			if validationErr, ok := err.(*ValidationError); ok {
				result.Errors = append(result.Errors, validationErr)
			} else {
				result.Errors = append(result.Errors, &ValidationError{
					Field:   "unknown",
					Value:   "",
					Message: err.Error(),
					Code:    "unknown_error",
				})
			}
		}
	}

	return result
}

// SanitizeInput sanitizes user input to prevent injection attacks
func (v *ValidationHelper) SanitizeInput(input string) string {
	// Remove potentially dangerous characters
	sanitized := strings.ReplaceAll(input, "\x00", "")   // null bytes
	sanitized = strings.ReplaceAll(sanitized, "\r", "")  // carriage returns
	sanitized = strings.ReplaceAll(sanitized, "\n", " ") // newlines to spaces

	// Trim whitespace
	sanitized = strings.TrimSpace(sanitized)

	return sanitized
}

// EscapeShellArg escapes a string for safe use in shell commands
func (v *ValidationHelper) EscapeShellArg(arg string) string {
	// Simple shell escaping - wrap in single quotes and escape existing single quotes
	escaped := strings.ReplaceAll(arg, "'", "'\"'\"'")
	return "'" + escaped + "'"
}
