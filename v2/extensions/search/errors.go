package search

import "errors"

// Common search errors
var (
	ErrNotConnected       = errors.New("search: not connected")
	ErrAlreadyConnected   = errors.New("search: already connected")
	ErrIndexNotFound      = errors.New("search: index not found")
	ErrIndexAlreadyExists = errors.New("search: index already exists")
	ErrDocumentNotFound   = errors.New("search: document not found")
	ErrInvalidQuery       = errors.New("search: invalid query")
	ErrInvalidSchema      = errors.New("search: invalid schema")
	ErrConnectionFailed   = errors.New("search: connection failed")
	ErrTimeout            = errors.New("search: operation timeout")
	ErrInvalidConfig      = errors.New("search: invalid configuration")
	ErrUnsupportedDriver  = errors.New("search: unsupported driver")
	ErrOperationFailed    = errors.New("search: operation failed")
)
