package grpc

import "errors"

// Common gRPC errors
var (
	ErrNotStarted        = errors.New("grpc: server not started")
	ErrAlreadyStarted    = errors.New("grpc: server already started")
	ErrServiceNotFound   = errors.New("grpc: service not found")
	ErrInvalidConfig     = errors.New("grpc: invalid configuration")
	ErrStartFailed       = errors.New("grpc: failed to start server")
	ErrStopFailed        = errors.New("grpc: failed to stop server")
	ErrHealthCheckFailed = errors.New("grpc: health check failed")
)
