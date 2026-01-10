package di

import (
	"context"
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

// ResolveReady resolves a service with type safety, ensuring it and its dependencies are started first.
// This is useful during extension Register() phase when you need a dependency
// to be fully initialized before use.
func ResolveReady[T any](ctx context.Context, c Container, name string) (T, error) {
	var zero T

	instance, err := c.ResolveReady(ctx, name)
	if err != nil {
		return zero, err
	}

	typed, ok := instance.(T)
	if !ok {
		return zero, fmt.Errorf("%w: service %s is not of type %T", errors2.ErrTypeMismatch, name, zero)
	}

	return typed, nil
}

// MustResolveReady resolves or panics, ensuring the service is started first.
// Use only during startup/registration phase.
func MustResolveReady[T any](ctx context.Context, c Container, name string) T {
	instance, err := ResolveReady[T](ctx, c, name)
	if err != nil {
		panic(fmt.Sprintf("failed to resolve ready %s: %v", name, err))
	}

	return instance
}

// RegisterSingleton is a convenience wrapper for singleton services.
func RegisterSingleton[T any](c Container, name string, factory func(Container) (T, error)) error {
	return c.Register(name, func(c Container) (any, error) {
		return factory(c)
	}, Singleton())
}

// RegisterTransient is a convenience wrapper for transient services.
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

// RegisterSingletonWith registers a singleton service with typed dependency injection.
// Accepts InjectOption arguments followed by a factory function.
//
// Usage:
//
//	di.RegisterSingletonWith[*UserService](c, "userService",
//	    di.Inject[*bun.DB]("database"),
//	    func(db *bun.DB) (*UserService, error) {
//	        return &UserService{db: db}, nil
//	    },
//	)
func RegisterSingletonWith[T any](c Container, name string, args ...any) error {
	return registerWithLifecycle[T](c, name, Singleton(), args...)
}

// RegisterTransientWith registers a transient service with typed dependency injection.
// Accepts InjectOption arguments followed by a factory function.
//
// Usage:
//
//	di.RegisterTransientWith[*Request](c, "request",
//	    di.Inject[*Context]("ctx"),
//	    func(ctx *Context) (*Request, error) {
//	        return &Request{ctx: ctx}, nil
//	    },
//	)
func RegisterTransientWith[T any](c Container, name string, args ...any) error {
	return registerWithLifecycle[T](c, name, Transient(), args...)
}

// RegisterScopedWith registers a scoped service with typed dependency injection.
// Accepts InjectOption arguments followed by a factory function.
//
// Usage:
//
//	di.RegisterScopedWith[*Session](c, "session",
//	    di.Inject[*User]("user"),
//	    func(user *User) (*Session, error) {
//	        return &Session{user: user}, nil
//	    },
//	)
func RegisterScopedWith[T any](c Container, name string, args ...any) error {
	return registerWithLifecycle[T](c, name, Scoped(), args...)
}

// registerWithLifecycle handles typed injection patterns.
func registerWithLifecycle[T any](c Container, name string, lifecycle RegisterOption, args ...any) error {
	if len(args) == 0 {
		return fmt.Errorf("register %s: no factory function provided", name)
	}

	// Collect InjectOptions and find the factory function
	var (
		injectOpts   []InjectOption
		factoryFn    any
		registerOpts []RegisterOption
	)

	registerOpts = append(registerOpts, lifecycle)

	for _, arg := range args {
		switch v := arg.(type) {
		case InjectOption:
			injectOpts = append(injectOpts, v)
		case RegisterOption:
			registerOpts = append(registerOpts, v)
		default:
			// Assume it's the factory function
			if factoryFn != nil {
				return fmt.Errorf("register %s: multiple factory functions provided", name)
			}

			factoryFn = arg
		}
	}

	if factoryFn == nil {
		return fmt.Errorf("register %s: no factory function provided", name)
	}

	// Extract dependencies for the graph
	deps := ExtractDeps(injectOpts)

	// Create the wrapper factory that resolves dependencies
	factory := func(container Container) (any, error) {
		// Resolve all dependencies according to their modes
		resolvedDeps := make([]any, len(injectOpts))

		for i, opt := range injectOpts {
			resolved, err := resolveDep(container, opt)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve dependency %s: %w", opt.Dep.Name, err)
			}

			resolvedDeps[i] = resolved
		}

		// Call the factory function with resolved dependencies
		return callFactory(factoryFn, resolvedDeps)
	}

	// Merge deps into options
	if len(deps) > 0 {
		registerOpts = append(registerOpts, shared.WithDeps(deps...))
	}

	return c.Register(name, factory, registerOpts...)
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
