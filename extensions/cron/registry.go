package cron

import (
	"context"
	"fmt"
	"sync"
)

// JobRegistry manages registration of job handlers for code-based jobs.
// It allows you to register named handlers that can be referenced by name
// when creating jobs programmatically or from configuration.
type JobRegistry struct {
	handlers map[string]JobHandler
	mu       sync.RWMutex
}

// NewJobRegistry creates a new job registry.
func NewJobRegistry() *JobRegistry {
	return &JobRegistry{
		handlers: make(map[string]JobHandler),
	}
}

// Register registers a job handler with the given name.
// The handler can then be referenced when creating jobs.
//
// Example:
//
//	registry.Register("sendEmail", func(ctx context.Context, job *Job) error {
//	    // Send email logic
//	    return nil
//	})
func (r *JobRegistry) Register(name string, handler JobHandler) error {
	if name == "" {
		return fmt.Errorf("handler name cannot be empty")
	}

	if handler == nil {
		return fmt.Errorf("handler cannot be nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.handlers[name]; exists {
		return fmt.Errorf("handler '%s' already registered", name)
	}

	r.handlers[name] = handler
	return nil
}

// MustRegister registers a job handler and panics on error.
// Useful for registration at startup where failure should be fatal.
func (r *JobRegistry) MustRegister(name string, handler JobHandler) {
	if err := r.Register(name, handler); err != nil {
		panic(err)
	}
}

// Unregister removes a job handler.
func (r *JobRegistry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.handlers[name]; !exists {
		return fmt.Errorf("handler '%s' not found", name)
	}

	delete(r.handlers, name)
	return nil
}

// Get retrieves a job handler by name.
func (r *JobRegistry) Get(name string) (JobHandler, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	handler, exists := r.handlers[name]
	if !exists {
		return nil, fmt.Errorf("%w: '%s'", ErrHandlerNotFound, name)
	}

	return handler, nil
}

// Has checks if a handler is registered.
func (r *JobRegistry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, exists := r.handlers[name]
	return exists
}

// List returns all registered handler names.
func (r *JobRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.handlers))
	for name := range r.handlers {
		names = append(names, name)
	}

	return names
}

// Count returns the number of registered handlers.
func (r *JobRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.handlers)
}

// Clear removes all registered handlers.
func (r *JobRegistry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.handlers = make(map[string]JobHandler)
}

// RegisterBatch registers multiple handlers at once.
// If any registration fails, all successful registrations are rolled back.
func (r *JobRegistry) RegisterBatch(handlers map[string]JobHandler) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Validate all handlers first
	for name, handler := range handlers {
		if name == "" {
			return fmt.Errorf("handler name cannot be empty")
		}
		if handler == nil {
			return fmt.Errorf("handler cannot be nil for '%s'", name)
		}
		if _, exists := r.handlers[name]; exists {
			return fmt.Errorf("handler '%s' already registered", name)
		}
	}

	// Register all handlers
	for name, handler := range handlers {
		r.handlers[name] = handler
	}

	return nil
}

// WrapHandler wraps a handler with additional functionality (middleware pattern).
// This is useful for adding logging, metrics, error handling, etc.
//
// Example:
//
//	wrapped := registry.WrapHandler("myHandler", func(ctx context.Context, job *Job) error {
//	    start := time.Now()
//	    defer func() {
//	        logger.Info("job completed", "duration", time.Since(start))
//	    }()
//	    return originalHandler(ctx, job)
//	})
func (r *JobRegistry) WrapHandler(name string, wrapper func(JobHandler) JobHandler) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	handler, exists := r.handlers[name]
	if !exists {
		return fmt.Errorf("%w: '%s'", ErrHandlerNotFound, name)
	}

	r.handlers[name] = wrapper(handler)
	return nil
}

// RegisterWithMiddleware registers a handler with middleware applied.
func (r *JobRegistry) RegisterWithMiddleware(name string, handler JobHandler, middleware ...func(JobHandler) JobHandler) error {
	if name == "" {
		return fmt.Errorf("handler name cannot be empty")
	}

	if handler == nil {
		return fmt.Errorf("handler cannot be nil")
	}

	// Apply middleware in reverse order (so they execute in the order specified)
	for i := len(middleware) - 1; i >= 0; i-- {
		if middleware[i] != nil {
			handler = middleware[i](handler)
		}
	}

	return r.Register(name, handler)
}

// CreatePanicRecoveryMiddleware creates middleware that recovers from panics.
func CreatePanicRecoveryMiddleware(onPanic func(ctx context.Context, job *Job, recovered interface{})) func(JobHandler) JobHandler {
	return func(next JobHandler) JobHandler {
		return func(ctx context.Context, job *Job) (err error) {
			defer func() {
				if r := recover(); r != nil {
					if onPanic != nil {
						onPanic(ctx, job, r)
					}
					err = fmt.Errorf("panic recovered: %v", r)
				}
			}()
			return next(ctx, job)
		}
	}
}

// CreateLoggingMiddleware creates middleware that logs job execution.
func CreateLoggingMiddleware(log func(ctx context.Context, job *Job, err error)) func(JobHandler) JobHandler {
	return func(next JobHandler) JobHandler {
		return func(ctx context.Context, job *Job) error {
			err := next(ctx, job)
			if log != nil {
				log(ctx, job, err)
			}
			return err
		}
	}
}

// CreateTimeoutMiddleware creates middleware that enforces a timeout.
func CreateTimeoutMiddleware() func(JobHandler) JobHandler {
	return func(next JobHandler) JobHandler {
		return func(ctx context.Context, job *Job) error {
			if job.Timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, job.Timeout)
				defer cancel()
			}
			return next(ctx, job)
		}
	}
}
