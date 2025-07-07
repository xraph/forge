package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ValidationMiddleware provides request validation functionality
type ValidationMiddleware struct {
	*BaseMiddleware
	config ValidationConfig
}

// ValidationConfig represents validation configuration
type ValidationConfig struct {
	// Body validation
	MaxBodySize         int64    `json:"max_body_size"`
	RequiredHeaders     []string `json:"required_headers"`
	RequiredParams      []string `json:"required_params"`
	AllowedMethods      []string `json:"allowed_methods"`
	AllowedContentTypes []string `json:"allowed_content_types"`

	// JSON validation
	ValidateJSON bool   `json:"validate_json"`
	JSONSchema   string `json:"json_schema"`
	JSONMaxDepth int    `json:"json_max_depth"`
	JSONMaxKeys  int    `json:"json_max_keys"`

	// Path validation
	PathValidators map[string]PathValidator `json:"-"`

	// Parameter validation
	ParamValidators map[string]ParamValidator `json:"-"`

	// Header validation
	HeaderValidators map[string]HeaderValidator `json:"-"`

	// Custom validators
	CustomValidators []CustomValidator `json:"-"`

	// Error handling
	ErrorHandler     ValidationErrorHandler `json:"-"`
	StopOnFirstError bool                   `json:"stop_on_first_error"`

	// Skip validation
	SkipPaths   []string `json:"skip_paths"`
	SkipMethods []string `json:"skip_methods"`

	// Sanitization
	SanitizeInput   bool `json:"sanitize_input"`
	SanitizeHeaders bool `json:"sanitize_headers"`
	SanitizeParams  bool `json:"sanitize_params"`

	// Rate limiting based on validation
	RateLimitInvalid   bool          `json:"rate_limit_invalid"`
	InvalidWindow      time.Duration `json:"invalid_window"`
	MaxInvalidRequests int           `json:"max_invalid_requests"`
}

// Validator interfaces

// PathValidator validates URL paths
type PathValidator interface {
	ValidatePath(path string) error
}

// ParamValidator validates query parameters
type ParamValidator interface {
	ValidateParam(name, value string) error
}

// HeaderValidator validates HTTP headers
type HeaderValidator interface {
	ValidateHeader(name, value string) error
}

// CustomValidator provides custom validation logic
type CustomValidator interface {
	ValidateRequest(r *http.Request) error
	Name() string
}

// ValidationErrorHandler handles validation errors
type ValidationErrorHandler interface {
	HandleValidationError(w http.ResponseWriter, r *http.Request, errors []ValidationError)
}

// ValidationError represents a validation error
type ValidationError struct {
	Field   string `json:"field"`
	Value   string `json:"value"`
	Message string `json:"message"`
	Code    string `json:"code"`
}

// ValidationResult represents validation results
type ValidationResult struct {
	Valid  bool              `json:"valid"`
	Errors []ValidationError `json:"errors"`
}

// NewValidationMiddleware creates a new validation middleware
func NewValidationMiddleware(config ValidationConfig) Middleware {
	return &ValidationMiddleware{
		BaseMiddleware: NewBaseMiddleware(
			"validation",
			PriorityApplication-10, // Before application logic
			"Request validation middleware",
		),
		config: config,
	}
}

// DefaultValidationConfig returns default validation configuration
func DefaultValidationConfig() ValidationConfig {
	return ValidationConfig{
		MaxBodySize:         10 * 1024 * 1024, // 10MB
		RequiredHeaders:     []string{},
		RequiredParams:      []string{},
		AllowedMethods:      []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"},
		AllowedContentTypes: []string{"application/json", "application/x-www-form-urlencoded", "multipart/form-data"},
		ValidateJSON:        true,
		JSONMaxDepth:        10,
		JSONMaxKeys:         100,
		PathValidators:      make(map[string]PathValidator),
		ParamValidators:     make(map[string]ParamValidator),
		HeaderValidators:    make(map[string]HeaderValidator),
		CustomValidators:    []CustomValidator{},
		StopOnFirstError:    false,
		SkipPaths:           []string{"/health", "/metrics"},
		SkipMethods:         []string{"OPTIONS"},
		SanitizeInput:       true,
		SanitizeHeaders:     true,
		SanitizeParams:      true,
		RateLimitInvalid:    false,
		InvalidWindow:       time.Minute,
		MaxInvalidRequests:  10,
	}
}

// StrictValidationConfig returns strict validation configuration
func StrictValidationConfig() ValidationConfig {
	config := DefaultValidationConfig()
	config.MaxBodySize = 1024 * 1024 // 1MB
	config.JSONMaxDepth = 5
	config.JSONMaxKeys = 50
	config.StopOnFirstError = true
	config.RateLimitInvalid = true
	config.MaxInvalidRequests = 5
	return config
}

// Handle implements the Middleware interface
func (vm *ValidationMiddleware) Handle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip validation for certain paths or methods
		if vm.shouldSkipValidation(r) {
			next.ServeHTTP(w, r)
			return
		}

		// Validate request
		result := vm.validateRequest(r)
		if !result.Valid {
			vm.handleValidationErrors(w, r, result.Errors)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// shouldSkipValidation checks if validation should be skipped
func (vm *ValidationMiddleware) shouldSkipValidation(r *http.Request) bool {
	// Check skip paths
	for _, skipPath := range vm.config.SkipPaths {
		if strings.HasPrefix(r.URL.Path, skipPath) {
			return true
		}
	}

	// Check skip methods
	for _, skipMethod := range vm.config.SkipMethods {
		if r.Method == skipMethod {
			return true
		}
	}

	return false
}

// validateRequest validates the entire request
func (vm *ValidationMiddleware) validateRequest(r *http.Request) ValidationResult {
	var errors []ValidationError

	// Validate method
	if err := vm.validateMethod(r); err != nil {
		errors = append(errors, ValidationError{
			Field:   "method",
			Value:   r.Method,
			Message: err.Error(),
			Code:    "INVALID_METHOD",
		})
		if vm.config.StopOnFirstError {
			return ValidationResult{Valid: false, Errors: errors}
		}
	}

	// Validate headers
	if headerErrors := vm.validateHeaders(r); len(headerErrors) > 0 {
		errors = append(errors, headerErrors...)
		if vm.config.StopOnFirstError {
			return ValidationResult{Valid: false, Errors: errors}
		}
	}

	// Validate parameters
	if paramErrors := vm.validateParameters(r); len(paramErrors) > 0 {
		errors = append(errors, paramErrors...)
		if vm.config.StopOnFirstError {
			return ValidationResult{Valid: false, Errors: errors}
		}
	}

	// Validate body
	if bodyErrors := vm.validateBody(r); len(bodyErrors) > 0 {
		errors = append(errors, bodyErrors...)
		if vm.config.StopOnFirstError {
			return ValidationResult{Valid: false, Errors: errors}
		}
	}

	// Validate path
	if pathErrors := vm.validatePath(r); len(pathErrors) > 0 {
		errors = append(errors, pathErrors...)
		if vm.config.StopOnFirstError {
			return ValidationResult{Valid: false, Errors: errors}
		}
	}

	// Run custom validators
	if customErrors := vm.runCustomValidators(r); len(customErrors) > 0 {
		errors = append(errors, customErrors...)
		if vm.config.StopOnFirstError {
			return ValidationResult{Valid: false, Errors: errors}
		}
	}

	return ValidationResult{
		Valid:  len(errors) == 0,
		Errors: errors,
	}
}

// validateMethod validates HTTP method
func (vm *ValidationMiddleware) validateMethod(r *http.Request) error {
	if len(vm.config.AllowedMethods) == 0 {
		return nil
	}

	for _, method := range vm.config.AllowedMethods {
		if r.Method == method {
			return nil
		}
	}

	return fmt.Errorf("method %s not allowed", r.Method)
}

// validateHeaders validates HTTP headers
func (vm *ValidationMiddleware) validateHeaders(r *http.Request) []ValidationError {
	var errors []ValidationError

	// Check required headers
	for _, header := range vm.config.RequiredHeaders {
		if r.Header.Get(header) == "" {
			errors = append(errors, ValidationError{
				Field:   "header:" + header,
				Value:   "",
				Message: fmt.Sprintf("header %s is required", header),
				Code:    "MISSING_HEADER",
			})
		}
	}

	// Validate content type
	if len(vm.config.AllowedContentTypes) > 0 {
		contentType := r.Header.Get("Content-Type")
		if contentType != "" {
			// Remove charset from content type
			if idx := strings.Index(contentType, ";"); idx != -1 {
				contentType = contentType[:idx]
			}
			contentType = strings.TrimSpace(contentType)

			allowed := false
			for _, allowedType := range vm.config.AllowedContentTypes {
				if contentType == allowedType {
					allowed = true
					break
				}
			}

			if !allowed {
				errors = append(errors, ValidationError{
					Field:   "header:Content-Type",
					Value:   contentType,
					Message: fmt.Sprintf("content type %s not allowed", contentType),
					Code:    "INVALID_CONTENT_TYPE",
				})
			}
		}
	}

	// Run header validators
	for headerName, validator := range vm.config.HeaderValidators {
		if value := r.Header.Get(headerName); value != "" {
			if err := validator.ValidateHeader(headerName, value); err != nil {
				errors = append(errors, ValidationError{
					Field:   "header:" + headerName,
					Value:   value,
					Message: err.Error(),
					Code:    "INVALID_HEADER",
				})
			}
		}
	}

	return errors
}

// validateParameters validates query parameters
func (vm *ValidationMiddleware) validateParameters(r *http.Request) []ValidationError {
	var errors []ValidationError

	// Check required parameters
	for _, param := range vm.config.RequiredParams {
		if r.URL.Query().Get(param) == "" {
			errors = append(errors, ValidationError{
				Field:   "param:" + param,
				Value:   "",
				Message: fmt.Sprintf("parameter %s is required", param),
				Code:    "MISSING_PARAM",
			})
		}
	}

	// Run parameter validators
	for paramName, validator := range vm.config.ParamValidators {
		if value := r.URL.Query().Get(paramName); value != "" {
			if err := validator.ValidateParam(paramName, value); err != nil {
				errors = append(errors, ValidationError{
					Field:   "param:" + paramName,
					Value:   value,
					Message: err.Error(),
					Code:    "INVALID_PARAM",
				})
			}
		}
	}

	return errors
}

// validateBody validates request body
func (vm *ValidationMiddleware) validateBody(r *http.Request) []ValidationError {
	var errors []ValidationError

	// Check body size
	if r.ContentLength > vm.config.MaxBodySize {
		errors = append(errors, ValidationError{
			Field:   "body",
			Value:   fmt.Sprintf("%d", r.ContentLength),
			Message: fmt.Sprintf("body size %d exceeds maximum %d", r.ContentLength, vm.config.MaxBodySize),
			Code:    "BODY_TOO_LARGE",
		})
		return errors
	}

	// Validate JSON if enabled
	if vm.config.ValidateJSON {
		contentType := r.Header.Get("Content-Type")
		if strings.Contains(contentType, "application/json") {
			if jsonErrors := vm.validateJSON(r); len(jsonErrors) > 0 {
				errors = append(errors, jsonErrors...)
			}
		}
	}

	return errors
}

// validateJSON validates JSON body
func (vm *ValidationMiddleware) validateJSON(r *http.Request) []ValidationError {
	var errors []ValidationError

	// Read body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		errors = append(errors, ValidationError{
			Field:   "body",
			Value:   "",
			Message: "failed to read request body",
			Code:    "BODY_READ_ERROR",
		})
		return errors
	}

	// Restore body for subsequent handlers
	r.Body = io.NopCloser(bytes.NewBuffer(body))

	// Parse JSON
	var jsonData interface{}
	if err := json.Unmarshal(body, &jsonData); err != nil {
		errors = append(errors, ValidationError{
			Field:   "body",
			Value:   string(body),
			Message: "invalid JSON: " + err.Error(),
			Code:    "INVALID_JSON",
		})
		return errors
	}

	// Validate JSON depth
	if vm.config.JSONMaxDepth > 0 {
		if depth := vm.getJSONDepth(jsonData); depth > vm.config.JSONMaxDepth {
			errors = append(errors, ValidationError{
				Field:   "body",
				Value:   fmt.Sprintf("%d", depth),
				Message: fmt.Sprintf("JSON depth %d exceeds maximum %d", depth, vm.config.JSONMaxDepth),
				Code:    "JSON_TOO_DEEP",
			})
		}
	}

	// Validate JSON key count
	if vm.config.JSONMaxKeys > 0 {
		if keyCount := vm.getJSONKeyCount(jsonData); keyCount > vm.config.JSONMaxKeys {
			errors = append(errors, ValidationError{
				Field:   "body",
				Value:   fmt.Sprintf("%d", keyCount),
				Message: fmt.Sprintf("JSON key count %d exceeds maximum %d", keyCount, vm.config.JSONMaxKeys),
				Code:    "JSON_TOO_MANY_KEYS",
			})
		}
	}

	return errors
}

// validatePath validates URL path
func (vm *ValidationMiddleware) validatePath(r *http.Request) []ValidationError {
	var errors []ValidationError

	// Run path validators
	for pattern, validator := range vm.config.PathValidators {
		if matched, _ := regexp.MatchString(pattern, r.URL.Path); matched {
			if err := validator.ValidatePath(r.URL.Path); err != nil {
				errors = append(errors, ValidationError{
					Field:   "path",
					Value:   r.URL.Path,
					Message: err.Error(),
					Code:    "INVALID_PATH",
				})
			}
		}
	}

	return errors
}

// runCustomValidators runs custom validators
func (vm *ValidationMiddleware) runCustomValidators(r *http.Request) []ValidationError {
	var errors []ValidationError

	for _, validator := range vm.config.CustomValidators {
		if err := validator.ValidateRequest(r); err != nil {
			errors = append(errors, ValidationError{
				Field:   validator.Name(),
				Value:   "",
				Message: err.Error(),
				Code:    "CUSTOM_VALIDATION_ERROR",
			})
		}
	}

	return errors
}

// getJSONDepth calculates JSON nesting depth
func (vm *ValidationMiddleware) getJSONDepth(data interface{}) int {
	switch v := data.(type) {
	case map[string]interface{}:
		maxDepth := 0
		for _, value := range v {
			if depth := vm.getJSONDepth(value); depth > maxDepth {
				maxDepth = depth
			}
		}
		return maxDepth + 1
	case []interface{}:
		maxDepth := 0
		for _, value := range v {
			if depth := vm.getJSONDepth(value); depth > maxDepth {
				maxDepth = depth
			}
		}
		return maxDepth + 1
	default:
		return 1
	}
}

// getJSONKeyCount counts JSON keys
func (vm *ValidationMiddleware) getJSONKeyCount(data interface{}) int {
	switch v := data.(type) {
	case map[string]interface{}:
		count := len(v)
		for _, value := range v {
			count += vm.getJSONKeyCount(value)
		}
		return count
	case []interface{}:
		count := 0
		for _, value := range v {
			count += vm.getJSONKeyCount(value)
		}
		return count
	default:
		return 0
	}
}

// handleValidationErrors handles validation errors
func (vm *ValidationMiddleware) handleValidationErrors(w http.ResponseWriter, r *http.Request, errors []ValidationError) {
	if vm.config.ErrorHandler != nil {
		vm.config.ErrorHandler.HandleValidationError(w, r, errors)
		return
	}

	// Default error response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)

	response := map[string]interface{}{
		"error":   "Validation failed",
		"message": "Request validation failed",
		"code":    "VALIDATION_ERROR",
		"errors":  errors,
	}

	json.NewEncoder(w).Encode(response)
}

// Configure implements the Middleware interface
func (vm *ValidationMiddleware) Configure(config map[string]interface{}) error {
	if maxBodySize, ok := config["max_body_size"].(int64); ok {
		vm.config.MaxBodySize = maxBodySize
	}
	if requiredHeaders, ok := config["required_headers"].([]string); ok {
		vm.config.RequiredHeaders = requiredHeaders
	}
	if requiredParams, ok := config["required_params"].([]string); ok {
		vm.config.RequiredParams = requiredParams
	}
	if allowedMethods, ok := config["allowed_methods"].([]string); ok {
		vm.config.AllowedMethods = allowedMethods
	}
	if allowedContentTypes, ok := config["allowed_content_types"].([]string); ok {
		vm.config.AllowedContentTypes = allowedContentTypes
	}
	if validateJSON, ok := config["validate_json"].(bool); ok {
		vm.config.ValidateJSON = validateJSON
	}
	if jsonMaxDepth, ok := config["json_max_depth"].(int); ok {
		vm.config.JSONMaxDepth = jsonMaxDepth
	}
	if jsonMaxKeys, ok := config["json_max_keys"].(int); ok {
		vm.config.JSONMaxKeys = jsonMaxKeys
	}
	if stopOnFirstError, ok := config["stop_on_first_error"].(bool); ok {
		vm.config.StopOnFirstError = stopOnFirstError
	}
	if skipPaths, ok := config["skip_paths"].([]string); ok {
		vm.config.SkipPaths = skipPaths
	}
	if skipMethods, ok := config["skip_methods"].([]string); ok {
		vm.config.SkipMethods = skipMethods
	}
	if sanitizeInput, ok := config["sanitize_input"].(bool); ok {
		vm.config.SanitizeInput = sanitizeInput
	}

	return nil
}

// Health implements the Middleware interface
func (vm *ValidationMiddleware) Health(ctx context.Context) error {
	if vm.config.MaxBodySize <= 0 {
		return fmt.Errorf("max body size must be positive")
	}
	return nil
}

// Built-in validators

// EmailValidator validates email addresses
type EmailValidator struct{}

func (v *EmailValidator) ValidateParam(name, value string) error {
	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	if !emailRegex.MatchString(value) {
		return fmt.Errorf("invalid email format")
	}
	return nil
}

// URLValidator validates URLs
type URLValidator struct{}

func (v *URLValidator) ValidateParam(name, value string) error {
	_, err := url.ParseRequestURI(value)
	if err != nil {
		return fmt.Errorf("invalid URL format")
	}
	return nil
}

// IntegerValidator validates integers
type IntegerValidator struct {
	Min int
	Max int
}

func (v *IntegerValidator) ValidateParam(name, value string) error {
	intValue, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("invalid integer format")
	}

	if v.Min != 0 && intValue < v.Min {
		return fmt.Errorf("value %d is less than minimum %d", intValue, v.Min)
	}

	if v.Max != 0 && intValue > v.Max {
		return fmt.Errorf("value %d is greater than maximum %d", intValue, v.Max)
	}

	return nil
}

// StringLengthValidator validates string length
type StringLengthValidator struct {
	MinLength int
	MaxLength int
}

func (v *StringLengthValidator) ValidateParam(name, value string) error {
	length := len(value)

	if v.MinLength > 0 && length < v.MinLength {
		return fmt.Errorf("string length %d is less than minimum %d", length, v.MinLength)
	}

	if v.MaxLength > 0 && length > v.MaxLength {
		return fmt.Errorf("string length %d is greater than maximum %d", length, v.MaxLength)
	}

	return nil
}

// RegexValidator validates using regular expressions
type RegexValidator struct {
	Pattern string
	regex   *regexp.Regexp
}

func NewRegexValidator(pattern string) *RegexValidator {
	return &RegexValidator{
		Pattern: pattern,
		regex:   regexp.MustCompile(pattern),
	}
}

func (v *RegexValidator) ValidateParam(name, value string) error {
	if !v.regex.MatchString(value) {
		return fmt.Errorf("value does not match pattern %s", v.Pattern)
	}
	return nil
}

func (v *RegexValidator) ValidateHeader(name, value string) error {
	return v.ValidateParam(name, value)
}

func (v *RegexValidator) ValidatePath(path string) error {
	if !v.regex.MatchString(path) {
		return fmt.Errorf("path does not match pattern %s", v.Pattern)
	}
	return nil
}

// JSONSchemaValidator validates JSON against a schema
type JSONSchemaValidator struct {
	Schema string
}

func (v *JSONSchemaValidator) ValidateRequest(r *http.Request) error {
	// Implementation would use a JSON schema validation library
	// This is a placeholder
	return nil
}

func (v *JSONSchemaValidator) Name() string {
	return "json_schema"
}

// IPValidator validates IP addresses
type IPValidator struct{}

func (v *IPValidator) ValidateParam(name, value string) error {
	// Simple IP validation
	parts := strings.Split(value, ".")
	if len(parts) != 4 {
		return fmt.Errorf("invalid IP address format")
	}

	for _, part := range parts {
		num, err := strconv.Atoi(part)
		if err != nil || num < 0 || num > 255 {
			return fmt.Errorf("invalid IP address format")
		}
	}

	return nil
}

// Utility functions

// ValidationMiddlewareFunc creates a simple validation middleware function
func ValidationMiddlewareFunc(config ValidationConfig) func(http.Handler) http.Handler {
	middleware := NewValidationMiddleware(config)
	return middleware.Handle
}

// WithValidation creates validation middleware with default configuration
func WithValidation() func(http.Handler) http.Handler {
	return ValidationMiddlewareFunc(DefaultValidationConfig())
}

// WithStrictValidation creates validation middleware with strict configuration
func WithStrictValidation() func(http.Handler) http.Handler {
	return ValidationMiddlewareFunc(StrictValidationConfig())
}

// WithJSONValidation creates validation middleware with JSON validation
func WithJSONValidation(maxDepth, maxKeys int) func(http.Handler) http.Handler {
	config := DefaultValidationConfig()
	config.ValidateJSON = true
	config.JSONMaxDepth = maxDepth
	config.JSONMaxKeys = maxKeys
	return ValidationMiddlewareFunc(config)
}

// WithRequiredHeaders creates validation middleware with required headers
func WithRequiredHeaders(headers []string) func(http.Handler) http.Handler {
	config := DefaultValidationConfig()
	config.RequiredHeaders = headers
	return ValidationMiddlewareFunc(config)
}

// WithRequiredParams creates validation middleware with required parameters
func WithRequiredParams(params []string) func(http.Handler) http.Handler {
	config := DefaultValidationConfig()
	config.RequiredParams = params
	return ValidationMiddlewareFunc(config)
}

// WithMaxBodySize creates validation middleware with body size limit
func WithMaxBodySize(maxSize int64) func(http.Handler) http.Handler {
	config := DefaultValidationConfig()
	config.MaxBodySize = maxSize
	return ValidationMiddlewareFunc(config)
}

// Default error handler

// DefaultValidationErrorHandler provides default error handling
type DefaultValidationErrorHandler struct{}

func (h *DefaultValidationErrorHandler) HandleValidationError(w http.ResponseWriter, r *http.Request, errors []ValidationError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)

	response := map[string]interface{}{
		"error":   "Validation failed",
		"message": "Request validation failed",
		"code":    "VALIDATION_ERROR",
		"errors":  errors,
		"count":   len(errors),
	}

	json.NewEncoder(w).Encode(response)
}

// Validation testing utilities

// TestValidationMiddleware provides utilities for testing validation
type TestValidationMiddleware struct {
	*ValidationMiddleware
}

// NewTestValidationMiddleware creates a test validation middleware
func NewTestValidationMiddleware(config ValidationConfig) *TestValidationMiddleware {
	return &TestValidationMiddleware{
		ValidationMiddleware: NewValidationMiddleware(config).(*ValidationMiddleware),
	}
}

// TestValidation tests validation with specific request
func (tvm *TestValidationMiddleware) TestValidation(r *http.Request) ValidationResult {
	return tvm.validateRequest(r)
}

// Validation utilities

// SanitizeString sanitizes a string by removing dangerous characters
func SanitizeString(input string) string {
	// Remove null bytes
	input = strings.ReplaceAll(input, "\x00", "")

	// Remove control characters
	var result strings.Builder
	for _, r := range input {
		if r >= 32 && r != 127 {
			result.WriteRune(r)
		}
	}

	return result.String()
}

// ValidateEmail validates email format
func ValidateEmail(email string) bool {
	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	return emailRegex.MatchString(email)
}

// ValidateURL validates URL format
func ValidateURL(urlStr string) bool {
	_, err := url.ParseRequestURI(urlStr)
	return err == nil
}

// ValidateIPAddress validates IP address format
func ValidateIPAddress(ip string) bool {
	parts := strings.Split(ip, ".")
	if len(parts) != 4 {
		return false
	}

	for _, part := range parts {
		num, err := strconv.Atoi(part)
		if err != nil || num < 0 || num > 255 {
			return false
		}
	}

	return true
}

// ValidatePhoneNumber validates phone number format
func ValidatePhoneNumber(phone string) bool {
	// Simple phone number validation
	phoneRegex := regexp.MustCompile(`^\+?[\d\s\-\(\)]+$`)
	return phoneRegex.MatchString(phone) && len(phone) >= 10
}

// ValidatePassword validates password strength
func ValidatePassword(password string) []string {
	var errors []string

	if len(password) < 8 {
		errors = append(errors, "password must be at least 8 characters long")
	}

	if !regexp.MustCompile(`[a-z]`).MatchString(password) {
		errors = append(errors, "password must contain at least one lowercase letter")
	}

	if !regexp.MustCompile(`[A-Z]`).MatchString(password) {
		errors = append(errors, "password must contain at least one uppercase letter")
	}

	if !regexp.MustCompile(`\d`).MatchString(password) {
		errors = append(errors, "password must contain at least one digit")
	}

	if !regexp.MustCompile(`[!@#$%^&*(),.?":{}|<>]`).MatchString(password) {
		errors = append(errors, "password must contain at least one special character")
	}

	return errors
}

// ValidateJSONString validates JSON string format
func ValidateJSONString(jsonStr string) error {
	var js interface{}
	return json.Unmarshal([]byte(jsonStr), &js)
}

// ValidateUUID validates UUID format
func ValidateUUID(uuid string) bool {
	uuidRegex := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	return uuidRegex.MatchString(uuid)
}

// ValidateISO8601Date validates ISO 8601 date format
func ValidateISO8601Date(date string) bool {
	_, err := time.Parse(time.RFC3339, date)
	return err == nil
}

// ValidateBase64 validates base64 encoding
func ValidateBase64(encoded string) bool {
	// Remove padding for validation
	trimmed := strings.TrimRight(encoded, "=")

	// Check valid base64 characters
	base64Regex := regexp.MustCompile(`^[A-Za-z0-9+/]*$`)
	return base64Regex.MatchString(trimmed)
}
