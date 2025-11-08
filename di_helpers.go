package forge

import (
	"github.com/xraph/forge/internal/di"
)

// Resolve with type safety.
func Resolve[T any](c Container, name string) (T, error) {
	return di.Resolve[T](c, name)
}

// Must resolves or panics - use only during startup.
func Must[T any](c Container, name string) T {
	return di.Must[T](c, name)
}

// RegisterSingleton is a convenience wrapper.
func RegisterSingleton[T any](c Container, name string, factory func(Container) (T, error)) error {
	return di.RegisterSingleton[T](c, name, factory)
}

// RegisterTransient is a convenience wrapper.
func RegisterTransient[T any](c Container, name string, factory func(Container) (T, error)) error {
	return di.RegisterTransient[T](c, name, factory)
}

// RegisterScoped is a convenience wrapper for request-scoped services.
func RegisterScoped[T any](c Container, name string, factory func(Container) (T, error)) error {
	return di.RegisterScoped[T](c, name, factory)
}

// RegisterInterface registers an implementation as an interface
// Supports all lifecycle options (Singleton, Scoped, Transient).
func RegisterInterface[I, T any](c Container, name string, factory func(Container) (T, error), opts ...RegisterOption) error {
	return di.RegisterInterface[I, T](c, name, factory, opts...)
}

// RegisterValue registers a pre-built instance (always singleton).
func RegisterValue[T any](c Container, name string, instance T) error {
	return di.RegisterValue[T](c, name, instance)
}

// RegisterSingletonInterface is a convenience wrapper.
func RegisterSingletonInterface[I, T any](c Container, name string, factory func(Container) (T, error)) error {
	return di.RegisterSingletonInterface[I, T](c, name, factory)
}

// RegisterScopedInterface is a convenience wrapper.
func RegisterScopedInterface[I, T any](c Container, name string, factory func(Container) (T, error)) error {
	return di.RegisterScopedInterface[I, T](c, name, factory)
}

// RegisterTransientInterface is a convenience wrapper.
func RegisterTransientInterface[I, T any](c Container, name string, factory func(Container) (T, error)) error {
	return di.RegisterTransientInterface[I, T](c, name, factory)
}

// ResolveScope is a helper for resolving from a scope.
func ResolveScope[T any](s Scope, name string) (T, error) {
	return di.ResolveScope[T](s, name)
}

// MustScope resolves from scope or panics.
func MustScope[T any](s Scope, name string) T {
	return di.MustScope[T](s, name)
}

// GetLogger resolves the logger from the container
// Returns the logger instance and an error if resolution fails.
func GetLogger(c Container) (Logger, error) {
	return di.GetLogger(c)
}

// GetMetrics resolves the metrics from the container
// Returns the metrics instance and an error if resolution fails.
func GetMetrics(c Container) (Metrics, error) {
	return di.GetMetrics(c)
}

// GetHealthManager resolves the health manager from the container
// Returns the health manager instance and an error if resolution fails.
func GetHealthManager(c Container) (HealthManager, error) {
	return di.GetHealthManager(c)
}
