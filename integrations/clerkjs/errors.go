package clerkjs

import (
	"time"

	"github.com/xraph/forge/pkg/common"
)

func ErrUnauthorized(message string) error {
	return &common.ForgeError{
		Code:      "UNAUTHORIZED",
		Message:   message,
		Timestamp: time.Now(),
	}
}

func ErrNotFound(message string) error {
	return &common.ForgeError{
		Code:      "NOT_FOUND",
		Message:   message,
		Timestamp: time.Now(),
	}
}

func ErrInternalError(message string, cause error) error {
	return &common.ForgeError{
		Code:      "INTERNAL_ERROR",
		Message:   message,
		Cause:     cause,
		Timestamp: time.Now(),
	}
}

func ErrServiceUnavailable(message string) error {
	return &common.ForgeError{
		Code:      "SERVICE_UNAVAILABLE",
		Message:   message,
		Timestamp: time.Now(),
	}
}
