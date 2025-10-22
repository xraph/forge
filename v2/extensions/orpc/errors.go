package orpc

import "errors"

// Package-level errors
var (
	ErrMethodExists      = errors.New("orpc: method already exists")
	ErrMethodNotFound    = errors.New("orpc: method not found")
	ErrInvalidMethodName = errors.New("orpc: invalid method name")
	ErrDisabled          = errors.New("orpc: extension is disabled")
	ErrBatchDisabled     = errors.New("orpc: batch requests are disabled")
	ErrBatchTooLarge     = errors.New("orpc: batch size exceeds limit")
	ErrRequestTooLarge   = errors.New("orpc: request size exceeds limit")
)

// GetErrorMessage returns the standard message for a JSON-RPC error code
func GetErrorMessage(code int) string {
	switch code {
	case ErrParseError:
		return "Parse error"
	case ErrInvalidRequest:
		return "Invalid Request"
	case ErrMethodNotFound:
		return "Method not found"
	case ErrInvalidParams:
		return "Invalid params"
	case ErrInternalError:
		return "Internal error"
	case ErrServerError:
		return "Server error"
	default:
		return "Unknown error"
	}
}

