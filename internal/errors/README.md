# Forge Error Package

A production-ready error handling package with support for the Go standard library's `errors.Is` and `errors.As` patterns.

## Features

✅ **Error Code Constants** - Type-safe error codes  
✅ **`errors.Is` Support** - Pattern matching for error types  
✅ **`errors.As` Support** - Type extraction from error chains  
✅ **Structured Errors** - Rich context with timestamps and metadata  
✅ **Error Wrapping** - Full chain traversal support  
✅ **HTTP Integration** - Automatic status code mapping  
✅ **Sentinel Errors** - Predefined error instances for comparison

## Error Code Constants

All error codes are defined as constants for type safety and consistency:

```go
const (
    CodeConfigError           = "CONFIG_ERROR"
    CodeValidationError       = "VALIDATION_ERROR"
    CodeLifecycleError        = "LIFECYCLE_ERROR"
    CodeContextCancelled      = "CONTEXT_CANCELLED"
    CodeServiceNotFound       = "SERVICE_NOT_FOUND"
    CodeServiceAlreadyExists  = "SERVICE_ALREADY_EXISTS"
    CodeCircularDependency    = "CIRCULAR_DEPENDENCY"
    CodeInvalidConfig         = "INVALID_CONFIG"
    CodeTimeoutError          = "TIMEOUT_ERROR"
)
```

## Quick Start

### Creating Errors

```go
// Using constructor functions (recommended)
err := errors.ErrServiceNotFound("database")
err := errors.ErrValidationError("email", fmt.Errorf("invalid format"))
err := errors.ErrTimeoutError("query", 5*time.Second)

// With additional context
err := errors.ErrServiceNotFound("cache").
    WithContext("host", "localhost").
    WithContext("port", 6379)
```

### Checking Error Types

```go
// Using sentinel errors (most concise)
if errors.Is(err, errors.ErrServiceNotFoundSentinel) {
    // Handle service not found
}

// Using helper functions (clearest intent)
if errors.IsServiceNotFound(err) {
    // Handle service not found
}

// Using error codes (most flexible)
var forgeErr *errors.ForgeError
if errors.As(err, &forgeErr) {
    switch forgeErr.Code {
    case errors.CodeServiceNotFound:
        // Handle service not found
    case errors.CodeTimeout:
        // Handle timeout
    }
}
```

### Working with Error Chains

```go
// Create a wrapped error
baseErr := errors.ErrServiceNotFound("auth")
wrappedErr := errors.ErrConfigError("initialization failed", baseErr)

// Check if any error in the chain matches
if errors.Is(wrappedErr, errors.ErrServiceNotFoundSentinel) {
    // This will match even though it's wrapped
}

// Extract specific error type from chain
var serviceErr *errors.ServiceError
if errors.As(wrappedErr, &serviceErr) {
    log.Printf("Service: %s, Operation: %s", serviceErr.Service, serviceErr.Operation)
}
```

### HTTP Error Handling

```go
err := errors.BadRequest("invalid input")

// Extract HTTP status code
statusCode := errors.GetHTTPStatusCode(err) // 400

// Works with wrapped errors too
wrappedErr := errors.ErrConfigError("failed", errors.Unauthorized("token expired"))
statusCode = errors.GetHTTPStatusCode(wrappedErr) // 401
```

## Error Types

### ForgeError

Structured error with error code, message, cause, timestamp, and context.

```go
type ForgeError struct {
    Code      string                 // Error code constant
    Message   string                 // Human-readable message
    Cause     error                  // Underlying error (can be nil)
    Timestamp time.Time              // When the error occurred
    Context   map[string]interface{} // Additional context
}
```

### ServiceError

Error specific to service operations.

```go
type ServiceError struct {
    Service   string // Service name
    Operation string // Operation that failed
    Err       error  // Underlying error
}
```

### HTTPError

HTTP-aware error with status code.

```go
type HTTPError struct {
    Code    int    // HTTP status code
    Message string // Error message
    Err     error  // Underlying error
}
```

## Sentinel Errors

Predefined error instances for use with `errors.Is`:

- `ErrServiceNotFoundSentinel`
- `ErrServiceAlreadyExistsSentinel`
- `ErrCircularDependencySentinel`
- `ErrInvalidConfigSentinel`
- `ErrValidationErrorSentinel`
- `ErrLifecycleErrorSentinel`
- `ErrContextCancelledSentinel`
- `ErrTimeoutErrorSentinel`
- `ErrConfigErrorSentinel`

## Helper Functions

Convenience functions for common error checks:

```go
IsServiceNotFound(err)      bool
IsServiceAlreadyExists(err) bool
IsCircularDependency(err)   bool
IsValidationError(err)      bool
IsContextCancelled(err)     bool
IsTimeout(err)              bool
GetHTTPStatusCode(err)      int
```

## Standard Library Integration

Wrapper functions for Go's `errors` package:

```go
errors.Is(err, target)      // errors.Is wrapper
errors.As(err, target)      // errors.As wrapper
errors.Unwrap(err)          // errors.Unwrap wrapper
errors.New(text)            // errors.New wrapper
errors.Join(errs...)        // errors.Join wrapper
```

## Best Practices

1. **Use constructor functions** - They ensure consistent error codes
2. **Add context** - Use `WithContext()` for debugging information
3. **Check error chains** - Always use `Is()` and `As()` for type checking
4. **Log at boundaries** - Don't log at every layer, only at boundaries
5. **Preserve causes** - Always wrap underlying errors, don't discard them

## Testing

Comprehensive test coverage including:
- Error matching with `Is()`
- Type extraction with `As()`
- Error chain traversal
- HTTP status code extraction
- Context preservation

Run tests:
```bash
go test -v ./v2/internal/errors/...
```

## Examples

See `examples_test.go` for detailed usage examples.

