# Extension Configuration Example

This example demonstrates how to use ConfigManager with Forge extensions.

## Features Demonstrated

1. **Dual-key configuration pattern**
   - Extensions try `extensions.{name}` first (preferred)
   - Fall back to `{name}` for v1 compatibility

2. **Programmatic overrides**
   - Options passed to `NewExtension()` override config file values

3. **Required configuration**
   - Use `WithRequireConfig(true)` to fail if config not found

## Configuration Files

See `config.yaml` for examples of:
- Namespaced pattern: `extensions.cache`, `extensions.mcp`
- Top-level pattern: `cache`, `mcp`

## Running the Examples

```bash
# Example 1: Load from config file
go run main.go

# Test the endpoints
curl http://localhost:8080/test
curl http://localhost:8080/_/mcp/info
```

## Code Examples

### Load from ConfigManager (default)

```go
app := forge.NewApp(forge.AppConfig{
    Extensions: []forge.Extension{
        cache.NewExtension(),  // Loads from config.yaml
        mcp.NewExtension(),    // Loads from config.yaml
    },
})
```

### Programmatic overrides

```go
app := forge.NewApp(forge.AppConfig{
    Extensions: []forge.Extension{
        cache.NewExtension(
            cache.WithDriver("redis"),
            cache.WithURL("redis://localhost:6379"),
        ),
    },
})
```

### Require configuration

```go
app := forge.NewApp(forge.AppConfig{
    Extensions: []forge.Extension{
        cache.NewExtension(
            cache.WithRequireConfig(true),  // Fails without config
        ),
    },
})
```

## Config Resolution Order

For the `cache` extension:

1. Try `extensions.cache` in ConfigManager
2. Try `cache` in ConfigManager (v1 compatibility)
3. Use programmatic config passed to constructor
4. Use default config
5. If `RequireConfig=true`, fail instead of using defaults

Programmatic options always override config file values.

