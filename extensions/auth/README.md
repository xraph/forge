<p align="center">
  <img src="assets/readme-banner.svg" alt="Forge Auth, authentication providers and route middleware for Go APIs built on Forge" width="100%">
</p>

# Forge Auth: API authentication middleware for Go

Forge Auth is the authentication package for [Forge](https://github.com/xraph/forge), the Go backend framework. It gives you ready-made providers for API keys, Bearer tokens such as JWTs, HTTP Basic, OAuth 2.0, OpenID Connect and LDAP or Active Directory, a registry that chains them, and route middleware that checks scopes, roles and permissions before your handler runs. You write the part that knows your users (a validator function), and the package handles reading the credential, trying providers in order and answering 401 or 403.

[![Go 1.26+](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/dl/) [![Go CI](https://github.com/xraph/forge/actions/workflows/go.yml/badge.svg)](https://github.com/xraph/forge/actions/workflows/go.yml) [![Go Reference](https://pkg.go.dev/badge/github.com/xraph/forge/extensions/auth.svg)](https://pkg.go.dev/github.com/xraph/forge/extensions/auth) [![Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](../../LICENSE)

[Documentation](https://www.xraph.com/docs/forge/extensions/auth/features) · [Quick start](#quick-start) · [Features](#features) · [Providers](#providers) · [Protecting routes](#protecting-routes) · [Authorization](#authorization) · [The extension](#the-forge-extension) · [FAQ](#faq)

## Quick start

You need [Go 1.26 or later](https://go.dev/dl/). Add Forge and the auth module:

```bash
go get github.com/xraph/forge@latest
go get github.com/xraph/forge/extensions/auth@latest
```

Save this as `main.go`. It protects one route with an API key and a second one with an API key plus the `admin` role:

```go
package main

import (
	"context"
	"log"
	"net/http"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/auth"
	"github.com/xraph/forge/extensions/auth/providers"
)

// Demo keys. A real service looks these up in its database.
var apiKeys = map[string]auth.AuthContext{
	"alice-key": {Subject: "alice", Roles: []string{"admin"}},
	"bob-key":   {Subject: "bob", Roles: []string{"viewer"}},
}

func validateKey(_ context.Context, key string) (*auth.AuthContext, error) {
	authCtx, ok := apiKeys[key]
	if !ok {
		return nil, auth.ErrInvalidCredentials
	}

	return &authCtx, nil
}

func main() {
	app := forge.NewApp(forge.AppConfig{
		Name:        "auth-demo",
		HTTPAddress: ":8080",
	})

	registry := auth.NewRegistry(app.Container(), app.Logger())

	err := registry.Register(providers.NewAPIKeyProvider("api-key",
		providers.WithAPIKeyValidator(validateKey),
	))
	if err != nil {
		log.Fatal(err)
	}

	signedIn := registry.MiddlewareWithRequirement(auth.Requirement{
		Providers: []string{"api-key"},
	})
	adminsOnly := registry.MiddlewareWithRequirement(auth.Requirement{
		Providers: []string{"api-key"},
		Roles:     []string{"admin"},
	})

	router := app.Router()

	err = router.GET("/me", func(ctx forge.Context) error {
		authCtx, ok := auth.GetAuthContext(ctx)
		if !ok {
			return forge.Unauthorized("not signed in")
		}

		return ctx.JSON(http.StatusOK, map[string]any{
			"subject":  authCtx.Subject,
			"roles":    authCtx.Roles,
			"provider": authCtx.ProviderName,
		})
	}, forge.WithMiddleware(signedIn), forge.WithAuth("api-key"))
	if err != nil {
		log.Fatal(err)
	}

	err = router.DELETE("/cache", func(ctx forge.Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"status": "cleared"})
	}, forge.WithMiddleware(adminsOnly), forge.WithAuth("api-key"), forge.WithAnyRole("admin"))
	if err != nil {
		log.Fatal(err)
	}

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
```

Run it and try a few requests:

```bash
go mod tidy
go run .

curl -i http://localhost:8080/me
# 401 Unauthorized
curl -H "X-API-Key: alice-key" http://localhost:8080/me
# {"provider":"api-key","roles":["admin"],"subject":"alice"}
curl -i -X DELETE -H "X-API-Key: bob-key" http://localhost:8080/cache
# 403 authorization failed: requires any of roles admin
curl -X DELETE -H "X-API-Key: alice-key" http://localhost:8080/cache
# {"status":"cleared"}
```

One thing in that program is there on purpose, and you'll want it in your own code for now. The registry comes from `auth.NewRegistry`, not from the extension. `auth.NewExtension()` registers a registry constructor that asks the container for a `forge.Container`, and nothing provides one, so resolving `auth.Registry` from the container fails today with "no provider for type di.Container". [The Forge extension](#the-forge-extension) has the details.

The handler reads the subject with `auth.GetAuthContext(ctx)`. The registry middleware stores it with `ctx.Set`, and what middleware stores that way reaches the handler.

## Features

| You need | Forge Auth gives you |
| --- | --- |
| API key authentication in Go | `providers.NewAPIKeyProvider`, reading a header (`X-API-Key` by default), a query parameter or a cookie |
| JWT or other Bearer tokens | `providers.NewBearerTokenProvider` with your own token validator |
| HTTP Basic, OAuth 2.0 and OpenID Connect | `NewBasicAuthProvider`, `NewOAuth2Provider` and `NewOIDCProvider` |
| LDAP or Active Directory sign-in | `providers.NewLDAPProvider`, with StartTLS, a connection pool, a result cache and group-to-role mapping |
| More than one way to sign in | Registry middleware that tries providers in order, or requires all of them |
| Role, scope and permission checks | `auth.Requirement` and a pluggable `auth.Authorizer` |
| Documented security in your API spec | `forge.WithAuth`, `forge.WithAnyRole` and `forge.WithAllPermissions` route metadata, read by the OpenAPI and client generators |
| Your own scheme | Any type that implements `auth.AuthProvider` |

## Providers

Each provider takes a name, which is what you pass to middleware and route options, and a validator you write. Your validator gets the raw credential and returns an `*auth.AuthContext`, or an error such as `auth.ErrInvalidCredentials`. If you forget the validator, the provider returns `auth.ErrInvalidConfiguration` on every request.

| Constructor | Reads | Options |
| --- | --- | --- |
| `NewAPIKeyProvider(name, ...)` | Header, then query, then cookie | `WithAPIKeyHeader`, `WithAPIKeyQuery`, `WithAPIKeyCookie`, `WithAPIKeyValidator`, `WithAPIKeyDescription`, `WithAPIKeyContainer` |
| `NewBearerTokenProvider(name, ...)` | `Authorization: Bearer <token>` | `WithBearerFormat`, `WithBearerValidator`, `WithBearerDescription`, `WithBearerContainer` |
| `NewBasicAuthProvider(name, ...)` | `Authorization: Basic <base64>` | `WithBasicAuthValidator`, `WithBasicAuthDescription`, `WithBasicAuthContainer` |
| `NewOAuth2Provider(name, flows, ...)` | A Bearer token | `WithOAuth2Validator`, `WithOAuth2Description`, `WithOAuth2Container` |
| `NewOIDCProvider(name, discoveryURL, ...)` | A Bearer token | `WithOIDCValidator`, `WithOIDCDescription`, `WithOIDCContainer` |
| `NewLDAPProvider(cfg, logger)` | HTTP Basic credentials, checked against your directory | `LDAPConfig`, starting from `DefaultLDAPConfig()` |

The Bearer, OAuth2 and OIDC providers don't verify tokens themselves. You check the signature, expiry and audience in your validator. The OAuth2 flows and OIDC discovery URL you pass only feed the OpenAPI security scheme. Here are a few registered side by side:

```go
jwt := providers.NewBearerTokenProvider("jwt",
	providers.WithBearerFormat("JWT"),
	providers.WithBearerValidator(verifyJWT),
)
basic := providers.NewBasicAuthProvider("basic",
	providers.WithBasicAuthValidator(checkPassword),
)
oauth := providers.NewOAuth2Provider("oauth2", &auth.OAuthFlows{
	AuthorizationCode: &auth.OAuthFlow{
		AuthorizationURL: "https://id.example.com/authorize",
		TokenURL:         "https://id.example.com/token",
		Scopes:           map[string]string{"read": "Read access"},
	},
}, providers.WithOAuth2Validator(verifyJWT))

for _, p := range []auth.AuthProvider{jwt, basic, oauth} {
	if err := registry.Register(p); err != nil {
		return err
	}
}
```

### LDAP and Active Directory

```go
cfg := providers.DefaultLDAPConfig()
cfg.Host = "ldap.example.com"
cfg.BaseDN = "dc=example,dc=com"
cfg.BindDN = "cn=reader,dc=example,dc=com"
cfg.BindPassword = "from-your-secret-store"
cfg.RoleMapping = map[string]string{"cn=admins,ou=groups,dc=example,dc=com": "admin"}

ldapProvider, err := providers.NewLDAPProvider(cfg, logger)
if err != nil {
	return err
}

err = registry.Register(ldapProvider) // registered as "ldap"
```

The provider binds with your service account, searches with `SearchFilter` (`(uid=%s)` by default, `(sAMAccountName=%s)` for Active Directory), binds as the user to check the password, then looks up groups with `GroupFilter`. With an empty `RoleMapping` every group DN is passed through, and with a mapping only the mapped names are kept. Either way they land in the auth context's `Scopes`, not `Roles`, so check them with `Requirement.Scopes`. The attributes you list in `Attributes` become claims, and the user's DN and raw groups go in `Metadata`. Successful sign-ins are cached for `CacheTTL` (five minutes by default).

TLS is on by default. The connection starts plain on `Port` (389) and upgrades with StartTLS, verifying the certificate and hostname with TLS 1.2 or newer. Install your directory's certificate authority in the host trust store before you connect. `InsecureSkipVerify` is still in `LDAPConfig` so older config decodes, but setting it to `true` makes `NewLDAPProvider` return an error before any connection is attempted.

### Writing your own provider

If your scheme isn't covered, implement `auth.AuthProvider` yourself: `Name`, `Type`, `Authenticate(ctx, *http.Request)`, `OpenAPIScheme` and `Middleware`. The registry only calls `Name` and `Authenticate` on the request path, and `OpenAPIScheme` when you ask it for `OpenAPISchemes()`. You only need a real `Middleware` if you plan to use the provider on its own, outside the registry.

## Protecting routes

The registry turns provider names into `forge.Middleware`. You attach it with `forge.WithMiddleware` on a route or `forge.WithGroupMiddleware` on a group.

| Registry method | Behaviour |
| --- | --- |
| `Middleware(names...)` | Tries each provider in order and uses the first that succeeds. 401 when all fail |
| `MiddlewareAnd(names...)` | Every provider must succeed. The first one's subject is kept, later claims and scopes are merged in |
| `MiddlewareWithScopes(name, scopes...)` | One provider plus required scopes |
| `MiddlewareWithRequirement(req)` | Authenticates with `req.Providers` (`Mode: "and"` for all-of), then calls the authorizer |

A failed sign-in gets a plain-text `401 Unauthorized`. A failed check gets `403` with the authorizer's error text. After a successful sign-in, the registry sets `ProviderName` on the auth context to the name the provider was registered under.

```go
api := router.Group("/api",
	forge.WithGroupMiddleware(registry.Middleware("jwt", "api-key")),
	forge.WithGroupAuth("jwt", "api-key"),
)

deleteUser := registry.MiddlewareWithRequirement(auth.Requirement{
	Providers:   []string{"jwt"},
	Permissions: []string{"users:write", "users:delete"},
})

err := api.DELETE("/users/:id", handler,
	forge.WithMiddleware(deleteUser),
	forge.WithAllPermissions("users:write", "users:delete"),
)
```

You'll notice each rule appears twice. `forge.WithAuth`, `forge.WithRequiredAuth`, `forge.WithAuthAnd`, `forge.WithAnyRole`, `forge.WithAllPermissions` and their `WithGroup...` versions only record route metadata. The OpenAPI generator turns it into `security` requirements and an `x-forge-authz` extension, and the client generators read it too, but nothing is enforced until the registry middleware is on the route.

### Reading the subject

`auth.GetAuthContext(ctx)` returns the `*auth.AuthContext`, in later middleware and in the handler alike. The registry doesn't put it in the request context, so `auth.FromContext(ctx.Context())` finds it only if you copy it there with `auth.WithContext`, which code that takes a plain `context.Context` needs. `MiddlewareWithRequirement` also stores the subject's roles and scopes under `auth.subject.roles` and `auth.subject.scopes` for router interceptors.

`AuthContext` carries `Subject`, `Claims`, `Scopes`, `Roles`, `Permissions`, `Metadata`, `Data` and `ProviderName`, and has `HasScope`, `HasScopes`, `HasRole`, `HasAnyRole`, `HasPermission`, `HasAllPermissions`, `GetClaim` and `GetClaimString`.

## Authorization

The default authorizer does plain set membership. The subject must hold all `Scopes`, any one of the `Roles` and all `Permissions`, and a requirement with none of the three passes. There's no role hierarchy and no wildcard matching. If you need real policy, you swap the authorizer:

```go
type tenantAuthorizer struct{}

func (tenantAuthorizer) Name() string { return "tenant" }

func (tenantAuthorizer) Authorize(ctx context.Context, a *auth.AuthContext, req auth.Requirement) error {
	if tenant, _ := a.GetClaimString("tenant"); tenant == "" {
		return fmt.Errorf("%w: no tenant claim", auth.ErrAuthorizationFailed)
	}

	return auth.NewDefaultAuthorizer().Authorize(ctx, a, req)
}

registry.SetAuthorizer(tenantAuthorizer{})
```

Return `nil` to allow. Any error becomes a 403 with the error's text. Wrap `auth.ErrAuthorizationFailed` or `auth.ErrInsufficientScopes` so callers can tell the cases apart with `errors.Is`. `SetAuthorizer(nil)` is ignored, so the registry always has one. [Warden](https://github.com/xraph/warden) is built to plug in here.

### Credentials from protocol frames

For transports that receive a token after the HTTP handshake (a WebSocket or SSE control frame, for example), `auth.ParseExplicitFrameCredential` parses a bare token, `Bearer <token>` or `DPoP <token>`. Trusted transport code can then mark it with `auth.WithExplicitFrameCredential`, and a provider can check the mark with `auth.MatchesExplicitFrameCredential`. The mark stores only a SHA-256 digest and grants no identity: your provider still validates the token.

## The Forge extension

`auth.NewExtension(opts...)` is the extension entry point:

| Option | Sets |
| --- | --- |
| `WithEnabled(bool)` | `false` skips registration |
| `WithAuthorizer(auth.Authorizer)` | The authorizer the registry starts with |
| `WithProviders(...ProviderConfig)`, `WithDefaultProvider(string)` | Stored on `Config`, but `Register` doesn't read them yet |
| `WithRequireConfig(bool)`, `WithConfig(auth.Config)` | The rest of `Config`. `NewExtensionWithConfig(cfg)` is `NewExtension(WithConfig(cfg))` |

The extension doesn't read YAML. When enabled, `Register` adds an `auth.Registry` constructor under the names `auth:registry` and `auth.Registry`, and `auth.GetRegistry`, `GetRegistryFromApp` and their `Must` versions resolve it. As the quick start explains, that constructor currently fails because it needs a `forge.Container` from the container. Until it changes, build the registry with `auth.NewRegistry(app.Container(), app.Logger())` and keep a reference to it. One knock-on effect: the OpenAPI generator's lookup of `auth:registry` finds nothing, so security scheme definitions aren't added to `components` automatically, even though each operation still gets its `security` requirements.

## Errors

| Error | Meaning |
| --- | --- |
| `ErrMissingCredentials` | The request had no credential the provider recognised |
| `ErrInvalidCredentials` | The credential was malformed or your validator rejected it |
| `ErrInvalidConfiguration` | A provider without a validator, or a provider with an empty name |
| `ErrProviderExists`, `ErrProviderNotFound` | `Register` with a duplicate name, or `Get`/`Unregister` with an unknown one |
| `ErrInsufficientScopes`, `ErrAuthorizationFailed` | Returned by authorizers |
| `ErrAuthenticationFailed`, `ErrTokenExpired`, `ErrTokenInvalid` | For your validators to return |

## Documentation

| Topic | Where |
| --- | --- |
| Features and usage | [Auth features](https://www.xraph.com/docs/forge/extensions/auth/features) |
| Configuration reference | [Auth configuration](https://www.xraph.com/docs/forge/extensions/auth/configuration) |
| Function reference | [Auth functions](https://www.xraph.com/docs/forge/extensions/auth/functions) |
| Go API | [pkg.go.dev](https://pkg.go.dev/github.com/xraph/forge/extensions/auth) and [the providers package](https://pkg.go.dev/github.com/xraph/forge/extensions/auth/providers) |
| Route metadata and OpenAPI output | [Forge OpenAPI guide](../../docs/content/docs/forge/openapi.mdx) |

The docs pages and older examples resolve the registry from the container and read the auth context from the request in the handler. Neither works as written today, for the two reasons in the [quick start](#quick-start).

## Examples

[`examples/auth_example`](examples/auth_example/) registers API key, JWT, Basic and OAuth2 providers and builds OpenAPI docs for them. It predates the two issues above, so it won't run unmodified: build the registry with `auth.NewRegistry` and attach `registry.MiddlewareWithRequirement(...)` to your routes, as the quick start does.

## FAQ

### How do I add API key authentication to a Go REST API?

Register `providers.NewAPIKeyProvider("api-key", providers.WithAPIKeyValidator(yourCheck))` with a registry, then put `registry.Middleware("api-key")` on the routes you want to protect. The [quick start](#quick-start) is a complete program.

### Does Forge Auth validate JWTs for me?

No. The Bearer provider pulls the token out of the `Authorization` header and passes it to your validator. Use the JWT library you trust to check the signature, expiry and audience there, and return the subject, roles and scopes.

### Can a route accept either an API key or a JWT?

Yes. `registry.Middleware("api-key", "jwt")` tries them in order and lets the request through on the first success. `MiddlewareAnd` does the opposite and requires both.

### How do I check roles and permissions per route?

Use `registry.MiddlewareWithRequirement` with `Roles` (any one is enough) or `Permissions` (all are required). Add `forge.WithAnyRole` or `forge.WithAllPermissions` too if you want the rule in your OpenAPI document.

### Does it support LDAP or Active Directory?

Yes, through `providers.NewLDAPProvider`. Clients send HTTP Basic credentials, the provider checks them against your directory over StartTLS, and `RoleMapping` turns directory groups into the scope strings your routes check.

## Contributing

Auth is its own Go module in the [Forge repository](https://github.com/xraph/forge). Build and test it from its directory the way CI does:

```bash
cd extensions/auth
GOWORK=off go build ./...
GOWORK=off go vet ./...
GOWORK=off go test -short -count=1 ./...
golangci-lint run ./...
```

[AGENTS.md](../../AGENTS.md) has the pinned lint version and the repository conventions. Commits follow [Conventional Commits](https://www.conventionalcommits.org/), for example `fix(auth): ...`.

## License

Forge Auth is part of Forge and is available under the [Apache License 2.0](../../LICENSE).
