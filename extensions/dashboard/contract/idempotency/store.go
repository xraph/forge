package idempotency

import (
	"context"
	"encoding/json"
	"time"
)

// Store deduplicates command invocations by (key, identity) tuple.
// Lookup returns a cached envelope if one is present and unexpired; the
// dispatcher writes back the cached envelope verbatim when found.
// Implementations MUST be safe for concurrent use.
type Store interface {
	Lookup(ctx context.Context, key, identity string) (*Cached, bool)
	Store(ctx context.Context, key, identity string, c Cached) error
}

// Cached is one cached command response.
type Cached struct {
	// Status is the HTTP status the original handler returned.
	Status int
	// WireBody is the JSON envelope the original handler produced, ready to
	// write back verbatim.
	WireBody json.RawMessage
	// StoredAt is when this entry landed in the store.
	StoredAt time.Time
	// TTL is how long the entry is considered fresh.
	TTL time.Duration
}

// Expired reports whether c is past its TTL relative to now.
func (c Cached) Expired(now time.Time) bool {
	if c.TTL <= 0 {
		return false
	}
	return now.After(c.StoredAt.Add(c.TTL))
}
