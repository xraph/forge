# Forge v2: Authentication Extension

Production-ready authentication system with automatic OpenAPI security scheme generation.

## Features

- ✅ **Multiple Auth Providers** - API Key, Bearer Token (JWT), OAuth2, OpenID Connect, Basic Auth
- ✅ **Auto OpenAPI Generation** - Security schemes automatically added to OpenAPI specs
- ✅ **DI Container Access** - Validators can access services for database lookups, caching, etc.
- ✅ **Flexible Configuration** - Route-level and group-level auth with OR/AND logic
- ✅ **Scope/Permission Support** - Fine-grained access control with required scopes
- ✅ **Thread-Safe** - Concurrent access to auth registry
- ✅ **Production-Ready** - Proper error handling, validation, and logging

## Quick Start

### 1. Register the Auth Extension

```go
import (
    "github.com/xraph/forge"
    "github.com/xraph/forge/extensions/auth"
    "github.com/xraph/forge/extensions/auth/providers"
)

func main() {
    app := forge.NewApp(forge.DefaultAppConfig())
    
    // Register auth extension
    authExt := auth.NewExtension()
    app.RegisterExtension(authExt)
    
    app.Run()
}
```

### 2. Register Auth Providers

```go
// Get auth registry
registry := forge.Must[auth.Registry](app.Container(), "auth:registry")

// Register API Key provider
apiKeyProvider := providers.NewAPIKeyProvider("api-key",
    providers.WithAPIKeyHeader("X-API-Key"),
    providers.WithAPIKeyValidator(func(ctx context.Context, key string) (*auth.AuthContext, error) {
        // Validate against database or cache
        if key == "secret-key-123" {
            return &auth.AuthContext{
                Subject: "user-123",
                Claims: map[string]interface{}{
                    "role": "admin",
                },
            }, nil
        }
        return nil, auth.ErrInvalidCredentials
    }),
)
registry.Register(apiKeyProvider)

// Register JWT Bearer provider
jwtProvider := providers.NewBearerTokenProvider("jwt",
    providers.WithBearerFormat("JWT"),
    providers.WithBearerValidator(func(ctx context.Context, token string) (*auth.AuthContext, error) {
        // Validate JWT token
        // Access services from DI container
        // jwtService := forge.Must[JWTService](app.Container(), "jwt")
        // claims, err := jwtService.Verify(token)
        
        return &auth.AuthContext{
            Subject: "user-from-jwt",
            Scopes:  []string{"read:users", "write:users"},
        }, nil
    }),
)
registry.Register(jwtProvider)
```

### 3. Protect Routes

```go
router := app.Router()

// Public route (no auth)
router.GET("/public", publicHandler)

// Protected route with API Key OR JWT
router.GET("/protected", protectedHandler,
    forge.WithAuth("api-key", "jwt"),
    forge.WithSummary("Protected endpoint"),
)

// Route requiring specific scopes
router.POST("/admin/users", adminHandler,
    forge.WithRequiredAuth("jwt", "write:users", "admin"),
    forge.WithSummary("Create user (Admin only)"),
)

// Group with auth (all routes inherit)
api := router.Group("/api/v1", forge.WithGroupAuth("jwt"))
{
    api.GET("/users", listUsersHandler)
    api.GET("/users/:id", getUserHandler)
    api.POST("/users", createUserHandler)
}
```

### 4. Access Auth Context in Handlers

```go
func protectedHandler(w http.ResponseWriter, r *http.Request) error {
    // Get auth context
    authCtx, ok := auth.FromContext(r.Context())
    if !ok {
        return forge.Unauthorized("not authenticated")
    }
    
    return forge.JSON(w, 200, map[string]interface{}{
        "user":     authCtx.Subject,
        "provider": authCtx.ProviderName,
        "scopes":   authCtx.Scopes,
        "claims":   authCtx.Claims,
    })
}
```

## Built-in Providers

### API Key Provider

Supports API keys in headers, query parameters, or cookies.

```go
provider := providers.NewAPIKeyProvider("api-key",
    providers.WithAPIKeyHeader("X-API-Key"),        // Header
    providers.WithAPIKeyQuery("api_key"),           // Query param
    providers.WithAPIKeyCookie("api_key"),          // Cookie
    providers.WithAPIKeyDescription("API Key Authentication"),
    providers.WithAPIKeyValidator(validateFunc),
)
```

**OpenAPI Output:**
```yaml
securitySchemes:
  api-key:
    type: apiKey
    name: X-API-Key
    in: header
```

### Bearer Token Provider (JWT)

For JWT tokens or other bearer tokens.

```go
provider := providers.NewBearerTokenProvider("jwt",
    providers.WithBearerFormat("JWT"),
    providers.WithBearerDescription("JWT Bearer Authentication"),
    providers.WithBearerValidator(validateFunc),
)
```

**OpenAPI Output:**
```yaml
securitySchemes:
  jwt:
    type: http
    scheme: bearer
    bearerFormat: JWT
```

### Basic Auth Provider

HTTP Basic Authentication.

```go
provider := providers.NewBasicAuthProvider("basic",
    providers.WithBasicAuthDescription("HTTP Basic Authentication"),
    providers.WithBasicAuthValidator(func(ctx context.Context, username, password string) (*auth.AuthContext, error) {
        // Validate credentials
        return &auth.AuthContext{Subject: username}, nil
    }),
)
```

**OpenAPI Output:**
```yaml
securitySchemes:
  basic:
    type: http
    scheme: basic
```

### OAuth2 Provider

OAuth 2.0 with configurable flows.

```go
provider := providers.NewOAuth2Provider("oauth2",
    &auth.OAuthFlows{
        AuthorizationCode: &auth.OAuthFlow{
            AuthorizationURL: "https://example.com/oauth/authorize",
            TokenURL:         "https://example.com/oauth/token",
            Scopes: map[string]string{
                "read":  "Read access",
                "write": "Write access",
            },
        },
    },
    providers.WithOAuth2Validator(validateFunc),
)
```

**OpenAPI Output:**
```yaml
securitySchemes:
  oauth2:
    type: oauth2
    flows:
      authorizationCode:
        authorizationUrl: https://example.com/oauth/authorize
        tokenUrl: https://example.com/oauth/token
        scopes:
          read: Read access
          write: Write access
```

### OpenID Connect Provider

OpenID Connect authentication.

```go
provider := providers.NewOIDCProvider("oidc",
    "https://example.com/.well-known/openid-configuration",
    providers.WithOIDCValidator(validateFunc),
)
```

## Route Options

### Single Provider (OR Logic)

```go
// Accepts either api-key OR jwt
router.GET("/protected", handler,
    forge.WithAuth("api-key", "jwt"),
)
```

**OpenAPI:**
```yaml
security:
  - api-key: []
  - jwt: []
```

### Required Scopes

```go
// Requires jwt with specific scopes
router.POST("/admin", handler,
    forge.WithRequiredAuth("jwt", "write:users", "admin"),
)
```

**OpenAPI:**
```yaml
security:
  - jwt: [write:users, admin]
```

### Multiple Providers (AND Logic)

```go
// Requires BOTH api-key AND jwt (rare use case)
router.GET("/high-security", handler,
    forge.WithAuthAnd("api-key", "jwt"),
)
```

**OpenAPI:**
```yaml
security:
  - api-key: []
    jwt: []
```

## Group Options

Apply authentication to all routes in a group:

```go
// All routes in this group require JWT
api := router.Group("/api/v1",
    forge.WithGroupAuth("jwt"),
    forge.WithGroupRequiredScopes("read"),
)

// Override group auth for specific route
api.GET("/public-in-group", handler, forge.WithAuth()) // No auth
```

## Custom Auth Providers

Implement the `AuthProvider` interface:

```go
type MyCustomProvider struct {
    name      string
    container forge.Container
}

func (p *MyCustomProvider) Name() string {
    return p.name
}

func (p *MyCustomProvider) Type() auth.SecuritySchemeType {
    return auth.SecurityTypeHTTP
}

func (p *MyCustomProvider) Authenticate(ctx context.Context, r *http.Request) (*auth.AuthContext, error) {
    // Your custom authentication logic
    // Access DI container services via p.container
    
    return &auth.AuthContext{
        Subject: "user-id",
        Claims:  map[string]interface{}{"custom": "data"},
    }, nil
}

func (p *MyCustomProvider) OpenAPIScheme() auth.SecurityScheme {
    return auth.SecurityScheme{
        Type:        auth.SecurityTypeHTTP,
        Description: "My custom authentication",
        Scheme:      "custom",
    }
}

func (p *MyCustomProvider) Middleware() forge.Middleware {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            authCtx, err := p.Authenticate(r.Context(), r)
            if err != nil {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
            }
            ctx := auth.WithContext(r.Context(), authCtx)
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}

// Register it
registry.Register(&MyCustomProvider{name: "custom"})
```

## AuthContext API

The `AuthContext` contains authenticated user information:

```go
type AuthContext struct {
    Subject      string                 // User/service ID
    Claims       map[string]interface{} // Additional claims
    Scopes       []string               // Permissions/scopes
    Metadata     map[string]interface{} // Provider-specific data
    ProviderName string                 // Which provider authenticated
}

// Helper methods
authCtx.HasScope("admin")              // Check single scope
authCtx.HasScopes("read", "write")     // Check multiple scopes
authCtx.GetClaim("email")              // Get claim value
authCtx.GetClaimString("role")         // Get string claim
```

## DI Container Access

Validators have access to the DI container for service lookup:

```go
// Create provider with container access
apiKeyProvider := providers.NewAPIKeyProvider("api-key",
    providers.WithAPIKeyContainer(app.Container()),
    providers.WithAPIKeyValidator(func(ctx context.Context, apiKey string) (*auth.AuthContext, error) {
        // Access services from container
        db, err := database.GetDatabase(app.Container())
        if err != nil {
            return nil, err
        }
        cache := forge.Must[Cache](app.Container(), "cache")
        
        // Check cache first
        if user, found := cache.Get(ctx, "apikey:"+apiKey); found {
            return user.(*auth.AuthContext), nil
        }
        
        // Query database
        user, err := db.FindUserByAPIKey(ctx, apiKey)
        if err != nil {
            return nil, auth.ErrInvalidCredentials
        }
        
        // Cache for next time
        authCtx := &auth.AuthContext{
            Subject: user.ID,
            Claims:  map[string]interface{}{"email": user.Email},
        }
        cache.Set(ctx, "apikey:"+apiKey, authCtx, 5*time.Minute)
        
        return authCtx, nil
    }),
)
```

## Error Handling

The auth extension provides standard errors:

```go
var (
    ErrProviderNotFound     = errors.New("auth provider not found")
    ErrProviderExists       = errors.New("auth provider already exists")
    ErrInvalidConfiguration = errors.New("invalid auth configuration")
    ErrMissingCredentials   = errors.New("missing authentication credentials")
    ErrInvalidCredentials   = errors.New("invalid credentials")
    ErrInsufficientScopes   = errors.New("insufficient scopes")
    ErrAuthenticationFailed = errors.New("authentication failed")
    ErrAuthorizationFailed  = errors.New("authorization failed")
    ErrTokenExpired         = errors.New("token expired")
    ErrTokenInvalid         = errors.New("invalid token")
)
```

## Testing

Mock providers for testing:

```go
type MockAuthProvider struct{}

func (m *MockAuthProvider) Name() string { return "mock" }
func (m *MockAuthProvider) Type() auth.SecuritySchemeType { return auth.SecurityTypeAPIKey }

func (m *MockAuthProvider) Authenticate(ctx context.Context, r *http.Request) (*auth.AuthContext, error) {
    return &auth.AuthContext{Subject: "test-user"}, nil
}

// ... implement other methods

// In tests
registry.Register(&MockAuthProvider{})
```

## Configuration

Load configuration from ConfigManager:

```go
// config.yaml
extensions:
  auth:
    enabled: true
    default_provider: "jwt"
    providers:
      - name: "api-key"
        type: "apiKey"
        enabled: true

// Create extension with config loading
authExt := auth.NewExtension(auth.WithRequireConfig(true))
```

## Best Practices

### 1. **Use Appropriate Auth for Use Case**

- **API Keys** - Machine-to-machine, service accounts
- **JWT Bearer** - User sessions, SPAs, mobile apps
- **OAuth2** - Third-party integrations
- **Basic Auth** - Simple internal tools, development

### 2. **Validate Thoroughly**

```go
providers.WithBearerValidator(func(ctx context.Context, token string) (*auth.AuthContext, error) {
    // 1. Verify signature
    // 2. Check expiration
    // 3. Validate issuer/audience
    // 4. Check revocation list
    // 5. Verify scopes/permissions
    
    return authCtx, nil
})
```

### 3. **Use Scopes for Fine-Grained Control**

```go
// Define clear scope hierarchy
const (
    ScopeReadUsers  = "read:users"
    ScopeWriteUsers = "write:users"
    ScopeAdmin      = "admin"
)

router.POST("/users", handler,
    forge.WithRequiredAuth("jwt", ScopeWriteUsers),
)
```

### 4. **Cache Validation Results**

```go
// Cache JWT validation results by token
cache.Set(ctx, "jwt:"+tokenHash, authCtx, tokenTTL)
```

### 5. **Log Authentication Events**

```go
logger.Info("authentication succeeded",
    "provider", authCtx.ProviderName,
    "subject", authCtx.Subject,
    "ip", r.RemoteAddr,
)
```

### 6. **Use Rate Limiting**

Combine with rate limiting middleware to prevent brute force attacks.

## Examples

See [examples/auth_example](../../examples/auth_example) for a complete working example with:

- Multiple auth providers
- Route and group protection
- Scope validation
- OpenAPI documentation
- DI container integration

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        Auth Extension                        │
├─────────────────────────────────────────────────────────────┤
│                                                               │
│  ┌───────────────┐          ┌──────────────────────────┐   │
│  │   Registry    │          │    Auth Providers        │   │
│  │               │◄─────────┤  - API Key               │   │
│  │ - Register    │          │  - Bearer Token (JWT)    │   │
│  │ - Get         │          │  - Basic Auth            │   │
│  │ - Middleware  │          │  - OAuth2                │   │
│  │ - OpenAPI     │          │  - OpenID Connect        │   │
│  └───────┬───────┘          │  - Custom                │   │
│          │                  └──────────────────────────┘   │
│          │                                                   │
│          ▼                                                   │
│  ┌───────────────────────────────────────────────────────┐ │
│  │              Router Integration                        │ │
│  │  - WithAuth()                                          │ │
│  │  - WithRequiredAuth()                                  │ │
│  │  - WithGroupAuth()                                     │ │
│  └───────────────────────────────────────────────────────┘ │
│          │                                                   │
│          ▼                                                   │
│  ┌───────────────────────────────────────────────────────┐ │
│  │          OpenAPI Generator Integration                 │ │
│  │  - Auto-generates security schemes                     │ │
│  │  - Adds security requirements to routes                │ │
│  └───────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

## License

Part of the Forge v2 framework.

