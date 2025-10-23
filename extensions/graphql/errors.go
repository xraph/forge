package graphql

import "errors"

// Common GraphQL errors
var (
	ErrNotInitialized     = errors.New("graphql: not initialized")
	ErrInvalidQuery       = errors.New("graphql: invalid query")
	ErrInvalidSchema      = errors.New("graphql: invalid schema")
	ErrTypeNotFound       = errors.New("graphql: type not found")
	ErrResolverNotFound   = errors.New("graphql: resolver not found")
	ErrExecutionFailed    = errors.New("graphql: execution failed")
	ErrComplexityExceeded = errors.New("graphql: query complexity exceeded")
	ErrDepthExceeded      = errors.New("graphql: query depth exceeded")
	ErrTimeout            = errors.New("graphql: query timeout")
	ErrInvalidConfig      = errors.New("graphql: invalid configuration")
)
