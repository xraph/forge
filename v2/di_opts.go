package forge

// RegisterOption is a configuration option for service registration
type RegisterOption struct {
	lifecycle    string // "singleton", "scoped", or "transient"
	dependencies []string
	metadata     map[string]string
	groups       []string
}

// Singleton makes the service a singleton (default)
func Singleton() RegisterOption {
	return RegisterOption{lifecycle: "singleton"}
}

// Transient makes the service created on each resolve
func Transient() RegisterOption {
	return RegisterOption{lifecycle: "transient"}
}

// Scoped makes the service live for the duration of a scope
func Scoped() RegisterOption {
	return RegisterOption{lifecycle: "scoped"}
}

// WithDependencies declares explicit dependencies
func WithDependencies(deps ...string) RegisterOption {
	return RegisterOption{dependencies: deps}
}

// WithDIMetadata adds diagnostic metadata to DI service registration
func WithDIMetadata(key, value string) RegisterOption {
	return RegisterOption{metadata: map[string]string{key: value}}
}

// WithGroup adds service to a named group
func WithGroup(group string) RegisterOption {
	return RegisterOption{groups: []string{group}}
}

// merge combines multiple options
func mergeOptions(opts []RegisterOption) RegisterOption {
	result := RegisterOption{
		lifecycle: "singleton", // default
		metadata:  make(map[string]string),
	}

	for _, opt := range opts {
		if opt.lifecycle != "" {
			result.lifecycle = opt.lifecycle
		}
		result.dependencies = append(result.dependencies, opt.dependencies...)
		for k, v := range opt.metadata {
			result.metadata[k] = v
		}
		result.groups = append(result.groups, opt.groups...)
	}

	return result
}
