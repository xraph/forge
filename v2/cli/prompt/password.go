package prompt

import (
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// PasswordPrompter handles password prompts
type PasswordPrompter struct {
	reader io.Reader
	writer io.Writer
	config PasswordConfig
}

// PasswordConfig contains password prompt configuration
type PasswordConfig struct {
	MaskChar        string `yaml:"mask_char" json:"mask_char"`
	ShowMask        bool   `yaml:"show_mask" json:"show_mask"`
	MinLength       int    `yaml:"min_length" json:"min_length"`
	MaxLength       int    `yaml:"max_length" json:"max_length"`
	RequireConfirm  bool   `yaml:"require_confirm" json:"require_confirm"`
	RetryOnMismatch bool   `yaml:"retry_on_mismatch" json:"retry_on_mismatch"`
	RetryOnInvalid  bool   `yaml:"retry_on_invalid" json:"retry_on_invalid"`
}

// NewPasswordPrompter creates a new password prompter
func NewPasswordPrompter(reader io.Reader, writer io.Writer) *PasswordPrompter {
	return &PasswordPrompter{
		reader: reader,
		writer: writer,
		config: DefaultPasswordConfig(),
	}
}

// NewPasswordPrompterWithConfig creates a password prompter with custom config
func NewPasswordPrompterWithConfig(reader io.Reader, writer io.Writer, config PasswordConfig) *PasswordPrompter {
	return &PasswordPrompter{
		reader: reader,
		writer: writer,
		config: config,
	}
}

// Password prompts for a password
func (pp *PasswordPrompter) Password(message string) (string, error) {
	for {
		fmt.Fprint(pp.writer, message+": ")

		password, err := pp.readPassword()
		if err != nil {
			return "", err
		}

		fmt.Fprintln(pp.writer) // New line after password input

		// Validate password
		if err := pp.validatePassword(password); err != nil {
			if !pp.config.RetryOnInvalid {
				return "", err
			}
			fmt.Fprintf(pp.writer, "Invalid password: %v\n", err)
			continue
		}

		// Confirm password if required
		if pp.config.RequireConfirm {
			confirmed, err := pp.confirmPassword(password)
			if err != nil {
				return "", err
			}
			if !confirmed {
				if !pp.config.RetryOnMismatch {
					return "", fmt.Errorf("passwords do not match")
				}
				fmt.Fprintln(pp.writer, "Passwords do not match. Please try again.")
				continue
			}
		}

		return password, nil
	}
}

// PasswordWithConfirm prompts for a password with confirmation
func (pp *PasswordPrompter) PasswordWithConfirm(message string) (string, error) {
	originalRequireConfirm := pp.config.RequireConfirm
	pp.config.RequireConfirm = true
	defer func() {
		pp.config.RequireConfirm = originalRequireConfirm
	}()

	return pp.Password(message)
}

// readPassword reads password input without echoing
func (pp *PasswordPrompter) readPassword() (string, error) {
	// Try to read from terminal first
	if file, ok := pp.reader.(*os.File); ok {
		if term.IsTerminal(int(file.Fd())) {
			password, err := term.ReadPassword(int(file.Fd()))
			if err != nil {
				return "", fmt.Errorf("failed to read password: %w", err)
			}
			return string(password), nil
		}
	}

	// Fallback to regular reading (for testing or non-terminal input)
	return pp.readPasswordFallback()
}

// readPasswordFallback reads password when terminal is not available
func (pp *PasswordPrompter) readPasswordFallback() (string, error) {
	var password strings.Builder
	var char [1]byte

	for {
		n, err := pp.reader.Read(char[:])
		if err != nil {
			return "", err
		}
		if n == 0 {
			continue
		}

		// Handle enter key
		if char[0] == '\n' || char[0] == '\r' {
			break
		}

		// Handle backspace
		if char[0] == 8 || char[0] == 127 { // Backspace or DEL
			if password.Len() > 0 {
				// Remove last character
				passwordStr := password.String()
				password.Reset()
				password.WriteString(passwordStr[:len(passwordStr)-1])

				if pp.config.ShowMask {
					fmt.Fprint(pp.writer, "\b \b")
				}
			}
			continue
		}

		// Add character to password
		password.WriteByte(char[0])

		// Show mask character if enabled
		if pp.config.ShowMask {
			fmt.Fprint(pp.writer, pp.config.MaskChar)
		}
	}

	return password.String(), nil
}

// confirmPassword prompts for password confirmation
func (pp *PasswordPrompter) confirmPassword(original string) (bool, error) {
	fmt.Fprint(pp.writer, "Confirm password: ")

	confirmation, err := pp.readPassword()
	if err != nil {
		return false, err
	}

	fmt.Fprintln(pp.writer) // New line after password input

	return original == confirmation, nil
}

// validatePassword validates the password
func (pp *PasswordPrompter) validatePassword(password string) error {
	if pp.config.MinLength > 0 && len(password) < pp.config.MinLength {
		return fmt.Errorf("password must be at least %d characters", pp.config.MinLength)
	}

	if pp.config.MaxLength > 0 && len(password) > pp.config.MaxLength {
		return fmt.Errorf("password must be at most %d characters", pp.config.MaxLength)
	}

	return nil
}

// PasswordStrengthValidator validates password strength
func PasswordStrengthValidator(minLength int, requireUpper, requireLower, requireDigit, requireSpecial bool) ValidationFunc {
	return func(password string) error {
		if len(password) < minLength {
			return fmt.Errorf("password must be at least %d characters", minLength)
		}

		hasUpper := false
		hasLower := false
		hasDigit := false
		hasSpecial := false

		for _, char := range password {
			switch {
			case char >= 'A' && char <= 'Z':
				hasUpper = true
			case char >= 'a' && char <= 'z':
				hasLower = true
			case char >= '0' && char <= '9':
				hasDigit = true
			case strings.ContainsRune("!@#$%^&*()_+-=[]{}|;:,.<>?", char):
				hasSpecial = true
			}
		}

		if requireUpper && !hasUpper {
			return fmt.Errorf("password must contain at least one uppercase letter")
		}

		if requireLower && !hasLower {
			return fmt.Errorf("password must contain at least one lowercase letter")
		}

		if requireDigit && !hasDigit {
			return fmt.Errorf("password must contain at least one digit")
		}

		if requireSpecial && !hasSpecial {
			return fmt.Errorf("password must contain at least one special character")
		}

		return nil
	}
}

// DefaultPasswordConfig returns default password configuration
func DefaultPasswordConfig() PasswordConfig {
	return PasswordConfig{
		MaskChar:        "*",
		ShowMask:        false,
		MinLength:       0,
		MaxLength:       0,
		RequireConfirm:  false,
		RetryOnMismatch: true,
		RetryOnInvalid:  true,
	}
}

// SecurePasswordConfig returns configuration for secure passwords
func SecurePasswordConfig() PasswordConfig {
	return PasswordConfig{
		MaskChar:        "*",
		ShowMask:        false,
		MinLength:       8,
		MaxLength:       128,
		RequireConfirm:  true,
		RetryOnMismatch: true,
		RetryOnInvalid:  true,
	}
}
