package di

import (
	"fmt"
	"sync"

	"github.com/xraph/forge/internal/shared"
)

// Lazy wraps a dependency that is resolved on first access.
// This is useful for breaking circular dependencies or deferring
// resolution of expensive services until they're actually needed.
type Lazy[T any] struct {
	container shared.Container
	name      string
	mu        sync.Once
	value     T
	err       error
	resolved  bool
}

// NewLazy creates a new lazy dependency wrapper.
func NewLazy[T any](container shared.Container, name string) *Lazy[T] {
	return &Lazy[T]{
		container: container,
		name:      name,
	}
}

// Get resolves the dependency and returns it.
// The resolution happens only once; subsequent calls return the cached value.
func (l *Lazy[T]) Get() (T, error) {
	l.mu.Do(func() {
		instance, err := l.container.Resolve(l.name)
		if err != nil {
			l.err = err
			return
		}

		typed, ok := instance.(T)
		if !ok {
			var zero T
			l.err = fmt.Errorf("lazy dependency %s: expected type %T, got %T", l.name, zero, instance)
			return
		}

		l.value = typed
		l.resolved = true
	})

	return l.value, l.err
}

// MustGet resolves the dependency and returns it, panicking on error.
func (l *Lazy[T]) MustGet() T {
	value, err := l.Get()
	if err != nil {
		panic(fmt.Sprintf("lazy dependency %s failed: %v", l.name, err))
	}
	return value
}

// IsResolved returns true if the dependency has been resolved.
func (l *Lazy[T]) IsResolved() bool {
	return l.resolved
}

// Name returns the name of the dependency.
func (l *Lazy[T]) Name() string {
	return l.name
}

// OptionalLazy wraps an optional dependency that is resolved on first access.
// Returns nil without error if the dependency is not found.
type OptionalLazy[T any] struct {
	container shared.Container
	name      string
	mu        sync.Once
	value     T
	err       error
	resolved  bool
	found     bool
}

// NewOptionalLazy creates a new optional lazy dependency wrapper.
func NewOptionalLazy[T any](container shared.Container, name string) *OptionalLazy[T] {
	return &OptionalLazy[T]{
		container: container,
		name:      name,
	}
}

// Get resolves the dependency and returns it.
// Returns the zero value without error if the dependency is not found.
func (l *OptionalLazy[T]) Get() (T, error) {
	l.mu.Do(func() {
		if !l.container.Has(l.name) {
			l.resolved = true
			l.found = false
			return
		}

		instance, err := l.container.Resolve(l.name)
		if err != nil {
			l.err = err
			return
		}

		typed, ok := instance.(T)
		if !ok {
			var zero T
			l.err = fmt.Errorf("optional lazy dependency %s: expected type %T, got %T", l.name, zero, instance)
			return
		}

		l.value = typed
		l.resolved = true
		l.found = true
	})

	return l.value, l.err
}

// MustGet resolves the dependency and returns it, panicking on error.
// Returns the zero value if the dependency is not found (does not panic).
func (l *OptionalLazy[T]) MustGet() T {
	value, err := l.Get()
	if err != nil {
		panic(fmt.Sprintf("optional lazy dependency %s failed: %v", l.name, err))
	}
	return value
}

// IsResolved returns true if the dependency has been resolved.
func (l *OptionalLazy[T]) IsResolved() bool {
	return l.resolved
}

// IsFound returns true if the dependency was found (only valid after resolution).
func (l *OptionalLazy[T]) IsFound() bool {
	return l.found
}

// Name returns the name of the dependency.
func (l *OptionalLazy[T]) Name() string {
	return l.name
}

// Provider wraps a dependency that creates new instances on each access.
// This is useful for transient dependencies where a fresh instance is needed each time.
type Provider[T any] struct {
	container shared.Container
	name      string
}

// NewProvider creates a new provider for transient dependencies.
func NewProvider[T any](container shared.Container, name string) *Provider[T] {
	return &Provider[T]{
		container: container,
		name:      name,
	}
}

// Provide resolves and returns a new instance of the dependency.
// Each call may return a different instance (if the service is transient).
func (p *Provider[T]) Provide() (T, error) {
	instance, err := p.container.Resolve(p.name)
	if err != nil {
		var zero T
		return zero, err
	}

	typed, ok := instance.(T)
	if !ok {
		var zero T
		return zero, fmt.Errorf("provider %s: expected type %T, got %T", p.name, zero, instance)
	}

	return typed, nil
}

// MustProvide resolves and returns a new instance, panicking on error.
func (p *Provider[T]) MustProvide() T {
	value, err := p.Provide()
	if err != nil {
		panic(fmt.Sprintf("provider %s failed: %v", p.name, err))
	}
	return value
}

// Name returns the name of the dependency.
func (p *Provider[T]) Name() string {
	return p.name
}
