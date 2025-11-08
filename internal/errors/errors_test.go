package errors

import (
	"errors"
	"net/http"
	"testing"
)

// TestForgeErrorIs tests the Is implementation for ForgeError.
func TestForgeErrorIs(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		target error
		want   bool
	}{
		{
			name:   "same error code matches",
			err:    ErrServiceNotFound("test-service"),
			target: ErrServiceNotFoundSentinel,
			want:   true,
		},
		{
			name:   "different error code does not match",
			err:    ErrServiceNotFound("test-service"),
			target: ErrServiceAlreadyExistsSentinel,
			want:   false,
		},
		{
			name:   "wrapped error matches",
			err:    ErrConfigError("invalid value", ErrServiceNotFound("db")),
			target: ErrServiceNotFoundSentinel,
			want:   true,
		},
		{
			name:   "nil target does not match",
			err:    ErrServiceNotFound("test"),
			target: nil,
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Is(tt.err, tt.target); got != tt.want {
				t.Errorf("Is() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestServiceErrorIs tests the Is implementation for ServiceError.
func TestServiceErrorIs(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		target error
		want   bool
	}{
		{
			name:   "same service and operation matches",
			err:    NewServiceError("db", "connect", errors.New("timeout")),
			target: NewServiceError("db", "connect", nil),
			want:   true,
		},
		{
			name:   "partial match with empty service",
			err:    NewServiceError("db", "connect", errors.New("timeout")),
			target: NewServiceError("", "connect", nil),
			want:   true,
		},
		{
			name:   "partial match with empty operation",
			err:    NewServiceError("db", "connect", errors.New("timeout")),
			target: NewServiceError("db", "", nil),
			want:   true,
		},
		{
			name:   "different service does not match",
			err:    NewServiceError("db", "connect", errors.New("timeout")),
			target: NewServiceError("cache", "connect", nil),
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Is(tt.err, tt.target); got != tt.want {
				t.Errorf("Is() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestHTTPErrorIs tests the Is implementation for HTTPError.
func TestHTTPErrorIs(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		target error
		want   bool
	}{
		{
			name:   "same status code matches",
			err:    BadRequest("invalid input"),
			target: NewHTTPError(http.StatusBadRequest, ""),
			want:   true,
		},
		{
			name:   "different status code does not match",
			err:    BadRequest("invalid input"),
			target: NewHTTPError(http.StatusNotFound, ""),
			want:   false,
		},
		{
			name:   "wrapped http error matches",
			err:    InternalError(BadRequest("test")),
			target: NewHTTPError(http.StatusBadRequest, ""),
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Is(tt.err, tt.target); got != tt.want {
				t.Errorf("Is() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestErrorAs tests the As wrapper function.
func TestErrorAs(t *testing.T) {
	t.Run("extract ForgeError", func(t *testing.T) {
		err := ErrServiceNotFound("test-service")

		var forgeErr *ForgeError
		if !As(err, &forgeErr) {
			t.Error("As() failed to extract ForgeError")
		}

		if forgeErr.Code != CodeServiceNotFound {
			t.Errorf("extracted error code = %s, want %s", forgeErr.Code, CodeServiceNotFound)
		}
	})

	t.Run("extract HTTPError", func(t *testing.T) {
		err := BadRequest("invalid")

		var httpErr *HTTPError
		if !As(err, &httpErr) {
			t.Error("As() failed to extract HTTPError")
		}

		if httpErr.Code != http.StatusBadRequest {
			t.Errorf("extracted status code = %d, want %d", httpErr.Code, http.StatusBadRequest)
		}
	})

	t.Run("extract from wrapped error", func(t *testing.T) {
		innerErr := BadRequest("test")
		wrappedErr := ErrConfigError("config failed", innerErr)

		var httpErr *HTTPError
		if !As(wrappedErr, &httpErr) {
			t.Error("As() failed to extract HTTPError from wrapped error")
		}

		if httpErr.Code != http.StatusBadRequest {
			t.Errorf("extracted status code = %d, want %d", httpErr.Code, http.StatusBadRequest)
		}
	})
}

// TestHelperFunctions tests the convenience helper functions.
func TestHelperFunctions(t *testing.T) {
	t.Run("IsServiceNotFound", func(t *testing.T) {
		err := ErrServiceNotFound("test")
		if !IsServiceNotFound(err) {
			t.Error("IsServiceNotFound() failed to identify service not found error")
		}
	})

	t.Run("IsValidationError", func(t *testing.T) {
		err := ErrValidationError("email", errors.New("invalid format"))
		if !IsValidationError(err) {
			t.Error("IsValidationError() failed to identify validation error")
		}
	})

	t.Run("IsTimeout", func(t *testing.T) {
		err := ErrTimeoutError("database query", 5000)
		if !IsTimeout(err) {
			t.Error("IsTimeout() failed to identify timeout error")
		}
	})
}

// TestGetHTTPStatusCode tests the status code extraction helper.
func TestGetHTTPStatusCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{
			name: "extract from HTTPError",
			err:  BadRequest("test"),
			want: http.StatusBadRequest,
		},
		{
			name: "extract from wrapped HTTPError",
			err:  ErrConfigError("failed", Unauthorized("not allowed")),
			want: http.StatusUnauthorized,
		},
		{
			name: "default to 500 for non-HTTP error",
			err:  ErrServiceNotFound("test"),
			want: http.StatusInternalServerError,
		},
		{
			name: "default to 500 for nil",
			err:  nil,
			want: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetHTTPStatusCode(tt.err); got != tt.want {
				t.Errorf("GetHTTPStatusCode() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestUnwrap tests the Unwrap wrapper function.
func TestUnwrap(t *testing.T) {
	innerErr := errors.New("inner error")
	wrappedErr := ErrConfigError("config failed", innerErr)

	unwrapped := Unwrap(wrappedErr)
	if !errors.Is(unwrapped, innerErr) {
		t.Errorf("Unwrap() returned wrong error: got %v, want %v", unwrapped, innerErr)
	}
}

// TestJoin tests the Join wrapper function.
func TestJoin(t *testing.T) {
	err1 := errors.New("error 1")
	err2 := errors.New("error 2")
	err3 := errors.New("error 3")

	joined := Join(err1, err2, err3)
	if joined == nil {
		t.Fatal("Join() returned nil")
	}

	// Check that all errors are in the chain
	if !errors.Is(joined, err1) {
		t.Error("joined error does not contain err1")
	}

	if !errors.Is(joined, err2) {
		t.Error("joined error does not contain err2")
	}

	if !errors.Is(joined, err3) {
		t.Error("joined error does not contain err3")
	}
}

// TestWithContext tests the WithContext method.
func TestWithContext(t *testing.T) {
	err := ErrServiceNotFound("test").
		WithContext("user_id", "123").
		WithContext("request_id", "abc")

	if err.Context["user_id"] != "123" {
		t.Error("context user_id not set correctly")
	}

	if err.Context["request_id"] != "abc" {
		t.Error("context request_id not set correctly")
	}
}

// Example usage demonstrating the new Is functionality.
func ExampleIs() {
	// Create an error
	err := ErrServiceNotFound("database")

	// Check using Is with sentinel error
	if Is(err, ErrServiceNotFoundSentinel) {
		// Handle service not found
	}

	// Or use the convenience helper
	if IsServiceNotFound(err) {
		// Handle service not found
	}
}

// Example showing error unwrapping.
func ExampleAs() {
	// Create a wrapped error
	innerErr := BadRequest("invalid input")
	wrappedErr := ErrConfigError("config failed", innerErr)

	// Extract the HTTPError from the chain
	var httpErr *HTTPError
	if As(wrappedErr, &httpErr) {
		// Use the extracted error
		statusCode := httpErr.Code
		_ = statusCode
	}
}
