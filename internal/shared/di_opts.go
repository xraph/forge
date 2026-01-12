package shared

import "github.com/xraph/go-utils/di"

// RegisterOption is a configuration option for service registration.
type RegisterOption = di.RegisterOption

// Singleton makes the service a singleton (default).
func Singleton() RegisterOption {
	return di.Singleton()
}

// Transient makes the service created on each resolve.
func Transient() RegisterOption {
	return di.Transient()
}

// Scoped makes the service live for the duration of a scope.
func Scoped() RegisterOption {
	return di.Scoped()
}

// WithDependencies declares explicit dependencies (string-based, backward compatible).
// All dependencies are treated as eager.
func WithDependencies(deps ...string) RegisterOption {
	return di.WithDependencies(deps...)
}

// WithDeps declares dependencies with full Dep specs (modes, types).
// This is the new, more powerful API for declaring dependencies.
func WithDeps(deps ...Dep) RegisterOption {
	return di.WithDeps(deps...)
}

// WithDIMetadata adds diagnostic metadata to DI service registration.
func WithDIMetadata(key, value string) RegisterOption {
	return di.WithDIMetadata(key, value)
}

// WithGroup adds service to a named group.
func WithGroup(group string) RegisterOption {
	return di.WithGroup(group)
}

// MergeOptions combines multiple options.
func MergeOptions(opts []RegisterOption) RegisterOption {
	return di.MergeOptions(opts)
}
