package errors

import (
	"fmt"
)

// Example_errorCodes demonstrates using error code constants
func Example_errorCodes() {
	// Create errors using constructors (they use the constants internally)
	err := ErrServiceNotFound("database")

	// You can access the code constant for custom logic
	var forgeErr *ForgeError
	if As(err, &forgeErr) {
		switch forgeErr.Code {
		case CodeServiceNotFound:
			fmt.Println("Service not found")
		case CodeServiceAlreadyExists:
			fmt.Println("Service already exists")
		case CodeCircularDependency:
			fmt.Println("Circular dependency")
		}
	}

	// Using sentinel errors for comparison
	if Is(err, ErrServiceNotFoundSentinel) {
		fmt.Println("Matched using sentinel")
	}

	// Using helper functions
	if IsServiceNotFound(err) {
		fmt.Println("Matched using helper")
	}

	// Output:
	// Service not found
	// Matched using sentinel
	// Matched using helper
}

// Example_errorConstants demonstrates the available error code constants
func Example_errorConstants() {
	// All available error code constants:
	codes := []string{
		CodeConfigError,          // "CONFIG_ERROR"
		CodeValidationError,      // "VALIDATION_ERROR"
		CodeLifecycleError,       // "LIFECYCLE_ERROR"
		CodeContextCancelled,     // "CONTEXT_CANCELLED"
		CodeServiceNotFound,      // "SERVICE_NOT_FOUND"
		CodeServiceAlreadyExists, // "SERVICE_ALREADY_EXISTS"
		CodeCircularDependency,   // "CIRCULAR_DEPENDENCY"
		CodeInvalidConfig,        // "INVALID_CONFIG"
		CodeTimeoutError,         // "TIMEOUT_ERROR"
	}

	for _, code := range codes {
		fmt.Printf("Code: %s\n", code)
	}

	// Output:
	// Code: CONFIG_ERROR
	// Code: VALIDATION_ERROR
	// Code: LIFECYCLE_ERROR
	// Code: CONTEXT_CANCELLED
	// Code: SERVICE_NOT_FOUND
	// Code: SERVICE_ALREADY_EXISTS
	// Code: CIRCULAR_DEPENDENCY
	// Code: INVALID_CONFIG
	// Code: TIMEOUT_ERROR
}

// Example_forgeErrorWithConstants shows creating custom ForgeErrors with constants
func Example_forgeErrorWithConstants() {
	// When creating custom ForgeErrors, use the constants
	customErr := &ForgeError{
		Code:    CodeValidationError,
		Message: "custom validation failed",
	}

	// Add context
	customErr.WithContext("field", "email").WithContext("rule", "format")

	// Check error type
	if Is(customErr, ErrValidationErrorSentinel) {
		fmt.Println("This is a validation error")
	}

	// Output:
	// This is a validation error
}

// Example_errorMatching demonstrates various error matching patterns
func Example_errorMatching() {
	// Create a wrapped error chain
	baseErr := ErrServiceNotFound("auth")
	wrappedErr := ErrConfigError("failed to initialize", baseErr)

	// Match by code using Is with sentinel
	if Is(wrappedErr, ErrServiceNotFoundSentinel) {
		fmt.Println("Found SERVICE_NOT_FOUND in chain")
	}

	// Match by code using Is with a new error instance
	if Is(wrappedErr, &ForgeError{Code: CodeConfigError}) {
		fmt.Println("Found CONFIG_ERROR in chain")
	}

	// Extract specific error type using As
	var configErr *ForgeError
	if As(wrappedErr, &configErr) {
		if configErr.Code == CodeConfigError {
			fmt.Println("Top-level error is CONFIG_ERROR")
		}
	}

	// Output:
	// Found SERVICE_NOT_FOUND in chain
	// Found CONFIG_ERROR in chain
	// Top-level error is CONFIG_ERROR
}
