<p align="center">
  <img src="assets/readme-banner.svg" alt="Forge Security, HTTP security middleware for Go services: headers, rate limiting, CORS, CSRF, JWT, sessions and password hashing" width="100%">
</p>

# Forge Security: HTTP security middleware for Go

Forge Security is the security extension for [Forge](https://github.com/xraph/forge), the Go backend framework. One extension gives your service security headers, per-IP rate limiting, CORS, CSRF protection, JWT issuing and verification, cookie sessions, API key checks, Argon2id and bcrypt password hashing, and audit logging. You switch each piece on with an option, and the ones you mark as auto-applied wrap every route in your app.

[![Go 1.26+](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/dl/) [![Go CI](https://github.com/xraph/forge/actions/workflows/go.yml/badge.svg)](https://github.com/xraph/forge/actions/workflows/go.yml) [![Go Reference](https://pkg.go.dev/badge/github.com/xraph/forge/extensions/security.svg)](https://pkg.go.dev/github.com/xraph/forge/extensions/security) [![Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](../../LICENSE)

[Documentation](https://www.xraph.com/docs/forge/extensions/security/features) · [Quick start](#quick-start) · [Features](#features) · [Middleware](#global-and-route-middleware) · [JWT](#jwt) · [Sessions](#sessions-and-cookies) · [Passwords](#password-hashing) · [Configuration](#configuration) · [FAQ](#faq)

## Quick start

You need [Go 1.26 or later](https://go.dev/dl/). Add Forge and the extension:

```bash
go get github.com/xraph/forge@latest
go get github.com/xraph/forge/extensions/security@latest
```

Save this as `main.go`. It adds security headers and a five-requests-a-minute rate limit to every route, issues a JWT from `/token`, and protects `/me` with that token and the `admin` role:

```go
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/security"
)

func main() {
	sec := security.NewExtension(
		security.WithJWTSigningKey(os.Getenv("JWT_SIGNING_KEY")),
		security.WithAutoApplySecurityHeaders(true),
		security.WithRateLimitPerIP(5, time.Minute),
		security.WithAutoApplyRateLimit(true),
	)

	app := forge.NewApp(forge.AppConfig{
		Name:        "security-demo",
		HTTPAddress: ":8080",
		Extensions:  []forge.Extension{sec},
	})

	// The JWT manager exists once the extension has registered.
	err := app.RegisterHookFn(forge.PhaseAfterRegister, "routes", func(_ context.Context, app forge.App) error {
		jwt := sec.JWTManager()
		router := app.Router()

		err := router.POST("/token", func(ctx forge.Context) error {
			token, err := jwt.GenerateToken(&security.JWTClaims{
				UserID: "user-42",
				Roles:  []string{"admin"},
			})
			if err != nil {
				return err
			}

			return ctx.JSON(http.StatusOK, map[string]string{"token": token})
		})
		if err != nil {
			return err
		}

		return router.GET("/me", func(ctx forge.Context) error {
			claims, ok := security.GetJWTClaims(ctx)
			if !ok {
				return forge.Unauthorized("no token")
			}

			return ctx.JSON(http.StatusOK, map[string]any{
				"user":  claims.UserID,
				"roles": claims.Roles,
			})
		}, forge.WithMiddleware(security.JWTMiddleware(jwt), security.RequireRoles("admin")))
	})
	if err != nil {
		log.Fatal(err)
	}

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
```

Run it with a signing key, then get a token and use it:

```bash
go mod tidy
JWT_SIGNING_KEY=a-long-random-secret-for-local-testing go run .

TOKEN=$(curl -s -X POST http://localhost:8080/token | jq -r .token)
curl -i -H "Authorization: Bearer $TOKEN" http://localhost:8080/me
# HTTP/1.1 200 OK
# X-Content-Type-Options: nosniff
# X-Frame-Options: SAMEORIGIN
# X-Ratelimit-Limit: 5
# {"roles":["admin"],"user":"user-42"}

curl http://localhost:8080/me
# Unauthorized
```

Call it a few more times and you'll get `429 Rate limit exceeded` with a `Retry-After` header. The limit counts every request from your IP, the token call included.

Leave out `JWT_SIGNING_KEY` and the app refuses to start. That is the default behaviour: JWT is enabled out of the box, and an HMAC signing method needs a key, so a bare `security.NewExtension()` fails with "signing key is required for HMAC signing methods". Pass `WithJWTSigningKey`, or `WithJWTEnabled(false)` if you don't need tokens.

`JWTMiddleware` stores the verified claims with `ctx.Set`. `RequireRoles` reads them further down the chain, and the handler reads them with `security.GetJWTClaims(ctx)`.

## Features

| You need | Forge Security gives you |
| --- | --- |
| Security headers on every response | `X-Frame-Options`, `X-Content-Type-Options`, HSTS, `Referrer-Policy` and more, with a stricter `WithSecureHeaders()` preset and a CSP builder |
| Rate limiting in Go | An in-memory token bucket per client IP, with `X-Ratelimit-*` and `Retry-After` headers |
| CORS for a browser front end | Allowed origins (including `*.example.com` patterns), methods, headers, credentials, preflight caching and private network access |
| CSRF protection | A double-submit token in a cookie and the `X-CSRF-Token` header, checked on unsafe methods |
| JWT authentication | `JWTManager` to issue and verify HMAC-signed tokens, `JWTMiddleware`, and `RequireRoles` |
| Cookie sessions | An in-memory session store, `CreateSession`, `SessionMiddleware`, `RequireSession` and `DestroySession` |
| API keys | `APIKeyManager` to create, validate, revoke and list keys, `APIKeyMiddleware`, and `RequireScopes` |
| Password storage | Argon2id (default) or bcrypt through `PasswordHasher`, plus strength checks and rehash detection |
| An audit trail | Structured audit entries for requests and for auth, data, admin and security events, with sensitive headers and fields redacted |

Every manager is registered in the Vessel container, so you can inject `*security.PasswordHasher`, `*security.JWTManager`, `*security.APIKeyManager` and the rest by type. The extension also has accessors (`sec.JWTManager()`, `sec.SessionStore()`, `sec.CookieManager()` and so on) that return them once it has registered.

## Global and route middleware

Each feature has an auto-apply switch. When it's on, the extension hands that middleware to Forge, and Forge installs it in front of every route, including `/_/health` and `/_/metrics`.

| Feature | Auto-apply option | Middleware you can use per route |
| --- | --- | --- |
| Security headers | `WithAutoApplySecurityHeaders(true)` | `SecurityHeadersMiddleware(manager)` |
| Rate limiting | `WithAutoApplyRateLimit(true)` | `RateLimitMiddleware(limiter)` |
| CORS | `WithAutoApplyCORS(true)` | `CORSMiddleware(manager)` |
| CSRF | `WithAutoApplyCSRF(true)` | `CSRFMiddleware(csrf, cookieManager)` |
| JWT | `WithAutoApplyJWT(true)` | `JWTMiddleware(jwtManager)`, then `RequireRoles(...)` |
| API keys | `WithAutoApplyAPIKey(true)` | `APIKeyMiddleware(manager)`, then `RequireScopes(...)` |
| Audit | `WithAutoApplyAudit(true)` | `AuditMiddleware(auditLogger)` |
| Sessions | `WithAutoApplyMiddleware(true)` | `SessionMiddleware(opts)`, then `RequireSession(...)` or `RequireSessionSimple()` |

Global middleware runs before route middleware. What it stores with `ctx.Set` (the session, JWT claims, API key info) is visible to your route middleware and your handler, so an auto-applied `JWTMiddleware` and a handler that calls `security.GetJWTClaims(ctx)` work together. None of these middlewares put their values in the request context, though. Read them through the `forge.Context`, not `ctx.Context()`.

The rate limiter keys on `X-Real-IP`, then the first `X-Forwarded-For` address, then the connection's address. Those headers are whatever the client sends, so put the service behind a proxy that overwrites them before you rely on the limit.

## JWT

`JWTManager.GenerateToken(&security.JWTClaims{...})` fills in the issuer, audience, issued-at, not-before, a 1-hour expiry (`TTL`) and a random token ID, then signs. `ValidateToken` checks the signature, the time claims, and the issuer and audience when you've configured them. `GenerateRefreshToken(userID)` issues a token with `RefreshTTL` (seven days by default).

`JWTClaims` carries `UserID`, `Username`, `Email`, `Roles` and `Metadata` on top of the registered claims. `JWTMiddleware` reads the token from `Authorization: Bearer ...` by default. Set `TokenLookup` in `JWTConfig` to a comma-separated list such as `header:Authorization,query:token,cookie:jwt` to look elsewhere.

The signing key is used as raw bytes, so in practice you use the HMAC methods: `HS256` (the default), `HS384` or `HS512`. Keep the key out of your source and pass it in from the environment or a secrets store.

## Sessions and cookies

Sessions live in the in-memory store. `WithSessionStore("redis")` passes validation but fails at registration with "redis session store not yet implemented", so for now sessions don't survive a restart or span replicas.

Create a session at sign-in, then protect routes with the session middleware on the route itself:

```go
err := router.POST("/login", func(ctx forge.Context) error {
	session, err := security.CreateSession(ctx.Context(), ctx.Response(), "user-42",
		sec.SessionStore(), sec.CookieManager(),
		security.SessionConfig{CookieName: "forge_session", TTL: 24 * time.Hour},
		map[string]any{"plan": "pro"},
	)
	if err != nil {
		return err
	}

	return ctx.JSON(http.StatusOK, map[string]string{"session": session.ID})
})

sessions := security.SessionMiddleware(security.SessionMiddlewareOptions{
	Store:         sec.SessionStore(),
	CookieManager: sec.CookieManager(),
	Config:        security.SessionConfig{CookieName: "forge_session", TTL: 24 * time.Hour},
})

err = router.GET("/account", func(ctx forge.Context) error {
	session, ok := security.GetSessionFromForgeContext(ctx)
	if !ok {
		return forge.Unauthorized("sign in first")
	}

	plan, _ := session.GetData("plan")

	return ctx.JSON(http.StatusOK, map[string]any{"user": session.UserID, "plan": plan})
}, forge.WithMiddleware(sessions, security.RequireSessionSimple()))
```

`SessionMiddleware` stores the session with `ctx.Set`, so `security.GetSessionFromForgeContext(ctx)` finds it. `security.GetSession(ctx.Context())` reads the request context under `security.SessionContextKey{}`, and nothing in the extension puts it there.

Like the JWT manager, `sec.SessionStore()` and `sec.CookieManager()` are only set after the extension registers, so define these routes in a `forge.PhaseAfterRegister` hook. Cookies default to `Secure`, `HttpOnly` and `SameSite=Lax`. A browser won't send a `Secure` cookie over plain HTTP, so during local development you'll want `WithCookieSecure(false)`.

## Password hashing

```go
hasher, err := forge.Inject[*security.PasswordHasher](app.Container())
if err != nil {
	return err
}

hash, err := hasher.Hash(plain)       // Argon2id by default
ok, err := hasher.Verify(plain, hash) // works for Argon2id and bcrypt hashes
stale, _ := hasher.NeedsRehash(hash)  // true when the cost parameters differ from the config
```

The default is Argon2id with 64 MiB of memory, 3 iterations and 4 lanes. `WithSecurePasswordHasher()` raises that to 256 MiB, 4 iterations and 8 lanes. Set `Algorithm: "bcrypt"` in `PasswordHasherConfig` if you need bcrypt (cost 12 by default). `CheckPasswordStrength`, `ValidatePassword` and `GenerateRandomPassword` cover the usual sign-up checks.

## API keys, CSRF, CORS and audit

API keys are held in memory by `APIKeyManager`. Create them at startup or from your own admin endpoint, then guard routes:

```go
keys := sec.APIKeyManager()

_, err := keys.CreateAPIKey(&security.APIKeyInfo{
	Name:   "reporting",
	UserID: "user-42",
	Scopes: []string{"reports:read"},
})
if err != nil {
	return err
}

err = router.GET("/reports", listReports,
	forge.WithMiddleware(security.APIKeyMiddleware(keys), security.RequireScopes("reports:read")),
)
```

The middleware reads `X-API-Key` by default and rejects expired or revoked keys. If you store keys yourself, `HashAPIKey` and `VerifyAPIKeyHash` hash them with the password hasher.

CSRF protection is on by default but only auto-applied when you ask for it. On the first safe request (`GET`, `HEAD`, `OPTIONS` or `TRACE`) from a client without a token, the middleware sets a `csrf_token` cookie and returns the token in an `X-Csrf-Token` response header. Later safe requests pass straight through. Your front end sends the token back in `X-CSRF-Token` on `POST`, `PUT`, `PATCH` and `DELETE`, and a missing or wrong token gets a 403. Tokens are kept in memory for 12 hours.

CORS defaults to allowing every origin without credentials. For a browser app on another domain, name the origin and allow credentials:

```go
security.NewExtension(
	security.WithJWTEnabled(false),
	security.WithCORSOrigins([]string{"https://app.example.com"}),
	security.WithCORSAllowCredentials(true),
	security.WithAutoApplyCORS(true),
	security.WithSecureHeaders(),
	security.WithAutoApplySecurityHeaders(true),
)
```

The audit logger writes entries through the app's logger, or to `AuditConfig.Handler` if you set one. The default level, `auth`, keeps auth and security events. `WithAuditLevel("all")` records everything. You can log events yourself with `LogAuthEvent`, `LogDataEvent`, `LogAdminEvent` and `LogSecurityEvent`.

## Configuration

Configure the extension with options, or under `extensions.security` (falling back to `security`) in your config file. Your options win over the file and the file wins over the defaults, one field at a time, so `session.cookie_name: sid` in YAML renames the cookie and leaves the session TTL at 24 hours. Watch the keys inside each section. Only `session` and `cookie` have snake_case YAML tags. Every other section (`jwt`, `csrf`, `rate_limit`, `cors` and the rest) uses the Go field names, so the JWT key is `jwt.SigningKey` and `jwt.signing_key` is silently ignored.

| Area | Options |
| --- | --- |
| Whole extension | `WithEnabled`, `WithConfig`, `WithRequireConfig`. `NewExtensionWithConfig(cfg)` is `NewExtension(WithConfig(cfg))` |
| Sessions | `WithSessionEnabled`, `WithSessionStore`, `WithSessionCookieName`, `WithSessionTTL`, `WithSessionIdleTimeout`, `WithSessionAutoRenew`, `WithAutoApplyMiddleware`, `WithSkipPaths`, `WithRedisAddress`, `WithRedisPassword`, `WithRedisDB` |
| Cookies | `WithCookieEnabled`, `WithCookieSecure`, `WithCookieHttpOnly`, `WithCookieSameSite`, `WithCookiePath`, `WithCookieDomain` |
| Headers | `WithSecurityHeadersEnabled`, `WithSecurityHeadersConfig`, `WithSecureHeaders`, `WithAutoApplySecurityHeaders` |
| Rate limiting | `WithRateLimitEnabled`, `WithRateLimitConfig`, `WithRateLimitPerIP`, `WithAutoApplyRateLimit` |
| CORS | `WithCORSEnabled`, `WithCORSConfig`, `WithCORSOrigins`, `WithCORSMethods`, `WithCORSHeaders`, `WithCORSExposeHeaders`, `WithCORSAllowCredentials`, `WithCORSMaxAge`, `WithCORSMaxAgeDuration`, `WithCORSAllowPrivateNetwork`, `WithCORSSkipPaths`, `WithAutoApplyCORS` |
| CSRF | `WithCSRFEnabled`, `WithCSRFConfig`, `WithAutoApplyCSRF` |
| JWT | `WithJWTEnabled`, `WithJWTConfig`, `WithJWTSigningKey`, `WithAutoApplyJWT` |
| API keys | `WithAPIKeyEnabled`, `WithAPIKeyConfig`, `WithAutoApplyAPIKey` |
| Passwords | `WithPasswordHasherConfig`, `WithSecurePasswordHasher` |
| Audit | `WithAuditEnabled`, `WithAuditConfig`, `WithAuditLevel`, `WithAutoApplyAudit` |

| Default | Value |
| --- | --- |
| Enabled features | All of them, with every auto-apply switch off |
| Session | in-memory, cookie `forge_session`, 24-hour TTL, 30-minute idle timeout, auto-renew on |
| Rate limit | 100 requests a minute per client, skipping `/health` and `/metrics` |
| Headers | `X-Frame-Options: SAMEORIGIN`, `nosniff`, HSTS for one year with subdomains, `strict-origin-when-cross-origin` |
| JWT | `HS256`, 1-hour tokens, 7-day refresh tokens, no signing key |
| CORS | every origin, no credentials, preflight cached for an hour |

## Documentation

| Topic | Where |
| --- | --- |
| Features and usage | [Security features](https://www.xraph.com/docs/forge/extensions/security/features) |
| Configuration reference | [Security configuration](https://www.xraph.com/docs/forge/extensions/security/configuration) |
| Function reference | [Security functions](https://www.xraph.com/docs/forge/extensions/security/functions) |
| Go API | [pkg.go.dev](https://pkg.go.dev/github.com/xraph/forge/extensions/security) |
| Middleware and lifecycle hooks in Forge | [Middleware guide](../../docs/content/docs/forge/%28router%29/middleware.mdx), [lifecycle guide](../../docs/content/docs/forge/lifecycle.mdx) |

## Examples

[`examples/security-auto-middleware`](examples/security-auto-middleware/) turns on auto-applied session middleware and builds login and profile routes. Its handlers read the session with `security.GetSession(r.Context())`, and the session middleware never puts it in the request context, so they don't find it today. Use `GetSessionFromForgeContext` as the session pattern above does.

## FAQ

### How do I add rate limiting to a Go API?

Add `security.WithRateLimitPerIP(100, time.Minute)` and `security.WithAutoApplyRateLimit(true)` to the extension. Every route then gets a per-IP token bucket and a 429 when it runs out. Remember that the client IP comes from `X-Real-IP` or `X-Forwarded-For` when those are present.

### Why does my Forge app fail with "signing key is required for HMAC signing methods"?

JWT is enabled by default and has no key. Pass `security.WithJWTSigningKey(...)`, or `security.WithJWTEnabled(false)` if you don't use tokens.

### Which password hashing algorithm should I use in Go?

The default, Argon2id, is the one to pick for new systems. `Verify` reads both Argon2id and bcrypt hashes, so you can keep old bcrypt hashes working. `NeedsRehash` only compares cost parameters within the same algorithm, so if you're moving users from bcrypt to Argon2id, check for the `$2` prefix yourself and rehash after a successful sign-in.

### Can I store sessions in Redis?

Not yet. The config accepts `redis`, but registration fails until a Redis store is implemented. You can pass your own `SessionStore` implementation to `SessionMiddleware` and `CreateSession` in the meantime.

### How do I add CSRF protection to forms in a Go web app?

Turn on `WithAutoApplyCSRF(true)`. Read the token from the `X-Csrf-Token` response header (or the `csrf_token` cookie) on a `GET`, and send it back in the `X-CSRF-Token` header on every state-changing request.

## Contributing

Security is its own Go module in the [Forge repository](https://github.com/xraph/forge). Build and test it from its directory the way CI does:

```bash
cd extensions/security
GOWORK=off go build ./...
GOWORK=off go vet ./...
GOWORK=off go test -short -count=1 ./...
golangci-lint run ./...
```

[AGENTS.md](../../AGENTS.md) has the pinned lint version and the repository conventions. Commits follow [Conventional Commits](https://www.conventionalcommits.org/), for example `fix(security): ...`.

## License

Forge Security is part of Forge and is available under the [Apache License 2.0](../../LICENSE).
