package contract

import "fmt"

// ErrorCode is a canonical, wire-stable code for contract errors.
// Contributor-specific codes are namespaced like "auth.SESSION_EXPIRED".
type ErrorCode string

const (
	CodeBadRequest         ErrorCode = "BAD_REQUEST"
	CodeUnauthenticated    ErrorCode = "UNAUTHENTICATED"
	CodePermissionDenied   ErrorCode = "PERMISSION_DENIED"
	CodeNotFound           ErrorCode = "NOT_FOUND"
	CodeConflict           ErrorCode = "CONFLICT"
	CodeRateLimited        ErrorCode = "RATE_LIMITED"
	CodeUnsupportedVersion ErrorCode = "UNSUPPORTED_VERSION"
	CodeUnavailable        ErrorCode = "UNAVAILABLE"
	CodeInternal           ErrorCode = "INTERNAL"
)

// Sentinel errors for use with errors.Is.
var (
	ErrBadRequest         = &Error{Code: CodeBadRequest}
	ErrUnauthenticated    = &Error{Code: CodeUnauthenticated}
	ErrPermissionDenied   = &Error{Code: CodePermissionDenied}
	ErrNotFound           = &Error{Code: CodeNotFound}
	ErrConflict           = &Error{Code: CodeConflict}
	ErrRateLimited        = &Error{Code: CodeRateLimited}
	ErrUnsupportedVersion = &Error{Code: CodeUnsupportedVersion}
	ErrUnavailable        = &Error{Code: CodeUnavailable}
	ErrInternal           = &Error{Code: CodeInternal}
)

// Error is the canonical contract error type. It serializes to the wire
// "error" object documented in DESIGN.md.
type Error struct {
	Code          ErrorCode      `json:"code"`
	Message       string         `json:"message,omitempty"`
	Details       map[string]any `json:"details,omitempty"`
	Retryable     bool           `json:"retryable,omitempty"`
	CorrelationID string         `json:"correlationID,omitempty"`
	Redactions    []string       `json:"redactions,omitempty"`
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Message == "" {
		return string(e.Code)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Is matches sentinel errors by Code.
func (e *Error) Is(target error) bool {
	if t, ok := target.(*Error); ok {
		return t.Code == e.Code
	}
	return false
}
