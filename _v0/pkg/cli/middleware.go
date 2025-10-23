package cli

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/logger"
)

// CLIMiddleware defines the interface for CLI middleware
type CLIMiddleware interface {
	Name() string
	Priority() int
	Execute(ctx CLIContext, next func() error) error
}

// middlewareChain executes middleware in priority order
type middlewareChain struct {
	middleware []CLIMiddleware
	handler    func() error
}

// Execute executes the middleware chain
func (mc *middlewareChain) Execute(ctx CLIContext) error {
	if len(mc.middleware) == 0 {
		return mc.handler()
	}

	// Sort middleware by priority
	sortedMiddleware := make([]CLIMiddleware, len(mc.middleware))
	copy(sortedMiddleware, mc.middleware)
	sort.Slice(sortedMiddleware, func(i, j int) bool {
		return sortedMiddleware[i].Priority() < sortedMiddleware[j].Priority()
	})

	var executeChain func(int) error
	executeChain = func(index int) error {
		if index >= len(sortedMiddleware) {
			return mc.handler()
		}

		middleware := sortedMiddleware[index]
		return middleware.Execute(ctx, func() error {
			return executeChain(index + 1)
		})
	}

	return executeChain(0)
}

// MiddlewareManager manages CLI middleware
type MiddlewareManager struct {
	middleware []CLIMiddleware
	mutex      sync.RWMutex
}

// NewMiddlewareManager creates a new middleware manager
func NewMiddlewareManager() *MiddlewareManager {
	return &MiddlewareManager{
		middleware: make([]CLIMiddleware, 0),
	}
}

// Add adds middleware to the manager
func (mm *MiddlewareManager) Add(middleware CLIMiddleware) error {
	mm.mutex.Lock()
	defer mm.mutex.Unlock()

	// Check for duplicate names
	for _, existing := range mm.middleware {
		if existing.Name() == middleware.Name() {
			return fmt.Errorf("middleware with name '%s' already exists", middleware.Name())
		}
	}

	mm.middleware = append(mm.middleware, middleware)

	// Sort by priority
	sort.Slice(mm.middleware, func(i, j int) bool {
		return mm.middleware[i].Priority() < mm.middleware[j].Priority()
	})

	return nil
}

// Remove removes middleware by name
func (mm *MiddlewareManager) Remove(name string) error {
	mm.mutex.Lock()
	defer mm.mutex.Unlock()

	for i, middleware := range mm.middleware {
		if middleware.Name() == name {
			mm.middleware = append(mm.middleware[:i], mm.middleware[i+1:]...)
			return nil
		}
	}

	return fmt.Errorf("middleware with name '%s' not found", name)
}

// Get returns middleware by name
func (mm *MiddlewareManager) Get(name string) (CLIMiddleware, error) {
	mm.mutex.RLock()
	defer mm.mutex.RUnlock()

	for _, middleware := range mm.middleware {
		if middleware.Name() == name {
			return middleware, nil
		}
	}

	return nil, fmt.Errorf("middleware with name '%s' not found", name)
}

// GetAll returns all middleware sorted by priority
func (mm *MiddlewareManager) GetAll() []CLIMiddleware {
	mm.mutex.RLock()
	defer mm.mutex.RUnlock()

	result := make([]CLIMiddleware, len(mm.middleware))
	copy(result, mm.middleware)
	return result
}

// Clear removes all middleware
func (mm *MiddlewareManager) Clear() {
	mm.mutex.Lock()
	defer mm.mutex.Unlock()

	mm.middleware = mm.middleware[:0]
}

// ExecuteChain creates and executes a middleware chain
func (mm *MiddlewareManager) ExecuteChain(ctx CLIContext, handler func() error, additionalMiddleware ...CLIMiddleware) error {
	mm.mutex.RLock()
	allMiddleware := make([]CLIMiddleware, len(mm.middleware)+len(additionalMiddleware))
	copy(allMiddleware, mm.middleware)
	copy(allMiddleware[len(mm.middleware):], additionalMiddleware)
	mm.mutex.RUnlock()

	chain := &middlewareChain{
		middleware: allMiddleware,
		handler:    handler,
	}

	return chain.Execute(ctx)
}

// BaseMiddleware provides a base implementation for middleware
type BaseMiddleware struct {
	name     string
	priority int
}

// NewBaseMiddleware creates a new base middleware
func NewBaseMiddleware(name string, priority int) *BaseMiddleware {
	return &BaseMiddleware{
		name:     name,
		priority: priority,
	}
}

// Name returns the middleware name
func (bm *BaseMiddleware) Name() string {
	return bm.name
}

// Priority returns the middleware priority
func (bm *BaseMiddleware) Priority() int {
	return bm.priority
}

// FunctionalMiddleware allows creating middleware from functions
type FunctionalMiddleware struct {
	*BaseMiddleware
	executeFunc func(ctx CLIContext, next func() error) error
}

// NewFunctionalMiddleware creates middleware from a function
func NewFunctionalMiddleware(name string, priority int, executeFunc func(ctx CLIContext, next func() error) error) CLIMiddleware {
	return &FunctionalMiddleware{
		BaseMiddleware: NewBaseMiddleware(name, priority),
		executeFunc:    executeFunc,
	}
}

// Execute executes the functional middleware
func (fm *FunctionalMiddleware) Execute(ctx CLIContext, next func() error) error {
	return fm.executeFunc(ctx, next)
}

// MiddlewareChain represents a chain of middleware
type MiddlewareChain []CLIMiddleware

// Execute executes the middleware chain
func (mc MiddlewareChain) Execute(ctx CLIContext, handler func() error) error {
	if len(mc) == 0 {
		return handler()
	}

	// Sort by priority
	sortedMiddleware := make([]CLIMiddleware, len(mc))
	copy(sortedMiddleware, mc)
	sort.Slice(sortedMiddleware, func(i, j int) bool {
		return sortedMiddleware[i].Priority() < sortedMiddleware[j].Priority()
	})

	var executeChain func(int) error
	executeChain = func(index int) error {
		if index >= len(sortedMiddleware) {
			return handler()
		}

		middleware := sortedMiddleware[index]
		return middleware.Execute(ctx, func() error {
			return executeChain(index + 1)
		})
	}

	return executeChain(0)
}

// Add adds middleware to the chain
func (mc *MiddlewareChain) Add(middleware CLIMiddleware) {
	*mc = append(*mc, middleware)
}

// ConditionalMiddleware wraps middleware with a condition
type ConditionalMiddleware struct {
	middleware CLIMiddleware
	condition  func(ctx CLIContext) bool
}

// NewConditionalMiddleware creates middleware that only executes when condition is true
func NewConditionalMiddleware(middleware CLIMiddleware, condition func(ctx CLIContext) bool) CLIMiddleware {
	return &ConditionalMiddleware{
		middleware: middleware,
		condition:  condition,
	}
}

// Name returns the wrapped middleware name
func (cm *ConditionalMiddleware) Name() string {
	return cm.middleware.Name()
}

// Priority returns the wrapped middleware priority
func (cm *ConditionalMiddleware) Priority() int {
	return cm.middleware.Priority()
}

// Execute executes the middleware conditionally
func (cm *ConditionalMiddleware) Execute(ctx CLIContext, next func() error) error {
	if cm.condition(ctx) {
		return cm.middleware.Execute(ctx, next)
	}
	return next()
}

// CombinedMiddleware combines multiple middleware into one
type CombinedMiddleware struct {
	name       string
	priority   int
	middleware []CLIMiddleware
}

// NewCombinedMiddleware creates middleware that combines multiple middleware
func NewCombinedMiddleware(name string, priority int, middleware ...CLIMiddleware) CLIMiddleware {
	return &CombinedMiddleware{
		name:       name,
		priority:   priority,
		middleware: middleware,
	}
}

// Name returns the middleware name
func (cm *CombinedMiddleware) Name() string {
	return cm.name
}

// Priority returns the middleware priority
func (cm *CombinedMiddleware) Priority() int {
	return cm.priority
}

// Execute executes all combined middleware
func (cm *CombinedMiddleware) Execute(ctx CLIContext, next func() error) error {
	chain := MiddlewareChain(cm.middleware)
	return chain.Execute(ctx, next)
}

// AsyncMiddleware allows middleware to run asynchronously
type AsyncMiddleware struct {
	middleware CLIMiddleware
	async      bool
}

// NewAsyncMiddleware creates middleware that can run asynchronously
func NewAsyncMiddleware(middleware CLIMiddleware, async bool) CLIMiddleware {
	return &AsyncMiddleware{
		middleware: middleware,
		async:      async,
	}
}

// Name returns the wrapped middleware name
func (am *AsyncMiddleware) Name() string {
	return am.middleware.Name()
}

// Priority returns the wrapped middleware priority
func (am *AsyncMiddleware) Priority() int {
	return am.middleware.Priority()
}

// Execute executes the middleware asynchronously if configured
func (am *AsyncMiddleware) Execute(ctx CLIContext, next func() error) error {
	if am.async {
		// For CLI middleware, we typically don't want true async behavior
		// as it can interfere with user interaction, but we can defer cleanup
		go func() {
			// Defer any cleanup operations
			defer func() {
				if r := recover(); r != nil {
					ctx.Logger().Error("async middleware panic",
						logger.String("middleware", am.middleware.Name()),
						logger.Any("panic", r),
					)
				}
			}()
		}()
	}

	return am.middleware.Execute(ctx, next)
}

// TimeoutMiddleware provides timeout functionality
type TimeoutMiddleware struct {
	*BaseMiddleware
	timeout time.Duration
}

// NewTimeoutMiddleware creates middleware with timeout support
func NewTimeoutMiddleware(name string, priority int, timeout time.Duration) CLIMiddleware {
	return &TimeoutMiddleware{
		BaseMiddleware: NewBaseMiddleware(name, priority),
		timeout:        timeout,
	}
}

// Execute executes the middleware with timeout
func (tm *TimeoutMiddleware) Execute(ctx CLIContext, next func() error) error {
	if tm.timeout <= 0 {
		return next()
	}

	done := make(chan error, 1)
	go func() {
		done <- next()
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(tm.timeout):
		return fmt.Errorf("command timeout after %v", tm.timeout)
	}
}
