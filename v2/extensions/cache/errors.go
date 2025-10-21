package cache

import "errors"

var (
	// ErrNotFound is returned when a key is not found in the cache
	ErrNotFound = errors.New("cache: key not found")

	// ErrNotConnected is returned when attempting operations on a disconnected cache
	ErrNotConnected = errors.New("cache: not connected")

	// ErrInvalidTTL is returned when an invalid TTL is provided
	ErrInvalidTTL = errors.New("cache: invalid TTL")

	// ErrKeyTooLarge is returned when a key exceeds the maximum allowed size
	ErrKeyTooLarge = errors.New("cache: key too large")

	// ErrValueTooLarge is returned when a value exceeds the maximum allowed size
	ErrValueTooLarge = errors.New("cache: value too large")

	// ErrUnsupportedOperation is returned when an operation is not supported by the backend
	ErrUnsupportedOperation = errors.New("cache: operation not supported")
)
