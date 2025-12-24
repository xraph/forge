package di

import (
	"reflect"

	"github.com/xraph/forge/internal/shared"
)

// InjectOption represents a dependency injection option.
// It carries type information and the dependency specification.
type InjectOption struct {
	Dep      shared.Dep
	TypeInfo reflect.Type
}

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
	var zero T
	return InjectOption{
		Dep: shared.Dep{
			Name: name,
			Type: reflect.TypeOf(zero),
			Mode: shared.DepEager,
		},
		TypeInfo: reflect.TypeOf(zero),
	}
}

// LazyInject creates a lazy injection option for a dependency.
// The dependency is resolved on first access via Lazy[T].Get().
//
// Usage:
//
//	forge.Provide(c, "userService",
//	    forge.LazyInject[*Cache]("cache"),
//	    func(cache *forge.Lazy[*Cache]) (*UserService, error) { ... },
//	)
func LazyInject[T any](name string) InjectOption {
	var zero T
	return InjectOption{
		Dep: shared.Dep{
			Name: name,
			Type: reflect.TypeOf(zero),
			Mode: shared.DepLazy,
		},
		TypeInfo: reflect.TypeOf(zero),
	}
}

// OptionalInject creates an optional injection option for a dependency.
// The dependency is resolved immediately but returns nil if not found.
//
// Usage:
//
//	forge.Provide(c, "userService",
//	    forge.OptionalInject[*Tracer]("tracer"),
//	    func(tracer *Tracer) (*UserService, error) { ... },
//	)
func OptionalInject[T any](name string) InjectOption {
	var zero T
	return InjectOption{
		Dep: shared.Dep{
			Name: name,
			Type: reflect.TypeOf(zero),
			Mode: shared.DepOptional,
		},
		TypeInfo: reflect.TypeOf(zero),
	}
}

// LazyOptionalInject creates a lazy optional injection option.
// The dependency is resolved on first access and returns nil if not found.
//
// Usage:
//
//	forge.Provide(c, "userService",
//	    forge.LazyOptionalInject[*Analytics]("analytics"),
//	    func(analytics *forge.OptionalLazy[*Analytics]) (*UserService, error) { ... },
//	)
func LazyOptionalInject[T any](name string) InjectOption {
	var zero T
	return InjectOption{
		Dep: shared.Dep{
			Name: name,
			Type: reflect.TypeOf(zero),
			Mode: shared.DepLazyOptional,
		},
		TypeInfo: reflect.TypeOf(zero),
	}
}

// ProviderInject creates an injection option for a transient dependency provider.
// Each call to Provider[T].Provide() creates a new instance.
//
// Usage:
//
//	forge.Provide(c, "handler",
//	    forge.ProviderInject[*Request]("request"),
//	    func(reqProvider *forge.Provider[*Request]) (*Handler, error) { ... },
//	)
func ProviderInject[T any](name string) InjectOption {
	var zero T
	return InjectOption{
		Dep: shared.Dep{
			Name: name,
			Type: reflect.TypeOf(zero),
			Mode: shared.DepLazy, // Providers are inherently lazy
		},
		TypeInfo: reflect.TypeOf(zero),
	}
}

// ExtractDeps extracts dependency specifications from inject options.
func ExtractDeps(opts []InjectOption) []shared.Dep {
	deps := make([]shared.Dep, len(opts))
	for i, opt := range opts {
		deps[i] = opt.Dep
	}
	return deps
}

// ExtractDepNames extracts just the names from inject options.
func ExtractDepNames(opts []InjectOption) []string {
	names := make([]string, len(opts))
	for i, opt := range opts {
		names[i] = opt.Dep.Name
	}
	return names
}

