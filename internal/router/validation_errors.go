package router

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ValidationError represents a single field validation error.
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
	Value   any    `json:"value,omitempty"`
	Code    string `json:"code,omitempty"`
}

// ValidationErrors is a collection of validation errors.
type ValidationErrors struct {
	Errors []ValidationError `json:"errors"`
}

// Error implements the error interface.
func (ve *ValidationErrors) Error() string {
	if len(ve.Errors) == 0 {
		return "validation failed"
	}

	var messages []string
	for _, err := range ve.Errors {
		messages = append(messages, fmt.Sprintf("%s: %s", err.Field, err.Message))
	}

	return strings.Join(messages, "; ")
}

// Add adds a validation error.
func (ve *ValidationErrors) Add(field, message string, value any) {
	ve.Errors = append(ve.Errors, ValidationError{
		Field:   field,
		Message: message,
		Value:   value,
	})
}

// AddWithCode adds a validation error with a code.
func (ve *ValidationErrors) AddWithCode(field, message, code string, value any) {
	ve.Errors = append(ve.Errors, ValidationError{
		Field:   field,
		Message: message,
		Value:   value,
		Code:    code,
	})
}

// HasErrors returns true if there are validation errors.
func (ve *ValidationErrors) HasErrors() bool {
	return len(ve.Errors) > 0
}

// Count returns the number of validation errors.
func (ve *ValidationErrors) Count() int {
	return len(ve.Errors)
}

// ToJSON converts validation errors to JSON bytes.
func (ve *ValidationErrors) ToJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"error":            "Validation failed",
		"validationErrors": ve.Errors,
	})
}

// NewValidationErrors creates a new ValidationErrors instance.
func NewValidationErrors() *ValidationErrors {
	return &ValidationErrors{
		Errors: make([]ValidationError, 0),
	}
}

// ValidationErrorResponse is the HTTP response for validation errors.
type ValidationErrorResponse struct {
	Error            string            `json:"error"`
	Code             int               `json:"code"`
	ValidationErrors []ValidationError `json:"validationErrors"`
}

// NewValidationErrorResponse creates a new validation error response.
func NewValidationErrorResponse(errors *ValidationErrors) *ValidationErrorResponse {
	return &ValidationErrorResponse{
		Error:            "Validation failed",
		Code:             422,
		ValidationErrors: errors.Errors,
	}
}

// Common validation error codes.
const (
	ErrCodeRequired      = "REQUIRED"
	ErrCodeInvalidType   = "INVALID_TYPE"
	ErrCodeInvalidFormat = "INVALID_FORMAT"
	ErrCodeMinLength     = "MIN_LENGTH"
	ErrCodeMaxLength     = "MAX_LENGTH"
	ErrCodeMinValue      = "MIN_VALUE"
	ErrCodeMaxValue      = "MAX_VALUE"
	ErrCodePattern       = "PATTERN"
	ErrCodeEnum          = "ENUM"
	ErrCodeMinItems      = "MIN_ITEMS"
	ErrCodeMaxItems      = "MAX_ITEMS"
	ErrCodeUniqueItems   = "UNIQUE_ITEMS"
)
