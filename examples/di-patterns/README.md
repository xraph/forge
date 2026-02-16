# DI Patterns Example

This example demonstrates both **key-based** (legacy) and **type-based** (new) dependency injection patterns in Forge.

## Patterns Comparison

### Type-Based DI (NEW - Recommended)

```go
// Register with constructor - dependencies auto-resolved by type
e.RegisterConstructor(func(logger forge.Logger, metrics forge.Metrics) (*MyService, error) {
    return NewMyService(logger, metrics), nil
})

// Resolve by type - no string keys needed
service, err := forge.InjectType[*MyService](container)
```

**Benefits:**
- ✅ Type-safe - compile-time checking
- ✅ Auto-dependency resolution
- ✅ IDE autocomplete support
- ✅ No string key management
- ✅ Easier refactoring

### Key-Based DI (OLD - Backward Compatible)

```go
// Register with string key
forge.Provide(container, func(c forge.Container) (*MyService, error) {
    return myService, nil
})

// Resolve by key
service, err := forge.Inject[*MyService](container)
```

**Drawbacks:**
- ❌ String keys can have typos
- ❌ No compile-time checking
- ❌ Manual dependency resolution
- ❌ Harder to refactor

## Core Services Available

Both patterns are supported for core services:

**Type-Based:**
```go
logger, _ := forge.InjectType[forge.Logger](container)
metrics, _ := forge.InjectType[forge.Metrics](container)
config, _ := forge.InjectType[forge.ConfigManager](container)
health, _ := forge.InjectType[forge.HealthManager](container)
router, _ := forge.InjectType[forge.Router](container)
```

**Key-Based:**
```go
logger, _ := forge.GetLogger(container)
metrics, _ := forge.GetMetrics(container)
config := app.Config()
health, _ := forge.GetHealthManager(container)
```

## Running the Example

```bash
go run main.go
```

Expected output shows both patterns working side-by-side.

## Migration Path

1. **New Extensions**: Use type-based DI from the start
2. **Existing Extensions**: Can migrate incrementally
3. **Backward Compatibility**: Both patterns will continue to work

## Documentation

- [Constructor Injection Pattern](../../docs/content/docs/extensions/constructor-injection.mdx)
- [Migration Guide](../../docs/content/docs/guides/extension-migration.mdx)

## Example Extensions

**Modern (Type-Based):**
- cache
- database  
- ai
- events
- queue

**Legacy (Key-Based):**
- Still fully supported
- Can be migrated at any time
