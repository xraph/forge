package shared

import "maps"

// RegisterOption is a configuration option for service registration.
type RegisterOption struct {
	Lifecycle    string // "singleton", "scoped", or "transient"
	Dependencies []string
	Deps         []Dep // New: typed dependency specs with modes
	Metadata     map[string]string
	Groups       []string
}

// Singleton makes the service a singleton (default).
func Singleton() RegisterOption {
	return RegisterOption{Lifecycle: "singleton"}
}

// Transient makes the service created on each resolve.
func Transient() RegisterOption {
	return RegisterOption{Lifecycle: "transient"}
}

// Scoped makes the service live for the duration of a scope.
func Scoped() RegisterOption {
	return RegisterOption{Lifecycle: "scoped"}
}

// WithDependencies declares explicit dependencies (string-based, backward compatible).
// All dependencies are treated as eager.
func WithDependencies(deps ...string) RegisterOption {
	return RegisterOption{Dependencies: deps}
}

// WithDeps declares dependencies with full Dep specs (modes, types).
// This is the new, more powerful API for declaring dependencies.
func WithDeps(deps ...Dep) RegisterOption {
	return RegisterOption{Deps: deps}
}

// WithDIMetadata adds diagnostic metadata to DI service registration.
func WithDIMetadata(key, value string) RegisterOption {
	return RegisterOption{Metadata: map[string]string{key: value}}
}

// WithGroup adds service to a named group.
func WithGroup(group string) RegisterOption {
	return RegisterOption{Groups: []string{group}}
}

// MergeOptions combines multiple options.
func MergeOptions(opts []RegisterOption) RegisterOption {
	result := RegisterOption{
		Lifecycle: "singleton", // default
		Metadata:  make(map[string]string),
	}

	for _, opt := range opts {
		if opt.Lifecycle != "" {
			result.Lifecycle = opt.Lifecycle
		}

		// Merge string-based dependencies (backward compatibility)
		result.Dependencies = append(result.Dependencies, opt.Dependencies...)

		// Merge Dep specs
		result.Deps = append(result.Deps, opt.Deps...)

		maps.Copy(result.Metadata, opt.Metadata)

		result.Groups = append(result.Groups, opt.Groups...)
	}

	return result
}

// GetAllDeps returns all dependencies as Dep specs.
// Converts string-based dependencies to eager Deps for unified handling.
func (o RegisterOption) GetAllDeps() []Dep {
	// Start with explicit Dep specs
	allDeps := make([]Dep, 0, len(o.Deps)+len(o.Dependencies))
	allDeps = append(allDeps, o.Deps...)

	// Convert string dependencies to eager Deps
	for _, name := range o.Dependencies {
		allDeps = append(allDeps, Eager(name))
	}

	return allDeps
}

// GetAllDepNames returns all dependency names (for graph building).
func (o RegisterOption) GetAllDepNames() []string {
	names := make([]string, 0, len(o.Deps)+len(o.Dependencies))

	// Add names from Dep specs
	for _, dep := range o.Deps {
		names = append(names, dep.Name)
	}

	// Add string-based dependencies
	names = append(names, o.Dependencies...)

	return names
}
