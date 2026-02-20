package forge

import (
	"github.com/xraph/forge/internal/health"
	"github.com/xraph/forge/internal/shared"
	"github.com/xraph/vessel"
)

// GetLogger resolves the logger from the container
// Returns the logger instance and an error if resolution fails.
func GetLogger(c Container) (Logger, error) {
	return vessel.Inject[Logger](c)
}

// GetMetrics resolves the metrics from the container
// Returns the metrics instance and an error if resolution fails.
func GetMetrics(c Container) (Metrics, error) {
	return vessel.Inject[Metrics](c)
}

// GetHealthManager resolves the health manager from the container
// Returns the health manager instance and an error if resolution fails.
func GetHealthManager(c Container) (HealthManager, error) {
	return health.GetManager(c)
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
type LazyRef[T any] = vessel.Lazy[T]

// OptionalLazyRef wraps an optional dependency that is resolved on first access.
// Returns nil without error if the dependency is not found.
type OptionalLazyRef[T any] = vessel.OptionalLazy[T]

// ProviderRef wraps a dependency that creates new instances on each access.
// This is useful for transient dependencies where a fresh instance is needed each time.
type ProviderRef[T any] = vessel.Provider[T]

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

// Inject creates an eager injection option for a dependency.
// The dependency is resolved immediately when the service is created.
//
// Usage:
//
//	forge.Provide(c, "userService",
//	    forge.Inject[*bun.DB](c),
//	    func(db *bun.DB) (*UserService, error) { ... },
//	)
func Inject[T any](c Container) (T, error) {
	return vessel.Inject[T](c)
}

// MustInject resolves a dependency and panics if it fails.
// The dependency is resolved immediately when the service is created.
//
// Usage:
//
//	forge.Provide(c, "userService",
//	    forge.MustInject[*bun.DB]("database"),
//	    func(db *bun.DB) (*UserService, error) { ... },
//	)
func MustInject[T any](c Container) T {
	return vessel.MustInject[T](c)
}

// Provide registers a constructor function with automatic dependency resolution.
// Dependencies are inferred from function parameters and all return types (except error)
// are registered as services.
//
// This follows the Uber dig pattern for constructor-based dependency injection:
//   - Function parameters become dependencies (resolved by type)
//   - Return types become provided services
//   - Error return type is handled for construction failures
//
// Example:
//
//	// Simple constructor
//	func NewUserService(db *Database, logger *Logger) *UserService {
//	    return &UserService{db: db, logger: logger}
//	}
//	Provide(c, NewUserService)
//
//	// Constructor with error
//	func NewDatabase(config *Config) (*Database, error) {
//	    return sql.Open(config.Driver, config.DSN)
//	}
//	Provide(c, NewDatabase)
//
//	// Using In struct for many dependencies
//	type ServiceParams struct {
//	    vessel.In
//	    DB     *Database
//	    Logger *Logger `optional:"true"`
//	}
//	func NewService(p ServiceParams) *Service {
//	    return &Service{db: p.DB, logger: p.Logger}
//	}
//	Provide(c, NewService)
func Provide(c Container, constructor any, opts ...ProvideOption) error {
	return vessel.Provide(c, constructor, opts...)
}

// =============================================================================
// Constructor Injection (Type-Based DI)
// =============================================================================

// ProvideConstructor registers a service constructor with automatic type-based dependency resolution.
// Dependencies are resolved by their return types, making this the cleanest DI pattern.
//
// Usage:
//
//	func NewDatabase(dsn string) *Database { return &Database{dsn: dsn} }
//	func NewUserService(db *Database, log forge.Logger) *UserService {
//	    return &UserService{db: db, log: log}
//	}
//
//	// Register constructors - dependencies auto-resolved by type
//	forge.ProvideConstructor(c, NewDatabase)
//	forge.ProvideConstructor(c, NewUserService)
//
//	// Resolve by type
//	userService, err := forge.InjectType[*UserService](c)
func ProvideConstructor(c Container, constructor any, opts ...vessel.ConstructorOption) error {
	return vessel.Provide(c, constructor, opts...)
}

// ProvideValue registers a pre-built instance as a singleton service.
// The instance is registered by its type and can be resolved with Inject[T].
//
// Example:
//
//	cfg := &Config{Port: 8080}
//	ProvideValue(c, cfg)
//
//	// Later:
//	config, _ := Inject[*Config](c)
func ProvideValue[T any](c Container, value T, opts ...ProvideOption) error {
	return vessel.ProvideValue(c, value, opts...)
}

// InjectType resolves a service by its type.
// This is used with constructor injection to resolve services without string keys.
//
// Usage:
//
//	db, err := forge.InjectType[*Database](c)
//	userService, err := forge.InjectType[*UserService](c)
func InjectType[T any](c Container) (T, error) {
	return vessel.Inject[T](c)
}

// InjectNamed resolves a named service by type.
// Used when you have multiple instances of the same type.
//
// Usage:
//
//	forge.ProvideConstructor(c, NewPrimaryDB, vessel.WithName("primary"))
//	forge.ProvideConstructor(c, NewReplicaDB, vessel.WithName("replica"))
//
//	primary, err := forge.InjectNamed[*Database](c, "primary")
//	replica, err := forge.InjectNamed[*Database](c, "replica")
func InjectNamed[T any](c Container, name string) (T, error) {
	return vessel.InjectNamed[T](c, name)
}

// MustInjectNamed resolves a named service by type or panics.
func MustInjectNamed[T any](c Container, name string) T {
	return vessel.MustInjectNamed[T](c, name)
}

// InjectGroup resolves all services in a group by type.
// Returns a slice of all services registered with the same group name.
//
// Usage:
//
//	forge.ProvideConstructor(c, NewHandler1, vessel.AsGroup("handlers"))
//	forge.ProvideConstructor(c, NewHandler2, vessel.AsGroup("handlers"))
//
//	handlers, err := forge.InjectGroup[Handler](c, "handlers")
func InjectGroup[T any](c Container, groupName string) ([]T, error) {
	return vessel.InjectGroup[T](c, groupName)
}

// MustInjectGroup resolves a group by type or panics.
func MustInjectGroup[T any](c Container, groupName string) []T {
	return vessel.MustInjectGroup[T](c, groupName)
}

// HasType checks if a service of the given type is registered.
func HasType[T any](c Container) bool {
	return vessel.HasType[T](c)
}

// HasTypeNamed checks if a named service of the given type is registered.
func HasTypeNamed[T any](c Container, name string) bool {
	return vessel.HasTypeNamed[T](c, name)
}

// =============================================================================
// Named Resolution (Backward-Compatible Helpers)
// =============================================================================

// Resolve resolves a named service by type from the container.
// This is a convenience wrapper for InjectNamed.
//
// Usage:
//
//	repo, err := forge.Resolve[*UserRepository](c, "userRepo")
func Resolve[T any](c Container, name string) (T, error) {
	return vessel.InjectNamed[T](c, name)
}

// Must resolves a named service by type or panics.
// Only use during application startup where a panic is acceptable.
//
// Usage:
//
//	repo := forge.Must[*UserRepository](c, "userRepo")
func Must[T any](c Container, name string) T {
	return vessel.MustInjectNamed[T](c, name)
}

// =============================================================================
// Named Registration (Backward-Compatible Helpers)
// =============================================================================

// RegisterSingleton registers a named singleton service with the container.
// The factory receives the container and returns the service instance.
//
// Usage:
//
//	forge.RegisterSingleton(c, "userRepo", func(c forge.Container) (*UserRepo, error) {
//	    db, err := forge.Inject[*sql.DB](c)
//	    if err != nil { return nil, err }
//	    return NewUserRepository(db), nil
//	})
func RegisterSingleton[T any](c Container, name string, factory func(Container) (T, error)) error {
	return vessel.ProvideNamed(c, name, func() (T, error) {
		return factory(c)
	}, vessel.AsSingleton())
}

// RegisterSingletonWith is an alias for RegisterSingleton.
//
// Deprecated: Use RegisterSingleton instead.
func RegisterSingletonWith[T any](c Container, name string, factory func(Container) (T, error)) error {
	return RegisterSingleton[T](c, name, factory)
}

// RegisterTransient registers a named transient service with the container.
// A new instance is created on every resolution.
//
// Usage:
//
//	forge.RegisterTransient[*RequestLogger](c, "requestLogger",
//	    func(c forge.Container) (*RequestLogger, error) {
//	        return NewRequestLogger(), nil
//	    },
//	)
func RegisterTransient[T any](c Container, name string, factory func(Container) (T, error)) error {
	return vessel.ProvideNamed(c, name, func() (T, error) {
		return factory(c)
	}, vessel.AsTransient())
}

// RegisterScoped registers a named scoped service with the container.
// One instance is created per scope (e.g., per HTTP request).
//
// Usage:
//
//	forge.RegisterScoped[*Transaction](c, "transaction",
//	    func(c forge.Container) (*Transaction, error) {
//	        db, err := forge.Inject[*sql.DB](c)
//	        if err != nil { return nil, err }
//	        tx, _ := db.Begin()
//	        return &Transaction{tx: tx}, nil
//	    },
//	)
func RegisterScoped[T any](c Container, name string, factory func(Container) (T, error)) error {
	return vessel.ProvideNamed(c, name, func() (T, error) {
		return factory(c)
	}, vessel.AsScoped())
}

// RegisterValue registers a pre-built instance as a named singleton service.
// The value is registered by its type under the given name.
//
// Usage:
//
//	cfg := &AppSettings{Debug: true}
//	forge.RegisterValue[*AppSettings](c, "settings", cfg)
//
//	// Later:
//	settings, _ := forge.Resolve[*AppSettings](c, "settings")
func RegisterValue[T any](c Container, name string, value T) error {
	return vessel.ProvideValue[T](c, value, vessel.WithName(name))
}

// Registration options (Singleton, Transient, Scoped, WithDependencies,
// WithGroup, WithDIMetadata) are defined in di_opts.go.
