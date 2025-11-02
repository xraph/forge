# Config Auto-Discovery Examples

This directory demonstrates Forge's automatic configuration file discovery and loading, similar to how Next.js handles environment files.

## Features

- **Automatic Discovery**: Searches for `config.yaml` and `config.local.yaml` automatically
- **Local Overrides**: `config.local.yaml` overrides `config.yaml` (add to `.gitignore`)
- **Monorepo Support**: App-scoped configs with `apps.{app-name}` sections
- **Hierarchical Search**: Searches current directory and parent directories
- **Next.js-like Behavior**: Familiar pattern for web developers

## Directory Structure

```
config-autodiscovery/
├── single-app/           # Single application example
│   ├── config.yaml       # Base config (committed)
│   ├── config.local.yaml # Local overrides (gitignored)
│   └── main.go
│
└── monorepo/             # Monorepo with multiple apps
    ├── config.yaml       # Root config for all apps
    ├── config.local.yaml # Local overrides for all apps
    └── apps/
        ├── api-service/
        │   └── main.go
        ├── admin-dashboard/
        │   └── main.go
        └── worker-service/
            └── main.go
```

## Single-App Pattern

### Directory Layout
```
my-app/
├── config.yaml       # Base configuration
├── config.local.yaml # Local overrides (in .gitignore)
└── main.go
```

### Usage
```go
app := forge.NewApp(forge.AppConfig{
    Name:    "my-app",
    Version: "1.0.0",
    // Config auto-discovery is enabled by default
    // No need to manually load config files!
})

// Access configuration
cfg := app.Config()
dbHost := cfg.GetString("database.host")
```

### How It Works
1. Forge searches for `config.yaml` or `config.yml`
2. Loads base configuration
3. Searches for `config.local.yaml` or `config.local.yml`
4. Merges local config over base (local takes precedence)

### Example Config Files

**config.yaml** (committed to git):
```yaml
database:
  host: db.production.com
  port: 5432

cache:
  driver: redis
  url: redis://cache.production.com:6379
```

**config.local.yaml** (in .gitignore):
```yaml
database:
  host: localhost  # Override for local dev

cache:
  driver: inmemory # Override for local dev
```

## Monorepo Pattern

### Directory Layout
```
monorepo/
├── config.yaml           # Root config with all apps
├── config.local.yaml     # Local overrides
└── apps/
    ├── api-service/
    │   └── main.go
    ├── admin-dashboard/
    │   └── main.go
    └── worker-service/
        └── main.go
```

### Usage in Each App
```go
// In apps/api-service/main.go
app := forge.NewApp(forge.AppConfig{
    Name:    "api-service",  // Matches apps.api-service in config
    Version: "1.0.0",
    // App-scoped config extraction is enabled by default
})
```

### How It Works
1. Forge searches up the directory tree for `config.yaml`
2. Finds root `config.yaml` and loads it
3. Extracts `apps.api-service` section
4. Merges global settings with app-specific settings
5. Applies local overrides from `config.local.yaml`

### Config Merge Order
```
Global Config (database, cache, logging)
    ↓
App-Scoped Config (apps.api-service)
    ↓
Local Overrides (config.local.yaml)
    ↓
Final Configuration
```

### Example Monorepo Config

**config.yaml**:
```yaml
# Global settings for all apps
database:
  driver: postgres
  host: db.company.com
  port: 5432

cache:
  driver: redis

# App-specific configurations
apps:
  api-service:
    app:
      port: 8080
    database:
      name: api_service_db
    api:
      rate_limit: 1000

  admin-dashboard:
    app:
      port: 8081
    database:
      name: admin_db
    admin:
      session_timeout: 30m
```

**config.local.yaml**:
```yaml
# Override for local development
database:
  host: localhost

apps:
  api-service:
    database:
      name: api_service_dev
    cache:
      driver: inmemory

  admin-dashboard:
    database:
      name: admin_dev
```

## Configuration Options

You can customize auto-discovery behavior:

```go
app := forge.NewApp(forge.AppConfig{
    Name:    "my-app",
    Version: "1.0.0",
    
    // Customize auto-discovery
    EnableConfigAutoDiscovery: true,  // Default: true
    EnableAppScopedConfig:     true,  // Default: true
    ConfigBaseNames:   []string{"config.yaml", "config.yml"},
    ConfigLocalNames:  []string{"config.local.yaml", "config.local.yml"},
    ConfigSearchPaths: []string{"/path/to/configs"},
})
```

## Running the Examples

### Single-App Example
```bash
cd examples/config-autodiscovery/single-app
go run main.go

# Visit http://localhost:8080/config
```

### Monorepo Example

Run API Service:
```bash
cd examples/config-autodiscovery/monorepo/apps/api-service
go run main.go

# Visit http://localhost:8080/config
```

Run Admin Dashboard:
```bash
cd examples/config-autodiscovery/monorepo/apps/admin-dashboard
go run main.go

# Visit http://localhost:8081/config
```

Run Worker Service:
```bash
cd examples/config-autodiscovery/monorepo/apps/worker-service
go run main.go

# Visit http://localhost:8082/config
```

## Best Practices

### 1. Use `.gitignore`
Always add local config files to `.gitignore`:
```gitignore
config.local.yaml
config.local.yml
*.local.yaml
*.local.yml
```

### 2. Document Config Structure
Provide a `config.example.yaml` or document all config keys:
```yaml
# config.example.yaml - Copy to config.local.yaml for local development
database:
  host: localhost
  port: 5432
  name: myapp_dev
```

### 3. Use Environment Variables for Secrets
Never commit secrets. Use environment variable expansion:
```yaml
database:
  password: ${DB_PASSWORD}  # Loaded from environment
  
  api_key: ${API_KEY}       # Loaded from environment
```

### 4. Monorepo Organization
```yaml
# Shared global settings
database: &database-defaults
  driver: postgres
  port: 5432

# App-specific configs
apps:
  api-service:
    database:
      <<: *database-defaults
      name: api_db
```

### 5. Config Validation
Always validate required config:
```go
cfg := app.Config()

// Validate required settings
if !cfg.IsSet("database.host") {
    log.Fatal("database.host is required")
}
```

## Troubleshooting

### Config Not Found
If config files aren't being discovered:

```go
// Enable debug logging to see search paths
app := forge.NewApp(forge.AppConfig{
    Name: "my-app",
    Logger: forge.NewBeautifulLogger("my-app"),
})

// Check search info
fmt.Println(config.GetConfigSearchInfo("my-app"))
```

### Wrong Config Loaded
Verify the merge order and precedence:
1. Base config (`config.yaml`) is loaded first
2. Local config (`config.local.yaml`) overrides base
3. App-scoped config (`apps.{name}`) overrides global
4. Local app-scoped overrides everything

### App-Scoped Not Working
Ensure:
1. App name matches the key in `apps.{name}`
2. `EnableAppScopedConfig` is `true` (default)
3. You're using the correct config structure:
   ```yaml
   apps:
     your-app-name:  # Must match AppConfig.Name
       # your config here
   ```

## Comparison with Next.js

| Feature | Next.js | Forge |
|---------|---------|-------|
| Base file | `.env` | `config.yaml` |
| Local overrides | `.env.local` | `config.local.yaml` |
| Environment-specific | `.env.production` | `config.{env}.yaml` |
| Monorepo support | Built-in | Built-in with `apps.{name}` |
| Auto-discovery | ✅ | ✅ |
| Hierarchical search | ✅ | ✅ |

## Additional Resources

- [Forge Configuration Guide](../../docs/configuration.md)
- [Environment Variables](../../docs/environment-variables.md)
- [Monorepo Best Practices](../../docs/monorepo.md)

