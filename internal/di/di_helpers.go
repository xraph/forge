package di

import (
	"fmt"

	errors2 "github.com/xraph/forge/errors"
	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/metrics"
	"github.com/xraph/forge/internal/shared"
)

// Resolve with type safety.
func Resolve[T any](c Container, name string) (T, error) {
	var zero T

	instance, err := c.Resolve(name)
	if err != nil {
		return zero, err
	}

	typed, ok := instance.(T)
	if !ok {
		return zero, fmt.Errorf("%w: service %s is not of type %T", errors2.ErrTypeMismatch, name, zero)
	}

	return typed, nil
}

// Must resolves or panics - use only during startup.
func Must[T any](c Container, name string) T {
	instance, err := Resolve[T](c, name)
	if err != nil {
		panic(fmt.Sprintf("failed to resolve %s: %v", name, err))
	}

	return instance
}

// RegisterSingleton is a convenience wrapper.
func RegisterSingleton[T any](c Container, name string, factory func(Container) (T, error)) error {
	return c.Register(name, func(c Container) (any, error) {
		return factory(c)
	}, Singleton())
}

// RegisterTransient is a convenience wrapper.
func RegisterTransient[T any](c Container, name string, factory func(Container) (T, error)) error {
	return c.Register(name, func(c Container) (any, error) {
		return factory(c)
	}, Transient())
}

// RegisterScoped is a convenience wrapper for request-scoped services.
func RegisterScoped[T any](c Container, name string, factory func(Container) (T, error)) error {
	return c.Register(name, func(c Container) (any, error) {
		return factory(c)
	}, Scoped())
}

// RegisterInterface registers an implementation as an interface
// Supports all lifecycle options (Singleton, Scoped, Transient).
func RegisterInterface[I, T any](c Container, name string, factory func(Container) (T, error), opts ...RegisterOption) error {
	return c.Register(name, func(c Container) (any, error) {
		impl, err := factory(c)
		if err != nil {
			return nil, err
		}
		// Return as any - the type will be checked at resolve time
		return any(impl), nil
	}, opts...)
}

// RegisterValue registers a pre-built instance (always singleton).
func RegisterValue[T any](c Container, name string, instance T) error {
	return c.Register(name, func(c Container) (any, error) {
		return instance, nil
	}, Singleton())
}

// RegisterSingletonInterface is a convenience wrapper.
func RegisterSingletonInterface[I, T any](c Container, name string, factory func(Container) (T, error)) error {
	return RegisterInterface[I, T](c, name, factory, Singleton())
}

// RegisterScopedInterface is a convenience wrapper.
func RegisterScopedInterface[I, T any](c Container, name string, factory func(Container) (T, error)) error {
	return RegisterInterface[I, T](c, name, factory, Scoped())
}

// RegisterTransientInterface is a convenience wrapper.
func RegisterTransientInterface[I, T any](c Container, name string, factory func(Container) (T, error)) error {
	return RegisterInterface[I, T](c, name, factory, Transient())
}

// ResolveScope is a helper for resolving from a scope.
func ResolveScope[T any](s Scope, name string) (T, error) {
	var zero T

	instance, err := s.Resolve(name)
	if err != nil {
		return zero, err
	}

	typed, ok := instance.(T)
	if !ok {
		return zero, fmt.Errorf("%w: service %s is not of type %T", errors2.ErrTypeMismatch, name, zero)
	}

	return typed, nil
}

// MustScope resolves from scope or panics.
func MustScope[T any](s Scope, name string) T {
	instance, err := ResolveScope[T](s, name)
	if err != nil {
		panic(fmt.Sprintf("failed to resolve %s from scope: %v", name, err))
	}

	return instance
}

// GetLogger resolves the logger from the container
// This is a convenience function for resolving the logger service
// The logger type is defined in the forge package, so this returns interface{}
// and should be type-asserted to the appropriate logger interface.
func GetLogger(c Container) (logger.Logger, error) {
	l, err := c.Resolve(shared.LoggerKey)
	if err != nil {
		return nil, err
	}

	log, ok := l.(logger.Logger)
	if !ok {
		return nil, fmt.Errorf("resolved instance is not Logger, got %T", l)
	}

	return log, nil
}

// GetMetrics resolves the metrics from the container
// This is a convenience function for resolving the metrics service
// The metrics type is defined in the forge package, so this returns interface{}
// and should be type-asserted to the appropriate metrics interface.
func GetMetrics(c Container) (metrics.Metrics, error) {
	m, err := c.Resolve(shared.MetricsKey)
	if err != nil {
		return nil, err
	}

	metrics, ok := m.(metrics.Metrics)
	if !ok {
		return nil, fmt.Errorf("resolved instance is not Metrics, got %T", m)
	}

	return metrics, nil
}

// GetHealthManager resolves the health manager from the container
// This is a convenience function for resolving the health manager service
// The health manager type is defined in the forge package, so this returns the interface.
func GetHealthManager(c Container) (shared.HealthManager, error) {
	hm, err := c.Resolve(shared.HealthManagerKey)
	if err != nil {
		return nil, err
	}

	healthManager, ok := hm.(shared.HealthManager)
	if !ok {
		return nil, fmt.Errorf("resolved instance is not HealthManager, got %T", hm)
	}

	return healthManager, nil
}
