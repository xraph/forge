// Package idempotency implements Idempotency-Key handling for forge routes: a
// Store that remembers one response per key, an in-memory Store, and the
// middleware that replays a stored response instead of running a handler
// twice.
//
// It lives under internal so that both the forge package (forge.WithIdempotency)
// and the public middleware package can build on it. middleware imports forge,
// so forge cannot import middleware; both can import this.
package idempotency

import (
	"context"
	"net/http"
	"time"
)

// Key identifies one idempotent operation.
type Key struct {
	// Principal is who made the request. Two principals never share a stored
	// response, even when they send the same key value.
	Principal string
	// Scope is where the key applies. The HTTP middleware uses "METHOD /path";
	// the dashboard contract uses its own constant.
	Scope string
	// Value is the client-supplied Idempotency-Key.
	Value string
}

// Response is one stored outcome, written back verbatim on a replay.
type Response struct {
	// Status is the HTTP status the handler wrote.
	Status int
	// Header holds the headers the handler set, not the ones outer middleware
	// set before it ran.
	Header http.Header
	// Body is the response body.
	Body []byte
	// Fingerprint identifies the request that produced this response, so a
	// reused key with a different request can be refused.
	Fingerprint string
	// StoredAt is when the response was stored.
	StoredAt time.Time
	// ExpiresAt is when the store may forget the response. Zero never expires.
	ExpiresAt time.Time
}

// State is what Begin found for a key.
type State int

const (
	// Acquired means nobody held the key. The caller now does and must
	// Complete or Release it.
	Acquired State = iota + 1
	// Replay means a completed response is stored for the key.
	Replay
	// InFlight means another request holds the key and has not finished.
	InFlight
)

// Begun is the result of Begin.
type Begun struct {
	// State is what Begin found.
	State State
	// Fingerprint is the fingerprint recorded by whoever began the key. Set
	// for Replay and InFlight, so a reused key can be compared.
	Fingerprint string
	// Response is set for Replay. It is a copy the caller may keep.
	Response *Response
	// Done is closed when the holder completes or releases the key. Set for
	// InFlight by stores that can signal; nil means poll Begin again.
	Done <-chan struct{}
}

// Store remembers responses by Key. Implementations must be safe for
// concurrent use.
type Store interface {
	// Begin claims key for a new request, or reports why it cannot. lease
	// bounds how long a claim survives a holder that never finishes.
	Begin(ctx context.Context, key Key, fingerprint string, lease time.Duration) (Begun, error)
	// Complete stores resp for key and ends the claim. Completing a key that
	// is not claimed stores resp anyway.
	Complete(ctx context.Context, key Key, resp Response) error
	// Release ends a claim without storing anything, so the key can be used
	// again.
	Release(ctx context.Context, key Key) error
}
