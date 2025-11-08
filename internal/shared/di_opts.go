package shared

import "maps"

// RegisterOption is a configuration option for service registration.
type RegisterOption struct {
	Lifecycle    string // "singleton", "scoped", or "transient"
	Dependencies []string
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

// WithDependencies declares explicit dependencies.
func WithDependencies(deps ...string) RegisterOption {
	return RegisterOption{Dependencies: deps}
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

		result.Dependencies = append(result.Dependencies, opt.Dependencies...)
		maps.Copy(result.Metadata, opt.Metadata)

		result.Groups = append(result.Groups, opt.Groups...)
	}

	return result
}
