package prompt

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// InputPrompter handles input prompts
type InputPrompter struct {
	reader io.Reader
	writer io.Writer
	config InputConfig
}

// InputConfig contains input prompt configuration
type InputConfig struct {
	DefaultValue   string `yaml:"default_value" json:"default_value"`
	ShowDefault    bool   `yaml:"show_default" json:"show_default"`
	Required       bool   `yaml:"required" json:"required"`
	RetryOnInvalid bool   `yaml:"retry_on_invalid" json:"retry_on_invalid"`
	MaxLength      int    `yaml:"max_length" json:"max_length"`
	MinLength      int    `yaml:"min_length" json:"min_length"`
	Pattern        string `yaml:"pattern" json:"pattern"`
	Mask           bool   `yaml:"mask" json:"mask"`
	MaskChar       string `yaml:"mask_char" json:"mask_char"`
}

// ValidationFunc defines a validation function for input
type ValidationFunc func(string) error

// NewInputPrompter creates a new input prompter
func NewInputPrompter(reader io.Reader, writer io.Writer) *InputPrompter {
	return &InputPrompter{
		reader: reader,
		writer: writer,
		config: DefaultInputConfig(),
	}
}

// NewInputPrompterWithConfig creates an input prompter with custom config
func NewInputPrompterWithConfig(reader io.Reader, writer io.Writer, config InputConfig) *InputPrompter {
	return &InputPrompter{
		reader: reader,
		writer: writer,
		config: config,
	}
}

// Input prompts for input with optional validation
func (ip *InputPrompter) Input(message string, validate ValidationFunc) (string, error) {
	scanner := bufio.NewScanner(ip.reader)

	for {
		// Display prompt
		prompt := message
		if ip.config.ShowDefault && ip.config.DefaultValue != "" {
			prompt += fmt.Sprintf(" [%s]", ip.config.DefaultValue)
		}
		if ip.config.Required {
			prompt += " *"
		}
		prompt += ": "

		fmt.Fprint(ip.writer, prompt)

		// Read input
		if !scanner.Scan() {
			if ip.config.DefaultValue != "" {
				return ip.config.DefaultValue, nil
			}
			return "", fmt.Errorf("no input received")
		}

		input := strings.TrimSpace(scanner.Text())

		// Handle empty input
		if input == "" {
			if ip.config.DefaultValue != "" {
				input = ip.config.DefaultValue
			} else if ip.config.Required {
				if !ip.config.RetryOnInvalid {
					return "", fmt.Errorf("input is required")
				}
				fmt.Fprintln(ip.writer, "Input is required.")
				continue
			} else {
				return "", nil
			}
		}

		// Validate input
		if err := ip.validateInput(input); err != nil {
			if !ip.config.RetryOnInvalid {
				return "", err
			}
			fmt.Fprintf(ip.writer, "Invalid input: %v\n", err)
			continue
		}

		// Custom validation
		if validate != nil {
			if err := validate(input); err != nil {
				if !ip.config.RetryOnInvalid {
					return "", err
				}
				fmt.Fprintf(ip.writer, "Validation failed: %v\n", err)
				continue
			}
		}

		return input, nil
	}
}

// InputWithDefault prompts for input with a specific default
func (ip *InputPrompter) InputWithDefault(message string, defaultValue string, validate ValidationFunc) (string, error) {
	originalDefault := ip.config.DefaultValue
	ip.config.DefaultValue = defaultValue
	defer func() {
		ip.config.DefaultValue = originalDefault
	}()

	return ip.Input(message, validate)
}

// RequiredInput prompts for required input
func (ip *InputPrompter) RequiredInput(message string, validate ValidationFunc) (string, error) {
	originalRequired := ip.config.Required
	ip.config.Required = true
	defer func() {
		ip.config.Required = originalRequired
	}()

	return ip.Input(message, validate)
}

// validateInput validates input against configuration rules
func (ip *InputPrompter) validateInput(input string) error {
	// Length validation
	if ip.config.MinLength > 0 && len(input) < ip.config.MinLength {
		return fmt.Errorf("input must be at least %d characters", ip.config.MinLength)
	}

	if ip.config.MaxLength > 0 && len(input) > ip.config.MaxLength {
		return fmt.Errorf("input must be at most %d characters", ip.config.MaxLength)
	}

	// Pattern validation
	if ip.config.Pattern != "" {
		matched, err := regexp.MatchString(ip.config.Pattern, input)
		if err != nil {
			return fmt.Errorf("pattern validation error: %w", err)
		}
		if !matched {
			return fmt.Errorf("input does not match required pattern")
		}
	}

	return nil
}

// Common validation functions

// EmailValidator validates email addresses
func EmailValidator() ValidationFunc {
	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
	return func(input string) error {
		if !emailRegex.MatchString(input) {
			return fmt.Errorf("invalid email address")
		}
		return nil
	}
}

// URLValidator validates URLs
func URLValidator() ValidationFunc {
	urlRegex := regexp.MustCompile(`^https?://[^\s/$.?#].[^\s]*$`)
	return func(input string) error {
		if !urlRegex.MatchString(input) {
			return fmt.Errorf("invalid URL")
		}
		return nil
	}
}

// NumericValidator validates numeric input
func NumericValidator() ValidationFunc {
	numericRegex := regexp.MustCompile(`^-?\d+(\.\d+)?$`)
	return func(input string) error {
		if !numericRegex.MatchString(input) {
			return fmt.Errorf("input must be numeric")
		}
		return nil
	}
}

// AlphaValidator validates alphabetic input
func AlphaValidator() ValidationFunc {
	alphaRegex := regexp.MustCompile(`^[a-zA-Z]+$`)
	return func(input string) error {
		if !alphaRegex.MatchString(input) {
			return fmt.Errorf("input must contain only letters")
		}
		return nil
	}
}

// AlphanumericValidator validates alphanumeric input
func AlphanumericValidator() ValidationFunc {
	alphanumericRegex := regexp.MustCompile(`^[a-zA-Z0-9]+$`)
	return func(input string) error {
		if !alphanumericRegex.MatchString(input) {
			return fmt.Errorf("input must contain only letters and numbers")
		}
		return nil
	}
}

// LengthValidator validates input length
func LengthValidator(min, max int) ValidationFunc {
	return func(input string) error {
		length := len(input)
		if min > 0 && length < min {
			return fmt.Errorf("input must be at least %d characters", min)
		}
		if max > 0 && length > max {
			return fmt.Errorf("input must be at most %d characters", max)
		}
		return nil
	}
}

// RegexValidator validates input against a regex pattern
func RegexValidator(pattern, message string) ValidationFunc {
	regex := regexp.MustCompile(pattern)
	return func(input string) error {
		if !regex.MatchString(input) {
			if message != "" {
				return errors.New(message)
			}
			return fmt.Errorf("input does not match required pattern")
		}
		return nil
	}
}

// CombineValidators combines multiple validators
func CombineValidators(validators ...ValidationFunc) ValidationFunc {
	return func(input string) error {
		for _, validator := range validators {
			if err := validator(input); err != nil {
				return err
			}
		}
		return nil
	}
}

// DefaultInputConfig returns default input configuration
func DefaultInputConfig() InputConfig {
	return InputConfig{
		DefaultValue:   "",
		ShowDefault:    true,
		Required:       false,
		RetryOnInvalid: true,
		MaxLength:      0,
		MinLength:      0,
		Pattern:        "",
		Mask:           false,
		MaskChar:       "*",
	}
}
