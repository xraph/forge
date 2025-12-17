# Feature Flags Extension

Enterprise-grade feature flags and A/B testing for Forge applications.

## Features

- ✅ **Multiple Providers** - Local, LaunchDarkly, Unleash, Flagsmith
- ✅ **Boolean Flags** - Simple on/off toggles
- ✅ **Multi-Variant Flags** - String, number, JSON flags
- ✅ **User Targeting** - Target specific users or groups
- ✅ **Percentage Rollouts** - Gradual feature rollouts
- ✅ **Real-time Updates** - Automatic flag refresh
- ✅ **Caching** - Local caching for performance
- ✅ **A/B Testing** - Experimentation support

## Installation

```bash
go get github.com/xraph/forge/extensions/features
```

## Quick Start

### Basic Usage

```go
package main

import (
    "github.com/xraph/forge"
    "github.com/xraph/forge/extensions/features"
)

func main() {
    app := forge.NewApp(forge.AppConfig{
        Extensions: []forge.Extension{
            features.NewExtension(
                features.WithEnabled(true),
                features.WithLocalFlags(map[string]features.FlagConfig{
                    "new-checkout": {
                        Key:         "new-checkout",
                        Name:        "New Checkout Flow",
                        Description: "Enable new checkout experience",
                        Type:        "boolean",
                        Enabled:     true,
                    },
                    "max-cart-items": {
                        Key:         "max-cart-items",
                        Name:        "Maximum Cart Items",
                        Type:        "number",
                        Value:       100,
                    },
                }),
            ),
        },
    })

    // Get features service using helper
    featuresService := features.MustGetFromApp(app)

    // Check if feature is enabled
    router := app.Router()
    router.POST("/checkout", func(ctx forge.Context) error {
        userCtx := features.NewUserContext(ctx.UserID()).
            WithEmail(ctx.User().Email)

        if featuresService.IsEnabled(ctx.Context(), "new-checkout", userCtx) {
            return handleNewCheckout(ctx)
        }
        return handleLegacyCheckout(ctx)
    })

    app.Run()
}
```

## Configuration

### YAML Configuration

```yaml
extensions:
  features:
    enabled: true
    provider: "local"
    refresh_interval: 30s
    enable_cache: true
    cache_ttl: 5m

    # Local provider (for development)
    local:
      flags:
        new-checkout:
          key: "new-checkout"
          name: "New Checkout Flow"
          type: "boolean"
          enabled: true
          targeting:
            - attribute: "email"
              operator: "contains"
              values: ["@company.com"]
              value: true
          rollout:
            percentage: 50
            attribute: "user_id"

        theme:
          key: "theme"
          name: "UI Theme"
          type: "string"
          value: "dark"

    # Default flag values
    default_flags:
      new-checkout: false
      max-cart-items: 100
```

### Programmatic Configuration

```go
features.NewExtension(
    features.WithEnabled(true),
    features.WithProvider("local"),
    features.WithRefreshInterval(30 * time.Second),
    features.WithCache(true, 5 * time.Minute),
    features.WithLocalFlags(map[string]features.FlagConfig{
        // ... flags
    }),
)
```

## Usage Examples

### Boolean Flags

```go
// Check if feature is enabled
userCtx := features.NewUserContext("user-123").
    WithEmail("user@example.com").
    WithGroups([]string{"beta-testers"})

if service.IsEnabled(ctx, "new-feature", userCtx) {
    // New feature enabled
}

// With custom default
enabled := service.IsEnabledWithDefault(ctx, "experimental-feature", userCtx, false)
```

### String Flags

```go
// Get string flag
theme := service.GetString(ctx, "theme", userCtx, "light")

// Use in templates
return ctx.Render("dashboard.html", map[string]interface{}{
    "theme": theme,
})
```

### Number Flags

```go
// Get number flag
maxItems := service.GetInt(ctx, "max-cart-items", userCtx, 50)
pageSize := service.GetInt(ctx, "page-size", userCtx, 20)
```

### JSON Flags

```go
// Get complex configuration
config := service.GetJSON(ctx, "api-config", userCtx, map[string]interface{}{
    "timeout": 30,
    "retry": 3,
})
```

### Get All Flags

```go
// Get all flags for a user
allFlags, err := service.GetAllFlags(ctx, userCtx)
if err != nil {
    return err
}

// Return to frontend
return ctx.JSON(200, map[string]interface{}{
    "flags": allFlags,
})
```

## Targeting Rules

### User Targeting

```go
flags:
  vip-features:
    targeting:
      - attribute: "email"
        operator: "in"
        values: ["admin@company.com", "ceo@company.com"]
        value: true
      
      - attribute: "group"
        operator: "in"
        values: ["vip", "enterprise"]
        value: true
```

### Percentage Rollout

```go
flags:
  new-ui:
    rollout:
      percentage: 25  # 25% of users
      attribute: "user_id"  # Consistent hashing on user_id
```

### Combined Targeting

```go
flags:
  beta-feature:
    targeting:
      # Enable for beta testers
      - attribute: "group"
        operator: "in"
        values: ["beta-testers"]
        value: true
    
    rollout:
      # Plus 10% of regular users
      percentage: 10
      attribute: "user_id"
```

## Providers

### Local Provider (Development)

```go
features.NewExtension(
    features.WithProvider("local"),
    features.WithLocalFlags(localFlags),
)
```

### LaunchDarkly (Production)

```go
features.NewExtension(
    features.WithLaunchDarkly(os.Getenv("LAUNCHDARKLY_SDK_KEY")),
)
```

### Unleash

```go
features.NewExtension(
    features.WithUnleash(
        "https://unleash.company.com",
        os.Getenv("UNLEASH_API_TOKEN"),
        "my-app",
    ),
)
```

### Flagsmith

```go
features.NewExtension(
    features.WithFlagsmith(
        "https://flagsmith.company.com",
        os.Getenv("FLAGSMITH_ENV_KEY"),
    ),
)
```

## Middleware Integration

```go
// Feature flag middleware
func FeatureFlagMiddleware(flagKey string, service *features.Service) forge.Middleware {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            userCtx := features.NewUserContext(getUserID(r))
            
            if !service.IsEnabled(r.Context(), flagKey, userCtx) {
                http.Error(w, "Feature not available", http.StatusNotFound)
                return
            }
            
            next.ServeHTTP(w, r)
        })
    }
}

// Use in routes
router.Use(FeatureFlagMiddleware("api-v2", featuresService))
```

## Best Practices

### 1. Use Descriptive Flag Names

```go
// ✅ Good
"new-checkout-flow"
"enable-email-notifications"
"max-upload-size"

// ❌ Bad
"flag1"
"new-feature"
"temp"
```

### 2. Always Provide Defaults

```go
// ✅ Good - provides fallback
enabled := service.IsEnabledWithDefault(ctx, "feature", userCtx, false)

// ❌ Bad - panics if flag not found
enabled := service.IsEnabled(ctx, "feature", userCtx)
```

### 3. Use User Context

```go
// ✅ Good - provides targeting context
userCtx := features.NewUserContext(user.ID).
    WithEmail(user.Email).
    WithGroups(user.Groups).
    WithAttribute("plan", user.Plan)

// ❌ Bad - no targeting possible
userCtx := nil
```

### 4. Cache Flag Values (When Appropriate)

```go
// For frequently checked flags in hot paths
var cachedFlag atomic.Bool

go func() {
    ticker := time.NewTicker(10 * time.Second)
    for range ticker.C {
        enabled := service.IsEnabled(ctx, "hot-feature", userCtx)
        cachedFlag.Store(enabled)
    }
}()
```

### 5. Clean Up Old Flags

```go
// Remove flags after full rollout
// Document flag lifecycle in code comments

// TODO: Remove this flag after 2025-06-01
if service.IsEnabled(ctx, "temp-feature", userCtx) {
    // ...
}
```

## Testing

```go
func TestFeatureFlag(t *testing.T) {
    // Create test provider
    provider := providers.NewLocalProvider(
        features.LocalProviderConfig{
            Flags: map[string]features.FlagConfig{
                "test-flag": {
                    Key:     "test-flag",
                    Type:    "boolean",
                    Enabled: true,
                },
            },
        },
        nil,
    )

    service := features.NewService(provider, logger)

    userCtx := features.NewUserContext("test-user")
    enabled := service.IsEnabled(context.Background(), "test-flag", userCtx)

    assert.True(t, enabled)
}
```

## Performance Considerations

- **Caching**: Enable caching for production (5-minute TTL recommended)
- **Refresh Interval**: 30 seconds for real-time, 5 minutes for less critical
- **User Context**: Only include necessary attributes
- **Hot Paths**: Cache flag values for high-traffic code paths

## Migration from Hardcoded Flags

```go
// Before
const enableNewFeature = true

if enableNewFeature {
    // ...
}

// After
if service.IsEnabled(ctx, "new-feature", userCtx) {
    // ...
}
```

## License

MIT License - see LICENSE file for details
