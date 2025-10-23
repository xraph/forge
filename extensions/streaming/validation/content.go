package validation

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// ContentValidatorConfig configures content validation.
type ContentValidatorConfig struct {
	// Length checks
	MaxContentLength int
	MinContentLength int

	// Required fields
	RequireUserID bool
	RequireRoomID bool
	RequireType   bool

	// Format validation
	ValidateURLs   bool
	ValidateEmails bool

	// Custom validators
	CustomValidators map[string]ContentValidatorFunc
}

// ContentValidatorFunc is a custom validation function.
type ContentValidatorFunc func(content any) error

// ContentValidator validates message content.
type ContentValidator struct {
	config     ContentValidatorConfig
	emailRegex *regexp.Regexp
}

// NewContentValidator creates a content validator.
func NewContentValidator(config ContentValidatorConfig) *ContentValidator {
	return &ContentValidator{
		config:     config,
		emailRegex: regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`),
	}
}

// Validate validates the message.
func (cv *ContentValidator) Validate(ctx context.Context, msg *streaming.Message, sender streaming.EnhancedConnection) error {
	// Check required fields
	if cv.config.RequireUserID && msg.UserID == "" {
		return NewValidationError("user_id", "user_id is required", "REQUIRED_FIELD")
	}

	if cv.config.RequireRoomID && msg.RoomID == "" {
		return NewValidationError("room_id", "room_id is required", "REQUIRED_FIELD")
	}

	if cv.config.RequireType && msg.Type == "" {
		return NewValidationError("type", "type is required", "REQUIRED_FIELD")
	}

	// Validate content
	if err := cv.ValidateContent(msg.Data); err != nil {
		return err
	}

	// Validate metadata
	if err := cv.ValidateMetadata(msg.Metadata); err != nil {
		return err
	}

	return nil
}

// ValidateContent validates content data.
func (cv *ContentValidator) ValidateContent(content any) error {
	if content == nil {
		return NewValidationError("content", "content cannot be nil", "CONTENT_REQUIRED")
	}

	// String content validation
	if str, ok := content.(string); ok {
		if cv.config.MinContentLength > 0 && len(str) < cv.config.MinContentLength {
			return NewValidationError("content", fmt.Sprintf("content too short (min: %d)", cv.config.MinContentLength), "LENGTH_MIN")
		}

		if cv.config.MaxContentLength > 0 && len(str) > cv.config.MaxContentLength {
			return NewValidationError("content", fmt.Sprintf("content too long (max: %d)", cv.config.MaxContentLength), "LENGTH_MAX")
		}

		// Validate URLs if enabled
		if cv.config.ValidateURLs {
			if err := cv.validateURLsInText(str); err != nil {
				return err
			}
		}

		// Validate emails if enabled
		if cv.config.ValidateEmails {
			if err := cv.validateEmailsInText(str); err != nil {
				return err
			}
		}
	}

	// Run custom validators
	for name, validator := range cv.config.CustomValidators {
		if err := validator(content); err != nil {
			return fmt.Errorf("custom validator '%s' failed: %w", name, err)
		}
	}

	return nil
}

// ValidateMetadata validates metadata.
func (cv *ContentValidator) ValidateMetadata(metadata map[string]any) error {
	// Basic metadata validation
	// Add any metadata-specific rules here
	return nil
}

func (cv *ContentValidator) validateURLsInText(text string) error {
	// Extract URLs
	urlRegex := regexp.MustCompile(`https?://[^\s]+`)
	urls := urlRegex.FindAllString(text, -1)

	for _, urlStr := range urls {
		if _, err := url.ParseRequestURI(urlStr); err != nil {
			return NewValidationError("content", fmt.Sprintf("invalid URL: %s", urlStr), "INVALID_URL")
		}
	}

	return nil
}

func (cv *ContentValidator) validateEmailsInText(text string) error {
	// Extract potential emails
	words := strings.Fields(text)
	for _, word := range words {
		if strings.Contains(word, "@") {
			if !cv.emailRegex.MatchString(word) {
				return NewValidationError("content", fmt.Sprintf("invalid email: %s", word), "INVALID_EMAIL")
			}
		}
	}

	return nil
}
