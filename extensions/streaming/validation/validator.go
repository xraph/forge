package validation

import (
	"context"
	"fmt"

	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// MessageValidator validates messages before acceptance.
type MessageValidator interface {
	// Validate checks message before acceptance
	Validate(ctx context.Context, msg *streaming.Message, sender streaming.EnhancedConnection) error

	// ValidateContent validates message content
	ValidateContent(content any) error

	// ValidateMetadata validates metadata
	ValidateMetadata(metadata map[string]any) error
}

// CompositeValidator chains multiple validators.
type CompositeValidator struct {
	validators []MessageValidator
}

// NewCompositeValidator creates a composite validator.
func NewCompositeValidator(validators ...MessageValidator) *CompositeValidator {
	return &CompositeValidator{
		validators: validators,
	}
}

// Validate runs all validators.
func (cv *CompositeValidator) Validate(ctx context.Context, msg *streaming.Message, sender streaming.EnhancedConnection) error {
	for _, validator := range cv.validators {
		if err := validator.Validate(ctx, msg, sender); err != nil {
			return err
		}
	}

	return nil
}

// ValidateContent validates content with all validators.
func (cv *CompositeValidator) ValidateContent(content any) error {
	for _, validator := range cv.validators {
		if err := validator.ValidateContent(content); err != nil {
			return err
		}
	}

	return nil
}

// ValidateMetadata validates metadata with all validators.
func (cv *CompositeValidator) ValidateMetadata(metadata map[string]any) error {
	for _, validator := range cv.validators {
		if err := validator.ValidateMetadata(metadata); err != nil {
			return err
		}
	}

	return nil
}

// Add adds a validator to the chain.
func (cv *CompositeValidator) Add(validator MessageValidator) {
	cv.validators = append(cv.validators, validator)
}

// ValidationError represents a validation failure.
type ValidationError struct {
	Field   string
	Message string
	Code    string
}

func (ve *ValidationError) Error() string {
	if ve.Field != "" {
		return fmt.Sprintf("validation error for field '%s': %s (code: %s)", ve.Field, ve.Message, ve.Code)
	}

	return fmt.Sprintf("validation error: %s (code: %s)", ve.Message, ve.Code)
}

// NewValidationError creates a validation error.
func NewValidationError(field, message, code string) error {
	return &ValidationError{
		Field:   field,
		Message: message,
		Code:    code,
	}
}
