package middleware

import (
	"time"

	forge "github.com/xraph/forge"
	"github.com/xraph/forge/internal/idempotency"
)

// IdempotencyStore remembers one response per Idempotency-Key. Begin claims a
// key or reports a stored or running request, Complete stores a response,
// Release gives the key back. Implementations must be safe for concurrent use.
type IdempotencyStore = idempotency.Store

// IdempotencyKey identifies one idempotent operation: who, where, which key.
type IdempotencyKey = idempotency.Key

// IdempotentResponse is a stored response.
type IdempotentResponse = idempotency.Response

// IdempotencyBegun is what IdempotencyStore.Begin found.
type IdempotencyBegun = idempotency.Begun

// IdempotencyToken proves which Begin holds a key. A store hands one out with
// IdempotencyAcquired and checks it on Complete and Release, so a holder whose
// lease lapsed cannot finish a claim that has passed to another request. A
// custom IdempotencyStore needs this name to declare Complete and Release.
type IdempotencyToken = idempotency.Token

// ErrIdempotencyNotHolder is what Complete and Release return when the token
// does not match the claim on the key. The store must leave the key untouched.
var ErrIdempotencyNotHolder = idempotency.ErrNotHolder

// IdempotencyState is the State in IdempotencyBegun.
type IdempotencyState = idempotency.State

const (
	// IdempotencyAcquired means the caller now holds the key.
	IdempotencyAcquired = idempotency.Acquired
	// IdempotencyReplay means a stored response exists.
	IdempotencyReplay = idempotency.Replay
	// IdempotencyInFlight means another request holds the key.
	IdempotencyInFlight = idempotency.InFlight
)

// IdempotencyOption configures Idempotency and forge.WithIdempotency.
type IdempotencyOption = idempotency.Option

// IdempotencyConflictMode selects waiting or 409 for a concurrent duplicate.
type IdempotencyConflictMode = idempotency.ConflictMode

const (
	// IdempotencyWait makes a concurrent duplicate wait, then replay.
	IdempotencyWait = idempotency.ConflictWait
	// IdempotencyReject answers a concurrent duplicate with 409.
	IdempotencyReject = idempotency.ConflictReject
)

// IdempotencyPrincipalFunc names who made a request.
type IdempotencyPrincipalFunc = idempotency.PrincipalFunc

// IdempotencyKeyHeader is the request header the middleware reads.
const IdempotencyKeyHeader = idempotency.HeaderName

// IdempotentReplayedHeader is set to "true" on a replayed response.
const IdempotentReplayedHeader = idempotency.ReplayedHeader

// DefaultIdempotencyMaxEntries is the in-memory store's default cap.
const DefaultIdempotencyMaxEntries = idempotency.DefaultMaxEntries

// MemoryIdempotencyStore is the in-memory IdempotencyStore.
type MemoryIdempotencyStore = idempotency.MemoryStore

// MemoryIdempotencyOption configures a MemoryIdempotencyStore.
type MemoryIdempotencyOption = idempotency.MemoryOption

// Idempotency runs a write handler at most once per principal, METHOD /path
// and Idempotency-Key, and replays the stored response to every repeat.
//
// A repeat with a different body gets 422. A repeat that arrives while the
// first is still running waits for it (or gets 409 with
// IdempotencyOnConflict(IdempotencyReject)). A handler that returns an error,
// panics or answers 5xx stores nothing, so the client's retry runs it again.
// GET, HEAD and OPTIONS, and writes without the header, pass straight through.
//
// The handler runs on a fresh context that shares the outer context's values
// and session, so ctx.Get and ctx.Session work behind this middleware. Other
// private state of the outer context, such as a DI scope it opened, is not
// carried across.
//
// Prefer forge.WithIdempotency on the route: it installs this middleware and
// also marks the operation x-forge-idempotent so generated clients know a
// replay is safe.
func Idempotency(store IdempotencyStore, opts ...IdempotencyOption) forge.Middleware {
	return idempotency.Middleware(store, opts...)
}

// NewMemoryIdempotencyStore returns an in-memory IdempotencyStore.
func NewMemoryIdempotencyStore(opts ...MemoryIdempotencyOption) *MemoryIdempotencyStore {
	return idempotency.NewMemoryStore(opts...)
}

// MemoryIdempotencyMaxEntries caps how many responses the store keeps.
func MemoryIdempotencyMaxEntries(n int) MemoryIdempotencyOption { return idempotency.WithMaxEntries(n) }

// MemoryIdempotencyClock replaces the store's clock, for tests.
func MemoryIdempotencyClock(now func() time.Time) MemoryIdempotencyOption {
	return idempotency.WithClock(now)
}

// IdempotencyTTL is how long a response is replayed. Default 24h.
func IdempotencyTTL(d time.Duration) IdempotencyOption { return idempotency.TTL(d) }

// IdempotencyLease bounds how long an unfinished request holds its key. Default 1m.
func IdempotencyLease(d time.Duration) IdempotencyOption { return idempotency.Lease(d) }

// IdempotencyWaitTimeout bounds how long a concurrent duplicate waits. Default 10s.
func IdempotencyWaitTimeout(d time.Duration) IdempotencyOption { return idempotency.WaitTimeout(d) }

// IdempotencyOnConflict selects waiting or 409 for a concurrent duplicate.
func IdempotencyOnConflict(mode IdempotencyConflictMode) IdempotencyOption {
	return idempotency.OnConflict(mode)
}

// IdempotencyPrincipal replaces DefaultIdempotencyPrincipal.
func IdempotencyPrincipal(fn IdempotencyPrincipalFunc) IdempotencyOption {
	return idempotency.Principal(fn)
}

// IdempotencyRequireKey answers 400 to a write without an Idempotency-Key.
func IdempotencyRequireKey() IdempotencyOption { return idempotency.RequireKey() }

// IdempotencyMaxBody caps the request body hashed for the fingerprint. Default 1 MiB.
func IdempotencyMaxBody(n int64) IdempotencyOption { return idempotency.MaxBody(n) }

// IdempotencyMaxResponse caps the response body stored for replay. Default 1 MiB.
func IdempotencyMaxResponse(n int) IdempotencyOption { return idempotency.MaxResponse(n) }

// IdempotencyClock replaces the middleware's clock, for tests.
func IdempotencyClock(now func() time.Time) IdempotencyOption { return idempotency.Clock(now) }

// DefaultIdempotencyPrincipal reads "auth.subject", then the Subject of
// "auth_context", and returns "" for an anonymous request.
func DefaultIdempotencyPrincipal(ctx forge.Context) string { return idempotency.DefaultPrincipal(ctx) }
