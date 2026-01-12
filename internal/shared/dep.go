package shared

import (
	"github.com/xraph/go-utils/di"
)

// DepMode specifies how a dependency should be resolved.
type DepMode = di.DepMode

const (
	// DepEager resolves the dependency immediately during service creation.
	// Fails if the dependency is not found.
	DepEager DepMode = di.DepEager

	// DepLazy defers resolution until the dependency is first accessed.
	// Useful for breaking circular dependencies or expensive services.
	DepLazy DepMode = di.DepLazy

	// DepOptional resolves immediately but returns nil if not found.
	// Does not fail if the dependency is missing.
	DepOptional DepMode = di.DepOptional

	// DepLazyOptional combines lazy resolution with optional behavior.
	// Defers resolution and returns nil if not found on access.
	DepLazyOptional DepMode = di.DepLazyOptional
)

// Dep represents a dependency specification for a service.
// It describes what service is needed, what type it should be,
// and how it should be resolved (eager, lazy, optional).
type Dep = di.Dep

// Eager creates an eager dependency specification.
// The dependency is resolved immediately and fails if not found.
func Eager(name string) Dep {
	return di.Eager(name)
}

// EagerTyped creates an eager dependency with type information.
func EagerTyped[T any](name string) Dep {
	return di.EagerTyped[T](name)
}

// Lazy creates a lazy dependency specification.
// The dependency is resolved on first access.
func Lazy(name string) Dep {
	return di.Lazy(name)
}

// LazyTyped creates a lazy dependency with type information.
func LazyTyped[T any](name string) Dep {
	return di.LazyTyped[T](name)
}

// Optional creates an optional dependency specification.
// The dependency is resolved immediately but returns nil if not found.
func Optional(name string) Dep {
	return di.Optional(name)
}

// OptionalTyped creates an optional dependency with type information.
func OptionalTyped[T any](name string) Dep {
	return di.OptionalTyped[T](name)
}

// LazyOptional creates a lazy optional dependency specification.
// The dependency is resolved on first access and returns nil if not found.
func LazyOptional(name string) Dep {
	return di.LazyOptional(name)
}

// LazyOptionalTyped creates a lazy optional dependency with type information.
func LazyOptionalTyped[T any](name string) Dep {
	return di.LazyOptionalTyped[T](name)
}

// DependencyProvider is implemented by services that declare their dependencies.
// The container will auto-discover and inject these dependencies.
type DependencyProvider interface {
	// Dependencies returns the list of dependencies this service requires.
	Dependencies() []Dep
}

// DepNames extracts just the names from a slice of Dep specs.
// Useful for backward compatibility with code expecting []string.
func DepNames(deps []Dep) []string {
	return di.DepNames(deps)
}

// DepsFromNames converts a slice of names to eager Dep specs.
// Useful for backward compatibility with old []string dependencies.
func DepsFromNames(names []string) []Dep {
	return di.DepsFromNames(names)
}
