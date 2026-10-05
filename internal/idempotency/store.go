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
	"errors"
	"net/http"
	"time"
)

// DefaultLease is the lease a Store applies when Begin is called with a lease
// of zero or less. A claim is never retakeable the instant it is made.
const DefaultLease = time.Minute

// ErrNotHolder is returned by Complete and Release when the caller's Token does
// not match the claim currently on the key. It means the caller's lease lapsed
// and the key moved on, so the caller's outcome must not be recorded. The store
// is left untouched.
var ErrNotHolder = errors.New("idempotency: caller does not hold the key")

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

// Token proves which Begin holds a key. A Store hands one out when it returns
// Acquired and checks it again on Complete and Release, so a holder whose lease
// lapsed cannot finish a claim that has since passed to somebody else. Treat it
// as opaque. The zero Token means "no claim".
type Token uint64

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
	// Token is set for Acquired. Pass it to Complete or Release.
	Token Token
	// Done is closed when the holder completes or releases the key, or when
	// the store sweeps the holder's lapsed claim. Set for InFlight by stores
	// that can signal; nil means poll Begin again. A store is not required to
	// close Done at the instant a lease expires, only at its next sweep, so a
	// waiter must bound its wait by the lease and Begin again when that runs
	// out.
	Done <-chan struct{}
}

// Store remembers responses by Key. Implementations must be safe for
// concurrent use.
type Store interface {
	// Begin claims key for a new request, or reports why it cannot. lease
	// bounds how long a claim survives a holder that never finishes; a lease of
	// zero or less means DefaultLease. An Acquired result carries the Token the
	// holder must present when it finishes.
	Begin(ctx context.Context, key Key, fingerprint string, lease time.Duration) (Begun, error)
	// Complete stores resp for key and ends the claim held under token. When
	// resp has no Fingerprint it takes the one the claim was begun with. A
	// token that does not match the current claim returns ErrNotHolder and
	// stores nothing. The zero token completes a key that nobody has claimed,
	// storing resp without a prior Begin; it cannot finish a live claim.
	Complete(ctx context.Context, key Key, token Token, resp Response) error
	// Release ends the claim held under token without storing anything, so the
	// key can be used again. A token that does not match the current claim
	// returns ErrNotHolder and changes nothing. Releasing a key that is gone is
	// a no-op.
	Release(ctx context.Context, key Key, token Token) error
}
