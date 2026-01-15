package router

import (
	"github.com/xraph/forge/internal/shared"
)

// ValidationError represents a single validation error for backward compatibility.
type ValidationError = shared.ValidationError
type ValidationErrors = shared.ValidationErrors
type ValidationErrorResponse = shared.ValidationErrorResponse

// Re-export validation functions.
var (
	NewValidationErrors        = shared.NewValidationErrors
	NewValidationErrorResponse = shared.NewValidationErrorResponse
)

// Re-export validation error codes.
const (
	ErrCodeRequired      = shared.ErrCodeRequired
	ErrCodeInvalidType   = shared.ErrCodeInvalidType
	ErrCodeInvalidFormat = shared.ErrCodeInvalidFormat
	ErrCodeMinLength     = shared.ErrCodeMinLength
	ErrCodeMaxLength     = shared.ErrCodeMaxLength
	ErrCodeMinValue      = shared.ErrCodeMinValue
	ErrCodeMaxValue      = shared.ErrCodeMaxValue
	ErrCodePattern       = shared.ErrCodePattern
	ErrCodeEnum          = shared.ErrCodeEnum
	ErrCodeMinItems      = shared.ErrCodeMinItems
	ErrCodeMaxItems      = shared.ErrCodeMaxItems
	ErrCodeUniqueItems   = shared.ErrCodeUniqueItems
)
