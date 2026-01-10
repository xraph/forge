package shared

import "reflect"

// DepMode specifies how a dependency should be resolved.
type DepMode int

const (
	// DepEager resolves the dependency immediately during service creation.
	// Fails if the dependency is not found.
	DepEager DepMode = iota

	// DepLazy defers resolution until the dependency is first accessed.
	// Useful for breaking circular dependencies or expensive services.
	DepLazy

	// DepOptional resolves immediately but returns nil if not found.
	// Does not fail if the dependency is missing.
	DepOptional

	// DepLazyOptional combines lazy resolution with optional behavior.
	// Defers resolution and returns nil if not found on access.
	DepLazyOptional
)

// String returns the string representation of the DepMode.
func (m DepMode) String() string {
	switch m {
	case DepEager:
		return "eager"
	case DepLazy:
		return "lazy"
	case DepOptional:
		return "optional"
	case DepLazyOptional:
		return "lazy_optional"
	default:
		return "unknown"
	}
}

// IsLazy returns true if the mode involves lazy resolution.
func (m DepMode) IsLazy() bool {
	return m == DepLazy || m == DepLazyOptional
}

// IsOptional returns true if the mode allows missing dependencies.
func (m DepMode) IsOptional() bool {
	return m == DepOptional || m == DepLazyOptional
}

// Dep represents a dependency specification for a service.
// It describes what service is needed, what type it should be,
// and how it should be resolved (eager, lazy, optional).
type Dep struct {
	// Name is the service name in the container
	Name string

	// Type is the expected type (for validation), can be nil
	Type reflect.Type

	// Mode specifies how the dependency should be resolved
	Mode DepMode
}

// Eager creates an eager dependency specification.
// The dependency is resolved immediately and fails if not found.
func Eager(name string) Dep {
	return Dep{
		Name: name,
		Mode: DepEager,
	}
}

// EagerTyped creates an eager dependency with type information.
func EagerTyped[T any](name string) Dep {
	var zero T

	return Dep{
		Name: name,
		Type: reflect.TypeOf(zero),
		Mode: DepEager,
	}
}

// Lazy creates a lazy dependency specification.
// The dependency is resolved on first access.
func Lazy(name string) Dep {
	return Dep{
		Name: name,
		Mode: DepLazy,
	}
}

// LazyTyped creates a lazy dependency with type information.
func LazyTyped[T any](name string) Dep {
	var zero T

	return Dep{
		Name: name,
		Type: reflect.TypeOf(zero),
		Mode: DepLazy,
	}
}

// Optional creates an optional dependency specification.
// The dependency is resolved immediately but returns nil if not found.
func Optional(name string) Dep {
	return Dep{
		Name: name,
		Mode: DepOptional,
	}
}

// OptionalTyped creates an optional dependency with type information.
func OptionalTyped[T any](name string) Dep {
	var zero T

	return Dep{
		Name: name,
		Type: reflect.TypeOf(zero),
		Mode: DepOptional,
	}
}

// LazyOptional creates a lazy optional dependency specification.
// The dependency is resolved on first access and returns nil if not found.
func LazyOptional(name string) Dep {
	return Dep{
		Name: name,
		Mode: DepLazyOptional,
	}
}

// LazyOptionalTyped creates a lazy optional dependency with type information.
func LazyOptionalTyped[T any](name string) Dep {
	var zero T

	return Dep{
		Name: name,
		Type: reflect.TypeOf(zero),
		Mode: DepLazyOptional,
	}
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
	names := make([]string, len(deps))
	for i, dep := range deps {
		names[i] = dep.Name
	}

	return names
}

// DepsFromNames converts a slice of names to eager Dep specs.
// Useful for backward compatibility with old []string dependencies.
func DepsFromNames(names []string) []Dep {
	deps := make([]Dep, len(names))
	for i, name := range names {
		deps[i] = Eager(name)
	}

	return deps
}
