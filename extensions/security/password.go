package security

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

// Password hashing errors.
var (
	ErrInvalidHash         = errors.New("invalid password hash format")
	ErrIncompatibleVersion = errors.New("incompatible argon2 version")
	ErrPasswordTooLong     = errors.New("password exceeds maximum length")
)

const (
	// MaxPasswordLength is the maximum password length to prevent DoS attacks.
	MaxPasswordLength = 72 // bcrypt limitation
)

// PasswordHasherConfig holds password hashing configuration.
type PasswordHasherConfig struct {
	// Algorithm specifies the hashing algorithm ("argon2id", "bcrypt")
	// Default: "argon2id" (recommended)
	Algorithm string

	// Argon2 parameters
	Argon2Memory      uint32 // Memory in KiB (default: 64MB = 65536)
	Argon2Iterations  uint32 // Number of iterations (default: 3)
	Argon2Parallelism uint8  // Degree of parallelism (default: 4)
	Argon2SaltLength  uint32 // Salt length in bytes (default: 16)
	Argon2KeyLength   uint32 // Derived key length (default: 32)

	// Bcrypt parameters
	BcryptCost int // Cost factor 4-31 (default: 12)
}

// DefaultPasswordHasherConfig returns the default password hasher configuration.
func DefaultPasswordHasherConfig() PasswordHasherConfig {
	return PasswordHasherConfig{
		Algorithm:         "argon2id",
		Argon2Memory:      64 * 1024, // 64 MB
		Argon2Iterations:  3,
		Argon2Parallelism: 4,
		Argon2SaltLength:  16,
		Argon2KeyLength:   32,
		BcryptCost:        12,
	}
}

// SecurePasswordHasherConfig returns a more secure configuration
// Use this for high-security applications (slower but more secure).
func SecurePasswordHasherConfig() PasswordHasherConfig {
	return PasswordHasherConfig{
		Algorithm:         "argon2id",
		Argon2Memory:      256 * 1024, // 256 MB
		Argon2Iterations:  4,
		Argon2Parallelism: 8,
		Argon2SaltLength:  16,
		Argon2KeyLength:   32,
		BcryptCost:        14,
	}
}

// PasswordHasher handles password hashing and verification.
type PasswordHasher struct {
	config PasswordHasherConfig
}

// NewPasswordHasher creates a new password hasher.
func NewPasswordHasher(config PasswordHasherConfig) *PasswordHasher {
	// Set defaults
	if config.Algorithm == "" {
		config.Algorithm = "argon2id"
	}

	if config.Argon2Memory == 0 {
		config.Argon2Memory = 64 * 1024
	}

	if config.Argon2Iterations == 0 {
		config.Argon2Iterations = 3
	}

	if config.Argon2Parallelism == 0 {
		config.Argon2Parallelism = 4
	}

	if config.Argon2SaltLength == 0 {
		config.Argon2SaltLength = 16
	}

	if config.Argon2KeyLength == 0 {
		config.Argon2KeyLength = 32
	}

	if config.BcryptCost == 0 {
		config.BcryptCost = 12
	}

	return &PasswordHasher{
		config: config,
	}
}

// Hash creates a hash of the password using the configured algorithm.
func (ph *PasswordHasher) Hash(password string) (string, error) {
	if len(password) > MaxPasswordLength {
		return "", ErrPasswordTooLong
	}

	switch ph.config.Algorithm {
	case "argon2id":
		return ph.hashArgon2ID(password)
	case "bcrypt":
		return ph.hashBcrypt(password)
	default:
		return "", fmt.Errorf("unsupported algorithm: %s", ph.config.Algorithm)
	}
}

// Verify checks if the password matches the hash.
func (ph *PasswordHasher) Verify(password, hash string) (bool, error) {
	if len(password) > MaxPasswordLength {
		return false, ErrPasswordTooLong
	}

	// Detect algorithm from hash format
	if strings.HasPrefix(hash, "$argon2id$") {
		return ph.verifyArgon2ID(password, hash)
	} else if strings.HasPrefix(hash, "$2a$") || strings.HasPrefix(hash, "$2b$") || strings.HasPrefix(hash, "$2y$") {
		return ph.verifyBcrypt(password, hash)
	}

	return false, ErrInvalidHash
}

// NeedsRehash checks if the hash needs to be rehashed with current parameters
// This is useful when upgrading hashing parameters.
func (ph *PasswordHasher) NeedsRehash(hash string) (bool, error) {
	if strings.HasPrefix(hash, "$argon2id$") {
		params, _, _, err := ph.decodeArgon2IDHash(hash)
		if err != nil {
			return false, err
		}

		// Check if any parameters are different
		return params.Memory != ph.config.Argon2Memory ||
			params.Iterations != ph.config.Argon2Iterations ||
			params.Parallelism != ph.config.Argon2Parallelism ||
			params.KeyLength != ph.config.Argon2KeyLength, nil
	} else if strings.HasPrefix(hash, "$2a$") || strings.HasPrefix(hash, "$2b$") || strings.HasPrefix(hash, "$2y$") {
		cost, err := bcrypt.Cost([]byte(hash))
		if err != nil {
			return false, err
		}

		return cost != ph.config.BcryptCost, nil
	}

	return false, ErrInvalidHash
}

// hashArgon2ID creates an Argon2id hash.
func (ph *PasswordHasher) hashArgon2ID(password string) (string, error) {
	// Generate random salt
	salt := make([]byte, ph.config.Argon2SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("failed to generate salt: %w", err)
	}

	// Generate hash
	hash := argon2.IDKey(
		[]byte(password),
		salt,
		ph.config.Argon2Iterations,
		ph.config.Argon2Memory,
		ph.config.Argon2Parallelism,
		ph.config.Argon2KeyLength,
	)

	// Encode to string: $argon2id$v=19$m=65536,t=3,p=4$salt$hash
	encodedSalt := base64.RawStdEncoding.EncodeToString(salt)
	encodedHash := base64.RawStdEncoding.EncodeToString(hash)

	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		ph.config.Argon2Memory,
		ph.config.Argon2Iterations,
		ph.config.Argon2Parallelism,
		encodedSalt,
		encodedHash,
	), nil
}

// argon2Params holds Argon2 parameters.
type argon2Params struct {
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

// decodeArgon2IDHash decodes an Argon2id hash string.
func (ph *PasswordHasher) decodeArgon2IDHash(encodedHash string) (*argon2Params, []byte, []byte, error) {
	// Format: $argon2id$v=19$m=65536,t=3,p=4$salt$hash
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 {
		return nil, nil, nil, ErrInvalidHash
	}

	// Check algorithm
	if parts[1] != "argon2id" {
		return nil, nil, nil, ErrInvalidHash
	}

	// Parse version
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return nil, nil, nil, ErrInvalidHash
	}

	if version != argon2.Version {
		return nil, nil, nil, ErrIncompatibleVersion
	}

	// Parse parameters
	params := &argon2Params{}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &params.Memory, &params.Iterations, &params.Parallelism); err != nil {
		return nil, nil, nil, ErrInvalidHash
	}

	// Decode salt
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return nil, nil, nil, ErrInvalidHash
	}

	params.SaltLength = uint32(len(salt))

	// Decode hash
	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return nil, nil, nil, ErrInvalidHash
	}

	params.KeyLength = uint32(len(hash))

	return params, salt, hash, nil
}

// verifyArgon2ID verifies a password against an Argon2id hash.
func (ph *PasswordHasher) verifyArgon2ID(password, encodedHash string) (bool, error) {
	params, salt, hash, err := ph.decodeArgon2IDHash(encodedHash)
	if err != nil {
		return false, err
	}

	// Generate hash with same parameters
	otherHash := argon2.IDKey(
		[]byte(password),
		salt,
		params.Iterations,
		params.Memory,
		params.Parallelism,
		params.KeyLength,
	)

	// Use constant-time comparison to prevent timing attacks
	if subtle.ConstantTimeCompare(hash, otherHash) == 1 {
		return true, nil
	}

	return false, nil
}

// hashBcrypt creates a bcrypt hash.
func (ph *PasswordHasher) hashBcrypt(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), ph.config.BcryptCost)
	if err != nil {
		return "", fmt.Errorf("failed to generate bcrypt hash: %w", err)
	}

	return string(hash), nil
}

// verifyBcrypt verifies a password against a bcrypt hash.
func (ph *PasswordHasher) verifyBcrypt(password, hash string) (bool, error) {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	if err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return false, nil
		}

		return false, err
	}

	return true, nil
}

// GenerateRandomPassword generates a cryptographically secure random password.
func GenerateRandomPassword(length int) (string, error) {
	if length <= 0 {
		length = 16
	}

	// Use a character set that's safe for most systems
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*()-_=+[]{}|;:,.<>?"

	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random password: %w", err)
	}

	for i, b := range bytes {
		bytes[i] = charset[b%byte(len(charset))]
	}

	return string(bytes), nil
}

// PasswordStrength represents password strength levels.
type PasswordStrength int

const (
	PasswordVeryWeak PasswordStrength = iota
	PasswordWeak
	PasswordFair
	PasswordStrong
	PasswordVeryStrong
)

// String returns the string representation of password strength.
func (ps PasswordStrength) String() string {
	return [...]string{"very weak", "weak", "fair", "strong", "very strong"}[ps]
}

// CheckPasswordStrength evaluates password strength.
func CheckPasswordStrength(password string) PasswordStrength {
	length := len(password)
	hasLower := false
	hasUpper := false
	hasDigit := false
	hasSpecial := false

	for _, char := range password {
		switch {
		case char >= 'a' && char <= 'z':
			hasLower = true
		case char >= 'A' && char <= 'Z':
			hasUpper = true
		case char >= '0' && char <= '9':
			hasDigit = true
		default:
			hasSpecial = true
		}
	}

	// Calculate score
	score := 0
	if length >= 8 {
		score++
	}

	if length >= 12 {
		score++
	}

	if length >= 16 {
		score++
	}

	if hasLower {
		score++
	}

	if hasUpper {
		score++
	}

	if hasDigit {
		score++
	}

	if hasSpecial {
		score++
	}

	// Map score to strength level
	switch {
	case score <= 2:
		return PasswordVeryWeak
	case score <= 4:
		return PasswordWeak
	case score <= 5:
		return PasswordFair
	case score <= 6:
		return PasswordStrong
	default:
		return PasswordVeryStrong
	}
}

// ValidatePassword validates a password against common requirements.
func ValidatePassword(password string, minLength, maxLength int, requireUpper, requireLower, requireDigit, requireSpecial bool) error {
	if len(password) < minLength {
		return fmt.Errorf("password must be at least %d characters long", minLength)
	}

	if maxLength > 0 && len(password) > maxLength {
		return fmt.Errorf("password must be at most %d characters long", maxLength)
	}

	hasUpper := false
	hasLower := false
	hasDigit := false
	hasSpecial := false

	for _, char := range password {
		switch {
		case char >= 'a' && char <= 'z':
			hasLower = true
		case char >= 'A' && char <= 'Z':
			hasUpper = true
		case char >= '0' && char <= '9':
			hasDigit = true
		default:
			hasSpecial = true
		}
	}

	if requireUpper && !hasUpper {
		return errors.New("password must contain at least one uppercase letter")
	}

	if requireLower && !hasLower {
		return errors.New("password must contain at least one lowercase letter")
	}

	if requireDigit && !hasDigit {
		return errors.New("password must contain at least one digit")
	}

	if requireSpecial && !hasSpecial {
		return errors.New("password must contain at least one special character")
	}

	return nil
}
