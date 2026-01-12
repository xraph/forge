package forge

import (
	"context"

	"github.com/xraph/forge/internal/shared"
	"github.com/xraph/vessel"
)

// Resolve with type safety.
func Resolve[T any](c Container, name string) (T, error) {
	return vessel.Resolve[T](c, name)
}

// Must resolves or panics - use only during startup.
func Must[T any](c Container, name string) T {
	return vessel.Must[T](c, name)
}

// ResolveReady resolves a service with type safety, ensuring it and its dependencies are started first.
// This is useful during extension Register() phase when you need a dependency
// to be fully initialized before use.
//
// Example usage in an extension's Register() method:
//
//	func (e *MyExtension) Register(app forge.App) error {
//	    ctx := context.Background()
//	    dbManager, err := forge.ResolveReady[*database.DatabaseManager](ctx, app.Container(), database.ManagerKey)
//	    if err != nil {
//	        return fmt.Errorf("database required: %w", err)
//	    }
//	    // dbManager is now fully started with open connections
//	    e.redis, _ = dbManager.Redis("cache")
//	    return nil
//	}
func ResolveReady[T any](ctx context.Context, c Container, name string) (T, error) {
	return vessel.ResolveReady[T](ctx, c, name)
}

// MustResolveReady resolves or panics, ensuring the service is started first.
// Use only during startup/registration phase.
func MustResolveReady[T any](ctx context.Context, c Container, name string) T {
	return vessel.MustResolveReady[T](ctx, c, name)
}

// RegisterSingleton is a convenience wrapper for singleton services.
func RegisterSingleton[T any](c Container, name string, factory func(Container) (T, error)) error {
	return vessel.RegisterSingleton[T](c, name, factory)
}

// RegisterTransient is a convenience wrapper for transient services.
func RegisterTransient[T any](c Container, name string, factory func(Container) (T, error)) error {
	return vessel.RegisterTransient[T](c, name, factory)
}

// RegisterScoped is a convenience wrapper for request-scoped services.
func RegisterScoped[T any](c Container, name string, factory func(Container) (T, error)) error {
	return vessel.RegisterScoped[T](c, name, factory)
}

// RegisterSingletonWith registers a singleton service with typed dependency injection.
// Accepts InjectOption arguments followed by a factory function.
//
// Usage:
//
//	forge.RegisterSingletonWith[*UserService](c, "userService",
//	    forge.Inject[*bun.DB]("database"),
//	    func(db *bun.DB) (*UserService, error) {
//	        return &UserService{db: db}, nil
//	    },
//	)
func RegisterSingletonWith[T any](c Container, name string, args ...any) error {
	return vessel.RegisterSingletonWith[T](c, name, args...)
}

// RegisterTransientWith registers a transient service with typed dependency injection.
// Accepts InjectOption arguments followed by a factory function.
//
// Usage:
//
//	forge.RegisterTransientWith[*Request](c, "request",
//	    forge.Inject[*Context]("ctx"),
//	    func(ctx *Context) (*Request, error) {
//	        return &Request{ctx: ctx}, nil
//	    },
//	)
func RegisterTransientWith[T any](c Container, name string, args ...any) error {
	return vessel.RegisterTransientWith[T](c, name, args...)
}

// RegisterScopedWith registers a scoped service with typed dependency injection.
// Accepts InjectOption arguments followed by a factory function.
//
// Usage:
//
//	forge.RegisterScopedWith[*Session](c, "session",
//	    forge.Inject[*User]("user"),
//	    func(user *User) (*Session, error) {
//	        return &Session{user: user}, nil
//	    },
//	)
func RegisterScopedWith[T any](c Container, name string, args ...any) error {
	return vessel.RegisterScopedWith[T](c, name, args...)
}

// RegisterInterface registers an implementation as an interface
// Supports all lifecycle options (Singleton, Scoped, Transient).
func RegisterInterface[I, T any](c Container, name string, factory func(Container) (T, error), opts ...RegisterOption) error {
	return vessel.RegisterInterface[I, T](c, name, factory, opts...)
}

// RegisterValue registers a pre-built instance (always singleton).
func RegisterValue[T any](c Container, name string, instance T) error {
	return vessel.RegisterValue[T](c, name, instance)
}

// RegisterSingletonInterface is a convenience wrapper.
func RegisterSingletonInterface[I, T any](c Container, name string, factory func(Container) (T, error)) error {
	return vessel.RegisterSingletonInterface[I, T](c, name, factory)
}

// RegisterScopedInterface is a convenience wrapper.
func RegisterScopedInterface[I, T any](c Container, name string, factory func(Container) (T, error)) error {
	return vessel.RegisterScopedInterface[I, T](c, name, factory)
}

// RegisterTransientInterface is a convenience wrapper.
func RegisterTransientInterface[I, T any](c Container, name string, factory func(Container) (T, error)) error {
	return vessel.RegisterTransientInterface[I, T](c, name, factory)
}

// ResolveScope is a helper for resolving from a scope.
func ResolveScope[T any](s Scope, name string) (T, error) {
	return vessel.ResolveScope[T](s, name)
}

// MustScope resolves from scope or panics.
func MustScope[T any](s Scope, name string) T {
	return vessel.MustScope[T](s, name)
}

// GetLogger resolves the logger from the container
// Returns the logger instance and an error if resolution fails.
func GetLogger(c Container) (Logger, error) {
	return vessel.GetLogger(c)
}

// GetMetrics resolves the metrics from the container
// Returns the metrics instance and an error if resolution fails.
func GetMetrics(c Container) (Metrics, error) {
	return vessel.GetMetrics(c)
}

// GetHealthManager resolves the health manager from the container
// Returns the health manager instance and an error if resolution fails.
func GetHealthManager(c Container) (HealthManager, error) {
	return vessel.GetHealthManager(c)
}

// =============================================================================
// Dependency Specification Types and Helpers
// =============================================================================

// Dep represents a dependency specification for a service.
// It describes what service is needed and how it should be resolved.
type Dep = shared.Dep

// DepMode specifies how a dependency should be resolved.
type DepMode = shared.DepMode

// Dependency mode constants.
const (
	// DepEager resolves the dependency immediately during service creation.
	DepEager = shared.DepEager
	// DepLazy defers resolution until the dependency is first accessed.
	DepLazy = shared.DepLazy
	// DepOptional resolves immediately but returns nil if not found.
	DepOptional = shared.DepOptional
	// DepLazyOptional combines lazy resolution with optional behavior.
	DepLazyOptional = shared.DepLazyOptional
)

// DepEagerSpec creates an eager dependency specification.
// The dependency is resolved immediately and fails if not found.
func DepEagerSpec(name string) Dep {
	return shared.Eager(name)
}

// DepLazySpec creates a lazy dependency specification.
// The dependency is resolved on first access.
func DepLazySpec(name string) Dep {
	return shared.Lazy(name)
}

// DepOptionalSpec creates an optional dependency specification.
// The dependency is resolved immediately but returns nil if not found.
func DepOptionalSpec(name string) Dep {
	return shared.Optional(name)
}

// DepLazyOptionalSpec creates a lazy optional dependency specification.
// The dependency is resolved on first access and returns nil if not found.
func DepLazyOptionalSpec(name string) Dep {
	return shared.LazyOptional(name)
}

// =============================================================================
// Lazy Wrapper Types
// =============================================================================

// LazyRef wraps a dependency that is resolved on first access.
// This is useful for breaking circular dependencies or deferring
// resolution of expensive services until they're actually needed.
type LazyRef[T any] = vessel.LazyRef[T]

// OptionalLazyRef wraps an optional dependency that is resolved on first access.
// Returns nil without error if the dependency is not found.
type OptionalLazyRef[T any] = vessel.OptionalLazy[T]

// ProviderRef wraps a dependency that creates new instances on each access.
// This is useful for transient dependencies where a fresh instance is needed each time.
type ProviderRef[T any] = vessel.Provider[T]

// LazyAny is a non-generic lazy wrapper used with LazyInject.
// Use this type in your factory function when using LazyInject[T].
type LazyAny = vessel.LazyAny

// OptionalLazyAny is a non-generic optional lazy wrapper used with LazyOptionalInject.
// Use this type in your factory function when using LazyOptionalInject[T].
type OptionalLazyAny = vessel.OptionalLazyAny

// NewLazyRef creates a new lazy dependency wrapper.
func NewLazyRef[T any](c Container, name string) *LazyRef[T] {
	return vessel.NewLazy[T](c, name)
}

// NewOptionalLazyRef creates a new optional lazy dependency wrapper.
func NewOptionalLazyRef[T any](c Container, name string) *OptionalLazyRef[T] {
	return vessel.NewOptionalLazy[T](c, name)
}

// NewProviderRef creates a new provider for transient dependencies.
func NewProviderRef[T any](c Container, name string) *ProviderRef[T] {
	return vessel.NewProvider[T](c, name)
}

// =============================================================================
// Typed Injection Helpers
// =============================================================================

// InjectOption represents a dependency injection option with type information.
type InjectOption = vessel.InjectOption

// Inject creates an eager injection option for a dependency.
// The dependency is resolved immediately when the service is created.
//
// Usage:
//
//	forge.Provide(c, "userService",
//	    forge.Inject[*bun.DB]("database"),
//	    func(db *bun.DB) (*UserService, error) { ... },
//	)
func Inject[T any](name string) InjectOption {
	return vessel.Inject[T](name)
}

// LazyInject creates a lazy injection option for a dependency.
// The dependency is resolved on first access via Lazy[T].Get().
func LazyInject[T any](name string) InjectOption {
	return vessel.LazyInject[T](name)
}

// OptionalInject creates an optional injection option for a dependency.
// The dependency is resolved immediately but returns nil if not found.
func OptionalInject[T any](name string) InjectOption {
	return vessel.OptionalInject[T](name)
}

// LazyOptionalInject creates a lazy optional injection option.
// The dependency is resolved on first access and returns nil if not found.
func LazyOptionalInject[T any](name string) InjectOption {
	return vessel.LazyOptionalInject[T](name)
}

// ProviderInject creates an injection option for a transient dependency provider.
func ProviderInject[T any](name string) InjectOption {
	return vessel.ProviderInject[T](name)
}

// Provide registers a service with typed dependency injection.
// It accepts InjectOption arguments followed by a factory function.
//
// The factory function receives the resolved dependencies in order and returns
// the service instance and an optional error.
//
// Usage:
//
//	forge.Provide[*UserService](c, "userService",
//	    forge.Inject[*bun.DB]("database"),
//	    forge.Inject[Logger]("logger"),
//	    forge.LazyInject[*Cache]("cache"),
//	    func(db *bun.DB, logger Logger, cache *forge.LazyRef[*Cache]) (*UserService, error) {
//	        return &UserService{db, logger, cache}, nil
//	    },
//	)
func Provide[T any](c Container, name string, args ...any) error {
	return vessel.Provide[T](c, name, args...)
}

// ProvideWithOpts is like Provide but accepts additional RegisterOptions.
func ProvideWithOpts[T any](c Container, name string, opts []RegisterOption, args ...any) error {
	return vessel.ProvideWithOpts[T](c, name, opts, args...)
}
