package forge

import "github.com/xraph/forge/v2/internal/shared"

// RegisterOption is a configuration option for service registration
type RegisterOption = shared.RegisterOption

// Singleton makes the service a singleton (default)
func Singleton() RegisterOption {
	return shared.Singleton()
}

// Transient makes the service created on each resolve
func Transient() RegisterOption {
	return shared.Transient()
}

// Scoped makes the service live for the duration of a scope
func Scoped() RegisterOption {
	return shared.Scoped()
}

// WithDependencies declares explicit dependencies
func WithDependencies(deps ...string) RegisterOption {
	return shared.WithDependencies(deps...)
}

// WithDIMetadata adds diagnostic metadata to DI service registration
func WithDIMetadata(key, value string) RegisterOption {
	return shared.WithDIMetadata(key, value)
}

// WithGroup adds service to a named group
func WithGroup(group string) RegisterOption {
	return shared.WithGroup(group)
}

// merge combines multiple options
func mergeOptions(opts []RegisterOption) RegisterOption {
	return shared.MergeOptions(opts)
}
