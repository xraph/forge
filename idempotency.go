package forge

import (
	"github.com/xraph/forge/internal/idempotency"
	"github.com/xraph/forge/internal/router"
)

// IdempotencyOption configures the idempotency middleware a route or group
// installs. It is the same type as middleware.IdempotencyOption, so the
// options in package middleware (middleware.IdempotencyTTL,
// middleware.IdempotencyOnConflict, middleware.IdempotencyAllowAnonymous and
// the rest) can be passed here directly.
type IdempotencyOption = idempotency.Option

// IdempotencyStore is where replayable responses are kept. It is the same
// type as middleware.IdempotencyStore.
type IdempotencyStore = idempotency.Store

// IdempotencyBackend selects the store. Without it, every route shares one
// process-wide in-memory store, which is right for a single instance and
// wrong behind a load balancer: use a shared store there.
func IdempotencyBackend(store IdempotencyStore) IdempotencyOption {
	return idempotency.WithStore(store)
}

// WithIdempotency makes a write route safe to retry. A repeated request with
// the same Idempotency-Key, from the same principal, to the same METHOD and
// path, gets the stored response instead of running the handler again. It
// also marks the operation x-forge-idempotent in the OpenAPI document, which
// tells generated clients that an offline outbox may resend the write after
// an uncertain failure.
//
// What the middleware does:
//
//   - A repeat with a different body gets 422.
//   - A repeat that arrives while the first is still running waits for it, or
//     gets 409 with Retry-After: 1 when the route uses
//     middleware.IdempotencyOnConflict(middleware.IdempotencyReject).
//   - A handler that returns an error, panics, or answers 408, 429 or 5xx
//     stores nothing, so the client's retry runs the handler again. Every
//     other status is stored and replayed until the TTL (24h by default).
//   - A request with no principal is not deduplicated: it runs as if it had
//     no key. Every anonymous caller shares the empty principal, so one could
//     otherwise be handed another's response. Pass
//     middleware.IdempotencyAllowAnonymous() to opt in on a route that has no
//     auth.
//   - GET, HEAD, OPTIONS and writes without the header pass straight through.
//
// The principal comes from the request context, so put any route option that
// authenticates before this one. Options apply in the order given.
//
// Example:
//
//	router.POST("/orders", createOrder, forge.WithIdempotency())
//	router.POST("/payments", pay, forge.WithIdempotency(
//	    forge.IdempotencyBackend(redisStore),
//	    middleware.IdempotencyTTL(48*time.Hour),
//	))
func WithIdempotency(opts ...IdempotencyOption) RouteOption {
	return router.WithIdempotent(idempotency.Middleware(nil, opts...))
}

// WithGroupIdempotency is WithIdempotency for every route in a group. The
// OpenAPI document marks only the group's writes (POST, PUT, PATCH, DELETE),
// because the middleware leaves reads alone.
func WithGroupIdempotency(opts ...IdempotencyOption) GroupOption {
	return router.WithGroupIdempotent(idempotency.Middleware(nil, opts...))
}
